// Package http is the Gin driving adapter for application use cases.
package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"shopnext-laos/internal/platform"
	"strconv"
	"strings"
	"time"
)

const PhoneKeyHeader = "X-Phone-Verification-Key"
const TrustDeviceHeader = "X-Trust-Device-Key"
const StaffCookie = "shopnext_staff"

type Handler struct {
	Service *application.Service
	Config  platform.Config
	Log     *zap.Logger
	Router  *gin.Engine
}

func New(s *application.Service, cfg platform.Config, log *zap.Logger) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	h := &Handler{s, cfg, log, r}
	r.Use(h.middleware)
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) { h.failure(c, domain.Fail("INTERNAL_ERROR", 500)) }))
	h.routes()
	return r, nil
}
func (h *Handler) failure(c *gin.Context, err error) {
	status := 500
	code := "INTERNAL_ERROR"
	message := "request failed"
	if errors.Is(err, domain.ErrNotFound) {
		status = 404
		code = "NOT_FOUND"
		message = "resource not found"
	}
	var business *domain.Error
	if errors.As(err, &business) {
		status = business.Status
		code = business.Code
		message = business.Message
		if business.RetryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(business.RetryAfter))
		}
	}
	if status >= 500 {
		h.Log.Error("request failed", zap.String("request_id", c.GetString("request_id")), zap.String("route", c.FullPath()), zap.Int("status", status))
	}
	details := gin.H{"code": code, "message": message}
	if code == "PHONE_VERIFICATION_REQUIRED" || code == "PHONE_KEY_EXPIRED" || code == "PHONE_KEY_EXHAUSTED" || code == "PHONE_OWNERSHIP_MISMATCH" || code == "INVALID_TRUST_DEVICE_KEY" {
		details["phone_verification_required"] = true
		details["otp_request_url"] = "/api/v1/otp/request"
		details["otp_verify_url"] = "/api/v1/otp/verify"
	}
	c.AbortWithStatusJSON(status, gin.H{"error": details, "request_id": c.GetString("request_id")})
}
func (h *Handler) send(c *gin.Context, data any, err error) {
	if err != nil {
		h.failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": data})
}
func decode(c *gin.Context, out any) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return domain.Fail("INVALID_JSON", 400)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return domain.Fail("INVALID_JSON", 400)
	}
	return nil
}
func cookie(c *gin.Context, name string) string { v, _ := c.Cookie(name); return v }
func (h *Handler) setCookie(c *gin.Context, name, value string, age int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: h.Config.Env == "production", SameSite: http.SameSiteLaxMode})
}
func (h *Handler) verified(c *gin.Context) (string, error) {
	return h.Service.VerifiedPhone(c.Request.Context(), c.GetHeader(PhoneKeyHeader))
}
func actor(c *gin.Context) domain.Actor { a, _ := c.Get("actor"); v, _ := a.(domain.Actor); return v }
func (h *Handler) permission(p string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := application.Require(actor(c), p); err != nil {
			h.failure(c, err)
		}
	}
}
func (h *Handler) middleware(c *gin.Context) {
	start := time.Now()
	id := domain.Token(12)
	c.Set("request_id", id)
	c.Header("X-Request-ID", id)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
	c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	c.Header("Cache-Control", "no-store")
	if h.Config.Env == "production" {
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	origin := c.GetHeader("Origin")
	allowed := false
	for _, v := range h.Config.AllowedOrigins {
		if v == origin {
			allowed = true
		}
	}
	if origin != "" {
		c.Header("Vary", "Origin")
		if !allowed {
			h.failure(c, domain.Fail("ORIGIN_FORBIDDEN", 403))
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, X-ShopNext-CSRF, X-Phone-Verification-Key, X-Trust-Device-Key")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID, Retry-After")
	}
	if c.Request.Method == "OPTIONS" {
		c.AbortWithStatus(204)
		return
	}
	mutating := c.Request.Method != "GET" && c.Request.Method != "HEAD"
	webhook := c.Request.URL.Path == "/api/v1/webhooks/phajay"
	maintenance := c.Request.URL.Path == "/api/v1/maintenance/release-expired"
	if mutating && !webhook && !maintenance {
		if c.GetHeader("X-ShopNext-CSRF") != "1" {
			h.failure(c, domain.Fail("CSRF_HEADER_REQUIRED", 403))
			return
		}
		if c.GetHeader("Sec-Fetch-Site") == "cross-site" && !allowed {
			h.failure(c, domain.Fail("ORIGIN_FORBIDDEN", 403))
			return
		}
	}
	maxBody := int64(1024 * 1024)
	if strings.Contains(c.Request.URL.Path, "/uploads") {
		maxBody = 6 * 1024 * 1024
	}
	if webhook {
		maxBody = 64 * 1024
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)
	if strings.HasPrefix(c.Request.URL.Path, "/api/v1/admin") {
		a, err := h.Service.Actor(c.Request.Context(), cookie(c, StaffCookie))
		if err != nil {
			h.failure(c, err)
			return
		}
		c.Set("actor", a)
	}
	c.Next()
	h.Log.Info("http request", zap.String("request_id", id), zap.String("method", c.Request.Method), zap.String("route", c.FullPath()), zap.Int("status", c.Writer.Status()), zap.Duration("duration", time.Since(start)))
}
func page(c *gin.Context) (int, int, error) {
	p, limit := 1, 30
	var err error
	if raw := c.Query("page"); raw != "" {
		p, err = strconv.Atoi(raw)
		if err != nil || p < 1 || p > 1000000 {
			return 0, 0, domain.Fail("INVALID_PAGE", 400)
		}
	}
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, domain.Fail("INVALID_LIMIT", 400)
		}
	}
	return p, limit, nil
}
func (h *Handler) routes() {
	v := h.Router.Group("/api/v1")
	v.GET("/health/live", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	v.GET("/health/ready", func(c *gin.Context) {
		ctx := c.Request.Context()
		if err := h.Service.Store.Ping(ctx); err != nil {
			h.failure(c, domain.Fail("DATABASE_UNAVAILABLE", 503))
			return
		}
		if err := h.Service.Limiter.Ping(ctx); err != nil {
			h.failure(c, domain.Fail("REDIS_UNAVAILABLE", 503))
			return
		}
		h.send(c, gin.H{"status": "ready"}, nil)
	})
	v.GET("/products", func(c *gin.Context) {
		p, limit, err := page(c)
		if err != nil {
			h.failure(c, err)
			return
		}
		sort := c.DefaultQuery("sort", "newest")
		if sort != "newest" && sort != "price-asc" && sort != "price-desc" {
			h.failure(c, domain.Fail("INVALID_SORT", 400))
			return
		}
		cards, total, err := h.Service.Catalog(c.Request.Context(), c.Query("category"), c.Query("q"), sort, p, limit)
		if err != nil {
			h.failure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": cards, "pagination": gin.H{"page": p, "limit": limit, "total": total}})
	})
	v.GET("/products/:slug", func(c *gin.Context) {
		view, err := h.Service.Product(c.Request.Context(), c.Param("slug"), false)
		h.send(c, view, err)
	})
	v.GET("/products/:slug/related", func(c *gin.Context) {
		view, err := h.Service.Related(c.Request.Context(), c.Param("slug"))
		h.send(c, view, err)
	})
	v.GET("/categories", func(c *gin.Context) {
		var rows []domain.Category
		err := h.Service.Store.Find(c.Request.Context(), application.Categories, application.Query{Sort: "sort_order"}, &rows)
		h.send(c, rows, err)
	})
	v.GET("/content", func(c *gin.Context) {
		var slides, banners []domain.Content
		ctx := c.Request.Context()
		q := application.Query{Eq: map[string]any{"active": true}, Sort: "position"}
		if err := h.Service.Store.Find(ctx, application.HeroSlides, q, &slides); err != nil {
			h.failure(c, err)
			return
		}
		err := h.Service.Store.Find(ctx, application.PromoBanners, q, &banners)
		h.send(c, gin.H{"hero_slides": slides, "promo_banners": banners}, err)
	})
	v.GET("/delivery-options", func(c *gin.Context) { result, err := h.Service.Delivery(c.Request.Context()); h.send(c, result, err) })
	v.GET("/geography", func(c *gin.Context) { h.send(c, application.Geography(), nil) })
	v.GET("/payment-methods", func(c *gin.Context) {
		codes := []string{"BCEL", "JDB", "LDB", "IB", "STB", "MMONEYX"}
		h.send(c, gin.H{"provider": h.Service.Payments.Name(), "banks": codes, "hold_minutes": int(h.Config.Hold.Minutes())}, nil)
	})
	v.POST("/otp/request", func(c *gin.Context) {
		var in struct {
			Phone string `json:"phone"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		code, err := h.Service.RequestOTP(c.Request.Context(), in.Phone, c.ClientIP())
		result := gin.H{"requested": true}
		if code != "" {
			result["dev_code"] = code
		}
		h.send(c, result, err)
	})
	v.POST("/otp/verify", func(c *gin.Context) {
		var in struct {
			Phone string `json:"phone"`
			Code  string `json:"code"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		token, err := h.Service.VerifyOTP(c.Request.Context(), in.Phone, in.Code)
		if err != nil {
			h.failure(c, err)
			return
		}
		h.send(c, gin.H{"verified": true, "verification_key": token, "expires_at": time.Now().UTC().Add(application.PhoneKeyLifetime), "max_uses": application.PhoneKeyMaxUses}, nil)
	})
	v.GET("/phone-verification", func(c *gin.Context) {
		phone, err := h.verified(c)
		h.send(c, gin.H{"verified": phone != "", "phone": phone}, err)
	})
	v.DELETE("/phone-verification", func(c *gin.Context) {
		err := h.Service.ClearPhone(c.Request.Context(), c.GetHeader(PhoneKeyHeader))
		h.send(c, gin.H{"verified": false}, err)
	})
	v.POST("/orders", func(c *gin.Context) {
		var in application.Checkout
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.CheckoutWithPhoneKey(c.Request.Context(), in, c.GetHeader(PhoneKeyHeader), c.GetHeader("Idempotency-Key"), c.ClientIP())
		if err != nil {
			h.failure(c, err)
			return
		}
		c.JSON(201, gin.H{"data": result})
	})
	v.GET("/bills", func(c *gin.Context) {
		result, err := h.Service.ClientBills(c.Request.Context(), c.Query("phone"), c.GetHeader(PhoneKeyHeader))
		h.send(c, result, err)
	})
	v.GET("/bills/search", func(c *gin.Context) {
		q := strings.TrimSpace(c.Query("q"))
		if q == "" {
			h.failure(c, domain.Fail("SEARCH_REQUIRED", 400))
			return
		}
		digits := strings.TrimPrefix(domain.NormalizePhone(q), "+")
		phone := true
		for _, r := range digits {
			if r < '0' || r > '9' {
				phone = false
			}
		}
		if phone {
			result, err := h.Service.ClientBills(c.Request.Context(), q, c.GetHeader(PhoneKeyHeader))
			h.send(c, gin.H{"type": "phone", "bills": result}, err)
		} else {
			result, err := h.Service.BillSummary(c.Request.Context(), q)
			h.send(c, gin.H{"type": "bill_number", "bill": result}, err)
		}
	})
	v.GET("/bills/:bill", func(c *gin.Context) {
		result, err := h.Service.BillSummary(c.Request.Context(), c.Param("bill"))
		h.send(c, result, err)
	})
	v.GET("/bills/:bill/details", func(c *gin.Context) {
		result, err := h.Service.ClientBill(c.Request.Context(), c.Param("bill"), c.GetHeader(PhoneKeyHeader), c.GetHeader(TrustDeviceHeader), c.GetHeader("User-Agent"))
		h.send(c, result, err)
	})

	v.POST("/bills/:bill/payment-attempts", func(c *gin.Context) {
		var in struct {
			Bank string `json:"bank"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		if err := h.Service.Rate(c.Request.Context(), "retry-payment", c.Param("bill")+":"+c.ClientIP(), 5, 5*time.Minute); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.ClientCreateAttempt(c.Request.Context(), c.Param("bill"), in.Bank, c.GetHeader(PhoneKeyHeader), c.GetHeader(TrustDeviceHeader), c.GetHeader("User-Agent"))
		h.send(c, result, err)
	})
	if h.Config.Env != "production" && h.Service.Payments.Name() == "dev" {
		v.POST("/bills/:bill/simulate-payment", func(c *gin.Context) {
			h.send(c, gin.H{"simulated": true}, h.Service.ClientSimulatePayment(c.Request.Context(), c.Param("bill"), c.GetHeader(PhoneKeyHeader), c.GetHeader(TrustDeviceHeader), c.GetHeader("User-Agent")))
		})
	}
	v.POST("/webhooks/phajay", h.webhook)
	v.POST("/maintenance/release-expired", func(c *gin.Context) {
		provided := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if h.Config.MaintenanceSecret == "" || !hmac.Equal([]byte(provided), []byte(h.Config.MaintenanceSecret)) {
			h.failure(c, domain.Fail("UNAUTHENTICATED", 401))
			return
		}
		count, err := h.Service.Expire(c.Request.Context(), "")
		h.send(c, gin.H{"released": count}, err)
	})
	v.POST("/admin/auth/login", func(c *gin.Context) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		token, a, err := h.Service.Login(c.Request.Context(), in.Email, in.Password, c.ClientIP())
		if err != nil {
			h.failure(c, err)
			return
		}
		h.setCookie(c, StaffCookie, token, 8*3600)
		h.send(c, a, nil)
	})
	v.GET("/admin/auth/session", h.permission("orders.view"), func(c *gin.Context) { h.send(c, actor(c), nil) })
	v.POST("/admin/auth/logout", func(c *gin.Context) {
		err := h.Service.Logout(c.Request.Context(), cookie(c, StaffCookie))
		h.setCookie(c, StaffCookie, "", -1)
		h.send(c, gin.H{"logged_out": true}, err)
	})
	admin := v.Group("/admin")
	admin.Use(func(c *gin.Context) {
		if actor(c).ID == "" {
			h.failure(c, domain.Fail("UNAUTHENTICATED", 401))
		}
	})
	h.adminRoutes(admin)
	h.Router.GET("/media/:filename", func(c *gin.Context) {
		name := c.Param("filename")
		if !h.Service.Media.ValidateURL("/media/" + name) {
			h.failure(c, domain.Fail("NOT_FOUND", 404))
			return
		}
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.File(filepath.Join(h.Config.UploadDir, name))
	})
	v.GET("/openapi.yaml", func(c *gin.Context) {
		raw, err := os.ReadFile("docs/openapi.yaml")
		if err != nil {
			h.failure(c, err)
			return
		}
		c.Data(200, "application/yaml", raw)
	})
	h.Router.NoRoute(func(c *gin.Context) { h.failure(c, domain.Fail("NOT_FOUND", 404)) })
}
func (h *Handler) webhook(c *gin.Context) {
	if err := h.Service.Rate(c.Request.Context(), "webhook-ip", c.ClientIP(), 120, time.Minute); err != nil {
		h.failure(c, err)
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(raw) == 0 {
		h.failure(c, domain.Fail("INVALID_WEBHOOK_BODY", 400))
		return
	}
	if h.Service.Payments.Name() != "dev" || h.Config.Env == "production" {
		sig, err := hex.DecodeString(strings.TrimSpace(c.GetHeader(h.Config.WebhookHeader)))
		mac := hmac.New(sha256.New, []byte(h.Config.WebhookSecret))
		_, _ = mac.Write(raw)
		if h.Config.WebhookSecret == "" || err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
			h.failure(c, domain.Fail("INVALID_WEBHOOK_SIGNATURE", 401))
			return
		}
	}
	outcome, err := h.Service.Webhook(c.Request.Context(), raw)
	h.send(c, gin.H{"outcome": outcome}, err)
}
func (h *Handler) export(c *gin.Context) {
	if err := application.Require(actor(c), "orders.view"); err != nil {
		h.failure(c, err)
		return
	}
	q, err := adminQuery(c, application.Orders)
	if err != nil {
		h.failure(c, err)
		return
	}
	q.Sort = "created_at"
	q.Desc = true
	q.Limit = 200
	q.Offset = 0
	var initial []domain.Order
	if err := h.Service.Store.Find(c.Request.Context(), application.Orders, q, &initial); err != nil {
		h.failure(c, err)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="orders-%s.csv"`, time.Now().Format("2006-01-02")))
	_, _ = c.Writer.Write([]byte("\xef\xbb\xbf"))
	w := csv.NewWriter(c.Writer)
	w.UseCRLF = true
	_ = w.Write([]string{"billNumber", "createdAt", "recipientName", "recipientPhone", "provider", "branch", "paymentType", "paymentStatus", "orderStatus", "subtotalKip", "shippingFeeKip", "totalKip"})
	rows := initial
	for {
		for _, order := range rows {
			var providers []domain.Provider
			var branches []domain.Branch
			if err := h.Service.Store.Find(c.Request.Context(), application.Providers, application.Query{Eq: map[string]any{"id": order.ExpressProviderID}}, &providers); err != nil {
				return
			}
			if err := h.Service.Store.Find(c.Request.Context(), application.Branches, application.Query{Eq: map[string]any{"id": order.BranchID}}, &branches); err != nil {
				return
			}
			provider, branch := "", ""
			if len(providers) > 0 {
				provider = providers[0].Name
			}
			if len(branches) > 0 {
				branch = branches[0].Name
			}
			fields := []string{order.BillNumber, order.CreatedAt.Format(time.RFC3339), order.RecipientName, order.RecipientPhone, provider, branch, order.PaymentType, order.PaymentStatus, order.OrderStatus, strconv.FormatInt(order.SubtotalKip, 10), strconv.FormatInt(order.ShippingFeeKip, 10), strconv.FormatInt(order.TotalKip, 10)}
			for i, f := range fields {
				trimmed := strings.TrimLeft(f, " \t\r\n")
				if strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "@") || strings.HasPrefix(f, "\t") || strings.HasPrefix(f, "\r") {
					fields[i] = "'" + f
				}
			}
			if err := w.Write(fields); err != nil {
				return
			}
		}
		w.Flush()
		if w.Error() != nil || len(rows) < q.Limit {
			return
		}
		q.Offset += q.Limit
		if err := h.Service.Store.Find(c.Request.Context(), application.Orders, q, &rows); err != nil {
			return
		}
	}
}

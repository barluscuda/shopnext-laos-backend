package http

import (
	"github.com/gin-gonic/gin"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"strings"
)

func adminQuery(c *gin.Context, e application.Entity) (application.Query, error) {
	p, limit, err := page(c)
	if err != nil {
		return application.Query{}, err
	}
	q := application.Query{Eq: map[string]any{}, Search: strings.TrimSpace(c.Query("q")), Sort: "created_at", Desc: true, Limit: limit, Offset: (p - 1) * limit}
	allowed := map[string]bool{"id": true, "created_at": true, "updated_at": true}
	switch e {
	case application.Products:
		for _, f := range []string{"name", "base_price_kip", "status"} {
			allowed[f] = true
		}
	case application.Orders:
		delete(allowed, "id")
		delete(allowed, "updated_at")
		for _, f := range []string{"bill_number", "total_kip", "order_status", "payment_status", "recipient_name"} {
			allowed[f] = true
		}
	case application.Attempts:
		allowed["amount_kip"] = true
		allowed["expires_at"] = true
		allowed["status"] = true
	case application.PaymentEvents:
		allowed["amount_kip"] = true
		allowed["normalized_status"] = true
	case application.Categories:
		allowed["name"] = true
		allowed["sort_order"] = true
	case application.Variants:
		allowed["position"] = true
		allowed["stock_on_hand"] = true
	case application.Images, application.HeroSlides, application.PromoBanners:
		allowed["position"] = true
	case application.StaffUsers:
		allowed["email"] = true
		allowed["name"] = true
	case application.Providers, application.Branches:
		allowed["name"] = true
	}
	if sort := c.Query("sort"); sort != "" {
		if !allowed[sort] {
			return q, domain.Fail("INVALID_SORT", 400)
		}
		q.Sort = sort
	}
	if order := c.Query("order"); order != "" {
		if order != "asc" && order != "desc" {
			return q, domain.Fail("INVALID_SORT_ORDER", 400)
		}
		q.Desc = order == "desc"
	}
	if len(q.Search) > 200 {
		return q, domain.Fail("INVALID_SEARCH", 400)
	}
	if status := c.Query("status"); status != "" {
		field := "status"
		switch e {
		case application.Orders:
			field = "order_status"
		case application.PaymentEvents:
			field = "normalized_status"
		case application.Products, application.Attempts, application.Refunds, application.OutboxEvents:
		default:
			return q, domain.Fail("INVALID_FILTER", 400)
		}
		q.Eq[field] = status
	}
	if payment := c.Query("payment"); payment != "" {
		if e != application.Orders {
			return q, domain.Fail("INVALID_FILTER", 400)
		}
		q.Eq["payment_status"] = payment
	}
	if e == application.Products {
		switch c.DefaultQuery("deleted", "exclude") {
		case "exclude":
			q.Eq["deleted_at"] = nil
		case "only":
			q.GT = map[string]any{"deleted_at": "1970-01-01"}
		case "include":
		default:
			return q, domain.Fail("INVALID_FILTER", 400)
		}
		if cat := c.Query("category_id"); cat != "" {
			q.Eq["category_id"] = cat
		}
	}
	if e == application.Variants || e == application.Images {
		if id := c.Query("product_id"); id != "" {
			q.Eq["product_id"] = id
		}
	}
	if e == application.Branches {
		if id := c.Query("provider_id"); id != "" {
			q.Eq["provider_id"] = id
		}
	}
	if e == application.StaffUsers {
		if role := c.Query("role"); role != "" {
			if !domain.ValidRole(role) {
				return q, domain.Fail("INVALID_ROLE", 400)
			}
			q.Eq["role"] = role
		}
	}
	if e == application.Attempts || e == application.Refunds || e == application.OrderEvents {
		if bill := c.Query("order_id"); bill != "" {
			q.Eq["order_id"] = bill
		}
	}
	return q, nil
}
func (h *Handler) list(e application.Entity) gin.HandlerFunc {
	return func(c *gin.Context) {
		q, err := adminQuery(c, e)
		if err != nil {
			h.failure(c, err)
			return
		}
		rows, total, err := h.Service.AdminList(c.Request.Context(), actor(c), e, q)
		if err != nil {
			h.failure(c, err)
			return
		}
		c.JSON(200, gin.H{"data": rows, "pagination": gin.H{"page": q.Offset/q.Limit + 1, "limit": q.Limit, "total": total}})
	}
}
func (h *Handler) adminRoutes(r *gin.RouterGroup) {
	resources := map[string]application.Entity{"products": application.Products, "categories": application.Categories, "providers": application.Providers, "branches": application.Branches, "hero-slides": application.HeroSlides, "promo-banners": application.PromoBanners, "orders": application.Orders, "payment-attempts": application.Attempts, "payment-events": application.PaymentEvents, "refunds": application.Refunds, "staff": application.StaffUsers, "sms-logs": application.SMSLogs, "outbox": application.OutboxEvents, "audit-logs": application.Audits}
	for path, e := range resources {
		r.GET("/"+path, h.list(e))
	}
	r.GET("/dashboard", func(c *gin.Context) {
		result, err := h.Service.Dashboard(c.Request.Context(), actor(c))
		h.send(c, result, err)
	})
	r.GET("/orders/export", h.export)
	r.GET("/orders/:bill", h.permission("orders.view"), func(c *gin.Context) {
		result, err := h.Service.Bill(c.Request.Context(), c.Param("bill"), "", true)
		h.send(c, result, err)
	})
	r.PUT("/orders/:bill/pickup-code", func(c *gin.Context) {
		var in struct {
			PickupCode string `json:"pickup_code"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		h.send(c, gin.H{"updated": true}, h.Service.SetPickupCode(c.Request.Context(), actor(c), c.Param("bill"), in.PickupCode, c.ClientIP()))
	})
	for path, action := range map[string]string{"mark-paid": "PAID", "mark-delivered": "DELIVERED", "cancel": "CANCELLED"} {
		r.POST("/orders/:bill/"+path, func(c *gin.Context) {
			var in struct {
				Note string `json:"note"`
			}
			if err := decode(c, &in); err != nil {
				h.failure(c, err)
				return
			}
			if len(in.Note) > 2000 {
				h.failure(c, domain.Fail("INVALID_NOTE", 400))
				return
			}
			h.send(c, gin.H{"updated": true}, h.Service.OrderAction(c.Request.Context(), actor(c), c.Param("bill"), action, in.Note, c.ClientIP()))
		})
	}
	r.POST("/orders/:bill/refunds", func(c *gin.Context) {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.RequestRefund(c.Request.Context(), actor(c), c.Param("bill"), in.Reason, c.ClientIP())
		h.send(c, result, err)
	})
	r.POST("/refunds/:id/approve", func(c *gin.Context) {
		h.send(c, gin.H{"approved": true}, h.Service.ApproveRefund(c.Request.Context(), actor(c), c.Param("id"), c.ClientIP()))
	})
	r.POST("/refunds/:id/resolve", func(c *gin.Context) {
		var in struct {
			ProviderRefundID string `json:"provider_refund_bill_id"`
			Success          bool   `json:"terminal_success"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		h.send(c, gin.H{"resolved": true}, h.Service.ResolveRefund(c.Request.Context(), actor(c), c.Param("id"), in.ProviderRefundID, in.Success, c.ClientIP()))
	})
	r.GET("/products/:id", h.permission("products.manage"), func(c *gin.Context) {
		var products []domain.Product
		err := h.Service.Store.Find(c.Request.Context(), application.Products, application.Query{Eq: map[string]any{"id": c.Param("id")}}, &products)
		if err != nil {
			h.failure(c, err)
			return
		}
		if len(products) == 0 {
			h.failure(c, domain.ErrNotFound)
			return
		}
		result, err := h.Service.Product(c.Request.Context(), products[0].Slug, true)
		h.send(c, result, err)
	})
	saveProduct := func(c *gin.Context) {
		var in application.ProductInput
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.SaveProduct(c.Request.Context(), actor(c), c.Param("id"), in, c.ClientIP())
		h.send(c, result, err)
	}
	r.POST("/products", saveProduct)
	r.PUT("/products/:id", saveProduct)
	r.DELETE("/products/:id", func(c *gin.Context) {
		h.send(c, gin.H{"deleted": true}, h.Service.ProductVisibility(c.Request.Context(), actor(c), c.Param("id"), false, c.ClientIP()))
	})
	r.POST("/products/:id/restore", func(c *gin.Context) {
		h.send(c, gin.H{"restored": true}, h.Service.ProductVisibility(c.Request.Context(), actor(c), c.Param("id"), true, c.ClientIP()))
	})
	r.GET("/products/:id/variants", func(c *gin.Context) {
		q := application.Query{Eq: map[string]any{"product_id": c.Param("id")}, Sort: "position", Limit: 100}
		rows, total, err := h.Service.AdminList(c.Request.Context(), actor(c), application.Variants, q)
		h.send(c, gin.H{"variants": rows, "total": total}, err)
	})
	saveVariant := func(c *gin.Context) {
		var in application.VariantInput
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.SaveVariant(c.Request.Context(), actor(c), c.Param("id"), c.Param("variant"), in, c.ClientIP())
		h.send(c, result, err)
	}
	r.POST("/products/:id/variants", saveVariant)
	r.PUT("/products/:id/variants/:variant", saveVariant)
	r.DELETE("/products/:id/variants/:variant", func(c *gin.Context) {
		h.send(c, gin.H{"deleted": true}, h.Service.DeleteVariant(c.Request.Context(), actor(c), c.Param("id"), c.Param("variant"), c.ClientIP()))
	})
	r.POST("/products/:id/images", func(c *gin.Context) {
		var in application.ImageInput
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.SaveImage(c.Request.Context(), actor(c), c.Param("id"), in, c.ClientIP())
		h.send(c, result, err)
	})
	r.DELETE("/products/:id/images/:image", func(c *gin.Context) {
		h.send(c, gin.H{"deleted": true}, h.Service.DeleteImage(c.Request.Context(), actor(c), c.Param("id"), c.Param("image"), c.ClientIP()))
	})
	saveCategory := func(c *gin.Context) {
		var in application.CategoryInput
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.SaveCategory(c.Request.Context(), actor(c), c.Param("id"), in, c.ClientIP())
		h.send(c, result, err)
	}
	r.POST("/categories", saveCategory)
	r.PUT("/categories/:id", saveCategory)
	saveProvider := func(c *gin.Context) {
		var in struct {
			Name           string `json:"name"`
			ShippingFeeKip int64  `json:"shipping_fee_kip"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.SaveProvider(c.Request.Context(), actor(c), c.Param("id"), domain.Provider{Name: in.Name, ShippingFeeKip: in.ShippingFeeKip}, c.ClientIP())
		h.send(c, result, err)
	}
	r.POST("/providers", saveProvider)
	r.PUT("/providers/:id", saveProvider)
	r.POST("/branches", func(c *gin.Context) {
		var in struct {
			ProviderID string `json:"provider_id"`
			Province   string `json:"province"`
			City       string `json:"city"`
			Name       string `json:"name"`
		}
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.SaveBranch(c.Request.Context(), actor(c), domain.Branch{ProviderID: in.ProviderID, Province: in.Province, City: in.City, Name: in.Name}, c.ClientIP())
		h.send(c, result, err)
	})
	for path, e := range map[string]application.Entity{"hero-slides": application.HeroSlides, "promo-banners": application.PromoBanners} {
		save := func(c *gin.Context) {
			var in struct {
				ImageURL   string `json:"image_url"`
				AltText    string `json:"alt_text"`
				Heading    string `json:"heading"`
				Subheading string `json:"subheading"`
				CTALabel   string `json:"cta_label"`
				CTAHref    string `json:"cta_href"`
				Position   int    `json:"position"`
				Active     *bool  `json:"active"`
			}
			if err := decode(c, &in); err != nil {
				h.failure(c, err)
				return
			}
			active := true
			if in.Active != nil {
				active = *in.Active
			}
			result, err := h.Service.SaveContent(c.Request.Context(), actor(c), e, c.Param("id"), domain.Content{ImageURL: in.ImageURL, AltText: in.AltText, Heading: in.Heading, Subheading: in.Subheading, CTALabel: in.CTALabel, CTAHref: in.CTAHref, Position: in.Position, Active: active}, c.ClientIP())
			h.send(c, result, err)
		}
		r.POST("/"+path, save)
		r.PUT("/"+path+"/:id", save)
	}
	for path, e := range map[string]application.Entity{"categories": application.Categories, "branches": application.Branches, "hero-slides": application.HeroSlides, "promo-banners": application.PromoBanners} {
		r.DELETE("/"+path+"/:id", func(c *gin.Context) {
			h.send(c, gin.H{"deleted": true}, h.Service.DeleteResource(c.Request.Context(), actor(c), e, c.Param("id"), c.ClientIP()))
		})
	}
	for path, e := range map[string]application.Entity{"categories": application.Categories, "hero-slides": application.HeroSlides, "promo-banners": application.PromoBanners, "products/:id/variants": application.Variants, "products/:id/images": application.Images} {
		r.POST("/"+path+"/reorder", func(c *gin.Context) {
			var in application.ReorderInput
			if err := decode(c, &in); err != nil {
				h.failure(c, err)
				return
			}
			h.send(c, gin.H{"reordered": true}, h.Service.Reorder(c.Request.Context(), actor(c), e, c.Param("id"), in, c.ClientIP()))
		})
	}
	r.POST("/staff", func(c *gin.Context) {
		var in application.StaffInput
		if err := decode(c, &in); err != nil {
			h.failure(c, err)
			return
		}
		result, err := h.Service.CreateStaff(c.Request.Context(), actor(c), in, false, c.ClientIP())
		h.send(c, result, err)
	})
	for _, action := range []string{"enable", "disable", "reset-password"} {
		r.POST("/staff/:id/"+action, func(c *gin.Context) {
			password := ""
			if action == "reset-password" {
				var in struct {
					Password string `json:"password"`
				}
				if err := decode(c, &in); err != nil {
					h.failure(c, err)
					return
				}
				password = in.Password
			}
			h.send(c, gin.H{"updated": true}, h.Service.ChangeStaff(c.Request.Context(), actor(c), c.Param("id"), action, password, c.ClientIP()))
		})
	}
	r.POST("/uploads", func(c *gin.Context) {
		a := actor(c)
		if !domain.Can(a.Role, "products.manage") && !domain.Can(a.Role, "content.manage") {
			h.failure(c, domain.Fail("FORBIDDEN", 403))
			return
		}
		content := c.Query("kind") == "content"
		if kind := c.Query("kind"); kind != "" && kind != "product" && kind != "content" {
			h.failure(c, domain.Fail("INVALID_UPLOAD_KIND", 400))
			return
		}
		if content && !domain.Can(a.Role, "content.manage") || !content && !domain.Can(a.Role, "products.manage") {
			h.failure(c, domain.Fail("FORBIDDEN", 403))
			return
		}
		file, err := c.FormFile("file")
		if err != nil {
			h.failure(c, domain.Fail("INVALID_UPLOAD", 400))
			return
		}
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		reader, err := file.Open()
		if err != nil {
			h.failure(c, err)
			return
		}
		defer reader.Close()
		url, err := h.Service.Media.Save(c.Request.Context(), reader, file.Header.Get("Content-Type"), content)
		if err != nil {
			h.failure(c, err)
			return
		}
		err = h.Service.Audit(c.Request.Context(), h.Service.Store, a, "media.upload", "media", url, "", c.ClientIP())
		h.send(c, gin.H{"url": url}, err)
	})
}

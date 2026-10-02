package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"shopnext-laos/internal/adapters/media"
	"shopnext-laos/internal/adapters/providers"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/platform"
)

// These tests use synthetic requests and temporary media only. Nil persistence
// is deliberate: every rejected request must stop before business persistence.
type securityAuditLimiter struct {
	key string
}

func (l *securityAuditLimiter) Check(_ context.Context, _, key string, _ int, _ time.Duration) (time.Duration, error) {
	l.key = key
	return 0, nil
}

func (*securityAuditLimiter) Ping(context.Context) error { return nil }

func securityAuditRouter(t *testing.T) (http.Handler, *securityAuditLimiter, platform.Config) {
	t.Helper()
	cfg := platform.Config{
		Env:            "production",
		AdminJWTSecret: strings.Repeat("a", 32),
		WebhookHeader:  "X-Test-Signature",
		WebhookSecret:  strings.Repeat("s", 32),
		AllowedOrigins: []string{"https://shop.example.test"},
		UploadDir:      t.TempDir(),
	}
	limiter := &securityAuditLimiter{}
	service := application.New(nil, limiter, providers.NewPayment("", "", false), nil, &media.Storage{Dir: cfg.UploadDir}, application.Options{Production: true, AdminJWTSecret: cfg.AdminJWTSecret})
	router, err := New(service, cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return router, limiter, cfg
}

func TestSecurityAuditAllAdminRoutesRejectMissingAndForgedTokens(t *testing.T) {
	router, _, _ := securityAuditRouter(t)
	// The engine exposes its registered routes; exercise every protected route,
	// including all mutations, with well formed synthetic JSON and a CSRF header.
	engine := router.(*gin.Engine)
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/admin/") || route.Path == "/api/v1/admin/auth/login" {
			continue
		}
		path := strings.NewReplacer(":bill", "BTEST123", ":id", "test-id", ":variant", "test-variant", ":image", "test-image").Replace(route.Path)
		for _, token := range []string{"", "Bearer forged.invalid.signature", "Bearer " + strings.Repeat("x", 4096)} {
			t.Run(route.Method+" "+route.Path+" token length "+strconv.Itoa(len(token)), func(t *testing.T) {
				req := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
				req.Header.Set("X-ShopNext-CSRF", "1")
				req.Header.Set("Authorization", token)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", rec.Code)
				}
			})
		}
	}
}

func TestSecurityAuditWebhookAuthenticationAndBodyLimit(t *testing.T) {
	router, limiter, cfg := securityAuditRouter(t)
	for _, test := range []struct {
		name string
		body string
		sign bool
		want int
	}{
		{"unsigned", `{}`, false, 401},
		{"signed_invalid_callback", `{}`, true, 400},
		{"oversized_signed", strings.Repeat("x", 64*1024+1), true, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/phajay", strings.NewReader(test.body))
			req.RemoteAddr = "198.51.100.21:12345"
			req.Header.Set("X-Forwarded-For", "203.0.113.77")
			if test.sign {
				mac := hmac.New(sha256.New, []byte(cfg.WebhookSecret))
				_, _ = mac.Write([]byte(test.body))
				req.Header.Set(cfg.WebhookHeader, hex.EncodeToString(mac.Sum(nil)))
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != test.want {
				t.Fatalf("status = %d, want %d", rec.Code, test.want)
			}
			if limiter.key != "198.51.100.21" {
				t.Fatal("untrusted forwarding header changed rate-limit identity")
			}
		})
	}
}

func TestSecurityAuditCSRFOriginJSONAndBodyLimit(t *testing.T) {
	router, _, _ := securityAuditRouter(t)
	for _, test := range []struct {
		name, origin, csrf, body string
		want                     int
	}{
		{"csrf_missing", "", "", `{}`, 403},
		{"foreign_origin", "https://attacker.example.test", "1", `{}`, 403},
		{"trailing_json", "", "1", `{} {}`, 400},
		{"unknown_field", "", "1", `{"unexpected":1}`, 400},
		{"oversized_json", "", "1", `{"email":"` + strings.Repeat("x", 1024*1024) + `","password":"synthetic"}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", strings.NewReader(test.body))
			req.Header.Set("Origin", test.origin)
			req.Header.Set("X-ShopNext-CSRF", test.csrf)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != test.want {
				t.Fatalf("status = %d, want %d", rec.Code, test.want)
			}
		})
	}
}

func TestSecurityAuditMediaRejectsTraversalAndSymlinks(t *testing.T) {
	router, _, cfg := securityAuditRouter(t)
	outside := filepath.Join(t.TempDir(), "synthetic.txt")
	if err := os.WriteFile(outside, []byte("synthetic-marker"), 0600); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("a", 24) + "-" + strings.Repeat("b", 8) + ".webp"
	if err := os.Symlink(outside, filepath.Join(cfg.UploadDir, name)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/media/" + name, "/media/../../synthetic.txt", "/media/%2e%2e%2fsynthetic.txt", "/media/not-an-upload.webp"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "synthetic-marker") {
			t.Fatalf("media path %q exposed a file or returned unexpected status %d", path, rec.Code)
		}
	}
}

func TestSecurityRecoveryNeverLogsRequestSecretsOrPanicValues(t *testing.T) {
	var captured bytes.Buffer
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &captured
	t.Cleanup(func() { gin.DefaultErrorWriter = previous })
	_, _, cfg := securityAuditRouter(t)
	log := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&captured), zap.DebugLevel))
	for _, panicValue := range []any{
		"synthetic-panic-marker", http.ErrAbortHandler,
		&net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE},
		&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET},
		nil,
	} {
		router, err := New(&application.Service{}, cfg, log)
		if err != nil {
			t.Fatal(err)
		}
		router.GET("/security-audit/panic", func(*gin.Context) { panic(panicValue) })
		req := httptest.NewRequest(http.MethodGet, "/security-audit/panic?customer=synthetic-query-marker", strings.NewReader("synthetic-body-marker"))
		req.Header.Set(PhoneKeyHeader, "synthetic-phone-marker")
		req.Header.Set(TrustDeviceHeader, "synthetic-trust-marker")
		req.Header.Set("Authorization", "Bearer synthetic-staff-marker")
		req.Header.Set("Cookie", "secret=synthetic-cookie-marker")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 500 || !strings.Contains(rec.Body.String(), "INTERNAL_ERROR") {
			t.Fatal("panic did not produce the generic error response")
		}
		if strings.Contains(rec.Body.String(), "synthetic-") || strings.Contains(captured.String(), "synthetic-") {
			t.Fatal("recovery disclosed a request secret or panic value")
		}
	}
	if !strings.Contains(captured.String(), "request_id") || !strings.Contains(captured.String(), `"status":500`) {
		t.Fatal("safe diagnostic fields were not logged")
	}
}

func TestSecurityRecoveryAbortsStartedResponse(t *testing.T) {
	router, _, _ := securityAuditRouter(t)
	router.(*gin.Engine).GET("/security-audit/stream", func(c *gin.Context) {
		c.String(200, "started")
		panic("synthetic-panic-marker")
	}, func(c *gin.Context) { c.String(200, "continued") })
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/security-audit/stream", nil))
	if rec.Code != 200 || rec.Body.String() != "started" {
		t.Fatal("recovery appended an error or continued an aborted response")
	}
}

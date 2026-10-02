package integration

import (
	"net/http"
	"net/http/httptest"
	"shopnext-laos/internal/application"
	"strings"
	"testing"
	"time"
)

func TestAdminJWTHTTPAndSessionRevocation(t *testing.T) {
	h := setup(t)
	credential := ownerAuthorization(t, h)
	for _, value := range []string{"", "Basic abc", "Bearer", "Bearer bad", credential.Value + " extra", credential.Value + "x"} {
		expect(t, request(t, h, "GET", "/api/v1/admin/auth/session", nil, &http.Cookie{Name: "Authorization", Value: value}), 401)
	}
	expect(t, request(t, h, "GET", "/api/v1/admin/auth/session", nil, &http.Cookie{Name: "shopnext_staff", Value: strings.TrimPrefix(credential.Value, "Bearer ")}), 401)
	expect(t, request(t, h, "GET", "/api/v1/admin/auth/session", nil, credential), 200)
	preflight := httptest.NewRequest("OPTIONS", "/api/v1/admin/orders", nil)
	preflight.Header.Set("Origin", "http://localhost:3000")
	preflight.Header.Set("Access-Control-Request-Headers", "authorization")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, preflight)
	expect(t, w, 204)
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatal("CORS does not allow bearer authentication")
	}
	// Database expiry is enforced even while the JWT signature and exp are valid.
	if err := h.db.DB.Exec("UPDATE staff_sessions SET expires_at = ?", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	expect(t, request(t, h, "GET", "/api/v1/admin/auth/session", nil, credential), 401)
	w = request(t, h, "POST", "/api/v1/admin/auth/login", map[string]string{"email": owner.Email, "password": "correct-password"})
	expect(t, w, 200)
	credential.Value = "Bearer " + data[adminLogin](t, w).AccessToken
	staff := data[adminLogin](t, w).Actor
	if err := h.s.ChangeStaff(ctx, staff, staff.ID, "reset-password", "new-password-123", "ip"); err != nil {
		t.Fatal(err)
	}
	expect(t, request(t, h, "GET", "/api/v1/admin/auth/session", nil, credential), 401)
}

func TestLegacyAdminJWTLogout(t *testing.T) {
	h := setup(t)
	hash, err := application.HashPassword("legacy-password-123")
	must(t, err)
	h.s.Options.LegacyPasswordHash = hash
	h.s.Options.LegacySessionSecret = strings.Repeat("l", 32)
	w := request(t, h, "POST", "/api/v1/admin/auth/login", map[string]string{"email": "admin", "password": "legacy-password-123"})
	expect(t, w, 200)
	login := data[adminLogin](t, w)
	if !login.Actor.Legacy || login.Actor.Role != "OWNER" || len(w.Result().Cookies()) != 0 {
		t.Fatal("legacy JWT login contract")
	}
	credential := &http.Cookie{Name: "Authorization", Value: "Bearer " + login.AccessToken}
	expect(t, request(t, h, "GET", "/api/v1/admin/auth/session", nil, credential), 200)
	expect(t, request(t, h, "POST", "/api/v1/admin/auth/logout", nil, credential), 200)
	expect(t, request(t, h, "GET", "/api/v1/admin/auth/session", nil, credential), 401)
	expect(t, request(t, h, "POST", "/api/v1/admin/auth/logout", nil), 401)
}

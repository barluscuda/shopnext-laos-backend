package application

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestAdminJWTValidation(t *testing.T) {
	s := &Service{Options: Options{AdminJWTSecret: strings.Repeat("a", 32), LegacyPasswordHash: "configured", LegacySessionSecret: strings.Repeat("l", 32)}}
	now := time.Now().Unix()
	claims := adminClaims{Issuer: adminJWTIssuer, Audience: adminJWTAudience, Subject: "staff-id", ID: "session-id", IssuedAt: now, ExpiresAt: now + 28800}
	token, err := s.signAdmin(claims)
	if err != nil {
		t.Fatal(err)
	}
	if got, valid := s.parseAdmin(token); !valid || got != claims {
		t.Fatal("valid admin JWT rejected")
	}
	parts := strings.Split(token, ".")
	for name, bad := range map[string]string{
		"empty": "", "opaque": "old-session-token", "oversize": strings.Repeat("a", 2049),
		"tampered signature": token + "x",
		"tampered payload":   parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"legacy","legacy":true}`)) + "." + parts[2],
		"unsigned":           base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + ".",
		"wrong algorithm":    base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS512","typ":"JWT"}`)) + "." + parts[1] + "." + parts[2],
	} {
		t.Run(name, func(t *testing.T) {
			if _, valid := s.parseAdmin(bad); valid {
				t.Fatal("invalid JWT accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*adminClaims){
		"expired":                func(c *adminClaims) { c.IssuedAt = now - 3600; c.ExpiresAt = now },
		"future":                 func(c *adminClaims) { c.IssuedAt = now + 60 },
		"missing issued time":    func(c *adminClaims) { c.IssuedAt = 0 },
		"missing expiry":         func(c *adminClaims) { c.ExpiresAt = 0 },
		"long lifetime":          func(c *adminClaims) { c.ExpiresAt++ },
		"wrong issuer":           func(c *adminClaims) { c.Issuer = "other" },
		"wrong audience":         func(c *adminClaims) { c.Audience = "customer" },
		"missing subject":        func(c *adminClaims) { c.Subject = "" },
		"missing session":        func(c *adminClaims) { c.ID = "" },
		"invalid legacy subject": func(c *adminClaims) { c.Legacy = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := claims
			mutate(&bad)
			key, err := s.signAdmin(bad)
			if err != nil {
				t.Fatal(err)
			}
			if _, valid := s.parseAdmin(key); valid {
				t.Fatal("invalid signed claims accepted")
			}
		})
	}
	other := *s
	other.Options.AdminJWTSecret = strings.Repeat("b", 32)
	if _, valid := other.parseAdmin(token); valid {
		t.Fatal("wrong key accepted")
	}
	device, err := (&Service{Options: Options{TrustDeviceSecret: s.Options.AdminJWTSecret}}).signDevice(deviceClaims{"bill", "nonce", now, now + 3600})
	if err != nil {
		t.Fatal(err)
	}
	if _, valid := s.parseAdmin(device); valid {
		t.Fatal("customer JWT accepted as admin JWT")
	}
	claims.Legacy, claims.Subject = true, "legacy"
	legacy, err := s.signAdmin(claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, valid := s.parseAdmin(legacy); !valid {
		t.Fatal("legacy JWT rejected")
	}
	other = *s
	other.Options.LegacySessionSecret = strings.Repeat("x", 32)
	if _, valid := other.parseAdmin(legacy); valid {
		t.Fatal("legacy JWT survived secret rotation")
	}
	other.Options.LegacyPasswordHash = ""
	if _, valid := other.parseAdmin(legacy); valid {
		t.Fatal("legacy JWT accepted with fallback disabled")
	}
	other.Options.AdminJWTSecret = ""
	if _, err := other.signAdmin(claims); err == nil {
		t.Fatal("missing admin secret accepted")
	}
}

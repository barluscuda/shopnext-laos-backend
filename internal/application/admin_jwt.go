package application

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"shopnext-laos/internal/domain"
	"strings"
	"time"
)

const AdminTokenLifetime = 8 * time.Hour
const adminJWTIssuer = "shopnext-laos"
const adminJWTAudience = "shopnext-admin"

type adminClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	Subject   string `json:"sub"`
	ID        string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Legacy    bool   `json:"legacy,omitempty"`
}

func (s *Service) adminSigningKey(legacy bool) string {
	if legacy {
		// Binding both secrets lets operators revoke legacy access independently.
		mac := hmac.New(sha256.New, []byte(s.Options.AdminJWTSecret))
		_, _ = mac.Write([]byte(s.Options.LegacySessionSecret))
		return string(mac.Sum(nil))
	}
	return s.Options.AdminJWTSecret
}

func (s *Service) signAdmin(claims adminClaims) (string, error) {
	if len(s.Options.AdminJWTSecret) < 32 || claims.Legacy && len(s.Options.LegacySessionSecret) < 32 {
		return "", domain.Fail("ADMIN_AUTH_UNAVAILABLE", 503)
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	raw := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(s.adminSigningKey(claims.Legacy)))
	_, _ = mac.Write([]byte(raw))
	return raw + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) parseAdmin(token string) (adminClaims, bool) {
	var claims adminClaims
	if len(token) > 2048 || len(s.Options.AdminJWTSecret) < 32 {
		return claims, false
	}
	parts := strings.Split(token, ".")
	// Accept only our fixed HS256 JWT header; never choose an algorithm from input.
	if len(parts) != 3 || parts[0] != base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) {
		return claims, false
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &claims) != nil {
		return claims, false
	}
	if claims.Legacy && (s.Options.LegacyPasswordHash == "" || len(s.Options.LegacySessionSecret) < 32) {
		return claims, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	mac := hmac.New(sha256.New, []byte(s.adminSigningKey(claims.Legacy)))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return claims, false
	}
	now := time.Now().Unix()
	valid := claims.Issuer == adminJWTIssuer && claims.Audience == adminJWTAudience && claims.Subject != "" && claims.ID != "" && claims.IssuedAt > 0 && claims.IssuedAt <= now && claims.ExpiresAt > now && claims.ExpiresAt > claims.IssuedAt && claims.ExpiresAt-claims.IssuedAt <= int64(AdminTokenLifetime.Seconds()) && (!claims.Legacy || claims.Subject == "legacy")
	return claims, valid
}

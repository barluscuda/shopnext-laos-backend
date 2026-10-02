package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"shopnext-laos/internal/domain"
	"strings"
	"time"
)

const PhoneKeyLifetime = 3 * 24 * time.Hour
const PhoneKeyMaxUses = 5
const TrustDeviceLifetime = 30 * 24 * time.Hour

// phoneKey validates ownership and consumes under a row lock. Successful
// operations commit usage together with their business writes.
func phoneKey(ctx context.Context, tx Store, key, expected string, consume bool) (domain.PhoneToken, error) {
	if key == "" || len(key) > 256 {
		return domain.PhoneToken{}, domain.Fail("PHONE_VERIFICATION_REQUIRED", 403)
	}
	pt, err := findOne[domain.PhoneToken](ctx, tx, PhoneTokens, Query{Eq: map[string]any{"token_hash": domain.Hash(key)}, Lock: consume})
	if errors.Is(err, domain.ErrNotFound) {
		return pt, domain.Fail("PHONE_VERIFICATION_REQUIRED", 403)
	}
	if err != nil {
		return pt, err
	}
	if !pt.ExpiresAt.After(time.Now()) {
		return pt, domain.Fail("PHONE_KEY_EXPIRED", 403)
	}
	if expected != "" && pt.Phone != domain.NormalizePhone(expected) {
		return pt, domain.Fail("PHONE_OWNERSHIP_MISMATCH", 403)
	}
	if consume {
		if pt.Uses >= PhoneKeyMaxUses {
			return pt, domain.Fail("PHONE_KEY_EXHAUSTED", 403)
		}
		err = tx.Update(ctx, PhoneTokens, pt.ID, changed(map[string]any{"uses": pt.Uses + 1}))
	}
	return pt, err
}

type deviceClaims struct {
	OrderID   string `json:"order_id"`
	Token     string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func (s *Service) signDevice(claims deviceClaims) (string, error) {
	if len(s.Options.TrustDeviceSecret) < 32 {
		return "", domain.Fail("TRUST_DEVICE_UNAVAILABLE", 503)
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	raw := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(s.Options.TrustDeviceSecret))
	_, _ = mac.Write([]byte(raw))
	return raw + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) deviceClaims(key, bill string) (deviceClaims, error) {
	var claims deviceClaims
	invalid := domain.Fail("INVALID_TRUST_DEVICE_KEY", 403)
	if len(key) > 2048 || len(s.Options.TrustDeviceSecret) < 32 {
		return claims, invalid
	}
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) {
		return claims, invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return claims, invalid
	}
	mac := hmac.New(sha256.New, []byte(s.Options.TrustDeviceSecret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return claims, invalid
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &claims) != nil || claims.OrderID != bill || claims.Token == "" || claims.ExpiresAt <= time.Now().Unix() || claims.IssuedAt > time.Now().Unix() {
		return claims, invalid
	}
	return claims, nil
}

func (s *Service) trustedDevice(ctx context.Context, tx Store, key, bill, userAgent string) error {
	if userAgent == "" || len(userAgent) > 1024 {
		return domain.Fail("INVALID_TRUST_DEVICE_KEY", 403)
	}
	claims, err := s.deviceClaims(key, bill)
	if err != nil {
		return err
	}
	device, err := findOne[domain.TrustedDevice](ctx, tx, TrustedDevices, Query{Eq: map[string]any{"order_id": bill, "token_hash": domain.Hash(claims.Token)}})
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Fail("INVALID_TRUST_DEVICE_KEY", 403)
	}
	if err != nil {
		return err
	}
	if device.UserAgent != userAgent || !device.ExpiresAt.After(time.Now()) {
		return domain.Fail("INVALID_TRUST_DEVICE_KEY", 403)
	}
	return nil
}

func (s *Service) authorizeOrder(ctx context.Context, tx Store, bill, key, trust, userAgent string) (domain.Order, error) {
	order, err := findOne[domain.Order](ctx, tx, Orders, Query{Eq: map[string]any{"bill_number": bill}})
	if err != nil {
		return order, err
	}
	if trust != "" {
		return order, s.trustedDevice(ctx, tx, trust, bill, userAgent)
	}
	_, err = phoneKey(ctx, tx, key, order.RecipientPhone, true)
	return order, err
}

type ClientBill struct {
	Bill
	TrustDeviceKey       string     `json:"trust_device_key,omitempty"`
	TrustDeviceExpiresAt *time.Time `json:"trust_device_expires_at,omitempty"`
}

func (s *Service) ClientBill(ctx context.Context, bill, key, trust, userAgent string) (ClientBill, error) {
	var result ClientBill
	if _, err := s.Expire(ctx, bill); err != nil {
		return result, err
	}
	err := s.Store.Transaction(ctx, func(tx Store) error {
		order, err := s.authorizeOrder(ctx, tx, bill, key, trust, userAgent)
		if err != nil {
			return err
		}
		if userAgent == "" || len(userAgent) > 1024 {
			return domain.Fail("USER_AGENT_REQUIRED", 400)
		}
		local := *s
		local.Store = tx
		result.Bill, err = local.bill(ctx, bill, order.RecipientPhone, false)
		if err != nil {
			return err
		}
		if trust == "" {
			now := time.Now().UTC()
			nonce := domain.Token(32)
			expiry := now.Add(TrustDeviceLifetime)
			result.TrustDeviceKey, err = s.signDevice(deviceClaims{bill, nonce, now.Unix(), expiry.Unix()})
			if err != nil {
				return err
			}
			result.TrustDeviceExpiresAt = &expiry
			return tx.Insert(ctx, TrustedDevices, &domain.TrustedDevice{Base: domain.NewBase(), OrderID: bill, TokenHash: domain.Hash(nonce), UserAgent: userAgent, ExpiresAt: expiry})
		}
		return nil
	})
	return result, err
}

func (s *Service) ClientBills(ctx context.Context, phone, key string) ([]domain.Order, error) {
	var result []domain.Order
	err := s.Store.Transaction(ctx, func(tx Store) error {
		pt, err := phoneKey(ctx, tx, key, phone, true)
		if err != nil {
			return err
		}
		return tx.Find(ctx, Orders, Query{Eq: map[string]any{"recipient_phone": pt.Phone}, Sort: "created_at", Desc: true, Limit: 50}, &result)
	})
	return result, err
}

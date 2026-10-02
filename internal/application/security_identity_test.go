package application

import (
	"context"
	"errors"
	"fmt"
	"shopnext-laos/internal/domain"
	"testing"
	"time"
)

// These local audit proofs document current weaknesses using synthetic records.
// They never connect to a database, Redis, or an SMS provider.
type securityIdentityStore struct {
	Store
	otp    domain.OTP
	audits int
}

func (s *securityIdentityStore) Transaction(ctx context.Context, fn func(Store) error) error {
	return fn(s)
}

func (s *securityIdentityStore) LockKey(context.Context, string) error { return nil }

func (s *securityIdentityStore) Find(_ context.Context, entity Entity, query Query, out any) error {
	switch entity {
	case OTPs:
		if s.otp.Phone == query.Eq["phone"] {
			*out.(*[]domain.OTP) = []domain.OTP{s.otp}
		}
	case StaffUsers:
		*out.(*[]domain.Staff) = nil
	default:
		return fmt.Errorf("unexpected audit proof entity: %s", entity)
	}
	return nil
}

func (s *securityIdentityStore) Update(_ context.Context, entity Entity, id string, values map[string]any) error {
	if entity != OTPs || id != s.otp.ID {
		return errors.New("unexpected audit proof update")
	}
	if attempts, ok := values["attempts"].(int); ok {
		s.otp.Attempts = attempts
	}
	return nil
}

func (s *securityIdentityStore) Insert(_ context.Context, entity Entity, _ any) error {
	if entity != Audits {
		return errors.New("unexpected audit proof insert")
	}
	s.audits++
	return nil
}

type securityIdentityLimiter struct {
	counts map[string]int
	calls  int
}

func (l *securityIdentityLimiter) Check(_ context.Context, scope, key string, max int, _ time.Duration) (time.Duration, error) {
	l.calls++
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	index := scope + ":" + key
	l.counts[index]++
	if l.counts[index] > max {
		return time.Minute, nil
	}
	return 0, nil
}

func (*securityIdentityLimiter) Ping(context.Context) error { return nil }

func securityIdentityWantCode(t *testing.T, err error, code string) {
	t.Helper()
	var business *domain.Error
	if !errors.As(err, &business) || business.Code != code {
		t.Fatalf("expected error code %s", code)
	}
}

func TestSecurityOTPVerificationExhaustsVictimWithoutRateCheck(t *testing.T) {
	store := &securityIdentityStore{otp: domain.OTP{
		Base: domain.NewBase(), Phone: "02055555555", CodeHash: domain.Hash("123456"),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}}
	limiter := &securityIdentityLimiter{}
	service := &Service{Store: store, Limiter: limiter}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, err := service.VerifyOTP(ctx, store.otp.Phone, "000000")
		securityIdentityWantCode(t, err, "INVALID_OTP")
	}
	_, err := service.VerifyOTP(ctx, store.otp.Phone, "123456")
	securityIdentityWantCode(t, err, "OTP_ATTEMPTS_EXCEEDED")
	if limiter.calls != 0 {
		t.Fatal("OTP verification unexpectedly applied a request limiter")
	}
}

func TestSecurityLegacyLoginAccountLimitBypassedWithAlias(t *testing.T) {
	hash, err := HashPassword("audit-only-correct-password")
	if err != nil {
		t.Fatal("could not create synthetic password hash")
	}
	store := &securityIdentityStore{}
	limiter := &securityIdentityLimiter{}
	service := &Service{Store: store, Limiter: limiter, Options: Options{LegacyPasswordHash: hash}}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, err := service.Login(ctx, "admin", "audit-only-wrong-password", "192.0.2.1")
		securityIdentityWantCode(t, err, "INVALID_CREDENTIALS")
	}
	_, _, err = service.Login(ctx, "admin", "audit-only-wrong-password", "192.0.2.1")
	securityIdentityWantCode(t, err, "RATE_LIMITED")
	_, _, err = service.Login(ctx, "rotated-alias", "audit-only-wrong-password", "192.0.2.1")
	securityIdentityWantCode(t, err, "INVALID_CREDENTIALS")
	if store.audits != 6 {
		t.Fatal("rotated alias did not reach credential verification")
	}
	// The per-IP ceiling still works. New source IPs and aliases restart both
	// counters even though all attempts target the same shared owner password.
	for i := 0; i < 8; i++ {
		_, _, err = service.Login(ctx, fmt.Sprintf("alias-%d", i), "audit-only-wrong-password", fmt.Sprintf("192.0.2.%d", i+2))
		securityIdentityWantCode(t, err, "INVALID_CREDENTIALS")
	}
}

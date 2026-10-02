package application

import (
	"context"
	"errors"
	"fmt"
	"shopnext-laos/internal/domain"
	"strings"
	"testing"
	"time"
)

// These security regressions use synthetic records.
// They never connect to a database, Redis, or an SMS provider.
type securityIdentityStore struct {
	Store
	otp         domain.OTP
	audits      int
	phoneTokens int
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
	if consumed, ok := values["consumed_at"].(time.Time); ok {
		s.otp.ConsumedAt = &consumed
	}
	return nil
}

func (s *securityIdentityStore) Insert(_ context.Context, entity Entity, value any) error {
	switch entity {
	case Audits:
		s.audits++
	case PhoneTokens:
		s.phoneTokens++
	case OTPs:
		s.otp = *value.(*domain.OTP)
	case SMSLogs:
	default:
		return errors.New("unexpected security regression insert")
	}
	return nil
}

type securityIdentityLimiter struct {
	counts       map[string]int
	calls        int
	blockedScope string
	err          error
}

func (l *securityIdentityLimiter) Check(_ context.Context, scope, key string, max int, _ time.Duration) (time.Duration, error) {
	l.calls++
	if l.err != nil {
		return 0, l.err
	}
	if scope == l.blockedScope {
		return time.Minute, nil
	}
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

func TestSecurityOTPChallengeProtectsAttemptBudget(t *testing.T) {
	challenge := domain.Token(32)
	store := &securityIdentityStore{otp: domain.OTP{
		Base: domain.NewBase(), Phone: "02055555555", CodeHash: domain.Hash("123456"),
		ChallengeHash: domain.Hash(challenge),
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}}
	limiter := &securityIdentityLimiter{}
	service := &Service{Store: store, Limiter: limiter}
	ctx := context.Background()
	for _, secret := range []string{"", "malformed", strings.Repeat("z", 64), domain.Token(32)} {
		for i := 0; i < 5; i++ {
			_, err := service.VerifyOTP(ctx, store.otp.Phone, "000000", secret, "192.0.2.1", "device")
			securityIdentityWantCode(t, err, "INVALID_OTP_CHALLENGE")
		}
	}
	if store.otp.Attempts != 0 || store.phoneTokens != 0 {
		t.Fatal("missing or guessed challenges changed the victim's attempt budget")
	}
	for i := 0; i < 5; i++ {
		_, err := service.VerifyOTP(ctx, store.otp.Phone, "000000", challenge, "192.0.2.2", "device")
		securityIdentityWantCode(t, err, "INVALID_OTP")
	}
	_, err := service.VerifyOTP(ctx, store.otp.Phone, "123456", challenge, "192.0.2.2", "device")
	securityIdentityWantCode(t, err, "OTP_ATTEMPTS_EXCEEDED")
	if store.otp.Attempts != 5 || store.phoneTokens != 0 || limiter.calls == 0 {
		t.Fatal("bound wrong codes did not enforce the attempt ceiling and limiter")
	}
}

func TestSecurityLegacyLoginAliasesShareAccountLimit(t *testing.T) {
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
	securityIdentityWantCode(t, err, "RATE_LIMITED")
	if store.audits != 5 {
		t.Fatal("rotated alias reached credential verification")
	}
	// Rotating source IPs and aliases cannot restart the shared principal quota.
	for i := 0; i < 8; i++ {
		_, _, err = service.Login(ctx, fmt.Sprintf("alias-%d", i), "audit-only-wrong-password", fmt.Sprintf("192.0.2.%d", i+2))
		securityIdentityWantCode(t, err, "RATE_LIMITED")
	}
	_, _, err = service.Login(ctx, "staff@example.test", "wrong", "192.0.2.20")
	securityIdentityWantCode(t, err, "INVALID_CREDENTIALS")
}

func TestSecurityOTPVerificationFailsClosedOnThrottle(t *testing.T) {
	for _, scope := range []string{"otp-verify-ip", "otp-verify-device", "otp-verify-challenge", "unavailable"} {
		t.Run(scope, func(t *testing.T) {
			challenge := domain.Token(32)
			store := &securityIdentityStore{otp: domain.OTP{Base: domain.NewBase(), Phone: "02055555555", CodeHash: domain.Hash("123456"), ChallengeHash: domain.Hash(challenge), ExpiresAt: time.Now().Add(time.Minute)}}
			limiter := &securityIdentityLimiter{blockedScope: scope}
			want := "RATE_LIMITED"
			if scope == "unavailable" {
				limiter.err = domain.Fail("RATE_LIMITER_UNAVAILABLE", 503)
				want = "RATE_LIMITER_UNAVAILABLE"
			}
			service := &Service{Store: store, Limiter: limiter}
			_, err := service.VerifyOTP(context.Background(), store.otp.Phone, "123456", challenge, "192.0.2.1", "device")
			securityIdentityWantCode(t, err, want)
			if store.otp.Attempts != 0 || store.otp.ConsumedAt != nil || store.phoneTokens != 0 {
				t.Fatal("throttled verification changed durable state")
			}
		})
	}
}

func TestSecurityOTPChallengeLifecycle(t *testing.T) {
	for _, state := range []string{"valid", "other-phone", "old-challenge", "pre-migration", "expired", "consumed"} {
		t.Run(state, func(t *testing.T) {
			challenge := domain.Token(32)
			store := &securityIdentityStore{otp: domain.OTP{Base: domain.NewBase(), Phone: "02055555555", CodeHash: domain.Hash("123456"), ChallengeHash: domain.Hash(challenge), ExpiresAt: time.Now().Add(time.Minute)}}
			phone, want := store.otp.Phone, ""
			switch state {
			case "other-phone":
				phone, want = "02066666666", "OTP_NOT_FOUND"
			case "old-challenge":
				store.otp.ChallengeHash, want = domain.Hash(domain.Token(32)), "INVALID_OTP_CHALLENGE"
			case "pre-migration":
				store.otp.ChallengeHash, want = "", "INVALID_OTP_CHALLENGE"
			case "expired":
				store.otp.ExpiresAt, want = time.Now().Add(-time.Minute), "OTP_EXPIRED"
			case "consumed":
				now := time.Now()
				store.otp.ConsumedAt, want = &now, "OTP_NOT_FOUND"
			}
			service := &Service{Store: store, Limiter: &securityIdentityLimiter{}}
			key, err := service.VerifyOTP(context.Background(), phone, "123456", challenge, "192.0.2.1", "device")
			if want != "" {
				securityIdentityWantCode(t, err, want)
				if key != "" || store.phoneTokens != 0 || store.otp.Attempts != 0 {
					t.Fatal("rejected issuance changed attempts or issued a key")
				}
				return
			}
			if err != nil || key == "" || store.phoneTokens != 1 || store.otp.ConsumedAt == nil {
				t.Fatal("valid issuance did not atomically consume OTP and issue a key")
			}
			_, err = service.VerifyOTP(context.Background(), phone, "123456", challenge, "192.0.2.1", "device")
			securityIdentityWantCode(t, err, "OTP_NOT_FOUND")
			if store.phoneTokens != 1 {
				t.Fatal("replay issued another phone key")
			}
		})
	}
}

type securityIdentitySMS struct{}

func (securityIdentitySMS) Send(context.Context, string, string) (bool, error) { return true, nil }

func TestSecurityOTPIssuanceHashesChallengeInAllEnvironments(t *testing.T) {
	for _, production := range []bool{false, true} {
		store := &securityIdentityStore{}
		service := &Service{Store: store, Limiter: &securityIdentityLimiter{}, SMS: securityIdentitySMS{}, Options: Options{Production: production}}
		issued, err := service.RequestOTP(context.Background(), "02055555555", "192.0.2.1")
		if err != nil || !issued.Requested || len(issued.Challenge) != 64 || store.otp.ChallengeHash != domain.Hash(issued.Challenge) {
			t.Fatal("issuance failed to return a secret and persist only its hash")
		}
		if production && issued.DevCode != "" || !production && len(issued.DevCode) != 6 {
			t.Fatal("development OTP disclosure does not match environment")
		}
	}
}

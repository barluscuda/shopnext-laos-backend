package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/crypto/scrypt"
	"shopnext-laos/internal/domain"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Service) Rate(ctx context.Context, scope, key string, max int, window time.Duration) error {
	retry, err := s.Limiter.Check(ctx, scope, key, max, window)
	if err != nil {
		return err
	}
	if retry > 0 {
		return &domain.Error{Code: "RATE_LIMITED", Message: "please retry later", Status: 429, RetryAfter: int(retry.Seconds()) + 1}
	}
	return nil
}
func HashPassword(password string) (string, error) {
	salt := domain.Token(16)
	key, err := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 64)
	if err != nil {
		return "", err
	}
	return "scrypt$" + salt + "$" + hex.EncodeToString(key), nil
}
func VerifyPassword(password, encoded string) bool {
	if len(password) > 1024 {
		return false
	}
	if !strings.HasPrefix(encoded, "scrypt$") {
		b, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return false
		}
		encoded = string(b)
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "scrypt" || len(parts[1]) != 32 || len(parts[2]) != 128 {
		return false
	}
	expected, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	key, err := scrypt.Key([]byte(password), []byte(parts[1]), 16384, 8, 1, 64)
	return err == nil && subtle.ConstantTimeCompare(key, expected) == 1
}
func (s *Service) Audit(ctx context.Context, st Store, a domain.Actor, action, target, id, note, ip string) error {
	return st.Insert(ctx, Audits, &domain.Audit{Base: domain.NewBase(), ActorEmail: a.Email, ActorRole: a.Role, ActorLegacy: a.Legacy, Action: action, TargetType: target, TargetID: id, Summary: note, IP: ip})
}
func Require(a domain.Actor, permission string) error {
	if a.ID == "" {
		return domain.Fail("UNAUTHENTICATED", 401)
	}
	if !domain.Can(a.Role, permission) {
		return domain.Fail("FORBIDDEN", 403)
	}
	return nil
}

func (s *Service) RequestOTP(ctx context.Context, rawPhone, ip string) (string, error) {
	phone := domain.NormalizePhone(rawPhone)
	if !domain.ValidPhone(phone) {
		return "", domain.Fail("INVALID_PHONE", 400)
	}
	if err := s.Rate(ctx, "otp-request-ip", ip, 5, time.Minute); err != nil {
		return "", err
	}
	code := domain.Digits()
	now := time.Now().UTC()
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if err := tx.LockKey(ctx, "otp:"+phone); err != nil {
			return err
		}
		recent, err := findOne[domain.OTP](ctx, tx, OTPs, Query{Eq: map[string]any{"phone": phone}, Sort: "created_at", Desc: true})
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if err == nil && recent.CreatedAt.Add(time.Minute).After(now) {
			return &domain.Error{Code: "RATE_LIMITED", Message: "OTP resend cooldown", Status: 429, RetryAfter: int(time.Until(recent.CreatedAt.Add(time.Minute)).Seconds()) + 1}
		}
		return tx.Insert(ctx, OTPs, &domain.OTP{Base: domain.NewBase(), Phone: phone, CodeHash: domain.Hash(code), ExpiresAt: now.Add(5 * time.Minute)})
	})
	if err != nil {
		return "", err
	}
	message := fmt.Sprintf("ShopNext Laos: ລະຫັດ OTP %s ໃຊ້ໄດ້ 5 ນາທີ", code)
	accepted, sendErr := s.SMS.Send(ctx, phone, message)
	if err := s.Store.Insert(ctx, SMSLogs, &domain.SMSLog{Base: domain.NewBase(), Phone: phone, Message: "OTP •••••• (5 minutes)", Accepted: sendErr == nil && accepted}); err != nil {
		return "", err
	}
	if s.Options.Production {
		return "", nil
	}
	return code, nil
}
func (s *Service) VerifyOTP(ctx context.Context, rawPhone, code string) (string, error) {
	phone := domain.NormalizePhone(rawPhone)
	if !domain.ValidPhone(phone) || len(code) != 6 {
		return "", domain.Fail("INVALID_OTP", 400)
	}
	var token string
	var businessErr error
	err := s.Store.Transaction(ctx, func(tx Store) error {
		if err := tx.LockKey(ctx, "otp:"+phone); err != nil {
			return err
		}
		record, err := findOne[domain.OTP](ctx, tx, OTPs, Query{Eq: map[string]any{"phone": phone}, Sort: "created_at", Desc: true, Lock: true})
		if errors.Is(err, domain.ErrNotFound) {
			businessErr = domain.Fail("OTP_NOT_FOUND", 400)
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		switch {
		case record.ConsumedAt != nil:
			businessErr = domain.Fail("OTP_NOT_FOUND", 400)
			return nil
		case record.Attempts >= 5:
			businessErr = domain.Fail("OTP_ATTEMPTS_EXCEEDED", 429)
			return nil
		case !record.ExpiresAt.After(now):
			businessErr = domain.Fail("OTP_EXPIRED", 400)
			return nil
		case subtle.ConstantTimeCompare([]byte(record.CodeHash), []byte(domain.Hash(code))) != 1:
			businessErr = domain.Fail("INVALID_OTP", 400)
			return tx.Update(ctx, OTPs, record.ID, changed(map[string]any{"attempts": record.Attempts + 1}))
		}
		if err := tx.Update(ctx, OTPs, record.ID, changed(map[string]any{"consumed_at": now})); err != nil {
			return err
		}
		token = domain.Token(32)
		return tx.Insert(ctx, PhoneTokens, &domain.PhoneToken{Base: domain.NewBase(), Phone: phone, TokenHash: domain.Hash(token), ExpiresAt: now.Add(PhoneKeyLifetime)})
	})
	if err != nil {
		return "", err
	}
	return token, businessErr
}
func (s *Service) VerifiedPhone(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	pt, err := findOne[domain.PhoneToken](ctx, s.Store, PhoneTokens, Query{Eq: map[string]any{"token_hash": domain.Hash(token)}, GT: map[string]any{"expires_at": time.Now()}})
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if pt.Uses >= PhoneKeyMaxUses {
		return "", nil
	}
	return pt.Phone, nil
}
func (s *Service) ClearPhone(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	pt, err := findOne[domain.PhoneToken](ctx, s.Store, PhoneTokens, Query{Eq: map[string]any{"token_hash": domain.Hash(token)}})
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Store.Delete(ctx, PhoneTokens, pt.ID)
}

func (s *Service) Login(ctx context.Context, email, password, ip string) (string, domain.Actor, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(password) > 1024 || len(email) > 254 {
		return "", domain.Actor{}, domain.Fail("INVALID_CREDENTIALS", 401)
	}
	if err := s.Rate(ctx, "staff-login-ip", ip, 10, 10*time.Minute); err != nil {
		return "", domain.Actor{}, err
	}
	if err := s.Rate(ctx, "staff-login-email", email, 5, 10*time.Minute); err != nil {
		return "", domain.Actor{}, err
	}
	var actor domain.Actor
	var token string
	if !strings.Contains(email, "@") && s.Options.LegacyPasswordHash != "" && VerifyPassword(password, s.Options.LegacyPasswordHash) {
		actor = domain.Actor{ID: "legacy", Email: "admin@local", Name: "Admin", Role: "OWNER", Legacy: true}
		body := fmt.Sprintf("legacy.%d.%s", time.Now().Add(8*time.Hour).Unix(), domain.Token(16))
		mac := hmac.New(sha256.New, []byte(s.Options.LegacySessionSecret))
		_, _ = mac.Write([]byte(body))
		token = body + "." + hex.EncodeToString(mac.Sum(nil))
		if err := s.Audit(ctx, s.Store, actor, "staff.login", "staff", actor.ID, "", ip); err != nil {
			return "", domain.Actor{}, err
		}
		return token, actor, nil
	}
	user, err := findOne[domain.Staff](ctx, s.Store, StaffUsers, Query{Eq: map[string]any{"email": email, "disabled_at": nil}})
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", actor, err
	}
	if err != nil || !VerifyPassword(password, user.PasswordHash) {
		a := domain.Actor{Email: email, Role: "UNKNOWN"}
		if auditErr := s.Audit(ctx, s.Store, a, "staff.login_failed", "staff", "", "", ip); auditErr != nil {
			return "", actor, auditErr
		}
		return "", actor, domain.Fail("INVALID_CREDENTIALS", 401)
	}
	actor = domain.Actor{ID: user.ID, Email: user.Email, Name: user.Name, Role: user.Role}
	token = domain.Token(32)
	err = s.Store.Transaction(ctx, func(tx Store) error {
		current, err := findOne[domain.Staff](ctx, tx, StaffUsers, locked(byID(user.ID)))
		if err != nil {
			return err
		}
		if current.DisabledAt != nil || current.PasswordHash != user.PasswordHash {
			return domain.Fail("INVALID_CREDENTIALS", 401)
		}
		if err := tx.Insert(ctx, Sessions, &domain.Session{Base: domain.NewBase(), UserID: user.ID, TokenHash: domain.Hash(token), ExpiresAt: time.Now().Add(8 * time.Hour)}); err != nil {
			return err
		}
		return s.Audit(ctx, tx, actor, "staff.login", "staff", user.ID, "", ip)
	})
	return token, actor, err
}
func (s *Service) Actor(ctx context.Context, token string) (domain.Actor, error) {
	if token == "" {
		return domain.Actor{}, nil
	}
	if strings.HasPrefix(token, "legacy.") {
		p := strings.Split(token, ".")
		if len(p) != 4 || s.Options.LegacySessionSecret == "" {
			return domain.Actor{}, nil
		}
		exp, err := strconv.ParseInt(p[1], 10, 64)
		sig, decodeErr := hex.DecodeString(p[3])
		if err != nil || decodeErr != nil || exp <= time.Now().Unix() {
			return domain.Actor{}, nil
		}
		mac := hmac.New(sha256.New, []byte(s.Options.LegacySessionSecret))
		_, _ = mac.Write([]byte(strings.Join(p[:3], ".")))
		if !hmac.Equal(sig, mac.Sum(nil)) {
			return domain.Actor{}, nil
		}
		return domain.Actor{ID: "legacy", Email: "admin@local", Name: "Admin", Role: "OWNER", Legacy: true}, nil
	}
	session, err := findOne[domain.Session](ctx, s.Store, Sessions, Query{Eq: map[string]any{"token_hash": domain.Hash(token), "revoked_at": nil}, GT: map[string]any{"expires_at": time.Now()}})
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Actor{}, nil
	}
	if err != nil {
		return domain.Actor{}, err
	}
	user, err := findOne[domain.Staff](ctx, s.Store, StaffUsers, Query{Eq: map[string]any{"id": session.UserID, "disabled_at": nil}})
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Actor{}, nil
	}
	return domain.Actor{ID: user.ID, Email: user.Email, Name: user.Name, Role: user.Role}, err
}
func (s *Service) Logout(ctx context.Context, token string) error {
	session, err := findOne[domain.Session](ctx, s.Store, Sessions, Query{Eq: map[string]any{"token_hash": domain.Hash(token), "revoked_at": nil}})
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Store.Update(ctx, Sessions, session.ID, changed(map[string]any{"revoked_at": time.Now()}))
}

type StaffInput struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

func (s *Service) CreateStaff(ctx context.Context, a domain.Actor, in StaffInput, bootstrap bool, ip string) (domain.Staff, error) {
	if !bootstrap {
		if err := Require(a, "staff.manage"); err != nil {
			return domain.Staff{}, err
		}
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	if !strings.Contains(in.Email, "@") || len(in.Email) > 254 || utf8.RuneCountInString(in.Name) < 2 || len(in.Name) > 240 || len(in.Password) < 10 || len(in.Password) > 1024 || !domain.ValidRole(in.Role) {
		return domain.Staff{}, domain.Fail("INVALID_STAFF", 400)
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return domain.Staff{}, err
	}
	user := domain.Staff{Base: domain.NewBase(), Email: in.Email, Name: in.Name, Role: in.Role, PasswordHash: hash}
	err = s.Store.Transaction(ctx, func(tx Store) error {
		if bootstrap {
			if err := tx.LockKey(ctx, "staff:bootstrap"); err != nil {
				return err
			}
			n, err := tx.Count(ctx, StaffUsers, Query{})
			if err != nil {
				return err
			}
			if n > 0 {
				return domain.Fail("OWNER_ALREADY_BOOTSTRAPPED", 409)
			}
			user.Role = "OWNER"
			a = domain.Actor{ID: "bootstrap", Email: "bootstrap", Role: "SYSTEM"}
		}
		if err := tx.Insert(ctx, StaffUsers, &user); err != nil {
			return err
		}
		return s.Audit(ctx, tx, a, "staff.create", "staff", user.ID, "role "+user.Role, ip)
	})
	return user, err
}
func (s *Service) ChangeStaff(ctx context.Context, a domain.Actor, id, action, password, ip string) error {
	if err := Require(a, "staff.manage"); err != nil {
		return err
	}
	values := map[string]any{}
	switch action {
	case "disable":
		if id == a.ID {
			return domain.Fail("CANNOT_DISABLE_SELF", 409)
		}
		values["disabled_at"] = time.Now()
	case "enable":
		values["disabled_at"] = nil
	case "reset-password":
		if len(password) < 10 || len(password) > 1024 {
			return domain.Fail("INVALID_PASSWORD", 400)
		}
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		values["password_hash"] = hash
		values["disabled_at"] = nil
	default:
		return domain.Fail("INVALID_ACTION", 400)
	}
	return s.Store.Transaction(ctx, func(tx Store) error {
		if _, err := findOne[domain.Staff](ctx, tx, StaffUsers, locked(byID(id))); err != nil {
			return err
		}
		if err := tx.Update(ctx, StaffUsers, id, changed(values)); err != nil {
			return err
		}
		if action != "enable" {
			var sessions []domain.Session
			if err := tx.Find(ctx, Sessions, Query{Eq: map[string]any{"user_id": id, "revoked_at": nil}}, &sessions); err != nil {
				return err
			}
			for _, session := range sessions {
				if err := tx.Update(ctx, Sessions, session.ID, changed(map[string]any{"revoked_at": time.Now()})); err != nil {
					return err
				}
			}
		}
		return s.Audit(ctx, tx, a, "staff."+action, "staff", id, "", ip)
	})
}

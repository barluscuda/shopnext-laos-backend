// Package application coordinates domain rules through infrastructure ports.
package application

import (
	"context"
	"io"
	"shopnext-laos/internal/domain"
	"time"
)

// Entity is an allowlisted persistence resource, never a user-supplied table.
type Entity string

const (
	Categories    Entity = "categories"
	Products      Entity = "products"
	Variants      Entity = "variants"
	Images        Entity = "images"
	HeroSlides    Entity = "hero_slides"
	PromoBanners  Entity = "promo_banners"
	Providers     Entity = "providers"
	Branches      Entity = "branches"
	Orders        Entity = "orders"
	Items         Entity = "items"
	OrderEvents   Entity = "order_events"
	Attempts      Entity = "attempts"
	PaymentEvents Entity = "payment_events"
	Refunds       Entity = "refunds"
	OTPs          Entity = "otp_codes"
	PhoneTokens   Entity = "phone_tokens"
	StaffUsers    Entity = "staff_users"
	Sessions      Entity = "staff_sessions"
	Audits        Entity = "audit_logs"
	OutboxEvents  Entity = "outbox_events"
	SMSLogs       Entity = "sms_logs"
	Idempotencies Entity = "idempotency_keys"
)

// Query describes persistence-independent predicates. Field names are validated
// by the adapter and HTTP query inputs are translated through explicit lists.
type Query struct {
	Eq         map[string]any
	In         map[string][]string
	LT         map[string]any
	GT         map[string]any
	Search     string
	Sort       string
	Desc       bool
	Limit      int
	Offset     int
	Lock       bool
	SkipLocked bool
}
type Store interface {
	Transaction(context.Context, func(Store) error) error
	LockKey(context.Context, string) error
	Find(context.Context, Entity, Query, any) error
	Count(context.Context, Entity, Query) (int64, error)
	Insert(context.Context, Entity, any) error
	Update(context.Context, Entity, string, map[string]any) error
	Delete(context.Context, Entity, string) error
	Ping(context.Context) error
	Catalog(context.Context, string, string, string, int, int) ([]domain.Product, int64, error)
}
type RateLimiter interface {
	Check(context.Context, string, string, int, time.Duration) (time.Duration, error)
	Ping(context.Context) error
}
type QR struct {
	TransactionID string
	Code          string
	Deeplink      string
}
type Callback struct {
	TransactionID string
	BillNumber    string
	Amount        *int64
	Status        string
	DedupeKey     string
}
type RefundResult struct {
	ID      string
	Status  string
	Success bool
	Failed  bool
	Amount  *int64
}
type PaymentProvider interface {
	Name() string
	CreateQR(context.Context, string, string, int64) (QR, error)
	ParseCallback([]byte) (Callback, error)
	SubmitRefund(context.Context, string) (RefundResult, error)
	RefundStatus(context.Context, string) (RefundResult, error)
}
type AmbiguousError struct{ Cause error }

func (e *AmbiguousError) Error() string { return "provider outcome unknown" }
func (e *AmbiguousError) Unwrap() error { return e.Cause }

type SMSProvider interface {
	Send(context.Context, string, string) (bool, error)
}
type MediaStorage interface {
	Save(context.Context, io.Reader, string, bool) (string, error)
	Delete(context.Context, string) error
	ValidateURL(string) bool
	ValidateContentURL(string) bool
}
type Options struct {
	Production          bool
	Hold                time.Duration
	LegacyPasswordHash  string
	LegacySessionSecret string
}
type Service struct {
	Store    Store
	Limiter  RateLimiter
	Payments PaymentProvider
	SMS      SMSProvider
	Media    MediaStorage
	Options  Options
}

func New(store Store, limiter RateLimiter, payments PaymentProvider, sms SMSProvider, media MediaStorage, options Options) *Service {
	return &Service{store, limiter, payments, sms, media, options}
}
func findOne[T any](ctx context.Context, st Store, e Entity, q Query) (T, error) {
	var rows []T
	q.Limit = 1
	err := st.Find(ctx, e, q, &rows)
	if err != nil {
		return *new(T), err
	}
	if len(rows) == 0 {
		return *new(T), domain.ErrNotFound
	}
	return rows[0], nil
}
func byID(id string) Query { return Query{Eq: map[string]any{"id": id}} }
func locked(q Query) Query { q.Lock = true; return q }
func changed(values map[string]any) map[string]any {
	values["updated_at"] = time.Now().UTC()
	return values
}

// Package domain defines framework-independent business records and rules.
package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
	"time"
)

type Error struct {
	Code       string
	Message    string
	Status     int
	RetryAfter int
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func Fail(code string, status int) error {
	return &Error{Code: code, Message: strings.ReplaceAll(strings.ToLower(code), "_", " "), Status: status}
}

var ErrNotFound = errors.New("record not found")

type Base struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewBase() Base {
	now := time.Now().UTC()
	return Base{ID: Token(16), CreatedAt: now, UpdatedAt: now}
}
func Token(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Hash(v string) string { s := sha256.Sum256([]byte(v)); return hex.EncodeToString(s[:]) }
func Digits() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%06d", n.Int64())
}
func BillNumber() string {
	alphabet := "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := []byte("B0000000")
	for i := 1; i < len(b); i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

var phoneRE = regexp.MustCompile(`^0(20|30|54|55|56)[0-9]{6,8}$`)

func NormalizePhone(s string) string {
	s = strings.NewReplacer(" ", "", "-", "", "\t", "", "\n", "").Replace(strings.TrimSpace(s))
	if strings.HasPrefix(s, "+856") {
		return "0" + s[4:]
	}
	if strings.HasPrefix(s, "856") {
		return "0" + s[3:]
	}
	return s
}
func ValidPhone(s string) bool { return phoneRE.MatchString(s) }
func MaskName(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		r := []rune(p)
		for j := 1; j < len(r)-1; j++ {
			r[j] = '_'
		}
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}
func LineTotal(unit, quantity int64) (int64, error) {
	if unit < 0 || quantity < 1 || quantity > 99 || unit > math.MaxInt64/quantity {
		return 0, Fail("INVALID_AMOUNT", 400)
	}
	return unit * quantity, nil
}
func AddMoney(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, Fail("INVALID_AMOUNT", 400)
	}
	return a + b, nil
}

type Category struct {
	Base
	ParentID  *string `json:"parent_id"`
	Name      string  `json:"name"`
	NameEn    string  `json:"name_en"`
	Slug      string  `json:"slug"`
	SortOrder int     `json:"sort_order"`
}
type Product struct {
	Base
	CategoryID   string     `json:"category_id"`
	SKU          string     `json:"sku"`
	Name         string     `json:"name"`
	NameEn       string     `json:"name_en"`
	Slug         string     `json:"slug"`
	Description  string     `json:"description"`
	BasePriceKip int64      `json:"base_price_kip"`
	Status       string     `json:"status"`
	DeletedAt    *time.Time `json:"deleted_at"`
}
type Variant struct {
	Base
	ProductID        string          `json:"product_id"`
	SKU              string          `json:"sku"`
	Attributes       json.RawMessage `json:"attributes"`
	PriceOverrideKip *int64          `json:"price_override_kip"`
	StockOnHand      int64           `json:"stock_on_hand"`
	StockReserved    int64           `json:"stock_reserved"`
	Position         int             `json:"position"`
}

func (v Variant) Available() int64 { return v.StockOnHand - v.StockReserved }

type Image struct {
	Base
	ProductID string `json:"product_id"`
	URL       string `json:"url"`
	AltText   string `json:"alt_text"`
	Position  int    `json:"position"`
}
type Content struct {
	Base
	ImageURL   string `json:"image_url"`
	AltText    string `json:"alt_text"`
	Heading    string `json:"heading"`
	Subheading string `json:"subheading"`
	CTALabel   string `json:"cta_label"`
	CTAHref    string `json:"cta_href"`
	Position   int    `json:"position"`
	Active     bool   `json:"active"`
}
type Provider struct {
	Base
	Name           string `json:"name"`
	ShippingFeeKip int64  `json:"shipping_fee_kip"`
}
type Branch struct {
	Base
	ProviderID string `json:"provider_id"`
	Province   string `json:"province"`
	City       string `json:"city"`
	Name       string `json:"name"`
}
type Order struct {
	BillNumber           string     `json:"bill_number"`
	PickupCode           string     `json:"pickup_code"`
	RecipientPhone       string     `json:"recipient_phone"`
	RecipientName        string     `json:"recipient_name"`
	ExpressProviderID    string     `json:"express_provider_id"`
	BranchID             string     `json:"branch_id"`
	PaymentType          string     `json:"payment_type"`
	PaymentMethod        string     `json:"payment_method"`
	PaymentStatus        string     `json:"payment_status"`
	OrderStatus          string     `json:"order_status"`
	SubtotalKip          int64      `json:"subtotal_kip"`
	ShippingFeeKip       int64      `json:"shipping_fee_kip"`
	TotalKip             int64      `json:"total_kip"`
	ReservationExpiresAt *time.Time `json:"reservation_expires_at"`
	VerifiedAt           time.Time  `json:"verified_at"`
	CreatedAt            time.Time  `json:"created_at"`
}
type Item struct {
	Base
	OrderID              string `json:"order_id"`
	ProductVariantID     string `json:"product_variant_id"`
	ProductNameSnapshot  string `json:"product_name_snapshot"`
	UnitPriceKipSnapshot int64  `json:"unit_price_kip_snapshot"`
	Quantity             int64  `json:"quantity"`
	LineTotalKip         int64  `json:"line_total_kip"`
}
type OrderEvent struct {
	Base
	OrderID string `json:"order_id"`
	Action  string `json:"action"`
	Note    string `json:"note"`
}
type Attempt struct {
	Base
	OrderID               string    `json:"order_id"`
	Provider              string    `json:"provider"`
	Bank                  string    `json:"bank"`
	ProviderTransactionID *string   `json:"provider_transaction_id"`
	AmountKip             int64     `json:"amount_kip"`
	Status                string    `json:"status"`
	QRCode                string    `json:"qr_code"`
	Deeplink              string    `json:"deeplink"`
	ExpiresAt             time.Time `json:"expires_at"`
	LastError             string    `json:"last_error"`
}
type PaymentEvent struct {
	Base
	Provider              string     `json:"provider"`
	ProviderTransactionID string     `json:"provider_transaction_id"`
	DedupeKey             string     `json:"dedupe_key"`
	BillNumber            string     `json:"bill_number"`
	NormalizedStatus      string     `json:"normalized_status"`
	AmountKip             *int64     `json:"amount_kip"`
	RawBody               string     `json:"raw_body"`
	Outcome               string     `json:"outcome"`
	ProcessedAt           *time.Time `json:"processed_at"`
}
type Refund struct {
	Base
	PaymentAttemptID     string     `json:"payment_attempt_id"`
	OrderID              string     `json:"order_id"`
	AmountKip            int64      `json:"amount_kip"`
	Reason               string     `json:"reason"`
	Status               string     `json:"status"`
	ProviderRefundBillID *string    `json:"provider_refund_bill_id"`
	ProviderStatus       string     `json:"provider_status"`
	RequestedBy          string     `json:"requested_by"`
	ApprovedBy           string     `json:"approved_by"`
	LastError            string     `json:"last_error"`
	SubmittedAt          *time.Time `json:"submitted_at"`
	CompletedAt          *time.Time `json:"completed_at"`
}
type OTP struct {
	Base
	Phone      string     `json:"phone"`
	CodeHash   string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Attempts   int        `json:"attempts"`
	ConsumedAt *time.Time `json:"consumed_at"`
}
type PhoneToken struct {
	Base
	Phone     string    `json:"phone"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	Uses      int       `json:"uses"`
}
type TrustedDevice struct {
	Base
	OrderID   string    `json:"order_id"`
	TokenHash string    `json:"-"`
	UserAgent string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
}
type ClientNotification struct {
	Base
	OrderID     string          `json:"order_id"`
	Payload     json.RawMessage `json:"-"`
	Status      string          `json:"status"`
	Attempts    int             `json:"attempts"`
	AvailableAt time.Time       `json:"available_at"`
	LeaseUntil  *time.Time      `json:"lease_until"`
	SentAt      *time.Time      `json:"sent_at"`
	LastError   string          `json:"last_error"`
}
type Staff struct {
	Base
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Role         string     `json:"role"`
	PasswordHash string     `json:"-"`
	DisabledAt   *time.Time `json:"disabled_at"`
}
type Session struct {
	Base
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}
type Actor struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Legacy bool   `json:"legacy"`
}
type Audit struct {
	Base
	ActorEmail  string `json:"actor_email"`
	ActorRole   string `json:"actor_role"`
	ActorLegacy bool   `json:"actor_legacy"`
	Action      string `json:"action"`
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id"`
	Summary     string `json:"summary"`
	IP          string `json:"ip"`
}
type Outbox struct {
	Base
	Phone       string     `json:"phone"`
	Message     string     `json:"-"`
	DedupeKey   string     `json:"dedupe_key"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error"`
	AvailableAt time.Time  `json:"available_at"`
	LeaseUntil  *time.Time `json:"lease_until"`
	SentAt      *time.Time `json:"sent_at"`
}
type SMSLog struct {
	Base
	Phone    string `json:"phone"`
	Message  string `json:"message"`
	Accepted bool   `json:"accepted"`
}
type Idempotency struct {
	Base
	Scope       string          `json:"scope"`
	Key         string          `json:"key"`
	RequestHash string          `json:"request_hash"`
	Response    json.RawMessage `json:"response"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

var rolePermissions = map[string][]string{
	"OWNER":      {"orders.view", "orders.act", "orders.cancel", "products.manage", "content.manage", "taxonomy.manage", "refunds.request", "refunds.approve", "refunds.resolve", "sms.view", "staff.manage", "dashboard.view"},
	"CATALOG":    {"orders.view", "products.manage", "content.manage", "taxonomy.manage", "dashboard.view"},
	"FULFILMENT": {"orders.view", "orders.act", "dashboard.view"},
	"FINANCE":    {"orders.view", "refunds.request", "refunds.approve", "refunds.resolve", "dashboard.view"},
	"SUPPORT":    {"orders.view", "orders.act", "orders.cancel", "refunds.request", "sms.view"},
	"AUDITOR":    {"orders.view", "sms.view", "dashboard.view"},
}

func ValidRole(role string) bool { _, ok := rolePermissions[role]; return ok }
func Can(role, permission string) bool {
	for _, p := range rolePermissions[role] {
		if p == permission {
			return true
		}
	}
	return false
}

var Banks = map[string]string{"BCEL": "bcel", "JDB": "jdb", "LDB": "ldb", "IB": "ib", "STB": "stb", "MMONEYX": "m-money"}

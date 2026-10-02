package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chai2010/webp"
	"github.com/pressly/goose/v3"
	"go.uber.org/zap"
	"image"
	api "shopnext-laos/internal/adapters/http"
	"shopnext-laos/internal/adapters/media"
	"shopnext-laos/internal/adapters/postgres"
	"shopnext-laos/internal/adapters/providers"
	cache "shopnext-laos/internal/adapters/redis"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"shopnext-laos/internal/platform"
	"shopnext-laos/migrations"
)

var ctx = context.Background()
var owner = domain.Actor{ID: "owner", Email: "owner@example.test", Name: "Owner", Role: "OWNER"}

const phone = "02055555555"

type unrestricted struct{}

func (unrestricted) Check(context.Context, string, string, int, time.Duration) (time.Duration, error) {
	return 0, nil
}
func (unrestricted) Ping(context.Context) error { return nil }

type adminLogin struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	ExpiresIn   int          `json:"expires_in"`
	Actor       domain.Actor `json:"actor"`
}

type harness struct {
	s       *application.Service
	db      *postgres.Store
	redis   *cache.Limiter
	handler http.Handler
	cfg     platform.Config
}

func setup(t *testing.T) *harness {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL and TEST_REDIS_URL for isolated integration tests")
	}
	db, err := postgres.Open(dsn)
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var name string
	must(t, db.DB.Raw("SELECT current_database()").Scan(&name).Error)
	if name != "shopnext_test" {
		t.Fatal("refusing to reset a non-test database")
	}
	pool, err := db.DB.DB()
	must(t, err)
	goose.SetBaseFS(migrations.FS)
	must(t, goose.SetDialect("postgres"))
	must(t, goose.Up(pool, "."))
	must(t, db.DB.Exec("TRUNCATE client_notifications,trusted_devices,idempotency_keys,sms_logs,outbox_events,audit_logs,staff_sessions,staff_users,phone_tokens,otp_codes,refunds,payment_events,attempts,order_events,items,orders,branches,providers,promo_banners,hero_slides,images,variants,products,categories CASCADE").Error)
	redisURL, err := url.Parse(os.Getenv("TEST_REDIS_URL"))
	must(t, err)
	if redisURL.Host != "localhost:56380" && redisURL.Host != "127.0.0.1:56380" && redisURL.Host != "shopnext-test-redis:6379" {
		t.Fatal("test Redis must be the isolated test Compose service")
	}
	redisURL.Path = "/15"
	limiter, err := cache.New(redisURL.String())
	must(t, err)
	t.Cleanup(func() { _ = limiter.Close() })
	must(t, limiter.Client.FlushDB(ctx).Err())
	cfg := platform.Config{Env: "test", UploadDir: t.TempDir(), AllowedOrigins: []string{"http://localhost:3000"}, Hold: 15 * time.Minute, MaintenanceSecret: strings.Repeat("m", 32), WebhookSecret: strings.Repeat("w", 32), WebhookHeader: "x-phajay-signature"}
	s := application.New(db, limiter, providers.NewPayment("", "", true), providers.NewSMS("", "", "", true, true), &media.Storage{Dir: cfg.UploadDir}, application.Options{AdminJWTSecret: strings.Repeat("a", 32), Hold: cfg.Hold, TrustDeviceSecret: strings.Repeat("t", 32)})
	r, err := api.New(s, cfg, zap.NewNop())
	must(t, err)
	return &harness{s, db, limiter, r, cfg}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
func rows[T any](t *testing.T, h *harness, e application.Entity, q application.Query) []T {
	t.Helper()
	var out []T
	must(t, h.db.Find(ctx, e, q, &out))
	return out
}
func fixture(t *testing.T, h *harness, stock int64) (domain.Product, domain.Variant, domain.Provider) {
	t.Helper()
	cat, err := h.s.SaveCategory(ctx, owner, "", application.CategoryInput{Name: "ເຄື່ອງໃຊ້", NameEn: "Home", Slug: "home"}, "test")
	must(t, err)
	p, err := h.s.SaveProduct(ctx, owner, "", application.ProductInput{CategoryID: cat.ID, Name: "ສິນຄ້າ", NameEn: "Product", Slug: "product", BasePriceKip: 50000, Status: "ACTIVE", InitialStock: stock}, "test")
	must(t, err)
	provider, err := h.s.SaveProvider(ctx, owner, "", domain.Provider{Name: "Express", ShippingFeeKip: 20000}, "test")
	must(t, err)
	return p, rows[domain.Variant](t, h, application.Variants, application.Query{})[0], provider
}
func input(v domain.Variant, p domain.Provider, payment string) application.Checkout {
	geo := application.Geography()[0]
	return application.Checkout{RecipientName: "Alice Customer", RecipientPhone: phone, ProviderID: p.ID, Province: geo.Name, City: geo.Districts[0], BranchName: "Main branch", PaymentType: payment, PaymentMethod: "BCEL", Items: []application.CheckoutItem{{VariantID: v.ID, Quantity: 1}}}
}
func request(t *testing.T, h *harness, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		must(t, json.NewEncoder(&b).Encode(body))
	}
	req := httptest.NewRequest(method, path, &b)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ShopNext-CSRF", "1")
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("User-Agent", "ShopNext-test-device")
	for _, c := range cookies {
		if c.Name == api.PhoneKeyHeader || c.Name == api.TrustDeviceHeader || c.Name == "Authorization" {
			req.Header.Set(c.Name, c.Value)
		} else {
			req.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	return w
}
func expect(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("HTTP %d wanted %d: %s", w.Code, status, w.Body.String())
	}
}
func data[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out struct {
		Data T `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.Data
}
func checkout(t *testing.T, h *harness, in application.Checkout, key string) application.CheckoutResult {
	t.Helper()
	r, err := h.s.Checkout(ctx, in, phone, key, domain.Token(4))
	must(t, err)
	return r
}
func callback(a domain.Attempt, bill string, amount int64, ref string) []byte {
	raw, _ := json.Marshal(map[string]any{"transactionId": *a.ProviderTransactionID, "billNumber": bill, "txnAmount": amount, "status": "PAYMENT_COMPLETED", "refNo": ref})
	return raw
}

func TestHTTPShoppingAndAdmin(t *testing.T) {
	h := setup(t)
	p, v, provider := fixture(t, h, 10)
	staff, err := h.s.CreateStaff(ctx, domain.Actor{}, application.StaffInput{Email: owner.Email, Name: owner.Name, Role: "OWNER", Password: "correct-password-123"}, true, "test")
	must(t, err)
	w := request(t, h, "POST", "/api/v1/admin/auth/login", map[string]any{"email": staff.Email, "password": "correct-password-123"})
	expect(t, w, 200)
	login := data[adminLogin](t, w)
	if len(w.Result().Cookies()) != 0 || login.TokenType != "Bearer" || login.ExpiresIn != 28800 || login.Actor.ID != staff.ID || len(strings.Split(login.AccessToken, ".")) != 3 {
		t.Fatal("admin JWT login contract")
	}
	staffBearer := &http.Cookie{Name: "Authorization", Value: "Bearer " + login.AccessToken}
	for _, path := range []string{"/api/v1/health/live", "/api/v1/health/ready", "/api/v1/products", "/api/v1/products/product", "/api/v1/products/product/related", "/api/v1/categories", "/api/v1/content", "/api/v1/delivery-options", "/api/v1/geography", "/api/v1/payment-methods"} {
		expect(t, request(t, h, "GET", path, nil), 200)
	}
	expect(t, request(t, h, "POST", "/api/v1/orders", input(v, provider, "ONLINE")), 403)
	w = request(t, h, "POST", "/api/v1/otp/request", map[string]string{"phone": phone})
	expect(t, w, 200)
	code := data[map[string]any](t, w)["dev_code"].(string)
	challenge := data[map[string]any](t, w)["challenge"].(string)
	expect(t, request(t, h, "POST", "/api/v1/otp/request", map[string]string{"phone": phone}), 429)
	w = request(t, h, "POST", "/api/v1/otp/verify", map[string]string{"phone": phone, "code": code, "challenge": challenge})
	expect(t, w, 200)
	pc := &http.Cookie{Name: api.PhoneKeyHeader, Value: data[map[string]any](t, w)["verification_key"].(string)}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("customer authentication must not set cookies")
	}
	expect(t, request(t, h, "POST", "/api/v1/otp/verify", map[string]string{"phone": phone, "code": code, "challenge": challenge}), 400)
	expect(t, request(t, h, "GET", "/api/v1/phone-verification", nil, pc), 200)
	w = request(t, h, "POST", "/api/v1/orders", input(v, provider, "ONLINE"), pc)
	expect(t, w, 201)
	bill := data[application.CheckoutResult](t, w)
	if bill.TotalKip != 70000 || bill.PickupCode != "" || bill.Payment == nil || bill.Payment.QRCode == "" {
		t.Fatalf("bad checkout %+v", bill)
	}
	w = request(t, h, "GET", "/api/v1/bills/"+bill.BillNumber, nil)
	expect(t, w, 200)
	public := data[application.BillSummary](t, w)
	if public.PickupCodeReady || public.PickupCode != "" || public.PaymentSuccessful {
		t.Fatal("public bill privacy/parity")
	}
	w = request(t, h, "GET", "/api/v1/bills/"+bill.BillNumber+"/details", nil, pc)
	expect(t, w, 200)
	private := data[application.ClientBill](t, w)
	if private.PaymentAttempt == nil || private.PaymentAttempt.QRCode == "" {
		t.Fatal("owner QR missing")
	}
	expect(t, request(t, h, "GET", "/api/v1/bills?phone="+phone, nil), 403)
	expect(t, request(t, h, "GET", "/api/v1/bills?phone="+phone, nil, pc), 200)
	expect(t, request(t, h, "GET", "/api/v1/bills/search?q="+bill.BillNumber, nil), 200)
	trust := &http.Cookie{Name: api.TrustDeviceHeader, Value: private.TrustDeviceKey}
	expect(t, request(t, h, "POST", "/api/v1/bills/"+bill.BillNumber+"/simulate-payment", nil, trust), 200)
	expect(t, request(t, h, "GET", "/api/v1/admin/orders/"+bill.BillNumber, nil, staffBearer), 200)
	expect(t, request(t, h, "POST", "/api/v1/admin/orders/"+bill.BillNumber+"/mark-delivered", map[string]string{"note": "collected"}, staffBearer), 200)
	expect(t, request(t, h, "GET", "/api/v1/admin/orders/export", nil, staffBearer), 200)
	for _, path := range []string{"dashboard", "orders", "products", "categories", "providers", "branches", "hero-slides", "promo-banners", "refunds", "staff", "audit-logs", "sms-logs", "outbox", "payment-events", "payment-attempts"} {
		expect(t, request(t, h, "GET", "/api/v1/admin/"+path, nil, staffBearer), 200)
	}
	w = request(t, h, "GET", "/api/v1/admin/products/"+p.ID, nil, staffBearer)
	expect(t, w, 200)
	worker := application.Worker{Service: h.s}
	_, err = worker.DrainOutbox(ctx)
	must(t, err)
	sent := rows[domain.Outbox](t, h, application.OutboxEvents, application.Query{Eq: map[string]any{"status": "SENT"}})
	if len(sent) != 2 {
		t.Fatalf("notifications %d", len(sent))
	}
	expect(t, request(t, h, "DELETE", "/api/v1/phone-verification", nil, pc), 200)
	if verified, err := h.s.VerifiedPhone(ctx, pc.Value); err != nil || verified != "" {
		t.Fatal("token not revoked")
	}
	expect(t, request(t, h, "POST", "/api/v1/admin/auth/logout", nil, staffBearer), 200)
	expect(t, request(t, h, "GET", "/api/v1/admin/orders", nil, staffBearer), 401)
}

func TestCheckoutAtomicIdempotencyAndStock(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 4)
	h.s.Limiter = unrestricted{}
	in := input(v, p, "ONLINE")
	var wg sync.WaitGroup
	var success atomic.Int32
	var first application.CheckoutResult
	var mu sync.Mutex
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := h.s.Checkout(ctx, in, phone, "same-key", "ip")
			if err != nil {
				t.Error(err)
				return
			}
			success.Add(1)
			mu.Lock()
			if first.BillNumber == "" {
				first = r
			} else if first.BillNumber != r.BillNumber || first.PickupCode != r.PickupCode || first.TotalKip != r.TotalKip || first.OrderID != r.OrderID {
				t.Error("idempotent order identity or total changed")
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if success.Load() != 12 {
		t.Fatal("idempotency requests failed")
	}
	orders := rows[domain.Order](t, h, application.Orders, application.Query{})
	if len(orders) != 1 {
		t.Fatalf("orders=%d", len(orders))
	}
	attempts := rows[domain.Attempt](t, h, application.Attempts, application.Query{})
	if len(attempts) != 1 || attempts[0].Status != "CREATED" || attempts[0].QRCode == "" {
		t.Fatal("idempotent checkout must generate one payment QR")
	}
	vv := rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if vv.StockReserved != 1 || vv.StockOnHand != 4 {
		t.Fatalf("stock %+v", vv)
	}
	in.RecipientName = "Different Customer"
	_, err := h.s.Checkout(ctx, in, phone, "same-key", "ip")
	wantCode(t, err, "IDEMPOTENCY_CONFLICT")
	in = input(v, p, "COD_PROVIDER")
	success.Store(0)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.s.Checkout(ctx, in, phone, domain.Token(8), "ip")
			if err == nil {
				success.Add(1)
			} else {
				var e *domain.Error
				if !errors.As(err, &e) || e.Code != "STOCK_CONFLICT" {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if success.Load() != 3 {
		t.Fatalf("oversell: successes=%d", success.Load())
	}
	vv = rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if vv.StockOnHand != 1 || vv.StockReserved != 1 {
		t.Fatalf("stock %+v", vv)
	}
	must(t, h.s.OrderAction(ctx, owner, first.BillNumber, "CANCELLED", "test", "ip"))
	vv = rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if vv.StockOnHand != 1 || vv.StockReserved != 0 {
		t.Fatal("cancel did not release hold")
	}
}

type failStore struct {
	application.Store
	entity application.Entity
}

func (f failStore) Transaction(c context.Context, fn func(application.Store) error) error {
	return f.Store.Transaction(c, func(tx application.Store) error { return fn(failStore{tx, f.entity}) })
}
func (f failStore) Insert(c context.Context, e application.Entity, v any) error {
	if e == f.entity {
		return errors.New("injected persistence failure")
	}
	return f.Store.Insert(c, e, v)
}
func TestCheckoutRollsBackNotificationsAndIdempotency(t *testing.T) {
	for _, entity := range []application.Entity{application.OutboxEvents, application.Idempotencies} {
		t.Run(string(entity), func(t *testing.T) {
			h := setup(t)
			_, v, p := fixture(t, h, 2)
			h.s.Store = failStore{h.db, entity}
			_, err := h.s.Checkout(ctx, input(v, p, "ONLINE"), phone, "rollback", "ip")
			if err == nil {
				t.Fatal("expected transaction failure")
			}
			for _, e := range []application.Entity{application.Orders, application.Items, application.OutboxEvents, application.Idempotencies, application.Branches} {
				n, err := h.db.Count(ctx, e, application.Query{})
				must(t, err)
				if n != 0 {
					t.Fatalf("%s escaped rollback", e)
				}
			}
			vv := rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
			if vv.StockReserved != 0 || vv.StockOnHand != 2 {
				t.Fatal("stock escaped rollback")
			}
		})
	}
}

func TestPaymentsExpiryAndRefundDualControl(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 4)
	r := checkout(t, h, input(v, p, "ONLINE"), "pay")
	a := rows[domain.Attempt](t, h, application.Attempts, application.Query{})[0]
	outcome, err := h.s.Webhook(ctx, callback(a, r.BillNumber, r.TotalKip+1, "wrong"))
	must(t, err)
	if outcome != "AMOUNT_MISMATCH" {
		t.Fatal(outcome)
	}
	raw := callback(a, r.BillNumber, r.TotalKip, "correct")
	outcome, err = h.s.Webhook(ctx, raw)
	must(t, err)
	if outcome != "APPLIED" {
		t.Fatal(outcome)
	}
	outcome, err = h.s.Webhook(ctx, raw)
	must(t, err)
	if outcome != "DEDUPED" {
		t.Fatal(outcome)
	}
	vv := rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if vv.StockOnHand != 3 || vv.StockReserved != 0 {
		t.Fatal("payment inventory conversion")
	}
	must(t, h.s.OrderAction(ctx, owner, r.BillNumber, "CANCELLED", "customer request", "ip"))
	bill, err := h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.PaymentStatus != "PAID" {
		t.Fatal("cancel must not invent a refund")
	}
	refund, err := h.s.RequestRefund(ctx, owner, r.BillNumber, "return", "ip")
	must(t, err)
	wantCode(t, h.s.ApproveRefund(ctx, owner, refund.ID, "ip"), "SELF_APPROVAL")
	finance := domain.Actor{ID: "finance", Email: "finance@example.test", Role: "FINANCE"}
	must(t, h.s.ApproveRefund(ctx, finance, refund.ID, "ip"))
	must(t, h.s.PollRefunds(ctx))
	bill, err = h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.PaymentStatus != "REFUNDED" {
		t.Fatal("refund completion")
	}
	vv = rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if vv.StockOnHand != 4 {
		t.Fatal("refund changed inventory twice")
	}
	r = checkout(t, h, input(v, p, "ONLINE"), "expire")
	a = rows[domain.Attempt](t, h, application.Attempts, application.Query{Eq: map[string]any{"order_id": r.BillNumber}})[0]
	must(t, h.db.Update(ctx, application.Orders, r.BillNumber, map[string]any{"reservation_expires_at": time.Now().Add(-time.Second)}))
	n, err := h.s.Expire(ctx, "")
	must(t, err)
	if n != 1 {
		t.Fatal(n)
	}
	n, err = h.s.Expire(ctx, "")
	must(t, err)
	if n != 0 {
		t.Fatal("double expiry")
	}
	outcome, err = h.s.Webhook(ctx, callback(a, r.BillNumber, r.TotalKip, "late"))
	must(t, err)
	if outcome != "RECONCILIATION_REQUIRED" {
		t.Fatal(outcome)
	}
	vv = rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if vv.StockReserved != 0 || vv.StockOnHand != 4 {
		t.Fatal("expiry stock")
	}
}

type earlyPayment struct {
	*providers.Payment
	notify func(application.QR, string, int64)
}

func (p earlyPayment) CreateQR(_ context.Context, bill, bank string, amount int64) (application.QR, error) {
	q := application.QR{TransactionID: "early-" + bill, Code: "qr"}
	p.notify(q, bill, amount)
	return q, nil
}
func TestCallbackBeforeQRResponse(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 2)
	h.s.Payments = earlyPayment{providers.NewPayment("", "", true), func(q application.QR, bill string, amount int64) {
		a := domain.Attempt{ProviderTransactionID: &q.TransactionID}
		outcome, err := h.s.Webhook(ctx, callback(a, bill, amount, "early"))
		must(t, err)
		if outcome != "UNMATCHED" {
			t.Fatal(outcome)
		}
	}}
	r := checkout(t, h, input(v, p, "ONLINE"), "early")
	must(t, h.s.ReconcileEvents(ctx))
	bill, err := h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.PaymentStatus != "PAID" {
		t.Fatal("early callback lost")
	}
}

func TestStaffPermissionsRevocationAndCatalogEditing(t *testing.T) {
	h := setup(t)
	p, v, _ := fixture(t, h, 10)
	bootstrap, err := h.s.CreateStaff(ctx, domain.Actor{}, application.StaffInput{Email: owner.Email, Name: "Owner", Role: "OWNER", Password: "correct-password"}, true, "ip")
	must(t, err)
	a := owner
	a.ID = bootstrap.ID
	auditor, err := h.s.CreateStaff(ctx, a, application.StaffInput{Email: "auditor@example.test", Name: "Auditor", Role: "AUDITOR", Password: "correct-password"}, false, "ip")
	must(t, err)
	token, actor, err := h.s.Login(ctx, auditor.Email, "correct-password", "ip")
	must(t, err)
	ac := &http.Cookie{Name: "Authorization", Value: "Bearer " + token}
	expect(t, request(t, h, "GET", "/api/v1/admin/orders/export", nil, ac), 200)
	expect(t, request(t, h, "GET", "/api/v1/admin/products", nil, ac), 403)
	_, err = h.s.SaveVariant(ctx, actor, p.ID, v.ID, application.VariantInput{SKU: v.SKU, StockOnHand: 5}, "ip")
	wantCode(t, err, "FORBIDDEN")
	must(t, h.s.ChangeStaff(ctx, a, auditor.ID, "disable", "", "ip"))
	expect(t, request(t, h, "GET", "/api/v1/admin/orders", nil, ac), 401)
	must(t, h.s.ChangeStaff(ctx, a, auditor.ID, "enable", "", "ip"))
	got, err := h.s.Actor(ctx, token)
	must(t, err)
	if got.ID != "" {
		t.Fatal("disabled sessions resurrected")
	}
	must(t, h.s.ChangeStaff(ctx, a, auditor.ID, "reset-password", "new-password-123", "ip"))
	_, _, err = h.s.Login(ctx, auditor.Email, "correct-password", "new-ip")
	wantCode(t, err, "INVALID_CREDENTIALS")
	_, _, err = h.s.Login(ctx, auditor.Email, "new-password-123", "new-ip")
	must(t, err)
	err = h.s.DeleteVariant(ctx, a, p.ID, v.ID, "ip")
	wantCode(t, err, "LAST_VARIANT")
	must(t, h.s.ProductVisibility(ctx, a, p.ID, false, "ip"))
	_, err = h.s.Product(ctx, p.Slug, false)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted product public")
	}
	must(t, h.s.ProductVisibility(ctx, a, p.ID, true, "ip"))
	_, err = h.s.Product(ctx, p.Slug, false)
	must(t, err)
	child, err := h.s.SaveCategory(ctx, a, "", application.CategoryInput{Name: "Child", NameEn: "Child", Slug: "child", ParentID: &p.CategoryID}, "ip")
	must(t, err)
	_, err = h.s.SaveCategory(ctx, a, p.CategoryID, application.CategoryInput{Name: "Home", NameEn: "Home", Slug: "home", ParentID: &child.ID}, "ip")
	wantCode(t, err, "CATEGORY_CYCLE")
}

func TestMediaContentAndInputSecurity(t *testing.T) {
	h := setup(t)
	p, _, _ := fixture(t, h, 10)
	var buf bytes.Buffer
	must(t, webp.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 600, 200)), &webp.Options{Lossless: true}))
	url, err := h.s.Media.Save(ctx, &buf, "image/webp", true)
	must(t, err)
	content, err := h.s.SaveContent(ctx, owner, application.HeroSlides, "", domain.Content{ImageURL: url, CTAHref: "/products", Heading: "Hello", Active: true}, "ip")
	must(t, err)
	_, err = h.s.SaveImage(ctx, owner, p.ID, application.ImageInput{URL: url}, "ip")
	must(t, err)
	must(t, h.s.DeleteResource(ctx, owner, application.HeroSlides, content.ID, "ip"))
	if !h.s.Media.ValidateURL(url) {
		t.Fatal("shared image deleted")
	}
	img := rows[domain.Image](t, h, application.Images, application.Query{})[0]
	must(t, h.s.DeleteImage(ctx, owner, p.ID, img.ID, "ip"))
	if h.s.Media.ValidateURL(url) {
		t.Fatal("unreferenced image not deleted")
	}
	req := httptest.NewRequest("POST", "/api/v1/otp/request", strings.NewReader(`{"phone":"02055555555"}`))
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	expect(t, w, 403)
	req = httptest.NewRequest("GET", "/api/v1/products", nil)
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	expect(t, w, 403)
	expect(t, request(t, h, "POST", "/api/v1/otp/request", map[string]any{"phone": phone, "unknown": true}), 400)
	expect(t, request(t, h, "GET", "/api/v1/products?sort=invalid", nil), 400)
	expect(t, request(t, h, "GET", "/api/v1/products?limit=100000", nil), 400)
	// Production rejects dev payment routes even if an adapter is miswired.
	prod := h.cfg
	prod.Env = "production"
	r, err := api.New(h.s, prod, zap.NewNop())
	must(t, err)
	req = httptest.NewRequest("POST", "/api/v1/bills/BABCDEFG/simulate-payment", nil)
	req.Header.Set("X-ShopNext-CSRF", "1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	expect(t, w, 404)
	req = httptest.NewRequest("POST", "/api/v1/webhooks/phajay", strings.NewReader(`{"transactionId":"x"}`))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	expect(t, w, 401)
}

func TestOTPFailureLimitAndRedisWindow(t *testing.T) {
	h := setup(t)
	issued, err := h.s.RequestOTP(ctx, phone, "ip")
	must(t, err)
	wrong := "000000"
	if issued.DevCode == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		_, err = h.s.VerifyOTP(ctx, phone, wrong, issued.Challenge, "ip", "test-device")
		wantCode(t, err, "INVALID_OTP")
	}
	_, err = h.s.VerifyOTP(ctx, phone, issued.DevCode, issued.Challenge, "ip", "test-device")
	wantCode(t, err, "OTP_ATTEMPTS_EXCEEDED")
	otp := rows[domain.OTP](t, h, application.OTPs, application.Query{})[0]
	if otp.Attempts != 5 || otp.CodeHash == issued.DevCode || otp.ChallengeHash != domain.Hash(issued.Challenge) {
		t.Fatal("OTP persistence")
	}
	for i := 0; i < 3; i++ {
		retry, err := h.redis.Check(ctx, "test-window", "key", 2, time.Second)
		must(t, err)
		if (i == 2) != (retry > 0) {
			t.Fatal("Redis limiter window")
		}
	}
}

func TestHistoricalSnapshotsAndCODDelivery(t *testing.T) {
	h := setup(t)
	p, v, provider := fixture(t, h, 2)
	r := checkout(t, h, input(v, provider, "COD_PROVIDER"), "cod")
	_, err := h.s.SaveProduct(ctx, owner, p.ID, application.ProductInput{CategoryID: p.CategoryID, SKU: p.SKU, Name: "Renamed Product", NameEn: "Renamed", Slug: p.Slug, BasePriceKip: 99000, Status: "ACTIVE"}, "ip")
	must(t, err)
	bill, err := h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.TotalKip != 70000 || bill.Items[0].ProductNameSnapshot != p.Name || bill.Items[0].UnitPriceKipSnapshot != 50000 || bill.OrderStatus != "CONFIRMED" {
		t.Fatal("historical snapshots changed")
	}
	must(t, h.s.OrderAction(ctx, owner, r.BillNumber, "DELIVERED", "", "ip"))
	bill, err = h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.OrderStatus != "DELIVERED" || bill.PaymentStatus != "PENDING" {
		t.Fatal("COD parity")
	}
}

func TestCatalogDisplayPriceAndCategoryFilter(t *testing.T) {
	h := setup(t)
	p, v, _ := fixture(t, h, 0)
	override := int64(70000)
	_, err := h.s.SaveVariant(ctx, owner, p.ID, "", application.VariantInput{SKU: "SECOND", StockOnHand: 3, PriceOverrideKip: &override, Position: 1}, "ip")
	must(t, err)
	cards, total, err := h.s.Catalog(ctx, "home", "product", "price-asc", 1, 30)
	must(t, err)
	if total != 1 || len(cards) != 1 || cards[0].DisplayPriceKip != 70000 || cards[0].DisplayVariantID == v.ID {
		t.Fatal("first available display variant parity")
	}
	cards, total, err = h.s.Catalog(ctx, "missing", "", "newest", 1, 30)
	must(t, err)
	if total != 0 || len(cards) != 0 {
		t.Fatal("category filter")
	}
	_, _, err = h.s.Catalog(ctx, "", `%' OR true --`, "newest", 1, 30)
	must(t, err)
}

func Example_checkoutWorkflow() {
	fmt.Println("OTP request → OTP verify → checkout → owner QR → authenticated callback → confirmed bill") // Output: OTP request → OTP verify → checkout → owner QR → authenticated callback → confirmed bill
}

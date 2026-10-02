package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	api "shopnext-laos/internal/adapters/http"
	"shopnext-laos/internal/adapters/providers"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func verificationKey(t *testing.T, h *harness, number string) string {
	t.Helper()
	code, err := h.s.RequestOTP(ctx, number, "test")
	must(t, err)
	key, err := h.s.VerifyOTP(ctx, number, code)
	must(t, err)
	record := rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{Eq: map[string]any{"token_hash": domain.Hash(key)}})[0]
	if record.Uses != 0 || time.Until(record.ExpiresAt) < 72*time.Hour-time.Minute || time.Until(record.ExpiresAt) > 72*time.Hour {
		t.Fatal("phone key lifetime or usage")
	}
	return key
}

func TestClientHeaderKeysAndDeviceOwnership(t *testing.T) {
	h := setup(t)
	h.s.Limiter = unrestricted{}
	_, variant, provider := fixture(t, h, 20)
	key := verificationKey(t, h, phone)
	first, err := h.s.CheckoutWithPhoneKey(ctx, input(variant, provider, "ONLINE"), key, "first", "ip")
	must(t, err)
	if first.PickupCode != "" || first.OrderID != first.BillNumber || first.Payment == nil || first.Payment.QRCode == "" {
		t.Fatal("online checkout response")
	}
	_, err = h.s.CheckoutWithPhoneKey(ctx, input(variant, provider, "ONLINE"), "", "missing", "ip")
	wantCode(t, err, "PHONE_VERIFICATION_REQUIRED")
	// A legacy cookie grants no customer access.
	expect(t, request(t, h, "GET", "/api/v1/bills/"+first.BillNumber+"/details", nil, &http.Cookie{Name: "shopnext_phone", Value: key}), 403)
	expect(t, request(t, h, "GET", "/api/v1/bills/"+first.BillNumber+"/details", nil), 403)
	credential := &http.Cookie{Name: api.PhoneKeyHeader, Value: key}
	w := request(t, h, "GET", "/api/v1/bills/"+first.BillNumber+"/details", nil, credential)
	expect(t, w, 200)
	detail := data[application.ClientBill](t, w)
	if detail.TrustDeviceKey == "" || detail.RecipientPhone != phone || detail.PaymentAttempt == nil {
		t.Fatal("protected bill and trust key")
	}
	trust := &http.Cookie{Name: api.TrustDeviceHeader, Value: detail.TrustDeviceKey}
	before := rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses
	for i := 0; i < 8; i++ {
		expect(t, request(t, h, "GET", "/api/v1/bills/"+first.BillNumber+"/details", nil, trust), 200)
	}
	if after := rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses; after != before {
		t.Fatal("trust access consumed OTP use")
	}
	_, err = h.s.ClientBill(ctx, first.BillNumber, "", detail.TrustDeviceKey, "changed-device")
	wantCode(t, err, "INVALID_TRUST_DEVICE_KEY")
	_, err = h.s.ClientBill(ctx, first.BillNumber, "", detail.TrustDeviceKey+"x", "ShopNext-test-device")
	wantCode(t, err, "INVALID_TRUST_DEVICE_KEY")
	second, err := h.s.CheckoutWithPhoneKey(ctx, input(variant, provider, "COD_PROVIDER"), key, "second", "ip")
	must(t, err)
	if second.Payment != nil || second.PickupCode != "" || second.OrderStatus != "CONFIRMED" {
		t.Fatal("COD response")
	}
	_, err = h.s.ClientBill(ctx, second.BillNumber, "", detail.TrustDeviceKey, "ShopNext-test-device")
	wantCode(t, err, "INVALID_TRUST_DEVICE_KEY")
	otherKey := verificationKey(t, h, "02066666666")
	_, err = h.s.ClientBill(ctx, first.BillNumber, otherKey, "", "ShopNext-test-device")
	wantCode(t, err, "PHONE_OWNERSHIP_MISMATCH")
	// Expiry, exhaustion and idempotency: exact checkout replay costs no use.
	for i := 0; i < 2; i++ {
		_, err = h.s.ClientBills(ctx, phone, key)
		must(t, err)
	}
	_, err = h.s.ClientBills(ctx, phone, key)
	wantCode(t, err, "PHONE_KEY_EXHAUSTED")
	replay, err := h.s.CheckoutWithPhoneKey(ctx, input(variant, provider, "ONLINE"), key, "first", "ip")
	must(t, err)
	if replay.OrderID != first.OrderID || replay.Payment == nil || replay.Payment.ID != first.Payment.ID {
		t.Fatal("checkout replay changed order/payment")
	}
	_, err = h.s.CheckoutWithPhoneKey(ctx, input(variant, provider, "COD_PROVIDER"), key, "third", "ip")
	wantCode(t, err, "PHONE_KEY_EXHAUSTED")
	pt := rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{Eq: map[string]any{"token_hash": domain.Hash(key)}})[0]
	must(t, h.db.Update(ctx, application.PhoneTokens, pt.ID, map[string]any{"expires_at": time.Now().Add(-time.Second)}))
	_, err = h.s.CheckoutWithPhoneKey(ctx, input(variant, provider, "ONLINE"), key, "first", "ip")
	wantCode(t, err, "PHONE_KEY_EXPIRED")
	device := rows[domain.TrustedDevice](t, h, application.TrustedDevices, application.Query{})[0]
	must(t, h.db.Update(ctx, application.TrustedDevices, device.ID, map[string]any{"expires_at": time.Now().Add(-time.Second)}))
	_, err = h.s.ClientBill(ctx, first.BillNumber, "", detail.TrustDeviceKey, "ShopNext-test-device")
	wantCode(t, err, "INVALID_TRUST_DEVICE_KEY")
}

func TestPhoneKeyConcurrentLimitAndRollback(t *testing.T) {
	h := setup(t)
	h.s.Limiter = unrestricted{}
	_, v, p := fixture(t, h, 20)
	key := verificationKey(t, h, phone)
	invalid := input(v, p, "COD_PROVIDER")
	invalid.Items[0].Quantity = 99
	_, err := h.s.CheckoutWithPhoneKey(ctx, invalid, key, "invalid", "ip")
	wantCode(t, err, "STOCK_CONFLICT")
	if rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses != 0 {
		t.Fatal("failed order consumed a use")
	}
	var wg sync.WaitGroup
	var success, exhausted atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.s.CheckoutWithPhoneKey(ctx, input(v, p, "COD_PROVIDER"), key, domain.Token(8), "ip")
			if err == nil {
				success.Add(1)
			} else {
				var e *domain.Error
				if errors.As(err, &e) && e.Code == "PHONE_KEY_EXHAUSTED" {
					exhausted.Add(1)
				} else {
					t.Errorf("checkout: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	if success.Load() != 5 || exhausted.Load() != 7 {
		t.Fatalf("concurrent usage: successful %d exhausted %d", success.Load(), exhausted.Load())
	}
	if rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses != 5 {
		t.Fatal("usage lost update")
	}
}

func TestManualPickupAssignment(t *testing.T) {
	h := setup(t)
	h.s.Limiter = unrestricted{}
	_, v, p := fixture(t, h, 10)
	first := checkout(t, h, input(v, p, "ONLINE"), "first")
	second := checkout(t, h, input(v, p, "COD_PROVIDER"), "second")
	err := h.s.SetPickupCode(ctx, owner, first.BillNumber, "MANUAL-123", "ip")
	wantCode(t, err, "INVALID_STATE")
	err = h.s.SetPickupCode(ctx, domain.Actor{ID: "catalog", Role: "CATALOG"}, second.BillNumber, "MANUAL-123", "ip")
	wantCode(t, err, "FORBIDDEN")
	must(t, h.s.SetPickupCode(ctx, owner, second.BillNumber, "MANUAL-123", "ip"))
	must(t, h.s.SetPickupCode(ctx, owner, second.BillNumber, "MANUAL-123", "ip"))
	summary, err := h.s.BillSummary(ctx, second.BillNumber)
	must(t, err)
	if summary.PickupCode != "MANUAL-123" || !summary.PickupCodeReady {
		t.Fatal("public pickup readiness")
	}
	must(t, h.s.SimulatePayment(ctx, first.BillNumber, phone))
	err = h.s.SetPickupCode(ctx, owner, first.BillNumber, "MANUAL-123", "ip")
	wantCode(t, err, "ALREADY_EXISTS")
	assigned := rows[domain.OrderEvent](t, h, application.OrderEvents, application.Query{Eq: map[string]any{"action": "PICKUP_CODE_ASSIGNED"}})
	if len(assigned) != 1 {
		t.Fatal("assignment was not atomic/idempotent")
	}
	sent := rows[domain.Outbox](t, h, application.OutboxEvents, application.Query{Eq: map[string]any{"dedupe_key": "sms:paid:" + first.BillNumber}})
	if len(sent) != 1 || strings.Contains(sent[0].Message, "ລະຫັດຮັບ") {
		t.Fatal("payment SMS claimed nonexistent pickup code")
	}
}

type recordingWebhook struct {
	ids  []string
	fail bool
}

func (w *recordingWebhook) Send(_ context.Context, id string, body []byte) error {
	var event application.ClientPaymentEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	if event.EventID != id || event.Type != "order.payment_succeeded" || event.PaymentStatus != "PAID" {
		return errors.New("invalid event")
	}
	w.ids = append(w.ids, id)
	if w.fail {
		return errors.New("temporary failure")
	}
	return nil
}

func TestPaymentClientWebhookTransactionRetryAndDedupe(t *testing.T) {
	h := setup(t)
	webhook := &recordingWebhook{fail: true}
	h.s.ClientWebhook = webhook
	_, v, p := fixture(t, h, 10)
	r := checkout(t, h, input(v, p, "ONLINE"), "first")
	attempt := *r.Payment
	raw := callback(attempt, r.BillNumber, r.TotalKip, "callback")
	// A notification enqueue failure rolls back approval, stock and SMS.
	store := h.s.Store
	h.s.Store = failStore{h.db, application.ClientNotifications}
	_, err := h.s.Webhook(ctx, raw)
	if err == nil {
		t.Fatal("notification failure did not fail payment transaction")
	}
	h.s.Store = store
	order := rows[domain.Order](t, h, application.Orders, application.Query{})[0]
	if order.PaymentStatus != "PENDING" {
		t.Fatal("payment committed without notification")
	}
	outcome, err := h.s.Webhook(ctx, raw)
	must(t, err)
	if outcome != "APPLIED" {
		t.Fatal(outcome)
	}
	_, err = h.s.Webhook(ctx, raw)
	must(t, err)
	pending := rows[domain.ClientNotification](t, h, application.ClientNotifications, application.Query{})
	if len(pending) != 1 {
		t.Fatal("duplicate client events")
	}
	worker := application.Worker{Service: h.s}
	n, err := worker.DrainClientNotifications(ctx)
	must(t, err)
	if n != 0 {
		t.Fatal("failed notification marked sent")
	}
	record := rows[domain.ClientNotification](t, h, application.ClientNotifications, application.Query{})[0]
	if record.Status != "FAILED" || record.Attempts != 1 {
		t.Fatal("retry state")
	}
	webhook.fail = false
	must(t, h.db.Update(ctx, application.ClientNotifications, record.ID, map[string]any{"available_at": time.Now().Add(-time.Second)}))
	n, err = worker.DrainClientNotifications(ctx)
	must(t, err)
	if n != 1 || len(webhook.ids) != 2 || webhook.ids[0] != webhook.ids[1] {
		t.Fatal("stable retry event ID")
	}
	// Expired leases are reclaimed and the retry limit dead-letters records.
	must(t, h.db.Update(ctx, application.ClientNotifications, record.ID, map[string]any{"status": "SENDING", "attempts": 8, "lease_until": time.Now().Add(-time.Minute)}))
	_, err = worker.DrainClientNotifications(ctx)
	must(t, err)
	if rows[domain.ClientNotification](t, h, application.ClientNotifications, application.Query{})[0].Status != "DEAD" {
		t.Fatal("client webhook lease recovery")
	}
}

type unavailableQR struct{ application.PaymentProvider }

func (p unavailableQR) CreateQR(context.Context, string, string, int64) (application.QR, error) {
	return application.QR{}, errors.New("provider unavailable")
}

func TestOnlineQRFailureReturnsCreatedOrder(t *testing.T) {
	h := setup(t)
	h.s.Payments = unavailableQR{providers.NewPayment("", "", true)}
	_, v, p := fixture(t, h, 5)
	key := verificationKey(t, h, phone)
	credential := &http.Cookie{Name: api.PhoneKeyHeader, Value: key}
	w := request(t, h, "POST", "/api/v1/orders", input(v, p, "ONLINE"), credential)
	expect(t, w, 201)
	result := data[application.CheckoutResult](t, w)
	if result.OrderID == "" || result.Payment != nil || result.PaymentError != "PAYMENT_QR_UNAVAILABLE" {
		t.Fatal("QR failure lost committed order")
	}
	// A failed simulation must not spend another phone-key use.
	before := rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses
	err := h.s.ClientSimulatePayment(ctx, result.BillNumber, key, "", "ShopNext-test-device")
	if err == nil {
		t.Fatal("simulation without a QR unexpectedly succeeded")
	}
	if rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses != before {
		t.Fatal("failed simulation consumed a key use")
	}
	h.s.Payments = providers.NewPayment("", "", true)
	w = request(t, h, "POST", "/api/v1/bills/"+result.BillNumber+"/payment-attempts", map[string]string{"bank": "BCEL"}, credential)
	expect(t, w, 200)
	if data[domain.Attempt](t, w).QRCode == "" {
		t.Fatal("QR retry failed")
	}
	must(t, h.s.ClientSimulatePayment(ctx, result.BillNumber, key, "", "ShopNext-test-device"))
	if rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses != before+2 {
		t.Fatal("retry and successful simulation must each consume one key use")
	}
	// Public status must never leak identity or QR data.
	w = request(t, h, "GET", "/api/v1/bills/"+result.BillNumber, nil)
	for _, private := range []string{"recipient_phone", "recipient_name", "qr_code", "deeplink", "items", "branch"} {
		if bytes.Contains(w.Body.Bytes(), []byte(private)) {
			t.Fatal("public summary exposed " + private)
		}
	}
}

package integration

import (
	"net/http"
	"net/url"
	api "shopnext-laos/internal/adapters/http"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"testing"
	"time"
)

// These probes use only the guarded shopnext_test harness and synthetic data.
func TestSecurityOTPChallengePreventsUnauthenticatedExhaustion(t *testing.T) {
	h := setup(t)
	w := request(t, h, "POST", "/api/v1/otp/request", map[string]string{"phone": phone})
	if w.Code != 200 {
		t.Fatalf("synthetic OTP request returned HTTP %d", w.Code)
	}
	code, ok := data[map[string]any](t, w)["dev_code"].(string)
	if !ok || len(code) != 6 {
		t.Fatal("test adapter did not provide the synthetic OTP")
	}
	challenge, ok := data[map[string]any](t, w)["challenge"].(string)
	if !ok || len(challenge) != 64 {
		t.Fatal("OTP issuance did not provide a challenge")
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	// Both omitted and independently guessed secrets must leave the victim's
	// attempt budget intact, even when the attacker knows the correct SMS code.
	for _, secret := range []string{"", domain.Token(32)} {
		for i := 0; i < 5; i++ {
			w = request(t, h, "POST", "/api/v1/otp/verify", map[string]string{"phone": phone, "code": wrong, "challenge": secret})
			expect(t, w, 400)
		}
	}
	w = request(t, h, "POST", "/api/v1/otp/verify", map[string]string{"phone": phone, "code": code, "challenge": domain.Token(32)})
	expect(t, w, 400)
	stored := rows[domain.OTP](t, h, application.OTPs, application.Query{})[0]
	if stored.Attempts != 0 || stored.ConsumedAt != nil || stored.ChallengeHash != domain.Hash(challenge) {
		t.Fatal("unbound probes changed the victim's active OTP")
	}
	n, err := h.db.Count(ctx, application.PhoneTokens, application.Query{})
	must(t, err)
	if n != 0 {
		t.Fatal("unbound probe issued a phone key")
	}
	w = request(t, h, "POST", "/api/v1/otp/verify", map[string]string{"phone": phone, "code": code, "challenge": challenge})
	expect(t, w, 200)
	if data[map[string]any](t, w)["verification_key"] == "" {
		t.Fatal("legitimate verification did not issue a key")
	}
}

func TestSecurityOTPNewestIssuanceAndConcurrentConsumption(t *testing.T) {
	h := setup(t)
	old, err := h.s.RequestOTP(ctx, phone, "192.0.2.1")
	must(t, err)
	stored := rows[domain.OTP](t, h, application.OTPs, application.Query{})[0]
	// Simulate the resend cooldown passing without expiring the original code.
	must(t, h.db.Update(ctx, application.OTPs, stored.ID, map[string]any{"created_at": time.Now().Add(-2 * time.Minute)}))
	newest, err := h.s.RequestOTP(ctx, phone, "192.0.2.1")
	must(t, err)
	if old.Challenge == newest.Challenge {
		t.Fatal("resend reused a challenge")
	}
	_, err = h.s.VerifyOTP(ctx, phone, old.DevCode, old.Challenge, "192.0.2.1", "device")
	wantCode(t, err, "INVALID_OTP_CHALLENGE")
	for _, record := range rows[domain.OTP](t, h, application.OTPs, application.Query{}) {
		if record.Attempts != 0 || record.ConsumedAt != nil {
			t.Fatal("old challenge changed either issuance")
		}
	}
	type result struct {
		key string
		err error
	}
	results := make(chan result, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			key, err := h.s.VerifyOTP(ctx, phone, newest.DevCode, newest.Challenge, "192.0.2.1", "device")
			results <- result{key, err}
		}()
	}
	successes := 0
	for i := 0; i < cap(results); i++ {
		r := <-results
		if r.err == nil {
			if r.key == "" {
				t.Fatal("successful verification did not issue a key")
			}
			successes++
		} else {
			wantCode(t, r.err, "OTP_NOT_FOUND")
		}
	}
	if successes != 1 {
		t.Fatal("concurrent verification consumed the OTP more than once")
	}
	n, err := h.db.Count(ctx, application.PhoneTokens, application.Query{})
	must(t, err)
	if n != 1 {
		t.Fatal("concurrent verification persisted more than one phone key")
	}
	_, err = h.s.VerifyOTP(ctx, phone, old.DevCode, old.Challenge, "192.0.2.1", "device")
	wantCode(t, err, "INVALID_OTP_CHALLENGE")
}

func TestSecurityCheckoutRejectsClientMoneyAndInvalidQuantities(t *testing.T) {
	h := setup(t)
	_, variant, provider := fixture(t, h, 10)
	key := verificationKey(t, h, phone)
	credential := &http.Cookie{Name: api.PhoneKeyHeader, Value: key}
	geo := application.Geography()[0]
	payload := map[string]any{
		"recipient_name": "Synthetic Customer", "recipient_phone": phone,
		"provider_id": provider.ID, "province": geo.Name, "city": geo.Districts[0],
		"branch_name": "Synthetic branch", "payment_type": "ONLINE", "payment_method": "BCEL",
		"items":     []map[string]any{{"variant_id": variant.ID, "quantity": 1}},
		"total_kip": 1,
	}
	expect(t, request(t, h, "POST", "/api/v1/orders", payload, credential), 400)
	delete(payload, "total_kip")
	for _, quantity := range []int64{-1, 0, 100} {
		payload["items"] = []map[string]any{{"variant_id": variant.ID, "quantity": quantity}}
		expect(t, request(t, h, "POST", "/api/v1/orders", payload, credential), 400)
	}
	for _, entity := range []application.Entity{application.Orders, application.Branches, application.Items, application.Attempts, application.OutboxEvents} {
		n, err := h.db.Count(ctx, entity, application.Query{})
		must(t, err)
		if n != 0 {
			t.Fatalf("invalid checkout changed %s", entity)
		}
	}
	stored := rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if stored.StockOnHand != 10 || stored.StockReserved != 0 {
		t.Fatal("invalid checkout changed stock")
	}
	if rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{})[0].Uses != 0 {
		t.Fatal("invalid checkout consumed phone authorization")
	}
	payload["items"] = []map[string]any{{"variant_id": variant.ID, "quantity": 1}}
	w := request(t, h, "POST", "/api/v1/orders", payload, credential)
	expect(t, w, 201)
	result := data[application.CheckoutResult](t, w)
	if result.TotalKip != 70000 || result.Payment == nil || result.Payment.AmountKip != 70000 {
		t.Fatal("checkout did not use server prices")
	}
}

func TestSecurityPaymentRetryRejectsAnotherPhoneWithoutSupersedingQR(t *testing.T) {
	h := setup(t)
	_, variant, provider := fixture(t, h, 10)
	result := checkout(t, h, input(variant, provider, "ONLINE"), "security-owner")
	otherKey := verificationKey(t, h, "02066666666")
	otherCredential := &http.Cookie{Name: api.PhoneKeyHeader, Value: otherKey}
	w := request(t, h, "POST", "/api/v1/bills/"+result.BillNumber+"/payment-attempts", map[string]string{"bank": "BCEL"}, otherCredential)
	expect(t, w, 403)
	if data[map[string]any](t, w)["qr_code"] != nil {
		t.Fatal("denied retry disclosed QR")
	}
	attempts := rows[domain.Attempt](t, h, application.Attempts, application.Query{})
	if len(attempts) != 1 || attempts[0].ID != result.Payment.ID || attempts[0].Status != "CREATED" {
		t.Fatal("denied retry superseded the owner's payment attempt")
	}
	storedKey := rows[domain.PhoneToken](t, h, application.PhoneTokens, application.Query{Eq: map[string]any{"token_hash": domain.Hash(otherKey)}})[0]
	if storedKey.Uses != 0 {
		t.Fatal("denied retry consumed another phone's key")
	}
}

func TestSecurityCallbackReferenceAndMissingAmountDoNotSettleOrder(t *testing.T) {
	h := setup(t)
	_, variant, provider := fixture(t, h, 10)
	result := checkout(t, h, input(variant, provider, "ONLINE"), "security-callback")
	attempt := result.Payment
	outcome, err := h.s.Webhook(ctx, callback(*attempt, "BOTHER00", result.TotalKip, "wrong-reference"))
	must(t, err)
	if outcome != "REFERENCE_MISMATCH" {
		t.Fatal("callback with another order reference was accepted")
	}
	raw := []byte(`{"transactionId":"` + *attempt.ProviderTransactionID + `","billNumber":"` + result.BillNumber + `","status":"PAYMENT_COMPLETED","refNo":"missing-amount"}`)
	outcome, err = h.s.Webhook(ctx, raw)
	must(t, err)
	if outcome != "AMOUNT_MISMATCH" {
		t.Fatal("callback without amount was accepted")
	}
	order := rows[domain.Order](t, h, application.Orders, application.Query{})[0]
	stock := rows[domain.Variant](t, h, application.Variants, application.Query{})[0]
	if order.PaymentStatus != "PENDING" || order.OrderStatus != "PENDING_PAYMENT" || stock.StockOnHand != 10 || stock.StockReserved != 1 {
		t.Fatal("invalid callback changed order or inventory")
	}
	n, err := h.db.Count(ctx, application.ClientNotifications, application.Query{})
	must(t, err)
	if n != 0 {
		t.Fatal("invalid callback emitted payment success")
	}
	// A later correct callback must still be able to settle the same attempt.
	outcome, err = h.s.Webhook(ctx, callback(*attempt, result.BillNumber, result.TotalKip, "correct-after-probes"))
	must(t, err)
	if outcome != "APPLIED" {
		t.Fatal("invalid callback poisoned later valid settlement")
	}
}

func TestSecurityCatalogSQLInputsAndCrossProductVariantContainment(t *testing.T) {
	h := setup(t)
	product, variant, _ := fixture(t, h, 10)
	other, err := h.s.SaveProduct(ctx, owner, "", application.ProductInput{
		CategoryID: product.CategoryID, SKU: "HIDDEN", Name: "Hidden Product", NameEn: "Hidden Product",
		Slug: "hidden-product", BasePriceKip: 1000, Status: "DRAFT", InitialStock: 10,
	}, "synthetic")
	must(t, err)
	for _, probe := range []string{"' OR 1=1 --", "%", "_", "'; DROP TABLE products; --"} {
		w := request(t, h, "GET", "/api/v1/products?q="+url.QueryEscape(probe), nil)
		expect(t, w, 200)
		if len(data[[]application.ProductView](t, w)) != 0 {
			t.Fatal("catalog treated SQL or wildcard probe as executable search")
		}
	}
	w := request(t, h, "GET", "/api/v1/products?sort="+url.QueryEscape("created_at; DROP TABLE products"), nil)
	expect(t, w, 400)
	w = request(t, h, "GET", "/api/v1/products?q="+url.QueryEscape("Hidden Product"), nil)
	expect(t, w, 200)
	if len(data[[]application.ProductView](t, w)) != 0 {
		t.Fatal("catalog search disclosed a draft product")
	}
	_, err = h.s.SaveVariant(ctx, owner, other.ID, variant.ID, application.VariantInput{SKU: variant.SKU, StockOnHand: 0}, "synthetic")
	wantCode(t, err, "FORBIDDEN")
	err = h.s.Reorder(ctx, owner, application.Variants, other.ID, application.ReorderInput{IDs: []string{variant.ID}}, "synthetic")
	wantCode(t, err, "INVALID_ORDERING_GROUP")
	stored := rows[domain.Variant](t, h, application.Variants, application.Query{Eq: map[string]any{"id": variant.ID}})[0]
	if stored.ProductID != product.ID || stored.StockOnHand != 10 {
		t.Fatal("cross-product operation changed the targeted variant")
	}
	products := rows[domain.Product](t, h, application.Products, application.Query{})
	if len(products) != 2 {
		t.Fatal("SQL probe changed durable catalog state")
	}
}

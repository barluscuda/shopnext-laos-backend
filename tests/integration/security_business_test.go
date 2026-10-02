package integration

import (
	"net/http"
	"net/url"
	api "shopnext-laos/internal/adapters/http"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"testing"
)

// These probes use only the guarded shopnext_test harness and synthetic data.
func TestSecurityProofUnauthenticatedOTPExhaustion(t *testing.T) {
	h := setup(t)
	w := request(t, h, "POST", "/api/v1/otp/request", map[string]string{"phone": phone})
	if w.Code != 200 {
		t.Fatalf("synthetic OTP request returned HTTP %d", w.Code)
	}
	code, ok := data[map[string]any](t, w)["dev_code"].(string)
	if !ok || len(code) != 6 {
		t.Fatal("test adapter did not provide the synthetic OTP")
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	// No verification key, trust key, staff login or challenge ID is supplied.
	for i := 0; i < 5; i++ {
		w = request(t, h, "POST", "/api/v1/otp/verify", map[string]string{"phone": phone, "code": wrong})
		if w.Code != 400 {
			t.Fatalf("unauthenticated wrong-code probe returned HTTP %d", w.Code)
		}
	}
	w = request(t, h, "POST", "/api/v1/otp/verify", map[string]string{"phone": phone, "code": code})
	if w.Code != 429 {
		t.Fatalf("victim's correct OTP after exhaustion returned HTTP %d", w.Code)
	}
	stored := rows[domain.OTP](t, h, application.OTPs, application.Query{})[0]
	if stored.Attempts != 5 || stored.ConsumedAt != nil {
		t.Fatal("wrong-code probes did not exhaust the victim's active OTP")
	}
	n, err := h.db.Count(ctx, application.PhoneTokens, application.Query{})
	must(t, err)
	if n != 0 {
		t.Fatal("exhausted OTP unexpectedly issued a phone key")
	}
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

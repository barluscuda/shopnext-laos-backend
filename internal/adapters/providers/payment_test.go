package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"shopnext-laos/internal/application"
	"strings"
	"testing"
)

func TestCallbackNormalization(t *testing.T) {
	p := NewPayment("", "", true)
	a, err := p.ParseCallback([]byte(`{"transactionId":"t","billNumber":"BABCDEFG","txnAmount":70000,"status":"PAYMENT_COMPLETED","refNo":123}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.ParseCallback([]byte(`{"refNo":"123","status":"PAYMENT_COMPLETED","txnAmount":"70000","billNumber":"BABCDEFG","transactionId":"t"}`))
	if err != nil || a.DedupeKey != b.DedupeKey || a.Amount == nil || *a.Amount != 70000 || a.Status != "COMPLETED" {
		t.Fatalf("canonical callback %+v %+v %v", a, b, err)
	}
	for _, amount := range []string{"1.5", "-1", "9223372036854775808", "\"NaN\""} {
		c, err := p.ParseCallback([]byte(`{"transactionId":"t","txnAmount":` + amount + `}`))
		if err != nil || c.Amount != nil {
			t.Fatal(amount, c, err)
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"transactionId":"t"} {}`, `bad`} {
		if _, err := p.ParseCallback([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
}
func TestPaymentHTTPContracts(t *testing.T) {
	var called []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = append(called, r.Method+" "+r.URL.Path)
		if r.Header.Get("secretKey") != "merchant" {
			t.Error("merchant auth")
		}
		var in map[string]any
		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case "/v1/api/payment/generate-bcel-qr":
			if in["tag1"] != "BABCDEFG" || in["amount"] != float64(70000) || strings.Contains(in["description"].(string), "ບ") {
				t.Error(in)
			}
			_, _ = w.Write([]byte(`{"transactionId":"txn","qrCode":"qr","link":"bank://pay"}`))
		case "/v1/api/refund":
			if in["transactionId"] != "txn" {
				t.Error(in)
			}
			_, _ = w.Write([]byte(`{"data":{"id":1234,"status":"REQUESTING"}}`))
		case "/v1/api/refund/1234":
			_, _ = w.Write([]byte(`{"data":{"id":"1234","status":"COMPLETED","amount":"70000"}}`))
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := NewPayment(srv.URL, "merchant", false)
	q, err := p.CreateQR(context.Background(), "BABCDEFG", "BCEL", 70000)
	if err != nil || q.TransactionID != "txn" || q.Code != "qr" {
		t.Fatal(q, err)
	}
	refund, err := p.SubmitRefund(context.Background(), "txn")
	if err != nil || refund.ID != "1234" || refund.Success {
		t.Fatal(refund, err)
	}
	refund, err = p.RefundStatus(context.Background(), refund.ID)
	if err != nil || !refund.Success || refund.Amount == nil || *refund.Amount != 70000 {
		t.Fatal(refund, err)
	}
	if len(called) != 3 {
		t.Fatal(called)
	}
}
func TestAmbiguousRefundAndTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"status":"REQUESTING"}}`))
	}))
	p := NewPayment(srv.URL, "key", false)
	_, err := p.SubmitRefund(context.Background(), "t")
	var ambiguous *application.AmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatal(err)
	}
	srv.Close()
	_, err = p.SubmitRefund(context.Background(), "t")
	if !errors.As(err, &ambiguous) {
		t.Fatal(err)
	}
}

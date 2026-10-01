package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http/httptest"
	"os"
	"path/filepath"
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

type refundGateway struct {
	*providers.Payment
	submissions atomic.Int32
	ambiguous   bool
	outage      bool
}

func (p *refundGateway) SubmitRefund(context.Context, string) (application.RefundResult, error) {
	p.submissions.Add(1)
	if p.ambiguous {
		return application.RefundResult{}, &application.AmbiguousError{Cause: errors.New("connection lost after send")}
	}
	return application.RefundResult{ID: "provider-refund", Status: "REQUESTING"}, nil
}
func (p *refundGateway) RefundStatus(context.Context, string) (application.RefundResult, error) {
	if p.outage {
		return application.RefundResult{}, errors.New("provider outage")
	}
	return application.RefundResult{ID: "provider-refund", Status: "REQUESTING"}, nil
}
func TestUnknownRefundNeverResubmitsAndCanResolve(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 2)
	r := checkout(t, h, input(v, p, "ONLINE"), "refund")
	must(t, h.s.SimulatePayment(ctx, r.BillNumber, phone))
	g := &refundGateway{Payment: providers.NewPayment("", "", true), ambiguous: true}
	h.s.Payments = g
	refund, err := h.s.RequestRefund(ctx, owner, r.BillNumber, "return", "ip")
	must(t, err)
	finance := domain.Actor{ID: "finance", Email: "finance@example.test", Role: "FINANCE"}
	must(t, h.s.ApproveRefund(ctx, finance, refund.ID, "ip"))
	must(t, h.s.PollRefunds(ctx))
	must(t, h.s.PollRefunds(ctx))
	if g.submissions.Load() != 1 {
		t.Fatal("ambiguous refund resubmitted")
	}
	current := rows[domain.Refund](t, h, application.Refunds, application.Query{})[0]
	if current.Status != "UNKNOWN" {
		t.Fatal(current.Status)
	}
	wantCode(t, h.s.ResolveRefund(ctx, finance, refund.ID, "portal-verified-id", true, "ip"), "REFUND_SUBMISSION_IN_PROGRESS")
	must(t, h.db.Update(ctx, application.Refunds, refund.ID, map[string]any{"submitted_at": time.Now().Add(-time.Minute)}))
	must(t, h.s.PollRefunds(ctx))
	current = rows[domain.Refund](t, h, application.Refunds, application.Query{})[0]
	if current.Status != "MANUAL_REVIEW" {
		t.Fatal(current.Status)
	}
	must(t, h.s.ResolveRefund(ctx, finance, refund.ID, "portal-verified-id", true, "ip"))
	bill, err := h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.PaymentStatus != "REFUNDED" {
		t.Fatal("manual refund did not complete")
	}
	if g.submissions.Load() != 1 {
		t.Fatal("resubmitted during resolution")
	}
}
func TestRefundOutageStillReachesDeadline(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 2)
	r := checkout(t, h, input(v, p, "ONLINE"), "refund-deadline")
	must(t, h.s.SimulatePayment(ctx, r.BillNumber, phone))
	g := &refundGateway{Payment: providers.NewPayment("", "", true), outage: true}
	h.s.Payments = g
	refund, err := h.s.RequestRefund(ctx, owner, r.BillNumber, "return", "ip")
	must(t, err)
	must(t, h.s.ApproveRefund(ctx, domain.Actor{ID: "finance", Email: "finance@example.test", Role: "FINANCE"}, refund.ID, "ip"))
	must(t, h.db.Update(ctx, application.Refunds, refund.ID, map[string]any{"submitted_at": time.Now().Add(-25 * time.Hour)}))
	must(t, h.s.PollRefunds(ctx))
	current := rows[domain.Refund](t, h, application.Refunds, application.Query{})[0]
	if current.Status != "MANUAL_REVIEW" {
		t.Fatal(current.Status)
	}
	bill, err := h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.PaymentStatus != "PAID" {
		t.Fatal("outage falsely refunded")
	}
}

type failingSMS struct{}

func (failingSMS) Send(context.Context, string, string) (bool, error) {
	return false, errors.New("unavailable")
}

type countingSMS struct{ n atomic.Int32 }

func (s *countingSMS) Send(context.Context, string, string) (bool, error) {
	s.n.Add(1)
	return true, nil
}
func TestOutboxRetryLeaseRecoveryAndDeadLetter(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 2)
	_ = checkout(t, h, input(v, p, "COD_PROVIDER"), "sms")
	h.s.SMS = failingSMS{}
	worker := application.Worker{Service: h.s}
	_, err := worker.DrainOutbox(ctx)
	must(t, err)
	out := rows[domain.Outbox](t, h, application.OutboxEvents, application.Query{})[0]
	if out.Status != "FAILED" || out.Attempts != 1 || out.LeaseUntil != nil || !out.AvailableAt.After(time.Now()) {
		t.Fatal("backoff not persisted", out)
	}
	must(t, h.db.Update(ctx, application.OutboxEvents, out.ID, map[string]any{"status": "SENDING", "attempts": 7, "lease_until": time.Now().Add(-time.Minute)}))
	_, err = worker.DrainOutbox(ctx)
	must(t, err)
	// A reclaimed lease may become due on the following tick because PostgreSQL
	// rounds timestamptz to microseconds while the application clock uses nanos.
	_, err = worker.DrainOutbox(ctx)
	must(t, err)
	out = rows[domain.Outbox](t, h, application.OutboxEvents, application.Query{})[0]
	if out.Status != "DEAD" || out.Attempts != 8 {
		t.Fatal("lease retry/dead letter", out)
	}
	_, err = worker.DrainOutbox(ctx)
	must(t, err)
	out = rows[domain.Outbox](t, h, application.OutboxEvents, application.Query{})[0]
	if out.Attempts != 8 {
		t.Fatal("dead letter retried")
	}
}
func TestConcurrentWorkersDoNotDoubleClaim(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 2)
	_ = checkout(t, h, input(v, p, "COD_PROVIDER"), "workers")
	sms := &countingSMS{}
	h.s.SMS = sms
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := application.Worker{Service: h.s}
			_, err := w.DrainOutbox(ctx)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if sms.n.Load() != 1 {
		t.Fatalf("SMS delivered %d times", sms.n.Load())
	}
}
func TestIdempotencyCleanupAndWorkerTick(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 2)
	r := checkout(t, h, input(v, p, "ONLINE"), "cleanup")
	keys := rows[domain.Idempotency](t, h, application.Idempotencies, application.Query{})
	must(t, h.db.Update(ctx, application.Idempotencies, keys[0].ID, map[string]any{"expires_at": time.Now().Add(-time.Second)}))
	must(t, h.s.CleanupIdempotency(ctx))
	n, err := h.db.Count(ctx, application.Idempotencies, application.Query{})
	must(t, err)
	if n != 0 {
		t.Fatal("idempotency cleanup")
	}
	must(t, h.db.Update(ctx, application.Orders, r.BillNumber, map[string]any{"reservation_expires_at": time.Now().Add(-time.Second)}))
	w := application.Worker{Service: h.s}
	must(t, w.Tick(ctx))
	bill, err := h.s.Bill(ctx, r.BillNumber, phone, false)
	must(t, err)
	if bill.OrderStatus != "EXPIRED" {
		t.Fatal("worker did not expire order")
	}
}

func TestWebhookHMACAndAtomicRollback(t *testing.T) {
	h := setup(t)
	_, v, p := fixture(t, h, 2)
	r := checkout(t, h, input(v, p, "ONLINE"), "hmac")
	a := rows[domain.Attempt](t, h, application.Attempts, application.Query{})[0]
	raw := callback(a, r.BillNumber, r.TotalKip, "signed")
	prod := h.cfg
	prod.Env = "production"
	router, err := api.New(h.s, prod, zap.NewNop())
	must(t, err)
	sign := func(raw []byte) string {
		mac := hmac.New(sha256.New, []byte(prod.WebhookSecret))
		_, _ = mac.Write(raw)
		return hex.EncodeToString(mac.Sum(nil))
	}
	send := func(body []byte, sig string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/webhooks/phajay", bytes.NewReader(body))
		req.Header.Set(prod.WebhookHeader, sig)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	expect(t, send(raw, "invalid"), 401)
	expect(t, send(append(raw, ' '), sign(raw)), 401)
	h.s.Store = failStore{h.db, application.OutboxEvents}
	expect(t, send(raw, sign(raw)), 500)
	n, err := h.db.Count(ctx, application.PaymentEvents, application.Query{})
	must(t, err)
	if n != 0 {
		t.Fatal("callback dedupe escaped failed payment transaction")
	}
	h.s.Store = h.db
	w := send(raw, sign(raw))
	expect(t, w, 200)
	if data[map[string]string](t, w)["outcome"] != "APPLIED" {
		t.Fatal(w.Body.String())
	}
}

func TestOpenAPICoversEveryRoute(t *testing.T) {
	h := setup(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	must(t, err)
	var spec struct {
		OpenAPI    string                                `json:"openapi"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	must(t, json.Unmarshal(raw, &spec))
	if spec.OpenAPI != "3.1.0" {
		t.Fatal(spec.OpenAPI)
	}
	router := h.handler.(*gin.Engine)
	for _, route := range router.Routes() {
		path := strings.TrimPrefix(route.Path, "/api/v1")
		parts := strings.Split(path, "/")
		for i, p := range parts {
			if strings.HasPrefix(p, ":") {
				parts[i] = "{" + p[1:] + "}"
			}
		}
		path = strings.Join(parts, "/")
		if _, ok := spec.Paths[path][strings.ToLower(route.Method)]; !ok {
			t.Errorf("undocumented route %s %s", route.Method, path)
		}
	}
	for _, required := range []string{"Checkout", "ProductInput", "Bill", "Refund", "Staff", "Dashboard"} {
		if _, ok := spec.Components.Schemas[required]; !ok {
			t.Error("missing API schema", required)
		}
	}
}

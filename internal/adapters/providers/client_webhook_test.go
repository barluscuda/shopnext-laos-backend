package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientWebhookSignatureAndNoRedirect(t *testing.T) {
	secret := strings.Repeat("s", 32)
	body := []byte(`{"event_id":"event-1","order_id":"B1234567","type":"order.payment_succeeded"}`)
	var status atomic.Int32
	status.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		timestamp := r.Header.Get("X-ShopNext-Timestamp")
		unix, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || time.Since(time.Unix(unix, 0)) > time.Minute {
			t.Error("invalid timestamp")
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(timestamp + "."))
		_, _ = mac.Write(raw)
		if r.Header.Get("X-ShopNext-Signature") != hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("X-ShopNext-Event-ID") != "event-1" || string(raw) != string(body) || r.Method != "POST" {
			t.Error("invalid signed webhook")
		}
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	client := NewClientWebhook(server.URL, secret)
	if err := client.Send(context.Background(), "event-1", body); err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{302, 400, 503} {
		status.Store(int32(code))
		if err := client.Send(context.Background(), "event-1", body); err == nil {
			t.Fatalf("accepted HTTP %d", code)
		}
	}
}

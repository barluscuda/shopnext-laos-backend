package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type ClientWebhook struct {
	URL    string
	Secret string
	Client *http.Client
}

func NewClientWebhook(url, secret string) *ClientWebhook {
	return &ClientWebhook{URL: url, Secret: secret, Client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (w *ClientWebhook) Send(ctx context.Context, eventID string, body []byte) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(w.Secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	req, err := http.NewRequestWithContext(ctx, "POST", w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("invalid client webhook request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ShopNext-Event-ID", eventID)
	req.Header.Set("X-ShopNext-Timestamp", timestamp)
	req.Header.Set("X-ShopNext-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := w.Client.Do(req)
	if err != nil {
		return fmt.Errorf("client webhook delivery failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("client webhook HTTP %d", resp.StatusCode)
	}
	return nil
}

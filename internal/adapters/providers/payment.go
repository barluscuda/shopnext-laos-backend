package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/domain"
	"strconv"
	"strings"
	"time"
)

type Payment struct {
	BaseURL, Key string
	Client       *http.Client
	Dev          bool
}

func (p *Payment) Name() string {
	if p.Dev {
		return "dev"
	}
	return "phajay"
}
func (p *Payment) request(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("secretKey", p.Key)
	resp, err := p.Client.Do(req)
	if err != nil {
		return &application.AmbiguousError{Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("provider HTTP %d", resp.StatusCode)
		if resp.StatusCode >= 500 {
			return &application.AmbiguousError{Cause: err}
		}
		return err
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return &application.AmbiguousError{Cause: err}
	}
	return nil
}
func (p *Payment) CreateQR(ctx context.Context, bill, bank string, amount int64) (application.QR, error) {
	endpoint, ok := domain.Banks[bank]
	if !ok {
		return application.QR{}, domain.Fail("UNSUPPORTED_BANK", 400)
	}
	if p.Dev {
		return application.QR{TransactionID: "dev-" + bill + "-" + domain.Token(8), Code: "shopnext-dev:" + bill + ":" + strconv.FormatInt(amount, 10)}, nil
	}
	var result struct {
		TransactionID string `json:"transactionId"`
		QRCode        string `json:"qrCode"`
		Link          string `json:"link"`
	}
	err := p.request(ctx, "POST", "/v1/api/payment/generate-"+endpoint+"-qr", map[string]any{"amount": amount, "description": "ShopNext Laos " + bill, "tag1": bill}, &result)
	if err != nil {
		return application.QR{}, err
	}
	if result.TransactionID == "" || result.QRCode == "" {
		return application.QR{}, errors.New("invalid QR provider response")
	}
	return application.QR{TransactionID: result.TransactionID, Code: result.QRCode, Deeplink: result.Link}, nil
}
func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}
func integer(v any) *int64 {
	s := scalar(v)
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return nil
	}
	return &n
}
func (p *Payment) ParseCallback(raw []byte) (application.Callback, error) {
	var in map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&in); err != nil {
		return application.Callback{}, domain.Fail("INVALID_WEBHOOK", 400)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return application.Callback{}, domain.Fail("INVALID_WEBHOOK", 400)
	}
	id := scalar(in["transactionId"])
	if id == "" {
		return application.Callback{}, domain.Fail("MISSING_TRANSACTION_ID", 400)
	}
	rawStatus := scalar(in["status"])
	status := "UNKNOWN"
	if rawStatus == "PAYMENT_COMPLETED" {
		status = "COMPLETED"
	} else if rawStatus != "" {
		status = "REJECTED"
	}
	bill := scalar(in["billNumber"])
	amount := scalar(in["txnAmount"])
	ref := scalar(in["refNo"])
	canonical, _ := json.Marshal(struct {
		T string `json:"t"`
		S string `json:"s"`
		A string `json:"a"`
		B string `json:"b"`
		R string `json:"r"`
	}{id, rawStatus, amount, bill, ref})
	return application.Callback{TransactionID: id, BillNumber: bill, Amount: integer(in["txnAmount"]), Status: status, DedupeKey: domain.Hash(string(canonical))}, nil
}
func refundResult(raw map[string]any) application.RefundResult {
	data, _ := raw["data"].(map[string]any)
	status := scalar(data["status"])
	if status == "" {
		status = "UNKNOWN"
	}
	upper := strings.ToUpper(status)
	return application.RefundResult{ID: scalar(data["id"]), Status: status, Success: upper == "SUCCEEDED" || upper == "COMPLETED" || upper == "SUCCESS", Failed: upper == "REJECTED" || upper == "FAILED" || upper == "CANCELLED", Amount: integer(data["amount"])}
}
func (p *Payment) SubmitRefund(ctx context.Context, transaction string) (application.RefundResult, error) {
	if p.Dev {
		return application.RefundResult{ID: "dev-refund-" + domain.Token(16), Status: "REQUESTING"}, nil
	}
	var raw map[string]any
	err := p.request(ctx, "POST", "/v1/api/refund", map[string]string{"transactionId": transaction}, &raw)
	if err != nil {
		return application.RefundResult{}, err
	}
	result := refundResult(raw)
	if result.ID == "" {
		return result, &application.AmbiguousError{Cause: errors.New("refund response lacks provider ID")}
	}
	return result, nil
}
func (p *Payment) RefundStatus(ctx context.Context, id string) (application.RefundResult, error) {
	if p.Dev {
		return application.RefundResult{ID: id, Status: "COMPLETED", Success: true}, nil
	}
	var raw map[string]any
	err := p.request(ctx, "GET", "/v1/api/refund/"+url.PathEscape(id), nil, &raw)
	return refundResult(raw), err
}
func NewPayment(base, key string, dev bool) *Payment {
	return &Payment{BaseURL: base, Key: key, Dev: dev, Client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

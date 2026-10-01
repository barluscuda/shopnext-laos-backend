package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"shopnext-laos/internal/domain"
	"strings"
	"time"
	"unicode/utf16"
)

type SMS struct {
	BaseURL, Token, Sender string
	UsePackage, Dev        bool
	Client                 *http.Client
}

var recipientRE = regexp.MustCompile(`^020[0-9]{7,8}$`)
var urlRE = regexp.MustCompile(`(?i)(https?://|www\.|[a-z0-9-]+\.(com|net|org|la|io|ly|link|xyz)\b)`)

func ValidateSMS(phone, message string) (string, string, error) {
	message = strings.TrimSpace(message)
	if !recipientRE.MatchString(phone) {
		return "", "", domain.Fail("INVALID_SMS_PHONE", 400)
	}
	if message == "" || len(utf16.Encode([]rune(message))) > 500 || urlRE.MatchString(message) {
		return "", "", domain.Fail("INVALID_SMS_MESSAGE", 400)
	}
	return phone[1:], message, nil
}
func (s *SMS) Send(ctx context.Context, phone, message string) (bool, error) {
	if s.Dev {
		return true, nil
	}
	recipient, text, err := ValidateSMS(phone, message)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(map[string]any{"header": s.Sender, "phoneNumber": recipient, "message": text, "token": s.Token, "usePackage": s.UsePackage})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(s.BaseURL, "/")+"/sms/package", bytes.NewReader(raw))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("SMS provider HTTP %d", resp.StatusCode)
	}
	var body struct {
		ResultCode string `json:"resultCode"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&body); err != nil {
		return false, err
	}
	if body.ResultCode != "" && body.ResultCode != "20000" {
		return false, fmt.Errorf("SMS provider rejection %s", body.ResultCode)
	}
	return true, nil
}
func NewSMS(base, token, sender string, usePackage, dev bool) *SMS {
	return &SMS{BaseURL: base, Token: token, Sender: sender, UsePackage: usePackage, Dev: dev, Client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

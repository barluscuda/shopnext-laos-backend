package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSMSValidation(t *testing.T) {
	phone, msg, err := ValidateSMS("02055555555", " ສະບາຍດີ ")
	if err != nil || phone != "2055555555" || msg != "ສະບາຍດີ" {
		t.Fatal(phone, msg, err)
	}
	for _, msg := range []string{"", strings.Repeat("a", 501), strings.Repeat("😀", 251), "visit https://example.com", "example.la"} {
		if _, _, err := ValidateSMS("02055555555", msg); err == nil {
			t.Fatal("invalid SMS")
		}
	}
	if _, _, err := ValidateSMS("03055555555", "hello"); err == nil {
		t.Fatal("unsupported Wenova phone")
	}
}
func TestSMSHTTPContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/sms/package" {
			t.Error(r.URL.Path)
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in["header"] != "SENDER" || in["phoneNumber"] != "2055555555" || in["token"] != "token" || in["usePackage"] != true {
			t.Error(in)
		}
		_, _ = w.Write([]byte(`{"resultCode":"20000"}`))
	}))
	defer srv.Close()
	s := NewSMS(srv.URL, "token", "SENDER", true, false)
	accepted, err := s.Send(context.Background(), "02055555555", "hello")
	if err != nil || !accepted {
		t.Fatal(accepted, err)
	}
}

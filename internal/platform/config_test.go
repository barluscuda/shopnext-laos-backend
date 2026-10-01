package platform

import (
	"strings"
	"testing"
)

func TestProductionConfiguration(t *testing.T) {
	t.Setenv("CONFIG_FILE", "")
	t.Setenv("APP_ENV", "production")
	t.Setenv("PAYMENT_PROVIDER", "dev")
	t.Setenv("SMS_PROVIDER", "dev")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("production accepted dev providers")
	}
	t.Setenv("PAYMENT_PROVIDER", "phajay")
	t.Setenv("SMS_PROVIDER", "wenova")
	t.Setenv("PHAJAY_SECRET_KEY", "merchant")
	t.Setenv("PHAJAY_WEBHOOK_SECRET", strings.Repeat("w", 32))
	t.Setenv("WENOVA_TOKEN", "sms")
	t.Setenv("CRON_SECRET", strings.Repeat("c", 32))
	t.Setenv("ALLOWED_ORIGINS", "https://shop.example")
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALLOWED_ORIGINS", "http://shop.example")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("production HTTP origin")
	}
}

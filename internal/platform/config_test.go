package platform

import (
	"strings"
	"testing"
)

func TestProductionConfiguration(t *testing.T) {
	t.Setenv("CONFIG_FILE", "")
	t.Setenv("ADMIN_JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("TRUST_DEVICE_SECRET", strings.Repeat("t", 32))
	t.Setenv("CLIENT_PAYMENT_WEBHOOK_URL", "https://shop.example/api/payments")
	t.Setenv("CLIENT_PAYMENT_WEBHOOK_SECRET", strings.Repeat("n", 32))
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
	for _, secret := range []string{"", "short", "development-only-admin-jwt-secret-change-for-production"} {
		t.Setenv("ADMIN_JWT_SECRET", secret)
		if _, err := LoadConfig(); err == nil {
			t.Fatal("production accepted missing, short or development admin JWT secret")
		}
	}
	t.Setenv("ADMIN_JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("ALLOWED_ORIGINS", "http://shop.example")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("production HTTP origin")
	}
}

package platform

import (
	"fmt"
	"github.com/spf13/viper"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	Env, Addr, DatabaseURL, RedisURL, UploadDir, PaymentProvider, SMSProvider, PhajayURL, PhajayKey, WebhookSecret, WebhookHeader, WenovaURL, WenovaToken, WenovaSender, MaintenanceSecret, LegacyHash, LegacySecret string
	AllowedOrigins, TrustedProxies                                                                                                                                                                                   []string
	Hold                                                                                                                                                                                                             time.Duration
	UsePackage                                                                                                                                                                                                       bool
}

func LoadConfig() (Config, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	defaults := map[string]any{"app_env": "development", "http_addr": ":8080", "database_url": "postgres://shopnext:shopnext@localhost:55432/shopnext?sslmode=disable", "redis_url": "redis://localhost:56379/0", "upload_dir": "data/uploads", "payment_provider": "dev", "sms_provider": "dev", "payment_hold_minutes": 15, "phajay_base_url": "https://payment-gateway.phajay.co", "phajay_webhook_signature_header": "x-phajay-signature", "wenova_base_url": "https://apimicroservices.wenova.fun", "wenova_sender_id": "WNV-info", "wenova_use_package": true, "allowed_origins": []string{"http://localhost:3000", "http://localhost:8080"}}
	for k, val := range defaults {
		v.SetDefault(k, val)
	}
	if file := v.GetString("config_file"); file != "" {
		v.SetConfigFile(file)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, err
		}
	}
	c := Config{Env: v.GetString("app_env"), Addr: v.GetString("http_addr"), DatabaseURL: v.GetString("database_url"), RedisURL: v.GetString("redis_url"), UploadDir: v.GetString("upload_dir"), PaymentProvider: v.GetString("payment_provider"), SMSProvider: v.GetString("sms_provider"), PhajayURL: v.GetString("phajay_base_url"), PhajayKey: v.GetString("phajay_secret_key"), WebhookSecret: v.GetString("phajay_webhook_secret"), WebhookHeader: v.GetString("phajay_webhook_signature_header"), WenovaURL: v.GetString("wenova_base_url"), WenovaToken: v.GetString("wenova_token"), WenovaSender: v.GetString("wenova_sender_id"), UsePackage: v.GetBool("wenova_use_package"), MaintenanceSecret: v.GetString("cron_secret"), LegacyHash: v.GetString("admin_password_hash"), LegacySecret: v.GetString("admin_session_secret"), AllowedOrigins: v.GetStringSlice("allowed_origins"), TrustedProxies: v.GetStringSlice("trusted_proxies"), Hold: time.Duration(v.GetInt("payment_hold_minutes")) * time.Minute}
	if c.Env != "development" && c.Env != "test" && c.Env != "production" {
		return c, fmt.Errorf("invalid APP_ENV")
	}
	if c.Hold <= 0 || c.Hold > 24*time.Hour {
		return c, fmt.Errorf("PAYMENT_HOLD_MINUTES must be between 1 and 1440")
	}
	if c.PaymentProvider != "dev" && c.PaymentProvider != "phajay" {
		return c, fmt.Errorf("invalid PAYMENT_PROVIDER")
	}
	if c.SMSProvider != "dev" && c.SMSProvider != "wenova" {
		return c, fmt.Errorf("invalid SMS_PROVIDER")
	}
	if c.PaymentProvider == "phajay" && (c.PhajayKey == "" || c.WebhookSecret == "") {
		return c, fmt.Errorf("PhaJay requires merchant and webhook secrets")
	}
	if c.SMSProvider == "wenova" && c.WenovaToken == "" {
		return c, fmt.Errorf("Wenova requires WENOVA_TOKEN")
	}
	if (c.LegacyHash == "") != (c.LegacySecret == "") || c.LegacySecret != "" && len(c.LegacySecret) < 32 {
		return c, fmt.Errorf("legacy admin requires a hash and 32-character session secret")
	}
	for _, base := range []string{c.PhajayURL, c.WenovaURL} {
		u, err := url.Parse(base)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" || c.Env == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("provider base URLs must be valid HTTP URLs and HTTPS in production")
		}
	}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" {
			return c, fmt.Errorf("invalid allowed origin")
		}
		if c.Env == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("production origins must use HTTPS")
		}
	}
	if c.Env == "production" && (c.PaymentProvider == "dev" || c.SMSProvider == "dev" || len(c.MaintenanceSecret) < 32 || len(c.WebhookSecret) < 32 || len(c.AllowedOrigins) == 0) {
		return c, fmt.Errorf("production requires real providers, explicit origins and a 32-character CRON_SECRET")
	}
	return c, nil
}

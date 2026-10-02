package bootstrap

import (
	"context"
	"go.uber.org/zap"
	"shopnext-laos/internal/adapters/media"
	"shopnext-laos/internal/adapters/postgres"
	"shopnext-laos/internal/adapters/providers"
	"shopnext-laos/internal/adapters/redis"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/platform"
	"time"
)

type Runtime struct {
	Config  platform.Config
	Service *application.Service
	Store   *postgres.Store
	Limiter *redis.Limiter
	Log     *zap.Logger
}

func Open(ctx context.Context) (*Runtime, error) {
	cfg, err := platform.LoadConfig()
	if err != nil {
		return nil, err
	}
	log, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	store, err := postgres.Open(cfg.DatabaseURL)
	if err != nil {
		_ = log.Sync()
		return nil, err
	}
	limiter, err := redis.New(cfg.RedisURL)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := store.Ping(startup); err != nil {
		_ = store.Close()
		_ = limiter.Close()
		return nil, err
	}
	if err := limiter.Ping(startup); err != nil {
		_ = store.Close()
		_ = limiter.Close()
		return nil, err
	}
	payment := providers.NewPayment(cfg.PhajayURL, cfg.PhajayKey, cfg.PaymentProvider == "dev")
	sms := providers.NewSMS(cfg.WenovaURL, cfg.WenovaToken, cfg.WenovaSender, cfg.UsePackage, cfg.SMSProvider == "dev")
	storage := &media.Storage{Dir: cfg.UploadDir}
	var clientWebhook application.ClientWebhook
	if cfg.ClientWebhookURL != "" {
		clientWebhook = providers.NewClientWebhook(cfg.ClientWebhookURL, cfg.ClientWebhookSecret)
	}
	s := application.New(store, limiter, payment, sms, storage, application.Options{AdminJWTSecret: cfg.AdminJWTSecret, ClientWebhook: clientWebhook, TrustDeviceSecret: cfg.TrustDeviceSecret, Production: cfg.Env == "production", Hold: cfg.Hold, LegacyPasswordHash: cfg.LegacyHash, LegacySessionSecret: cfg.LegacySecret})
	return &Runtime{cfg, s, store, limiter, log}, nil
}
func (r *Runtime) Close() { _ = r.Limiter.Close(); _ = r.Store.Close(); _ = r.Log.Sync() }

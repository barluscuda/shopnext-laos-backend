package main

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"shopnext-laos/internal/application"
	"shopnext-laos/internal/bootstrap"
	"syscall"
	"time"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	rt, err := bootstrap.Open(ctx)
	if err != nil {
		return err
	}
	defer rt.Close()
	worker := &application.Worker{Service: rt.Service}
	rt.Log.Info("worker started", zap.Duration("tick_interval", time.Minute), zap.Duration("notification_interval", 5*time.Second), zap.Duration("cleanup_interval", time.Hour))
	ticks := time.NewTicker(time.Minute)
	defer ticks.Stop()
	notifications := time.NewTicker(5 * time.Second)
	defer notifications.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	work := func() {
		tick, cancel := context.WithTimeout(ctx, 50*time.Second)
		defer cancel()
		if err := worker.Tick(tick); err != nil && ctx.Err() == nil {
			rt.Log.Error("maintenance tick failed", zap.Error(err))
		}
	}
	work()
	for {
		select {
		case <-ctx.Done():
			rt.Log.Info("worker stopped")
			return nil
		case <-ticks.C:
			work()
		case <-notifications.C:
			notificationCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
			if _, err := worker.DrainClientNotifications(notificationCtx); err != nil && ctx.Err() == nil {
				rt.Log.Error("client notification delivery failed", zap.Error(err))
			}
			if _, err := worker.DrainOutbox(notificationCtx); err != nil && ctx.Err() == nil {
				rt.Log.Error("SMS notification delivery failed", zap.Error(err))
			}
			cancel()
		case <-cleanup.C:
			cleanupCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
			err := rt.Service.CleanupIdempotency(cleanupCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				rt.Log.Error("idempotency cleanup failed", zap.Error(err))
			}
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker failed:", err)
		os.Exit(1)
	}
}

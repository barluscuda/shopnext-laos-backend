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
	ticks := time.NewTicker(time.Minute)
	defer ticks.Stop()
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
			return nil
		case <-ticks.C:
			work()
		case <-cleanup.C:
			if err := rt.Service.CleanupIdempotency(ctx); err != nil {
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

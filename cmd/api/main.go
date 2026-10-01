package main

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"net/http"
	"os"
	"os/signal"
	api "shopnext-laos/internal/adapters/http"
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
	router, err := api.New(rt.Service, rt.Config, rt.Log)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: rt.Config.Addr, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	rt.Log.Info("API listening", zap.String("addr", rt.Config.Addr))
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return err
		}
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "API startup or shutdown failed:", err)
		os.Exit(1)
	}
}

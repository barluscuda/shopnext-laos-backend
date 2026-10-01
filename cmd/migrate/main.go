package main

import (
	"context"
	"fmt"
	"github.com/pressly/goose/v3"
	"os"
	"shopnext-laos/internal/adapters/postgres"
	"shopnext-laos/internal/platform"
	"shopnext-laos/migrations"
)

func run() error {
	cfg, err := platform.LoadConfig()
	if err != nil {
		return err
	}
	st, err := postgres.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	db, err := st.DB.DB()
	if err != nil {
		return err
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command != "up" && command != "status" && command != "down" {
		return fmt.Errorf("usage: migrate [up|status|down]")
	}
	if command == "down" && os.Getenv("ALLOW_MIGRATION_DOWN") != "1" {
		return fmt.Errorf("down requires explicit ALLOW_MIGRATION_DOWN=1")
	}
	return goose.RunContext(context.Background(), command, db, ".")
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		os.Exit(1)
	}
}

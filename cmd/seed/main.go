// Command seed loads seeds/dev.sql into DATABASE_URL. It exists because psql may not be installed.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
	slog.Info("seed loaded")
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sql, err := os.ReadFile("seeds/dev.sql")
	if err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	// Simple protocol: the file is a fixed script with no parameters.
	_, err = conn.Exec(ctx, string(sql), pgx.QueryExecModeSimpleProtocol)
	return err
}

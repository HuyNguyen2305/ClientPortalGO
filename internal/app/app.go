// Package app wires repositories, services and the router; main and the integration tests share it.
package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"clientportal/internal/handler"
)

// New returns the HTTP handler for the whole API.
func New(pool *pgxpool.Pool, queryTimeout time.Duration, log *slog.Logger) http.Handler {
	_ = queryTimeout // used by the repositories added in later tasks
	return handler.NewRouter(handler.Deps{
		Log:  log,
		Ping: pool.Ping,
	})
}

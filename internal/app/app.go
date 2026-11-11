// Package app wires repositories, services and the router; main and the integration tests share it.
package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"clientportal/internal/handler"
	"clientportal/internal/repository"
	"clientportal/internal/service"
)

// New returns the HTTP handler for the whole API.
func New(pool *pgxpool.Pool, queryTimeout time.Duration, log *slog.Logger) http.Handler {
	return handler.NewRouter(handler.Deps{
		Log:       log,
		Ping:      pool.Ping,
		Customers: service.NewCustomerService(repository.NewCustomers(pool, queryTimeout)),
		Locations: service.NewLocationService(repository.NewLocations(pool, queryTimeout)),
	})
}

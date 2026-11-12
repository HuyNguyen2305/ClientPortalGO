// Package app wires repositories, services and the router; main and the integration tests share it.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"clientportal/internal/config"
	"clientportal/internal/handler"
	"clientportal/internal/repository"
	"clientportal/internal/service"
)

// New returns the HTTP handler for the whole API.
func New(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger) http.Handler {
	queryTimeout := cfg.QueryTimeout
	locations := repository.NewLocations(pool, queryTimeout)
	documents := docRepo{repository.NewBillingDocuments(pool, queryTimeout)}
	return handler.NewRouter(handler.Deps{
		Log:       log,
		Ping:      pool.Ping,
		Customers: service.NewCustomerService(repository.NewCustomers(pool, queryTimeout)),
		Locations: service.NewLocationService(locations),
		Documents: service.NewBillingDocumentService(documents, locations, time.Now),
		Catalog:   service.NewCatalog(repository.NewServices(pool, queryTimeout)),
	})
}

// docRepo adapts the repository's WithTx to the service interface, so the repository
// package never imports the service package.
type docRepo struct{ *repository.BillingDocuments }

func (r docRepo) WithTx(ctx context.Context, fn func(service.BillingDocumentRepository) error) error {
	return r.BillingDocuments.WithTx(ctx, func(tx *repository.BillingDocuments) error {
		return fn(docRepo{tx})
	})
}

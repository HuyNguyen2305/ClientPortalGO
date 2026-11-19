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

// New returns the HTTP handler for the whole API. gw may be nil, which switches the saved-cards routes off.
func New(pool *pgxpool.Pool, cfg config.Config, log *slog.Logger, gw service.PaymentGateway, mailer service.Mailer) http.Handler {
	queryTimeout := cfg.QueryTimeout
	customers := repository.NewCustomers(pool, queryTimeout)
	locations := repository.NewLocations(pool, queryTimeout)
	documents := docRepo{repository.NewBillingDocuments(pool, queryTimeout)}
	services := repository.NewServices(pool, queryTimeout)
	// Dates are business-local, like bookings: a UTC server must not date an evening conversion tomorrow.
	now := func() time.Time { return time.Now().In(cfg.Hours.Loc) }
	invoices := service.NewBillingDocumentService(documents, locations, now)
	var paymentMethods *service.PaymentMethodService
	if gw != nil {
		paymentMethods = service.NewPaymentMethodService(customers, gw, invoices)
	}
	return handler.NewRouter(handler.Deps{
		Log:            log,
		AdminKey:       cfg.AdminAPIKey,
		Ping:           pool.Ping,
		Customers:      service.NewCustomerService(customers),
		Auth:           service.NewAuthService(repository.NewAuth(pool, queryTimeout), mailer, cfg.ResetURLBase, cfg.SessionTTL, time.Now),
		Locations:      service.NewLocationService(locations),
		Documents:      invoices,
		Catalog:        service.NewCatalog(services),
		Bookings:       service.NewBookingService(documents, locations, services, cfg.Hours, cfg.MaxLiveBookings, now),
		PaymentMethods: paymentMethods,
		TrustedProxies: cfg.TrustedProxies,
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

package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
	"clientportal/internal/repository/db"
)

const serviceColumns = `id, name, duration_minutes, price_cents, created_at, updated_at`

// Services stores the bookable services.
type Services struct {
	db      db.DBTX
	timeout time.Duration
}

// NewServices returns a service repository.
func NewServices(db db.DBTX, timeout time.Duration) *Services {
	return &Services{db: db, timeout: timeout}
}

func scanService(row pgx.Row) (model.Service, error) {
	var s model.Service
	err := row.Scan(&s.ID, &s.Name, &s.DurationMinutes, &s.PriceCents, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// Create inserts a service.
func (r *Services) Create(ctx context.Context, s model.Service) (model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanService(r.db.QueryRow(ctx,
		`INSERT INTO services (name, duration_minutes, price_cents) VALUES ($1, $2, $3) RETURNING `+serviceColumns,
		s.Name, s.DurationMinutes, s.PriceCents))
	if err != nil {
		return model.Service{}, fmt.Errorf("insert service: %w", mapErr(err))
	}
	return out, nil
}

// Get returns one service.
func (r *Services) Get(ctx context.Context, id int64) (model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	s, err := scanService(r.db.QueryRow(ctx, `SELECT `+serviceColumns+` FROM services WHERE id = $1`, id))
	if err != nil {
		return model.Service{}, fmt.Errorf("get service %d: %w", id, mapErr(err))
	}
	return s, nil
}

// List returns every service by name.
func (r *Services) List(ctx context.Context) ([]model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx, `SELECT `+serviceColumns+` FROM services ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Service, error) { return scanService(row) })
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	return out, nil
}

// Update replaces a service's name, duration and price.
func (r *Services) Update(ctx context.Context, s model.Service) (model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanService(r.db.QueryRow(ctx,
		`UPDATE services SET name = $2, duration_minutes = $3, price_cents = $4, updated_at = now()
		 WHERE id = $1 RETURNING `+serviceColumns,
		s.ID, s.Name, s.DurationMinutes, s.PriceCents))
	if err != nil {
		return model.Service{}, fmt.Errorf("update service %d: %w", s.ID, mapErr(err))
	}
	return out, nil
}

// Delete removes a service; ErrConflict while bookings use it.
func (r *Services) Delete(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db.Exec(ctx, `DELETE FROM services WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete service %d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete service %d: %w", id, model.ErrNotFound)
	}
	return nil
}

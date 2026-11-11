package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
	"clientportal/internal/repository/db"
)

const locationColumns = `id, customer_id, street, city, state, postal_code, created_at, updated_at`

// Locations stores customer service addresses. Every query is scoped to a customer.
type Locations struct {
	db      db.DBTX
	timeout time.Duration
}

// NewLocations returns a location repository.
func NewLocations(db db.DBTX, timeout time.Duration) *Locations {
	return &Locations{db: db, timeout: timeout}
}

func scanLocation(row pgx.Row) (model.Location, error) {
	var l model.Location
	err := row.Scan(&l.ID, &l.CustomerID, &l.Street, &l.City, &l.State, &l.PostalCode, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// Create inserts a location.
func (r *Locations) Create(ctx context.Context, l model.Location) (model.Location, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanLocation(r.db.QueryRow(ctx,
		`INSERT INTO locations (customer_id, street, city, state, postal_code)
		 VALUES ($1, $2, $3, $4, $5) RETURNING `+locationColumns,
		l.CustomerID, l.Street, l.City, l.State, l.PostalCode))
	if err != nil {
		return model.Location{}, fmt.Errorf("insert location: %w", mapErr(err))
	}
	return out, nil
}

// Get returns one of the customer's locations.
func (r *Locations) Get(ctx context.Context, customerID, id int64) (model.Location, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	l, err := scanLocation(r.db.QueryRow(ctx,
		`SELECT `+locationColumns+` FROM locations WHERE id = $1 AND customer_id = $2`, id, customerID))
	if err != nil {
		return model.Location{}, fmt.Errorf("get location %d: %w", id, mapErr(err))
	}
	return l, nil
}

// Update replaces the address of one of the customer's locations.
func (r *Locations) Update(ctx context.Context, l model.Location) (model.Location, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanLocation(r.db.QueryRow(ctx,
		`UPDATE locations SET street = $3, city = $4, state = $5, postal_code = $6, updated_at = now()
		 WHERE id = $1 AND customer_id = $2 RETURNING `+locationColumns,
		l.ID, l.CustomerID, l.Street, l.City, l.State, l.PostalCode))
	if err != nil {
		return model.Location{}, fmt.Errorf("update location %d: %w", l.ID, mapErr(err))
	}
	return out, nil
}

// Delete removes one of the customer's locations; ErrConflict while documents reference it.
func (r *Locations) Delete(ctx context.Context, customerID, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db.Exec(ctx, `DELETE FROM locations WHERE id = $1 AND customer_id = $2`, id, customerID)
	if err != nil {
		return fmt.Errorf("delete location %d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete location %d: %w", id, model.ErrNotFound)
	}
	return nil
}

package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
)

// List returns all of a customer's locations; it feeds the "Filter by Locations" dropdown.
func (r *Locations) List(ctx context.Context, customerID int64) ([]model.Location, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx,
		`SELECT `+locationColumns+` FROM locations WHERE customer_id = $1 ORDER BY id`, customerID)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Location, error) {
		return scanLocation(row)
	})
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	return out, nil
}

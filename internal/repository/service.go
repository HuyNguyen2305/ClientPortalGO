package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
	"clientportal/internal/repository/db"
)

const serviceColumns = `id, name, duration_minutes, price_cents, color, status, repeat_unit, repeat_every, repeat_by,
	ends_kind, ends_after, ends_on, set_confirmed, created_at, updated_at`

// Services stores the bookable service templates.
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
	var by *string
	var endsAfter *int
	r := &s.Recurrence
	err := row.Scan(&s.ID, &s.Name, &s.DurationMinutes, &s.PriceCents, &s.Color, &s.Status, &r.Unit, &r.Every, &by,
		&r.EndsKind, &endsAfter, &r.EndsOn, &s.SetConfirmed, &s.CreatedAt, &s.UpdatedAt)
	if by != nil {
		r.By = *by
	}
	if endsAfter != nil {
		r.EndsAfter = *endsAfter
	}
	return s, err
}

// writeArgs are the values for $1..$16 of Create and Update: the service columns, then the items
// as parallel arrays for unnest.
func writeArgs(s model.Service) []any {
	r := s.Recurrence
	var by *string
	if r.By != "" {
		by = &r.By
	}
	var endsAfter *int
	if r.EndsKind == model.EndsAfter {
		endsAfter = &r.EndsAfter
	}
	var pos, qty []int32
	var names []string
	var descs []*string
	var price []int64
	for i, it := range s.Items {
		pos = append(pos, int32(i))
		names = append(names, it.Name)
		descs = append(descs, it.Description)
		qty = append(qty, int32(it.Quantity))
		price = append(price, it.UnitPriceCents)
	}
	return []any{s.Name, s.DurationMinutes, s.PriceCents, s.Color, r.Unit, r.Every, by, r.EndsKind, endsAfter, r.EndsOn,
		s.SetConfirmed, pos, names, descs, qty, price}
}

// insertItems is a CTE body that stores the items for the row in CTE `s`. A data-modifying CTE runs
// even though nothing selects from it, so the service and its items are written by one statement.
const insertItems = `
	INSERT INTO service_items (service_id, position, name, description, quantity, unit_price_cents)
	SELECT s.id, t.pos, t.name, t.description, t.qty, t.price
	FROM s, unnest($12::int[], $13::text[], $14::text[], $15::int[], $16::bigint[]) AS t(pos, name, description, qty, price)`

// Create inserts a service and its items.
func (r *Services) Create(ctx context.Context, s model.Service) (model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanService(r.db.QueryRow(ctx, `
		WITH s AS (
			INSERT INTO services (name, duration_minutes, price_cents, color, repeat_unit, repeat_every, repeat_by,
				ends_kind, ends_after, ends_on, set_confirmed)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING `+serviceColumns+`),
		i AS (`+insertItems+`)
		SELECT `+serviceColumns+` FROM s`, writeArgs(s)...))
	if err != nil {
		return model.Service{}, fmt.Errorf("insert service: %w", mapErr(err))
	}
	out.Items = s.Items
	return out, nil
}

// Update replaces a service's fields and items; $17 is the id.
func (r *Services) Update(ctx context.Context, s model.Service) (model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanService(r.db.QueryRow(ctx, `
		WITH s AS (
			UPDATE services SET name = $1, duration_minutes = $2, price_cents = $3, color = $4, repeat_unit = $5,
				repeat_every = $6, repeat_by = $7, ends_kind = $8, ends_after = $9, ends_on = $10,
				set_confirmed = $11, updated_at = now()
			WHERE id = $17 AND status <> 'deleted' RETURNING `+serviceColumns+`),
		d AS (DELETE FROM service_items WHERE service_id = $17 AND EXISTS (SELECT 1 FROM s)),
		i AS (`+insertItems+`)
		SELECT `+serviceColumns+` FROM s`, append(writeArgs(s), s.ID)...))
	if err != nil {
		return model.Service{}, fmt.Errorf("update service %d: %w", s.ID, mapErr(err))
	}
	out.Items = s.Items
	return out, nil
}

// withItems fills Items for every service with one query.
func (r *Services) withItems(ctx context.Context, ss []model.Service) error {
	if len(ss) == 0 {
		return nil
	}
	ids := make([]int64, len(ss))
	at := make(map[int64]int, len(ss))
	for i, s := range ss {
		ids[i], at[s.ID] = s.ID, i
	}
	rows, err := r.db.Query(ctx,
		`SELECT service_id, name, description, quantity, unit_price_cents FROM service_items
		 WHERE service_id = ANY($1) ORDER BY service_id, position`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var it model.Item
		if err := rows.Scan(&id, &it.Name, &it.Description, &it.Quantity, &it.UnitPriceCents); err != nil {
			return err
		}
		ss[at[id]].Items = append(ss[at[id]].Items, it)
	}
	return rows.Err()
}

// Get returns one service of any status that has not been deleted.
func (r *Services) Get(ctx context.Context, id int64) (model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	s, err := scanService(r.db.QueryRow(ctx,
		`SELECT `+serviceColumns+` FROM services WHERE id = $1 AND status <> 'deleted'`, id))
	if err != nil {
		return model.Service{}, fmt.Errorf("get service %d: %w", id, mapErr(err))
	}
	ss := []model.Service{s}
	if err := r.withItems(ctx, ss); err != nil {
		return model.Service{}, fmt.Errorf("get service %d items: %w", id, err)
	}
	return ss[0], nil
}

// List returns the services with the given status by name.
func (r *Services) List(ctx context.Context, status string) ([]model.Service, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx, `SELECT `+serviceColumns+` FROM services WHERE status = $1 ORDER BY name, id`, status)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Service, error) { return scanService(row) })
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	if err := r.withItems(ctx, out); err != nil {
		return nil, fmt.Errorf("list services items: %w", err)
	}
	return out, nil
}

// SetStatus archives, restores or soft-deletes a service. Bookings keep pointing at it.
func (r *Services) SetStatus(ctx context.Context, id int64, status string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db.Exec(ctx,
		`UPDATE services SET status = $2, updated_at = now() WHERE id = $1 AND status <> 'deleted'`, id, status)
	if err != nil {
		return fmt.Errorf("set service %d status: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set service %d status: %w", id, model.ErrNotFound)
	}
	return nil
}

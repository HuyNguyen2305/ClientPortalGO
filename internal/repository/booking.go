package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
)

// Booking SQL lives on BillingDocuments because a booking is written in the same transaction
// as its estimate (created together, cancelled when the estimate is declined).

const bookingSelect = `
SELECT b.id, b.customer_id, b.starts_at, b.ends_at, b.cancelled_at,
       s.id, s.name, s.duration_minutes, s.price_cents, s.created_at, s.updated_at,
       l.id, l.customer_id, l.street, l.city, l.state, l.postal_code, l.created_at, l.updated_at,
       d.id, d.number, d.status, d.total_cents
FROM bookings b
JOIN services s ON s.id = b.service_id
JOIN locations l ON l.id = b.location_id
JOIN billing_documents d ON d.id = b.estimate_id`

func scanBooking(row pgx.Row) (model.Booking, error) {
	var b model.Booking
	var status string
	s, l, d := &b.Service, &b.Location, &b.Estimate
	err := row.Scan(&b.ID, &b.CustomerID, &b.StartsAt, &b.EndsAt, &b.CancelledAt,
		&s.ID, &s.Name, &s.DurationMinutes, &s.PriceCents, &s.CreatedAt, &s.UpdatedAt,
		&l.ID, &l.CustomerID, &l.Street, &l.City, &l.State, &l.PostalCode, &l.CreatedAt, &l.UpdatedAt,
		&d.ID, &d.Number, &status, &d.TotalCents)
	d.ParentType, d.Status = model.ParentEstimate, model.Status(status)
	return b, err
}

// CreateBooking inserts a booking. Overlapping a live booking violates bookings_no_overlap,
// which mapErr turns into ErrConflict.
func (r *BillingDocuments) CreateBooking(ctx context.Context, b model.Booking) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO bookings (customer_id, location_id, service_id, estimate_id, starts_at, ends_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		b.CustomerID, b.Location.ID, b.Service.ID, b.Estimate.ID, b.StartsAt, b.EndsAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert booking: %w", mapErr(err))
	}
	return id, nil
}

// CancelBookingForEstimate cancels the live booking that created the estimate; no booking is fine.
func (r *BillingDocuments) CancelBookingForEstimate(ctx context.Context, estimateID int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx,
		`UPDATE bookings SET cancelled_at = now(), updated_at = now()
		 WHERE estimate_id = $1 AND cancelled_at IS NULL`, estimateID); err != nil {
		return fmt.Errorf("cancel booking for estimate %d: %w", estimateID, err)
	}
	return nil
}

// HasConvertedInvoice reports whether an invoice was created from the estimate; the unique
// index on converted_from_id serves the lookup.
func (r *BillingDocuments) HasConvertedInvoice(ctx context.Context, estimateID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var found bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM billing_documents WHERE converted_from_id = $1)`, estimateID).Scan(&found); err != nil {
		return false, fmt.Errorf("check converted invoice for estimate %d: %w", estimateID, err)
	}
	return found, nil
}

// BusyIntervals returns every live booking overlapping [from, to), whoever booked it.
// The range test matches bookings_no_overlap, so its GiST index serves the query.
func (r *BillingDocuments) BusyIntervals(ctx context.Context, from, to time.Time) ([]model.Interval, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx,
		`SELECT starts_at, ends_at FROM bookings
		 WHERE cancelled_at IS NULL AND tstzrange(starts_at, ends_at) && tstzrange($1, $2)`, from, to)
	if err != nil {
		return nil, fmt.Errorf("busy intervals: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Interval, error) {
		var iv model.Interval
		err := row.Scan(&iv.Start, &iv.End)
		return iv, err
	})
	if err != nil {
		return nil, fmt.Errorf("busy intervals: %w", err)
	}
	return out, nil
}

// GetBooking returns one of the customer's bookings, cancelled or not.
func (r *BillingDocuments) GetBooking(ctx context.Context, customerID, id int64) (model.Booking, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	b, err := scanBooking(r.db.QueryRow(ctx, bookingSelect+` WHERE b.id = $1 AND b.customer_id = $2`, id, customerID))
	if err != nil {
		return model.Booking{}, fmt.Errorf("get booking %d: %w", id, mapErr(err))
	}
	return b, nil
}

// ListUpcoming returns the customer's live bookings that end after now, soonest first.
func (r *BillingDocuments) ListUpcoming(ctx context.Context, customerID int64, now time.Time) ([]model.Booking, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx, bookingSelect+`
		WHERE b.customer_id = $1 AND b.cancelled_at IS NULL AND b.ends_at > $2
		ORDER BY b.starts_at`, customerID, now)
	if err != nil {
		return nil, fmt.Errorf("list bookings: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Booking, error) { return scanBooking(row) })
	if err != nil {
		return nil, fmt.Errorf("list bookings: %w", err)
	}
	return out, nil
}

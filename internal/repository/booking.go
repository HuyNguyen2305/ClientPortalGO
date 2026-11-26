package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
)

// Booking SQL lives on BillingDocuments because a booking is written in the same transaction
// as its estimate (created together, cancelled when the estimate is declined).

const bookingSelect = `
SELECT b.id, b.customer_id, b.starts_at, b.ends_at, b.cancelled_at, b.series_id,
       w.id, w.status, w.started_at, w.completed_at, w.notes,
       s.id, s.name, s.duration_minutes, s.price_cents, s.created_at, s.updated_at,
       l.id, l.customer_id, l.street, l.city, l.state, l.postal_code, l.created_at, l.updated_at,
       d.id, d.number, d.status, d.total_cents
FROM bookings b
JOIN work_orders w ON w.booking_id = b.id
JOIN services s ON s.id = b.service_id
JOIN locations l ON l.id = b.location_id
JOIN billing_documents d ON d.id = b.estimate_id`

func scanBooking(row pgx.Row) (model.Booking, error) {
	var b model.Booking
	var status, work string
	s, l, d := &b.Service, &b.Location, &b.Estimate
	err := row.Scan(&b.ID, &b.CustomerID, &b.StartsAt, &b.EndsAt, &b.CancelledAt, &b.SeriesID,
		&b.WorkOrderID, &work, &b.StartedAt, &b.CompletedAt, &b.Notes,
		&s.ID, &s.Name, &s.DurationMinutes, &s.PriceCents, &s.CreatedAt, &s.UpdatedAt,
		&l.ID, &l.CustomerID, &l.Street, &l.City, &l.State, &l.PostalCode, &l.CreatedAt, &l.UpdatedAt,
		&d.ID, &d.Number, &status, &d.TotalCents)
	d.ParentType, d.Status = model.ParentEstimate, model.Status(status)
	b.Work = model.WorkStatus(work)
	return b, err
}

// AdvanceWork moves a live booking's work order from one status to the next, stamping started_at
// or completed_at. A missing booking is ErrNotFound; one that is cancelled or not in status from
// is ErrConflict.
func (r *BillingDocuments) AdvanceWork(ctx context.Context, id int64, from, to model.WorkStatus, notes *string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db.Exec(ctx,
		`UPDATE work_orders SET status = $3, updated_at = now(),
		        started_at = CASE WHEN $3 = 'in_progress' THEN now() ELSE started_at END,
		        completed_at = CASE WHEN $3 = 'completed' THEN now() ELSE completed_at END,
		        notes = COALESCE($4, notes)
		 WHERE booking_id = $1 AND status = $2
		   AND EXISTS (SELECT 1 FROM bookings WHERE id = $1 AND cancelled_at IS NULL)`, id, from, to, notes)
	if err != nil {
		return fmt.Errorf("advance work on booking %d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		var one int
		if err := r.db.QueryRow(ctx, `SELECT 1 FROM bookings WHERE id = $1`, id).Scan(&one); err != nil {
			return fmt.Errorf("advance work on booking %d: %w", id, mapErr(err))
		}
		return fmt.Errorf("%w: booking %d is not %s or is cancelled", model.ErrConflict, id, from)
	}
	return nil
}

// CreateBooking inserts a booking and its work order in one statement. Overlapping a live
// booking violates bookings_no_overlap, which mapErr turns into ErrConflict.
func (r *BillingDocuments) CreateBooking(ctx context.Context, b model.Booking) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id int64
	err := r.db.QueryRow(ctx,
		`WITH bk AS (
		     INSERT INTO bookings (customer_id, location_id, service_id, estimate_id, starts_at, ends_at, series_id)
		     VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, customer_id, service_id)
		 INSERT INTO work_orders (booking_id, customer_id, service_id)
		 SELECT id, customer_id, service_id FROM bk RETURNING booking_id`,
		b.CustomerID, b.Location.ID, b.Service.ID, b.Estimate.ID, b.StartsAt, b.EndsAt, b.SeriesID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert booking: %w", mapErr(err))
	}
	return id, nil
}

// CreateSeries inserts the series the jobs of a recurring booking point at; no job is placed
// yet, and generatedThrough is the date before the first one.
func (r *BillingDocuments) CreateSeries(ctx context.Context, customerID, locationID, serviceID int64, firstStart, generatedThrough time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO booking_series (customer_id, location_id, service_id, first_starts_at, generated_through)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`, customerID, locationID, serviceID, firstStart, generatedThrough).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert booking series: %w", mapErr(err))
	}
	return id, nil
}

// HasLiveSeries reports whether the customer has a series of the service that has not ended.
func (r *BillingDocuments) HasLiveSeries(ctx context.Context, customerID, serviceID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var found bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM booking_series
		                WHERE customer_id = $1 AND service_id = $2 AND ended_at IS NULL)`, customerID, serviceID).Scan(&found); err != nil {
		return false, fmt.Errorf("check live series: %w", err)
	}
	return found, nil
}

// EndSeries ends the customer's running series so it places no more jobs; a missing, foreign or
// already ended series is ErrNotFound.
func (r *BillingDocuments) EndSeries(ctx context.Context, customerID, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db.Exec(ctx,
		`UPDATE booking_series SET ended_at = now() WHERE id = $1 AND customer_id = $2 AND ended_at IS NULL`, id, customerID)
	if err != nil {
		return fmt.Errorf("end series %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("series %d: %w", id, model.ErrNotFound)
	}
	return nil
}

// ListDueSeries returns the ids of running series with dates after their cursor up to `through`.
func (r *BillingDocuments) ListDueSeries(ctx context.Context, through time.Time) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx,
		`SELECT id FROM booking_series WHERE ended_at IS NULL AND generated_through < $1 ORDER BY id`, through)
	if err != nil {
		return nil, fmt.Errorf("list due series: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("list due series: %w", err)
	}
	return ids, nil
}

// LockSeries returns a running series and locks it until the transaction ends. One another
// instance holds, or one that has ended, is ErrNotFound.
func (r *BillingDocuments) LockSeries(ctx context.Context, id int64) (model.SeriesRun, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var s model.SeriesRun
	err := r.db.QueryRow(ctx,
		`SELECT id, service_id, customer_id, location_id, first_starts_at, generated_through
		 FROM booking_series WHERE id = $1 AND ended_at IS NULL FOR UPDATE SKIP LOCKED`, id).
		Scan(&s.ID, &s.ServiceID, &s.CustomerID, &s.LocationID, &s.FirstStart, &s.GeneratedThrough)
	if err != nil {
		return model.SeriesRun{}, fmt.Errorf("lock series %d: %w", id, mapErr(err))
	}
	return s, nil
}

// AdvanceSeries moves the series cursor to `through`, and ends the series when done.
func (r *BillingDocuments) AdvanceSeries(ctx context.Context, id int64, through time.Time, done bool) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx,
		`UPDATE booking_series SET generated_through = $2,
		        ended_at = CASE WHEN $3 THEN now() ELSE ended_at END WHERE id = $1`, id, through, done); err != nil {
		return fmt.Errorf("advance series %d: %w", id, err)
	}
	return nil
}

// SeriesJobEstimate returns the customer and estimate of a booking that is a job of a series;
// ok is false for any other booking.
func (r *BillingDocuments) SeriesJobEstimate(ctx context.Context, bookingID int64) (customerID, estimateID int64, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	err = r.db.QueryRow(ctx,
		`SELECT customer_id, estimate_id FROM bookings WHERE id = $1 AND series_id IS NOT NULL`, bookingID).Scan(&customerID, &estimateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("series job of booking %d: %w", bookingID, err)
	}
	return customerID, estimateID, true, nil
}

// CancelBookingForEstimate cancels the live booking that created the estimate; no booking is fine.
// A booking whose work has started or finished is left alone, so history is never rewritten as cancelled.
func (r *BillingDocuments) CancelBookingForEstimate(ctx context.Context, estimateID int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx,
		`UPDATE bookings SET cancelled_at = now(), updated_at = now()
		 WHERE estimate_id = $1 AND cancelled_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM work_orders w WHERE w.booking_id = bookings.id AND w.status <> 'scheduled')`,
		estimateID); err != nil {
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

// ListWorkOrders returns the customer's work orders newest first, cancelled bookings included, as
// bookings carrying their work order. A "cancelled" status means the booking is cancelled; the
// others exclude cancelled bookings.
func (r *BillingDocuments) ListWorkOrders(ctx context.Context, f model.WorkFilter) ([]model.Booking, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx, bookingSelect+`
		WHERE w.customer_id = $1
		  AND ($2::bigint = 0 OR w.id < $2)
		  AND ($3::bigint = 0 OR w.service_id = $3)
		  AND ($4 = '' OR ($4 = 'cancelled' AND b.cancelled_at IS NOT NULL)
		       OR ($4 <> 'cancelled' AND b.cancelled_at IS NULL AND w.status = $4))
		ORDER BY w.id DESC LIMIT $5`, f.CustomerID, f.AfterID, f.ServiceID, string(f.Status), f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list work orders: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Booking, error) { return scanBooking(row) })
	if err != nil {
		return nil, fmt.Errorf("list work orders: %w", err)
	}
	return out, nil
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

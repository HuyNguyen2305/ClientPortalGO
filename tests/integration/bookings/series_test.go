//go:build integration

package bookings_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

// tomorrow is the first bookable date, the one a series booked now has its first job on; with the
// 24-hour lead it falls due as soon as GenerateDue runs.
func tomorrow() string {
	first, _ := testapp.Hours.BookableRange(time.Now())
	return first.Format(time.DateOnly)
}

func (w world) template(t *testing.T, body map[string]any) dto.ServiceResponse {
	t.Helper()
	rec := w.app.Do(t, http.MethodPost, "/api/v1/services", 0, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return testapp.Decode[dto.ServiceResponse](t, rec)
}

func (w world) bookSeries(t *testing.T, customer, location, service int64, date, hhmm string) (int, dto.SeriesResponse) {
	t.Helper()
	rec := w.app.Do(t, http.MethodPost, "/api/v1/booking-series", customer, map[string]any{
		"service_id": service, "location_id": location, "date": date, "time": hhmm,
	})
	return rec.Code, decodeIf[dto.SeriesResponse](rec.Code, rec.Body.Bytes())
}

func (w world) generate(t *testing.T) {
	t.Helper()
	require.NoError(t, w.app.Jobs.GenerateDue(t.Context()))
}

// jobs returns the customer's work orders, newest first.
func (w world) jobs(t *testing.T) []dto.BookingResponse {
	t.Helper()
	rec := w.app.Do(t, http.MethodGet, "/api/v1/work-orders", w.customer, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return testapp.Decode[dto.Page[dto.BookingResponse]](t, rec).Data
}

func TestRecurringJob(t *testing.T) {
	w := newWorld(t)
	weekly := w.template(t, map[string]any{
		"name": "Weekly Service", "duration_minutes": 30, "price_cents": 1,
		"repeat": map[string]any{"unit": "weekly", "every": 1},
		"items": []map[string]any{
			{"name": "Visit", "quantity": 1, "unit_price_cents": 3000},
			{"name": "Supplies", "quantity": 1, "unit_price_cents": 1500},
		},
	})

	code, series := w.bookSeries(t, w.customer, w.location, weekly.ID, tomorrow(), "09:00")
	require.Equal(t, http.StatusCreated, code)
	require.NotZero(t, series.ID)

	t.Run("booking places no work order yet", func(t *testing.T) {
		assert.Empty(t, w.jobs(t))
	})

	t.Run("a second running series of the same template is refused", func(t *testing.T) {
		code, _ := w.bookSeries(t, w.customer, w.location, weekly.ID, tomorrow(), "11:00")
		assert.Equal(t, http.StatusConflict, code)
	})

	t.Run("the job falls due: a work order and an estimate with the template's items", func(t *testing.T) {
		w.generate(t)
		w.generate(t) // running again places nothing more
		jobs := w.jobs(t)
		require.Len(t, jobs, 1)
		j := jobs[0]
		assert.Equal(t, tomorrow(), j.Date)
		assert.Equal(t, "09:00", j.Time)
		assert.Equal(t, "scheduled", j.WorkOrder.Status)
		assert.Equal(t, "open", j.Estimate.Status)
		assert.Equal(t, int64(4500), j.Estimate.TotalCents)
		var lines int
		require.NoError(t, w.app.Pool.QueryRow(t.Context(),
			`SELECT count(*) FROM billing_document_items WHERE billing_document_id = $1`, j.Estimate.ID).Scan(&lines))
		assert.Equal(t, 2, lines)
	})

	t.Run("completing the work accepts the estimate and creates the invoice", func(t *testing.T) {
		j := w.jobs(t)[0]
		require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/start", j.ID), 0, nil).Code)
		require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/complete", j.ID), 0, map[string]any{}).Code)

		var estimate, invoice string
		var total, items int64
		require.NoError(t, w.app.Pool.QueryRow(t.Context(),
			`SELECT e.status, i.status, i.total_cents, (SELECT count(*) FROM billing_document_items WHERE billing_document_id = i.id)
			 FROM billing_documents e JOIN billing_documents i ON i.converted_from_id = e.id
			 WHERE e.id = $1`, j.Estimate.ID).Scan(&estimate, &invoice, &total, &items))
		assert.Equal(t, "accepted", estimate)
		assert.Equal(t, "draft", invoice)
		assert.Equal(t, int64(4500), total)
		assert.Equal(t, int64(2), items)
	})

	t.Run("ending the series", func(t *testing.T) {
		other, _ := newCustomer(t, w.app, "B")
		path := fmt.Sprintf("/api/v1/booking-series/%d", series.ID)
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodDelete, path, other, nil).Code, "another customer's series")
		assert.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodDelete, path, w.customer, nil).Code)
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodDelete, path, w.customer, nil).Code, "already ended")
		code, _ := w.bookSeries(t, w.customer, w.location, weekly.ID, tomorrow(), "11:00")
		assert.Equal(t, http.StatusCreated, code, "an ended series can be booked again")
	})

	t.Run("a series cannot be booked through the single booking endpoint", func(t *testing.T) {
		rec := w.app.Do(t, http.MethodPost, "/api/v1/bookings", w.customer, map[string]any{
			"service_id": weekly.ID, "location_id": w.location, "date": w.day, "time": "11:00",
		})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("a service that does not repeat is refused", func(t *testing.T) {
		code, _ := w.bookSeries(t, w.customer, w.location, w.service.ID, tomorrow(), "09:00")
		assert.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("an archived template cannot be booked", func(t *testing.T) {
		require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/services/%d/archive", weekly.ID), 0, nil).Code)
		code, _ := w.bookSeries(t, w.customer, w.location, weekly.ID, tomorrow(), "13:00")
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestRecurringJobSlotTaken(t *testing.T) {
	w := newWorld(t)
	weekly := w.template(t, map[string]any{
		"name": "Weekly Service", "duration_minutes": 30, "price_cents": 4500,
		"repeat": map[string]any{"unit": "weekly", "every": 1},
	})
	// Another customer holds the series' slot before its job is placed.
	other, otherLoc := newCustomer(t, w.app, "B")
	rec := w.app.Do(t, http.MethodPost, "/api/v1/bookings", other, map[string]any{
		"service_id": w.service.ID, "location_id": otherLoc, "date": tomorrow(), "time": "09:00",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	code, series := w.bookSeries(t, w.customer, w.location, weekly.ID, tomorrow(), "09:15")
	require.Equal(t, http.StatusCreated, code)
	w.generate(t)

	assert.Empty(t, w.jobs(t), "the taken date is skipped")
	var through string
	require.NoError(t, w.app.Pool.QueryRow(t.Context(),
		`SELECT generated_through::text FROM booking_series WHERE id = $1`, series.ID).Scan(&through))
	assert.Equal(t, tomorrow(), through, "and not retried")
}

func TestRecurringJobEnds(t *testing.T) {
	w := newWorld(t)
	once := w.template(t, map[string]any{
		"name": "One Visit", "duration_minutes": 30, "price_cents": 2000, "set_confirmed": false,
		"repeat": map[string]any{"unit": "daily", "every": 2, "ends": "after", "ends_after": 1},
	})
	code, series := w.bookSeries(t, w.customer, w.location, once.ID, tomorrow(), "08:00")
	require.Equal(t, http.StatusCreated, code)
	w.generate(t)

	jobs := w.jobs(t)
	require.Len(t, jobs, 1)
	assert.Equal(t, "draft", jobs[0].Estimate.Status, "set_confirmed=false makes a draft estimate")

	var ended bool
	require.NoError(t, w.app.Pool.QueryRow(t.Context(),
		`SELECT ended_at IS NOT NULL FROM booking_series WHERE id = $1`, series.ID).Scan(&ended))
	assert.True(t, ended, "the rule ran out")

	t.Run("completing bills the draft estimate too", func(t *testing.T) {
		id := jobs[0].ID
		require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/start", id), 0, nil).Code)
		require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/complete", id), 0, map[string]any{}).Code)
		var n int
		require.NoError(t, w.app.Pool.QueryRow(t.Context(),
			`SELECT count(*) FROM billing_documents WHERE converted_from_id = $1 AND status = 'draft'`, jobs[0].Estimate.ID).Scan(&n))
		assert.Equal(t, 1, n)
	})
}

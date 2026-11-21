//go:build integration

package bookings_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

func (w world) work(t *testing.T, id int64) dto.WorkOrder {
	t.Helper()
	rec := w.app.Do(t, http.MethodGet, fmt.Sprintf("/api/v1/bookings/%d", id), w.customer, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return testapp.Decode[dto.BookingResponse](t, rec).WorkOrder
}

func TestWorkOrderFlow(t *testing.T) {
	w := newWorld(t)
	r := w.book(t, w.customer, w.location, "09:00")
	require.Equal(t, http.StatusCreated, r.code, r.body)
	id := r.booking.ID
	start := fmt.Sprintf("/api/v1/bookings/%d/start", id)
	complete := fmt.Sprintf("/api/v1/bookings/%d/complete", id)

	assert.Equal(t, "scheduled", r.booking.WorkOrder.Status)

	t.Run("completing before starting is a conflict", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, w.app.Do(t, http.MethodPost, complete, 0, map[string]any{}).Code)
	})
	t.Run("start then complete", func(t *testing.T) {
		require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, start, 0, nil).Code)
		got := w.work(t, id)
		assert.Equal(t, "in_progress", got.Status)
		assert.NotNil(t, got.StartedAt)

		assert.Equal(t, http.StatusConflict, w.app.Do(t, http.MethodPost, start, 0, nil).Code, "starting twice")

		require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, complete, 0, map[string]any{"notes": "Filter replaced"}).Code)
		got = w.work(t, id)
		assert.Equal(t, "completed", got.Status)
		assert.NotNil(t, got.CompletedAt)
		require.NotNil(t, got.Notes)
		assert.Equal(t, "Filter replaced", *got.Notes)
	})
	t.Run("a missing booking is 404", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodPost, "/api/v1/bookings/999999/start", 0, nil).Code)
	})
	t.Run("staff endpoints need the admin key", func(t *testing.T) {
		rec := w.app.DoWith(t, http.MethodPost, start, 0, nil, map[string]string{"X-Admin-Key": "wrong"})
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestWorkOrderOfACancelledBooking(t *testing.T) {
	w := newWorld(t)
	r := w.book(t, w.customer, w.location, "10:00")
	require.Equal(t, http.StatusCreated, r.code, r.body)
	require.Equal(t, http.StatusOK, w.cancel(t, w.customer, r.booking.ID).Code)

	assert.Equal(t, "cancelled", w.work(t, r.booking.ID).Status)
	rec := w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/start", r.booking.ID), 0, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

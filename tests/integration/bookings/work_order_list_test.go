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

func (w world) workOrders(t *testing.T, customer int64, query string) dto.Page[dto.BookingResponse] {
	t.Helper()
	rec := w.app.Do(t, http.MethodGet, "/api/v1/work-orders"+query, customer, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return testapp.Decode[dto.Page[dto.BookingResponse]](t, rec)
}

func TestCustomerWorkOrderList(t *testing.T) {
	w := newWorld(t)
	a := w.book(t, w.customer, w.location, "08:00")
	b := w.book(t, w.customer, w.location, "09:00")
	c := w.book(t, w.customer, w.location, "10:00")
	for _, r := range []*testRec{a, b, c} {
		require.Equal(t, http.StatusCreated, r.code, r.body)
	}
	other, otherLoc := newCustomer(t, w.app, "Someone Else")
	o := w.book(t, other, otherLoc, "13:00")
	require.Equal(t, http.StatusCreated, o.code, o.body)

	// a is completed, b is cancelled, c stays scheduled.
	require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/start", a.booking.ID), 0, nil).Code)
	require.Equal(t, http.StatusNoContent, w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/complete", a.booking.ID), 0, map[string]any{}).Code)
	require.Equal(t, http.StatusOK, w.cancel(t, w.customer, b.booking.ID).Code)

	ids := func(p dto.Page[dto.BookingResponse]) []int64 {
		out := make([]int64, 0, len(p.Data))
		for _, it := range p.Data {
			out = append(out, it.ID)
		}
		return out
	}

	t.Run("only the caller's, newest first, with every status", func(t *testing.T) {
		p := w.workOrders(t, w.customer, "")
		assert.Equal(t, []int64{c.booking.ID, b.booking.ID, a.booking.ID}, ids(p))
		assert.Empty(t, p.NextCursor)
		assert.Equal(t, []string{"scheduled", "cancelled", "completed"},
			[]string{p.Data[0].WorkOrder.Status, p.Data[1].WorkOrder.Status, p.Data[2].WorkOrder.Status})
		assert.Equal(t, w.service.ID, p.Data[0].Service.ID, "linked to the service template")
		assert.Equal(t, []int64{o.booking.ID}, ids(w.workOrders(t, other, "")))
	})

	t.Run("status filter", func(t *testing.T) {
		for status, want := range map[string]int64{"scheduled": c.booking.ID, "cancelled": b.booking.ID, "completed": a.booking.ID} {
			assert.Equal(t, []int64{want}, ids(w.workOrders(t, w.customer, "?status="+status)), status)
		}
		assert.Empty(t, w.workOrders(t, w.customer, "?status=in_progress").Data)
	})

	t.Run("service filter", func(t *testing.T) {
		assert.Len(t, w.workOrders(t, w.customer, fmt.Sprintf("?service_id=%d", w.service.ID)).Data, 3)
		assert.Empty(t, w.workOrders(t, w.customer, "?service_id=999999").Data)
	})

	t.Run("pages with a cursor", func(t *testing.T) {
		first := w.workOrders(t, w.customer, "?limit=2")
		assert.Equal(t, []int64{c.booking.ID, b.booking.ID}, ids(first))
		require.NotEmpty(t, first.NextCursor)
		second := w.workOrders(t, w.customer, "?limit=2&cursor="+first.NextCursor)
		assert.Equal(t, []int64{a.booking.ID}, ids(second))
		assert.Empty(t, second.NextCursor)
	})

	t.Run("a new booking comes with its work order", func(t *testing.T) {
		assert.Equal(t, "scheduled", c.booking.WorkOrder.Status)
		assert.NotZero(t, c.booking.WorkOrder.ID)
	})
}

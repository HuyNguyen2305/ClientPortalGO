//go:build integration

package bookings_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

// world is a fresh API with one service and one customer who has one location.
type world struct {
	app      *testapp.App
	customer int64
	location int64
	service  dto.ServiceResponse
	day      string // a bookable date: the day after tomorrow in the business timezone
}

func newCustomer(t *testing.T, app *testapp.App, name string) (customerID, locationID int64) {
	t.Helper()
	rec := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code)
	customerID = testapp.Decode[dto.CustomerResponse](t, rec).ID
	rec = app.Do(t, http.MethodPost, "/api/v1/locations", customerID, map[string]any{
		"street": "West Loop 1604 South", "city": "San Antonio", "state": "TX", "postal_code": "12341",
	})
	require.Equal(t, http.StatusCreated, rec.Code)
	return customerID, testapp.Decode[dto.LocationResponse](t, rec).ID
}

func newWorld(t *testing.T) world {
	t.Helper()
	app := testapp.New(t)
	rec := app.Do(t, http.MethodPost, "/api/v1/services", 0,
		map[string]any{"name": "Initial Service", "duration_minutes": 45, "price_cents": 9000})
	require.Equal(t, http.StatusCreated, rec.Code)
	svc := testapp.Decode[dto.ServiceResponse](t, rec)
	c, l := newCustomer(t, app, "William Saliba")
	first, _ := testapp.Hours.BookableRange(time.Now())
	return world{app: app, customer: c, location: l, service: svc, day: first.AddDate(0, 0, 1).Format(time.DateOnly)}
}

func (w world) book(t *testing.T, customer, location int64, hhmm string) *testRec {
	t.Helper()
	rec := w.app.Do(t, http.MethodPost, "/api/v1/bookings", customer, map[string]any{
		"service_id": w.service.ID, "location_id": location, "date": w.day, "time": hhmm,
	})
	return &testRec{code: rec.Code, body: rec.Body.String(), booking: decodeIf[dto.BookingResponse](rec.Code, rec.Body.Bytes())}
}

type testRec struct {
	code    int
	body    string
	booking dto.BookingResponse
}

func decodeIf[T any](code int, b []byte) T {
	var v T
	if code < 300 {
		_ = json.Unmarshal(b, &v)
	}
	return v
}

func (w world) slots(t *testing.T) []string {
	t.Helper()
	rec := w.app.Do(t, http.MethodGet, fmt.Sprintf("/api/v1/services/%d/slots?date=%s", w.service.ID, w.day), w.customer, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return testapp.Decode[dto.SlotsResponse](t, rec).Slots
}

func TestBookingFlow(t *testing.T) {
	w := newWorld(t)
	assert.Contains(t, w.slots(t), "09:00")

	r := w.book(t, w.customer, w.location, "09:00")
	require.Equal(t, http.StatusCreated, r.code, r.body)
	b := r.booking
	assert.Equal(t, w.day, b.Date)
	assert.Equal(t, "09:00", b.Time)
	assert.Equal(t, "Initial Service", b.Service.Name)
	assert.Equal(t, w.location, b.Location.ID)
	assert.False(t, b.Cancelled)
	assert.Equal(t, 45*time.Minute, b.EndsAt.Sub(b.StartsAt))
	assert.Equal(t, "open", b.Estimate.Status)
	assert.Equal(t, int64(9000), b.Estimate.TotalCents)
	assert.Equal(t, int64(5000), b.Estimate.Number)

	t.Run("the slot and the starts that would run into it disappear", func(t *testing.T) {
		slots := w.slots(t)
		for _, gone := range []string{"08:30", "08:45", "09:00", "09:15", "09:30"} {
			assert.NotContains(t, slots, gone)
		}
		assert.Contains(t, slots, "08:15") // ends at 09:00
		assert.Contains(t, slots, "09:45") // starts as the visit ends
	})

	t.Run("listed and readable by its customer only", func(t *testing.T) {
		rec := w.app.Do(t, http.MethodGet, "/api/v1/bookings", w.customer, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		list := testapp.Decode[dto.Page[dto.BookingResponse]](t, rec).Data
		require.Len(t, list, 1)
		assert.Equal(t, b.ID, list[0].ID)

		other, _ := newCustomer(t, w.app, "B")
		p := fmt.Sprintf("/api/v1/bookings/%d", b.ID)
		assert.Equal(t, http.StatusOK, w.app.Do(t, http.MethodGet, p, w.customer, nil).Code)
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodGet, p, other, nil).Code)
		rec = w.app.Do(t, http.MethodGet, "/api/v1/bookings", other, nil)
		assert.JSONEq(t, `{"data":[]}`, rec.Body.String())
	})

	t.Run("one visit at a time across customers", func(t *testing.T) {
		other, otherLoc := newCustomer(t, w.app, "C")
		assert.Equal(t, http.StatusConflict, w.book(t, other, otherLoc, "09:15").code)
	})

	t.Run("the estimate flows on to a paid invoice", func(t *testing.T) {
		est := fmt.Sprintf("/api/v1/estimates/%d", b.Estimate.ID)
		require.Equal(t, http.StatusOK, w.app.Do(t, http.MethodPost, est+"/accept", w.customer, nil).Code)
		rec := w.app.Do(t, http.MethodPost, est+"/convert", w.customer, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		inv := testapp.Decode[dto.DocumentResponse](t, rec)
		invPath := fmt.Sprintf("/api/v1/invoices/%d", inv.ID)
		require.Equal(t, http.StatusOK, w.app.Do(t, http.MethodPost, invPath+"/send", w.customer, nil).Code)
		rec = w.app.Do(t, http.MethodPost, invPath+"/payments", w.customer, map[string]any{"amount_cents": 9000})
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, "paid", testapp.Decode[dto.DocumentResponse](t, rec).Status)

		// Accepting and converting keep the visit booked.
		rec = w.app.Do(t, http.MethodGet, fmt.Sprintf("/api/v1/bookings/%d", b.ID), w.customer, nil)
		assert.False(t, testapp.Decode[dto.BookingResponse](t, rec).Cancelled)
	})
}

func TestDecliningFreesTheSlot(t *testing.T) {
	w := newWorld(t)
	r := w.book(t, w.customer, w.location, "10:00")
	require.Equal(t, http.StatusCreated, r.code, r.body)
	assert.NotContains(t, w.slots(t), "10:00")

	rec := w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/estimates/%d/decline", r.booking.Estimate.ID), w.customer, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Contains(t, w.slots(t), "10:00")
	rec = w.app.Do(t, http.MethodGet, fmt.Sprintf("/api/v1/bookings/%d", r.booking.ID), w.customer, nil)
	assert.True(t, testapp.Decode[dto.BookingResponse](t, rec).Cancelled)
	rec = w.app.Do(t, http.MethodGet, "/api/v1/bookings", w.customer, nil)
	assert.JSONEq(t, `{"data":[]}`, rec.Body.String())

	again := w.book(t, w.customer, w.location, "10:00")
	assert.Equal(t, http.StatusCreated, again.code, again.body)
}

func (w world) cancel(t *testing.T, customer, bookingID int64) *httptest.ResponseRecorder {
	t.Helper()
	return w.app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/bookings/%d/cancel", bookingID), customer, nil)
}

func TestCancelBooking(t *testing.T) {
	w := newWorld(t)

	t.Run("an open booking is cancelled and its slot freed", func(t *testing.T) {
		r := w.book(t, w.customer, w.location, "08:00")
		require.Equal(t, http.StatusCreated, r.code, r.body)
		rec := w.cancel(t, w.customer, r.booking.ID)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		got := testapp.Decode[dto.BookingResponse](t, rec)
		assert.True(t, got.Cancelled)
		assert.Equal(t, "declined", got.Estimate.Status)
		assert.Contains(t, w.slots(t), "08:00")
		assert.Equal(t, http.StatusConflict, w.cancel(t, w.customer, r.booking.ID).Code, "cancelling twice")
	})

	t.Run("an accepted booking can be cancelled", func(t *testing.T) {
		r := w.book(t, w.customer, w.location, "09:00")
		require.Equal(t, http.StatusCreated, r.code, r.body)
		est := fmt.Sprintf("/api/v1/estimates/%d/accept", r.booking.Estimate.ID)
		require.Equal(t, http.StatusOK, w.app.Do(t, http.MethodPost, est, w.customer, nil).Code)
		rec := w.cancel(t, w.customer, r.booking.ID)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.True(t, testapp.Decode[dto.BookingResponse](t, rec).Cancelled)
	})

	t.Run("a booking whose estimate became an invoice cannot be cancelled", func(t *testing.T) {
		r := w.book(t, w.customer, w.location, "10:00")
		require.Equal(t, http.StatusCreated, r.code, r.body)
		est := fmt.Sprintf("/api/v1/estimates/%d", r.booking.Estimate.ID)
		require.Equal(t, http.StatusOK, w.app.Do(t, http.MethodPost, est+"/accept", w.customer, nil).Code)
		require.Equal(t, http.StatusCreated, w.app.Do(t, http.MethodPost, est+"/convert", w.customer, nil).Code)
		assert.Equal(t, http.StatusConflict, w.cancel(t, w.customer, r.booking.ID).Code)
	})

	t.Run("another customer's booking is not found", func(t *testing.T) {
		r := w.book(t, w.customer, w.location, "11:00")
		require.Equal(t, http.StatusCreated, r.code, r.body)
		other, _ := newCustomer(t, w.app, "B")
		assert.Equal(t, http.StatusNotFound, w.cancel(t, other, r.booking.ID).Code)
	})

	t.Run("an unknown booking is not found", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, w.cancel(t, w.customer, 999999).Code)
	})
}

func TestBookingCapPerCustomer(t *testing.T) {
	w := newWorld(t) // the test API allows 3 upcoming bookings per customer
	var first int64
	for i, hhmm := range []string{"07:00", "08:00", "09:00"} {
		r := w.book(t, w.customer, w.location, hhmm)
		require.Equal(t, http.StatusCreated, r.code, r.body)
		if i == 0 {
			first = r.booking.ID
		}
	}
	assert.Equal(t, http.StatusConflict, w.book(t, w.customer, w.location, "10:00").code, "fourth booking")

	other, otherLoc := newCustomer(t, w.app, "B")
	assert.Equal(t, http.StatusCreated, w.book(t, other, otherLoc, "10:00").code, "the cap is per customer")

	require.Equal(t, http.StatusOK, w.cancel(t, w.customer, first).Code)
	assert.Equal(t, http.StatusCreated, w.book(t, w.customer, w.location, "11:00").code, "cancelling frees a place")
}

func TestConcurrentBookingsOfOneSlot(t *testing.T) {
	w := newWorld(t)
	other, otherLoc := newCustomer(t, w.app, "B")
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i, c := range []struct{ customer, location int64 }{{w.customer, w.location}, {other, otherLoc}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = w.book(t, c.customer, c.location, "11:00").code
		}()
	}
	wg.Wait()
	assert.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, codes)
}

func TestBookingRejectsBadRequests(t *testing.T) {
	w := newWorld(t)
	first, last := testapp.Hours.BookableRange(time.Now())
	today := first.AddDate(0, 0, -1).Format(time.DateOnly)

	post := func(body map[string]any) int {
		return w.app.Do(t, http.MethodPost, "/api/v1/bookings", w.customer, body).Code
	}
	body := func(date, hhmm string) map[string]any {
		return map[string]any{"service_id": w.service.ID, "location_id": w.location, "date": date, "time": hhmm}
	}
	assert.Equal(t, http.StatusBadRequest, post(body(today, "09:00")), "today")
	assert.Equal(t, http.StatusBadRequest, post(body(last.AddDate(0, 0, 1).Format(time.DateOnly), "09:00")), "past the horizon")
	assert.Equal(t, http.StatusBadRequest, post(body(w.day, "09:10")), "off the grid")
	assert.Equal(t, http.StatusBadRequest, post(body(w.day, "16:30")), "45 minutes runs past closing")
	assert.Equal(t, http.StatusBadRequest, post(body(w.day, "6:00pm")), "bad time")
	assert.Equal(t, http.StatusBadRequest, post(body("12/11/2026", "09:00")), "bad date")

	_, otherLoc := newCustomer(t, w.app, "B")
	b := body(w.day, "09:00")
	b["location_id"] = otherLoc
	assert.Equal(t, http.StatusNotFound, post(b), "another customer's location")
	b = body(w.day, "09:00")
	b["service_id"] = 999999
	assert.Equal(t, http.StatusNotFound, post(b), "unknown service")

	rec := w.app.Do(t, http.MethodGet, fmt.Sprintf("/api/v1/services/%d/slots?date=%s", w.service.ID, today), w.customer, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = w.app.Do(t, http.MethodGet, fmt.Sprintf("/api/v1/services/%d/slots?date=%s", w.service.ID, w.day), 0, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "the header is required")
}

func TestBookingDays(t *testing.T) {
	w := newWorld(t)
	first, last := testapp.Hours.BookableRange(time.Now())
	days := func(query string) (int, dto.DaysResponse) {
		rec := w.app.Do(t, http.MethodGet, "/api/v1/booking/days"+query, w.customer, nil)
		if rec.Code != http.StatusOK {
			return rec.Code, dto.DaysResponse{}
		}
		return rec.Code, testapp.Decode[dto.DaysResponse](t, rec)
	}

	code, d := days("")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, d.Days, 7)
	assert.Equal(t, first.Format(time.DateOnly), d.Days[0].Date)
	assert.Equal(t, first.Weekday().String(), d.Days[0].Weekday)
	assert.Nil(t, d.Previous)
	require.NotNil(t, d.Next)
	assert.Equal(t, first.AddDate(0, 0, 7).Format(time.DateOnly), *d.Next)

	code, d = days("?from=" + *d.Next)
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, d.Previous)
	assert.Equal(t, first.Format(time.DateOnly), *d.Previous)

	code, d = days("?from=" + last.Format(time.DateOnly))
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, d.Days, 1)
	assert.Nil(t, d.Next)

	code, _ = days("?from=" + first.AddDate(0, 0, -1).Format(time.DateOnly))
	assert.Equal(t, http.StatusBadRequest, code)
}

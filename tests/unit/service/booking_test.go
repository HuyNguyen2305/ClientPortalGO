package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"clientportal/internal/model"
	"clientportal/internal/service"
	"clientportal/tests/unit/mocks"
)

type bookingFixture struct {
	docs     *mocks.MockBillingDocumentRepository
	locs     *mocks.MockLocationRepository
	services *mocks.MockServiceRepository
	svc      *service.BookingService
	loc      *time.Location
}

// newBookingFixture runs with fixedNow (2026-11-11 09:30 in Chicago), so tomorrow is Nov 12.
func newBookingFixture(t *testing.T) bookingFixture {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	hours := model.Hours{Loc: loc, Open: 7 * 60, Close: 17 * 60, Step: 15, HorizonDays: 90}
	docs := mocks.NewMockBillingDocumentRepository(t)
	docs.EXPECT().WithTx(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, fn func(service.BillingDocumentRepository) error) error { return fn(docs) }).
		Maybe()
	locs := mocks.NewMockLocationRepository(t)
	services := mocks.NewMockServiceRepository(t)
	return bookingFixture{docs: docs, locs: locs, services: services, loc: loc,
		svc: service.NewBookingService(docs, locs, services, hours, 3, fixedNow)}
}

var biMonthly = model.Service{ID: 2, Name: "Bi-Monthly Service", DurationMinutes: 30, PriceCents: 6500}

func validBooking() model.BookingInput {
	return model.BookingInput{ServiceID: 2, LocationID: 7, Date: "2026-11-12", Time: "07:00"}
}

func TestBook(t *testing.T) {
	t.Run("unknown service", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(model.Service{}, model.ErrNotFound)
		_, err := f.svc.Book(context.Background(), custID, validBooking())
		assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
	})

	t.Run("bad slot", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(biMonthly, nil)
		in := validBooking()
		in.Time = "16:45" // a 30-minute visit would run past 17:00
		_, err := f.svc.Book(context.Background(), custID, in)
		assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
	})

	t.Run("another customer's location", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(biMonthly, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{}, model.ErrNotFound)
		_, err := f.svc.Book(context.Background(), custID, validBooking())
		assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
	})

	t.Run("creates an open estimate and the booking", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(biMonthly, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(make([]model.Booking, 2), nil)
		f.docs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool {
			return d.ParentType == model.ParentEstimate && d.Status == model.StatusOpen &&
				d.CustomerID == custID && d.Location.ID == 7 && d.Title == "Bi-Monthly Service" &&
				d.DocumentDate.Equal(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)) && d.TotalCents == 6500 &&
				len(d.Items) == 1 && d.Items[0].Name == "Bi-Monthly Service" &&
				d.Items[0].Quantity == 1 && d.Items[0].UnitPriceCents == 6500
		})).Return(int64(41), nil)
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.MatchedBy(func(b model.Booking) bool {
			return b.CustomerID == custID && b.Service.ID == 2 && b.Location.ID == 7 && b.Estimate.ID == 41 &&
				b.StartsAt.Equal(time.Date(2026, 11, 12, 7, 0, 0, 0, f.loc)) &&
				b.EndsAt.Equal(time.Date(2026, 11, 12, 7, 30, 0, 0, f.loc))
		})).Return(int64(8), nil)
		f.docs.EXPECT().GetBooking(mock.Anything, custID, int64(8)).Return(model.Booking{ID: 8}, nil)

		got, err := f.svc.Book(context.Background(), custID, validBooking())
		require.NoError(t, err)
		assert.Equal(t, int64(8), got.ID)
	})

	t.Run("a customer at the cap cannot book more", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(biMonthly, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(make([]model.Booking, 3), nil)
		_, err := f.svc.Book(context.Background(), custID, validBooking())
		assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
	})

	t.Run("slot taken meanwhile", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(biMonthly, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(nil, nil)
		f.docs.EXPECT().Create(mock.Anything, mock.Anything).Return(int64(41), nil)
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.Anything).Return(int64(0), model.ErrConflict)
		_, err := f.svc.Book(context.Background(), custID, validBooking())
		assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
	})
}

func TestCancelBooking(t *testing.T) {
	cancelledAt := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	live := model.Booking{ID: 8, CustomerID: custID, Estimate: model.BillingDocument{ID: 41}}
	cancelled := live
	cancelled.CancelledAt = &cancelledAt

	tests := []struct {
		name      string
		booking   model.Booking
		getErr    error
		estimate  model.BillingDocument
		converted bool
		wantErr   error
		wantWrite bool // the estimate is declined and the booking cancelled
	}{
		{"not the customer's booking", live, model.ErrNotFound, model.BillingDocument{}, false, model.ErrNotFound, false},
		{"already cancelled", cancelled, nil, model.BillingDocument{}, false, model.ErrConflict, false},
		{"estimate already declined", live, nil, model.BillingDocument{ID: 41, Status: model.StatusDeclined}, false, model.ErrConflict, false},
		{"estimate became an invoice", live, nil, model.BillingDocument{ID: 41, Status: model.StatusAccepted}, true, model.ErrConflict, false},
		{"open estimate", live, nil, model.BillingDocument{ID: 41, Status: model.StatusOpen}, false, nil, true},
		{"accepted estimate", live, nil, model.BillingDocument{ID: 41, Status: model.StatusAccepted}, false, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBookingFixture(t)
			f.docs.EXPECT().GetBooking(mock.Anything, custID, int64(8)).Return(tt.booking, tt.getErr).Once()
			if tt.getErr == nil && tt.booking.CancelledAt == nil {
				f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(41)).Return(tt.estimate, nil)
			}
			if tt.estimate.Status == model.StatusOpen || tt.estimate.Status == model.StatusAccepted {
				f.docs.EXPECT().HasConvertedInvoice(mock.Anything, int64(41)).Return(tt.converted, nil)
			}
			if tt.wantWrite {
				f.docs.EXPECT().UpdateStatus(mock.Anything, int64(41), model.StatusDeclined).Return(nil).Once()
				f.docs.EXPECT().CancelBookingForEstimate(mock.Anything, int64(41)).Return(nil).Once()
				f.docs.EXPECT().GetBooking(mock.Anything, custID, int64(8)).Return(cancelled, nil).Once()
			}

			got, err := f.svc.Cancel(context.Background(), custID, 8)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, got.CancelledAt)
		})
	}
}

func TestSlots(t *testing.T) {
	f := newBookingFixture(t)
	f.services.EXPECT().Get(mock.Anything, int64(2)).Return(biMonthly, nil)
	dayStart := time.Date(2026, 11, 12, 0, 0, 0, 0, f.loc)
	f.docs.EXPECT().BusyIntervals(mock.Anything, mock.MatchedBy(dayStart.Equal), mock.Anything).
		Return([]model.Interval{{Start: time.Date(2026, 11, 12, 7, 0, 0, 0, f.loc), End: time.Date(2026, 11, 12, 8, 0, 0, 0, f.loc)}}, nil)

	day, slots, err := f.svc.Slots(context.Background(), 2, "2026-11-12")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), day)
	assert.Equal(t, "08:00", slots[0])

	_, _, err = f.svc.Slots(context.Background(), 2, "2026-11-11") // today is not bookable
	assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
}

func TestDaysDefaultsToTomorrow(t *testing.T) {
	f := newBookingFixture(t)
	days, prev, next, err := f.svc.Days("")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), days[0])
	assert.Nil(t, prev)
	assert.NotNil(t, next)

	_, _, _, err = f.svc.Days("not-a-date")
	assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
}

func TestDecliningAnEstimateCancelsItsBooking(t *testing.T) {
	f := newDocFixture(t)
	f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(41)).
		Return(model.BillingDocument{ID: 41, Status: model.StatusOpen}, nil)
	f.docs.EXPECT().UpdateStatus(mock.Anything, int64(41), model.StatusDeclined).Return(nil)
	f.docs.EXPECT().CancelBookingForEstimate(mock.Anything, int64(41)).Return(nil).Once()
	f.docs.EXPECT().Get(mock.Anything, custID, model.ParentEstimate, int64(41)).Return(model.BillingDocument{ID: 41}, nil)
	f.docs.EXPECT().ListItems(mock.Anything, int64(41)).Return(nil, nil)

	_, err := f.svc.Transition(context.Background(), custID, model.ParentEstimate, 41, model.StatusDeclined)
	require.NoError(t, err)
}

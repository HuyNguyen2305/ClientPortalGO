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

var biMonthly = model.Service{ID: 2, Name: "Bi-Monthly Service", DurationMinutes: 30, PriceCents: 6500,
	Status: model.ServiceActive, SetConfirmed: true}

// weekly is a template that repeats every week and never ends.
var weekly = model.Service{ID: 3, Name: "Weekly Service", DurationMinutes: 30, PriceCents: 4500,
	Status: model.ServiceActive, SetConfirmed: true,
	Recurrence: model.Recurrence{Unit: model.RepeatWeekly, Every: 1, EndsKind: model.EndsNever},
	Items:      []model.Item{{Name: "Visit", Quantity: 1, UnitPriceCents: 3000}, {Name: "Supplies", Quantity: 1, UnitPriceCents: 1500}}}

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

	t.Run("an archived or recurring service is refused", func(t *testing.T) {
		f := newBookingFixture(t)
		archived := biMonthly
		archived.Status = model.ServiceArchived
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(archived, nil).Once()
		_, err := f.svc.Book(context.Background(), custID, validBooking())
		assert.ErrorIs(t, err, model.ErrNotFound)

		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(weekly, nil).Once()
		in := validBooking()
		in.ServiceID = 3
		_, err = f.svc.Book(context.Background(), custID, in)
		assert.ErrorIs(t, err, model.ErrValidation)
	})

	t.Run("series jobs do not count against the cap", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(biMonthly, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		seriesID := int64(1)
		jobs := []model.Booking{{SeriesID: &seriesID}, {SeriesID: &seriesID}, {SeriesID: &seriesID}, {}}
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(jobs, nil)
		f.docs.EXPECT().Create(mock.Anything, mock.Anything).Return(int64(41), nil)
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.Anything).Return(int64(8), nil)
		f.docs.EXPECT().GetBooking(mock.Anything, custID, int64(8)).Return(model.Booking{ID: 8}, nil)
		_, err := f.svc.Book(context.Background(), custID, validBooking())
		require.NoError(t, err)
	})

	t.Run("a template that is not confirmed makes a draft estimate", func(t *testing.T) {
		f := newBookingFixture(t)
		unconfirmed := biMonthly
		unconfirmed.SetConfirmed = false
		f.services.EXPECT().Get(mock.Anything, int64(2)).Return(unconfirmed, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(nil, nil)
		f.docs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool { return d.Status == model.StatusDraft })).
			Return(int64(41), nil)
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.Anything).Return(int64(8), nil)
		f.docs.EXPECT().GetBooking(mock.Anything, custID, int64(8)).Return(model.Booking{ID: 8}, nil)
		_, err := f.svc.Book(context.Background(), custID, validBooking())
		require.NoError(t, err)
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

func seriesInput() model.BookingInput {
	return model.BookingInput{ServiceID: 3, LocationID: 7, Date: "2026-11-12", Time: "07:00"} // a Thursday
}

func TestBookSeries(t *testing.T) {
	t.Run("a service that does not repeat", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(biMonthly, nil)
		_, err := f.svc.BookSeries(context.Background(), custID, seriesInput())
		assert.ErrorIs(t, err, model.ErrValidation)
	})

	t.Run("the customer already has this series", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(weekly, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		seriesID := int64(1)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).
			Return([]model.Booking{{SeriesID: &seriesID, Service: model.Service{ID: 3}}}, nil)
		_, err := f.svc.BookSeries(context.Background(), custID, seriesInput())
		assert.ErrorIs(t, err, model.ErrConflict)
	})

	t.Run("books every week to the horizon and skips a taken date", func(t *testing.T) {
		f := newBookingFixture(t)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(weekly, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(nil, nil)
		// Another customer holds 07:15-07:45 on Nov 19, which overlaps the 07:00-07:30 visit.
		busy := model.Interval{Start: time.Date(2026, 11, 19, 7, 15, 0, 0, f.loc), End: time.Date(2026, 11, 19, 7, 45, 0, 0, f.loc)}
		f.docs.EXPECT().BusyIntervals(mock.Anything, mock.Anything, mock.Anything).Return([]model.Interval{busy}, nil)
		f.docs.EXPECT().CreateSeries(mock.Anything, custID, int64(7), int64(3), mock.Anything).Return(int64(5), nil)
		// Tomorrow (Nov 12) through today + 90 days (Feb 9) holds 13 Thursdays; one is skipped.
		var created []model.BillingDocument
		f.docs.EXPECT().Create(mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, d model.BillingDocument) (int64, error) {
				created = append(created, d)
				return int64(100 + len(created)), nil
			})
		var seriesIDs []int64
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, b model.Booking) (int64, error) {
				require.NotNil(t, b.SeriesID)
				seriesIDs = append(seriesIDs, *b.SeriesID)
				return int64(len(seriesIDs)), nil
			})
		f.docs.EXPECT().ListSeriesBookings(mock.Anything, custID, int64(5)).Return([]model.Booking{{ID: 1}}, nil)

		got, err := f.svc.BookSeries(context.Background(), custID, seriesInput())
		require.NoError(t, err)
		assert.Equal(t, int64(5), got.ID)
		assert.Equal(t, []time.Time{time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)}, got.Skipped)
		assert.Len(t, created, 12)
		assert.Len(t, seriesIDs, 12)
		for _, d := range created {
			assert.Equal(t, model.StatusOpen, d.Status)
			assert.Equal(t, int64(4500), d.TotalCents)
			assert.Len(t, d.Items, 2, "template items are copied onto each estimate")
		}
	})

	t.Run("every date taken", func(t *testing.T) {
		f := newBookingFixture(t)
		one := weekly
		one.Recurrence.EndsKind, one.Recurrence.EndsAfter = model.EndsAfter, 1
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(one, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(nil, nil)
		busy := model.Interval{Start: time.Date(2026, 11, 12, 7, 0, 0, 0, f.loc), End: time.Date(2026, 11, 12, 8, 0, 0, 0, f.loc)}
		f.docs.EXPECT().BusyIntervals(mock.Anything, mock.Anything, mock.Anything).Return([]model.Interval{busy}, nil)
		_, err := f.svc.BookSeries(context.Background(), custID, seriesInput())
		assert.ErrorIs(t, err, model.ErrConflict)
	})

	t.Run("the series ended before the first date", func(t *testing.T) {
		f := newBookingFixture(t)
		ended := weekly
		end := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		ended.Recurrence.EndsKind, ended.Recurrence.EndsOn = model.EndsOnDate, &end
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(ended, nil)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
		f.docs.EXPECT().ListUpcoming(mock.Anything, custID, mock.Anything).Return(nil, nil)
		_, err := f.svc.BookSeries(context.Background(), custID, seriesInput())
		assert.ErrorIs(t, err, model.ErrValidation)
	})
}

func TestCancelBooking(t *testing.T) {
	cancelledAt := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	live := model.Booking{ID: 8, CustomerID: custID, Estimate: model.BillingDocument{ID: 41}, StartsAt: fixedNow().Add(24 * time.Hour)}
	cancelled := live
	cancelled.CancelledAt = &cancelledAt
	started := live
	started.StartsAt = fixedNow().Add(-time.Hour)

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
		{"already started", started, nil, model.BillingDocument{}, false, model.ErrConflict, false},
		{"estimate already declined", live, nil, model.BillingDocument{ID: 41, Status: model.StatusDeclined}, false, model.ErrConflict, false},
		{"estimate became an invoice", live, nil, model.BillingDocument{ID: 41, Status: model.StatusAccepted}, true, model.ErrConflict, false},
		{"open estimate", live, nil, model.BillingDocument{ID: 41, Status: model.StatusOpen}, false, nil, true},
		{"accepted estimate", live, nil, model.BillingDocument{ID: 41, Status: model.StatusAccepted}, false, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBookingFixture(t)
			f.docs.EXPECT().GetBooking(mock.Anything, custID, int64(8)).Return(tt.booking, tt.getErr).Once()
			if tt.getErr == nil && tt.booking.CancelledAt == nil && tt.booking.StartsAt.After(fixedNow()) {
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

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"clientportal/internal/model"
)

// dueSeries is a weekly series whose first job is tomorrow, 07:00, with no job placed yet.
func dueSeries(f bookingFixture) model.SeriesRun {
	return model.SeriesRun{ID: 5, ServiceID: 3, CustomerID: custID, LocationID: 7,
		FirstStart:       time.Date(2026, 11, 12, 7, 0, 0, 0, f.loc),
		GeneratedThrough: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}
}

var nov12 = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)

func TestGenerateDue(t *testing.T) {
	t.Run("places the job that falls due and moves the cursor", func(t *testing.T) {
		f := newBookingFixture(t)
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return([]int64{5}, nil)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(5)).Return(dueSeries(f), nil)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(weekly, nil)
		f.docs.EXPECT().BusyIntervals(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		f.docs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool {
			return d.ParentType == model.ParentEstimate && d.CustomerID == custID && d.Status == model.StatusOpen &&
				d.TotalCents == 4500 && len(d.Items) == 2
		})).Return(int64(41), nil)
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.MatchedBy(func(b model.Booking) bool {
			return b.SeriesID != nil && *b.SeriesID == 5 && b.Estimate.ID == 41 && b.Location.ID == 7 &&
				b.StartsAt.Equal(time.Date(2026, 11, 12, 7, 0, 0, 0, f.loc)) && b.EndsAt.Sub(b.StartsAt) == 30*time.Minute
		})).Return(int64(8), nil)
		f.docs.EXPECT().AdvanceSeries(mock.Anything, int64(5), nov12, false).Return(nil)
		require.NoError(t, f.svc.GenerateDue(context.Background()))
	})

	t.Run("a taken slot is skipped and the cursor still moves", func(t *testing.T) {
		f := newBookingFixture(t)
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return([]int64{5}, nil)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(5)).Return(dueSeries(f), nil)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(weekly, nil)
		busy := model.Interval{Start: time.Date(2026, 11, 12, 7, 15, 0, 0, f.loc), End: time.Date(2026, 11, 12, 7, 45, 0, 0, f.loc)}
		f.docs.EXPECT().BusyIntervals(mock.Anything, mock.Anything, mock.Anything).Return([]model.Interval{busy}, nil)
		f.docs.EXPECT().AdvanceSeries(mock.Anything, int64(5), nov12, false).Return(nil)
		require.NoError(t, f.svc.GenerateDue(context.Background()))
	})

	t.Run("a job whose time has passed is skipped", func(t *testing.T) {
		f := newBookingFixture(t)
		run := dueSeries(f)
		run.FirstStart = time.Date(2026, 11, 11, 9, 0, 0, 0, f.loc) // today 09:00, before the 09:30 clock
		run.GeneratedThrough = time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
		daily := weekly
		daily.Recurrence.Unit = model.RepeatDaily
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return([]int64{5}, nil)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(5)).Return(run, nil)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(daily, nil)
		f.docs.EXPECT().BusyIntervals(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		f.docs.EXPECT().Create(mock.Anything, mock.Anything).Return(int64(41), nil).Once() // Nov 12 only
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.Anything).Return(int64(8), nil).Once()
		f.docs.EXPECT().AdvanceSeries(mock.Anything, int64(5), nov12, false).Return(nil)
		require.NoError(t, f.svc.GenerateDue(context.Background()))
	})

	t.Run("a series another instance holds is left alone", func(t *testing.T) {
		f := newBookingFixture(t)
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return([]int64{5}, nil)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(5)).Return(model.SeriesRun{}, model.ErrNotFound)
		require.NoError(t, f.svc.GenerateDue(context.Background()))
	})

	t.Run("nothing due", func(t *testing.T) {
		f := newBookingFixture(t)
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return(nil, nil)
		require.NoError(t, f.svc.GenerateDue(context.Background()))
	})

	t.Run("the last job of a finite series ends it", func(t *testing.T) {
		f := newBookingFixture(t)
		one := weekly
		one.Recurrence.EndsKind, one.Recurrence.EndsAfter = model.EndsAfter, 1
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return([]int64{5}, nil)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(5)).Return(dueSeries(f), nil)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(one, nil)
		f.docs.EXPECT().BusyIntervals(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		f.docs.EXPECT().Create(mock.Anything, mock.Anything).Return(int64(41), nil)
		f.docs.EXPECT().CreateBooking(mock.Anything, mock.Anything).Return(int64(8), nil)
		f.docs.EXPECT().AdvanceSeries(mock.Anything, int64(5), nov12, true).Return(nil)
		require.NoError(t, f.svc.GenerateDue(context.Background()))
	})

	t.Run("a deleted template places nothing and ends the series", func(t *testing.T) {
		f := newBookingFixture(t)
		gone := weekly
		gone.Status = model.ServiceDeleted
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return([]int64{5}, nil)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(5)).Return(dueSeries(f), nil)
		f.services.EXPECT().Get(mock.Anything, int64(3)).Return(gone, nil)
		f.docs.EXPECT().AdvanceSeries(mock.Anything, int64(5), nov12, true).Return(nil)
		require.NoError(t, f.svc.GenerateDue(context.Background()))
	})

	t.Run("one failing series does not stop the next", func(t *testing.T) {
		f := newBookingFixture(t)
		f.docs.EXPECT().ListDueSeries(mock.Anything, nov12).Return([]int64{5, 6}, nil)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(5)).Return(model.SeriesRun{}, assert.AnError)
		f.docs.EXPECT().LockSeries(mock.Anything, int64(6)).Return(model.SeriesRun{}, model.ErrNotFound)
		assert.ErrorIs(t, f.svc.GenerateDue(context.Background()), assert.AnError)
	})
}

func TestEndSeries(t *testing.T) {
	f := newBookingFixture(t)
	f.docs.EXPECT().EndSeries(mock.Anything, custID, int64(5)).Return(nil).Once()
	assert.NoError(t, f.svc.EndSeries(context.Background(), custID, 5))
	f.docs.EXPECT().EndSeries(mock.Anything, custID, int64(6)).Return(model.ErrNotFound).Once()
	assert.ErrorIs(t, f.svc.EndSeries(context.Background(), custID, 6), model.ErrNotFound)
}

func TestCompleteSeriesJob(t *testing.T) {
	est := func(s model.Status) model.BillingDocument {
		return model.BillingDocument{ID: 41, CustomerID: custID, Status: s, Title: "Weekly Service", Location: model.Location{ID: 7}}
	}
	tests := []struct {
		name        string
		series      bool
		estimate    model.BillingDocument
		converted   bool
		wantAccept  bool // the estimate is set accepted first
		wantInvoice bool
	}{
		{"not a series job", false, model.BillingDocument{}, false, false, false},
		{"open estimate", true, est(model.StatusOpen), false, true, true},
		{"draft estimate", true, est(model.StatusDraft), false, true, true},
		{"accepted estimate", true, est(model.StatusAccepted), false, false, true},
		{"declined estimate", true, est(model.StatusDeclined), false, false, false},
		{"already converted", true, est(model.StatusAccepted), true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBookingFixture(t)
			f.docs.EXPECT().AdvanceWork(mock.Anything, int64(9), model.WorkInProgress, model.WorkCompleted, mock.Anything).Return(nil)
			f.docs.EXPECT().SeriesJobEstimate(mock.Anything, int64(9)).Return(custID, int64(41), tt.series, nil)
			if tt.series {
				f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(41)).Return(tt.estimate, nil)
			}
			if tt.series && tt.estimate.Status != model.StatusDeclined {
				f.docs.EXPECT().HasConvertedInvoice(mock.Anything, int64(41)).Return(tt.converted, nil)
			}
			if tt.wantAccept {
				f.docs.EXPECT().UpdateStatus(mock.Anything, int64(41), model.StatusAccepted).Return(nil).Once()
			}
			if tt.wantInvoice && !tt.converted {
				f.docs.EXPECT().ListItems(mock.Anything, int64(41)).Return([]model.Item{{ID: 1, Name: "Visit", Quantity: 1, UnitPriceCents: 3000}}, nil)
				f.docs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool {
					return d.ParentType == model.ParentInvoice && d.Status == model.StatusDraft && d.TotalCents == 3000 &&
						d.ConvertedFromID != nil && *d.ConvertedFromID == 41 && d.Items[0].ID == 0
				})).Return(int64(60), nil).Once()
			}
			assert.NoError(t, f.svc.Complete(context.Background(), 9, nil))
		})
	}
}

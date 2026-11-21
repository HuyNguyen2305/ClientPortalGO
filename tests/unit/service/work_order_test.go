package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"clientportal/internal/model"
)

func TestListWork(t *testing.T) {
	row := func(id int64) model.Booking { return model.Booking{WorkOrderID: id} }
	tests := []struct {
		name      string
		filter    model.WorkFilter
		repo      []model.Booking
		repoErr   error
		wantIDs   []int64
		wantNext  int64
		wantErrIs error
	}{
		{name: "a full page returns the next id", filter: model.WorkFilter{CustomerID: 1, Limit: 2},
			repo: []model.Booking{row(9), row(8), row(7)}, wantIDs: []int64{9, 8}, wantNext: 8},
		{name: "the last page has no next id", filter: model.WorkFilter{CustomerID: 1, Limit: 2},
			repo: []model.Booking{row(9), row(8)}, wantIDs: []int64{9, 8}},
		{name: "empty", filter: model.WorkFilter{CustomerID: 1, Limit: 5}, wantIDs: []int64{}},
		{name: "limit below one", filter: model.WorkFilter{CustomerID: 1}, wantErrIs: model.ErrValidation},
		{name: "unknown status", filter: model.WorkFilter{CustomerID: 1, Limit: 5, Status: "lost"}, wantErrIs: model.ErrValidation},
		{name: "cancelled is a status", filter: model.WorkFilter{CustomerID: 1, Limit: 5, Status: model.WorkCancelled}, wantIDs: []int64{}},
		{name: "repository error passes through", filter: model.WorkFilter{CustomerID: 1, Limit: 5},
			repoErr: model.ErrNotFound, wantErrIs: model.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBookingFixture(t)
			if tt.wantErrIs == nil || tt.repoErr != nil {
				asked := tt.filter
				asked.Limit++ // the service asks for one extra row to see whether another page follows
				f.docs.EXPECT().ListWorkOrders(mock.Anything, asked).Return(tt.repo, tt.repoErr)
			}
			got, next, err := f.svc.ListWork(context.Background(), tt.filter)
			if tt.wantErrIs != nil {
				assert.True(t, errors.Is(err, tt.wantErrIs), "got %v", err)
				return
			}
			assert.NoError(t, err)
			ids := make([]int64, 0, len(got))
			for _, b := range got {
				ids = append(ids, b.WorkOrderID)
			}
			assert.Equal(t, tt.wantIDs, ids)
			assert.Equal(t, tt.wantNext, next)
		})
	}
}

func TestStartAndComplete(t *testing.T) {
	notes := "all done"
	tests := []struct {
		name     string
		run      func(f bookingFixture) error
		from, to model.WorkStatus
		notes    *string
	}{
		{"start", func(f bookingFixture) error { return f.svc.Start(context.Background(), 9) },
			model.WorkScheduled, model.WorkInProgress, nil},
		{"complete", func(f bookingFixture) error { return f.svc.Complete(context.Background(), 9, &notes) },
			model.WorkInProgress, model.WorkCompleted, &notes},
	}
	for _, tt := range tests {
		for _, repoErr := range []error{nil, model.ErrNotFound, model.ErrConflict} {
			t.Run(tt.name, func(t *testing.T) {
				f := newBookingFixture(t)
				f.docs.EXPECT().AdvanceWork(mock.Anything, int64(9), tt.from, tt.to, tt.notes).Return(repoErr)
				err := tt.run(f)
				if repoErr == nil {
					assert.NoError(t, err)
					return
				}
				assert.True(t, errors.Is(err, repoErr), "got %v", err)
			})
		}
	}
}

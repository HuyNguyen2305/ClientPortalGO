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

func TestCatalog_CreateValidates(t *testing.T) {
	tests := []struct {
		name     string
		in       model.Service
		wantName string
		wantErr  error
	}{
		{"trims the name", model.Service{Name: "  Initial Service ", DurationMinutes: 45, PriceCents: 9000}, "Initial Service", nil},
		{"blank name", model.Service{Name: "  ", DurationMinutes: 30}, "", model.ErrValidation},
		{"duration off the 15-minute grid", model.Service{Name: "x", DurationMinutes: 20}, "", model.ErrValidation},
		{"duration too short", model.Service{Name: "x", DurationMinutes: 0}, "", model.ErrValidation},
		{"duration too long", model.Service{Name: "x", DurationMinutes: 495}, "", model.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockServiceRepository(t)
			if tt.wantErr == nil {
				repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s model.Service) bool {
					return s.Name == tt.wantName && s.DurationMinutes == 45
				})).Return(model.Service{ID: 1, Name: tt.wantName}, nil)
			}
			_, err := service.NewCatalog(repo).Create(context.Background(), tt.in)
			if tt.wantErr != nil {
				assert.True(t, errors.Is(err, tt.wantErr), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCatalog_UpdateKeepsID(t *testing.T) {
	repo := mocks.NewMockServiceRepository(t)
	repo.EXPECT().Update(mock.Anything, mock.MatchedBy(func(s model.Service) bool { return s.ID == 4 })).
		Return(model.Service{}, model.ErrNotFound)
	_, err := service.NewCatalog(repo).Update(context.Background(), 4, model.Service{Name: "x", DurationMinutes: 15})
	assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
}

func TestCatalog_RecurrenceValidation(t *testing.T) {
	endDate := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		r       model.Recurrence
		want    model.Recurrence
		wantErr bool
	}{
		{"empty means off", model.Recurrence{}, model.Recurrence{Unit: "off", Every: 1, EndsKind: "never"}, false},
		{"monthly defaults to day of month", model.Recurrence{Unit: "monthly"}, model.Recurrence{Unit: "monthly", Every: 1, By: "monthday", EndsKind: "never"}, false},
		{"by on a weekly repeat", model.Recurrence{Unit: "weekly", By: "weekday"}, model.Recurrence{}, true},
		{"after needs a count", model.Recurrence{Unit: "daily", EndsKind: "after"}, model.Recurrence{}, true},
		{"on_date needs a date", model.Recurrence{Unit: "daily", EndsKind: "on_date"}, model.Recurrence{}, true},
		{"ends_after is dropped unless ends after", model.Recurrence{Unit: "daily", EndsAfter: 3, EndsOn: &endDate}, model.Recurrence{Unit: "daily", Every: 1, EndsKind: "never"}, false},
		{"after keeps its count", model.Recurrence{Unit: "daily", EndsKind: "after", EndsAfter: 3}, model.Recurrence{Unit: "daily", Every: 1, EndsKind: "after", EndsAfter: 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockServiceRepository(t)
			if !tt.wantErr {
				repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s model.Service) bool { return s.Recurrence == tt.want })).
					Return(model.Service{ID: 1}, nil)
			}
			_, err := service.NewCatalog(repo).Create(context.Background(), model.Service{Name: "x", DurationMinutes: 15, Recurrence: tt.r})
			if tt.wantErr {
				assert.ErrorIs(t, err, model.ErrValidation)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCatalog_ItemsSetThePrice(t *testing.T) {
	repo := mocks.NewMockServiceRepository(t)
	repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s model.Service) bool { return s.PriceCents == 2*3000+500 })).
		Return(model.Service{ID: 1}, nil)
	_, err := service.NewCatalog(repo).Create(context.Background(), model.Service{
		Name: "x", DurationMinutes: 15, PriceCents: 1,
		Items: []model.Item{{Name: "a", Quantity: 2, UnitPriceCents: 3000}, {Name: "b", Quantity: 1, UnitPriceCents: 500}},
	})
	require.NoError(t, err)
}

func TestCatalog_ListStatus(t *testing.T) {
	repo := mocks.NewMockServiceRepository(t)
	repo.EXPECT().List(mock.Anything, "active").Return(nil, nil).Twice()
	repo.EXPECT().List(mock.Anything, "archived").Return(nil, nil).Once()
	c := service.NewCatalog(repo)
	for _, status := range []string{"", "active", "archived"} {
		_, err := c.List(context.Background(), status)
		require.NoError(t, err, status)
	}
	_, err := c.List(context.Background(), "gone")
	assert.ErrorIs(t, err, model.ErrValidation)
}

func TestCatalog_GetActiveHidesArchived(t *testing.T) {
	repo := mocks.NewMockServiceRepository(t)
	repo.EXPECT().Get(mock.Anything, int64(1)).Return(model.Service{ID: 1, Status: model.ServiceActive}, nil)
	repo.EXPECT().Get(mock.Anything, int64(2)).Return(model.Service{ID: 2, Status: model.ServiceArchived}, nil)
	c := service.NewCatalog(repo)
	_, err := c.GetActive(context.Background(), 1)
	require.NoError(t, err)
	_, err = c.GetActive(context.Background(), 2)
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestCatalog_RestoreSetsActive(t *testing.T) {
	repo := mocks.NewMockServiceRepository(t)
	repo.EXPECT().SetStatus(mock.Anything, int64(4), model.ServiceActive).Return(nil).Once()
	repo.EXPECT().SetStatus(mock.Anything, int64(5), model.ServiceActive).Return(model.ErrNotFound).Once()
	c := service.NewCatalog(repo)
	require.NoError(t, c.Restore(context.Background(), 4))
	assert.ErrorIs(t, c.Restore(context.Background(), 5), model.ErrNotFound, "a deleted service stays deleted")
}

func TestCatalog_ArchiveAndDeleteSetStatus(t *testing.T) {
	repo := mocks.NewMockServiceRepository(t)
	repo.EXPECT().SetStatus(mock.Anything, int64(4), model.ServiceArchived).Return(nil)
	repo.EXPECT().SetStatus(mock.Anything, int64(4), model.ServiceDeleted).Return(model.ErrNotFound)
	c := service.NewCatalog(repo)
	require.NoError(t, c.Archive(context.Background(), 4))
	assert.ErrorIs(t, c.Delete(context.Background(), 4), model.ErrNotFound)
}

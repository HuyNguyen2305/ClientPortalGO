package service_test

import (
	"context"
	"errors"
	"testing"

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

func TestCatalog_DeleteConflictPassesThrough(t *testing.T) {
	repo := mocks.NewMockServiceRepository(t)
	repo.EXPECT().Delete(mock.Anything, int64(4)).Return(model.ErrConflict)
	err := service.NewCatalog(repo).Delete(context.Background(), 4)
	assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
}

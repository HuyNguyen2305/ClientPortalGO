package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"clientportal/internal/model"
	"clientportal/internal/service"
	"clientportal/tests/unit/mocks"
)

func TestLocationService_CreateValidates(t *testing.T) {
	valid := model.Location{Street: "West Loop 1604 South", City: "San Antonio", State: "TX", PostalCode: "12341"}
	tests := []struct {
		name    string
		mutate  func(*model.Location)
		wantErr error
	}{
		{"valid", func(*model.Location) {}, nil},
		{"blank street", func(l *model.Location) { l.Street = "   " }, model.ErrValidation},
		{"blank city", func(l *model.Location) { l.City = "" }, model.ErrValidation},
		{"blank state", func(l *model.Location) { l.State = " " }, model.ErrValidation},
		{"blank postal code", func(l *model.Location) { l.PostalCode = "\t" }, model.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockLocationRepository(t)
			in := valid
			tt.mutate(&in)
			if tt.wantErr == nil {
				repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(l model.Location) bool {
					return l.CustomerID == 3 && l.Street == valid.Street
				})).Return(model.Location{ID: 1}, nil)
			}
			_, err := service.NewLocationService(repo).Create(context.Background(), 3, in)
			if tt.wantErr != nil {
				assert.True(t, errors.Is(err, tt.wantErr), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLocationService_UpdateIsScopedToCustomer(t *testing.T) {
	repo := mocks.NewMockLocationRepository(t)
	repo.EXPECT().Update(mock.Anything, mock.MatchedBy(func(l model.Location) bool {
		return l.ID == 5 && l.CustomerID == 3
	})).Return(model.Location{}, fmt.Errorf("update: %w", model.ErrNotFound))
	_, err := service.NewLocationService(repo).Update(context.Background(), 3, 5,
		model.Location{Street: "a", City: "b", State: "c", PostalCode: "d"})
	assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
}

func TestLocationService_KeepsRepositoryErrorsMatchable(t *testing.T) {
	repo := mocks.NewMockLocationRepository(t)
	repo.EXPECT().Delete(mock.Anything, int64(3), int64(5)).Return(fmt.Errorf("delete: %w", model.ErrConflict))
	err := service.NewLocationService(repo).Delete(context.Background(), 3, 5)
	assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
}

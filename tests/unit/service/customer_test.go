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

func TestCustomerService_Create(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantErr  error
	}{
		{"trims the name", "  William Saliba  ", "William Saliba", nil},
		{"rejects a blank name", "   ", "", model.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockCustomerRepository(t)
			if tt.wantErr == nil {
				repo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(c model.Customer) bool {
					return c.Name == tt.wantName
				})).Return(model.Customer{ID: 1, Name: tt.wantName}, nil)
			}
			got, err := service.NewCustomerService(repo).Create(context.Background(), tt.input, nil, nil)
			if tt.wantErr != nil {
				assert.True(t, errors.Is(err, tt.wantErr), "got %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

func TestCustomerService_List(t *testing.T) {
	rows := func(n int) []model.Customer {
		out := make([]model.Customer, n)
		for i := range out {
			out[i] = model.Customer{ID: int64(i + 1)}
		}
		return out
	}
	tests := []struct {
		name     string
		returned int
		wantLen  int
		wantNext int64
	}{
		{"more rows than the page", 21, 20, 20},
		{"exactly a page", 20, 20, 0},
		{"short page", 3, 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockCustomerRepository(t)
			repo.EXPECT().List(mock.Anything, int64(0), 21).Return(rows(tt.returned), nil)
			items, next, err := service.NewCustomerService(repo).List(context.Background(), 0, 20)
			require.NoError(t, err)
			assert.Len(t, items, tt.wantLen)
			assert.Equal(t, tt.wantNext, next)
		})
	}
}

func TestCustomerService_WrapsRepositoryErrors(t *testing.T) {
	repo := mocks.NewMockCustomerRepository(t)
	repo.EXPECT().Get(mock.Anything, int64(9)).Return(model.Customer{}, fmt.Errorf("query: %w", model.ErrNotFound))
	_, err := service.NewCustomerService(repo).Get(context.Background(), 9)
	assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
}

func TestCustomerService_UpdateRejectsBlankName(t *testing.T) {
	repo := mocks.NewMockCustomerRepository(t)
	_, err := service.NewCustomerService(repo).Update(context.Background(), 1, " ", nil, nil)
	assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
}

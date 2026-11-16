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

func TestPaymentMethodService_SetupIntent(t *testing.T) {
	t.Run("first use creates and links the gateway customer", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("", nil)
		gw.EXPECT().CreateCustomer(mock.Anything, int64(3)).Return("cus_1", nil)
		links.EXPECT().SetStripeID(mock.Anything, int64(3), "cus_1").Return(nil)
		gw.EXPECT().CreateSetupIntent(mock.Anything, "cus_1").Return("secret", nil)
		got, err := service.NewPaymentMethodService(links, gw).SetupIntent(context.Background(), 3)
		require.NoError(t, err)
		assert.Equal(t, "secret", got)
	})

	t.Run("a linked customer is reused", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("cus_1", nil)
		gw.EXPECT().CreateSetupIntent(mock.Anything, "cus_1").Return("secret", nil)
		_, err := service.NewPaymentMethodService(links, gw).SetupIntent(context.Background(), 3)
		require.NoError(t, err)
	})

	t.Run("gateway failure is returned and nothing is linked", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		boom := errors.New("stripe down")
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("", nil)
		gw.EXPECT().CreateCustomer(mock.Anything, int64(3)).Return("", boom)
		_, err := service.NewPaymentMethodService(links, gw).SetupIntent(context.Background(), 3)
		assert.ErrorIs(t, err, boom)
	})
}

func TestPaymentMethodService_List(t *testing.T) {
	t.Run("no gateway customer means no cards and no gateway call", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("", nil)
		got, err := service.NewPaymentMethodService(links, gw).List(context.Background(), 3)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("returns the gateway cards", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		cards := []model.Card{{ID: "pm_1", Last4: "4242"}}
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("cus_1", nil)
		gw.EXPECT().ListCards(mock.Anything, "cus_1").Return(cards, nil)
		got, err := service.NewPaymentMethodService(links, gw).List(context.Background(), 3)
		require.NoError(t, err)
		assert.Equal(t, cards, got)
	})
}

func TestPaymentMethodService_Delete(t *testing.T) {
	tests := []struct {
		name     string
		stripeID string
		cardID   string
		detach   bool
		wantErr  error
	}{
		{"own card is detached", "cus_1", "pm_1", true, nil},
		{"someone else's card is not found", "cus_1", "pm_other", false, model.ErrNotFound},
		{"no gateway customer is not found", "", "pm_1", false, model.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
			links.EXPECT().StripeID(mock.Anything, int64(3)).Return(tt.stripeID, nil)
			if tt.stripeID != "" {
				gw.EXPECT().ListCards(mock.Anything, tt.stripeID).Return([]model.Card{{ID: "pm_1"}}, nil)
			}
			if tt.detach {
				gw.EXPECT().DetachCard(mock.Anything, tt.cardID).Return(nil)
			}
			err := service.NewPaymentMethodService(links, gw).Delete(context.Background(), 3, tt.cardID)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

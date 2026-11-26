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

func TestPaymentMethodService_SetupIntent(t *testing.T) {
	t.Run("first use creates and links the gateway customer", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("", nil)
		gw.EXPECT().CreateCustomer(mock.Anything, int64(3)).Return("cus_1", nil)
		links.EXPECT().SetStripeID(mock.Anything, int64(3), "cus_1").Return(nil)
		gw.EXPECT().CreateSetupIntent(mock.Anything, "cus_1").Return("secret", nil)
		got, err := service.NewPaymentMethodService(links, gw, nil).SetupIntent(context.Background(), 3)
		require.NoError(t, err)
		assert.Equal(t, "secret", got)
	})

	t.Run("a linked customer is reused", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("cus_1", nil)
		gw.EXPECT().CreateSetupIntent(mock.Anything, "cus_1").Return("secret", nil)
		_, err := service.NewPaymentMethodService(links, gw, nil).SetupIntent(context.Background(), 3)
		require.NoError(t, err)
	})

	t.Run("gateway failure is returned and nothing is linked", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		boom := errors.New("stripe down")
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("", nil)
		gw.EXPECT().CreateCustomer(mock.Anything, int64(3)).Return("", boom)
		_, err := service.NewPaymentMethodService(links, gw, nil).SetupIntent(context.Background(), 3)
		assert.ErrorIs(t, err, boom)
	})
}

func TestPaymentMethodService_List(t *testing.T) {
	t.Run("no gateway customer means no cards and no gateway call", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("", nil)
		got, err := service.NewPaymentMethodService(links, gw, nil).List(context.Background(), 3)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("returns the gateway cards", func(t *testing.T) {
		links, gw := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t)
		cards := []model.Card{{ID: "pm_1", Last4: "4242"}}
		links.EXPECT().StripeID(mock.Anything, int64(3)).Return("cus_1", nil)
		gw.EXPECT().ListCards(mock.Anything, "cus_1").Return(cards, nil)
		got, err := service.NewPaymentMethodService(links, gw, nil).List(context.Background(), 3)
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
				gw.EXPECT().ListCards(mock.Anything, tt.stripeID).Return([]model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242"}}, nil)
			}
			if tt.detach {
				gw.EXPECT().DetachCard(mock.Anything, tt.cardID).Return(nil)
			}
			err := service.NewPaymentMethodService(links, gw, nil).Delete(context.Background(), 3, tt.cardID)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPaymentMethodService_PayInvoice(t *testing.T) {
	declined := fmt.Errorf("%w: card declined", model.ErrPaymentFailed)
	invoice := func(status model.Status) model.BillingDocument {
		return model.BillingDocument{ID: 9, Status: status, TotalCents: 6500, AmountPaidCents: 1500}
	}
	tests := []struct {
		name      string
		status    model.Status
		cards     []model.Card
		chargeErr error
		bookErr   error
		wantErr   error
		charges   bool
		books     bool
	}{
		{"open invoice is charged its balance due and booked", model.StatusOpen, []model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242"}}, nil, nil, nil, true, true},
		{"partially paid invoice can be paid", model.StatusPartiallyPaid, []model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242"}}, nil, nil, nil, true, true},
		{"draft is a conflict and never charged", model.StatusDraft, nil, nil, nil, model.ErrConflict, false, false},
		{"paid is a conflict and never charged", model.StatusPaid, nil, nil, nil, model.ErrConflict, false, false},
		{"void is a conflict and never charged", model.StatusVoid, nil, nil, nil, model.ErrConflict, false, false},
		{"someone else's card is not found and never charged", model.StatusOpen, []model.Card{{ID: "pm_other"}}, nil, nil, model.ErrNotFound, false, false},
		{"a decline books nothing", model.StatusOpen, []model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242"}}, declined, nil, model.ErrPaymentFailed, true, false},
		{"a booking failure after the charge is returned", model.StatusOpen, []model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242"}}, nil, model.ErrConflict, model.ErrConflict, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links, gw, inv := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t), mocks.NewMockInvoiceBooker(t)
			inv.EXPECT().Get(mock.Anything, int64(3), model.ParentInvoice, int64(9)).Return(invoice(tt.status), nil)
			inv.EXPECT().PaymentRecorded(mock.Anything, int64(3), int64(9), "key-1").Return(false, nil)
			if tt.status == model.StatusOpen || tt.status == model.StatusPartiallyPaid {
				links.EXPECT().StripeID(mock.Anything, int64(3)).Return("cus_1", nil)
				gw.EXPECT().ListCards(mock.Anything, "cus_1").Return(tt.cards, nil)
			}
			if tt.charges {
				gw.EXPECT().Charge(mock.Anything, "cus_1", "pm_1", int64(5000), "invoice-9-key-1").Return(tt.chargeErr)
			}
			if tt.books {
				inv.EXPECT().RecordPayment(mock.Anything, int64(3), int64(9), int64(5000), "key-1", visa4242).
					Return(model.BillingDocument{ID: 9}, tt.bookErr)
			}
			_, err := service.NewPaymentMethodService(links, gw, inv).PayInvoice(context.Background(), 3, 9, "pm_1", "key-1")
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPaymentMethodService_PayInvoice_SameKeyOnTwoInvoicesChargesWithDifferentGatewayKeys(t *testing.T) {
	links, gw, inv := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t), mocks.NewMockInvoiceBooker(t)
	links.EXPECT().StripeID(mock.Anything, int64(3)).Return("cus_1", nil)
	gw.EXPECT().ListCards(mock.Anything, "cus_1").Return([]model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242"}}, nil)
	for _, id := range []int64{9, 10} {
		open := model.BillingDocument{ID: id, Status: model.StatusOpen, TotalCents: 5000}
		inv.EXPECT().Get(mock.Anything, int64(3), model.ParentInvoice, id).Return(open, nil)
		inv.EXPECT().PaymentRecorded(mock.Anything, int64(3), id, "key-1").Return(false, nil)
		gw.EXPECT().Charge(mock.Anything, "cus_1", "pm_1", int64(5000), fmt.Sprintf("invoice-%d-key-1", id)).Return(nil)
		inv.EXPECT().RecordPayment(mock.Anything, int64(3), id, int64(5000), "key-1", visa4242).Return(open, nil)
	}
	svc := service.NewPaymentMethodService(links, gw, inv)
	for _, id := range []int64{9, 10} {
		_, err := svc.PayInvoice(context.Background(), 3, id, "pm_1", "key-1")
		require.NoError(t, err)
	}
}

func TestPaymentMethodService_PayInvoice_RetryDoesNotChargeAgain(t *testing.T) {
	links, gw, inv := mocks.NewMockGatewayLinkRepository(t), mocks.NewMockPaymentGateway(t), mocks.NewMockInvoiceBooker(t)
	paid := model.BillingDocument{ID: 9, Status: model.StatusPaid, TotalCents: 6500, AmountPaidCents: 6500}
	inv.EXPECT().Get(mock.Anything, int64(3), model.ParentInvoice, int64(9)).Return(paid, nil)
	inv.EXPECT().PaymentRecorded(mock.Anything, int64(3), int64(9), "key-1").Return(true, nil)
	got, err := service.NewPaymentMethodService(links, gw, inv).PayInvoice(context.Background(), 3, 9, "pm_1", "key-1")
	require.NoError(t, err)
	assert.Equal(t, paid, got)
}

// visa4242 is how a payment with the test cards above is booked: as a card payment with the card's summary.
var visa4242 = model.PaymentSource{Method: model.MethodCard, CardBrand: "visa", CardLast4: "4242"}

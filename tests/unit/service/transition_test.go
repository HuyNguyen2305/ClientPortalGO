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
)

func TestTransition(t *testing.T) {
	t.Run("draft invoice is sent", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).
			Return(model.BillingDocument{ID: 9, Status: model.StatusDraft}, nil)
		f.docs.EXPECT().UpdateStatus(mock.Anything, int64(9), model.StatusOpen).Return(nil).Once()
		f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).
			Return(model.BillingDocument{ID: 9, Status: model.StatusOpen}, nil)
		f.docs.EXPECT().ListItems(mock.Anything, int64(9)).Return(nil, nil)

		got, err := f.svc.Transition(context.Background(), custID, model.ParentInvoice, 9, model.StatusOpen)
		require.NoError(t, err)
		assert.Equal(t, model.StatusOpen, got.Status)
	})

	t.Run("paid invoice cannot be voided", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).
			Return(model.BillingDocument{ID: 9, Status: model.StatusPaid}, nil)
		_, err := f.svc.Transition(context.Background(), custID, model.ParentInvoice, 9, model.StatusVoid)
		assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
	})

	t.Run("open estimate is accepted", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(4)).
			Return(model.BillingDocument{ID: 4, Status: model.StatusOpen}, nil)
		f.docs.EXPECT().UpdateStatus(mock.Anything, int64(4), model.StatusAccepted).Return(nil).Once()
		f.docs.EXPECT().Get(mock.Anything, custID, model.ParentEstimate, int64(4)).Return(model.BillingDocument{ID: 4}, nil)
		f.docs.EXPECT().ListItems(mock.Anything, int64(4)).Return(nil, nil)
		_, err := f.svc.Transition(context.Background(), custID, model.ParentEstimate, 4, model.StatusAccepted)
		require.NoError(t, err)
	})

	t.Run("not found passes through", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).
			Return(model.BillingDocument{}, fmt.Errorf("get: %w", model.ErrNotFound))
		_, err := f.svc.Transition(context.Background(), custID, model.ParentInvoice, 9, model.StatusOpen)
		assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
	})
}

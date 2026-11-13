package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"clientportal/internal/model"
)

func TestRecordPayment(t *testing.T) {
	tests := []struct {
		name       string
		status     model.Status
		paid       int64
		amount     int64
		wantStatus model.Status // empty means ErrConflict and no AddPayment
	}{
		{"first partial payment", model.StatusOpen, 0, 1000, model.StatusPartiallyPaid},
		{"second partial payment", model.StatusPartiallyPaid, 1000, 2000, model.StatusPartiallyPaid},
		{"final payment", model.StatusPartiallyPaid, 3000, 3500, model.StatusPaid},
		{"paid in full at once", model.StatusOpen, 0, 6500, model.StatusPaid},
		{"overpayment", model.StatusOpen, 0, 6501, ""},
		{"draft", model.StatusDraft, 0, 100, ""},
		{"void", model.StatusVoid, 0, 100, ""},
		{"already paid", model.StatusPaid, 6500, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newDocFixture(t)
			f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).
				Return(model.BillingDocument{ID: 9, ParentType: model.ParentInvoice, Status: tt.status, TotalCents: 6500, AmountPaidCents: tt.paid}, nil)
			if tt.wantStatus != "" {
				f.docs.EXPECT().AddPayment(mock.Anything, int64(9), tt.amount, tt.wantStatus, "").Return(nil).Once()
				f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(model.BillingDocument{ID: 9}, nil)
				f.docs.EXPECT().ListItems(mock.Anything, int64(9)).Return(nil, nil)
			}

			_, err := f.svc.RecordPayment(context.Background(), custID, 9, tt.amount, "")
			if tt.wantStatus == "" {
				assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRecordPayment_IdempotencyKey(t *testing.T) {
	open := model.BillingDocument{ID: 9, ParentType: model.ParentInvoice, Status: model.StatusOpen, TotalCents: 6500}
	returnsInvoice := func(f docFixture) {
		f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(model.BillingDocument{ID: 9}, nil)
		f.docs.EXPECT().ListItems(mock.Anything, int64(9)).Return(nil, nil)
	}

	t.Run("a new key records the payment with the key", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(open, nil)
		f.docs.EXPECT().HasPaymentKey(mock.Anything, int64(9), "k1").Return(false, nil)
		f.docs.EXPECT().AddPayment(mock.Anything, int64(9), int64(1000), model.StatusPartiallyPaid, "k1").Return(nil).Once()
		returnsInvoice(f)
		_, err := f.svc.RecordPayment(context.Background(), custID, 9, 1000, "k1")
		require.NoError(t, err)
	})

	t.Run("a seen key records nothing, even when the retry would now overpay", func(t *testing.T) {
		f := newDocFixture(t)
		paid := open
		paid.Status, paid.AmountPaidCents = model.StatusPaid, 6500
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(paid, nil)
		f.docs.EXPECT().HasPaymentKey(mock.Anything, int64(9), "k1").Return(true, nil)
		returnsInvoice(f) // AddPayment is not expected: the mock fails the test if it is called
		_, err := f.svc.RecordPayment(context.Background(), custID, 9, 6500, "k1")
		require.NoError(t, err)
	})

	t.Run("a lookup failure stops the payment", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(open, nil)
		f.docs.EXPECT().HasPaymentKey(mock.Anything, int64(9), "k1").Return(false, errors.New("db down"))
		_, err := f.svc.RecordPayment(context.Background(), custID, 9, 1000, "k1")
		assert.Error(t, err)
	})
}

func TestListPaymentsChecksOwnership(t *testing.T) {
	f := newDocFixture(t)
	f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(model.BillingDocument{}, model.ErrNotFound)
	_, err := f.svc.ListPayments(context.Background(), custID, 9)
	assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
}

func TestBalance(t *testing.T) {
	f := newDocFixture(t)
	loc := int64(7)
	f.docs.EXPECT().Balance(mock.Anything, custID, &loc).Return(int64(12000), nil)
	got, err := f.svc.Balance(context.Background(), custID, &loc)
	require.NoError(t, err)
	assert.Equal(t, int64(12000), got)
}

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

// fixedNow is the clock every service test runs with: 2026-11-11 15:30 UTC.
func fixedNow() time.Time { return time.Date(2026, 11, 11, 15, 30, 0, 0, time.UTC) }

func acceptedEstimate() model.BillingDocument {
	return model.BillingDocument{
		ID: 4, ParentType: model.ParentEstimate, Status: model.StatusAccepted, CustomerID: custID,
		Title: "Bi-Monthly Service", Location: model.Location{ID: 7}, TotalCents: 13300,
	}
}

func TestConvert(t *testing.T) {
	t.Run("only accepted estimates convert", func(t *testing.T) {
		for _, s := range []model.Status{model.StatusDraft, model.StatusOpen, model.StatusDeclined} {
			f := newDocFixture(t)
			est := acceptedEstimate()
			est.Status = s
			f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(4)).Return(est, nil)
			_, err := f.svc.Convert(context.Background(), custID, 4)
			assert.True(t, errors.Is(err, model.ErrConflict), "%s: got %v", s, err)
		}
	})

	t.Run("accepted estimate becomes a draft invoice", func(t *testing.T) {
		f := newDocFixture(t)
		items := []model.Item{
			{ID: 50, BillingDocumentID: 4, Name: "Service", Quantity: 2, UnitPriceCents: 6500},
			{ID: 51, BillingDocumentID: 4, Name: "Bait", Position: 1, Quantity: 3, UnitPriceCents: 100},
		}
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(4)).Return(acceptedEstimate(), nil)
		f.docs.EXPECT().ListItems(mock.Anything, int64(4)).Return(items, nil).Once()
		f.docs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool {
			return d.ParentType == model.ParentInvoice && d.Status == model.StatusDraft &&
				d.CustomerID == custID && d.Title == "Bi-Monthly Service" && d.Location.ID == 7 &&
				d.DocumentDate.Equal(time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)) &&
				d.ConvertedFromID != nil && *d.ConvertedFromID == 4 &&
				d.TotalCents == 13300 && len(d.Items) == 2 &&
				d.Items[0].ID == 0 && d.Items[1].ID == 0 && d.Items[0].Name == "Service"
		})).Return(int64(90), nil)
		f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(90)).Return(model.BillingDocument{ID: 90}, nil)
		f.docs.EXPECT().ListItems(mock.Anything, int64(90)).Return(items, nil)

		got, err := f.svc.Convert(context.Background(), custID, 4)
		require.NoError(t, err)
		assert.Equal(t, int64(90), got.ID)
	})

	t.Run("dated with the local calendar day", func(t *testing.T) {
		// 06:00 on the 12th at +07:00 is still the 11th in UTC.
		clock := func() time.Time { return time.Date(2026, 11, 12, 6, 0, 0, 0, time.FixedZone("ICT", 7*3600)) }
		docs := mocks.NewMockBillingDocumentRepository(t)
		docs.EXPECT().WithTx(mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, fn func(service.BillingDocumentRepository) error) error { return fn(docs) })
		docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(4)).Return(acceptedEstimate(), nil)
		docs.EXPECT().ListItems(mock.Anything, mock.Anything).Return(nil, nil)
		docs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool {
			return d.DocumentDate.Equal(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
		})).Return(int64(90), nil)
		docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(90)).Return(model.BillingDocument{ID: 90}, nil)

		svc := service.NewBillingDocumentService(docs, mocks.NewMockLocationRepository(t), clock)
		_, err := svc.Convert(context.Background(), custID, 4)
		require.NoError(t, err)
	})

	t.Run("already converted", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(4)).Return(acceptedEstimate(), nil)
		f.docs.EXPECT().ListItems(mock.Anything, int64(4)).Return(nil, nil)
		f.docs.EXPECT().Create(mock.Anything, mock.Anything).Return(int64(0), model.ErrConflict)
		_, err := f.svc.Convert(context.Background(), custID, 4)
		assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
	})
}

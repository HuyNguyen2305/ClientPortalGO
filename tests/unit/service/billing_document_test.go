package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"clientportal/internal/model"
	"clientportal/internal/service"
	"clientportal/tests/unit/mocks"
)

const custID = int64(3)

type docFixture struct {
	docs *mocks.MockBillingDocumentRepository
	locs *mocks.MockLocationRepository
	svc  *service.BillingDocumentService
}

func newDocFixture(t *testing.T) docFixture {
	docs := mocks.NewMockBillingDocumentRepository(t)
	locs := mocks.NewMockLocationRepository(t)
	// The transaction mock runs its callback against the same repository mock.
	docs.EXPECT().WithTx(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, fn func(service.BillingDocumentRepository) error) error { return fn(docs) }).
		Maybe()
	return docFixture{docs: docs, locs: locs, svc: service.NewBillingDocumentService(docs, locs)}
}

func twoItems() []model.Item {
	return []model.Item{{Name: "Service", Quantity: 2, UnitPriceCents: 6500}, {Name: "Bait", Quantity: 3, UnitPriceCents: 100}}
}

func validInput() model.DocumentInput {
	return model.DocumentInput{
		LocationID:   7,
		Title:        "Bi-Monthly Service",
		DocumentDate: time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC),
		Items:        twoItems(),
	}
}

func TestBillingDocumentService_Create(t *testing.T) {
	t.Run("unknown location", func(t *testing.T) {
		f := newDocFixture(t)
		f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{}, fmt.Errorf("get: %w", model.ErrNotFound))
		_, err := f.svc.Create(context.Background(), custID, model.ParentInvoice, validInput())
		assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
	})

	t.Run("blank title", func(t *testing.T) {
		f := newDocFixture(t)
		in := validInput()
		in.Title = "   "
		_, err := f.svc.Create(context.Background(), custID, model.ParentInvoice, in)
		assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
	})

	for _, pt := range []model.ParentType{model.ParentInvoice, model.ParentEstimate} {
		t.Run("valid "+string(pt), func(t *testing.T) {
			f := newDocFixture(t)
			f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7, CustomerID: custID}, nil)
			f.docs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool {
				return d.Status == model.StatusDraft && d.TotalCents == 13300 && d.ParentType == pt &&
					d.CustomerID == custID && d.Location.ID == 7 && d.Title == "Bi-Monthly Service" && len(d.Items) == 2
			})).Return(int64(40), nil)
			f.docs.EXPECT().Get(mock.Anything, custID, pt, int64(40)).Return(model.BillingDocument{ID: 40, ParentType: pt}, nil)
			f.docs.EXPECT().ListItems(mock.Anything, int64(40)).Return(twoItems(), nil)

			got, err := f.svc.Create(context.Background(), custID, pt, validInput())
			require.NoError(t, err)
			assert.Equal(t, int64(40), got.ID)
			assert.Len(t, got.Items, 2)
		})
	}
}

func TestBillingDocumentService_Get(t *testing.T) {
	t.Run("returns the items", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(model.BillingDocument{ID: 9}, nil)
		f.docs.EXPECT().ListItems(mock.Anything, int64(9)).Return(twoItems(), nil)
		got, err := f.svc.Get(context.Background(), custID, model.ParentInvoice, 9)
		require.NoError(t, err)
		assert.Equal(t, twoItems(), got.Items)
	})

	t.Run("document lookup fails", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(model.BillingDocument{}, model.ErrNotFound)
		f.docs.EXPECT().ListItems(mock.Anything, int64(9)).Return(nil, nil).Maybe()
		_, err := f.svc.Get(context.Background(), custID, model.ParentInvoice, 9)
		assert.True(t, errors.Is(err, model.ErrNotFound), "got %v", err)
	})

	t.Run("item lookup fails", func(t *testing.T) {
		f := newDocFixture(t)
		boom := errors.New("boom")
		f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(model.BillingDocument{ID: 9}, nil).Maybe()
		f.docs.EXPECT().ListItems(mock.Anything, int64(9)).Return(nil, boom)
		_, err := f.svc.Get(context.Background(), custID, model.ParentInvoice, 9)
		assert.True(t, errors.Is(err, boom), "got %v", err)
	})
}

func TestBillingDocumentService_UpdateAndDeleteNeedDraft(t *testing.T) {
	for _, status := range []model.Status{model.StatusOpen, model.StatusDraft} {
		t.Run("update "+string(status), func(t *testing.T) {
			f := newDocFixture(t)
			f.locs.EXPECT().Get(mock.Anything, custID, int64(7)).Return(model.Location{ID: 7}, nil)
			f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentInvoice, int64(9)).
				Return(model.BillingDocument{ID: 9, Status: status}, nil)
			if status == model.StatusDraft {
				f.docs.EXPECT().Update(mock.Anything, mock.MatchedBy(func(d model.BillingDocument) bool {
					return d.ID == 9 && d.TotalCents == 13300 && d.Location.ID == 7
				})).Return(nil).Once()
				f.docs.EXPECT().Get(mock.Anything, custID, model.ParentInvoice, int64(9)).Return(model.BillingDocument{ID: 9}, nil)
				f.docs.EXPECT().ListItems(mock.Anything, int64(9)).Return(twoItems(), nil)
			}
			_, err := f.svc.Update(context.Background(), custID, model.ParentInvoice, 9, validInput())
			if status == model.StatusOpen {
				assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
				return
			}
			require.NoError(t, err)
		})

		t.Run("delete "+string(status), func(t *testing.T) {
			f := newDocFixture(t)
			f.docs.EXPECT().GetForUpdate(mock.Anything, custID, model.ParentEstimate, int64(9)).
				Return(model.BillingDocument{ID: 9, Status: status}, nil)
			if status == model.StatusDraft {
				f.docs.EXPECT().Delete(mock.Anything, int64(9)).Return(nil).Once()
			}
			err := f.svc.Delete(context.Background(), custID, model.ParentEstimate, 9)
			if status == model.StatusOpen {
				assert.True(t, errors.Is(err, model.ErrConflict), "got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestBillingDocumentService_List(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 12, d, 0, 0, 0, 0, time.UTC) }
	rows := []model.BillingDocument{{ID: 3, DocumentDate: day(10)}, {ID: 2, DocumentDate: day(9)}, {ID: 1, DocumentDate: day(8)}}

	t.Run("status of the other type", func(t *testing.T) {
		f := newDocFixture(t)
		s := model.StatusAccepted
		_, _, err := f.svc.List(context.Background(), model.DocumentFilter{CustomerID: custID, ParentType: model.ParentInvoice, Status: &s, Limit: 2})
		assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
	})

	t.Run("more rows than the page", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().List(mock.Anything, mock.MatchedBy(func(fl model.DocumentFilter) bool { return fl.Limit == 3 })).Return(rows, nil)
		got, next, err := f.svc.List(context.Background(), model.DocumentFilter{CustomerID: custID, ParentType: model.ParentInvoice, Limit: 2})
		require.NoError(t, err)
		assert.Len(t, got, 2)
		require.NotNil(t, next)
		assert.Equal(t, model.DocumentCursor{Date: day(9), ID: 2}, *next)
	})

	t.Run("last page", func(t *testing.T) {
		f := newDocFixture(t)
		f.docs.EXPECT().List(mock.Anything, mock.Anything).Return(rows[:2], nil)
		got, next, err := f.svc.List(context.Background(), model.DocumentFilter{CustomerID: custID, ParentType: model.ParentInvoice, Limit: 2})
		require.NoError(t, err)
		assert.Len(t, got, 2)
		assert.Nil(t, next)
	})
}

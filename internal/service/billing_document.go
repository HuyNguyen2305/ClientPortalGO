package service

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"clientportal/internal/model"
)

// BillingDocumentRepository is the storage for invoices and estimates. Lookups are scoped
// by customer and parent type; methods taking only an ID run after such a lookup.
type BillingDocumentRepository interface {
	// WithTx runs fn in one transaction, with a repository bound to it.
	WithTx(ctx context.Context, fn func(BillingDocumentRepository) error) error
	// Create inserts the document and its items; the number comes from the type's sequence.
	Create(ctx context.Context, d model.BillingDocument) (int64, error)
	// Get returns the document with its location, without items.
	Get(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error)
	// GetForUpdate is Get with the document row locked until the transaction ends.
	GetForUpdate(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error)
	ListItems(ctx context.Context, documentID int64) ([]model.Item, error)
	// List returns documents with their locations, without items, newest first.
	List(ctx context.Context, f model.DocumentFilter) ([]model.BillingDocument, error)
	// Update replaces title, date, location, total and items.
	Update(ctx context.Context, d model.BillingDocument) error
	Delete(ctx context.Context, id int64) error
	UpdateStatus(ctx context.Context, id int64, status model.Status) error
	// AddPayment records a payment and moves amount_paid_cents and status with it. An empty
	// idempotencyKey stores none.
	AddPayment(ctx context.Context, documentID, amountCents int64, newStatus model.Status, idempotencyKey string) error
	// HasPaymentKey reports whether the invoice already has a payment recorded under the key.
	HasPaymentKey(ctx context.Context, documentID int64, key string) (bool, error)
	ListPayments(ctx context.Context, documentID int64) ([]model.Payment, error)
	// Balance sums the balance due over the customer's open and partially paid invoices.
	Balance(ctx context.Context, customerID int64, locationID *int64) (int64, error)

	// Bookings live here because each is written in the same transaction as its estimate.

	// CreateBooking inserts a booking; an overlap with a live booking is ErrConflict.
	CreateBooking(ctx context.Context, b model.Booking) (int64, error)
	// CancelBookingForEstimate cancels the live booking that created the estimate, if any.
	CancelBookingForEstimate(ctx context.Context, estimateID int64) error
	// BusyIntervals returns every live booking overlapping [from, to), for any customer.
	BusyIntervals(ctx context.Context, from, to time.Time) ([]model.Interval, error)
	GetBooking(ctx context.Context, customerID, id int64) (model.Booking, error)
	// HasConvertedInvoice reports whether an invoice was created from the estimate.
	HasConvertedInvoice(ctx context.Context, estimateID int64) (bool, error)
	// ListUpcoming returns the customer's live bookings that end after now, soonest first.
	ListUpcoming(ctx context.Context, customerID int64, now time.Time) ([]model.Booking, error)
}

// BillingDocumentService manages invoices and estimates.
type BillingDocumentService struct {
	docs BillingDocumentRepository
	locs LocationRepository
	now  func() time.Time
}

// NewBillingDocumentService returns a billing document service; now dates converted invoices.
func NewBillingDocumentService(docs BillingDocumentRepository, locs LocationRepository, now func() time.Time) *BillingDocumentService {
	return &BillingDocumentService{docs: docs, locs: locs, now: now}
}

// checkInput trims the title and confirms the location belongs to the customer.
func (s *BillingDocumentService) checkInput(ctx context.Context, customerID int64, in model.DocumentInput) (model.DocumentInput, error) {
	title, err := required("title", in.Title)
	if err != nil {
		return in, err
	}
	in.Title = title
	if _, err := s.locs.Get(ctx, customerID, in.LocationID); err != nil {
		return in, fmt.Errorf("location: %w", err)
	}
	return in, nil
}

// Create adds a draft document with its items.
func (s *BillingDocumentService) Create(ctx context.Context, customerID int64, pt model.ParentType, in model.DocumentInput) (model.BillingDocument, error) {
	in, err := s.checkInput(ctx, customerID, in)
	if err != nil {
		return model.BillingDocument{}, err
	}
	var id int64
	err = s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		var err error
		id, err = tx.Create(ctx, model.BillingDocument{
			ParentType:   pt,
			CustomerID:   customerID,
			Location:     model.Location{ID: in.LocationID},
			Title:        in.Title,
			Status:       model.StatusDraft,
			DocumentDate: in.DocumentDate,
			TotalCents:   model.SumItems(in.Items),
			Items:        in.Items,
		})
		return err
	})
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("create %s: %w", pt, err)
	}
	return s.Get(ctx, customerID, pt, id)
}

// Get returns a document with its items. The document and its items are fetched concurrently.
func (s *BillingDocumentService) Get(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error) {
	var doc model.BillingDocument
	var items []model.Item
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		doc, err = s.docs.Get(gctx, customerID, pt, id)
		return err
	})
	g.Go(func() error {
		var err error
		items, err = s.docs.ListItems(gctx, id)
		return err
	})
	if err := g.Wait(); err != nil {
		return model.BillingDocument{}, fmt.Errorf("get %s: %w", pt, err)
	}
	doc.Items = items
	return doc, nil
}

// List returns one page of documents and the cursor of the next page, nil on the last one.
func (s *BillingDocumentService) List(ctx context.Context, f model.DocumentFilter) ([]model.BillingDocument, *model.DocumentCursor, error) {
	if f.Status != nil && !model.ValidStatus(f.ParentType, *f.Status) {
		return nil, nil, fmt.Errorf("%w: unknown %s status %q", model.ErrValidation, f.ParentType, *f.Status)
	}
	limit := f.Limit
	if limit < 1 {
		return nil, nil, fmt.Errorf("%w: limit must be at least 1", model.ErrValidation)
	}
	f.Limit = limit + 1
	docs, err := s.docs.List(ctx, f)
	if err != nil {
		return nil, nil, fmt.Errorf("list %s: %w", f.ParentType, err)
	}
	if len(docs) <= limit {
		return docs, nil, nil
	}
	docs = docs[:limit]
	last := docs[limit-1]
	return docs, &model.DocumentCursor{Date: last.DocumentDate, ID: last.ID}, nil
}

// lockDraft locks the document and fails with ErrConflict unless it is a draft.
func lockDraft(ctx context.Context, tx BillingDocumentRepository, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error) {
	doc, err := tx.GetForUpdate(ctx, customerID, pt, id)
	if err != nil {
		return doc, err
	}
	if doc.Status != model.StatusDraft {
		return doc, fmt.Errorf("%w: %s %d is %s; only drafts can change", model.ErrConflict, pt, doc.Number, doc.Status)
	}
	return doc, nil
}

// Update replaces a draft's title, date, location and items.
func (s *BillingDocumentService) Update(ctx context.Context, customerID int64, pt model.ParentType, id int64, in model.DocumentInput) (model.BillingDocument, error) {
	in, err := s.checkInput(ctx, customerID, in)
	if err != nil {
		return model.BillingDocument{}, err
	}
	err = s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		doc, err := lockDraft(ctx, tx, customerID, pt, id)
		if err != nil {
			return err
		}
		doc.Location = model.Location{ID: in.LocationID}
		doc.Title, doc.DocumentDate, doc.Items = in.Title, in.DocumentDate, in.Items
		doc.TotalCents = model.SumItems(in.Items)
		return tx.Update(ctx, doc)
	})
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("update %s: %w", pt, err)
	}
	return s.Get(ctx, customerID, pt, id)
}

// Transition moves a document to status `to` when the transition table allows it.
func (s *BillingDocumentService) Transition(ctx context.Context, customerID int64, pt model.ParentType, id int64, to model.Status) (model.BillingDocument, error) {
	err := s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		doc, err := tx.GetForUpdate(ctx, customerID, pt, id)
		if err != nil {
			return err
		}
		if !model.CanTransition(pt, doc.Status, to) {
			return fmt.Errorf("%w: %s %d cannot move from %s to %s", model.ErrConflict, pt, doc.Number, doc.Status, to)
		}
		// A $0 invoice could never be paid, so it would sit open until voided.
		if pt == model.ParentInvoice && to == model.StatusOpen && doc.TotalCents == 0 {
			return fmt.Errorf("%w: invoice %d has a $0 total and cannot be sent", model.ErrConflict, doc.Number)
		}
		if err := tx.UpdateStatus(ctx, id, to); err != nil {
			return err
		}
		// Declining the estimate a booking created cancels the visit and frees its slot.
		if pt == model.ParentEstimate && to == model.StatusDeclined {
			return tx.CancelBookingForEstimate(ctx, id)
		}
		return nil
	})
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("%s %s: %w", to, pt, err)
	}
	return s.Get(ctx, customerID, pt, id)
}

// RecordPayment applies a payment to an open or partially paid invoice. The invoice row is
// locked for the whole transaction, so two concurrent payments cannot overpay it.
// A non-empty idempotencyKey makes a retry a no-op: the invoice comes back unchanged, whatever
// amount the retry carried.
func (s *BillingDocumentService) RecordPayment(ctx context.Context, customerID, invoiceID, amountCents int64, idempotencyKey string) (model.BillingDocument, error) {
	err := s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		doc, err := tx.GetForUpdate(ctx, customerID, model.ParentInvoice, invoiceID)
		if err != nil {
			return err
		}
		if idempotencyKey != "" {
			seen, err := tx.HasPaymentKey(ctx, invoiceID, idempotencyKey)
			if err != nil || seen {
				return err
			}
		}
		if amountCents > doc.BalanceDueCents() {
			return fmt.Errorf("%w: payment of %d exceeds the balance due of %d", model.ErrConflict, amountCents, doc.BalanceDueCents())
		}
		next := model.StatusAfterPayment(doc.TotalCents, doc.AmountPaidCents+amountCents)
		if !model.CanTransition(model.ParentInvoice, doc.Status, next) {
			return fmt.Errorf("%w: a %s invoice cannot take payments", model.ErrConflict, doc.Status)
		}
		return tx.AddPayment(ctx, invoiceID, amountCents, next, idempotencyKey)
	})
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("record payment: %w", err)
	}
	return s.Get(ctx, customerID, model.ParentInvoice, invoiceID)
}

// ListPayments returns the payments on one of the customer's invoices.
func (s *BillingDocumentService) ListPayments(ctx context.Context, customerID, invoiceID int64) ([]model.Payment, error) {
	if _, err := s.docs.Get(ctx, customerID, model.ParentInvoice, invoiceID); err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	ps, err := s.docs.ListPayments(ctx, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	return ps, nil
}

// Balance is what the customer owes, optionally for one location.
func (s *BillingDocumentService) Balance(ctx context.Context, customerID int64, locationID *int64) (int64, error) {
	b, err := s.docs.Balance(ctx, customerID, locationID)
	if err != nil {
		return 0, fmt.Errorf("balance: %w", err)
	}
	return b, nil
}

// Convert turns an accepted estimate into a new draft invoice with the same title, location
// and items, dated today. An estimate converts once; a second attempt is ErrConflict.
func (s *BillingDocumentService) Convert(ctx context.Context, customerID, estimateID int64) (model.BillingDocument, error) {
	var invoiceID int64
	err := s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		est, err := tx.GetForUpdate(ctx, customerID, model.ParentEstimate, estimateID)
		if err != nil {
			return err
		}
		if est.Status != model.StatusAccepted {
			return fmt.Errorf("%w: estimate %d is %s; only accepted estimates convert", model.ErrConflict, est.Number, est.Status)
		}
		items, err := tx.ListItems(ctx, estimateID)
		if err != nil {
			return err
		}
		for i := range items {
			items[i].ID, items[i].BillingDocumentID = 0, 0
		}
		n := s.now() // the clock's own calendar day (the app's clock is business-local), not the UTC one
		today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
		invoiceID, err = tx.Create(ctx, model.BillingDocument{
			ParentType:      model.ParentInvoice,
			CustomerID:      customerID,
			Location:        model.Location{ID: est.Location.ID},
			Title:           est.Title,
			Status:          model.StatusDraft,
			DocumentDate:    today,
			TotalCents:      model.SumItems(items),
			ConvertedFromID: &est.ID,
			Items:           items,
		})
		return err
	})
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("convert estimate: %w", err)
	}
	return s.Get(ctx, customerID, model.ParentInvoice, invoiceID)
}

// Delete removes a draft.
func (s *BillingDocumentService) Delete(ctx context.Context, customerID int64, pt model.ParentType, id int64) error {
	err := s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		if _, err := lockDraft(ctx, tx, customerID, pt, id); err != nil {
			return err
		}
		return tx.Delete(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("delete %s: %w", pt, err)
	}
	return nil
}

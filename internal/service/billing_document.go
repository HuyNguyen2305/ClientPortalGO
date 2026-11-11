package service

import (
	"context"
	"fmt"

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
}

// BillingDocumentService manages invoices and estimates.
type BillingDocumentService struct {
	docs BillingDocumentRepository
	locs LocationRepository
}

// NewBillingDocumentService returns a billing document service.
func NewBillingDocumentService(docs BillingDocumentRepository, locs LocationRepository) *BillingDocumentService {
	return &BillingDocumentService{docs: docs, locs: locs}
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

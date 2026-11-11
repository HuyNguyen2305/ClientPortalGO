package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"clientportal/internal/model"
	"clientportal/internal/repository/db"
)

// numberSequences maps each parent type to the sequence its display numbers come from.
var numberSequences = map[model.ParentType]string{
	model.ParentInvoice:  "invoice_number_seq",
	model.ParentEstimate: "estimate_number_seq",
}

const documentSelect = `
SELECT d.id, d.parent_type, d.number, d.customer_id, d.title, d.status, d.document_date,
       d.total_cents, d.amount_paid_cents, d.converted_from_id, d.created_at, d.updated_at,
       l.id, l.customer_id, l.street, l.city, l.state, l.postal_code, l.created_at, l.updated_at
FROM billing_documents d
JOIN locations l ON l.id = d.location_id`

// BillingDocuments stores invoices and estimates and their line items.
type BillingDocuments struct {
	pool    *pgxpool.Pool // nil when bound to a transaction
	db      db.DBTX
	timeout time.Duration
}

// NewBillingDocuments returns a repository on the pool.
func NewBillingDocuments(pool *pgxpool.Pool, timeout time.Duration) *BillingDocuments {
	return &BillingDocuments{pool: pool, db: pool, timeout: timeout}
}

func scanDocument(row pgx.Row) (model.BillingDocument, error) {
	var d model.BillingDocument
	var pt, status string
	l := &d.Location
	err := row.Scan(&d.ID, &pt, &d.Number, &d.CustomerID, &d.Title, &status, &d.DocumentDate,
		&d.TotalCents, &d.AmountPaidCents, &d.ConvertedFromID, &d.CreatedAt, &d.UpdatedAt,
		&l.ID, &l.CustomerID, &l.Street, &l.City, &l.State, &l.PostalCode, &l.CreatedAt, &l.UpdatedAt)
	d.ParentType, d.Status = model.ParentType(pt), model.Status(status)
	return d, err
}

// Create inserts the document and its items. Call it inside WithTx.
func (r *BillingDocuments) Create(ctx context.Context, d model.BillingDocument) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	seq, ok := numberSequences[d.ParentType]
	if !ok {
		return 0, fmt.Errorf("insert document: unknown parent type %q", d.ParentType)
	}
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO billing_documents
		   (parent_type, number, customer_id, location_id, title, status, document_date, total_cents, converted_from_id)
		 VALUES ($1, nextval($2::regclass), $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id`,
		string(d.ParentType), seq, d.CustomerID, d.Location.ID, d.Title, string(d.Status),
		d.DocumentDate, d.TotalCents, d.ConvertedFromID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert document: %w", mapErr(err))
	}
	if err := r.insertItems(ctx, id, d.Items); err != nil {
		return 0, err
	}
	return id, nil
}

// insertItems writes all items in one statement; position is the slice index.
func (r *BillingDocuments) insertItems(ctx context.Context, documentID int64, items []model.Item) error {
	n := len(items)
	positions, quantities := make([]int32, n), make([]int32, n)
	names, descriptions := make([]string, n), make([]*string, n)
	prices := make([]int64, n)
	for i, it := range items {
		positions[i], names[i], descriptions[i] = int32(i), it.Name, it.Description
		quantities[i], prices[i] = int32(it.Quantity), it.UnitPriceCents
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO billing_document_items
		   (billing_document_id, position, name, description, quantity, unit_price_cents)
		 SELECT $1, * FROM unnest($2::int[], $3::text[], $4::text[], $5::int[], $6::bigint[])`,
		documentID, positions, names, descriptions, quantities, prices)
	if err != nil {
		return fmt.Errorf("insert items: %w", mapErr(err))
	}
	return nil
}

func (r *BillingDocuments) get(ctx context.Context, customerID int64, pt model.ParentType, id int64, lock string) (model.BillingDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	d, err := scanDocument(r.db.QueryRow(ctx,
		documentSelect+` WHERE d.id = $1 AND d.customer_id = $2 AND d.parent_type = $3`+lock,
		id, customerID, string(pt)))
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("get %s %d: %w", pt, id, mapErr(err))
	}
	return d, nil
}

// Get returns one of the customer's documents of the given type, without items.
func (r *BillingDocuments) Get(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error) {
	return r.get(ctx, customerID, pt, id, "")
}

// GetForUpdate is Get with the document row locked until the transaction ends. Call it inside WithTx.
func (r *BillingDocuments) GetForUpdate(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error) {
	return r.get(ctx, customerID, pt, id, ` FOR UPDATE OF d`)
}

// ListItems returns a document's items in display order.
func (r *BillingDocuments) ListItems(ctx context.Context, documentID int64) ([]model.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx,
		`SELECT id, billing_document_id, position, name, description, quantity, unit_price_cents
		 FROM billing_document_items WHERE billing_document_id = $1 ORDER BY position`, documentID)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Item, error) {
		var it model.Item
		err := row.Scan(&it.ID, &it.BillingDocumentID, &it.Position, &it.Name, &it.Description, &it.Quantity, &it.UnitPriceCents)
		return it, err
	})
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	return items, nil
}

// List returns one page of documents, newest first, with their locations and without items.
func (r *BillingDocuments) List(ctx context.Context, f model.DocumentFilter) ([]model.BillingDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var status *string
	if f.Status != nil {
		s := string(*f.Status)
		status = &s
	}
	var afterDate *time.Time
	var afterID *int64
	if f.After != nil {
		afterDate, afterID = &f.After.Date, &f.After.ID
	}
	rows, err := r.db.Query(ctx, documentSelect+`
		WHERE d.customer_id = $1 AND d.parent_type = $2
		  AND ($3::bigint IS NULL OR d.location_id = $3)
		  AND ($4::text IS NULL OR d.status = $4)
		  AND ($5::date IS NULL OR (d.document_date, d.id) < ($5::date, $6::bigint))
		ORDER BY d.document_date DESC, d.id DESC
		LIMIT $7`,
		f.CustomerID, string(f.ParentType), f.LocationID, status, afterDate, afterID, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	docs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.BillingDocument, error) {
		return scanDocument(row)
	})
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return docs, nil
}

// Update replaces title, date, location, total and items. Call it inside WithTx.
func (r *BillingDocuments) Update(ctx context.Context, d model.BillingDocument) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx,
		`UPDATE billing_documents
		 SET location_id = $2, title = $3, document_date = $4, total_cents = $5, updated_at = now()
		 WHERE id = $1`,
		d.ID, d.Location.ID, d.Title, d.DocumentDate, d.TotalCents); err != nil {
		return fmt.Errorf("update document %d: %w", d.ID, mapErr(err))
	}
	if _, err := r.db.Exec(ctx, `DELETE FROM billing_document_items WHERE billing_document_id = $1`, d.ID); err != nil {
		return fmt.Errorf("replace items: %w", err)
	}
	return r.insertItems(ctx, d.ID, d.Items)
}

// UpdateStatus sets a document's status. Call it inside WithTx after GetForUpdate.
func (r *BillingDocuments) UpdateStatus(ctx context.Context, id int64, status model.Status) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx,
		`UPDATE billing_documents SET status = $2, updated_at = now() WHERE id = $1`, id, string(status)); err != nil {
		return fmt.Errorf("update status of %d: %w", id, mapErr(err))
	}
	return nil
}

// Delete removes a document; its items go with it.
func (r *BillingDocuments) Delete(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx, `DELETE FROM billing_documents WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete document %d: %w", id, mapErr(err))
	}
	return nil
}

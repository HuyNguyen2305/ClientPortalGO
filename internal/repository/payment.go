package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
)

// AddPayment records a payment and updates the invoice's paid amount and status.
// Call it inside WithTx after GetForUpdate, so concurrent payments are serialised.
func (r *BillingDocuments) AddPayment(ctx context.Context, documentID, amountCents int64, newStatus model.Status) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx,
		`INSERT INTO payments (billing_document_id, amount_cents) VALUES ($1, $2)`, documentID, amountCents); err != nil {
		return fmt.Errorf("insert payment: %w", mapErr(err))
	}
	if _, err := r.db.Exec(ctx,
		`UPDATE billing_documents
		 SET amount_paid_cents = amount_paid_cents + $2, status = $3, updated_at = now()
		 WHERE id = $1`,
		documentID, amountCents, string(newStatus)); err != nil {
		return fmt.Errorf("apply payment: %w", mapErr(err))
	}
	return nil
}

// ListPayments returns an invoice's payments, oldest first.
func (r *BillingDocuments) ListPayments(ctx context.Context, documentID int64) ([]model.Payment, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx,
		`SELECT id, billing_document_id, amount_cents, paid_at
		 FROM payments WHERE billing_document_id = $1 ORDER BY paid_at, id`, documentID)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Payment, error) {
		var p model.Payment
		err := row.Scan(&p.ID, &p.BillingDocumentID, &p.AmountCents, &p.PaidAt)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	return out, nil
}

// Balance sums the balance due over the customer's open and partially paid invoices.
func (r *BillingDocuments) Balance(ctx context.Context, customerID int64, locationID *int64) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var cents int64
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(total_cents - amount_paid_cents), 0)::bigint
		 FROM billing_documents
		 WHERE customer_id = $1 AND parent_type = 'invoice' AND status IN ('open', 'partially_paid')
		   AND ($2::bigint IS NULL OR location_id = $2)`,
		customerID, locationID).Scan(&cents)
	if err != nil {
		return 0, fmt.Errorf("balance: %w", err)
	}
	return cents, nil
}

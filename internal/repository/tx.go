package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// WithTx runs fn with a copy of the repository bound to one transaction. It commits when fn
// returns nil and rolls back on an error or a panic. On a copy already bound to a
// transaction it just calls fn, so nested use joins the outer transaction.
func (r *BillingDocuments) WithTx(ctx context.Context, fn func(*BillingDocuments) error) (err error) {
	if r.pool == nil {
		return fn(r)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(&BillingDocuments{db: tx, timeout: r.timeout}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", mapErr(err))
	}
	return nil
}

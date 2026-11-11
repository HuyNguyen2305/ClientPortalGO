package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"clientportal/internal/model"
	"clientportal/internal/repository/db"
)

const customerColumns = `id, account_number, name, email, phone, created_at, updated_at`

// Customers stores customers.
type Customers struct {
	db      db.DBTX
	timeout time.Duration
}

// NewCustomers returns a customer repository.
func NewCustomers(db db.DBTX, timeout time.Duration) *Customers {
	return &Customers{db: db, timeout: timeout}
}

func scanCustomer(row pgx.Row) (model.Customer, error) {
	var c model.Customer
	err := row.Scan(&c.ID, &c.AccountNumber, &c.Name, &c.Email, &c.Phone, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// Create inserts a customer; the account number comes from its sequence.
func (r *Customers) Create(ctx context.Context, c model.Customer) (model.Customer, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanCustomer(r.db.QueryRow(ctx,
		`INSERT INTO customers (name, email, phone) VALUES ($1, $2, $3) RETURNING `+customerColumns,
		c.Name, c.Email, c.Phone))
	if err != nil {
		return model.Customer{}, fmt.Errorf("insert customer: %w", mapErr(err))
	}
	return out, nil
}

// Get returns one customer.
func (r *Customers) Get(ctx context.Context, id int64) (model.Customer, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	c, err := scanCustomer(r.db.QueryRow(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = $1`, id))
	if err != nil {
		return model.Customer{}, fmt.Errorf("get customer %d: %w", id, mapErr(err))
	}
	return c, nil
}

// List returns up to limit customers with an ID above afterID, in ID order.
func (r *Customers) List(ctx context.Context, afterID int64, limit int) ([]model.Customer, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.Query(ctx,
		`SELECT `+customerColumns+` FROM customers WHERE id > $1 ORDER BY id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Customer, error) {
		return scanCustomer(row)
	})
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	return out, nil
}

// Update replaces a customer's name, email and phone.
func (r *Customers) Update(ctx context.Context, c model.Customer) (model.Customer, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanCustomer(r.db.QueryRow(ctx,
		`UPDATE customers SET name = $2, email = $3, phone = $4, updated_at = now()
		 WHERE id = $1 RETURNING `+customerColumns,
		c.ID, c.Name, c.Email, c.Phone))
	if err != nil {
		return model.Customer{}, fmt.Errorf("update customer %d: %w", c.ID, mapErr(err))
	}
	return out, nil
}

// Delete removes a customer; it fails with ErrConflict while anything references it.
func (r *Customers) Delete(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.db.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete customer %d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete customer %d: %w", id, model.ErrNotFound)
	}
	return nil
}

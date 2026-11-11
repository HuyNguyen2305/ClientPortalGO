//go:build integration

package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/tests/integration/testdb"
)

func pgCode(t *testing.T, err error) string {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected a Postgres error, got %v", err)
	return pgErr.Code
}

// seed inserts one customer and one location and returns their IDs.
func seed(t *testing.T, pool *pgxpool.Pool) (customerID, locationID int64) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO customers (name) VALUES ('William Saliba') RETURNING id`).Scan(&customerID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO locations (customer_id, street, city, state, postal_code)
		 VALUES ($1, 'West Loop 1604 South', 'San Antonio', 'TX', '12341') RETURNING id`,
		customerID).Scan(&locationID))
	return customerID, locationID
}

func insertDoc(pool *pgxpool.Pool, customerID, locationID int64, parentType, status string, number int64, convertedFrom *int64) (int64, error) {
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO billing_documents
		   (parent_type, number, customer_id, location_id, title, status, document_date, converted_from_id)
		 VALUES ($1, $2, $3, $4, 'Bi-Monthly Service', $5, '2026-12-10', $6) RETURNING id`,
		parentType, number, customerID, locationID, status, convertedFrom).Scan(&id)
	return id, err
}

func TestMigrations(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	cust, loc := seed(t, pool)

	t.Run("valid rows", func(t *testing.T) {
		_, err := insertDoc(pool, cust, loc, "invoice", "paid", 1, nil)
		require.NoError(t, err)
		_, err = insertDoc(pool, cust, loc, "estimate", "accepted", 1, nil)
		require.NoError(t, err)
	})

	t.Run("status of the wrong type", func(t *testing.T) {
		_, err := insertDoc(pool, cust, loc, "invoice", "accepted", 2, nil)
		assert.Equal(t, "23514", pgCode(t, err))
		_, err = insertDoc(pool, cust, loc, "estimate", "paid", 2, nil)
		assert.Equal(t, "23514", pgCode(t, err))
	})

	t.Run("unknown parent type", func(t *testing.T) {
		_, err := insertDoc(pool, cust, loc, "quote", "draft", 3, nil)
		assert.Equal(t, "23514", pgCode(t, err))
	})

	t.Run("duplicate number", func(t *testing.T) {
		_, err := insertDoc(pool, cust, loc, "invoice", "draft", 90000, nil)
		require.NoError(t, err)
		_, err = insertDoc(pool, cust, loc, "invoice", "draft", 90000, nil)
		assert.Equal(t, "23505", pgCode(t, err))
	})

	t.Run("same number across types", func(t *testing.T) {
		_, err := insertDoc(pool, cust, loc, "invoice", "draft", 90001, nil)
		require.NoError(t, err)
		_, err = insertDoc(pool, cust, loc, "estimate", "draft", 90001, nil)
		require.NoError(t, err)
	})

	t.Run("second conversion", func(t *testing.T) {
		est, err := insertDoc(pool, cust, loc, "estimate", "accepted", 90002, nil)
		require.NoError(t, err)
		_, err = insertDoc(pool, cust, loc, "invoice", "draft", 90003, &est)
		require.NoError(t, err)
		_, err = insertDoc(pool, cust, loc, "invoice", "draft", 90004, &est)
		assert.Equal(t, "23505", pgCode(t, err))
	})

	t.Run("bad item", func(t *testing.T) {
		doc, err := insertDoc(pool, cust, loc, "invoice", "draft", 90005, nil)
		require.NoError(t, err)
		ins := `INSERT INTO billing_document_items (billing_document_id, position, name, quantity, unit_price_cents)
		        VALUES ($1, 0, 'x', $2, $3)`
		_, err = pool.Exec(ctx, ins, doc, 0, 100)
		assert.Equal(t, "23514", pgCode(t, err))
		_, err = pool.Exec(ctx, ins, doc, 1, -1)
		assert.Equal(t, "23514", pgCode(t, err))
	})

	t.Run("bad payment", func(t *testing.T) {
		doc, err := insertDoc(pool, cust, loc, "invoice", "open", 90006, nil)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO payments (billing_document_id, amount_cents) VALUES ($1, 0)`, doc)
		assert.Equal(t, "23514", pgCode(t, err))
	})

	t.Run("item cascade", func(t *testing.T) {
		doc, err := insertDoc(pool, cust, loc, "invoice", "draft", 90007, nil)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO billing_document_items (billing_document_id, position, name, quantity, unit_price_cents)
		                         VALUES ($1, 0, 'x', 1, 100)`, doc)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `DELETE FROM billing_documents WHERE id = $1`, doc)
		require.NoError(t, err)
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM billing_document_items WHERE billing_document_id = $1`, doc).Scan(&n))
		assert.Zero(t, n)
	})

	t.Run("sequences start where the spec says", func(t *testing.T) {
		var account int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT account_number FROM customers WHERE id = $1`, cust).Scan(&account))
		assert.Equal(t, int64(5000), account, "first customer's account number")
		for seq, want := range map[string]int64{
			"invoice_number_seq":  85000,
			"estimate_number_seq": 5000,
		} {
			var got int64
			require.NoError(t, pool.QueryRow(ctx, `SELECT nextval($1)`, seq).Scan(&got))
			assert.Equal(t, want, got, seq)
		}
	})

	t.Run("down", func(t *testing.T) {
		testdb.MigrateDown(t, pool)
		var reg *string
		require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('customers')::text`).Scan(&reg))
		assert.Nil(t, reg)
		require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('invoice_number_seq')::text`).Scan(&reg))
		assert.Nil(t, reg)
	})
}

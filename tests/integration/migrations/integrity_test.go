//go:build integration

package migrations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/tests/integration/testdb"
)

func TestIntegrityConstraints(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	cust, loc := seed(t, pool)

	var other, otherLoc int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO customers (name) VALUES ('B') RETURNING id`).Scan(&other))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO locations (customer_id, street, city, state, postal_code) VALUES ($1, 's', 'c', 'TX', '1') RETURNING id`,
		other).Scan(&otherLoc))

	t.Run("money columns", func(t *testing.T) {
		ins := `INSERT INTO billing_documents
		          (parent_type, number, customer_id, location_id, title, status, document_date, total_cents, amount_paid_cents)
		        VALUES ('invoice', $1, $2, $3, 't', 'open', '2026-12-10', $4, $5)`
		_, err := pool.Exec(ctx, ins, 1, cust, loc, 100, 100)
		require.NoError(t, err, "paid in full is fine")
		_, err = pool.Exec(ctx, ins, 2, cust, loc, 100, 101)
		assert.Equal(t, "23514", pgCode(t, err), "paid beyond the total")
		_, err = pool.Exec(ctx, ins, 3, cust, loc, -1, 0)
		assert.Equal(t, "23514", pgCode(t, err), "negative total")
		_, err = pool.Exec(ctx, ins, 4, cust, loc, 100, -1)
		assert.Equal(t, "23514", pgCode(t, err), "negative paid amount")
	})

	t.Run("a document needs a location of its own customer", func(t *testing.T) {
		_, err := insertDoc(pool, cust, otherLoc, "invoice", "draft", 10, nil)
		assert.Equal(t, "23503", pgCode(t, err))
	})

	t.Run("a booking needs its customer's location and estimate", func(t *testing.T) {
		var svc int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO services (name, duration_minutes, price_cents) VALUES ('s', 30, 100) RETURNING id`).Scan(&svc))
		mine, err := insertDoc(pool, cust, loc, "estimate", "open", 11, nil)
		require.NoError(t, err)
		theirs, err := insertDoc(pool, other, otherLoc, "estimate", "open", 12, nil)
		require.NoError(t, err)
		book := func(customer, location, estimate int64) error {
			_, err := pool.Exec(ctx,
				`INSERT INTO bookings (customer_id, location_id, service_id, estimate_id, starts_at, ends_at)
				 VALUES ($1, $2, $3, $4, '2026-12-10 09:00-06', '2026-12-10 09:30-06')`, customer, location, svc, estimate)
			return err
		}
		assert.Equal(t, "23503", pgCode(t, book(cust, otherLoc, mine)), "another customer's location")
		assert.Equal(t, "23503", pgCode(t, book(cust, loc, theirs)), "another customer's estimate")
		require.NoError(t, book(cust, loc, mine))
	})

	t.Run("payment idempotency keys", func(t *testing.T) {
		a, err := insertDoc(pool, cust, loc, "invoice", "open", 13, nil)
		require.NoError(t, err)
		b, err := insertDoc(pool, cust, loc, "invoice", "open", 14, nil)
		require.NoError(t, err)
		pay := func(doc int64, key any) error {
			_, err := pool.Exec(ctx, `INSERT INTO payments (billing_document_id, amount_cents, idempotency_key) VALUES ($1, 1, $2)`, doc, key)
			return err
		}
		require.NoError(t, pay(a, "k"))
		assert.Equal(t, "23505", pgCode(t, pay(a, "k")), "same key on the same invoice")
		require.NoError(t, pay(b, "k"), "the same key on another invoice")
		require.NoError(t, pay(a, nil))
		require.NoError(t, pay(a, nil), "payments without a key never collide")
	})
}

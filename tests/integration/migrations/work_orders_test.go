//go:build integration

package migrations_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/tests/integration/testdb"
)

func TestWorkOrdersTable(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	cust, loc := seed(t, pool)

	var svc, booking int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO services (name, duration_minutes, price_cents) VALUES ('Bi-Monthly Service', 30, 6500) RETURNING id`).Scan(&svc))
	est, err := insertDoc(pool, cust, loc, "estimate", "open", 1001, nil)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO bookings (customer_id, location_id, service_id, estimate_id, starts_at, ends_at)
		 VALUES ($1, $2, $3, $4, '2026-12-10 09:00-06', '2026-12-10 09:30-06') RETURNING id`,
		cust, loc, svc, est).Scan(&booking))
	insertWork := func(status string) error {
		_, err := pool.Exec(ctx,
			`INSERT INTO work_orders (booking_id, customer_id, service_id, status) VALUES ($1, $2, $3, $4)`,
			booking, cust, svc, status)
		return err
	}

	t.Run("checks", func(t *testing.T) {
		assert.Equal(t, "23514", pgCode(t, insertWork("lost")))
		require.NoError(t, insertWork("completed"))
		assert.Equal(t, "23505", pgCode(t, insertWork("scheduled")), "one work order per booking")
	})

	t.Run("down restores the booking columns and up moves them back", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE work_orders SET notes = 'Filter replaced' WHERE booking_id = $1`, booking)
		require.NoError(t, err)

		run := func(file string) {
			t.Helper()
			sql, err := os.ReadFile("../../../migrations/" + file)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, string(sql))
			require.NoError(t, err, file)
		}
		run("000015_work_orders_table.down.sql")
		var status, notes string
		require.NoError(t, pool.QueryRow(ctx, `SELECT status, notes FROM bookings WHERE id = $1`, booking).Scan(&status, &notes))
		assert.Equal(t, []string{"completed", "Filter replaced"}, []string{status, notes})

		run("000015_work_orders_table.up.sql")
		require.NoError(t, pool.QueryRow(ctx, `SELECT status, notes FROM work_orders WHERE booking_id = $1`, booking).Scan(&status, &notes))
		assert.Equal(t, []string{"completed", "Filter replaced"}, []string{status, notes})
		var cols int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.columns WHERE table_name = 'bookings' AND column_name IN ('status', 'notes')`).Scan(&cols))
		assert.Zero(t, cols)
	})
}

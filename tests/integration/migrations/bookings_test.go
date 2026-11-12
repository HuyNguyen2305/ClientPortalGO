//go:build integration

package migrations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/tests/integration/testdb"
)

func TestBookingConstraints(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	cust, loc := seed(t, pool)

	var svc int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO services (name, duration_minutes, price_cents) VALUES ('Bi-Monthly Service', 30, 6500) RETURNING id`).Scan(&svc))

	estimateNumber := int64(1000)
	book := func(start, end string) (int64, error) {
		estimateNumber++
		est, err := insertDoc(pool, cust, loc, "estimate", "open", estimateNumber, nil)
		require.NoError(t, err)
		var id int64
		err = pool.QueryRow(ctx,
			`INSERT INTO bookings (customer_id, location_id, service_id, estimate_id, starts_at, ends_at)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			cust, loc, svc, est, "2026-12-10 "+start+"-06", "2026-12-10 "+end+"-06").Scan(&id)
		return id, err
	}

	t.Run("service checks", func(t *testing.T) {
		for _, d := range []int{0, 10, 495, 20} {
			_, err := pool.Exec(ctx, `INSERT INTO services (name, duration_minutes, price_cents) VALUES ('x', $1, 0)`, d)
			assert.Equal(t, "23514", pgCode(t, err), "duration %d", d)
		}
		_, err := pool.Exec(ctx, `INSERT INTO services (name, duration_minutes, price_cents) VALUES ('x', 15, -1)`)
		assert.Equal(t, "23514", pgCode(t, err))
	})

	first, err := book("09:00", "09:30")
	require.NoError(t, err)

	t.Run("overlap is rejected", func(t *testing.T) {
		_, err := book("09:15", "09:45")
		assert.Equal(t, "23P01", pgCode(t, err))
	})

	t.Run("touching visits are allowed", func(t *testing.T) {
		_, err := book("09:30", "10:00")
		require.NoError(t, err)
		_, err = book("08:30", "09:00")
		require.NoError(t, err)
	})

	t.Run("a cancelled booking frees its slot", func(t *testing.T) {
		_, err := pool.Exec(ctx, `UPDATE bookings SET cancelled_at = now() WHERE id = $1`, first)
		require.NoError(t, err)
		_, err = book("09:00", "09:30")
		require.NoError(t, err)
	})

	t.Run("end must follow start", func(t *testing.T) {
		_, err := book("11:00", "11:00")
		assert.Equal(t, "23514", pgCode(t, err))
	})

	t.Run("service in use cannot be deleted", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM services WHERE id = $1`, svc)
		assert.Equal(t, "23503", pgCode(t, err))
	})

	t.Run("down", func(t *testing.T) {
		testdb.MigrateDown(t, pool)
		var reg *string
		require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('bookings')::text`).Scan(&reg))
		assert.Nil(t, reg)
	})
}

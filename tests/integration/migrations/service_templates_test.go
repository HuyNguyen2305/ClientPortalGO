//go:build integration

package migrations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/tests/integration/testdb"
)

func TestServiceTemplateConstraints(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	bad := func(name, cols, vals string) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO services (name, duration_minutes, price_cents, `+cols+`) VALUES ('x', 30, 0, `+vals+`)`)
		assert.Equal(t, "23514", pgCode(t, err), name)
	}
	ok := func(name, cols, vals string) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO services (name, duration_minutes, price_cents, `+cols+`) VALUES ('x', 30, 0, `+vals+`)`)
		assert.NoError(t, err, name)
	}

	t.Run("defaults make a plain active service", func(t *testing.T) {
		var status, unit, kind string
		var every int
		var confirmed bool
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO services (name, duration_minutes, price_cents) VALUES ('plain', 15, 0)
			 RETURNING status, repeat_unit, repeat_every, ends_kind, set_confirmed`).Scan(&status, &unit, &every, &kind, &confirmed))
		assert.Equal(t, []any{"active", "off", 1, "never", true}, []any{status, unit, every, kind, confirmed})
	})

	t.Run("checks", func(t *testing.T) {
		bad("color", "color", `'blue'`)
		bad("short color", "color", `'#FFF'`)
		ok("color", "color", `'#A84BC4'`)
		bad("status", "status", `'gone'`)
		bad("repeat unit", "repeat_unit", `'hourly'`)
		bad("repeat every", "repeat_every", `0`)
		bad("repeat_by needs monthly", "repeat_unit, repeat_by", `'weekly', 'weekday'`)
		ok("monthly by weekday", "repeat_unit, repeat_by", `'monthly', 'weekday'`)
		bad("after needs a count", "ends_kind", `'after'`)
		bad("after count of zero", "ends_kind, ends_after", `'after', 0`)
		ok("after 3", "ends_kind, ends_after", `'after', 3`)
		bad("on_date needs a date", "ends_kind", `'on_date'`)
		ok("on_date", "ends_kind, ends_on", `'on_date', '2027-01-01'`)
		bad("never with a count", "ends_after", `3`)
	})

	t.Run("items are checked and cascade with their service", func(t *testing.T) {
		var svc int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO services (name, duration_minutes, price_cents) VALUES ('with items', 30, 100) RETURNING id`).Scan(&svc))
		_, err := pool.Exec(ctx,
			`INSERT INTO service_items (service_id, position, name, quantity, unit_price_cents) VALUES ($1, 0, 'a', 0, 1)`, svc)
		assert.Equal(t, "23514", pgCode(t, err), "quantity")
		_, err = pool.Exec(ctx,
			`INSERT INTO service_items (service_id, position, name, quantity, unit_price_cents) VALUES ($1, 0, 'a', 1, -1)`, svc)
		assert.Equal(t, "23514", pgCode(t, err), "price")
		_, err = pool.Exec(ctx,
			`INSERT INTO service_items (service_id, position, name, quantity, unit_price_cents) VALUES ($1, 0, 'a', 1, 100)`, svc)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `DELETE FROM services WHERE id = $1`, svc)
		require.NoError(t, err)
		var left int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM service_items WHERE service_id = $1`, svc).Scan(&left))
		assert.Zero(t, left)
	})

	t.Run("down", func(t *testing.T) {
		testdb.MigrateDown(t, pool)
		var reg *string
		require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('booking_series')::text`).Scan(&reg))
		assert.Nil(t, reg)
	})
}

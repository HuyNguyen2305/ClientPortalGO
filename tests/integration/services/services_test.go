//go:build integration

package services_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

func TestServices(t *testing.T) {
	app := testapp.New(t)

	create := func(name string, minutes int, cents int64) dto.ServiceResponse {
		rec := app.Do(t, http.MethodPost, "/api/v1/services", 0,
			map[string]any{"name": name, "duration_minutes": minutes, "price_cents": cents})
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		return testapp.Decode[dto.ServiceResponse](t, rec)
	}

	monthly := create("Monthly Service", 15, 4500)
	create("Bi-Monthly Service", 30, 6500)
	assert.Equal(t, "Monthly Service", monthly.Name)
	assert.Equal(t, 15, monthly.DurationMinutes)
	assert.Equal(t, int64(4500), monthly.PriceCents)

	rec := app.Do(t, http.MethodGet, "/api/v1/services", 0, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	list := testapp.Decode[dto.Page[dto.ServiceResponse]](t, rec).Data
	require.Len(t, list, 2)
	assert.Equal(t, "Bi-Monthly Service", list[0].Name, "sorted by name")

	path := fmt.Sprintf("/api/v1/services/%d", monthly.ID)
	rec = app.Do(t, http.MethodPut, path, 0, map[string]any{"name": "Monthly Service", "duration_minutes": 30, "price_cents": 5000})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 30, testapp.Decode[dto.ServiceResponse](t, rec).DurationMinutes)

	assert.Equal(t, http.StatusOK, app.Do(t, http.MethodGet, path, 0, nil).Code)
	assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, "/api/v1/services/999999", 0, nil).Code)
	assert.Equal(t, http.StatusBadRequest, app.Do(t, http.MethodPost, "/api/v1/services", 0,
		map[string]any{"name": "x", "duration_minutes": 20, "price_cents": 1}).Code)

	t.Run("delete blocked by a booking", func(t *testing.T) {
		ctx := context.Background()
		var cust, loc, est int64
		require.NoError(t, app.Pool.QueryRow(ctx, `INSERT INTO customers (name) VALUES ('A') RETURNING id`).Scan(&cust))
		require.NoError(t, app.Pool.QueryRow(ctx,
			`INSERT INTO locations (customer_id, street, city, state, postal_code) VALUES ($1, 's', 'c', 'TX', '1') RETURNING id`, cust).Scan(&loc))
		require.NoError(t, app.Pool.QueryRow(ctx,
			`INSERT INTO billing_documents (parent_type, number, customer_id, location_id, title, status, document_date)
			 VALUES ('estimate', 1, $1, $2, 't', 'open', '2026-12-10') RETURNING id`, cust, loc).Scan(&est))
		_, err := app.Pool.Exec(ctx,
			`INSERT INTO bookings (customer_id, location_id, service_id, estimate_id, starts_at, ends_at)
			 VALUES ($1, $2, $3, $4, '2026-12-10 09:00-06', '2026-12-10 09:30-06')`, cust, loc, monthly.ID, est)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, app.Do(t, http.MethodDelete, path, 0, nil).Code)
	})

	t.Run("delete an unused service", func(t *testing.T) {
		s := create("Call Back Service", 15, 0)
		p := fmt.Sprintf("/api/v1/services/%d", s.ID)
		assert.Equal(t, http.StatusNoContent, app.Do(t, http.MethodDelete, p, 0, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, p, 0, nil).Code)
	})
}

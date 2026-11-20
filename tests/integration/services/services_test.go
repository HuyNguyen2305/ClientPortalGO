//go:build integration

package services_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	t.Run("delete is soft, so a service with a booking can be deleted", func(t *testing.T) {
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
		assert.Equal(t, http.StatusNoContent, app.Do(t, http.MethodDelete, path, 0, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, path, 0, nil).Code)
		var kept int
		require.NoError(t, app.Pool.QueryRow(ctx, `SELECT count(*) FROM services WHERE id = $1 AND status = 'deleted'`, monthly.ID).Scan(&kept))
		assert.Equal(t, 1, kept, "the row stays for its bookings")
	})

	t.Run("delete an unused service", func(t *testing.T) {
		s := create("Call Back Service", 15, 0)
		p := fmt.Sprintf("/api/v1/services/%d", s.ID)
		assert.Equal(t, http.StatusNoContent, app.Do(t, http.MethodDelete, p, 0, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, p, 0, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodDelete, p, 0, nil).Code, "already deleted")
	})
}

func TestServiceTemplates(t *testing.T) {
	app := testapp.New(t)
	create := func(body map[string]any) *httptest.ResponseRecorder {
		return app.Do(t, http.MethodPost, "/api/v1/services", 0, body)
	}

	rec := create(map[string]any{
		"name": "Bi-Monthly Service", "duration_minutes": 30, "price_cents": 1, "color": "#A84BC4", "set_confirmed": false,
		"repeat": map[string]any{"unit": "monthly", "every": 2, "by": "weekday", "ends": "after", "ends_after": 6},
		"items": []map[string]any{
			{"name": "General maintenance", "description": "every other month", "quantity": 1, "unit_price_cents": 6500},
			{"name": "Filter", "quantity": 2, "unit_price_cents": 1000},
		},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	tpl := testapp.Decode[dto.ServiceResponse](t, rec)
	assert.Equal(t, int64(8500), tpl.PriceCents, "the price is the sum of the items")
	assert.Equal(t, "active", tpl.Status)
	assert.False(t, tpl.SetConfirmed)
	require.NotNil(t, tpl.Color)
	assert.Equal(t, "#A84BC4", *tpl.Color)
	assert.Equal(t, dto.RepeatResponse{Unit: "monthly", Every: 2, By: "weekday", Ends: "after", EndsAfter: 6}, tpl.Repeat)
	require.Len(t, tpl.Items, 2)
	assert.Equal(t, "General maintenance", tpl.Items[0].Name)
	assert.Equal(t, int64(2000), tpl.Items[1].TotalCents)

	path := fmt.Sprintf("/api/v1/services/%d", tpl.ID)
	t.Run("items come back on read", func(t *testing.T) {
		got := testapp.Decode[dto.ServiceResponse](t, app.Do(t, http.MethodGet, path, 0, nil))
		assert.Equal(t, tpl.Items[0].Name, got.Items[0].Name)
		assert.Equal(t, int64(8500), got.PriceCents)
	})

	t.Run("a plain service defaults to no repeat and no items", func(t *testing.T) {
		rec := create(map[string]any{"name": "Call Back Service", "duration_minutes": 15, "price_cents": 0})
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		got := testapp.Decode[dto.ServiceResponse](t, rec)
		assert.Equal(t, dto.RepeatResponse{Unit: "off", Every: 1, Ends: "never"}, got.Repeat)
		assert.True(t, got.SetConfirmed)
		assert.Empty(t, got.Items)
	})

	t.Run("update replaces the items", func(t *testing.T) {
		rec := app.Do(t, http.MethodPut, path, 0, map[string]any{
			"name": "Bi-Monthly Service", "duration_minutes": 30, "price_cents": 1,
			"items": []map[string]any{{"name": "Only line", "quantity": 1, "unit_price_cents": 7000}},
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		got := testapp.Decode[dto.ServiceResponse](t, app.Do(t, http.MethodGet, path, 0, nil))
		require.Len(t, got.Items, 1)
		assert.Equal(t, int64(7000), got.PriceCents)
		assert.Equal(t, "off", got.Repeat.Unit, "the repeat rule is replaced too")
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodPut, "/api/v1/services/999999", 0, map[string]any{
			"name": "x", "duration_minutes": 15, "price_cents": 1}).Code)
	})

	t.Run("invalid repeat rules are refused", func(t *testing.T) {
		for name, repeat := range map[string]map[string]any{
			"by on a weekly repeat": {"unit": "weekly", "by": "weekday"},
			"after without a count": {"unit": "daily", "ends": "after"},
			"on_date without date":  {"unit": "daily", "ends": "on_date"},
		} {
			rec := create(map[string]any{"name": "x", "duration_minutes": 15, "price_cents": 1, "repeat": repeat})
			assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		}
	})

	t.Run("archived templates are hidden from customers but listed for admins", func(t *testing.T) {
		require.Equal(t, http.StatusNoContent, app.Do(t, http.MethodPost, path+"/archive", 0, nil).Code)

		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, path, 0, nil).Code)
		names := func(url string) []string {
			rec := app.Do(t, http.MethodGet, url, 0, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var out []string
			for _, s := range testapp.Decode[dto.Page[dto.ServiceResponse]](t, rec).Data {
				out = append(out, s.Name)
			}
			return out
		}
		assert.Equal(t, []string{"Call Back Service"}, names("/api/v1/services"))
		assert.Equal(t, []string{"Bi-Monthly Service"}, names("/api/v1/admin/services?status=archived"))
		assert.Equal(t, []string{"Call Back Service"}, names("/api/v1/admin/services"))
		assert.Equal(t, http.StatusBadRequest, app.Do(t, http.MethodGet, "/api/v1/admin/services?status=gone", 0, nil).Code)

		require.Equal(t, http.StatusNoContent, app.Do(t, http.MethodDelete, path, 0, nil).Code)
		assert.Equal(t, []string{"Bi-Monthly Service"}, names("/api/v1/admin/services?status=deleted"))
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodPost, path+"/archive", 0, nil).Code, "a deleted service cannot be archived")
	})
}

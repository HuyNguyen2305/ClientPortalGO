//go:build integration

package locations_test

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

func newCustomer(t *testing.T, app *testapp.App, name string) int64 {
	t.Helper()
	rec := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code)
	return testapp.Decode[dto.CustomerResponse](t, rec).ID
}

var sanAntonio = map[string]any{
	"street": "West Loop 1604 South", "city": "San Antonio", "state": "TX", "postal_code": "12341",
}

func TestLocations(t *testing.T) {
	app := testapp.New(t)
	a := newCustomer(t, app, "A")
	b := newCustomer(t, app, "B")

	rec := app.Do(t, http.MethodPost, "/api/v1/locations", a, sanAntonio)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	loc := testapp.Decode[dto.LocationResponse](t, rec)
	assert.Equal(t, "San Antonio", loc.City)
	path := fmt.Sprintf("/api/v1/locations/%d", loc.ID)

	list := func(customer int64) []dto.LocationResponse {
		rec := app.Do(t, http.MethodGet, "/api/v1/locations", customer, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		page := testapp.Decode[dto.Page[dto.LocationResponse]](t, rec)
		assert.Empty(t, page.NextCursor)
		return page.Data
	}
	assert.Len(t, list(a), 1)
	assert.Empty(t, list(b))

	t.Run("another customer cannot see it", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, path, b, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodPut, path, b, sanAntonio).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodDelete, path, b, nil).Code)
	})

	t.Run("owner updates it", func(t *testing.T) {
		rec := app.Do(t, http.MethodPut, path, a, map[string]any{
			"street": "Wall Street", "city": "New York", "state": "NY", "postal_code": "1232312",
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, "New York", testapp.Decode[dto.LocationResponse](t, rec).City)
	})

	t.Run("header rules", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, app.Do(t, http.MethodGet, "/api/v1/locations", 0, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, "/api/v1/locations", 999999, nil).Code)
	})

	t.Run("delete blocked by a document", func(t *testing.T) {
		_, err := app.Pool.Exec(context.Background(),
			`INSERT INTO billing_documents (parent_type, number, customer_id, location_id, title, status, document_date)
			 VALUES ('invoice', 1, $1, $2, 't', 'draft', '2026-12-10')`, a, loc.ID)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, app.Do(t, http.MethodDelete, path, a, nil).Code)
	})

	t.Run("delete a free location", func(t *testing.T) {
		rec := app.Do(t, http.MethodPost, "/api/v1/locations", a, sanAntonio)
		require.Equal(t, http.StatusCreated, rec.Code)
		free := fmt.Sprintf("/api/v1/locations/%d", testapp.Decode[dto.LocationResponse](t, rec).ID)
		assert.Equal(t, http.StatusNoContent, app.Do(t, http.MethodDelete, free, a, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, free, a, nil).Code)
	})

	t.Run("validation", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, app.Do(t, http.MethodPost, "/api/v1/locations", a, map[string]any{"street": "x"}).Code)
		assert.Equal(t, http.StatusBadRequest, app.Do(t, http.MethodGet, "/api/v1/locations/abc", a, nil).Code)
	})
}

//go:build integration

package customers_test

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

func create(t *testing.T, app *testapp.App, name string) dto.CustomerResponse {
	t.Helper()
	rec := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return testapp.Decode[dto.CustomerResponse](t, rec)
}

func TestCustomers(t *testing.T) {
	app := testapp.New(t)

	first := create(t, app, "William Saliba")
	assert.Equal(t, int64(5000), first.AccountNumber)
	assert.Equal(t, int64(5001), create(t, app, "Second").AccountNumber)

	path := fmt.Sprintf("/api/v1/customers/%d", first.ID)
	rec := app.Do(t, http.MethodGet, path, 0, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "William Saliba", testapp.Decode[dto.CustomerResponse](t, rec).Name)

	rec = app.Do(t, http.MethodPut, path, 0, map[string]any{"name": "W. Saliba", "email": "w@example.com"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updated := testapp.Decode[dto.CustomerResponse](t, rec)
	assert.Equal(t, "W. Saliba", updated.Name)
	require.NotNil(t, updated.Email)
	assert.Equal(t, "w@example.com", *updated.Email)

	assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, "/api/v1/customers/999999", 0, nil).Code)
	assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodPut, "/api/v1/customers/999999", 0, map[string]any{"name": "x"}).Code)

	t.Run("pagination", func(t *testing.T) {
		create(t, app, "Third")
		var names []string
		cursor := ""
		pages := 0
		for {
			rec := app.Do(t, http.MethodGet, "/api/v1/customers?limit=2"+cursor, 0, nil)
			require.Equal(t, http.StatusOK, rec.Code)
			page := testapp.Decode[dto.Page[dto.CustomerResponse]](t, rec)
			pages++
			for _, c := range page.Data {
				names = append(names, c.Name)
			}
			if page.NextCursor == "" {
				break
			}
			cursor = "&cursor=" + page.NextCursor
		}
		assert.Equal(t, 2, pages)
		assert.Equal(t, []string{"W. Saliba", "Second", "Third"}, names)
	})

	t.Run("delete", func(t *testing.T) {
		victim := create(t, app, "Delete me")
		p := fmt.Sprintf("/api/v1/customers/%d", victim.ID)
		assert.Equal(t, http.StatusNoContent, app.Do(t, http.MethodDelete, p, 0, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodGet, p, 0, nil).Code)
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodDelete, p, 0, nil).Code)
	})

	t.Run("delete blocked by a location", func(t *testing.T) {
		_, err := app.Pool.Exec(context.Background(),
			`INSERT INTO locations (customer_id, street, city, state, postal_code) VALUES ($1, 's', 'c', 'TX', '1')`, first.ID)
		require.NoError(t, err)
		rec := app.Do(t, http.MethodDelete, path, 0, nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "conflict", testapp.Decode[dto.ErrorResponse](t, rec).Error.Code)
	})

	t.Run("empty list is an array", func(t *testing.T) {
		empty := testapp.New(t)
		rec := empty.Do(t, http.MethodGet, "/api/v1/customers", 0, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"data":[]}`, rec.Body.String())
	})
}

func TestUpdateWithoutEmailKeepsALoginEmail(t *testing.T) {
	app := testapp.New(t)
	rec := app.DoWith(t, http.MethodPost, "/api/v1/auth/register", 0,
		map[string]any{"name": "Ada", "email": "ada@example.com", "password": "correct horse"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	registered := testapp.Decode[dto.AuthResponse](t, rec)

	// A registered customer (has a password): a blank email must not lock them out.
	path := fmt.Sprintf("/api/v1/customers/%d", registered.Customer.ID)
	rec = app.Do(t, http.MethodPut, path, 0, map[string]any{"name": "Ada L."})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, testapp.Decode[dto.CustomerResponse](t, rec).Email)
	rec = app.DoWith(t, http.MethodPost, "/api/v1/auth/login", 0,
		map[string]any{"email": "ada@example.com", "password": "correct horse"}, nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// A staff-created customer has no password, so the email can still be cleared.
	rec = app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": "Walk-in", "email": "walkin@example.com"})
	require.Equal(t, http.StatusCreated, rec.Code)
	walkin := testapp.Decode[dto.CustomerResponse](t, rec)
	rec = app.Do(t, http.MethodPut, fmt.Sprintf("/api/v1/customers/%d", walkin.ID), 0, map[string]any{"name": "Walk-in"})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, testapp.Decode[dto.CustomerResponse](t, rec).Email)
}

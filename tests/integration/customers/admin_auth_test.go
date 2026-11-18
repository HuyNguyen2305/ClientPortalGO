//go:build integration

package customers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

func TestAdminRoutesNeedKey(t *testing.T) {
	app := testapp.New(t)
	for _, r := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/customers"},
		{http.MethodGet, "/api/v1/customers"},
		{http.MethodGet, "/api/v1/customers/1"},
		{http.MethodPut, "/api/v1/customers/1"},
		{http.MethodDelete, "/api/v1/customers/1"},
		{http.MethodPost, "/api/v1/services"},
		{http.MethodPut, "/api/v1/services/1"},
		{http.MethodDelete, "/api/v1/services/1"},
		{http.MethodPost, "/api/v1/invoices"},
		{http.MethodPut, "/api/v1/invoices/1"},
		{http.MethodDelete, "/api/v1/invoices/1"},
		{http.MethodPost, "/api/v1/invoices/1/send"},
		{http.MethodPost, "/api/v1/invoices/1/void"},
		{http.MethodPost, "/api/v1/invoices/1/payments"},
		{http.MethodPost, "/api/v1/estimates"},
		{http.MethodPut, "/api/v1/estimates/1"},
		{http.MethodDelete, "/api/v1/estimates/1"},
		{http.MethodPost, "/api/v1/estimates/1/send"},
		{http.MethodPost, "/api/v1/estimates/1/convert"},
	} {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			req := httptest.NewRequest(r.method, r.path, strings.NewReader("{}"))
			rec := httptest.NewRecorder()
			app.Handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}

	// The catalog stays readable without the key.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
	rec := httptest.NewRecorder()
	app.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// A customer still reads their own invoices without the key.
	created := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": "A"})
	require.Equal(t, http.StatusCreated, created.Code)
	id := testapp.Decode[dto.CustomerResponse](t, created).ID
	req = httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil)
	req.Header.Set("Authorization", "Bearer "+app.Session(t, id, time.Hour))
	rec = httptest.NewRecorder()
	app.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

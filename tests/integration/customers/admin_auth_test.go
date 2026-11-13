//go:build integration

package customers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

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
}

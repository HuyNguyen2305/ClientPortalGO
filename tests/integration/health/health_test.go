//go:build integration

package health_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

func TestHealth(t *testing.T) {
	app := testapp.New(t)

	rec := app.Do(t, http.MethodGet, "/healthz", 0, nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	app.Pool.Close()
	rec = app.Do(t, http.MethodGet, "/healthz", 0, nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "internal", testapp.Decode[dto.ErrorResponse](t, rec).Error.Code)
}

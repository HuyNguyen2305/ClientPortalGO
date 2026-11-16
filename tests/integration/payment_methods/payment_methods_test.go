//go:build integration

package payment_methods_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/internal/model"
	"clientportal/tests/integration/testapp"
)

func newCustomer(t *testing.T, app *testapp.App, name string) int64 {
	t.Helper()
	rec := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code)
	return testapp.Decode[dto.CustomerResponse](t, rec).ID
}

func TestPaymentMethods(t *testing.T) {
	app := testapp.New(t)
	a := newCustomer(t, app, "A")
	b := newCustomer(t, app, "B")
	const base = "/api/v1/payment-methods"

	t.Run("no cards before any setup", func(t *testing.T) {
		rec := app.Do(t, http.MethodGet, base, a, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Empty(t, testapp.Decode[dto.Page[dto.CardResponse]](t, rec).Data)
	})

	t.Run("setup intent links the gateway customer once", func(t *testing.T) {
		for range 2 {
			rec := app.Do(t, http.MethodPost, base+"/setup-intent", a, nil)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			assert.Equal(t, fmt.Sprintf("seti_secret_cus_%d", a), testapp.Decode[dto.SetupIntentResponse](t, rec).ClientSecret)
		}
		var sid string
		require.NoError(t, app.Pool.QueryRow(t.Context(),
			`SELECT stripe_customer_id FROM customers WHERE id = $1`, a).Scan(&sid))
		assert.Equal(t, fmt.Sprintf("cus_%d", a), sid)
	})

	gidA := fmt.Sprintf("cus_%d", a)
	app.Gateway.Cards[gidA] = []model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030}}

	t.Run("lists the card", func(t *testing.T) {
		rec := app.Do(t, http.MethodGet, base, a, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, []dto.CardResponse{{ID: "pm_1", Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030}},
			testapp.Decode[dto.Page[dto.CardResponse]](t, rec).Data)
	})

	t.Run("another customer cannot remove it", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodDelete, base+"/pm_1", b, nil).Code)
		assert.Len(t, app.Gateway.Cards[gidA], 1)
	})

	t.Run("owner removes it", func(t *testing.T) {
		assert.Equal(t, http.StatusNoContent, app.Do(t, http.MethodDelete, base+"/pm_1", a, nil).Code)
		assert.Empty(t, app.Gateway.Cards[gidA])
		assert.Equal(t, http.StatusNotFound, app.Do(t, http.MethodDelete, base+"/pm_1", a, nil).Code)
	})

	t.Run("needs a customer", func(t *testing.T) {
		assert.GreaterOrEqual(t, app.Do(t, http.MethodGet, base, 0, nil).Code, 400)
	})
}

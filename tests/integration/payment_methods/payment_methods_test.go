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

// openInvoice creates a $65.00 invoice for the customer and sends it, so it is open.
func openInvoice(t *testing.T, app *testapp.App, customer int64) int64 {
	t.Helper()
	loc := app.Do(t, http.MethodPost, "/api/v1/locations", customer, map[string]any{
		"street": "West Loop 1604 South", "city": "San Antonio", "state": "TX", "postal_code": "12341",
	})
	require.Equal(t, http.StatusCreated, loc.Code, loc.Body.String())
	rec := app.Do(t, http.MethodPost, "/api/v1/invoices", customer, map[string]any{
		"location_id": testapp.Decode[dto.LocationResponse](t, loc).ID, "title": "Bi-Monthly Service", "document_date": "2026-11-01",
		"items": []map[string]any{{"name": "Service", "description": "General maintenance", "quantity": 1, "unit_price_cents": 6500}},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	id := testapp.Decode[dto.DocumentResponse](t, rec).ID
	require.Equal(t, http.StatusOK, app.Do(t, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), customer, nil).Code)
	return id
}

func TestPayInvoice(t *testing.T) {
	app := testapp.New(t)
	a := newCustomer(t, app, "A")
	b := newCustomer(t, app, "B")
	app.Gateway.Cards[fmt.Sprintf("cus_%d", a)] = []model.Card{{ID: "pm_1", Brand: "visa", Last4: "4242"}}
	require.Equal(t, http.StatusCreated, app.Do(t, http.MethodPost, "/api/v1/payment-methods/setup-intent", a, nil).Code)
	invoice := openInvoice(t, app, a)
	path := fmt.Sprintf("/api/v1/invoices/%d/pay", invoice)
	pay := func(customer int64, card, key string) int {
		headers := map[string]string{}
		if key != "" {
			headers["Idempotency-Key"] = key
		}
		return app.DoWith(t, http.MethodPost, path, customer, map[string]any{"payment_method_id": card}, headers).Code
	}
	payments := func() int {
		var n int
		require.NoError(t, app.Pool.QueryRow(t.Context(), `SELECT count(*) FROM payments WHERE billing_document_id = $1`, invoice).Scan(&n))
		return n
	}

	t.Run("needs an idempotency key", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, pay(a, "pm_1", ""))
	})

	t.Run("a decline is 402 and changes nothing", func(t *testing.T) {
		app.Gateway.Decline = true
		defer func() { app.Gateway.Decline = false }()
		assert.Equal(t, http.StatusPaymentRequired, pay(a, "pm_1", "k-declined"))
		assert.Zero(t, payments())
	})

	t.Run("another customer cannot pay it, or use the card", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, pay(b, "pm_1", "k-b"))
		assert.Empty(t, app.Gateway.Charges)
	})

	t.Run("pays the balance once, even when retried", func(t *testing.T) {
		for range 2 {
			rec := app.DoWith(t, http.MethodPost, path, a, map[string]any{"payment_method_id": "pm_1"},
				map[string]string{"Idempotency-Key": "k-1"})
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			doc := testapp.Decode[dto.DocumentResponse](t, rec)
			assert.Equal(t, "paid", doc.Status)
			assert.EqualValues(t, 0, *doc.BalanceDueCents)
		}
		assert.Equal(t, map[string]int64{"k-1": 6500}, app.Gateway.Charges)
		assert.Equal(t, 1, payments())
	})

	t.Run("a paid invoice is a conflict", func(t *testing.T) {
		assert.Equal(t, http.StatusConflict, pay(a, "pm_1", "k-2"))
	})
}

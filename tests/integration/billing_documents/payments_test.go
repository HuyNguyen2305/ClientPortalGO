//go:build integration

package billing_documents_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

// openInvoice creates and sends a 6500-cent invoice at the given location.
func (w world) openInvoice(t *testing.T, location int64) dto.DocumentResponse {
	t.Helper()
	rec := w.app.Do(t, http.MethodPost, "/api/v1/invoices", w.customer, docBody(location, "2026-12-10", item("Bi-Monthly Service", 1, 6500)))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	inv := testapp.Decode[dto.DocumentResponse](t, rec)
	return w.do(t, http.MethodPost, docPath("invoices", inv.ID, "/send"), nil, http.StatusOK)
}

func pay(cents int64) map[string]any { return map[string]any{"amount_cents": cents} }

func (w world) balance(t *testing.T, query string) int64 {
	t.Helper()
	rec := w.app.Do(t, http.MethodGet, "/api/v1/balance"+query, w.customer, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return testapp.Decode[dto.BalanceResponse](t, rec).BalanceCents
}

func TestPayments(t *testing.T) {
	w := newWorld(t)
	inv := w.openInvoice(t, w.location)
	payments := docPath("invoices", inv.ID, "/payments")

	got := w.do(t, http.MethodPost, payments, pay(1000), http.StatusCreated)
	assert.Equal(t, "partially_paid", got.Status)
	assert.Equal(t, int64(1000), *got.AmountPaidCents)
	assert.Equal(t, int64(5500), *got.BalanceDueCents)

	w.do(t, http.MethodPost, docPath("invoices", inv.ID, "/void"), nil, http.StatusConflict)

	got = w.do(t, http.MethodPost, payments, pay(5500), http.StatusCreated)
	assert.Equal(t, "paid", got.Status)
	assert.Equal(t, int64(0), *got.BalanceDueCents)
	w.do(t, http.MethodPost, payments, pay(1), http.StatusConflict)

	rec := w.app.Do(t, http.MethodGet, payments, w.customer, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	list := testapp.Decode[dto.Page[dto.PaymentResponse]](t, rec).Data
	require.Len(t, list, 2)
	assert.Equal(t, int64(1000), list[0].AmountCents)
	assert.Equal(t, int64(5500), list[1].AmountCents)

	t.Run("draft invoice", func(t *testing.T) {
		d := w.create(t, "invoices", "2026-12-10")
		w.do(t, http.MethodPost, docPath("invoices", d.ID, "/payments"), pay(100), http.StatusConflict)
	})

	t.Run("estimate id on the invoice route", func(t *testing.T) {
		e := w.create(t, "estimates", "2026-12-10")
		w.do(t, http.MethodPost, docPath("invoices", e.ID, "/payments"), pay(100), http.StatusNotFound)
	})

	t.Run("another customer", func(t *testing.T) {
		b, _ := newCustomer(t, w.app, "B")
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodPost, payments, b, pay(1)).Code)
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodGet, payments, b, nil).Code)
	})
}

func TestBalance(t *testing.T) {
	w := newWorld(t)
	l1 := w.location
	l2 := newLocation(t, w.app, w.customer)

	w.openInvoice(t, l1) // open, 6500 due
	partial := w.openInvoice(t, l2)
	w.do(t, http.MethodPost, docPath("invoices", partial.ID, "/payments"), pay(1000), http.StatusCreated) // 5500 due
	paid := w.openInvoice(t, l1)
	w.do(t, http.MethodPost, docPath("invoices", paid.ID, "/payments"), pay(6500), http.StatusCreated)
	w.create(t, "invoices", "2026-12-10") // draft
	voided := w.create(t, "invoices", "2026-12-10")
	w.do(t, http.MethodPost, docPath("invoices", voided.ID, "/void"), nil, http.StatusOK)
	est := w.create(t, "estimates", "2026-12-10")
	w.do(t, http.MethodPost, docPath("estimates", est.ID, "/send"), nil, http.StatusOK)

	assert.Equal(t, int64(12000), w.balance(t, ""))
	assert.Equal(t, int64(6500), w.balance(t, fmt.Sprintf("?location_id=%d", l1)))
	assert.Equal(t, http.StatusBadRequest, w.app.Do(t, http.MethodGet, "/api/v1/balance?location_id=x", w.customer, nil).Code)

	b, _ := newCustomer(t, w.app, "B")
	rec := w.app.Do(t, http.MethodGet, "/api/v1/balance", b, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"balance_cents":0}`, rec.Body.String())
}

func TestConcurrentPaymentsCannotOverpay(t *testing.T) {
	w := newWorld(t)
	inv := w.openInvoice(t, w.location)
	payments := docPath("invoices", inv.ID, "/payments")

	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = w.app.Do(t, http.MethodPost, payments, w.customer, pay(4000)).Code
		}()
	}
	wg.Wait()

	assert.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, codes)
	got := w.do(t, http.MethodGet, docPath("invoices", inv.ID, ""), nil, http.StatusOK)
	assert.Equal(t, int64(4000), *got.AmountPaidCents)
}

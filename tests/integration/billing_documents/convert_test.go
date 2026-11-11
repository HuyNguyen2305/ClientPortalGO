//go:build integration

package billing_documents_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
)

// acceptedEstimate creates an estimate with two items, sends it and accepts it.
func (w world) acceptedEstimate(t *testing.T) dto.DocumentResponse {
	t.Helper()
	est := w.create(t, "estimates", "2026-12-08", item("Bi-Monthly Service", 1, 6500), item("Bait", 2, 250))
	w.do(t, http.MethodPost, docPath("estimates", est.ID, "/send"), nil, http.StatusOK)
	return w.do(t, http.MethodPost, docPath("estimates", est.ID, "/accept"), nil, http.StatusOK)
}

func (w world) invoiceCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, w.app.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM billing_documents WHERE parent_type = 'invoice'`).Scan(&n))
	return n
}

func TestConvert(t *testing.T) {
	w := newWorld(t)

	t.Run("not accepted", func(t *testing.T) {
		draft := w.create(t, "estimates", "2026-12-08")
		w.do(t, http.MethodPost, docPath("estimates", draft.ID, "/convert"), nil, http.StatusConflict)

		open := w.create(t, "estimates", "2026-12-08")
		w.do(t, http.MethodPost, docPath("estimates", open.ID, "/send"), nil, http.StatusOK)
		w.do(t, http.MethodPost, docPath("estimates", open.ID, "/convert"), nil, http.StatusConflict)

		declined := w.create(t, "estimates", "2026-12-08")
		w.do(t, http.MethodPost, docPath("estimates", declined.ID, "/send"), nil, http.StatusOK)
		w.do(t, http.MethodPost, docPath("estimates", declined.ID, "/decline"), nil, http.StatusOK)
		w.do(t, http.MethodPost, docPath("estimates", declined.ID, "/convert"), nil, http.StatusConflict)
	})

	est := w.acceptedEstimate(t)
	inv := w.do(t, http.MethodPost, docPath("estimates", est.ID, "/convert"), nil, http.StatusCreated)
	assert.Equal(t, "draft", inv.Status)
	assert.Equal(t, est.Title, inv.Title)
	assert.Equal(t, est.Location.ID, inv.Location.ID)
	assert.Equal(t, int64(7000), inv.TotalCents)
	require.Len(t, inv.Items, 2)
	assert.Equal(t, "Bi-Monthly Service", inv.Items[0].Name)
	assert.Equal(t, "Bait", inv.Items[1].Name)
	assert.NotNil(t, inv.AmountPaidCents, "the result is an invoice")
	w.do(t, http.MethodGet, docPath("invoices", inv.ID, ""), nil, http.StatusOK)

	assert.Equal(t, "accepted", w.do(t, http.MethodGet, docPath("estimates", est.ID, ""), nil, http.StatusOK).Status)

	before := w.invoiceCount(t)
	w.do(t, http.MethodPost, docPath("estimates", est.ID, "/convert"), nil, http.StatusConflict)
	assert.Equal(t, before, w.invoiceCount(t))

	t.Run("another customer", func(t *testing.T) {
		b, _ := newCustomer(t, w.app, "B")
		e := w.acceptedEstimate(t)
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodPost, docPath("estimates", e.ID, "/convert"), b, nil).Code)
	})

	t.Run("concurrent converts", func(t *testing.T) {
		e := w.acceptedEstimate(t)
		codes := make([]int, 2)
		var wg sync.WaitGroup
		for i := range codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = w.app.Do(t, http.MethodPost, docPath("estimates", e.ID, "/convert"), w.customer, nil).Code
			}()
		}
		wg.Wait()
		assert.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, codes)
	})
}

// TestFullPath walks the whole customer journey through the API.
func TestFullPath(t *testing.T) {
	w := newWorld(t)

	est := w.create(t, "estimates", "2026-12-08", item("Bi-Monthly Service", 1, 6500))
	w.do(t, http.MethodPost, docPath("estimates", est.ID, "/send"), nil, http.StatusOK)
	w.do(t, http.MethodPost, docPath("estimates", est.ID, "/accept"), nil, http.StatusOK)
	inv := w.do(t, http.MethodPost, docPath("estimates", est.ID, "/convert"), nil, http.StatusCreated)
	w.do(t, http.MethodPost, docPath("invoices", inv.ID, "/send"), nil, http.StatusOK)

	assert.Equal(t, int64(6500), w.balance(t, ""))
	w.do(t, http.MethodPost, docPath("invoices", inv.ID, "/payments"), pay(2000), http.StatusCreated)
	assert.Equal(t, int64(4500), w.balance(t, ""))
	paid := w.do(t, http.MethodPost, docPath("invoices", inv.ID, "/payments"), pay(4500), http.StatusCreated)
	assert.Equal(t, "paid", paid.Status)
	assert.Equal(t, int64(0), w.balance(t, ""))

	rec := w.app.Do(t, http.MethodGet, "/api/v1/invoices?status=paid", w.customer, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"paid"`)
	list := decodePage(t, rec.Body.Bytes())
	require.Len(t, list, 1)
	assert.Equal(t, inv.ID, list[0].ID)
}

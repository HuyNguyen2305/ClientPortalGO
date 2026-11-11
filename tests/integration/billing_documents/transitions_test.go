//go:build integration

package billing_documents_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInvoiceTransitions(t *testing.T) {
	w := newWorld(t)
	inv := w.create(t, "invoices", "2026-12-10")
	send := docPath("invoices", inv.ID, "/send")
	void := docPath("invoices", inv.ID, "/void")

	assert.Equal(t, "open", w.do(t, http.MethodPost, send, nil, http.StatusOK).Status)
	w.do(t, http.MethodPost, send, nil, http.StatusConflict)

	t.Run("open invoice is locked for edits", func(t *testing.T) {
		w.do(t, http.MethodPut, docPath("invoices", inv.ID, ""), docBody(w.location, "2026-12-10", item("x", 1, 1)), http.StatusConflict)
		w.do(t, http.MethodDelete, docPath("invoices", inv.ID, ""), nil, http.StatusConflict)
	})

	assert.Equal(t, "void", w.do(t, http.MethodPost, void, nil, http.StatusOK).Status)
	w.do(t, http.MethodPost, void, nil, http.StatusConflict)

	t.Run("a draft can be voided", func(t *testing.T) {
		d := w.create(t, "invoices", "2026-12-11")
		assert.Equal(t, "void", w.do(t, http.MethodPost, docPath("invoices", d.ID, "/void"), nil, http.StatusOK).Status)
	})

	t.Run("another customer", func(t *testing.T) {
		d := w.create(t, "invoices", "2026-12-12")
		b, _ := newCustomer(t, w.app, "B")
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodPost, docPath("invoices", d.ID, "/send"), b, nil).Code)
	})

	t.Run("estimate actions do not exist on invoices", func(t *testing.T) {
		d := w.create(t, "invoices", "2026-12-13")
		assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodPost, docPath("invoices", d.ID, "/accept"), w.customer, nil).Code)
	})
}

func TestEstimateTransitions(t *testing.T) {
	w := newWorld(t)
	est := w.create(t, "estimates", "2026-12-10")

	w.do(t, http.MethodPost, docPath("estimates", est.ID, "/accept"), nil, http.StatusConflict)
	assert.Equal(t, "open", w.do(t, http.MethodPost, docPath("estimates", est.ID, "/send"), nil, http.StatusOK).Status)
	assert.Equal(t, "accepted", w.do(t, http.MethodPost, docPath("estimates", est.ID, "/accept"), nil, http.StatusOK).Status)
	w.do(t, http.MethodPost, docPath("estimates", est.ID, "/decline"), nil, http.StatusConflict)

	second := w.create(t, "estimates", "2026-12-11")
	w.do(t, http.MethodPost, docPath("estimates", second.ID, "/send"), nil, http.StatusOK)
	assert.Equal(t, "declined", w.do(t, http.MethodPost, docPath("estimates", second.ID, "/decline"), nil, http.StatusOK).Status)

	assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodPost, docPath("estimates", est.ID, "/void"), w.customer, nil).Code)
}

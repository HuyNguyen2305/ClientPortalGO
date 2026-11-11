//go:build integration

package billing_documents_test

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

func TestDocumentCRUD(t *testing.T) {
	for _, tc := range []struct {
		prefix      string
		firstNumber int64
		isInvoice   bool
	}{
		{"invoices", 85000, true},
		{"estimates", 5000, false},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			w := newWorld(t)
			ctx := context.Background()

			created := w.create(t, tc.prefix, "2026-12-10", item("Bi-Monthly Service", 2, 6500), item("Bait", 3, 100))
			assert.Equal(t, "draft", created.Status)
			assert.Equal(t, tc.firstNumber, created.Number)
			assert.Equal(t, int64(13300), created.SubtotalCents)
			assert.Equal(t, int64(13300), created.TotalCents)
			assert.Equal(t, "2026-12-10", created.DocumentDate)

			t.Run("invoice-only money fields", func(t *testing.T) {
				rec := w.app.Do(t, http.MethodGet, docPath(tc.prefix, created.ID, ""), w.customer, nil)
				require.Equal(t, http.StatusOK, rec.Code)
				raw := testapp.Decode[map[string]any](t, rec)
				if tc.isInvoice {
					assert.EqualValues(t, 0, raw["amount_paid_cents"])
					assert.EqualValues(t, 13300, raw["balance_due_cents"])
				} else {
					assert.NotContains(t, raw, "amount_paid_cents")
					assert.NotContains(t, raw, "balance_due_cents")
				}
			})

			t.Run("detail", func(t *testing.T) {
				got := w.do(t, http.MethodGet, docPath(tc.prefix, created.ID, ""), nil, http.StatusOK)
				require.Len(t, got.Items, 2)
				assert.Equal(t, "Bi-Monthly Service", got.Items[0].Name)
				assert.Equal(t, int64(13000), got.Items[0].TotalCents)
				assert.Equal(t, "Bait", got.Items[1].Name)
				assert.Equal(t, int64(300), got.Items[1].TotalCents)
				assert.Equal(t, "San Antonio", got.Location.City)
				assert.Equal(t, w.location, got.Location.ID)
			})

			t.Run("update replaces items", func(t *testing.T) {
				got := w.do(t, http.MethodPut, docPath(tc.prefix, created.ID, ""),
					docBody(w.location, "2026-12-11", item("Bi-Monthly Service", 1, 6500)), http.StatusOK)
				assert.Equal(t, int64(6500), got.TotalCents)
				assert.Len(t, got.Items, 1)
				assert.Equal(t, "2026-12-11", got.DocumentDate)
			})

			t.Run("list rows have a location and no items", func(t *testing.T) {
				rec := w.app.Do(t, http.MethodGet, "/api/v1/"+tc.prefix, w.customer, nil)
				require.Equal(t, http.StatusOK, rec.Code)
				raw := testapp.Decode[map[string][]map[string]any](t, rec)
				require.Len(t, raw["data"], 1)
				assert.Contains(t, raw["data"][0], "location")
				assert.NotContains(t, raw["data"][0], "items")
			})

			t.Run("wrong type route", func(t *testing.T) {
				other := "estimates"
				if !tc.isInvoice {
					other = "invoices"
				}
				w.do(t, http.MethodGet, docPath(other, created.ID, ""), nil, http.StatusNotFound)
			})

			t.Run("another customer", func(t *testing.T) {
				b, bLoc := newCustomer(t, w.app, "B")
				p := docPath(tc.prefix, created.ID, "")
				assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodGet, p, b, nil).Code)
				assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodPut, p, b, docBody(bLoc, "2026-12-10", item("x", 1, 1))).Code)
				assert.Equal(t, http.StatusNotFound, w.app.Do(t, http.MethodDelete, p, b, nil).Code)
				rec := w.app.Do(t, http.MethodGet, "/api/v1/"+tc.prefix, b, nil)
				assert.JSONEq(t, `{"data":[]}`, rec.Body.String())

				// A's request naming B's location.
				rec = w.app.Do(t, http.MethodPost, "/api/v1/"+tc.prefix, w.customer, docBody(bLoc, "2026-12-10", item("x", 1, 1)))
				assert.Equal(t, http.StatusNotFound, rec.Code)
			})

			t.Run("delete a draft", func(t *testing.T) {
				d := w.create(t, tc.prefix, "2026-12-01")
				w.do(t, http.MethodDelete, docPath(tc.prefix, d.ID, ""), nil, http.StatusNoContent)
				w.do(t, http.MethodGet, docPath(tc.prefix, d.ID, ""), nil, http.StatusNotFound)
				var n int
				require.NoError(t, w.app.Pool.QueryRow(ctx,
					`SELECT count(*) FROM billing_document_items WHERE billing_document_id = $1`, d.ID).Scan(&n))
				assert.Zero(t, n)
			})
		})
	}
}

func TestDocumentListPagination(t *testing.T) {
	w := newWorld(t)
	// Five invoices on three days; two pairs share a date.
	for _, date := range []string{"2026-12-01", "2026-12-03", "2026-12-01", "2026-12-02", "2026-12-03"} {
		w.create(t, "invoices", date)
	}

	type row struct {
		date string
		id   int64
	}
	var seen []row
	var sizes []int
	cursor := ""
	for range 10 {
		rec := w.app.Do(t, http.MethodGet, "/api/v1/invoices?limit=2"+cursor, w.customer, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		page := testapp.Decode[dto.Page[dto.DocumentResponse]](t, rec)
		sizes = append(sizes, len(page.Data))
		for _, d := range page.Data {
			seen = append(seen, row{d.DocumentDate, d.ID})
		}
		if page.NextCursor == "" {
			break
		}
		cursor = "&cursor=" + page.NextCursor
	}
	assert.Equal(t, []int{2, 2, 1}, sizes)
	require.Len(t, seen, 5)
	ids := map[int64]bool{}
	for i, r := range seen {
		ids[r.id] = true
		if i > 0 {
			prev := seen[i-1]
			ordered := prev.date > r.date || (prev.date == r.date && prev.id > r.id)
			assert.True(t, ordered, "row %d %v should come after %v", i, r, prev)
		}
	}
	assert.Len(t, ids, 5, "no row repeated or skipped")
}

func TestDocumentListFilters(t *testing.T) {
	w := newWorld(t)
	other := newLocation(t, w.app, w.customer)
	w.create(t, "invoices", "2026-12-01")
	rec := w.app.Do(t, http.MethodPost, "/api/v1/invoices", w.customer, docBody(other, "2026-12-02", item("x", 1, 100)))
	require.Equal(t, http.StatusCreated, rec.Code)
	atOther := testapp.Decode[dto.DocumentResponse](t, rec)
	_, err := w.app.Pool.Exec(context.Background(), `UPDATE billing_documents SET status = 'open' WHERE id = $1`, atOther.ID)
	require.NoError(t, err)

	list := func(query string) []dto.DocumentResponse {
		rec := w.app.Do(t, http.MethodGet, "/api/v1/invoices"+query, w.customer, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return testapp.Decode[dto.Page[dto.DocumentResponse]](t, rec).Data
	}
	byLoc := list(fmt.Sprintf("?location_id=%d", other))
	require.Len(t, byLoc, 1)
	assert.Equal(t, atOther.ID, byLoc[0].ID)

	drafts := list("?status=draft")
	require.Len(t, drafts, 1)
	assert.Equal(t, "draft", drafts[0].Status)

	assert.Len(t, list(""), 2)
}

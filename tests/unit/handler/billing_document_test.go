package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"clientportal/internal/handler"
	"clientportal/internal/model"
	"clientportal/internal/service"
	"clientportal/tests/unit/mocks"
)

// docRouter builds the router with every repository mocked. Only the customer lookup in the
// header middleware may be called; any other repository call fails the test.
func docRouter(t *testing.T) http.Handler {
	customers := mocks.NewMockCustomerRepository(t)
	customers.EXPECT().Get(mock.Anything, int64(1)).Return(model.Customer{ID: 1}, nil).Maybe()
	locs := mocks.NewMockLocationRepository(t)
	docs := mocks.NewMockBillingDocumentRepository(t)
	services := mocks.NewMockServiceRepository(t)
	hours := model.Hours{Loc: time.UTC, Open: 7 * 60, Close: 17 * 60, Step: 15, HorizonDays: 90}
	return handler.NewRouter(handler.Deps{
		Log:       quietLog(),
		Customers: service.NewCustomerService(customers),
		Locations: service.NewLocationService(locs),
		Documents: service.NewBillingDocumentService(docs, locs, time.Now),
		Catalog:   service.NewCatalog(services),
		Bookings:  service.NewBookingService(docs, locs, services, hours, time.Now),
	})
}

func sendAs(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Customer-ID", "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// body returns a valid create body with one field of the given item or document replaced.
func body(t *testing.T, mutate func(doc map[string]any, item map[string]any)) string {
	item := map[string]any{"name": "Service", "quantity": 1, "unit_price_cents": 6500}
	doc := map[string]any{"location_id": 7, "title": "Bi-Monthly Service", "document_date": "2026-12-10"}
	mutate(doc, item)
	if _, ok := doc["items"]; !ok {
		doc["items"] = []any{item}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBillingDocumentHandler_RejectsBadBodies(t *testing.T) {
	many := make([]any, 101)
	for i := range many {
		many[i] = map[string]any{"name": "x", "quantity": 1, "unit_price_cents": 1}
	}
	cases := map[string]func(doc, item map[string]any){
		"items missing":         func(doc, _ map[string]any) { doc["items"] = nil },
		"items empty":           func(doc, _ map[string]any) { doc["items"] = []any{} },
		"101 items":             func(doc, _ map[string]any) { doc["items"] = many },
		"quantity zero":         func(_, item map[string]any) { item["quantity"] = 0 },
		"quantity too big":      func(_, item map[string]any) { item["quantity"] = 1000001 },
		"unit price missing":    func(_, item map[string]any) { delete(item, "unit_price_cents") },
		"unit price negative":   func(_, item map[string]any) { item["unit_price_cents"] = -1 },
		"unit price too big":    func(_, item map[string]any) { item["unit_price_cents"] = int64(10000000001) },
		"impossible date":       func(doc, _ map[string]any) { doc["document_date"] = "2026-13-40" },
		"us-style date":         func(doc, _ map[string]any) { doc["document_date"] = "12/10/2026" },
		"date missing":          func(doc, _ map[string]any) { delete(doc, "document_date") },
		"title missing":         func(doc, _ map[string]any) { delete(doc, "title") },
		"location missing":      func(doc, _ map[string]any) { delete(doc, "location_id") },
		"item name missing":     func(_, item map[string]any) { delete(item, "name") },
		"location not a number": func(doc, _ map[string]any) { doc["location_id"] = "seven" },
	}
	for _, prefix := range []string{"/api/v1/invoices", "/api/v1/estimates"} {
		for name, mutate := range cases {
			t.Run(prefix+" "+name, func(t *testing.T) {
				rec := sendAs(docRouter(t), http.MethodPost, prefix, body(t, mutate))
				assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			})
		}
	}
}

func TestBillingDocumentHandler_AcceptsZeroUnitPrice(t *testing.T) {
	// A zero price passes binding and reaches the location check, which the mock rejects.
	customers := mocks.NewMockCustomerRepository(t)
	customers.EXPECT().Get(mock.Anything, int64(1)).Return(model.Customer{ID: 1}, nil)
	locs := mocks.NewMockLocationRepository(t)
	locs.EXPECT().Get(mock.Anything, int64(1), int64(7)).Return(model.Location{}, model.ErrNotFound)
	r := handler.NewRouter(handler.Deps{
		Log:       quietLog(),
		Customers: service.NewCustomerService(customers),
		Locations: service.NewLocationService(locs),
		Documents: service.NewBillingDocumentService(mocks.NewMockBillingDocumentRepository(t), locs, time.Now),
	})
	rec := sendAs(r, http.MethodPost, "/api/v1/invoices", body(t, func(_, item map[string]any) { item["unit_price_cents"] = 0 }))
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestPaymentHandler_RejectsBadAmounts(t *testing.T) {
	for _, b := range []string{`{"amount_cents":0}`, `{"amount_cents":-5}`, `{}`, `{"amount_cents":"ten"}`} {
		t.Run(b, func(t *testing.T) {
			rec := sendAs(docRouter(t), http.MethodPost, "/api/v1/invoices/9/payments", b)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestBillingDocumentHandler_RejectsBadQueries(t *testing.T) {
	paths := []string{
		"/api/v1/invoices/abc",
		"/api/v1/invoices?limit=0",
		"/api/v1/invoices?location_id=x",
		"/api/v1/invoices?cursor=!!!",
		"/api/v1/invoices?status=accepted",
		"/api/v1/estimates?status=paid",
		"/api/v1/estimates?status=bogus",
		"/api/v1/estimates/0",
		"/api/v1/balance?location_id=x",
		"/api/v1/invoices/abc/payments",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			rec := sendAs(docRouter(t), http.MethodGet, p, "")
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

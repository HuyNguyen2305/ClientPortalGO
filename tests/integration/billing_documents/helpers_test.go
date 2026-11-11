//go:build integration

package billing_documents_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

// world is one customer with one location on a fresh API.
type world struct {
	app      *testapp.App
	customer int64
	location int64
}

func newCustomer(t *testing.T, app *testapp.App, name string) (customerID, locationID int64) {
	t.Helper()
	rec := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code)
	customerID = testapp.Decode[dto.CustomerResponse](t, rec).ID
	locationID = newLocation(t, app, customerID)
	return customerID, locationID
}

func newLocation(t *testing.T, app *testapp.App, customerID int64) int64 {
	t.Helper()
	rec := app.Do(t, http.MethodPost, "/api/v1/locations", customerID, map[string]any{
		"street": "West Loop 1604 South", "city": "San Antonio", "state": "TX", "postal_code": "12341",
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return testapp.Decode[dto.LocationResponse](t, rec).ID
}

func newWorld(t *testing.T) world {
	t.Helper()
	app := testapp.New(t)
	c, l := newCustomer(t, app, "William Saliba")
	return world{app: app, customer: c, location: l}
}

func item(name string, qty int, cents int64) map[string]any {
	return map[string]any{"name": name, "description": "General maintenance", "quantity": qty, "unit_price_cents": cents}
}

func docBody(location int64, date string, items ...map[string]any) map[string]any {
	return map[string]any{"location_id": location, "title": "Bi-Monthly Service", "document_date": date, "items": items}
}

// create posts a document and returns it; prefix is "invoices" or "estimates".
func (w world) create(t *testing.T, prefix, date string, items ...map[string]any) dto.DocumentResponse {
	t.Helper()
	if len(items) == 0 {
		items = []map[string]any{item("Bi-Monthly Service", 1, 6500)}
	}
	rec := w.app.Do(t, http.MethodPost, "/api/v1/"+prefix, w.customer, docBody(w.location, date, items...))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return testapp.Decode[dto.DocumentResponse](t, rec)
}

// do sends a request as the world's customer and requires the status.
func (w world) do(t *testing.T, method, path string, body any, want int) dto.DocumentResponse {
	t.Helper()
	rec := w.app.Do(t, method, path, w.customer, body)
	require.Equal(t, want, rec.Code, "%s %s: %s", method, path, rec.Body.String())
	if want >= 300 || rec.Body.Len() == 0 {
		return dto.DocumentResponse{}
	}
	return testapp.Decode[dto.DocumentResponse](t, rec)
}

func docPath(prefix string, id int64, suffix string) string {
	return fmt.Sprintf("/api/v1/%s/%d%s", prefix, id, suffix)
}

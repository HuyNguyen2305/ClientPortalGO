package handler_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"clientportal/internal/handler"
	"clientportal/internal/service"
	"clientportal/tests/unit/mocks"
)

func TestServiceHandler_RejectsBadInput(t *testing.T) {
	tests := []struct{ name, method, path, body string }{
		{"empty body", http.MethodPost, "/api/v1/services", `{}`},
		{"duration missing", http.MethodPost, "/api/v1/services", `{"name":"x","price_cents":100}`},
		{"price missing", http.MethodPost, "/api/v1/services", `{"name":"x","duration_minutes":30}`},
		{"negative price", http.MethodPost, "/api/v1/services", `{"name":"x","duration_minutes":30,"price_cents":-1}`},
		{"duration as text", http.MethodPost, "/api/v1/services", `{"name":"x","duration_minutes":"30","price_cents":1}`},
		{"bad repeat unit", http.MethodPost, "/api/v1/services", `{"name":"x","duration_minutes":30,"price_cents":1,"repeat":{"unit":"hourly"}}`},
		{"bad ends date", http.MethodPost, "/api/v1/services", `{"name":"x","duration_minutes":30,"price_cents":1,"repeat":{"unit":"daily","ends":"on_date","ends_on":"soon"}}`},
		{"bad color", http.MethodPost, "/api/v1/services", `{"name":"x","duration_minutes":30,"price_cents":1,"color":"blue"}`},
		{"item without a name", http.MethodPost, "/api/v1/services", `{"name":"x","duration_minutes":30,"price_cents":1,"items":[{"quantity":1,"unit_price_cents":5}]}`},
		{"bad archive id", http.MethodPost, "/api/v1/services/abc/archive", ""},
		{"bad status filter", http.MethodGet, "/api/v1/admin/services?status=gone", ""},
		{"bad id", http.MethodGet, "/api/v1/services/abc", ""},
		{"bad id on update", http.MethodPut, "/api/v1/services/0", `{"name":"x","duration_minutes":30,"price_cents":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := handler.NewRouter(handler.Deps{Log: quietLog(), AdminKey: testAdminKey, Catalog: service.NewCatalog(mocks.NewMockServiceRepository(t))})
			rec := send(r, tt.method, tt.path, tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

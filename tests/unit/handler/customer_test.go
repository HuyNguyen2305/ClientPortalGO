package handler_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"clientportal/internal/handler"
	"clientportal/internal/model"
	"clientportal/internal/service"
	"clientportal/tests/unit/mocks"
)

const testAdminKey = "test-admin-key"

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func customerRouter(t *testing.T) (*gin.Engine, *mocks.MockCustomerRepository) {
	repo := mocks.NewMockCustomerRepository(t)
	r := handler.NewRouter(handler.Deps{Log: quietLog(), AdminKey: testAdminKey, Customers: service.NewCustomerService(repo)})
	return r, repo
}

func send(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", testAdminKey)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestCustomerHandler_RejectsOversizedBody(t *testing.T) {
	r, _ := customerRouter(t) // the mock fails the test if the repository is called
	rec := send(r, http.MethodPost, "/api/v1/customers", `{"name":"`+strings.Repeat("a", 2<<20)+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCustomerHandler_BindErrorsHideValidatorInternals(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"failed rule names the field", `{}`, `Name failed the \"required\" rule`},
		{"malformed json", `{"name":`, "body is not valid JSON of the expected shape"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := customerRouter(t)
			rec := send(r, http.MethodPost, "/api/v1/customers", tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
			assert.NotContains(t, rec.Body.String(), "Key:")
		})
	}
}

func TestCustomerHandler_RejectsBadInput(t *testing.T) {
	tests := []struct {
		name, method, path, body string
	}{
		{"empty body", http.MethodPost, "/api/v1/customers", `{}`},
		{"bad email", http.MethodPost, "/api/v1/customers", `{"name":"A","email":"nope"}`},
		{"malformed json", http.MethodPost, "/api/v1/customers", `{"name":`},
		{"non-numeric id", http.MethodGet, "/api/v1/customers/abc", ""},
		{"limit zero", http.MethodGet, "/api/v1/customers?limit=0", ""},
		{"limit too big", http.MethodGet, "/api/v1/customers?limit=101", ""},
		{"limit not a number", http.MethodGet, "/api/v1/customers?limit=x", ""},
		{"bad cursor", http.MethodGet, "/api/v1/customers?cursor=x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := customerRouter(t) // the mock fails the test if the repository is called
			rec := send(r, tt.method, tt.path, tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestCustomerHandler_Create(t *testing.T) {
	r, repo := customerRouter(t)
	repo.EXPECT().Create(mock.Anything, mock.Anything).Return(model.Customer{ID: 1, AccountNumber: 5000, Name: "A"}, nil)
	rec := send(r, http.MethodPost, "/api/v1/customers", `{"name":"A"}`)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"account_number":5000`)
}

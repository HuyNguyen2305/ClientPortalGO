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

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func customerRouter(t *testing.T) (*gin.Engine, *mocks.MockCustomerRepository) {
	repo := mocks.NewMockCustomerRepository(t)
	r := handler.NewRouter(handler.Deps{Log: quietLog(), Customers: service.NewCustomerService(repo)})
	return r, repo
}

func send(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
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

package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"clientportal/internal/handler"
	"clientportal/internal/middleware"
	"clientportal/internal/model"
)

func TestRequireCustomer(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		existsErr  error
		wantCalled bool
		wantStatus int
		wantCode   string
	}{
		{"absent", "", nil, false, http.StatusBadRequest, "invalid_request"},
		{"not a number", "abc", nil, false, http.StatusBadRequest, "invalid_request"},
		{"zero", "0", nil, false, http.StatusBadRequest, "invalid_request"},
		{"negative", "-1", nil, false, http.StatusBadRequest, "invalid_request"},
		{"unknown customer", "7", fmt.Errorf("get customer: %w", model.ErrNotFound), true, http.StatusNotFound, "not_found"},
		{"known customer", "7", nil, true, http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			exists := func(_ context.Context, id int64) error {
				called = true
				assert.Equal(t, int64(7), id)
				return tt.existsErr
			}
			var seen int64
			r := gin.New()
			r.GET("/", middleware.RequireCustomer(exists, handler.RespondError), func(c *gin.Context) {
				seen = middleware.CustomerID(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("X-Customer-ID", tt.header)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCalled, called)
			if tt.wantCode != "" {
				assert.Contains(t, rec.Body.String(), `"code":"`+tt.wantCode+`"`)
			} else {
				assert.Equal(t, int64(7), seen)
			}
		})
	}
}

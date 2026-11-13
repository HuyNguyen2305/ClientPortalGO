package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"clientportal/internal/handler"
	"clientportal/internal/middleware"
)

func TestRequireAdminKey(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		header     string
		wantCalled bool
		wantStatus int
	}{
		{"absent", "secret", "", false, http.StatusUnauthorized},
		{"wrong", "secret", "nope", false, http.StatusUnauthorized},
		{"prefix of the key", "secret", "sec", false, http.StatusUnauthorized},
		{"right", "secret", "secret", true, http.StatusOK},
		{"unset key rejects an absent header", "", "", false, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			r := gin.New()
			r.GET("/", middleware.RequireAdminKey(tt.key, handler.RespondError), func(c *gin.Context) {
				called = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("X-Admin-Key", tt.header)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCalled, called)
		})
	}
}

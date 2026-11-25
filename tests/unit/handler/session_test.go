package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"clientportal/internal/handler"
	"clientportal/internal/middleware"
	"clientportal/internal/model"
)

func TestRequireSession(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		authErr    error
		wantCalled bool
		wantStatus int
	}{
		{"absent", "", nil, false, http.StatusUnauthorized},
		{"wrong scheme", "Basic abc", nil, false, http.StatusUnauthorized},
		{"empty token", "Bearer ", nil, false, http.StatusUnauthorized},
		{"unknown token", "Bearer abc", fmt.Errorf("%w: invalid or expired token", model.ErrUnauthorized), true, http.StatusUnauthorized},
		{"valid token", "Bearer abc", nil, true, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			authenticate := func(_ context.Context, token string) (int64, error) {
				called = true
				assert.Equal(t, "abc", token)
				return 7, tt.authErr
			}
			var id int64
			var token string
			r := gin.New()
			r.GET("/", middleware.RequireSession(authenticate, handler.RespondError), func(c *gin.Context) {
				id, token = middleware.CustomerID(c), middleware.Token(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			// The old header must not open a customer route.
			req.Header.Set("X-Customer-ID", "7")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCalled, called)
			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, int64(7), id)
				assert.Equal(t, "abc", token)
			}
		})
	}
}

func TestRateLimitWindowExpires(t *testing.T) {
	r := gin.New()
	r.GET("/", middleware.RateLimit(1, 30*time.Millisecond, handler.RespondError), func(c *gin.Context) { c.Status(http.StatusOK) })
	send := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1000"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusOK, send())
	assert.Equal(t, http.StatusTooManyRequests, send())
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, http.StatusOK, send(), "a spent window starts a fresh allowance")
}

func TestRateLimit(t *testing.T) {
	r := gin.New()
	r.GET("/", middleware.RateLimit(2, time.Hour, handler.RespondError), func(c *gin.Context) { c.Status(http.StatusOK) })

	send := func(addr string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusOK, send("1.2.3.4:1000"))
	assert.Equal(t, http.StatusOK, send("1.2.3.4:1001"))
	assert.Equal(t, http.StatusTooManyRequests, send("1.2.3.4:1002"))
	// Another client has its own allowance.
	assert.Equal(t, http.StatusOK, send("5.6.7.8:1000"))
}

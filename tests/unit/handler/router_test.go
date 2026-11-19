package handler_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/handler"
)

func newRouterDeps(proxies []string) handler.Deps {
	return handler.Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), TrustedProxies: proxies}
}

// loginFrom posts an empty body to the rate-limited login route; an empty body is a 400 until the limit trips.
func loginFrom(r http.Handler, peer, forwardedFor string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(""))
	req.RemoteAddr = peer
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func TestRouterIgnoresForgedForwardedFor(t *testing.T) {
	r := handler.NewRouter(newRouterDeps(nil))
	var last int
	for i := range 11 {
		last = loginFrom(r, "1.2.3.4:1000", fmt.Sprintf("9.9.9.%d", i))
	}
	assert.Equal(t, http.StatusTooManyRequests, last, "a new fake IP per request must not get a new allowance")
}

func TestRouterTrustsListedProxy(t *testing.T) {
	r := handler.NewRouter(newRouterDeps([]string{"10.0.0.1"}))
	// Eleven different clients behind the trusted proxy each have their own allowance.
	for i := range 11 {
		require.Equal(t, http.StatusBadRequest, loginFrom(r, "10.0.0.1:1000", fmt.Sprintf("9.9.9.%d", i)))
	}
	// One client behind it is still limited.
	var last int
	for range 11 {
		last = loginFrom(r, "10.0.0.1:1000", "8.8.8.8")
	}
	assert.Equal(t, http.StatusTooManyRequests, last)
}

func TestRouterRejectsInvalidTrustedProxy(t *testing.T) {
	assert.Panics(t, func() { handler.NewRouter(newRouterDeps([]string{"not-an-ip"})) })
}

func TestRouterPanicAnswersStandardError(t *testing.T) {
	d := newRouterDeps(nil) // a nil Ping makes /healthz panic
	r := handler.NewRouter(d)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"internal","message":"internal server error"}}`, rec.Body.String())
}

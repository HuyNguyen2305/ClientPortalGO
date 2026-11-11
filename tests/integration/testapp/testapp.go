//go:build integration

// Package testapp runs the real API wiring against a throwaway database.
package testapp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"clientportal/internal/app"
	"clientportal/tests/integration/testdb"
)

// App is the API under test plus direct database access for setup and assertions.
type App struct {
	Handler http.Handler
	Pool    *pgxpool.Pool
}

// New starts the API on a fresh, migrated database.
func New(t *testing.T) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pool := testdb.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &App{Handler: app.New(pool, 5*time.Second, log), Pool: pool}
}

// Do sends a request. customerID 0 sends no X-Customer-ID header; body nil sends no body.
func (a *App) Do(t *testing.T, method, path string, customerID int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		if raw, ok := body.(string); ok {
			r = bytes.NewBufferString(raw)
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			r = bytes.NewReader(b)
		}
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if customerID != 0 {
		req.Header.Set("X-Customer-ID", strconv.FormatInt(customerID, 10))
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	return rec
}

// Decode unmarshals the response body.
func Decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

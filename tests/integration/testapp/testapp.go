//go:build integration

// Package testapp runs the real API wiring against a throwaway database.
package testapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"clientportal/internal/app"
	"clientportal/internal/config"
	"clientportal/internal/model"
	"clientportal/tests/integration/testdb"
)

// App is the API under test plus direct database access for setup and assertions.
type App struct {
	Handler http.Handler
	Pool    *pgxpool.Pool
	Gateway *FakeGateway
	Mailer  *FakeMailer
}

// Mail is one email the fake mailer was asked to send.
type Mail struct{ To, Subject, Body string }

// FakeMailer records emails instead of sending them.
type FakeMailer struct{ Sent []Mail }

func (m *FakeMailer) Send(_ context.Context, to, subject, body string) error {
	m.Sent = append(m.Sent, Mail{to, subject, body})
	return nil
}

// ResetURL is the reset link base the test API puts in emails.
const ResetURL = "https://portal.test/reset"

// LastToken returns the reset token in the most recent email, failing the test when there is none.
func (m *FakeMailer) LastToken(t *testing.T) string {
	t.Helper()
	if len(m.Sent) == 0 {
		t.Fatal("no email was sent")
	}
	_, token, ok := strings.Cut(m.Sent[len(m.Sent)-1].Body, ResetURL+"?token=")
	if !ok {
		t.Fatalf("no reset link in %q", m.Sent[len(m.Sent)-1].Body)
	}
	token, _, _ = strings.Cut(token, "\n")
	return token
}

// Hours are the opening hours the test API runs with: 07:00 to 17:00 every day, a 15-minute
// grid, 90 days ahead, Chicago time.
var Hours = func() model.Hours {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		panic(err)
	}
	return model.Hours{Loc: loc, Open: 7 * 60, Close: 17 * 60, Step: 15, HorizonDays: 90}
}()

// New starts the API on a fresh, migrated database.
func New(t *testing.T) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pool := testdb.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{AdminAPIKey: AdminKey, QueryTimeout: 5 * time.Second, SessionTTL: time.Hour, ResetURLBase: ResetURL, Hours: Hours, MaxLiveBookings: 3}
	gw := &FakeGateway{Cards: map[string][]model.Card{}, Charges: map[string]int64{}}
	mailer := &FakeMailer{}
	return &App{Handler: app.New(pool, cfg, log, gw, mailer), Pool: pool, Gateway: gw, Mailer: mailer}
}

// FakeGateway is an in-memory payment gateway: no network, no Stripe key.
type FakeGateway struct {
	Cards   map[string][]model.Card // by gateway customer id
	Charges map[string]int64        // amount by idempotency key: a repeated key charges once, like Stripe
	Decline bool                    // makes every new charge fail
}

func (g *FakeGateway) Charge(_ context.Context, _, _ string, amountCents int64, key string) error {
	if _, seen := g.Charges[key]; seen {
		return nil
	}
	if g.Decline {
		return fmt.Errorf("%w: card declined", model.ErrPaymentFailed)
	}
	g.Charges[key] = amountCents
	return nil
}

func (g *FakeGateway) CreateCustomer(_ context.Context, customerID int64) (string, error) {
	return "cus_" + strconv.FormatInt(customerID, 10), nil
}

func (g *FakeGateway) CreateSetupIntent(_ context.Context, gid string) (string, error) {
	return "seti_secret_" + gid, nil
}

func (g *FakeGateway) ListCards(_ context.Context, gid string) ([]model.Card, error) {
	return g.Cards[gid], nil
}

func (g *FakeGateway) DetachCard(_ context.Context, cardID string) error {
	for gid, cards := range g.Cards {
		for i, c := range cards {
			if c.ID == cardID {
				g.Cards[gid] = append(cards[:i:i], cards[i+1:]...)
			}
		}
	}
	return nil
}

// AdminKey is the X-Admin-Key the test API accepts; Do sends it on every request.
const AdminKey = "test-admin-key"

// Do sends a request. customerID 0 sends no X-Customer-ID header; body nil sends no body.
func (a *App) Do(t *testing.T, method, path string, customerID int64, body any) *httptest.ResponseRecorder {
	t.Helper()
	return a.DoWith(t, method, path, customerID, body, nil)
}

// DoWith is Do plus extra request headers.
func (a *App) DoWith(t *testing.T, method, path string, customerID int64, body any, headers map[string]string) *httptest.ResponseRecorder {
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
	req.Header.Set("X-Admin-Key", AdminKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if customerID != 0 {
		// Customer routes read the bearer token, staff routes read X-Customer-ID; send both.
		req.Header.Set("X-Customer-ID", strconv.FormatInt(customerID, 10))
		req.Header.Set("Authorization", "Bearer "+a.Session(t, customerID, time.Hour))
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	return rec
}

// Session inserts a login session for the customer straight into the database and returns its token.
// A negative ttl makes it already expired.
func (a *App) Session(t *testing.T, customerID int64, ttl time.Duration) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("token: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	_, err := a.Pool.Exec(context.Background(),
		`INSERT INTO sessions (token_hash, customer_id, expires_at) VALUES ($1, $2, now() + $3 * interval '1 second')`,
		sum[:], customerID, int64(ttl/time.Second))
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return token
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

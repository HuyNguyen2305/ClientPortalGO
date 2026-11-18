//go:build integration

package auth_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/tests/integration/testapp"
)

const newPassword = "brand new pass"

func requestReset(t *testing.T, app *testapp.App, email string) int {
	t.Helper()
	return app.DoWith(t, http.MethodPost, "/api/v1/auth/password-reset", 0, map[string]any{"email": email}, nil).Code
}

func confirmReset(t *testing.T, app *testapp.App, token, pw string) int {
	t.Helper()
	return app.DoWith(t, http.MethodPost, "/api/v1/auth/password-reset/confirm", 0,
		map[string]any{"token": token, "new_password": pw}, nil).Code
}

func loginCode(t *testing.T, app *testapp.App, email, pw string) int {
	t.Helper()
	return app.DoWith(t, http.MethodPost, "/api/v1/auth/login", 0, map[string]any{"email": email, "password": pw}, nil).Code
}

func TestResetFlow(t *testing.T) {
	app := testapp.New(t)
	got := register(t, app, "ada@example.com")

	require.Equal(t, http.StatusAccepted, requestReset(t, app, "ada@example.com"))
	require.Len(t, app.Mailer.Sent, 1)
	assert.Equal(t, "ada@example.com", app.Mailer.Sent[0].To)
	token := app.Mailer.LastToken(t)

	require.Equal(t, http.StatusNoContent, confirmReset(t, app, token, newPassword))

	assert.Equal(t, http.StatusUnauthorized, loginCode(t, app, "ada@example.com", password), "old password is gone")
	assert.Equal(t, http.StatusOK, loginCode(t, app, "ada@example.com", newPassword))
	assert.Equal(t, http.StatusUnauthorized, get(t, app, "/api/v1/invoices", got.Token), "old sessions end")
}

func TestResetTokenIsSingleUse(t *testing.T) {
	app := testapp.New(t)
	register(t, app, "ada@example.com")
	requestReset(t, app, "ada@example.com")
	token := app.Mailer.LastToken(t)

	require.Equal(t, http.StatusNoContent, confirmReset(t, app, token, newPassword))
	assert.Equal(t, http.StatusBadRequest, confirmReset(t, app, token, "another new pass"))
	assert.Equal(t, http.StatusOK, loginCode(t, app, "ada@example.com", newPassword), "second use changed nothing")
}

func TestResetRejectsBadTokens(t *testing.T) {
	app := testapp.New(t)
	got := register(t, app, "ada@example.com")
	assert.Equal(t, http.StatusBadRequest, confirmReset(t, app, "garbage", newPassword))

	// An expired token, inserted directly, is refused.
	sum := sha256.Sum256([]byte("expired-token"))
	_, err := app.Pool.Exec(context.Background(),
		`INSERT INTO password_resets (token_hash, customer_id, expires_at) VALUES ($1, $2, now() - interval '1 minute')`,
		sum[:], got.Customer.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, confirmReset(t, app, "expired-token", newPassword))

	// A weak new password is refused and does not spend the token.
	requestReset(t, app, "ada@example.com")
	token := app.Mailer.LastToken(t)
	assert.Equal(t, http.StatusBadRequest, confirmReset(t, app, token, "short"))
	assert.Equal(t, http.StatusNoContent, confirmReset(t, app, token, newPassword))
}

func TestResetForUnknownEmailLooksTheSame(t *testing.T) {
	app := testapp.New(t)
	assert.Equal(t, http.StatusAccepted, requestReset(t, app, "nobody@example.com"))
	assert.Empty(t, app.Mailer.Sent)
}

func TestNewestResetLinkWins(t *testing.T) {
	app := testapp.New(t)
	register(t, app, "ada@example.com")
	requestReset(t, app, "ada@example.com")
	first := app.Mailer.LastToken(t)
	requestReset(t, app, "ada@example.com")
	second := app.Mailer.LastToken(t)
	require.NotEqual(t, first, second)

	assert.Equal(t, http.StatusBadRequest, confirmReset(t, app, first, newPassword))
	assert.Equal(t, http.StatusNoContent, confirmReset(t, app, second, newPassword))
}

func TestStaffCreatedCustomerCanSetAPasswordByReset(t *testing.T) {
	app := testapp.New(t)
	rec := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": "Walk-in", "email": "walkin@example.com"})
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, http.StatusUnauthorized, loginCode(t, app, "walkin@example.com", newPassword))

	requestReset(t, app, "walkin@example.com")
	require.Equal(t, http.StatusNoContent, confirmReset(t, app, app.Mailer.LastToken(t), newPassword))
	assert.Equal(t, http.StatusOK, loginCode(t, app, "walkin@example.com", newPassword))
}

func TestResetEndpointsAreRateLimited(t *testing.T) {
	app := testapp.New(t)
	var last int
	for range 12 {
		last = requestReset(t, app, "x@example.com")
	}
	assert.Equal(t, http.StatusTooManyRequests, last)
}

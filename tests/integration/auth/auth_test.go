//go:build integration

package auth_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/tests/integration/testapp"
)

const password = "correct horse"

func register(t *testing.T, app *testapp.App, email string) dto.AuthResponse {
	t.Helper()
	rec := app.DoWith(t, http.MethodPost, "/api/v1/auth/register", 0,
		map[string]any{"name": "Ada", "email": email, "password": password}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return testapp.Decode[dto.AuthResponse](t, rec)
}

// get calls a customer route with only a bearer token (and no admin key, like a real customer).
func get(t *testing.T, app *testapp.App, path, token string) int {
	t.Helper()
	rec := app.DoWith(t, http.MethodGet, path, 0, nil, map[string]string{"X-Admin-Key": "", "Authorization": "Bearer " + token})
	return rec.Code
}

func TestRegisterThenUseCustomerRoutes(t *testing.T) {
	app := testapp.New(t)
	got := register(t, app, "ada@example.com")
	assert.NotEmpty(t, got.Token)
	assert.Equal(t, "Ada", got.Customer.Name)

	assert.Equal(t, http.StatusOK, get(t, app, "/api/v1/invoices", got.Token))
	assert.Equal(t, http.StatusOK, get(t, app, "/api/v1/locations", got.Token))
}

func TestCustomerRoutesNeedAToken(t *testing.T) {
	app := testapp.New(t)
	got := register(t, app, "ada@example.com")

	assert.Equal(t, http.StatusUnauthorized, get(t, app, "/api/v1/invoices", ""))
	assert.Equal(t, http.StatusUnauthorized, get(t, app, "/api/v1/invoices", "garbage"))

	// The old header alone no longer identifies a customer.
	rec := app.DoWith(t, http.MethodGet, "/api/v1/invoices", 0, nil,
		map[string]string{"X-Customer-ID": strconv.FormatInt(got.Customer.ID, 10)})
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	expired := app.Session(t, got.Customer.ID, -time.Hour)
	assert.Equal(t, http.StatusUnauthorized, get(t, app, "/api/v1/invoices", expired))
}

func TestStaffRoutesStillUseAdminKeyAndHeader(t *testing.T) {
	app := testapp.New(t)
	got := register(t, app, "ada@example.com")
	rec := app.DoWith(t, http.MethodGet, "/api/v1/customers/"+strconv.FormatInt(got.Customer.ID, 10), 0, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestDuplicateEmailIsConflictIgnoringCase(t *testing.T) {
	app := testapp.New(t)
	register(t, app, "ada@example.com")
	rec := app.DoWith(t, http.MethodPost, "/api/v1/auth/register", 0,
		map[string]any{"name": "Other", "email": "ADA@Example.com", "password": password}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestRegisterValidation(t *testing.T) {
	app := testapp.New(t)
	for name, body := range map[string]map[string]any{
		"short password": {"name": "Ada", "email": "a@example.com", "password": "short"},
		"bad email":      {"name": "Ada", "email": "nope", "password": password},
		"no name":        {"email": "a@example.com", "password": password},
	} {
		t.Run(name, func(t *testing.T) {
			rec := app.DoWith(t, http.MethodPost, "/api/v1/auth/register", 0, body, nil)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}

func TestLogin(t *testing.T) {
	app := testapp.New(t)
	register(t, app, "ada@example.com")
	login := func(email, pw string) *http.Response {
		rec := app.DoWith(t, http.MethodPost, "/api/v1/auth/login", 0, map[string]any{"email": email, "password": pw}, nil)
		return &http.Response{StatusCode: rec.Code, Body: nil, Header: http.Header{"X-Body": {rec.Body.String()}}}
	}

	assert.Equal(t, http.StatusOK, login("ada@example.com", password).StatusCode)
	assert.Equal(t, http.StatusOK, login("ADA@example.com", password).StatusCode)

	wrong := login("ada@example.com", "wrong password")
	unknown := login("nobody@example.com", password)
	assert.Equal(t, http.StatusUnauthorized, wrong.StatusCode)
	assert.Equal(t, http.StatusUnauthorized, unknown.StatusCode)
	assert.Equal(t, wrong.Header.Get("X-Body"), unknown.Header.Get("X-Body"), "the two failures must look identical")
}

func TestStaffCreatedCustomerCannotLogInOrBeClaimed(t *testing.T) {
	app := testapp.New(t)
	rec := app.Do(t, http.MethodPost, "/api/v1/customers", 0, map[string]any{"name": "Walk-in", "email": "walkin@example.com"})
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = app.DoWith(t, http.MethodPost, "/api/v1/auth/login", 0, map[string]any{"email": "walkin@example.com", "password": password}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = app.DoWith(t, http.MethodPost, "/api/v1/auth/register", 0,
		map[string]any{"name": "Thief", "email": "walkin@example.com", "password": password}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestLogoutEndsTheSession(t *testing.T) {
	app := testapp.New(t)
	got := register(t, app, "ada@example.com")
	rec := app.DoWith(t, http.MethodPost, "/api/v1/auth/logout", 0, nil, map[string]string{"Authorization": "Bearer " + got.Token})
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, http.StatusUnauthorized, get(t, app, "/api/v1/invoices", got.Token))
}

func TestChangePassword(t *testing.T) {
	app := testapp.New(t)
	current := register(t, app, "ada@example.com")
	other := app.Session(t, current.Customer.ID, time.Hour) // a second device

	change := func(old, next string) int {
		return app.DoWith(t, http.MethodPost, "/api/v1/auth/password", 0,
			map[string]any{"old_password": old, "new_password": next},
			map[string]string{"Authorization": "Bearer " + current.Token}).Code
	}
	assert.Equal(t, http.StatusUnauthorized, change("not the password", "brand new pass"))
	assert.Equal(t, http.StatusBadRequest, change(password, "short"))
	require.Equal(t, http.StatusNoContent, change(password, "brand new pass"))

	assert.Equal(t, http.StatusOK, get(t, app, "/api/v1/invoices", current.Token), "this session survives")
	assert.Equal(t, http.StatusUnauthorized, get(t, app, "/api/v1/invoices", other), "other sessions end")

	rec := app.DoWith(t, http.MethodPost, "/api/v1/auth/login", 0, map[string]any{"email": "ada@example.com", "password": password}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "old password no longer works")
	rec = app.DoWith(t, http.MethodPost, "/api/v1/auth/login", 0, map[string]any{"email": "ada@example.com", "password": "brand new pass"}, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestChangePasswordIsRateLimited(t *testing.T) {
	app := testapp.New(t)
	got := register(t, app, "ada@example.com")
	var last int
	for range 12 {
		last = app.DoWith(t, http.MethodPost, "/api/v1/auth/password", 0,
			map[string]any{"old_password": "not the password", "new_password": "brand new pass"},
			map[string]string{"Authorization": "Bearer " + got.Token}).Code
	}
	assert.Equal(t, http.StatusTooManyRequests, last)
}

func TestAuthEndpointsAreRateLimited(t *testing.T) {
	app := testapp.New(t)
	var last int
	for range 12 {
		last = app.DoWith(t, http.MethodPost, "/api/v1/auth/login", 0, map[string]any{"email": "x@example.com", "password": "whatever pass"}, nil).Code
	}
	assert.Equal(t, http.StatusTooManyRequests, last)
}

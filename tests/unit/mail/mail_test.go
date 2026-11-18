package mail_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/mail"
)

func TestResend_Send(t *testing.T) {
	var gotAuth, gotType string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &got))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := mail.NewResend("key123", "Portal <no-reply@example.com>", srv.URL).Send(context.Background(), "ada@example.com", "Hi", "body text")
	require.NoError(t, err)
	assert.Equal(t, "Bearer key123", gotAuth)
	assert.Equal(t, "application/json", gotType)
	assert.Equal(t, "Portal <no-reply@example.com>", got["from"])
	assert.Equal(t, []any{"ada@example.com"}, got["to"])
	assert.Equal(t, "Hi", got["subject"])
	assert.Equal(t, "body text", got["text"])
}

func TestResend_SendProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := mail.NewResend("k", "f@example.com", srv.URL).Send(context.Background(), "a@example.com", "s", "b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestResend_SendUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	assert.Error(t, mail.NewResend("k", "f@example.com", url).Send(context.Background(), "a@example.com", "s", "b"))
}

func TestLog_Send(t *testing.T) {
	assert.NoError(t, mail.NewLog(slog.New(slog.NewTextHandler(io.Discard, nil))).Send(context.Background(), "a@example.com", "s", "b"))
}

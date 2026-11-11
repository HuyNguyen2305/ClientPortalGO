package model_test

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/model"
)

func TestCursorRoundTrip(t *testing.T) {
	c := model.DocumentCursor{Date: time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC), ID: 12}
	got, err := model.DecodeCursor(c.Encode())
	require.NoError(t, err)
	assert.Equal(t, c, got)
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	b64 := base64.RawURLEncoding.EncodeToString
	for _, in := range []string{"", "!!!", b64([]byte("nodate|x")), b64([]byte("2026-12-10")), b64([]byte("2026-12-10|0"))} {
		_, err := model.DecodeCursor(in)
		assert.True(t, errors.Is(err, model.ErrValidation), "input %q: %v", in, err)
	}
}

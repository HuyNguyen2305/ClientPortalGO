package handler_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/dto"
	"clientportal/internal/handler"
	"clientportal/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

func TestRespondError(t *testing.T) {
	notFound := fmt.Errorf("get invoice: %w", model.ErrNotFound)
	conflict := fmt.Errorf("%w: invoice is not a draft", model.ErrConflict)
	invalid := fmt.Errorf("%w: title is required", model.ErrValidation)
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{"not found", notFound, http.StatusNotFound, "not_found", notFound.Error()},
		{"conflict", conflict, http.StatusConflict, "conflict", conflict.Error()},
		{"validation", invalid, http.StatusBadRequest, "invalid_request", invalid.Error()},
		{"unknown", errors.New("connection refused"), http.StatusInternalServerError, "internal", "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			handler.RespondError(c, tt.err)

			assert.Equal(t, tt.status, rec.Code)
			var body dto.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.code, body.Error.Code)
			assert.Equal(t, tt.message, body.Error.Message)
		})
	}
}

// Package handler binds HTTP requests, calls services and writes responses.
package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/model"
)

// RespondError is the only place that maps an error to an HTTP status and error body.
func RespondError(c *gin.Context, err error) {
	status, code, msg := http.StatusInternalServerError, "internal", "internal server error"
	switch {
	case errors.Is(err, model.ErrValidation):
		status, code, msg = http.StatusBadRequest, "invalid_request", err.Error()
	case errors.Is(err, model.ErrNotFound):
		status, code, msg = http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, model.ErrConflict):
		status, code, msg = http.StatusConflict, "conflict", err.Error()
	default:
		slog.ErrorContext(c.Request.Context(), "request failed",
			"method", c.Request.Method, "path", c.FullPath(), "err", err)
	}
	c.AbortWithStatusJSON(status, dto.ErrorResponse{Error: dto.ErrorBody{Code: code, Message: msg}})
}

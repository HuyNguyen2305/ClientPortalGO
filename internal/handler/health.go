package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
)

func health(ping func(context.Context) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := ping(c.Request.Context()); err != nil {
			slog.WarnContext(c.Request.Context(), "health check failed", "err", err)
			c.JSON(http.StatusServiceUnavailable, dto.ErrorResponse{
				Error: dto.ErrorBody{Code: "internal", Message: "database unavailable"},
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

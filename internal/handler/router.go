package handler

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"clientportal/internal/middleware"
)

// Deps is everything the router needs.
type Deps struct {
	Log  *slog.Logger
	Ping func(context.Context) error
}

// NewRouter builds the Gin engine with every route.
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Logging(d.Log))
	r.GET("/healthz", health(d.Ping))
	return r
}

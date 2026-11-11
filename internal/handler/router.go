package handler

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"

	"clientportal/internal/middleware"
	"clientportal/internal/service"
)

// Deps is everything the router needs.
type Deps struct {
	Log       *slog.Logger
	Ping      func(context.Context) error
	Customers *service.CustomerService
}

// NewRouter builds the Gin engine with every route.
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Logging(d.Log))
	r.GET("/healthz", health(d.Ping))

	api := r.Group("/api/v1")

	customers := customerHandler{svc: d.Customers}
	api.POST("/customers", customers.create)
	api.GET("/customers", customers.list)
	api.GET("/customers/:id", customers.get)
	api.PUT("/customers/:id", customers.update)
	api.DELETE("/customers/:id", customers.delete)

	return r
}

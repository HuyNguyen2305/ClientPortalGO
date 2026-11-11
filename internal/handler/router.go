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
	Locations *service.LocationService
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

	// Everything below acts for the customer named in X-Customer-ID.
	scoped := api.Group("", middleware.RequireCustomer(func(ctx context.Context, id int64) error {
		_, err := d.Customers.Get(ctx, id)
		return err
	}, RespondError))

	locations := locationHandler{svc: d.Locations}
	scoped.POST("/locations", locations.create)
	scoped.GET("/locations", locations.list)
	scoped.GET("/locations/:id", locations.get)
	scoped.PUT("/locations/:id", locations.update)
	scoped.DELETE("/locations/:id", locations.delete)

	return r
}

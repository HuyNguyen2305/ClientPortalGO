package handler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"clientportal/internal/middleware"
	"clientportal/internal/model"
	"clientportal/internal/service"
)

// Deps is everything the router needs.
type Deps struct {
	Log       *slog.Logger
	AdminKey  string
	Ping      func(context.Context) error
	Customers *service.CustomerService
	Auth      *service.AuthService
	Locations *service.LocationService
	Documents *service.BillingDocumentService
	Catalog   *service.Catalog
	Bookings  *service.BookingService
	// PaymentMethods is nil when Stripe is not configured.
	PaymentMethods *service.PaymentMethodService
	// TrustedProxies are the proxies whose X-Forwarded-For is believed; nil trusts none, so clients cannot fake their IP.
	TrustedProxies []string
}

// NewRouter builds the Gin engine with every route. It panics on an invalid TrustedProxies entry: that is a startup misconfiguration.
func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		panic(fmt.Sprintf("trusted proxies: %v", err))
	}
	// A panic answers the standard 500 body and is logged through RespondError like any other failure.
	r.Use(gin.CustomRecovery(func(c *gin.Context, v any) { RespondError(c, fmt.Errorf("panic: %v", v)) }), middleware.Logging(d.Log))
	r.GET("/healthz", health(d.Ping))

	api := r.Group("/api/v1")

	// Admin routes need X-Admin-Key; the service catalog stays readable by customers.
	admin := api.Group("", middleware.RequireAdminKey(d.AdminKey, RespondError))

	customers := customerHandler{svc: d.Customers}
	admin.POST("/customers", customers.create)
	admin.GET("/customers", customers.list)
	admin.GET("/customers/:id", customers.get)
	admin.PUT("/customers/:id", customers.update)
	admin.DELETE("/customers/:id", customers.delete)

	services := serviceHandler{svc: d.Catalog}
	api.GET("/services", services.list)
	api.GET("/services/:id", services.get)
	admin.GET("/admin/services", services.listAll)
	admin.POST("/services", services.create)
	admin.PUT("/services/:id", services.update)
	admin.POST("/services/:id/archive", services.archive)
	admin.POST("/services/:id/restore", services.restore)
	admin.DELETE("/services/:id", services.delete)

	// Customers register and log in without a token; the limiter slows password guessing.
	auth := authHandler{svc: d.Auth}
	limit := middleware.RateLimit(10, time.Minute, RespondError)
	api.POST("/auth/register", limit, auth.register)
	api.POST("/auth/login", limit, auth.login)
	api.POST("/auth/password-reset", limit, auth.requestReset)
	api.POST("/auth/password-reset/confirm", limit, auth.confirmReset)

	// Customer routes act for the owner of the bearer token.
	scoped := api.Group("", middleware.RequireSession(d.Auth.Authenticate, RespondError))
	scoped.POST("/auth/logout", auth.logout)
	scoped.POST("/auth/password", limit, auth.changePassword)

	// Document writes change what a customer owes: staff send the admin key and name the customer in X-Customer-ID.
	requireCustomer := middleware.RequireCustomer(func(ctx context.Context, id int64) error {
		_, err := d.Customers.Get(ctx, id)
		return err
	}, RespondError)
	staff := admin.Group("", requireCustomer)

	locations := locationHandler{svc: d.Locations}
	scoped.POST("/locations", locations.create)
	scoped.GET("/locations", locations.list)
	scoped.GET("/locations/:id", locations.get)
	scoped.PUT("/locations/:id", locations.update)
	scoped.DELETE("/locations/:id", locations.delete)

	// Invoices and estimates share one handler type, fixed to a parent type per route group.
	// read is the customer's own group; write is the staff group on the same path.
	docGroup := func(path string, pt model.ParentType) (read, write *gin.RouterGroup, h *BillingDocumentHandler) {
		h = NewBillingDocumentHandler(d.Documents, pt)
		read, write = scoped.Group(path), staff.Group(path)
		read.GET("", h.List)
		read.GET("/:id", h.Get)
		write.POST("", h.Create)
		write.PUT("/:id", h.Update)
		write.DELETE("/:id", h.Delete)
		write.POST("/:id/send", h.Transition(model.StatusOpen))
		return read, write, h
	}

	invoices, invoiceWrites, inv := docGroup("/invoices", model.ParentInvoice)
	invoiceWrites.POST("/:id/void", inv.Transition(model.StatusVoid))
	invoiceWrites.POST("/:id/payments", inv.RecordPayment)
	invoices.GET("/:id/payments", inv.ListPayments)
	scoped.GET("/balance", inv.Balance)

	estimates, estimateWrites, est := docGroup("/estimates", model.ParentEstimate)
	estimates.POST("/:id/accept", est.Transition(model.StatusAccepted))
	estimates.POST("/:id/decline", est.Transition(model.StatusDeclined))
	estimateWrites.POST("/:id/convert", est.Convert)

	bookings := bookingHandler{svc: d.Bookings}
	scoped.GET("/booking/days", bookings.days)
	scoped.GET("/services/:id/slots", bookings.slots)
	scoped.POST("/bookings", bookings.create)
	scoped.POST("/booking-series", bookings.createSeries)
	scoped.GET("/bookings", bookings.list)
	scoped.GET("/bookings/:id", bookings.get)
	scoped.POST("/bookings/:id/cancel", bookings.cancel)
	scoped.GET("/work-orders", bookings.listWork)
	// Work orders are moved by staff, who name no customer.
	admin.POST("/bookings/:id/start", bookings.start)
	admin.POST("/bookings/:id/complete", bookings.complete)

	// Off until a Stripe key is configured.
	if d.PaymentMethods != nil {
		pm := paymentMethodHandler{svc: d.PaymentMethods}
		scoped.POST("/payment-methods/setup-intent", pm.setupIntent)
		scoped.GET("/payment-methods", pm.list)
		scoped.DELETE("/payment-methods/:id", pm.delete)
		scoped.POST("/invoices/:id/pay", pm.payInvoice)
	}

	return r
}

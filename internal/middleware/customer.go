package middleware

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"clientportal/internal/model"
)

const customerKey = "customer_id"

// RequireCustomer reads X-Customer-ID, checks the customer exists and stores the ID on the context.
// shortcut: trusts X-Customer-ID and looks the customer up on every request; replace with real auth.
func RequireCustomer(exists func(ctx context.Context, id int64) error, fail func(*gin.Context, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.GetHeader("X-Customer-ID"), 10, 64)
		if err != nil || id <= 0 {
			fail(c, fmt.Errorf("%w: X-Customer-ID header must be a positive integer", model.ErrValidation))
			return
		}
		if err := exists(c.Request.Context(), id); err != nil {
			fail(c, err)
			return
		}
		c.Set(customerKey, id)
		c.Next()
	}
}

// CustomerID returns the ID stored by RequireCustomer.
func CustomerID(c *gin.Context) int64 {
	return c.GetInt64(customerKey)
}

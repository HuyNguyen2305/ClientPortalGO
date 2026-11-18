package middleware

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"clientportal/internal/model"
)

const (
	customerKey = "customer_id"
	tokenKey    = "session_token"
)

// RequireCustomer reads X-Customer-ID, checks the customer exists and stores the ID on the context.
// shortcut: trusts X-Customer-ID, so it is only for staff routes already behind the admin key; customers use RequireSession.
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

// RequireSession reads "Authorization: Bearer <token>", resolves it to a customer and stores the ID and token on the context.
func RequireSession(authenticate func(ctx context.Context, token string) (int64, error), fail func(*gin.Context, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || token == "" {
			fail(c, fmt.Errorf("%w: missing bearer token", model.ErrUnauthorized))
			return
		}
		id, err := authenticate(c.Request.Context(), token)
		if err != nil {
			fail(c, err)
			return
		}
		c.Set(customerKey, id)
		c.Set(tokenKey, token)
		c.Next()
	}
}

// CustomerID returns the ID stored by RequireCustomer or RequireSession.
func CustomerID(c *gin.Context) int64 {
	return c.GetInt64(customerKey)
}

// Token returns the bearer token stored by RequireSession.
func Token(c *gin.Context) string {
	return c.GetString(tokenKey)
}

package middleware

import (
	"crypto/subtle"
	"fmt"

	"github.com/gin-gonic/gin"

	"clientportal/internal/model"
)

// RequireAdminKey lets a request through only when X-Admin-Key equals key.
// shortcut: one shared static key, no rotation or per-admin identity; replace with real admin auth.
func RequireAdminKey(key string, fail func(*gin.Context, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		// An unset key must reject everything, not match an absent header.
		if key == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Admin-Key")), []byte(key)) != 1 {
			fail(c, fmt.Errorf("%w: missing or wrong X-Admin-Key header", model.ErrUnauthorized))
			return
		}
		c.Next()
	}
}

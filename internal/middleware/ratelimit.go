package middleware

import (
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"clientportal/internal/model"
)

// RateLimit allows each client IP at most limit requests per window.
// shortcut: in-memory fixed window, so it is per process and resets on restart; move to a shared store when the API
// runs on several instances. Behind a reverse proxy, list it in TRUSTED_PROXIES or every client shares one bucket.
func RateLimit(limit int, window time.Duration, fail func(*gin.Context, error)) gin.HandlerFunc {
	type bucket struct {
		start time.Time
		n     int
	}
	var mu sync.Mutex
	buckets := map[string]*bucket{}
	return func(c *gin.Context) {
		now := time.Now()
		mu.Lock()
		for ip, b := range buckets { // drop spent windows so the map cannot grow without bound
			if now.Sub(b.start) >= window {
				delete(buckets, ip)
			}
		}
		b := buckets[c.ClientIP()]
		if b == nil {
			b = &bucket{start: now}
			buckets[c.ClientIP()] = b
		}
		b.n++
		over := b.n > limit
		mu.Unlock()
		if over {
			fail(c, fmt.Errorf("%w: too many attempts, try again later", model.ErrTooManyRequests))
			return
		}
		c.Next()
	}
}

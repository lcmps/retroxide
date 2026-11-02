package http

import (
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/lcmps/retroxide/application/fault"
)

// maxJSONBodyBytes bounds every JSON request body; uploads use their own multipart limits.
const maxJSONBodyBytes = 1 << 20

const (
	// Credential endpoints allow a burst of credentialBurst attempts per client, then one every credentialRefill.
	credentialBurst  = 10
	credentialRefill = 6 * time.Second
	limiterIdleTTL   = 10 * time.Minute
)

const (
	apiContentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'"
	// The web app's full policy ships as a <meta> tag in its HTML; frame-ancestors only works as a header.
	frontendContentSecurityPolicy = "frame-ancestors 'none'"
)

// securityHeadersMiddleware sets browser hardening headers on every response.
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		if isAPIPath(c.Request.URL.Path) {
			header.Set("Content-Security-Policy", apiContentSecurityPolicy)
		} else {
			header.Set("Content-Security-Policy", frontendContentSecurityPolicy)
		}
		c.Next()
	}
}

// attemptLimiter is a per-key token bucket that throttles password guessing and the Argon2 work it triggers.
type attemptLimiter struct {
	mu        sync.Mutex
	entries   map[string]*limiterEntry
	limit     rate.Limit
	burst     int
	idleTTL   time.Duration
	lastSweep time.Time
	now       func() time.Time
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newAttemptLimiter(refill time.Duration, burst int) *attemptLimiter {
	return &attemptLimiter{
		entries: make(map[string]*limiterEntry),
		limit:   rate.Every(refill),
		burst:   burst,
		idleTTL: limiterIdleTTL,
		now:     time.Now,
	}
}

// allow consumes one attempt for key, or reports how long until the next attempt is permitted.
func (limiter *attemptLimiter) allow(key string) (bool, time.Duration) {
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if now.Sub(limiter.lastSweep) >= limiter.idleTTL {
		for entryKey, entry := range limiter.entries {
			if now.Sub(entry.lastSeen) >= limiter.idleTTL {
				delete(limiter.entries, entryKey)
			}
		}
		limiter.lastSweep = now
	}

	entry, ok := limiter.entries[key]
	if !ok {
		entry = &limiterEntry{limiter: rate.NewLimiter(limiter.limit, limiter.burst)}
		limiter.entries[key] = entry
	}
	entry.lastSeen = now

	reservation := entry.limiter.ReserveN(now, 1)
	if delay := reservation.DelayFrom(now); delay > 0 {
		reservation.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// rateLimitMiddleware rejects requests once the client's attempts for this route are exhausted.
func rateLimitMiddleware(limiter *attemptLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, retryAfter := limiter.allow(c.ClientIP() + " " + c.FullPath())
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
			writeError(c, fault.RateLimited())
			return
		}
		c.Next()
	}
}

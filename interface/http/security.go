package http

import (
	"github.com/gin-gonic/gin"
)

// maxJSONBodyBytes bounds every JSON request body; uploads use their own multipart limits.
const maxJSONBodyBytes = 1 << 20

const (
	apiContentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'"
	// The web app's full policy ships as a <meta> tag in its HTML; frame-ancestors only works as a header.
	frontendContentSecurityPolicy = "frame-ancestors 'none'"
)

// securityHeadersMiddleware sets browser hardening headers on every response.
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(context *gin.Context) {
		header := context.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		if isAPIPath(context.Request.URL.Path) {
			header.Set("Content-Security-Policy", apiContentSecurityPolicy)
		} else {
			header.Set("Content-Security-Policy", frontendContentSecurityPolicy)
		}
		context.Next()
	}
}

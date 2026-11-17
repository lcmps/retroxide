package http

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
)

const (
	requestIDKey = "request_id"
	principalKey = "principal"
)

// requestIDMiddleware tags each request with an ID, echoes it in X-Request-ID, and logs the outcome.
func requestIDMiddleware(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		requestID := NewRequestID()
		c.Set(requestIDKey, requestID)
		c.Header("x-request-id", requestID)
		started := time.Now()
		c.Next()

		ctx := c.Request.Context()
		if c.Writer.Status() >= http.StatusInternalServerError && len(c.Errors) > 0 {
			logger.ErrorContext(ctx, "http request failed",
				"request_id", requestID,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"error", c.Errors.Last().Err,
			)
		}
		level := slog.LevelInfo
		if isQuietRequest(c.Request.Method, c.Request.URL.Path, c.Writer.Status()) {
			level = slog.LevelDebug
		}
		logger.Log(ctx, level, "http request handled",
			"request_id", requestID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(started).Milliseconds(),
		)
	}
}

// isQuietRequest reports a request worth logging only at debug level: a successful response for
// a static asset of the web app or an image. A library page loads dozens of each, and logging
// every one buries the requests that matter.
func isQuietRequest(method, path string, status int) bool {
	if status >= http.StatusBadRequest {
		return false
	}
	return !isAPIPath(path) || isImagePath(method, path)
}

// corsMiddleware answers preflight requests and decorates responses for the configured origins.
// With no origins configured, CORS headers are never sent.
func corsMiddleware(origins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(origins))
	allowAny := false
	for _, origin := range origins {
		if origin == "*" {
			allowAny = true
		}
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		_, isAllowed := allowed[origin]
		if c.Request.Method == http.MethodOptions {
			requestedMethod := c.GetHeader("Access-Control-Request-Method")
			if len(allowed) == 0 || origin == "" || requestedMethod == "" {
				c.Next()
				return
			}
			if (!allowAny && !isAllowed) || !corsMethodAllowed(requestedMethod) || !corsHeadersAllowed(c.GetHeader("Access-Control-Request-Headers")) {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			setCORSHeaders(c, origin, allowAny)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		if origin != "" && (allowAny || isAllowed) {
			setCORSHeaders(c, origin, allowAny)
		}
		c.Next()
	}
}

func setCORSHeaders(c *gin.Context, origin string, allowAny bool) {
	if allowAny {
		c.Header("Access-Control-Allow-Origin", "*")
	} else {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
	}
	c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Upload-Offset")
	c.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Disposition, Content-Range, ETag, Last-Modified, X-Request-ID")
}

func corsMethodAllowed(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func corsHeadersAllowed(header string) bool {
	for requested := range strings.SplitSeq(header, ",") {
		switch strings.ToLower(strings.TrimSpace(requested)) {
		case "", "authorization", "content-type":
		default:
			return false
		}
	}
	return true
}

// publicRoutes are API endpoints reachable without a bearer token.
var publicRoutes = map[string]map[string]struct{}{
	http.MethodGet: {
		"/health": {}, "/api/openapi.json": {}, setupPath: {},
		// Authorized by the signed token in the query string instead of a bearer token.
		downloadLinkPath: {},
		// Presentation settings the sign-in page needs.
		appearancePath: {},
	},
	http.MethodPost: {
		"/api/v1/session": {}, "/api/v1/session/refresh": {},
	},
}

// authMiddleware requires a valid bearer token on every non-public API path and stores the
// resolved requester for handlers. Image paths may instead carry a signed token in their query,
// which their handlers check: a request to one without a bearer token passes through here, and
// one with a bearer token is authenticated as usual.
func authMiddleware(authenticator Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		method, path := c.Request.Method, c.Request.URL.Path
		if _, ok := publicRoutes[method][path]; ok || isPublicPath(method, path) {
			c.Next()
			return
		}
		token, ok := auth.ExtractBearerToken(c.GetHeader("Authorization"))
		if !ok && isOptionalAuthPath(method, path) {
			c.Next()
			return
		}
		if !ok || authenticator == nil {
			abortUnauthorized(c)
			return
		}
		requester, err := authenticator.Authenticate(c.Request.Context(), token)
		switch {
		case fault.IsCode(err, fault.CodeUnauthorized):
			abortUnauthorized(c)
			return
		case err != nil:
			// Infrastructure failures (e.g. the database being down) are not the client's fault.
			writeError(c, err)
			return
		}
		c.Set(principalKey, requester)
		c.Next()
	}
}

// isOptionalAuthPath reports a path that serves anonymous requests as well as authenticated
// ones: images carrying a signed token, and the bootstrap, which adds the profile when signed in.
func isOptionalAuthPath(method, path string) bool {
	return isImagePath(method, path) || (method == http.MethodGet && path == bootstrapPath)
}

// isImagePath reports a GET of a ROM's cover, cover thumbnail, or media image: the paths that
// accept a signed token in place of a bearer token.
func isImagePath(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	rest, ok := strings.CutPrefix(path, "/api/v1/rom/")
	if !ok {
		return false
	}
	romID, resource, found := strings.Cut(rest, "/")
	if !found || romID == "" {
		return false
	}
	if resource == "cover" || resource == "cover/thumb" {
		return true
	}
	kind, ok := strings.CutPrefix(resource, "media/")
	return ok && kind != "" && !strings.Contains(kind, "/")
}

func isPublicPath(method, path string) bool {
	if !isAPIPath(path) {
		return true
	}
	if method == http.MethodGet && (path == "/api/v1/i18n" || strings.HasPrefix(path, "/api/v1/i18n/")) {
		return true
	}
	return path == "/api/docs" || strings.HasPrefix(path, "/api/docs/")
}

// responseCountingMiddleware reports each response's status once the handler is done.
func responseCountingMiddleware(count func(status int)) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if count != nil {
			count(c.Writer.Status())
		}
	}
}

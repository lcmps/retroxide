package http

import (
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/domain/identity"
)

type Dependencies struct {
	Uploads    uploads.Service
	ROMs       rom.API
	Libraries  libraries.API
	Sessions   auth.SessionAPI
	Clients    auth.ClientAPI
	Tokens     auth.TokenService
	ClientRepo auth.ClientRepository
	Locales    map[string][]byte
	Logger     *slog.Logger
}

type Options struct {
	MaxUploadBytes int64
	CORSOrigins    []string
	// Frontend serves non-API paths; nil leaves them as 404s.
	Frontend http.Handler
}

func NewRouter(dependencies Dependencies, options Options) *gin.Engine {
	router := gin.New()
	router.Use(gin.CustomRecovery(func(context *gin.Context, recovered any) {
		slog.ErrorContext(context.Request.Context(), "http handler panic", "panic", recovered)
		writeError(context, fault.Internal(errors.New("http handler panic")))
	}))
	router.Use(requestIDMiddleware(dependencies.Logger))
	router.Use(corsMiddleware(options.CORSOrigins))
	router.Use(authMiddleware(dependencies.Tokens, dependencies.ClientRepo))

	router.GET("/health", func(context *gin.Context) {
		context.String(http.StatusOK, "ok")
	})
	registerSetupStatusRoute(router)
	registerRoutes(router, dependencies, options)
	router.NoRoute(frontendFallback(options.Frontend, func(context *gin.Context) {
		writeError(context, fault.NotFound())
	}))
	return router
}

func requestIDMiddleware(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(context *gin.Context) {
		requestID := NewRequestID()
		context.Set("request_id", requestID)
		context.Header("x-request-id", requestID)
		started := time.Now()
		context.Next()
		logger.InfoContext(context.Request.Context(), "http request handled",
			"request_id", requestID,
			"method", context.Request.Method,
			"path", context.Request.URL.Path,
			"status", context.Writer.Status(),
			"latency_ms", time.Since(started).Milliseconds(),
		)
	}
}

func corsMiddleware(origins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(origins))
	allowAny := false
	for _, origin := range origins {
		if origin == "*" {
			allowAny = true
		}
		allowed[origin] = struct{}{}
	}
	return func(context *gin.Context) {
		origin := context.GetHeader("Origin")
		_, isAllowed := allowed[origin]
		if context.Request.Method == http.MethodOptions {
			requestedMethod := context.GetHeader("Access-Control-Request-Method")
			if len(allowed) == 0 || origin == "" || requestedMethod == "" {
				context.Next()
				return
			}
			if (!allowAny && !isAllowed) || !corsMethodAllowed(requestedMethod) || !corsHeadersAllowed(context.GetHeader("Access-Control-Request-Headers")) {
				context.AbortWithStatus(http.StatusForbidden)
				return
			}
			setCORSHeaders(context, origin, allowAny)
			context.Status(http.StatusNoContent)
			context.Abort()
			return
		}
		if origin != "" && (allowAny || isAllowed) {
			setCORSHeaders(context, origin, allowAny)
		}
		context.Next()
	}
}

func setCORSHeaders(context *gin.Context, origin string, allowAny bool) {
	if allowAny {
		context.Header("Access-Control-Allow-Origin", "*")
	} else {
		context.Header("Access-Control-Allow-Origin", origin)
		context.Header("Vary", "Origin")
	}
	context.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	context.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
	context.Header("Access-Control-Expose-Headers", "Accept-Ranges, Content-Disposition, Content-Range, ETag, Last-Modified, X-Request-ID")
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
	for _, requested := range strings.Split(header, ",") {
		switch strings.ToLower(strings.TrimSpace(requested)) {
		case "", "authorization", "content-type":
		default:
			return false
		}
	}
	return true
}

func authMiddleware(tokens auth.TokenService, clients auth.ClientRepository) gin.HandlerFunc {
	publicExact := map[string]map[string]struct{}{
		http.MethodGet: {
			"/health": {}, "/api/openapi.json": {}, setupPath: {},
		},
		http.MethodPost: {
			"/api/v1/session": {}, "/api/v1/session/refresh": {},
		},
	}
	return func(context *gin.Context) {
		method := context.Request.Method
		path := context.Request.URL.Path
		if _, ok := publicExact[method][path]; ok || publicPrefix(method, path) {
			context.Next()
			return
		}
		rawHeader := context.GetHeader("Authorization")
		token, ok := auth.ExtractBearerToken(rawHeader)
		if !ok || tokens == nil || clients == nil {
			abortUnauthorized(context)
			return
		}
		requester, err := auth.ResolvePrincipal(context.Request.Context(), token, tokens, clients)
		if err != nil {
			abortUnauthorized(context)
			return
		}
		context.Set("principal", requester)
		context.Next()
	}
}

func publicPrefix(method, path string) bool {
	if !isAPIPath(path) {
		return true
	}
	if method == http.MethodGet && (path == "/api/v1/i18n" || strings.HasPrefix(path, "/api/v1/i18n/")) {
		return true
	}
	return path == "/api/docs" || strings.HasPrefix(path, "/api/docs/")
}

func abortUnauthorized(context *gin.Context) {
	requestID := requestIDFromContext(context)
	status, response := PresentError(fault.Unauthorized(), requestID)
	context.Header("WWW-Authenticate", "Bearer")
	context.AbortWithStatusJSON(status, response)
}

func requestIDFromContext(context *gin.Context) string {
	value, ok := context.Get("request_id")
	if !ok {
		return ""
	}
	requestID, _ := value.(string)
	return requestID
}

func requesterFromContext(context *gin.Context) identity.Requester {
	value, _ := context.Get("principal")
	requester, _ := value.(identity.Requester)
	return requester
}

func writeError(context *gin.Context, err error) {
	status, response := PresentError(err, requestIDFromContext(context))
	writeErrorResponse(context, status, response)
}

func writeErrorResponse(context *gin.Context, status int, response ErrorResponse) {
	if status == http.StatusUnauthorized {
		context.Header("WWW-Authenticate", "Bearer")
	}
	context.AbortWithStatusJSON(status, response)
}

func writeSpecificError(context *gin.Context, status int, code, message string) {
	context.AbortWithStatusJSON(status, ErrorResponse{
		Code: code, Message: message, RequestID: requestIDFromContext(context),
	})
}

func bindJSON(context *gin.Context, target any) bool {
	mediaType, _, mediaTypeErr := mime.ParseMediaType(context.GetHeader("Content-Type"))
	if mediaTypeErr != nil || mediaType != "application/json" {
		writeError(context, fault.UnsupportedMediaType("request content type must be application/json"))
		return false
	}
	if err := context.ShouldBindJSON(target); err != nil {
		writeError(context, fault.Validation("invalid JSON request body"))
		return false
	}
	return true
}

func requireDependencies(context *gin.Context, dependencies ...any) bool {
	for _, dependency := range dependencies {
		if dependency == nil {
			writeError(context, fault.Internal(errors.New("service is unavailable")))
			return false
		}
	}
	return true
}

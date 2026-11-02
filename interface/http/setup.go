package http

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/setup"
)

const setupPath = "/api/v1/setup"

// NewSetupRouter serves only health, locales, and the setup wizard API until configuration is complete.
func NewSetupRouter(service setup.Service, locales map[string][]byte, logger *slog.Logger, frontend http.Handler) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.Use(gin.CustomRecovery(func(context *gin.Context, recovered any) {
		slog.ErrorContext(context.Request.Context(), "http handler panic", "panic", recovered)
		writeError(context, fault.Internal(errors.New("http handler panic")))
	}))
	router.Use(securityHeadersMiddleware())
	router.Use(requestIDMiddleware(logger))

	router.GET("/health", func(context *gin.Context) {
		context.String(http.StatusOK, "ok")
	})
	router.GET("/api/v1/i18n/:locale", func(context *gin.Context) {
		catalog, ok := locales[context.Param("locale")]
		if !ok {
			writeError(context, fault.NotFound())
			return
		}
		context.Data(http.StatusOK, "application/json", catalog)
	})
	router.GET(setupPath, func(context *gin.Context) {
		context.JSON(http.StatusOK, service.Status())
	})
	router.POST(setupPath+"/mysql/test", func(context *gin.Context) {
		var connection setup.MySQLConnection
		if !bindJSON(context, &connection) {
			return
		}
		if err := service.TestMySQL(context.Request.Context(), connection); err != nil {
			writeError(context, err)
			return
		}
		context.Status(http.StatusNoContent)
	})
	router.POST(setupPath, func(context *gin.Context) {
		var request setup.Request
		if !bindJSON(context, &request) {
			return
		}
		if err := service.Complete(context.Request.Context(), request); err != nil {
			writeError(context, err)
			return
		}
		context.Status(http.StatusNoContent)
	})
	router.NoRoute(frontendFallback(frontend, func(context *gin.Context) {
		writeSpecificError(context, http.StatusServiceUnavailable, "setup_required", "server configuration is required")
	}))
	return router
}

// frontendFallback sends unmatched non-API requests to the web app and everything else to apiFallback.
func frontendFallback(frontend http.Handler, apiFallback gin.HandlerFunc) gin.HandlerFunc {
	return func(context *gin.Context) {
		if frontend == nil || isAPIPath(context.Request.URL.Path) {
			apiFallback(context)
			return
		}
		// Gin presets 404 for unmatched routes; the frontend decides the real status.
		context.Status(http.StatusOK)
		frontend.ServeHTTP(context.Writer, context.Request)
	}
}

func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

func registerSetupStatusRoute(router *gin.Engine) {
	router.GET(setupPath, func(context *gin.Context) {
		context.JSON(http.StatusOK, setup.Status{Required: false})
	})
}

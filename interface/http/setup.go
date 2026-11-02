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
	router.Use(
		gin.CustomRecovery(func(c *gin.Context, recovered any) {
			slog.ErrorContext(c.Request.Context(), "http handler panic", "panic", recovered)
			writeError(c, fault.Internal(errors.New("http handler panic")))
		}),
		securityHeadersMiddleware(),
		requestIDMiddleware(logger),
	)

	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	registerLocaleRoutes(router.Group("/api/v1"), locales)

	handler := setupHandler{setup: service}
	router.GET(setupPath, handler.status)
	router.POST(setupPath+"/mysql/test", handler.testMySQL)
	router.POST(setupPath, handler.complete)
	router.NoRoute(frontendFallback(frontend, func(c *gin.Context) {
		writeSpecificError(c, http.StatusServiceUnavailable, "setup_required", "server configuration is required")
	}))
	return router
}

type setupHandler struct {
	setup setup.Service
}

func (handler setupHandler) status(c *gin.Context) {
	c.JSON(http.StatusOK, handler.setup.Status())
}

func (handler setupHandler) testMySQL(c *gin.Context) {
	var connection setup.MySQLConnection
	if !bindJSON(c, &connection) {
		return
	}
	respondNoContent(c, handler.setup.TestMySQL(c.Request.Context(), connection))
}

func (handler setupHandler) complete(c *gin.Context) {
	var request setup.Request
	if !bindJSON(c, &request) {
		return
	}
	respondNoContent(c, handler.setup.Complete(c.Request.Context(), request))
}

// frontendFallback sends unmatched non-API requests to the web app and everything else to apiFallback.
func frontendFallback(frontend http.Handler, apiFallback gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if frontend == nil || isAPIPath(c.Request.URL.Path) {
			apiFallback(c)
			return
		}
		// Gin presets 404 for unmatched routes; the frontend decides the real status.
		c.Status(http.StatusOK)
		frontend.ServeHTTP(c.Writer, c.Request)
	}
}

func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// registerSetupStatusRoute tells the frontend, once configured, that setup is no longer required.
func registerSetupStatusRoute(router *gin.Engine) {
	router.GET(setupPath, func(c *gin.Context) {
		c.JSON(http.StatusOK, setup.Status{Required: false})
	})
}

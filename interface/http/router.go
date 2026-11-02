package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
)

// Dependencies are the application services the API exposes. A nil service answers its routes
// with an internal error instead of panicking.
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
	// TrustedProxies lists proxy IPs/CIDRs whose forwarding headers are believed; empty trusts none.
	TrustedProxies []string
	// Frontend serves non-API paths; nil leaves them as 404s.
	Frontend http.Handler
}

func NewRouter(dependencies Dependencies, options Options) *gin.Engine {
	router := gin.New()
	if err := router.SetTrustedProxies(options.TrustedProxies); err != nil {
		slog.Warn("ignoring invalid trusted proxies; forwarding headers will not be trusted", "error", err)
		_ = router.SetTrustedProxies(nil)
	}
	router.Use(
		gin.CustomRecovery(func(c *gin.Context, recovered any) {
			slog.ErrorContext(c.Request.Context(), "http handler panic", "panic", recovered)
			writeError(c, fault.Internal(errors.New("http handler panic")))
		}),
		securityHeadersMiddleware(),
		requestIDMiddleware(dependencies.Logger),
		corsMiddleware(options.CORSOrigins),
		authMiddleware(dependencies.Tokens, dependencies.ClientRepo),
	)

	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	registerSetupStatusRoute(router)
	registerRoutes(router.Group("/api/v1"), dependencies, options)
	router.NoRoute(frontendFallback(options.Frontend, func(c *gin.Context) {
		writeError(c, fault.NotFound())
	}))
	return router
}

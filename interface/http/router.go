package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/bios"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/settings"
	"github.com/lcmps/retroxide/application/share"
	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/application/users"
	"github.com/lcmps/retroxide/domain/identity"
)

// Authenticator resolves a bearer token to the requester it represents.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (identity.Requester, error)
}

// DownloadLinks issues and verifies short-lived signed download capabilities.
type DownloadLinks interface {
	Sign(requester identity.Requester, romID string) (token string, expiresAt time.Time, err error)
	Verify(token string) (requester identity.Requester, romID string, err error)
}

// Dependencies are the application services the API exposes. A nil service answers its routes
// with an internal error instead of panicking.
type Dependencies struct {
	Uploads       uploads.Service
	ROMs          rom.API
	BIOS          bios.API
	Share         share.API
	Libraries     libraries.API
	Sessions      auth.SessionAPI
	Clients       auth.ClientAPI
	Profiles      auth.ProfileAPI
	Settings      settings.API
	Users         users.API
	Authenticator Authenticator
	DownloadLinks DownloadLinks
	Locales       map[string][]byte
	Logger        *slog.Logger
	// Metrics serves monitoring metrics at /metrics; nil leaves the path unserved. It checks its
	// own credentials.
	Metrics http.Handler
	// CountResponse, when set, is told every response's status.
	CountResponse func(status int)
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
		abortFailedDownload(),
		gin.CustomRecovery(func(c *gin.Context, recovered any) {
			slog.ErrorContext(c.Request.Context(), "http handler panic", "panic", recovered)
			writeError(c, fault.Internal(errors.New("http handler panic")))
		}),
		responseCountingMiddleware(dependencies.CountResponse),
		securityHeadersMiddleware(),
		requestIDMiddleware(dependencies.Logger),
		compressionMiddleware(),
		corsMiddleware(options.CORSOrigins),
		authMiddleware(dependencies.Authenticator),
	)

	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	if dependencies.Metrics != nil {
		router.GET("/metrics", gin.WrapH(dependencies.Metrics))
	}
	registerSetupStatusRoute(router)
	registerRoutes(router.Group("/api/v1"), dependencies, options)
	router.NoRoute(frontendFallback(options.Frontend, func(c *gin.Context) {
		writeError(c, fault.NotFound())
	}))
	return router
}

package factory

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/infrastructure/auth"
	"github.com/lcmps/retroxide/infrastructure/config"
	httpadapter "github.com/lcmps/retroxide/interface/http"
	"github.com/lcmps/retroxide/interface/http/i18n"
	"github.com/lcmps/retroxide/interface/web"
)

// downloadLinkLifetime bounds how long a signed download link stays usable; browsers open it at once.
const downloadLinkLifetime = time.Minute

func NewRouter(settings config.Config, services *Services, logger *slog.Logger) (http.Handler, error) {
	locales, err := i18n.Catalogs()
	if err != nil {
		return nil, fault.Internal(fmt.Errorf("loading locale catalogs: %w", err))
	}
	return httpadapter.NewRouter(httpadapter.Dependencies{
		Uploads:       services.Uploads,
		ROMs:          services.ROMs,
		Libraries:     services.Libraries,
		Sessions:      services.Sessions,
		Clients:       services.Clients,
		Authenticator: services.Authenticator,
		DownloadLinks: auth.NewDownloadLinkSigner(settings.JWTSecret, downloadLinkLifetime),
		Locales:       locales,
		Logger:        logger,
	}, httpadapter.Options{
		MaxUploadBytes: settings.MaxUploadBytes,
		CORSOrigins:    settings.CORSAllowedOrigins,
		TrustedProxies: settings.TrustedProxies,
		Frontend:       web.New(),
	}), nil
}

package factory

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/infrastructure/config"
	httpadapter "github.com/lcmps/retroxide/interface/http"
	"github.com/lcmps/retroxide/interface/http/i18n"
	"github.com/lcmps/retroxide/interface/web"
)

func NewRouter(settings config.Config, services *Services, tokens auth.TokenService, clients auth.ClientRepository, logger *slog.Logger) (http.Handler, error) {
	locales, err := i18n.Catalogs()
	if err != nil {
		return nil, fault.Internal(fmt.Errorf("loading locale catalogs: %w", err))
	}
	return httpadapter.NewRouter(httpadapter.Dependencies{
		Uploads:    services.Uploads,
		ROMs:       services.ROMs,
		Libraries:  services.Libraries,
		Sessions:   services.Sessions,
		Clients:    services.Clients,
		Tokens:     tokens,
		ClientRepo: clients,
		Locales:    locales,
		Logger:     logger,
	}, httpadapter.Options{
		MaxUploadBytes: settings.MaxUploadBytes,
		CORSOrigins:    settings.CORSAllowedOrigins,
		TrustedProxies: settings.TrustedProxies,
		Frontend:       web.New(),
	}), nil
}

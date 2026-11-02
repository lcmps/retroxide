package factory

import (
	"log/slog"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/infrastructure/config"
	hashadapter "github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/observability"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

type Services struct {
	Uploads    *uploads.UploadService
	ROMs       *rom.Service
	Libraries  *libraries.Service
	Sessions   *auth.SessionService
	Clients    *auth.ClientService
	Identifier *uploads.IdentifyService
}

func NewServices(settings config.Config, repositories *persistence.Repositories, storage *storage.Filesystem, hasher auth.PasswordHasher, tokens auth.TokenService, logger *slog.Logger) *Services {
	audit := observability.NewAuditLogger(logger)
	return &Services{
		Uploads: uploads.NewUploadService(
			repositories, repositories, storage, hashadapter.New(), repositories, audit,
			uploads.UploadPolicy{
				MaxUploadBytes:    settings.MaxUploadBytes,
				AllowedMIMETypes:  settings.AllowedUploadMIMETypes,
				AllowedExtensions: settings.AllowedUploadExtensions,
			},
		),
		ROMs:       rom.NewService(repositories, storage, audit, nil),
		Libraries:  libraries.NewService(repositories, repositories),
		Sessions:   auth.NewSessionService(repositories, hasher, tokens, repositories),
		Clients:    auth.NewClientService(repositories),
		Identifier: uploads.NewIdentifyService(repositories, repositories),
	}
}

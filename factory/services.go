package factory

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/jobs"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/infrastructure/config"
	hashadapter "github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/observability"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/scraper"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

const scraperTimeout = 30 * time.Second

func NewScraper() *scraper.Client {
	return scraper.NewClient(&http.Client{Timeout: scraperTimeout}, scraper.DefaultEndpoints())
}

type Services struct {
	Uploads       *uploads.UploadService
	ROMs          *rom.Service
	Libraries     *libraries.Service
	Sessions      *auth.SessionService
	Clients       *auth.ClientService
	Authenticator *auth.Authenticator
	Identifier    *persistence.Identifier
}

func NewServices(settings config.Config, repositories *persistence.Repositories, queue jobs.Enqueuer, storage *storage.Filesystem, hasher auth.PasswordHasher, tokens auth.TokenService, logger *slog.Logger) *Services {
	audit := observability.NewAuditLogger(logger)
	return &Services{
		Uploads: uploads.NewUploadService(
			repositories, repositories, storage, hashadapter.New(), queue, audit,
			uploads.UploadPolicy{
				MaxUploadBytes:    settings.MaxUploadBytes,
				AllowedMIMETypes:  settings.AllowedUploadMIMETypes,
				AllowedExtensions: settings.AllowedUploadExtensions,
			},
		),
		ROMs:          rom.NewService(repositories, storage, audit, NewScraper()),
		Libraries:     libraries.NewService(repositories, repositories),
		Sessions:      auth.NewSessionService(repositories, hasher, tokens, repositories),
		Clients:       auth.NewClientService(repositories),
		Authenticator: auth.NewAuthenticator(tokens, repositories, repositories),
		Identifier:    persistence.NewIdentifier(repositories.DB),
	}
}

package factory

import (
	"context"
	"github.com/lcmps/retroxide/application/discs"
	"github.com/lcmps/retroxide/infrastructure/discimage"
	"github.com/lcmps/retroxide/infrastructure/imaging"
	"github.com/lcmps/retroxide/infrastructure/metrics"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/jobs"
	"github.com/lcmps/retroxide/application/libraries"
	"github.com/lcmps/retroxide/application/rom"
	"github.com/lcmps/retroxide/application/uploads"
	"github.com/lcmps/retroxide/application/users"
	"github.com/lcmps/retroxide/infrastructure/config"
	hashadapter "github.com/lcmps/retroxide/infrastructure/hash"
	"github.com/lcmps/retroxide/infrastructure/observability"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"github.com/lcmps/retroxide/infrastructure/scraper"
	"github.com/lcmps/retroxide/infrastructure/storage"
)

const scraperTimeout = 30 * time.Second

// datCacheMaxAge is how long downloaded libretro databases are used before being revalidated.
const datCacheMaxAge = 7 * 24 * time.Hour

// NewScraper returns the metadata scraper, keeping libretro's databases in the storage folder
// (a hidden folder the storage layout ignores) between restarts.
func NewScraper(settings config.Config) *scraper.Client {
	return scraper.NewClient(&http.Client{Timeout: scraperTimeout}, scraper.DefaultEndpoints()).
		CacheDatabasesIn(filepath.Join(settings.StorageRoot, ".cache", "libretro"), datCacheMaxAge)
}

type Services struct {
	Uploads       *uploads.UploadService
	ROMs          *rom.Service
	Libraries     *libraries.Service
	Sessions      *auth.SessionService
	Clients       *auth.ClientService
	Authenticator *auth.Authenticator
	Users         *users.Service
	Identifier    *persistence.Identifier
	// Metrics counts the server's activity for monitoring.
	Metrics *metrics.Registry
	// Discs indexes, assembles, compresses, and archives disc images.
	Discs *discs.Service
	// DiscsUploaded is signalled when a disc is stored as tracks, to start compressing it.
	DiscsUploaded chan struct{}
}

func NewServices(settings config.Config, repositories *persistence.Repositories, queue *jobs.NotifyingQueue, storage *storage.Filesystem, hasher auth.PasswordHasher, tokens auth.TokenService, logger *slog.Logger) *Services {
	registry := metrics.NewRegistry(func(ctx context.Context) (metrics.Counts, error) {
		counts, err := repositories.CountForMetrics(ctx)
		return metrics.Counts(counts), err
	}, storage.FreeSpace)
	audit := metrics.NewCountingAudit(observability.NewAuditLogger(logger), registry)
	sessions := auth.NewSessionService(repositories, hasher, tokens, repositories)
	discService := discs.NewService(repositories, storage, hashadapter.New(), discimage.NewImages(hashadapter.New()), discimage.NewCompressor())
	discsUploaded := make(chan struct{}, 1)
	wakeCompressor := func() {
		select {
		case discsUploaded <- struct{}{}:
		default: // a wake-up is already pending
		}
	}
	return &Services{
		Discs:         discService,
		DiscsUploaded: discsUploaded,
		Uploads: uploads.NewUploadService(
			repositories, repositories, storage, hashadapter.New(), queue, audit,
			uploads.UploadPolicy{
				MaxUploadBytes:    settings.MaxUploadBytes,
				AllowedMIMETypes:  settings.AllowedUploadMIMETypes,
				AllowedExtensions: settings.AllowedUploadExtensions,
			},
		).WithSessions(repositories, storage, hashadapter.New()).WithDiscAssembly(discService, wakeCompressor, settings.CompressDiscs),
		ROMs:          rom.NewService(repositories, storage, audit, metrics.NewCountingScraper(NewScraper(settings), registry)).WithThumbnails(imaging.NewThumbnailer()).WithDiscs(discService, wakeCompressor),
		Libraries:     libraries.NewService(repositories, repositories),
		Sessions:      sessions,
		Metrics:       registry,
		Users:         users.NewService(repositories, hasher, sessions),
		Clients:       auth.NewClientService(repositories),
		Authenticator: auth.NewAuthenticator(tokens, repositories, repositories),
		Identifier:    persistence.NewIdentifier(repositories.DB).WithDiscs(discService),
	}
}

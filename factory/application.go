package factory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	authfeature "github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	"github.com/lcmps/retroxide/application/jobs"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
	jobsdomain "github.com/lcmps/retroxide/domain/jobs"
	"github.com/lcmps/retroxide/infrastructure/auth"
	"github.com/lcmps/retroxide/infrastructure/config"
	"github.com/lcmps/retroxide/infrastructure/persistence"
	"gorm.io/gorm"
)

type Application struct {
	Config   config.Config
	Database *gorm.DB
	Router   http.Handler
	Logger   *slog.Logger
	JobQueue jobs.Queue
	// JobPools are the background workers that process JobQueue.
	JobPools   []jobs.Pool
	Identifier jobs.Identifier
	// Maintenance runs periodic chores, such as removing unreferenced files, until its context
	// ends.
	Maintenance func(context.Context)
}

func NewApplication(ctx context.Context, settings config.Config, logger *slog.Logger) (*Application, error) {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	repositories, database, err := NewRepositories(ctx, settings)
	if err != nil {
		return nil, err
	}
	return assembleApplication(ctx, settings, logger, database, repositories)
}

func assembleApplication(ctx context.Context, settings config.Config, logger *slog.Logger, database *gorm.DB, repositories *persistence.Repositories) (*Application, error) {
	fail := func(buildErr error) (*Application, error) {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
		return nil, buildErr
	}
	storage := NewStorage(settings)
	if err := prepareStorage(ctx, logger, repositories, storage); err != nil {
		return fail(err)
	}
	hasher := auth.NewArgon2PasswordHasher()
	tokens := auth.NewJWTService(settings.JWTSecret, settings.JWTAccessExpirySecs, settings.JWTRefreshExpirySecs)
	if err := seedAdmin(ctx, repositories, hasher, settings.AdminEmail, settings.AdminPassword); err != nil {
		return fail(err)
	}
	identifyNotifier, scrapeNotifier := jobs.NewNotifier(), jobs.NewNotifier()
	queue := jobs.NewNotifyingQueue(repositories, identifyNotifier, scrapeNotifier)
	services := NewServices(settings, repositories, queue, storage, hasher, tokens, logger)
	pools := newJobPools(settings, queue, services, identifyNotifier, scrapeNotifier)
	router, err := NewRouter(settings, services, logger)
	if err != nil {
		return fail(err)
	}
	return &Application{
		Config: settings, Database: database, Router: router, Logger: logger,
		JobQueue: queue, JobPools: pools, Identifier: services.Identifier,
		Maintenance: func(ctx context.Context) {
			var chores sync.WaitGroup
			chores.Go(func() { runDiscCompression(ctx, logger, services.Discs, services.DiscsUploaded) })
			runMaintenance(ctx, logger,
				indexPendingFiles(services.Discs),
				expireUploadSessions(services.Uploads),
				expireUploadParts(repositories),
				sweepUnreferencedFiles(repositories, storage),
			)
			chores.Wait()
		},
	}, nil
}

func seedAdmin(ctx context.Context, users authfeature.UserRepository, hasher authfeature.PasswordHasher, email, password string) error {
	if email == "" && password == "" {
		return nil
	}
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return fault.Internal(errors.New("admin credentials must both be set and non-empty"))
	}
	existing, err := users.FindByEmail(ctx, email)
	if err != nil || existing != nil {
		return err
	}
	passwordHash, err := hasher.Hash(password)
	if err != nil {
		return err
	}
	userID, err := id.New("usr")
	if err != nil {
		return fault.Internal(fmt.Errorf("generating admin id: %w", err))
	}
	return users.SaveUser(ctx, authdomain.User{
		ID: userID, Email: email, PasswordHash: passwordHash, Role: identity.RoleAdmin,
	})
}

// newJobPools splits the workers so scraping, which waits on remote services, never delays
// identifying new uploads: one pool takes every job except scrapes, the other only scrapes.
// A zero scrape worker count (settings not loaded through config.Load) still runs one.
func newJobPools(settings config.Config, queue jobs.Enqueuer, services *Services, identifyNotifier, scrapeNotifier *jobs.Notifier) []jobs.Pool {
	handlers := jobs.ROMPipeline(services.Identifier, queue, services.ROMs)
	scrapes := jobsdomain.KindFilter{Kinds: []string{jobsdomain.KindScrapeROM}}
	return []jobs.Pool{
		{
			Name: "identify", Workers: settings.WorkerCount, Handlers: handlers, Notifier: identifyNotifier,
			Kinds: jobsdomain.KindFilter{Kinds: scrapes.Kinds, Exclude: true},
		},
		{Name: "scrape", Workers: max(settings.ScrapeWorkerCount, 1), Handlers: handlers, Notifier: scrapeNotifier, Kinds: scrapes},
	}
}

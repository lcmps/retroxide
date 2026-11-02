package factory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	authfeature "github.com/lcmps/retroxide/application/auth"
	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/id"
	"github.com/lcmps/retroxide/application/jobs"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
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
	// JobNotifier wakes workers when JobQueue receives a job.
	JobNotifier *jobs.Notifier
	Identifier  jobs.Identifier
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
	hasher := auth.NewArgon2PasswordHasher()
	tokens := auth.NewJWTService(settings.JWTSecret, settings.JWTAccessExpirySecs, settings.JWTRefreshExpirySecs)
	if err := seedAdmin(ctx, repositories, hasher, settings.AdminEmail, settings.AdminPassword); err != nil {
		return fail(err)
	}
	notifier := jobs.NewNotifier()
	queue := jobs.NewNotifyingQueue(repositories, notifier)
	services := NewServices(settings, repositories, queue, storage, hasher, tokens, logger)
	router, err := NewRouter(settings, services, logger)
	if err != nil {
		return fail(err)
	}
	return &Application{
		Config: settings, Database: database, Router: router, Logger: logger,
		JobQueue: queue, JobNotifier: notifier, Identifier: services.Identifier,
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

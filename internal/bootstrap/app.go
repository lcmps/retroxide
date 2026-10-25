package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lcmps/retroxide/internal/adapters/auth"
	hashadapter "github.com/lcmps/retroxide/internal/adapters/hash"
	httpadapter "github.com/lcmps/retroxide/internal/adapters/http"
	"github.com/lcmps/retroxide/internal/adapters/http/i18n"
	"github.com/lcmps/retroxide/internal/adapters/observability"
	"github.com/lcmps/retroxide/internal/adapters/persistence"
	storageadapter "github.com/lcmps/retroxide/internal/adapters/storage"
	authfeature "github.com/lcmps/retroxide/internal/features/auth"
	"github.com/lcmps/retroxide/internal/features/jobs"
	libraryfeature "github.com/lcmps/retroxide/internal/features/libraries"
	romfeature "github.com/lcmps/retroxide/internal/features/rom"
	"github.com/lcmps/retroxide/internal/features/uploads"
	"github.com/lcmps/retroxide/internal/platform/config"
	"github.com/lcmps/retroxide/internal/platform/id"
	"github.com/lcmps/retroxide/internal/shared/fault"
	"github.com/lcmps/retroxide/internal/shared/identity"
	"gorm.io/gorm"
)

const shutdownTimeout = 10 * time.Second

type App struct {
	config     config.Config
	database   *gorm.DB
	router     http.Handler
	logger     *slog.Logger
	jobQueue   jobs.Queue
	identifier jobs.Identifier
}

func Build(ctx context.Context, settings config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	database, err := persistence.OpenDatabase(ctx, settings.DBDriver, settings.SQLitePath, settings.MySQLURL)
	if err != nil {
		return nil, err
	}
	fail := func(buildErr error) (*App, error) {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
		return nil, buildErr
	}

	repositories := persistence.NewRepositories(database)
	storage := storageadapter.New(settings.StorageRoot)
	hasher := auth.NewArgon2PasswordHasher()
	tokens := auth.NewJWTService(settings.JWTSecret, settings.JWTAccessExpirySecs, settings.JWTRefreshExpirySecs)
	audit := observability.NewAuditLogger(logger)

	if err := seedAdmin(ctx, repositories, hasher, settings.AdminEmail, settings.AdminPassword); err != nil {
		return fail(err)
	}

	uploadService := uploads.NewUploadService(
		repositories, repositories, storage, hashadapter.New(), repositories, audit,
		uploads.UploadPolicy{
			MaxUploadBytes:    settings.MaxUploadBytes,
			AllowedMIMETypes:  settings.AllowedUploadMIMETypes,
			AllowedExtensions: settings.AllowedUploadExtensions,
		},
	)
	romService := romfeature.NewService(repositories, storage, audit)
	libraryService := libraryfeature.NewService(repositories, repositories)
	sessionService := authfeature.NewSessionService(repositories, hasher, tokens, repositories)
	clientService := authfeature.NewClientService(repositories)
	identifyService := uploads.NewIdentifyService(repositories, repositories)
	locales, err := i18n.Catalogs()
	if err != nil {
		return fail(fault.Internal(fmt.Errorf("loading locale catalogs: %w", err)))
	}

	router := httpadapter.NewRouter(httpadapter.Dependencies{
		Uploads:    uploadService,
		ROMs:       romService,
		Libraries:  libraryService,
		Sessions:   sessionService,
		Clients:    clientService,
		Tokens:     tokens,
		ClientRepo: repositories,
		Locales:    locales,
		Logger:     logger,
	}, httpadapter.Options{
		MaxUploadBytes: settings.MaxUploadBytes,
		CORSOrigins:    settings.CORSAllowedOrigins,
	})
	return &App{
		config: settings, database: database, router: router, logger: logger,
		jobQueue: repositories, identifier: identifyService,
	}, nil
}

func (app *App) Router() http.Handler {
	return app.router
}

func (app *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", net.JoinHostPort(app.config.Host, fmt.Sprint(app.config.Port)))
	if err != nil {
		return fmt.Errorf("binding HTTP listener: %w", err)
	}
	server := &http.Server{
		Handler:           app.router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	workerContext, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerResult := make(chan error, 1)
	go func() {
		workerResult <- jobs.Run(workerContext, app.config.WorkerCount, app.jobQueue, app.identifier)
	}()
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- server.Serve(listener)
	}()

	var runErr error
	workerFinished := false
	select {
	case <-ctx.Done():
		runErr = errors.Join(runErr, shutdownServer(server))
		if serveErr := <-serverResult; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = errors.Join(runErr, serveErr)
		}
	case serveErr := <-serverResult:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = errors.Join(runErr, serveErr)
		}
	case workerErr := <-workerResult:
		workerFinished = true
		if workerErr != nil && !errors.Is(workerErr, context.Canceled) {
			runErr = errors.Join(runErr, workerErr)
		}
		runErr = errors.Join(runErr, shutdownServer(server))
		if serveErr := <-serverResult; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = errors.Join(runErr, serveErr)
		}
	}
	cancelWorker()
	if !workerFinished {
		if workerErr := <-workerResult; workerErr != nil && !errors.Is(workerErr, context.Canceled) {
			runErr = errors.Join(runErr, workerErr)
		}
	}
	if closeErr := app.Close(); closeErr != nil {
		runErr = errors.Join(runErr, closeErr)
	}
	return runErr
}

func shutdownServer(server *http.Server) error {
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return server.Shutdown(shutdownContext)
}

func (app *App) Close() error {
	if app.database == nil {
		return nil
	}
	database, err := app.database.DB()
	if err != nil {
		return err
	}
	return database.Close()
}

func RunFromEnvironment() error {
	settings, err := config.LoadProcessEnvironment()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	app, err := Build(context.Background(), settings, logger)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx)
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
	return users.SaveUser(ctx, authfeature.User{
		ID: userID, Email: email, PasswordHash: passwordHash, Role: identity.RoleAdmin,
	})
}

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
	"syscall"
	"time"

	"github.com/lcmps/retroxide/application/jobs"
	"github.com/lcmps/retroxide/factory"
	"github.com/lcmps/retroxide/infrastructure/config"
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
	application, err := factory.NewApplication(ctx, settings, logger)
	if err != nil {
		return nil, err
	}
	return &App{
		config: application.Config, database: application.Database, router: application.Router, logger: application.Logger,
		jobQueue: application.JobQueue, identifier: application.Identifier,
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
	settings, err := factory.NewConfig()
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

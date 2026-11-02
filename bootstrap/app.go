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
	"sync/atomic"
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
	return newApp(application), nil
}

func newApp(application *factory.Application) *App {
	return &App{
		config: application.Config, database: application.Database, router: application.Router, logger: application.Logger,
		jobQueue: application.JobQueue, identifier: application.Identifier,
	}
}

func (app *App) Router() http.Handler {
	return app.router
}

func (app *App) Run(ctx context.Context) error {
	listener, err := listen(app.config.Host, app.config.Port)
	if err != nil {
		return err
	}
	server := newServer(app.router)
	return app.supervise(ctx, server, serve(server, listener))
}

func listen(host string, port uint16) (net.Listener, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, fmt.Errorf("binding HTTP listener: %w", err)
	}
	return listener, nil
}

func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func serve(server *http.Server, listener net.Listener) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- server.Serve(listener)
	}()
	return result
}

func (app *App) supervise(ctx context.Context, server *http.Server, serverResult <-chan error) error {
	workerContext, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerResult := make(chan error, 1)
	go func() {
		workerResult <- jobs.Run(workerContext, app.config.WorkerCount, app.jobQueue, app.identifier)
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	settings, err := factory.NewConfig()
	if errors.Is(err, config.ErrSetupRequired) {
		return runSetup(ctx, logger)
	}
	if err != nil {
		return err
	}
	app, err := Build(context.Background(), settings, logger)
	if err != nil {
		return err
	}
	return app.Run(ctx)
}

// runSetup serves the setup wizard, then switches the same listener to the full application once it completes.
func runSetup(ctx context.Context, logger *slog.Logger) error {
	process := config.Lookup(config.ProcessLookup)
	configPath := config.FilePath(process)
	fileValues, err := config.ReadFile(configPath)
	if err != nil {
		return err
	}
	host, port, err := config.ListenAddress(config.Layered(process, fileValues))
	if err != nil {
		return err
	}

	handler := &switchableHandler{}
	ready := make(chan *factory.Application, 1)
	service := factory.NewSetupService(configPath, process, logger, func(application *factory.Application) {
		handler.set(application.Router)
		ready <- application
	})
	setupRouter, err := factory.NewSetupRouter(service, logger)
	if err != nil {
		return err
	}
	handler.set(setupRouter)

	listener, err := listen(host, port)
	if err != nil {
		return err
	}
	server := newServer(handler)
	serverResult := serve(server, listener)
	logger.Warn("configuration required: complete setup in the web interface",
		"address", listener.Addr().String(), "config_file", configPath)

	select {
	case application := <-ready:
		logger.Info("setup completed", "config_file", configPath)
		return newApp(application).supervise(ctx, server, serverResult)
	case <-ctx.Done():
		runErr := shutdownServer(server)
		if serveErr := <-serverResult; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = errors.Join(runErr, serveErr)
		}
		select {
		case application := <-ready:
			runErr = errors.Join(runErr, newApp(application).Close())
		default:
		}
		return runErr
	case serveErr := <-serverResult:
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return serveErr
	}
}

type switchableHandler struct {
	current atomic.Pointer[http.Handler]
}

func (handler *switchableHandler) set(next http.Handler) {
	handler.current.Store(&next)
}

func (handler *switchableHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	(*handler.current.Load()).ServeHTTP(writer, request)
}

package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

var defaultShutdownTimeout = 5 * time.Second

// HTTPServer is the configured HTTP surface owned by the app lifecycle.
type HTTPServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

// ShutdownHook adapts Prism resources that need ordered shutdown.
type ShutdownHook func(context.Context) error

// Options lists the runtime resources App owns in explicit shutdown order.
type Options struct {
	HTTPServer        HTTPServer
	RealtimeShutdown  []ShutdownHook
	SideEffectDrain   []ShutdownHook
	SchedulerStop     ShutdownHook
	ServiceClose      []ShutdownHook
	TelemetryShutdown ShutdownHook
	DBClose           ShutdownHook
}

// App owns Prism runtime resources once startup has completed.
type App struct {
	httpServer        HTTPServer
	realtimeShutdown  []ShutdownHook
	sideEffectDrain   []ShutdownHook
	schedulerStop     ShutdownHook
	serviceClose      []ShutdownHook
	telemetryShutdown ShutdownHook
	dbClose           ShutdownHook

	shutdownOnce sync.Once
	shutdownErr  error
}

func NewApp(options Options) *App {
	return &App{
		httpServer:        options.HTTPServer,
		realtimeShutdown:  append([]ShutdownHook(nil), options.RealtimeShutdown...),
		sideEffectDrain:   append([]ShutdownHook(nil), options.SideEffectDrain...),
		schedulerStop:     options.SchedulerStop,
		serviceClose:      append([]ShutdownHook(nil), options.ServiceClose...),
		telemetryShutdown: options.TelemetryShutdown,
		dbClose:           options.DBClose,
	}
}

func (app *App) Run(ctx context.Context) error {
	if app == nil {
		<-ctx.Done()
		return nil
	}
	if app.httpServer == nil {
		<-ctx.Done()
		shutdownCtx, cancel := shutdownContext(ctx)
		defer cancel()
		return app.Shutdown(shutdownCtx)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- app.httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if isExpectedServeStop(err) {
			return nil
		}
		return errors.Join(err, app.Shutdown(ctx))
	case <-ctx.Done():
		shutdownCtx, cancel := shutdownContext(ctx)
		defer cancel()
		shutdownErr := app.Shutdown(shutdownCtx)
		err := <-serveErr
		if isExpectedServeStop(err) {
			return shutdownErr
		}
		return errors.Join(err, shutdownErr)
	}
}

func (app *App) Shutdown(ctx context.Context) error {
	if app == nil {
		return nil
	}
	app.shutdownOnce.Do(func() {
		var errs []error
		if app.httpServer != nil {
			logShutdownPhaseStarted("http_shutdown")
			shutdownErr := app.httpServer.Shutdown(ctx)
			errs = appendError(errs, shutdownErr)
			if shutdownErr != nil {
				if closer, ok := app.httpServer.(interface{ Close() error }); ok {
					errs = appendError(errs, closer.Close())
				}
			}
		}
		if len(app.realtimeShutdown) > 0 {
			logShutdownPhaseStarted("realtime_shutdown")
		}
		for _, hook := range app.realtimeShutdown {
			errs = appendError(errs, hook(ctx))
		}
		if len(app.sideEffectDrain) > 0 {
			logShutdownPhaseStarted("side_effect_drain")
		}
		for _, hook := range app.sideEffectDrain {
			errs = appendError(errs, hook(ctx))
		}
		if app.schedulerStop != nil {
			logShutdownPhaseStarted("scheduler_stop")
			errs = appendError(errs, app.schedulerStop(ctx))
		}
		if len(app.serviceClose) > 0 {
			logShutdownPhaseStarted("service_close")
		}
		for _, hook := range app.serviceClose {
			errs = appendError(errs, hook(ctx))
		}
		if app.telemetryShutdown != nil {
			logShutdownPhaseStarted("telemetry_shutdown")
			errs = appendError(errs, app.telemetryShutdown(ctx))
		}
		if app.dbClose != nil {
			logShutdownPhaseStarted("db_close")
			errs = appendError(errs, app.dbClose(ctx))
		}
		app.shutdownErr = errors.Join(errs...)
		slog.Info("lifecycle shutdown completed", "error", app.shutdownErr)
	})
	return app.shutdownErr
}

func shutdownContext(ctx context.Context) (context.Context, context.CancelFunc) {
	shutdownCtx := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(shutdownCtx, deadline)
	}
	return context.WithTimeout(shutdownCtx, defaultShutdownTimeout)
}

func logShutdownPhaseStarted(phase string) {
	slog.Info("lifecycle shutdown phase started", "phase", phase)
}

func appendError(errs []error, err error) []error {
	if err == nil {
		return errs
	}
	return append(errs, err)
}

func isExpectedServeStop(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed)
}

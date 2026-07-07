package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/lifecycle"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	platformtelemetry "github.com/coachpo/prism/backend/internal/platform/telemetry"
)

const (
	defaultBootstrapConfigPath = "config.json"
	startupRunTimeout          = 30 * time.Second
)

type startupRunner interface {
	Run(context.Context) (startup.Result, error)
}

var (
	newStartupRunner = func(options startup.Options) (startupRunner, error) {
		return startup.New(options)
	}
	newTelemetryProviders = platformtelemetry.BuildProviders
	newPlatformApp        = lifecycle.NewProductionApp
)

type bootstrapStartupConfig struct {
	Settings   config.Settings
	ConfigPath string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		if fatal, ok := err.(runError); ok {
			slog.Error(fatal.message, "error", fatal.err)
		} else {
			slog.Error("prism backend failed", "error", err)
		}
		os.Exit(1)
	}
}

type runError struct {
	message string
	err     error
}

func (e runError) Error() string {
	if e.err == nil {
		return e.message
	}
	return e.message + ": " + e.err.Error()
}

func (e runError) Unwrap() error {
	return e.err
}

func newRunError(message string, err error) error {
	return runError{message: message, err: err}
}

func run(ctx context.Context) error {
	bootstrapConfig, err := loadBootstrapSettings()
	if err != nil {
		return newRunError("failed to load bootstrap config", err)
	}
	settings := bootstrapConfig.Settings
	if shouldPrintEffectiveStartupSettings() {
		if err := printEffectiveStartupSettings(bootstrapConfig.ConfigPath, settings); err != nil {
			return newRunError("failed to print effective startup settings", err)
		}
		return nil
	}

	telemetryProviders, err := newTelemetryProviders(ctx, settings.Telemetry)
	if err != nil {
		return newRunError("failed to build telemetry providers", err)
	}
	telemetryOwnedByLifecycle := false
	defer func() {
		if !telemetryOwnedByLifecycle {
			shutdownStartupTelemetry(telemetryProviders)
		}
	}()

	startupService, err := newStartupRunner(startup.Options{
		DatabaseURL:         settings.DatabaseURL,
		SecretEncryptionKey: settings.SecretEncryptionKey,
		StepObserver: func(step startup.Step) {
			slog.Info("startup step started", slog.String("step", string(step)))
		},
	})
	if err != nil {
		return newRunError("failed to build startup service", err)
	}

	startupResult, err := runStartupWithTimeout(ctx, startupService, startupRunTimeout)
	if err != nil {
		return newRunError("startup sequence failed", err)
	}
	slog.Info(
		"startup sequence completed",
		"migration_outcome",
		startupResult.Migration.Outcome,
	)

	app, server, err := newPlatformApp(ctx, settings, lifecycle.ProductionOptions{
		TelemetryShutdown: telemetryProviders.Shutdown,
	})
	if err != nil {
		return newRunError("failed to build server", err)
	}
	telemetryOwnedByLifecycle = true

	slog.Info(
		"starting prism backend",
		"addr",
		server.Addr,
	)

	if err := app.Run(ctx); err != nil {
		return newRunError("server exited", err)
	}
	return nil
}

func runStartupWithTimeout(ctx context.Context, service startupRunner, timeout time.Duration) (startup.Result, error) {
	startupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return service.Run(startupCtx)
}

func shutdownStartupTelemetry(telemetryProviders interface{ Shutdown(context.Context) error }) {
	if telemetryProviders == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := telemetryProviders.Shutdown(shutdownCtx); err != nil {
		slog.Warn("startup telemetry shutdown failed", "error", err)
	}
}

func loadBootstrapSettings() (bootstrapStartupConfig, error) {
	bootstrapConfigPath := strings.TrimSpace(os.Getenv(config.BootstrapConfigPathEnv))
	if bootstrapConfigPath == "" {
		bootstrapConfigPath = defaultBootstrapConfigPath
	}

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	_, settings, err := loadBootstrapConfigDocumentWithRepair(manager, bootstrapConfigPath)
	if err != nil {
		return bootstrapStartupConfig{}, err
	}
	return bootstrapStartupConfig{
		Settings:   settings,
		ConfigPath: bootstrapConfigPath,
	}, nil
}

func loadBootstrapConfigDocumentWithRepair(manager config.BootstrapConfigManager, bootstrapConfigPath string) (config.BootstrapConfigSnapshot, config.Settings, error) {
	snapshot, settings, err := manager.LoadBootstrapConfigDocument(bootstrapConfigPath)
	if err == nil {
		return snapshot, settings, nil
	}
	if !shouldRepairBootstrapConfig(err) {
		return config.BootstrapConfigSnapshot{}, config.Settings{}, err
	}
	if err := reseedBootstrapConfig(manager, bootstrapConfigPath); err != nil {
		return config.BootstrapConfigSnapshot{}, config.Settings{}, err
	}
	return manager.LoadBootstrapConfigDocument(bootstrapConfigPath)
}

func shouldRepairBootstrapConfig(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && errors.Is(pathErr.Err, os.ErrNotExist) {
		return true
	}
	return strings.Contains(err.Error(), `unknown field "docsEnabled"`)
}

func reseedBootstrapConfig(manager config.BootstrapConfigManager, bootstrapConfigPath string) error {
	if err := os.Remove(bootstrapConfigPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale bootstrap config %q: %w", bootstrapConfigPath, err)
	}
	if _, err := manager.LoadOrSeed(bootstrapConfigPath); err != nil {
		return fmt.Errorf("reseed bootstrap config %q: %w", bootstrapConfigPath, err)
	}
	return nil
}

func shouldPrintEffectiveStartupSettings() bool {
	return os.Getenv("PRISM_PRINT_EFFECTIVE_STARTUP_SETTINGS") == "1"
}

func printEffectiveStartupSettings(bootstrapConfigPath string, settings config.Settings) error {
	if _, err := fmt.Fprintf(os.Stdout, "%s=%s\n", config.BootstrapConfigPathEnv, bootstrapConfigPath); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "DATABASE_URL=%s\n", settings.DatabaseURL); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "SERVER_HOST=%s\n", settings.Host); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "SERVER_PORT=%d\n", settings.Port); err != nil {
		return err
	}
	_, err := fmt.Fprintf(os.Stdout, "SERVER_ADDR=%s\n", settings.Address())
	return err
}

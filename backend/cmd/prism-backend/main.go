package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

const defaultBootstrapConfigPath = "config.json"

type bootstrapStartupConfig struct {
	Settings           config.Settings
	ConfigPath         string
	LoadedRevision     int
	LoadedDocumentETag string
}

func main() {
	bootstrapConfig, err := loadBootstrapSettings()
	if err != nil {
		slog.Error("failed to load bootstrap config", "error", err)
		os.Exit(1)
	}
	settings := bootstrapConfig.Settings
	if shouldPrintEffectiveStartupSettings() {
		if err := printEffectiveStartupSettings(bootstrapConfig.ConfigPath, settings); err != nil {
			slog.Error("failed to print effective startup settings", "error", err)
			os.Exit(1)
		}
		return
	}

	startupService, err := startup.New(startup.Options{
		DatabaseURL:         settings.DatabaseURL,
		SecretEncryptionKey: settings.SecretEncryptionKey,
	})
	if err != nil {
		slog.Error("failed to build startup service", "error", err)
		os.Exit(1)
	}

	startupResult, err := startupService.Run(context.Background())
	if err != nil {
		slog.Error("startup sequence failed", "error", err)
		os.Exit(1)
	}

	server, err := platformhttp.NewServer(settings, platformhttp.ServerOptions{
		BootstrapConfig: platformhttp.BootstrapConfigOptions{
			ConfigPath:         bootstrapConfig.ConfigPath,
			LoadedRevision:     bootstrapConfig.LoadedRevision,
			LoadedDocumentETag: bootstrapConfig.LoadedDocumentETag,
		},
	})
	if err != nil {
		slog.Error("failed to build server", "error", err)
		os.Exit(1)
	}

	slog.Info(
		"starting prism backend",
		"addr",
		server.Addr,
		"docs_enabled",
		settings.DocsEnabled(),
		"migration_outcome",
		startupResult.Migration.Outcome,
	)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func loadBootstrapSettings() (bootstrapStartupConfig, error) {
	bootstrapConfigPath := strings.TrimSpace(os.Getenv(config.BootstrapConfigPathEnv))
	if bootstrapConfigPath == "" {
		bootstrapConfigPath = defaultBootstrapConfigPath
	}

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	if _, err := manager.LoadOrSeed(bootstrapConfigPath); err != nil {
		return bootstrapStartupConfig{}, err
	}
	snapshot, settings, err := manager.LoadBootstrapConfigDocument(bootstrapConfigPath)
	if err != nil {
		return bootstrapStartupConfig{}, err
	}
	return bootstrapStartupConfig{
		Settings:           settings,
		ConfigPath:         bootstrapConfigPath,
		LoadedRevision:     snapshot.FileRevision,
		LoadedDocumentETag: snapshot.DocumentETag,
	}, nil
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

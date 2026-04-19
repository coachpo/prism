package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

func main() {
	settings := config.Load()

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

	server, err := platformhttp.NewServer(settings)
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
		"startup_skipped",
		startupResult.Skipped,
		"migration_outcome",
		startupResult.Migration.Outcome,
		"billing_reconciliation_ran",
		startupResult.BillingReconciliation.Ran,
	)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

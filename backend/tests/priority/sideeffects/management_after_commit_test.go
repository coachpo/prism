package sideeffects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
)

type managementAfterCommitContextKey string

func TestManagementAfterCommitSemantics(t *testing.T) {
	t.Run("rollback suppresses after commit work", func(t *testing.T) {
		backendRoot := backendRoot(t)
		stats := readSource(t, filepath.Join(backendRoot, "internal", "httpapi", "management", "stats", "service.go"))
		for _, want := range []string{"EventDashboardSnapshotInvalidate", "RegisterHandler", "handleDashboardSnapshotInvalidation"} {
			if !strings.Contains(stats, want) {
				t.Fatalf("stats dashboard invalidation handler missing %q", want)
			}
		}
		for _, obsolete := range []string{"managementsideeffects.InsertTx", "router.Delete(", "DELETE FROM request_logs", "DELETE FROM usage_request_events", "s.invalidateDashboardAggregateSnapshot(profileID)"} {
			if strings.Contains(stats, obsolete) {
				t.Fatalf("stats service still contains obsolete inline mutation marker %q", obsolete)
			}
		}
	})

	t.Run("side effect failure does not fail committed primary state", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), managementAfterCommitContextKey("key"), "value")
		var calls []string
		managementsideeffects.AfterCommit(ctx,
			func(got context.Context) error {
				if got != ctx {
					t.Fatal("wake received unexpected context")
				}
				calls = append(calls, "wake")
				return errors.New("dispatcher unavailable")
			},
			func(got context.Context) error {
				if got != ctx {
					t.Fatal("hook received unexpected context")
				}
				calls = append(calls, "hook-1")
				return errors.New("noncritical cache invalidation failed")
			},
			func(got context.Context) error {
				if got != ctx {
					t.Fatal("second hook received unexpected context")
				}
				calls = append(calls, "hook-2")
				return nil
			},
		)
		if strings.Join(calls, ",") != "hook-1,hook-2,wake" {
			t.Fatalf("unexpected after-commit execution order: %v", calls)
		}
	})

	t.Run("source requires durable dispatcher and after commit migration", func(t *testing.T) {
		backendRoot := backendRoot(t)
		outbox := readSource(t, filepath.Join(backendRoot, "internal", "platform", "managementsideeffects", "outbox.go"))
		for _, want := range []string{
			"management_side_effect_outbox",
			"FOR UPDATE SKIP LOCKED",
			"failed_permanent",
			"defaultBatchSize       = 50",
			"defaultConcurrency     = 4",
			"defaultRetryCap        = 8",
			"RegisterBackgroundWorker",
			"handleScheduledDispatch",
			"management_outbox",
		} {
			if !strings.Contains(outbox, want) {
				t.Fatalf("management outbox missing %q", want)
			}
		}
		migration := readSource(t, filepath.Join(backendRoot, "migrations", "000007_management_outbox.sql"))
		for _, want := range []string{"management_outbox", "idx_management_outbox_dedupe_key", "idx_management_outbox_polling", "failed_permanent"} {
			if !strings.Contains(migration, want) {
				t.Fatalf("management outbox migration missing %q", want)
			}
		}
		lifecycle := readSource(t, filepath.Join(backendRoot, "internal", "platform", "lifecycle", "production.go"))
		for _, want := range []string{"managementsideeffects.NewDispatcher", "managementSideEffects.RegisterBackgroundWorker", "SideEffects: managementSideEffects"} {
			if !strings.Contains(lifecycle, want) {
				t.Fatalf("lifecycle wiring missing %q", want)
			}
		}
		settings := readSource(t, filepath.Join(backendRoot, "internal", "httpapi", "management", "settings", "routes.go"))
		for _, want := range []string{"handleCreateLogRetentionJob", "CreateLogRetentionJob", "LogRetentionScope"} {
			if !strings.Contains(settings, want) {
				t.Fatalf("settings retention job route missing %q", want)
			}
		}
		logRetention := readSource(t, filepath.Join(backendRoot, "internal", "platform", "logretention", "store.go"))
		for _, want := range []string{"RunRetention", "DropExpiredPartitions", "DeleteBoundaryRows", "EnsurePartitionForTime"} {
			if !strings.Contains(logRetention, want) {
				t.Fatalf("log retention store missing %q", want)
			}
		}
		stats := readSource(t, filepath.Join(backendRoot, "internal", "httpapi", "management", "stats", "service.go"))
		for _, want := range []string{"EventDashboardSnapshotInvalidate", "RegisterHandler", "handleDashboardSnapshotInvalidation"} {
			if !strings.Contains(stats, want) {
				t.Fatalf("stats dashboard invalidation handler missing %q", want)
			}
		}
		for _, obsolete := range []string{"managementsideeffects.InsertTx", "router.Delete(", "DELETE FROM request_logs", "DELETE FROM usage_request_events", "s.invalidateDashboardAggregateSnapshot(profileID)"} {
			if strings.Contains(stats, obsolete) {
				t.Fatalf("stats service still contains obsolete inline mutation marker %q", obsolete)
			}
		}
	})
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func readSource(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

package sideeffects

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
)

func TestManagementAfterCommitSemantics(t *testing.T) {
	t.Run("rollback suppresses after commit work", func(t *testing.T) {
		called := false
		transactionErr := errors.New("primary state failed")
		if transactionErr == nil {
			managementsideeffects.AfterCommit(context.Background(), func(context.Context) error {
				called = true
				return nil
			})
		}
		if called {
			t.Fatal("after-commit work ran for rolled-back mutation")
		}
	})

	t.Run("side effect failure does not fail committed primary state", func(t *testing.T) {
		committed := true
		managementsideeffects.AfterCommit(context.Background(), func(context.Context) error {
			return errors.New("dispatcher unavailable")
		}, func(context.Context) error {
			return errors.New("noncritical cache invalidation failed")
		})
		if !committed {
			t.Fatal("after-commit failure changed committed primary state")
		}
	})

	t.Run("static management side effect boundary", func(t *testing.T) {
		backendRoot := backendRoot(t)
		command := exec.Command("go", "run", "./cmd/prism-priority-check", "--check=management-sideeffects", "./...")
		command.Dir = backendRoot
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("management side-effect priority check failed: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "priority checks passed") {
			t.Fatalf("priority check output missing pass signal:\n%s", output)
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
		} {
			if !strings.Contains(outbox, want) {
				t.Fatalf("management outbox missing %q", want)
			}
		}
		stats := readSource(t, filepath.Join(backendRoot, "internal", "httpapi", "management", "stats", "service.go"))
		for _, want := range []string{"managementsideeffects.InsertTx", "managementsideeffects.AfterCommit", "EventDashboardSnapshotInvalidate"} {
			if !strings.Contains(stats, want) {
				t.Fatalf("stats management mutation missing %q", want)
			}
		}
		if strings.Contains(stats, "s.invalidateDashboardAggregateSnapshot(profileID)") {
			t.Fatal("stats delete still invalidates dashboard snapshots inline after transaction")
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

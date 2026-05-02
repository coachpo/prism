package startup

import (
	"context"
	"strings"
	"testing"
)

func TestRunWithConnObservesMigrationsBeforeSeedSteps(t *testing.T) {
	observedSteps := []Step{}
	service, err := New(Options{
		MigrationsDir:       t.TempDir(),
		SecretEncryptionKey: "startup-order-test-secret",
		StepObserver: func(step Step) {
			observedSteps = append(observedSteps, step)
		},
	})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}

	_, err = service.RunWithConn(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no SQL migrations found") {
		t.Fatalf("expected empty migration directory error, got %v", err)
	}
	if len(observedSteps) != 1 || observedSteps[0] != StepMigrations {
		t.Fatalf("expected first observed startup step to be %q, got %v", StepMigrations, observedSteps)
	}
}

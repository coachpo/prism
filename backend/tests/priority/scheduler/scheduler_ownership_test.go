package scheduler

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSchedulerOwnsBackgroundWork(t *testing.T) {
	backendRoot := backendRoot(t)
	command := exec.Command("go", "run", "./cmd/prism-priority-check", "--check=unmanaged-goroutine", "--check=unregistered-background-work", "./...")
	command.Dir = backendRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("priority scheduler ownership check failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"priority checks passed",
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("priority check output missing %q:\n%s", want, output)
		}
	}
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

package async_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyFeedbackAndTelemetryAreBounded(t *testing.T) {
	repoRoot := findBackendRoot(t)
	runtimePath := filepath.Join(repoRoot, "internal", "httpapi", "runtime", "runtime.go")
	observabilityPath := filepath.Join(repoRoot, "internal", "httpapi", "runtime", "observability.go")
	feedbackPath := filepath.Join(repoRoot, "internal", "httpapi", "runtime", "feedback_pipeline.go")
	sideEffectsPath := filepath.Join(repoRoot, "internal", "httpapi", "runtime", "runtime_side_effects.go")

	runtimeSource := readSource(t, runtimePath)
	for _, forbidden := range []string{"runtimeFeedbackContext", "persist runtime success feedback", "persist runtime failure feedback", "persist runtime transport failure", "persist runtime probe-eligible feedback"} {
		if strings.Contains(runtimeSource, forbidden) {
			t.Fatalf("runtime proxy feedback path still contains synchronous feedback evidence %q", forbidden)
		}
	}
	for _, want := range []string{"recordRuntimeSuccess", "TryEnqueue", "recordRuntimeFailoverHTTPFailure", "recordRuntimeTransportFailure"} {
		if !strings.Contains(runtimeSource, want) {
			t.Fatalf("runtime proxy feedback path missing async evidence %q", want)
		}
	}

	observabilitySource := readSource(t, observabilityPath)
	for _, forbidden := range []string{"telemetryOutbox.Enqueue", "context.WithTimeout(context.Background(), 5*time.Second)", "context.WithTimeout(context.Background(), 5 * time.Second)"} {
		if strings.Contains(observabilitySource, forbidden) {
			t.Fatalf("runtime telemetry finalizer still directly persists telemetry with %q", forbidden)
		}
	}
	for _, want := range []string{"RuntimeActivityIntent", "SubmitRuntimeActivity", "RuntimeSideEffectAccepted"} {
		if !strings.Contains(observabilitySource, want) {
			t.Fatalf("runtime telemetry finalizer missing side-effect submission evidence %q", want)
		}
	}

	feedbackSource := readSource(t, feedbackPath)
	for _, want := range []string{"runtime_feedback", "runtime_feedback_pipeline", "TryEnqueue", "handleScheduledFeedback", "CoalesceNone"} {
		if !strings.Contains(feedbackSource, want) {
			t.Fatalf("runtime feedback pipeline missing bounded feedback evidence %q", want)
		}
	}

	sideEffectsSource := readSource(t, sideEffectsPath)
	for _, want := range []string{"runtime_side_effects_activity", "SubmitRuntimeActivity", "handleRuntimeActivity", "outbox.Enqueue", "DrainFlush"} {
		if !strings.Contains(sideEffectsSource, want) {
			t.Fatalf("runtime side-effect manager missing durable telemetry evidence %q", want)
		}
	}
}

func findBackendRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatalf("could not locate backend root from %s", wd)
	return ""
}

func readSource(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

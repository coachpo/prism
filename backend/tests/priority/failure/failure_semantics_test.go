package failure_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestPriorityFailureSemantics(t *testing.T) {
	t.Run("scheduler outage and DB lane exhaustion fail visibly", func(t *testing.T) {
		var nilScheduler *background.Scheduler
		if got := nilScheduler.Submit(context.Background(), background.JobRequest{Worker: "missing"}); got.Status != background.SubmitRejectedUnknownWorker {
			t.Fatalf("nil scheduler should reject work as unavailable, got %+v", got)
		}
		controller := admission.NewController(admission.Limits{Proxy: 1, ManagementM2: 1, Background: 1})
		_, releaseProxy, err := controller.Admit(context.Background(), admission.Spec{Name: "held proxy", Metadata: priority.Metadata{Priority: priority.PriorityProxy}, Timeout: time.Second})
		if err != nil {
			t.Fatalf("hold proxy lane: %v", err)
		}
		defer releaseProxy()
		_, _, err = controller.Admit(context.Background(), admission.Spec{Name: "exhausted proxy", Metadata: priority.Metadata{Priority: priority.PriorityProxy}, Timeout: time.Second})
		var overload *admission.OverloadError
		if !errors.As(err, &overload) || overload.Resource != "proxy admission" || overload.RetryAfter <= 0 {
			t.Fatalf("expected proxy lane exhaustion overload, got %v", err)
		}
	})

	t.Run("telemetry is durable while feedback is lossy under pressure", func(t *testing.T) {
		sideEffects := readBackendFile(t, "internal/httpapi/runtime/runtime_side_effects.go")
		for _, want := range []string{"RuntimeSideEffectAccepted", "RetryPolicy", "terminalFailure", "ForcedAbandoned", "outbox.Enqueue"} {
			if !strings.Contains(sideEffects, want) {
				t.Fatalf("runtime side-effect source missing durable telemetry semantic %q", want)
			}
		}
		feedback := readBackendFile(t, "internal/httpapi/runtime/feedback_pipeline.go")
		for _, want := range []string{"RuntimeFeedbackDroppedBackpressure", "pipeline_closed", "dropped_invalid", "return background.JobResult{Status: background.JobSucceeded}"} {
			if !strings.Contains(feedback, want) {
				t.Fatalf("runtime feedback source missing lossy semantic %q", want)
			}
		}
	})

	t.Run("rollback restart and cache-generation race semantics remain structural", func(t *testing.T) {
		management := readBackendFile(t, "internal/platform/managementsideeffects/outbox.go")
		for _, want := range []string{"AfterCommit(context.Background(), dispatcher.Wake", "status IN ('pending', 'retry')", "failed_permanent", "FOR UPDATE SKIP LOCKED"} {
			if !strings.Contains(management, want) {
				t.Fatalf("management side-effect source missing rollback/retry semantic %q", want)
			}
		}
		telemetry := readBackendFile(t, "internal/httpapi/runtime/telemetry_outbox.go")
		for _, want := range []string{"runtime_telemetry_outbox", "FOR UPDATE SKIP LOCKED", "Close() TelemetryOutboxCloseResult", "PendingRows", "runtime telemetry outbox closed"} {
			if !strings.Contains(telemetry, want) {
				t.Fatalf("telemetry outbox source missing restart/drain semantic %q", want)
			}
		}
		cache := readBackendFile(t, "internal/httpapi/runtime/cache.go")
		for _, want := range []string{"ErrRuntimeSnapshotGenerationChanged", "beforeVector", "afterVector", "ErrRuntimeSnapshotRefreshRequired"} {
			if !strings.Contains(cache, want) {
				t.Fatalf("cache source missing generation-race semantic %q", want)
			}
		}
		middleware := readBackendFile(t, "internal/platform/http/runtime_cache_invalidation.go")
		for _, forbidden := range []string{"RefreshNow(ctx, request)", "failed to publish runtime auth snapshot immediately"} {
			if strings.Contains(middleware, forbidden) {
				t.Fatalf("cache invalidation middleware still has authoritative fallback %q", forbidden)
			}
		}
	})
}

func readBackendFile(t *testing.T, relativePath string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(backendRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(raw)
}

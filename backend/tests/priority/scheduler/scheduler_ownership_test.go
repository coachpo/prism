package scheduler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSchedulerOwnsBackgroundWork(t *testing.T) {
	backendRoot := backendRoot(t)
	assertFileContainsAll(t, backendRoot, "internal/platform/lifecycle/production.go", []string{
		"background.NewScheduler(background.Config{})",
		"runtimePlanningCache.RegisterBackgroundWorker",
		"managementAuthService.RegisterBackgroundWorkers",
		"logRetentionStore.RegisterBackgroundWorker",
		"asyncDashboardPublisher.RegisterBackgroundWorker",
		"runtimeService.RegisterBackgroundWorkers",
		"resources.scheduler.Start(ctx)",
		"SchedulerStop:",
		"resources.schedulerStopHook()",
		"resources.scheduler.Stop(ctx, deadline)",
	})
	assertFileExcludesAll(t, backendRoot, "internal/platform/http/server.go", []string{
		"OpenDatabasePools",
		"DatabasePools.Close",
		"databasePools.Close",
		"background.NewScheduler",
		"RegisterBackgroundWorker",
		"RegisterBackgroundWorkers",
		"resources.scheduler.Start",
		"resources.scheduler.Stop",
		".Start(context.Background())",
		".Stop(ctx",
		"RegisterOnShutdown",
	})
	assertFileContainsAll(t, backendRoot, "internal/httpapi/runtime/cache.go", []string{
		"RegisterBackgroundWorker",
		"runtime_shared_cache_refresh",
		"handleScheduledRefresh",
	})
	assertFileContainsAll(t, backendRoot, "internal/httpapi/runtime/telemetry_outbox.go", []string{
		"RegisterBackgroundWorker",
		"runtime_telemetry_outbox",
		"handleScheduledTelemetry",
	})
	assertFileContainsAll(t, backendRoot, "internal/httpapi/runtime/runtime_side_effects.go", []string{
		"RegisterBackgroundWorker",
		"runtime_side_effects_activity",
		"handleRuntimeActivity",
	})
	assertFileContainsAll(t, backendRoot, "internal/httpapi/runtime/feedback_pipeline.go", []string{
		"RegisterBackgroundWorker",
		"runtime_feedback_pipeline",
		"handleScheduledFeedback",
	})
	assertFileContainsAll(t, backendRoot, "internal/httpapi/realtime/async_publisher.go", []string{
		"RegisterBackgroundWorker",
		"async_dashboard_publisher",
		"handleScheduledPublish",
	})
	assertFileContainsAll(t, backendRoot, "internal/httpapi/management/auth/proxy_key_usage_writer.go", []string{
		"RegisterBackgroundWorker",
		"proxy_key_usage_writer",
		"handleScheduledFlush",
	})
	assertFileContainsAll(t, backendRoot, "internal/platform/managementsideeffects/outbox.go", []string{
		"RegisterBackgroundWorker",
		"management_side_effect_outbox",
		"handleScheduledDispatch",
		"EventDashboardSnapshotInvalidate",
	})

	assertFileContainsAll(t, backendRoot, "internal/platform/logretention/maintenance.go", []string{
		"RegisterBackgroundWorker",
		"log_partition_maintenance",
		"handlePartitionMaintenance",
		"PeriodicTrigger:  &background.PeriodicTrigger{Interval: maintenanceInterval}",
		"maintenanceInterval = time.Hour",
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

func assertFileContainsAll(t *testing.T, backendRoot string, relativePath string, wants []string) {
	t.Helper()
	text := readBackendFile(t, backendRoot, relativePath)
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing %q", relativePath, want)
		}
	}
}

func assertFileExcludesAll(t *testing.T, backendRoot string, relativePath string, forbidden []string) {
	t.Helper()
	text := readBackendFile(t, backendRoot, relativePath)
	for _, value := range forbidden {
		if strings.Contains(text, value) {
			t.Fatalf("%s still contains %q", relativePath, value)
		}
	}
}

func readBackendFile(t *testing.T, backendRoot string, relativePath string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(backendRoot, relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(content)
}

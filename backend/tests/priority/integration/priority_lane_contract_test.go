package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestPriorityLaneContract(t *testing.T) {
	t.Run("inventory classifies route resource job ownership", func(t *testing.T) {
		inventory := priority.DefaultInventory()
		if problems := inventory.ValidationProblems(); len(problems) != 0 {
			t.Fatalf("priority inventory should be fully classified, first problem: %+v", problems[0])
		}
		resourcePriorities := map[string]priority.LogicalPriority{}
		for _, resource := range inventory.Resources {
			resourcePriorities[resource.Name] = resource.Priority
		}
		for name, want := range map[string]priority.LogicalPriority{
			"runtime execution pool":        priority.PriorityProxy,
			"management pool":               priority.PriorityManagement,
			"runtime telemetry pool":        priority.PriorityBackground,
			"runtime feedback pool":         priority.PriorityBackground,
			"runtime shared-cache refresh":  priority.PriorityBackground,
			"management side-effect outbox": priority.PriorityBackground,
			"auth email outbox enqueue":     priority.PriorityManagement,
			"runtime shared-cache readers":  priority.PriorityProxy,
		} {
			if got := resourcePriorities[name]; got != want {
				t.Fatalf("resource %q priority = %q, want %q", name, got, want)
			}
		}
	})

	t.Run("physical lanes and metrics labels are explicit", func(t *testing.T) {
		lanes := platformdb.ComponentLaneAssignments()
		for _, lane := range []config.PostgresPoolLane{config.PostgresLaneRuntimeExecution, config.PostgresLaneRuntimeTelemetry, config.PostgresLaneRuntimeFeedback, config.PostgresLaneManagement, config.PostgresLaneRealtime, config.PostgresLaneCacheRefresh, config.PostgresLaneBackgroundJobs} {
			if got, ok := lanes[string(lane)]; !ok || got != lane {
				t.Fatalf("lane %q missing or mismatched in component assignments: %q ok=%v", lane, got, ok)
			}
		}
		dbSource := readBackendFile(t, "internal/platform/db/pools.go")
		for _, marker := range []string{"prism_db_pool_acquired_connections{lane=%q}", "prism_db_pool_max_connections{lane=%q}", "AcquireTimeoutCount", "postgres pool budget"} {
			if !strings.Contains(dbSource, marker) {
				t.Fatalf("db pool metrics/budget source missing %q", marker)
			}
		}
	})

	t.Run("server wiring binds components to declared lanes", func(t *testing.T) {
		server := readBackendFile(t, "internal/platform/http/server.go")
		for _, want := range []string{
			"runtimeExecutionPool := databasePools.RuntimeExecution.Raw()",
			"runtimeTelemetryPool := databasePools.RuntimeTelemetry.Raw()",
			"runtimeFeedbackPool := databasePools.RuntimeFeedback.Raw()",
			"managementPool := databasePools.Management.Raw()",
			"realtimePool := databasePools.Realtime.Raw()",
			"cacheRefreshPool := databasePools.CacheRefresh.Raw()",
			"backgroundJobsPool := databasePools.BackgroundJobs.Raw()",
			"FeedbackPool: runtimeFeedbackPool",
			"RealtimePool: realtimePool",
			"RefreshPool: cacheRefreshPool",
			"ProxyKeyUsagePool: backgroundJobsPool",
			"TelemetryPool: runtimeTelemetryPool",
		} {
			if !strings.Contains(server, want) {
				t.Fatalf("server wiring missing declared lane marker %q", want)
			}
		}
		for _, forbidden := range []string{"FeedbackPool: runtimeExecutionPool", "FeedbackPool: runtimeTelemetryPool", "RealtimePool: managementPool", "RefreshPool: managementPool", "TelemetryPool: runtimeExecutionPool", "ProxyKeyUsagePool: managementPool"} {
			if strings.Contains(server, forbidden) {
				t.Fatalf("server wiring borrows forbidden lane %q", forbidden)
			}
		}
	})

	t.Run("admission evidence separates proxy management and background", func(t *testing.T) {
		admissionSource := readBackendFile(t, "internal/platform/http/admission.go")
		for _, want := range []string{"priority.PriorityProxy", "priority.PriorityManagement", "ManagementTierM1", "ManagementTierM2", "ManagementTierM3", "writeAdmissionError", "Retry-After"} {
			if !strings.Contains(admissionSource, want) {
				t.Fatalf("admission source missing %q", want)
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

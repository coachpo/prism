package db_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
)

func TestDBPoolLaneIsolation(t *testing.T) {
	t.Run("default budget names every physical lane", func(t *testing.T) {
		budget := config.DefaultPostgresPoolsBudget()
		if err := budget.Validate(); err != nil {
			t.Fatalf("default budget should validate: %v", err)
		}
		if budget.TotalMaxConns != 24 || budget.SumMaxConns() != 24 {
			t.Fatalf("expected total budget and lane sum to be 24, got total=%d sum=%d", budget.TotalMaxConns, budget.SumMaxConns())
		}
		for _, lane := range []config.PostgresPoolLane{config.PostgresLaneRuntimeExecution, config.PostgresLaneRuntimeTelemetry, config.PostgresLaneRuntimeFeedback, config.PostgresLaneManagement, config.PostgresLaneRealtime, config.PostgresLaneCacheRefresh, config.PostgresLaneBackgroundJobs} {
			if _, ok := platformdb.ComponentLaneAssignments()[string(lane)]; !ok {
				t.Fatalf("lane %q missing from component assignments", lane)
			}
		}
	})

	t.Run("bootstrap validation fails missing zero and over budget lanes", func(t *testing.T) {
		testCases := []struct {
			name    string
			mutate  func(map[string]any)
			wantErr string
		}{
			{
				name: "zero realtime lane",
				mutate: func(payload map[string]any) {
					payload["database"].(map[string]any)["pools"].(map[string]any)["realtime"].(map[string]any)["maxConns"] = float64(0)
				},
				wantErr: "invalid postgres pool config: lane=realtime max_conns must be greater than zero",
			},
			{
				name: "missing cache refresh lane",
				mutate: func(payload map[string]any) {
					delete(payload["database"].(map[string]any)["pools"].(map[string]any), "cacheRefresh")
				},
				wantErr: "invalid postgres pool config: lane=cache_refresh is required",
			},
			{
				name: "over budget",
				mutate: func(payload map[string]any) {
					payload["database"].(map[string]any)["pools"].(map[string]any)["totalMaxConns"] = float64(10)
				},
				wantErr: "postgres pool budget exceeded: total_max_conns=10 lane_sum=24",
			},
		}
		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				payload := loadBootstrapPayload(t)
				testCase.mutate(payload)
				raw, err := json.Marshal(payload)
				if err != nil {
					t.Fatalf("marshal mutated bootstrap payload: %v", err)
				}
				_, err = config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{}).Parse(raw)
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("expected error containing %q, got %v", testCase.wantErr, err)
				}
			})
		}
	})

	t.Run("lifecycle wiring uses isolated non management lanes", func(t *testing.T) {
		content := readBackendFile(t, "internal/platform/lifecycle/production.go")
		for _, want := range []string{
			"runtimeFeedbackPool := databasePools.RuntimeFeedback.Raw()",
			"realtimePool := databasePools.Realtime.Raw()",
			"cacheRefreshPool := databasePools.CacheRefresh.Raw()",
			"backgroundJobsPool := databasePools.BackgroundJobs.Raw()",
			"FeedbackPool: runtimeFeedbackPool",
			"RealtimePool: realtimePool",
			"RefreshPool: cacheRefreshPool",
			"ProxyKeyUsagePool: backgroundJobsPool",
			"platformdb.OpenDatabasePools(ctx, settings.DatabaseURL",
			"resources.dbClose = func(context.Context) error",
			"databasePools.Close()",
			"SchedulerStop:    resources.schedulerStopHook()",
			"DBClose:          resources.dbClose",
		} {
			if !strings.Contains(content, want) {
				t.Fatalf("expected lifecycle wiring to contain %q", want)
			}
		}
		for _, forbidden := range []string{"FeedbackPool: runtimeExecutionPool", "FeedbackPool: runtimeTelemetryPool", "RealtimePool: managementPool", "RefreshPool: managementPool", "ProxyKeyUsagePool: managementPool"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("lifecycle wiring contains forbidden management or runtime lane borrowing %q", forbidden)
			}
		}
		serverContent := readBackendFile(t, "internal/platform/http/server.go")
		for _, forbidden := range []string{"OpenDatabasePools", "DatabasePools.Close", "databasePools.Close", "background.NewScheduler", "RegisterBackgroundWorker", "RegisterBackgroundWorkers", "RegisterOnShutdown"} {
			if strings.Contains(serverContent, forbidden) {
				t.Fatalf("server assembly still owns app lifecycle through %q", forbidden)
			}
		}
	})
}

func loadBootstrapPayload(t *testing.T) map[string]any {
	t.Helper()
	raw := readBackendFile(t, "testdata/bootstrap/bootstrap-valid-v1.json")
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode bootstrap fixture: %v", err)
	}
	pools := payload["database"].(map[string]any)["pools"].(map[string]any)
	pools["totalMaxConns"] = float64(24)
	pools["management"] = map[string]any{"maxConns": float64(4), "minIdleConns": float64(1)}
	pools["runtimeExecution"] = map[string]any{"maxConns": float64(8), "minIdleConns": float64(2)}
	pools["runtimeTelemetry"] = map[string]any{"maxConns": float64(4), "minIdleConns": float64(1)}
	pools["runtimeFeedback"] = map[string]any{"maxConns": float64(2), "minIdleConns": float64(0)}
	pools["realtime"] = map[string]any{"maxConns": float64(2), "minIdleConns": float64(0)}
	pools["cacheRefresh"] = map[string]any{"maxConns": float64(2), "minIdleConns": float64(0)}
	pools["backgroundJobs"] = map[string]any{"maxConns": float64(2), "minIdleConns": float64(0)}
	return payload
}

func readBackendFile(t *testing.T, relativePath string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	raw, err := os.ReadFile(filepath.Join(backendRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(raw)
}

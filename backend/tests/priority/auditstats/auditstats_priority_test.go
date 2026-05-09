package auditstats

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestPartitionRetentionGuardrails(t *testing.T) {
	root := backendRoot(t)
	files := []string{
		"internal/httpapi/management/settings/routes.go",
		"internal/httpapi/management/stats/service.go",
		"internal/httpapi/management/audit/service.go",
		"internal/httpapi/management/loadbalance/observability.go",
		"internal/platform/managementjobs/jobs.go",
		"internal/platform/logretention/store.go",
	}
	for _, relative := range files {
		source := readSource(t, root, relative)
		for _, forbidden := range []string{
			"VACUUM FULL",
			"CLUSTER",
			"pg_repack",
		} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s contains forbidden retention operation %q", relative, forbidden)
			}
		}
		for _, tableName := range []string{"request_logs", "audit_logs", "usage_request_events", "loadbalance_events"} {
			pattern := regexp.MustCompile(`(?i)DELETE\s+FROM\s+(?:public\.)?` + tableName + `\b`)
			if pattern.MatchString(source) {
				t.Fatalf("%s contains forbidden parent-root DELETE FROM %s", relative, tableName)
			}
		}
	}
	logRetention := readSource(t, root, "internal/platform/logretention/store.go")
	if !strings.Contains(logRetention, "DeleteBoundaryRows") || !strings.Contains(logRetention, "quoteQualified(publicSchema, partition.PartitionName)") {
		t.Fatalf("boundary cleanup must target a listed child partition")
	}
	if !strings.Contains(logRetention, "VACUUM (ANALYZE, PROCESS_TOAST TRUE)") {
		t.Fatalf("boundary cleanup must vacuum/analyze the child partition")
	}
	if !strings.Contains(logRetention, "EnsurePartitionHorizonForTable") {
		t.Fatalf("delete_all must recreate the target table partition horizon")
	}
	for _, want := range []string{"DropExpiredPartitions", "DropAllPartitions", "quoteQualified(publicSchema, partition.PartitionName)", "DELETE FROM `+quoteQualified(publicSchema, partition.PartitionName)+` WHERE created_at < $1"} {
		if !strings.Contains(logRetention, want) {
			t.Fatalf("log retention source missing child-partition guardrail marker %q", want)
		}
	}

	migration := readSource(t, root, "migrations/000013_partitioned_log_retention.sql")
	for _, want := range []string{"request_log_id bigint", "request_log_created_at timestamp with time zone", "ingress_request_id character varying(36)"} {
		if !strings.Contains(migration, want) {
			t.Fatalf("partitioned audit schema missing weak request marker %q", want)
		}
	}
	for _, forbidden := range []string{"REFERENCES public.request_logs", "REFERENCES request_logs", "FOREIGN KEY (request_log_id"} {
		if strings.Contains(migration, forbidden) {
			t.Fatalf("partitioned audit schema reintroduces hard request-log coupling marker %q", forbidden)
		}
	}

	lifecycle := readSource(t, root, "internal/platform/lifecycle/production.go")
	for _, want := range []string{"backgroundJobsPool := databasePools.BackgroundJobs.Raw()", "logRetentionStore := logretention.NewStore(logretention.Options{Pool: backgroundJobsPool})", "managementJobs := managementjobs.NewStore(managementjobs.Options{Pool: backgroundJobsPool"} {
		if !strings.Contains(lifecycle, want) {
			t.Fatalf("lifecycle source missing retention lane marker %q", want)
		}
	}
	for _, forbidden := range []string{"logretention.Options{Pool: runtimeExecutionPool}", "logretention.Options{Pool: runtimeTelemetryPool}", "logretention.Options{Pool: runtimeFeedbackPool}", "managementjobs.Options{Pool: runtimeExecutionPool", "managementjobs.Options{Pool: runtimeTelemetryPool", "managementjobs.Options{Pool: runtimeFeedbackPool"} {
		if strings.Contains(lifecycle, forbidden) {
			t.Fatalf("retention or management jobs borrow protected runtime lane marker %q", forbidden)
		}
	}
}

func TestBoundedAggregationCheckpoint(t *testing.T) {
	root := backendRoot(t)
	rollups := readSource(t, root, "internal/domain/stats/rollups.go")
	jobs := readSource(t, root, "internal/platform/managementjobs/jobs.go")
	lifecycle := readSource(t, root, "internal/platform/lifecycle/production.go")
	audit := readSource(t, root, "internal/httpapi/management/audit/service.go")

	for _, want := range []string{"management_stat_buckets", "management_stat_refresh_state", "source_high_water_mark", "DashboardStatsStaleAfter"} {
		if !strings.Contains(rollups, want) {
			t.Fatalf("dashboard rollup implementation missing %q", want)
		}
	}
	if strings.Contains(rollups, "LoadDashboardStats") && strings.Contains(sourceBetween(rollups, "func LoadDashboardStats", "func RefreshDashboardStatsRollup"), "request_logs") {
		t.Fatalf("LoadDashboardStats must not read live request_logs")
	}
	for _, want := range []string{"WorkerName", "management_audit_delete_jobs", "FOR UPDATE SKIP LOCKED", "defaultBatchSize", "progress_json"} {
		if !strings.Contains(jobs, want) {
			t.Fatalf("audit delete job worker missing %q", want)
		}
	}
	if !strings.Contains(lifecycle, "managementJobs.RegisterBackgroundWorker") || !strings.Contains(lifecycle, "backgroundJobsPool := databasePools.BackgroundJobs.Raw()") {
		t.Fatalf("management jobs worker must be registered through the background scheduler lane")
	}
	if !strings.Contains(audit, "http.StatusAccepted") || strings.Contains(sourceBetween(audit, "func (s *Service) handleDeleteLogs", "func (s *Service) handleCreateDeleteJob"), "auditdomain.DeleteLogs(") {
		t.Fatalf("audit delete handler must enqueue asynchronous jobs instead of deleting inline")
	}
}

func readSource(t *testing.T, root string, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(raw)
}

func sourceBetween(source string, start string, end string) string {
	_, remainder, ok := strings.Cut(source, start)
	if !ok {
		return ""
	}
	remainder = start + remainder
	before, _, ok := strings.Cut(remainder, end)
	if !ok {
		return remainder
	}
	return before
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

package auditstats

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	startIndex := strings.Index(source, start)
	if startIndex < 0 {
		return ""
	}
	remainder := source[startIndex:]
	endIndex := strings.Index(remainder, end)
	if endIndex < 0 {
		return remainder
	}
	return remainder[:endIndex]
}

func backendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

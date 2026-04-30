package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
)

var phase7Now = time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)

func TestManagementAuditKeysetPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "audit_keyset_pagination")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertAuditLog(t, ctx, conn, profileID, 2101, phase7Now.Add(-3*time.Minute))
	phase7InsertAuditLog(t, ctx, conn, profileID, 2102, phase7Now.Add(-2*time.Minute))
	phase7InsertAuditLog(t, ctx, conn, profileID, 2103, phase7Now.Add(-1*time.Minute))
	from := phase7Now.Add(-time.Hour)
	to := phase7Now.Add(time.Minute)

	first, err := auditdomain.ListLogs(ctx, conn, auditdomain.ListParams{ProfileID: profileID, FromTime: &from, ToTime: &to, Limit: 1})
	if err != nil {
		t.Fatalf("load first audit keyset page: %v", err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != 2103 || first.NextCursor == nil || !first.HasMore {
		t.Fatalf("expected first page newest row with cursor, got %+v", first)
	}
	second, err := auditdomain.ListLogs(ctx, conn, auditdomain.ListParams{ProfileID: profileID, FromTime: &from, ToTime: &to, Limit: 1, Cursor: *first.NextCursor})
	if err != nil {
		t.Fatalf("load second audit keyset page: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != 2102 || second.NextCursor == nil || !second.HasMore {
		t.Fatalf("expected second keyset page to continue after newest row, got %+v", second)
	}
}

func TestManagementAuditQueryUsesBoundedIndex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "audit_bounded_indexes")
	defer func() { _ = conn.Close(ctx) }()
	for _, indexName := range []string{"idx_audit_logs_profile_created_id_desc", "idx_audit_logs_profile_request_created_id_desc", "idx_audit_logs_profile_status_created_id_desc"} {
		if phase7CountRows(t, ctx, conn, `SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'audit_logs' AND indexname = $1`, indexName) != 1 {
			t.Fatalf("expected bounded audit index %s to exist", indexName)
		}
	}
	source := phase7ReadBackendSource(t, "internal/domain/audit/service.go")
	listLogsSource := phase7SourceBetween(source, "func ListLogs", "func GetLog")
	if !strings.Contains(listLogsSource, "ORDER BY created_at DESC, id DESC") || !strings.Contains(listLogsSource, "LIMIT $") || !strings.Contains(listLogsSource, "limit+1") {
		t.Fatalf("expected audit list source to use bounded keyset ordering and limit+1, got:\n%s", listLogsSource)
	}
}

func TestManagementAuditNoBroadCount(t *testing.T) {
	source := phase7ReadBackendSource(t, "internal/domain/audit/service.go")
	listLogsSource := phase7SourceBetween(source, "func ListLogs", "func GetLog")
	if strings.Contains(strings.ToUpper(listLogsSource), "COUNT(") || strings.Contains(strings.ToUpper(listLogsSource), " OFFSET ") {
		t.Fatalf("audit ListLogs must not use broad COUNT or OFFSET, got:\n%s", listLogsSource)
	}
	if !strings.Contains(listLogsSource, "HasMore") || !strings.Contains(listLogsSource, "NextCursor") {
		t.Fatalf("expected audit ListLogs to derive pagination from cursor and limit+1, got:\n%s", listLogsSource)
	}
}

func TestManagementStatsReadFromRollupsOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_rollups_only")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1001, 500, phase7Now.Add(-time.Hour))

	response, err := stats.LoadDashboardStats(ctx, conn, profileID, "24h", phase7Now)
	if err != nil {
		t.Fatalf("load dashboard stats without rollups: %v", err)
	}
	if response.Metrics.RequestCount != 0 || !response.Freshness.Stale {
		t.Fatalf("expected dashboard read to avoid live request_logs fallback, got %+v", response)
	}
}

func TestManagementStatsRefreshHighWaterMark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_high_water")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1101, 200, phase7Now.Add(-time.Hour))
	phase7InsertRequestLog(t, ctx, conn, profileID, 1102, 500, phase7Now.Add(-30*time.Minute))
	phase7InsertAuditLog(t, ctx, conn, profileID, 1201, phase7Now.Add(-20*time.Minute))

	if err := stats.RefreshDashboardStatsRollup(ctx, conn, profileID, "24h", phase7Now); err != nil {
		t.Fatalf("refresh dashboard stats rollup: %v", err)
	}
	response, err := stats.LoadDashboardStats(ctx, conn, profileID, "24h", phase7Now)
	if err != nil {
		t.Fatalf("load refreshed dashboard stats: %v", err)
	}
	if response.Metrics.RequestCount != 2 || response.Metrics.ErrorCount != 1 || response.Metrics.AuditEventCount != 1 || response.Freshness.Stale {
		t.Fatalf("expected refreshed dashboard metrics with fresh high-water mark, got %+v", response)
	}
}

func TestManagementStatsStaleNoLiveFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_stale_no_fallback")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1301, 200, phase7Now.Add(-time.Minute))
	if _, err := conn.Exec(ctx, `INSERT INTO management_stat_buckets (bucket_start, bucket_size, metric, dimension_key, dimension_value, value, source_high_water_mark, generated_at) VALUES ($1, '24h', 'request_count', 'profile_id', $2, 7, $3, $3)`, phase7Now.Add(-24*time.Hour).Truncate(time.Hour), fmt.Sprintf("%d", profileID), phase7Now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("seed stale dashboard rollup: %v", err)
	}
	response, err := stats.LoadDashboardStats(ctx, conn, profileID, "24h", phase7Now)
	if err != nil {
		t.Fatalf("load stale dashboard stats: %v", err)
	}
	if response.Metrics.RequestCount != 7 || !response.Freshness.Stale {
		t.Fatalf("expected stale rollup value without live fallback, got %+v", response)
	}
}

func TestManagementStructuredLogsForAuditStatsJobs(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	managementjobs.LogTransition("job_phase7_structured", "running")
	logLine := output.String()
	if !strings.Contains(logLine, `"msg":"management.job.transition"`) || !strings.Contains(logLine, `"job_id":"job_phase7_structured"`) || !strings.Contains(logLine, `"state":"running"`) {
		t.Fatalf("expected structured management job transition log fields, got %s", logLine)
	}

	rollups := phase7ReadBackendSource(t, "internal/domain/stats/rollups.go")
	if !strings.Contains(rollups, "management_stat_refresh_state") || !strings.Contains(rollups, "last_source_high_water_mark") || !strings.Contains(rollups, "last_success_at") {
		t.Fatalf("expected dashboard stats refresh to persist structured high-water state, got:\n%s", rollups)
	}
}

func TestManagementRolloutRejectsLegacyUnboundedAuditRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "audit_legacy_unbounded_reject")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)

	_, err := auditdomain.ListLogs(ctx, conn, auditdomain.ListParams{ProfileID: profileID, Limit: 50})
	if err == nil {
		t.Fatal("expected legacy unbounded audit list to be rejected")
	}
	httpErr, ok := err.(*auditdomain.HTTPError)
	if !ok || httpErr.Code != "audit_window_required" || httpErr.StatusCode != 400 {
		t.Fatalf("expected audit_window_required for unbounded list, got %#v", err)
	}
}

func TestManagementAuditDeleteJobChunking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_chunking")
	defer pool.Close()
	for id := 1; id <= 501; id++ {
		phase7InsertAuditLog(t, ctx, pool, profileID, id, phase7Now.Add(-48*time.Hour))
	}
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "chunking", false)
	if err := store.ProcessDue(ctx); err != nil {
		t.Fatalf("process first audit delete chunk: %v", err)
	}
	job, err := store.GetJob(ctx, job.ID, profileID)
	if err != nil {
		t.Fatalf("reload chunked job: %v", err)
	}
	if job.State != "queued" || job.Progress.RowsDeleted != 500 || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 1 {
		t.Fatalf("expected one bounded chunk to delete 500 rows and requeue, got job=%+v", job)
	}
}

func TestManagementAuditDeleteJobSuccessfulChunksDoNotConsumeRetryAttempts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_many_chunks")
	defer pool.Close()
	for id := 1; id <= 4501; id++ {
		phase7InsertAuditLog(t, ctx, pool, profileID, id, phase7Now.Add(-48*time.Hour))
	}
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "many-chunks", false)
	for chunks := 0; chunks < 12; chunks++ {
		if err := store.ProcessDue(ctx); err != nil {
			t.Fatalf("process successful audit delete chunk %d: %v", chunks, err)
		}
		job, _ = store.GetJob(ctx, job.ID, profileID)
		if job.State == "succeeded" {
			break
		}
	}
	if job.State != "succeeded" || job.AttemptCount != 0 || job.Progress.BatchesCompleted != 10 || job.Progress.RowsDeleted != 4501 {
		t.Fatalf("expected >8 successful chunks to finish without consuming retry attempts, got job=%+v", job)
	}
	if remaining := phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID); remaining != 0 {
		t.Fatalf("expected all audit rows deleted after many chunks, got %d", remaining)
	}
}

func TestManagementAuditDeleteJobTransientFailuresBoundedByMaxAttempts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_failure_attempts")
	defer pool.Close()
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "failure-attempts", false)
	if _, err := pool.Exec(ctx, `DROP TABLE audit_logs`); err != nil {
		t.Fatalf("drop audit_logs to force transient delete failures: %v", err)
	}
	for attempt := 1; attempt <= 8; attempt++ {
		if err := store.ProcessDue(ctx); err == nil {
			t.Fatalf("expected forced delete failure on attempt %d", attempt)
		}
		job, _ = store.GetJob(ctx, job.ID, profileID)
		if attempt < 8 {
			if job.State != "queued" || job.AttemptCount != attempt {
				t.Fatalf("expected queued retry after failure %d, got job=%+v", attempt, job)
			}
			if _, err := pool.Exec(ctx, `UPDATE management_jobs SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, job.ID); err != nil {
				t.Fatalf("make failed job due again: %v", err)
			}
		}
	}
	if job.State != "failed" || job.AttemptCount != 8 || job.ErrorCode == nil || *job.ErrorCode != "transient_delete_error" {
		t.Fatalf("expected max transient failures to mark job failed visibly, got job=%+v", job)
	}
}

func TestManagementAuditDeleteJobActiveLeaseNotReclaimed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_active_lease")
	defer pool.Close()
	phase7InsertAuditLog(t, ctx, pool, profileID, 1, phase7Now.Add(-48*time.Hour))
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "active-lease", false)
	if _, err := pool.Exec(ctx, `UPDATE management_jobs SET state = 'running', locked_by = 'other-worker', locked_until = now() + interval '5 minutes', last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("seed active running lease: %v", err)
	}
	if err := store.ProcessDue(ctx); err != nil {
		t.Fatalf("process due with active running lease: %v", err)
	}
	job, err := store.GetJob(ctx, job.ID, profileID)
	if err != nil {
		t.Fatalf("reload active lease job: %v", err)
	}
	if job.State != "running" || job.Progress.RowsDeleted != 0 || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 1 {
		t.Fatalf("expected active running lease to be respected without processing, got job=%+v", job)
	}
	phase7AssertJobLease(t, ctx, pool, job.ID, "other-worker", true)
}

func TestManagementAuditDeleteJobStaleLeaseResumes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_stale_lease")
	defer pool.Close()
	phase7InsertAuditLog(t, ctx, pool, profileID, 1, phase7Now.Add(-48*time.Hour))
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "stale-lease", false)
	if _, err := pool.Exec(ctx, `UPDATE management_jobs SET state = 'running', locked_by = 'stale-worker', locked_until = now() - interval '1 second', last_heartbeat_at = now() - interval '5 minutes', updated_at = now() WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("seed stale running lease: %v", err)
	}
	if err := store.ProcessDue(ctx); err != nil {
		t.Fatalf("process due with stale running lease: %v", err)
	}
	job, err := store.GetJob(ctx, job.ID, profileID)
	if err != nil {
		t.Fatalf("reload stale lease job: %v", err)
	}
	if job.State != "queued" || job.Progress.RowsDeleted != 1 || job.Progress.BatchesCompleted != 1 || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 0 {
		t.Fatalf("expected stale running lease to resume and process one chunk, got job=%+v", job)
	}
}

func TestManagementAuditDeleteJobResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_resume")
	defer pool.Close()
	for id := 1; id <= 3; id++ {
		phase7InsertAuditLog(t, ctx, pool, profileID, id, phase7Now.Add(-48*time.Hour))
	}
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "resume", false)
	for attempts := 0; attempts < 3; attempts++ {
		if err := store.ProcessDue(ctx); err != nil {
			t.Fatalf("process audit delete resume attempt %d: %v", attempts, err)
		}
		job, _ = store.GetJob(ctx, job.ID, profileID)
		if job.State == "succeeded" {
			break
		}
	}
	if job.State != "succeeded" || job.Progress.RowsDeleted != 3 || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 0 {
		t.Fatalf("expected resumed delete job to finish, got job=%+v", job)
	}
}

func TestManagementAuditDeleteJobCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_cancel")
	defer pool.Close()
	phase7InsertAuditLog(t, ctx, pool, profileID, 1, phase7Now.Add(-48*time.Hour))
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "cancel", false)
	job, err := store.CancelJob(ctx, job.ID, profileID)
	if err != nil {
		t.Fatalf("cancel audit delete job: %v", err)
	}
	if job.State != "cancelled" || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 1 {
		t.Fatalf("expected queued cancel to leave audit rows untouched, got job=%+v", job)
	}
}

func TestManagementAuditDeleteJobDoesNotDeleteJobAudit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_job_events")
	defer pool.Close()
	phase7InsertAuditLog(t, ctx, pool, profileID, 1, phase7Now.Add(-48*time.Hour))
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "events", true)
	for attempts := 0; attempts < 3; attempts++ {
		if err := store.ProcessDue(ctx); err != nil {
			t.Fatalf("process audit delete all attempt %d: %v", attempts, err)
		}
	}
	if phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM management_job_events WHERE job_id = $1`, job.ID) == 0 {
		t.Fatalf("expected management job events to survive audit_logs deletion for job %s", job.ID)
	}
}

func phase7MigratedConn(t *testing.T, ctx context.Context, name string) *pgx.Conn {
	t.Helper()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, ctx, name)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", name, err)
	}
	return conn
}

func phase7JobStore(t *testing.T, ctx context.Context, name string) (*managementjobs.Store, *pgxpool.Pool, int) {
	t.Helper()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, ctx, name)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", name, err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(name))
	if err != nil {
		t.Fatalf("open phase7 pool: %v", err)
	}
	return managementjobs.NewStore(managementjobs.Options{Pool: pool, Now: func() time.Time { return phase7Now }}), pool, profileID
}

func phase7CreateDeleteJob(t *testing.T, ctx context.Context, store *managementjobs.Store, profileID int, key string, deleteAll bool) managementjobs.Job {
	t.Helper()
	before := phase7Now.Add(-24 * time.Hour)
	job, err := store.CreateAuditDeleteJob(ctx, managementjobs.CreateAuditDeleteJobRequest{ProfileID: profileID, RequestedBy: fmt.Sprintf("profile:%d", profileID), IdempotencyKey: key, Reason: "retention cleanup", Scope: managementjobs.AuditDeleteScope{Before: &before, DeleteAll: deleteAll}})
	if err != nil {
		t.Fatalf("create audit delete job: %v", err)
	}
	return job
}

func phase7InsertProfile(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) int {
	t.Helper()
	var profileID int
	if err := exec.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ('Phase 7', NULL, TRUE, TRUE, TRUE, 1, $1, $1) RETURNING id`, phase7Now).Scan(&profileID); err != nil {
		t.Fatalf("insert phase7 profile: %v", err)
	}
	return profileID
}

func phase7InsertRequestLog(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, statusCode int, createdAt time.Time) {
	t.Helper()
	if _, err := exec.Exec(ctx, `INSERT INTO request_logs (id, profile_id, model_id, api_family, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'phase7-model', 'openai', $3, 100, FALSE, $4, TRUE, TRUE, '/v1/chat/completions', $5, FALSE, FALSE)`, id, profileID, statusCode, statusCode >= 200 && statusCode < 300, createdAt.UTC()); err != nil {
		t.Fatalf("insert phase7 request log %d: %v", id, err)
	}
}

func phase7InsertAuditLog(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, createdAt time.Time) {
	t.Helper()
	if _, err := exec.Exec(ctx, `INSERT INTO audit_logs (id, profile_id, model_id, request_method, request_url, request_headers, request_body_stored, response_status, response_headers, response_body_stored, is_stream, duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, 'phase7-model', 'POST', 'https://phase7.invalid/v1/chat/completions', '{}', FALSE, 200, '{}', FALSE, FALSE, 100, TRUE, FALSE, $3)`, id, profileID, createdAt.UTC()); err != nil {
		t.Fatalf("insert phase7 audit log %d: %v", id, err)
	}
}

func phase7CountRows(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, query string, args ...any) int {
	t.Helper()
	var count int
	if err := exec.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("count phase7 rows: %v", err)
	}
	return count
}

func phase7AssertJobLease(t *testing.T, ctx context.Context, pool *pgxpool.Pool, jobID string, lockedBy string, lockedInFuture bool) {
	t.Helper()
	var gotLockedBy string
	var lockIsFuture bool
	if err := pool.QueryRow(ctx, `SELECT COALESCE(locked_by, ''), locked_until > now() FROM management_jobs WHERE id = $1`, jobID).Scan(&gotLockedBy, &lockIsFuture); err != nil {
		t.Fatalf("load job lease for %s: %v", jobID, err)
	}
	if gotLockedBy != lockedBy || lockIsFuture != lockedInFuture {
		t.Fatalf("expected lease locked_by=%q future=%v, got locked_by=%q future=%v", lockedBy, lockedInFuture, gotLockedBy, lockIsFuture)
	}
}

func phase7ReadBackendSource(t *testing.T, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(phase7BackendRoot(t), relative))
	if err != nil {
		t.Fatalf("read backend source %s: %v", relative, err)
	}
	return string(raw)
}

func phase7BackendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func phase7SourceBetween(source string, start string, end string) string {
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

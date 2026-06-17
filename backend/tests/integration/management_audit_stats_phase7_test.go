package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/domain/stats"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	platformtelemetry "github.com/coachpo/prism/backend/internal/platform/telemetry"
)

var phase7Now = time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)

func TestLogRetentionJobDropsExpiredPartitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, ctx, "log_retention_job_partitions")
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString("log_retention_job_partitions"))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	dayOne := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	for offset := range 3 {
		if err := retentionStore.EnsurePartitionForTime(ctx, "request_logs", dayOne.AddDate(0, 0, offset)); err != nil {
			t.Fatalf("ensure request_logs partition %d: %v", offset, err)
		}
	}
	jobStore := managementjobs.NewStore(managementjobs.Options{Pool: pool, LogRetention: retentionStore, Now: func() time.Time { return phase7Now }})
	cutoff := dayOne.AddDate(0, 0, 1).Add(12 * time.Hour)
	job, err := jobStore.CreateLogRetentionJob(ctx, managementjobs.CreateLogRetentionJobRequest{RequestedBy: "test", Reason: "drop expired partitions", Scope: managementjobs.LogRetentionScope{Table: "request_logs", Cutoff: &cutoff}})
	if err != nil {
		t.Fatalf("create log retention job: %v", err)
	}
	if err := jobStore.ProcessDue(ctx); err != nil {
		t.Fatalf("process log retention job: %v", err)
	}
	partitions, err := retentionStore.ListPartitions(ctx, "request_logs")
	if err != nil {
		t.Fatalf("list request_logs partitions: %v", err)
	}
	if phase7PartitionExists(partitions, "request_logs_p20260110") {
		t.Fatalf("expected expired partition to be dropped after job %s", job.ID)
	}
	if !phase7PartitionExists(partitions, "request_logs_p20260111") || !phase7PartitionExists(partitions, "request_logs_p20260112") {
		t.Fatalf("expected boundary and future partitions to remain, got %+v", partitions)
	}
	completed, err := jobStore.GetGlobalJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("load completed job: %v", err)
	}
	if completed.State != "succeeded" {
		t.Fatalf("expected job to succeed, got %s: %v", completed.State, completed.ErrorMessage)
	}
}

func TestLogRetentionSettingsAndJobRoutesAreGlobal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, ctx, "log_retention_settings_routes")
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString("log_retention_settings_routes"))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	jobStore := managementjobs.NewStore(managementjobs.Options{Pool: pool, LogRetention: retentionStore, Now: func() time.Time { return phase7Now }})
	service, err := managementsettings.NewService(config.Settings{}, managementsettings.Options{Pool: pool, Jobs: jobStore, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create settings service: %v", err)
	}
	router := chiRouterForSettings(service)
	put := httptest.NewRequest(http.MethodPut, "/settings/log-retention", bytes.NewBufferString(`{"request_logs_retention_days":3,"audit_logs_retention_days":4,"statistics_retention_days":5,"loadbalance_events_retention_days":6}`))
	put.Header.Set("X-Profile-Id", "999999")
	put.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, put)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT /settings/log-retention status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/settings/log-retention", nil)
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), `"loadbalance_events_retention_days":6`) {
		t.Fatalf("GET /settings/log-retention status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/maintenance/log-retention/jobs", bytes.NewBufferString(`{"table":"loadbalance_events","reason":"global loadbalance retention"}`))
	post.Header.Set("Content-Type", "application/json")
	postRecorder := httptest.NewRecorder()
	router.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusAccepted || !strings.Contains(postRecorder.Body.String(), `"job_id"`) {
		t.Fatalf("POST /maintenance/log-retention/jobs status=%d body=%s", postRecorder.Code, postRecorder.Body.String())
	}
	var jobPayload map[string]any
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &jobPayload); err != nil {
		t.Fatalf("decode log retention job response: %v", err)
	}
	scope, ok := jobPayload["scope"].(map[string]any)
	if !ok || scope["table"] != "loadbalance_events" || scope["cutoff"] == nil {
		t.Fatalf("expected loadbalance retention job cutoff fallback, got %+v", jobPayload)
	}
}

func TestManagementGlobalQueuedLogRetentionCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "global_queued_cancel")
	defer pool.Close()
	job := phase7CreateLogRetentionJob(t, ctx, store, "global-queued-cancel")
	service, err := managementaudit.NewService(config.Settings{}, managementaudit.Options{Pool: pool, Jobs: store, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create audit service: %v", err)
	}
	router := chiRouterForAudit(service)
	request := httptest.NewRequest(http.MethodPost, "/management/jobs/"+job.ID+"/cancel", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("POST global queued cancel status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	cancelled, err := store.GetGlobalJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("reload global queued job: %v", err)
	}
	if cancelled.State != "cancelled" || !cancelled.CancelRequested || cancelled.FinishedAt == nil {
		t.Fatalf("expected queued global log retention job to cancel, got %+v", cancelled)
	}
	if events := phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM management_job_events WHERE job_id = $1 AND event_type = 'cancel_requested'`, job.ID); events != 1 {
		t.Fatalf("expected one cancel_requested event for queued global job, got %d", events)
	}
}

func TestManagementGlobalRunningLogRetentionCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "global_running_cancel")
	defer pool.Close()
	job := phase7CreateLogRetentionJob(t, ctx, store, "global-running-cancel")
	if _, err := pool.Exec(ctx, `UPDATE management_jobs SET state = 'running', locked_by = 'phase7-worker', locked_until = now() + interval '5 minutes', last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("seed running global job: %v", err)
	}
	service, err := managementaudit.NewService(config.Settings{}, managementaudit.Options{Pool: pool, Jobs: store, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create audit service: %v", err)
	}
	router := chiRouterForAudit(service)
	request := httptest.NewRequest(http.MethodPost, "/management/jobs/"+job.ID+"/cancel", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("POST global running cancel status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	cancelled, err := store.GetGlobalJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("reload global running job: %v", err)
	}
	if cancelled.State != "cancel_requested" || !cancelled.CancelRequested || cancelled.FinishedAt != nil {
		t.Fatalf("expected running global log retention job to request cancellation, got %+v", cancelled)
	}
}

func TestManagementGlobalCancelPreservesProfileStoreErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "global_cancel_store_error")
	defer pool.Close()
	service, err := managementaudit.NewService(config.Settings{}, managementaudit.Options{Pool: pool, Jobs: store, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create audit service: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE management_jobs RENAME TO management_jobs_unavailable`); err != nil {
		t.Fatalf("break management job store: %v", err)
	}
	router := chiRouterForAudit(service)
	request := httptest.NewRequest(http.MethodPost, "/management/jobs/job_missing/cancel", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "Job not found") {
		t.Fatalf("expected profile cancel store error to remain 500, got status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

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

func TestDashboardRollupHelperReadsRollupsOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_rollups_only")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1001, 500, phase7Now.Add(-time.Hour))

	response, err := stats.LoadDashboardRollupStats(ctx, conn, profileID, "24h", phase7Now)
	if err != nil {
		t.Fatalf("load internal dashboard rollups without rows: %v", err)
	}
	if response.Metrics.RequestCount != 0 || !response.Health.Stale {
		t.Fatalf("expected internal rollup helper to avoid live request_logs fallback, got %+v", response)
	}
}

func TestManagementDashboardStatsRouteReturnsAggregateSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_dashboard_aggregate_route"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1002, 200, phase7Now.Add(-30*time.Minute))
	phase7InsertUsageEvent(t, ctx, conn, profileID, 1003, phase7Now.Add(-30*time.Minute))
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open dashboard stats pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)

	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard?window=24h", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode dashboard aggregate response: %v", err)
	}
	for _, key := range []string{"generated_at", "snapshot_revision", "source_watermark", "coverage_24h", "coverage_30d", "health", "metric_snapshot", "api_family_rows", "top_spending_models", "routing_health_map", "topology_graph"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected aggregate dashboard field %q, got %+v", key, payload)
		}
	}
	for _, legacyKey := range []string{"window", "covers", "freshness", "metrics", legacyDashboardActivityRowsKey()} {
		if _, ok := payload[legacyKey]; ok {
			t.Fatalf("dashboard route must not expose legacy top-level %q, got %+v", legacyKey, payload)
		}
	}
	if revision, ok := payload["snapshot_revision"].(string); !ok || len(revision) != 26 {
		t.Fatalf("expected dashboard snapshot_revision ULID, got %+v", payload["snapshot_revision"])
	}
	watermark := payload["source_watermark"].(map[string]any)
	if watermark["latest_usage_event_id"] != float64(1003) || watermark["latest_usage_event_created_at"] == nil {
		t.Fatalf("expected dashboard source watermark from latest usage event, got %+v", watermark)
	}
	metricSnapshot := payload["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(1) || metricSnapshot["success_rate"] != float64(100) {
		t.Fatalf("expected aggregate dashboard metrics from seeded activity, got %+v", metricSnapshot)
	}
}

func TestDashboardStatsEmptyProfileStatsOnlyContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_dashboard_empty_profile_contract"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open empty dashboard stats pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)

	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard?window=24h", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard empty profile status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{legacyDashboardActivityRowsKey(), "request_log_id", "ingress_request_id", "request_cursor"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("empty dashboard snapshot must not expose request-log payload field %q: %s", forbidden, body)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode empty dashboard response: %v", err)
	}
	if revision, ok := payload["snapshot_revision"].(string); !ok || len(revision) != 26 {
		t.Fatalf("expected empty dashboard snapshot_revision ULID, got %+v", payload["snapshot_revision"])
	}
	watermark, ok := payload["source_watermark"].(map[string]any)
	if !ok {
		t.Fatalf("expected empty dashboard source_watermark object, got %+v", payload["source_watermark"])
	}
	if watermark["latest_usage_event_created_at"] != nil || watermark["latest_usage_event_id"] != nil {
		t.Fatalf("expected empty dashboard source watermark nulls, got %+v", watermark)
	}
	metricSnapshot := payload["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(0) {
		t.Fatalf("expected empty dashboard total_requests=0, got %+v", metricSnapshot)
	}
}

func TestDashboardRecentActivityEmptyContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "dashboard_recent_activity_empty"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open recent activity pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()

	payload := phase7StatsRoutePayload(t, chiRouterForStats(service), "/stats/dashboard/recent-activity", profileID)
	if _, ok := payload["generated_at"].(string); !ok {
		t.Fatalf("expected recent activity generated_at string, got %+v", payload)
	}
	watermark := payload["activity_watermark"].(map[string]any)
	if watermark["latest_request_log_created_at"] != nil || watermark["latest_request_log_id"] != nil {
		t.Fatalf("expected empty recent activity watermark nulls, got %+v", watermark)
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("expected empty recent activity items, got %+v", payload)
	}
}

func TestDashboardRecentActivityBoundedContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "dashboard_recent_activity_bounded"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	route := phase7InsertDashboardRoutingTarget(t, ctx, conn, profileID, "recent-activity")
	for i := range 55 {
		phase7InsertRecentActivityRequestLog(t, ctx, conn, profileID, 4200+i, route, http.StatusOK, phase7Now.Add(-time.Duration(i+3)*time.Minute))
	}
	phase7InsertRecentActivityRequestLog(t, ctx, conn, profileID, 4100, route, http.StatusOK, phase7Now.Add(-time.Minute))
	phase7InsertRecentActivityRequestLog(t, ctx, conn, profileID, 4101, route, http.StatusOK, phase7Now.Add(-time.Minute))
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open bounded recent activity pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)

	defaultPayload := phase7StatsRoutePayload(t, router, "/stats/dashboard/recent-activity", profileID)
	defaultItems := defaultPayload["items"].([]any)
	if len(defaultItems) != 12 {
		t.Fatalf("expected default recent activity limit 12, got %d", len(defaultItems))
	}
	first := defaultItems[0].(map[string]any)
	second := defaultItems[1].(map[string]any)
	if first["request_log_id"] != float64(4101) || second["request_log_id"] != float64(4100) {
		t.Fatalf("expected created_at/id deterministic ordering, got first=%+v second=%+v", first, second)
	}
	for _, key := range []string{"request_log_id", "created_at", "model_id", "model_label", "resolved_target_model_id", "resolved_target_model_label", "endpoint_id", "endpoint_label", "status_code", "response_time_ms", "ttft_ms", "completion_duration_ms", "is_stream", "stream_outcome", "total_tokens", "total_cost_user_currency_micros", "priced_flag", "unpriced_reason", "report_currency_symbol"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("expected recent activity item field %q, got %+v", key, first)
		}
	}
	for _, forbidden := range []string{"id", "api_family", "connection_id", "terminal_target_id", "filter_options", "offset"} {
		if _, ok := first[forbidden]; ok {
			t.Fatalf("recent activity item must not expose request-log list field %q: %+v", forbidden, first)
		}
	}
	if first["model_label"] != "Phase 7 recent-activity" || first["endpoint_label"] != "Phase 7 recent-activity Endpoint" {
		t.Fatalf("expected recent activity labels from current config, got %+v", first)
	}
	watermark := defaultPayload["activity_watermark"].(map[string]any)
	if watermark["latest_request_log_id"] != float64(4101) || watermark["latest_request_log_created_at"] == nil {
		t.Fatalf("expected recent activity watermark from newest request log, got %+v", watermark)
	}
	maxPayload := phase7StatsRoutePayload(t, router, "/stats/dashboard/recent-activity?limit=99", profileID)
	if maxItems := maxPayload["items"].([]any); len(maxItems) != 50 {
		t.Fatalf("expected recent activity max limit 50, got %d", len(maxItems))
	}
	explicitPayload := phase7StatsRoutePayload(t, router, "/stats/dashboard/recent-activity?limit=2", profileID)
	if explicitItems := explicitPayload["items"].([]any); len(explicitItems) != 2 {
		t.Fatalf("expected explicit recent activity limit 2, got %d", len(explicitItems))
	}
	invalidRequest := httptest.NewRequest(http.MethodGet, "/stats/dashboard/recent-activity?limit=0", nil)
	invalidRequest.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	invalidRecorder := httptest.NewRecorder()
	router.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest || !strings.Contains(invalidRecorder.Body.String(), "invalid limit") {
		t.Fatalf("expected invalid recent activity limit 400, got status=%d body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}

func TestDashboardRecentActivityDoesNotPolluteSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "dashboard_recent_activity_snapshot_clean"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	route := phase7InsertDashboardRoutingTarget(t, ctx, conn, profileID, "snapshot-clean")
	phase7InsertRecentActivityRequestLog(t, ctx, conn, profileID, 4301, route, http.StatusOK, phase7Now.Add(-time.Minute))
	phase7InsertUsageEvent(t, ctx, conn, profileID, 4302, phase7Now.Add(-time.Minute))
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open snapshot clean pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()

	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard?window=24h", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	chiRouterForStats(service).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{legacyDashboardActivityRowsKey(), "recent_activity", "activity_watermark", "request_log_id"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("dashboard snapshot must stay stats-only and not expose %q: %s", forbidden, body)
		}
	}
}

func TestDashboardSnapshotStatsOnlyBuilderWithUsageEventsWithoutRequestLogs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_dashboard_usage_without_request_logs"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	firstUsageAt := phase7Now.Add(-30 * time.Minute)
	secondUsageAt := phase7Now.Add(-20 * time.Minute)
	phase7EnsureLogPartition(t, ctx, conn, "usage_request_events", firstUsageAt)
	phase7EnsureLogPartition(t, ctx, conn, "usage_request_events", secondUsageAt)
	if _, err := conn.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, stream_outcome, created_at, response_time_ms) VALUES ($1, $2, $3, 'phase7-model', 'openai', 'phase7-model endpoint', 200, TRUE, TRUE, TRUE, 7, 11, 18, 1250, 'USD', '$', 1, '/v1/chat/completions', 'completed', $4, 100)`, 1701, profileID, "phase7-usage-without-request-log-1", firstUsageAt); err != nil {
		t.Fatalf("insert streaming usage event without request log: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, stream_outcome, created_at, response_time_ms) VALUES ($1, $2, $3, 'phase7-model', 'openai', 'phase7-model endpoint', 200, TRUE, TRUE, TRUE, 3, 5, 8, 500, 'USD', '$', 1, '/v1/chat/completions', 'not_streaming', $4, 80)`, 1702, profileID, "phase7-usage-without-request-log-2", secondUsageAt); err != nil {
		t.Fatalf("insert non-streaming usage event without request log: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open usage-only dashboard stats pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)
	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard?window=24h", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard usage-only status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode usage-only dashboard response: %v", err)
	}
	watermark := payload["source_watermark"].(map[string]any)
	if watermark["latest_usage_event_id"] != float64(1702) || watermark["latest_usage_event_created_at"] == nil {
		t.Fatalf("expected usage-only source watermark, got %+v", watermark)
	}
	metricSnapshot := payload["metric_snapshot"].(map[string]any)
	if metricSnapshot["stream_share"] != float64(50) {
		t.Fatalf("expected usage-event-backed stream share, got %+v", metricSnapshot)
	}
}

func TestDashboardSnapshotIgnoresRequestLogOnlyFixtures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_dashboard_request_log_only"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1601, 200, phase7Now.Add(-30*time.Minute))
	if _, err := conn.Exec(ctx, `UPDATE request_logs SET is_stream = TRUE, stream_outcome = 'completed' WHERE id = 1601 AND profile_id = $1`, profileID); err != nil {
		t.Fatalf("mark request-log-only fixture as streaming: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open request-log-only dashboard stats pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)
	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard?window=24h", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard request-log-only status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode request-log-only dashboard response: %v", err)
	}
	watermark := payload["source_watermark"].(map[string]any)
	if watermark["latest_usage_event_created_at"] != nil || watermark["latest_usage_event_id"] != nil {
		t.Fatalf("request-log-only fixture must not populate source watermark, got %+v", watermark)
	}
	metricSnapshot := payload["metric_snapshot"].(map[string]any)
	if metricSnapshot["stream_share"] != float64(0) || metricSnapshot["total_requests"] != float64(0) {
		t.Fatalf("request-log-only fixture must not populate dashboard metrics, got %+v", metricSnapshot)
	}
}

func TestStatsSummaryFromUsageEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_summary_usage_events")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	fromTime := phase7Now.Add(-24 * time.Hour)
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2101, ProfileID: profileID, IngressRequestID: "summary-usage-1", ModelID: "summary-model-a", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: true, PricedFlag: true, InputTokens: 7, OutputTokens: 11, TotalTokens: 18, TotalCostMicros: int64Ptr(1200), ResponseTimeMS: intPtr(100), CreatedAt: phase7Now.Add(-30 * time.Minute)})
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2102, ProfileID: profileID, IngressRequestID: "summary-usage-2", ModelID: "summary-model-b", APIFamily: "anthropic", StatusCode: 503, SuccessFlag: false, BillableFlag: false, PricedFlag: false, InputTokens: 3, OutputTokens: 5, TotalTokens: 8, ResponseTimeMS: intPtr(80), CreatedAt: phase7Now.Add(-20 * time.Minute)})

	summary, err := stats.GetDashboardStatsSummary(ctx, conn, stats.StatsSummaryParams{ProfileID: profileID, FromTime: &fromTime, ToTime: &phase7Now})
	if err != nil {
		t.Fatalf("load dashboard usage-event summary: %v", err)
	}
	if summary.TotalRequests != 2 || summary.SuccessCount != 1 || summary.ErrorCount != 1 || summary.TotalInputTokens != 10 || summary.TotalOutputTokens != 16 || summary.TotalTokens != 26 || summary.AvgResponseTimeMS != 90 || summary.P95ResponseTimeMS != 99 {
		t.Fatalf("expected dashboard summary from usage events, got %+v", summary)
	}
	requestLogSummary, err := stats.GetStatsSummary(ctx, conn, stats.StatsSummaryParams{ProfileID: profileID, FromTime: &fromTime, ToTime: &phase7Now})
	if err != nil {
		t.Fatalf("load generic request-log summary: %v", err)
	}
	if requestLogSummary.TotalRequests != 0 {
		t.Fatalf("generic request-log summary semantics changed, got %+v", requestLogSummary)
	}
}

func TestThroughputFromUsageEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_throughput_usage_events")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	fromTime := phase7Now.Add(-2 * time.Minute)
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2201, ProfileID: profileID, IngressRequestID: "throughput-usage-1", ModelID: "throughput-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: true, PricedFlag: true, TotalTokens: 1, TotalCostMicros: int64Ptr(100), CreatedAt: fromTime})
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2202, ProfileID: profileID, IngressRequestID: "throughput-usage-2", ModelID: "throughput-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: true, PricedFlag: true, TotalTokens: 1, TotalCostMicros: int64Ptr(100), CreatedAt: fromTime.Add(30 * time.Second)})
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2203, ProfileID: profileID, IngressRequestID: "throughput-usage-3", ModelID: "throughput-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: true, PricedFlag: true, TotalTokens: 1, TotalCostMicros: int64Ptr(100), CreatedAt: phase7Now.Add(-30 * time.Second)})

	throughput, err := stats.GetDashboardThroughput(ctx, conn, stats.ThroughputParams{ProfileID: profileID, FromTime: &fromTime, ToTime: &phase7Now})
	if err != nil {
		t.Fatalf("load dashboard usage-event throughput: %v", err)
	}
	if throughput.TotalRequests != 3 || throughput.AverageRPM != 1.5 || throughput.PeakRPM != 2 || throughput.CurrentRPM != 1 || len(throughput.Buckets) != 2 {
		t.Fatalf("expected dashboard throughput from usage events, got %+v", throughput)
	}
	requestLogThroughput, err := stats.GetThroughput(ctx, conn, stats.ThroughputParams{ProfileID: profileID, FromTime: &fromTime, ToTime: &phase7Now})
	if err != nil {
		t.Fatalf("load generic request-log throughput: %v", err)
	}
	if requestLogThroughput.TotalRequests != 0 {
		t.Fatalf("generic request-log throughput semantics changed, got %+v", requestLogThroughput)
	}
}

func TestDashboardRoutingHealthFromUsageEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_dashboard_routing_usage_events")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	route := phase7InsertDashboardRoutingTarget(t, ctx, conn, profileID, "routing-health")
	fromTime := phase7Now.Add(-24 * time.Hour)

	phase7InsertDashboardRequestLog(t, ctx, conn, profileID, 2401, route, http.StatusOK, phase7Now.Add(-10*time.Minute))
	phase7InsertDashboardUsageEvent(t, ctx, conn, profileID, 2402, route, http.StatusOK, true, phase7Now.Add(-8*time.Minute))
	phase7InsertDashboardUsageEvent(t, ctx, conn, profileID, 2403, route, http.StatusServiceUnavailable, false, phase7Now.Add(-7*time.Minute))

	aggregate, err := stats.BuildDashboardAggregateSnapshot(ctx, conn, profileID, phase7Now)
	if err != nil {
		t.Fatalf("build dashboard aggregate snapshot: %v", err)
	}
	link := phase7DashboardRoutingLinkForEndpoint(t, aggregate.RoutingHealthMap, route.EndpointID)
	if link.RequestCount24H != 2 || link.SuccessCount24H != 1 || link.ErrorCount24H != 1 || link.SuccessRate24H == nil || *link.SuccessRate24H != 50 || link.TrafficRequestCount24H != 1 {
		t.Fatalf("expected dashboard routing health from usage events only, got %+v", link)
	}

	genericRates, err := stats.GetConnectionSuccessRates(ctx, conn, stats.ConnectionSuccessRateParams{ProfileID: profileID, FromTime: &fromTime, ToTime: &phase7Now})
	if err != nil {
		t.Fatalf("load generic connection success rates: %v", err)
	}
	if len(genericRates) != 1 || genericRates[0].ConnectionID != route.ConnectionID || genericRates[0].TotalRequests != 1 || genericRates[0].SuccessCount != 1 || genericRates[0].ErrorCount != 0 {
		t.Fatalf("expected generic connection success rates to remain request-log-backed, got %+v", genericRates)
	}
}

func TestDashboardTopologyUsageEventHealth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_dashboard_topology_usage_events")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	route := phase7InsertDashboardRoutingTarget(t, ctx, conn, profileID, "topology-health")

	phase7InsertDashboardRequestLog(t, ctx, conn, profileID, 2501, route, http.StatusInternalServerError, phase7Now.Add(-10*time.Minute))
	phase7InsertDashboardUsageEvent(t, ctx, conn, profileID, 2502, route, http.StatusOK, true, phase7Now.Add(-8*time.Minute))
	phase7InsertDashboardUsageEvent(t, ctx, conn, profileID, 2503, route, http.StatusServiceUnavailable, false, phase7Now.Add(-7*time.Minute))

	aggregate, err := stats.BuildDashboardAggregateSnapshot(ctx, conn, profileID, phase7Now)
	if err != nil {
		t.Fatalf("build dashboard aggregate snapshot: %v", err)
	}
	node := phase7DashboardTopologyConnectionNode(t, aggregate.TopologyGraph, route.ConnectionID)
	if node.RecentRequestCount == nil || *node.RecentRequestCount != 2 || node.RecentSuccessRate == nil || *node.RecentSuccessRate != 50 || node.LastRequestAt == nil {
		t.Fatalf("expected dashboard topology telemetry from usage events only, got %+v", node)
	}
}

func TestUsageEventAggregateMixedOutcomes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_usage_event_mixed_outcomes"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	fromTime := phase7Now.Add(-24 * time.Hour)
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2301, ProfileID: profileID, IngressRequestID: "mixed-priced", ModelID: "mixed-model-a", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: true, PricedFlag: true, InputTokens: 10, OutputTokens: 15, TotalTokens: 25, TotalCostMicros: int64Ptr(2500), ResponseTimeMS: intPtr(100), CreatedAt: phase7Now.Add(-3 * time.Minute)})
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2302, ProfileID: profileID, IngressRequestID: "mixed-unpriced", ModelID: "mixed-model-a", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: true, PricedFlag: false, UnpricedReason: stringPtr("PRICING_DISABLED"), InputTokens: 8, OutputTokens: 12, TotalTokens: 20, ResponseTimeMS: intPtr(200), CreatedAt: phase7Now.Add(-2 * time.Minute)})
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2303, ProfileID: profileID, IngressRequestID: "mixed-error", ModelID: "mixed-model-b", APIFamily: "anthropic", StatusCode: 500, SuccessFlag: false, BillableFlag: false, PricedFlag: false, InputTokens: 12, OutputTokens: 18, TotalTokens: 30, ResponseTimeMS: intPtr(300), CreatedAt: phase7Now.Add(-1 * time.Minute)})
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close mixed setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open mixed stats pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create mixed stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)
	summary := phase7StatsRoutePayload(t, router, fmt.Sprintf("/stats/summary?from_time=%s", fromTime.Format(time.RFC3339)), profileID)
	if int(summary["total_requests"].(float64)) != 3 || int(summary["success_count"].(float64)) != 2 || int(summary["error_count"].(float64)) != 1 || int(summary["total_tokens"].(float64)) != 75 || int(summary["total_input_tokens"].(float64)) != 30 || int(summary["total_output_tokens"].(float64)) != 45 {
		t.Fatalf("expected dashboard summary fast path from mixed usage events, got %+v", summary)
	}
	throughput := phase7StatsRoutePayload(t, router, fmt.Sprintf("/stats/throughput?from_time=%s&to_time=%s", fromTime.Format(time.RFC3339), phase7Now.Format(time.RFC3339)), profileID)
	if int(throughput["total_requests"].(float64)) != 3 || throughput["average_rpm"].(float64) != 0.002 {
		t.Fatalf("expected dashboard throughput fast path from mixed usage events, got %+v", throughput)
	}
	dashboard := phase7StatsRoutePayload(t, router, "/stats/dashboard?window=24h", profileID)
	metricSnapshot := dashboard["metric_snapshot"].(map[string]any)
	if int(metricSnapshot["total_requests"].(float64)) != 3 || int(metricSnapshot["priced_request_count"].(float64)) != 1 || int(metricSnapshot["unpriced_request_count"].(float64)) != 1 || int(metricSnapshot["total_cost"].(float64)) != 2500 {
		t.Fatalf("expected mixed usage-event dashboard metric snapshot, got %+v", metricSnapshot)
	}
}

func TestManagementAuditStatsTopologyGraphDistinguishesTerminalRouteAndEndpointBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_dashboard_topology_graph"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	now := phase7Now.UTC()

	var entryModelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'phase7-entry', 'Phase 7 Entry', NULL, 'dual_native', TRUE, $2, $2) RETURNING id`, profileID, now).Scan(&entryModelID); err != nil {
		t.Fatalf("insert entry model: %v", err)
	}
	var terminalModelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'phase7-terminal', 'Phase 7 Terminal', NULL, 'dual_native', TRUE, $2, $2) RETURNING id`, profileID, now).Scan(&terminalModelID); err != nil {
		t.Fatalf("insert terminal model: %v", err)
	}
	var disabledModelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'phase7-disabled', 'Phase 7 Disabled', NULL, 'dual_native', FALSE, $2, $2) RETURNING id`, profileID, now).Scan(&disabledModelID); err != nil {
		t.Fatalf("insert disabled model: %v", err)
	}
	var endpointID int
	if err := conn.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, 'Phase 7 Topology Endpoint', 'https://phase7-topology.invalid', 'phase7-key', 0, $2, $2) RETURNING id`, profileID, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert topology endpoint: %v", err)
	}
	var terminalTargetID int
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, NULL, 'chat_completions_only', FALSE, 0, 'Phase 7 Terminal Target', NULL, NULL, 'unhealthy', 'probe failure', $3, $4, $4) RETURNING id`, profileID, endpointID, now.Add(-5*time.Minute), now).Scan(&terminalTargetID); err != nil {
		t.Fatalf("insert topology terminal target: %v", err)
	}
	var modelToModelEdgeID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, 0, TRUE, $4, $4) RETURNING id`, profileID, entryModelID, terminalModelID, now).Scan(&modelToModelEdgeID); err != nil {
		t.Fatalf("insert model-to-model access target: %v", err)
	}
	var modelToTerminalEdgeID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4) RETURNING id`, profileID, terminalModelID, terminalTargetID, now).Scan(&modelToTerminalEdgeID); err != nil {
		t.Fatalf("insert model-to-terminal access target: %v", err)
	}
	firstUsageAt := now.Add(-20 * time.Minute)
	secondUsageAt := now.Add(-5 * time.Minute)
	phase7EnsureLogPartition(t, ctx, conn, "usage_request_events", firstUsageAt)
	phase7EnsureLogPartition(t, ctx, conn, "usage_request_events", secondUsageAt)
	if _, err := conn.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_id, endpoint_label_snapshot, connection_id, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, 'phase7-terminal', 'openai', $4, 'Phase 7 Topology Endpoint', $5, 200, TRUE, TRUE, TRUE, 3, 5, 8, 750, 'USD', '$', 1, '/v1/chat/completions', $6, 100)`, 2001, profileID, "phase7-topology-1", endpointID, terminalTargetID, firstUsageAt); err != nil {
		t.Fatalf("insert first topology usage event: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_id, endpoint_label_snapshot, connection_id, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, 'phase7-terminal', 'openai', $4, 'Phase 7 Topology Endpoint', $5, 503, FALSE, FALSE, FALSE, 2, 1, 3, 0, 'USD', '$', 1, '/v1/chat/completions', $6, 120)`, 2002, profileID, "phase7-topology-2", endpointID, terminalTargetID, secondUsageAt); err != nil {
		t.Fatalf("insert second topology usage event: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close topology setup conn: %v", err)
	}

	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open topology stats pool: %v", err)
	}
	defer pool.Close()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create topology stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)
	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard topology status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode topology dashboard response: %v", err)
	}
	topologyGraph, ok := payload["topology_graph"].(map[string]any)
	if !ok {
		t.Fatalf("expected topology_graph payload, got %+v", payload)
	}
	topologyStats := topologyGraph["stats"].(map[string]any)
	if topologyStats["model_count"] != float64(3) || topologyStats["disabled_model_count"] != float64(1) || topologyStats["terminal_target_count"] != float64(1) || topologyStats["inactive_terminal_target_count"] != float64(1) || topologyStats["endpoint_count"] != float64(1) || topologyStats["edge_count"] != float64(3) {
		t.Fatalf("expected topology stats counts, got %+v", topologyStats)
	}
	var terminalNode map[string]any
	var disabledNode map[string]any
	for _, raw := range topologyGraph["nodes"].([]any) {
		node := raw.(map[string]any)
		if node["id"] == fmt.Sprintf("terminal-target-%d", terminalTargetID) {
			terminalNode = node
		}
		if node["id"] == fmt.Sprintf("model-%d", disabledModelID) {
			disabledNode = node
		}
	}
	if disabledNode == nil || disabledNode["status"] != "disabled" {
		t.Fatalf("expected disabled model node in topology graph, got %+v", topologyGraph["nodes"])
	}
	if terminalNode == nil || terminalNode["kind"] != "connection" || terminalNode["product_kind"] != "terminal_target" || terminalNode["active"] != false || terminalNode["health_status"] != "unhealthy" || terminalNode["recent_request_count"] != float64(2) || terminalNode["recent_success_rate"] != float64(50) || terminalNode["last_request_at"] == nil {
		t.Fatalf("expected backend-derived inactive terminal-target telemetry, got %+v", terminalNode)
	}
	var modelToModelEdge map[string]any
	var modelToTerminalEdge map[string]any
	var bindingEdge map[string]any
	for _, raw := range topologyGraph["edges"].([]any) {
		edge := raw.(map[string]any)
		switch edge["id"] {
		case fmt.Sprintf("access-target-%d", modelToModelEdgeID):
			modelToModelEdge = edge
		case fmt.Sprintf("access-target-%d", modelToTerminalEdgeID):
			modelToTerminalEdge = edge
		case fmt.Sprintf("terminal-target-binding-%d", terminalTargetID):
			bindingEdge = edge
		}
	}
	if modelToModelEdge == nil || modelToModelEdge["kind"] != "model_to_model" || modelToModelEdge["source_node_id"] != fmt.Sprintf("model-%d", entryModelID) || modelToModelEdge["target_node_id"] != fmt.Sprintf("model-%d", terminalModelID) {
		t.Fatalf("expected distinct model-to-model topology edge, got %+v", modelToModelEdge)
	}
	if modelToTerminalEdge == nil || modelToTerminalEdge["kind"] != "model_to_connection" || modelToTerminalEdge["product_kind"] != "model_to_terminal_target" || modelToTerminalEdge["source_node_id"] != fmt.Sprintf("model-%d", terminalModelID) || modelToTerminalEdge["target_node_id"] != fmt.Sprintf("terminal-target-%d", terminalTargetID) {
		t.Fatalf("expected distinct model-to-terminal-target topology edge, got %+v", modelToTerminalEdge)
	}
	if bindingEdge == nil || bindingEdge["kind"] != "connection_to_endpoint" || bindingEdge["product_kind"] != "terminal_target_to_endpoint" || bindingEdge["source_node_id"] != fmt.Sprintf("terminal-target-%d", terminalTargetID) || bindingEdge["target_node_id"] != fmt.Sprintf("endpoint-%d", endpointID) {
		t.Fatalf("expected terminal-target endpoint binding edge, got %+v", bindingEdge)
	}
}

func TestManagementAuditStatsPhase7StartupOTLPKeepsRetainedStatsAPIs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(collector.Close)
	settings := phase7StartupOTLPSettings(t, collector.URL)
	phase7InstallStartupOTLPProviders(t, settings)

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_phase7_startup_otlp_retained_apis"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	createdAt := phase7Now.Add(-30 * time.Minute)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1401, http.StatusOK, createdAt)
	phase7InsertUsageEvent(t, ctx, conn, profileID, 1402, createdAt)
	phase7InsertAuditLog(t, ctx, conn, profileID, 1403, createdAt)
	phase7InsertLoadbalanceEvent(t, ctx, conn, profileID, 1404, createdAt)
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close retained stats setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open retained stats pool: %v", err)
	}
	defer pool.Close()
	for tableName, want := range map[string]int{"request_logs": 1, "usage_request_events": 1, "audit_logs": 1, "loadbalance_events": 1} {
		if got := phase7CountRows(t, ctx, pool, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE profile_id = $1`, tableName), profileID); got != want {
			t.Fatalf("expected %s to keep %d retained rows under startup OTLP, got %d", tableName, want, got)
		}
	}

	service, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	if err != nil {
		t.Fatalf("create retained stats service with startup OTLP settings: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)
	requestHistory := phase7StatsRoutePayload(t, router, "/stats/requests?limit=50&offset=0", profileID)
	items, ok := requestHistory["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected retained request-history API to return the DB row under startup OTLP, got %+v", requestHistory)
	}
	spending := phase7StatsRoutePayload(t, router, "/stats/spending?preset=1h&group_by=none&limit=50&offset=0", profileID)
	spendingSummary := spending["summary"].(map[string]any)
	if spendingSummary["successful_request_count"] != float64(1) || spendingSummary["total_cost_micros"] != float64(1250) {
		t.Fatalf("expected retained spending API to use usage_request_events, got %+v", spendingSummary)
	}
	usageSnapshot := phase7StatsRoutePayload(t, router, "/stats/usage-snapshot?preset=1h", profileID)
	usageOverview := usageSnapshot["overview"].(map[string]any)
	if usageOverview["total_requests"] != float64(1) || usageOverview["total_tokens"] != float64(18) {
		t.Fatalf("expected retained usage snapshot API to use durable usage rows, got %+v", usageOverview)
	}
	dashboard := phase7StatsRoutePayload(t, router, "/stats/dashboard?window=24h", profileID)
	metricSnapshot := dashboard["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(1) || metricSnapshot["success_rate"] != float64(100) {
		t.Fatalf("expected retained dashboard aggregate API to use usage events, got %+v", metricSnapshot)
	}
}

func TestManagementDashboardStatsKeepsCachedAggregateAtStaleThreshold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_dashboard_stale_threshold"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open threshold dashboard stats pool: %v", err)
	}
	defer pool.Close()
	dashboardSnapshots := stats.NewDashboardAggregateStore()
	dashboardSnapshots.StoreProfile(stats.DashboardAggregateSnapshot{ProfileID: profileID, GeneratedAt: phase7Now.Add(-stats.DashboardStatsStaleAfter), StatsSummary24H: stats.StatsSummaryResponse{TotalRequests: 7}})
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }, DashboardSnapshots: dashboardSnapshots})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)

	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard threshold status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode threshold dashboard aggregate response: %v", err)
	}
	metricSnapshot := payload["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(7) {
		t.Fatalf("expected threshold cached dashboard aggregate to remain reusable, got %+v", metricSnapshot)
	}
	health := payload["health"].(map[string]any)
	if health["stale"] != false || health["lag_seconds"] != float64(stats.DashboardStatsStaleAfter/time.Second) {
		t.Fatalf("expected threshold cached dashboard aggregate to report fresh boundary health, got %+v", health)
	}
}

func TestManagementDashboardStatsRebuildsStaleCachedAggregate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "stats_dashboard_stale_aggregate"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1004, 200, phase7Now.Add(-30*time.Second))
	phase7InsertUsageEvent(t, ctx, conn, profileID, 1005, phase7Now.Add(-30*time.Second))
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open stale dashboard stats pool: %v", err)
	}
	defer pool.Close()
	dashboardSnapshots := stats.NewDashboardAggregateStore()
	dashboardSnapshots.StoreProfile(stats.DashboardAggregateSnapshot{ProfileID: profileID, GeneratedAt: phase7Now.Add(-stats.DashboardStatsStaleAfter - time.Second)})
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return phase7Now }, DashboardSnapshots: dashboardSnapshots})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()
	router := chiRouterForStats(service)

	request := httptest.NewRequest(http.MethodGet, "/stats/dashboard", nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /stats/dashboard status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode rebuilt dashboard aggregate response: %v", err)
	}
	metricSnapshot := payload["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(1) {
		t.Fatalf("expected stale cached dashboard aggregate to rebuild from seeded activity, got %+v", metricSnapshot)
	}
	health := payload["health"].(map[string]any)
	if health["stale"] != false || health["lag_seconds"] != float64(0) {
		t.Fatalf("expected rebuilt dashboard aggregate to report fresh health, got %+v", health)
	}
}

func TestDashboardSnapshotInvalidationEvictsCachedProfiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	databaseName := "dashboard_snapshot_invalidation"
	conn := harness.openDatabase(t, ctx, databaseName)
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	dashboardSnapshots := stats.NewDashboardAggregateStore()
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, DashboardSnapshots: dashboardSnapshots})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	defer service.Close()

	dashboardSnapshots.StoreProfile(stats.DashboardAggregateSnapshot{ProfileID: 101, GeneratedAt: phase7Now})
	dashboardSnapshots.StoreProfile(stats.DashboardAggregateSnapshot{ProfileID: 202, GeneratedAt: phase7Now})
	service.InvalidateDashboardSnapshot(101)
	if _, ok := dashboardSnapshots.LoadProfile(101); ok {
		t.Fatal("expected profile-specific dashboard snapshot invalidation to evict profile 101")
	}
	if _, ok := dashboardSnapshots.LoadProfile(202); !ok {
		t.Fatal("expected profile-specific dashboard snapshot invalidation to preserve profile 202")
	}
	service.InvalidateAllDashboardSnapshots()
	if _, ok := dashboardSnapshots.LoadProfile(202); ok {
		t.Fatal("expected global dashboard snapshot invalidation to evict remaining profiles")
	}
}

func TestDashboardRollupUsageWatermark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_high_water")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1101, 200, phase7Now.Add(-time.Hour))
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2401, ProfileID: profileID, IngressRequestID: "rollup-usage-success", ModelID: "phase7-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: true, PricedFlag: true, TotalTokens: 18, TotalCostMicros: int64Ptr(1250), CreatedAt: phase7Now.Add(-30 * time.Second)})
	phase7InsertUsageEventAggregate(t, ctx, conn, phase7UsageEventAggregateSeed{ID: 2402, ProfileID: profileID, IngressRequestID: "rollup-usage-error", ModelID: "phase7-model", APIFamily: "openai", StatusCode: 500, SuccessFlag: false, BillableFlag: false, PricedFlag: false, TotalTokens: 0, CreatedAt: phase7Now.Add(-10 * time.Second)})
	phase7InsertAuditLog(t, ctx, conn, profileID, 1201, phase7Now.Add(-20*time.Minute))

	if err := stats.RefreshDashboardStatsRollup(ctx, conn, profileID, "24h", phase7Now); err != nil {
		t.Fatalf("refresh dashboard stats rollup: %v", err)
	}
	response, err := stats.LoadDashboardRollupStats(ctx, conn, profileID, "24h", phase7Now)
	if err != nil {
		t.Fatalf("load refreshed dashboard stats: %v", err)
	}
	if response.Metrics.RequestCount != 2 || response.Metrics.ErrorCount != 1 || response.Metrics.AuditEventCount != 1 || response.Health.Stale {
		t.Fatalf("expected refreshed dashboard metrics with usage-event high-water mark, got %+v", response)
	}
	var persistedHighWater time.Time
	if err := conn.QueryRow(ctx, `SELECT last_source_high_water_mark FROM management_stat_refresh_state WHERE job_name = 'dashboard_stats'`).Scan(&persistedHighWater); err != nil {
		t.Fatalf("load dashboard stats refresh watermark: %v", err)
	}
	if !persistedHighWater.Equal(phase7Now.Add(-10 * time.Second)) {
		t.Fatalf("expected usage-event high-water mark, got %s", persistedHighWater.Format(time.RFC3339Nano))
	}
}

func TestDashboardStatsRollupRefreshPartitionPruning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_partition_pruning")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7EnsureLogPartition(t, ctx, conn, "usage_request_events", phase7Now.AddDate(0, 0, -2))
	phase7EnsureLogPartition(t, ctx, conn, "usage_request_events", phase7Now)
	phase7EnsureLogPartition(t, ctx, conn, "audit_logs", phase7Now.AddDate(0, 0, -2))
	phase7EnsureLogPartition(t, ctx, conn, "audit_logs", phase7Now)
	phase7InsertUsageEvent(t, ctx, conn, profileID, 1251, phase7Now.Add(-time.Hour))
	phase7InsertAuditLog(t, ctx, conn, profileID, 1252, phase7Now.Add(-30*time.Minute))

	requestPlan := phase7ExplainPlan(t, ctx, conn, `SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3`, profileID, phase7Now.Add(-24*time.Hour), phase7Now.Add(time.Hour))
	if !strings.Contains(requestPlan, "usage_request_events_p20260430") || strings.Contains(requestPlan, "usage_request_events_p20260428") {
		t.Fatalf("expected usage_request_events created_at filter to prune old partitions, got %s", requestPlan)
	}
	auditPlan := phase7ExplainPlan(t, ctx, conn, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3 AND audit_enabled_at_request = TRUE`, profileID, phase7Now.Add(-24*time.Hour), phase7Now.Add(time.Hour))
	if !strings.Contains(auditPlan, "audit_logs_p20260430") || strings.Contains(auditPlan, "audit_logs_p20260428") {
		t.Fatalf("expected audit_logs created_at filter to prune old partitions, got %s", auditPlan)
	}
	if err := stats.RefreshDashboardStatsRollup(ctx, conn, profileID, "24h", phase7Now); err != nil {
		t.Fatalf("refresh dashboard stats rollup with partitioned roots: %v", err)
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
	response, err := stats.LoadDashboardRollupStats(ctx, conn, profileID, "24h", phase7Now)
	if err != nil {
		t.Fatalf("load stale dashboard stats: %v", err)
	}
	if response.Metrics.RequestCount != 7 || !response.Health.Stale {
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
	if job.State != "succeeded" || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 0 {
		t.Fatalf("expected audit retention job to drop expired audit rows, got job=%+v", job)
	}
}

func TestManagementAuditDeleteJobSuccessfulChunksDoNotConsumeRetryAttempts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_many_chunks")
	defer pool.Close()
	for index := range 4501 {
		phase7InsertAuditLog(t, ctx, pool, profileID, index+1, phase7Now.Add(-48*time.Hour))
	}
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "many-chunks", false)
	for chunks := range 12 {
		if err := store.ProcessDue(ctx); err != nil {
			t.Fatalf("process successful audit delete chunk %d: %v", chunks, err)
		}
		job, _ = store.GetJob(ctx, job.ID, profileID)
		if job.State == "succeeded" {
			break
		}
	}
	if job.State != "succeeded" || job.AttemptCount != 0 || job.Progress.BatchesCompleted != 1 {
		t.Fatalf("expected partition-backed audit retention to finish without consuming retry attempts, got job=%+v", job)
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
	phase7InsertAuditLog(t, ctx, pool, profileID, 1, phase7Now.Add(-48*time.Hour))
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "failure-attempts", false)
	partitionName := fmt.Sprintf("audit_logs_p%s", phase7Now.Add(-48*time.Hour).UTC().Format("20060102"))
	if _, err := pool.Exec(ctx, `ALTER TABLE `+quoteIdentifier(partitionName)+` RENAME TO `+quoteIdentifier(partitionName+"_missing")); err != nil {
		t.Fatalf("rename audit partition to force transient delete failures: %v", err)
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
	if job.State != "failed" || job.AttemptCount != 8 || job.ErrorCode == nil || *job.ErrorCode != "retention_error" {
		t.Fatalf("expected max retention failures to mark job failed visibly, got job=%+v", job)
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
	if job.State != "succeeded" || job.Progress.BatchesCompleted != 1 || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 0 {
		t.Fatalf("expected stale running lease to resume and finish retention, got job=%+v", job)
	}
}

func TestManagementAuditDeleteJobResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, pool, profileID := phase7JobStore(t, ctx, "audit_delete_resume")
	defer pool.Close()
	for index := range 3 {
		phase7InsertAuditLog(t, ctx, pool, profileID, index+1, phase7Now.Add(-48*time.Hour))
	}
	job := phase7CreateDeleteJob(t, ctx, store, profileID, "resume", false)
	for attempts := range 3 {
		if err := store.ProcessDue(ctx); err != nil {
			t.Fatalf("process audit delete resume attempt %d: %v", attempts, err)
		}
		job, _ = store.GetJob(ctx, job.ID, profileID)
		if job.State == "succeeded" {
			break
		}
	}
	if job.State != "succeeded" || job.Progress.BatchesCompleted != 1 || phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 0 {
		t.Fatalf("expected resumed retention job to finish, got job=%+v", job)
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
	for attempts := range 3 {
		if err := store.ProcessDue(ctx); err != nil {
			t.Fatalf("process audit delete all attempt %d: %v", attempts, err)
		}
	}
	if phase7CountRows(t, ctx, pool, `SELECT COUNT(*) FROM management_job_events WHERE job_id = $1`, job.ID) == 0 {
		t.Fatalf("expected management job events to survive audit_logs deletion for job %s", job.ID)
	}
}

func phase7StartupOTLPSettings(t *testing.T, endpoint string) config.Settings {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "bootstrap.json")
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	if _, err := manager.LoadOrSeed(configPath); err != nil {
		t.Fatalf("seed phase7 startup OTLP bootstrap config: %v", err)
	}
	mutateStartupBootstrapJSON(t, configPath, func(payload map[string]any) {
		payload["telemetry"] = startupTelemetryBootstrapPayload(endpoint)
	})
	settings, err := manager.Load(configPath)
	if err != nil {
		t.Fatalf("load phase7 startup OTLP bootstrap config: %v", err)
	}
	return settings
}

func phase7InstallStartupOTLPProviders(t *testing.T, settings config.Settings) {
	t.Helper()
	providers, err := platformtelemetry.BuildProviders(context.Background(), settings.Telemetry)
	if err != nil {
		t.Fatalf("build phase7 startup OTLP providers: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := providers.Shutdown(ctx); err != nil {
			t.Fatalf("shutdown phase7 startup OTLP providers: %v", err)
		}
	})
}

func phase7StatsRoutePayload(t *testing.T, router http.Handler, path string, profileID int) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s response: %v", path, err)
	}
	return payload
}

func chiRouterForSettings(service *managementsettings.Service) http.Handler {
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func chiRouterForAudit(service *managementaudit.Service) http.Handler {
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func chiRouterForStats(service *managementstats.Service) http.Handler {
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func phase7PartitionExists(partitions []logretention.Partition, partitionName string) bool {
	for _, partition := range partitions {
		if partition.PartitionName == partitionName {
			return true
		}
	}
	return false
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
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool, Now: func() time.Time { return phase7Now }})
	return managementjobs.NewStore(managementjobs.Options{Pool: pool, LogRetention: retentionStore, Now: func() time.Time { return phase7Now }}), pool, profileID
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

func phase7CreateLogRetentionJob(t *testing.T, ctx context.Context, store *managementjobs.Store, key string) managementjobs.Job {
	t.Helper()
	cutoff := phase7Now.Add(-24 * time.Hour)
	job, err := store.CreateLogRetentionJob(ctx, managementjobs.CreateLogRetentionJobRequest{RequestedBy: "global", IdempotencyKey: key, Reason: "global retention cleanup", Scope: managementjobs.LogRetentionScope{Table: "request_logs", Cutoff: &cutoff}})
	if err != nil {
		t.Fatalf("create log retention job: %v", err)
	}
	return job
}

func phase7EnsureLogPartition(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, tableName string, timestamp time.Time) {
	t.Helper()
	day := time.Date(timestamp.UTC().Year(), timestamp.UTC().Month(), timestamp.UTC().Day(), 0, 0, 0, 0, time.UTC)
	partitionName := fmt.Sprintf("%s_p%s", tableName, day.Format("20060102"))
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS public.%s PARTITION OF public.%s FOR VALUES FROM ('%s') TO ('%s')`, quoteIdentifier(partitionName), quoteIdentifier(tableName), day.Format("2006-01-02 15:04:05-07:00"), day.AddDate(0, 0, 1).Format("2006-01-02 15:04:05-07:00"))
	if _, err := exec.Exec(ctx, query); err != nil {
		t.Fatalf("ensure %s partition for %s: %v", tableName, day.Format("2006-01-02"), err)
	}
}

func phase7ExplainPlan(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, query string, args ...any) string {
	t.Helper()
	var plan string
	if err := exec.QueryRow(ctx, `EXPLAIN (FORMAT JSON) `+query, args...).Scan(&plan); err != nil {
		t.Fatalf("explain query %q: %v", query, err)
	}
	return plan
}

type phase7DashboardRoutingTarget struct {
	ModelConfigID int
	ModelID       string
	EndpointID    int
	EndpointName  string
	ConnectionID  int
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
	phase7EnsureLogPartition(t, ctx, exec, "request_logs", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO request_logs (id, profile_id, model_id, api_family, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'phase7-model', 'openai', $3, 100, FALSE, $4, TRUE, TRUE, '/v1/chat/completions', $5, FALSE, FALSE)`, id, profileID, statusCode, statusCode >= 200 && statusCode < 300, createdAt.UTC()); err != nil {
		t.Fatalf("insert phase7 request log %d: %v", id, err)
	}
}

func phase7InsertDashboardRoutingTarget(t *testing.T, ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, suffix string) phase7DashboardRoutingTarget {
	t.Helper()
	now := phase7Now.UTC()
	modelID := "phase7-" + suffix
	var modelConfigID int
	if err := exec.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $3, NULL, 'dual_native', TRUE, $4, $4) RETURNING id`, profileID, modelID, "Phase 7 "+suffix, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert dashboard routing model: %v", err)
	}
	endpointName := "Phase 7 " + suffix + " Endpoint"
	var endpointID int
	if err := exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, 'phase7-key', 0, $4, $4) RETURNING id`, profileID, endpointName, "https://"+suffix+".invalid", now).Scan(&endpointID); err != nil {
		t.Fatalf("insert dashboard routing endpoint: %v", err)
	}
	var connectionID int
	if err := exec.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, $3, NULL, NULL, 'healthy', NULL, NULL, $4, $4) RETURNING id`, profileID, endpointID, "Phase 7 "+suffix+" Connection", now).Scan(&connectionID); err != nil {
		t.Fatalf("insert dashboard routing connection: %v", err)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, modelConfigID, connectionID, now); err != nil {
		t.Fatalf("attach dashboard routing connection: %v", err)
	}
	return phase7DashboardRoutingTarget{ModelConfigID: modelConfigID, ModelID: modelID, EndpointID: endpointID, EndpointName: endpointName, ConnectionID: connectionID}
}

func phase7InsertDashboardRequestLog(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, route phase7DashboardRoutingTarget, statusCode int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "request_logs", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, endpoint_description, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, 'openai', $4, $5, $6, 1, $7, $8, 100, FALSE, $9, TRUE, TRUE, '/v1/chat/completions', $10, $11, FALSE, FALSE)`, id, profileID, route.ModelID, route.EndpointID, route.ConnectionID, fmt.Sprintf("phase7-dashboard-request-%d", id), "https://dashboard-routing.invalid", statusCode, statusCode >= 200 && statusCode < 300, route.EndpointName, createdAt.UTC()); err != nil {
		t.Fatalf("insert dashboard request log %d: %v", id, err)
	}
}

func phase7InsertRecentActivityRequestLog(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, route phase7DashboardRoutingTarget, statusCode int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "request_logs", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, endpoint_base_url, status_code, response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, $3, 'openai', $4, $5, $6, $7, 123, 45, 678, TRUE, 'completed', 5, 7, 12, $8, TRUE, TRUE, 3456, 'USD', '$', '/v1/chat/completions', $9, FALSE, FALSE)`, id, profileID, route.ModelID, route.EndpointID, route.ConnectionID, "https://recent-activity.invalid", statusCode, statusCode >= 200 && statusCode < 300, createdAt.UTC()); err != nil {
		t.Fatalf("insert recent activity request log %d: %v", id, err)
	}
}

func phase7InsertDashboardUsageEvent(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, route phase7DashboardRoutingTarget, statusCode int, successFlag bool, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "usage_request_events", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_id, endpoint_label_snapshot, connection_id, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, $4, 'openai', $5, $6, $7, $8, $9, $10, $10, 3, 5, 8, $11, 'USD', '$', 1, '/v1/chat/completions', $12, 100)`, id, profileID, fmt.Sprintf("phase7-dashboard-usage-%d", id), route.ModelID, route.EndpointID, route.EndpointName, route.ConnectionID, statusCode, successFlag, successFlag, int64(750), createdAt.UTC()); err != nil {
		t.Fatalf("insert dashboard usage event %d: %v", id, err)
	}
}

func phase7DashboardRoutingLinkForEndpoint(t *testing.T, routing stats.DashboardRoutingHealthMap, endpointID int) stats.DashboardRoutingLink {
	t.Helper()
	for _, link := range routing.Links {
		if link.EndpointID == endpointID {
			return link
		}
	}
	t.Fatalf("expected dashboard routing link for endpoint %d, got %+v", endpointID, routing.Links)
	return stats.DashboardRoutingLink{}
}

func phase7DashboardTopologyConnectionNode(t *testing.T, graph stats.DashboardTopologyGraph, connectionID int) stats.DashboardTopologyNode {
	t.Helper()
	for _, node := range graph.Nodes {
		if node.ConnectionID != nil && *node.ConnectionID == connectionID {
			return node
		}
	}
	t.Fatalf("expected dashboard topology connection node for connection %d, got %+v", connectionID, graph.Nodes)
	return stats.DashboardTopologyNode{}
}

func phase7InsertUsageEvent(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "usage_request_events", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, 'phase7-model', 'openai', 'phase7-model endpoint', 200, TRUE, TRUE, TRUE, 7, 11, 18, 1250, 'USD', '$', 1, '/v1/chat/completions', $4, 100)`, id, profileID, fmt.Sprintf("phase7-ingress-%d", id), createdAt.UTC()); err != nil {
		t.Fatalf("insert phase7 usage event %d: %v", id, err)
	}
}

type phase7UsageEventAggregateSeed struct {
	ID               int
	ProfileID        int
	IngressRequestID string
	ModelID          string
	APIFamily        string
	StatusCode       int
	SuccessFlag      bool
	BillableFlag     bool
	PricedFlag       bool
	UnpricedReason   *string
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
	TotalCostMicros  *int64
	ResponseTimeMS   *int
	CreatedAt        time.Time
}

func phase7InsertUsageEventAggregate(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, seed phase7UsageEventAggregateSeed) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "usage_request_events", seed.CreatedAt)
	var unpricedReason any
	if seed.UnpricedReason != nil {
		unpricedReason = *seed.UnpricedReason
	}
	var totalCost any
	if seed.TotalCostMicros != nil {
		totalCost = *seed.TotalCostMicros
	}
	var responseTime any
	if seed.ResponseTimeMS != nil {
		responseTime = *seed.ResponseTimeMS
	}
	if _, err := exec.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, billable_flag, priced_flag, unpriced_reason, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, 'USD', '$', 1, '/v1/chat/completions', $16, $17)`, seed.ID, seed.ProfileID, seed.IngressRequestID, seed.ModelID, seed.APIFamily, seed.ModelID+" endpoint", seed.StatusCode, seed.SuccessFlag, seed.BillableFlag, seed.PricedFlag, unpricedReason, seed.InputTokens, seed.OutputTokens, seed.TotalTokens, totalCost, seed.CreatedAt.UTC(), responseTime); err != nil {
		t.Fatalf("insert phase7 aggregate usage event %d: %v", seed.ID, err)
	}
}

func int64Ptr(value int64) *int64 {
	resolved := value
	return &resolved
}

func stringPtr(value string) *string {
	resolved := value
	return &resolved
}

func legacyDashboardActivityRowsKey() string {
	return "recent" + "_" + "requests"
}

func phase7InsertAuditLog(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "audit_logs", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO audit_logs (id, profile_id, model_id, request_method, request_url, request_headers, request_body_stored, response_status, response_headers, response_body_stored, is_stream, duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, 'phase7-model', 'POST', 'https://phase7.invalid/v1/chat/completions', '{}', FALSE, 200, '{}', FALSE, FALSE, 100, TRUE, FALSE, $3)`, id, profileID, createdAt.UTC()); err != nil {
		t.Fatalf("insert phase7 audit log %d: %v", id, err)
	}
}

func phase7InsertLoadbalanceEvent(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, connectionID int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "loadbalance_events", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO loadbalance_events (profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, ban_mode, created_at) VALUES ($1, $2, 'retry_scheduled', 'transient_http', 1, 1, $3, 60000, 'off', $4)`, profileID, connectionID, createdAt.UTC().Add(time.Minute), createdAt.UTC()); err != nil {
		t.Fatalf("insert phase7 loadbalance event for connection %d: %v", connectionID, err)
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
	_, remainder, found := strings.Cut(source, start)
	if !found {
		return ""
	}
	remainder = start + remainder
	before, _, found := strings.Cut(remainder, end)
	if !found {
		return remainder
	}
	return before
}

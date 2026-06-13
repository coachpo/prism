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
	for _, key := range []string{"generated_at", "coverage_24h", "coverage_30d", "health", "metric_snapshot", "api_family_rows", "recent_requests", "top_spending_models", "routing_health_map", "topology_graph"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected aggregate dashboard field %q, got %+v", key, payload)
		}
	}
	for _, legacyKey := range []string{"window", "covers", "freshness", "metrics"} {
		if _, ok := payload[legacyKey]; ok {
			t.Fatalf("dashboard route must not expose legacy top-level %q, got %+v", legacyKey, payload)
		}
	}
	metricSnapshot := payload["metric_snapshot"].(map[string]any)
	if metricSnapshot["total_requests"] != float64(1) || metricSnapshot["success_rate"] != float64(100) {
		t.Fatalf("expected aggregate dashboard metrics from seeded activity, got %+v", metricSnapshot)
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
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'phase7-entry', 'Phase 7 Entry', NULL, TRUE, $2, $2) RETURNING id`, profileID, now).Scan(&entryModelID); err != nil {
		t.Fatalf("insert entry model: %v", err)
	}
	var terminalModelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'phase7-terminal', 'Phase 7 Terminal', NULL, TRUE, $2, $2) RETURNING id`, profileID, now).Scan(&terminalModelID); err != nil {
		t.Fatalf("insert terminal model: %v", err)
	}
	var disabledModelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'phase7-disabled', 'Phase 7 Disabled', NULL, FALSE, $2, $2) RETURNING id`, profileID, now).Scan(&disabledModelID); err != nil {
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
	if err := conn.QueryRow(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, 0, 1, 0, TRUE, $4, $4) RETURNING id`, profileID, entryModelID, terminalModelID, now).Scan(&modelToModelEdgeID); err != nil {
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
		t.Fatalf("expected retained dashboard aggregate API to use DB request history, got %+v", metricSnapshot)
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
	response, err := stats.LoadDashboardRollupStats(ctx, conn, profileID, "24h", phase7Now)
	if err != nil {
		t.Fatalf("load refreshed dashboard stats: %v", err)
	}
	if response.Metrics.RequestCount != 2 || response.Metrics.ErrorCount != 1 || response.Metrics.AuditEventCount != 1 || response.Health.Stale {
		t.Fatalf("expected refreshed dashboard metrics with fresh high-water mark, got %+v", response)
	}
}

func TestDashboardStatsRollupRefreshPartitionPruning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := phase7MigratedConn(t, ctx, "stats_partition_pruning")
	defer func() { _ = conn.Close(ctx) }()
	profileID := phase7InsertProfile(t, ctx, conn)
	phase7EnsureLogPartition(t, ctx, conn, "request_logs", phase7Now.AddDate(0, 0, -2))
	phase7EnsureLogPartition(t, ctx, conn, "request_logs", phase7Now)
	phase7EnsureLogPartition(t, ctx, conn, "audit_logs", phase7Now.AddDate(0, 0, -2))
	phase7EnsureLogPartition(t, ctx, conn, "audit_logs", phase7Now)
	phase7InsertRequestLog(t, ctx, conn, profileID, 1251, 200, phase7Now.Add(-time.Hour))
	phase7InsertAuditLog(t, ctx, conn, profileID, 1252, phase7Now.Add(-30*time.Minute))

	requestPlan := phase7ExplainPlan(t, ctx, conn, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3`, profileID, phase7Now.Add(-24*time.Hour), phase7Now.Add(time.Hour))
	if !strings.Contains(requestPlan, "request_logs_p20260430") || strings.Contains(requestPlan, "request_logs_p20260428") {
		t.Fatalf("expected request_logs created_at filter to prune old partitions, got %s", requestPlan)
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
	for attempts := 0; attempts < 3; attempts++ {
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

func phase7InsertUsageEvent(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "usage_request_events", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, billable_flag, priced_flag, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, 'phase7-model', 'openai', 'phase7-model endpoint', 200, TRUE, TRUE, TRUE, 7, 11, 18, 1250, 'USD', '$', 1, '/v1/chat/completions', $4, 100)`, id, profileID, fmt.Sprintf("phase7-ingress-%d", id), createdAt.UTC()); err != nil {
		t.Fatalf("insert phase7 usage event %d: %v", id, err)
	}
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

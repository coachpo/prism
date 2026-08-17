package integrationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/domain/stats"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
)

var phase7Now = time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)

func TestRetentionCoverageAppendHandoff(t *testing.T) {
	ctx := t.Context()
	conn := newPostgresHarness(t).openDatabase(t, ctx, "retention_coverage_append_handoff")
	defer func() { _ = conn.Close(ctx) }()
	profileID := task9InsertProfile(t, ctx, conn)
	must := func(test *testing.T, err error) {
		if err != nil {
			test.Fatal(err)
		}
	}
	refresh := func(test *testing.T, domain string, now time.Time) {
		source, err := stats.LoadRetentionSourceProjection(ctx, conn, domain, now)
		must(test, err)
		must(test, stats.RefreshActualCoverageProjection(ctx, conn, source, now))
	}
	handoff := func(test *testing.T, domain string, at time.Time, marker string, row int, mutation func(pgx.Tx)) {
		tx, err := conn.Begin(ctx)
		must(test, err)
		defer func() { _ = tx.Rollback(ctx) }()
		if mutation != nil {
			mutation(tx)
		} else {
			task9InsertManagedLogRow(test, ctx, tx, domain, profileID, marker, row, at)
		}
		must(test, stats.RecordActualCoverageAppend(ctx, tx, domain, []time.Time{at}, at))
		must(test, tx.Commit(ctx))
	}
	assertState := func(test *testing.T, domain string, at time.Time, fresh, realGap bool, revision string) string {
		must(test, conn.QueryRow(ctx, `SELECT coverage_revision FROM retention_coverage_read_models WHERE dataset = $1 AND CASE WHEN $3 THEN complete AND freshness = 'fresh' AND NOT dirty AND coverage_revision <> '' AND coverage_hash = coverage_revision AND source_revision <> '' AND earliest_retained_at <= $2 AND latest_retained_at >= $2 AND NOT gaps @> '[{"from_time":null,"to_time":null,"reason":"no_retained_intersection"}]'::jsonb AND (NOT $4 OR gaps @> '[{"reason":"known_gap"}]'::jsonb) AND materialization_cut->>'kind' = CASE WHEN dataset = 'request_logs' THEN 'request_visibility_cut' WHEN dataset = 'usage_request_events' THEN 'usage_hybrid_cut' ELSE 'event_hybrid_cut' END AND COALESCE(materialization_cut->>'request_committed_cut', materialization_cut->>'raw_committed_cut') = $5 ELSE dirty AND freshness = 'stale' AND coverage_revision = $6 END`, domain, at, fresh, realGap, at.Format(time.RFC3339Nano), revision).Scan(&revision))
		return revision
	}
	for _, domain := range logretention.ManagedTables() {
		t.Run(domain, func(t *testing.T) {
			ensureDailyLogPartition(t, ctx, conn, domain, phase7Now, "coverage_handoff")
			refresh(t, domain, phase7Now)
			_, err := conn.Exec(ctx, `UPDATE retention_coverage_read_models SET gaps = gaps || '[{"from_time":"2026-04-30T10:00:00Z","to_time":"2026-04-30T11:00:00Z","reason":"known_gap"}]'::jsonb WHERE dataset = $1`, domain)
			must(t, err)
			handoff(t, domain, phase7Now, "coverage-first-"+domain, 0, nil)
			revision := assertState(t, domain, phase7Now, true, true, "")
			_, err = conn.Exec(ctx, `UPDATE retention_coverage_read_models SET freshness = 'stale', dirty = false WHERE dataset = $1`, domain)
			must(t, err)
			later := phase7Now.Add(time.Minute)
			handoff(t, domain, later, "coverage-second-"+domain, 1, nil)
			assertState(t, domain, later, false, false, revision)
			for _, mutation := range []string{`UPDATE %s SET created_at = created_at WHERE created_at = $1`, `DELETE FROM %s WHERE created_at = $1`} {
				refresh(t, domain, later)
				revision = assertState(t, domain, later, true, false, "")
				handoff(t, domain, later, "", 0, func(tx pgx.Tx) {
					_, mutationErr := tx.Exec(ctx, fmt.Sprintf(mutation, quoteIdentifier(domain)), later)
					must(t, mutationErr)
				})
				assertState(t, domain, later, false, false, revision)
			}
			if domain == "audit_logs" {
				refresh(t, domain, later)
				suppressed, allowed := phase7Now.Add(-time.Minute), later.Add(time.Minute)
				_, err = conn.Exec(ctx, `INSERT INTO audit_retention_tombstones (profile_id, ingress_request_id, cutoff, retention_generation, reason, created_at) VALUES ($1, 'coverage-suppressed', $2, 1, 'test', $2)`, profileID, later)
				must(t, err)
				handoff(t, domain, suppressed, "coverage-suppressed", 2, nil)
				must(t, conn.QueryRow(ctx, `SELECT coverage_revision FROM retention_coverage_read_models WHERE dataset = $1 AND NOT dirty AND freshness = 'fresh' AND earliest_retained_at = $2 AND latest_retained_at = $2`, domain, phase7Now).Scan(&revision))
				handoff(t, domain, allowed, "", 0, func(tx pgx.Tx) {
					task9InsertManagedLogRow(t, ctx, tx, domain, profileID, "coverage-suppressed", 3, suppressed)
					task9InsertManagedLogRow(t, ctx, tx, domain, profileID, "coverage-allowed", 4, allowed)
				})
				must(t, conn.QueryRow(ctx, `SELECT coverage_revision FROM retention_coverage_read_models WHERE dataset = $1 AND NOT dirty AND freshness = 'fresh' AND earliest_retained_at = $2 AND latest_retained_at = $3`, domain, phase7Now, allowed).Scan(&revision))
			}
			if domain == "loadbalance_events" {
				refresh(t, domain, later)
				revision = assertState(t, domain, phase7Now, true, false, "")
				racedAt := later.Add(time.Minute)
				task9InsertManagedLogRow(t, ctx, conn, domain, profileID, "coverage-raced", 2, racedAt)
				if err := stats.RecordActualCoverageAppend(ctx, conn, domain, []time.Time{racedAt}, racedAt); err == nil {
					t.Fatal("non-transactional coverage handoff was accepted")
				}
				assertState(t, domain, racedAt, false, false, revision)
				refresh(t, domain, racedAt)
				revision = assertState(t, domain, racedAt, true, false, "")
				rawAt := racedAt.Add(time.Minute)
				task9InsertManagedLogRow(t, ctx, conn, domain, profileID, "coverage-raw", 3, rawAt)
				pairedAt := rawAt.Add(time.Minute)
				handoff(t, domain, pairedAt, "coverage-paired", 4, nil)
				assertState(t, domain, pairedAt, false, false, revision)
				refresh(t, domain, pairedAt)
				multiAt := pairedAt.Add(time.Minute)
				handoff(t, domain, multiAt, "", 0, func(tx pgx.Tx) {
					task9InsertManagedLogRow(t, ctx, tx, domain, profileID, "coverage-multi-a", 5, multiAt)
					task9InsertManagedLogRow(t, ctx, tx, domain, profileID, "coverage-multi-b", 6, multiAt)
				})
				assertState(t, domain, multiAt, true, false, "")
			}
			cutoff := later.Add(time.Hour)
			_, err = conn.Exec(ctx, `UPDATE log_retention_policy_resources SET configured_logical_cutoff = $2, updated_at = $2 WHERE dataset = $1`, domain, cutoff)
			must(t, err)
			refresh(t, domain, cutoff)
			handoff(t, domain, later.Add(time.Minute), "below-"+domain, 7, nil)
			must(t, conn.QueryRow(ctx, `SELECT coverage_revision FROM retention_coverage_read_models WHERE dataset = $1 AND complete AND freshness = 'fresh' AND NOT dirty AND earliest_retained_at IS NULL AND latest_retained_at IS NULL AND gaps @> '[{"from_time":null,"to_time":null,"reason":"no_retained_intersection"}]'::jsonb`, domain).Scan(&revision))
		})
	}
}

func TestLogRetentionJobDropsExpiredPartitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, ctx, "log_retention_job_partitions")
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
	job, err := jobStore.CreateManualRetentionJob(ctx, "request_logs", &cutoff, false, "phase7-drop-expired")
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
	completed, err := jobStore.GetGlobalRetentionJobDetailDTO(ctx, job.ID)
	if err != nil {
		t.Fatalf("load completed job: %v", err)
	}
	if completed.Job.State != "succeeded" {
		t.Fatalf("expected job to succeed, got %s", completed.Job.State)
	}
}

func TestLogRetentionSettingsAndJobRoutesAreGlobal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, ctx, "log_retention_settings_routes")
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
	// Seed a 1-day baseline so the PUT is a non-destructive extension (no
	// preflight needed) while still exercising the global settings route.
	if _, err := pool.Exec(ctx, `UPDATE log_retention_settings SET request_logs_retention_days = 1, audit_logs_retention_days = 1, statistics_retention_days = 1, loadbalance_events_retention_days = 1, updated_at = now() WHERE singleton_key = 'global'`); err != nil {
		t.Fatalf("seed baseline retention policy: %v", err)
	}
	// The production startup owner refreshes actual coverage after the additive
	// migration. This route-level fixture does not run the full startup
	// sequence, so initialize the same bounded owner projections explicitly;
	// no policy, job, or base-table data is changed.
	for _, dataset := range []string{"request_logs", "audit_logs", "usage_request_events", "loadbalance_events"} {
		source, err := stats.LoadRetentionSourceProjection(ctx, pool, dataset, phase7Now)
		if err != nil {
			t.Fatalf("load %s retention source: %v", dataset, err)
		}
		if err := stats.RefreshActualCoverageProjection(ctx, pool, source, phase7Now); err != nil {
			t.Fatalf("refresh %s actual coverage: %v", dataset, err)
		}
	}
	put := httptest.NewRequest(http.MethodPut, "/settings/log-retention", bytes.NewBufferString(`{"operation_id":"route-op-1","expected_revision":"1","policies":{"request_logs_retention_days":3,"audit_logs_retention_days":4,"statistics_retention_days":5,"loadbalance_events_retention_days":6}}`))
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
	if err := retentionStore.EnsurePartitionForTime(ctx, "loadbalance_events", phase7Now.AddDate(0, 0, -7)); err != nil {
		t.Fatalf("ensure boundary partition: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE loadbalance_events_p20260423`); err != nil {
		t.Fatalf("analyze boundary partition: %v", err)
	}
	// Destructive manual cleanup: fresh preflight then sealed job acceptance
	// (SPEC §6). The old table/cutoff create route is removed.
	preflight := httptest.NewRequest(http.MethodPost, "/maintenance/log-retention/preflights", bytes.NewBufferString(`{"kind":"manual_cleanup","operation_id":"route-op-2","preflight_attempt_id":"attempt-1","dataset":"loadbalance_events","selection":{"mode":"keep_days","days":7}}`))
	preflight.Header.Set("Content-Type", "application/json")
	preflightRecorder := httptest.NewRecorder()
	router.ServeHTTP(preflightRecorder, preflight)
	if preflightRecorder.Code != http.StatusCreated || !strings.Contains(preflightRecorder.Body.String(), `"name":"loadbalance_events_p20260423"`) {
		t.Fatalf("POST preflight status=%d body=%s", preflightRecorder.Code, preflightRecorder.Body.String())
	}
	var preflightPayload struct {
		PreflightToken string `json:"preflight_token"`
	}
	if err := json.Unmarshal(preflightRecorder.Body.Bytes(), &preflightPayload); err != nil || preflightPayload.PreflightToken == "" {
		t.Fatalf("decode preflight response: %v body=%s", err, preflightRecorder.Body.String())
	}
	jobBody := fmt.Sprintf(`{"operation_id":"route-op-2","preflight_token":%q,"confirmation":{"keyword":"DELETE"}}`, preflightPayload.PreflightToken)
	post := httptest.NewRequest(http.MethodPost, "/maintenance/log-retention/jobs", bytes.NewBufferString(jobBody))
	post.Header.Set("Content-Type", "application/json")
	postRecorder := httptest.NewRecorder()
	router.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusAccepted || !strings.Contains(postRecorder.Body.String(), `"job"`) {
		t.Fatalf("POST /maintenance/log-retention/jobs status=%d body=%s", postRecorder.Code, postRecorder.Body.String())
	}
	var jobPayload map[string]any
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &jobPayload); err != nil {
		t.Fatalf("decode log retention job response: %v", err)
	}
	job, ok := jobPayload["job"].(map[string]any)
	if !ok || job["dataset"] != "loadbalance_events" {
		t.Fatalf("expected loadbalance retention manual job, got %+v", jobPayload)
	}
}

func TestManualRetentionPreflightStaleBeforeExecutionIsTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, ctx, "manual_preflight_stale_before_execution")
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString("manual_preflight_stale_before_execution"))
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
	for _, dataset := range []string{"request_logs", "audit_logs", "usage_request_events", "loadbalance_events"} {
		source, err := stats.LoadRetentionSourceProjection(ctx, pool, dataset, phase7Now)
		if err != nil {
			t.Fatalf("load %s retention source: %v", dataset, err)
		}
		if err := stats.RefreshActualCoverageProjection(ctx, pool, source, phase7Now); err != nil {
			t.Fatalf("refresh %s actual coverage: %v", dataset, err)
		}
	}
	router := chiRouterForSettings(service)
	preflight := httptest.NewRequest(http.MethodPost, "/maintenance/log-retention/preflights", bytes.NewBufferString(`{"kind":"manual_cleanup","operation_id":"stale-op","preflight_attempt_id":"attempt-1","dataset":"loadbalance_events","selection":{"mode":"keep_days","days":7}}`))
	preflight.Header.Set("Content-Type", "application/json")
	preflightRecorder := httptest.NewRecorder()
	router.ServeHTTP(preflightRecorder, preflight)
	if preflightRecorder.Code != http.StatusCreated {
		t.Fatalf("POST preflight status=%d body=%s", preflightRecorder.Code, preflightRecorder.Body.String())
	}
	var preflightPayload struct {
		PreflightToken string `json:"preflight_token"`
	}
	if err := json.Unmarshal(preflightRecorder.Body.Bytes(), &preflightPayload); err != nil || preflightPayload.PreflightToken == "" {
		t.Fatalf("decode preflight response: %v body=%s", err, preflightRecorder.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/maintenance/log-retention/jobs", bytes.NewBufferString(fmt.Sprintf(`{"operation_id":"stale-op","preflight_token":%q,"confirmation":{"keyword":"DELETE"}}`, preflightPayload.PreflightToken)))
	post.Header.Set("Content-Type", "application/json")
	postRecorder := httptest.NewRecorder()
	router.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusAccepted {
		t.Fatalf("POST manual job status=%d body=%s", postRecorder.Code, postRecorder.Body.String())
	}
	var jobPayload struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &jobPayload); err != nil || jobPayload.Job.ID == "" {
		t.Fatalf("decode manual job response: %v body=%s", err, postRecorder.Body.String())
	}
	// A semantic owner fence transition after acceptance must be caught before
	// purge_state enters running and before any physical operation is attempted.
	if _, err := pool.Exec(ctx, `UPDATE log_retention_policy_resources SET
		fence_generation = fence_generation + 1, updated_at = now()
		WHERE dataset = 'loadbalance_events'`); err != nil {
		t.Fatalf("advance owner fence: %v", err)
	}
	if err := jobStore.ProcessDue(ctx); err == nil || !strings.Contains(err.Error(), "preflight_stale_before_execution") {
		t.Fatalf("expected terminal stale-preflight error, got %v", err)
	}
	detail, err := jobStore.GetGlobalRetentionJobDetailDTO(ctx, jobPayload.Job.ID)
	if err != nil {
		t.Fatalf("load stale job detail: %v", err)
	}
	if detail.Job.State != "failed" || detail.Job.Error == nil || detail.Job.Error.Code != "preflight_stale_before_execution" {
		t.Fatalf("expected terminal stale preflight job, got %+v", detail.Job)
	}
	var purgeState string
	if err := pool.QueryRow(ctx, `SELECT purge_state FROM log_retention_policy_resources WHERE dataset = 'loadbalance_events'`).Scan(&purgeState); err != nil {
		t.Fatalf("load owner purge state: %v", err)
	}
	if purgeState != "idle" {
		t.Fatalf("stale preflight must not acquire a purge fence, got %s", purgeState)
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
	request := httptest.NewRequest(http.MethodPost, "/management/jobs/"+job.ID+"/cancel?scope=global&type=log_retention", bytes.NewBufferString(`{"operation_id":"cancel-op-queued"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST global queued cancel status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		OperationID string `json:"operation_id"`
		Replayed    bool   `json:"replayed"`
		Job         struct {
			State         string `json:"state"`
			CancelAllowed bool   `json:"cancel_allowed"`
		} `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if payload.OperationID != "cancel-op-queued" || payload.Replayed || payload.Job.State != "cancelled" {
		t.Fatalf("expected queued global log retention job to cancel, got %+v", payload)
	}
	detail, err := store.GetGlobalRetentionJobDetailDTO(ctx, job.ID)
	if err != nil {
		t.Fatalf("reload global queued job: %v", err)
	}
	if detail.Job.State != "cancelled" || detail.Job.FinishedAt == nil || detail.Job.Progress.DroppedPartitionNamesPreview == nil || len(detail.Job.Progress.DroppedPartitionNamesPreview) != 0 {
		t.Fatalf("expected cancelled global job to have terminal empty-partition progress, got %+v", detail)
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
	request := httptest.NewRequest(http.MethodPost, "/management/jobs/"+job.ID+"/cancel?scope=global&type=log_retention", bytes.NewBufferString(`{"operation_id":"cancel-op-running"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	// A manual purge job is cancellable only while queued; once running it
	// must complete or recover (SPEC §7.3).
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "purge_not_cancellable") {
		t.Fatalf("POST global running manual cancel status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGlobalLogRetentionRunningCancelStore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store, pool, _ := phase7JobStore(t, ctx, "global_running_cancel_store")
	defer pool.Close()
	job := phase7CreateLogRetentionJob(t, ctx, store, "global-running-cancel-store")
	if _, err := pool.Exec(ctx, `UPDATE management_jobs SET state = 'running', locked_by = 'phase7-worker', locked_until = now() + interval '5 minutes', last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("seed running global job: %v", err)
	}

	_, _, err := store.CancelGlobalRetentionJobDTO(ctx, job.ID, "cancel-op-running-store")
	if err == nil || !managementjobs.IsPurgeNotCancellable(err) {
		t.Fatalf("expected running manual global job cancel to refuse, got %v", err)
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

func TestDashboardRecentActivityEmptyContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	databaseName := "dashboard_recent_activity_empty"
	conn := harness.openDatabase(t, ctx, databaseName)
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
	databaseName := "dashboard_recent_activity_bounded"
	conn := harness.openDatabase(t, ctx, databaseName)
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
	for _, key := range []string{"request_log_id", "created_at", "model_id", "model_label", "resolved_target_model_id", "resolved_target_model_label", "endpoint_id", "endpoint_label", "status_code", "response_time_ms", "ttft_ms", "completion_duration_ms", "is_stream", "stream_outcome", "total_tokens", "total_cost_user_currency_micros", "pricing_status", "unpriced_reason", "report_currency_symbol"} {
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
	conn := harness.openDatabase(t, ctx, name)
	return conn
}

func phase7JobStore(t *testing.T, ctx context.Context, name string) (*managementjobs.Store, *pgxpool.Pool, int) {
	t.Helper()
	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, ctx, name)
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

func phase7CreateLogRetentionJob(t *testing.T, ctx context.Context, store *managementjobs.Store, key string) managementjobs.RetentionJobSummaryDTO {
	t.Helper()
	cutoff := phase7Now.Add(-24 * time.Hour)
	job, err := store.CreateManualRetentionJob(ctx, "request_logs", &cutoff, false, "phase7-"+key)
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
	if err := exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, created_at, updated_at) VALUES ($1, $2, $3, 'phase7-key', $4, $4) RETURNING id`, profileID, endpointName, "https://"+suffix+".invalid", now).Scan(&endpointID); err != nil {
		t.Fatalf("insert dashboard routing endpoint: %v", err)
	}
	var connectionID int
	if err := exec.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, $3, NULL, NULL, 'healthy', NULL, NULL, $4, $4) RETURNING id`, profileID, endpointID, "Phase 7 "+suffix+" Connection", now).Scan(&connectionID); err != nil {
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
	if _, err := exec.Exec(ctx, `INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, endpoint_description, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, 'openai', $4, $5, $6, 1, $7, 'upstream', 'runtime_scrubbed', $8, 100, FALSE, $9, 'ineligible', 'trusted', '/v1/chat/completions', $10, $11, FALSE, FALSE)`, id, profileID, route.ModelID, route.EndpointID, route.ConnectionID, fmt.Sprintf("phase7-dashboard-request-%d", id), "https://dashboard-routing.invalid", statusCode, statusCode >= 200 && statusCode < 300, route.EndpointName, createdAt.UTC()); err != nil {
		t.Fatalf("insert dashboard request log %d: %v", id, err)
	}
}

func phase7InsertRecentActivityRequestLog(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, route phase7DashboardRoutingTarget, statusCode int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "request_logs", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, endpoint_base_url, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, input_tokens, output_tokens, total_tokens, success_flag, pricing_status, pricing_evidence_trust, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, $3, 'openai', $4, $5, $6, 'upstream', 'runtime_scrubbed', $7, 123, 45, 678, TRUE, 'completed', 5, 7, 12, $8, 'ineligible', 'trusted', NULL, NULL, NULL, '/v1/chat/completions', $9, FALSE, FALSE)`, id, profileID, route.ModelID, route.EndpointID, route.ConnectionID, "https://recent-activity.invalid", statusCode, statusCode >= 200 && statusCode < 300, createdAt.UTC()); err != nil {
		t.Fatalf("insert recent activity request log %d: %v", id, err)
	}
}

func phase7InsertDashboardUsageEvent(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, route phase7DashboardRoutingTarget, statusCode int, successFlag bool, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "usage_request_events", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_id, endpoint_label_snapshot, connection_id, status_code, success_flag, pricing_status, pricing_evidence_trust, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, attempt_count, request_path, created_at, response_time_ms) VALUES ($1, $2, $3, $4, 'openai', $5, $6, $7, $8, $9, 'ineligible', 'trusted', 3, 5, 8, NULL, NULL, NULL, 1, '/v1/chat/completions', $10, 100)`, id, profileID, fmt.Sprintf("phase7-dashboard-usage-%d", id), route.ModelID, route.EndpointID, route.EndpointName, route.ConnectionID, statusCode, successFlag, createdAt.UTC()); err != nil {
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

func phase7InsertAuditLog(t *testing.T, ctx context.Context, exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, profileID int, id int, createdAt time.Time) {
	t.Helper()
	phase7EnsureLogPartition(t, ctx, exec, "audit_logs", createdAt)
	if _, err := exec.Exec(ctx, `INSERT INTO audit_logs (id, profile_id, model_id, request_method, request_url, request_headers, request_headers_scrub_provenance, request_headers_capture_status, upstream_status_code, response_headers, response_headers_scrub_provenance, response_headers_capture_status, is_stream, row_kind, url_scrub_provenance, attempt_duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, 'phase7-model', 'POST', 'https://phase7.invalid/v1/chat/completions', '{}', 'runtime_scrubbed', 'captured', 200, '{}', 'runtime_scrubbed', 'captured', FALSE, 'upstream', 'runtime_scrubbed', 100, TRUE, FALSE, $3)`, id, profileID, createdAt.UTC()); err != nil {
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

func int64Ptr(value int64) *int64 {
	resolved := value
	return &resolved
}

func stringPtr(value string) *string {
	resolved := value
	return &resolved
}

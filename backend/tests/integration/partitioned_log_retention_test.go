package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/lifecycle"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
)

func TestLogRetentionPartitionLifecycle(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "log_retention_partition_lifecycle"
	databaseURL := harness.connectionString(databaseName)
	conn := harness.openDatabase(t, testContext, databaseName)

	service := newStartupService(t, databaseURL, nil)
	if _, err := service.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup sequence on fresh database: %v", err)
	}
	bootstrapStart := task9UTCDay(time.Now().UTC())
	app, _, err := lifecycle.NewProductionApp(testContext, productionLifecycleSettings(databaseURL))
	if err != nil {
		t.Fatalf("build production app for log partition bootstrap: %v", err)
	}
	appShutdown := false
	defer func() {
		if !appShutdown {
			_ = app.Shutdown(context.Background())
		}
	}()
	task9AssertBootstrapHorizon(t, testContext, conn, bootstrapStart)
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown production app: %v", err)
	}
	appShutdown = true
	if err := conn.Close(testContext); err != nil {
		t.Fatalf("close startup connection: %v", err)
	}

	pool, err := pgxpool.New(testContext, databaseURL)
	if err != nil {
		t.Fatalf("open retention pool: %v", err)
	}
	defer pool.Close()

	retentionNow := time.Date(2026, time.February, 20, 9, 0, 0, 0, time.UTC)
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool, Now: func() time.Time { return retentionNow }})
	jobStore := managementjobs.NewStore(managementjobs.Options{Pool: pool, LogRetention: retentionStore, Now: func() time.Time { return retentionNow }})
	profileID := task9LoadActiveProfileID(t, testContext, pool)
	baseDay := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	cutoff := baseDay.AddDate(0, 0, 1).Add(12 * time.Hour)

	task9InsertRuntimeActivityAcrossDays(t, testContext, databaseURL, pool, retentionStore, profileID, baseDay)
	for _, tableName := range []string{"request_logs", "usage_request_events"} {
		task9AssertChildRows(t, testContext, pool, tableName, baseDay, 1)
		task9AssertChildRows(t, testContext, pool, tableName, baseDay.AddDate(0, 0, 1), 1)
		task9AssertChildRows(t, testContext, pool, tableName, baseDay.AddDate(0, 0, 2), 1)
	}

	for _, tableName := range logretention.ManagedTables() {
		task9SeedManagedTableRows(t, testContext, pool, retentionStore, tableName, profileID, baseDay)
		task9AssertChildRows(t, testContext, pool, tableName, baseDay, task9ExpectedSeededRows(tableName, 1))
		task9AssertChildRows(t, testContext, pool, tableName, baseDay.AddDate(0, 0, 1), task9ExpectedSeededRows(tableName, 2))
		task9AssertChildRows(t, testContext, pool, tableName, baseDay.AddDate(0, 0, 2), task9ExpectedSeededRows(tableName, 1))
	}

	for _, tableName := range logretention.ManagedTables() {
		task9RunLogRetentionJob(t, testContext, jobStore, tableName, &cutoff, false, int64(task9BoundaryRowsOlderThanCutoff(tableName)))
		task9AssertPostCutoffRetention(t, testContext, pool, tableName, baseDay, cutoff)
	}

	for _, tableName := range logretention.ManagedTables() {
		task9RunLogRetentionJob(t, testContext, jobStore, tableName, nil, true, 0)
		task9AssertDeleteAllRebootstrap(t, testContext, pool, retentionStore, tableName, retentionNow)
	}
}

func TestScheduledGlobalLogRetentionProcessesStoredSettings(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "scheduled_global_log_retention"
	conn := harness.openDatabase(t, testContext, databaseName)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := conn.Close(testContext); err != nil {
		t.Fatalf("close migration connection: %v", err)
	}

	pool, err := pgxpool.New(testContext, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open scheduled retention pool: %v", err)
	}
	defer pool.Close()

	retentionNow := time.Date(2026, time.February, 20, 9, 0, 0, 0, time.UTC)
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool, Now: func() time.Time { return retentionNow }})
	jobStore := managementjobs.NewStore(managementjobs.Options{Pool: pool, LogRetention: retentionStore, Now: func() time.Time { return retentionNow }})
	profileID := task9InsertProfile(t, testContext, pool)
	baseDay := time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC)
	cutoff := retentionNow.Add(-2 * 24 * time.Hour)

	for _, tableName := range logretention.ManagedTables() {
		task9SeedManagedTableRows(t, testContext, pool, retentionStore, tableName, profileID, baseDay)
	}
	_, err = pool.Exec(testContext, `UPDATE log_retention_settings SET request_logs_retention_days = 2, audit_logs_retention_days = 2, statistics_retention_days = 2, loadbalance_events_retention_days = 2, updated_at = $1 WHERE singleton_key = 'global'`, retentionNow)
	if err != nil {
		t.Fatalf("update global retention settings: %v", err)
	}
	if err := jobStore.ScheduleGlobalLogRetention(testContext); err != nil {
		t.Fatalf("schedule global log retention: %v", err)
	}
	for range logretention.ManagedTables() {
		if err := jobStore.ProcessDue(testContext); err != nil {
			t.Fatalf("process scheduled global retention job: %v", err)
		}
	}
	if err := jobStore.ScheduleGlobalLogRetention(testContext); err != nil {
		t.Fatalf("reschedule global log retention: %v", err)
	}
	if got := task9CountScheduledRetentionJobs(t, testContext, pool); got != len(logretention.ManagedTables()) {
		t.Fatalf("expected one scheduled retention job per managed table, got %d", got)
	}
	for _, tableName := range logretention.ManagedTables() {
		task9AssertPostCutoffRetentionWithFutureRows(t, testContext, pool, tableName, baseDay, cutoff, 1)
		task9AssertScheduledRetentionJobSucceeded(t, testContext, pool, tableName)
	}
}

func TestLogPartitionToastDiagnostics(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "log_partition_toast_diagnostics"
	conn := harness.openDatabase(t, testContext, databaseName)
	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := conn.Close(testContext); err != nil {
		t.Fatalf("close migration connection: %v", err)
	}

	pool, err := pgxpool.New(testContext, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open toast diagnostics pool: %v", err)
	}
	defer pool.Close()

	retentionNow := time.Date(2026, time.February, 20, 9, 0, 0, 0, time.UTC)
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool, Now: func() time.Time { return retentionNow }})
	jobStore := managementjobs.NewStore(managementjobs.Options{Pool: pool, LogRetention: retentionStore, Now: func() time.Time { return retentionNow }})
	profileID := task9InsertProfile(t, testContext, pool)
	toastDay := time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC)
	if err := retentionStore.EnsurePartitionForTime(testContext, "request_logs", toastDay); err != nil {
		t.Fatalf("ensure request_logs toast diagnostics partition: %v", err)
	}

	childName := task9PartitionName("request_logs", toastDay)
	payload := task9OversizedPayload()
	task9InsertToastRequestLog(t, testContext, pool, profileID, toastDay.Add(8*time.Hour), payload)
	sizeFacts := task12LoadRelationSizeFacts(t, testContext, pool, "request_logs", childName)
	task12AssertValidRelationSizeFacts(t, sizeFacts)
	if rows := task9CountRowsInRelation(t, testContext, pool, sizeFacts.toastSchemaName, sizeFacts.toastName); rows == 0 {
		t.Fatalf("expected oversized request log to create rows in toast relation %s.%s", sizeFacts.toastSchemaName, sizeFacts.toastName)
	}
	t.Logf("request_logs diagnostics: root=%s total=%d main=%d child=%s total=%d main=%d toast=%s.%s total=%d main=%d", sizeFacts.rootName, sizeFacts.rootTotalBytes, sizeFacts.rootMainBytes, sizeFacts.childName, sizeFacts.childTotalBytes, sizeFacts.childMainBytes, sizeFacts.toastSchemaName, sizeFacts.toastName, sizeFacts.toastTotalBytes, sizeFacts.toastMainBytes)

	cutoff := toastDay.AddDate(0, 0, 1)
	task9RunLogRetentionJob(t, testContext, jobStore, "request_logs", &cutoff, false, 0)
	task9AssertPartitionDropped(t, testContext, pool, "request_logs", childName)
	task9AssertRelationOIDMissing(t, testContext, pool, sizeFacts.childOID)
	task9AssertRelationOIDMissing(t, testContext, pool, sizeFacts.toastOID)
}

type task9Exec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type task9Queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type task9QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type task12RelationSizeFacts struct {
	rootName        string
	rootOID         int64
	rootTotalBytes  int64
	rootMainBytes   int64
	rootToastOID    int64
	childName       string
	childOID        int64
	childTotalBytes int64
	childMainBytes  int64
	childToastOID   int64
	toastSchemaName string
	toastName       string
	toastOID        int64
	toastTotalBytes int64
	toastMainBytes  int64
}

func task9SeedManagedTableRows(t *testing.T, ctx context.Context, exec task9Exec, retentionStore *logretention.Store, tableName string, profileID int, baseDay time.Time) {
	t.Helper()
	for offset := range 3 {
		if err := retentionStore.EnsurePartitionForTime(ctx, tableName, baseDay.AddDate(0, 0, offset)); err != nil {
			t.Fatalf("ensure %s partition offset %d: %v", tableName, offset, err)
		}
	}

	rows := []time.Time{
		baseDay.Add(8 * time.Hour),
		baseDay.AddDate(0, 0, 1).Add(4 * time.Hour),
		baseDay.AddDate(0, 0, 1).Add(18 * time.Hour),
		baseDay.AddDate(0, 0, 2).Add(8 * time.Hour),
	}
	for index, createdAt := range rows {
		marker := fmt.Sprintf("t9-%s-%d", task9TableMarkerPrefix(tableName), index)
		task9InsertManagedLogRow(t, ctx, exec, tableName, profileID, marker, index, createdAt)
	}
}

func task9InsertManagedLogRow(t *testing.T, ctx context.Context, exec task9Exec, tableName string, profileID int, marker string, rowIndex int, createdAt time.Time) {
	t.Helper()
	switch tableName {
	case "request_logs":
		_, err := exec.Exec(ctx, `INSERT INTO request_logs (profile_id, model_id, api_family, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, created_at) VALUES ($1, 'task9-model', 'openai', 200, 25, FALSE, TRUE, TRUE, TRUE, $2, $3)`, profileID, "/v1/task9/"+marker, createdAt.UTC())
		if err != nil {
			t.Fatalf("insert request_logs row %s: %v", marker, err)
		}
	case "audit_logs":
		_, err := exec.Exec(ctx, `INSERT INTO audit_logs (profile_id, model_id, request_method, request_url, request_headers, response_status, response_headers, is_stream, duration_ms, created_at) VALUES ($1, 'task9-model', 'POST', $2, '{}', 200, '{}', FALSE, 25, $3)`, profileID, "https://task9.invalid/"+marker, createdAt.UTC())
		if err != nil {
			t.Fatalf("insert audit_logs row %s: %v", marker, err)
		}
	case "usage_request_events":
		_, err := exec.Exec(ctx, `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, billable_flag, priced_flag, attempt_count, request_path, created_at) VALUES ($1, $2, 'task9-model', 'openai', 'task9 retained endpoint', 200, TRUE, TRUE, TRUE, 1, '/v1/task9', $3)`, profileID, marker, createdAt.UTC())
		if err != nil {
			t.Fatalf("insert usage_request_events row %s: %v", marker, err)
		}
	case "loadbalance_events":
		_, err := exec.Exec(ctx, `INSERT INTO loadbalance_events (profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, ban_mode, created_at) VALUES ($1, $2, 'retry_scheduled', 'transient_http', $3, $3, $4, 60000, 'off', $5)`, profileID, 9000+rowIndex, rowIndex+1, createdAt.UTC().Add(time.Minute), createdAt.UTC())
		if err != nil {
			t.Fatalf("insert loadbalance_events row %s: %v", marker, err)
		}
	default:
		t.Fatalf("unknown managed table %s", tableName)
	}
}

func task9InsertToastRequestLog(t *testing.T, ctx context.Context, exec task9Exec, profileID int, createdAt time.Time, payload string) {
	t.Helper()
	_, err := exec.Exec(ctx, `INSERT INTO request_logs (profile_id, model_id, api_family, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, error_detail, request_generation_params, created_at) VALUES ($1, 'task9-toast-model', 'openai', 500, 250, FALSE, FALSE, TRUE, FALSE, '/v1/task9/toast', $2, jsonb_build_object('payload', $3::text), $4)`, profileID, payload, payload, createdAt.UTC())
	if err != nil {
		t.Fatalf("insert toast-bearing request log: %v", err)
	}
}

func task9InsertProfile(t *testing.T, ctx context.Context, exec task9QueryRower) int {
	t.Helper()
	var profileID int
	if err := exec.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ('Task 9 Retention', NULL, FALSE, FALSE, TRUE, 1, $1, $1) RETURNING id`, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)).Scan(&profileID); err != nil {
		t.Fatalf("insert task9 profile: %v", err)
	}
	return profileID
}

func task9RunLogRetentionJob(t *testing.T, ctx context.Context, store *managementjobs.Store, tableName string, cutoff *time.Time, deleteAll bool, expectedRowsDeleted int64) managementjobs.Job {
	t.Helper()
	job, err := store.CreateLogRetentionJob(ctx, managementjobs.CreateLogRetentionJobRequest{
		RequestedBy: "task9",
		Reason:      "partition retention integration coverage",
		Scope: managementjobs.LogRetentionScope{
			Table:     tableName,
			Cutoff:    cutoff,
			DeleteAll: deleteAll,
		},
	})
	if err != nil {
		t.Fatalf("create %s log retention job deleteAll=%v: %v", tableName, deleteAll, err)
	}
	if err := store.ProcessDue(ctx); err != nil {
		t.Fatalf("process %s log retention job %s: %v", tableName, job.ID, err)
	}
	completed, err := store.GetGlobalJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("load completed log retention job %s: %v", job.ID, err)
	}
	if completed.State != "succeeded" {
		t.Fatalf("expected %s log retention job %s to succeed, got %s: %v", tableName, job.ID, completed.State, completed.ErrorMessage)
	}
	if completed.Progress.RowsDeleted != expectedRowsDeleted {
		t.Fatalf("expected %s job %s rows_deleted=%d, got %d", tableName, job.ID, expectedRowsDeleted, completed.Progress.RowsDeleted)
	}
	return completed
}

func task9CountScheduledRetentionJobs(t *testing.T, ctx context.Context, queryer task9QueryRower) int {
	t.Helper()
	var count int
	if err := queryer.QueryRow(ctx, `SELECT count(*) FROM management_jobs WHERE type = 'log_retention' AND requested_by = 'scheduled-log-retention'`).Scan(&count); err != nil {
		t.Fatalf("count scheduled log retention jobs: %v", err)
	}
	return count
}

func task9AssertScheduledRetentionJobSucceeded(t *testing.T, ctx context.Context, queryer task9QueryRower, tableName string) {
	t.Helper()
	var count int
	err := queryer.QueryRow(ctx, `
		SELECT count(*)
		FROM management_jobs
		WHERE type = 'log_retention'
		  AND requested_by = 'scheduled-log-retention'
		  AND state = 'succeeded'
		  AND profile_id = 0
		  AND scope_json->>'table' = $1
		  AND idempotency_key = $1 || ':2026-02-20'
	`, tableName).Scan(&count)
	if err != nil {
		t.Fatalf("count scheduled %s retention jobs: %v", tableName, err)
	}
	if count != 1 {
		t.Fatalf("expected one succeeded scheduled retention job for %s, got %d", tableName, count)
	}
}

func task9AssertPostCutoffRetention(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string, baseDay time.Time, cutoff time.Time) {
	t.Helper()
	task9AssertPostCutoffRetentionWithFutureRows(t, ctx, pool, tableName, baseDay, cutoff, task9ExpectedRuntimeRows(tableName)+1)
}

func task9AssertPostCutoffRetentionWithFutureRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string, baseDay time.Time, cutoff time.Time, futureRetainedRows int) {
	t.Helper()
	task9AssertPartitionDropped(t, ctx, pool, tableName, task9PartitionName(tableName, baseDay))
	boundaryChild := task9PartitionName(tableName, baseDay.AddDate(0, 0, 1))
	futureChild := task9PartitionName(tableName, baseDay.AddDate(0, 0, 2))
	boundaryRetainedRows := 1
	task9AssertChildRows(t, ctx, pool, tableName, baseDay.AddDate(0, 0, 1), boundaryRetainedRows)
	task9AssertChildRows(t, ctx, pool, tableName, baseDay.AddDate(0, 0, 2), futureRetainedRows)
	if got := task9CountChildRowsBefore(t, ctx, pool, boundaryChild, cutoff); got != 0 {
		t.Fatalf("expected %s boundary child to have no rows older than cutoff, got %d", boundaryChild, got)
	}
	if got := task9CountChildRowsOnOrAfter(t, ctx, pool, boundaryChild, cutoff); got != boundaryRetainedRows {
		t.Fatalf("expected %s boundary child to retain %d rows at or after cutoff, got %d", boundaryChild, boundaryRetainedRows, got)
	}
	if got := task9CountRowsInRelation(t, ctx, pool, "public", futureChild); got != futureRetainedRows {
		t.Fatalf("expected future child %s retained row count %d, got %d", futureChild, futureRetainedRows, got)
	}
	if got := task9CountRootRowsBefore(t, ctx, pool, tableName, cutoff); got != 0 {
		t.Fatalf("expected %s root to have no rows older than cutoff after retention, got %d", tableName, got)
	}
}

func task9AssertDeleteAllRebootstrap(t *testing.T, ctx context.Context, pool *pgxpool.Pool, retentionStore *logretention.Store, tableName string, retentionNow time.Time) {
	t.Helper()
	partitions, err := retentionStore.ListPartitions(ctx, tableName)
	if err != nil {
		t.Fatalf("list %s partitions after delete-all: %v", tableName, err)
	}
	if len(partitions) != logretention.HorizonDays() {
		t.Fatalf("expected %d %s partitions after delete-all rebootstrap, got %+v", logretention.HorizonDays(), tableName, partitions)
	}
	expectedStart := task9UTCDay(retentionNow)
	for offset, partition := range partitions {
		expectedDay := expectedStart.AddDate(0, 0, offset)
		expectedName := task9PartitionName(tableName, expectedDay)
		if partition.PartitionName != expectedName || !partition.Start.Equal(expectedDay) || !partition.End.Equal(expectedDay.AddDate(0, 0, 1)) {
			t.Fatalf("unexpected %s partition at offset %d after delete-all: %+v, expected %s", tableName, offset, partition, expectedName)
		}
		if got := task9CountRowsInRelation(t, ctx, pool, "public", partition.PartitionName); got != 0 {
			t.Fatalf("expected rebootstrap partition %s to be empty, got %d rows", partition.PartitionName, got)
		}
	}
}

func task9AssertBootstrapHorizon(t *testing.T, ctx context.Context, queryer task9Queryer, expectedStart time.Time) {
	t.Helper()
	for _, tableName := range logretention.ManagedTables() {
		partitionNames := task9CatalogPartitionNames(t, ctx, queryer, tableName)
		if task9PartitionNamesMatchHorizon(tableName, partitionNames, expectedStart) {
			continue
		}
		if task9PartitionNamesMatchHorizon(tableName, partitionNames, expectedStart.AddDate(0, 0, 1)) {
			continue
		}
		t.Fatalf("expected startup bootstrap horizon for %s from %s, got %v", tableName, expectedStart.Format("2006-01-02"), partitionNames)
	}
}

func task9AssertChildRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string, day time.Time, expected int) {
	t.Helper()
	childName := task9PartitionName(tableName, day)
	if !task9PartitionAttached(t, ctx, pool, tableName, childName) {
		t.Fatalf("expected partition %s to be attached to %s", childName, tableName)
	}
	if got := task9CountRowsInRelation(t, ctx, pool, "public", childName); got != expected {
		t.Fatalf("expected %s to contain %d rows, got %d", childName, expected, got)
	}
}

func task9AssertPartitionDropped(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string, childName string) {
	t.Helper()
	if task9PartitionAttached(t, ctx, pool, tableName, childName) {
		t.Fatalf("expected partition %s to be detached from %s", childName, tableName)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+childName).Scan(&exists); err != nil {
		t.Fatalf("check dropped partition %s regclass: %v", childName, err)
	}
	if exists {
		t.Fatalf("expected dropped partition relation %s to be absent from catalog", childName)
	}
}

func task9PartitionAttached(t *testing.T, ctx context.Context, queryer task9QueryRower, tableName string, childName string) bool {
	t.Helper()
	var exists bool
	if err := queryer.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_inherits inheritance
			JOIN pg_class parent ON parent.oid = inheritance.inhparent
			JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
			JOIN pg_class child ON child.oid = inheritance.inhrelid
			JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
			WHERE parent_ns.nspname = 'public' AND child_ns.nspname = 'public'
			  AND parent.relname = $1 AND child.relname = $2
		)`, tableName, childName).Scan(&exists); err != nil {
		t.Fatalf("check partition %s attachment to %s: %v", childName, tableName, err)
	}
	return exists
}

func task9CatalogPartitionNames(t *testing.T, ctx context.Context, queryer task9Queryer, tableName string) []string {
	t.Helper()
	rows, err := queryer.Query(ctx, `
		SELECT child.relname
		FROM pg_inherits inheritance
		JOIN pg_class parent ON parent.oid = inheritance.inhparent
		JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
		JOIN pg_class child ON child.oid = inheritance.inhrelid
		JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
		WHERE parent_ns.nspname = 'public' AND child_ns.nspname = 'public' AND parent.relname = $1
		ORDER BY child.relname`, tableName)
	if err != nil {
		t.Fatalf("query %s child partitions: %v", tableName, err)
	}
	defer rows.Close()
	partitionNames := []string{}
	for rows.Next() {
		var partitionName string
		if err := rows.Scan(&partitionName); err != nil {
			t.Fatalf("scan %s child partition: %v", tableName, err)
		}
		partitionNames = append(partitionNames, partitionName)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s child partitions: %v", tableName, err)
	}
	return partitionNames
}

func task9PartitionNamesMatchHorizon(tableName string, partitionNames []string, expectedStart time.Time) bool {
	if len(partitionNames) != logretention.HorizonDays() {
		return false
	}
	for offset, partitionName := range partitionNames {
		if partitionName != task9PartitionName(tableName, expectedStart.AddDate(0, 0, offset)) {
			return false
		}
	}
	return true
}

func task9CountRowsInRelation(t *testing.T, ctx context.Context, queryer task9QueryRower, schemaName string, relationName string) int {
	t.Helper()
	query := fmt.Sprintf(`SELECT count(*) FROM %s.%s`, quoteIdentifier(schemaName), quoteIdentifier(relationName))
	var count int
	if err := queryer.QueryRow(ctx, query).Scan(&count); err != nil {
		t.Fatalf("count rows in %s.%s: %v", schemaName, relationName, err)
	}
	return count
}

func task9CountChildRowsBefore(t *testing.T, ctx context.Context, queryer task9QueryRower, childName string, cutoff time.Time) int {
	t.Helper()
	query := fmt.Sprintf(`SELECT count(*) FROM public.%s WHERE created_at < $1`, quoteIdentifier(childName))
	var count int
	if err := queryer.QueryRow(ctx, query, cutoff.UTC()).Scan(&count); err != nil {
		t.Fatalf("count rows before cutoff in %s: %v", childName, err)
	}
	return count
}

func task9CountChildRowsOnOrAfter(t *testing.T, ctx context.Context, queryer task9QueryRower, childName string, cutoff time.Time) int {
	t.Helper()
	query := fmt.Sprintf(`SELECT count(*) FROM public.%s WHERE created_at >= $1`, quoteIdentifier(childName))
	var count int
	if err := queryer.QueryRow(ctx, query, cutoff.UTC()).Scan(&count); err != nil {
		t.Fatalf("count rows on or after cutoff in %s: %v", childName, err)
	}
	return count
}

func task9CountRootRowsBefore(t *testing.T, ctx context.Context, queryer task9QueryRower, tableName string, cutoff time.Time) int {
	t.Helper()
	query := fmt.Sprintf(`SELECT count(*) FROM public.%s WHERE created_at < $1`, quoteIdentifier(tableName))
	var count int
	if err := queryer.QueryRow(ctx, query, cutoff.UTC()).Scan(&count); err != nil {
		t.Fatalf("count %s root rows before cutoff: %v", tableName, err)
	}
	return count
}

func task12LoadRelationSizeFacts(t *testing.T, ctx context.Context, queryer task9QueryRower, tableName string, childName string) task12RelationSizeFacts {
	t.Helper()
	var facts task12RelationSizeFacts
	if err := queryer.QueryRow(ctx, `
		SELECT
			root.relname,
			root.oid::int8,
			pg_total_relation_size(root.oid)::int8,
			pg_relation_size(root.oid)::int8,
			root.reltoastrelid::int8,
			child.relname,
			child.oid::int8,
			pg_total_relation_size(child.oid)::int8,
			pg_relation_size(child.oid)::int8,
			child.reltoastrelid::int8,
			toast_ns.nspname,
			toast.relname,
			toast.oid::int8,
			pg_total_relation_size(toast.oid)::int8,
			pg_relation_size(toast.oid)::int8
		FROM pg_class root
		JOIN pg_namespace root_ns ON root_ns.oid = root.relnamespace
		JOIN pg_inherits inheritance ON inheritance.inhparent = root.oid
		JOIN pg_class child ON child.oid = inheritance.inhrelid
		JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
		JOIN pg_class toast ON toast.oid = child.reltoastrelid
		JOIN pg_namespace toast_ns ON toast_ns.oid = toast.relnamespace
		WHERE root_ns.nspname = 'public'
		  AND child_ns.nspname = 'public'
		  AND root.relname = $1
		  AND child.relname = $2`, tableName, childName).Scan(
		&facts.rootName,
		&facts.rootOID,
		&facts.rootTotalBytes,
		&facts.rootMainBytes,
		&facts.rootToastOID,
		&facts.childName,
		&facts.childOID,
		&facts.childTotalBytes,
		&facts.childMainBytes,
		&facts.childToastOID,
		&facts.toastSchemaName,
		&facts.toastName,
		&facts.toastOID,
		&facts.toastTotalBytes,
		&facts.toastMainBytes,
	); err != nil {
		t.Fatalf("load relation size facts for %s/%s: %v", tableName, childName, err)
	}
	return facts
}

func task12AssertValidRelationSizeFacts(t *testing.T, facts task12RelationSizeFacts) {
	t.Helper()
	if facts.rootName != "request_logs" {
		t.Fatalf("expected root relation request_logs, got %s", facts.rootName)
	}
	if facts.childName == "" || facts.childOID == 0 {
		t.Fatalf("expected child relation identity, got %+v", facts)
	}
	if facts.rootOID == 0 || facts.rootTotalBytes < 0 || facts.rootMainBytes < 0 {
		t.Fatalf("expected valid root size facts, got %+v", facts)
	}
	if facts.rootToastOID != 0 {
		t.Fatalf("expected partitioned root to have reltoastrelid=0, got %+v", facts)
	}
	if facts.childTotalBytes <= 0 || facts.childMainBytes <= 0 {
		t.Fatalf("expected valid child size facts, got %+v", facts)
	}
	if facts.childToastOID == 0 || facts.childToastOID != facts.toastOID {
		t.Fatalf("expected child reltoastrelid to identify toast relation, got %+v", facts)
	}
	if facts.toastSchemaName == "" || facts.toastName == "" || facts.toastTotalBytes <= 0 || facts.toastMainBytes <= 0 {
		t.Fatalf("expected valid toast size facts, got %+v", facts)
	}
}

func task9AssertRelationOIDMissing(t *testing.T, ctx context.Context, queryer task9QueryRower, oid int64) {
	t.Helper()
	var exists bool
	if err := queryer.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE oid = $1::oid)`, fmt.Sprint(oid)).Scan(&exists); err != nil {
		t.Fatalf("check relation oid %d: %v", oid, err)
	}
	if exists {
		t.Fatalf("expected relation oid %d to be absent from catalog", oid)
	}
}

func task9OversizedPayload() string {
	var builder strings.Builder
	builder.Grow(512 * 1024)
	for index := range 8192 {
		input := fmt.Appendf(nil, "task-9-toast-payload-%06d", index)
		sum := sha256.Sum256(input)
		builder.WriteString(hex.EncodeToString(sum[:]))
	}
	return builder.String()
}

func task9PartitionName(tableName string, day time.Time) string {
	return fmt.Sprintf("%s_p%s", tableName, task9UTCDay(day).Format("20060102"))
}

func task9UTCDay(timestamp time.Time) time.Time {
	year, month, day := timestamp.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func task9TableMarkerPrefix(tableName string) string {
	switch tableName {
	case "request_logs":
		return "req"
	case "audit_logs":
		return "aud"
	case "usage_request_events":
		return "use"
	case "loadbalance_events":
		return "lb"
	default:
		return "unknown"
	}
}

type task9RuntimeHarness struct {
	service     *runtimeapi.Service
	cache       *runtimeapi.SharedCache
	server      *httptest.Server
	client      *http.Client
	upstreamURL string
	now         time.Time
}

type task9RuntimeRoute struct {
	publicModelID string
	targetModelID string
}

func task9InsertRuntimeActivityAcrossDays(t *testing.T, ctx context.Context, databaseURL string, pool *pgxpool.Pool, retentionStore *logretention.Store, profileID int, baseDay time.Time) {
	t.Helper()
	for offset := range 3 {
		for _, tableName := range []string{"request_logs", "audit_logs", "usage_request_events"} {
			if err := retentionStore.EnsurePartitionForTime(ctx, tableName, baseDay.AddDate(0, 0, offset)); err != nil {
				t.Fatalf("ensure runtime %s partition offset %d: %v", tableName, offset, err)
			}
		}
	}
	harness := task9NewRuntimeHarness(t, ctx, databaseURL, pool, baseDay.Add(6*time.Hour))
	route := task9SeedRuntimeRoute(t, ctx, pool, profileID, harness.upstreamURL)
	if err := harness.cache.RefreshNow(ctx, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}}); err != nil {
		t.Fatalf("refresh task9 runtime snapshot: %v", err)
	}

	for offset := range 3 {
		observedAt := baseDay.AddDate(0, 0, offset).Add(6 * time.Hour)
		harness.now = observedAt
		task9SendRuntimeRequest(t, harness, route.publicModelID, offset)
		task9WaitForRuntimePartitionRows(t, ctx, pool, profileID, observedAt, 1)
	}
}

func task9NewRuntimeHarness(t *testing.T, ctx context.Context, databaseURL string, pool *pgxpool.Pool, initialNow time.Time) *task9RuntimeHarness {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "task9-runtime", "usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 5, "total_tokens": 8}})
	}))
	t.Cleanup(upstream.Close)

	settings := config.Settings{
		Host:                "127.0.0.1",
		Port:                8000,
		AppEnv:              config.EnvironmentProduction,
		DatabaseURL:         databaseURL,
		SecretEncryptionKey: "task9-runtime-secret",
		AuthJWTSecret:       "task9-runtime-jwt-secret",
	}
	cache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
	if err := cache.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap task9 runtime cache: %v", err)
	}
	telemetryPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open task9 runtime telemetry pool: %v", err)
	}
	t.Cleanup(telemetryPool.Close)
	feedbackPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open task9 runtime feedback pool: %v", err)
	}
	t.Cleanup(feedbackPool.Close)
	harness := &task9RuntimeHarness{cache: cache, upstreamURL: upstream.URL, now: initialNow}
	service, err := runtimeapi.NewService(settings, runtimeapi.Options{
		ExecutionPool: pool,
		TelemetryPool: telemetryPool,
		FeedbackPool:  feedbackPool,
		HTTPClient:    upstream.Client(),
		Cache:         cache,
		Now:           func() time.Time { return harness.now },
	})
	if err != nil {
		t.Fatalf("build task9 runtime service: %v", err)
	}
	t.Cleanup(service.Close)
	harness.service = service
	server := httptest.NewServer(middleware.RequestID(service.Handler()))
	t.Cleanup(server.Close)
	harness.server = server
	harness.client = server.Client()
	return harness
}

func task9SeedRuntimeRoute(t *testing.T, ctx context.Context, exec interface {
	task9Exec
	task9QueryRower
}, profileID int, upstreamURL string) task9RuntimeRoute {
	t.Helper()
	suffix := randomSuffix(t)
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	strategyID := task9InsertRuntimeStrategy(t, ctx, exec, profileID, "task9-runtime-strategy-"+suffix, now)
	publicModelID := "task9-runtime-public-" + suffix
	targetModelID := "task9-runtime-target-" + suffix
	targetModelConfigID := task9InsertRuntimeModel(t, ctx, exec, profileID, "openai", targetModelID, "native", &strategyID, now)
	publicModelConfigID := task9InsertRuntimeModel(t, ctx, exec, profileID, "openai", publicModelID, "proxy", &strategyID, now)
	task9InsertRuntimeProxyTarget(t, ctx, exec, publicModelConfigID, targetModelConfigID)
	endpointID := task9InsertRuntimeEndpoint(t, ctx, exec, profileID, "task9-runtime-endpoint-"+suffix, upstreamURL, "task9-runtime-key", now)
	task9InsertRuntimeConnection(t, ctx, exec, profileID, targetModelConfigID, endpointID, "task9-runtime-connection-"+suffix, now)
	return task9RuntimeRoute{publicModelID: publicModelID, targetModelID: targetModelID}
}

func task9InsertRuntimeStrategy(t *testing.T, ctx context.Context, exec task9QueryRower, profileID int, name string, now time.Time) int {
	t.Helper()
	var strategyID int
	if err := exec.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'round-robin', ARRAY[403,422,429,500,502,503,504,529], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $3, $3) RETURNING id`, profileID, name, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert task9 runtime strategy: %v", err)
	}
	return strategyID
}

func task9InsertRuntimeModel(t *testing.T, ctx context.Context, exec task9QueryRower, profileID int, apiFamily string, modelID string, _ string, strategyID *int, now time.Time) int {
	t.Helper()
	var modelConfigID int
	if err := exec.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, $2::varchar(50), $3, NULL, $4, CASE WHEN $2::varchar(50) = 'openai' THEN 'dual_native'::text ELSE NULL::text END, TRUE, $5, $5) RETURNING id`, profileID, apiFamily, modelID, nullableTask9Int(strategyID), now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert task9 runtime model %s: %v", modelID, err)
	}
	return modelConfigID
}

func task9InsertRuntimeProxyTarget(t *testing.T, ctx context.Context, exec task9Exec, publicModelConfigID int, targetModelConfigID int) {
	t.Helper()
	var profileID int
	if err := exec.QueryRow(ctx, `SELECT profile_id FROM model_configs WHERE id = $1`, publicModelConfigID).Scan(&profileID); err != nil {
		t.Fatalf("load task9 runtime source model profile: %v", err)
	}
	if _, err := exec.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, 0, TRUE, now(), now())`, profileID, publicModelConfigID, targetModelConfigID); err != nil {
		t.Fatalf("insert task9 runtime model target: %v", err)
	}
}

func task9InsertRuntimeEndpoint(t *testing.T, ctx context.Context, exec task9QueryRower, profileID int, name string, baseURL string, apiKey string, now time.Time) int {
	t.Helper()
	var endpointID int
	if err := exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, 0, $5, $5) RETURNING id`, profileID, name, baseURL, apiKey, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert task9 runtime endpoint: %v", err)
	}
	return endpointID
}

func task9InsertRuntimeConnection(t *testing.T, ctx context.Context, exec task9QueryRower, profileID int, modelConfigID int, endpointID int, name string, now time.Time) int {
	t.Helper()
	var apiFamily string
	if err := exec.QueryRow(ctx, `SELECT api_family FROM model_configs WHERE id = $1`, modelConfigID).Scan(&apiFamily); err != nil {
		t.Fatalf("load task9 runtime model api family: %v", err)
	}
	var connectionID int
	if err := exec.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $6, TRUE, 0, $4, NULL, NULL, 'healthy', NULL, NULL, $5, $5) RETURNING id`, profileID, apiFamily, endpointID, name, now, openAITextCapabilityForTask9APIFamily(apiFamily)).Scan(&connectionID); err != nil {
		t.Fatalf("insert task9 runtime connection: %v", err)
	}
	if err := exec.QueryRow(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4) RETURNING id`, profileID, modelConfigID, connectionID, now).Scan(new(int)); err != nil {
		t.Fatalf("insert task9 runtime connection target: %v", err)
	}
	return connectionID
}

func task9SendRuntimeRequest(t *testing.T, harness *task9RuntimeHarness, publicModelID string, offset int) {
	t.Helper()
	body := map[string]any{"model": publicModelID, "messages": []map[string]any{{"role": "user", "content": fmt.Sprintf("task9 runtime day %d", offset)}}}
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal task9 runtime request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, harness.server.URL+"/v1/chat/completions", bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("build task9 runtime request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := harness.client.Do(request)
	if err != nil {
		t.Fatalf("send task9 runtime request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		t.Fatalf("expected task9 runtime status 200, got %d with body %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
}

func task9WaitForRuntimePartitionRows(t *testing.T, ctx context.Context, queryer task9QueryRower, profileID int, day time.Time, expectedPerTable int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if task9RuntimePartitionCountsMatch(ctx, queryer, profileID, day, expectedPerTable) {
			return
		}
		if time.Now().After(deadline) {
			requestCount := task9CountProfileRowsInPartition(t, ctx, queryer, "request_logs", day, profileID)
			auditCount := task9CountProfileRowsInPartition(t, ctx, queryer, "audit_logs", day, profileID)
			usageCount := task9CountProfileRowsInPartition(t, ctx, queryer, "usage_request_events", day, profileID)
			t.Fatalf("timed out waiting for runtime rows on %s: request_logs=%d audit_logs=%d usage_request_events=%d want=%d", day.Format("2006-01-02"), requestCount, auditCount, usageCount, expectedPerTable)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func task9RuntimePartitionCountsMatch(ctx context.Context, queryer task9QueryRower, profileID int, day time.Time, expected int) bool {
	return task9CountProfileRowsInPartitionNoFatal(ctx, queryer, "request_logs", day, profileID) == expected &&
		task9CountProfileRowsInPartitionNoFatal(ctx, queryer, "usage_request_events", day, profileID) == expected
}

func task9CountProfileRowsInPartition(t *testing.T, ctx context.Context, queryer task9QueryRower, tableName string, day time.Time, profileID int) int {
	t.Helper()
	count, err := task9CountProfileRowsInPartitionRaw(ctx, queryer, tableName, day, profileID)
	if err != nil {
		t.Fatalf("count runtime %s rows in %s for profile %d: %v", tableName, task9PartitionName(tableName, day), profileID, err)
	}
	return count
}

func task9CountProfileRowsInPartitionNoFatal(ctx context.Context, queryer task9QueryRower, tableName string, day time.Time, profileID int) int {
	count, err := task9CountProfileRowsInPartitionRaw(ctx, queryer, tableName, day, profileID)
	if err != nil {
		return -1
	}
	return count
}

func task9CountProfileRowsInPartitionRaw(ctx context.Context, queryer task9QueryRower, tableName string, day time.Time, profileID int) (int, error) {
	partitionName := task9PartitionName(tableName, day)
	query := fmt.Sprintf(`SELECT count(*) FROM public.%s WHERE profile_id = $1`, quoteIdentifier(partitionName))
	var count int
	if err := queryer.QueryRow(ctx, query, profileID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func task9ExpectedSeededRows(tableName string, directRows int) int {
	return task9ExpectedRuntimeRows(tableName) + directRows
}

func task9ExpectedRuntimeRows(tableName string) int {
	switch tableName {
	case "request_logs", "usage_request_events":
		return 1
	default:
		return 0
	}
}

func nullableTask9Int(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func openAITextCapabilityForTask9APIFamily(apiFamily string) any {
	if apiFamily != "openai" {
		return nil
	}
	return "chat_completions_only"
}

func task9LoadActiveProfileID(t *testing.T, ctx context.Context, queryer task9QueryRower) int {
	t.Helper()
	var profileID int
	if err := queryer.QueryRow(ctx, `SELECT id FROM profiles WHERE is_active = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load task9 active profile id: %v", err)
	}
	return profileID
}

func task9BoundaryRowsOlderThanCutoff(tableName string) int {
	return task9ExpectedRuntimeRows(tableName) + 1
}

package integration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

func TestLogRetentionStoreEnsurePartitionHorizon(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "logretention_store_horizon"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}

	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	fixedNow := time.Date(2026, 5, 9, 23, 30, 0, 0, time.FixedZone("late-west", -7*60*60))
	store := logretention.NewStore(logretention.Options{Pool: pool, Now: func() time.Time { return fixedNow }})
	if err := store.EnsurePartitionHorizon(ctx); err != nil {
		t.Fatalf("ensure partition horizon: %v", err)
	}

	expectedStart := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	for _, tableName := range logretention.ManagedTables() {
		partitions, err := store.ListPartitions(ctx, tableName)
		if err != nil {
			t.Fatalf("list partitions for %s: %v", tableName, err)
		}
		if len(partitions) != logretention.HorizonDays() {
			t.Fatalf("expected %d partitions for %s, got %d", logretention.HorizonDays(), tableName, len(partitions))
		}
		for index, partition := range partitions {
			expectedDay := expectedStart.AddDate(0, 0, index)
			expectedName := fmt.Sprintf("%s_p%s", tableName, expectedDay.Format("20060102"))
			if partition.PartitionName != expectedName {
				t.Fatalf("unexpected partition at %s index %d: %s", tableName, index, partition.PartitionName)
			}
			if !partition.Start.Equal(expectedDay) || !partition.End.Equal(expectedDay.AddDate(0, 0, 1)) {
				t.Fatalf("unexpected bounds for %s: [%s, %s)", partition.PartitionName, partition.Start, partition.End)
			}
		}
	}

	assertIntegrationNoDefaultPartitions(t, ctx, pool)
	assertIntegrationChildReloptions(t, ctx, pool, fmt.Sprintf("request_logs_p%s", expectedStart.Format("20060102")))
	if err := store.EnsurePartitionForTime(ctx, "request_logs", expectedStart.Add(6*time.Hour)); err != nil {
		t.Fatalf("ensure existing partition is idempotent: %v", err)
	}
	partitions, err := store.ListPartitions(ctx, "request_logs")
	if err != nil {
		t.Fatalf("list request log partitions after idempotent ensure: %v", err)
	}
	if len(partitions) != logretention.HorizonDays() {
		t.Fatalf("expected idempotent ensure to keep %d request log partitions, got %d", logretention.HorizonDays(), len(partitions))
	}
}

func TestLogRetentionStoreDropExpiredPartitionsAndBoundaryDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "logretention_store_cutoff"
	conn := harness.openDatabase(t, ctx, databaseName)
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("close setup conn: %v", err)
	}

	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	store := logretention.NewStore(logretention.Options{Pool: pool})
	dayOne := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	for offset := range 3 {
		if err := store.EnsurePartitionForTime(ctx, "audit_logs", dayOne.AddDate(0, 0, offset)); err != nil {
			t.Fatalf("ensure audit partition %d: %v", offset, err)
		}
		if err := store.EnsurePartitionForTime(ctx, "request_logs", dayOne.AddDate(0, 0, offset)); err != nil {
			t.Fatalf("ensure request partition %d: %v", offset, err)
		}
	}

	dropped, err := store.DropExpiredPartitions(ctx, "audit_logs", dayOne.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("drop expired partitions: %v", err)
	}
	if len(dropped) != 1 || dropped[0].PartitionName != "audit_logs_p20260110" {
		t.Fatalf("expected only first audit partition to drop, got %+v", dropped)
	}
	remaining, err := store.ListPartitions(ctx, "audit_logs")
	if err != nil {
		t.Fatalf("list audit partitions: %v", err)
	}
	if len(remaining) != 2 || remaining[0].PartitionName != "audit_logs_p20260111" {
		t.Fatalf("expected cutoff to keep boundary and future partitions, got %+v", remaining)
	}

	profileID := task9InsertProfile(t, ctx, pool)
	task9InsertManagedLogRow(t, ctx, pool, "request_logs", profileID, "boundary-old", 0, dayOne.Add(2*time.Hour))
	task9InsertManagedLogRow(t, ctx, pool, "request_logs", profileID, "boundary-delete", 1, dayOne.AddDate(0, 0, 1).Add(2*time.Hour))
	task9InsertManagedLogRow(t, ctx, pool, "request_logs", profileID, "boundary-keep", 2, dayOne.AddDate(0, 0, 1).Add(18*time.Hour))

	deleted, err := store.DeleteBoundaryRows(ctx, "request_logs", dayOne.AddDate(0, 0, 1).Add(12*time.Hour))
	if err != nil {
		t.Fatalf("delete boundary rows: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one boundary row deleted, got %d", deleted)
	}
	assertIntegrationRequestPathCount(t, ctx, pool, "/v1/task9/boundary-old", 1)
	assertIntegrationRequestPathCount(t, ctx, pool, "/v1/task9/boundary-delete", 0)
	assertIntegrationRequestPathCount(t, ctx, pool, "/v1/task9/boundary-keep", 1)
	if err := store.VacuumAnalyzePartition(ctx, "request_logs", "request_logs_p20260111"); err != nil {
		t.Fatalf("vacuum analyze child partition: %v", err)
	}
}

func assertIntegrationNoDefaultPartitions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_inherits inheritance
		JOIN pg_class child ON child.oid = inheritance.inhrelid
		WHERE pg_get_expr(child.relpartbound, child.oid) = 'DEFAULT'`).Scan(&count); err != nil {
		t.Fatalf("count default partitions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no DEFAULT partitions, got %d", count)
	}
}

func assertIntegrationChildReloptions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, partitionName string) {
	t.Helper()
	var reloptions string
	var toastOID int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(array_to_string(c.reloptions, ','), ''), c.reltoastrelid::oid::int8
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = $1`, partitionName).Scan(&reloptions, &toastOID); err != nil {
		t.Fatalf("load child reloptions for %s: %v", partitionName, err)
	}
	assertIntegrationReloptionsContain(t, partitionName, reloptions, []string{
		"autovacuum_vacuum_scale_factor=0.02",
		"autovacuum_vacuum_threshold=10000",
	})
	if toastOID == 0 {
		t.Fatalf("expected %s to have a TOAST relation", partitionName)
	}
	var toastReloptions string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(array_to_string(reloptions, ','), '') FROM pg_class WHERE oid = $1::oid`, toastOID).Scan(&toastReloptions); err != nil {
		t.Fatalf("load toast reloptions for %s: %v", partitionName, err)
	}
	assertIntegrationReloptionsContain(t, partitionName+" toast", toastReloptions, []string{
		"autovacuum_vacuum_scale_factor=0.02",
		"autovacuum_vacuum_threshold=10000",
	})
}

func assertIntegrationReloptionsContain(t *testing.T, relationName string, reloptions string, expectedOptions []string) {
	t.Helper()
	for _, option := range expectedOptions {
		if !strings.Contains(reloptions, option) {
			t.Fatalf("expected %s reloptions to contain %q, got %q", relationName, option, reloptions)
		}
	}
}

func assertIntegrationRequestPathCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestPath string, expected int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM request_logs WHERE request_path = $1`, requestPath).Scan(&count); err != nil {
		t.Fatalf("count request path %s: %v", requestPath, err)
	}
	if count != expected {
		t.Fatalf("expected %s count %d, got %d", requestPath, expected, count)
	}
}

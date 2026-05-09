package logretention

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

func TestPartitionNameGeneration(t *testing.T) {
	localZone := time.FixedZone("test", -7*60*60)
	input := time.Date(2026, 5, 8, 18, 30, 0, 0, localZone)
	if got := partitionNameForDay("request_logs", input); got != "request_logs_p20260509" {
		t.Fatalf("expected UTC partition name request_logs_p20260509, got %s", got)
	}

	partition, err := partitionFromName("request_logs", "request_logs_p20260509")
	if err != nil {
		t.Fatalf("parse partition name: %v", err)
	}
	expectedStart := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	if !partition.Start.Equal(expectedStart) || !partition.End.Equal(expectedStart.AddDate(0, 0, 1)) {
		t.Fatalf("expected half-open UTC bounds [%s, %s), got [%s, %s)", expectedStart, expectedStart.AddDate(0, 0, 1), partition.Start, partition.End)
	}
}

func TestRejectUnknownManagedTable(t *testing.T) {
	ctx := context.Background()
	store := &Store{}
	unknownNames := []string{"", "unknown_logs", `request_logs; DROP TABLE request_logs; --`}
	for _, tableName := range unknownNames {
		assertUnknownManagedTable(t, store.EnsurePartitionForTime(ctx, tableName, time.Now()))
		_, err := store.ListPartitions(ctx, tableName)
		assertUnknownManagedTable(t, err)
		_, err = store.DropExpiredPartitions(ctx, tableName, time.Now())
		assertUnknownManagedTable(t, err)
		_, err = store.DeleteBoundaryRows(ctx, tableName, time.Now())
		assertUnknownManagedTable(t, err)
		assertUnknownManagedTable(t, store.VacuumAnalyzePartition(ctx, tableName, "request_logs_p20260509"))
	}
}

func TestEnsurePartitionHorizon(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newMigratedPool(t, ctx, "horizon")
	defer pool.Close()

	fixedNow := time.Date(2026, 5, 9, 23, 30, 0, 0, time.FixedZone("late-west", -7*60*60))
	store := NewStore(Options{Pool: pool, Now: func() time.Time { return fixedNow }})
	if err := store.EnsurePartitionHorizon(ctx); err != nil {
		t.Fatalf("ensure partition horizon: %v", err)
	}

	expectedStart := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	for _, tableName := range ManagedTables() {
		partitions, err := store.ListPartitions(ctx, tableName)
		if err != nil {
			t.Fatalf("list partitions for %s: %v", tableName, err)
		}
		if len(partitions) != horizonDays {
			t.Fatalf("expected %d partitions for %s, got %d", horizonDays, tableName, len(partitions))
		}
		for index, partition := range partitions {
			expectedDay := expectedStart.AddDate(0, 0, index)
			if partition.PartitionName != partitionNameForDay(tableName, expectedDay) {
				t.Fatalf("unexpected partition at %s index %d: %s", tableName, index, partition.PartitionName)
			}
			if !partition.Start.Equal(expectedDay) || !partition.End.Equal(expectedDay.AddDate(0, 0, 1)) {
				t.Fatalf("unexpected bounds for %s: [%s, %s)", partition.PartitionName, partition.Start, partition.End)
			}
		}
	}
	assertNoDefaultPartitions(t, ctx, pool)
	assertChildReloptions(t, ctx, pool, partitionNameForDay("request_logs", expectedStart))
	if err := store.EnsurePartitionForTime(ctx, "request_logs", expectedStart.Add(6*time.Hour)); err != nil {
		t.Fatalf("ensure existing partition is idempotent: %v", err)
	}
	partitions, err := store.ListPartitions(ctx, "request_logs")
	if err != nil {
		t.Fatalf("list request log partitions after idempotent ensure: %v", err)
	}
	if len(partitions) != horizonDays {
		t.Fatalf("expected idempotent ensure to keep %d request log partitions, got %d", horizonDays, len(partitions))
	}
}

func TestDropExpiredPartitionsAndBoundaryDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newMigratedPool(t, ctx, "cutoff")
	defer pool.Close()
	store := NewStore(Options{Pool: pool})
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

	insertRequestLog(t, ctx, pool, dayOne.Add(2*time.Hour), "/old-partition")
	insertRequestLog(t, ctx, pool, dayOne.AddDate(0, 0, 1).Add(2*time.Hour), "/boundary-delete")
	insertRequestLog(t, ctx, pool, dayOne.AddDate(0, 0, 1).Add(18*time.Hour), "/boundary-keep")
	deleted, err := store.DeleteBoundaryRows(ctx, "request_logs", dayOne.AddDate(0, 0, 1).Add(12*time.Hour))
	if err != nil {
		t.Fatalf("delete boundary rows: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one boundary row deleted, got %d", deleted)
	}
	assertRequestPathCount(t, ctx, pool, "/old-partition", 1)
	assertRequestPathCount(t, ctx, pool, "/boundary-delete", 0)
	assertRequestPathCount(t, ctx, pool, "/boundary-keep", 1)
	if err := store.VacuumAnalyzePartition(ctx, "request_logs", "request_logs_p20260111"); err != nil {
		t.Fatalf("vacuum analyze child partition: %v", err)
	}
}

func assertUnknownManagedTable(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrUnknownManagedTable) {
		t.Fatalf("expected ErrUnknownManagedTable, got %v", err)
	}
}

func newMigratedPool(t *testing.T, ctx context.Context, databaseName string) *pgxpool.Pool {
	t.Helper()
	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, ctx, databaseName)
	defer func() { _ = conn.Close(ctx) }()
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("connect pgx pool: %v", err)
	}
	return pool
}

func assertNoDefaultPartitions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
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

func assertChildReloptions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, partitionName string) {
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
	assertReloptionsContain(t, partitionName, reloptions, []string{
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
	assertReloptionsContain(t, partitionName+" toast", toastReloptions, []string{
		"autovacuum_vacuum_scale_factor=0.02",
		"autovacuum_vacuum_threshold=10000",
	})
}

func assertReloptionsContain(t *testing.T, relationName string, reloptions string, expectedOptions []string) {
	t.Helper()
	for _, option := range expectedOptions {
		if !strings.Contains(reloptions, option) {
			t.Fatalf("expected %s reloptions to contain %q, got %q", relationName, option, reloptions)
		}
	}
}

func insertRequestLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, createdAt time.Time, requestPath string) {
	t.Helper()
	ensureTestProfile(t, ctx, pool)
	_, err := pool.Exec(ctx, `
		INSERT INTO request_logs (profile_id, model_id, api_family, status_code, response_time_ms, is_stream, request_path, created_at)
		VALUES (1, 'model', 'openai', 200, 10, false, $1, $2)`, requestPath, createdAt)
	if err != nil {
		t.Fatalf("insert request log %s: %v", requestPath, err)
	}
}

func ensureTestProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO profiles (id, name, description, is_active, is_default, is_editable, version, created_at, updated_at)
		VALUES (1, 'Default', NULL, true, true, true, 1, now(), now())
		ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		t.Fatalf("ensure test profile: %v", err)
	}
}

func assertRequestPathCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestPath string, expected int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM request_logs WHERE request_path = $1`, requestPath).Scan(&count); err != nil {
		t.Fatalf("count request path %s: %v", requestPath, err)
	}
	if count != expected {
		t.Fatalf("expected %s count %d, got %d", requestPath, expected, count)
	}
}

type postgresHarness struct {
	containerName string
	hostPort      string
}

func newPostgresHarness(t *testing.T) postgresHarness {
	t.Helper()
	containerName := "prism-logretention-" + randomSuffix(t)
	runDockerCommand(t, context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine")
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupContext, "docker", "rm", "-f", containerName).Run()
	})
	hostPort := dockerPort(t, containerName)
	waitForPostgres(t, hostPort)
	return postgresHarness{containerName: containerName, hostPort: hostPort}
}

func (h postgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := connect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return connect(t, ctx, h.connectionString(databaseName))
}

func (h postgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func connect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func dockerPort(t *testing.T, containerName string) string {
	t.Helper()
	output := runDockerCommand(t, context.Background(), "port", containerName, "5432/tcp")
	firstLine := strings.TrimSpace(strings.Split(output, "\n")[0])
	_, port, err := net.SplitHostPort(firstLine)
	if err != nil {
		t.Fatalf("parse docker port output %q: %v", firstLine, err)
	}
	return port
}

func waitForPostgres(t *testing.T, hostPort string) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", hostPort))
		if err == nil {
			_ = conn.Close(ctx)
			cancel()
			return
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres container on port %s did not become ready in time", hostPort)
}

func runDockerCommand(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s failed: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(buffer)
}

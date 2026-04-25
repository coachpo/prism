package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

func TestBaselineFreshApply(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "fresh_apply")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run baseline on empty database: %v", err)
	}

	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected empty database to apply baseline, got %q", result.Outcome)
	}
	if got, want := result.Versions, []string{migrate.DefaultBaselineVersion, "000002_request_logs_audit_enabled_at_request_not_null", "000003_runtime_telemetry_outbox"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("expected applied versions %v, got %v", want, got)
	}

	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion, "000002_request_logs_audit_enabled_at_request_not_null", "000003_runtime_telemetry_outbox"})
	assertRequestLogAuditEnabledColumnContract(t, testContext, conn)
}

func TestBaselineExistingDatabaseWithoutHistoryFails(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "existing_without_history")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := conn.Exec(testContext, `CREATE TABLE unmanaged_table (id BIGSERIAL PRIMARY KEY)`); err != nil {
		t.Fatalf("seed unmanaged table: %v", err)
	}

	result, err := runner.Run(testContext, conn)
	if err == nil {
		t.Fatalf("expected unmanaged database without migration history to fail, got %+v", result)
	}
	if !strings.Contains(err.Error(), "prism_schema_migrations is missing") {
		t.Fatalf("expected missing history error, got %v", err)
	}

	assertHistoryTableMissing(t, testContext, conn)
}

func TestBaselineSecondRunNoop(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "baseline_second_run_noop")
	defer func() { _ = conn.Close(testContext) }()

	firstResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run baseline before noop check: %v", err)
	}
	if firstResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected first run to apply baseline, got %q", firstResult.Outcome)
	}

	secondResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("rerun baseline after apply: %v", err)
	}
	if secondResult.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected second run to noop, got %q", secondResult.Outcome)
	}
	if len(secondResult.Versions) != 0 {
		t.Fatalf("expected noop run to report no versions, got %v", secondResult.Versions)
	}

	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion, "000002_request_logs_audit_enabled_at_request_not_null", "000003_runtime_telemetry_outbox"})
}

func TestRequestLogAuditEnabledAtRequestMigrationBackfillsAndEnforcesNotNull(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "request_logs_audit_enabled_backfill")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := conn.Exec(testContext, `CREATE TABLE prism_schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		t.Fatalf("create prism migration history table: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO prism_schema_migrations (version, applied_at) VALUES ($1, NOW())`, migrate.DefaultBaselineVersion); err != nil {
		t.Fatalf("seed prism baseline history: %v", err)
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE request_logs (id BIGSERIAL PRIMARY KEY, audit_enabled_at_request boolean)`); err != nil {
		t.Fatalf("create legacy request_logs table: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO request_logs (audit_enabled_at_request) VALUES (NULL), (TRUE), (FALSE)`); err != nil {
		t.Fatalf("seed legacy request_logs rows: %v", err)
	}

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run request-log audit snapshot migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected legacy request_logs database to apply migration, got %q", result.Outcome)
	}
	if got, want := result.Versions, []string{"000002_request_logs_audit_enabled_at_request_not_null", "000003_runtime_telemetry_outbox"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected applied versions %v, got %v", want, got)
	}

	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion, "000002_request_logs_audit_enabled_at_request_not_null", "000003_runtime_telemetry_outbox"})
	assertRequestLogAuditEnabledRows(t, testContext, conn, []bool{false, true, false})
	assertRequestLogAuditEnabledColumnContract(t, testContext, conn)
}

type postgresHarness struct {
	containerName string
	hostPort      string
}

func newPostgresHarness(t *testing.T) postgresHarness {
	t.Helper()

	containerName := "prism-s3-" + randomSuffix(t)
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

func newRunner(t *testing.T) migrate.Runner {
	t.Helper()

	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}

	return runner
}

func assertHistoryVersions(t *testing.T, ctx context.Context, conn *pgx.Conn, expected []string) {
	t.Helper()

	rows, err := conn.Query(ctx, `SELECT version FROM prism_schema_migrations ORDER BY version ASC`)
	if err != nil {
		t.Fatalf("query prism migration history: %v", err)
	}
	defer rows.Close()

	versions := []string{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("scan prism migration history: %v", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate prism migration history: %v", err)
	}

	if len(versions) != len(expected) {
		t.Fatalf("expected prism migration history %v, got %v", expected, versions)
	}
	for index := range expected {
		if versions[index] != expected[index] {
			t.Fatalf("expected prism migration history %v, got %v", expected, versions)
		}
	}
}

func assertHistoryTableMissing(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	var exists bool
	if err := conn.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM pg_tables
			WHERE schemaname = 'public' AND tablename = 'prism_schema_migrations'
		)`,
	).Scan(&exists); err != nil {
		t.Fatalf("check prism migration history table existence: %v", err)
	}
	if exists {
		t.Fatalf("expected prism migration history table to remain absent")
	}
}

func assertRequestLogAuditEnabledRows(t *testing.T, ctx context.Context, conn *pgx.Conn, expected []bool) {
	t.Helper()

	rows, err := conn.Query(ctx, `SELECT audit_enabled_at_request FROM request_logs ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query request_logs audit_enabled_at_request rows: %v", err)
	}
	defer rows.Close()

	values := make([]bool, 0, len(expected))
	for rows.Next() {
		var value bool
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("scan request_logs audit_enabled_at_request row: %v", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate request_logs audit_enabled_at_request rows: %v", err)
	}
	if len(values) != len(expected) {
		t.Fatalf("expected request_logs audit_enabled_at_request rows %v, got %v", expected, values)
	}
	for index := range expected {
		if values[index] != expected[index] {
			t.Fatalf("expected request_logs audit_enabled_at_request rows %v, got %v", expected, values)
		}
	}
}

func assertRequestLogAuditEnabledColumnContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	var isNullable string
	var columnDefault string
	if err := conn.QueryRow(
		ctx,
		`SELECT is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'request_logs' AND column_name = 'audit_enabled_at_request'`,
	).Scan(&isNullable, &columnDefault); err != nil {
		t.Fatalf("load request_logs audit_enabled_at_request column contract: %v", err)
	}
	if isNullable != "NO" {
		t.Fatalf("expected request_logs.audit_enabled_at_request to be NOT NULL, got is_nullable=%q", isNullable)
	}
	if !strings.Contains(strings.ToLower(columnDefault), "false") {
		t.Fatalf("expected request_logs.audit_enabled_at_request default to contain false, got %q", columnDefault)
	}
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

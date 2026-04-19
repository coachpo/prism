package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

const legacyAlembicTip = "0026_request_log_audit_enabled_at_request"

func TestBaselineFreshApply(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "fresh_apply")
	defer conn.Close(testContext)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run baseline on empty database: %v", err)
	}

	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected empty database to apply baseline, got %q", result.Outcome)
	}
	if got, want := result.Versions, []string{migrate.DefaultBaselineVersion}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected applied versions %v, got %v", want, got)
	}

	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion})
	assertCutoverMatch(t, testContext, runner, conn)
}

func TestBaselineExistingStamp(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "existing_stamp")
	defer conn.Close(testContext)

	applySQLFile(t, testContext, conn, cutoverSchemaPath(t))
	seedLegacyAlembicVersion(t, testContext, conn)

	before := snapshotApplicationSchema(t, testContext, runner, conn)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("stamp matching schema: %v", err)
	}

	if result.Outcome != migrate.OutcomeStamp {
		t.Fatalf("expected existing matching schema to stamp baseline, got %q", result.Outcome)
	}
	if got, want := result.Versions, []string{migrate.DefaultBaselineVersion}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected stamped versions %v, got %v", want, got)
	}

	after := snapshotApplicationSchema(t, testContext, runner, conn)
	if before != after {
		t.Fatalf("expected stamp path to leave application schema unchanged")
	}

	assertHistoryVersions(t, testContext, conn, []string{migrate.DefaultBaselineVersion})
	assertLegacyAlembicVersion(t, testContext, conn, legacyAlembicTip)
}

func TestBaselineSchemaEquivalence(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)

	baselineConn := harness.openDatabase(t, testContext, "schema_equivalence_baseline")
	defer baselineConn.Close(testContext)
	result, err := runner.Run(testContext, baselineConn)
	if err != nil {
		t.Fatalf("run baseline for schema equivalence: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected baseline database to apply migration, got %q", result.Outcome)
	}

	cutoverConn := harness.openDatabase(t, testContext, "schema_equivalence_cutover")
	defer cutoverConn.Close(testContext)
	applySQLFile(t, testContext, cutoverConn, cutoverSchemaPath(t))

	baselineSnapshot := snapshotApplicationSchema(t, testContext, runner, baselineConn)
	cutoverSnapshot := snapshotApplicationSchema(t, testContext, runner, cutoverConn)
	expected := readNormalizedFile(t, cutoverSchemaPath(t))

	if baselineSnapshot != expected {
		t.Fatalf("expected baseline output to match cutover artifact")
	}
	if cutoverSnapshot != expected {
		t.Fatalf("expected direct cutover schema to match cutover artifact")
	}
	if baselineSnapshot != cutoverSnapshot {
		t.Fatalf("expected baseline output and cutover artifact application to be equivalent")
	}
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
	defer adminConn.Close(ctx)

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

func snapshotApplicationSchema(t *testing.T, ctx context.Context, runner migrate.Runner, conn *pgx.Conn) string {
	t.Helper()

	snapshot, err := runner.SnapshotApplicationSchema(ctx, conn)
	if err != nil {
		t.Fatalf("snapshot application schema: %v", err)
	}

	return snapshot
}

func assertCutoverMatch(t *testing.T, ctx context.Context, runner migrate.Runner, conn *pgx.Conn) {
	t.Helper()

	match, actual, expected, err := runner.ApplicationSchemaMatchesCutover(ctx, conn)
	if err != nil {
		t.Fatalf("compare application schema to cutover artifact: %v", err)
	}
	if !match {
		t.Fatalf("expected application schema to match cutover artifact\n--- actual ---\n%s\n--- expected ---\n%s", actual, expected)
	}
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

func seedLegacyAlembicVersion(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS alembic_version (version_num VARCHAR(128) NOT NULL PRIMARY KEY)`,
		`INSERT INTO alembic_version (version_num) VALUES ('` + legacyAlembicTip + `')`,
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("seed legacy alembic version table: %v", err)
		}
	}
}

func assertLegacyAlembicVersion(t *testing.T, ctx context.Context, conn *pgx.Conn, expected string) {
	t.Helper()

	var version string
	if err := conn.QueryRow(ctx, `SELECT version_num FROM alembic_version`).Scan(&version); err != nil {
		t.Fatalf("read legacy alembic version row: %v", err)
	}
	if version != expected {
		t.Fatalf("expected legacy alembic version %q, got %q", expected, version)
	}
}

func applySQLFile(t *testing.T, ctx context.Context, conn *pgx.Conn, path string) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SQL file %s: %v", path, err)
	}

	for _, statement := range splitSQLStatements(string(raw)) {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("apply SQL file %s: %v\nstatement:\n%s", path, err, statement)
		}
	}
}

func splitSQLStatements(sql string) []string {
	rawStatements := strings.Split(sql, ";")
	statements := make([]string, 0, len(rawStatements))
	for _, rawStatement := range rawStatements {
		statement := strings.TrimSpace(rawStatement)
		if statement == "" {
			continue
		}
		statements = append(statements, statement)
	}
	return statements
}

func readNormalizedFile(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}

	return migrate.NormalizeSchemaSQL(string(raw))
}

func cutoverSchemaPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "schema", "cutover-live.sql"))
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

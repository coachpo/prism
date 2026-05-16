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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

var expectedPrismMigrationVersions = loadExpectedPrismMigrationVersions()

func loadExpectedPrismMigrationVersions() []string {
	entries, err := os.ReadDir(migrate.DefaultMigrationsDir())
	if err != nil {
		panic(fmt.Sprintf("read migrations directory: %v", err))
	}
	versions := []string{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		versions = append(versions, strings.TrimSuffix(entry.Name(), ".sql"))
	}
	return versions
}

func migrationVersionByOrdinal(t *testing.T, ordinal int) string {
	t.Helper()
	prefix := fmt.Sprintf("%06d_", ordinal)
	for _, version := range expectedPrismMigrationVersions {
		if strings.HasPrefix(version, prefix) {
			return version
		}
	}
	t.Fatalf("unknown migration ordinal %d", ordinal)
	return ""
}

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
	expectedVersions := expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion)
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)
	assertHistoryVersions(t, testContext, conn, expectedVersions)
	assertRequestLogAuditEnabledColumnContract(t, testContext, conn)
	assertRequestLogGenerationParamsColumnContract(t, testContext, conn)
	assertRuntimeCacheGenerationContract(t, testContext, conn)
	assertPartitionedLogSchemaContract(t, testContext, conn)
	assertSidecarSchemaContract(t, testContext, conn)
}

func TestPartitionedLogSchemaContract(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "partitioned_log_schema_contract")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run partitioned log schema migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected partitioned log schema migration to apply, got %q", result.Outcome)
	}

	assertPartitionedLogSchemaContract(t, testContext, conn)
}

func TestSidecarSchemaContract(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "sidecar_schema_contract")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run sidecar schema migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected sidecar schema migration to apply, got %q", result.Outcome)
	}

	expectedVersions := expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion)
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)
	assertHistoryVersions(t, testContext, conn, expectedVersions)
	assertSidecarSchemaContract(t, testContext, conn)
	assertLogRetentionSettingsContract(t, testContext, conn)
}

func TestSidecarHistoricalMigrationOrdinalsReachCurrentSchema(t *testing.T) {
	for _, ordinal := range []int{17, 18, 19, 20, 21, 22, 23} {
		t.Run(fmt.Sprintf("ordinal_%02d", ordinal), func(t *testing.T) {
			testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			harness := newPostgresHarness(t)
			runner := newRunner(t)
			conn := harness.openDatabase(t, testContext, fmt.Sprintf("sidecar_ordinal_%02d", ordinal))
			defer func() { _ = conn.Close(testContext) }()

			throughVersion := migrationVersionByOrdinal(t, ordinal)
			applyMigrationsThrough(t, testContext, conn, throughVersion)

			result, err := runner.Run(testContext, conn)
			if err != nil {
				t.Fatalf("run sidecar migrations after ordinal %02d: %v", ordinal, err)
			}
			if result.Outcome != migrate.OutcomeApply {
				t.Fatalf("expected sidecar migrations after ordinal %02d to apply, got %q", ordinal, result.Outcome)
			}
			assertMigrationVersions(t, "applied versions", result.Versions, expectedMigrationVersionsFrom(t, migrationVersionByOrdinal(t, ordinal+1)))
			assertHistoryVersions(t, testContext, conn, expectedPrismMigrationVersions)
			assertSidecarSchemaContract(t, testContext, conn)
			assertLogRetentionSettingsContract(t, testContext, conn)
		})
	}
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

	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))
}

func TestRuntimeCacheGenerationMigration(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "runtime_cache_generation_migration")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run runtime cache generation migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected runtime cache generation migration to apply, got %q", result.Outcome)
	}
	assertRuntimeCacheGenerationContract(t, testContext, conn)
}

func TestProxyTargetSelectionStrategiesMigrationBackfillsAndEnforcesContracts(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "proxy_target_selection_strategies_migration")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := conn.Exec(testContext, `CREATE TABLE prism_schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		t.Fatalf("create prism migration history table: %v", err)
	}
	for _, version := range expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion)[:11] {
		if _, err := conn.Exec(testContext, `INSERT INTO prism_schema_migrations (version, applied_at) VALUES ($1, NOW())`, version); err != nil {
			t.Fatalf("seed prism migration history %s: %v", version, err)
		}
	}
	createLegacyProxyTargetSelectionTables(t, testContext, conn)
	if _, err := conn.Exec(testContext, `INSERT INTO model_configs (id, model_type, loadbalance_strategy_id) VALUES (1, 'native', 7), (2, 'proxy', NULL)`); err != nil {
		t.Fatalf("seed legacy model_configs rows: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position) VALUES (2, 1, 4)`); err != nil {
		t.Fatalf("seed legacy model_proxy_targets row: %v", err)
	}

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run proxy target selection strategies migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected proxy target selection strategies migration to apply, got %q", result.Outcome)
	}
	expectedVersions := expectedMigrationVersionsFrom(t, "000012_proxy_target_selection_strategies")
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)
	assertProxyTargetSelectionMigration(t, testContext, conn)
}

func TestStreamOutcomeTelemetryMigrationBackfillsAndEnforcesContracts(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "stream_outcome_telemetry_migration")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := conn.Exec(testContext, `CREATE TABLE prism_schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		t.Fatalf("create prism migration history table: %v", err)
	}
	for _, version := range []string{migrate.DefaultBaselineVersion, "000002_request_logs_audit_enabled_at_request_not_null", "000003_runtime_telemetry_outbox", "000004_user_settings_retention_policy", "000005_audit_log_request_time_provenance", "000006_request_generation_params_telemetry", "000007_management_outbox", "000008_email_outbox", "000009_management_audit_stats_phase7", "000010_runtime_cache_generations"} {
		if _, err := conn.Exec(testContext, `INSERT INTO prism_schema_migrations (version, applied_at) VALUES ($1, NOW())`, version); err != nil {
			t.Fatalf("seed prism migration history %s: %v", version, err)
		}
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE request_logs (id BIGSERIAL PRIMARY KEY, profile_id integer NOT NULL, ingress_request_id character varying(36), attempt_number integer, is_stream boolean NOT NULL, completion_duration_ms integer, ttft_ms integer)`); err != nil {
		t.Fatalf("create stream telemetry legacy request_logs table: %v", err)
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE usage_request_events (id BIGSERIAL PRIMARY KEY, profile_id integer NOT NULL, ingress_request_id character varying(36) NOT NULL, attempt_count integer NOT NULL, completion_duration_ms integer)`); err != nil {
		t.Fatalf("create stream telemetry legacy usage_request_events table: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO request_logs (profile_id, ingress_request_id, attempt_number, is_stream, completion_duration_ms, ttft_ms) VALUES (1, 'req-nonstream', 1, FALSE, NULL, NULL), (1, 'req-completed', 1, TRUE, 250, 40), (1, 'req-unknown', 2, TRUE, NULL, 35)`); err != nil {
		t.Fatalf("seed stream telemetry legacy request_logs rows: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO usage_request_events (profile_id, ingress_request_id, attempt_count, completion_duration_ms) VALUES (1, 'req-nonstream', 1, NULL), (1, 'req-completed', 1, NULL), (1, 'req-unknown', 2, NULL), (1, 'req-usage-completed', 1, 90), (1, 'req-usage-unknown', 1, NULL)`); err != nil {
		t.Fatalf("seed stream telemetry legacy usage_request_events rows: %v", err)
	}
	createLegacyProxyTargetSelectionTables(t, testContext, conn)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run stream outcome telemetry migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected stream outcome telemetry migration to apply, got %q", result.Outcome)
	}
	expectedVersions := expectedMigrationVersionsFrom(t, "000011_stream_outcome_telemetry")
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)

	assertStreamOutcomeTelemetryColumnContracts(t, testContext, conn)
	assertCleanBreakLogRows(t, testContext, conn, "request_logs")
	assertCleanBreakLogRows(t, testContext, conn, "usage_request_events")
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
	if _, err := conn.Exec(testContext, `CREATE TABLE request_logs (id BIGSERIAL PRIMARY KEY, profile_id integer NOT NULL, audit_enabled_at_request boolean)`); err != nil {
		t.Fatalf("create legacy request_logs table: %v", err)
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE audit_logs (id BIGSERIAL PRIMARY KEY, profile_id integer NOT NULL, request_log_id bigint, request_body text, response_body text)`); err != nil {
		t.Fatalf("create legacy audit_logs table: %v", err)
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE user_settings (id BIGSERIAL PRIMARY KEY, profile_id integer NOT NULL, report_currency_code character varying(3) NOT NULL, report_currency_symbol character varying(5) NOT NULL, timezone_preference character varying(100), created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`); err != nil {
		t.Fatalf("create legacy user_settings table: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO request_logs (profile_id, audit_enabled_at_request) VALUES (1, NULL), (1, TRUE), (1, FALSE)`); err != nil {
		t.Fatalf("seed legacy request_logs rows: %v", err)
	}
	createLegacyProxyTargetSelectionTables(t, testContext, conn)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run request-log audit snapshot migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected legacy request_logs database to apply migration, got %q", result.Outcome)
	}
	expectedVersions := expectedMigrationVersionsFrom(t, "000002_request_logs_audit_enabled_at_request_not_null")
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)

	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))
	assertCleanBreakLogRows(t, testContext, conn, "request_logs")
	assertRequestLogAuditEnabledColumnContract(t, testContext, conn)
	assertRequestLogGenerationParamsColumnContract(t, testContext, conn)
}

func TestAuditLogRequestTimeProvenanceMigrationBackfillsAndEnforcesNotNull(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "audit_log_request_time_provenance")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := conn.Exec(testContext, `CREATE TABLE prism_schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		t.Fatalf("create prism migration history table: %v", err)
	}
	for _, version := range []string{migrate.DefaultBaselineVersion, "000002_request_logs_audit_enabled_at_request_not_null", "000003_runtime_telemetry_outbox", "000004_user_settings_retention_policy"} {
		if _, err := conn.Exec(testContext, `INSERT INTO prism_schema_migrations (version, applied_at) VALUES ($1, NOW())`, version); err != nil {
			t.Fatalf("seed prism migration history %s: %v", version, err)
		}
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE request_logs (id BIGSERIAL PRIMARY KEY, profile_id integer NOT NULL, audit_enabled_at_request boolean NOT NULL)`); err != nil {
		t.Fatalf("create legacy request_logs table: %v", err)
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE audit_logs (id BIGSERIAL PRIMARY KEY, profile_id integer NOT NULL, request_log_id bigint, request_body text, response_body text)`); err != nil {
		t.Fatalf("create legacy audit_logs table: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO request_logs (id, profile_id, audit_enabled_at_request) VALUES (1, 2, TRUE), (2, 2, FALSE)`); err != nil {
		t.Fatalf("seed legacy request_logs rows: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO audit_logs (id, profile_id, request_log_id, request_body, response_body) VALUES (10, 2, 1, '{"request":true}', '{"response":true}'), (11, 2, 2, NULL, NULL), (12, 2, NULL, '{"orphan":true}', NULL)`); err != nil {
		t.Fatalf("seed legacy audit_logs rows: %v", err)
	}
	createLegacyProxyTargetSelectionTables(t, testContext, conn)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run audit-log provenance migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected legacy audit_logs database to apply migration, got %q", result.Outcome)
	}
	expectedVersions := expectedMigrationVersionsFrom(t, "000005_audit_log_request_time_provenance")
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))

	assertCleanBreakLogRows(t, testContext, conn, "request_logs")

	var requestLogIsNullable string
	var requestLogDefault string
	if err := conn.QueryRow(testContext, `SELECT is_nullable, COALESCE(column_default, '') FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'request_logs' AND column_name = 'audit_capture_bodies_at_request'`).Scan(&requestLogIsNullable, &requestLogDefault); err != nil {
		t.Fatalf("load request_logs audit_capture_bodies_at_request column contract: %v", err)
	}
	if requestLogIsNullable != "NO" || !strings.Contains(strings.ToLower(requestLogDefault), "false") {
		t.Fatalf("expected request_logs.audit_capture_bodies_at_request NOT NULL with false default, got is_nullable=%q default=%q", requestLogIsNullable, requestLogDefault)
	}

	assertCleanBreakLogRows(t, testContext, conn, "audit_logs")
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

func expectedMigrationVersionsFrom(t *testing.T, start string) []string {
	t.Helper()
	for index, version := range expectedPrismMigrationVersions {
		if version == start {
			return append([]string(nil), expectedPrismMigrationVersions[index:]...)
		}
	}
	t.Fatalf("unknown migration version %q", start)
	return nil
}

func assertMigrationVersions(t *testing.T, label string, got []string, expected []string) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("expected %s %v, got %v", label, expected, got)
	}
	for index := range expected {
		if got[index] != expected[index] {
			t.Fatalf("expected %s %v, got %v", label, expected, got)
		}
	}
}

func createLegacyProxyTargetSelectionTables(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `CREATE TABLE model_configs (id BIGSERIAL PRIMARY KEY, model_type character varying(20) NOT NULL, loadbalance_strategy_id integer)`); err != nil {
		t.Fatalf("create legacy model_configs table: %v", err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE model_proxy_targets (id BIGSERIAL PRIMARY KEY, source_model_config_id integer NOT NULL, target_model_config_id integer NOT NULL, position integer NOT NULL)`); err != nil {
		t.Fatalf("create legacy model_proxy_targets table: %v", err)
	}
}

func assertProxyTargetSelectionMigration(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var nativeSelector string
	var proxySelector string
	if err := conn.QueryRow(ctx, `SELECT COALESCE(proxy_selection_strategy, '') FROM model_configs WHERE id = 1`).Scan(&nativeSelector); err != nil {
		t.Fatalf("load migrated native proxy_selection_strategy: %v", err)
	}
	if nativeSelector != "" {
		t.Fatalf("expected native proxy_selection_strategy to stay null, got %q", nativeSelector)
	}
	if err := conn.QueryRow(ctx, `SELECT proxy_selection_strategy FROM model_configs WHERE id = 2`).Scan(&proxySelector); err != nil {
		t.Fatalf("load migrated proxy proxy_selection_strategy: %v", err)
	}
	if proxySelector != "ordered_fallback" {
		t.Fatalf("expected proxy selector ordered_fallback, got %q", proxySelector)
	}

	var weight int
	var targetPriority int
	if err := conn.QueryRow(ctx, `SELECT weight, target_priority FROM model_proxy_targets WHERE source_model_config_id = 2 AND target_model_config_id = 1`).Scan(&weight, &targetPriority); err != nil {
		t.Fatalf("load migrated proxy target metadata: %v", err)
	}
	if weight != 1 || targetPriority != 4 {
		t.Fatalf("expected migrated proxy target weight=1 target_priority=4, got weight=%d target_priority=%d", weight, targetPriority)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO model_configs (model_type, loadbalance_strategy_id, proxy_selection_strategy) VALUES ('proxy', NULL, NULL)`); err == nil {
		t.Fatal("expected proxy model without proxy_selection_strategy to violate migration constraint")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position, weight, target_priority) VALUES (2, 1, 5, 0, 5)`); err == nil {
		t.Fatal("expected proxy target with weight 0 to violate migration constraint")
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

func assertCleanBreakTableRows(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string) {
	t.Helper()
	var count int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM `+quoteIdentifier(tableName)).Scan(&count); err != nil {
		t.Fatalf("count clean-break %s rows: %v", tableName, err)
	}
	if count != 0 {
		t.Fatalf("expected clean-break %s migration to discard legacy rows, got %d", tableName, count)
	}
}

func assertCleanBreakLogRows(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string) {
	assertCleanBreakTableRows(t, ctx, conn, tableName)
}

func assertSidecarSchemaContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	tables := []string{"sidecar_instances", "sidecar_auth_snapshots", "sidecar_provider_snapshots"}
	for _, tableName := range tables {
		var idDefault string
		var createdType string
		var updatedType string
		if err := conn.QueryRow(ctx, `
SELECT COALESCE(id.column_default, ''), created_at.data_type, updated_at.data_type
FROM information_schema.columns id
JOIN information_schema.columns created_at ON created_at.table_schema = id.table_schema AND created_at.table_name = id.table_name AND created_at.column_name = 'created_at'
JOIN information_schema.columns updated_at ON updated_at.table_schema = id.table_schema AND updated_at.table_name = id.table_name AND updated_at.column_name = 'updated_at'
WHERE id.table_schema = 'public' AND id.table_name = $1 AND id.column_name = 'id'`, tableName).Scan(&idDefault, &createdType, &updatedType); err != nil {
			t.Fatalf("load sidecar table %s timestamp/id contract: %v", tableName, err)
		}
		if !strings.Contains(idDefault, "nextval") || createdType != "timestamp with time zone" || updatedType != "timestamp with time zone" {
			t.Fatalf("unexpected sidecar table %s id/timestamp contract: id_default=%q created=%q updated=%q", tableName, idDefault, createdType, updatedType)
		}
	}
	assertColumnDataType(t, ctx, conn, "sidecar_instances", "deleted_at", "timestamp with time zone")
	for tableName, columnNames := range map[string][]string{
		"sidecar_auth_snapshots":     {"recent_requests_json", "model_states_json", "snapshot_json"},
		"sidecar_provider_snapshots": {"snapshot_json"},
	} {
		for _, columnName := range columnNames {
			assertColumnDataType(t, ctx, conn, tableName, columnName, "jsonb")
		}
	}
	assertIndexDefinitionContains(t, ctx, conn, "uq_sidecar_instances_live_name", "lower(name)", "deleted_at IS NULL")
	assertIndexDefinitionContains(t, ctx, conn, "uq_sidecar_instances_live_base_url_canonical", "base_url_canonical", "deleted_at IS NULL")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_instances_management_auth_state", "invalid_management_auth")
	assertCurrentSidecarTables(t, ctx, conn)
}

func assertColumnDataType(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnName string, dataType string) {
	t.Helper()
	var got string
	if err := conn.QueryRow(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, tableName, columnName).Scan(&got); err != nil {
		t.Fatalf("load %s.%s column type: %v", tableName, columnName, err)
	}
	if got != dataType {
		t.Fatalf("expected %s.%s type %q, got %q", tableName, columnName, dataType, got)
	}
}

func assertCurrentSidecarTables(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind IN ('r', 'p')
		  AND left(c.relname, length('sidecar_')) = 'sidecar_'
		ORDER BY c.relname ASC`)
	if err != nil {
		t.Fatalf("load current sidecar tables: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			t.Fatalf("scan current sidecar table: %v", err)
		}
		got[tableName] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate current sidecar tables: %v", err)
	}
	assertStringSet(t, "sidecar tables", got, map[string]bool{"sidecar_auth_snapshots": true, "sidecar_instances": true, "sidecar_provider_snapshots": true})
}

func assertStringSet(t *testing.T, label string, got map[string]bool, expected map[string]bool) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("%s = %v want %v", label, got, expected)
	}
	for value := range expected {
		if !got[value] {
			t.Fatalf("%s missing %s: got %v want %v", label, value, got, expected)
		}
	}
	for value := range got {
		if !expected[value] {
			t.Fatalf("%s has unexpected %s: got %v want %v", label, value, got, expected)
		}
	}
}

func assertIndexDefinitionContains(t *testing.T, ctx context.Context, conn *pgx.Conn, indexName string, fragments ...string) {
	t.Helper()
	var definition string
	if err := conn.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`, indexName).Scan(&definition); err != nil {
		t.Fatalf("load index definition %s: %v", indexName, err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("expected index %s definition %q to contain %q", indexName, definition, fragment)
		}
	}
}

func assertConstraintDefinitionContains(t *testing.T, ctx context.Context, conn *pgx.Conn, constraintName string, fragments ...string) {
	t.Helper()
	var definition string
	if err := conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, constraintName).Scan(&definition); err != nil {
		t.Fatalf("load constraint definition %s: %v", constraintName, err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(definition, fragment) {
			t.Fatalf("expected constraint %s definition %q to contain %q", constraintName, definition, fragment)
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

func assertRequestLogGenerationParamsColumnContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'request_logs' AND column_name = ANY($1::text[]) ORDER BY column_name ASC`, []string{"request_generation_params", "request_generation_params_status"})
	if err != nil {
		t.Fatalf("load request_logs generation params column contract: %v", err)
	}
	defer rows.Close()
	got := map[string][2]string{}
	for rows.Next() {
		var name string
		var dataType string
		var nullable string
		if err := rows.Scan(&name, &dataType, &nullable); err != nil {
			t.Fatalf("scan request_logs generation params column contract: %v", err)
		}
		got[name] = [2]string{dataType, nullable}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate request_logs generation params column contract: %v", err)
	}
	if got["request_generation_params"] != [2]string{"jsonb", "YES"} {
		t.Fatalf("expected request_generation_params nullable jsonb, got %+v", got["request_generation_params"])
	}
	if got["request_generation_params_status"] != [2]string{"character varying", "YES"} {
		t.Fatalf("expected request_generation_params_status nullable varchar, got %+v", got["request_generation_params_status"])
	}
}

func assertRuntimeCacheGenerationContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT domain, scope_type, scope_id, version
		FROM runtime_cache_generations
		ORDER BY domain ASC`)
	if err != nil {
		t.Fatalf("query runtime cache generations: %v", err)
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var domain string
		var scopeType string
		var scopeID string
		var version int64
		if err := rows.Scan(&domain, &scopeType, &scopeID, &version); err != nil {
			t.Fatalf("scan runtime cache generation: %v", err)
		}
		got[domain+":"+scopeType+":"+scopeID] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime cache generations: %v", err)
	}
	for _, key := range []string{"auth:global:*", "model_catalog:global:*", "profile_runtime:global:*", "runtime_planning:global:*"} {
		version, ok := got[key]
		if !ok {
			t.Fatalf("expected runtime cache generation row %q, got %+v", key, got)
		}
		if version != 0 {
			t.Fatalf("expected bootstrap runtime cache generation %q version 0, got %d", key, version)
		}
	}
	var constraintName string
	if err := conn.QueryRow(ctx, `
		SELECT constraint_name
		FROM information_schema.table_constraints
		WHERE table_schema = 'public'
		  AND table_name = 'runtime_cache_generations'
		  AND constraint_type = 'PRIMARY KEY'`).Scan(&constraintName); err != nil {
		t.Fatalf("load runtime cache generations primary key: %v", err)
	}
	if strings.TrimSpace(constraintName) == "" {
		t.Fatal("expected runtime_cache_generations primary key")
	}
}

type streamTelemetryColumnContract struct {
	dataType      string
	maxLength     int
	isNullable    string
	columnDefault string
}

func assertStreamOutcomeTelemetryColumnContracts(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT table_name, column_name, data_type, COALESCE(character_maximum_length, 0), is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = ANY($1::text[])
		  AND column_name = ANY($2::text[])
		ORDER BY table_name ASC, column_name ASC`, []string{"request_logs", "usage_request_events"}, []string{"stream_outcome", "stream_error_kind", "stream_error_detail"})
	if err != nil {
		t.Fatalf("load stream telemetry column contracts: %v", err)
	}
	defer rows.Close()

	contracts := map[string]streamTelemetryColumnContract{}
	for rows.Next() {
		var tableName string
		var columnName string
		var contract streamTelemetryColumnContract
		if err := rows.Scan(&tableName, &columnName, &contract.dataType, &contract.maxLength, &contract.isNullable, &contract.columnDefault); err != nil {
			t.Fatalf("scan stream telemetry column contract: %v", err)
		}
		contracts[tableName+"."+columnName] = contract
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stream telemetry column contracts: %v", err)
	}

	assertStreamTelemetryColumn(t, contracts, "request_logs.stream_outcome", "character varying", 50, "NO", "not_streaming")
	assertStreamTelemetryColumn(t, contracts, "request_logs.stream_error_kind", "character varying", 50, "YES", "")
	assertStreamTelemetryColumn(t, contracts, "request_logs.stream_error_detail", "text", 0, "YES", "")
	assertStreamTelemetryColumn(t, contracts, "usage_request_events.stream_outcome", "character varying", 50, "NO", "not_streaming")
	assertStreamTelemetryColumn(t, contracts, "usage_request_events.stream_error_kind", "character varying", 50, "YES", "")
	if _, exists := contracts["usage_request_events.stream_error_detail"]; exists {
		t.Fatal("expected usage_request_events.stream_error_detail to remain absent")
	}
}

func assertStreamTelemetryColumn(t *testing.T, contracts map[string]streamTelemetryColumnContract, key string, dataType string, maxLength int, isNullable string, defaultContains string) {
	t.Helper()
	contract, ok := contracts[key]
	if !ok {
		t.Fatalf("expected stream telemetry column %s to exist", key)
	}
	if contract.dataType != dataType || contract.maxLength != maxLength || contract.isNullable != isNullable {
		t.Fatalf("expected %s contract type=%q length=%d nullable=%q, got %+v", key, dataType, maxLength, isNullable, contract)
	}
	if defaultContains == "" {
		if contract.columnDefault != "" {
			t.Fatalf("expected %s to have no default, got %q", key, contract.columnDefault)
		}
		return
	}
	if !strings.Contains(strings.ToLower(contract.columnDefault), strings.ToLower(defaultContains)) {
		t.Fatalf("expected %s default to contain %q, got %q", key, defaultContains, contract.columnDefault)
	}
}

func assertPartitionedLogSchemaContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	logTables := []string{"audit_logs", "request_logs", "usage_request_events", "loadbalance_events"}
	assertPartitionedLogRoots(t, ctx, conn, logTables)
	assertPartitionedLogPrimaryKeys(t, ctx, conn, logTables)
	assertPartitionedLogIDDefaults(t, ctx, conn, logTables)
	assertPartitionedLogRootIDIndexes(t, ctx, conn, logTables)
	assertPartitionedLogLookupIndexes(t, ctx, conn)
	assertPartitionedLogStorageParameterLimitation(t, ctx, conn, logTables)
	assertAuditLogWeakRequestLinkContract(t, ctx, conn)
	assertLogRetentionSettingsContract(t, ctx, conn)
	assertNoLogChildPartitions(t, ctx, conn)
}

func assertPartitionedLogRoots(t *testing.T, ctx context.Context, conn *pgx.Conn, logTables []string) {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT c.relname, c.relkind::text
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = ANY($1::text[])
		ORDER BY c.relname ASC`, logTables)
	if err != nil {
		t.Fatalf("load partitioned log roots: %v", err)
	}
	defer rows.Close()

	kinds := map[string]string{}
	for rows.Next() {
		var name string
		var relkind string
		if err := rows.Scan(&name, &relkind); err != nil {
			t.Fatalf("scan partitioned log root: %v", err)
		}
		kinds[name] = relkind
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate partitioned log roots: %v", err)
	}
	for _, tableName := range logTables {
		if kinds[tableName] != "p" {
			t.Fatalf("expected %s to have relkind p, got %q in %+v", tableName, kinds[tableName], kinds)
		}
	}
}

func assertPartitionedLogPrimaryKeys(t *testing.T, ctx context.Context, conn *pgx.Conn, logTables []string) {
	t.Helper()
	for _, tableName := range logTables {
		var columns string
		if err := conn.QueryRow(ctx, `
			SELECT string_agg(att.attname, ',' ORDER BY keys.key_order)
			FROM pg_constraint con
			JOIN unnest(con.conkey) WITH ORDINALITY AS keys(attnum, key_order) ON true
			JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = keys.attnum
			WHERE con.conrelid = $1::regclass AND con.contype = 'p'`, "public."+tableName).Scan(&columns); err != nil {
			t.Fatalf("load %s primary key columns: %v", tableName, err)
		}
		if columns != "created_at,id" {
			t.Fatalf("expected %s primary key on created_at,id, got %q", tableName, columns)
		}
	}
}

func assertPartitionedLogIDDefaults(t *testing.T, ctx context.Context, conn *pgx.Conn, logTables []string) {
	t.Helper()
	for _, tableName := range logTables {
		var dataType string
		var isNullable string
		var columnDefault string
		if err := conn.QueryRow(ctx, `
			SELECT data_type, is_nullable, COALESCE(column_default, '')
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = 'id'`, tableName).Scan(&dataType, &isNullable, &columnDefault); err != nil {
			t.Fatalf("load %s id column contract: %v", tableName, err)
		}
		if dataType != "bigint" || isNullable != "NO" {
			t.Fatalf("expected %s.id bigint not null, got type=%q nullable=%q", tableName, dataType, isNullable)
		}
		if !strings.Contains(columnDefault, "nextval") || !strings.Contains(columnDefault, tableName+"_id_seq") {
			t.Fatalf("expected %s.id sequence default, got %q", tableName, columnDefault)
		}
	}
}

func assertPartitionedLogRootIDIndexes(t *testing.T, ctx context.Context, conn *pgx.Conn, logTables []string) {
	t.Helper()
	for _, tableName := range logTables {
		assertIndexUniqueness(t, ctx, conn, tableName, "ix_"+tableName+"_id", false)
	}
}

func assertPartitionedLogLookupIndexes(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	expectedIndexes := map[string]string{
		"audit_logs":           "idx_audit_logs_profile_request_created_id_desc",
		"request_logs":         "idx_request_logs_profile_created_at",
		"usage_request_events": "idx_usage_request_events_profile_ingress_request",
		"loadbalance_events":   "idx_loadbalance_events_profile_created",
	}
	for tableName, indexName := range expectedIndexes {
		assertIndexExists(t, ctx, conn, tableName, indexName)
	}
	assertIndexUniqueness(t, ctx, conn, "audit_logs", "ix_audit_logs_request_log_id", false)
}

type partitionedLogStorageState struct {
	reloptions  string
	toastRelOID int64
}

func assertPartitionedLogStorageParameterLimitation(t *testing.T, ctx context.Context, conn *pgx.Conn, logTables []string) {
	t.Helper()

	// PostgreSQL 16 proof: .sisyphus/evidence/task-1-partitioned-root-reloptions-proof.txt.
	// Partitioned parents reject heap autovacuum reloptions and expose no TOAST
	// relation. If a server ever exposes either surface, require the planned
	// reloptions here instead of letting a missing storage contract pass silently.
	states := loadPartitionedLogStorageStates(t, ctx, conn, logTables)
	for _, tableName := range logTables {
		state := states[tableName]
		if state.reloptions != "" {
			assertReloptionsContain(t, tableName, state.reloptions, []string{
				"autovacuum_vacuum_scale_factor=0.02",
				"autovacuum_vacuum_threshold=10000",
			})
			continue
		}

		assertPartitionedRootHeapReloptionsRejected(t, ctx, conn, tableName)
	}

	for _, tableName := range logTables {
		state := states[tableName]
		if state.toastRelOID == 0 {
			continue
		}
		assertToastReloptions(t, ctx, conn, tableName, state.toastRelOID)
	}
}

func loadPartitionedLogStorageStates(t *testing.T, ctx context.Context, conn *pgx.Conn, logTables []string) map[string]partitionedLogStorageState {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT c.relname, COALESCE(array_to_string(c.reloptions, ','), ''), c.reltoastrelid::oid::int8
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = ANY($1::text[])`, logTables)
	if err != nil {
		t.Fatalf("load partitioned log storage states: %v", err)
	}
	defer rows.Close()

	states := map[string]partitionedLogStorageState{}
	for rows.Next() {
		var tableName string
		var toastRelOID int64
		var state partitionedLogStorageState
		if err := rows.Scan(&tableName, &state.reloptions, &toastRelOID); err != nil {
			t.Fatalf("scan partitioned log storage state: %v", err)
		}
		state.toastRelOID = toastRelOID
		states[tableName] = state
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate partitioned log storage states: %v", err)
	}
	for _, tableName := range logTables {
		if _, ok := states[tableName]; !ok {
			t.Fatalf("expected storage state for %s", tableName)
		}
	}
	return states
}

func assertPartitionedRootHeapReloptionsRejected(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string) {
	t.Helper()
	_, err := conn.Exec(ctx, `ALTER TABLE public.`+quoteIdentifier(tableName)+` SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 10000)`)
	if err == nil {
		t.Fatalf("expected PostgreSQL 16 to reject heap autovacuum reloptions on partitioned root %s", tableName)
	}
	if !strings.Contains(err.Error(), "cannot specify storage parameters for a partitioned table") {
		t.Fatalf("expected partitioned root storage-parameter rejection for %s, got %v", tableName, err)
	}
}

func assertToastReloptions(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, toastRelOID int64) {
	t.Helper()
	var toastReloptions string
	if err := conn.QueryRow(ctx, `SELECT COALESCE(array_to_string(reloptions, ','), '') FROM pg_class WHERE oid = $1::oid`, toastRelOID).Scan(&toastReloptions); err != nil {
		t.Fatalf("load %s toast reloptions: %v", tableName, err)
	}
	assertReloptionsContain(t, tableName+" toast", toastReloptions, []string{
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

func assertIndexExists(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, indexName string) {
	t.Helper()
	assertIndexPresence(t, ctx, conn, tableName, indexName, true)
}

func assertIndexPresence(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, indexName string, wantExists bool) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_index idx
			JOIN pg_class index_class ON index_class.oid = idx.indexrelid
			JOIN pg_class table_class ON table_class.oid = idx.indrelid
			JOIN pg_namespace n ON n.oid = table_class.relnamespace
			WHERE n.nspname = 'public' AND table_class.relname = $1 AND index_class.relname = $2
		)`, tableName, indexName).Scan(&exists); err != nil {
		t.Fatalf("check index %s on %s: %v", indexName, tableName, err)
	}
	if exists != wantExists {
		t.Fatalf("expected index %s on %s exists=%v, got %v", indexName, tableName, wantExists, exists)
	}
}

func assertIndexUniqueness(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, indexName string, wantUnique bool) {
	t.Helper()
	var isUnique bool
	if err := conn.QueryRow(ctx, `
		SELECT idx.indisunique
		FROM pg_index idx
		JOIN pg_class index_class ON index_class.oid = idx.indexrelid
		JOIN pg_class table_class ON table_class.oid = idx.indrelid
		JOIN pg_namespace n ON n.oid = table_class.relnamespace
		WHERE n.nspname = 'public' AND table_class.relname = $1 AND index_class.relname = $2`, tableName, indexName).Scan(&isUnique); err != nil {
		t.Fatalf("load index %s on %s uniqueness: %v", indexName, tableName, err)
	}
	if isUnique != wantUnique {
		t.Fatalf("expected index %s on %s unique=%v, got %v", indexName, tableName, wantUnique, isUnique)
	}
}

type partitionedLogColumnContract struct {
	dataType  string
	maxLength int
	nullable  string
}

func assertAuditLogWeakRequestLinkContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT column_name, data_type, COALESCE(character_maximum_length, 0), is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'audit_logs'
		  AND column_name = ANY($1::text[])`, []string{"request_log_id", "request_log_created_at", "ingress_request_id"})
	if err != nil {
		t.Fatalf("load audit weak request-link columns: %v", err)
	}
	defer rows.Close()

	columns := map[string]partitionedLogColumnContract{}
	for rows.Next() {
		var name string
		var contract partitionedLogColumnContract
		if err := rows.Scan(&name, &contract.dataType, &contract.maxLength, &contract.nullable); err != nil {
			t.Fatalf("scan audit weak request-link column: %v", err)
		}
		columns[name] = contract
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit weak request-link columns: %v", err)
	}

	assertColumnContract(t, columns, "request_log_id", "bigint", 0, "YES")
	assertColumnContract(t, columns, "request_log_created_at", "timestamp with time zone", 0, "YES")
	assertColumnContract(t, columns, "ingress_request_id", "character varying", 36, "YES")

	var hasHardFK bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = 'public.audit_logs'::regclass
			  AND conname = 'audit_logs_request_log_id_fkey'
		)`).Scan(&hasHardFK); err != nil {
		t.Fatalf("check audit_logs request_log_id fkey absence: %v", err)
	}
	if hasHardFK {
		t.Fatal("expected audit_logs_request_log_id_fkey to be absent")
	}
}

func assertColumnContract(t *testing.T, columns map[string]partitionedLogColumnContract, columnName string, dataType string, maxLength int, nullable string) {
	t.Helper()
	contract, ok := columns[columnName]
	if !ok {
		t.Fatalf("expected column %s to exist", columnName)
	}
	if contract.dataType != dataType || contract.maxLength != maxLength || contract.nullable != nullable {
		t.Fatalf("expected column %s type=%q length=%d nullable=%q, got %+v", columnName, dataType, maxLength, nullable, contract)
	}
}

func assertLogRetentionSettingsContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	dayColumns := []string{"request_logs_retention_days", "audit_logs_retention_days", "statistics_retention_days", "loadbalance_events_retention_days"}
	rows, err := conn.Query(ctx, `
		SELECT column_name, data_type, COALESCE(character_maximum_length, 0), is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'log_retention_settings'
		  AND right(column_name, length('_retention_days')) = '_retention_days'`)
	if err != nil {
		t.Fatalf("load log_retention_settings day columns: %v", err)
	}
	defer rows.Close()

	columns := map[string]partitionedLogColumnContract{}
	for rows.Next() {
		var name string
		var contract partitionedLogColumnContract
		if err := rows.Scan(&name, &contract.dataType, &contract.maxLength, &contract.nullable); err != nil {
			t.Fatalf("scan log_retention_settings day column: %v", err)
		}
		columns[name] = contract
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate log_retention_settings day columns: %v", err)
	}
	if len(columns) != len(dayColumns) {
		t.Fatalf("log_retention_settings day columns = %v want %v", columns, dayColumns)
	}
	for _, columnName := range dayColumns {
		assertColumnContract(t, columns, columnName, "integer", 0, "YES")
	}

	var singletonRows int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM public.log_retention_settings WHERE singleton_key = 'global'`).Scan(&singletonRows); err != nil {
		t.Fatalf("count global log_retention_settings row: %v", err)
	}
	if singletonRows != 1 {
		t.Fatalf("expected one global log_retention_settings row, got %d", singletonRows)
	}

	var constraintCount int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid = 'public.log_retention_settings'::regclass
		  AND contype = 'c'`).Scan(&constraintCount); err != nil {
		t.Fatalf("count log_retention_settings check constraints: %v", err)
	}
	if constraintCount != 5 {
		t.Fatalf("expected five log_retention_settings check constraints, got %d", constraintCount)
	}
	assertConstraintDefinitionContains(t, ctx, conn, "log_retention_settings_singleton_key_check", "'global'")
}

func applyMigrationsThrough(t *testing.T, ctx context.Context, conn *pgx.Conn, throughVersion string) {
	t.Helper()
	subsetDir := t.TempDir()
	entries, err := os.ReadDir(migrate.DefaultMigrationsDir())
	if err != nil {
		t.Fatalf("read migrations directory for subset: %v", err)
	}
	copied := false
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), ".sql")
		if version > throughVersion {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(migrate.DefaultMigrationsDir(), entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s for subset: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(subsetDir, entry.Name()), raw, 0o600); err != nil {
			t.Fatalf("write migration %s to subset: %v", entry.Name(), err)
		}
		if version == throughVersion {
			copied = true
		}
	}
	if !copied {
		t.Fatalf("migration subset did not include requested version %s", throughVersion)
	}

	runner, err := migrate.New(migrate.Options{MigrationsDir: subsetDir})
	if err != nil {
		t.Fatalf("build subset migration runner: %v", err)
	}
	result, err := runner.Run(ctx, conn)
	if err != nil {
		t.Fatalf("apply migrations through %s: %v", throughVersion, err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected subset migrations through %s to apply, got %q", throughVersion, result.Outcome)
	}
	assertMigrationVersions(t, "applied subset versions", result.Versions, expectedMigrationVersionsThrough(t, throughVersion))
}

func expectedMigrationVersionsThrough(t *testing.T, throughVersion string) []string {
	t.Helper()
	versions := []string{}
	for _, version := range expectedPrismMigrationVersions {
		versions = append(versions, version)
		if version == throughVersion {
			return versions
		}
	}
	t.Fatalf("unknown migration version %q", throughVersion)
	return nil
}

func assertNoLogChildPartitions(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var childCount int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_inherits
		WHERE inhparent IN (
			'public.audit_logs'::regclass,
			'public.request_logs'::regclass,
			'public.usage_request_events'::regclass,
			'public.loadbalance_events'::regclass
		)`).Scan(&childCount); err != nil {
		t.Fatalf("count log child partitions: %v", err)
	}
	if childCount != 0 {
		t.Fatalf("expected migration to create partitioned roots only, got %d child partitions", childCount)
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

func TestSidecarCurrentSchemaConstraints(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "sidecar_current_constraints")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run sidecar current schema migrations: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected sidecar current schema migrations to apply, got %q", result.Outcome)
	}

	assertSidecarSchemaContract(t, testContext, conn)
}

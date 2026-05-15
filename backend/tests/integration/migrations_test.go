package integration_test

import (
	"context"
	"crypto/rand"
	"database/sql"
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

var expectedPrismMigrationVersions = []string{
	migrate.DefaultBaselineVersion,
	"000002_request_logs_audit_enabled_at_request_not_null",
	"000003_runtime_telemetry_outbox",
	"000004_user_settings_retention_policy",
	"000005_audit_log_request_time_provenance",
	"000006_request_generation_params_telemetry",
	"000007_management_outbox",
	"000008_email_outbox",
	"000009_management_audit_stats_phase7",
	"000010_runtime_cache_generations",
	"000011_stream_outcome_telemetry",
	"000012_proxy_target_selection_strategies",
	"000013_partitioned_log_retention",
	"000014_cli_proxy_sidecars",
	"000015_sidecar_watchdog_probe_first_quota",
	"000016_sidecar_watchdog_action_auth_name",
	"000017_sidecar_watchdog_action_history_retention_split",
	"000018_sidecar_quota_inventory_and_cooldown",
	"000019_sidecar_watchdog_probe_concurrency",
	"000020_sidecar_watchdog_policy_revisions_and_sweeps",
	"000021_sidecar_watchdog_four_band_priorities",
	"000022_sidecar_watchdog_sweep_items",
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

func TestSidecarActionHistoryMigrationDiscardsLegacyRowsAndEnforcesContracts(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "sidecar_action_history_migration")
	defer func() { _ = conn.Close(testContext) }()

	seedLegacySidecarActionHistoryMigrationFixture(t, testContext, conn)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run sidecar action history migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected sidecar action history migration to apply, got %q", result.Outcome)
	}

	expectedVersions := expectedMigrationVersionsFrom(t, "000017_sidecar_watchdog_action_history_retention_split")
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)
	assertHistoryVersions(t, testContext, conn, expectedPrismMigrationVersions)
	assertSidecarSchemaContract(t, testContext, conn)
	assertLogRetentionSettingsContract(t, testContext, conn)
	assertCleanBreakTableRows(t, testContext, conn, "sidecar_watchdog_actions")
	assertCleanBreakTableRows(t, testContext, conn, "sidecar_watchdog_pending_actions")

	var holdCount int
	if err := conn.QueryRow(testContext, `SELECT count(*) FROM sidecar_watchdog_holds`).Scan(&holdCount); err != nil {
		t.Fatalf("count surviving sidecar hold rows: %v", err)
	}
	if holdCount != 1 {
		t.Fatalf("expected legacy hold rows to survive the split, got %d", holdCount)
	}
}

func TestSidecarQuotaInventoryMigrationRenamesProbeEvidenceAndDropsInternalCursor(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "sidecar_quota_inventory_migration")
	defer func() { _ = conn.Close(testContext) }()

	applyMigrationsThrough(t, testContext, conn, "000017_sidecar_watchdog_action_history_retention_split")
	var sidecarID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password)
VALUES ($1, $2, $3, $4)
RETURNING id`, "quota-migration-sidecar", "https://quota-migration.example.test", "https://quota-migration.example.test", "enc:quota-migration").Scan(&sidecarID); err != nil {
		t.Fatalf("seed sidecar before quota inventory migration: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_watchdog_policies (sidecar_id, enabled, probe_cursor_auth_id) VALUES ($1, true, 'hidden-pre-000018-cursor')`, sidecarID); err != nil {
		t.Fatalf("seed policy cursor before quota inventory migration: %v", err)
	}
	var legacyObservationID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_watchdog_probe_observations (
sidecar_id, auth_id, auth_index, provider, probed_at, probe_status, upstream_status_code,
quota_exceeded, quota_reason, windows_json, error_code)
VALUES ($1, 'auth-legacy-quota', 'idx-legacy-quota', 'codex', NOW(), 'probe_succeeded', 200,
false, 'healthy', '[]'::jsonb, NULL)
RETURNING id`, sidecarID).Scan(&legacyObservationID); err != nil {
		t.Fatalf("seed legacy probe observation before quota inventory migration: %v", err)
	}

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run quota inventory migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected quota inventory migration to apply, got %q", result.Outcome)
	}
	expectedVersions := expectedMigrationVersionsFrom(t, "000018_sidecar_quota_inventory_and_cooldown")
	assertMigrationVersions(t, "applied versions", result.Versions, expectedVersions)
	assertHistoryVersions(t, testContext, conn, expectedPrismMigrationVersions)
	assertSidecarSchemaContract(t, testContext, conn)

	var authID string
	var authIndex string
	if err := conn.QueryRow(testContext, `SELECT auth_id, auth_index FROM sidecar_quota_probe_observations WHERE id = $1`, legacyObservationID).Scan(&authID, &authIndex); err != nil {
		t.Fatalf("load renamed probe observation: %v", err)
	}
	if authID != "auth-legacy-quota" || authIndex != "idx-legacy-quota" {
		t.Fatalf("renamed probe observation did not preserve sanitized evidence: auth=%q index=%q", authID, authIndex)
	}
	var cooldownSeconds int
	var quotaInventoryEnabled bool
	var initialScanEnabled bool
	var rollingRefreshEnabled bool
	if err := conn.QueryRow(testContext, `SELECT probe_batch_cooldown_seconds, quota_inventory_enabled, initial_scan_enabled, rolling_refresh_enabled FROM sidecar_watchdog_policies WHERE sidecar_id = $1`, sidecarID).Scan(&cooldownSeconds, &quotaInventoryEnabled, &initialScanEnabled, &rollingRefreshEnabled); err != nil {
		t.Fatalf("load migrated quota policy fields: %v", err)
	}
	if cooldownSeconds != 30 || !quotaInventoryEnabled || !initialScanEnabled || !rollingRefreshEnabled {
		t.Fatalf("unexpected migrated quota policy defaults: cooldown=%d inventory=%v initial=%v rolling=%v", cooldownSeconds, quotaInventoryEnabled, initialScanEnabled, rollingRefreshEnabled)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_auth_quota_states (sidecar_id, auth_id, auth_index, auth_name, provider, quota_band, reason_code, last_observation_id)
VALUES ($1, 'auth-legacy-quota', 'idx-legacy-quota', 'legacy-quota.json', 'codex', 'error', 'healthy', $2)`, sidecarID, legacyObservationID); err != nil {
		t.Fatalf("insert quota state referencing renamed observation: %v", err)
	}
	if _, err := conn.Exec(testContext, `DELETE FROM sidecar_quota_probe_observations WHERE id = $1`, legacyObservationID); err != nil {
		t.Fatalf("delete renamed observation to exercise quota-state privacy FK: %v", err)
	}
	var observationCleared bool
	if err := conn.QueryRow(testContext, `SELECT last_observation_id IS NULL FROM sidecar_auth_quota_states WHERE sidecar_id = $1 AND auth_id = 'auth-legacy-quota'`, sidecarID).Scan(&observationCleared); err != nil {
		t.Fatalf("load quota-state observation nullability after delete: %v", err)
	}
	if !observationCleared {
		t.Fatalf("expected quota state observation reference to clear on renamed observation delete")
	}
}

func TestSidecarWatchdogProbeConcurrencyMigrationBackfillsBatchSizeAndDropsOldColumn(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "sidecar_probe_concurrency_migration")
	defer func() { _ = conn.Close(testContext) }()

	applyMigrationsThrough(t, testContext, conn, "000018_sidecar_quota_inventory_and_cooldown")
	var sidecarID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password)
VALUES ($1, $2, $3, $4)
RETURNING id`, "probe-concurrency-migration-sidecar", "https://probe-concurrency-migration.example.test", "https://probe-concurrency-migration.example.test", "enc:probe-concurrency").Scan(&sidecarID); err != nil {
		t.Fatalf("seed sidecar before probe concurrency migration: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_watchdog_policies (sidecar_id, enabled, probe_batch_size, probe_timeout_seconds) VALUES ($1, true, 7, 1)`, sidecarID); err != nil {
		t.Fatalf("seed policy batch size before probe concurrency migration: %v", err)
	}

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run probe concurrency migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected probe concurrency migration to apply, got %q", result.Outcome)
	}
	assertMigrationVersions(t, "applied versions", result.Versions, expectedMigrationVersionsFrom(t, "000019_sidecar_watchdog_probe_concurrency"))
	assertHistoryVersions(t, testContext, conn, expectedPrismMigrationVersions)

	var probeConcurrency int
	if err := conn.QueryRow(testContext, `SELECT probe_concurrency FROM sidecar_watchdog_policies WHERE sidecar_id = $1`, sidecarID).Scan(&probeConcurrency); err != nil {
		t.Fatalf("load migrated probe concurrency: %v", err)
	}
	if probeConcurrency != 7 {
		t.Fatalf("expected probe_concurrency to preserve legacy probe_batch_size 7, got %d", probeConcurrency)
	}
	assertColumnDataType(t, testContext, conn, "sidecar_watchdog_policies", "probe_concurrency", "integer")
	assertColumnMissing(t, testContext, conn, "sidecar_watchdog_policies", "probe_batch_size")
	assertConstraintDefinitionContains(t, testContext, conn, "ck_sidecar_watchdog_policies_thresholds", "probe_concurrency >= 1", "probe_concurrency <= 8", "probe_timeout_seconds <= 25")
	assertConstraintDefinitionExcludes(t, testContext, conn, "ck_sidecar_watchdog_policies_thresholds", "probe_batch_size", "*")
	assertColumnDataType(t, testContext, conn, "sidecar_watchdog_policies", "active_revision_id", "bigint")
	assertColumnDataType(t, testContext, conn, "sidecar_watchdog_policies", "pending_revision_id", "bigint")
	var activeRevisionID int64
	if err := conn.QueryRow(testContext, `SELECT active_revision_id FROM sidecar_watchdog_policies WHERE sidecar_id = $1`, sidecarID).Scan(&activeRevisionID); err != nil {
		t.Fatalf("load active watchdog policy revision after migration: %v", err)
	}
	var revisionConcurrency int
	if err := conn.QueryRow(testContext, `SELECT probe_concurrency FROM sidecar_watchdog_policy_revisions WHERE id = $1`, activeRevisionID).Scan(&revisionConcurrency); err != nil {
		t.Fatalf("load backfilled watchdog policy revision after migration: %v", err)
	}
	if revisionConcurrency != 7 {
		t.Fatalf("expected backfilled revision probe_concurrency 7, got %d", revisionConcurrency)
	}
}

func TestSidecarWatchdogFourBandPriorityMigrationBackfillsLegacyZeroes(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "sidecar_four_band_priority_migration")
	defer func() { _ = conn.Close(testContext) }()

	applyMigrationsThrough(t, testContext, conn, "000020_sidecar_watchdog_policy_revisions_and_sweeps")
	var sidecarID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password)
VALUES ($1, $2, $3, $4)
RETURNING id`, "four-band-migration-sidecar", "https://four-band-migration.example.test", "https://four-band-migration.example.test", "enc:four-band").Scan(&sidecarID); err != nil {
		t.Fatalf("seed sidecar before four-band migration: %v", err)
	}
	var policyID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_watchdog_policies (sidecar_id, enabled, using_priority, quota_exceeded_priority, error_priority, probe_concurrency, probe_timeout_seconds)
VALUES ($1, true, 1, 0, 0, 3, 8)
RETURNING id`, sidecarID).Scan(&policyID); err != nil {
		t.Fatalf("seed legacy watchdog policy before four-band migration: %v", err)
	}
	var revisionID int64
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_watchdog_policy_revisions (policy_id, sidecar_id, enabled, using_priority, quota_exceeded_priority, error_priority, probe_concurrency, probe_timeout_seconds)
VALUES ($1, $2, true, 1, 0, 0, 3, 8)
RETURNING id`, policyID, sidecarID).Scan(&revisionID); err != nil {
		t.Fatalf("seed legacy watchdog revision before four-band migration: %v", err)
	}
	if _, err := conn.Exec(testContext, `UPDATE sidecar_watchdog_policies SET active_revision_id=$1 WHERE id=$2`, revisionID, policyID); err != nil {
		t.Fatalf("point policy at legacy revision before four-band migration: %v", err)
	}

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run four-band priority migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected four-band priority migration to apply, got %q", result.Outcome)
	}
	assertMigrationVersions(t, "applied versions", result.Versions, expectedMigrationVersionsFrom(t, "000021_sidecar_watchdog_four_band_priorities"))
	assertHistoryVersions(t, testContext, conn, expectedPrismMigrationVersions)

	assertFourBandPriorityRow(t, testContext, conn, "sidecar_watchdog_policies", "id", policyID)
	assertFourBandPriorityRow(t, testContext, conn, "sidecar_watchdog_policy_revisions", "id", revisionID)
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_watchdog_sweeps (sweep_id, sidecar_id, policy_revision_id, status, snapshot_json, started_at) VALUES ('sweep-active-a', $1, $2, 'running', '[]'::jsonb, now())`, sidecarID, revisionID); err != nil {
		t.Fatalf("seed active watchdog sweep after migration: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_watchdog_sweeps (sweep_id, sidecar_id, policy_revision_id, status, snapshot_json, started_at) VALUES ('sweep-active-b', $1, $2, 'paused', '[]'::jsonb, now())`, sidecarID, revisionID); err == nil {
		t.Fatalf("expected active sweep uniqueness to reject overlapping running/paused sweeps")
	}
	completedAt := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_watchdog_sweeps (sweep_id, sidecar_id, policy_revision_id, status, snapshot_json, started_at, completed_at) VALUES ('sweep-completed', $1, $2, 'completed', '[]'::jsonb, $3, $3)`, sidecarID, revisionID, completedAt); err != nil {
		t.Fatalf("terminal sweep history should remain insertable after active uniqueness check: %v", err)
	}
}

func TestSidecarWatchdogRuntimeMigrationDisposesLegacyActiveState(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "sidecar_watchdog_runtime_migration")
	defer func() { _ = conn.Close(testContext) }()

	applyMigrationsThrough(t, testContext, conn, "000021_sidecar_watchdog_four_band_priorities")
	var sidecarID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password)
VALUES ($1, $2, $3, $4)
RETURNING id`, "runtime-migration-sidecar", "https://runtime-migration.example.test", "https://runtime-migration.example.test", "enc:runtime-migration").Scan(&sidecarID); err != nil {
		t.Fatalf("seed sidecar before runtime migration: %v", err)
	}
	var policyID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_watchdog_policies (sidecar_id, enabled) VALUES ($1, true) RETURNING id`, sidecarID).Scan(&policyID); err != nil {
		t.Fatalf("seed watchdog policy before runtime migration: %v", err)
	}
	var revisionID int64
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_watchdog_policy_revisions (policy_id, sidecar_id, enabled) VALUES ($1, $2, true) RETURNING id`, policyID, sidecarID).Scan(&revisionID); err != nil {
		t.Fatalf("seed watchdog revision before runtime migration: %v", err)
	}
	if _, err := conn.Exec(testContext, `UPDATE sidecar_watchdog_policies SET active_revision_id=$1 WHERE id=$2`, revisionID, policyID); err != nil {
		t.Fatalf("link active revision before runtime migration: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_watchdog_sweeps (sweep_id, sidecar_id, policy_revision_id, status, snapshot_json, lease_expires_at) VALUES ('legacy-active-sweep', $1, $2, 'running', '[{"auth_id":"legacy"}]'::jsonb, now() + interval '5 minutes')`, sidecarID, revisionID); err != nil {
		t.Fatalf("seed legacy active sweep before runtime migration: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status, planned_count) VALUES ($1, 'manual', 'queued', 3)`, sidecarID); err != nil {
		t.Fatalf("seed legacy active scan before runtime migration: %v", err)
	}

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run watchdog runtime migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected watchdog runtime migration to apply, got %q", result.Outcome)
	}
	assertMigrationVersions(t, "applied versions", result.Versions, expectedMigrationVersionsFrom(t, "000022_sidecar_watchdog_sweep_items"))
	assertHistoryVersions(t, testContext, conn, expectedPrismMigrationVersions)
	assertSidecarSchemaContract(t, testContext, conn)

	var sweepStatus string
	var sweepCompletedAt, sweepCancelRequestedAt sql.NullTime
	var sweepLeaseExpiresAt sql.NullTime
	var sweepCancelReason string
	if err := conn.QueryRow(testContext, `SELECT status, completed_at, cancel_requested_at, lease_expires_at, cancel_reason FROM sidecar_watchdog_sweeps WHERE sweep_id='legacy-active-sweep'`).Scan(&sweepStatus, &sweepCompletedAt, &sweepCancelRequestedAt, &sweepLeaseExpiresAt, &sweepCancelReason); err != nil {
		t.Fatalf("load migrated legacy sweep: %v", err)
	}
	if sweepStatus != "cancelled" || !sweepCompletedAt.Valid || !sweepCancelRequestedAt.Valid || sweepLeaseExpiresAt.Valid || sweepCancelReason != "legacy_runtime_discarded" {
		t.Fatalf("legacy active sweep was not deterministically cancelled: status=%s completed=%v cancel=%v lease=%v reason=%q", sweepStatus, sweepCompletedAt.Valid, sweepCancelRequestedAt.Valid, sweepLeaseExpiresAt.Valid, sweepCancelReason)
	}
	var scanStatus string
	var scanCompletedAt, scanCancelRequestedAt sql.NullTime
	var scanErrorCode string
	if err := conn.QueryRow(testContext, `SELECT status, completed_at, cancel_requested_at, last_error_code FROM sidecar_quota_scan_runs WHERE sidecar_id=$1`, sidecarID).Scan(&scanStatus, &scanCompletedAt, &scanCancelRequestedAt, &scanErrorCode); err != nil {
		t.Fatalf("load migrated legacy scan run: %v", err)
	}
	if scanStatus != "cancelled" || !scanCompletedAt.Valid || !scanCancelRequestedAt.Valid || scanErrorCode != "legacy_runtime_discarded" {
		t.Fatalf("legacy active scan run was not deterministically cancelled: status=%s completed=%v cancel=%v error=%q", scanStatus, scanCompletedAt.Valid, scanCancelRequestedAt.Valid, scanErrorCode)
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
	tables := []string{"sidecar_instances", "sidecar_auth_snapshots", "sidecar_provider_snapshots", "sidecar_watchdog_policies", "sidecar_watchdog_holds", "sidecar_watchdog_actions", "sidecar_watchdog_pending_actions", "sidecar_quota_scan_runs", "sidecar_watchdog_sweep_items"}
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
	for _, column := range []struct {
		tableName  string
		columnName string
		dataType   string
	}{
		{"sidecar_watchdog_actions", "auth_snapshot_id", "integer"},
		{"sidecar_watchdog_actions", "hold_id", "integer"},
		{"sidecar_watchdog_actions", "auth_id", "text"},
		{"sidecar_watchdog_actions", "auth_name", "text"},
		{"sidecar_watchdog_actions", "auth_index", "text"},
		{"sidecar_watchdog_actions", "provider", "text"},
		{"sidecar_watchdog_actions", "action_type", "text"},
		{"sidecar_watchdog_actions", "reason", "text"},
		{"sidecar_watchdog_actions", "previous_priority", "integer"},
		{"sidecar_watchdog_actions", "target_priority", "integer"},
		{"sidecar_watchdog_actions", "hold_until", "timestamp with time zone"},
		{"sidecar_watchdog_actions", "status", "text"},
		{"sidecar_watchdog_actions", "error_message", "text"},
		{"sidecar_watchdog_actions", "completed_at", "timestamp with time zone"},
		{"sidecar_quota_probe_observations", "id", "integer"},
		{"sidecar_quota_probe_observations", "sidecar_id", "integer"},
		{"sidecar_quota_probe_observations", "auth_id", "text"},
		{"sidecar_quota_probe_observations", "auth_index", "text"},
		{"sidecar_quota_probe_observations", "provider", "text"},
		{"sidecar_quota_probe_observations", "probed_at", "timestamp with time zone"},
		{"sidecar_quota_probe_observations", "probe_status", "text"},
		{"sidecar_quota_probe_observations", "upstream_status_code", "integer"},
		{"sidecar_quota_probe_observations", "quota_exceeded", "boolean"},
		{"sidecar_quota_probe_observations", "reason_code", "text"},
		{"sidecar_quota_probe_observations", "quota_reset_at", "timestamp with time zone"},
		{"sidecar_quota_probe_observations", "blocking_window", "text"},
		{"sidecar_quota_probe_observations", "windows_json", "jsonb"},
		{"sidecar_quota_probe_observations", "error_code", "text"},
		{"sidecar_quota_probe_observations", "created_at", "timestamp with time zone"},
		{"sidecar_watchdog_pending_actions", "id", "integer"},
		{"sidecar_watchdog_pending_actions", "sidecar_id", "integer"},
		{"sidecar_watchdog_pending_actions", "hold_id", "integer"},
		{"sidecar_watchdog_pending_actions", "action_history_created_at", "timestamp with time zone"},
		{"sidecar_watchdog_pending_actions", "action_history_id", "integer"},
		{"sidecar_watchdog_pending_actions", "auth_id", "text"},
		{"sidecar_watchdog_pending_actions", "auth_name", "text"},
		{"sidecar_watchdog_pending_actions", "auth_index", "text"},
		{"sidecar_watchdog_pending_actions", "provider", "text"},
		{"sidecar_watchdog_pending_actions", "action_type", "text"},
		{"sidecar_watchdog_pending_actions", "reason", "text"},
		{"sidecar_watchdog_pending_actions", "previous_priority", "integer"},
		{"sidecar_watchdog_pending_actions", "target_priority", "integer"},
		{"sidecar_watchdog_pending_actions", "hold_until", "timestamp with time zone"},
		{"sidecar_watchdog_pending_actions", "attempt_count", "integer"},
		{"sidecar_watchdog_pending_actions", "last_attempt_at", "timestamp with time zone"},
		{"sidecar_watchdog_pending_actions", "last_error_message", "text"},
		{"sidecar_watchdog_pending_actions", "created_at", "timestamp with time zone"},
		{"sidecar_watchdog_pending_actions", "updated_at", "timestamp with time zone"},
	} {
		assertColumnDataType(t, ctx, conn, column.tableName, column.columnName, column.dataType)
	}
	for _, columnName := range []string{"auth_snapshot_id", "status", "error_message", "completed_at"} {
		assertColumnMissing(t, ctx, conn, "sidecar_watchdog_pending_actions", columnName)
	}
	assertColumnMissing(t, ctx, conn, "sidecar_quota_probe_observations", "updated_at")
	assertColumnMissing(t, ctx, conn, "sidecar_watchdog_holds", "last_action_id")
	assertColumnMissing(t, ctx, conn, "sidecar_watchdog_policies", "probe_cursor_auth_id")
	assertColumnMissing(t, ctx, conn, "sidecar_watchdog_policies", "probe_batch_size")
	for _, column := range []struct {
		tableName  string
		columnName string
		dataType   string
	}{
		{"sidecar_watchdog_policies", "active_revision_id", "bigint"},
		{"sidecar_watchdog_policies", "pending_revision_id", "bigint"},
		{"sidecar_watchdog_policies", "probe_concurrency", "integer"},
		{"sidecar_watchdog_policies", "probe_batch_cooldown_seconds", "integer"},
		{"sidecar_watchdog_policies", "probe_last_batch_completed_at", "timestamp with time zone"},
		{"sidecar_watchdog_policies", "quota_inventory_enabled", "boolean"},
		{"sidecar_watchdog_policies", "initial_scan_enabled", "boolean"},
		{"sidecar_watchdog_policies", "rolling_refresh_enabled", "boolean"},
		{"sidecar_watchdog_policies", "rolling_refresh_after_seconds", "integer"},
		{"sidecar_watchdog_policies", "working_priority", "integer"},
		{"sidecar_watchdog_policies", "empty_quota_priority", "integer"},
		{"sidecar_watchdog_policies", "initial_priority", "integer"},
		{"sidecar_watchdog_policy_revisions", "id", "bigint"},
		{"sidecar_watchdog_policy_revisions", "policy_id", "integer"},
		{"sidecar_watchdog_policy_revisions", "sidecar_id", "integer"},
		{"sidecar_watchdog_policy_revisions", "enabled", "boolean"},
		{"sidecar_watchdog_policy_revisions", "watchdog_sweep_interval_seconds", "integer"},
		{"sidecar_watchdog_policy_revisions", "failure_threshold", "integer"},
		{"sidecar_watchdog_policy_revisions", "failure_window_seconds", "integer"},
		{"sidecar_watchdog_policy_revisions", "fallback_cooldown_seconds", "integer"},
		{"sidecar_watchdog_policy_revisions", "quota_exceeded_priority", "integer"},
		{"sidecar_watchdog_policy_revisions", "using_priority", "integer"},
		{"sidecar_watchdog_policy_revisions", "working_priority", "integer"},
		{"sidecar_watchdog_policy_revisions", "empty_quota_priority", "integer"},
		{"sidecar_watchdog_policy_revisions", "initial_priority", "integer"},
		{"sidecar_watchdog_policy_revisions", "error_priority", "integer"},
		{"sidecar_watchdog_policy_revisions", "manual_override_pause_seconds", "integer"},
		{"sidecar_watchdog_policy_revisions", "probe_concurrency", "integer"},
		{"sidecar_watchdog_policy_revisions", "probe_timeout_seconds", "integer"},
		{"sidecar_watchdog_policy_revisions", "probe_batch_cooldown_seconds", "integer"},
		{"sidecar_watchdog_policy_revisions", "probe_jitter_min_ms", "integer"},
		{"sidecar_watchdog_policy_revisions", "probe_jitter_max_ms", "integer"},
		{"sidecar_watchdog_policy_revisions", "cooldown_jitter_percent", "integer"},
		{"sidecar_watchdog_policy_revisions", "quota_inventory_enabled", "boolean"},
		{"sidecar_watchdog_policy_revisions", "initial_scan_enabled", "boolean"},
		{"sidecar_watchdog_policy_revisions", "rolling_refresh_enabled", "boolean"},
		{"sidecar_watchdog_policy_revisions", "rolling_refresh_after_seconds", "integer"},
		{"sidecar_watchdog_policy_revisions", "created_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "sweep_id", "text"},
		{"sidecar_watchdog_sweeps", "sidecar_id", "integer"},
		{"sidecar_watchdog_sweeps", "policy_revision_id", "bigint"},
		{"sidecar_watchdog_sweeps", "status", "text"},
		{"sidecar_watchdog_sweeps", "snapshot_json", "jsonb"},
		{"sidecar_watchdog_sweeps", "next_item_index", "integer"},
		{"sidecar_watchdog_sweeps", "batch_index", "integer"},
		{"sidecar_watchdog_sweeps", "next_batch_after", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "last_heartbeat_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "lease_expires_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "pause_reason", "text"},
		{"sidecar_watchdog_sweeps", "failure_reason", "text"},
		{"sidecar_watchdog_sweeps", "restart_requested_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "restart_target_policy_revision_id", "bigint"},
		{"sidecar_watchdog_sweeps", "restart_reason", "text"},
		{"sidecar_watchdog_sweeps", "cancel_requested_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "cancel_reason", "text"},
		{"sidecar_watchdog_sweeps", "started_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "completed_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "created_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweeps", "updated_at", "timestamp with time zone"},
		{"sidecar_quota_scan_runs", "id", "bigint"},
		{"sidecar_quota_scan_runs", "scan_type", "text"},
		{"sidecar_quota_scan_runs", "status", "text"},
		{"sidecar_quota_scan_runs", "requested_by", "text"},
		{"sidecar_quota_scan_runs", "cursor_auth_id", "text"},
		{"sidecar_quota_scan_runs", "planned_count", "integer"},
		{"sidecar_quota_scan_runs", "attempted_count", "integer"},
		{"sidecar_quota_scan_runs", "using_count", "integer"},
		{"sidecar_quota_scan_runs", "quota_exceeded_count", "integer"},
		{"sidecar_quota_scan_runs", "error_count", "integer"},
		{"sidecar_quota_scan_runs", "skipped_count", "integer"},
		{"sidecar_quota_scan_runs", "cancel_requested_at", "timestamp with time zone"},
		{"sidecar_quota_scan_runs", "started_at", "timestamp with time zone"},
		{"sidecar_quota_scan_runs", "completed_at", "timestamp with time zone"},
		{"sidecar_quota_scan_runs", "last_error_code", "text"},
		{"sidecar_quota_scan_runs", "created_at", "timestamp with time zone"},
		{"sidecar_quota_scan_runs", "updated_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweep_items", "id", "bigint"},
		{"sidecar_watchdog_sweep_items", "sweep_id", "text"},
		{"sidecar_watchdog_sweep_items", "sidecar_id", "integer"},
		{"sidecar_watchdog_sweep_items", "policy_revision_id", "bigint"},
		{"sidecar_watchdog_sweep_items", "item_index", "integer"},
		{"sidecar_watchdog_sweep_items", "source", "text"},
		{"sidecar_watchdog_sweep_items", "source_rank", "integer"},
		{"sidecar_watchdog_sweep_items", "priority", "integer"},
		{"sidecar_watchdog_sweep_items", "due_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweep_items", "auth_id", "text"},
		{"sidecar_watchdog_sweep_items", "auth_index", "text"},
		{"sidecar_watchdog_sweep_items", "provider", "text"},
		{"sidecar_watchdog_sweep_items", "hold_id", "integer"},
		{"sidecar_watchdog_sweep_items", "auth_snapshot_id", "integer"},
		{"sidecar_watchdog_sweep_items", "selection_json", "jsonb"},
		{"sidecar_watchdog_sweep_items", "status", "text"},
		{"sidecar_watchdog_sweep_items", "lease_owner", "text"},
		{"sidecar_watchdog_sweep_items", "lease_expires_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweep_items", "attempt_token", "integer"},
		{"sidecar_watchdog_sweep_items", "started_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweep_items", "completed_at", "timestamp with time zone"},
		{"sidecar_watchdog_sweep_items", "result_observation_id", "integer"},
		{"sidecar_watchdog_sweep_items", "last_error_code", "text"},
		{"sidecar_auth_quota_states", "sidecar_id", "integer"},
		{"sidecar_auth_quota_states", "auth_id", "text"},
		{"sidecar_auth_quota_states", "auth_index", "text"},
		{"sidecar_auth_quota_states", "auth_name", "text"},
		{"sidecar_auth_quota_states", "provider", "text"},
		{"sidecar_auth_quota_states", "snapshot_observed_at", "timestamp with time zone"},
		{"sidecar_auth_quota_states", "quota_band", "text"},
		{"sidecar_auth_quota_states", "probe_status", "text"},
		{"sidecar_auth_quota_states", "quota_exceeded", "boolean"},
		{"sidecar_auth_quota_states", "reason_code", "text"},
		{"sidecar_auth_quota_states", "quota_reset_at", "timestamp with time zone"},
		{"sidecar_auth_quota_states", "blocking_window", "text"},
		{"sidecar_auth_quota_states", "last_observation_id", "integer"},
		{"sidecar_auth_quota_states", "last_probed_at", "timestamp with time zone"},
		{"sidecar_auth_quota_states", "last_error_code", "text"},
		{"sidecar_auth_quota_states", "created_at", "timestamp with time zone"},
		{"sidecar_auth_quota_states", "updated_at", "timestamp with time zone"},
	} {
		assertColumnDataType(t, ctx, conn, column.tableName, column.columnName, column.dataType)
	}
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_policies_thresholds", "probe_concurrency >= 1", "probe_concurrency <= 8", "probe_timeout_seconds <= 25", "probe_batch_cooldown_seconds > 0", "working_priority >= empty_quota_priority", "empty_quota_priority >= initial_priority", "initial_priority >= error_priority", "rolling_refresh_after_seconds > 0")
	assertConstraintDefinitionExcludes(t, ctx, conn, "ck_sidecar_watchdog_policies_thresholds", "probe_batch_size", "*")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_policy_revisions_pkey", "PRIMARY KEY (id)")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_policy_revisions_policy_id_fkey", "ON DELETE CASCADE")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_policy_revisions_sidecar_id_fkey", "ON DELETE CASCADE")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_policy_revisions_values", "watchdog_sweep_interval_seconds > 0", "probe_concurrency >= 1", "probe_concurrency <= 8", "probe_timeout_seconds <= 25", "working_priority >= empty_quota_priority", "empty_quota_priority >= initial_priority", "initial_priority >= error_priority", "rolling_refresh_after_seconds > 0")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_sweeps_pkey", "PRIMARY KEY (sweep_id)")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_sweeps_sidecar_id_fkey", "ON DELETE CASCADE")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_sweeps_policy_revision_id_fkey", "ON DELETE RESTRICT")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_sweeps_restart_target_revision_fkey", "ON DELETE RESTRICT")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweeps_status", "running", "paused", "completed", "failed", "cancelled")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweeps_checkpoint", "next_item_index >= 0", "batch_index >= 0", "jsonb_typeof")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweeps_restart_intent", "restart_requested_at IS NULL", "restart_target_policy_revision_id IS NOT NULL")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweeps_cancel_intent", "cancel_reason IS NULL", "cancel_requested_at IS NOT NULL")
	assertIndexUniqueness(t, ctx, conn, "sidecar_watchdog_sweeps", "uq_sidecar_watchdog_sweeps_active_sidecar", true)
	assertIndexDefinitionContains(t, ctx, conn, "uq_sidecar_watchdog_sweeps_active_sidecar", "sidecar_id", "running", "paused")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_quota_scan_runs_scan_type", "initial", "manual", "scheduled")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_quota_scan_runs_status", "completed", "cancelled", "failed")
	assertConstraintDefinitionExcludes(t, ctx, conn, "ck_sidecar_quota_scan_runs_status", "queued", "running")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_quota_scan_runs_pkey", "PRIMARY KEY (id)")
	assertIndexMissing(t, ctx, conn, "sidecar_quota_scan_runs", "uq_sidecar_quota_scan_runs_active_sidecar")
	assertIndexExists(t, ctx, conn, "sidecar_quota_scan_runs", "idx_sidecar_quota_scan_runs_sidecar_history")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_sweep_items_pkey", "PRIMARY KEY (id)")
	assertConstraintDefinitionContains(t, ctx, conn, "uq_sidecar_watchdog_sweep_items_sweep_index", "UNIQUE (sweep_id, item_index)")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_sweep_items_sweep_id_fkey", "ON DELETE CASCADE")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_watchdog_sweep_items_policy_revision_id_fkey", "ON DELETE RESTRICT")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweep_items_status", "queued", "leased", "succeeded", "failed", "cancelled", "superseded")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweep_items_shape", "item_index >= 0", "source_rank >= 0", "attempt_token >= 0", "jsonb_typeof")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweep_items_lease", "lease_owner IS NOT NULL", "lease_expires_at IS NOT NULL", "attempt_token > 0")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_watchdog_sweep_items_completion", "completed_at IS NULL", "completed_at IS NOT NULL")
	assertIndexExists(t, ctx, conn, "sidecar_watchdog_sweep_items", "idx_sidecar_watchdog_sweep_items_claimable")
	assertIndexExists(t, ctx, conn, "sidecar_watchdog_sweep_items", "idx_sidecar_watchdog_sweep_items_leased")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_auth_quota_states_band", "using", "quota_exceeded", "error")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_auth_quota_states_pkey", "PRIMARY KEY (sidecar_id, auth_id)")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_auth_quota_states_last_observation_id_fkey", "ON DELETE SET NULL")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_quota_probe_observations_required_text", "btrim(auth_id)", "btrim(probe_status)")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_quota_probe_observations_upstream_status", "upstream_status_code", "599")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_quota_probe_observations_windows_array", "jsonb_typeof", "array")
	assertConstraintDefinitionContains(t, ctx, conn, "sidecar_quota_probe_observations_sidecar_id_fkey", "ON DELETE CASCADE")
	var tableExists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'sidecar_watchdog_probe_observations')`).Scan(&tableExists); err != nil {
		t.Fatalf("check old probe observation table absence: %v", err)
	}
	if tableExists {
		t.Fatalf("expected sidecar_watchdog_probe_observations table to be renamed away")
	}
	assertIndexExists(t, ctx, conn, "sidecar_quota_probe_observations", "idx_sidecar_quota_probe_observations_sidecar_probed")
	assertIndexExists(t, ctx, conn, "sidecar_quota_probe_observations", "idx_sidecar_quota_probe_observations_auth_probed")
	assertIndexExists(t, ctx, conn, "sidecar_quota_probe_observations", "idx_sidecar_quota_probe_observations_probed_at")

	var relkind string
	if err := conn.QueryRow(ctx, `
		SELECT c.relkind::text
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = 'sidecar_watchdog_actions'`).Scan(&relkind); err != nil {
		t.Fatalf("load sidecar_watchdog_actions relkind: %v", err)
	}
	if relkind != "p" {
		t.Fatalf("expected sidecar_watchdog_actions to be partitioned, got relkind=%q", relkind)
	}
	var pkColumns string
	if err := conn.QueryRow(ctx, `
		SELECT string_agg(att.attname, ',' ORDER BY keys.key_order)
		FROM pg_constraint con
		JOIN unnest(con.conkey) WITH ORDINALITY AS keys(attnum, key_order) ON true
		JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = keys.attnum
		WHERE con.conrelid = 'public.sidecar_watchdog_actions'::regclass AND con.contype = 'p'`).Scan(&pkColumns); err != nil {
		t.Fatalf("load sidecar_watchdog_actions primary key columns: %v", err)
	}
	if pkColumns != "created_at,id" {
		t.Fatalf("expected sidecar_watchdog_actions primary key on created_at,id, got %q", pkColumns)
	}
	assertIndexExists(t, ctx, conn, "sidecar_watchdog_actions", "ix_sidecar_watchdog_actions_id")
	assertIndexExists(t, ctx, conn, "sidecar_watchdog_actions", "idx_sidecar_watchdog_actions_sidecar_created")
	assertIndexExists(t, ctx, conn, "sidecar_watchdog_pending_actions", "idx_sidecar_watchdog_pending_actions_sidecar_created")
	assertIndexUniqueness(t, ctx, conn, "sidecar_watchdog_pending_actions", "uq_sidecar_watchdog_pending_actions_action_history_key", true)
	assertIndexDefinitionContains(t, ctx, conn, "uq_sidecar_watchdog_pending_actions_action_history_key", "action_history_created_at", "action_history_id")
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
	assertIndexDefinitionContains(t, ctx, conn, "uq_sidecar_watchdog_holds_active_auth", "status = ANY", "active", "paused")
	assertConstraintDefinitionContains(t, ctx, conn, "ck_sidecar_instances_management_auth_state", "invalid_management_auth")
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

func assertColumnMissing(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnName string) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2)`, tableName, columnName).Scan(&exists); err != nil {
		t.Fatalf("check %s.%s column absence: %v", tableName, columnName, err)
	}
	if exists {
		t.Fatalf("expected %s.%s column to be absent", tableName, columnName)
	}
}

func assertFourBandPriorityRow(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, idColumn string, id any) {
	t.Helper()
	var usingPriority, quotaPriority, workingPriority, emptyQuotaPriority, initialPriority, errorPriority int
	query := fmt.Sprintf(`SELECT using_priority, quota_exceeded_priority, working_priority, empty_quota_priority, initial_priority, error_priority FROM %s WHERE %s = $1`, tableName, idColumn)
	if err := conn.QueryRow(ctx, query, id).Scan(&usingPriority, &quotaPriority, &workingPriority, &emptyQuotaPriority, &initialPriority, &errorPriority); err != nil {
		t.Fatalf("load four-band priorities from %s: %v", tableName, err)
	}
	if usingPriority != 99 || quotaPriority != 90 || workingPriority != 99 || emptyQuotaPriority != 90 || initialPriority != 50 || errorPriority != 10 {
		t.Fatalf("unexpected four-band priorities in %s: using=%d quota=%d working=%d empty=%d initial=%d error=%d", tableName, usingPriority, quotaPriority, workingPriority, emptyQuotaPriority, initialPriority, errorPriority)
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

func assertConstraintDefinitionExcludes(t *testing.T, ctx context.Context, conn *pgx.Conn, constraintName string, fragments ...string) {
	t.Helper()
	var definition string
	if err := conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, constraintName).Scan(&definition); err != nil {
		t.Fatalf("load constraint definition %s: %v", constraintName, err)
	}
	for _, fragment := range fragments {
		if strings.Contains(definition, fragment) {
			t.Fatalf("expected constraint %s definition %q not to contain %q", constraintName, definition, fragment)
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

func assertIndexMissing(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, indexName string) {
	t.Helper()
	assertIndexPresence(t, ctx, conn, tableName, indexName, false)
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
	dayColumns := []string{"request_logs_retention_days", "audit_logs_retention_days", "statistics_retention_days", "loadbalance_events_retention_days", "sidecar_action_history_retention_days"}
	rows, err := conn.Query(ctx, `
		SELECT column_name, data_type, COALESCE(character_maximum_length, 0), is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'log_retention_settings'
		  AND column_name = ANY($1::text[])`, dayColumns)
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
	if constraintCount != 6 {
		t.Fatalf("expected six log_retention_settings check constraints, got %d", constraintCount)
	}
	assertConstraintDefinitionContains(t, ctx, conn, "log_retention_settings_singleton_key_check", "'global'")
	assertConstraintDefinitionContains(t, ctx, conn, "log_retention_settings_sidecar_action_history_retention_days_check", "sidecar_action_history_retention_days", ">= 1")
}

func seedLegacySidecarActionHistoryMigrationFixture(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	applyMigrationsThrough(t, ctx, conn, "000016_sidecar_watchdog_action_auth_name")

	var sidecarID int
	if err := conn.QueryRow(ctx, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password)
VALUES ($1, $2, $3, $4)
RETURNING id`, "legacy-sidecar", "https://legacy-sidecar.example.test", "https://legacy-sidecar.example.test", "enc:legacy").Scan(&sidecarID); err != nil {
		t.Fatalf("seed pre-split sidecar instance: %v", err)
	}

	var actionID int
	if err := conn.QueryRow(ctx, `INSERT INTO sidecar_watchdog_actions (
sidecar_id, auth_id, auth_name, auth_index, provider, action_type, reason,
previous_priority, target_priority, hold_until, status, created_at, updated_at)
VALUES ($1, 'auth-legacy', 'legacy-auth.json', 'idx-legacy', 'codex', 'deprioritize',
'quota_exceeded', 10, 0, NOW(), 'pending', NOW() - INTERVAL '1 day', NOW() - INTERVAL '1 day')
RETURNING id`, sidecarID).Scan(&actionID); err != nil {
		t.Fatalf("seed legacy sidecar_watchdog_actions row: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO sidecar_watchdog_holds (
sidecar_id, auth_id, auth_index, provider, reason, condition_hash, previous_priority,
target_priority, hold_until, status, last_action_id)
VALUES ($1, 'auth-legacy', 'idx-legacy', 'codex', 'quota_exceeded', 'legacy-hash', 10,
0, NOW(), 'active', $2)`, sidecarID, actionID); err != nil {
		t.Fatalf("seed legacy sidecar_watchdog_holds row: %v", err)
	}
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

func TestSidecarQuotaInventoryMigrationConstraints(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "quota_inventory_constraints")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run quota inventory migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected quota inventory migration to apply, got %q", result.Outcome)
	}

	var sidecarID int
	if err := conn.QueryRow(testContext, `INSERT INTO sidecar_instances (name, base_url, base_url_canonical, management_password)
VALUES ($1, $2, $3, $4)
RETURNING id`, "quota inventory constraints", "https://quota.example.test", "https://quota.example.test", "enc:fixture").Scan(&sidecarID); err != nil {
		t.Fatalf("seed sidecar for quota inventory migration constraints: %v", err)
	}

	for _, query := range []struct {
		name string
		sql  string
	}{
		{name: "probe concurrency positive", sql: `INSERT INTO sidecar_watchdog_policies (sidecar_id, probe_concurrency) VALUES ($1, 0)`},
		{name: "probe concurrency max", sql: `INSERT INTO sidecar_watchdog_policies (sidecar_id, probe_concurrency) VALUES ($1, 9)`},
		{name: "probe timeout max", sql: `INSERT INTO sidecar_watchdog_policies (sidecar_id, probe_timeout_seconds) VALUES ($1, 26)`},
		{name: "cooldown positive", sql: `INSERT INTO sidecar_watchdog_policies (sidecar_id, probe_batch_cooldown_seconds) VALUES ($1, 0)`},
		{name: "refresh positive", sql: `INSERT INTO sidecar_watchdog_policies (sidecar_id, rolling_refresh_after_seconds) VALUES ($1, 0)`},
		{name: "scan type enum", sql: `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'bogus', 'queued')`},
		{name: "scan status enum", sql: `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'manual', 'bogus')`},
		{name: "quota band enum", sql: `INSERT INTO sidecar_auth_quota_states (sidecar_id, auth_id, quota_band) VALUES ($1, 'auth-bad', 'bogus')`},
	} {
		t.Run(query.name, func(t *testing.T) {
			if _, err := conn.Exec(testContext, query.sql, sidecarID); err == nil {
				t.Fatalf("expected %s to fail schema validation", query.name)
			}
		})
	}

	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_watchdog_policies (sidecar_id, probe_concurrency, probe_timeout_seconds) VALUES ($1, 8, 25)`, sidecarID); err != nil {
		t.Fatalf("expected concurrent probe policy to allow max concurrency with max timeout: %v", err)
	}

	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'manual', 'queued')`, sidecarID); err == nil {
		t.Fatalf("expected queued quota scan run to be rejected after projection-only demotion")
	}
	if _, err := conn.Exec(testContext, `INSERT INTO sidecar_quota_scan_runs (sidecar_id, scan_type, status) VALUES ($1, 'manual', 'completed'), ($1, 'scheduled', 'failed')`, sidecarID); err != nil {
		t.Fatalf("expected projection-only quota scan history rows to insert without active uniqueness: %v", err)
	}
}

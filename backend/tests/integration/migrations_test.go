package integrationtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

const updateMigrationSchemaGoldenEnv = "PRISM_UPDATE_MIGRATION_SCHEMA_GOLDEN"

var expectedPrismMigrationVersions = []string{
	migrate.DefaultBaselineVersion,
	"000002_context_overflow_promotion_target",
	"000003_openai_text_capability",
	"000004_endpoint_label_snapshot",
	"000005_remove_access_target_weight_priority_add_audit_family_settings",
	"000006_openai_accepted_format",
	"000007_remove_context_capabilities_and_facades",
	"000008_drop_dead_auth_tables",
	"000009_stats_write_coherence",
	"000010_drop_management_stat_rollups",
	"000011_alert_webhook_outbox",
}

func TestSingleBaselineAppliesToFreshDatabase(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "fresh_apply"
	conn := harness.openEmptyDatabase(t, testContext, databaseName)
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
	assertMigratedSchemaGolden(t, testContext, harness, databaseName)
	assertFreshBaselineSeedRows(t, testContext, conn)
}

func TestAccessTargetRankingRemovalAndAuditFamilySettingsMigration(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "access_target_ranking_drop_audit_family")
	defer func() { _ = conn.Close(testContext) }()

	seedPreAuditFamilyMigrationSchema(t, testContext, conn)

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply access-target ranking removal migration: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected access-target ranking removal migration to apply, got %q", result.Outcome)
	}
	assertMigrationVersions(t, "access-target ranking removal migration versions", result.Versions, []string{"000005_remove_access_target_weight_priority_add_audit_family_settings", "000006_openai_accepted_format", "000007_remove_context_capabilities_and_facades", "000008_drop_dead_auth_tables", "000009_stats_write_coherence", "000010_drop_management_stat_rollups", "000011_alert_webhook_outbox"})
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))
	assertColumnsAbsent(t, testContext, conn, "model_access_targets", "weight", "target_priority")
	assertColumnsAbsent(t, testContext, conn, "model_configs", "facade_enabled", "facade_selection_policy", "facade_fallback_policy")
	assertColumnsAbsent(t, testContext, conn, "connections", "context_window_tokens", "context_window_tokens_overridden", "default_output_token_reserve", "default_output_token_reserve_overridden", "max_context_utilization", "max_context_utilization_overridden", "preferred_context_utilization_threshold", "preferred_context_utilization_threshold_overridden")
	assertTablePresence(t, testContext, conn, "profile_api_family_audit_settings", true)
}

func TestProfileAPIFamilyAuditSettingsFreshConstraints(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "audit_family_settings_constraints")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run audit family settings fresh baseline: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected audit family settings fresh baseline to apply, got %q", result.Outcome)
	}

	assertProfileAPIFamilyAuditSettingsDataConstraints(t, testContext, conn)
}

func TestEndpointLabelSnapshotFreshSchemaContract(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "endpoint_label_snapshot_fresh")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run endpoint label snapshot fresh baseline: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected endpoint label snapshot fresh baseline to apply, got %q", result.Outcome)
	}

	assertColumnNotNull(t, testContext, conn, "usage_request_events", "endpoint_label_snapshot")
	partitionName := ensureDailyLogPartition(t, testContext, conn, "usage_request_events", time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC), "fresh")
	assertColumnNotNull(t, testContext, conn, partitionName, "endpoint_label_snapshot")
}

func TestEndpointLabelSnapshotMigrationBackfillsExistingRows(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	oldRunner := newRunnerThroughMigration(t, "000003_openai_text_capability", func(version string, sql string) string {
		if version != migrate.DefaultBaselineVersion {
			return sql
		}
		return strings.Replace(sql, "    endpoint_label_snapshot text NOT NULL,\n", "", 1)
	})
	conn := harness.openEmptyDatabase(t, testContext, "endpoint_label_snapshot_backfill")
	defer func() { _ = conn.Close(testContext) }()

	oldResult, err := oldRunner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run pre-endpoint-label-snapshot migrations: %v", err)
	}
	if oldResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected pre-endpoint-label-snapshot migrations to apply, got %q", oldResult.Outcome)
	}
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsThrough(t, "000003_openai_text_capability"))

	fixture := seedEndpointLabelSnapshotBackfillRows(t, testContext, conn, "migration", false)

	newResult, err := newRunner(t).Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply endpoint label snapshot migration: %v", err)
	}
	if newResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected endpoint label snapshot migration to apply, got %q", newResult.Outcome)
	}
	assertMigrationVersions(t, "endpoint label snapshot migration versions", newResult.Versions, []string{"000004_endpoint_label_snapshot", "000005_remove_access_target_weight_priority_add_audit_family_settings", "000006_openai_accepted_format", "000007_remove_context_capabilities_and_facades", "000008_drop_dead_auth_tables", "000009_stats_write_coherence", "000010_drop_management_stat_rollups", "000011_alert_webhook_outbox"})
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))
	assertColumnNotNull(t, testContext, conn, "usage_request_events", "endpoint_label_snapshot")
	assertColumnNotNull(t, testContext, conn, fixture.usagePartition, "endpoint_label_snapshot")
	assertEndpointLabelSnapshotBackfillRows(t, testContext, conn, fixture.expectedLabels)
}

func TestMigrationBackfillsOpenAIAcceptedFormatToDualNative(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	oldRunner := newRunnerThroughMigration(t, "000005_remove_access_target_weight_priority_add_audit_family_settings", nil)
	conn := harness.openEmptyDatabase(t, testContext, "openai_accepted_format_backfill")
	defer func() { _ = conn.Close(testContext) }()

	oldResult, err := oldRunner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run pre-openai-accepted-format migrations: %v", err)
	}
	if oldResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected pre-openai-accepted-format migrations to apply, got %q", oldResult.Outcome)
	}
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsThrough(t, "000005_remove_access_target_weight_priority_add_audit_family_settings"))

	profileID := seedOpenAIAcceptedFormatProfile(t, testContext, conn)
	seedOpenAIAcceptedFormatModel(t, testContext, conn, profileID, "gpt-existing", "openai")
	seedOpenAIAcceptedFormatModel(t, testContext, conn, profileID, "claude-existing", "anthropic")

	newResult, err := newRunner(t).Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply openai accepted format migration: %v", err)
	}
	if newResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected openai accepted format migration to apply, got %q", newResult.Outcome)
	}
	assertMigrationVersions(t, "openai accepted format migration versions", newResult.Versions, []string{"000006_openai_accepted_format", "000007_remove_context_capabilities_and_facades", "000008_drop_dead_auth_tables", "000009_stats_write_coherence", "000010_drop_management_stat_rollups", "000011_alert_webhook_outbox"})
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))
	assertColumnsExist(t, testContext, conn, "model_configs", "openai_accepted_format")
	assertOpenAIAcceptedFormatBackfillRows(t, testContext, conn)
}

func TestStatsWriteCoherenceMigrationBackfillsHistoricalRows(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	oldRunner := newRunnerThroughMigration(t, "000008_drop_dead_auth_tables", nil)
	conn := harness.openEmptyDatabase(t, testContext, "stats_write_coherence_backfill")
	defer func() { _ = conn.Close(testContext) }()

	oldResult, err := oldRunner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run pre-stats-coherence migrations: %v", err)
	}
	if oldResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected pre-stats-coherence migrations to apply, got %q", oldResult.Outcome)
	}
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsThrough(t, "000008_drop_dead_auth_tables"))

	createdAt := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	ensureDailyLogPartition(t, testContext, conn, "request_logs", createdAt, "stats-coherence")
	ensureDailyLogPartition(t, testContext, conn, "usage_request_events", createdAt, "stats-coherence")
	profileID := seedEndpointLabelSnapshotProfile(t, testContext, conn, "stats-coherence")

	insertStatsCoherenceRequestLog(t, testContext, conn, profileID, "request-missing-cost", true, nil, nil, nil, nil, nil, nil, createdAt)
	insertStatsCoherenceRequestLog(t, testContext, conn, profileID, "request-has-cost", false, nil, int64Ptr(1250), stringPtr("USD"), stringPtr("USD"), nil, nil, createdAt.Add(time.Minute))
	insertStatsCoherenceRequestLog(t, testContext, conn, profileID, "request-explicit-reason", true, stringPtr("  MISSING_TOKEN_USAGE  "), nil, nil, nil, nil, nil, createdAt.Add(2*time.Minute))
	insertStatsCoherenceUsageEvent(t, testContext, conn, profileID, "usage-missing-cost", true, nil, nil, nil, nil, nil, nil, createdAt)
	insertStatsCoherenceUsageEvent(t, testContext, conn, profileID, "usage-has-cost", false, nil, int64Ptr(1250), stringPtr("USD"), stringPtr("USD"), nil, nil, createdAt.Add(time.Minute))

	newResult, err := newRunner(t).Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply stats write coherence migrations: %v", err)
	}
	if newResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected stats write coherence migrations to apply, got %q", newResult.Outcome)
	}
	assertMigrationVersions(t, "stats write coherence migration versions", newResult.Versions, []string{"000009_stats_write_coherence", "000010_drop_management_stat_rollups", "000011_alert_webhook_outbox"})
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))

	assertStatsCoherenceRow(t, testContext, conn, "request_logs", "request-missing-cost", false, "MISSING_PRICE_DATA", nil, nil)
	assertStatsCoherenceRow(t, testContext, conn, "request_logs", "request-has-cost", true, "", stringPtr("1"), stringPtr("DEFAULT_1_TO_1"))
	assertStatsCoherenceRow(t, testContext, conn, "request_logs", "request-explicit-reason", false, "MISSING_TOKEN_USAGE", nil, nil)
	assertStatsCoherenceRow(t, testContext, conn, "usage_request_events", "usage-missing-cost", false, "MISSING_PRICE_DATA", nil, nil)
	assertStatsCoherenceRow(t, testContext, conn, "usage_request_events", "usage-has-cost", true, "", stringPtr("1"), stringPtr("DEFAULT_1_TO_1"))
	assertManagementStatRollupTablesAbsent(t, testContext, conn)
}

func TestDirtyDatabaseWithoutMigrationHistoryFails(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)

	t.Run("missing_history", func(t *testing.T) {
		conn := harness.openEmptyDatabase(t, testContext, "existing_without_history")
		defer func() { _ = conn.Close(testContext) }()

		if _, err := conn.Exec(testContext, `CREATE TABLE app_auth_settings (id BIGSERIAL PRIMARY KEY)`); err != nil {
			t.Fatalf("seed app table: %v", err)
		}

		result, err := runner.Run(testContext, conn)
		if err == nil {
			t.Fatalf("expected database without migration history to fail, got %+v", result)
		}
		if !strings.Contains(err.Error(), "prism_schema_migrations is missing") {
			t.Fatalf("expected missing history error, got %v", err)
		}

		assertHistoryTableMissing(t, testContext, conn)
	})

	t.Run("obsolete_history", func(t *testing.T) {
		conn := harness.openEmptyDatabase(t, testContext, "existing_obsolete_history")
		defer func() { _ = conn.Close(testContext) }()

		if _, err := conn.Exec(testContext, `CREATE TABLE prism_schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
			t.Fatalf("create obsolete migration history table: %v", err)
		}
		if _, err := conn.Exec(testContext, `INSERT INTO prism_schema_migrations (version, applied_at) VALUES ('000001_baseline', NOW())`); err != nil {
			t.Fatalf("seed obsolete migration history: %v", err)
		}
		if _, err := conn.Exec(testContext, `CREATE TABLE app_auth_settings (id BIGSERIAL PRIMARY KEY)`); err != nil {
			t.Fatalf("seed app table: %v", err)
		}

		result, err := runner.Run(testContext, conn)
		if err == nil {
			t.Fatalf("expected database with obsolete migration history to fail, got %+v", result)
		}
		if !strings.Contains(err.Error(), "current baseline "+migrate.DefaultBaselineVersion) {
			t.Fatalf("expected current-baseline history error, got %v", err)
		}

		assertHistoryVersionMissing(t, testContext, conn, migrate.DefaultBaselineVersion)
	})
}

func TestBaselineSecondRunNoop(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "baseline_second_run_noop")
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
	assertIndexPresence(t, testContext, conn, "model_access_targets", "uq_model_access_targets_connection_owner", true)
}

func TestTranslatedObservabilitySchemaGuardUpgradesStampedDatabase(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "translated_observability_schema_guard")
	defer func() { _ = conn.Close(testContext) }()

	firstResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run baseline before translated observability guard check: %v", err)
	}
	if firstResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected first run to apply baseline, got %q", firstResult.Outcome)
	}

	fixture := seedEndpointLabelSnapshotBackfillRows(t, testContext, conn, "guard", true)

	for _, statement := range []string{
		`ALTER TABLE public.request_logs DROP COLUMN IF EXISTS upstream_operation_name`,
		`ALTER TABLE public.request_logs DROP COLUMN IF EXISTS operation_translation_mode`,
		`ALTER TABLE public.request_logs DROP COLUMN IF EXISTS upstream_request_path`,
		`ALTER TABLE public.usage_request_events DROP COLUMN IF EXISTS upstream_operation_name`,
		`ALTER TABLE public.usage_request_events DROP COLUMN IF EXISTS operation_translation_mode`,
		`ALTER TABLE public.usage_request_events DROP COLUMN IF EXISTS upstream_request_path`,
		`ALTER TABLE public.usage_request_events DROP COLUMN IF EXISTS endpoint_label_snapshot`,
	} {
		if _, err := conn.Exec(testContext, statement); err != nil {
			t.Fatalf("drop translated observability schema surface with %q: %v", statement, err)
		}
	}

	guardResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("rerun baseline with stamped database missing translated observability schema: %v", err)
	}
	if guardResult.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected translated observability guard rerun to noop, got %q", guardResult.Outcome)
	}
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))
	assertTranslatedObservabilityColumns(t, testContext, conn)
	assertColumnNotNull(t, testContext, conn, "usage_request_events", "endpoint_label_snapshot")
	assertColumnNotNull(t, testContext, conn, fixture.usagePartition, "endpoint_label_snapshot")
	assertEndpointLabelSnapshotBackfillRows(t, testContext, conn, fixture.expectedLabels)
}

func TestModelPrivateConnectionOwnershipSchemaGuard(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)

	t.Run("creates_missing_index_for_stamped_database", func(t *testing.T) {
		conn := harness.openEmptyDatabase(t, testContext, "ownership_guard_clean")
		defer func() { _ = conn.Close(testContext) }()

		result, err := runner.Run(testContext, conn)
		if err != nil {
			t.Fatalf("run baseline before schema guard: %v", err)
		}
		if result.Outcome != migrate.OutcomeApply {
			t.Fatalf("expected initial baseline to apply, got %q", result.Outcome)
		}

		profileID, connectionID := seedModelOwnershipConnection(t, testContext, conn, "clean")
		seedModelAccessTargetConnectionOwner(t, testContext, conn, profileID, connectionID, "guard-clean-owner")
		dropModelAccessTargetConnectionOwnerIndex(t, testContext, conn)
		assertIndexPresence(t, testContext, conn, "model_access_targets", "uq_model_access_targets_connection_owner", false)

		guardResult, err := runner.Run(testContext, conn)
		if err != nil {
			t.Fatalf("run schema guard on stamped clean database: %v", err)
		}
		if guardResult.Outcome != migrate.OutcomeNoop {
			t.Fatalf("expected stamped schema guard run to noop, got %q", guardResult.Outcome)
		}
		assertIndexPresence(t, testContext, conn, "model_access_targets", "uq_model_access_targets_connection_owner", true)
	})

	t.Run("fails_duplicate_owners_before_creating_index", func(t *testing.T) {
		conn := harness.openEmptyDatabase(t, testContext, "ownership_guard_duplicates")
		defer func() { _ = conn.Close(testContext) }()

		result, err := runner.Run(testContext, conn)
		if err != nil {
			t.Fatalf("run baseline before duplicate guard check: %v", err)
		}
		if result.Outcome != migrate.OutcomeApply {
			t.Fatalf("expected initial baseline to apply, got %q", result.Outcome)
		}

		dropModelAccessTargetConnectionOwnerIndex(t, testContext, conn)
		profileID, connectionID := seedModelOwnershipConnection(t, testContext, conn, "duplicates")
		firstSourceID := seedModelAccessTargetConnectionOwner(t, testContext, conn, profileID, connectionID, "guard-duplicate-owner-a")
		secondSourceID := seedModelAccessTargetConnectionOwner(t, testContext, conn, profileID, connectionID, "guard-duplicate-owner-b")

		_, err = runner.Run(testContext, conn)
		if err == nil {
			t.Fatal("expected duplicate ownership guard to fail")
		}
		errorText := err.Error()
		for _, fragment := range []string{
			"uq_model_access_targets_connection_owner",
			fmt.Sprintf("target_connection_id=%d", connectionID),
			"owner_count=2",
			fmt.Sprintf("source_model_config_ids=[%d %d]", firstSourceID, secondSourceID),
		} {
			if !strings.Contains(errorText, fragment) {
				t.Fatalf("expected duplicate guard error %q to contain %q", errorText, fragment)
			}
		}
		assertIndexPresence(t, testContext, conn, "model_access_targets", "uq_model_access_targets_connection_owner", false)
	})
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

func expectedMigrationVersionsThrough(t *testing.T, end string) []string {
	t.Helper()
	for index, version := range expectedPrismMigrationVersions {
		if version == end {
			return append([]string(nil), expectedPrismMigrationVersions[:index+1]...)
		}
	}
	t.Fatalf("unknown migration version %q", end)
	return nil
}

func newRunnerThroughMigration(t *testing.T, latestVersion string, mutateSQL func(version string, sql string) string) migrate.Runner {
	t.Helper()

	migrationsDir := t.TempDir()
	sourceDir := migrate.DefaultMigrationsDir()
	for _, version := range expectedMigrationVersionsThrough(t, latestVersion) {
		filename := version + ".sql"
		raw, err := os.ReadFile(filepath.Join(sourceDir, filename))
		if err != nil {
			t.Fatalf("read source migration %s: %v", filename, err)
		}
		sql := string(raw)
		if mutateSQL != nil {
			sql = mutateSQL(version, sql)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, filename), []byte(sql), 0o600); err != nil {
			t.Fatalf("write temporary migration %s: %v", filename, err)
		}
	}

	runner, err := migrate.New(migrate.Options{MigrationsDir: migrationsDir})
	if err != nil {
		t.Fatalf("build temporary migration runner: %v", err)
	}
	return runner
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

func assertHistoryVersionMissing(t *testing.T, ctx context.Context, conn *pgx.Conn, version string) {
	t.Helper()

	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM prism_schema_migrations WHERE version = $1)`, version).Scan(&exists); err != nil {
		t.Fatalf("check prism migration history version %s absence: %v", version, err)
	}
	if exists {
		t.Fatalf("expected prism migration history version %s to remain absent", version)
	}
}

func seedPreAuditFamilyMigrationSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	statements := []string{
		`CREATE TABLE prism_schema_migrations (version VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE profiles (id integer PRIMARY KEY, name character varying(200) NOT NULL, description text, is_active boolean NOT NULL, is_default boolean NOT NULL, is_editable boolean NOT NULL, version integer NOT NULL, deleted_at timestamp with time zone, created_at timestamp with time zone NOT NULL, updated_at timestamp with time zone NOT NULL)`,
		`CREATE TABLE model_configs (id integer PRIMARY KEY, profile_id integer NOT NULL, api_family character varying(50) NOT NULL, model_id character varying(200) NOT NULL, facade_enabled boolean DEFAULT false NOT NULL, facade_selection_policy character varying(100), facade_fallback_policy character varying(100), is_enabled boolean NOT NULL, created_at timestamp with time zone NOT NULL, updated_at timestamp with time zone NOT NULL, CONSTRAINT ck_model_configs_facade_policy_contract CHECK (((NOT facade_enabled) AND ((facade_selection_policy IS NULL) OR ((facade_selection_policy)::text = 'weighted_eligible_context'::text)) AND ((facade_fallback_policy IS NULL) OR ((facade_fallback_policy)::text = 'redistribute_ineligible_weight'::text))) OR (facade_enabled AND ((facade_selection_policy)::text = 'weighted_eligible_context'::text) AND ((facade_fallback_policy)::text = 'redistribute_ineligible_weight'::text))))`,
		`CREATE TABLE model_access_targets (id integer PRIMARY KEY, profile_id integer NOT NULL, source_model_config_id integer NOT NULL, target_type character varying(20) NOT NULL, target_model_config_id integer, target_connection_id integer, position integer NOT NULL, weight integer, target_priority integer, is_enabled boolean NOT NULL, created_at timestamp with time zone NOT NULL, updated_at timestamp with time zone NOT NULL, CONSTRAINT chk_model_access_targets_target_metadata CHECK (((((target_type)::text = 'model'::text) AND (weight IS NOT NULL) AND (weight >= 1) AND (target_priority IS NOT NULL) AND (target_priority >= 0)) OR (((target_type)::text = 'connection'::text) AND (weight IS NULL) AND (target_priority IS NULL)))), CONSTRAINT chk_model_access_targets_one_target CHECK (((((target_type)::text = 'model'::text) AND (target_model_config_id IS NOT NULL) AND (target_connection_id IS NULL)) OR (((target_type)::text = 'connection'::text) AND (target_model_config_id IS NULL) AND (target_connection_id IS NOT NULL)))))`,
		`CREATE TABLE request_logs (id integer PRIMARY KEY, profile_id integer NOT NULL, status_code integer, success_flag boolean, priced_flag boolean, unpriced_reason text, total_cost_user_currency_micros bigint, currency_code_original character varying(10), report_currency_code character varying(10), fx_rate_used character varying(20), fx_rate_source character varying(30))`,
		`CREATE TABLE usage_request_events (id integer PRIMARY KEY, profile_id integer NOT NULL, status_code integer, success_flag boolean, billable_flag boolean, priced_flag boolean, unpriced_reason text, total_cost_user_currency_micros bigint, currency_code_original character varying(10), report_currency_code character varying(10), fx_rate_used character varying(20), fx_rate_source character varying(30), endpoint_label_snapshot text NOT NULL DEFAULT '')`,
		`INSERT INTO profiles (id, name, is_active, is_default, is_editable, version, created_at, updated_at) VALUES (1, 'pre-audit-family', FALSE, FALSE, TRUE, 1, NOW(), NOW())`,
		`INSERT INTO model_configs (id, profile_id, api_family, model_id, facade_enabled, facade_selection_policy, facade_fallback_policy, is_enabled, created_at, updated_at) VALUES (1, 1, 'openai', 'pre-router', FALSE, NULL, NULL, TRUE, NOW(), NOW()), (2, 1, 'openai', 'pre-target', FALSE, NULL, NULL, TRUE, NOW(), NOW())`,
		`INSERT INTO model_access_targets (id, profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at) VALUES (1, 1, 1, 'model', 2, 0, 7, 3, TRUE, NOW(), NOW())`,
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("seed pre-audit-family migration schema with %q: %v", statement, err)
		}
	}
	for _, version := range expectedMigrationVersionsThrough(t, "000004_endpoint_label_snapshot") {
		if _, err := conn.Exec(ctx, `INSERT INTO prism_schema_migrations (version) VALUES ($1)`, version); err != nil {
			t.Fatalf("seed migration history version %s: %v", version, err)
		}
	}
}

func assertMigratedSchemaGolden(t *testing.T, ctx context.Context, harness postgresHarness, databaseName string) {
	t.Helper()
	actual := normalizeSchemaDump(runDockerCommandOrFail(
		t,
		ctx,
		"exec",
		"-e",
		"PGPASSWORD=prism",
		harness.containerName,
		"pg_dump",
		"--host=127.0.0.1",
		"--username=prism",
		"--dbname="+databaseName,
		"--schema=public",
		"--schema-only",
		"--no-comments",
		"--no-owner",
		"--no-privileges",
		"--no-security-labels",
		"--no-tablespaces",
	))

	path := filepath.Join("testdata", "migrations", "schema.sql")
	if os.Getenv(updateMigrationSchemaGoldenEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create migration schema golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(actual+"\n"), 0o644); err != nil {
			t.Fatalf("update migration schema golden: %v", err)
		}
	}

	rawExpected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration schema golden %s: %v", path, err)
	}
	expected := strings.TrimSpace(string(rawExpected))
	if actual != expected {
		t.Fatalf("migration schema golden mismatch\n%s\n\nset %s=1 to update", firstSchemaDiff(expected, actual), updateMigrationSchemaGoldenEnv)
	}
}

func normalizeSchemaDump(value string) string {
	lines := strings.Split(value, "\n")
	statements := make([]string, 0, len(lines)/3)
	current := make([]string, 0, 8)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "--") ||
			strings.HasPrefix(trimmed, "SET ") ||
			strings.HasPrefix(trimmed, "SELECT pg_catalog.set_config(") ||
			strings.HasPrefix(trimmed, `\restrict`) ||
			strings.HasPrefix(trimmed, `\unrestrict`) {
			continue
		}
		current = append(current, trimmed)
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, strings.Join(strings.Fields(strings.Join(current, " ")), " "))
			current = current[:0]
		}
	}
	if len(current) > 0 {
		statements = append(statements, strings.Join(strings.Fields(strings.Join(current, " ")), " "))
	}
	return strings.Join(statements, "\n")
}

func firstSchemaDiff(expected string, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	limit := len(expectedLines)
	if len(actualLines) > limit {
		limit = len(actualLines)
	}
	for index := range limit {
		var want, got string
		if index < len(expectedLines) {
			want = expectedLines[index]
		}
		if index < len(actualLines) {
			got = actualLines[index]
		}
		if want != got {
			return fmt.Sprintf("line %d\nwant: %s\ngot:  %s", index+1, want, got)
		}
	}
	return "schema lengths differ"
}

func assertFreshBaselineSeedRows(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	rows, err := conn.Query(ctx, `SELECT domain, scope_type, scope_id, version FROM runtime_cache_generations ORDER BY domain ASC`)
	if err != nil {
		t.Fatalf("query runtime cache generations: %v", err)
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var domain, scopeType, scopeID string
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
			t.Fatalf("expected runtime cache generation %q, got %+v", key, got)
		}
		if version != 0 {
			t.Fatalf("expected runtime cache generation %q version 0, got %d", key, version)
		}
	}

	var retentionRows int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM log_retention_settings WHERE singleton_key = 'global'`).Scan(&retentionRows); err != nil {
		t.Fatalf("count global log_retention_settings row: %v", err)
	}
	if retentionRows != 1 {
		t.Fatalf("expected one global log_retention_settings row, got %d", retentionRows)
	}
}

func assertTablePresence(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, wantExists bool) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`, tableName).Scan(&exists); err != nil {
		t.Fatalf("check table %s presence: %v", tableName, err)
	}
	if exists != wantExists {
		t.Fatalf("expected table %s exists=%v, got %v", tableName, wantExists, exists)
	}
}

func assertColumnsExist(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnNames ...string) {
	t.Helper()
	for _, columnName := range columnNames {
		assertColumnPresence(t, ctx, conn, tableName, columnName, true)
	}
}

func assertColumnsAbsent(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnNames ...string) {
	t.Helper()
	for _, columnName := range columnNames {
		assertColumnPresence(t, ctx, conn, tableName, columnName, false)
	}
}

func assertColumnPresence(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnName string, wantExists bool) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2)`, tableName, columnName).Scan(&exists); err != nil {
		t.Fatalf("check %s.%s presence: %v", tableName, columnName, err)
	}
	if exists != wantExists {
		t.Fatalf("expected %s.%s exists=%v, got %v", tableName, columnName, wantExists, exists)
	}
}

func assertColumnNotNull(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnName string) {
	t.Helper()
	var nullable string
	if err := conn.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, tableName, columnName).Scan(&nullable); err != nil {
		t.Fatalf("load %s.%s nullability: %v", tableName, columnName, err)
	}
	if nullable != "NO" {
		t.Fatalf("expected %s.%s to be NOT NULL, got %q", tableName, columnName, nullable)
	}
}

func assertTranslatedObservabilityColumns(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	for _, tableName := range []string{"request_logs", "usage_request_events"} {
		assertColumnsExist(t, ctx, conn, tableName, "upstream_operation_name", "operation_translation_mode", "upstream_request_path")
	}
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

func seedOpenAIAcceptedFormatProfile(t *testing.T, ctx context.Context, conn *pgx.Conn) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ('accepted-format-profile', NULL, FALSE, FALSE, TRUE, 1, $1, $1) RETURNING id`, now).Scan(&profileID); err != nil {
		t.Fatalf("seed accepted-format profile: %v", err)
	}
	return profileID
}

func seedOpenAIAcceptedFormatModel(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, modelID string, apiFamily string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := conn.Exec(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $3, FALSE, $4, $4)`, profileID, apiFamily, modelID, now); err != nil {
		t.Fatalf("seed accepted-format model %q: %v", modelID, err)
	}
}

func assertOpenAIAcceptedFormatBackfillRows(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT model_id, openai_accepted_format FROM model_configs ORDER BY model_id ASC`)
	if err != nil {
		t.Fatalf("query accepted-format backfill rows: %v", err)
	}
	defer rows.Close()
	values := map[string]*string{}
	for rows.Next() {
		var modelID string
		var format sql.NullString
		if err := rows.Scan(&modelID, &format); err != nil {
			t.Fatalf("scan accepted-format backfill row: %v", err)
		}
		if format.Valid {
			value := format.String
			values[modelID] = &value
		} else {
			values[modelID] = nil
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate accepted-format backfill rows: %v", err)
	}
	if values["gpt-existing"] == nil || *values["gpt-existing"] != "dual_native" {
		t.Fatalf("expected OpenAI model to backfill dual_native, got %+v", values["gpt-existing"])
	}
	if values["claude-existing"] != nil {
		t.Fatalf("expected non-OpenAI model to keep null accepted format, got %q", *values["claude-existing"])
	}
}

func assertProfileAPIFamilyAuditSettingsDataConstraints(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ('audit-family-constraints', NULL, FALSE, FALSE, TRUE, 1, NULL, $1, $1) RETURNING id`, now).Scan(&profileID); err != nil {
		t.Fatalf("seed audit family profile: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, 'openai', TRUE, TRUE, $2, $2)`, profileID, now); err != nil {
		t.Fatalf("insert valid audit family setting: %v", err)
	}
	_, err := conn.Exec(ctx, `INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, 'openai', FALSE, FALSE, $2, $2)`, profileID, now)
	assertSQLConstraintError(t, err, "uq_profile_api_family_audit_settings_profile_family")
	_, err = conn.Exec(ctx, `INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, 'mistral', TRUE, FALSE, $2, $2)`, profileID, now)
	assertSQLConstraintError(t, err, "chk_profile_api_family_audit_settings_api_family")
	_, err = conn.Exec(ctx, `INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, 'anthropic', FALSE, TRUE, $2, $2)`, profileID, now)
	assertSQLConstraintError(t, err, "chk_profile_api_family_audit_settings_capture_requires_enabled")
	if _, err := conn.Exec(ctx, `DELETE FROM profiles WHERE id = $1`, profileID); err != nil {
		t.Fatalf("delete audit family profile for cascade check: %v", err)
	}
	var remaining int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM profile_api_family_audit_settings WHERE profile_id = $1`, profileID).Scan(&remaining); err != nil {
		t.Fatalf("count audit family rows after profile delete: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected profile delete to cascade audit family settings, got %d rows", remaining)
	}
}

func assertSQLConstraintError(t *testing.T, err error, constraintName string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected constraint %s to reject insert", constraintName)
	}
	if !strings.Contains(err.Error(), constraintName) {
		t.Fatalf("expected constraint error %s, got %v", constraintName, err)
	}
}

func dropModelAccessTargetConnectionOwnerIndex(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `DROP INDEX IF EXISTS uq_model_access_targets_connection_owner`); err != nil {
		t.Fatalf("drop model access target connection owner index: %v", err)
	}
}

func seedModelOwnershipConnection(t *testing.T, ctx context.Context, conn *pgx.Conn, label string) (int, int) {
	t.Helper()
	now := time.Now().UTC()

	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, NULL, $2, $2) RETURNING id`, "ownership-guard-"+label, now).Scan(&profileID); err != nil {
		t.Fatalf("seed ownership profile %q: %v", label, err)
	}

	var endpointID int
	if err := conn.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, 'plain-api-key', 0, $4, $4) RETURNING id`, profileID, "Ownership Guard Endpoint "+label, "https://ownership-guard-"+label+".invalid", now).Scan(&endpointID); err != nil {
		t.Fatalf("seed ownership endpoint %q: %v", label, err)
	}

	var connectionID int
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, $3, NULL, NULL, 'healthy', NULL, NULL, $4, $4) RETURNING id`, profileID, endpointID, "ownership-guard-"+label, now).Scan(&connectionID); err != nil {
		t.Fatalf("seed ownership connection %q: %v", label, err)
	}

	return profileID, connectionID
}

func seedModelAccessTargetConnectionOwner(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, connectionID int, modelID string) int {
	t.Helper()
	now := time.Now().UTC()

	var sourceModelConfigID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, 'dual_native', TRUE, $3, $3) RETURNING id`, profileID, modelID, now).Scan(&sourceModelConfigID); err != nil {
		t.Fatalf("seed ownership source model %q: %v", modelID, err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, sourceModelConfigID, connectionID, now); err != nil {
		t.Fatalf("seed ownership access target for model %q connection %d: %v", modelID, connectionID, err)
	}
	return sourceModelConfigID
}

func assertEndpointLabelSnapshotBackfillRows(t *testing.T, ctx context.Context, conn *pgx.Conn, expected map[string]string) {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT ingress_request_id, endpoint_label_snapshot
		FROM public.usage_request_events
		WHERE ingress_request_id = ANY($1::text[])
		ORDER BY ingress_request_id ASC`, sortedMapKeys(expected))
	if err != nil {
		t.Fatalf("load endpoint label snapshot backfill rows: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var ingressRequestID string
		var endpointLabelSnapshot string
		if err := rows.Scan(&ingressRequestID, &endpointLabelSnapshot); err != nil {
			t.Fatalf("scan endpoint label snapshot backfill row: %v", err)
		}
		got[ingressRequestID] = endpointLabelSnapshot
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate endpoint label snapshot backfill rows: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("expected endpoint label snapshot rows %+v, got %+v", expected, got)
	}
	for ingressRequestID, expectedLabel := range expected {
		label, ok := got[ingressRequestID]
		if !ok {
			t.Fatalf("missing endpoint label snapshot for ingress %q in %+v", ingressRequestID, got)
		}
		if label != expectedLabel {
			t.Fatalf("expected endpoint label snapshot for ingress %q to be %q, got %q", ingressRequestID, expectedLabel, label)
		}
		if strings.TrimSpace(label) == "" {
			t.Fatalf("expected non-empty endpoint label snapshot for ingress %q", ingressRequestID)
		}
	}
}

func insertStatsCoherenceRequestLog(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, ingressRequestID string, priced bool, unpricedReason *string, cost *int64, currencyCodeOriginal *string, reportCurrencyCode *string, fxRateUsed *string, fxRateSource *string, createdAt time.Time) {
	t.Helper()
	_, err := conn.Exec(ctx, `
		INSERT INTO request_logs (profile_id, ingress_request_id, model_id, api_family, status_code, success_flag, billable_flag, priced_flag, unpriced_reason, total_cost_user_currency_micros, currency_code_original, report_currency_code, fx_rate_used, fx_rate_source, response_time_ms, is_stream, request_path, created_at)
		VALUES ($1, $2, 'stats-coherence-model', 'openai', 200, TRUE, TRUE, $3, $4, $5, $6, $7, $8, $9, 100, FALSE, '/v1/chat/completions', $10)`,
		profileID, ingressRequestID, priced, nullableStringValue(unpricedReason), nullableInt64Value(cost), nullableStringValue(currencyCodeOriginal), nullableStringValue(reportCurrencyCode), nullableStringValue(fxRateUsed), nullableStringValue(fxRateSource), createdAt.UTC())
	if err != nil {
		t.Fatalf("insert stats coherence request log %q: %v", ingressRequestID, err)
	}
}

func insertStatsCoherenceUsageEvent(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, ingressRequestID string, priced bool, unpricedReason *string, cost *int64, currencyCodeOriginal *string, reportCurrencyCode *string, fxRateUsed *string, fxRateSource *string, createdAt time.Time) {
	t.Helper()
	_, err := conn.Exec(ctx, `
		INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, status_code, success_flag, billable_flag, priced_flag, unpriced_reason, total_cost_user_currency_micros, currency_code_original, report_currency_code, fx_rate_used, fx_rate_source, attempt_count, request_path, created_at, endpoint_label_snapshot)
		VALUES ($1, $2, 'stats-coherence-model', 'openai', 200, TRUE, TRUE, $3, $4, $5, $6, $7, $8, $9, 1, '/v1/chat/completions', $10, 'Stats Coherence Endpoint')`,
		profileID, ingressRequestID, priced, nullableStringValue(unpricedReason), nullableInt64Value(cost), nullableStringValue(currencyCodeOriginal), nullableStringValue(reportCurrencyCode), nullableStringValue(fxRateUsed), nullableStringValue(fxRateSource), createdAt.UTC())
	if err != nil {
		t.Fatalf("insert stats coherence usage event %q: %v", ingressRequestID, err)
	}
}

func assertStatsCoherenceRow(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, ingressRequestID string, wantPriced bool, wantReason string, wantFXRate *string, wantFXSource *string) {
	t.Helper()
	if tableName != "request_logs" && tableName != "usage_request_events" {
		t.Fatalf("unsupported stats coherence table %q", tableName)
	}
	var priced sql.NullBool
	var reason sql.NullString
	var fxRate sql.NullString
	var fxSource sql.NullString
	if err := conn.QueryRow(ctx, fmt.Sprintf(`SELECT priced_flag, unpriced_reason, fx_rate_used, fx_rate_source FROM public.%s WHERE ingress_request_id = $1`, tableName), ingressRequestID).Scan(&priced, &reason, &fxRate, &fxSource); err != nil {
		t.Fatalf("load %s stats coherence row %q: %v", tableName, ingressRequestID, err)
	}
	if !priced.Valid || priced.Bool != wantPriced {
		t.Fatalf("expected %s row %q priced=%t, got %+v", tableName, ingressRequestID, wantPriced, priced)
	}
	if wantReason == "" {
		if reason.Valid {
			t.Fatalf("expected %s row %q unpriced_reason=NULL, got %q", tableName, ingressRequestID, reason.String)
		}
	} else if !reason.Valid || reason.String != wantReason {
		t.Fatalf("expected %s row %q unpriced_reason=%q, got %+v", tableName, ingressRequestID, wantReason, reason)
	}
	assertNullableStringValue(t, tableName+"."+ingressRequestID+".fx_rate_used", fxRate, wantFXRate)
	assertNullableStringValue(t, tableName+"."+ingressRequestID+".fx_rate_source", fxSource, wantFXSource)
}

func assertNullableStringValue(t *testing.T, label string, got sql.NullString, want *string) {
	t.Helper()
	if want == nil {
		if got.Valid {
			t.Fatalf("expected %s=NULL, got %q", label, got.String)
		}
		return
	}
	if !got.Valid || got.String != *want {
		t.Fatalf("expected %s=%q, got %+v", label, *want, got)
	}
}

func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func assertManagementStatRollupTablesAbsent(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var count int
	tableNames := []string{"management_stat_" + "buckets", "management_stat_" + "refresh_state"}
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name = ANY($1::text[])`, tableNames).Scan(&count); err != nil {
		t.Fatalf("check management stat rollup tables: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected management stat rollup tables to be absent, got %d", count)
	}
}

type endpointLabelSnapshotFixture struct {
	expectedLabels map[string]string
	usagePartition string
}

func seedEndpointLabelSnapshotBackfillRows(t *testing.T, ctx context.Context, conn *pgx.Conn, label string, includeSnapshot bool) endpointLabelSnapshotFixture {
	t.Helper()
	createdAt := time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC)
	ensureDailyLogPartition(t, ctx, conn, "request_logs", createdAt, label)
	usagePartition := ensureDailyLogPartition(t, ctx, conn, "usage_request_events", createdAt, label)

	profileID := seedEndpointLabelSnapshotProfile(t, ctx, conn, label)
	endpointID := seedEndpointLabelSnapshotEndpoint(t, ctx, conn, profileID, "Current Endpoint "+label, "https://current-"+label+".invalid")
	blankNameEndpointID := seedEndpointLabelSnapshotEndpoint(t, ctx, conn, profileID, "   ", "https://blank-name-"+label+".invalid")
	missingEndpointID := 900000 + len(label)

	rankedIngress := label + "-ranked-desc"
	insertEndpointSnapshotRequestLog(t, ctx, conn, profileID, endpointID, rankedIngress, 1, createdAt.Add(time.Minute), "old description "+label, "https://old-"+label+".invalid")
	insertEndpointSnapshotRequestLog(t, ctx, conn, profileID, endpointID, rankedIngress, 2, createdAt.Add(3*time.Minute), "ranked description "+label, "https://ranked-"+label+".invalid")
	insertEndpointSnapshotRequestLog(t, ctx, conn, profileID, endpointID, rankedIngress, 2, createdAt.Add(2*time.Minute), "wrong latest "+label, "https://wrong-latest-"+label+".invalid")

	tieIngress := label + "-tie-base"
	tieCreatedAt := createdAt.Add(10 * time.Minute)
	insertEndpointSnapshotRequestLog(t, ctx, conn, profileID, endpointID, tieIngress, 3, tieCreatedAt, "wrong id "+label, "https://wrong-id-"+label+".invalid")
	insertEndpointSnapshotRequestLog(t, ctx, conn, profileID, endpointID, tieIngress, 3, tieCreatedAt, "", "https://tie-base-"+label+".invalid")

	currentEndpointIngress := label + "-current-name"
	insertEndpointSnapshotRequestLog(t, ctx, conn, profileID, endpointID, currentEndpointIngress, 1, createdAt.Add(20*time.Minute), "", "")

	endpointBaseIngress := label + "-endpoint-base"
	endpointIDIngress := label + "-endpoint-id"
	unknownIngress := label + "-unknown"

	insertEndpointSnapshotUsageEvent(t, ctx, conn, profileID, endpointID, rankedIngress, createdAt.Add(30*time.Minute), includeSnapshot)
	insertEndpointSnapshotUsageEvent(t, ctx, conn, profileID, endpointID, tieIngress, createdAt.Add(31*time.Minute), includeSnapshot)
	insertEndpointSnapshotUsageEvent(t, ctx, conn, profileID, endpointID, currentEndpointIngress, createdAt.Add(32*time.Minute), includeSnapshot)
	insertEndpointSnapshotUsageEvent(t, ctx, conn, profileID, blankNameEndpointID, endpointBaseIngress, createdAt.Add(33*time.Minute), includeSnapshot)
	insertEndpointSnapshotUsageEvent(t, ctx, conn, profileID, missingEndpointID, endpointIDIngress, createdAt.Add(34*time.Minute), includeSnapshot)
	insertEndpointSnapshotUsageEvent(t, ctx, conn, profileID, 0, unknownIngress, createdAt.Add(35*time.Minute), includeSnapshot)

	return endpointLabelSnapshotFixture{
		expectedLabels: map[string]string{
			rankedIngress:          "ranked description " + label,
			tieIngress:             "https://tie-base-" + label + ".invalid",
			currentEndpointIngress: "Current Endpoint " + label,
			endpointBaseIngress:    "https://blank-name-" + label + ".invalid",
			endpointIDIngress:      fmt.Sprintf("Endpoint %d", missingEndpointID),
			unknownIngress:         "Unknown Endpoint",
		},
		usagePartition: usagePartition,
	}
}

func seedEndpointLabelSnapshotProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, label string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, NULL, $2, $2) RETURNING id`, "endpoint-snapshot-"+label, now).Scan(&profileID); err != nil {
		t.Fatalf("seed endpoint snapshot profile %q: %v", label, err)
	}
	return profileID
}

func seedEndpointLabelSnapshotEndpoint(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, name string, baseURL string) int {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := conn.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, 'plain-api-key', 0, $4, $4) RETURNING id`, profileID, name, baseURL, now).Scan(&endpointID); err != nil {
		t.Fatalf("seed endpoint snapshot endpoint %q: %v", name, err)
	}
	return endpointID
}

func insertEndpointSnapshotRequestLog(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, endpointID int, ingressRequestID string, attemptNumber int, createdAt time.Time, endpointDescription string, endpointBaseURL string) {
	t.Helper()
	_, err := conn.Exec(ctx, `
		INSERT INTO request_logs (profile_id, model_id, api_family, endpoint_id, ingress_request_id, attempt_number, endpoint_base_url, status_code, response_time_ms, is_stream, request_path, endpoint_description, created_at)
		VALUES ($1, 'endpoint-snapshot-model', 'openai', $2, $3, $4, $5, 200, 100, FALSE, '/v1/chat/completions', $6, $7)`,
		profileID, endpointID, ingressRequestID, attemptNumber, endpointBaseURL, endpointDescription, createdAt)
	if err != nil {
		t.Fatalf("insert endpoint snapshot request log %q: %v", ingressRequestID, err)
	}
}

func insertEndpointSnapshotUsageEvent(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, endpointID int, ingressRequestID string, createdAt time.Time, includeSnapshot bool) {
	t.Helper()
	var endpointValue any
	if endpointID != 0 {
		endpointValue = endpointID
	}
	if includeSnapshot {
		_, err := conn.Exec(ctx, `
			INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_id, status_code, success_flag, billable_flag, priced_flag, attempt_count, request_path, created_at, endpoint_label_snapshot)
			VALUES ($1, $2, 'endpoint-snapshot-model', 'openai', $3, 200, TRUE, TRUE, TRUE, 1, '/v1/chat/completions', $4, 'pre-drop placeholder')`,
			profileID, ingressRequestID, endpointValue, createdAt)
		if err != nil {
			t.Fatalf("insert endpoint snapshot usage event %q with snapshot: %v", ingressRequestID, err)
		}
		return
	}
	_, err := conn.Exec(ctx, `
		INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_id, status_code, success_flag, billable_flag, priced_flag, attempt_count, request_path, created_at)
		VALUES ($1, $2, 'endpoint-snapshot-model', 'openai', $3, 200, TRUE, TRUE, TRUE, 1, '/v1/chat/completions', $4)`,
		profileID, ingressRequestID, endpointValue, createdAt)
	if err != nil {
		t.Fatalf("insert endpoint snapshot usage event %q: %v", ingressRequestID, err)
	}
}

func ensureDailyLogPartition(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, createdAt time.Time, label string) string {
	t.Helper()
	start := time.Date(createdAt.UTC().Year(), createdAt.UTC().Month(), createdAt.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	partitionName := fmt.Sprintf("%s_%s_%s", tableName, start.Format("20060102"), strings.ReplaceAll(label, "-", "_"))
	_, err := conn.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE public.%s PARTITION OF public.%s FOR VALUES FROM (%s) TO (%s)`,
		quoteIdentifier(partitionName),
		quoteIdentifier(tableName),
		quoteLiteral(start.Format(time.RFC3339)),
		quoteLiteral(end.Format(time.RFC3339)),
	))
	if err != nil {
		t.Fatalf("create %s daily partition %s: %v", tableName, partitionName, err)
	}
	return partitionName
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

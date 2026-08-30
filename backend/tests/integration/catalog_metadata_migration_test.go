package integrationtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestModelCatalogMetadataUpgradePreservesRetainedData proves both catalog
// migrations are additive: 000024 preserves retained model/pricing state,
func TestModelCatalogMetadataUpgradePreservesRetainedData(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "catalog_metadata_upgrade")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("run full migration set: %v", err)
	}

	// Simulate the pre-000024 shape: drop the new table/columns/indexes and
	// un-stamp the version.
	if _, err := conn.Exec(testContext, `DELETE FROM prism_schema_migrations WHERE version = '000024_model_catalog_metadata'`); err != nil {
		t.Fatalf("un-stamp 000024: %v", err)
	}
	if _, err := conn.Exec(testContext, `DROP TABLE model_catalog_bindings`); err != nil {
		t.Fatalf("drop bindings table: %v", err)
	}
	if _, err := conn.Exec(testContext, `DROP INDEX uq_pricing_templates_catalog_offering`); err != nil {
		t.Fatalf("drop offering index: %v", err)
	}
	if _, err := conn.Exec(testContext, `ALTER TABLE pricing_templates DROP COLUMN catalog_provider_id, DROP COLUMN catalog_model_id`); err != nil {
		t.Fatalf("drop template link columns: %v", err)
	}
	if _, err := conn.Exec(testContext, `ALTER TABLE pricing_template_revisions DROP COLUMN revision_source, DROP COLUMN catalog_revision`); err != nil {
		t.Fatalf("drop revision evidence columns: %v", err)
	}

	seedTx, err := conn.Begin(testContext)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	defer func() { _ = seedTx.Rollback(testContext) }()

	now := time.Now().UTC()
	var profileID int
	if err := seedTx.QueryRow(testContext, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ('catalog-upgrade', NULL, TRUE, TRUE, TRUE, 1, NULL, $1, $1) RETURNING id`, now).Scan(&profileID); err != nil {
		t.Fatalf("seed upgrade profile: %v", err)
	}
	var strategyID int
	if err := seedTx.QueryRow(testContext, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, 'Catalog Upgrade Strategy', 'fill-first', '{500}', 'off', 60000, 2.0, 0.2, 900000, 2, 0, 0, $2, $2) RETURNING id`, profileID, now).Scan(&strategyID); err != nil {
		t.Fatalf("seed upgrade strategy: %v", err)
	}
	var endpointID int
	if err := seedTx.QueryRow(testContext, `INSERT INTO endpoints (profile_id, name, base_url, api_key, created_at, updated_at) VALUES ($1, 'Catalog Upgrade Endpoint', 'https://upgrade.invalid', 'plain-api-key', $2, $2) RETURNING id`, profileID, now).Scan(&endpointID); err != nil {
		t.Fatalf("seed upgrade endpoint: %v", err)
	}
	var modelID int
	if err := seedTx.QueryRow(testContext, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, openai_accepted_format, created_at, updated_at) VALUES ($1, 'openai', 'upgrade-model', 'Upgrade Model', $2, TRUE, 'dual_native', $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&modelID); err != nil {
		t.Fatalf("seed upgrade model: %v", err)
	}
	var connectionID int
	if err := seedTx.QueryRow(testContext, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, 'Upgrade Connection', NULL, NULL, 'healthy', NULL, NULL, $3, $3) RETURNING id`, profileID, endpointID, now).Scan(&connectionID); err != nil {
		t.Fatalf("seed upgrade connection: %v", err)
	}
	var settingsEpoch int64
	if err := seedTx.QueryRow(testContext, `INSERT INTO reporting_currency_epochs (profile_id, epoch, currency_code, currency_symbol, effective_at, superseded_at, created_at, updated_at) VALUES ($1, 1, 'USD', '$', NULL, NULL, $2, $2) RETURNING id`, profileID, now).Scan(&settingsEpoch); err != nil {
		t.Fatalf("seed upgrade epoch: %v", err)
	}
	if _, err := seedTx.Exec(testContext, `INSERT INTO user_settings (profile_id, report_currency_code, report_currency_symbol, timezone_preference, current_reporting_currency_epoch_id, pricing_migration_state, pricing_template_generation, pricing_reference_generation, created_at, updated_at) VALUES ($1, 'USD', '$', NULL, $2, 'ready', 0, 0, $3, $3)`, profileID, settingsEpoch, now); err != nil {
		t.Fatalf("seed upgrade user settings: %v", err)
	}
	var templateID int
	var revisionID int64
	if err := seedTx.QueryRow(testContext, `INSERT INTO pricing_templates (profile_id, name, description, current_revision_id, created_at, updated_at) VALUES ($1, 'Upgrade Template', NULL, NULL, $2, $2) RETURNING id`, profileID, now).Scan(&templateID); err != nil {
		t.Fatalf("seed upgrade template: %v", err)
	}
	if _, err := seedTx.Exec(testContext, `INSERT INTO pricing_mutation_operation_reservations (operation_id, profile_id, intended_result_kind, normalized_identity_hash, created_at) VALUES ('0b95ed86-0000-4000-8000-000000000002', $1, 'template_create', 'prism-pricing-mutation:upgrade-template', $2)`, profileID, now); err != nil {
		t.Fatalf("seed upgrade operation reservation: %v", err)
	}
	if _, err := seedTx.Exec(testContext, `INSERT INTO pricing_mutation_operations (operation_id, profile_id, result_kind, normalized_payload_hash, preview_hash, operation_recorded_at, success_summary, result_hash, created_at) VALUES ('0b95ed86-0000-4000-8000-000000000002', $1, 'template_create', 'prism-pricing-mutation:upgrade-template', 'prism-pricing-mutation:upgrade-template', $2, '{"template_id":1}', 'prism-pricing-mutation:upgrade-template', $2)`, profileID, now); err != nil {
		t.Fatalf("seed upgrade mutation operation: %v", err)
	}
	if err := seedTx.QueryRow(testContext, `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, tier_input_tokens_above, effective_at, created_at, created_by_kind, created_by_operation_id) VALUES ($1, 7, 'PER_1M', 'USD', $2, 1, 'active_epoch', 'tiered', 200000, $3, $3, 'manual_edit', '0b95ed86-0000-4000-8000-000000000002') RETURNING id`, templateID, settingsEpoch, now).Scan(&revisionID); err != nil {
		t.Fatalf("seed upgrade revision: %v", err)
	}
	if _, err := seedTx.Exec(testContext, `INSERT INTO pricing_mutation_result_items (operation_id, ordinal, template_id, action, version, revision_id, template_name_snapshot) VALUES ('0b95ed86-0000-4000-8000-000000000002', 1, $1, 'revision_created', 7, $2, 'Upgrade Template')`, templateID, revisionID); err != nil {
		t.Fatalf("seed upgrade mutation result item: %v", err)
	}
	if _, err := seedTx.Exec(testContext, `UPDATE pricing_templates SET current_revision_id = $1 WHERE id = $2`, revisionID, templateID); err != nil {
		t.Fatalf("close seeded template pointer: %v", err)
	}
	if _, err := seedTx.Exec(testContext, `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price) VALUES ($1, 'tiered', 'tier_base', '3', '15'), ($1, 'tiered', 'tier_above', '6', '30')`, revisionID); err != nil {
		t.Fatalf("seed upgrade cards: %v", err)
	}
	if _, err := seedTx.Exec(testContext, `UPDATE connections SET pricing_template_id = $1 WHERE id = $2`, templateID, connectionID); err != nil {
		t.Fatalf("reference seeded template from terminal target: %v", err)
	}

	if err := seedTx.Commit(testContext); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}

	upgradeResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000024 upgrade: %v", err)
	}
	if upgradeResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected upgrade to apply 000024, got %q", upgradeResult.Outcome)
	}

	countMustEqual := func(query string, want int, args ...any) {
		t.Helper()
		var got int
		if err := conn.QueryRow(testContext, query, args...).Scan(&got); err != nil {
			t.Fatalf("count rows (%s): %v", query, err)
		}
		if got != want {
			t.Fatalf("%s = %d, want %d", query, got, want)
		}
	}

	// Every retained row survives: model identity, template pointer, both
	// revisions' versions, cards, and the Terminal Target reference.
	countMustEqual(`SELECT COUNT(*) FROM model_configs WHERE id = $1 AND model_id = 'upgrade-model' AND api_family = 'openai'`, 1, modelID)
	countMustEqual(`SELECT COUNT(*) FROM pricing_templates WHERE id = $1 AND current_revision_id = $2 AND name = 'Upgrade Template'`, 1, templateID, revisionID)
	countMustEqual(`SELECT COUNT(*) FROM pricing_template_revisions WHERE id = $1 AND version = 7 AND tier_input_tokens_above = 200000`, 1, revisionID)
	countMustEqual(`SELECT COUNT(*) FROM pricing_template_cards WHERE revision_id = $1`, 2, revisionID)
	countMustEqual(`SELECT COUNT(*) FROM connections WHERE id = $1 AND pricing_template_id = $2`, 1, connectionID, templateID)

	// Existing revisions read as manual history; the new columns are writable.
	var revisionSource string
	if err := conn.QueryRow(testContext, `SELECT revision_source FROM pricing_template_revisions WHERE id = $1`, revisionID).Scan(&revisionSource); err != nil {
		t.Fatalf("read upgraded revision source: %v", err)
	}
	if revisionSource != "manual" {
		t.Fatalf("retained revisions must default to manual source, got %q", revisionSource)
	}

	// The binding table is usable right after the upgrade without touching
	// the model's runtime identity columns.
	if _, err := conn.Exec(testContext, `INSERT INTO model_catalog_bindings (model_config_id, provider_id, catalog_model_id, match_source, catalog_revision, fetched_at, source_name, source_open_weights, updated_at) VALUES ($1, 'openai', 'upgrade-model', 'manual', '"catalog-upgrade-etag"', $2, 'Upgrade Model', FALSE, $2)`, modelID, now); err != nil {
		t.Fatalf("insert post-upgrade binding: %v", err)
	}
	var boundModelID int
	if err := conn.QueryRow(testContext, `SELECT model_config_id FROM model_catalog_bindings WHERE provider_id = 'openai' AND catalog_model_id = 'upgrade-model'`).Scan(&boundModelID); err != nil {
		t.Fatalf("read back post-upgrade binding: %v", err)
	}
	if boundModelID != modelID {
		t.Fatalf("binding points at wrong model: %d vs %d", boundModelID, modelID)
	}

	if _, err := conn.Exec(testContext, `DELETE FROM prism_schema_migrations WHERE version IN ('000027_model_pi_catalog_bindings', '000028_model_pi_binding_identity')`); err != nil {
		t.Fatalf("un-stamp Pi binding migrations: %v", err)
	}
	if _, err := conn.Exec(testContext, `DROP TABLE model_pi_catalog_bindings`); err != nil {
		t.Fatalf("drop Pi binding table: %v", err)
	}
	piUpgradeResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000027/000028 upgrade: %v", err)
	}
	if piUpgradeResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected upgrade to apply Pi binding migrations, got %q", piUpgradeResult.Outcome)
	}
	countMustEqual(`SELECT COUNT(*) FROM model_configs WHERE id = $1 AND model_id = 'upgrade-model' AND api_family = 'openai'`, 1, modelID)
	countMustEqual(`SELECT COUNT(*) FROM model_catalog_bindings WHERE model_config_id = $1 AND provider_id = 'openai' AND catalog_model_id = 'upgrade-model'`, 1, modelID)
	countMustEqual(`SELECT COUNT(*) FROM pricing_templates WHERE id = $1 AND current_revision_id = $2`, 1, templateID, revisionID)
	countMustEqual(`SELECT COUNT(*) FROM pricing_template_revisions WHERE id = $1 AND version = 7`, 1, revisionID)
	countMustEqual(`SELECT COUNT(*) FROM pricing_template_cards WHERE revision_id = $1`, 2, revisionID)

	if _, err := conn.Exec(testContext, `INSERT INTO model_pi_catalog_bindings (
		model_config_id, provider_id, catalog_model_id, api, prism_model_id_at_bind, bind_source, catalog_revision, fetched_at,
		source_name, source_dropped_fields, updated_at
	) VALUES ($1, 'openai', 'upgrade-model', 'openai-responses', 'upgrade-model', 'manual', 'sha256-upgrade-fixture', $2,
		'Upgrade Model', '["compat.allowedFallbackModels"]'::jsonb, $2)`, modelID, now); err != nil {
		t.Fatalf("insert post-upgrade Pi binding: %v", err)
	}
	countMustEqual(`SELECT COUNT(*) FROM model_pi_catalog_bindings WHERE model_config_id = $1 AND prism_model_id_at_bind = 'upgrade-model' AND source_dropped_fields = '["compat.allowedFallbackModels"]'::jsonb`, 1, modelID)

	// A second full run stays noop: the upgrade is idempotent.
	noopResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("re-run migrations: %v", err)
	}
	if noopResult.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected second run to be noop, got %q", noopResult.Outcome)
	}
}

func TestModelPiBindingIdentityUpgradePreservesRetainedData(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "pi_binding_identity_upgrade")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("run full migration set: %v", err)
	}

	if _, err := conn.Exec(testContext, `DELETE FROM prism_schema_migrations WHERE version = '000028_model_pi_binding_identity'`); err != nil {
		t.Fatalf("un-stamp 000028: %v", err)
	}
	if _, err := conn.Exec(testContext, `ALTER TABLE model_pi_catalog_bindings DROP COLUMN prism_model_id_at_bind`); err != nil {
		t.Fatalf("drop identity column: %v", err)
	}

	now := time.Now().UTC()
	var profileID, strategyID, modelA, modelB int
	if err := conn.QueryRow(testContext, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ('pi-identity', NULL, TRUE, TRUE, TRUE, 1, NULL, $1, $1) RETURNING id`, now).Scan(&profileID); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	if err := conn.QueryRow(testContext, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, 'Pi Identity Strategy', 'fill-first', '{500}', 'off', 60000, 2.0, 0.2, 900000, 2, 0, 0, $2, $2) RETURNING id`, profileID, now).Scan(&strategyID); err != nil {
		t.Fatalf("seed strategy: %v", err)
	}
	if err := conn.QueryRow(testContext, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, openai_accepted_format, created_at, updated_at) VALUES ($1, 'openai', 'same-id-model', 'Same', $2, TRUE, 'dual_native', $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&modelA); err != nil {
		t.Fatalf("seed model A: %v", err)
	}
	if err := conn.QueryRow(testContext, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, openai_accepted_format, created_at, updated_at) VALUES ($1, 'openai', 'renamed-model', 'Renamed', $2, TRUE, 'dual_native', $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&modelB); err != nil {
		t.Fatalf("seed model B: %v", err)
	}

	seedBinding := func(modelConfigID int, directoryID, name string, contextWindow int64, overrideName *string, revision string) {
		t.Helper()
		if _, err := conn.Exec(testContext, `INSERT INTO model_pi_catalog_bindings (
			model_config_id, provider_id, catalog_model_id, api, bind_source, catalog_revision, fetched_at,
			source_name, source_context_window, source_dropped_fields, override_name, updated_at
		) VALUES ($1, 'openai', $2, 'openai-responses', 'manual', $3, $4,
			$5, $6, '["headers"]'::jsonb, $7, $4)`,
			modelConfigID, directoryID, revision, now, name, contextWindow, overrideName); err != nil {
			t.Fatalf("seed Pi binding for model %d: %v", modelConfigID, err)
		}
	}
	seedBinding(modelA, "same-id-model", "Source A", 400000, nil, "sha256-a")
	seedBinding(modelB, "old-model-id", "Source B", 200000, ptrString("Operator Override"), "sha256-b")

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000028 upgrade: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected 000028 to apply, got %q", result.Outcome)
	}

	var count int
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM model_pi_catalog_bindings`).Scan(&count); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if count != 2 {
		t.Fatalf("every retained binding must survive the upgrade, got %d", count)
	}

	type snapshot struct {
		directoryID string
		prismAtBind string
		revision    string
		sourceName  string
		sourceCW    int64
		dropped     string
		override    *string
	}
	load := func(modelConfigID int) snapshot {
		t.Helper()
		var row snapshot
		if err := conn.QueryRow(testContext, `SELECT catalog_model_id, prism_model_id_at_bind, catalog_revision, source_name, source_context_window, source_dropped_fields::text, override_name FROM model_pi_catalog_bindings WHERE model_config_id = $1`, modelConfigID).Scan(
			&row.directoryID, &row.prismAtBind, &row.revision, &row.sourceName, &row.sourceCW, &row.dropped, &row.override,
		); err != nil {
			t.Fatalf("load binding for model %d: %v", modelConfigID, err)
		}
		return row
	}

	keptA := load(modelA)
	if keptA.prismAtBind != "same-id-model" || keptA.directoryID != "same-id-model" {
		t.Fatalf("healthy row must backfill to an identity match: %+v", keptA)
	}
	if keptA.sourceName != "Source A" || keptA.sourceCW != 400000 || keptA.revision != "sha256-a" || keptA.dropped != `["headers"]` {
		t.Fatalf("upgrade must not rewrite frozen source evidence: %+v", keptA)
	}

	keptB := load(modelB)
	if keptB.prismAtBind != "old-model-id" {
		t.Fatalf("drifted row must backfill from catalog_model_id, not from the current model id: %+v", keptB)
	}
	var currentPrismID string
	if err := conn.QueryRow(testContext, `SELECT model_id FROM model_configs WHERE id = $1`, modelB).Scan(&currentPrismID); err != nil {
		t.Fatalf("read model B identity: %v", err)
	}
	if keptB.prismAtBind == currentPrismID {
		t.Fatalf("backfill must not silently heal an existing rename drift: snapshot=%s current=%s", keptB.prismAtBind, currentPrismID)
	}
	if keptB.override == nil || *keptB.override != "Operator Override" {
		t.Fatalf("operator override must survive the upgrade: %+v", keptB)
	}

	if _, err := conn.Exec(testContext, `DELETE FROM model_pi_catalog_bindings WHERE model_config_id = $1`, modelA); err != nil {
		t.Fatalf("prepare constraint checks: %v", err)
	}
	_, missingSnapshotErr := conn.Exec(testContext, `INSERT INTO model_pi_catalog_bindings (
		model_config_id, provider_id, catalog_model_id, api, bind_source, catalog_revision, fetched_at, source_dropped_fields, updated_at
	) VALUES ($1, 'openai', 'no-snapshot', 'openai-responses', 'manual', 'sha256-c', $2, '[]'::jsonb, $2)`, modelA, now)
	var missingSnapshotPgErr *pgconn.PgError
	if !errors.As(missingSnapshotErr, &missingSnapshotPgErr) || missingSnapshotPgErr.Code != "23502" || missingSnapshotPgErr.ColumnName != "prism_model_id_at_bind" {
		t.Fatalf("missing prism_model_id_at_bind error = %v, want SQLSTATE 23502 on the snapshot column", missingSnapshotErr)
	}
	_, emptySnapshotErr := conn.Exec(testContext, `INSERT INTO model_pi_catalog_bindings (
		model_config_id, provider_id, catalog_model_id, api, prism_model_id_at_bind, bind_source, catalog_revision, fetched_at, source_dropped_fields, updated_at
	) VALUES ($1, 'openai', 'empty-snapshot', 'openai-responses', '', 'manual', 'sha256-c', $2, '[]'::jsonb, $2)`, modelA, now)
	var emptySnapshotPgErr *pgconn.PgError
	if !errors.As(emptySnapshotErr, &emptySnapshotPgErr) || emptySnapshotPgErr.Code != "23514" || emptySnapshotPgErr.ConstraintName != "ck_mpcb_prism_model_id_at_bind" {
		t.Fatalf("empty prism_model_id_at_bind error = %v, want ck_mpcb_prism_model_id_at_bind", emptySnapshotErr)
	}

	noopResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("re-run migrations: %v", err)
	}
	if noopResult.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected second run to be noop, got %q", noopResult.Outcome)
	}
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM model_pi_catalog_bindings`).Scan(&count); err != nil {
		t.Fatalf("recount bindings: %v", err)
	}
	if count != 1 {
		t.Fatalf("noop run must not change the remaining valid binding row, got %d", count)
	}
}

func ptrString(value string) *string { return &value }

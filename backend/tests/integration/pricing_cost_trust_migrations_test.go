package integrationtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

// ---------------------------------------------------------------------------
// Helpers: build a 000002-era database (000001 + 000002 only) so the real
// runner can later apply 000003 + 000004 as an existing-data upgrade.
// ---------------------------------------------------------------------------

func openPrism002EraDatabase(t *testing.T, ctx context.Context, harness postgresHarness, databaseName string) *pgx.Conn {
	t.Helper()
	conn := harness.openEmptyDatabase(t, ctx, databaseName)

	legacyDir := t.TempDir()
	for _, version := range []string{"000001_initial_schema.sql", "000002_connection_custom_request_parameters.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", version))
		if err != nil {
			t.Fatalf("read legacy migration %s: %v", version, err)
		}
		if err := os.WriteFile(filepath.Join(legacyDir, version), raw, 0o644); err != nil {
			t.Fatalf("stage legacy migration %s: %v", version, err)
		}
	}

	legacyRunner, err := migrate.New(migrate.Options{MigrationsDir: legacyDir})
	if err != nil {
		t.Fatalf("build 000002-era migration runner: %v", err)
	}
	result, err := legacyRunner.Run(ctx, conn)
	if err != nil {
		t.Fatalf("apply 000002-era migrations: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected 000002-era apply, got %q", result.Outcome)
	}
	assertHistoryVersions(t, ctx, conn, []string{
		migrate.DefaultBaselineVersion,
		"000002_connection_custom_request_parameters",
	})
	return conn
}

func seedPrism002EraProfileSettings(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, code string, symbol string) (int, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, TRUE, FALSE, TRUE, 1, NULL, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("seed 000002-era profile %q: %v", name, err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO user_settings (profile_id, report_currency_code, report_currency_symbol, timezone_preference, created_at, updated_at) VALUES ($1, $2, $3, NULL, $4, $4)`, profileID, code, symbol, now); err != nil {
		t.Fatalf("seed 000002-era user settings for profile %d: %v", profileID, err)
	}
	return profileID, now
}

func seedPrism002EraTemplate(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, name string, unit string, currency string, input string, output string, cached string, cacheCreation string, reasoning string, version int) int {
	t.Helper()
	now := time.Now().UTC()
	var id int
	if err := conn.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11) RETURNING id`, profileID, name, unit, currency, input, output, cached, cacheCreation, reasoning, version, now).Scan(&id); err != nil {
		t.Fatalf("seed 000002-era pricing template %q: %v", name, err)
	}
	return id
}

func seedPrism002EraFxRow(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, modelID string, endpointID int, fxRate string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := conn.Exec(ctx, `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)`, profileID, modelID, endpointID, fxRate, now); err != nil {
		t.Fatalf("seed 000002-era FX row: %v", err)
	}
}

func seedPrism002EraEndpointConnection(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, modelID string, templateID int) (int, int) {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := conn.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, 'plain-api-key', 0, $4, $4) RETURNING id`, profileID, "Upgrade Endpoint "+modelID, "https://upgrade-"+modelID+".invalid", now).Scan(&endpointID); err != nil {
		t.Fatalf("seed 000002-era endpoint: %v", err)
	}
	var connectionID int
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, $3, NULL, NULL, NULL, 'dual_native', TRUE, 0, $4, NULL, NULL, 'healthy', NULL, NULL, $5, $5) RETURNING id`, profileID, endpointID, templateID, "Upgrade Connection "+modelID, now).Scan(&connectionID); err != nil {
		t.Fatalf("seed 000002-era connection: %v", err)
	}
	var modelConfigID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, 'dual_native', TRUE, $3, $3) RETURNING id`, profileID, modelID, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("seed 000002-era model config: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, modelConfigID, connectionID, now); err != nil {
		t.Fatalf("seed 000002-era model access target: %v", err)
	}
	return endpointID, connectionID
}

func insertPricingUpgradeRequestLog(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, ingress string, statusCode int, inputCost *int64, outputCost *int64, reasoningCost *int64, cacheReadCost *int64, cacheCreationCost *int64, totalOriginal *int64, totalUser *int64, pricedFlag bool, snapshotUnit string, snapshotInput string, snapshotOutput string, reason string, createdAt time.Time) {
	t.Helper()
	var reasonValue any
	if reason != "" {
		reasonValue = reason
	}
	var unitValue any
	if snapshotUnit != "" {
		unitValue = snapshotUnit
	}
	var inputValue any
	if snapshotInput != "" {
		inputValue = snapshotInput
	}
	var outputValue any
	if snapshotOutput != "" {
		outputValue = snapshotOutput
	}
	_, err := conn.Exec(ctx, `
		INSERT INTO request_logs (
			profile_id, model_id, api_family, ingress_request_id, status_code,
			response_time_ms, is_stream, request_path, created_at,
			input_cost_micros, output_cost_micros, reasoning_cost_micros,
			cache_read_input_cost_micros, cache_creation_input_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros,
			priced_flag, unpriced_reason,
			pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output
		) VALUES ($1, 'upgrade-model', 'openai', $2, $3, 100, FALSE, '/v1/chat/completions', $4,
			$5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		profileID, ingress, statusCode, createdAt,
		inputCost, outputCost, reasoningCost, cacheReadCost, cacheCreationCost,
		totalOriginal, totalUser, pricedFlag, reasonValue, unitValue, inputValue, outputValue)
	if err != nil {
		t.Fatalf("insert pricing upgrade request log %q: %v", ingress, err)
	}
}

// ---------------------------------------------------------------------------
// Fresh install: singleton reaches final, canonical seed creates epoch 1 and
// a ready settings row in a single startup sequence (SPEC 11.1).
// ---------------------------------------------------------------------------

func TestPricingCostTrustFreshInstallFinalState(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openEmptyDatabase(t, testContext, "pricing_cost_trust_fresh")
	defer func() { _ = conn.Close(testContext) }()

	service, err := startup.New(startup.Options{
		DatabaseURL:         harness.connectionString("pricing_cost_trust_fresh"),
		SecretEncryptionKey: "test-secret-encryption-key",
		TimeNow:             func() time.Time { return time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	result, err := service.RunWithConn(testContext, conn)
	if err != nil {
		t.Fatalf("run fresh startup (migrations + seeds): %v", err)
	}
	if result.Migration.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected fresh migrations to apply, got %q", result.Migration.Outcome)
	}
	expectedVersions := expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion)
	assertMigrationVersions(t, "fresh applied versions", result.Migration.Versions, expectedVersions)

	assertPricingTransitionFinal(t, testContext, conn)
	assertFinalSchemaContract(t, testContext, conn)

	// Seed created the Default profile, epoch 1 and a ready settings row.
	var profileID int
	if err := conn.QueryRow(testContext, `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load seeded default profile: %v", err)
	}
	var epochID int64
	var epoch int
	var code string
	if err := conn.QueryRow(testContext, `SELECT id, epoch, currency_code FROM reporting_currency_epochs WHERE profile_id = $1`, profileID).Scan(&epochID, &epoch, &code); err != nil {
		t.Fatalf("load seeded epoch 1: %v", err)
	}
	if epoch != 1 || code != "USD" {
		t.Fatalf("expected epoch 1 USD, got epoch=%d code=%s", epoch, code)
	}
	var settingsEpoch int64
	var state string
	if err := conn.QueryRow(testContext, `SELECT current_reporting_currency_epoch_id, pricing_migration_state FROM user_settings WHERE profile_id = $1`, profileID).Scan(&settingsEpoch, &state); err != nil {
		t.Fatalf("load seeded settings: %v", err)
	}
	if settingsEpoch != epochID {
		t.Fatalf("expected settings pointer to epoch %d, got %d", epochID, settingsEpoch)
	}
	if state != "ready" {
		t.Fatalf("expected ready migration state, got %q", state)
	}
}

// ---------------------------------------------------------------------------
// Fail-closed upgrades: blocking legacy data rejects the whole migration with
// a typed error and the database stays untouched at the 000002 baseline.
// ---------------------------------------------------------------------------

func TestPricingCostTrustUpgradeFailClosed(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)

	t.Run("invalid_price_blocks", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(testContext, 2*time.Minute)
		defer cancel()
		conn := openPrism002EraDatabase(t, ctx, harness, "pricing_upgrade_fail_invalid_price")
		defer func() { _ = conn.Close(ctx) }()
		profileID, _ := seedPrism002EraProfileSettings(t, ctx, conn, "Invalid Price Profile", "USD", "$")
		seedPrism002EraTemplate(t, ctx, conn, profileID, "Broken", "PER_1M", "USD", "abc", "1", "0", "0", "0", 1)
		assertPricingUpgradeRejected(t, ctx, conn, "unresolved_inventory_count=1")
	})

	t.Run("foreign_currency_template_blocks", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(testContext, 2*time.Minute)
		defer cancel()
		conn := openPrism002EraDatabase(t, ctx, harness, "pricing_upgrade_fail_foreign")
		defer func() { _ = conn.Close(ctx) }()
		profileID, _ := seedPrism002EraProfileSettings(t, ctx, conn, "Foreign Profile", "USD", "$")
		seedPrism002EraTemplate(t, ctx, conn, profileID, "Euro", "PER_1M", "EUR", "3", "2", "0", "0", "0", 1)
		assertPricingUpgradeRejected(t, ctx, conn, "unresolved_inventory_count=1")
	})

	t.Run("invalid_reporting_currency_blocks", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(testContext, 2*time.Minute)
		defer cancel()
		conn := openPrism002EraDatabase(t, ctx, harness, "pricing_upgrade_fail_currency")
		defer func() { _ = conn.Close(ctx) }()
		seedPrism002EraProfileSettings(t, ctx, conn, "Invalid Currency Profile", "US", "$")
		assertPricingUpgradeRejected(t, ctx, conn, "unresolved_inventory_count=1")
	})

	t.Run("invalid_http_status_blocks_quarantine", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(testContext, 2*time.Minute)
		defer cancel()
		conn := openPrism002EraDatabase(t, ctx, harness, "pricing_upgrade_fail_quarantine")
		defer func() { _ = conn.Close(ctx) }()
		profileID, _ := seedPrism002EraProfileSettings(t, ctx, conn, "Quarantine Profile", "USD", "$")
		createdAt := time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC)
		seedPrism002EraRequestLogPartition(t, ctx, conn, createdAt)
		insertPricingUpgradeRequestLog(t, ctx, conn, profileID, "bad-status", 0, nil, nil, nil, nil, nil, nil, nil, false, "", "", "", "", createdAt)
		assertPricingUpgradeRejected(t, ctx, conn, "unresolved_quarantine_count=1")
	})

	t.Run("live_fx_dependency_blocks", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(testContext, 2*time.Minute)
		defer cancel()
		conn := openPrism002EraDatabase(t, ctx, harness, "pricing_upgrade_fail_live_fx")
		defer func() { _ = conn.Close(ctx) }()
		profileID, _ := seedPrism002EraProfileSettings(t, ctx, conn, "Live FX Profile", "USD", "$")
		foreignTemplate := seedPrism002EraTemplate(t, ctx, conn, profileID, "Euro Live", "PER_1M", "EUR", "3", "2", "0", "0", "0", 1)
		endpointID, _ := seedPrism002EraEndpointConnection(t, ctx, conn, profileID, "live-fx-model", foreignTemplate)
		seedPrism002EraFxRow(t, ctx, conn, profileID, "live-fx-model", endpointID, "1.25")
		assertPricingUpgradeRejected(t, ctx, conn, "000009 rejected")
	})
}

func assertPricingUpgradeRejected(t *testing.T, ctx context.Context, conn *pgx.Conn, expectedFragment string) {
	t.Helper()
	_, err := newRunner(t).Run(ctx, conn)
	if err == nil {
		t.Fatalf("expected pricing cost-trust upgrade to be rejected with %q", expectedFragment)
	}
	if !strings.Contains(err.Error(), expectedFragment) {
		t.Fatalf("expected rejection %q to contain %q, got: %v", err.Error(), expectedFragment, err)
	}
	// The whole upgrade rolled back: the database stays at the 000002 era.
	assertHistoryVersions(t, ctx, conn, []string{
		migrate.DefaultBaselineVersion,
		"000002_connection_custom_request_parameters",
	})
	assertColumnsExist(t, ctx, conn, "pricing_templates", "pricing_unit", "pricing_currency_code", "input_price")
	assertTablePresence(t, ctx, conn, "endpoint_fx_rate_settings", true)
}

// ---------------------------------------------------------------------------
// Final-schema invariants (fresh install, post-migration)
// ---------------------------------------------------------------------------

func TestPricingCostTrustFinalSchemaInvariants(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openEmptyDatabase(t, testContext, "pricing_cost_trust_invariants")
	defer func() { _ = conn.Close(testContext) }()

	service, err := startup.New(startup.Options{
		DatabaseURL:         harness.connectionString("pricing_cost_trust_invariants"),
		SecretEncryptionKey: "test-secret-encryption-key",
		TimeNow:             func() time.Time { return time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := service.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run fresh startup: %v", err)
	}

	var profileID int
	if err := conn.QueryRow(testContext, `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile: %v", err)
	}

	// Canonical create flow: reservation -> logical template -> operation
	// parent -> v1 revision -> result item -> pointer, all in ONE transaction
	// (deferred invariants close at commit; SPEC 6.1/6.2/6.3.1).
	now := time.Now().UTC()
	var templateID int
	var revisionID int64
	var operationID string
	tx, err := conn.Begin(testContext)
	if err != nil {
		t.Fatalf("begin create flow transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(testContext) }()
	if err := tx.QueryRow(testContext, `
		INSERT INTO pricing_templates (profile_id, name, description, current_revision_id, created_at, updated_at)
		VALUES ($1, '  Invariant Template  ', NULL, NULL, $2, $2)
		RETURNING id`, profileID, now).Scan(&templateID); err != nil {
		t.Fatalf("insert logical template: %v", err)
	}
	if err := tx.QueryRow(testContext, `SELECT gen_random_uuid()::text`).Scan(&operationID); err != nil {
		t.Fatalf("generate operation id: %v", err)
	}
	if _, err := tx.Exec(testContext, `
		INSERT INTO pricing_mutation_operation_reservations (
			operation_id, profile_id, intended_result_kind, normalized_identity_hash, created_at
		) VALUES ($1, $2, 'template_create', $3, $4)`,
		operationID, profileID, "invariant-create", now); err != nil {
		t.Fatalf("insert operation reservation: %v", err)
	}
	if _, err := tx.Exec(testContext, `
		INSERT INTO pricing_mutation_operations (
			operation_id, profile_id, result_kind, normalized_payload_hash,
			preview_hash, operation_recorded_at, success_summary, result_hash, created_at
		) VALUES ($1, $2, 'template_create', $3, $4, $5, $6, $7, $5)`,
		operationID, profileID, "payload-hash", "preview-hash", now,
		`{"template_id": `+fmt.Sprintf("%d", templateID)+`}`, "result-hash"); err != nil {
		t.Fatalf("insert operation parent: %v", err)
	}
	if err := tx.QueryRow(testContext, `
		INSERT INTO pricing_template_revisions (
			template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id,
			reporting_currency_epoch, currency_attribution, template_kind,
			effective_at, created_at, created_by_kind, created_by_operation_id
		) VALUES ($1, 1, 'PER_1M', 'USD', (SELECT current_reporting_currency_epoch_id FROM user_settings WHERE profile_id = $2),
			1, 'active_epoch', 'standard', $3, $3, 'manual_create', $4)
		RETURNING id`, templateID, profileID, now, operationID).Scan(&revisionID); err != nil {
		t.Fatalf("insert v1 revision: %v", err)
	}
	if _, err := tx.Exec(testContext, `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price) VALUES ($1, 'standard', 'standard', '1', '2', NULL, NULL, NULL)`, revisionID); err != nil {
		t.Fatalf("insert invariant standard card: %v", err)
	}
	if _, err := tx.Exec(testContext, `
		INSERT INTO pricing_mutation_result_items (
			operation_id, ordinal, template_id, action, version, revision_id,
			revision_effective_at, template_name_snapshot
		) VALUES ($1, 1, $2, 'created', 1, $3, $4, 'Invariant Template')`,
		operationID, templateID, revisionID, now); err != nil {
		t.Fatalf("insert operation result item: %v", err)
	}
	if _, err := tx.Exec(testContext, `UPDATE pricing_templates SET current_revision_id = $1, updated_at = $2 WHERE id = $3`, revisionID, now, templateID); err != nil {
		t.Fatalf("set current revision pointer: %v", err)
	}
	if err := tx.Commit(testContext); err != nil {
		t.Fatalf("commit create flow: %v", err)
	}

	// Canonical name materialized (outer whitespace trimmed, byte identity).
	var name string
	var nameIdentity []byte
	if err := conn.QueryRow(testContext, `SELECT name, name_identity FROM pricing_templates WHERE id = $1`, templateID).Scan(&name, &nameIdentity); err != nil {
		t.Fatalf("load materialized name identity: %v", err)
	}
	if name != "Invariant Template" {
		t.Fatalf("expected trimmed canonical name, got %q", name)
	}
	if string(nameIdentity) != "Invariant Template" {
		t.Fatalf("expected byte-exact name identity, got %q", string(nameIdentity))
	}

	// Append-only: revision UPDATE/DELETE rejected.
	if _, err := conn.Exec(testContext, `UPDATE pricing_template_revisions SET input_price = '9' WHERE id = $1`, revisionID); err == nil {
		t.Fatal("expected revision UPDATE to be rejected")
	}
	if _, err := conn.Exec(testContext, `DELETE FROM pricing_template_revisions WHERE id = $1`, revisionID); err == nil {
		t.Fatal("expected revision DELETE to be rejected")
	}

	// Epoch immutability: identity fields cannot change; DELETE rejected.
	if _, err := conn.Exec(testContext, `DELETE FROM reporting_currency_epochs WHERE profile_id = $1`, profileID); err == nil {
		t.Fatal("expected epoch DELETE to be rejected")
	}
	if _, err := conn.Exec(testContext, `UPDATE reporting_currency_epochs SET currency_code = 'EUR' WHERE profile_id = $1`, profileID); err == nil {
		t.Fatal("expected epoch identity UPDATE to be rejected")
	}

	// Settings/epoch divergence rejected by the deferred coherence trigger.
	if _, err := conn.Exec(testContext, `UPDATE user_settings SET report_currency_code = 'EUR' WHERE profile_id = $1`, profileID); err == nil {
		t.Fatal("expected settings currency divergence from active epoch to be rejected")
	}

	// Template name invalid (control character) rejected.
	if _, err := conn.Exec(testContext, `INSERT INTO pricing_templates (profile_id, name, created_at, updated_at) VALUES ($1, E'bad\x01name', $2, $2)`, profileID, now); err == nil {
		t.Fatal("expected invalid template name to be rejected")
	}

	// Soft-delete frees the name identity for reuse (partial unique).
	if _, err := conn.Exec(testContext, `UPDATE pricing_templates SET deleted_at = $1, updated_at = $2 WHERE id = $3`, now, now, templateID); err != nil {
		t.Fatalf("soft-delete template: %v", err)
	}
	var reusedID int
	if err := conn.QueryRow(testContext, `
		INSERT INTO pricing_templates (profile_id, name, description, current_revision_id, created_at, updated_at)
		VALUES ($1, 'Invariant Template', NULL, NULL, $2, $2)
		RETURNING id`, profileID, now).Scan(&reusedID); err != nil {
		t.Fatalf("reuse name after soft-delete: %v", err)
	}
	if reusedID <= 0 {
		t.Fatal("expected positive reused template id")
	}

	// New runtime partition created through the production logretention
	// ensurer inherits pricing columns and attaches the pricing index shape
	// (SPEC 6.5 future-partition template).
	pool, err := pgxpool.New(testContext, harness.connectionString("pricing_cost_trust_invariants"))
	if err != nil {
		t.Fatalf("build logretention pool: %v", err)
	}
	defer pool.Close()
	retentionStore := logretention.NewStore(logretention.Options{Pool: pool})
	partitionDay := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	if err := retentionStore.EnsurePartitionForTime(testContext, "request_logs", partitionDay); err != nil {
		t.Fatalf("ensure runtime request_logs partition: %v", err)
	}
	partitionName := "request_logs_p20260810"
	assertColumnNotNull(t, testContext, conn, partitionName, "pricing_status")
	assertIndexPresence(t, testContext, conn, partitionName, partitionName+"_pricing_status_idx", true)
	assertIndexPresence(t, testContext, conn, partitionName, partitionName+"_unpriced_reason_idx", true)
	assertIndexPresence(t, testContext, conn, partitionName, partitionName+"_epoch_idx", true)
	// Partition rows satisfy the pricing CHECKs enforced on the parent.
	if _, err := conn.Exec(testContext, `
		INSERT INTO request_logs (
			profile_id, model_id, api_family, ingress_request_id, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms,
			response_time_ms, is_stream, request_path, pricing_status,
			pricing_evidence_trust, input_cost_micros, output_cost_micros,
			reasoning_cost_micros, cache_read_input_cost_micros,
			cache_creation_input_cost_micros, total_cost_original_micros,
			total_cost_user_currency_micros, created_at
		) VALUES ($1, 'invariant-model', 'openai', 'invariant-ingress', 'upstream', 'runtime_scrubbed', 200, 100, 100, FALSE,
			'/v1/chat/completions', 'priced', 'trusted', 0, 0, 0, 0, 0, 0, 0, $2)`,
		profileID, partitionDay); err != nil {
		t.Fatalf("insert priced row into runtime partition: %v", err)
	}
	if _, err := conn.Exec(testContext, `
		INSERT INTO request_logs (
			profile_id, model_id, api_family, ingress_request_id, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms,
			response_time_ms, is_stream, request_path, pricing_status,
			pricing_evidence_trust, created_at
		) VALUES ($1, 'invariant-model', 'openai', 'invariant-bad', 'upstream', 'runtime_scrubbed', 200, 100, 100, FALSE,
			'/v1/chat/completions', 'unpriced', 'trusted', $2)`,
		profileID, partitionDay); err == nil {
		t.Fatal("expected unpriced row without a reason to be rejected")
	}
}

func assertPricingTransitionFinal(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var rowCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pricing_schema_transition_state`).Scan(&rowCount); err != nil {
		t.Fatalf("count pricing schema transition rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly one pricing schema transition row, got %d", rowCount)
	}
	var phase string
	var acquisitionOpen bool
	var token int64
	if err := conn.QueryRow(ctx, `SELECT phase, lease_acquisition_open, finalizer_fencing_token FROM pricing_schema_transition_state WHERE id = 1`).Scan(&phase, &acquisitionOpen, &token); err != nil {
		t.Fatalf("load pricing schema transition singleton: %v", err)
	}
	if phase != "final" {
		t.Fatalf("expected phase=final, got %q", phase)
	}
	if acquisitionOpen {
		t.Fatal("expected lease acquisition closed")
	}
	if token < 1 {
		t.Fatalf("expected a finalizer fencing token >= 1, got %d", token)
	}
	var activeLeases int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pricing_schema_generation_leases WHERE released_at IS NULL`).Scan(&activeLeases); err != nil {
		t.Fatalf("count active generation leases: %v", err)
	}
	if activeLeases != 0 {
		t.Fatalf("expected zero active generation leases, got %d", activeLeases)
	}
}

func assertFinalSchemaContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	// Legacy surfaces are hard-deleted.
	assertColumnsAbsent(t, ctx, conn, "pricing_templates", "pricing_unit", "pricing_currency_code", "input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price", "version")
	assertColumnsAbsent(t, ctx, conn, "user_settings", "pricing_report_currency_code_v2", "pricing_report_currency_symbol_v2")
	assertTablePresence(t, ctx, conn, "endpoint_fx_rate_settings", false)
	assertColumnsAbsent(t, ctx, conn, "request_logs", "billable_flag", "priced_flag")
	assertColumnsAbsent(t, ctx, conn, "usage_request_events", "billable_flag", "priced_flag")
	// Canonical surfaces present.
	assertColumnsExist(t, ctx, conn, "user_settings", "report_currency_code", "report_currency_symbol", "current_reporting_currency_epoch_id", "pricing_migration_state", "pricing_template_generation", "pricing_reference_generation")
	for _, tableName := range []string{"request_logs", "usage_request_events"} {
		assertColumnNotNull(t, ctx, conn, tableName, "pricing_status")
		assertColumnNotNull(t, ctx, conn, tableName, "pricing_evidence_trust")
		assertColumnsExist(t, ctx, conn, tableName, "pricing_status", "unpriced_reason", "pricing_resolution_kind", "pricing_evidence_trust", "missing_price_components", "pricing_template_id_used", "pricing_template_name_snapshot", "pricing_template_revision_id_used", "pricing_version_effective_at", "reporting_currency_epoch")
	}
	assertColumnsExist(t, ctx, conn, "pricing_templates", "current_revision_id", "deleted_at", "name_identity")
	assertTablePresence(t, ctx, conn, "pricing_template_revisions", true)
	assertTablePresence(t, ctx, conn, "reporting_currency_epochs", true)
	assertTablePresence(t, ctx, conn, "pricing_schema_transition_state", true)
	assertTablePresence(t, ctx, conn, "pricing_migration_inventories", true)
	assertTablePresence(t, ctx, conn, "currency_migration_ledger", true)
}

func seedPrism002EraRequestLogPartition(t *testing.T, ctx context.Context, conn *pgx.Conn, createdAt time.Time) string {
	t.Helper()
	return ensureDailyLogPartition(t, ctx, conn, "request_logs", createdAt, "pricing_upgrade")
}

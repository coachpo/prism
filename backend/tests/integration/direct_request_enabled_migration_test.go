package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestDirectRequestEnabledMigrationPreservesExistingRows proves the generic
// 000032 upgrade only adds the non-null entry bit and defaults retained rows to
// true. Instance-specific reclassification is intentionally outside this
// migration and therefore cannot alter model, target, connection, or history
// rows while the upgrade runs.
func TestDirectRequestEnabledMigrationPreservesExistingRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	pre032Runner := newMigrationRunnerBefore(t, "000032")

	conn := harness.openEmptyDatabase(t, ctx, "direct-request-enabled-upgrade")
	defer func() { _ = conn.Close(ctx) }()
	assertMigrationApplied(t, ctx, conn, pre032Runner, "through 000031")

	profileID := seedMigrationProfile(t, ctx, conn, "direct-request-enabled", time.Now().UTC())
	strategyID := seedDirectRequestStrategy(t, ctx, conn, profileID)
	modelID, updatedAt := seedDirectRequestModel(t, ctx, conn, profileID, strategyID)
	historyAt := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	ensureDailyLogPartition(t, ctx, conn, "request_logs", historyAt, "direct_request_upgrade")
	ensureDailyLogPartition(t, ctx, conn, "usage_request_events", historyAt, "direct_request_upgrade")
	endpointID := seedEndpointForUpstreamHistory(t, ctx, conn, profileID, "Direct Request Upgrade Endpoint")
	connectionID := seedConnectionForUpstreamHistory(t, ctx, conn, profileID, endpointID, historyAt)
	seedOwnerForUpstreamHistory(t, ctx, conn, profileID, modelID, connectionID, 0, historyAt)
	seedRowsForUpstreamHistory(t, ctx, conn, profileID, endpointID, connectionID, historyAt)
	execUpstreamFixture(t, ctx, conn, "seed models.dev binding", `INSERT INTO model_catalog_bindings (model_config_id, provider_id, catalog_model_id, match_source, catalog_revision, fetched_at, source_name, updated_at) VALUES ($1, 'openai', 'direct-request-retained', 'manual', 'models-revision', $2, 'Retained models.dev name', $2)`, modelID, historyAt)
	execUpstreamFixture(t, ctx, conn, "seed pi binding", `INSERT INTO model_pi_catalog_bindings (model_config_id, provider_id, catalog_model_id, api, bind_source, catalog_revision, fetched_at, source_name, prism_model_id_at_bind, updated_at) VALUES ($1, 'openai', 'direct-request-retained', 'openai-responses', 'manual', 'pi-revision', $2, 'Retained pi.dev name', 'direct-request-retained', $2)`, modelID, historyAt)

	before := loadDirectRequestMigrationState(t, ctx, conn, modelID, connectionID)

	assertMigrationApplied(t, ctx, conn, runner, "through 000032")
	after := loadDirectRequestMigrationState(t, ctx, conn, modelID, connectionID)
	if before != after {
		t.Fatalf("000032 changed retained model-adjacent data:\nbefore=%+v\nafter=%+v", before, after)
	}
	var direct bool
	var afterUpdatedAt time.Time
	if err := conn.QueryRow(ctx, `SELECT direct_request_enabled, updated_at FROM model_configs WHERE id = $1`, modelID).Scan(&direct, &afterUpdatedAt); err != nil {
		t.Fatalf("read migrated model: %v", err)
	}
	if !direct {
		t.Fatal("expected retained model to default to direct_request_enabled=true")
	}
	if !afterUpdatedAt.Equal(updatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("migration rewrote model updated_at: before=%s after=%s", updatedAt, afterUpdatedAt)
	}
}

type directRequestMigrationState struct {
	ModelWithoutDirectBit string
	AccessTarget          string
	Connection            string
	RequestLog            string
	UsageEvent            string
	ModelsDevBinding      string
	PiBinding             string
}

func loadDirectRequestMigrationState(t *testing.T, ctx context.Context, conn *pgx.Conn, modelID int, connectionID int) directRequestMigrationState {
	t.Helper()
	return directRequestMigrationState{
		ModelWithoutDirectBit: loadDirectRequestMigrationJSON(t, ctx, conn, `SELECT (to_jsonb(row_value) - 'direct_request_enabled')::text FROM model_configs AS row_value WHERE id = $1`, modelID),
		AccessTarget:          loadDirectRequestMigrationJSON(t, ctx, conn, `SELECT to_jsonb(row_value)::text FROM model_access_targets AS row_value WHERE source_model_config_id = $1 AND target_connection_id = $2`, modelID, connectionID),
		Connection:            loadDirectRequestMigrationJSON(t, ctx, conn, `SELECT to_jsonb(row_value)::text FROM connections AS row_value WHERE id = $1`, connectionID),
		RequestLog:            loadDirectRequestMigrationJSON(t, ctx, conn, `SELECT to_jsonb(row_value)::text FROM request_logs AS row_value WHERE ingress_request_id = 'upstream-history-ingress'`),
		UsageEvent:            loadDirectRequestMigrationJSON(t, ctx, conn, `SELECT to_jsonb(row_value)::text FROM usage_request_events AS row_value WHERE ingress_request_id = 'upstream-history-ingress'`),
		ModelsDevBinding:      loadDirectRequestMigrationJSON(t, ctx, conn, `SELECT to_jsonb(row_value)::text FROM model_catalog_bindings AS row_value WHERE model_config_id = $1`, modelID),
		PiBinding:             loadDirectRequestMigrationJSON(t, ctx, conn, `SELECT to_jsonb(row_value)::text FROM model_pi_catalog_bindings AS row_value WHERE model_config_id = $1`, modelID),
	}
}

func loadDirectRequestMigrationJSON(t *testing.T, ctx context.Context, conn *pgx.Conn, query string, args ...any) string {
	t.Helper()
	var value string
	if err := conn.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		t.Fatalf("load direct-request migration state: %v", err)
	}
	return value
}

func TestDirectRequestEnabledFreshSchemaDefaultsTrue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, ctx, "direct-request-enabled-fresh")
	defer func() { _ = conn.Close(ctx) }()
	assertMigrationApplied(t, ctx, conn, runner, "fresh through 000032")

	var columnDefault *string
	var nullable string
	if err := conn.QueryRow(ctx, `SELECT column_default, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'model_configs' AND column_name = 'direct_request_enabled'`).Scan(&columnDefault, &nullable); err != nil {
		t.Fatalf("read fresh direct-request schema: %v", err)
	}
	if columnDefault == nil || *columnDefault != "true" || nullable != "NO" {
		t.Fatalf("expected fresh direct_request_enabled default true and NOT NULL, default=%v nullable=%s", columnDefault, nullable)
	}
}

func seedDirectRequestStrategy(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int) int {
	t.Helper()
	var strategyID int
	now := time.Now().UTC()
	if err := conn.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, 'direct-request-test', 'single', ARRAY[500]::integer[], 'until_reset', 1, 2, 0, 100, 1, 1, 0, $2, $2) RETURNING id`, profileID, now).Scan(&strategyID); err != nil {
		t.Fatalf("seed direct-request strategy: %v", err)
	}
	return strategyID
}

func seedDirectRequestModel(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, strategyID int) (int, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	var modelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'direct-request-retained', $2, 'dual_native', FALSE, $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&modelID); err != nil {
		t.Fatalf("seed direct-request model: %v", err)
	}
	return modelID, now
}

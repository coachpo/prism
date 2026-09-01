package integrationtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

// TestUpstreamModelIDDecouplingUpgradePath proves the 000031 upstream model id
// migration is additive and data-preserving on a copy of a pre-000031
// database: owned connections are backfilled exactly from their owner model's
// current model_id, orphan connections without an owner edge keep NULL, the
// nullable snapshots on request_logs and usage_request_events stay NULL for
// retained rows (no history rewrite). Legacy outbox materialization belongs to
// the runtime outbox regression, where a real worker consumes a valid v2 core
// payload whose envelope predates this field.
func TestUpstreamModelIDDecouplingUpgradePath(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	runner := newRunner(t)
	pre031Runner := newMigrationRunnerBefore(t, "000031")

	t.Run("backfills owners without rewriting history", func(t *testing.T) {
		conn := harness.openEmptyDatabase(t, testContext, "upstream_model_id_upgrade")
		defer func() { _ = conn.Close(testContext) }()
		assertMigrationApplied(t, testContext, conn, pre031Runner, "through 000030")

		historyAt := time.Date(2026, 2, 10, 8, 0, 0, 0, time.UTC)
		partitions := []string{
			ensureDailyLogPartition(t, testContext, conn, "request_logs", historyAt, "upstream-history"),
			ensureDailyLogPartition(t, testContext, conn, "usage_request_events", historyAt, "upstream-history"),
		}
		profileID := seedMigrationProfile(t, testContext, conn, "upstream-history", historyAt)
		ownerID := seedModelForUpstreamHistory(t, testContext, conn, profileID, "upstream-history-owner")
		endpointID := seedEndpointForUpstreamHistory(t, testContext, conn, profileID, "upstream-history-endpoint")
		ownedID := seedConnectionForUpstreamHistory(t, testContext, conn, profileID, endpointID, historyAt)
		orphanID := seedConnectionForUpstreamHistory(t, testContext, conn, profileID, endpointID, historyAt)
		seedOwnerForUpstreamHistory(t, testContext, conn, profileID, ownerID, ownedID, 0, historyAt)
		seedRowsForUpstreamHistory(t, testContext, conn, profileID, endpointID, ownedID, historyAt)
		before := loadUpstreamMigrationCounts(t, testContext, conn, profileID)

		for _, table := range []string{"connections", "request_logs", "usage_request_events"} {
			assertColumnsAbsent(t, testContext, conn, table, "upstream_model_id")
		}
		assertMigrationApplied(t, testContext, conn, runner, "000031")
		assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))
		if after := loadUpstreamMigrationCounts(t, testContext, conn, profileID); after != before {
			t.Fatalf("000031 changed retained rows: before=%+v after=%+v", before, after)
		}
		assertUpstreamModelIDStored(t, testContext, conn, ownedID, "upstream-history-owner")
		assertUpstreamModelIDStored(t, testContext, conn, orphanID, "")
		execUpstreamFixture(t, testContext, conn, "rename owner model", `UPDATE model_configs SET model_id = 'upstream-history-owner-renamed' WHERE id = $1`, ownerID)
		assertUpstreamModelIDStored(t, testContext, conn, ownedID, "upstream-history-owner")
		for _, partition := range partitions {
			assertHistoricalUpstreamModelIDNull(t, testContext, conn, partition)
		}

		blanks := []struct{ name, value string }{
			{name: "ascii-control", value: "\t\n"}, {name: "no-break-space", value: "\u00a0"},
			{name: "ogham-space-mark", value: "\u1680"}, {name: "figure-space", value: "\u2007"},
			{name: "narrow-no-break-space", value: "\u202f"},
		}
		for _, table := range []string{"connections", "request_logs", "usage_request_events"} {
			for _, blank := range blanks {
				_, err := conn.Exec(testContext, `UPDATE `+quoteIdentifier(table)+` SET upstream_model_id = $2 WHERE profile_id = $1`, profileID, blank.value)
				if constraint := "ck_" + table + "_upstream_model_id_blank"; err == nil || !strings.Contains(err.Error(), constraint) {
					t.Fatalf("expected %s to reject %s upstream_model_id, got %v", constraint, blank.name, err)
				}
			}
		}
	})

	t.Run("rejects multiple owners before changing schema", func(t *testing.T) {
		conn := harness.openEmptyDatabase(t, testContext, "upstream_model_id_multiple_owners")
		defer func() { _ = conn.Close(testContext) }()
		assertMigrationApplied(t, testContext, conn, pre031Runner, "through 000030")
		now := time.Date(2026, 2, 11, 8, 0, 0, 0, time.UTC)
		profileID := seedMigrationProfile(t, testContext, conn, "upstream-multiple-owners", now)
		endpointID := seedEndpointForUpstreamHistory(t, testContext, conn, profileID, "upstream-multiple-owner-endpoint")
		connectionID := seedConnectionForUpstreamHistory(t, testContext, conn, profileID, endpointID, now)
		execUpstreamFixture(t, testContext, conn, "drop owner uniqueness", `DROP INDEX public.uq_model_access_targets_connection_owner`)
		for position, modelID := range []int{
			seedModelForUpstreamHistory(t, testContext, conn, profileID, "upstream-multiple-owner-a"),
			seedModelForUpstreamHistory(t, testContext, conn, profileID, "upstream-multiple-owner-b"),
		} {
			seedOwnerForUpstreamHistory(t, testContext, conn, profileID, modelID, connectionID, position, now)
		}

		result, err := runner.Run(testContext, conn)
		if err == nil || !strings.Contains(err.Error(), "requires unique connection owners") {
			t.Fatalf("expected readable multiple-owner failure, result=%+v err=%v", result, err)
		}
		for _, table := range []string{"connections", "request_logs", "usage_request_events"} {
			assertColumnsAbsent(t, testContext, conn, table, "upstream_model_id")
		}
		var recorded bool
		if err := conn.QueryRow(testContext, `SELECT EXISTS (SELECT 1 FROM prism_schema_migrations WHERE version = '000031_terminal_target_upstream_model_identity')`).Scan(&recorded); err != nil {
			t.Fatalf("check failed migration history: %v", err)
		}
		if recorded {
			t.Fatal("failed 000031 migration was recorded")
		}
	})
}

type upstreamMigrationCounts struct {
	models, endpoints, connections, accessTargets, requestLogs, usageEvents int
}

func loadUpstreamMigrationCounts(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int) upstreamMigrationCounts {
	t.Helper()
	counts := upstreamMigrationCounts{}
	err := conn.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM model_configs WHERE profile_id = $1),
		(SELECT COUNT(*) FROM endpoints WHERE profile_id = $1),
		(SELECT COUNT(*) FROM connections WHERE profile_id = $1),
		(SELECT COUNT(*) FROM model_access_targets WHERE profile_id = $1),
		(SELECT COUNT(*) FROM request_logs WHERE profile_id = $1),
		(SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1)`, profileID).Scan(
		&counts.models, &counts.endpoints, &counts.connections, &counts.accessTargets,
		&counts.requestLogs, &counts.usageEvents,
	)
	if err != nil {
		t.Fatalf("load retained upstream migration counts: %v", err)
	}
	return counts
}

func assertMigrationApplied(t *testing.T, ctx context.Context, conn *pgx.Conn, runner migrate.Runner, label string) {
	t.Helper()
	if result, err := runner.Run(ctx, conn); err != nil || result.Outcome != migrate.OutcomeApply {
		t.Fatalf("apply migrations %s: result=%+v err=%v", label, result, err)
	}
}

func assertUpstreamModelIDStored(t *testing.T, ctx context.Context, conn *pgx.Conn, connectionID int, want string) {
	t.Helper()
	var stored *string
	if err := conn.QueryRow(ctx, `SELECT upstream_model_id FROM connections WHERE id = $1`, connectionID).Scan(&stored); err != nil {
		t.Fatalf("load connection %d upstream_model_id: %v", connectionID, err)
	}
	if want == "" {
		if stored != nil {
			t.Fatalf("expected NULL upstream_model_id for connection %d, got %q", connectionID, *stored)
		}
		return
	}
	if stored == nil || *stored != want {
		t.Fatalf("expected upstream_model_id %q for connection %d, got %+v", want, connectionID, stored)
	}
}

func assertHistoricalUpstreamModelIDNull(t *testing.T, ctx context.Context, conn *pgx.Conn, partition string) {
	t.Helper()
	var got *string
	if err := conn.QueryRow(ctx, `SELECT upstream_model_id FROM `+quoteIdentifier(partition)+` WHERE ingress_request_id = 'upstream-history-ingress'`).Scan(&got); err != nil {
		t.Fatalf("load historical snapshot from %s: %v", partition, err)
	}
	if got != nil {
		t.Fatalf("historical snapshot in %s was rewritten: %q", partition, *got)
	}
}

func seedModelForUpstreamHistory(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, modelID string) int {
	t.Helper()
	var modelConfigID int
	if err := conn.QueryRow(ctx, `
		INSERT INTO model_configs (profile_id, api_family, model_id, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at)
		VALUES ($1, 'openai', $2, NULL, 'dual_native', TRUE, $3, $3) RETURNING id`,
		profileID, modelID, time.Date(2026, 2, 10, 7, 0, 0, 0, time.UTC)).Scan(&modelConfigID); err != nil {
		t.Fatalf("seed model %s: %v", modelID, err)
	}
	return modelConfigID
}

func seedEndpointForUpstreamHistory(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, name string) int {
	t.Helper()
	var endpointID int
	if err := conn.QueryRow(ctx, `
		INSERT INTO endpoints (profile_id, name, base_url, api_key, config_revision, created_at, updated_at)
		VALUES ($1, $2, $3, 'upstream-history-key', 1, $4, $4) RETURNING id`,
		profileID, name, "https://upstream-history-endpoint.invalid", time.Date(2026, 2, 10, 7, 0, 0, 0, time.UTC)).Scan(&endpointID); err != nil {
		t.Fatalf("seed endpoint %s: %v", name, err)
	}
	return endpointID
}

func seedConnectionForUpstreamHistory(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID, endpointID int, at time.Time) int {
	t.Helper()
	var connectionID int
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, openai_text_capability, is_active, priority, health_status, monitoring_probe_interval_seconds, created_at, updated_at) VALUES ($1, 'openai', $2, 'dual_native', TRUE, 0, 'unknown', 300, $3, $3) RETURNING id`, profileID, endpointID, at).Scan(&connectionID); err != nil {
		t.Fatalf("seed upstream history connection: %v", err)
	}
	return connectionID
}

func seedOwnerForUpstreamHistory(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID, modelID, connectionID, position int, at time.Time) {
	t.Helper()
	execUpstreamFixture(t, ctx, conn, "seed owner edge", `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, TRUE, $5, $5)`, profileID, modelID, connectionID, position, at)
}

func seedRowsForUpstreamHistory(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID, endpointID, connectionID int, at time.Time) {
	t.Helper()
	execUpstreamFixture(t, ctx, conn, "seed historical request log", `INSERT INTO request_logs (profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, row_kind, url_scrub_provenance, legacy_status_code, legacy_duration_ms, is_stream, pricing_status, pricing_evidence_trust, request_path, created_at) VALUES ($1, 'upstream-history-public', 'upstream-history-owner', 'openai', $2, $3, 'upstream-history-ingress', 'legacy_unknown', 'legacy_unknown', 200, 900, TRUE, 'unknown', 'legacy_untrusted', '/v1/chat/completions', $4)`, profileID, endpointID, connectionID, at)
	execUpstreamFixture(t, ctx, conn, "seed historical usage event", `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, endpoint_label_snapshot, status_code, success_flag, pricing_status, pricing_evidence_trust, attempt_count, request_path, created_at) VALUES ($1, 'upstream-history-ingress', 'upstream-history-public', 'upstream-history-owner', 'openai', $2, $3, 'History Endpoint', 200, TRUE, 'unknown', 'legacy_untrusted', 1, '/v1/chat/completions', $4)`, profileID, endpointID, connectionID, at)
}

func execUpstreamFixture(t *testing.T, ctx context.Context, conn *pgx.Conn, label, query string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(ctx, query, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

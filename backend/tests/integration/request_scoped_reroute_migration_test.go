package integrationtest

import (
	"context"
	"testing"
	"time"
)

// TestRequestScopedRerouteMigrationPreservesStrategiesAndHistory proves 000035
// only adds the reroute set and widens the trigger constraints: retained
// strategy fields and request history are unchanged, every strategy receives
// {400} unless it already fails over on 400, and the upgraded schema matches
// the fresh one.
func TestRequestScopedRerouteMigrationPreservesStrategiesAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	const databaseName = "request-scoped-reroute-upgrade"
	conn := harness.openEmptyDatabase(t, ctx, databaseName)
	defer func() { _ = conn.Close(ctx) }()
	assertMigrationApplied(t, ctx, conn, newMigrationRunnerBefore(t, "000035"), "through 000034")

	historyAt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	profileID := seedMigrationProfile(t, ctx, conn, "request-scoped-reroute", historyAt)
	wantReroute := map[string]string{"retained-without-400": "{400}", "retained-failover-400": "{}"}
	for name, failure := range map[string]string{"retained-without-400": "{500,503}", "retained-failover-400": "{400,503}"} {
		execUpstreamFixture(t, ctx, conn, "seed retained strategy", `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'fill-first', $3::integer[], 'temporary', 1000, 2, 0.1, 60000, 2, 4, 30, $4, $4)`, profileID, name, failure, historyAt)
	}
	partitions := []string{
		ensureDailyLogPartition(t, ctx, conn, "request_logs", historyAt, "request_scoped_reroute"),
		ensureDailyLogPartition(t, ctx, conn, "usage_request_events", historyAt, "request_scoped_reroute"),
	}
	endpointID := seedEndpointForUpstreamHistory(t, ctx, conn, profileID, "Request Scoped Reroute Endpoint")
	seedRowsForUpstreamHistory(t, ctx, conn, profileID, endpointID, seedConnectionForUpstreamHistory(t, ctx, conn, profileID, endpointID, historyAt), historyAt)

	state := func() [3]string {
		var value [3]string
		for index, query := range []string{
			`SELECT jsonb_agg(to_jsonb(row_value) - 'reroute_status_codes' ORDER BY id)::text FROM loadbalance_strategies AS row_value WHERE profile_id = $1`,
			`SELECT jsonb_agg(to_jsonb(row_value) ORDER BY id)::text FROM request_logs AS row_value WHERE profile_id = $1`,
			`SELECT jsonb_agg(to_jsonb(row_value) ORDER BY id)::text FROM usage_request_events AS row_value WHERE profile_id = $1`,
		} {
			if err := conn.QueryRow(ctx, query, profileID).Scan(&value[index]); err != nil {
				t.Fatalf("load retained state: %v", err)
			}
		}
		return value
	}
	before := state()
	assertMigrationApplied(t, ctx, conn, newRunner(t), "through 000035")
	if after := state(); after != before {
		t.Fatalf("000035 changed retained rows:\nbefore=%v\nafter=%v", before, after)
	}
	for name, want := range wantReroute {
		var got string
		if err := conn.QueryRow(ctx, `SELECT reroute_status_codes::text FROM loadbalance_strategies WHERE profile_id = $1 AND name = $2`, profileID, name).Scan(&got); err != nil || got != want {
			t.Fatalf("strategy %q reroute_status_codes = %q (err %v), want %q", name, got, err, want)
		}
	}
	// The seeded history partitions are the only objects a fresh database lacks.
	for _, partition := range partitions {
		execUpstreamFixture(t, ctx, conn, "drop seeded history partition", `DROP TABLE public.`+quoteIdentifier(partition))
	}
	assertSchemaMatchesGolden(t, migratedSchemaDump(t, ctx, harness, databaseName))
}

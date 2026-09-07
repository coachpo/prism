package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestRetentionCoverageTriggerIsStatementScoped proves 000034 moved the
// append path of the coverage trigger to statement level with the same
// dirty/bounds semantics, and left UPDATE/DELETE at row level. The split is
// the contract: appends are the hot path and always go through the
// partitioned parent, while retention deletes boundary rows straight from a
// child partition, which a parent-only statement trigger would never see.
func TestRetentionCoverageTriggerIsStatementScoped(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	conn := harness.openDatabase(t, testContext, "retention_coverage_statement_trigger")
	defer func() { _ = conn.Close(testContext) }()

	// tgtype bit 0 distinguishes row-level from statement-level triggers,
	// bit 2 marks INSERT. Four datasets: one statement-level append trigger
	// and one row-level update/delete trigger each, the latter cloned onto
	// every partition.
	var statementAppendTriggers, rowUpdateDeleteTriggers int
	if err := conn.QueryRow(testContext, `SELECT
			COUNT(*) FILTER (WHERE (tgtype & 1) = 0 AND (tgtype & 4) = 4),
			COUNT(*) FILTER (WHERE (tgtype & 1) = 1 AND (tgtype & 4) = 0)
		FROM pg_trigger JOIN pg_class ON pg_class.oid = pg_trigger.tgrelid
		WHERE NOT tgisinternal AND tgname LIKE '%retention_coverage_dirty%'
		  AND pg_class.relkind = 'p'`).Scan(&statementAppendTriggers, &rowUpdateDeleteTriggers); err != nil {
		t.Fatalf("inspect coverage trigger scope: %v", err)
	}
	if statementAppendTriggers != 4 {
		t.Fatalf("expected four statement-level append triggers, found %d", statementAppendTriggers)
	}
	if rowUpdateDeleteTriggers != 4 {
		t.Fatalf("expected four row-level update/delete triggers, found %d", rowUpdateDeleteTriggers)
	}
	var profileID int
	if err := conn.QueryRow(testContext, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at)
		VALUES ('coverage-trigger', NULL, TRUE, TRUE, TRUE, 1, NULL, NOW(), NOW()) RETURNING id`).Scan(&profileID); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	base := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	for _, table := range []string{"request_logs", "usage_request_events"} {
		if _, err := conn.Exec(testContext, `CREATE TABLE IF NOT EXISTS `+table+`_p20260826 PARTITION OF public.`+table+`
			FOR VALUES FROM ('2026-08-26 00:00:00+00') TO ('2026-08-27 00:00:00+00')`); err != nil {
			t.Fatalf("create %s partition: %v", table, err)
		}
	}

	// The row-level trigger has to reach the partitions, because retention
	// deletes boundary rows from a child table directly.
	var partitionTriggers int
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM pg_trigger
		JOIN pg_class ON pg_class.oid = pg_trigger.tgrelid
		WHERE NOT tgisinternal AND tgname LIKE '%retention_coverage_dirty'
		  AND pg_class.relname LIKE 'request_logs_p%'`).Scan(&partitionTriggers); err != nil {
		t.Fatalf("inspect partition coverage triggers: %v", err)
	}
	if partitionTriggers == 0 {
		t.Fatalf("expected the row-level coverage trigger to be cloned onto request_logs partitions")
	}

	// A clean owner: an append must set the dirty bit without inventing
	// bounds, because the owning writer resolves them.
	if _, err := conn.Exec(testContext, `UPDATE retention_coverage_read_models
		SET dirty = FALSE, complete = TRUE, freshness = 'fresh',
		    earliest_retained_at = NULL, latest_retained_at = NULL
		WHERE dataset = 'request_logs'`); err != nil {
		t.Fatalf("seed clean coverage owner: %v", err)
	}
	insertRetainedRows(t, testContext, conn, profileID, base, 5)

	var dirty, complete bool
	var freshness string
	var earliest, latest *time.Time
	if err := conn.QueryRow(testContext, `SELECT dirty, complete, freshness, earliest_retained_at, latest_retained_at
		FROM retention_coverage_read_models WHERE dataset = 'request_logs'`).Scan(&dirty, &complete, &freshness, &earliest, &latest); err != nil {
		t.Fatalf("read coverage owner after clean append: %v", err)
	}
	if !dirty {
		t.Fatalf("expected an append to mark the coverage projection dirty")
	}
	if earliest != nil || latest != nil {
		t.Fatalf("expected a clean owner's bounds to survive the append handoff, got %v..%v", earliest, latest)
	}
	if freshness != "fresh" {
		t.Fatalf("expected a clean owner's freshness to survive the append handoff, got %q", freshness)
	}

	// A dirty owner left behind by another transaction: the next statement
	// extends the bounds across the whole batch, not just its first row.
	if _, err := conn.Exec(testContext, `UPDATE retention_coverage_read_models
		SET dirty = TRUE, freshness = 'stale', earliest_retained_at = NULL, latest_retained_at = NULL
		WHERE dataset = 'request_logs'`); err != nil {
		t.Fatalf("seed dirty coverage owner: %v", err)
	}
	batchBase := base.Add(2 * time.Hour)
	insertRetainedRows(t, testContext, conn, profileID, batchBase, 5)
	if err := conn.QueryRow(testContext, `SELECT earliest_retained_at, latest_retained_at
		FROM retention_coverage_read_models WHERE dataset = 'request_logs'`).Scan(&earliest, &latest); err != nil {
		t.Fatalf("read coverage owner after dirty append: %v", err)
	}
	if earliest == nil || latest == nil {
		t.Fatalf("expected a dirty owner to take the batch bounds, got %v..%v", earliest, latest)
	}
	wantEarliest := batchBase
	wantLatest := batchBase.Add(4 * time.Minute)
	if !earliest.UTC().Equal(wantEarliest) || !latest.UTC().Equal(wantLatest) {
		t.Fatalf("expected batch bounds %s..%s, got %s..%s", wantEarliest, wantLatest, earliest.UTC(), latest.UTC())
	}

	// An append statement that touches nothing is not a mutation.
	if _, err := conn.Exec(testContext, `UPDATE retention_coverage_read_models
		SET dirty = FALSE, freshness = 'fresh' WHERE dataset = 'request_logs'`); err != nil {
		t.Fatalf("reset coverage owner: %v", err)
	}
	if _, err := conn.Exec(testContext, `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind,
			url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag,
			pricing_status, pricing_evidence_trust, attempt_trigger, attempt_result, is_winner, request_path, created_at)
		SELECT $1, 'coverage-model', 'openai', 'never', 1, 'upstream', 'runtime_scrubbed', 200, 10, FALSE, TRUE,
			'ineligible', 'trusted', 'initial', 'completed', TRUE, '/v1/chat/completions', $2
		WHERE FALSE`, profileID, base); err != nil {
		t.Fatalf("run zero-row append: %v", err)
	}
	if err := conn.QueryRow(testContext, `SELECT dirty FROM retention_coverage_read_models WHERE dataset = 'request_logs'`).Scan(&dirty); err != nil {
		t.Fatalf("read coverage owner after zero-row statement: %v", err)
	}
	if dirty {
		t.Fatalf("expected a zero-row append to leave the coverage projection clean")
	}

	// A delete straight from a child partition — how retention purges the
	// boundary day — must still mark the projection dirty.
	if _, err := conn.Exec(testContext, `UPDATE retention_coverage_read_models
		SET dirty = FALSE, freshness = 'fresh' WHERE dataset = 'request_logs'`); err != nil {
		t.Fatalf("reset coverage owner: %v", err)
	}
	if _, err := conn.Exec(testContext, `DELETE FROM request_logs_p20260826 WHERE created_at < $1`, base.Add(time.Minute)); err != nil {
		t.Fatalf("delete boundary rows from the partition: %v", err)
	}
	if err := conn.QueryRow(testContext, `SELECT dirty FROM retention_coverage_read_models WHERE dataset = 'request_logs'`).Scan(&dirty); err != nil {
		t.Fatalf("read coverage owner after partition delete: %v", err)
	}
	if !dirty {
		t.Fatalf("expected a partition-scoped delete to mark the coverage projection dirty")
	}
}

// insertRetainedRows appends count retained rows one minute apart in a single
// statement, so the trigger sees them as one batch.
func insertRetainedRows(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, base time.Time, count int) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO request_logs (
			profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind,
			url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag,
			pricing_status, pricing_evidence_trust, attempt_trigger, attempt_result, is_winner,
			request_path, created_at)
		SELECT $1, 'coverage-model', 'openai', 'cov-' || extract(epoch from $2::timestamptz)::bigint::text || '-' || n::text, 1, 'upstream',
			'runtime_scrubbed', 200, 10, FALSE, TRUE, 'ineligible', 'trusted', 'initial', 'completed', TRUE,
			'/v1/chat/completions', $2::timestamptz + ((n - 1) * interval '1 minute')
		FROM generate_series(1, $3) n`, profileID, base, count); err != nil {
		t.Fatalf("append %d retained rows: %v", count, err)
	}
}

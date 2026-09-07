package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

// Indexes 000033 removes from the retained-history parents.
var retainedHistoryIndexesDropped = map[string]string{
	"idx_request_logs_ingress_chain":      "CREATE INDEX idx_request_logs_ingress_chain ON public.request_logs USING btree (profile_id, ingress_request_id, attempt_number, created_at, id)",
	"idx_request_logs_ingress_request_id": "CREATE INDEX idx_request_logs_ingress_request_id ON public.request_logs USING btree (ingress_request_id)",
	"ix_request_logs_api_family":          "CREATE INDEX ix_request_logs_api_family ON public.request_logs USING btree (api_family)",
	"ix_usage_request_events_api_family":  "CREATE INDEX ix_usage_request_events_api_family ON public.usage_request_events USING btree (api_family)",
	"ix_usage_request_events_created_at":  "CREATE INDEX ix_usage_request_events_created_at ON public.usage_request_events USING btree (created_at)",
}

// Indexes that must survive: each still owns a query shape no other index
// leads with, so a slimming pass must not take them along.
var retainedHistoryIndexesRetained = []string{
	"idx_request_logs_ingress_created_id",
	"idx_request_logs_profile_created_totals",
	"ix_request_logs_id",
	"ix_request_logs_endpoint_id",
	"ix_request_logs_connection_id",
	"ix_request_logs_model_id",
	"idx_usage_request_events_profile_created_at",
	"idx_usage_request_events_profile_ingress_id",
	"ix_usage_request_events_id",
	"ix_usage_request_events_endpoint_id",
}

// TestRetainedHistoryIndexSlimmingRemovesUnreachableIndexes proves 000033
// removes the five unreachable indexes from a database that still carries
// them, leaves the covering set intact, and propagates the drop to existing
// child partitions.
func TestRetainedHistoryIndexSlimmingRemovesUnreachableIndexes(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "retained_history_index_slimming")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("run full migration set: %v", err)
	}

	// Roll back to the pre-000033 index shape, then re-run the runner so only
	// this migration's upgrade path executes.
	if _, err := conn.Exec(testContext, `DELETE FROM prism_schema_migrations WHERE version = '000033_retained_history_index_slimming'`); err != nil {
		t.Fatalf("un-stamp 000033: %v", err)
	}
	for name, ddl := range retainedHistoryIndexesDropped {
		if _, err := conn.Exec(testContext, ddl); err != nil {
			t.Fatalf("recreate dropped index %s: %v", name, err)
		}
	}
	// A child partition created before the upgrade must lose the inherited
	// indexes with its parent.
	const requestChild = "request_logs_p20260826"
	if _, err := conn.Exec(testContext, `CREATE TABLE `+requestChild+` PARTITION OF public.request_logs
		FOR VALUES FROM ('2026-08-26 00:00:00+00') TO ('2026-08-27 00:00:00+00')`); err != nil {
		t.Fatalf("create request_logs test partition: %v", err)
	}

	upgradeResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000033 upgrade: %v", err)
	}
	if upgradeResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected upgrade to apply 000033, got %q", upgradeResult.Outcome)
	}

	for name := range retainedHistoryIndexesDropped {
		var exists bool
		if err := conn.QueryRow(testContext, `SELECT EXISTS (SELECT 1 FROM pg_class WHERE relname = $1 AND relkind IN ('i','I'))`, name).Scan(&exists); err != nil {
			t.Fatalf("check index %s absence: %v", name, err)
		}
		if exists {
			t.Fatalf("expected index %s to be dropped", name)
		}
	}
	for _, name := range retainedHistoryIndexesRetained {
		var valid bool
		if err := conn.QueryRow(testContext, `SELECT i.indisvalid FROM pg_index i
			JOIN pg_class c ON c.oid = i.indexrelid
			WHERE c.relname = $1 AND c.relkind = 'I'`, name).Scan(&valid); err != nil {
			t.Fatalf("expected index %s to survive: %v", name, err)
		}
		if !valid {
			t.Fatalf("expected surviving index %s to be valid", name)
		}
	}

	// No index on the pre-upgrade child may still descend from a dropped
	// parent index.
	var orphanedChildIndexes int
	if err := conn.QueryRow(testContext, `SELECT COUNT(*)
		FROM pg_index i
		JOIN pg_class tbl ON tbl.oid = i.indrelid
		JOIN pg_inherits inh ON inh.inhrelid = i.indexrelid
		JOIN pg_class root_idx ON root_idx.oid = inh.inhparent
		WHERE tbl.relname = $1 AND root_idx.relname = ANY($2)`,
		requestChild, []string{"idx_request_logs_ingress_chain", "idx_request_logs_ingress_request_id", "ix_request_logs_api_family"}).Scan(&orphanedChildIndexes); err != nil {
		t.Fatalf("check child index ancestry: %v", err)
	}
	if orphanedChildIndexes != 0 {
		t.Fatalf("expected dropped parent indexes to take their child indexes with them, found %d", orphanedChildIndexes)
	}
}

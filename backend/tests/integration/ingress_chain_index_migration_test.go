package integrationtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

// Ingress-chain read-path index set introduced by
// 000025_ingress_chain_query_indexes.
var ingressChainIndexesCreated = []string{
	"idx_request_logs_ingress_created_id",
	"idx_request_logs_profile_created_totals",
	"idx_usage_request_events_profile_ingress_id",
}

// Indexes the same migration removes from the partitioned parents.
var ingressChainIndexesDropped = []string{
	"idx_request_logs_profile_created_at",
	"ix_request_logs_status_code",
	"idx_usage_request_events_profile_ingress_request",
	"idx_usage_request_events_ingress_request_id",
	"ix_usage_request_events_profile_id",
}

// TestIngressChainQueryIndexUpgradeSwapsIndexSet proves 000025 applies to a
// database still carrying the pre-000025 index shape, leaves no retained row
// behind, and that every created index is valid on both the partitioned
// parent and existing child partitions while the five replaced indexes are
// gone from parents and children alike.
func TestIngressChainQueryIndexUpgradeSwapsIndexSet(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "ingress_chain_indexes")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("run full migration set: %v", err)
	}

	// Roll the schema back to the pre-000025 index shape, then re-run the
	// runner so only this migration's upgrade path executes.
	if _, err := conn.Exec(testContext, `DELETE FROM prism_schema_migrations WHERE version = '000025_ingress_chain_query_indexes'`); err != nil {
		t.Fatalf("un-stamp 000025: %v", err)
	}
	for _, indexName := range ingressChainIndexesCreated {
		if _, err := conn.Exec(testContext, `DROP INDEX IF EXISTS public.`+indexName); err != nil {
			t.Fatalf("drop new index %s: %v", indexName, err)
		}
	}
	recreatedOld := map[string]string{
		"idx_request_logs_profile_created_at":              "CREATE INDEX idx_request_logs_profile_created_at ON public.request_logs USING btree (profile_id, created_at)",
		"ix_request_logs_status_code":                      "CREATE INDEX ix_request_logs_status_code ON public.request_logs USING btree (status_code)",
		"idx_usage_request_events_profile_ingress_request": "CREATE INDEX idx_usage_request_events_profile_ingress_request ON public.usage_request_events USING btree (profile_id, ingress_request_id)",
		"idx_usage_request_events_ingress_request_id":      "CREATE INDEX idx_usage_request_events_ingress_request_id ON public.usage_request_events USING btree (ingress_request_id)",
		"ix_usage_request_events_profile_id":               "CREATE INDEX ix_usage_request_events_profile_id ON public.usage_request_events USING btree (profile_id)",
	}
	for _, ddl := range recreatedOld {
		if _, err := conn.Exec(testContext, ddl); err != nil {
			t.Fatalf("recreate old index (%s): %v", ddl, err)
		}
	}

	upgradeResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000025 upgrade: %v", err)
	}
	if upgradeResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected upgrade to apply 000025, got %q", upgradeResult.Outcome)
	}

	assertIndexState := func(relkindFilter string, wantPresent []string, wantAbsent []string) {
		t.Helper()
		for _, indexName := range wantPresent {
			var valid bool
			if err := conn.QueryRow(testContext, `SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
				WHERE c.relname = $1 AND `+relkindFilter, indexName).Scan(&valid); err != nil {
				t.Fatalf("expected index %s to exist: %v", indexName, err)
			}
			if !valid {
				t.Fatalf("expected index %s to be valid", indexName)
			}
		}
		for _, indexName := range wantAbsent {
			var exists bool
			if err := conn.QueryRow(testContext, `SELECT EXISTS (SELECT 1 FROM pg_class c WHERE c.relname = $1 AND c.relkind = 'i')`, indexName).Scan(&exists); err != nil {
				t.Fatalf("check index %s absence: %v", indexName, err)
			}
			if exists {
				t.Fatalf("expected index %s to be dropped", indexName)
			}
		}
	}

	// Parent-level state (partitioned parents declare relkind 'I').
	assertIndexState(`c.relkind = 'I'`, ingressChainIndexesCreated, append([]string(nil), ingressChainIndexesDropped...))

	// Child partitions are created at runtime (not by migrations). Create one
	// daily child per managed table the way the runtime partition ensurer
	// does, then prove the parent swap propagated to it in both directions.
	requestChild, usageChild := "request_logs_p20260826", "usage_request_events_p20260826"
	if _, err := conn.Exec(testContext, `CREATE TABLE `+requestChild+` PARTITION OF public.request_logs
		FOR VALUES FROM ('2026-08-26 00:00:00+00') TO ('2026-08-27 00:00:00+00')`); err != nil {
		t.Fatalf("create request_logs test partition: %v", err)
	}
	if _, err := conn.Exec(testContext, `CREATE TABLE `+usageChild+` PARTITION OF public.usage_request_events
		FOR VALUES FROM ('2026-08-26 00:00:00+00') TO ('2026-08-27 00:00:00+00')`); err != nil {
		t.Fatalf("create usage_request_events test partition: %v", err)
	}
	// Child indexes inherit from the partitioned parent index through
	// pg_inherits on index OIDs and receive generated names, so ancestry —
	// not the child name — proves the swap propagated.
	indexChildState := func(childTable string, indexName string) (exists bool, valid bool, err error) {
		err = conn.QueryRow(testContext, `SELECT COUNT(*) > 0, COALESCE(bool_and(i.indisvalid), FALSE)
			FROM pg_index i
			JOIN pg_class tbl ON tbl.oid = i.indrelid
			LEFT JOIN pg_inherits inh ON inh.inhrelid = i.indexrelid
			LEFT JOIN pg_class root_idx ON root_idx.oid = inh.inhparent
			WHERE tbl.relname = $1 AND COALESCE(root_idx.relname, '') = $2`, childTable, indexName).Scan(&exists, &valid)
		return
	}
	indexDomainTable := func(indexName string) string {
		if strings.HasPrefix(indexName, "idx_usage_request_events") || strings.HasPrefix(indexName, "ix_usage_request_events") {
			return "usage_request_events"
		}
		return "request_logs"
	}
	for _, indexName := range ingressChainIndexesCreated {
		child := requestChild
		if indexDomainTable(indexName) == "usage_request_events" {
			child = usageChild
		}
		exists, valid, err := indexChildState(child, indexName)
		if err != nil {
			t.Fatalf("check child index %s on %s: %v", indexName, child, err)
		}
		if !exists || !valid {
			t.Fatalf("expected inherited index %s on %s to exist and be valid (exists=%t valid=%t)", indexName, child, exists, valid)
		}
	}
	for _, indexName := range ingressChainIndexesDropped {
		child := requestChild
		if indexDomainTable(indexName) == "usage_request_events" {
			child = usageChild
		}
		exists, _, err := indexChildState(child, indexName)
		if err != nil {
			t.Fatalf("check child index %s absence on %s: %v", indexName, child, err)
		}
		if exists {
			t.Fatalf("expected dropped index %s to be absent from %s", indexName, child)
		}
	}

	// Retained history is untouched by an index-only migration.
	var rowCount int64
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM request_logs`).Scan(&rowCount); err != nil {
		t.Fatalf("count request logs after upgrade: %v", err)
	}

	// A second full run stays noop: the swap is idempotent.
	noopResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("re-run migrations: %v", err)
	}
	if noopResult.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected second run to be noop, got %q", noopResult.Outcome)
	}
}

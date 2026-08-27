package integrationtest

import (
	"context"
	"testing"
	"time"
)

func TestResolvedTargetIndexesAttachToFuturePartitions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	harness := newPostgresHarness(t)
	conn := harness.openEmptyDatabase(t, ctx, "resolved_target_indexes")
	defer func() { _ = conn.Close(ctx) }()
	if _, err := newRunner(t).Run(ctx, conn); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	requestChild := "request_logs_p20260828"
	usageChild := "usage_request_events_p20260828"
	if _, err := conn.Exec(ctx, `CREATE TABLE `+requestChild+` PARTITION OF public.request_logs FOR VALUES FROM ('2026-08-28') TO ('2026-08-29')`); err != nil {
		t.Fatalf("create route_attempt partition: %v", err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE `+usageChild+` PARTITION OF public.usage_request_events FOR VALUES FROM ('2026-08-28') TO ('2026-08-29')`); err != nil {
		t.Fatalf("create final_execution partition: %v", err)
	}

	for parentIndex, childTable := range map[string]string{
		"idx_request_logs_resolved_target_created":         requestChild,
		"idx_request_logs_terminal_target_actual":          requestChild,
		"idx_usage_request_events_resolved_target_created": usageChild,
		"idx_usage_request_events_terminal_target_final":   usageChild,
	} {
		var parentValid, childAttached, childValid bool
		if err := conn.QueryRow(ctx, `SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname=$1`, parentIndex).Scan(&parentValid); err != nil {
			t.Fatalf("load parent index %s: %v", parentIndex, err)
		}
		if err := conn.QueryRow(ctx, `SELECT COUNT(*) > 0, COALESCE(bool_and(i.indisvalid), false)
			FROM pg_index i JOIN pg_class tbl ON tbl.oid=i.indrelid
			JOIN pg_inherits inh ON inh.inhrelid=i.indexrelid
			JOIN pg_class parent ON parent.oid=inh.inhparent
			WHERE tbl.relname=$1 AND parent.relname=$2`, childTable, parentIndex).Scan(&childAttached, &childValid); err != nil {
			t.Fatalf("load child index for %s: %v", parentIndex, err)
		}
		if !parentValid || !childAttached || !childValid {
			t.Fatalf("index %s parent_valid=%t child_attached=%t child_valid=%t", parentIndex, parentValid, childAttached, childValid)
		}
	}
}

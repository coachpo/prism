package contracttest

import (
	"context"
	"fmt"
	"testing"

	"time"

	"github.com/coachpo/prism/backend/tests/testsupport/logpartitions"
)

func contractTestLogPartitionFor(tableName string, timestamp time.Time) logpartitions.Partition {
	return logpartitions.For(tableName, timestamp)
}

func ensureContractTestLogPartitions(tb testing.TB, harness *contractHarness, partitions ...logpartitions.Partition) {
	tb.Helper()
	if harness == nil || harness.conn == nil {
		tb.Fatal("contract log partition helper requires harness connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logpartitions.Ensure(tb, partitions, func(tableName string, day time.Time) error {
		return ensureContractTestLogPartition(ctx, harness, tableName, day)
	})
}

func ensureContractTestLogPartition(ctx context.Context, harness *contractHarness, tableName string, day time.Time) error {
	if !isContractManagedLogTable(tableName) {
		return fmt.Errorf("unknown managed log table %q", tableName)
	}
	partitionName := fmt.Sprintf("%s_p%s", tableName, day.Format("20060102"))
	end := day.AddDate(0, 0, 1)
	query := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS public.%s PARTITION OF public.%s FOR VALUES FROM (%s) TO (%s) WITH (`+
			`autovacuum_vacuum_scale_factor = 0.02, `+
			`autovacuum_vacuum_threshold = 10000, `+
			`toast.autovacuum_vacuum_scale_factor = 0.02, `+
			`toast.autovacuum_vacuum_threshold = 10000)`,
		quoteIdentifier(partitionName),
		quoteIdentifier(tableName),
		logpartitions.QuoteTimestamp(day),
		logpartitions.QuoteTimestamp(end),
	)
	if _, err := harness.conn.Exec(ctx, query); err != nil {
		return fmt.Errorf("create partition %s for %s: %w", partitionName, tableName, err)
	}
	var attached bool
	if err := harness.conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_inherits inheritance
			JOIN pg_class parent ON parent.oid = inheritance.inhparent
			JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
			JOIN pg_class child ON child.oid = inheritance.inhrelid
			JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
			WHERE parent_ns.nspname = 'public'
			  AND parent.relname = $1
			  AND child_ns.nspname = 'public'
			  AND child.relname = $2
		)`, tableName, partitionName).Scan(&attached); err != nil {
		return fmt.Errorf("verify partition %s for %s: %w", partitionName, tableName, err)
	}
	if !attached {
		return fmt.Errorf("partition %s is not attached to %s", partitionName, tableName)
	}
	return nil
}

func isContractManagedLogTable(tableName string) bool {
	switch tableName {
	case "request_logs", "audit_logs", "usage_request_events", "loadbalance_events":
		return true
	default:
		return false
	}
}

func utcContractPartitionDay(timestamp time.Time) time.Time {
	return logpartitions.Day(timestamp)
}

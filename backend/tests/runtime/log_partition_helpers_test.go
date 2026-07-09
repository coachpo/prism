package runtimetest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/logretention"
	"github.com/coachpo/prism/backend/tests/testsupport/logpartitions"
)

func runtimeTestLogPartitionFor(tableName string, timestamp time.Time) logpartitions.Partition {
	return logpartitions.For(tableName, timestamp)
}

func ensureRuntimeTestLogPartitions(tb testing.TB, databaseName string, partitions ...logpartitions.Partition) {
	tb.Helper()
	if databaseName == "" {
		tb.Fatal("runtime log partition helper requires database name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, sharedPostgresHarness.connectionString(databaseName))
	if err != nil {
		tb.Fatalf("create log partition helper pool: %v", err)
	}
	defer pool.Close()

	store := logretention.NewStore(logretention.Options{Pool: pool})
	logpartitions.Ensure(tb, partitions, func(tableName string, day time.Time) error {
		return store.EnsurePartitionForTime(ctx, tableName, day)
	})
}

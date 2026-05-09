package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

type runtimeTestLogPartition struct {
	tableName string
	timestamp time.Time
}

func runtimeTestLogPartitionFor(tableName string, timestamp time.Time) runtimeTestLogPartition {
	return runtimeTestLogPartition{tableName: tableName, timestamp: timestamp.UTC()}
}

func ensureRuntimeTestLogPartitions(tb testing.TB, databaseName string, partitions ...runtimeTestLogPartition) {
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
	seen := make(map[string]struct{}, len(partitions))
	for _, partition := range partitions {
		key := partition.tableName + ":" + partition.timestamp.UTC().Format("20060102")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := store.EnsurePartitionForTime(ctx, partition.tableName, partition.timestamp); err != nil {
			tb.Fatalf("ensure %s partition for %s: %v", partition.tableName, partition.timestamp.UTC().Format("2006-01-02"), err)
		}
	}
}

package logretention

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPartitionNameGeneration(t *testing.T) {
	localZone := time.FixedZone("test", -7*60*60)
	input := time.Date(2026, 5, 8, 18, 30, 0, 0, localZone)
	if got := partitionNameForDay("request_logs", input); got != "request_logs_p20260509" {
		t.Fatalf("expected UTC partition name request_logs_p20260509, got %s", got)
	}

	partition, err := partitionFromName("request_logs", "request_logs_p20260509")
	if err != nil {
		t.Fatalf("parse partition name: %v", err)
	}
	expectedStart := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	if !partition.Start.Equal(expectedStart) || !partition.End.Equal(expectedStart.AddDate(0, 0, 1)) {
		t.Fatalf("expected half-open UTC bounds [%s, %s), got [%s, %s)", expectedStart, expectedStart.AddDate(0, 0, 1), partition.Start, partition.End)
	}
}

func TestRejectUnknownManagedTable(t *testing.T) {
	ctx := context.Background()
	store := &Store{}
	unknownNames := []string{"", "unknown_logs", `request_logs; DROP TABLE request_logs; --`}
	for _, tableName := range unknownNames {
		assertUnknownManagedTable(t, store.EnsurePartitionForTime(ctx, tableName, time.Now()))
		_, err := store.ListPartitions(ctx, tableName)
		assertUnknownManagedTable(t, err)
		_, err = store.DropExpiredPartitions(ctx, tableName, time.Now())
		assertUnknownManagedTable(t, err)
		_, err = store.DeleteBoundaryRows(ctx, tableName, time.Now())
		assertUnknownManagedTable(t, err)
		assertUnknownManagedTable(t, store.VacuumAnalyzePartition(ctx, tableName, "request_logs_p20260509"))
	}
}

func assertUnknownManagedTable(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrUnknownManagedTable) {
		t.Fatalf("expected ErrUnknownManagedTable, got %v", err)
	}
}

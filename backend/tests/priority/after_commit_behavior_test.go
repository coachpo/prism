package priority

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
)

func TestManagementSideEffectsHonorTransactionCommit(t *testing.T) {
	ctx, pool, _ := openPriorityTestPool(t)

	if _, err := pool.Exec(ctx, `DELETE FROM management_outbox`); err != nil {
		t.Fatalf("clear management_outbox: %v", err)
	}

	rollbackKey := "priority-after-commit-rollback-" + priorityRandomSuffix()
	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback transaction: %v", err)
	}
	if _, err := managementsideeffects.InsertTx(ctx, rollbackTx, managementsideeffects.Intent{
		OperationID:   rollbackKey,
		EventType:     managementsideeffects.EventDashboardSnapshotInvalidate,
		AggregateType: "profile",
		AggregateID:   "1",
		DedupeKey:     rollbackKey,
		Payload:       managementsideeffects.DashboardSnapshotInvalidatePayload{ProfileID: 1},
	}); err != nil {
		t.Fatalf("enqueue rollback intent: %v", err)
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}
	if got := countManagementOutboxRows(t, ctx, pool, rollbackKey); got != 0 {
		t.Fatalf("expected rollback to leave no outbox row, got %d", got)
	}

	commitKey := "priority-after-commit-commit-" + priorityRandomSuffix()
	commitTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin commit transaction: %v", err)
	}
	if _, err := managementsideeffects.InsertTx(ctx, commitTx, managementsideeffects.Intent{
		OperationID:   commitKey,
		EventType:     managementsideeffects.EventDashboardSnapshotInvalidate,
		AggregateType: "profile",
		AggregateID:   "1",
		DedupeKey:     commitKey,
		Payload:       managementsideeffects.DashboardSnapshotInvalidatePayload{ProfileID: 1},
	}); err != nil {
		t.Fatalf("enqueue commit intent: %v", err)
	}
	if err := commitTx.Commit(ctx); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	if got := countManagementOutboxRows(t, ctx, pool, commitKey); got != 1 {
		t.Fatalf("expected committed outbox row, got %d", got)
	}
}

func countManagementOutboxRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, operationID string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM management_outbox WHERE operation_id = $1`, operationID).Scan(&count); err != nil {
		t.Fatalf("count management_outbox rows for %s: %v", operationID, err)
	}
	return count
}

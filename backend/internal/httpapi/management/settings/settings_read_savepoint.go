package settings

// Settings read savepoints isolate optional owner projections inside one
// repeatable-read transaction. A missing or transient owner read rolls back
// only its savepoint and becomes an explicit unavailable projection; later
// settings reads remain usable.
//
// The protection deadline is evidence read from a durable automatic job. It is
// never derived from a browser timestamp. Dataset indexes make savepoint names
// deterministic while keeping the four dataset owners distinct.
//
// Savepoint names are generated only from package-owned dataset indexes; no
// request value is interpolated into SQL. The callback runs inside the caller's
// transaction and inherits its snapshot and admission boundary.
//
// A successful callback releases its savepoint. A failed callback rolls back
// the local read before releasing it, leaving the outer transaction usable.
// Savepoint ownership is local to settings reads; it never replaces the
// transaction boundary of a mutation.
//
//
// This seam keeps optional reads from poisoning the parent transaction.
//
import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// physicalReclaimNotBefore returns the persisted protection evidence for the
// newest nonterminal automatic job. It never derives a deadline from a UI
// request timestamp; rows without evidence stay unavailable.
func (s *retentionService) physicalReclaimNotBefore(ctx context.Context, tx pgx.Tx, dataset string) *string {
	var deadline *time.Time
	err := tx.QueryRow(ctx, `SELECT MAX(NULLIF(progress_json->'protection'->>'deadline', '')::timestamptz)
		FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND origin = 'automatic'
		  AND resource_key = $1 AND state IN ('queued','running','cancel_requested')`, dataset).Scan(&deadline)
	if err != nil || deadline == nil {
		return nil
	}
	formatted := deadline.UTC().Format(time.RFC3339)
	return &formatted
}

// withSettingsReadSavepoint makes an owner projection genuinely optional. A
// transiently unavailable owner table/read model must become an explicit
// unavailable projection, not abort the surrounding Settings response
// transaction and turn a later diagnostic query into 25P02.
func withSettingsReadSavepoint(ctx context.Context, tx pgx.Tx, name string, fn func() error) error {
	if _, err := tx.Exec(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	err := fn()
	if err != nil {
		if _, rollbackErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name); rollbackErr != nil {
			return fmt.Errorf("rollback settings read savepoint %s: %w (original: %v)", name, rollbackErr, err)
		}
		if _, releaseErr := tx.Exec(ctx, "RELEASE SAVEPOINT "+name); releaseErr != nil {
			return fmt.Errorf("release settings read savepoint %s: %w (original: %v)", name, releaseErr, err)
		}
		return err
	}
	_, err = tx.Exec(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

func datasetProtectionSavepointIndex(dataset string) int {
	switch dataset {
	case retentionDatasetRequestLogs:
		return 0
	case retentionDatasetUsageRequestEvents:
		return 1
	case retentionDatasetLoadbalanceEvents:
		return 2
	default:
		return 99
	}
}

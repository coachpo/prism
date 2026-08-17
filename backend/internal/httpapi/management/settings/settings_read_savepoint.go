package settings

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

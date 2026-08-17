package managementjobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// Legacy v1 log-retention rows are never restarted under the current contract. They
// either drain through the frozen executor below or, when they provably never
// executed, terminalize as superseded. Both paths stay fenced by the database
// until the startup cutover authorizes legacy claim/delete.

// AuthorizeLegacyDrain flips the DB fence flags so the frozen generation-tagged
// v1 executor may claim/delete previously accepted legacy log-retention rows.
// It is called exactly once by the startup cutover; creates by legacy workers
// remain permanently fenced.
func (s *Store) AuthorizeLegacyDrain(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE retention_worker_transition_state
		SET legacy_claim_authorized = TRUE, legacy_delete_authorized = TRUE, updated_at = now()
		WHERE id = 1`)
	if err != nil {
		return fmt.Errorf("authorize legacy retention drain: %w", err)
	}
	return nil
}

func (s *Store) claimLegacyRetentionJob(ctx context.Context) (retentionJobRow, bool, error) {
	var job retentionJobRow
	found := false
	err := pgxutil.InTx(ctx, s.pool, "retention_claim_legacy", func(tx pgx.Tx) error {
		query := `WITH claimable AS (
			SELECT id FROM management_jobs
			WHERE type = 'log_retention' AND contract_version = 1
			  AND (state = 'queued' OR (state = 'running' AND locked_until < now()))
			  AND cancel_requested = FALSE AND next_attempt_at <= now()
			ORDER BY requested_at ASC, id ASC LIMIT 1 FOR UPDATE SKIP LOCKED
		) UPDATE management_jobs j SET state = 'running', started_at = COALESCE(started_at, now()),
			locked_by = $1, locked_until = now() + $2::interval, last_heartbeat_at = now(), updated_at = now()
		FROM claimable WHERE j.id = claimable.id RETURNING ` + retentionSelectColumnsQualified()
		row := tx.QueryRow(ctx, query, s.workerID, intervalLiteral(defaultLeaseDuration))
		scanned, err := scanRetentionRow(row)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		job = scanned
		found = true
		return nil
	})
	return job, found, err
}

// drainLegacyRetentionJob is the frozen generation-tagged v1 executor: it
// resumes a previously accepted legacy job from its real checkpoint and
// reaches a truthful terminal result (SPEC §7.2). Never superseded, never
// restarted from an empty counter.
func (s *Store) drainLegacyRetentionJob(ctx context.Context, job retentionJobRow) error {
	if s.logRetention == nil {
		return s.failRetentionJob(ctx, job, "retention_store_missing", "log retention store is required")
	}
	if job.ClassificationEvidenceHash == nil {
		return s.failRetentionJob(ctx, job, "legacy_evidence_missing", "legacy job has no classification evidence")
	}
	scope := job.Scope
	summary, err := s.logRetention.RunRetention(ctx, scope.Table, scope.Cutoff, scope.DeleteAll)
	if err != nil {
		return s.failRetentionJob(ctx, job, "retention_error", "legacy retention drain failed")
	}
	progressJSON, err := json.Marshal(map[string]any{
		"dropped_partitions":   summary.DroppedPartitions,
		"boundary_partition":   summary.BoundaryPartition,
		"legacy_boundary_only": true,
	})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE management_jobs SET
		state = CASE WHEN cancel_requested THEN 'cancelled' ELSE 'succeeded' END,
		terminal_disposition = CASE WHEN cancel_requested THEN 'cancelled' ELSE 'completed' END,
		finished_at = now(), locked_by = NULL, locked_until = NULL,
		boundary_rows_deleted = COALESCE(boundary_rows_deleted, 0) + $2,
		batches_completed = batches_completed + 1, progress_json = $3::jsonb,
		visibility_state = 'legacy_unknown', stage = 'finished',
		last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID, summary.BoundaryRowsDeleted, progressJSON)
	if err != nil {
		return fmt.Errorf("complete legacy drain: %w", err)
	}
	_ = s.appendEvent(ctx, job.ID, "finished", "legacy log retention job drained", summary.BoundaryRowsDeleted)
	return nil
}

// SupersedeLegacyNeverExecutedAutomatic terminalizes legacy queued /
// cancel-requested automatic intents that provably never executed, with the
// exact superseded disposition and original state (SPEC §7.2). Safe unknown
// rows drain as manual; repair_required rows block.
func (s *Store) SupersedeLegacyNeverExecutedAutomatic(ctx context.Context) error {
	return pgxutil.InTx(ctx, s.pool, "retention_supersede", func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM management_jobs
			WHERE type = 'log_retention' AND contract_version = 1
			  AND state IN ('queued','cancel_requested')
			  AND origin = 'automatic'
			  AND legacy_origin_provenance = 'proven_automatic_scheduler'
			  AND legacy_execution_provenance = 'proven_never_executed'
			  AND classification_evidence_hash IS NOT NULL
			ORDER BY requested_at ASC, id ASC FOR UPDATE`)
		if err != nil {
			return fmt.Errorf("scan supersedable legacy jobs: %w", err)
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range ids {
			// legacy_original_state is preserved by the COALESCE below, so the
			// pre-update value never needs to be read back.
			if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
				state = 'superseded',
				terminal_disposition = 'superseded_by_v2_planning',
				legacy_original_state = COALESCE(legacy_original_state, CASE WHEN cancel_requested THEN 'cancel_requested' ELSE 'queued' END),
				finished_at = now(), updated_at = now()
				WHERE id = $1 AND state IN ('queued','cancel_requested')`, id); err != nil {
				return fmt.Errorf("supersede legacy automatic job %s: %w", id, err)
			}
			_ = s.appendEvent(ctx, id, "superseded", "legacy never-executed automatic intent superseded by v2 planning", 0)
		}
		return nil
	})
}

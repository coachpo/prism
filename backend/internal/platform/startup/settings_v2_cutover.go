package startup

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// runSettingsV2Cutover performs the one-time v2 cutover after migrations.
// Databases whose staged migration prefix predates 000012 have no
// retention_worker_transition_state row; the cutover is a no-op there.
func (s Service) runSettingsV2Cutover(ctx context.Context, conn *pgx.Conn) error {
	var transitionExists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.retention_worker_transition_state') IS NOT NULL`).Scan(&transitionExists); err != nil {
		return err
	}
	if !transitionExists {
		return nil
	}
	return pgxutil.InTx(ctx, conn, "settings_v2_cutover", func(tx pgx.Tx) error {
		// The transition singleton exists after 000012; a pre-000012 database
		// fails migrations before this step can run.
		if _, err := tx.Exec(ctx, `UPDATE retention_worker_transition_state
			SET legacy_claim_authorized = TRUE, legacy_delete_authorized = TRUE, updated_at = now()
			WHERE id = 1`); err != nil {
			return fmt.Errorf("authorize legacy retention drain: %w", err)
		}

		// Supersede only proven-never-executed automatic intents. The same
		// predicate the executor uses is applied here under the transaction;
		// state transitions remain guarded by the DB trigger.
		if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
			state = 'superseded',
			terminal_disposition = 'superseded_by_v2_planning',
			legacy_original_state = COALESCE(legacy_original_state, CASE WHEN cancel_requested THEN 'cancel_requested' ELSE 'queued' END),
			finished_at = now(),
			updated_at = now()
			WHERE type = 'log_retention'
			  AND contract_version = 1
			  AND state IN ('queued','cancel_requested')
			  AND origin = 'automatic'
			  AND legacy_origin_provenance = 'proven_automatic_scheduler'
			  AND legacy_execution_provenance = 'proven_never_executed'
			  AND classification_evidence_hash IS NOT NULL`); err != nil {
			return fmt.Errorf("supersede never-executed legacy automatic retention intents: %w", err)
		}

		// Record cutover events for the superseded rows (bounded evidence).
		if _, err := tx.Exec(ctx, `INSERT INTO management_job_events (job_id, event_type, message, rows_deleted, created_at)
			SELECT id, 'superseded', 'legacy never-executed automatic intent superseded by v2 planning', 0, now()
			FROM management_jobs
			WHERE type = 'log_retention' AND state = 'superseded'
			  AND terminal_disposition = 'superseded_by_v2_planning'
			  AND NOT EXISTS (
				SELECT 1 FROM management_job_events existing
				WHERE existing.job_id = management_jobs.id
				  AND existing.event_type = 'superseded'
				  AND existing.message = 'legacy never-executed automatic intent superseded by v2 planning'
			  )`); err != nil {
			return fmt.Errorf("record supersession events: %w", err)
		}
		return nil
	})
}

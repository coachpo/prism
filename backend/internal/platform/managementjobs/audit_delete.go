package managementjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// audit_delete is the profile-scoped v1 job type. It keeps the original
// single-shot executor; global log_retention work runs through the current retention contract.

type CreateAuditDeleteJobRequest struct {
	ProfileID      int
	RequestedBy    string
	IdempotencyKey string
	Reason         string
	Scope          AuditDeleteScope
}

func (s *Store) CreateAuditDeleteJob(ctx context.Context, req CreateAuditDeleteJobRequest) (Job, error) {
	if s == nil || s.pool == nil {
		return Job{}, fmt.Errorf("management job store is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return Job{}, fmt.Errorf("delete_reason_required")
	}
	if !req.Scope.DeleteAll && req.Scope.Before == nil {
		return Job{}, fmt.Errorf("delete_scope_required")
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return Job{}, fmt.Errorf("delete_idempotency_required")
	}
	requestedBy := strings.TrimSpace(req.RequestedBy)
	if requestedBy == "" {
		requestedBy = fmt.Sprintf("profile:%d", req.ProfileID)
	}
	jobID := "job_" + randomHex(12)
	scopeJSON, err := json.Marshal(req.Scope)
	if err != nil {
		return Job{}, err
	}
	var id string
	err = s.pool.QueryRow(ctx, `INSERT INTO management_jobs (id, type, state, requested_by, requested_at, priority, idempotency_key, profile_id, scope_json, reason, created_at, updated_at) VALUES ($1, 'audit_delete', 'queued', $2, $3, 'maintenance', $4, $5, $6, $7, $3, $3) ON CONFLICT (type, requested_by, idempotency_key) WHERE idempotency_key IS NOT NULL DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key RETURNING id`, jobID, requestedBy, s.now().UTC(), req.IdempotencyKey, req.ProfileID, scopeJSON, strings.TrimSpace(req.Reason)).Scan(&id)
	if err != nil {
		return Job{}, fmt.Errorf("insert management job: %w", err)
	}
	_ = s.appendEvent(ctx, id, "created", "audit delete job accepted", 0)
	_ = s.Wake(context.Background())
	return s.GetJob(ctx, id, req.ProfileID)
}

func (s *Store) claimOne(ctx context.Context) (Job, bool, error) {
	result, err := pgxutil.InTxValue(ctx, s.pool, "management_jobs_claim", func(tx pgx.Tx) (struct {
		Job Job
		OK  bool
	}, error) {
		row := tx.QueryRow(ctx, `WITH claimable AS (SELECT id FROM management_jobs WHERE type = 'audit_delete' AND cancel_requested = FALSE AND next_attempt_at <= now() AND (state = 'queued' OR (state = 'running' AND locked_until < now())) ORDER BY created_at ASC, id ASC LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE management_jobs j SET state = 'running', started_at = COALESCE(started_at, now()), locked_by = $1, locked_until = now() + $2::interval, last_heartbeat_at = now(), updated_at = now() FROM claimable WHERE j.id = claimable.id RETURNING `+jobColumnsQualified, s.workerID, intervalLiteral(defaultLeaseDuration))
		job, err := scanJob(row)
		if err == pgx.ErrNoRows {
			return struct {
				Job Job
				OK  bool
			}{}, nil
		}
		return struct {
			Job Job
			OK  bool
		}{Job: job, OK: true}, err
	})
	return result.Job, result.OK, err
}

// processAuditDelete runs the whole job in one pass against the audit_logs
// partition set: the profile-scoped contract has no checkpoint or fence
// requirements, so a failure requeues the job until max_attempts is reached.
func (s *Store) processAuditDelete(ctx context.Context, job Job) error {
	if s.logRetention == nil {
		return s.failJob(ctx, job, "retention_store_missing", "log retention store is required")
	}
	job.Scope.Table = "audit_logs"
	job.Scope.Cutoff = job.Scope.Before
	summary, err := s.logRetention.RunRetention(ctx, job.Scope.Table, job.Scope.Cutoff, job.Scope.DeleteAll)
	if err != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET attempt_count = LEAST(attempt_count + 1, max_attempts), state = CASE WHEN attempt_count + 1 >= max_attempts THEN 'failed' ELSE 'queued' END, error_code = 'retention_error', error_message = $2, next_attempt_at = CASE WHEN attempt_count + 1 >= max_attempts THEN next_attempt_at ELSE now() + interval '5 seconds' END, finished_at = CASE WHEN attempt_count + 1 >= max_attempts THEN now() ELSE finished_at END, locked_by = NULL, locked_until = NULL, updated_at = now() WHERE id = $1`, job.ID, err.Error())
		return err
	}
	progressJSON, err := json.Marshal(map[string]any{"dropped_partitions": summary.DroppedPartitions, "boundary_partition": summary.BoundaryPartition})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE management_jobs SET state = CASE WHEN cancel_requested THEN 'cancelled' ELSE 'succeeded' END, finished_at = now(), locked_by = NULL, locked_until = NULL, rows_deleted = rows_deleted + $2, batches_completed = batches_completed + 1, progress_json = $3::jsonb, last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID, summary.BoundaryRowsDeleted, progressJSON)
	_ = s.appendEvent(ctx, job.ID, "finished", "log retention job finished", summary.BoundaryRowsDeleted)
	return err
}

func (s *Store) failJob(ctx context.Context, job Job, code string, message string) error {
	_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET attempt_count = LEAST(attempt_count + 1, max_attempts), state = 'failed', error_code = $2, error_message = $3, finished_at = now(), locked_by = NULL, locked_until = NULL, updated_at = now() WHERE id = $1`, job.ID, code, message)
	return fmt.Errorf("%s", message)
}

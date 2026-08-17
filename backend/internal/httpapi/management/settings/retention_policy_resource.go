package settings

import (
	"context"
	"fmt"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/jackc/pgx/v5"
)

// applyPolicyResource advances the per-dataset resource and creates durable
// desired work for destructive/enabling changes.
func (s *retentionService) applyPolicyResource(ctx context.Context, tx pgx.Tx, dataset string, before, after *int, settingsRevision int64, now time.Time) (*time.Time, *retentionScheduledWork, error) {
	var resource policyResourceRow
	err := tx.QueryRow(ctx, `SELECT policy_generation, fence_generation, settings_revision, configured_logical_cutoff,
		published_retention_floor, retention_revocation_epoch, purge_state
		FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, dataset).
		Scan(&resource.PolicyGeneration, &resource.FenceGeneration, &resource.SettingsRevision, &resource.ConfiguredLogicalCutoff,
			&resource.PublishedRetentionFloor, &resource.RevocationEpoch, &resource.PurgeState)
	if err != nil {
		return nil, nil, err
	}
	// A queued manual purge is already a sealed destructive reservation.  A
	// policy mutation must not invalidate that preflight between acceptance and
	// the worker's execution fence.  Running/recovery states are likewise
	// immutable until the owning job publishes or explicitly recovers them.
	if resource.PurgeState == "running" || resource.PurgeState == "recovery_required" {
		return nil, nil, &settingsConflictError{code: "retention_job_conflict"}
	}
	var manualReservation bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'manual' AND resource_key = $1
		  AND state IN ('queued','running','cancel_requested')
	)`, dataset).Scan(&manualReservation); err != nil {
		return nil, nil, err
	}
	if manualReservation {
		return nil, nil, &settingsConflictError{code: "retention_job_conflict"}
	}
	var cutoff *time.Time
	if after != nil {
		value := utcDayCutoff(now, *after)
		cutoff = &value
	}
	newGeneration := resource.PolicyGeneration + 1
	if _, err := tx.Exec(ctx, `UPDATE log_retention_policy_resources SET
		policy_generation = $2, fence_generation = fence_generation + 1,
		settings_revision = $3, configured_logical_cutoff = $4, updated_at = now()
		WHERE dataset = $1`, dataset, newGeneration, settingsRevision, cutoff); err != nil {
		return nil, nil, err
	}
	// A policy/floor transition changes the owner source revision. Refresh its
	// actual-coverage materialization in this same transaction so a subsequent
	// preflight sees a coherent source/coverage pair rather than a transient
	// stale projection that Settings would have to synthesize or silently trust.
	ownerSource, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
	if err != nil {
		return nil, nil, err
	}
	if err := statsdomain.RefreshActualCoverageProjection(ctx, tx, ownerSource, now); err != nil {
		return nil, nil, err
	}

	// Only enabling cleanup (NULL -> N) or shortening (N -> smaller N) creates
	// destructive work. Extending or disabling a policy advances the owner
	// resource but must not manufacture a cleanup job for an older cutoff.
	work := (*retentionScheduledWork)(nil)
	// A newer policy generation supersedes every queued automatic intent for
	// this dataset before a replacement is created. Running work is only
	// cancel-requested; its worker must re-check the same generation fence at
	// its next irreversible checkpoint.
	if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
		state = CASE WHEN state = 'queued' THEN 'cancelled' ELSE state END,
		terminal_disposition = CASE WHEN state = 'queued' THEN 'cancelled' ELSE NULL END,
		cancel_requested = TRUE,
		finished_at = CASE WHEN state = 'queued' THEN now() ELSE finished_at END,
		updated_at = now()
		WHERE type = 'log_retention' AND contract_version = 2 AND origin = 'automatic'
		  AND resource_key = $1 AND state IN ('queued','running','cancel_requested')
		  AND COALESCE(policy_generation, 0) < $2`, dataset, newGeneration); err != nil {
		return nil, nil, err
	}
	if isDestructiveTransition(before, after) && after != nil && cutoff != nil {
		jobID, err := s.createScheduledJob(ctx, tx, dataset, *cutoff, settingsRevision, newGeneration, now)
		if err != nil {
			return nil, nil, err
		}
		work = &retentionScheduledWork{
			Dataset:          dataset,
			PolicyGeneration: fmt.Sprintf("%d", newGeneration),
			Disposition:      "created",
			JobID:            jobID,
		}
	}
	return cutoff, work, nil
}

func (s *retentionService) createScheduledJob(ctx context.Context, tx pgx.Tx, dataset string, cutoff time.Time, settingsRevision int64, policyGeneration int64, now time.Time) (string, error) {
	jobID, err := s.jobs.CreateAutomaticRetentionJobTx(ctx, tx, dataset, cutoff, settingsRevision, policyGeneration, now)
	if err != nil {
		return "", err
	}
	return jobID, nil
}

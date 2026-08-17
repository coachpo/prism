package managementjobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

// claimRetentionJob claims the next due log-retention job with worker
// evidence. One job at a time; lease/fencing follows the v1 pattern.
func (s *Store) claimRetentionJob(ctx context.Context) (retentionJobRow, bool, error) {
	var job retentionJobRow
	found := false
	err := pgxutil.InTx(ctx, s.pool, "retention_claim_v2", func(tx pgx.Tx) error {
		query := `WITH claimable AS (
			SELECT id FROM management_jobs
			WHERE type = 'log_retention' AND contract_version = 2
			  AND (
				state = 'queued'
				OR (state = 'running' AND (locked_until IS NULL OR locked_until < now()) AND (
					(origin = 'manual' AND purge_state IN ('running','recovery_required'))
					OR origin = 'automatic'
				))
			  )
			  AND cancel_requested = FALSE AND next_attempt_at <= now()
			  AND (origin <> 'automatic' OR stage <> 'waiting_for_resource' OR NOT EXISTS (
				SELECT 1 FROM log_retention_policy_resources AS resource
				WHERE resource.dataset = management_jobs.resource_key
				  AND resource.purge_state IN ('running','recovery_required')
			  ))
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

// processRetentionJob executes a claimed job:
//   - automatic scheduled: protection gate -> physical reclaim -> publish
//   - manual purge: purge fence -> running -> final publish (revocation epoch)
func (s *Store) processRetentionJob(ctx context.Context, job retentionJobRow) error {
	if job.Origin == nil {
		return s.failRetentionJob(ctx, job, "origin_missing", "retention job has no origin")
	}
	if *job.Origin == "automatic" {
		return s.processAutomaticRetentionJob(ctx, job)
	}
	return s.processManualRetentionPurge(ctx, job)
}

func (s *Store) processAutomaticRetentionJob(ctx context.Context, job retentionJobRow) error {
	if job.Scope.Table == "" {
		return s.failRetentionJob(ctx, job, "retention_scope_invalid", "automatic retention job has no table scope")
	}
	if job.Scope.Cutoff == nil {
		return s.failRetentionJob(ctx, job, "retention_scope_invalid", "automatic retention job has no cutoff")
	}

	// Resource generation revalidation: a policy change terminal-cancels this
	// generation before any irreversible step (SPEC §5.4 rule 9).
	var resourceGeneration int64
	var resourceFenceGeneration int64
	var resourceCutoff *time.Time
	var resourcePurgeState string
	if err := s.pool.QueryRow(ctx, `SELECT policy_generation, fence_generation, configured_logical_cutoff, purge_state
		FROM log_retention_policy_resources WHERE dataset = $1`, job.Scope.Table).
		Scan(&resourceGeneration, &resourceFenceGeneration, &resourceCutoff, &resourcePurgeState); err != nil {
		return s.failRetentionJob(ctx, job, "retention_resource_missing", "policy resource missing for dataset")
	}
	if job.PolicyGeneration == nil || resourceGeneration != *job.PolicyGeneration ||
		resourceCutoff == nil || !resourceCutoff.Equal(*job.Scope.Cutoff) {
		// Superseded by a newer policy generation: terminal-cancel without
		// data change (never a fake success).
		return s.cancelRetentionJobWithoutDataChange(ctx, job, "superseded_by_newer_policy")
	}
	if resourcePurgeState == "running" || resourcePurgeState == "recovery_required" {
		_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET stage = 'waiting_for_resource',
			next_attempt_at = now() + interval '5 seconds', last_heartbeat_at = now(), updated_at = now()
			WHERE id = $1 AND state = 'running'`, job.ID)
		return nil
	}
	if job.FenceGeneration == nil || resourceFenceGeneration != *job.FenceGeneration {
		binding, err := s.pool.Exec(ctx, `UPDATE management_jobs SET fence_generation = $2,
			stage = CASE WHEN stage = 'waiting_for_resource' THEN 'queued' ELSE stage END,
			last_heartbeat_at = now(), updated_at = now() WHERE id = $1 AND state = 'running'`, job.ID, resourceFenceGeneration)
		if err != nil || binding.RowsAffected() != 1 {
			return s.failRetentionJob(ctx, job, "retention_resource_generation_changed", "retention fence binding failed")
		}
		job.FenceGeneration = &resourceFenceGeneration
	}
	var manualReserved bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'manual' AND resource_key = $1 AND state IN ('queued','running')
	)`, job.Scope.Table).Scan(&manualReserved); err != nil {
		return s.failRetentionJob(ctx, job, "retention_resource_unavailable", "manual retention reservation check failed")
	}
	if manualReserved {
		_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET stage = 'waiting_for_resource',
			next_attempt_at = now() + interval '5 seconds', last_heartbeat_at = now(), updated_at = now()
			WHERE id = $1 AND state = 'running'`, job.ID)
		return nil
	}

	// Protection gate for the three Observe domains (audit uses its own
	// fence projection and never waits on a fixed 48h window).
	if job.Scope.Table != "audit_logs" {
		deadline := observeProtectionDeadline(job.RequestedAt)
		if s.now().UTC().Before(deadline) {
			_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET stage = 'waiting_for_protection',
				next_attempt_at = $2, last_heartbeat_at = now(), updated_at = now() WHERE id = $1`,
				job.ID, deadline)
			return nil
		}
	}

	// Physical execution: partitions + boundary rows with checkpoints.
	if err := s.executePhysicalReclaim(ctx, job); err != nil {
		return err
	}
	return s.publishScheduledRetention(ctx, job)
}

// executePhysicalReclaim drops complete expired partitions and deletes
// boundary rows in bounded batches, checkpointing before every irreversible
// step (SPEC §7.5).
func (s *Store) executePhysicalReclaim(ctx context.Context, job retentionJobRow) error {
	if s.logRetention == nil {
		return s.failRetentionJob(ctx, job, "retention_store_missing", "log retention store is required")
	}
	cutoff := *job.Scope.Cutoff

	// Audit owns a separate materializer/artifact boundary.  Establish the
	// append-only tombstone and scrub pending staging/outbox artifacts before
	// dropping audit partitions or deleting the boundary rows.  This is the
	// coordinated audit purge fence; it is intentionally not the Observe token
	// grace contract used by the other three datasets.
	if job.Scope.Table == "audit_logs" {
		if _, err := s.prepareAuditPurgeUnderFence(ctx, job, cutoff); err != nil {
			return s.failRetentionJob(ctx, job, "audit_purge_prepare_failed", "prepare coordinated audit purge failed")
		}
	}

	// Complete partitions whose authoritative end <= cutoff are dropped.
	if _, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
		stage = 'dropping_partitions', last_heartbeat_at = now(), updated_at = now()
		WHERE id = $1`, job.ID); err != nil {
		return fmt.Errorf("checkpoint before partition drop: %w", err)
	}
	_ = s.appendEvent(ctx, job.ID, "before_partition_drop", "checkpointed before dropping expired partitions", 0)
	dropped, err := s.dropExpiredPartitionsUnderFence(ctx, job, cutoff)
	if err != nil {
		return s.failRetentionJob(ctx, job, "partition_drop_failed", "drop expired partitions failed")
	}
	if len(dropped) > 0 {
		names := make([]string, 0, len(dropped))
		for _, partition := range dropped {
			names = append(names, partition.PartitionName)
		}
		if _, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
			stage = 'dropping_partitions', dropped_partition_count = $2,
			dropped_partition_count_accuracy = 'exact', progress_json = COALESCE(progress_json, '{}'::jsonb) || $3::jsonb,
			last_heartbeat_at = now(), updated_at = now() WHERE id = $1`,
			job.ID, int64(len(dropped)), mustJSON(map[string]any{"dropped_partitions": names})); err != nil {
			return fmt.Errorf("checkpoint dropped partitions: %w", err)
		}
		_ = s.appendEvent(ctx, job.ID, "partitions_dropped", "dropped expired partitions", 0)
	}

	var boundaryRowsDeleted int64
	var boundary logretention.Partition
	var ok bool
	if _, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
		stage = 'deleting_boundary_rows', last_heartbeat_at = now(), updated_at = now()
		WHERE id = $1`, job.ID); err != nil {
		return fmt.Errorf("checkpoint before boundary delete: %w", err)
	}
	_ = s.appendEvent(ctx, job.ID, "before_boundary_delete", "checkpointed before deleting boundary rows", 0)
	boundary, boundaryRowsDeleted, ok, err = s.deleteBoundaryRowsUnderFence(ctx, job, cutoff)
	if err != nil {
		return s.failRetentionJob(ctx, job, "boundary_delete_failed", "boundary row delete failed")
	}
	if ok {
		if _, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
			stage = 'deleting_boundary_rows', boundary_rows_deleted = $2,
			last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID, boundaryRowsDeleted); err != nil {
			return fmt.Errorf("checkpoint boundary rows: %w", err)
		}
		_ = s.appendEvent(ctx, job.ID, "boundary_rows_deleted", "deleted boundary rows", boundaryRowsDeleted)
		if err := s.logRetention.VacuumAnalyzePartition(ctx, job.Scope.Table, boundary.PartitionName); err != nil {
			// VACUUM failure is restart-safe: completed drop/delete evidence
			// stays durable and the stage retries without repeating work.
			_ = s.appendEvent(ctx, job.ID, "vacuum_retry", "vacuum boundary partition failed; will retry", 0)
		}
	}
	return nil
}

type auditPurgePreparation struct {
	TombstonesCreated       int64
	StagingItemsTombstoned  int64
	ArtifactsDeleted        int64
	OutboxExtensionsOmitted int64
}

// prepareAuditPurgeUnderFence seals the audit owner boundary before any
// physical delete.  A v2 outbox item may still be needed for request/usage
// materialization, so only its audit extension is omitted; the artifact and
// staging payloads are scrubbed/deleted.  The tombstone trigger then protects
// the same ingress from a late materializer retry.
func (s *Store) prepareAuditPurgeUnderFence(ctx context.Context, job retentionJobRow, cutoff time.Time) (auditPurgePreparation, error) {
	prepared := auditPurgePreparation{}
	err := pgxutil.InTx(ctx, s.pool, "audit_retention_prepare", func(tx pgx.Tx) error {
		if err := auditdomain.MarkAuditRetentionDraining(ctx, tx, s.now().UTC()); err != nil {
			return err
		}
		var generation int64
		if err := tx.QueryRow(ctx, `SELECT policy_generation
			FROM log_retention_policy_resources WHERE dataset = 'audit_logs' FOR UPDATE`).Scan(&generation); err != nil {
			return fmt.Errorf("lock audit retention generation: %w", err)
		}
		tombstones, err := tx.Exec(ctx, `INSERT INTO audit_retention_tombstones (
			profile_id, ingress_request_id, cutoff, retention_generation, reason, created_at
		)
		SELECT candidates.profile_id, candidates.ingress_request_id, $1, $2, $3, now()
		FROM (
			SELECT DISTINCT profile_id, ingress_request_id
			FROM audit_logs
				WHERE ingress_request_id IS NOT NULL AND created_at < $1
			UNION
			SELECT DISTINCT profile_id, ingress_request_id
			FROM runtime_telemetry_artifacts
			WHERE ingress_request_id IS NOT NULL
				  AND COALESCE(audit_component_created_at, created_at) < $1
			UNION
			SELECT DISTINCT profile_id, ingress_request_id
			FROM audit_artifact_staging
				WHERE ingress_request_id IS NOT NULL AND created_at < $1
			UNION
			SELECT DISTINCT profile_id, ingress_request_id
			FROM runtime_telemetry_outbox
				WHERE ingress_request_id IS NOT NULL AND created_at < $1
		) AS candidates
		ON CONFLICT (profile_id, ingress_request_id, cutoff, retention_generation) DO NOTHING`,
			cutoff.UTC(), generation, auditPurgeReason(job))
		if err != nil {
			return fmt.Errorf("record audit retention tombstones: %w", err)
		}
		prepared.TombstonesCreated = tombstones.RowsAffected()

		staging, err := tx.Exec(ctx, `UPDATE audit_artifact_staging AS staging SET
			state = 'tombstoned', payload = '{}'::jsonb,
			last_safe_error_code = 'audit_retention_tombstoned', updated_at = now()
			WHERE staging.created_at < $1
			  AND EXISTS (
				SELECT 1 FROM audit_retention_tombstones AS tombstone
				WHERE tombstone.profile_id = staging.profile_id
				  AND tombstone.ingress_request_id = staging.ingress_request_id
				  AND staging.created_at < tombstone.cutoff
			  )
			  AND staging.state <> 'tombstoned'`, cutoff.UTC())
		if err != nil {
			return fmt.Errorf("tombstone audit staging: %w", err)
		}
		prepared.StagingItemsTombstoned = staging.RowsAffected()

		artifacts, err := tx.Exec(ctx, `DELETE FROM runtime_telemetry_artifacts AS artifact
				WHERE COALESCE(artifact.audit_component_created_at, artifact.created_at) < $1
			  AND EXISTS (
				SELECT 1 FROM audit_retention_tombstones AS tombstone
				WHERE tombstone.profile_id = artifact.profile_id
				  AND tombstone.ingress_request_id = artifact.ingress_request_id
				  AND COALESCE(artifact.audit_component_created_at, artifact.created_at) < tombstone.cutoff
			  )`, cutoff.UTC())
		if err != nil {
			return fmt.Errorf("delete audit artifacts: %w", err)
		}
		prepared.ArtifactsDeleted = artifacts.RowsAffected()

		outbox, err := tx.Exec(ctx, `UPDATE runtime_telemetry_outbox AS outbox SET
			audit_extension_payload = NULL, extension_state = 'omitted'
				WHERE outbox.schema_version = 2 AND outbox.created_at < $1
			  AND EXISTS (
				SELECT 1 FROM audit_retention_tombstones AS tombstone
				WHERE tombstone.profile_id = outbox.profile_id
				  AND tombstone.ingress_request_id = outbox.ingress_request_id
				  AND outbox.created_at < tombstone.cutoff
			  )
			  AND outbox.extension_state <> 'omitted'`, cutoff.UTC())
		if err != nil {
			return fmt.Errorf("omit audit outbox extensions: %w", err)
		}
		prepared.OutboxExtensionsOmitted = outbox.RowsAffected()

		return nil
	})
	if err != nil {
		return auditPurgePreparation{}, err
	}
	count := prepared.StagingItemsTombstoned + prepared.ArtifactsDeleted + prepared.OutboxExtensionsOmitted
	if _, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
		stage = 'cleaning_rollup_and_staging', staged_items_tombstoned = $2,
		progress_json = COALESCE(progress_json, '{}'::jsonb) || $3::jsonb,
		last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID, count,
		mustJSON(map[string]any{
			"audit_tombstones_created":        prepared.TombstonesCreated,
			"audit_staging_tombstoned":        prepared.StagingItemsTombstoned,
			"audit_artifacts_deleted":         prepared.ArtifactsDeleted,
			"audit_outbox_extensions_omitted": prepared.OutboxExtensionsOmitted,
		})); err != nil {
		return auditPurgePreparation{}, fmt.Errorf("checkpoint coordinated audit purge: %w", err)
	}
	_ = s.appendEvent(ctx, job.ID, "audit_purge_prepared", "audit rows and materializer evidence fenced", count)
	return prepared, nil
}

func auditPurgeReason(job retentionJobRow) string {
	if job.Origin != nil && *job.Origin == "manual" {
		return "manual_retention_purge"
	}
	return "scheduled_retention_purge"
}

func (s *Store) lockRetentionResourceForJob(ctx context.Context, tx pgx.Tx, job retentionJobRow) error {
	var generation int64
	var fenceGeneration int64
	var cutoff *time.Time
	var purgeState string
	if err := tx.QueryRow(ctx, `SELECT policy_generation, fence_generation, configured_logical_cutoff, purge_state
		FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, job.Scope.Table).
		Scan(&generation, &fenceGeneration, &cutoff, &purgeState); err != nil {
		return fmt.Errorf("lock retention resource %s: %w", job.Scope.Table, err)
	}
	if job.Origin != nil && *job.Origin == "automatic" {
		if job.PolicyGeneration == nil || job.FenceGeneration == nil ||
			generation != *job.PolicyGeneration || fenceGeneration != *job.FenceGeneration ||
			cutoff == nil || !cutoff.Equal(*job.Scope.Cutoff) {
			return fmt.Errorf("retention_resource_generation_changed")
		}
		return nil
	}
	if job.FenceGeneration == nil || fenceGeneration != *job.FenceGeneration {
		return fmt.Errorf("retention_purge_fence_changed")
	}
	if purgeState != "running" && purgeState != "recovery_required" {
		return fmt.Errorf("retention_purge_fence_changed")
	}
	return nil
}

func (s *Store) dropExpiredPartitionsUnderFence(ctx context.Context, job retentionJobRow, cutoff time.Time) ([]logretention.Partition, error) {
	var dropped []logretention.Partition
	err := pgxutil.InTx(ctx, s.pool, "retention_drop_fenced", func(tx pgx.Tx) error {
		if err := s.lockRetentionResourceForJob(ctx, tx, job); err != nil {
			return err
		}
		var err error
		dropped, err = s.logRetention.DropExpiredPartitionsTx(ctx, tx, job.Scope.Table, cutoff)
		return err
	})
	return dropped, err
}

func (s *Store) deleteBoundaryRowsUnderFence(ctx context.Context, job retentionJobRow, cutoff time.Time) (logretention.Partition, int64, bool, error) {
	var boundary logretention.Partition
	var deleted int64
	var ok bool
	err := pgxutil.InTx(ctx, s.pool, "retention_boundary_fenced", func(tx pgx.Tx) error {
		if err := s.lockRetentionResourceForJob(ctx, tx, job); err != nil {
			return err
		}
		var err error
		boundary, ok, err = s.logRetention.BoundaryPartitionForCutoffTx(ctx, tx, job.Scope.Table, cutoff)
		if err != nil || !ok {
			return err
		}
		deleted, err = s.logRetention.DeleteBoundaryRowsTx(ctx, tx, job.Scope.Table, cutoff)
		return err
	})
	return boundary, deleted, ok, err
}

// publishScheduledRetention publishes the logical completion for a scheduled job:
// floor advances to the cutoff and the terminal state is written.
func (s *Store) publishScheduledRetention(ctx context.Context, job retentionJobRow) error {
	cutoff := *job.Scope.Cutoff
	err := pgxutil.InTx(ctx, s.pool, "retention_scheduled_publish", func(tx pgx.Tx) error {
		var resourceGeneration int64
		var resourceFenceGeneration int64
		var resourceCutoff *time.Time
		if job.FenceGeneration == nil {
			return fmt.Errorf("publish retention floor fence generation unavailable")
		}
		if err := tx.QueryRow(ctx, `UPDATE log_retention_policy_resources SET
			published_retention_floor = CASE
				WHEN published_retention_floor IS NULL OR published_retention_floor < $2 THEN $2
				ELSE published_retention_floor END,
			fence_generation = fence_generation + CASE
				WHEN published_retention_floor IS NULL OR published_retention_floor < $2 THEN 1 ELSE 0 END,
			updated_at = now()
			WHERE dataset = $1 AND policy_generation = $3 AND fence_generation = $4 AND configured_logical_cutoff = $2
			RETURNING policy_generation, fence_generation, configured_logical_cutoff`, job.Scope.Table, cutoff, job.PolicyGeneration, job.FenceGeneration).Scan(&resourceGeneration, &resourceFenceGeneration, &resourceCutoff); err != nil {
			return fmt.Errorf("publish retention floor fence: %w", err)
		}
		if resourceGeneration == 0 || resourceFenceGeneration == 0 || resourceCutoff == nil {
			return fmt.Errorf("publish retention floor fence unavailable")
		}
		source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, job.Scope.Table, s.now().UTC())
		if err != nil {
			return fmt.Errorf("load retention coverage source: %w", err)
		}
		if err := statsdomain.RefreshActualCoverageProjection(ctx, tx, source, s.now().UTC()); err != nil {
			return fmt.Errorf("publish retention coverage: %w", err)
		}
		if job.Scope.Table == "audit_logs" {
			if err := auditdomain.MarkAuditRetentionReady(ctx, tx, s.now().UTC()); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE management_jobs SET
			state = 'succeeded', terminal_disposition = 'completed',
			stage = 'finished', visibility_state = 'scheduled_cutoff_active',
			finished_at = now(), locked_by = NULL, locked_until = NULL, last_heartbeat_at = now(), updated_at = now()
			WHERE id = $1 AND state = 'running'`, job.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("scheduled retention job is no longer running")
		}
		return nil
	})
	if err != nil {
		return s.failRetentionJob(ctx, job, "floor_publish_failed", "publish retention floor failed")
	}
	_ = s.appendEvent(ctx, job.ID, "succeeded", "log retention job succeeded", 0)
	return nil
}

// acquireManualRetentionFence atomically binds a manual job to the resource's
// semantic fence generation. A fresh purge advances the generation exactly
// when purge_state enters running; a recovery retry reuses the generation that
// was recorded when the resource entered recovery_required.
func (s *Store) acquireManualRetentionFence(ctx context.Context, job retentionJobRow) (int64, error) {
	var fenceGeneration int64
	err := pgxutil.InTx(ctx, s.pool, "retention_manual_fence", func(tx pgx.Tx) error {
		var purgeState string
		if err := tx.QueryRow(ctx, `SELECT fence_generation, purge_state
			FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, job.Scope.Table).
			Scan(&fenceGeneration, &purgeState); err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("retention_purge_fence_changed")
			}
			return fmt.Errorf("load retention fence: %w", err)
		}
		switch purgeState {
		case "idle", "published":
			// Validate the sealed intent while the resource is still outside
			// purge.  The validation must precede the state transition so a
			// stale preflight cannot make reads unavailable.
			if err := s.validateManualPreflightBeforeFence(ctx, tx, job); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `UPDATE log_retention_policy_resources SET
				purge_state = 'running', fence_generation = fence_generation + 1, updated_at = now()
				WHERE dataset = $1 AND fence_generation = $2 AND purge_state IN ('idle','published')
				RETURNING fence_generation`, job.Scope.Table, fenceGeneration).Scan(&fenceGeneration); err != nil {
				return fmt.Errorf("acquire retention resource fence: %w", err)
			}
		case "running", "recovery_required":
			// Only the recovery retry of the job that owns the existing fence
			// may reuse it.  A fresh job must never attach to another purge.
			if job.FenceGeneration == nil || *job.FenceGeneration != fenceGeneration {
				return fmt.Errorf("retention_purge_fence_changed")
			}
		default:
			return fmt.Errorf("retention_purge_fence_changed")
		}
		if job.FenceGeneration != nil && purgeState != "idle" && purgeState != "published" && *job.FenceGeneration != fenceGeneration {
			return fmt.Errorf("retention_purge_fence_changed")
		}
		tag, err := tx.Exec(ctx, `UPDATE management_jobs SET
			fence_generation = COALESCE(fence_generation, $2),
			stage = 'acquiring_purge_fence', last_heartbeat_at = now(), updated_at = now()
			WHERE id = $1 AND (fence_generation IS NULL OR fence_generation = $2)`, job.ID, fenceGeneration)
		if err != nil {
			return fmt.Errorf("bind retention job fence: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("retention_purge_fence_changed")
		}
		return nil
	})
	return fenceGeneration, err
}

// processManualRetentionPurge executes a manual purge job under the owning purge
// fence with delete-all purge_to_time freezing and final epoch/coverage
// publication (SPEC §6.5).
func (s *Store) processManualRetentionPurge(ctx context.Context, job retentionJobRow) error {
	if job.Scope.Table == "" {
		return s.failRetentionJob(ctx, job, "retention_scope_invalid", "manual purge job has no table scope")
	}

	// Enter the owner fence before deriving the delete-all execution cutoff.
	// A recovery retry keeps the existing fence; a fresh manual job acquires it
	// exactly once. This ordering makes the cutoff and purge state one
	// execution decision and leaves a recoverable durable state after a crash.
	fenceGeneration, err := s.acquireManualRetentionFence(ctx, job)
	if err != nil {
		if errors.Is(err, errManualPreflightStale) {
			return s.failManualPreflightBeforeExecution(ctx, job)
		}
		return s.failRetentionJob(ctx, job, "purge_fence_unavailable", "retention resource fence failed")
	}
	job.FenceGeneration = &fenceGeneration

	// Execution fence: freeze purge_to_time exactly once for delete-all.
	var purgeToTime *time.Time
	if job.Scope.DeleteAll {
		var frozen time.Time
		var newlyFrozen bool
		err := s.pool.QueryRow(ctx, `WITH current AS (
			SELECT purge_to_time FROM management_jobs WHERE id = $1 FOR UPDATE
		), updated AS (
			UPDATE management_jobs AS j SET purge_to_time = COALESCE(j.purge_to_time, $2),
				stage = 'acquiring_purge_fence', last_heartbeat_at = now(), updated_at = now()
			FROM current WHERE j.id = $1
			RETURNING j.purge_to_time
		) SELECT updated.purge_to_time, current.purge_to_time IS NULL
		FROM updated CROSS JOIN current`, job.ID, s.now().UTC()).Scan(&frozen, &newlyFrozen)
		if err != nil {
			return s.failRetentionJob(ctx, job, "purge_fence_unavailable", "freeze purge_to_time failed")
		}
		purgeToTime = &frozen
		if newlyFrozen {
			_ = s.appendEvent(ctx, job.ID, "purge_to_time_frozen", "delete-all purge_to_time frozen at execution fence", 0)
		}
	} else {
		purgeToTime = job.Scope.Cutoff
	}
	if purgeToTime == nil || purgeToTime.After(s.now().UTC()) {
		return s.failRetentionJob(ctx, job, "retention_cutoff_invalid", "manual purge cutoff is missing or in the future")
	}

	// Enter coordinated purge state: affected reads fail closed while running.
	jobTag, err := s.pool.Exec(ctx, `UPDATE management_jobs SET purge_state = 'running',
		stage = 'purge_running', visibility_state = 'purge_unavailable',
		last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID)
	if err != nil {
		return s.failRetentionJob(ctx, job, "purge_fence_unavailable", "enter purge running failed")
	}
	if jobTag.RowsAffected() != 1 {
		return s.failRetentionJob(ctx, job, "purge_fence_unavailable", "manual purge job is no longer active")
	}
	_ = s.appendEvent(ctx, job.ID, "purge_started", "manual coordinated purge started", 0)

	// Reuse the fenced physical path with the execution-time cutoff frozen
	// above. The scope remains delete_all for truthful job reporting.
	if job.Scope.DeleteAll {
		job.Scope.Cutoff = purgeToTime
	}
	if err := s.executePhysicalReclaim(ctx, job); err != nil {
		return err
	}

	// Final publish atomically bumps the revocation epoch, publishes the
	// frozen floor, and terminalizes the job. The resource predicate prevents
	// a recovered/competing worker from publishing a second epoch.
	err = pgxutil.InTx(ctx, s.pool, "retention_manual_publish", func(tx pgx.Tx) error {
		var epoch int64
		var fenceGeneration int64
		if job.FenceGeneration == nil {
			return fmt.Errorf("publish revocation fence generation unavailable")
		}
		if err := tx.QueryRow(ctx, `UPDATE log_retention_policy_resources SET
			retention_revocation_epoch = retention_revocation_epoch + 1,
				purge_state = 'published', published_retention_floor = CASE
					WHEN published_retention_floor IS NULL OR published_retention_floor < $2 THEN $2
					ELSE published_retention_floor END,
				fence_generation = fence_generation + 1, updated_at = now()
			WHERE dataset = $1 AND purge_state IN ('running','recovery_required') AND fence_generation = $3
			RETURNING retention_revocation_epoch, fence_generation`, job.Scope.Table, purgeToTime, job.FenceGeneration).Scan(&epoch, &fenceGeneration); err != nil {
			return fmt.Errorf("publish revocation epoch: %w", err)
		}
		source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, job.Scope.Table, s.now().UTC())
		if err != nil {
			return fmt.Errorf("load manual retention coverage source: %w", err)
		}
		if err := statsdomain.RefreshActualCoverageProjection(ctx, tx, source, s.now().UTC()); err != nil {
			return fmt.Errorf("publish manual retention coverage: %w", err)
		}
		if job.Scope.Table == "audit_logs" {
			if err := auditdomain.MarkAuditRetentionReady(ctx, tx, s.now().UTC()); err != nil {
				return err
			}
		}
		resultJSON := mustJSON(map[string]any{
			"published_epoch":    fmt.Sprintf("%d", epoch),
			"published_floor":    purgeToTime.UTC().Format(time.RFC3339),
			"last_checkpoint_at": s.now().UTC().Format(time.RFC3339),
		})
		tag, err := tx.Exec(ctx, `UPDATE management_jobs SET
			state = 'succeeded', terminal_disposition = 'completed', stage = 'finished',
			purge_state = 'published', visibility_state = 'revoked',
			progress_json = COALESCE(progress_json, '{}'::jsonb) || $2::jsonb,
			finished_at = now(), locked_by = NULL, locked_until = NULL, last_heartbeat_at = now(), updated_at = now()
			WHERE id = $1 AND state = 'running'`, job.ID, resultJSON)
		if err != nil {
			return fmt.Errorf("complete manual purge: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("manual purge job is no longer running")
		}
		return nil
	})
	if err != nil {
		return s.failRetentionJob(ctx, job, "revocation_publish_failed", "publish revocation epoch failed")
	}
	_ = s.appendEvent(ctx, job.ID, "purge_published", "manual purge published revocation epoch and coverage", 0)
	return nil
}

func (s *Store) failRetentionJob(ctx context.Context, job retentionJobRow, code string, message string) error {
	if job.Scope.Table == "audit_logs" {
		var materializerState string
		if err := s.pool.QueryRow(ctx, `SELECT materializer_state FROM audit_retention_fence_projections WHERE id = 1`).Scan(&materializerState); err == nil && materializerState != "ready" {
			// The owner projection remains fail-closed after any post-fence
			// failure. Recovery/final publish is the only path allowed to clear
			// it; a generic retry must not reopen audit evidence.
			_ = auditdomain.MarkAuditRetentionBlocked(ctx, s.pool, s.now().UTC())
		}
	}
	if job.Origin != nil && *job.Origin == "manual" && job.Scope.Table != "" {
		var purgeState string
		if err := s.pool.QueryRow(ctx, `SELECT purge_state FROM log_retention_policy_resources WHERE dataset = $1`, job.Scope.Table).Scan(&purgeState); err == nil && (purgeState == "running" || purgeState == "recovery_required") {
			// A manual purge that has acquired its fence is recoverable state,
			// not a terminal failure. Keep the job visibly running and keep reads
			// fenced until an explicit recovery/final-publish path repairs the
			// epoch and floor.
			var recoveryFenceGeneration int64
			if err := s.pool.QueryRow(ctx, `UPDATE log_retention_policy_resources SET
				purge_state = 'recovery_required',
				fence_generation = fence_generation + CASE WHEN purge_state = 'running' THEN 1 ELSE 0 END,
				updated_at = now()
				WHERE dataset = $1 AND purge_state IN ('running','recovery_required')
				RETURNING fence_generation`, job.Scope.Table).Scan(&recoveryFenceGeneration); err == nil {
				_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET fence_generation = $2
					WHERE id = $1`, job.ID, recoveryFenceGeneration)
			}
			_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET
				state = 'running', terminal_disposition = NULL, error_code = $2,
				error_message = $3, finished_at = NULL, stage = 'publishing_epoch_coverage',
				locked_by = NULL, locked_until = NULL,
				last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID, code, message)
			return fmt.Errorf("%s", message)
		}
	}
	_, _ = s.pool.Exec(ctx, `UPDATE management_jobs SET
		attempt_count = LEAST(attempt_count + 1, max_attempts),
		state = CASE WHEN attempt_count + 1 >= max_attempts THEN 'failed' ELSE 'queued' END,
		error_code = $2, error_message = $3,
		next_attempt_at = CASE WHEN attempt_count + 1 >= max_attempts THEN next_attempt_at ELSE now() + interval '5 seconds' END,
		finished_at = CASE WHEN attempt_count + 1 >= max_attempts THEN now() ELSE finished_at END,
		locked_by = NULL, locked_until = NULL, updated_at = now() WHERE id = $1`, job.ID, code, message)
	return fmt.Errorf("%s", message)
}

func (s *Store) cancelRetentionJobWithoutDataChange(ctx context.Context, job retentionJobRow, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
		state = 'cancelled', terminal_disposition = 'cancelled', stage = 'finished',
		finished_at = now(), locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1`, job.ID)
	if err != nil {
		return fmt.Errorf("cancel superseded job: %w", err)
	}
	_ = s.appendEvent(ctx, job.ID, "cancelled", reason, 0)
	return nil
}

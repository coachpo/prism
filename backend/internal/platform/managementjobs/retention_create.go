package managementjobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

const currentWorkerGeneration = 1

// newOperationID returns a random UUIDv4-formatted operation id.
func newOperationID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("op-%d", time.Now().UnixNano())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// canonicalRequestHash hashes the canonical non-secret request identity.
func canonicalRequestHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func isRetentionDataset(dataset string) bool {
	switch dataset {
	case "request_logs", "audit_logs", "usage_request_events", "loadbalance_events":
		return true
	default:
		return false
	}
}

// CreateManualRetentionJob creates one durable manual purge job (store-level
// entry point used by tests and the settings route composition).
func (s *Store) CreateManualRetentionJob(ctx context.Context, dataset string, cutoff *time.Time, deleteAll bool, operationID string) (RetentionJobSummaryDTO, error) {
	var result RetentionJobSummaryDTO
	err := pgxutil.InTx(ctx, s.pool, "retention_manual_create", func(tx pgx.Tx) error {
		job, err := s.CreateManualRetentionJobTx(ctx, tx, dataset, cutoff, deleteAll, operationID, "", s.now().UTC())
		if err != nil {
			return err
		}
		result = job
		return nil
	})
	return result, err
}

// CreateAutomaticRetentionJobTx creates one durable automatic job inside the
// caller's transaction and returns its id.
func (s *Store) CreateAutomaticRetentionJobTx(ctx context.Context, tx pgx.Tx, dataset string, cutoff time.Time, settingsRevision int64, policyGeneration int64, now time.Time) (string, error) {
	var fenceGeneration int64
	var purgeState string
	if err := tx.QueryRow(ctx, `SELECT fence_generation, purge_state FROM log_retention_policy_resources WHERE dataset = $1 FOR SHARE`, dataset).Scan(&fenceGeneration, &purgeState); err != nil {
		return "", fmt.Errorf("load retention fence generation %s: %w", dataset, err)
	}
	return s.createAutomaticRetentionJobTx(ctx, tx, dataset, cutoff, settingsRevision, policyGeneration, fenceGeneration, purgeState, now)
}

// CreateManualRetentionJobTx creates one durable manual purge job from a sealed
// preflight inside the caller's transaction (SPEC §6.4). The dataset resource
// reservation is enforced by the partial unique index.
func (s *Store) CreateManualRetentionJobTx(ctx context.Context, tx pgx.Tx, dataset string, cutoff *time.Time, deleteAll bool, operationID string, preflightID string, now time.Time) (RetentionJobSummaryDTO, error) {
	if !isRetentionDataset(dataset) {
		return RetentionJobSummaryDTO{}, fmt.Errorf("retention_dataset_invalid")
	}
	var purgeState string
	if err := tx.QueryRow(ctx, `SELECT purge_state FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, dataset).Scan(&purgeState); err != nil {
		return RetentionJobSummaryDTO{}, fmt.Errorf("retention_resource_missing")
	}
	if purgeState == "running" || purgeState == "recovery_required" {
		return RetentionJobSummaryDTO{}, fmt.Errorf("retention_job_conflict")
	}
	var runningAutomatic bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'automatic' AND resource_key = $1 AND state IN ('running','cancel_requested')
	)`, dataset).Scan(&runningAutomatic); err != nil {
		return RetentionJobSummaryDTO{}, err
	}
	if runningAutomatic {
		return RetentionJobSummaryDTO{}, fmt.Errorf("retention_job_conflict")
	}
	// A manual reservation supersedes only unclaimed automatic intent. A
	// running automatic job was rejected above and is never silently cancelled.
	if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
		state = 'cancelled', terminal_disposition = 'cancelled', finished_at = now(),
		cancel_requested = TRUE, updated_at = now()
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'automatic' AND resource_key = $1 AND state = 'queued'`, dataset); err != nil {
		return RetentionJobSummaryDTO{}, err
	}
	jobID := "job_" + randomHex(12)
	requestHash := canonicalRequestHash("manual", operationID, preflightID, dataset)
	scopeJSON, err := json.Marshal(LogRetentionScope{Table: dataset, Cutoff: cutoff, DeleteAll: deleteAll})
	if err != nil {
		return RetentionJobSummaryDTO{}, err
	}
	progressJSON, err := retentionProtectionEvidence(ctx, tx, dataset, "manual", now)
	if err != nil {
		return RetentionJobSummaryDTO{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO management_jobs (
		id, type, state, requested_by, requested_at, priority, profile_id, scope_json, reason,
		contract_version, operation_id, request_hash, resource_key, origin, preflight_id,
		purge_state, stage, visibility_state, boundary_rows_deleted, worker_generation, progress_json, created_at, updated_at
	) VALUES ($1, 'log_retention', 'queued', 'operator', $2, 'maintenance', 0, $3, $4,
		2, $5, $6, $7, 'manual', $8, 'not_started', 'queued', 'unchanged', 0, $9, $10, $2, $2)`,
		jobID, now.UTC(), scopeJSON, "manual log retention purge", operationID, requestHash, dataset, preflightID, currentWorkerGeneration, progressJSON)
	if err != nil {
		return RetentionJobSummaryDTO{}, fmt.Errorf("insert manual retention job: %w", err)
	}
	row, err := s.loadRetentionRow(ctx, tx, jobID)
	if err != nil {
		return RetentionJobSummaryDTO{}, err
	}
	return RetentionJobSummary(row), nil
}

func (s *Store) createAutomaticRetentionJobTx(ctx context.Context, tx pgx.Tx, dataset string, cutoff time.Time, settingsRevision int64, policyGeneration int64, fenceGeneration int64, purgeState string, now time.Time) (string, error) {
	scopeJSON, err := json.Marshal(LogRetentionScope{Table: dataset, Cutoff: &cutoff})
	if err != nil {
		return "", err
	}
	progressJSON, err := retentionProtectionEvidence(ctx, tx, dataset, "automatic", now)
	if err != nil {
		return "", err
	}
	operationID := newOperationID()
	requestHash := canonicalRequestHash(dataset, cutoff.UTC().Format(time.RFC3339), fmt.Sprintf("%d", policyGeneration))
	idempotencyKey := fmt.Sprintf("%s:%s:%d", dataset, cutoff.UTC().Format("2006-01-02"), policyGeneration)
	jobID := "job_" + randomHex(12)
	initialStage := "queued"
	if purgeState == "running" || purgeState == "recovery_required" {
		initialStage = "waiting_for_resource"
	}
	var manualReserved bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'manual' AND resource_key = $1 AND state IN ('queued','running')
	)`, dataset).Scan(&manualReserved); err != nil {
		return "", fmt.Errorf("check manual retention reservation for %s: %w", dataset, err)
	}
	if manualReserved {
		initialStage = "waiting_for_resource"
	}

	var insertedID *string
	err = tx.QueryRow(ctx, `INSERT INTO management_jobs (
		id, type, state, requested_by, requested_at, priority, idempotency_key, profile_id,
		scope_json, reason, contract_version, operation_id, request_hash, resource_key,
		origin, policy_revision, policy_generation, fence_generation, purge_state, stage, visibility_state,
		boundary_rows_deleted, worker_generation, progress_json, created_at, updated_at
	) VALUES ($1, 'log_retention', 'queued', $2, $3, 'maintenance', $4, 0, $5, $6,
		2, $7, $8, $9, 'automatic', $10, $11, $12, 'not_started', $15, 'unchanged', 0, $13, $14, $3, $3)
	ON CONFLICT (type, requested_by, idempotency_key) WHERE idempotency_key IS NOT NULL
	DO NOTHING
	RETURNING id`,
		jobID, scheduledLogRetentionRequestedBy, now, idempotencyKey, scopeJSON,
		scheduledLogRetentionReason, operationID, requestHash, dataset, settingsRevision, policyGeneration, fenceGeneration, currentWorkerGeneration, progressJSON, initialStage).Scan(&insertedID)
	if err != nil && err != pgx.ErrNoRows {
		return "", fmt.Errorf("insert automatic retention job for %s: %w", dataset, err)
	}
	if insertedID == nil {
		// Idempotent duplicate: the same identity already created a job.
		var existing string
		if err := tx.QueryRow(ctx, `SELECT id FROM management_jobs
			WHERE type = 'log_retention' AND requested_by = $1 AND idempotency_key = $2`,
			scheduledLogRetentionRequestedBy, idempotencyKey).Scan(&existing); err == nil {
			return existing, nil
		}
		return jobID, nil
	}
	return *insertedID, nil
}

// retentionProtectionEvidence is persisted with the accepted job intent so
// the job center can render the exact discriminated protection union after a
// refresh. It is deliberately captured in the same transaction as the job;
// the summary layer never derives protection from requested_at or purge_to_time.
func retentionProtectionEvidence(ctx context.Context, tx pgx.Tx, dataset, origin string, now time.Time) ([]byte, error) {
	if origin == "automatic" && dataset != "audit_logs" {
		return json.Marshal(map[string]any{
			"protection": map[string]any{
				"kind":     "observe_query_token",
				"deadline": observeProtectionDeadline(now).UTC().Format(time.RFC3339),
			},
		})
	}
	if dataset != "audit_logs" {
		return json.Marshal(map[string]any{"protection": map[string]any{"kind": "none"}})
	}

	var epoch int64
	var publishedFloor *time.Time
	if err := tx.QueryRow(ctx, `SELECT retention_revocation_epoch, published_retention_floor
		FROM log_retention_policy_resources WHERE dataset = $1 FOR SHARE`, dataset).
		Scan(&epoch, &publishedFloor); err != nil {
		return nil, fmt.Errorf("load audit retention epoch for job protection: %w", err)
	}
	projection, err := auditdomain.LoadAuditFenceMaterializerProjection(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"protection": map[string]any{
			"kind":                  "audit_retention_fence",
			"audit_retention_epoch": fmt.Sprintf("%d", epoch),
			"published_floor":       formatTimePtr(publishedFloor),
			"reader_fence_state":    projection.ReaderFenceState,
			"materializer_state":    projection.MaterializerState,
		},
	})
}

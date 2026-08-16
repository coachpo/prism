package managementjobs

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

// v2 log-retention planning and execution (Settings SPEC §5.4/§6.5/§7).
//
// The v2 contract replaces the legacy rolling `now - N*24h` scheduler with a
// UTC day-aligned logical cutoff, durable per-dataset policy resources,
// protection-gated physical reclaim (24h token TTL + 24h grace for the three
// Observe domains; audit uses its own fence projection), manual purge with
// execution-fence `purge_to_time` and final epoch/coverage publication.
//
// All v2 rows carry contract_version=2, a durable operation_id/request_hash,
// origin and worker_generation >= the fenced minimum. Legacy v1 rows drain
// only through the frozen generation-tagged v1 executor after the cutover
// authorizes legacy claim/delete.

const (
	v2WorkerGeneration = 1

	observeTokenTTLSeconds        = int64(24 * 60 * 60)
	observeProtectionGraceSeconds = int64(24 * 60 * 60)
)

// v2RetentionJobRow is the full v2/legacy log-retention job row projection.
type v2RetentionJobRow struct {
	ID                         string
	State                      string
	RequestedBy                string
	RequestedAt                time.Time
	StartedAt                  *time.Time
	FinishedAt                 *time.Time
	Scope                      LogRetentionScope
	Reason                     string
	ContractVersion            int
	OperationID                *string
	RequestHash                *string
	ResourceKey                *string
	Origin                     *string
	LegacyOriginProvenance     *string
	LegacyExecutionProvenance  *string
	PreflightID                *string
	PolicyRevision             *int64
	PolicyGeneration           *int64
	FenceGeneration            *int64
	PurgeToTime                *time.Time
	PurgeState                 *string
	Stage                      *string
	TerminalDisposition        *string
	LegacyOriginalState        *string
	BoundaryRowsDeleted        int64
	DroppedPartitionCount      *int64
	DroppedRowsEstimate        *int64
	StagedItemsTombstoned      *int64
	SensitiveArtifactBytes     *int64
	ClassificationEvidenceHash *string
	VisibilityState            *string
	WorkerGeneration           *int64
	AttemptCount               int
	CancelRequested            bool
	CancelAllowed              bool
	LastHeartbeatAt            *time.Time
	ErrorCode                  *string
	ErrorMessage               *string
	RowsMatchedEstimate        *int64
	BatchesCompleted           int64
	ProgressJSON               []byte
	NextAttemptAt              time.Time
}

const v2RetentionSelect = `
SELECT id, state, requested_by, requested_at, started_at, finished_at, scope_json,
       reason, contract_version, operation_id, request_hash, resource_key, origin,
       legacy_origin_provenance, legacy_execution_provenance, preflight_id,
	       policy_revision, policy_generation, fence_generation, purge_to_time, purge_state, stage,
       terminal_disposition, legacy_original_state, boundary_rows_deleted,
       dropped_partition_count, dropped_rows_estimate, staged_items_tombstoned,
       sensitive_artifact_bytes_deleted, classification_evidence_hash,
       visibility_state, worker_generation, attempt_count, cancel_requested,
       last_heartbeat_at, error_code, error_message, rows_matched_estimate,
       batches_completed, progress_json, next_attempt_at
FROM management_jobs`

func scanV2RetentionRow(scanner interface{ Scan(...any) error }) (v2RetentionJobRow, error) {
	var row v2RetentionJobRow
	var scopeRaw []byte
	var startedAt, finishedAt, purgeToTime, lastHeartbeat, nextAttempt *time.Time
	var contractVersion int
	if err := scanner.Scan(
		&row.ID, &row.State, &row.RequestedBy, &row.RequestedAt, &startedAt, &finishedAt, &scopeRaw,
		&row.Reason, &contractVersion, &row.OperationID, &row.RequestHash, &row.ResourceKey, &row.Origin,
		&row.LegacyOriginProvenance, &row.LegacyExecutionProvenance, &row.PreflightID,
		&row.PolicyRevision, &row.PolicyGeneration, &row.FenceGeneration, &purgeToTime, &row.PurgeState, &row.Stage,
		&row.TerminalDisposition, &row.LegacyOriginalState, &row.BoundaryRowsDeleted,
		&row.DroppedPartitionCount, &row.DroppedRowsEstimate, &row.StagedItemsTombstoned,
		&row.SensitiveArtifactBytes, &row.ClassificationEvidenceHash,
		&row.VisibilityState, &row.WorkerGeneration, &row.AttemptCount, &row.CancelRequested,
		&lastHeartbeat, &row.ErrorCode, &row.ErrorMessage, &row.RowsMatchedEstimate,
		&row.BatchesCompleted, &row.ProgressJSON, &nextAttempt,
	); err != nil {
		return v2RetentionJobRow{}, err
	}
	row.ContractVersion = contractVersion
	row.StartedAt = startedAt
	row.FinishedAt = finishedAt
	row.PurgeToTime = purgeToTime
	row.LastHeartbeatAt = lastHeartbeat
	if nextAttempt != nil {
		row.NextAttemptAt = *nextAttempt
	}
	_ = json.Unmarshal(scopeRaw, &row.Scope)
	return row, nil
}

func (s *Store) loadV2RetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (v2RetentionJobRow, error) {
	return scanV2RetentionRow(exec.QueryRow(ctx, v2RetentionSelect+` WHERE id = $1`, id))
}

func (s *Store) loadGlobalV2RetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (v2RetentionJobRow, error) {
	return scanV2RetentionRow(exec.QueryRow(ctx, v2RetentionSelect+` WHERE id = $1 AND type = 'log_retention' AND profile_id = 0`, id))
}

// authorizeLegacyDrain flips the DB fence flags so the frozen generation-tagged
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

// utcDayAlignedCutoff returns the UTC day-aligned logical cutoff for a policy
// (SPEC §3.3): date_trunc('day', server_now at UTC) - N days.
func utcDayAlignedCutoff(now time.Time, retentionDays int) time.Time {
	dayStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return dayStart.AddDate(0, 0, -retentionDays)
}

// observeProtectionDeadline is the earliest allowed physical reclaim instant:
// logical publication + token TTL + grace (at least 48h).
func observeProtectionDeadline(publishedAt time.Time) time.Time {
	return publishedAt.Add(time.Duration(observeTokenTTLSeconds+observeProtectionGraceSeconds) * time.Second)
}

// planScheduledRetentionV2 advances per-dataset policy resources and creates
// one durable v2 automatic job per destructive change (SPEC §5.4/§7.4).
func (s *Store) planScheduledRetentionV2(ctx context.Context) error {
	return pgxutil.InTx(ctx, s.pool, "retention_plan", func(tx pgx.Tx) error {
		var settingsRow struct {
			RequestLogs *int32
			AuditLogs   *int32
			Statistics  *int32
			Loadbalance *int32
			Revision    int64
		}
		err := tx.QueryRow(ctx, `SELECT request_logs_retention_days, audit_logs_retention_days,
			statistics_retention_days, loadbalance_events_retention_days, revision
			FROM log_retention_settings WHERE singleton_key = 'global'`).
			Scan(&settingsRow.RequestLogs, &settingsRow.AuditLogs, &settingsRow.Statistics, &settingsRow.Loadbalance, &settingsRow.Revision)
		if err != nil {
			return fmt.Errorf("load global log retention settings: %w", err)
		}

		now := s.now().UTC()
		datasets := []struct {
			dataset string
			days    *int32
		}{
			{"request_logs", settingsRow.RequestLogs},
			{"audit_logs", settingsRow.AuditLogs},
			{"usage_request_events", settingsRow.Statistics},
			{"loadbalance_events", settingsRow.Loadbalance},
		}

		for _, item := range datasets {
			var desiredCutoff *time.Time
			if item.days != nil {
				cutoff := utcDayAlignedCutoff(now, int(*item.days))
				desiredCutoff = &cutoff
			}
			var resourceCutoff *time.Time
			var resourceGeneration int64
			var resourceFenceGeneration int64
			var resourcePurgeState string
			err := tx.QueryRow(ctx, `SELECT configured_logical_cutoff, policy_generation, fence_generation, purge_state
				FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, item.dataset).
				Scan(&resourceCutoff, &resourceGeneration, &resourceFenceGeneration, &resourcePurgeState)
			if err != nil {
				return fmt.Errorf("load policy resource %s: %w", item.dataset, err)
			}
			if (desiredCutoff == nil && resourceCutoff == nil) ||
				(desiredCutoff != nil && resourceCutoff != nil && desiredCutoff.Equal(*resourceCutoff)) {
				continue
			}
			// A manual purge owns the resource while running or recovering. The
			// scheduler may observe the newer settings on its next tick, but it
			// must not rewrite the fenced source underneath a partial purge.
			if resourcePurgeState == "running" || resourcePurgeState == "recovery_required" {
				continue
			}
			destructive := (resourceCutoff == nil && desiredCutoff != nil) ||
				(resourceCutoff != nil && desiredCutoff != nil && desiredCutoff.Before(*resourceCutoff))

			newGeneration := resourceGeneration + 1
			if _, err := tx.Exec(ctx, `UPDATE log_retention_policy_resources
				SET policy_generation = $2, fence_generation = fence_generation + 1,
					settings_revision = $3, configured_logical_cutoff = $4, updated_at = now()
				WHERE dataset = $1`, item.dataset, newGeneration, settingsRow.Revision, desiredCutoff); err != nil {
				return fmt.Errorf("advance policy resource %s: %w", item.dataset, err)
			}
			ownerSource, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, item.dataset, now)
			if err != nil {
				return fmt.Errorf("load refreshed policy source %s: %w", item.dataset, err)
			}
			if err := statsdomain.RefreshActualCoverageProjection(ctx, tx, ownerSource, now); err != nil {
				return fmt.Errorf("refresh policy coverage %s: %w", item.dataset, err)
			}

			// Extension and disable are non-destructive changes. They advance the
			// owner generation but must not manufacture work for an older cutoff;
			// queued old-generation work is cancelled while a running worker must
			// revalidate the owner fence before every physical step.
			if !destructive {
				if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
					state = CASE WHEN state = 'queued' THEN 'cancelled' ELSE state END,
					terminal_disposition = CASE WHEN state = 'queued' THEN 'cancelled' ELSE NULL END,
					cancel_requested = TRUE,
					finished_at = CASE WHEN state = 'queued' THEN now() ELSE finished_at END,
					updated_at = now()
					WHERE type = 'log_retention' AND contract_version = 2 AND origin = 'automatic'
					  AND resource_key = $1 AND state IN ('queued','running','cancel_requested')
					  AND COALESCE(policy_generation, 0) < $2`, item.dataset, newGeneration); err != nil {
					return fmt.Errorf("cancel stale automatic retention work for %s: %w", item.dataset, err)
				}
				continue
			}

			if _, err := s.createV2AutomaticJobTx(ctx, tx, item.dataset, *desiredCutoff, settingsRow.Revision, newGeneration, resourceFenceGeneration+1, resourcePurgeState, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// ObserveTokenTTLSeconds exposes the fixed 24h token TTL (SPEC §3.3).
func ObserveTokenTTLSeconds() int64 { return observeTokenTTLSeconds }

// ObserveProtectionGraceSeconds exposes the fixed 24h extra protection grace.
func ObserveProtectionGraceSeconds() int64 { return observeProtectionGraceSeconds }

// ObserveProtectionDeadline returns the earliest allowed physical reclaim
// instant: logical publication + token TTL + grace (>= 48h).
func ObserveProtectionDeadline(publishedAt time.Time) time.Time {
	return observeProtectionDeadline(publishedAt)
}

// V2RetentionJobSummary is the settings-facing job DTO projection
// (Settings SPEC §7.2 GlobalRetentionJobSummary).
func V2RetentionJobSummary(row v2RetentionJobRow) V2RetentionJobSummaryDTO {
	dataset := ""
	if row.ResourceKey != nil {
		dataset = *row.ResourceKey
	}
	origin := ""
	if row.Origin != nil {
		origin = *row.Origin
	}
	mode := "cutoff"
	if row.Scope.DeleteAll {
		mode = "delete_all"
	}
	progress := V2JobProgressDTO{
		AccountingProvenance:          "v2_exact",
		Stage:                         "queued",
		VisibilityState:               "unchanged",
		PurgeState:                    "not_started",
		BoundaryRowsDeleted:           "0",
		BoundaryBatchesCompleted:      "0",
		RowsMatchedAccuracy:           "unavailable",
		DroppedPartitionCountAccuracy: "unavailable",
		DroppedRowsAccuracy:           "unavailable",
	}
	var persistedProgress struct {
		DroppedPartitions []string `json:"dropped_partitions"`
		Protection        any      `json:"protection"`
		LastCheckpointAt  *string  `json:"last_checkpoint_at"`
	}
	_ = json.Unmarshal(row.ProgressJSON, &persistedProgress)
	if persistedProgress.Protection != nil {
		progress.Protection = persistedProgress.Protection
	} else if row.ContractVersion == 1 {
		progress.Protection = map[string]any{"kind": "legacy_unknown"}
	} else if origin == "manual" && dataset != "audit_logs" {
		// Manual non-audit work has no Observe query-token protection of its
		// own. The execution fence is exposed by purge_state/stage instead.
		progress.Protection = map[string]any{"kind": "none"}
	} else {
		// Never infer an audit fence or a scheduled deadline from a display
		// timestamp. Rows created before v2 protection evidence was persisted
		// remain explicitly unknown and are handled fail-closed by the worker.
		progress.Protection = map[string]any{"kind": "legacy_unknown"}
	}
	progress.DroppedPartitionNamesPreview = boundedStringPreview(persistedProgress.DroppedPartitions, 20)
	if len(persistedProgress.DroppedPartitions) > 0 {
		total := fmt.Sprintf("%d", len(persistedProgress.DroppedPartitions))
		progress.DroppedPartitionNamesTotalCount = &total
		progress.DroppedPartitionNamesTruncated = len(persistedProgress.DroppedPartitions) > 20
	}
	if row.ContractVersion == 1 {
		progress.AccountingProvenance = "legacy_boundary_only"
		progress.VisibilityState = "legacy_unknown"
		progress.PurgeState = "legacy_unknown"
	}
	if row.Stage != nil {
		progress.Stage = *row.Stage
	}
	if row.VisibilityState != nil {
		progress.VisibilityState = *row.VisibilityState
	}
	if row.PurgeState != nil {
		progress.PurgeState = *row.PurgeState
	}
	progress.BoundaryRowsDeleted = fmt.Sprintf("%d", row.BoundaryRowsDeleted)
	progress.BoundaryBatchesCompleted = fmt.Sprintf("%d", row.BatchesCompleted)
	if row.RowsMatchedEstimate != nil {
		value := fmt.Sprintf("%d", *row.RowsMatchedEstimate)
		progress.RowsMatchedEstimate = &value
		progress.RowsMatchedAccuracy = "estimated"
	}
	if row.DroppedPartitionCount != nil {
		value := fmt.Sprintf("%d", *row.DroppedPartitionCount)
		progress.DroppedPartitionCount = &value
		progress.DroppedPartitionCountAccuracy = "exact"
	}
	if row.DroppedRowsEstimate != nil {
		value := fmt.Sprintf("%d", *row.DroppedRowsEstimate)
		progress.DroppedRowsEstimate = &value
		progress.DroppedRowsAccuracy = "estimated"
	}
	if row.StagedItemsTombstoned != nil {
		value := fmt.Sprintf("%d", *row.StagedItemsTombstoned)
		progress.StagedItemsTombstoned = &value
	}
	if row.SensitiveArtifactBytes != nil {
		value := fmt.Sprintf("%d", *row.SensitiveArtifactBytes)
		progress.SensitiveArtifactBytesDeleted = &value
	}
	progress.LastCheckpointAt = persistedProgress.LastCheckpointAt
	if progress.LastCheckpointAt == nil {
		progress.LastCheckpointAt = formatTimePtr2(row.LastHeartbeatAt)
	}

	cancelAllowed := false
	if row.ContractVersion == 2 {
		if origin == "manual" && row.State == "queued" {
			cancelAllowed = true
		}
		if origin == "automatic" && (row.State == "queued" || row.State == "running") && !row.CancelRequested {
			cancelAllowed = true
		}
	}
	summary := V2RetentionJobSummaryDTO{
		ID:                        row.ID,
		ContractVersion:           row.ContractVersion,
		Type:                      "log_retention",
		JobScope:                  "instance",
		Origin:                    origin,
		LegacyOriginProvenance:    row.LegacyOriginProvenance,
		LegacyExecutionProvenance: row.LegacyExecutionProvenance,
		Dataset:                   dataset,
		State:                     row.State,
		TerminalDisposition:       row.TerminalDisposition,
		LegacyOriginalState:       row.LegacyOriginalState,
		Mode:                      mode,
		Cutoff:                    formatTimePtr2(row.Scope.Cutoff),
		PurgeToTime:               formatTimePtr2(row.PurgeToTime),
		PolicyRevision:            formatInt64Ptr(row.PolicyRevision),
		PreflightID:               row.PreflightID,
		OperationID:               row.OperationID,
		RequestedAt:               row.RequestedAt.UTC().Format(time.RFC3339),
		StartedAt:                 formatTimePtr2(row.StartedAt),
		FinishedAt:                formatTimePtr2(row.FinishedAt),
		LastHeartbeatAt:           formatTimePtr2(row.LastHeartbeatAt),
		AttemptCount:              row.AttemptCount,
		CancelAllowed:             cancelAllowed,
		Progress:                  progress,
	}
	if row.ErrorCode != nil {
		message := ""
		if row.ErrorMessage != nil {
			message = *row.ErrorMessage
		}
		summary.Error = &V2JobSafeError{Code: *row.ErrorCode, Message: message}
	}
	return summary
}

func boundedStringPreview(values []string, limit int) []string {
	// Public progress requires an array even when persistence has no names;
	// a nil slice would encode as JSON null.
	if len(values) == 0 {
		return []string{}
	}
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

// V2RetentionJobSummaryDTO mirrors Settings SPEC §7.2.
type V2RetentionJobSummaryDTO struct {
	ID                        string           `json:"id"`
	ContractVersion           int              `json:"contract_version"`
	Type                      string           `json:"type"`
	JobScope                  string           `json:"job_scope"`
	Origin                    string           `json:"origin"`
	LegacyOriginProvenance    *string          `json:"legacy_origin_provenance"`
	LegacyExecutionProvenance *string          `json:"legacy_execution_provenance"`
	Dataset                   string           `json:"dataset"`
	State                     string           `json:"state"`
	TerminalDisposition       *string          `json:"terminal_disposition"`
	LegacyOriginalState       *string          `json:"legacy_original_state"`
	Mode                      string           `json:"mode"`
	Cutoff                    *string          `json:"cutoff"`
	PurgeToTime               *string          `json:"purge_to_time"`
	PolicyRevision            *string          `json:"policy_revision"`
	PreflightID               *string          `json:"preflight_id"`
	OperationID               *string          `json:"operation_id"`
	RequestedAt               string           `json:"requested_at"`
	StartedAt                 *string          `json:"started_at"`
	FinishedAt                *string          `json:"finished_at"`
	LastHeartbeatAt           *string          `json:"last_heartbeat_at"`
	AttemptCount              int              `json:"attempt_count"`
	CancelAllowed             bool             `json:"cancel_allowed"`
	Progress                  V2JobProgressDTO `json:"progress"`
	Error                     *V2JobSafeError  `json:"error"`
}

type V2JobProgressDTO struct {
	AccountingProvenance            string   `json:"accounting_provenance"`
	Stage                           string   `json:"stage"`
	VisibilityState                 string   `json:"visibility_state"`
	PurgeState                      string   `json:"purge_state"`
	Protection                      any      `json:"protection"`
	RowsMatchedEstimate             *string  `json:"rows_matched_estimate"`
	RowsMatchedAccuracy             string   `json:"rows_matched_accuracy"`
	BoundaryRowsDeleted             string   `json:"boundary_rows_deleted"`
	BoundaryBatchesCompleted        string   `json:"boundary_batches_completed"`
	DroppedPartitionCount           *string  `json:"dropped_partition_count"`
	DroppedPartitionCountAccuracy   string   `json:"dropped_partition_count_accuracy"`
	DroppedPartitionNamesPreview    []string `json:"dropped_partition_names_preview"`
	DroppedPartitionNamesTotalCount *string  `json:"dropped_partition_names_total_count"`
	DroppedPartitionNamesTruncated  bool     `json:"dropped_partition_names_truncated"`
	DroppedRowsEstimate             *string  `json:"dropped_rows_estimate"`
	DroppedRowsAccuracy             string   `json:"dropped_rows_accuracy"`
	StagedItemsTombstoned           *string  `json:"staged_items_tombstoned"`
	SensitiveArtifactBytesDeleted   *string  `json:"sensitive_artifact_bytes_deleted"`
	LastCheckpointAt                *string  `json:"last_checkpoint_at"`
}

type V2JobSafeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func formatTimePtr2(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func formatInt64Ptr(value *int64) *string {
	if value == nil {
		return nil
	}
	formatted := fmt.Sprintf("%d", *value)
	return &formatted
}

// CreateV2ManualJob creates one durable v2 manual purge job (store-level
// entry point used by tests and the settings route composition).
func (s *Store) CreateV2ManualJob(ctx context.Context, dataset string, cutoff *time.Time, deleteAll bool, operationID string) (V2RetentionJobSummaryDTO, error) {
	var result V2RetentionJobSummaryDTO
	err := pgxutil.InTx(ctx, s.pool, "retention_manual_create", func(tx pgx.Tx) error {
		job, err := s.CreateV2ManualJobTx(ctx, tx, dataset, cutoff, deleteAll, operationID, "", s.now().UTC())
		if err != nil {
			return err
		}
		result = job
		return nil
	})
	return result, err
}

// PlanScheduledRetentionV2 is the exported v2 UTC-day planner entry point.
func (s *Store) PlanScheduledRetentionV2(ctx context.Context) error {
	return s.planScheduledRetentionV2(ctx)
}

// CreateV2AutomaticJobTx creates one durable v2 automatic job inside the
// caller's transaction and returns its id.
func (s *Store) CreateV2AutomaticJobTx(ctx context.Context, tx pgx.Tx, dataset string, cutoff time.Time, settingsRevision int64, policyGeneration int64, now time.Time) (string, error) {
	var fenceGeneration int64
	var purgeState string
	if err := tx.QueryRow(ctx, `SELECT fence_generation, purge_state FROM log_retention_policy_resources WHERE dataset = $1 FOR SHARE`, dataset).Scan(&fenceGeneration, &purgeState); err != nil {
		return "", fmt.Errorf("load retention fence generation %s: %w", dataset, err)
	}
	return s.createV2AutomaticJobTx(ctx, tx, dataset, cutoff, settingsRevision, policyGeneration, fenceGeneration, purgeState, now)
}

// CreateV2ManualJobTx creates one durable v2 manual purge job from a sealed
// preflight inside the caller's transaction (SPEC §6.4). The dataset resource
// reservation is enforced by the partial unique index.
func (s *Store) CreateV2ManualJobTx(ctx context.Context, tx pgx.Tx, dataset string, cutoff *time.Time, deleteAll bool, operationID string, preflightID string, now time.Time) (V2RetentionJobSummaryDTO, error) {
	if !isV2RetentionDataset(dataset) {
		return V2RetentionJobSummaryDTO{}, fmt.Errorf("retention_dataset_invalid")
	}
	var purgeState string
	if err := tx.QueryRow(ctx, `SELECT purge_state FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, dataset).Scan(&purgeState); err != nil {
		return V2RetentionJobSummaryDTO{}, fmt.Errorf("retention_resource_missing")
	}
	if purgeState == "running" || purgeState == "recovery_required" {
		return V2RetentionJobSummaryDTO{}, fmt.Errorf("retention_job_conflict")
	}
	var runningAutomatic bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'automatic' AND resource_key = $1 AND state IN ('running','cancel_requested')
	)`, dataset).Scan(&runningAutomatic); err != nil {
		return V2RetentionJobSummaryDTO{}, err
	}
	if runningAutomatic {
		return V2RetentionJobSummaryDTO{}, fmt.Errorf("retention_job_conflict")
	}
	// A manual reservation supersedes only unclaimed automatic intent. A
	// running automatic job was rejected above and is never silently cancelled.
	if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
		state = 'cancelled', terminal_disposition = 'cancelled', finished_at = now(),
		cancel_requested = TRUE, updated_at = now()
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'automatic' AND resource_key = $1 AND state = 'queued'`, dataset); err != nil {
		return V2RetentionJobSummaryDTO{}, err
	}
	jobID := "job_" + randomHex(12)
	requestHash := canonicalRequestHash("manual", operationID, preflightID, dataset)
	scopeJSON, err := json.Marshal(LogRetentionScope{Table: dataset, Cutoff: cutoff, DeleteAll: deleteAll})
	if err != nil {
		return V2RetentionJobSummaryDTO{}, err
	}
	progressJSON, err := v2RetentionProtectionEvidence(ctx, tx, dataset, "manual", now)
	if err != nil {
		return V2RetentionJobSummaryDTO{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO management_jobs (
		id, type, state, requested_by, requested_at, priority, profile_id, scope_json, reason,
		contract_version, operation_id, request_hash, resource_key, origin, preflight_id,
		purge_state, stage, visibility_state, boundary_rows_deleted, worker_generation, progress_json, created_at, updated_at
	) VALUES ($1, 'log_retention', 'queued', 'operator', $2, 'maintenance', 0, $3, $4,
		2, $5, $6, $7, 'manual', $8, 'not_started', 'queued', 'unchanged', 0, $9, $10, $2, $2)`,
		jobID, now.UTC(), scopeJSON, "manual log retention purge", operationID, requestHash, dataset, preflightID, v2WorkerGeneration, progressJSON)
	if err != nil {
		return V2RetentionJobSummaryDTO{}, fmt.Errorf("insert v2 manual retention job: %w", err)
	}
	row, err := s.loadV2RetentionRow(ctx, tx, jobID)
	if err != nil {
		return V2RetentionJobSummaryDTO{}, err
	}
	return V2RetentionJobSummary(row), nil
}

func isV2RetentionDataset(dataset string) bool {
	switch dataset {
	case "request_logs", "audit_logs", "usage_request_events", "loadbalance_events":
		return true
	default:
		return false
	}
}

// v2RetentionProtectionEvidence is persisted with the accepted job intent so
// the job center can render the exact discriminated protection union after a
// refresh. It is deliberately captured in the same transaction as the job;
// the summary layer never derives protection from requested_at or purge_to_time.
func v2RetentionProtectionEvidence(ctx context.Context, tx pgx.Tx, dataset, origin string, now time.Time) ([]byte, error) {
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
			"published_floor":       formatTimePtr2(publishedFloor),
			"reader_fence_state":    projection.ReaderFenceState,
			"materializer_state":    projection.MaterializerState,
		},
	})
}

func (s *Store) createV2AutomaticJobTx(ctx context.Context, tx pgx.Tx, dataset string, cutoff time.Time, settingsRevision int64, policyGeneration int64, fenceGeneration int64, purgeState string, now time.Time) (string, error) {
	scopeJSON, err := json.Marshal(LogRetentionScope{Table: dataset, Cutoff: &cutoff})
	if err != nil {
		return "", err
	}
	progressJSON, err := v2RetentionProtectionEvidence(ctx, tx, dataset, "automatic", now)
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
		scheduledLogRetentionReason, operationID, requestHash, dataset, settingsRevision, policyGeneration, fenceGeneration, v2WorkerGeneration, progressJSON, initialStage).Scan(&insertedID)
	if err != nil && err != pgx.ErrNoRows {
		return "", fmt.Errorf("insert v2 automatic retention job for %s: %w", dataset, err)
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

// claimV2RetentionJob claims the next due v2 log-retention job with worker
// evidence. One job at a time; lease/fencing follows the v1 pattern.
func (s *Store) claimV2RetentionJob(ctx context.Context) (v2RetentionJobRow, bool, error) {
	var job v2RetentionJobRow
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
		FROM claimable WHERE j.id = claimable.id RETURNING ` + v2RetentionSelectColumnsQualified()
		row := tx.QueryRow(ctx, query, s.workerID, intervalLiteral(defaultLeaseDuration))
		scanned, err := scanV2RetentionRow(row)
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

// v2RetentionSelectColumns returns the column list without the FROM clause.
func v2RetentionSelectColumns() string {
	withoutFrom := strings.SplitN(v2RetentionSelect, "\nFROM ", 2)[0]
	trimmed := strings.TrimSpace(withoutFrom)
	return strings.TrimPrefix(trimmed, "SELECT ")
}

// v2RetentionSelectColumnsQualified returns the same column list qualified
// with the target alias so RETURNING never collides with a joined CTE.
func v2RetentionSelectColumnsQualified() string {
	raw := strings.Split(v2RetentionSelectColumns(), ",")
	qualified := make([]string, 0, len(raw))
	for _, column := range raw {
		qualified = append(qualified, "j."+strings.TrimSpace(column))
	}
	return strings.Join(qualified, ", ")
}

func (s *Store) claimLegacyRetentionJob(ctx context.Context) (v2RetentionJobRow, bool, error) {
	var job v2RetentionJobRow
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
		FROM claimable WHERE j.id = claimable.id RETURNING ` + v2RetentionSelectColumnsQualified()
		row := tx.QueryRow(ctx, query, s.workerID, intervalLiteral(defaultLeaseDuration))
		scanned, err := scanV2RetentionRow(row)
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

// processV2RetentionJob executes a claimed v2 job:
//   - automatic scheduled: protection gate -> physical reclaim -> publish
//   - manual purge: purge fence -> running -> final publish (revocation epoch)
func (s *Store) processV2RetentionJob(ctx context.Context, job v2RetentionJobRow) error {
	if job.Origin == nil {
		return s.failV2Job(ctx, job, "origin_missing", "v2 retention job has no origin")
	}
	if *job.Origin == "automatic" {
		return s.processV2AutomaticJob(ctx, job)
	}
	return s.processV2ManualPurge(ctx, job)
}

func (s *Store) processV2AutomaticJob(ctx context.Context, job v2RetentionJobRow) error {
	if job.Scope.Table == "" {
		return s.failV2Job(ctx, job, "retention_scope_invalid", "automatic retention job has no table scope")
	}
	if job.Scope.Cutoff == nil {
		return s.failV2Job(ctx, job, "retention_scope_invalid", "automatic retention job has no cutoff")
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
		return s.failV2Job(ctx, job, "retention_resource_missing", "policy resource missing for dataset")
	}
	if job.PolicyGeneration == nil || resourceGeneration != *job.PolicyGeneration ||
		resourceCutoff == nil || !resourceCutoff.Equal(*job.Scope.Cutoff) {
		// Superseded by a newer policy generation: terminal-cancel without
		// data change (never a fake success).
		return s.cancelV2JobWithoutDataChange(ctx, job, "superseded_by_newer_policy")
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
			return s.failV2Job(ctx, job, "retention_resource_generation_changed", "retention fence binding failed")
		}
		job.FenceGeneration = &resourceFenceGeneration
	}
	var manualReserved bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'manual' AND resource_key = $1 AND state IN ('queued','running')
	)`, job.Scope.Table).Scan(&manualReserved); err != nil {
		return s.failV2Job(ctx, job, "retention_resource_unavailable", "manual retention reservation check failed")
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
	return s.publishV2Scheduled(ctx, job)
}

// executePhysicalReclaim drops complete expired partitions and deletes
// boundary rows in bounded batches, checkpointing before every irreversible
// step (SPEC §7.5).
func (s *Store) executePhysicalReclaim(ctx context.Context, job v2RetentionJobRow) error {
	if s.logRetention == nil {
		return s.failV2Job(ctx, job, "retention_store_missing", "log retention store is required")
	}
	cutoff := *job.Scope.Cutoff

	// Audit owns a separate materializer/artifact boundary.  Establish the
	// append-only tombstone and scrub pending staging/outbox artifacts before
	// dropping audit partitions or deleting the boundary rows.  This is the
	// coordinated audit purge fence; it is intentionally not the Observe token
	// grace contract used by the other three datasets.
	if job.Scope.Table == "audit_logs" {
		if _, err := s.prepareAuditPurgeUnderFence(ctx, job, cutoff); err != nil {
			return s.failV2Job(ctx, job, "audit_purge_prepare_failed", "prepare coordinated audit purge failed")
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
		return s.failV2Job(ctx, job, "partition_drop_failed", "drop expired partitions failed")
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
		return s.failV2Job(ctx, job, "boundary_delete_failed", "boundary row delete failed")
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
func (s *Store) prepareAuditPurgeUnderFence(ctx context.Context, job v2RetentionJobRow, cutoff time.Time) (auditPurgePreparation, error) {
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

func auditPurgeReason(job v2RetentionJobRow) string {
	if job.Origin != nil && *job.Origin == "manual" {
		return "manual_retention_purge"
	}
	return "scheduled_retention_purge"
}

func (s *Store) lockRetentionResourceForJob(ctx context.Context, tx pgx.Tx, job v2RetentionJobRow) error {
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

func (s *Store) dropExpiredPartitionsUnderFence(ctx context.Context, job v2RetentionJobRow, cutoff time.Time) ([]logretention.Partition, error) {
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

func (s *Store) deleteBoundaryRowsUnderFence(ctx context.Context, job v2RetentionJobRow, cutoff time.Time) (logretention.Partition, int64, bool, error) {
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

// publishV2Scheduled publishes the logical completion for a scheduled job:
// floor advances to the cutoff and the terminal state is written.
func (s *Store) publishV2Scheduled(ctx context.Context, job v2RetentionJobRow) error {
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
		return s.failV2Job(ctx, job, "floor_publish_failed", "publish retention floor failed")
	}
	_ = s.appendEvent(ctx, job.ID, "succeeded", "log retention job succeeded", 0)
	return nil
}

// acquireManualRetentionFence atomically binds a manual job to the resource's
// semantic fence generation. A fresh purge advances the generation exactly
// when purge_state enters running; a recovery retry reuses the generation that
// was recorded when the resource entered recovery_required.
func (s *Store) acquireManualRetentionFence(ctx context.Context, job v2RetentionJobRow) (int64, error) {
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

// processV2ManualPurge executes a manual purge job under the owning purge
// fence with delete-all purge_to_time freezing and final epoch/coverage
// publication (SPEC §6.5).
func (s *Store) processV2ManualPurge(ctx context.Context, job v2RetentionJobRow) error {
	if job.Scope.Table == "" {
		return s.failV2Job(ctx, job, "retention_scope_invalid", "manual purge job has no table scope")
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
		return s.failV2Job(ctx, job, "purge_fence_unavailable", "retention resource fence failed")
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
			return s.failV2Job(ctx, job, "purge_fence_unavailable", "freeze purge_to_time failed")
		}
		purgeToTime = &frozen
		if newlyFrozen {
			_ = s.appendEvent(ctx, job.ID, "purge_to_time_frozen", "delete-all purge_to_time frozen at execution fence", 0)
		}
	} else {
		purgeToTime = job.Scope.Cutoff
	}
	if purgeToTime == nil || purgeToTime.After(s.now().UTC()) {
		return s.failV2Job(ctx, job, "retention_cutoff_invalid", "manual purge cutoff is missing or in the future")
	}

	// Enter coordinated purge state: affected reads fail closed while running.
	jobTag, err := s.pool.Exec(ctx, `UPDATE management_jobs SET purge_state = 'running',
		stage = 'purge_running', visibility_state = 'purge_unavailable',
		last_heartbeat_at = now(), updated_at = now() WHERE id = $1`, job.ID)
	if err != nil {
		return s.failV2Job(ctx, job, "purge_fence_unavailable", "enter purge running failed")
	}
	if jobTag.RowsAffected() != 1 {
		return s.failV2Job(ctx, job, "purge_fence_unavailable", "manual purge job is no longer active")
	}
	_ = s.appendEvent(ctx, job.ID, "purge_started", "manual coordinated purge started", 0)

	if job.Scope.DeleteAll {
		if s.logRetention == nil {
			return s.failV2Job(ctx, job, "retention_store_missing", "log retention store is required")
		}
		// Reuse the fenced physical path with the execution-time cutoff frozen
		// above. The scope remains delete_all for truthful job reporting.
		job.Scope.Cutoff = purgeToTime
		if err := s.executePhysicalReclaim(ctx, job); err != nil {
			return err
		}
	} else {
		if err := s.executePhysicalReclaim(ctx, job); err != nil {
			return err
		}
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
		return s.failV2Job(ctx, job, "revocation_publish_failed", "publish revocation epoch failed")
	}
	_ = s.appendEvent(ctx, job.ID, "purge_published", "manual purge published revocation epoch and coverage", 0)
	return nil
}

// drainLegacyRetentionJob is the frozen generation-tagged v1 executor: it
// resumes a previously accepted legacy job from its real checkpoint and
// reaches a truthful terminal result (SPEC §7.2). Never superseded, never
// restarted from an empty counter.
func (s *Store) drainLegacyRetentionJob(ctx context.Context, job v2RetentionJobRow) error {
	if s.logRetention == nil {
		return s.failV2Job(ctx, job, "retention_store_missing", "log retention store is required")
	}
	if job.ClassificationEvidenceHash == nil {
		return s.failV2Job(ctx, job, "legacy_evidence_missing", "legacy job has no classification evidence")
	}
	scope := job.Scope
	summary, err := s.logRetention.RunRetention(ctx, scope.Table, scope.Cutoff, scope.DeleteAll)
	if err != nil {
		return s.failV2Job(ctx, job, "retention_error", "legacy retention drain failed")
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
			originalState := "queued"
			if err := tx.QueryRow(ctx, `SELECT legacy_original_state FROM management_jobs WHERE id = $1`, id).Scan(&originalState); err != nil {
				_ = originalState
			}
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

// ListGlobalRetentionJobs lists global log-retention jobs with keyset
// pagination and origin/state filters (SPEC §7.1).
func (s *Store) ListGlobalRetentionJobs(ctx context.Context, origin string, state []string, limit int, cursor *string) (globalJobListResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	origin = strings.TrimSpace(origin)
	state = canonicalJobStates(state)
	query := `SELECT ` + v2RetentionSelectColumns() + `
		FROM management_jobs
		WHERE type = 'log_retention' AND profile_id = 0`
	args := []any{}
	if origin != "" {
		args = append(args, origin)
		query += fmt.Sprintf(` AND origin = $%d`, len(args))
	}
	if len(state) > 0 {
		query += ` AND state = ANY($` + fmt.Sprintf("%d", len(args)+1) + `::text[])`
		args = append(args, state)
	}
	if cursor != nil && strings.TrimSpace(*cursor) != "" {
		value, ok := s.decodeJobsCursor(*cursor)
		if !ok {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		if value.Origin != origin || !sameJobStates(value.States, state) || value.Limit != limit || value.Sort != jobsCursorSort {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		upperAt, err := time.Parse(time.RFC3339Nano, value.UpperAt)
		if err != nil {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		upperArgs := []any{upperAt, value.UpperID}
		query += fmt.Sprintf(` AND (requested_at, id) <= ($%d, $%d)`, len(args)+1, len(args)+2)
		args = append(args, upperArgs...)
		positionAt, err := time.Parse(time.RFC3339Nano, value.PositionAt)
		if err != nil {
			return globalJobListResult{}, errInvalidJobsCursor
		}
		args = append(args, positionAt, value.PositionID)
		query += fmt.Sprintf(` AND (requested_at, id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	query += ` ORDER BY requested_at DESC, id DESC LIMIT $` + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return globalJobListResult{}, fmt.Errorf("list global retention jobs: %w", err)
	}
	defer rows.Close()
	items := []v2RetentionJobRow{}
	for rows.Next() {
		item, err := scanV2RetentionRow(rows)
		if err != nil {
			return globalJobListResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return globalJobListResult{}, err
	}
	result := globalJobListResult{GeneratedAt: s.now().UTC().Format(time.RFC3339)}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		upper := items[0]
		payload := jobsCursorPayload{
			Version:    2,
			Origin:     origin,
			States:     append([]string(nil), state...),
			Limit:      limit,
			Sort:       jobsCursorSort,
			UpperAt:    upper.RequestedAt.UTC().Format(time.RFC3339Nano),
			UpperID:    upper.ID,
			PositionAt: last.RequestedAt.UTC().Format(time.RFC3339Nano),
			PositionID: last.ID,
		}
		encoded := s.encodeJobsCursor(payload)
		result.NextCursor = &encoded
		result.HasMore = true
	}
	result.Items = items
	return result, nil
}

type globalJobListResult struct {
	Items       []v2RetentionJobRow
	HasMore     bool
	NextCursor  *string
	GeneratedAt string
}

func (s *Store) GetGlobalRetentionJob(ctx context.Context, id string) (v2RetentionJobRow, error) {
	return s.loadGlobalV2RetentionRow(ctx, s.pool, id)
}

func (s *Store) CancelV2RetentionJob(ctx context.Context, id string, operationID string) (v2RetentionJobRow, bool, error) {
	var row v2RetentionJobRow
	replayed := false
	err := pgxutil.InTx(ctx, s.pool, "retention_cancel_v2", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(ctx, tx); err != nil {
			return fmt.Errorf("retention cancel owner admission: %w", err)
		}
		requestHash := canonicalRequestHash("retention_job_cancel", id, operationID)
		var recordedHash string
		var recordedResult []byte
		operationErr := tx.QueryRow(ctx, `SELECT request_hash, result_json FROM settings_mutation_operations
			WHERE resource_kind = 'retention_job_cancel' AND operation_id = $1`, operationID).
			Scan(&recordedHash, &recordedResult)
		if operationErr == nil {
			if recordedHash != requestHash || len(recordedResult) == 0 {
				return errRetentionCancelOperationConflict
			}
			replayed = true
			var loadErr error
			row, loadErr = s.loadGlobalV2RetentionRow(ctx, tx, id)
			return loadErr
		}
		if operationErr != pgx.ErrNoRows {
			return operationErr
		}
		existing, err := scanV2RetentionRow(tx.QueryRow(ctx, v2RetentionSelect+` WHERE id = $1 AND type = 'log_retention' AND profile_id = 0 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if existing.ContractVersion != 2 {
			return errLegacyJobNotCancellable
		}
		if existing.State == "succeeded" || existing.State == "failed" || existing.State == "superseded" {
			return errJobTerminal
		}
		// Manual purge jobs are cancellable only while queued and before the
		// purge fence; once running they must complete or recover (SPEC §7.3).
		isManual := existing.Origin != nil && *existing.Origin == "manual"
		if isManual && existing.State == "running" {
			return errPurgeNotCancellable
		}
		if existing.State == "cancel_requested" || existing.State == "cancelled" {
			row = existing
			return recordRetentionCancelOperation(ctx, tx, operationID, requestHash, V2RetentionJobSummary(existing))
		}
		// Queued manual/automatic cancels commit directly; running automatic
		// enters cancel_requested at the next safe checkpoint.
		newState := "cancelled"
		if existing.State == "running" {
			newState = "cancel_requested"
		}
		if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
			state = $2, terminal_disposition = CASE WHEN $2 = 'cancelled' THEN 'cancelled' ELSE NULL END, cancel_requested = TRUE,
			finished_at = CASE WHEN $2 = 'cancelled' THEN now() ELSE finished_at END,
			updated_at = now() WHERE id = $1 AND type = 'log_retention' AND profile_id = 0`, id, newState); err != nil {
			return err
		}
		row, err = s.loadGlobalV2RetentionRow(ctx, tx, id)
		if err != nil {
			return err
		}
		return recordRetentionCancelOperation(ctx, tx, operationID, requestHash, V2RetentionJobSummary(row))
	})
	if err != nil {
		return v2RetentionJobRow{}, false, err
	}
	return row, replayed, nil
}

func recordRetentionCancelOperation(ctx context.Context, tx pgx.Tx, operationID, requestHash string, summary V2RetentionJobSummaryDTO) error {
	resultJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
		resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
	) VALUES ('retention_job_cancel', $1, $2, 'completed', $3, now(), now())
	ON CONFLICT (resource_kind, operation_id) DO NOTHING`, operationID, requestHash, resultJSON)
	return err
}

func (s *Store) failV2Job(ctx context.Context, job v2RetentionJobRow, code string, message string) error {
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

func (s *Store) cancelV2JobWithoutDataChange(ctx context.Context, job v2RetentionJobRow, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
		state = 'cancelled', terminal_disposition = 'cancelled', stage = 'finished',
		finished_at = now(), locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1`, job.ID)
	if err != nil {
		return fmt.Errorf("cancel superseded v2 job: %w", err)
	}
	_ = s.appendEvent(ctx, job.ID, "cancelled", reason, 0)
	return nil
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

var (
	errLegacyJobNotCancellable          = fmt.Errorf("legacy_job_not_cancellable")
	errJobTerminal                      = fmt.Errorf("job_terminal")
	errPurgeNotCancellable              = fmt.Errorf("purge_not_cancellable")
	errInvalidJobsCursor                = fmt.Errorf("invalid_jobs_cursor")
	errRetentionCancelOperationConflict = fmt.Errorf("retention_cancel_operation_conflict")
)

const jobsCursorSort = "requested_at_desc_id_desc"

type jobsCursorPayload struct {
	Version    int      `json:"v"`
	Origin     string   `json:"origin,omitempty"`
	States     []string `json:"states,omitempty"`
	Limit      int      `json:"limit"`
	Sort       string   `json:"sort"`
	UpperAt    string   `json:"upper_at"`
	UpperID    string   `json:"upper_id"`
	PositionAt string   `json:"position_at"`
	PositionID string   `json:"position_id"`
	Signature  string   `json:"sig"`
}

func canonicalJobStates(states []string) []string {
	seen := make(map[string]struct{}, len(states))
	result := make([]string, 0, len(states))
	for _, state := range states {
		state = strings.TrimSpace(state)
		if state == "" {
			continue
		}
		if _, exists := seen[state]; exists {
			continue
		}
		seen[state] = struct{}{}
		result = append(result, state)
	}
	sort.Strings(result)
	return result
}

func sameJobStates(left, right []string) bool {
	left = canonicalJobStates(left)
	right = canonicalJobStates(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (s *Store) encodeJobsCursor(payload jobsCursorPayload) string {
	payload.Signature = ""
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(raw)
	payload.Signature = hex.EncodeToString(mac.Sum(nil))
	encoded, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func (s *Store) decodeJobsCursor(encoded string) (jobsCursorPayload, bool) {
	var payload jobsCursorPayload
	if len(encoded) > 4096 {
		return payload, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 {
		return payload, false
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Version != 2 || payload.Limit < 1 || payload.Limit > 100 || payload.Sort != jobsCursorSort || strings.TrimSpace(payload.UpperID) == "" || strings.TrimSpace(payload.PositionID) == "" || strings.TrimSpace(payload.Signature) == "" {
		return jobsCursorPayload{}, false
	}
	provided, err := hex.DecodeString(payload.Signature)
	if err != nil || len(provided) != sha256.Size {
		return jobsCursorPayload{}, false
	}
	signature := payload.Signature
	payload.Signature = ""
	unsigned, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(unsigned)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return jobsCursorPayload{}, false
	}
	payload.Signature = signature
	if _, err := time.Parse(time.RFC3339Nano, payload.UpperAt); err != nil {
		return jobsCursorPayload{}, false
	}
	if _, err := time.Parse(time.RFC3339Nano, payload.PositionAt); err != nil {
		return jobsCursorPayload{}, false
	}
	return payload, true
}

type evidenceCursorPayload struct {
	Version    int    `json:"v"`
	Kind       string `json:"kind"`
	JobID      string `json:"job_id"`
	Limit      int    `json:"limit"`
	Sort       string `json:"sort"`
	UpperID    int64  `json:"upper_id"`
	PositionID int64  `json:"position_id"`
	Signature  string `json:"sig"`
}

func (s *Store) encodeEvidenceCursor(payload evidenceCursorPayload) string {
	payload.Signature = ""
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(raw)
	payload.Signature = hex.EncodeToString(mac.Sum(nil))
	encoded, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func (s *Store) decodeEvidenceCursor(encoded, kind, jobID string, limit int) (evidenceCursorPayload, bool) {
	var payload evidenceCursorPayload
	if len(encoded) > 4096 {
		return payload, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(raw, &payload) != nil || payload.Version != 2 || payload.Kind != kind || payload.JobID != jobID || payload.Limit != limit || payload.Sort != evidenceCursorSort || payload.UpperID < 0 || payload.PositionID < 0 || payload.PositionID > payload.UpperID || strings.TrimSpace(payload.Signature) == "" {
		return evidenceCursorPayload{}, false
	}
	provided, err := hex.DecodeString(payload.Signature)
	if err != nil || len(provided) != sha256.Size {
		return evidenceCursorPayload{}, false
	}
	signature := payload.Signature
	payload.Signature = ""
	unsigned, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.cursorKey)
	_, _ = mac.Write(unsigned)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return evidenceCursorPayload{}, false
	}
	payload.Signature = signature
	return payload, true
}

// ---- settings-facing DTO queries (audit service consumes these) ----

// V2JobListDTO mirrors GlobalRetentionJobList (SPEC §7.2).
type V2JobListDTO struct {
	Items       []V2RetentionJobSummaryDTO `json:"items"`
	HasMore     bool                       `json:"has_more"`
	NextCursor  *string                    `json:"next_cursor"`
	GeneratedAt string                     `json:"generated_at"`
}

// V2JobTerminalResultDTO mirrors RetentionJobTerminalResult (SPEC §7.2).
type V2JobTerminalResultDTO struct {
	Kind                 string          `json:"kind"`
	FinishedAt           string          `json:"finished_at"`
	VisibilityState      *string         `json:"visibility_state,omitempty"`
	PublishedEpoch       *string         `json:"published_epoch,omitempty"`
	PublishedFloor       *string         `json:"published_floor,omitempty"`
	AccountingProvenance string          `json:"accounting_provenance"`
	CancellationScope    *string         `json:"cancellation_scope,omitempty"`
	CoherentOutcome      *string         `json:"coherent_outcome,omitempty"`
	SafeError            *V2JobSafeError `json:"safe_error,omitempty"`
	Disposition          *string         `json:"disposition,omitempty"`
	LegacyOriginalState  *string         `json:"legacy_original_state,omitempty"`
	ReplacementJobID     *string         `json:"replacement_job_id,omitempty"`
}

// V2JobCheckpointPageDTO mirrors RetentionJobCheckpointPage (SPEC §7.2).
type V2JobCheckpointPageDTO struct {
	Items       []V2JobCheckpointDTO `json:"items"`
	HasMore     bool                 `json:"has_more"`
	NextCursor  *string              `json:"next_cursor"`
	GeneratedAt string               `json:"generated_at"`
}

// V2JobCheckpointDTO mirrors RetentionJobCheckpoint (SPEC §7.2).
type V2JobCheckpointDTO struct {
	Sequence              string  `json:"sequence"`
	RecordedAt            string  `json:"recorded_at"`
	Stage                 string  `json:"stage"`
	Kind                  string  `json:"kind"`
	BoundaryRowsDelta     string  `json:"boundary_rows_delta"`
	DroppedPartitionDelta string  `json:"dropped_partition_delta"`
	SafeDetailCode        *string `json:"safe_detail_code"`
}

// V2JobPartitionPageDTO mirrors RetentionJobPartitionPage (SPEC §7.2).
type V2JobPartitionPageDTO struct {
	Items       []V2JobPartitionEvidenceDTO `json:"items"`
	HasMore     bool                        `json:"has_more"`
	NextCursor  *string                     `json:"next_cursor"`
	GeneratedAt string                      `json:"generated_at"`
}

// V2JobPartitionEvidenceDTO mirrors RetentionJobPartitionEvidence (SPEC §7.2).
type V2JobPartitionEvidenceDTO struct {
	Sequence            string  `json:"sequence"`
	PartitionName       string  `json:"partition_name"`
	Action              string  `json:"action"`
	EvidenceAt          string  `json:"evidence_at"`
	BoundaryRowsDeleted string  `json:"boundary_rows_deleted"`
	DroppedRowsEstimate *string `json:"dropped_rows_estimate"`
	DroppedRowsAccuracy string  `json:"dropped_rows_accuracy"`
}

const (
	defaultJobEvidencePageLimit = 20
	maxJobEvidencePageLimit     = 100
	evidenceCursorSort          = "sequence_asc"
)

// V2JobDetailDTO mirrors GlobalRetentionJobDetail (SPEC §7.2).
type V2JobDetailDTO struct {
	Job            V2RetentionJobSummaryDTO `json:"job"`
	TerminalResult *V2JobTerminalResultDTO  `json:"terminal_result"`
	Checkpoints    V2JobCheckpointPageDTO   `json:"checkpoints"`
	Partitions     V2JobPartitionPageDTO    `json:"partitions"`
}

// ListGlobalRetentionJobsDTO lists global log-retention jobs with keyset
// pagination and origin/state filters (SPEC §7.1).
func (s *Store) ListGlobalRetentionJobsDTO(ctx context.Context, origin string, states []string, limit int, cursor *string) (V2JobListDTO, error) {
	result, err := s.ListGlobalRetentionJobs(ctx, origin, states, limit, cursor)
	if err != nil {
		return V2JobListDTO{}, err
	}
	dto := V2JobListDTO{HasMore: result.HasMore, NextCursor: result.NextCursor, GeneratedAt: result.GeneratedAt}
	dto.Items = make([]V2RetentionJobSummaryDTO, 0, len(result.Items))
	for _, item := range result.Items {
		dto.Items = append(dto.Items, V2RetentionJobSummary(item))
	}
	return dto, nil
}

// GetGlobalRetentionJobDetailDTO returns the exact detail with embedded
// bounded checkpoint/partition pages (SPEC §7.2).
func (s *Store) GetGlobalRetentionJobDetailDTO(ctx context.Context, id string) (V2JobDetailDTO, error) {
	row, err := s.loadGlobalV2RetentionRow(ctx, s.pool, id)
	if err != nil {
		return V2JobDetailDTO{}, err
	}
	checkpoints, err := s.checkpointPage(ctx, id, defaultJobEvidencePageLimit, "")
	if err != nil {
		return V2JobDetailDTO{}, err
	}
	partitions, err := s.partitionPage(ctx, row, defaultJobEvidencePageLimit, "")
	if err != nil {
		return V2JobDetailDTO{}, err
	}
	detail := V2JobDetailDTO{
		Job:         V2RetentionJobSummary(row),
		Checkpoints: checkpoints,
		Partitions:  partitions,
	}
	detail.TerminalResult = terminalResultFor(row)
	return detail, nil
}

// GetGlobalRetentionJobCheckpointsDTO returns the bounded checkpoint page.
func (s *Store) GetGlobalRetentionJobCheckpointsDTO(ctx context.Context, id string, limit int, cursor string) (V2JobCheckpointPageDTO, error) {
	row, err := s.loadGlobalV2RetentionRow(ctx, s.pool, id)
	if err != nil {
		return V2JobCheckpointPageDTO{}, err
	}
	_ = row
	return s.checkpointPage(ctx, id, limit, cursor)
}

// GetGlobalRetentionJobPartitionsDTO returns the bounded partition evidence page.
func (s *Store) GetGlobalRetentionJobPartitionsDTO(ctx context.Context, id string, limit int, cursor string) (V2JobPartitionPageDTO, error) {
	row, err := s.loadGlobalV2RetentionRow(ctx, s.pool, id)
	if err != nil {
		return V2JobPartitionPageDTO{}, err
	}
	return s.partitionPage(ctx, row, limit, cursor)
}

// CancelGlobalRetentionJobDTO cancels a v2 global log-retention job with
// operation identity and durable replay.
func (s *Store) CancelGlobalRetentionJobDTO(ctx context.Context, id string, operationID string) (V2RetentionJobSummaryDTO, bool, error) {
	row, replayed, err := s.CancelV2RetentionJob(ctx, id, operationID)
	if err != nil {
		return V2RetentionJobSummaryDTO{}, false, err
	}
	return V2RetentionJobSummary(row), replayed, nil
}

// EncodeJobsCursor / DecodeJobsCursor expose the opaque keyset cursor.
func EncodeJobsCursor(requestedAt time.Time, id string) string {
	key := deriveCursorSigningKey("", "public-helper")
	store := &Store{cursorKey: key}
	stamp := requestedAt.UTC().Format(time.RFC3339Nano)
	return store.encodeJobsCursor(jobsCursorPayload{
		Version:    2,
		Limit:      20,
		Sort:       jobsCursorSort,
		UpperAt:    stamp,
		UpperID:    id,
		PositionAt: stamp,
		PositionID: id,
	})
}
func DecodeJobsCursor(encoded string) (time.Time, string, bool) {
	key := deriveCursorSigningKey("", "public-helper")
	store := &Store{cursorKey: key}
	decoded, ok := store.decodeJobsCursor(encoded)
	if !ok {
		return time.Time{}, "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, decoded.PositionAt)
	if err != nil {
		return time.Time{}, "", false
	}
	return parsed, decoded.PositionID, true
}

// IsLegacyJobNotCancellable / IsJobTerminal / IsPurgeNotCancellable classify
// cancel errors for the typed problem writer.
func IsLegacyJobNotCancellable(err error) bool { return err == errLegacyJobNotCancellable }
func IsJobTerminal(err error) bool             { return err == errJobTerminal }
func IsPurgeNotCancellable(err error) bool     { return err == errPurgeNotCancellable }
func IsInvalidJobsCursor(err error) bool       { return err == errInvalidJobsCursor }
func IsRetentionCancelOperationConflict(err error) bool {
	return err == errRetentionCancelOperationConflict
}

func (s *Store) checkpointPage(ctx context.Context, jobID string, limit int, cursor string) (V2JobCheckpointPageDTO, error) {
	if limit <= 0 || limit > maxJobEvidencePageLimit {
		limit = defaultJobEvidencePageLimit
	}
	page := V2JobCheckpointPageDTO{GeneratedAt: s.now().UTC().Format(time.RFC3339)}
	positionID := int64(0)
	upperID := int64(0)
	if strings.TrimSpace(cursor) != "" {
		decoded, ok := s.decodeEvidenceCursor(cursor, "checkpoints", jobID, limit)
		if !ok {
			return page, errInvalidJobsCursor
		}
		positionID = decoded.PositionID
		upperID = decoded.UpperID
	}
	args := []any{jobID}
	where := "job_id = $1"
	if upperID > 0 {
		args = append(args, upperID)
		where += fmt.Sprintf(" AND id <= $%d", len(args))
	}
	if positionID > 0 {
		args = append(args, positionID)
		where += fmt.Sprintf(" AND id > $%d", len(args))
	}
	args = append(args, limit+1)
	rows, err := s.pool.Query(ctx, `SELECT id, event_type, rows_deleted, created_at,
		MAX(id) OVER () AS upper_id
		FROM management_job_events WHERE `+where+` ORDER BY id ASC LIMIT $`+fmt.Sprintf("%d", len(args)), args...)
	if err != nil {
		return page, fmt.Errorf("list job checkpoints: %w", err)
	}
	defer rows.Close()
	type checkpointEvidence struct {
		id          int64
		eventType   string
		rowsDeleted int64
		createdAt   time.Time
	}
	items := make([]checkpointEvidence, 0, limit+1)
	for rows.Next() {
		var eventID int64
		var eventType string
		var rowsDeleted int64
		var createdAt time.Time
		var queryUpperID int64
		if err := rows.Scan(&eventID, &eventType, &rowsDeleted, &createdAt, &queryUpperID); err != nil {
			return page, fmt.Errorf("scan job checkpoint: %w", err)
		}
		if upperID == 0 {
			upperID = queryUpperID
		}
		items = append(items, checkpointEvidence{id: eventID, eventType: eventType, rowsDeleted: rowsDeleted, createdAt: createdAt})
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("iterate job checkpoints: %w", err)
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
	}
	for _, item := range items {
		page.Items = append(page.Items, V2JobCheckpointDTO{
			Sequence:              fmt.Sprintf("%d", item.id),
			RecordedAt:            item.createdAt.UTC().Format(time.RFC3339),
			Stage:                 mapEventStage(item.eventType),
			Kind:                  mapEventKind(item.eventType),
			BoundaryRowsDelta:     fmt.Sprintf("%d", item.rowsDeleted),
			DroppedPartitionDelta: "0",
		})
	}
	if page.HasMore && len(items) > 0 {
		encoded := s.encodeEvidenceCursor(evidenceCursorPayload{Version: 2, Kind: "checkpoints", JobID: jobID, Limit: limit, Sort: evidenceCursorSort, UpperID: upperID, PositionID: items[len(items)-1].id})
		page.NextCursor = &encoded
	}
	return page, nil
}

func mapEventKind(eventType string) string {
	switch eventType {
	case "created":
		return "claimed"
	case "partitions_dropped":
		return "partition_dropped"
	case "boundary_rows_deleted":
		return "boundary_batch_committed"
	case "purge_started", "purge_to_time_frozen":
		return "purge_state_changed"
	case "purge_published", "superseded", "succeeded", "failed", "cancelled", "finished":
		return "coverage_published"
	default:
		return "coverage_published"
	}
}

func mapEventStage(eventType string) string {
	switch eventType {
	case "partitions_dropped":
		return "dropping_partitions"
	case "boundary_rows_deleted":
		return "deleting_boundary_rows"
	case "purge_started":
		return "purge_running"
	case "purge_published":
		return "publishing_epoch_coverage"
	case "succeeded":
		return "finished"
	default:
		return "queued"
	}
}

func (s *Store) partitionPage(ctx context.Context, row v2RetentionJobRow, limit int, cursor string) (V2JobPartitionPageDTO, error) {
	if limit <= 0 || limit > maxJobEvidencePageLimit {
		limit = defaultJobEvidencePageLimit
	}
	page := V2JobPartitionPageDTO{GeneratedAt: s.now().UTC().Format(time.RFC3339)}
	var progress struct {
		DroppedPartitions []string `json:"dropped_partitions"`
	}
	_ = json.Unmarshal(row.ProgressJSON, &progress)
	evidenceAt := "unknown"
	if row.LastHeartbeatAt != nil {
		evidenceAt = row.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}
	positionID := int64(0)
	upperID := int64(len(progress.DroppedPartitions))
	if strings.TrimSpace(cursor) != "" {
		decoded, ok := s.decodeEvidenceCursor(cursor, "partitions", row.ID, limit)
		if !ok {
			return page, errInvalidJobsCursor
		}
		if decoded.UpperID > 0 && decoded.UpperID < upperID {
			upperID = decoded.UpperID
		}
		positionID = decoded.PositionID
	}
	end := int64(len(progress.DroppedPartitions))
	if upperID > 0 && end > upperID {
		end = upperID
	}
	if positionID < 0 {
		positionID = 0
	}
	for index := positionID; index < end && int64(len(page.Items)) < int64(limit); index++ {
		name := progress.DroppedPartitions[index]
		page.Items = append(page.Items, V2JobPartitionEvidenceDTO{
			Sequence:            fmt.Sprintf("%d", index+1),
			PartitionName:       name,
			Action:              "dropped",
			EvidenceAt:          evidenceAt,
			BoundaryRowsDeleted: "0",
			DroppedRowsAccuracy: "unavailable",
		})
	}
	if end > positionID+int64(len(page.Items)) {
		page.HasMore = true
	}
	if page.HasMore && len(page.Items) > 0 {
		last := positionID + int64(len(page.Items))
		encoded := s.encodeEvidenceCursor(evidenceCursorPayload{Version: 2, Kind: "partitions", JobID: row.ID, Limit: limit, Sort: evidenceCursorSort, UpperID: upperID, PositionID: last})
		page.NextCursor = &encoded
	}
	return page, nil
}

const (
	cancellationScopeQueuedNoDataChanged         = "queued_no_data_changed"
	cancellationScopeAutomaticRemainingStepsOnly = "automatic_remaining_steps_only"
	cancellationScopeLegacyUnknown               = "legacy_unknown"
)

func cancellationScopeFor(row v2RetentionJobRow) string {
	if row.ContractVersion == 1 {
		return cancellationScopeLegacyUnknown
	}
	// cancel_requested also records queued cancellations. started_at is durable
	// execution evidence, but it does not prove an automatic job: a manual job
	// can be requeued after a pre-fence failure and then cancelled. Require the
	// automatic origin as well so requeued manual work is never reported as a
	// partial automatic cancellation.
	if row.ContractVersion == 2 && row.CancelRequested && row.StartedAt != nil &&
		row.Origin != nil && *row.Origin == "automatic" {
		return cancellationScopeAutomaticRemainingStepsOnly
	}
	return cancellationScopeQueuedNoDataChanged
}

func terminalResultFor(row v2RetentionJobRow) *V2JobTerminalResultDTO {
	if row.FinishedAt == nil {
		return nil
	}
	provenance := "v2_exact"
	if row.ContractVersion == 1 {
		provenance = "legacy_boundary_only"
	}
	finishedAt := row.FinishedAt.UTC().Format(time.RFC3339)
	var published struct {
		PublishedEpoch string `json:"published_epoch"`
		PublishedFloor string `json:"published_floor"`
	}
	_ = json.Unmarshal(row.ProgressJSON, &published)
	var publishedEpoch, publishedFloor *string
	if published.PublishedEpoch != "" {
		publishedEpoch = &published.PublishedEpoch
	}
	if published.PublishedFloor != "" {
		publishedFloor = &published.PublishedFloor
	}
	switch row.State {
	case "succeeded":
		return &V2JobTerminalResultDTO{
			Kind: "succeeded", FinishedAt: finishedAt, AccountingProvenance: provenance,
			VisibilityState: row.VisibilityState, PublishedEpoch: publishedEpoch, PublishedFloor: publishedFloor,
		}
	case "cancelled":
		scope := cancellationScopeFor(row)
		return &V2JobTerminalResultDTO{
			Kind: "cancelled", FinishedAt: finishedAt, AccountingProvenance: provenance,
			CancellationScope: &scope,
		}
	case "failed":
		outcome := "stable_final_state"
		if row.ContractVersion == 1 {
			outcome = "legacy_unknown"
		}
		result := &V2JobTerminalResultDTO{
			Kind: "failed", FinishedAt: finishedAt, AccountingProvenance: provenance,
			CoherentOutcome: &outcome,
		}
		if row.ErrorCode != nil {
			message := ""
			if row.ErrorMessage != nil {
				message = *row.ErrorMessage
			}
			result.SafeError = &V2JobSafeError{Code: *row.ErrorCode, Message: message}
		}
		return result
	case "superseded":
		disposition := "superseded_by_v2_planning"
		return &V2JobTerminalResultDTO{
			Kind: "superseded", FinishedAt: finishedAt, AccountingProvenance: provenance,
			Disposition: &disposition, LegacyOriginalState: row.LegacyOriginalState,
		}
	default:
		return nil
	}
}

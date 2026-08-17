package managementjobs

import (
	"encoding/json"
	"fmt"
	"time"
)

// Settings job-center DTOs (SPEC §7.2). Every numeric quantity is rendered as
// a string with an explicit accuracy discriminator so the UI can tell an exact
// count from an estimate from an unavailable fact.

// RetentionJobSummaryDTO mirrors GlobalRetentionJobSummary.
type RetentionJobSummaryDTO struct {
	ID                        string         `json:"id"`
	ContractVersion           int            `json:"contract_version"`
	Type                      string         `json:"type"`
	JobScope                  string         `json:"job_scope"`
	Origin                    string         `json:"origin"`
	LegacyOriginProvenance    *string        `json:"legacy_origin_provenance"`
	LegacyExecutionProvenance *string        `json:"legacy_execution_provenance"`
	Dataset                   string         `json:"dataset"`
	State                     string         `json:"state"`
	TerminalDisposition       *string        `json:"terminal_disposition"`
	LegacyOriginalState       *string        `json:"legacy_original_state"`
	Mode                      string         `json:"mode"`
	Cutoff                    *string        `json:"cutoff"`
	PurgeToTime               *string        `json:"purge_to_time"`
	PolicyRevision            *string        `json:"policy_revision"`
	PreflightID               *string        `json:"preflight_id"`
	OperationID               *string        `json:"operation_id"`
	RequestedAt               string         `json:"requested_at"`
	StartedAt                 *string        `json:"started_at"`
	FinishedAt                *string        `json:"finished_at"`
	LastHeartbeatAt           *string        `json:"last_heartbeat_at"`
	AttemptCount              int            `json:"attempt_count"`
	CancelAllowed             bool           `json:"cancel_allowed"`
	Progress                  JobProgressDTO `json:"progress"`
	Error                     *JobSafeError  `json:"error"`
}

type JobProgressDTO struct {
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

type JobSafeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JobListDTO mirrors GlobalRetentionJobList.
type JobListDTO struct {
	Items       []RetentionJobSummaryDTO `json:"items"`
	HasMore     bool                     `json:"has_more"`
	NextCursor  *string                  `json:"next_cursor"`
	GeneratedAt string                   `json:"generated_at"`
}

// JobDetailDTO mirrors GlobalRetentionJobDetail.
type JobDetailDTO struct {
	Job            RetentionJobSummaryDTO `json:"job"`
	TerminalResult *JobTerminalResultDTO  `json:"terminal_result"`
	Checkpoints    JobCheckpointPageDTO   `json:"checkpoints"`
	Partitions     JobPartitionPageDTO    `json:"partitions"`
}

// JobTerminalResultDTO mirrors RetentionJobTerminalResult.
type JobTerminalResultDTO struct {
	Kind                 string        `json:"kind"`
	FinishedAt           string        `json:"finished_at"`
	VisibilityState      *string       `json:"visibility_state,omitempty"`
	PublishedEpoch       *string       `json:"published_epoch,omitempty"`
	PublishedFloor       *string       `json:"published_floor,omitempty"`
	AccountingProvenance string        `json:"accounting_provenance"`
	CancellationScope    *string       `json:"cancellation_scope,omitempty"`
	CoherentOutcome      *string       `json:"coherent_outcome,omitempty"`
	SafeError            *JobSafeError `json:"safe_error,omitempty"`
	Disposition          *string       `json:"disposition,omitempty"`
	LegacyOriginalState  *string       `json:"legacy_original_state,omitempty"`
	ReplacementJobID     *string       `json:"replacement_job_id,omitempty"`
}

// JobCheckpointPageDTO mirrors RetentionJobCheckpointPage.
type JobCheckpointPageDTO struct {
	Items       []JobCheckpointDTO `json:"items"`
	HasMore     bool               `json:"has_more"`
	NextCursor  *string            `json:"next_cursor"`
	GeneratedAt string             `json:"generated_at"`
}

// JobCheckpointDTO mirrors RetentionJobCheckpoint.
type JobCheckpointDTO struct {
	Sequence              string  `json:"sequence"`
	RecordedAt            string  `json:"recorded_at"`
	Stage                 string  `json:"stage"`
	Kind                  string  `json:"kind"`
	BoundaryRowsDelta     string  `json:"boundary_rows_delta"`
	DroppedPartitionDelta string  `json:"dropped_partition_delta"`
	SafeDetailCode        *string `json:"safe_detail_code"`
}

// JobPartitionPageDTO mirrors RetentionJobPartitionPage.
type JobPartitionPageDTO struct {
	Items       []JobPartitionEvidenceDTO `json:"items"`
	HasMore     bool                      `json:"has_more"`
	NextCursor  *string                   `json:"next_cursor"`
	GeneratedAt string                    `json:"generated_at"`
}

// JobPartitionEvidenceDTO mirrors RetentionJobPartitionEvidence.
type JobPartitionEvidenceDTO struct {
	Sequence            string  `json:"sequence"`
	PartitionName       string  `json:"partition_name"`
	Action              string  `json:"action"`
	EvidenceAt          string  `json:"evidence_at"`
	BoundaryRowsDeleted string  `json:"boundary_rows_deleted"`
	DroppedRowsEstimate *string `json:"dropped_rows_estimate"`
	DroppedRowsAccuracy string  `json:"dropped_rows_accuracy"`
}

// RetentionJobSummary projects a durable row onto the settings-facing DTO.
func RetentionJobSummary(row retentionJobRow) RetentionJobSummaryDTO {
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
	progress := JobProgressDTO{
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
		// timestamp. Rows created before protection evidence was persisted
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
		progress.LastCheckpointAt = formatTimePtr(row.LastHeartbeatAt)
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
	summary := RetentionJobSummaryDTO{
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
		Cutoff:                    formatTimePtr(row.Scope.Cutoff),
		PurgeToTime:               formatTimePtr(row.PurgeToTime),
		PolicyRevision:            formatInt64Ptr(row.PolicyRevision),
		PreflightID:               row.PreflightID,
		OperationID:               row.OperationID,
		RequestedAt:               row.RequestedAt.UTC().Format(time.RFC3339),
		StartedAt:                 formatTimePtr(row.StartedAt),
		FinishedAt:                formatTimePtr(row.FinishedAt),
		LastHeartbeatAt:           formatTimePtr(row.LastHeartbeatAt),
		AttemptCount:              row.AttemptCount,
		CancelAllowed:             cancelAllowed,
		Progress:                  progress,
	}
	if row.ErrorCode != nil {
		message := ""
		if row.ErrorMessage != nil {
			message = *row.ErrorMessage
		}
		summary.Error = &JobSafeError{Code: *row.ErrorCode, Message: message}
	}
	return summary
}

const (
	cancellationScopeQueuedNoDataChanged         = "queued_no_data_changed"
	cancellationScopeAutomaticRemainingStepsOnly = "automatic_remaining_steps_only"
	cancellationScopeLegacyUnknown               = "legacy_unknown"
)

func cancellationScopeFor(row retentionJobRow) string {
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

func terminalResultFor(row retentionJobRow) *JobTerminalResultDTO {
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
		return &JobTerminalResultDTO{
			Kind: "succeeded", FinishedAt: finishedAt, AccountingProvenance: provenance,
			VisibilityState: row.VisibilityState, PublishedEpoch: publishedEpoch, PublishedFloor: publishedFloor,
		}
	case "cancelled":
		scope := cancellationScopeFor(row)
		return &JobTerminalResultDTO{
			Kind: "cancelled", FinishedAt: finishedAt, AccountingProvenance: provenance,
			CancellationScope: &scope,
		}
	case "failed":
		outcome := "stable_final_state"
		if row.ContractVersion == 1 {
			outcome = "legacy_unknown"
		}
		result := &JobTerminalResultDTO{
			Kind: "failed", FinishedAt: finishedAt, AccountingProvenance: provenance,
			CoherentOutcome: &outcome,
		}
		if row.ErrorCode != nil {
			message := ""
			if row.ErrorMessage != nil {
				message = *row.ErrorMessage
			}
			result.SafeError = &JobSafeError{Code: *row.ErrorCode, Message: message}
		}
		return result
	case "superseded":
		disposition := "superseded_by_v2_planning"
		return &JobTerminalResultDTO{
			Kind: "superseded", FinishedAt: finishedAt, AccountingProvenance: provenance,
			Disposition: &disposition, LegacyOriginalState: row.LegacyOriginalState,
		}
	default:
		return nil
	}
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

func formatTimePtr(value *time.Time) *string {
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

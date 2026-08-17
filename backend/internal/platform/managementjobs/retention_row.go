package managementjobs

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// retentionJobRow is the full current/legacy log-retention job row projection.
type retentionJobRow struct {
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

// retentionSelectColumns parses this constant, so the leading newline, the
// `SELECT ` prefix and the trailing `\nFROM ` are load-bearing.
const retentionSelect = `
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

// retentionSelectColumns returns the column list without the FROM clause.
func retentionSelectColumns() string {
	withoutFrom := strings.SplitN(retentionSelect, "\nFROM ", 2)[0]
	trimmed := strings.TrimSpace(withoutFrom)
	return strings.TrimPrefix(trimmed, "SELECT ")
}

// retentionSelectColumnsQualified returns the same column list qualified
// with the target alias so RETURNING never collides with a joined CTE.
func retentionSelectColumnsQualified() string {
	raw := strings.Split(retentionSelectColumns(), ",")
	qualified := make([]string, 0, len(raw))
	for _, column := range raw {
		qualified = append(qualified, "j."+strings.TrimSpace(column))
	}
	return strings.Join(qualified, ", ")
}

func scanRetentionRow(scanner interface{ Scan(...any) error }) (retentionJobRow, error) {
	var row retentionJobRow
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
		return retentionJobRow{}, err
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

func (s *Store) loadRetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (retentionJobRow, error) {
	return scanRetentionRow(exec.QueryRow(ctx, retentionSelect+` WHERE id = $1`, id))
}

func (s *Store) loadGlobalRetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (retentionJobRow, error) {
	return scanRetentionRow(exec.QueryRow(ctx, retentionSelect+` WHERE id = $1 AND type = 'log_retention' AND profile_id = 0`, id))
}

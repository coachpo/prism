package settings

// Settings mutation operations persist replay identity beside the durable
// result. The retention routes resolve this record before consuming a mutable
// preflight token, so a lost response can be replayed without a second write.
//
// Resource kind scopes operation ids across retention policy, owner-drift
// archive, and manual-job workflows. Request hashes reject reuse with a
// different payload. Result JSON is the exact response materialization stored
// for successful replay.
//
// This module owns the small SQL boundary for that record. It does not classify
// conflicts, write HTTP envelopes, or decide which workflow may use an id.
//
// `loadOperation` is read inside the caller's transaction before mutable
// preflight state is consumed. `recordOperation` uses conflict convergence so
// the original committed result remains authoritative if a retry races with
// response delivery.
//
// Result JSON is stored as bytes rather than reconstructed from current
// settings. Historical replay therefore returns the original response shape,
// revision, job identity, and coverage evidence.
//
// Operation rows are deliberately narrow. Workflow-specific payload hashes,
// preflight rows, and job rows remain in their own modules.
//
// A missing operation is the only normal lookup miss. SQL errors propagate to
// the owning route and are translated as internal failures rather than as a
// replayable success.
//
// The conflict-convergent insert deliberately does not overwrite an existing
// result. This protects the first committed response from a later retry that
// carries the same operation id.
//
// No operation row is created for a validation failure that never reaches the
// transaction's durable mutation boundary.
//
// The operation table is not a general job queue. It records a completed
// settings response after the owning mutation has committed its domain state.
//
// Callers that need queued execution use managementjobs and keep this record as
// the HTTP replay identity only.
// The row never owns retention cleanup or a worker lease.
//
//
// A durable result is the only successful replay source.
//
// Missing results remain observable failures.
//
import (
	"context"

	"github.com/jackc/pgx/v5"
)

type settingsOperationRow struct {
	ResourceKind string
	OperationID  string
	RequestHash  string
	ResultJSON   []byte
}

func (s *retentionService) loadOperation(ctx context.Context, tx pgx.Tx, resourceKind string, operationID string) (settingsOperationRow, error) {
	var row settingsOperationRow
	err := tx.QueryRow(ctx, `SELECT resource_kind, operation_id, request_hash, result_json
		FROM settings_mutation_operations WHERE resource_kind = $1 AND operation_id = $2`,
		resourceKind, operationID).Scan(&row.ResourceKind, &row.OperationID, &row.RequestHash, &row.ResultJSON)
	if err != nil {
		return settingsOperationRow{}, err
	}
	return row, nil
}

func (s *retentionService) recordOperation(ctx context.Context, tx pgx.Tx, resourceKind string, operationID string, requestHash string, resultJSON []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
		resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
	) VALUES ($1, $2, $3, 'completed', $4, now(), now())
	ON CONFLICT (resource_kind, operation_id) DO NOTHING`,
		resourceKind, operationID, requestHash, resultJSON)
	return err
}

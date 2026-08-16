package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AuditFenceMaterializerProjection is the Requests/Audit-owned protection
// contract consumed by Settings. Retention source/floor/epoch facts remain
// Observe-owned; this projection contains only the reader and materializer
// fence generations and states.
type AuditFenceMaterializerProjection struct {
	ContractVersion        int    `json:"contract_version"`
	FenceGeneration        string `json:"fence_generation"`
	ReaderFenceState       string `json:"reader_fence_state"`
	MaterializerGeneration string `json:"materializer_generation"`
	MaterializerState      string `json:"materializer_state"`
	GeneratedAt            string `json:"generated_at"`
}

type retentionFenceExecutor interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type retentionFenceWriter interface {
	retentionFenceExecutor
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// AffectedWriterUnavailableError is returned when Requests/Audit has not
// published the v2 writer fence required by a Settings/Auth/Proxy mutation.
// The state is intentionally diagnostic-only; HTTP owners map it to their
// registered bounded-unavailable problem without exposing database details.
type AffectedWriterUnavailableError struct {
	State string
}

func (err *AffectedWriterUnavailableError) Error() string {
	if err == nil || err.State == "" {
		return "affected writer admission unavailable"
	}
	return "affected writer admission unavailable: " + err.State
}

const affectedWriterAdvisoryKey = "prism:requests-audit:affected-writer-v2"

// AcquireAffectedWriterAdmission is the first database operation in every
// mutation that can change a Requests/Audit-visible owner. It validates the
// owner's durable writer generation and takes the shared transaction-scoped
// advisory fence before any Settings, Proxy or Auth domain lock. The upgrade
// owner can therefore exclude these writers while draining/finalizing without
// creating a second Settings liveness singleton.
func AcquireAffectedWriterAdmission(ctx context.Context, tx retentionFenceWriter) error {
	if err := loadAffectedWriterState(ctx, tx, true); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended($1, 0))`, affectedWriterAdvisoryKey); err != nil {
		return &AffectedWriterUnavailableError{State: "lock_unavailable"}
	}
	return nil
}

// CheckAffectedWriterAdmission validates the durable writer generation for a
// read-only owner projection. Read paths must not acquire the mutation fence:
// PostgreSQL rejects SELECT FOR SHARE in a read-only transaction, while the
// repeatable-read snapshot still keeps the state and owner data coherent.
func CheckAffectedWriterAdmission(ctx context.Context, tx retentionFenceExecutor) error {
	return loadAffectedWriterState(ctx, tx, false)
}

func loadAffectedWriterState(ctx context.Context, tx retentionFenceExecutor, lockRow bool) error {
	var state string
	var generation int64
	var fenceActive bool
	query := `SELECT state, writer_generation, writer_fence_active
		FROM observability_v2_upgrade_state WHERE id = 1`
	if lockRow {
		query += ` FOR SHARE`
	}
	if err := tx.QueryRow(ctx, query).Scan(&state, &generation, &fenceActive); err != nil {
		return &AffectedWriterUnavailableError{State: "missing"}
	}
	if !fenceActive || generation < 1 || (state != "backfill_ready" && state != "final") {
		return &AffectedWriterUnavailableError{State: state}
	}
	return nil
}

// LoadAuditFenceMaterializerProjection reads the bounded owner projection in
// the caller's transaction/snapshot. The read is deliberately lock-free:
// audit list/coverage handlers use a PostgreSQL read-only transaction, where
// SELECT FOR SHARE is illegal. The owner state is still frozen by the caller's
// repeatable-read snapshot; purge writers use the separate admission/fence
// protocol before publishing a new state.
func LoadAuditFenceMaterializerProjection(ctx context.Context, exec retentionFenceExecutor, now time.Time) (AuditFenceMaterializerProjection, error) {
	var projection AuditFenceMaterializerProjection
	var generatedAt time.Time
	if err := exec.QueryRow(ctx, `SELECT contract_version, fence_generation::text,
		reader_fence_state, materializer_generation::text, materializer_state,
		generated_at
		FROM audit_retention_fence_projections WHERE id = 1`).Scan(
		&projection.ContractVersion,
		&projection.FenceGeneration,
		&projection.ReaderFenceState,
		&projection.MaterializerGeneration,
		&projection.MaterializerState,
		&generatedAt); err != nil {
		return AuditFenceMaterializerProjection{}, fmt.Errorf("load audit retention fence projection: %w", err)
	}
	if projection.ContractVersion != 1 || projection.FenceGeneration == "" || projection.MaterializerGeneration == "" {
		return AuditFenceMaterializerProjection{}, fmt.Errorf("audit retention fence projection is invalid")
	}
	projection.GeneratedAt = generatedAt.UTC().Format(time.RFC3339Nano)
	_ = now // now is part of the owner-read contract; generated_at is stored evidence.
	return projection, nil
}

// MarkAuditRetentionDraining enters the exclusive purge preparation state.
// It is called by the Requests/Audit owner inside the same transaction that
// creates tombstones and scrubs materializer evidence.
func MarkAuditRetentionDraining(ctx context.Context, exec retentionFenceWriter, now time.Time) error {
	_, err := exec.Exec(ctx, `UPDATE audit_retention_fence_projections SET
		fence_generation = fence_generation + 1,
		reader_fence_state = 'waiting_for_readers',
		materializer_generation = materializer_generation + 1,
		materializer_state = 'draining', generated_at = $1, updated_at = $1
		WHERE id = 1`, now.UTC())
	if err != nil {
		return fmt.Errorf("enter audit retention fence: %w", err)
	}
	return nil
}

// MarkAuditRetentionReady publishes the owner protection state after the
// enclosing floor/epoch transaction has reached its terminal checkpoint.
func MarkAuditRetentionReady(ctx context.Context, exec retentionFenceWriter, now time.Time) error {
	_, err := exec.Exec(ctx, `UPDATE audit_retention_fence_projections SET
		fence_generation = fence_generation + 1,
		reader_fence_state = 'clear',
		materializer_generation = materializer_generation + 1,
		materializer_state = 'ready', generated_at = $1, updated_at = $1
		WHERE id = 1`, now.UTC())
	if err != nil {
		return fmt.Errorf("publish audit retention fence: %w", err)
	}
	return nil
}

// MarkAuditRetentionBlocked keeps the owner fail-closed after a recovery
// boundary. The row is intentionally not reset by a generic retry; only the
// explicit coherent recovery/final publish path may call Mark...Ready.
func MarkAuditRetentionBlocked(ctx context.Context, exec retentionFenceWriter, now time.Time) error {
	_, err := exec.Exec(ctx, `UPDATE audit_retention_fence_projections SET
		materializer_generation = materializer_generation + 1,
		reader_fence_state = 'waiting_for_readers',
		materializer_state = 'blocked', generated_at = $1, updated_at = $1
		WHERE id = 1`, now.UTC())
	if err != nil {
		return fmt.Errorf("mark audit retention fence blocked: %w", err)
	}
	return nil
}

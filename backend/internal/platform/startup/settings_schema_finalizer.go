package startup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// settingsSchemaFinalizer is the DB-backed explicit finalizer for the
// Settings/Routing schema (Settings SPEC §14.2, UXM-008 history marker). It
// runs as a low-priority startup component in a SEPARATE transaction after the
// additive migration committed:
//
//  1. acquires the fenced finalizer lease on settings_schema_transition;
//  2. proves every persisted gate (audit provenance complete, owner-drift
//     current heads converged|archived, no >36500 repair issues, no
//     nonterminal legacy log-retention jobs, auth desired/effective converged
//     with no transition, Routing already final from 000007);
//  3. takes ACCESS EXCLUSIVE on the legacy-shape tables, validates the
//     retention 1..36500 NOT VALID CHECKs, drops the duplicated legacy
//     user_settings retention columns and the retired mutable auth columns;
//  4. appends the exact UXM-008 marker through the frozen migration manifest
//     helper and atomically publishes settings_schema_transition.final.
//
// Not-ready or failed finalization never records the marker; the additive
// application stays serviceable in repair/readiness mode and retries under a
// new lease. There is no second process-liveness owner: worker generations
// are the DB-enforced retention_worker_transition_state minimum and the
// Requests/Audit-owned observability_v2_upgrade_state (this repo's §5.6
// realization). No UXM-008 SQL file exists (CI/startup assert).
type settingsSchemaFinalizer struct {
	pool      *pgxpool.Pool
	leaseTTL  time.Duration
	timestamp func() time.Time
}

func newSettingsSchemaFinalizer(pool *pgxpool.Pool) *settingsSchemaFinalizer {
	return &settingsSchemaFinalizer{
		pool:      pool,
		leaseTTL:  2 * time.Minute,
		timestamp: time.Now,
	}
}

// newSettingsSchemaFinalizerWithConn adapts the finalizer to the startup
// connection (single-connection startup phase).
type connFinalizer struct {
	conn      *pgx.Conn
	leaseTTL  time.Duration
	timestamp func() time.Time
}

func newSettingsSchemaFinalizerWithConn(conn *pgx.Conn) *connFinalizer {
	return &connFinalizer{conn: conn, leaseTTL: 2 * time.Minute, timestamp: time.Now}
}

type finalizerTxRunner interface {
	Begin(context.Context) (pgx.Tx, error)
}

func finalizerInTx(ctx context.Context, runner finalizerTxRunner, fn func(pgx.Tx) error) error {
	tx, err := runner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Run executes one fenced finalization pass on the startup connection.
func (f *connFinalizer) Run(ctx context.Context) (bool, error) {
	return runSettingsSchemaFinalization(ctx, f.conn, f.leaseTTL, f.timestamp)
}

// Run attempts one fenced finalization pass; it returns (finalized, error).
func (f *settingsSchemaFinalizer) Run(ctx context.Context) (bool, error) {
	return runSettingsSchemaFinalization(ctx, f.pool, f.leaseTTL, f.timestamp)
}

// runSettingsSchemaFinalization deliberately commits the externally visible
// quiescing and finalizing phases before doing any gate work or DDL. The
// management admission guard therefore cannot observe a seemingly healthy
// schema while finalization is waiting on locks or changing table shape.
func runSettingsSchemaFinalization(ctx context.Context, runner finalizerTxRunner, leaseTTL time.Duration, timestamp func() time.Time) (bool, error) {
	if runner == nil {
		return false, fmt.Errorf("settings schema finalizer database runner is nil")
	}
	if timestamp == nil {
		timestamp = time.Now
	}

	var phase string
	if err := finalizerInTx(ctx, runner, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT domain_phase FROM settings_schema_transition WHERE id = 1`).Scan(&phase)
	}); err != nil {
		return false, err
	}
	if phase == "final" {
		return true, nil
	}

	now := timestamp().UTC()
	token := leaseToken(now)
	var acquired bool
	if err := finalizerInTx(ctx, runner, func(tx pgx.Tx) error {
		commandTag, err := tx.Exec(ctx, `UPDATE settings_schema_transition SET
			domain_phase = CASE WHEN domain_phase = 'finalizing' THEN 'finalizing' ELSE 'quiescing' END,
			finalizer_lease_owner = $1,
			finalizer_lease_token = $2,
			finalizer_lease_expires_at = $3,
			last_safe_error = NULL,
			updated_at = now()
			WHERE id = 1 AND (finalizer_lease_owner IS NULL OR finalizer_lease_expires_at IS NULL OR finalizer_lease_expires_at < now())`,
			"settings-finalizer", token, now.Add(leaseTTL))
		if err != nil {
			return err
		}
		acquired = commandTag.RowsAffected() == 1
		return nil
	}); err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}

	var gatesBlocked bool
	if err := finalizerInTx(ctx, runner, func(tx pgx.Tx) error {
		if err := assertFinalizerLease(ctx, tx, token); err != nil {
			return err
		}
		if err := assertFinalizationGates(ctx, tx); err != nil {
			gatesBlocked = true
			return recordFinalizerError(ctx, tx, token, err)
		}
		_, err := tx.Exec(ctx, `UPDATE settings_schema_transition SET
			domain_phase = 'finalizing', updated_at = now()
			WHERE id = 1 AND finalizer_lease_owner = 'settings-finalizer' AND finalizer_lease_token = $1`, token)
		return err
	}); err != nil {
		return false, err
	}
	if gatesBlocked {
		return false, nil
	}

	ddlErr := finalizerInTx(ctx, runner, func(tx pgx.Tx) error {
		if err := assertFinalizerLease(ctx, tx, token); err != nil {
			return err
		}
		if err := finalizeSchemaDDL(ctx, tx); err != nil {
			return err
		}
		if err := appendUXM008Marker(ctx, tx, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE settings_schema_transition SET
			domain_phase = 'final',
			finalizer_lease_owner = NULL,
			finalizer_lease_token = NULL,
			finalizer_lease_expires_at = NULL,
			finalized_at = now(),
			updated_at = now()
			WHERE id = 1 AND finalizer_lease_owner = 'settings-finalizer' AND finalizer_lease_token = $1`, token)
		return err
	})
	if ddlErr != nil {
		repairErr := finalizerInTx(ctx, runner, func(tx pgx.Tx) error {
			return recordFinalizerError(ctx, tx, token, ddlErr)
		})
		if repairErr != nil {
			return false, errors.Join(ddlErr, repairErr)
		}
		return false, ddlErr
	}
	return true, nil
}

func assertFinalizerLease(ctx context.Context, tx pgx.Tx, token string) error {
	var phase string
	if err := tx.QueryRow(ctx, `SELECT domain_phase FROM settings_schema_transition
		WHERE id = 1 AND finalizer_lease_owner = 'settings-finalizer' AND finalizer_lease_token = $1
		FOR UPDATE`, token).Scan(&phase); err != nil {
		return fmt.Errorf("settings schema finalizer lease lost: %w", err)
	}
	if phase != "quiescing" && phase != "finalizing" {
		return fmt.Errorf("settings schema finalizer phase is %s", phase)
	}
	return nil
}

// finalizeSchemaDDL performs the final DDL steps inside the fenced
// transaction: validates the retention CHECKs, drops the duplicated legacy
// user_settings retention columns and the retired in-place auth columns.
func finalizeSchemaDDL(ctx context.Context, tx pgx.Tx) error {
	for _, column := range []string{"audit_logs_retention_days", "loadbalance_events_retention_days", "request_logs_retention_days", "statistics_retention_days"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`ALTER TABLE public.log_retention_settings VALIDATE CONSTRAINT log_retention_settings_%s_check`, column)); err != nil {
			return fmt.Errorf("validate retention check %s: %w", column, err)
		}
	}
	for _, column := range []string{"request_logs_retention_days", "statistics_retention_days", "audit_logs_retention_days"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`ALTER TABLE public.user_settings DROP COLUMN IF EXISTS %s`, column)); err != nil {
			return fmt.Errorf("drop legacy user_settings column %s: %w", column, err)
		}
	}
	for _, column := range []string{"auth_enabled", "username", "password_hash", "token_version"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`ALTER TABLE public.app_auth_settings DROP COLUMN IF EXISTS %s`, column)); err != nil {
			return fmt.Errorf("drop retired auth column %s: %w", column, err)
		}
	}
	return nil
}

// assertFinalizationGates verifies every persisted gate; any failure returns
// a typed error and the pass is skipped without DDL.
func assertFinalizationGates(ctx context.Context, tx pgx.Tx) error {
	if err := assertRequestsAuditOwnerFinal(ctx, tx); err != nil {
		return err
	}
	// 1. No owner-drift current head may remain in drift.
	var driftHeads int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM settings_migration_evidence
		WHERE is_current AND resolution_state = 'drift'`).Scan(&driftHeads); err != nil {
		return err
	}
	if driftHeads > 0 {
		return fmt.Errorf("finalization gate: %d owner-drift current head(s) still drift", driftHeads)
	}
	// 2. No retention repair issue may remain.
	var repairIssues int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM log_retention_settings
		WHERE request_logs_retention_days > 36500 OR audit_logs_retention_days > 36500
		   OR statistics_retention_days > 36500 OR loadbalance_events_retention_days > 36500`).Scan(&repairIssues); err != nil {
		return err
	}
	if repairIssues > 0 {
		return fmt.Errorf("finalization gate: %d retention repair issue(s) remain", repairIssues)
	}
	// 3. No nonterminal legacy log-retention job may remain; every legacy row
	// needs a current classification evidence head.
	var legacyNonterminal int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 1
		  AND state IN ('queued','running','cancel_requested')`).Scan(&legacyNonterminal); err != nil {
		return err
	}
	if legacyNonterminal > 0 {
		return fmt.Errorf("finalization gate: %d nonterminal legacy log-retention job(s) remain", legacyNonterminal)
	}
	var legacyUnclassified int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM management_jobs AS j
		WHERE j.type = 'log_retention' AND j.contract_version = 1 AND j.classification_evidence_hash IS NULL`).Scan(&legacyUnclassified); err != nil {
		return err
	}
	if legacyUnclassified > 0 {
		return fmt.Errorf("finalization gate: %d legacy job(s) lack classification evidence", legacyUnclassified)
	}
	// 4. Auth desired/effective converged, no transition in progress. The
	// legacy transition columns remain present during the additive window and
	// are checked as well; a partially published old writer must never be
	// hidden by the new pointer columns.
	var authTransitions int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM app_auth_settings
		WHERE transition_state IS NOT NULL
		   OR desired_config_version_id IS DISTINCT FROM effective_config_version_id
		   OR desired_generation IS DISTINCT FROM effective_generation`).Scan(&authTransitions); err != nil {
		return err
	}
	if authTransitions > 0 {
		return fmt.Errorf("finalization gate: auth transition in progress")
	}
	var legacyAuthColumns bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'app_auth_settings'
		  AND column_name = 'auth_transition_state'
	)`).Scan(&legacyAuthColumns); err != nil {
		return err
	}
	if legacyAuthColumns {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM app_auth_settings
			WHERE auth_transition_state IS NOT NULL`).Scan(&authTransitions); err != nil {
			return err
		}
		if authTransitions > 0 {
			return fmt.Errorf("finalization gate: legacy auth transition in progress")
		}
	}
	// 5. Audit family provenance complete for every family row.
	var auditProvenanceMissing int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM profile_api_family_audit_settings
		WHERE migration_provenance IS NULL`).Scan(&auditProvenanceMissing); err != nil {
		return err
	}
	if auditProvenanceMissing > 0 {
		return fmt.Errorf("finalization gate: %d audit family row(s) missing provenance", auditProvenanceMissing)
	}
	// 6. Worker fence converged: no unclassified origin state on v2 rows.
	var v2Unclassified int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND origin IS NULL`).Scan(&v2Unclassified); err != nil {
		return err
	}
	if v2Unclassified > 0 {
		return fmt.Errorf("finalization gate: %d v2 job(s) lack origin", v2Unclassified)
	}
	return nil
}

// assertRequestsAuditOwnerFinal consumes the existing Requests/Audit upgrade
// state. Settings does not create a second liveness or admission singleton;
// final DDL is allowed only after the owner has permanently fenced legacy
// writers and completed every scrub/backfill domain.
func assertRequestsAuditOwnerFinal(ctx context.Context, tx pgx.Tx) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('public.observability_v2_upgrade_state') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("finalization gate: Requests/Audit owner state is unavailable")
	}
	var state string
	var writerGeneration int64
	var writerFenceActive bool
	var v1OutboxCount, v1AcceptedCount, oldLeaseCount, oldWriterCount int64
	if err := tx.QueryRow(ctx, `SELECT state, writer_generation, writer_fence_active,
		v1_finalized_outbox_count, v1_accepted_outbox_count,
		old_owner_lease_count, old_writer_generation_count
		FROM observability_v2_upgrade_state WHERE id = 1`).Scan(
		&state, &writerGeneration, &writerFenceActive, &v1OutboxCount,
		&v1AcceptedCount, &oldLeaseCount, &oldWriterCount); err != nil {
		return err
	}
	if state != "final" || !writerFenceActive || writerGeneration < 1 ||
		v1OutboxCount != 0 || v1AcceptedCount != 0 || oldLeaseCount != 0 || oldWriterCount != 0 {
		return fmt.Errorf("finalization gate: Requests/Audit owner is not final (state=%s generation=%d fence=%t outbox=%d accepted=%d leases=%d writers=%d)",
			state, writerGeneration, writerFenceActive, v1OutboxCount, v1AcceptedCount, oldLeaseCount, oldWriterCount)
	}
	var unready int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM observability_v2_backfill_state WHERE status <> 'ready'`).Scan(&unready); err != nil {
		return err
	}
	if unready > 0 {
		return fmt.Errorf("finalization gate: %d Requests/Audit backfill domain(s) are not ready", unready)
	}
	return nil
}

func recordFinalizerError(ctx context.Context, tx pgx.Tx, token string, gateErr error) error {
	_, err := tx.Exec(ctx, `UPDATE settings_schema_transition SET
		domain_phase = 'repair_ready',
		finalizer_lease_owner = NULL,
		finalizer_lease_token = NULL,
		finalizer_lease_expires_at = NULL,
		last_safe_error = $1,
		updated_at = now()
		WHERE id = 1 AND finalizer_lease_owner = 'settings-finalizer' AND finalizer_lease_token = $2`, gateErr.Error(), token)
	return err
}

// appendUXM008Marker appends the exact frozen-manifest marker for the
// explicit finalizer (Settings SPEC §14.2). There is deliberately no
// <000013>_*.sql file; CI/startup assert this.
func appendUXM008Marker(ctx context.Context, tx pgx.Tx, now time.Time) error {
	const canonicalPayload = "uxm_slot|UXM-008|Settings|marker|settings-schema-finalizer"
	const physicalVersion = "000015-marker"
	const markerName = "settings-schema-finalizer"
	contentHash := sha256.Sum256([]byte(canonicalPayload))
	var existingPhysical, existingOwner, existingKind, existingName, existingHash string
	err := tx.QueryRow(ctx, `SELECT physical_version, owner, kind, filename_or_marker, content_hash
		FROM prism_migration_history
		WHERE history_identity = 'uxm_slot' AND logical_slot = 'UXM-008'`).Scan(
		&existingPhysical, &existingOwner, &existingKind, &existingName, &existingHash)
	if err == nil {
		if existingPhysical != physicalVersion || existingOwner != "Settings" || existingKind != "marker" ||
			existingName != markerName || existingHash != hex.EncodeToString(contentHash[:]) {
			return fmt.Errorf("UXM-008 history marker does not match the frozen manifest")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var prefixCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM prism_migration_history
		WHERE history_identity = 'uxm_slot' AND logical_slot = 'UXM-007'
		  AND physical_version = '000015' AND owner = 'Settings'
		  AND kind = 'migration' AND filename_or_marker = '000015_settings_safety_additive.sql'`).Scan(&prefixCount); err != nil {
		return err
	}
	if prefixCount != 1 {
		return fmt.Errorf("UXM-008 history marker prefix is not exactly at UXM-007")
	}
	_, err = tx.Exec(ctx, `INSERT INTO prism_migration_history (
		history_identity, logical_slot, physical_version, owner, kind, filename_or_marker, content_hash, recorded_at
	) VALUES ('uxm_slot', 'UXM-008', $1, 'Settings', 'marker', $2, $3, $4)`,
		physicalVersion, markerName, hex.EncodeToString(contentHash[:]), now)
	return err
}

func leaseToken(now time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("settings-finalizer|%d", now.UnixNano())))
	return hex.EncodeToString(sum[:])
}

package settings

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	"github.com/jackc/pgx/v5"
)

func (s *retentionService) handleCreateManualRetentionJob(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	var request createManualRetentionJobRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.OperationID) == "" || strings.TrimSpace(request.PreflightToken) == "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "operation_id and preflight_token are required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}

	var summary managementjobs.RetentionJobSummaryDTO
	replayed := false
	accepted := false
	err := pgxutil.InTx(r.Context(), s.pool, "settings manual retention job", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		// Replay first: a lost-response retry carries the same operation id and
		// request hash and must return the recorded job even though the
		// preflight token was already consumed (SPEC §6.4).
		operation, err := s.loadOperation(r.Context(), tx, "manual_retention_job", request.OperationID)
		if err == nil {
			hash := canonicalManualJobHash(request)
			if operation.RequestHash == hash {
				replayed = true
				if operation.ResultJSON != nil {
					return json.Unmarshal(operation.ResultJSON, &summary)
				}
				return nil
			}
			return &settingsConflictError{code: "operation_id_conflict", operationID: request.OperationID}
		}
		if err != pgx.ErrNoRows {
			return err
		}

		tokenHash := hashToken(request.PreflightToken)
		var preflight retentionPreflightRow
		err = tx.QueryRow(r.Context(), `SELECT id, kind, operation_id, preflight_attempt_id, token_hash, request_hash,
			settings_revision, principal_generation, affected_domains, expires_at, consumed_at
			FROM log_retention_preflights WHERE token_hash = $1 FOR UPDATE`, tokenHash).Scan(
			&preflight.ID, &preflight.Kind, &preflight.OperationID, &preflight.PreflightAttemptID, &preflight.TokenHash,
			&preflight.RequestHash, &preflight.SettingsRevision, &preflight.PrincipalGeneration, &preflight.AffectedDomains, &preflight.ExpiresAt, &preflight.ConsumedAt)
		if err != nil {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.Kind != "manual_cleanup" || preflight.OperationID != request.OperationID {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.PrincipalGeneration == nil || *preflight.PrincipalGeneration != managementPrincipalGeneration(r) {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if request.Confirmation.Keyword != "DELETE" {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.ExpiresAt.Before(s.now().UTC()) {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.ConsumedAt != nil {
			// The first request may have committed the job and lost its HTTP
			// response. Re-read the durable outcome before declaring the
			// single-use token stale.
			operation, operationErr := s.loadOperation(r.Context(), tx, "manual_retention_job", request.OperationID)
			if operationErr == nil && operation.RequestHash == canonicalManualJobHash(request) && operation.ResultJSON != nil {
				replayed = true
				return json.Unmarshal(operation.ResultJSON, &summary)
			}
			return &settingsConflictError{code: "retention_preflight_stale"}
		}

		// The sealed preflight is the single source of truth for the scope.
		var domains []retentionAffectedDomain
		if err := json.Unmarshal(preflight.AffectedDomains, &domains); err != nil {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if len(domains) != 1 {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		dataset := domains[0].Dataset
		currentSnapshot, snapshotErr := s.ownerSnapshotFor(r.Context(), tx, dataset, s.now().UTC())
		if snapshotErr != nil {
			return snapshotErr
		}
		if !domains[0].Impact.SemanticFactsComplete {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		if canonicalOwnerSemanticSnapshotHash(domains[0].OwnerSnapshot) != canonicalOwnerSemanticSnapshotHash(currentSnapshot) {
			return &settingsConflictError{
				code:               "retention_preflight_stale",
				currentGenerations: map[string]string{dataset: ownerSnapshotGeneration(currentSnapshot)},
			}
		}
		mode := ""
		if change, ok := domains[0].Impact.Change.(map[string]any); ok {
			if value, ok := change["mode"].(string); ok {
				mode = value
			}
		}
		var cutoff *time.Time
		if domains[0].Impact.ResolvedCutoff != nil {
			parsed, parseErr := time.Parse(time.RFC3339Nano, *domains[0].Impact.ResolvedCutoff)
			if parseErr != nil {
				return &settingsConflictError{code: "retention_preflight_stale"}
			}
			cutoff = &parsed
		}
		deleteAll := mode == "delete_all"

		job, err := s.jobs.CreateManualRetentionJobTx(r.Context(), tx, dataset, cutoff, deleteAll, request.OperationID, preflight.ID, s.now().UTC())
		if err != nil {
			if isJobConflict(err) {
				return &settingsConflictError{code: "retention_job_conflict"}
			}
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE log_retention_preflights SET consumed_at = now(),
			consumed_operation_id = $2 WHERE id = $1`, preflight.ID, request.OperationID); err != nil {
			return err
		}
		summary = job
		accepted = true
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return s.recordOperation(r.Context(), tx, "manual_retention_job", request.OperationID, canonicalManualJobHash(request), raw)
	})
	if err != nil {
		writeSettingsError(w, r, corsSnapshot, err)
		return
	}
	status := http.StatusOK
	if accepted && !replayed {
		status = http.StatusAccepted
	}
	writeSettingsJSON(w, status, map[string]any{
		"operation_id": request.OperationID,
		"replayed":     replayed,
		"job":          summary,
	})
}

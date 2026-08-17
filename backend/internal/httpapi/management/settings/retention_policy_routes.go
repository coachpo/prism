package settings

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/jackc/pgx/v5"
)

func (s *retentionService) handleGetRetentionSettings(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "settings retention read", func(tx pgx.Tx) (logRetentionSettingsResponse, error) {
		return s.buildSettingsResponse(r.Context(), tx, s.now().UTC())
	})
	if err != nil {
		writeSettingsInternalError(w, r, corsSnapshot, err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *retentionService) handlePutRetentionSettings(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	var request putLogRetentionSettingsRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.OperationID) == "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "operation_id is required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}
	violations := validateRetentionPolicies(request.Policies)
	if len(violations) > 0 {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "invalid_retention_policy", Detail: "invalid retention policy", Params: map[string]any{}, Details: map[string]any{"violations": violations}}, http.StatusUnprocessableEntity)
		return
	}

	var result putLogRetentionSettingsResult
	replayed := false
	err := pgxutil.InTx(r.Context(), s.pool, "settings retention put", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		// Resolve the durable operation before touching a single-use preflight.
		// A lost response must be replayable after the original transaction has
		// consumed its token; checking the token first would turn a valid retry
		// into a false stale-preflight error.
		operation, operationErr := s.loadOperation(r.Context(), tx, "log_retention", request.OperationID)
		if operationErr == nil {
			hash := canonicalRequestHashForBody(request)
			if operation.RequestHash == hash {
				if operation.ResultJSON != nil {
					replayed = true
					return json.Unmarshal(operation.ResultJSON, &result)
				}
				return &settingsConflictError{code: "operation_outcome_unavailable", operationID: request.OperationID}
			}
			return &settingsConflictError{code: "operation_id_conflict", operationID: request.OperationID}
		}
		if operationErr != pgx.ErrNoRows {
			return operationErr
		}

		current, err := loadRetentionRowForUpdate(r.Context(), tx)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", current.Revision) != request.ExpectedRevision {
			return &settingsConflictError{code: "retention_settings_changed", currentRevision: fmt.Sprintf("%d", current.Revision)}
		}

		// Destructive classifier over the full four-field draft.
		destructiveDatasets := []string{}
		for _, dataset := range retentionDatasets {
			before := policyFieldForRow(current, dataset)
			after := policyFieldValue(request.Policies, dataset)
			if isDestructiveTransition(before, after) {
				destructiveDatasets = append(destructiveDatasets, dataset)
			}
		}
		if len(destructiveDatasets) > 0 {
			if request.PreflightToken == nil || strings.TrimSpace(*request.PreflightToken) == "" {
				return &settingsConflictError{code: "retention_preflight_required"}
			}
			if request.Confirmation == nil || strings.TrimSpace(request.Confirmation.Keyword) == "" {
				return &settingsConflictError{code: "retention_preflight_required"}
			}
			if err := s.consumePreflight(r.Context(), tx, *request.PreflightToken, request.OperationID, current.Revision, canonicalPolicyBindingHash(request.OperationID, request.ExpectedRevision, request.Policies), destructiveDatasets); err != nil {
				return err
			}
			if request.Confirmation.Keyword != "DELETE" {
				return &settingsConflictError{code: "retention_preflight_stale"}
			}
		}

		newRevision := current.Revision + 1
		now := s.now().UTC()

		// Owner-drift lineage: terminalize changed-field heads and append
		// post-commit successors (SPEC §14.1 item 12).
		if err := s.advanceOwnerDriftLineage(r.Context(), tx, current, request.Policies, now); err != nil {
			return err
		}

		// Atomic full replacement.
		if _, err := tx.Exec(r.Context(), `UPDATE log_retention_settings SET
			request_logs_retention_days = $1, audit_logs_retention_days = $2,
			statistics_retention_days = $3, loadbalance_events_retention_days = $4,
			revision = $5, updated_at = $6 WHERE singleton_key = 'global'`,
			request.Policies.RequestLogsRetentionDays, request.Policies.AuditLogsRetentionDays,
			request.Policies.StatisticsRetentionDays, request.Policies.LoadbalanceEventsRetentionDays,
			newRevision, now); err != nil {
			return err
		}

		// Advance policy resources + desired work for changed datasets.
		changes := []retentionChangeItem{}
		scheduledWork := []retentionScheduledWork{}
		for _, dataset := range retentionDatasets {
			before := policyFieldForRow(current, dataset)
			after := policyFieldValue(request.Policies, dataset)
			if intPtrsEqual(before, after) {
				continue
			}
			change := retentionChangeItem{
				Dataset:     dataset,
				Before:      taggedPolicyValue(before),
				AfterDays:   after,
				Destructive: isDestructiveTransition(before, after),
			}
			cutoff, work, err := s.applyPolicyResource(r.Context(), tx, dataset, before, after, newRevision, now)
			if err != nil {
				return err
			}
			change.LogicalCutoff = formatTimePtr(cutoff)
			if work != nil {
				scheduledWork = append(scheduledWork, *work)
			}
			changes = append(changes, change)
		}

		// Build the post-commit settings response.
		settingsResponse, err := s.buildSettingsResponse(r.Context(), tx, now)
		if err != nil {
			return err
		}
		result = putLogRetentionSettingsResult{
			Settings:      settingsResponse,
			Changes:       changes,
			ScheduledWork: scheduledWork,
			OperationID:   request.OperationID,
			Replayed:      false,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		hash := canonicalRequestHashForBody(request)
		return s.recordOperation(r.Context(), tx, "log_retention", request.OperationID, hash, raw)
	})
	if err != nil {
		writeSettingsError(w, r, corsSnapshot, err)
		return
	}
	result.Replayed = replayed
	writeSettingsJSON(w, http.StatusOK, result)
}

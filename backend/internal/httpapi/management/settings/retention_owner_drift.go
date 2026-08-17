package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/jackc/pgx/v5"
)

func (s *retentionService) loadOwnerDriftInventory(ctx context.Context, tx pgx.Tx) (*retentionOwnerDriftInventory, error) {
	var inventoryGeneration string
	var generatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT inventory_generation, updated_at FROM settings_owner_drift_inventory WHERE id = 1`).
		Scan(&inventoryGeneration, &generatedAt)
	if err != nil {
		return nil, fmt.Errorf("load owner drift inventory: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT head_id, lineage_generation, predecessor_head_id, field, evidence_hash,
		instance_value, legacy_copy_value, resolution_state, generated_at, resolved_at
		FROM settings_migration_evidence WHERE is_current ORDER BY field ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	heads := []retentionOwnerDriftHead{}
	state := "resolved"
	for rows.Next() {
		head := retentionOwnerDriftHead{}
		var instanceRaw, legacyRaw []byte
		var predecessor *string
		var generatedAt time.Time
		var resolvedAt *time.Time
		if err := rows.Scan(&head.HeadID, &head.LineageGeneration, &predecessor, &head.Field, &head.EvidenceHash,
			&instanceRaw, &legacyRaw, &head.ResolutionState, &generatedAt, &resolvedAt); err != nil {
			return nil, err
		}
		head.PredecessorHeadID = predecessor
		head.Resolution = "archive_legacy_copy_keep_instance_owner"
		head.GeneratedAt = generatedAt.UTC().Format(time.RFC3339)
		if resolvedAt != nil {
			formatted := resolvedAt.UTC().Format(time.RFC3339)
			head.ResolvedAt = &formatted
		}
		_ = json.Unmarshal(instanceRaw, &head.InstanceValue)
		_ = json.Unmarshal(legacyRaw, &head.LegacyCopyValue)
		if head.ResolutionState == "drift" {
			state = "action_required"
		}
		heads = append(heads, head)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &retentionOwnerDriftInventory{
		InventoryGeneration: inventoryGeneration,
		State:               state,
		CurrentHeads:        heads,
		GeneratedAt:         generatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// advanceOwnerDriftLineage implements the per-field current-head lineage rule
// (SPEC §14.1 item 12): for every duplicated field whose authoritative
// instance value changes while the legacy column still exists, the old current
// head is terminalized with superseded_by_policy_change and exactly one
// successor is appended using the new instance value plus the unchanged legacy
// value; the inventory generation advances in the same commit. Unchanged-field
// heads remain current.
func (s *retentionService) advanceOwnerDriftLineage(ctx context.Context, tx pgx.Tx, current retentionRow, policies retentionPolicies, now time.Time) error {
	var inventoryGeneration string
	if err := tx.QueryRow(ctx, `SELECT inventory_generation FROM settings_owner_drift_inventory WHERE id = 1 FOR UPDATE`).Scan(&inventoryGeneration); err != nil {
		return err
	}
	for _, field := range []string{"request_logs_retention_days", "statistics_retention_days", "audit_logs_retention_days"} {
		var before *int
		var after *int
		switch field {
		case "request_logs_retention_days":
			before = current.RequestLogsRetentionDays
			after = policies.RequestLogsRetentionDays
		case "statistics_retention_days":
			before = current.StatisticsRetentionDays
			after = policies.StatisticsRetentionDays
		default:
			before = current.AuditLogsRetentionDays
			after = policies.AuditLogsRetentionDays
		}
		if intPtrsEqual(before, after) {
			continue
		}

		// Legacy copy value stays unchanged; instance value becomes `after`.
		var headID, evidenceHash, legacyRaw []byte
		if err := tx.QueryRow(ctx, `SELECT head_id, evidence_hash, legacy_copy_value
			FROM settings_migration_evidence WHERE field = $1 AND is_current FOR UPDATE`, field).
			Scan(&headID, &evidenceHash, &legacyRaw); err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return err
		}
		instanceJSON, err := json.Marshal(taggedPolicyValue(after))
		if err != nil {
			return err
		}
		newGeneration := nextLineageGeneration(inventoryGeneration)
		newHeadID := canonicalHash("drift-head", field, newGeneration)
		newEvidenceHash := canonicalHash("gen", newGeneration, "field", field, "instance", string(instanceJSON), "legacy", string(legacyRaw))

		if _, err := tx.Exec(ctx, `UPDATE settings_migration_evidence SET
			is_current = FALSE, terminal_disposition = 'superseded_by_policy_change', resolved_at = $2
			WHERE field = $1 AND is_current`, field, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO settings_migration_evidence (
			head_id, lineage_generation, predecessor_head_id, field, evidence_hash,
			instance_value, legacy_copy_value, resolution_state, terminal_disposition,
			is_current, generated_at, resolved_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, TRUE, $9, NULL)`,
			newHeadID, newGeneration, string(headID), field, newEvidenceHash,
			instanceJSON, legacyRaw, equalTagged(instanceJSON, legacyRaw), now); err != nil {
			return err
		}
	}
	// Advance the inventory generation exactly once per policy commit.
	newGeneration := nextLineageGeneration(inventoryGeneration)
	if _, err := tx.Exec(ctx, `UPDATE settings_owner_drift_inventory SET
		inventory_generation = $1, updated_at = now() WHERE id = 1`, newGeneration); err != nil {
		return err
	}
	return nil
}

func nextLineageGeneration(current string) string {
	var number int
	_, _ = fmt.Sscanf(current, "%d", &number)
	return fmt.Sprintf("%d", number+1)
}

func equalTagged(instanceRaw, legacyRaw []byte) string {
	if string(instanceRaw) == string(legacyRaw) {
		return "converged"
	}
	return "drift"
}

func (s *retentionService) handleArchiveOwnerDrift(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	var request archiveRetentionOwnerDriftRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.OperationID) == "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "operation_id is required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}
	if request.Acknowledgement != "keep_instance_policy_and_archive_legacy_copy" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "invalid acknowledgement", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}

	var result map[string]any
	replayed := false
	err := pgxutil.InTx(r.Context(), s.pool, "settings owner drift archive", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		// Resolve the durable outcome before checking the mutable revision. A
		// response-loss retry must replay even after another safe Settings
		// mutation has advanced the revision.
		operation, operationErr := s.loadOperation(r.Context(), tx, "owner_drift_archive", request.OperationID)
		if operationErr == nil {
			hash := canonicalArchiveHash(request)
			if operation.RequestHash == hash {
				replayed = true
				if operation.ResultJSON != nil {
					return json.Unmarshal(operation.ResultJSON, &result)
				}
				return &settingsConflictError{code: "operation_outcome_unavailable", operationID: request.OperationID}
			}
			return &settingsConflictError{code: "operation_id_conflict", operationID: request.OperationID}
		}
		if operationErr != pgx.ErrNoRows {
			return operationErr
		}

		current, err := loadRetentionRow(r.Context(), tx)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", current.Revision) != request.ExpectedRevision {
			return &settingsConflictError{code: "retention_settings_changed", currentRevision: fmt.Sprintf("%d", current.Revision)}
		}
		var inventoryGeneration string
		if err := tx.QueryRow(r.Context(), `SELECT inventory_generation FROM settings_owner_drift_inventory WHERE id = 1`).Scan(&inventoryGeneration); err != nil {
			return err
		}
		if inventoryGeneration != request.ExpectedInventoryGeneration {
			return &settingsConflictError{code: "retention_owner_drift_changed"}
		}

		// Verify each requested head is an exact current drift head.
		if len(request.Heads) == 0 {
			return &settingsConflictError{code: "validation_failed"}
		}
		seenFields := map[string]struct{}{}
		archived := []map[string]any{}
		for _, head := range request.Heads {
			if _, exists := seenFields[head.Field]; exists {
				return &settingsConflictError{code: "validation_failed"}
			}
			seenFields[head.Field] = struct{}{}
			var currentHead retentionOwnerDriftHead
			var instanceRaw, legacyRaw []byte
			err := tx.QueryRow(r.Context(), `SELECT head_id, evidence_hash, instance_value, legacy_copy_value, resolution_state
				FROM settings_migration_evidence WHERE field = $1 AND is_current FOR UPDATE`, head.Field).
				Scan(&currentHead.HeadID, &currentHead.EvidenceHash, &instanceRaw, &legacyRaw, &currentHead.ResolutionState)
			if err != nil {
				return &settingsConflictError{code: "retention_owner_drift_changed"}
			}
			if currentHead.HeadID != head.HeadID || currentHead.EvidenceHash != head.EvidenceHash {
				return &settingsConflictError{code: "retention_owner_drift_changed"}
			}
			if currentHead.ResolutionState != "drift" {
				return &settingsConflictError{code: "retention_owner_drift_changed"}
			}
			if _, err := tx.Exec(r.Context(), `UPDATE settings_migration_evidence SET
				resolution_state = 'archived', resolved_at = now() WHERE head_id = $1`, head.HeadID); err != nil {
				return err
			}
			archived = append(archived, map[string]any{"field": head.Field, "head_id": head.HeadID})
		}
		if _, err := tx.Exec(r.Context(), `UPDATE settings_owner_drift_inventory SET updated_at = now() WHERE id = 1`); err != nil {
			return err
		}

		settingsResponse, err := s.buildSettingsResponse(r.Context(), tx, s.now().UTC())
		if err != nil {
			return err
		}
		result = map[string]any{
			"operation_id":         request.OperationID,
			"replayed":             false,
			"archived_heads":       archived,
			"archived_field_count": len(archived),
			"settings":             settingsResponse,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return s.recordOperation(r.Context(), tx, "owner_drift_archive", request.OperationID, canonicalArchiveHash(request), raw)
	})
	if err != nil {
		writeSettingsError(w, r, corsSnapshot, err)
		return
	}
	if replayed {
		result["replayed"] = true
	}
	writeSettingsJSON(w, http.StatusOK, result)
}

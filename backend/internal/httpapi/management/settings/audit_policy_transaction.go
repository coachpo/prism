package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) loadAuditGroupStateDetails(ctx context.Context, tx pgx.Tx, profileID int, forUpdate bool) (auditGroupState, error) {
	var state auditGroupState
	query := `SELECT revision, updated_at FROM profile_audit_settings_state WHERE profile_id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, query, profileID).Scan(&state.Revision, &state.UpdatedAt)
	if err == nil {
		return state, nil
	}
	if err != pgx.ErrNoRows {
		return auditGroupState{}, err
	}
	if !forUpdate {
		// Read-only projection: a missing group row means the profile has no
		// saved audit settings yet; the default revision is 1 without writing
		// inside the read-only transaction.
		return auditGroupState{Revision: 1, UpdatedAt: s.now().UTC()}, nil
	}
	// Fresh profiles get a generation-1 group state lazily on first write.
	if _, err := tx.Exec(ctx, `INSERT INTO profile_audit_settings_state (profile_id, revision, writer_generation, updated_at)
		VALUES ($1, 1, 1, now()) ON CONFLICT (profile_id) DO NOTHING`, profileID); err != nil {
		return auditGroupState{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT revision, updated_at FROM profile_audit_settings_state WHERE profile_id = $1 FOR UPDATE`, profileID).Scan(&state.Revision, &state.UpdatedAt); err != nil {
		return auditGroupState{}, err
	}
	return state, nil
}

func (s *Service) loadAuditGroupState(ctx context.Context, tx pgx.Tx, profileID int, forUpdate bool) (int64, error) {
	state, err := s.loadAuditGroupStateDetails(ctx, tx, profileID, forUpdate)
	return state.Revision, err
}

// putAuditSettingsInTransaction owns the single policy CAS transaction.
// It keeps writer admission, replay-before-revision, full family replacement,
// revision publication, and operation recording atomic.
func (s *Service) putAuditSettingsInTransaction(ctx context.Context, r *http.Request, request putAuditSettingsRequest) (putAuditSettingsResponse, bool, error) {
	var result putAuditSettingsResponse
	replayed := false
	err := pgxutil.InTx(ctx, s.pool, "settings audit put", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(ctx, tx); err != nil {
			return newSettingsMutationError(http.StatusServiceUnavailable, SettingsProblem{
				Code:    "settings_owner_unavailable",
				Detail:  "Requests/Audit writer admission is temporarily unavailable",
				Params:  map[string]any{},
				Details: map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		// Replay is checked before the current revision so a lost response stays
		// replayable after the successful write advanced the group revision.
		var operationRow struct {
			RequestHash string
			ResultJSON  []byte
		}
		err := tx.QueryRow(ctx, `SELECT request_hash, result_json FROM settings_mutation_operations
			WHERE resource_kind = 'audit_settings' AND operation_id = $1`, request.OperationID).
			Scan(&operationRow.RequestHash, &operationRow.ResultJSON)
		if err == nil {
			hash := canonicalAuditHash(request)
			if operationRow.RequestHash == hash {
				replayed = true
				if len(operationRow.ResultJSON) > 0 {
					return json.Unmarshal(operationRow.ResultJSON, &result)
				}
				return nil
			}
			return newSettingsMutationError(http.StatusConflict, SettingsProblem{
				Code:    "operation_id_conflict",
				Detail:  "operation id already used with a different request",
				Params:  map[string]any{},
				Details: map[string]any{"operation_id": request.OperationID, "recovery": "inspect_operation"},
			})
		}
		if err != pgx.ErrNoRows {
			return err
		}

		profile, err := profiledomain.ResolveEffectiveProfile(ctx, tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return err
		}
		revision, err := s.loadAuditGroupState(ctx, tx, profile.ID, true)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", revision) != request.ExpectedRevision {
			return newSettingsMutationError(http.StatusConflict, SettingsProblem{
				Code:    "audit_settings_changed",
				Detail:  "audit settings changed concurrently",
				Params:  map[string]any{},
				Details: map[string]any{"current_revision": fmt.Sprintf("%d", revision), "recovery": "refresh"},
			})
		}

		// Upsert preserving the immutable migration provenance.
		for _, policy := range request.Policies {
			enabled, capture := flagsFromMode(policy.Mode)
			if _, err := tx.Exec(ctx, `INSERT INTO profile_api_family_audit_settings (
				profile_id, api_family, audit_enabled, audit_capture_bodies, migration_provenance, created_at, updated_at
			) VALUES ($1, $2, $3, $4, 'explicit', now(), now())
			ON CONFLICT ON CONSTRAINT uq_profile_api_family_audit_settings_profile_family DO UPDATE
			SET audit_enabled = $3, audit_capture_bodies = $4, updated_at = now()`,
				profile.ID, policy.Family, enabled, capture); err != nil {
				return err
			}
		}
		updatedAt := s.now().UTC()
		commandTag, err := tx.Exec(ctx, `UPDATE profile_audit_settings_state SET revision = revision + 1, updated_at = $3
			WHERE profile_id = $1 AND revision = $2`, profile.ID, revision, updatedAt)
		if err != nil {
			return err
		}
		if commandTag.RowsAffected() != 1 {
			return newSettingsMutationError(http.StatusConflict, SettingsProblem{
				Code:    "audit_settings_changed",
				Detail:  "audit settings changed concurrently",
				Params:  map[string]any{},
				Details: map[string]any{"recovery": "refresh"},
			})
		}

		newRevision := revision + 1
		policies := []auditPolicyRow{}
		for _, family := range auditFamilies {
			mode := auditModeDisabled
			for _, policy := range request.Policies {
				if policy.Family == family {
					mode = policy.Mode
				}
			}
			policies = append(policies, auditPolicyRow{Family: family, Mode: mode})
		}
		result = putAuditSettingsResponse{
			OperationID: request.OperationID,
			Replayed:    false,
			Settings: targetAuditSettingsResponse{
				Revision:  fmt.Sprintf("%d", newRevision),
				UpdatedAt: updatedAt.Format(time.RFC3339),
				Policies:  policies,
				FixedCaptureLimits: map[string]int64{
					"per_request_body_bytes":               4 * 1024 * 1024,
					"aggregate_request_body_bytes":         12 * 1024 * 1024,
					"final_response_body_bytes":            4 * 1024 * 1024,
					"aggregate_raw_body_bytes_per_ingress": 16 * 1024 * 1024,
				},
			},
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
			resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
		) VALUES ('audit_settings', $1, $2, 'completed', $3, now(), now())
		ON CONFLICT (resource_kind, operation_id) DO NOTHING`,
			request.OperationID, canonicalAuditHash(request), raw)
		return err
	})
	return result, replayed, err
}

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func authSettingsLegacyColumnsExistInTransaction(ctx context.Context, tx pgx.Tx) (bool, error) {
	var legacyColumnsExist bool
	err := tx.QueryRow(ctx, `SELECT COUNT(*) > 0 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'app_auth_settings' AND column_name = 'auth_enabled'`).Scan(&legacyColumnsExist)
	return legacyColumnsExist, err
}

func buildAuthSettingsMutationResult(row authSettingsReadRow, request putAuthSettingsRequest, readiness proxyKeyReadinessSnapshot, input authSettingsMutationInput, draft authSettingsVersionDraft) authOperationResult {
	config := authConfigVersionRow{
		ID: draft.configID, SubjectKey: "app", Generation: draft.newGeneration,
		DesiredMode: draft.desiredMode, Username: draft.username, PasswordHash: draft.passwordHash,
		SessionVersion: draft.sessionVersion, State: "effective",
		CreatedAt: draft.commitAt, UpdatedAt: draft.commitAt, ZeroKeyAck: input.zeroKeyAcknowledged,
	}
	effectiveRow := row
	configIDCopy := draft.configID
	generationCopy := draft.newGeneration
	effectiveRow.Revision = row.Revision + 1
	effectiveRow.DesiredConfigID = &configIDCopy
	effectiveRow.EffectiveConfigID = &configIDCopy
	effectiveRow.DesiredGeneration = &generationCopy
	effectiveRow.EffectiveGeneration = &generationCopy
	effectiveRow.DesiredAuthEnabled = request.DesiredAuthEnabled
	effectiveRow.DesiredUsername = draft.username
	effectiveRow.DesiredPasswordHash = draft.passwordHash
	effectiveRow.LegacyAuthEnabled = request.DesiredAuthEnabled
	effectiveRow.LegacyUsername = draft.username
	effectiveRow.LegacyPasswordHash = draft.passwordHash
	effectiveRow.LegacyTokenVersion = draft.sessionVersion
	effectiveRow.UpdatedAt = draft.commitAt
	effectiveRow.TransitionOperationID = nil
	effectiveRow.TransitionKind = nil
	effectiveRow.TransitionState = nil
	settingsResponse := buildAuthSettingsResponseFromState(effectiveRow, &config, readiness, false)
	sessionAction := "none"
	if input.enabling || (input.accountUpdate && row.LegacyAuthEnabled) {
		sessionAction = "clear_and_login"
	} else if input.disabling && row.LegacyAuthEnabled {
		sessionAction = "clear_and_continue"
	}
	return authOperationResult{
		OperationID:         request.OperationID,
		State:               "effective",
		DesiredGeneration:   draft.newGeneration,
		EffectiveGeneration: draft.newGeneration,
		Retryable:           false,
		SessionAction:       sessionAction,
		Settings:            settingsResponse,
	}
}

func recordAuthSettingsOperationInTransaction(ctx context.Context, tx pgx.Tx, request putAuthSettingsRequest, result authOperationResult, recordedAt time.Time) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
		resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
	) VALUES ('auth_settings', $1, $2, 'completed', $3, $4, $4)
	ON CONFLICT (resource_kind, operation_id) DO NOTHING`,
		request.OperationID, canonicalAuthRequestHash(request), raw, recordedAt)
	return err
}

// publishAuthSettingsPointerInTransaction is the final SQL phase. It keeps
// the additive legacy mirror path and final pointer-only path with their
// existing predicates; callers must not execute further SQL after this update.
func publishAuthSettingsPointerInTransaction(ctx context.Context, tx pgx.Tx, row authSettingsReadRow, request putAuthSettingsRequest, readiness proxyKeyReadinessSnapshot, input authSettingsMutationInput, draft authSettingsVersionDraft, legacyColumnsExist bool, readinessGeneration int64) error {
	activationGuardRequired := input.enabling && !input.zeroKeyAcknowledged
	if legacyColumnsExist {
		commandTag, err := tx.Exec(ctx, `UPDATE app_auth_settings SET
			auth_revision = $2,
			desired_config_version_id = $3, effective_config_version_id = $3,
			desired_generation = $4, effective_generation = $4,
			effective_auth_generation = $5,
			transition_operation_id = NULL, transition_kind = NULL, transition_state = NULL,
			auth_transition_state = NULL, auth_transition_operation_id = NULL,
			auth_transition_retry_after_at = NULL, auth_transition_attempts = 0,
			auth_enabled = $6, username = $7, password_hash = $8, token_version = $9,
			updated_at = $12
			WHERE id = $1 AND auth_revision = $2 - 1
			  AND (NOT $10 OR EXISTS (SELECT 1 FROM proxy_api_keys
				WHERE is_active = TRUE AND (expires_at IS NULL OR expires_at > clock_timestamp() + make_interval(secs => $11)))
			  AND (NOT $10 OR (SELECT generation FROM proxy_key_readiness_state WHERE id = 1) = $13))`,
			row.ID, row.Revision+1, draft.configID, draft.newGeneration, draft.newAuthGeneration, request.DesiredAuthEnabled, draft.username, draft.passwordHash, draft.sessionVersion, activationGuardRequired, authActivationSafetyHorizonSeconds, draft.commitAt, readinessGeneration)
		if err != nil {
			return err
		}
		if commandTag.RowsAffected() != 1 {
			if input.enabling {
				return readinessConflictProblem(readiness, "auth_readiness_changed", request.OperationID)
			}
			return authSettingsProblem(http.StatusConflict, "auth_settings_changed", "auth_settings_changed", map[string]any{
				"details": map[string]any{"current_revision": fmt.Sprintf("%d", row.Revision), "recovery": "refresh"},
			})
		}
		return nil
	}
	commandTag, err := tx.Exec(ctx, `UPDATE app_auth_settings SET
		auth_revision = $2,
		desired_config_version_id = $3, effective_config_version_id = $3,
		desired_generation = $4, effective_generation = $4,
		effective_auth_generation = $5,
		transition_operation_id = NULL, transition_kind = NULL, transition_state = NULL,
		auth_transition_state = NULL, auth_transition_operation_id = NULL,
		auth_transition_retry_after_at = NULL, auth_transition_attempts = 0,
		updated_at = $8
		WHERE id = $1 AND auth_revision = $2 - 1
		  AND (NOT $6 OR EXISTS (SELECT 1 FROM proxy_api_keys
			WHERE is_active = TRUE AND (expires_at IS NULL OR expires_at > clock_timestamp() + make_interval(secs => $7)))
		  AND (NOT $6 OR (SELECT generation FROM proxy_key_readiness_state WHERE id = 1) = $9))`,
		row.ID, row.Revision+1, draft.configID, draft.newGeneration, draft.newAuthGeneration, activationGuardRequired, authActivationSafetyHorizonSeconds, draft.commitAt, readinessGeneration)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		if input.enabling {
			return readinessConflictProblem(readiness, "auth_readiness_changed", request.OperationID)
		}
		return authSettingsProblem(http.StatusConflict, "auth_settings_changed", "auth_settings_changed", map[string]any{
			"details": map[string]any{"current_revision": fmt.Sprintf("%d", row.Revision), "recovery": "refresh"},
		})
	}
	return nil
}

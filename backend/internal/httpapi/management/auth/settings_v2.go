package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// v2 auth settings contract (Settings SPEC §8): immutable staged config
// versions, desired/effective pointers, Proxy-owned key readiness with a
// single counted_at instant and the fixed 30-second activation safety
// horizon, three conditional acknowledgements and durable operation recovery.

const (
	authActivationSafetyHorizonSeconds  = 30
	authActivationCommitDeadlineSeconds = 5
	authMaxUsernameLength               = 200
	authMaxPasswordLength               = 512
)

// putAuthSettingsRequestV2 mirrors the target PUT body (SPEC §8.2).
type putAuthSettingsRequestV2 struct {
	OperationID                         string  `json:"operation_id"`
	ExpectedRevision                    string  `json:"expected_revision"`
	ExpectedProxyKeyReadinessGeneration *string `json:"expected_proxy_key_readiness_generation"`
	DesiredAuthEnabled                  bool    `json:"desired_auth_enabled"`
	AccountChange                       struct {
		Kind        string  `json:"kind"`
		Username    *string `json:"username,omitempty"`
		NewPassword *string `json:"new_password,omitempty"`
	} `json:"account_change"`
	Acknowledgements struct {
		EnableWithoutActiveProxyKeys *bool `json:"enable_without_active_proxy_keys,omitempty"`
		DisableToPermissiveAccess    *bool `json:"disable_to_permissive_access,omitempty"`
		InvalidateOperatorSessions   *bool `json:"invalidate_operator_sessions,omitempty"`
	} `json:"acknowledgements"`
}

type authOperationResultV2 struct {
	OperationID         string `json:"operation_id"`
	State               string `json:"state"`
	DesiredGeneration   string `json:"desired_generation"`
	EffectiveGeneration string `json:"effective_generation"`
	Retryable           bool   `json:"retryable"`
	SafeError           *struct {
		Code              string `json:"code"`
		RetryAfterSeconds *int   `json:"retry_after_seconds"`
	} `json:"safe_error"`
	ReadinessConflict *authReadinessConflictResultV2 `json:"readiness_conflict"`
	SessionAction     string                         `json:"session_action"`
	Settings          map[string]any                 `json:"settings"`
}

type authReadinessConflictResultV2 struct {
	Code                     string               `json:"code"`
	CurrentProxyKeyReadiness proxyKeyReadinessDTO `json:"current_proxy_key_readiness"`
	RequiredAcknowledgements []string             `json:"required_acknowledgements"`
	NewOperationIDRequired   bool                 `json:"new_operation_id_required"`
}

type authReadinessConflictError struct {
	*domainError
	OperationID string
	Request     putAuthSettingsRequestV2
	Readiness   proxyKeyReadinessSnapshot
}

func (err *authReadinessConflictError) Unwrap() error { return err.domainError }

type authPutSettingsResponseV2 struct {
	OperationID        string         `json:"operation_id"`
	Replayed           bool           `json:"replayed"`
	EffectState        string         `json:"effect_state"`
	Settings           map[string]any `json:"settings"`
	SessionAction      string         `json:"session_action"`
	OperationStatusURL string         `json:"operation_status_url"`
}

// handlePutAuthSettingsV2: PUT /api/settings/auth (target contract). One
// transaction stages the immutable config version, rechecks Proxy readiness,
// validates acknowledgements and atomically flips the effective pointer with
// a final clock_timestamp() guard. The legacy credential columns are kept as
// a transitional mirror so existing login/session consumers stay consistent;
// the explicit finalizer removes them only after every consumer uses the
// pointer.
func (s *Service) handlePutAuthSettingsV2(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	var request putAuthSettingsRequestV2
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeAuthSettingsV2Problem(w, r, s.corsSnapshot(), http.StatusBadRequest, "validation_failed", "Invalid request body", map[string]any{"violations": []any{}})
		return
	}
	if strings.TrimSpace(request.OperationID) == "" {
		writeAuthSettingsV2Problem(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "validation_failed", "operation_id is required", map[string]any{
			"violations": []map[string]any{{"path": "operation_id", "reason": "required"}},
		})
		return
	}

	var result authOperationResultV2
	replayed := false
	activationCtx, cancel := context.WithTimeout(r.Context(), authActivationCommitDeadlineSeconds*time.Second)
	defer cancel()
	err := pgxutil.InTx(activationCtx, s.pool, "settings auth put", func(tx pgx.Tx) error {
		txCtx := activationCtx
		if err := auditdomain.AcquireAffectedWriterAdmission(txCtx, tx); err != nil {
			return authSettingsProblem(http.StatusServiceUnavailable, "auth_readiness_unavailable", "auth_readiness_unavailable", map[string]any{
				"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		// Proxy readiness is the first domain fence. Every auth staging and
		// activation path takes it before the auth control singleton, matching
		// the ordering used by proxy-key mutations.
		if err := lockProxyKeyReadiness(txCtx, tx); err != nil {
			return authSettingsProblem(http.StatusServiceUnavailable, "auth_readiness_unavailable", "auth_readiness_unavailable", map[string]any{
				"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		// Replay is checked before the current revision. A lost response must
		// remain replayable even after the successful operation advanced it.
		var operationRow struct {
			RequestHash string
			ResultJSON  []byte
		}
		err := tx.QueryRow(txCtx, `SELECT request_hash, result_json FROM settings_mutation_operations
			WHERE resource_kind = 'auth_settings' AND operation_id = $1`, request.OperationID).
			Scan(&operationRow.RequestHash, &operationRow.ResultJSON)
		if err == nil {
			hash := canonicalAuthRequestHash(request)
			if operationRow.RequestHash == hash {
				replayed = true
				if request.AccountChange.NewPassword != nil {
					var storedHash *string
					if err := tx.QueryRow(txCtx, `SELECT password_hash FROM auth_config_versions
							WHERE created_operation_id = $1 ORDER BY id DESC LIMIT 1`, request.OperationID).Scan(&storedHash); err != nil || storedHash == nil || !verifyPassword(*request.AccountChange.NewPassword, *storedHash) {
						return authSettingsProblem(http.StatusConflict, "operation_id_conflict", "operation_id_conflict", nil)
					}
				}
				if len(operationRow.ResultJSON) > 0 {
					return json.Unmarshal(operationRow.ResultJSON, &result)
				}
				return nil
			}
			return authSettingsProblem(http.StatusConflict, "operation_id_conflict", "operation_id_conflict", map[string]any{
				"details": map[string]any{"operation_id": request.OperationID, "recovery": "inspect_operation"},
			})
		}
		if err != pgx.ErrNoRows {
			return err
		}

		row, err := s.loadAuthSettingsV2(txCtx, tx, true)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", row.Revision) != request.ExpectedRevision {
			return authSettingsProblem(http.StatusConflict, "auth_settings_changed", "auth_settings_changed", map[string]any{
				"details": map[string]any{"current_revision": fmt.Sprintf("%d", row.Revision), "recovery": "refresh"},
			})
		}
		if row.TransitionState != nil {
			effectiveGeneration := "1"
			if row.EffectiveGeneration != nil {
				effectiveGeneration = *row.EffectiveGeneration
			}
			return authSettingsProblem(http.StatusConflict, "auth_transition_in_progress", "auth_transition_in_progress", map[string]any{
				"details": map[string]any{
					"transition_state":     *row.TransitionState,
					"effective_generation": effectiveGeneration,
					"recovery":             "inspect_existing_transition",
					"retry_after_seconds":  nil,
				},
			})
		}

		accountUpdate := request.AccountChange.Kind == "update"
		if request.AccountChange.Kind != "preserve" && request.AccountChange.Kind != "update" {
			return authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "account_change.kind", "reason": "unsupported"}}},
			})
		}
		enabling := request.DesiredAuthEnabled && !row.LegacyAuthEnabled
		disabling := !request.DesiredAuthEnabled && row.LegacyAuthEnabled
		readiness, readinessErr := s.captureProxyKeyReadiness(txCtx, tx)
		if readinessErr != nil {
			return authSettingsProblem(http.StatusServiceUnavailable, "auth_readiness_unavailable", "auth_readiness_unavailable", map[string]any{
				"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}

		// Acknowledgements.
		if disabling && !row.LegacyAuthEnabled && row.TransitionState == nil {
			// Disabling while already disabled is a no-op; still recorded.
		}
		disableAck := request.Acknowledgements.DisableToPermissiveAccess != nil && *request.Acknowledgements.DisableToPermissiveAccess
		sessionAck := request.Acknowledgements.InvalidateOperatorSessions != nil && *request.Acknowledgements.InvalidateOperatorSessions
		zeroKeyAck := request.Acknowledgements.EnableWithoutActiveProxyKeys != nil && *request.Acknowledgements.EnableWithoutActiveProxyKeys
		if request.Acknowledgements.EnableWithoutActiveProxyKeys != nil && !enabling {
			return authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid acknowledgement for this mutation", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "acknowledgements.enable_without_active_proxy_keys", "reason": "not_applicable"}}},
			})
		}
		if request.Acknowledgements.DisableToPermissiveAccess != nil && !disabling {
			return authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid acknowledgement for this mutation", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "acknowledgements.disable_to_permissive_access", "reason": "not_applicable"}}},
			})
		}
		if request.Acknowledgements.InvalidateOperatorSessions != nil && !(accountUpdate && row.LegacyAuthEnabled) {
			return authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid acknowledgement for this mutation", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "acknowledgements.invalidate_operator_sessions", "reason": "not_applicable"}}},
			})
		}

		// Account validation.
		var username *string
		var newPassword *string
		if accountUpdate {
			username = request.AccountChange.Username
			newPassword = request.AccountChange.NewPassword
			if username == nil || strings.TrimSpace(*username) == "" {
				return authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
					"details": map[string]any{"violations": []map[string]any{{"path": "account_change.username", "reason": "required"}}},
				})
			}
			if len(*username) > authMaxUsernameLength {
				return authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
					"details": map[string]any{"violations": []map[string]any{{"path": "account_change.username", "reason": "too_long", "limit": authMaxUsernameLength}}},
				})
			}
			normalized := strings.TrimSpace(*username)
			username = &normalized
			if newPassword != nil && (len(*newPassword) < 8 || len(*newPassword) > authMaxPasswordLength) {
				return authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
					"details": map[string]any{"violations": []map[string]any{{"path": "account_change.new_password", "reason": "length", "limit": authMaxPasswordLength}}},
				})
			}
			// An unconfigured account requires a password on update.
			hasExistingHash := row.LegacyPasswordHash != nil
			if !hasExistingHash && (newPassword == nil || strings.TrimSpace(*newPassword) == "") {
				return authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
					"details": map[string]any{"violations": []map[string]any{{"path": "account_change.new_password", "reason": "required"}}},
				})
			}
		}
		if accountUpdate && row.LegacyAuthEnabled && !sessionAck {
			return authSettingsProblem(http.StatusConflict, "auth_acknowledgement_required", "auth_acknowledgement_required", map[string]any{
				"details": map[string]any{
					"operation_id": request.OperationID, "operation_recorded": false,
					"new_operation_id_required": true,
					"required_acknowledgements": []string{"invalidate_operator_sessions"},
					"recovery":                  "review_and_resubmit",
				},
			})
		}

		// Enabling requires a ready operator account and Proxy readiness.
		if enabling {
			if username == nil {
				username = row.LegacyUsername
			}
			// Enable hard prerequisite: the resulting desired account must be
			// ready (username + password). An unconfigured account becomes ready
			// through the staged new_password of this same PUT.
			effectiveHash := row.LegacyPasswordHash
			if newPassword != nil && strings.TrimSpace(*newPassword) != "" {
				effectiveHash = newPassword
			}
			if effectiveHash == nil || username == nil || strings.TrimSpace(*username) == "" {
				return authSettingsProblem(http.StatusConflict, "auth_readiness_changed", "auth_readiness_changed", map[string]any{
					"details": map[string]any{"operation_id": request.OperationID, "operation_recorded": false, "new_operation_id_required": true, "recovery": "review_and_resubmit"},
				})
			}
			if request.ExpectedProxyKeyReadinessGeneration == nil ||
				*request.ExpectedProxyKeyReadinessGeneration != readiness.Generation {
				return readinessConflictProblem(readiness, "auth_readiness_changed", request.OperationID)
			}
			if readiness.SafeActive == 0 && !zeroKeyAck {
				return readinessConflictProblem(readiness, "auth_acknowledgement_required", request.OperationID)
			}
		}
		if disabling && !disableAck {
			return authSettingsProblem(http.StatusConflict, "auth_acknowledgement_required", "auth_acknowledgement_required", map[string]any{
				"details": map[string]any{
					"operation_id": request.OperationID, "operation_recorded": false,
					"new_operation_id_required": true,
					"required_acknowledgements": []string{"disable_to_permissive_access"},
					"recovery":                  "review_and_resubmit",
				},
			})
		}
		if request.ExpectedProxyKeyReadinessGeneration != nil && !enabling {
			return authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid readiness generation for this mutation", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "expected_proxy_key_readiness_generation", "reason": "must_be_null_unless_enabling"}}},
			})
		}

		// Stage the immutable config version.
		newAuthGeneration := nextGenerationInt(row.EffectiveGeneration)
		newGeneration := fmt.Sprintf("%d", newAuthGeneration)
		sessionVersion := row.LegacyTokenVersion
		if accountUpdate || disabling && row.LegacyAuthEnabled || enabling && !row.LegacyAuthEnabled {
			sessionVersion++
		}
		var passwordHash *string
		if newPassword != nil && strings.TrimSpace(*newPassword) != "" {
			hash, err := hashPassword(*newPassword)
			if err != nil {
				return err
			}
			passwordHash = &hash
		} else if accountUpdate {
			passwordHash = row.LegacyPasswordHash
		} else {
			passwordHash = row.LegacyPasswordHash
		}
		if username == nil {
			username = row.LegacyUsername
		}
		desiredMode := "disabled"
		if request.DesiredAuthEnabled {
			desiredMode = "enabled"
		}
		var configID int64
		if err := tx.QueryRow(txCtx, `INSERT INTO auth_config_versions (
			subject_key, generation, desired_mode, username, password_hash, session_version,
			state, created_operation_id, readiness_generation, counted_at, active_count,
			expired_count, disabled_count, safe_active_count, zero_key_acknowledged,
			created_at, updated_at
		) VALUES ('app', $1, $2, $3, $4, $5, 'staged', $6, $7, $8, $9, $10, $11, $12, $13, now(), now())
		RETURNING id`, newGeneration, desiredMode, username, passwordHash, sessionVersion,
			request.OperationID,
			func() *string {
				if readiness.Generation == "" {
					return nil
				}
				return &readiness.Generation
			}(),
			func() *time.Time {
				if readiness.CountedAt.IsZero() {
					return nil
				}
				return &readiness.CountedAt
			}(),
			func() *string {
				if readiness.Generation == "" {
					return nil
				}
				value := fmt.Sprintf("%d", readiness.Active)
				return &value
			}(),
			func() *string {
				if readiness.Generation == "" {
					return nil
				}
				value := fmt.Sprintf("%d", readiness.Expired)
				return &value
			}(),
			func() *string {
				if readiness.Generation == "" {
					return nil
				}
				value := fmt.Sprintf("%d", readiness.Disabled)
				return &value
			}(),
			func() *string {
				if readiness.Generation == "" {
					return nil
				}
				value := fmt.Sprintf("%d", readiness.SafeActive)
				return &value
			}(),
			zeroKeyAck).Scan(&configID); err != nil {
			return err
		}
		commitAt := s.nowUTC()
		if _, err := tx.Exec(txCtx, `UPDATE auth_config_versions SET state = 'effective', updated_at = $2 WHERE id = $1`, configID, commitAt); err != nil {
			return err
		}

		newRevision := row.Revision + 1
		if row.EffectiveConfigID != nil && *row.EffectiveConfigID != configID {
			if _, err := tx.Exec(txCtx, `UPDATE auth_config_versions SET state = 'superseded', updated_at = $2
				WHERE id = $1 AND state = 'effective'`, *row.EffectiveConfigID, commitAt); err != nil {
				return err
			}
		}
		// Session invalidation is committed before effective publication. A
		// failed final pointer guard rolls the entire transaction back.
		if (disabling && row.LegacyAuthEnabled) || (accountUpdate && row.LegacyAuthEnabled) {
			if _, err := tx.Exec(txCtx, `UPDATE refresh_tokens SET revoked_at = $2
				WHERE auth_subject_id = $1 AND revoked_at IS NULL`, row.ID, commitAt); err != nil {
				return err
			}
		}
		// The transitional in-place credential columns exist only while the
		// schema is additive; the finalizer drops them once every verifier
		// consumes the pointer. Mirror writes are skipped on the final schema.
		var legacyColumnsExist bool
		if err := tx.QueryRow(txCtx, `SELECT COUNT(*) > 0 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'app_auth_settings' AND column_name = 'auth_enabled'`).Scan(&legacyColumnsExist); err != nil {
			return err
		}
		activationGuardRequired := enabling && !zeroKeyAck
		config := authConfigVersionRow{
			ID: configID, SubjectKey: "app", Generation: newGeneration,
			DesiredMode: desiredMode, Username: username, PasswordHash: passwordHash,
			SessionVersion: int64(sessionVersion), State: "effective",
			CreatedAt: commitAt, UpdatedAt: commitAt, ZeroKeyAck: zeroKeyAck,
		}
		effectiveRow := row
		configIDCopy := configID
		generationCopy := newGeneration
		effectiveRow.Revision = newRevision
		effectiveRow.DesiredConfigID = &configIDCopy
		effectiveRow.EffectiveConfigID = &configIDCopy
		effectiveRow.DesiredGeneration = &generationCopy
		effectiveRow.EffectiveGeneration = &generationCopy
		effectiveRow.DesiredAuthEnabled = request.DesiredAuthEnabled
		effectiveRow.DesiredUsername = username
		effectiveRow.DesiredPasswordHash = passwordHash
		effectiveRow.LegacyAuthEnabled = request.DesiredAuthEnabled
		effectiveRow.LegacyUsername = username
		effectiveRow.LegacyPasswordHash = passwordHash
		effectiveRow.LegacyTokenVersion = int64(sessionVersion)
		effectiveRow.UpdatedAt = commitAt
		effectiveRow.TransitionOperationID = nil
		effectiveRow.TransitionKind = nil
		effectiveRow.TransitionState = nil
		settingsResponse := buildAuthSettingsResponseV2FromState(effectiveRow, &config, readiness, false)
		sessionAction := "none"
		if enabling || (accountUpdate && row.LegacyAuthEnabled) {
			sessionAction = "clear_and_login"
		} else if disabling && row.LegacyAuthEnabled {
			sessionAction = "clear_and_continue"
		}
		result = authOperationResultV2{
			OperationID:         request.OperationID,
			State:               "effective",
			DesiredGeneration:   newGeneration,
			EffectiveGeneration: newGeneration,
			Retryable:           false,
			SessionAction:       sessionAction,
			Settings:            settingsResponse,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(txCtx, `INSERT INTO settings_mutation_operations (
			resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
		) VALUES ('auth_settings', $1, $2, 'completed', $3, $4, $4)
		ON CONFLICT (resource_kind, operation_id) DO NOTHING`,
			request.OperationID, canonicalAuthRequestHash(request), raw, commitAt); err != nil {
			return err
		}
		var readinessGeneration int64
		if enabling {
			if _, scanErr := fmt.Sscanf(readiness.Generation, "%d", &readinessGeneration); scanErr != nil {
				return fmt.Errorf("invalid proxy key readiness generation: %w", scanErr)
			}
		}
		if legacyColumnsExist {
			commandTag, err := tx.Exec(txCtx, `UPDATE app_auth_settings SET
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
				row.ID, newRevision, configID, newGeneration, newAuthGeneration, request.DesiredAuthEnabled, username, passwordHash, sessionVersion, activationGuardRequired, authActivationSafetyHorizonSeconds, commitAt, readinessGeneration)
			if err != nil {
				return err
			}
			if commandTag.RowsAffected() != 1 {
				if enabling {
					return readinessConflictProblem(readiness, "auth_readiness_changed", request.OperationID)
				}
				return authSettingsProblem(http.StatusConflict, "auth_settings_changed", "auth_settings_changed", map[string]any{
					"details": map[string]any{"current_revision": fmt.Sprintf("%d", row.Revision), "recovery": "refresh"},
				})
			}
		} else {
			commandTag, err := tx.Exec(txCtx, `UPDATE app_auth_settings SET
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
				row.ID, newRevision, configID, newGeneration, newAuthGeneration, activationGuardRequired, authActivationSafetyHorizonSeconds, commitAt, readinessGeneration)
			if err != nil {
				return err
			}
			if commandTag.RowsAffected() != 1 {
				if enabling {
					return readinessConflictProblem(readiness, "auth_readiness_changed", request.OperationID)
				}
				return authSettingsProblem(http.StatusConflict, "auth_settings_changed", "auth_settings_changed", map[string]any{
					"details": map[string]any{"current_revision": fmt.Sprintf("%d", row.Revision), "recovery": "refresh"},
				})
			}
		}
		// The conditional pointer publication above is the final database
		// statement on the successful path. The transaction commit below is the
		// linearization point for the new immutable auth generation.
		return nil
	})
	if err != nil {
		var readinessConflict *authReadinessConflictError
		if errors.As(err, &readinessConflict) {
			readinessConflict.Request = request
			if recordErr := s.recordAuthReadinessConflict(activationCtx, readinessConflict); recordErr != nil {
				// Never emit a readiness conflict claiming durable recovery when
				// the outcome record itself could not be committed. The safe
				// fallback is a bounded unavailable response; the caller may
				// re-read and submit a new operation identity.
				writeAuthSettingsV2Problem(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "auth_settings_unavailable", "Failed to record authentication readiness outcome", map[string]any{
					"recovery":            "retry",
					"retry_after_seconds": 5,
				})
				return
			}
			if details, ok := readinessConflict.Fields["details"].(map[string]any); ok {
				details["operation_recorded"] = true
			}
		}
		var authErr *domainError
		if errors.As(err, &authErr) {
			writeDomainError(w, r, s.corsSnapshot(), err)
		} else {
			slog.Error("authentication settings mutation failed", "error", err)
			writeAuthSettingsV2Problem(w, r, s.corsSnapshot(), http.StatusInternalServerError, "auth_settings_unavailable", "Failed to apply authentication settings", nil)
		}
		return
	}
	// The database pointer is authoritative only after commit. Invalidate the
	// management snapshot and runtime decision cache at that boundary so a
	// successful generation cannot be shadowed by a browser/process-local
	// stale auth mode.
	s.invalidateAppAuthSettingsSnapshot()
	s.InvalidateRuntimeCache()
	if result.SessionAction == "clear_and_login" || result.SessionAction == "clear_and_continue" {
		// Cookie mutation happens only after the transaction commits. This
		// prevents a rolled-back write from invalidating a still-valid browser
		// session and makes replay behavior deterministic.
		s.clearAuthCookies(w, s.runtimeAuthConfigSnapshot())
	}
	responseutil.WriteJSON(w, http.StatusOK, authPutSettingsResponseV2{
		OperationID:        result.OperationID,
		Replayed:           replayed,
		EffectState:        result.State,
		Settings:           result.Settings,
		SessionAction:      result.SessionAction,
		OperationStatusURL: "/api/auth/operations/" + result.OperationID + "/status",
	})
}

func (s *Service) recordAuthReadinessConflict(ctx context.Context, conflict *authReadinessConflictError) error {
	return pgxutil.InTx(ctx, s.pool, "settings auth readiness conflict", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(ctx, tx); err != nil {
			return err
		}
		// Keep the same Proxy -> Auth fence order as successful activation and
		// proxy-key mutations. This outcome is written after the failed
		// activation transaction rolled back, but it must still serialize with a
		// concurrent readiness refresh before publishing its recovery snapshot.
		if err := lockProxyKeyReadiness(ctx, tx); err != nil {
			return err
		}
		row, err := s.loadAuthSettingsV2(ctx, tx, true)
		if err != nil {
			return err
		}
		// Keep a deliberately slow verifier for a secret-bearing conflict so
		// an exact replay can prove the same password without persisting the
		// raw secret or a cheap request digest. The version is unusable and is
		// never pointed to by app_auth_settings.
		if conflict.Request.AccountChange.NewPassword != nil {
			hash, hashErr := hashPassword(*conflict.Request.AccountChange.NewPassword)
			if hashErr != nil {
				return hashErr
			}
			username := conflict.Request.AccountChange.Username
			if username == nil {
				username = row.DesiredUsername
			}
			generation := "unusable-" + conflict.OperationID
			if _, err := tx.Exec(ctx, `INSERT INTO auth_config_versions (
				subject_key, generation, desired_mode, username, password_hash, session_version,
				state, created_operation_id, created_at, updated_at
			) VALUES ('app', $1, $2, $3, $4, $5, 'unusable', $6, now(), now())
			ON CONFLICT (subject_key, generation) DO NOTHING`, generation,
				func() string {
					if row.DesiredAuthEnabled {
						return "enabled"
					}
					return "disabled"
				}(),
				username, hash, row.LegacyTokenVersion, conflict.OperationID); err != nil {
				return err
			}
		}
		settingsResponse, err := s.buildAuthSettingsResponseV2(ctx, tx)
		if err != nil {
			return err
		}
		desiredGeneration := "1"
		if row.DesiredGeneration != nil {
			desiredGeneration = *row.DesiredGeneration
		}
		effectiveGeneration := "1"
		if row.EffectiveGeneration != nil {
			effectiveGeneration = *row.EffectiveGeneration
		}
		result := authOperationResultV2{
			OperationID:         conflict.OperationID,
			State:               "rolled_back",
			DesiredGeneration:   desiredGeneration,
			EffectiveGeneration: effectiveGeneration,
			Retryable:           false,
			SafeError: &struct {
				Code              string `json:"code"`
				RetryAfterSeconds *int   `json:"retry_after_seconds"`
			}{Code: conflict.domainError.Code, RetryAfterSeconds: nil},
			ReadinessConflict: &authReadinessConflictResultV2{
				Code:                     conflict.domainError.Code,
				CurrentProxyKeyReadiness: readinessDTO(conflict.Readiness),
				RequiredAcknowledgements: []string{"enable_without_active_proxy_keys"},
				NewOperationIDRequired:   true,
			},
			SessionAction: "none",
			Settings:      settingsResponse,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
			resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
		) VALUES ('auth_settings', $1, $2, 'rolled_back', $3, now(), now())
		ON CONFLICT (resource_kind, operation_id) DO NOTHING`, conflict.OperationID, canonicalAuthRequestHash(conflict.Request), raw)
		return err
	})
}

func readinessConflictProblem(readiness proxyKeyReadinessSnapshot, code string, operationID string) error {
	details := map[string]any{
		"operation_id":                operationID,
		"operation_recorded":          false,
		"new_operation_id_required":   true,
		"required_acknowledgements":   []string{"enable_without_active_proxy_keys"},
		"current_proxy_key_readiness": readinessDTO(readiness),
		"recovery":                    "review_and_resubmit",
	}
	return &authReadinessConflictError{
		domainError: authSettingsProblem(http.StatusConflict, code, code, map[string]any{"details": details}).(*domainError),
		OperationID: operationID,
		Readiness:   readiness,
	}
}

func canonicalAuthRequestHash(request putAuthSettingsRequestV2) string {
	account := request.AccountChange.Kind
	username := ""
	if request.AccountChange.Username != nil {
		username = *request.AccountChange.Username
	}
	passwordDiscriminator := "absent"
	if request.AccountChange.NewPassword != nil {
		passwordDiscriminator = "present"
	}
	ack := fmt.Sprintf("%v:%v:%v",
		boolValue(request.Acknowledgements.EnableWithoutActiveProxyKeys),
		boolValue(request.Acknowledgements.DisableToPermissiveAccess),
		boolValue(request.Acknowledgements.InvalidateOperatorSessions))
	readinessGeneration := "null"
	if request.ExpectedProxyKeyReadinessGeneration != nil {
		readinessGeneration = *request.ExpectedProxyKeyReadinessGeneration
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%v|%s|%s|%s",
		request.OperationID, request.ExpectedRevision, request.DesiredAuthEnabled, account, username,
		passwordDiscriminator+"|"+readinessGeneration+"|"+ack)))
	return hex.EncodeToString(sum[:])
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func nextGenerationInt(current *string) int64 {
	var number int64
	if current != nil {
		if _, err := fmt.Sscanf(*current, "%d", &number); err == nil {
			return number + 1
		}
	}
	// Non-numeric legacy generation (e.g. test seeds): fall back to a
	// monotonically increasing timestamp so the generation stays unique and
	// strictly increasing.
	return time.Now().UTC().UnixNano()
}

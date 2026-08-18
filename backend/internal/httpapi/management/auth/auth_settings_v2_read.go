package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

type authConfigVersionRow struct {
	ID             int64
	SubjectKey     string
	Generation     string
	DesiredMode    string
	Username       *string
	PasswordHash   *string
	SessionVersion int64
	State          string
	ZeroKeyAck     bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type authSettingsV2Row struct {
	ID                    int64
	Revision              int64
	DesiredConfigID       *int64
	EffectiveConfigID     *int64
	DesiredGeneration     *string
	EffectiveGeneration   *string
	TransitionOperationID *string
	TransitionKind        *string
	TransitionState       *string
	DesiredAuthEnabled    bool
	DesiredUsername       *string
	DesiredPasswordHash   *string
	LegacyAuthEnabled     bool
	LegacyUsername        *string
	LegacyPasswordHash    *string
	LegacyTokenVersion    int64
	UpdatedAt             time.Time
}

func (s *Service) loadAuthSettingsV2(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, forUpdate bool) (authSettingsV2Row, error) {
	// The transitional in-place credential columns exist only while the schema
	// is additive; the finalizer drops them once every verifier consumes the
	// pointer, so the row projection follows the schema.
	var legacyColumnsExist bool
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) > 0 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'app_auth_settings' AND column_name = 'auth_enabled'`).Scan(&legacyColumnsExist); err != nil {
		return authSettingsV2Row{}, err
	}
	if legacyColumnsExist {
		return s.loadAuthSettingsV2Legacy(ctx, exec, forUpdate)
	}
	return s.loadAuthSettingsV2Pointer(ctx, exec, forUpdate)
}

func (s *Service) loadAuthSettingsV2Pointer(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, forUpdate bool) (authSettingsV2Row, error) {
	query := `SELECT a.id, a.auth_revision, a.desired_config_version_id, a.effective_config_version_id,
		a.desired_generation, a.effective_generation, a.transition_operation_id, a.transition_kind, a.transition_state,
		v.desired_mode, v.username, v.password_hash, v.session_version, a.updated_at
		FROM app_auth_settings AS a
		LEFT JOIN auth_config_versions AS v ON v.id = a.effective_config_version_id
		WHERE a.singleton_key = 'app'`
	if forUpdate {
		query += ` FOR UPDATE OF a`
	}
	query += ` LIMIT 1`
	var row authSettingsV2Row
	var desiredID, effectiveID *int64
	var desiredGen, effectiveGen, transitionOp, transitionKind, transitionState *string
	var configMode, configUsername, configPasswordHash *string
	var configSessionVersion *int64
	if err := exec.QueryRow(ctx, query).Scan(&row.ID, &row.Revision, &desiredID, &effectiveID,
		&desiredGen, &effectiveGen, &transitionOp, &transitionKind, &transitionState,
		&configMode, &configUsername, &configPasswordHash, &configSessionVersion, &row.UpdatedAt); err != nil {
		return authSettingsV2Row{}, err
	}
	row.DesiredConfigID = desiredID
	row.EffectiveConfigID = effectiveID
	row.DesiredGeneration = desiredGen
	row.EffectiveGeneration = effectiveGen
	row.TransitionOperationID = transitionOp
	row.TransitionKind = transitionKind
	row.TransitionState = transitionState
	if configMode != nil {
		row.LegacyAuthEnabled = *configMode == "enabled"
	}
	if configUsername != nil {
		username := *configUsername
		row.LegacyUsername = &username
	}
	if configPasswordHash != nil {
		hash := *configPasswordHash
		row.LegacyPasswordHash = &hash
	}
	if configSessionVersion != nil {
		row.LegacyTokenVersion = *configSessionVersion
	}
	if desiredID != nil {
		var desiredMode string
		var desiredUsername, desiredPasswordHash *string
		if err := exec.QueryRow(ctx, `SELECT desired_mode, username, password_hash
			FROM auth_config_versions WHERE id = $1`, *desiredID).Scan(&desiredMode, &desiredUsername, &desiredPasswordHash); err != nil {
			return authSettingsV2Row{}, err
		}
		row.DesiredAuthEnabled = desiredMode == "enabled"
		row.DesiredUsername = desiredUsername
		row.DesiredPasswordHash = desiredPasswordHash
	}
	return row, nil
}

func (s *Service) loadAuthSettingsV2Legacy(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, forUpdate bool) (authSettingsV2Row, error) {
	query := `SELECT id, auth_revision, desired_config_version_id, effective_config_version_id,
		desired_generation, effective_generation, transition_operation_id, transition_kind, transition_state,
		auth_enabled, username, password_hash, token_version, updated_at
		FROM app_auth_settings WHERE singleton_key = 'app'`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	var row authSettingsV2Row
	var desiredID, effectiveID *int64
	var desiredGen, effectiveGen, transitionOp, transitionKind, transitionState *string
	var legacyUsername, legacyHash *string
	var legacyEnabled bool
	var legacyTokenVersion int64
	if err := exec.QueryRow(ctx, query).Scan(&row.ID, &row.Revision, &desiredID, &effectiveID,
		&desiredGen, &effectiveGen, &transitionOp, &transitionKind, &transitionState,
		&legacyEnabled, &legacyUsername, &legacyHash, &legacyTokenVersion, &row.UpdatedAt); err != nil {
		return authSettingsV2Row{}, err
	}
	row.DesiredConfigID = desiredID
	row.EffectiveConfigID = effectiveID
	row.DesiredGeneration = desiredGen
	row.EffectiveGeneration = effectiveGen
	row.TransitionOperationID = transitionOp
	row.TransitionKind = transitionKind
	row.TransitionState = transitionState
	row.LegacyAuthEnabled = legacyEnabled
	row.LegacyUsername = legacyUsername
	row.LegacyPasswordHash = legacyHash
	row.LegacyTokenVersion = legacyTokenVersion
	row.DesiredAuthEnabled = legacyEnabled
	row.DesiredUsername = legacyUsername
	row.DesiredPasswordHash = legacyHash
	return row, nil
}

func (s *Service) loadConfigVersion(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64) (authConfigVersionRow, error) {
	var row authConfigVersionRow
	var username, passwordHash *string
	if err := exec.QueryRow(ctx, `SELECT id, subject_key, generation, desired_mode, username,
		password_hash, session_version, state, zero_key_acknowledged, created_at, updated_at
		FROM auth_config_versions WHERE id = $1`, id).Scan(
		&row.ID, &row.SubjectKey, &row.Generation, &row.DesiredMode, &username,
		&passwordHash, &row.SessionVersion, &row.State, &row.ZeroKeyAck, &row.CreatedAt, &row.UpdatedAt); err != nil {
		return authConfigVersionRow{}, err
	}
	row.Username = username
	row.PasswordHash = passwordHash
	return row, nil
}

type proxyKeyReadinessDTO struct {
	State               string  `json:"state"`
	ReadinessGeneration string  `json:"readiness_generation,omitempty"`
	CountedAt           string  `json:"counted_at,omitempty"`
	Active              string  `json:"active,omitempty"`
	Expired             string  `json:"expired,omitempty"`
	Disabled            string  `json:"disabled,omitempty"`
	ActivationGuard     *any    `json:"activation_guard,omitempty"`
	ReasonCode          *string `json:"reason_code,omitempty"`
	RetryAfterSeconds   *int    `json:"retry_after_seconds,omitempty"`
	LastReadyGeneration *string `json:"last_ready_generation,omitempty"`
}

func readinessDTO(snapshot proxyKeyReadinessSnapshot) proxyKeyReadinessDTO {
	guard := any(map[string]any{
		"safety_horizon_seconds": authActivationSafetyHorizonSeconds,
		"safe_active":            fmt.Sprintf("%d", snapshot.SafeActive),
	})
	return proxyKeyReadinessDTO{
		State:               "ready",
		ReadinessGeneration: snapshot.Generation,
		CountedAt:           snapshot.CountedAt.UTC().Format(time.RFC3339),
		Active:              fmt.Sprintf("%d", snapshot.Active),
		Expired:             fmt.Sprintf("%d", snapshot.Expired),
		Disabled:            fmt.Sprintf("%d", snapshot.Disabled),
		ActivationGuard:     &guard,
	}
}

func readinessUnavailableDTO(snapshot proxyKeyReadinessSnapshot) proxyKeyReadinessDTO {
	var lastReady *string
	if snapshot.LastReadyGeneration != "" {
		lastReady = &snapshot.LastReadyGeneration
	}
	retryAfter := 5
	return proxyKeyReadinessDTO{
		State:               "unavailable",
		ReasonCode:          stringPtrV2("storage_unavailable"),
		RetryAfterSeconds:   &retryAfter,
		LastReadyGeneration: lastReady,
	}
}

// buildAuthSettingsResponse assembles the target AuthSettingsResponse.
func (s *Service) buildAuthSettingsResponseV2(ctx context.Context, tx pgx.Tx) (map[string]any, error) {
	row, err := s.loadAuthSettingsV2(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	readiness, err := s.captureProxyKeyReadiness(ctx, tx)
	readinessUnavailable := err != nil
	var config *authConfigVersionRow
	if row.EffectiveConfigID != nil {
		loaded, loadErr := s.loadConfigVersion(ctx, tx, *row.EffectiveConfigID)
		if loadErr != nil {
			return nil, loadErr
		}
		config = &loaded
	}
	return buildAuthSettingsResponseV2FromState(row, config, readiness, readinessUnavailable), nil
}

// buildAuthSettingsResponseV2FromState is intentionally query-free. Auth
// activation uses it before the final effective-pointer UPDATE so that the
// pointer flip remains the final database statement in the transaction.
func buildAuthSettingsResponseV2FromState(row authSettingsV2Row, config *authConfigVersionRow, readiness proxyKeyReadinessSnapshot, readinessUnavailable bool) map[string]any {
	accessState := "enabled"
	effectiveMode := "enabled"
	desiredMode := "disabled"
	if row.DesiredAuthEnabled {
		desiredMode = "enabled"
	}
	desiredGeneration := "1"
	effectiveGeneration := "1"
	if row.EffectiveGeneration != nil {
		effectiveGeneration = *row.EffectiveGeneration
	}
	if row.DesiredGeneration != nil {
		desiredGeneration = *row.DesiredGeneration
	}
	if !row.LegacyAuthEnabled {
		effectiveMode = "disabled"
		accessState = "disabled"
	}
	if row.TransitionState != nil {
		switch *row.TransitionState {
		case "staged", "publishing", "retrying":
			if row.TransitionKind != nil && *row.TransitionKind == "disable" {
				accessState = "disabling_enforced"
			} else if row.TransitionKind != nil && *row.TransitionKind == "account_update" {
				if effectiveMode == "enabled" {
					accessState = "account_transition_enabled"
				} else {
					accessState = "account_transition_disabled"
				}
			} else {
				accessState = "enabling_fail_closed"
			}
		case "rollback_required":
			accessState = "rollback_required"
		}
	}

	// Effective operator account from the effective config version.
	accountState := "unconfigured"
	var username *string
	hasPassword := false
	sessionVersion := "0"
	var updatedAt *string
	if config != nil {
		username = config.Username
		hasPassword = config.PasswordHash != nil
		sessionVersion = fmt.Sprintf("%d", config.SessionVersion)
		updatedAt = stringPtrV2(config.UpdatedAt.UTC().Format(time.RFC3339))
		if config.DesiredMode != "" {
			if config.DesiredMode == "enabled" && (config.Username == nil || config.PasswordHash == nil) {
				accountState = "repair_required"
			} else if config.Username != nil {
				accountState = "ready"
			}
		}
	}

	readinessDTOValue := readinessDTO(readiness)
	if readinessUnavailable {
		readinessDTOValue = readinessUnavailableDTO(readiness)
	}
	response := map[string]any{
		"revision": fmt.Sprintf("%d", row.Revision),
		"auth_mode": map[string]any{
			"desired":              desiredMode,
			"effective":            effectiveMode,
			"access_state":         accessState,
			"desired_generation":   desiredGeneration,
			"effective_generation": effectiveGeneration,
		},
		"operator_account": map[string]any{
			"effective": map[string]any{
				"state":           accountState,
				"username":        username,
				"has_password":    hasPassword,
				"session_version": sessionVersion,
				"updated_at":      updatedAt,
			},
			"desired": map[string]any{
				"state": func() string {
					if row.DesiredUsername == nil && row.DesiredPasswordHash == nil {
						return "unconfigured"
					}
					if row.DesiredUsername == nil || row.DesiredPasswordHash == nil {
						return "repair_required"
					}
					return "ready"
				}(),
				"username":     row.DesiredUsername,
				"has_password": row.DesiredPasswordHash != nil,
			},
		},
		"transition":                     nil,
		"proxy_key_readiness":            readinessDTOValue,
		"attribution_mode_when_disabled": "permissive",
		"updated_at":                     row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.TransitionState != nil && row.TransitionOperationID != nil {
		response["transition"] = map[string]any{
			"operation_id":    *row.TransitionOperationID,
			"kind":            row.TransitionKind,
			"state":           *row.TransitionState,
			"retryable":       *row.TransitionState == "retrying",
			"last_safe_error": nil,
		}
	}
	return response
}

func stringPtrV2(value string) *string { return &value }

// handleGetAuthSettingsV2: GET /api/settings/auth (target contract).
func (s *Service) handleGetAuthSettingsV2(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings auth read", func(tx pgx.Tx) (map[string]any, error) {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return nil, authSettingsProblem(http.StatusServiceUnavailable, "auth_readiness_unavailable", "auth_readiness_unavailable", map[string]any{
				"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		return s.buildAuthSettingsResponseV2(r.Context(), tx)
	})
	if err != nil {
		var authErr *domainError
		if errors.As(err, &authErr) {
			writeDomainError(w, r, s.corsSnapshot(), err)
		} else {
			writeAuthSettingsV2Problem(w, r, s.corsSnapshot(), http.StatusInternalServerError, "auth_settings_unavailable", "Failed to load authentication settings", nil)
		}
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleGetPublicAuthStatusV2: GET /api/auth/status strict union (SPEC §8.2).
func (s *Service) handleGetPublicAuthStatusV2(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	row, err := s.loadAuthSettingsV2(r.Context(), s.pool, false)
	if err != nil {
		writeAuthSettingsV2Problem(w, r, s.corsSnapshot(), http.StatusInternalServerError, "auth_settings_unavailable", "Failed to load authentication settings", nil)
		return
	}
	effectiveGeneration := "1"
	if row.EffectiveGeneration != nil {
		effectiveGeneration = *row.EffectiveGeneration
	}
	response := map[string]any{
		"state":                "disabled",
		"transition_state":     nil,
		"effective_generation": effectiveGeneration,
		"login_available":      false,
		"retry_after_seconds":  nil,
	}
	if row.LegacyAuthEnabled {
		response["state"] = "enabled"
		response["login_available"] = true
	}
	if row.TransitionState != nil {
		switch *row.TransitionState {
		case "rollback_required":
			response["state"] = "transition_fail_closed"
			response["transition_state"] = "rollback_required"
			response["login_available"] = false
		case "staged", "publishing", "retrying":
			if row.TransitionKind != nil && *row.TransitionKind == "disable" {
				response["state"] = "enabled"
				response["transition_state"] = "disabling_enforced"
				response["login_available"] = true
			} else {
				response["state"] = "transition_fail_closed"
				response["transition_state"] = "enabling_fail_closed"
				response["login_available"] = false
			}
		}
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

package auth

import (
	"context"
	"net/http"
	"time"
)

// Auth settings contract (Settings SPEC §8): immutable staged config
// versions, desired/effective pointers, Proxy-owned key readiness with a
// single counted_at instant and the fixed 30-second activation safety
// horizon, three conditional acknowledgements and durable operation recovery.

const (
	authActivationSafetyHorizonSeconds  = 30
	authActivationCommitDeadlineSeconds = 5
	authMaxUsernameLength               = 200
	authMaxPasswordLength               = 512
)

// putAuthSettingsRequest mirrors the target PUT body (SPEC §8.2).
type putAuthSettingsRequest struct {
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

type authOperationResult struct {
	OperationID         string `json:"operation_id"`
	State               string `json:"state"`
	DesiredGeneration   string `json:"desired_generation"`
	EffectiveGeneration string `json:"effective_generation"`
	Retryable           bool   `json:"retryable"`
	SafeError           *struct {
		Code              string `json:"code"`
		RetryAfterSeconds *int   `json:"retry_after_seconds"`
	} `json:"safe_error"`
	ReadinessConflict *authReadinessConflictResult `json:"readiness_conflict"`
	SessionAction     string                       `json:"session_action"`
	Settings          map[string]any               `json:"settings"`
}

type authReadinessConflictResult struct {
	Code                     string               `json:"code"`
	CurrentProxyKeyReadiness proxyKeyReadinessDTO `json:"current_proxy_key_readiness"`
	RequiredAcknowledgements []string             `json:"required_acknowledgements"`
	NewOperationIDRequired   bool                 `json:"new_operation_id_required"`
}

type authReadinessConflictError struct {
	*domainError
	OperationID string
	Request     putAuthSettingsRequest
	Readiness   proxyKeyReadinessSnapshot
}

func (err *authReadinessConflictError) Unwrap() error { return err.domainError }

type authPutSettingsResponse struct {
	OperationID        string         `json:"operation_id"`
	Replayed           bool           `json:"replayed"`
	EffectState        string         `json:"effect_state"`
	Settings           map[string]any `json:"settings"`
	SessionAction      string         `json:"session_action"`
	OperationStatusURL string         `json:"operation_status_url"`
}

type authSettingsMutationOutcome struct {
	result   authOperationResult
	replayed bool
}

// handlePutAuthSettings is the thin HTTP orchestration for PUT
// /api/settings/auth. Request decoding, the bounded mutation transaction,
// failure recovery, and post-commit publication each have their own owner.
func (s *Service) handlePutAuthSettings(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	request, ok := s.decodePutAuthSettingsRequest(w, r)
	if !ok {
		return
	}

	activationCtx, cancel := context.WithTimeout(r.Context(), authActivationCommitDeadlineSeconds*time.Second)
	defer cancel()
	outcome, err := s.executeAuthSettingsMutation(activationCtx, request)
	if err != nil {
		s.writeAuthSettingsMutationFailure(w, r, activationCtx, request, err)
		return
	}
	s.publishAuthSettingsMutation(w, outcome)
}

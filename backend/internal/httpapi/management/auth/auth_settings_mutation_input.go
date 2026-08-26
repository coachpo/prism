package auth

import (
	"net/http"
	"strings"
)

// authSettingsMutationInput is the frozen result of readiness-aware request
// validation. It contains normalized account values and the decisions used by
// the one transaction, while the original request remains available for its
// exact operation hash.
type authSettingsMutationInput struct {
	accountUpdate       bool
	enabling            bool
	disabling           bool
	disableAcknowledged bool
	sessionAcknowledged bool
	zeroKeyAcknowledged bool
	username            *string
	newPassword         *string
}

func validateAuthSettingsMutationKind(request putAuthSettingsRequest) error {
	if request.AccountChange.Kind != "preserve" && request.AccountChange.Kind != "update" {
		return authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
			"details": map[string]any{"violations": []map[string]any{{"path": "account_change.kind", "reason": "unsupported"}}},
		})
	}
	return nil
}

// prepareAuthSettingsMutation owns field, acknowledgement, account, and
// readiness validation after the Proxy readiness snapshot has been frozen.
// It deliberately does not query or mutate storage.
func prepareAuthSettingsMutation(request putAuthSettingsRequest, row authSettingsReadRow, readiness proxyKeyReadinessSnapshot) (authSettingsMutationInput, error) {
	accountUpdate := request.AccountChange.Kind == "update"
	enabling := request.DesiredAuthEnabled && !row.LegacyAuthEnabled
	disabling := !request.DesiredAuthEnabled && row.LegacyAuthEnabled

	// Acknowledgements.
	if disabling && !row.LegacyAuthEnabled && row.TransitionState == nil {
		// Disabling while already disabled is a no-op; still recorded.
	}
	disableAck := request.Acknowledgements.DisableToPermissiveAccess != nil && *request.Acknowledgements.DisableToPermissiveAccess
	sessionAck := request.Acknowledgements.InvalidateOperatorSessions != nil && *request.Acknowledgements.InvalidateOperatorSessions
	zeroKeyAck := request.Acknowledgements.EnableWithoutActiveProxyKeys != nil && *request.Acknowledgements.EnableWithoutActiveProxyKeys
	if request.Acknowledgements.EnableWithoutActiveProxyKeys != nil && !enabling {
		return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid acknowledgement for this mutation", map[string]any{
			"details": map[string]any{"violations": []map[string]any{{"path": "acknowledgements.enable_without_active_proxy_keys", "reason": "not_applicable"}}},
		})
	}
	if request.Acknowledgements.DisableToPermissiveAccess != nil && !disabling {
		return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid acknowledgement for this mutation", map[string]any{
			"details": map[string]any{"violations": []map[string]any{{"path": "acknowledgements.disable_to_permissive_access", "reason": "not_applicable"}}},
		})
	}
	if request.Acknowledgements.InvalidateOperatorSessions != nil && !(accountUpdate && row.LegacyAuthEnabled) {
		return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid acknowledgement for this mutation", map[string]any{
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
			return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "account_change.username", "reason": "required"}}},
			})
		}
		if len(*username) > authMaxUsernameLength {
			return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "account_change.username", "reason": "too_long", "limit": authMaxUsernameLength}}},
			})
		}
		normalized := strings.TrimSpace(*username)
		username = &normalized
		if newPassword != nil && (len(*newPassword) < 8 || len(*newPassword) > authMaxPasswordLength) {
			return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "account_change.new_password", "reason": "length", "limit": authMaxPasswordLength}}},
			})
		}
		// An unconfigured account requires a password on update.
		hasExistingHash := row.LegacyPasswordHash != nil
		if !hasExistingHash && (newPassword == nil || strings.TrimSpace(*newPassword) == "") {
			return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "invalid_operator_account", "invalid_operator_account", map[string]any{
				"details": map[string]any{"violations": []map[string]any{{"path": "account_change.new_password", "reason": "required"}}},
			})
		}
	}
	if accountUpdate && row.LegacyAuthEnabled && !sessionAck {
		return authSettingsMutationInput{}, authSettingsProblem(http.StatusConflict, "auth_acknowledgement_required", "auth_acknowledgement_required", map[string]any{
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
			return authSettingsMutationInput{}, authSettingsProblem(http.StatusConflict, "auth_readiness_changed", "auth_readiness_changed", map[string]any{
				"details": map[string]any{"operation_id": request.OperationID, "operation_recorded": false, "new_operation_id_required": true, "recovery": "review_and_resubmit"},
			})
		}
		if request.ExpectedProxyKeyReadinessGeneration == nil ||
			*request.ExpectedProxyKeyReadinessGeneration != readiness.Generation {
			return authSettingsMutationInput{}, readinessConflictProblem(readiness, "auth_readiness_changed", request.OperationID)
		}
		if readiness.SafeActive == 0 && !zeroKeyAck {
			return authSettingsMutationInput{}, readinessConflictProblem(readiness, "auth_acknowledgement_required", request.OperationID)
		}
	}
	if disabling && !disableAck {
		return authSettingsMutationInput{}, authSettingsProblem(http.StatusConflict, "auth_acknowledgement_required", "auth_acknowledgement_required", map[string]any{
			"details": map[string]any{
				"operation_id": request.OperationID, "operation_recorded": false,
				"new_operation_id_required": true,
				"required_acknowledgements": []string{"disable_to_permissive_access"},
				"recovery":                  "review_and_resubmit",
			},
		})
	}
	if request.ExpectedProxyKeyReadinessGeneration != nil && !enabling {
		return authSettingsMutationInput{}, authSettingsProblem(http.StatusUnprocessableEntity, "validation_failed", "invalid readiness generation for this mutation", map[string]any{
			"details": map[string]any{"violations": []map[string]any{{"path": "expected_proxy_key_readiness_generation", "reason": "must_be_null_unless_enabling"}}},
		})
	}

	return authSettingsMutationInput{
		accountUpdate:       accountUpdate,
		enabling:            enabling,
		disabling:           disabling,
		disableAcknowledged: disableAck,
		sessionAcknowledged: sessionAck,
		zeroKeyAcknowledged: zeroKeyAck,
		username:            username,
		newPassword:         newPassword,
	}, nil
}

package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

// PublicAuthOperationStatus is the bounded, fixed-shape projection of an
// auth-control transition operation for the initiating browser. It never
// returns the operation id (the URL selector is the lookup key), username,
// desired mode, settings, session actions, error prose, secrets or extension
// maps, and its serialized form is capped at 1 KiB.
type PublicAuthOperationStatus struct {
	State               string `json:"state"` // transitioning | retrying | rollback_required | effective | rolled_back | failed
	AccessState         string `json:"access_state"`
	EffectiveGeneration string `json:"effective_generation"`
	RetryAfterSeconds   *int64 `json:"retry_after_seconds"`
}

const publicAuthOperationStatusMaxBytes = 1024

// publicAuthOperationStatusPathPattern matches exactly
// /api/auth/operations/<RFC4122-UUID>/status with canonical segments. It is
// intentionally not a prefix matcher: percent-encoded, empty, trailing or
// extra segments and wrong methods never reach the public handler.
var publicAuthOperationStatusPathPattern = regexp.MustCompile(`^/api/auth/operations/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/status$`)

// isPublicAuthOperationStatusPath is the server-side exemption matcher. The
// server cannot observe browser fragments or prove browser same-origin, so it
// validates only what it can observe: exact method, raw path segments, empty
// query and a canonical UUID.
func isPublicAuthOperationStatusPath(method string, rawPath string, rawQuery string) bool {
	if method != http.MethodGet {
		return false
	}
	if rawQuery != "" {
		return false
	}
	return publicAuthOperationStatusPathPattern.MatchString(rawPath)
}

// parsePublicAuthOperationID extracts the canonical UUID when the raw path is
// the exact public operation-status shape.
func parsePublicAuthOperationID(rawPath string) (string, bool) {
	match := publicAuthOperationStatusPathPattern.FindStringSubmatch(rawPath)
	if match == nil {
		return "", false
	}
	return strings.ToLower(match[1]), true
}

func (s *Service) handleGetAuthOperationStatus(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	if r.URL.RawQuery != "" {
		s.writeAuthOperationNotFound(w, r)
		return
	}
	operationID, ok := parsePublicAuthOperationID(r.URL.Path)
	if !ok {
		s.writeAuthOperationNotFound(w, r)
		return
	}
	settingsRow, err := s.loadOrCreateAppAuthSettings(r.Context(), s.pool)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
		return
	}
	status := PublicAuthOperationStatus{}
	if settingsRow.TransitionState.Valid && settingsRow.TransitionOperationID.String == operationID {
		status.AccessState = settingsRow.TransitionState.String
		status.EffectiveGeneration = fmt.Sprintf("%d", settingsRow.EffectiveAuthGeneration)
		status.RetryAfterSeconds = s.transitionRetryAfterSeconds(settingsRow)
		switch settingsRow.TransitionState.String {
		case "rollback_required":
			status.State = "rollback_required"
		case "retrying":
			status.State = "retrying"
		case "enabling_fail_closed", "disabling_enforced", "account_transition_enabled", "account_transition_disabled":
			status.State = "transitioning"
		default:
			s.writeAuthOperationNotFound(w, r)
			return
		}
	} else {
		// A committed/rolled-back result remains discoverable after the active
		// transition pointer has been cleared. The result projection is read
		// through the durable operation store and is deliberately reduced to the
		// public union below.
		var raw []byte
		if err := s.pool.QueryRow(r.Context(), `SELECT result_json FROM settings_mutation_operations
			WHERE resource_kind = 'auth_settings' AND operation_id = $1`, operationID).Scan(&raw); err != nil {
			s.writeAuthOperationNotFound(w, r)
			return
		}
		var result struct {
			State               string         `json:"state"`
			EffectiveGeneration string         `json:"effective_generation"`
			Settings            map[string]any `json:"settings"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			s.writeAuthOperationNotFound(w, r)
			return
		}
		if result.State != "effective" && result.State != "rolled_back" && result.State != "failed" {
			s.writeAuthOperationNotFound(w, r)
			return
		}
		status.State = result.State
		status.EffectiveGeneration = result.EffectiveGeneration
		status.AccessState = "disabled"
		if mode, ok := result.Settings["auth_mode"].(map[string]any); ok {
			if accessState, ok := mode["access_state"].(string); ok {
				status.AccessState = accessState
			}
		}
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to encode auth operation status")
		return
	}
	if len(encoded) > publicAuthOperationStatusMaxBytes {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to encode auth operation status")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	responseutil.WriteJSON(w, http.StatusOK, status)
}

func (s *Service) writeAuthOperationNotFound(w http.ResponseWriter, r *http.Request) {
	responseutil.WriteProblem(w, r, s.corsSnapshot(), http.StatusNotFound, "auth_operation_not_found", "身份验证操作不存在", map[string]any{}, nil)
}

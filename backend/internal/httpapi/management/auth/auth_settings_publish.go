package auth

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

// publishAuthSettingsMutation owns all effects after transaction commit:
// management snapshot invalidation, runtime-auth cache publication, cookie
// mutation, and the successful response envelope.
func (s *Service) publishAuthSettingsMutation(w http.ResponseWriter, outcome authSettingsMutationOutcome) {
	result := outcome.result
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
	responseutil.WriteJSON(w, http.StatusOK, authPutSettingsResponse{
		OperationID:        result.OperationID,
		Replayed:           outcome.replayed,
		EffectState:        result.State,
		Settings:           result.Settings,
		SessionAction:      result.SessionAction,
		OperationStatusURL: "/api/auth/operations/" + result.OperationID + "/status",
	})
}

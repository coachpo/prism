package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
)

// writeAuthSettingsMutationFailure maps a failed mutation to its registered
// envelope and invokes durable readiness-result persistence when required.
func (s *Service) writeAuthSettingsMutationFailure(w http.ResponseWriter, r *http.Request, activationCtx context.Context, request putAuthSettingsRequest, err error) {
	var readinessConflict *authReadinessConflictError
	if errors.As(err, &readinessConflict) {
		readinessConflict.Request = request
		if recordErr := s.recordAuthReadinessConflict(activationCtx, readinessConflict); recordErr != nil {
			// Never emit a readiness conflict claiming durable recovery when
			// the outcome record itself could not be committed. The safe
			// fallback is a bounded unavailable response; the caller may
			// re-read and submit a new operation identity.
			writeAuthSettingsProblem(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "auth_settings_unavailable", "Failed to record authentication readiness outcome", map[string]any{
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
		writeAuthSettingsProblem(w, r, s.corsSnapshot(), http.StatusInternalServerError, "auth_settings_unavailable", "Failed to apply authentication settings", nil)
	}
}

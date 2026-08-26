package auth

import (
	"net/http"
	"strings"
)

// decodePutAuthSettingsRequest owns the strict wire decoding and the
// operation identity check that must happen before any database work.
func (s *Service) decodePutAuthSettingsRequest(w http.ResponseWriter, r *http.Request) (putAuthSettingsRequest, bool) {
	var request putAuthSettingsRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeAuthSettingsProblem(w, r, s.corsSnapshot(), http.StatusBadRequest, "validation_failed", "Invalid request body", map[string]any{"violations": []any{}})
		return putAuthSettingsRequest{}, false
	}
	if strings.TrimSpace(request.OperationID) == "" {
		writeAuthSettingsProblem(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "validation_failed", "operation_id is required", map[string]any{
			"violations": []map[string]any{{"path": "operation_id", "reason": "required"}},
		})
		return putAuthSettingsRequest{}, false
	}
	return request, true
}

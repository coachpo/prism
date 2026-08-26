package runtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func writeRuntimeObservabilityHandoffError(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "runtime_observability_handoff_failed", "Runtime observability handoff failed", nil)
}

func writeDomainError(w http.ResponseWriter, err error) {
	var runtimeErr *domainError
	if errors.As(err, &runtimeErr) {
		writeError(w, runtimeErr.StatusCode, runtimeErr.ErrorCode, runtimeErr.Detail, runtimeErr.Fields)
		return
	}
	writeError(w, http.StatusInternalServerError, "", "Internal server error", nil)
}

func writeError(w http.ResponseWriter, statusCode int, errorCode string, detail string, fields map[string]any) {
	payload := map[string]any{"detail": detail}
	if strings.TrimSpace(errorCode) != "" {
		payload["error"] = strings.TrimSpace(errorCode)
	}
	for key, value := range fields {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		payload[key] = value
	}
	writeJSON(w, statusCode, payload)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

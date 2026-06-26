package responseutil

import (
	"encoding/json"
	"net/http"

	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func WriteProfileHTTPError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *profiledomain.HTTPError) {
	if err == nil {
		return
	}
	applyAllowedOrigin(w, r, corsSnapshot)
	WriteJSON(w, err.StatusCode, err.ResponseBody())
}

func WriteJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func WriteError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail any) {
	WriteErrorFields(w, r, corsSnapshot, statusCode, detail, nil)
}

func WriteErrorFields(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail any, fields map[string]any) {
	applyAllowedOrigin(w, r, corsSnapshot)
	payload := map[string]any{"detail": detail}
	for key, value := range fields {
		payload[key] = value
	}
	WriteJSON(w, statusCode, payload)
}

func applyAllowedOrigin(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
}

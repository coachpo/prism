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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(err.StatusCode)
	_ = json.NewEncoder(w).Encode(err.ResponseBody())
}

func applyAllowedOrigin(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
}

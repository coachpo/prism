package responseutil

import (
	"encoding/json"
	"net/http"
	"strings"

	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func WriteProfileHTTPError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err *profiledomain.HTTPError) {
	if err == nil {
		return
	}
	applyAllowedOrigin(w, r, allowedOrigins)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(err.StatusCode)
	_ = json.NewEncoder(w).Encode(err.ResponseBody())
}

func applyAllowedOrigin(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return
	}
	if _, ok := allowedOrigins[origin]; !ok {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Vary", "Origin")
}

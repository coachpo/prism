package platformhttp

import (
	"encoding/json"
	"errors"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"net/http"
	"strconv"
	"time"
)

func writeAdmissionError(w http.ResponseWriter, err error) {
	if overload, ok := errors.AsType[*admission.OverloadError](err); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if overload.RetryAfter > 0 {
			w.Header().Set("Retry-After", retryAfterHeaderValue(overload.RetryAfter))
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Management route temporarily overloaded. Retry later."})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": "Admission failed"})
}

func retryAfterHeaderValue(duration time.Duration) string {
	seconds := int(duration.Round(time.Second) / time.Second)
	seconds = max(seconds, 1)
	return strconv.Itoa(seconds)
}

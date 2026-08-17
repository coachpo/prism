package stats

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func invalidQueryParameter(key string, reason string) error {
	return &statsdomain.HTTPError{
		StatusCode: http.StatusUnprocessableEntity,
		Code:       "invalid_query_parameter",
		Detail:     fmt.Sprintf("%s %s", key, reason),
		Details:    map[string]any{"parameter": key},
	}
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	var statsErr *statsdomain.HTTPError
	if errors.As(err, &statsErr) {
		if statsErr.Code != "" {
			writeStructuredError(w, r, corsSnapshot, statsErr)
			return
		}
		responseutil.WriteError(w, r, corsSnapshot, statsErr.StatusCode, statsErr.Detail)
		return
	}
	if strings.Contains(r.URL.Path, "/stats/requests") {
		slog.Error("stats requests handler error", "path", r.URL.Path, "error", err)
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeStructuredError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *statsdomain.HTTPError) {
	var details any
	if len(err.Details) > 0 {
		details = err.Details
	}
	responseutil.WriteProblem(w, r, corsSnapshot, err.StatusCode, err.Code, err.Detail, map[string]any{}, details)
}

func parseOptionalBool(r *http.Request, key string) (*bool, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "true":
		parsed := true
		return &parsed, nil
	case "false":
		parsed := false
		return &parsed, nil
	default:
		return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid " + key}
	}
}

func parseUnpricedReasonValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "PRICING_DISABLED", "MISSING_TOKEN_USAGE", "STREAM_USAGE_UNAVAILABLE", "MISSING_PRICE_DATA":
		return trimmed, nil
	default:
		return "", &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid unpriced_reason"}
	}
}

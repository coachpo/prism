package configrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	return responseutil.SanitizeDecodeError(json.NewDecoder(request.Body).Decode(target))
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var configErr *domainError
	if errors.As(err, &configErr) {
		responseutil.WriteError(w, r, corsSnapshot, configErr.StatusCode, configErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func parseBooleanQuery(request *http.Request, name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(request.URL.Query().Get(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s must be a boolean", name)}
	}
	return parsed, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

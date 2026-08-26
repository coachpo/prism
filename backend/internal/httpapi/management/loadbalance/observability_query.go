package loadbalance

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func parseRepeatableQuery(r *http.Request, key string) ([]string, error) {
	rawValues, ok := r.URL.Query()[key]
	if !ok || len(rawValues) == 0 {
		return nil, nil
	}
	items := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				items = append(items, trimmed)
			}
		}
	}
	return items, nil
}

func optionalTrimmedQuery(r *http.Request, key string) *string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	return &value
}

func parseRequiredPositiveIntQuery(r *http.Request, key string) (int, error) {
	parsed, err := parseOptionalPositiveIntQuery(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return 0, fmt.Errorf("%s is required", key)
	}
	return *parsed, nil
}

func parseOptionalPositiveIntQuery(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("invalid %s", key)
	}
	resolved := parsed
	return &resolved, nil
}

func parsePositiveIntQueryWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalPositiveIntQuery(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	return *parsed, nil
}

func routeInt64(request *http.Request, name string) (int64, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

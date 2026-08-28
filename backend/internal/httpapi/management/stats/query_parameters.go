package stats

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/go-chi/chi/v5"
)

func repeatableQueryValues(r *http.Request, key string) []string {
	values, ok := r.URL.Query()[key]
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func repeatableQueryInts(r *http.Request, key string) ([]int, error) {
	values, ok := r.URL.Query()[key]
	if !ok {
		return nil, nil
	}
	result := make([]int, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		var parsed int
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err != nil {
			return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid " + key}
		}
		result = append(result, parsed)
	}
	return result, nil
}

func parseStatsSummaryParams(r *http.Request, profileID int) (statsdomain.StatsSummaryParams, error) {
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	toTime, err := parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	endpointID, err := parseOptionalPositiveStatsID(r, "endpoint_id")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	connectionID, err := parseOptionalPositiveStatsID(r, "terminal_target_id")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	scope := queryStringOrDefault(r, "scope", "ingress")
	normalizedScope, err := statsdomain.NormalizeScope(scope)
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	keys := make([]string, 0, len(r.URL.Query()))
	for key := range r.URL.Query() {
		keys = append(keys, key)
	}
	if err := statsdomain.ValidateScopeQueryKeys(normalizedScope, keys); err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	if err := validateAggregateEndpointQueryKeys("summary", normalizedScope, keys); err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	groupBy := normalizedQueryString(r, "group_by")
	validatedGroup, err := statsdomain.ValidateGroupBy(normalizedScope, valueOrEmpty(groupBy))
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	if validatedGroup == statsdomain.GroupProxyAPIKey {
		return statsdomain.StatsSummaryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "group_invalid", Detail: "summary does not support group_by proxy_api_key"}
	}
	return statsdomain.StatsSummaryParams{
		ProfileID: profileID, FromTime: fromTime, ToTime: toTime, Preset: queryStringOrDefault(r, "preset", "24h"),
		ReferenceNow: time.Now().UTC(), GroupBy: groupBy,
		IngressModelID: normalizedQueryString(r, "ingress_model_id"), FinalTargetModelID: normalizedQueryString(r, "final_target_model_id"),
		AttemptTargetModelID: normalizedQueryString(r, "attempt_target_model_id"), APIFamily: normalizedQueryString(r, "api_family"),
		EndpointID: endpointID, ConnectionID: connectionID, AttemptTrigger: normalizedQueryString(r, "attempt_trigger"), AttemptResult: normalizedQueryString(r, "attempt_result"), Scope: normalizedScope,
	}, nil
}

func validateAggregateEndpointQueryKeys(endpoint string, scope string, keys []string) error {
	allowed := map[string]struct{}{"scope": {}, "preset": {}, "from_time": {}, "to_time": {}, "api_family": {}}
	switch endpoint {
	case "summary":
		allowed["group_by"] = struct{}{}
	case "spending":
		for _, key := range []string{"group_by", "limit", "offset", "top_n"} {
			allowed[key] = struct{}{}
		}
	case "throughput":
	default:
		return &statsdomain.HTTPError{StatusCode: http.StatusInternalServerError, Code: "endpoint_contract_invalid", Detail: "unknown aggregate endpoint contract"}
	}
	switch scope {
	case statsdomain.ScopeIngress:
		allowed["ingress_model_id"] = struct{}{}
	case statsdomain.ScopeFinal:
		allowed["final_target_model_id"] = struct{}{}
		allowed["endpoint_id"] = struct{}{}
		allowed["terminal_target_id"] = struct{}{}
	case statsdomain.ScopeRouteAttempt:
		if endpoint != "spending" {
			allowed["attempt_target_model_id"] = struct{}{}
			allowed["endpoint_id"] = struct{}{}
			allowed["terminal_target_id"] = struct{}{}
		}
		if endpoint == "summary" {
			allowed["attempt_trigger"] = struct{}{}
			allowed["attempt_result"] = struct{}{}
		}
	}
	for _, key := range keys {
		if _, ok := allowed[key]; !ok {
			return &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "filter_invalid", Detail: fmt.Sprintf("filter %q is not supported by %s for scope %q", key, endpoint, scope)}
		}
	}
	return nil
}

func decodeModelMetricsRequest(r *http.Request) (modelMetricsBatchRequest, error) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var requestBody modelMetricsBatchRequest
	if err := decoder.Decode(&requestBody); err != nil {
		return modelMetricsBatchRequest{}, responseutil.SanitizeDecodeError(err)
	}
	return requestBody, nil
}

func parseOptionalTime(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, invalidQueryParameter(key, "must be an RFC3339 timestamp")
		}
	}
	resolved := parsed.UTC()
	return &resolved, nil
}

func parseOptionalInt(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil, invalidQueryParameter(key, "must be an integer")
	}
	if parsed > math.MaxInt32 || parsed < math.MinInt32 {
		return nil, invalidQueryParameter(key, fmt.Sprintf("must be within [%d, %d]", math.MinInt32, math.MaxInt32))
	}
	resolved := parsed
	return &resolved, nil
}

func parsePositiveIntWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalInt(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	if *parsed <= 0 {
		return 0, invalidQueryParameter(key, "must be >= 1")
	}
	return *parsed, nil
}

func parseNonNegativeIntWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalInt(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	if *parsed < 0 {
		return 0, invalidQueryParameter(key, "must be >= 0")
	}
	return *parsed, nil
}

func normalizedQueryString(r *http.Request, key string) *string {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}
	return &raw
}

func queryStringOrDefault(r *http.Request, key string, defaultValue string) string {
	if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
		return value
	}
	return defaultValue
}

func routeInt(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, invalidQueryParameter(name, "must be a positive integer")
	}
	return parsed, nil
}

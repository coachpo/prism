package stats

import (
	"database/sql"
	"fmt"
	"strings"
)

var failedAttemptRequestFilterValues = []string{
	string(attemptClassHTTPError),
	string(attemptClassStreamError),
	string(attemptClassTransportError),
	string(attemptClassClientDisconnected),
	string(attemptClassUnknown),
	"__null__",
}

func attemptErrorWhere(profileID int, bounds QueryBounds, params UsageErrorsParams) (string, []any, error) {
	clauses := []string{"profile_id = $1", "row_kind = 'upstream'", "created_at >= $2", "created_at < $3"}
	args := []any{profileID, bounds.UsageFrom.UTC(), bounds.UsageTo.UTC()}
	add := func(value any, format string) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(format, len(args)))
	}
	addStrings := func(column string, values []string, allowed map[string]struct{}) error {
		if len(values) == 0 {
			return nil
		}
		parts := make([]string, 0, len(values))
		for _, raw := range values {
			value := strings.TrimSpace(raw)
			if value == "__null__" {
				parts = append(parts, column+" IS NULL")
				continue
			}
			if value == "" {
				return &HTTPError{StatusCode: 422, Code: "filter_invalid", Detail: "empty " + column + " filter"}
			}
			if allowed != nil {
				if _, ok := allowed[value]; !ok {
					return &HTTPError{StatusCode: 422, Code: "filter_invalid", Detail: "invalid " + column + ": " + value}
				}
			}
			args = append(args, value)
			parts = append(parts, fmt.Sprintf("%s = $%d", column, len(args)))
		}
		clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
		return nil
	}
	if params.AttemptTargetModelID != nil {
		add(strings.TrimSpace(*params.AttemptTargetModelID), "resolved_target_model_id = $%d")
	}
	if params.APIFamily != nil {
		add(strings.TrimSpace(*params.APIFamily), "api_family = $%d")
	}
	if params.EndpointID != nil {
		add(*params.EndpointID, "endpoint_id = $%d")
	}
	if params.TerminalTargetID != nil {
		add(*params.TerminalTargetID, "connection_id = $%d")
	}
	attemptResults := map[string]struct{}{"completed": {}, "http_error": {}, "stream_error": {}, "transport_error": {}, "cancelled": {}, "client_disconnected": {}, "unknown": {}}
	attemptTriggers := map[string]struct{}{"initial": {}, "retry_same_target": {}, "hedge": {}, "failover": {}}
	streamOutcomes := map[string]struct{}{
		"not_streaming": {}, "completed": {}, "gateway_timeout": {}, "provider_incomplete": {}, "client_disconnected": {},
		"upstream_read_error": {}, "upstream_ended_without_terminal": {}, "unknown": {},
	}
	if err := addStrings("attempt_result", params.AttemptResult, attemptResults); err != nil {
		return "", nil, err
	}
	if err := addStrings("attempt_trigger", params.AttemptTrigger, attemptTriggers); err != nil {
		return "", nil, err
	}
	if err := addStrings("stream_outcome", params.StreamOutcome, streamOutcomes); err != nil {
		return "", nil, err
	}
	if err := addStrings("stream_error_kind", params.StreamErrorKind, nil); err != nil {
		return "", nil, err
	}
	if len(params.StatusCode) > 0 {
		parts := make([]string, 0, len(params.StatusCode))
		for _, status := range params.StatusCode {
			if status < 100 || status > 599 {
				return "", nil, &HTTPError{StatusCode: 422, Code: "filter_invalid", Detail: fmt.Sprintf("invalid status_code: %d", status)}
			}
			args = append(args, status)
			parts = append(parts, fmt.Sprintf("$%d", len(args)))
		}
		clauses = append(clauses, "upstream_status_code IN ("+strings.Join(parts, ", ")+")")
	}
	return strings.Join(clauses, " AND "), args, nil
}

func attemptErrorGroupIdentity(groupBy string, target sql.NullString, endpointID sql.NullInt32, connectionID sql.NullInt32, apiFamily sql.NullString, trigger sql.NullString, attemptResult sql.NullString) (string, string, *string) {
	stringIdentity := func(value sql.NullString, nullLabel string) (string, string, *string) {
		if value.Valid && strings.TrimSpace(value.String) != "" {
			resolved := strings.TrimSpace(value.String)
			return "value:" + resolved, resolved, &resolved
		}
		return "null", nullLabel, nil
	}
	switch groupBy {
	case GroupAttemptTargetModel:
		return stringIdentity(target, "Unattributed")
	case GroupEndpoint:
		if endpointID.Valid && endpointID.Int32 > 0 {
			resolved := fmt.Sprintf("%d", endpointID.Int32)
			return "value:" + resolved, "Endpoint #" + resolved, &resolved
		}
		return "null", "Unattributed", nil
	case GroupTerminalTarget:
		if connectionID.Valid && connectionID.Int32 > 0 {
			resolved := fmt.Sprintf("%d", connectionID.Int32)
			return "value:" + resolved, "Terminal Target #" + resolved, &resolved
		}
		return "null", "Unattributed", nil
	case GroupAPIFamily:
		return stringIdentity(apiFamily, "Unknown")
	case GroupAttemptTrigger:
		return stringIdentity(trigger, "unknown")
	case GroupAttemptResult:
		return stringIdentity(attemptResult, "unknown")
	default:
		resolved := "total"
		return "total", "Total", &resolved
	}
}

func attemptBaseRequestFilters(params UsageErrorsParams) map[string][]string {
	filters := map[string][]string{"row_kind": {"upstream"}}
	if params.AttemptTargetModelID != nil && strings.TrimSpace(*params.AttemptTargetModelID) != "" {
		filters["attempt_target_model_id"] = []string{strings.TrimSpace(*params.AttemptTargetModelID)}
	}
	if params.EndpointID != nil {
		filters["endpoint_id"] = []string{fmt.Sprintf("%d", *params.EndpointID)}
	}
	if params.TerminalTargetID != nil {
		filters["terminal_target_id"] = []string{fmt.Sprintf("%d", *params.TerminalTargetID)}
	}
	if params.APIFamily != nil && strings.TrimSpace(*params.APIFamily) != "" {
		filters["api_family"] = []string{strings.TrimSpace(*params.APIFamily)}
	}
	if len(params.AttemptTrigger) > 0 {
		filters["attempt_trigger"] = append([]string(nil), params.AttemptTrigger...)
	}
	if len(params.AttemptResult) > 0 {
		filters["attempt_result"] = append([]string(nil), params.AttemptResult...)
	}
	if len(params.StatusCode) > 0 {
		values := make([]string, 0, len(params.StatusCode))
		for _, value := range params.StatusCode {
			values = append(values, fmt.Sprintf("%d", value))
		}
		filters["status_code"] = values
	}
	if len(params.StreamOutcome) > 0 {
		filters["stream_outcome"] = append([]string(nil), params.StreamOutcome...)
	}
	if len(params.StreamErrorKind) > 0 {
		filters["stream_error_kind"] = append([]string(nil), params.StreamErrorKind...)
	}
	return filters
}

func attemptProblemFilters(params UsageErrorsParams) map[string][]string {
	filters := attemptBaseRequestFilters(params)
	if _, filtered := filters["attempt_result"]; !filtered {
		filters["attempt_result"] = append([]string(nil), failedAttemptRequestFilterValues...)
	}
	return filters
}

func attemptHTTPStatusFilters(params UsageErrorsParams, statusCode int) map[string][]string {
	filters := attemptProblemFilters(params)
	filters["status_code"] = []string{fmt.Sprintf("%d", statusCode)}
	return filters
}

func attemptStreamOutcomeFilters(params UsageErrorsParams, outcome string) map[string][]string {
	filters := attemptProblemFilters(params)
	filters["stream_outcome"] = []string{outcome}
	return filters
}

func attemptStreamKindFilters(params UsageErrorsParams, outcome string, kind *string) map[string][]string {
	filters := attemptStreamOutcomeFilters(params, outcome)
	if kind == nil {
		filters["stream_error_kind"] = []string{"__null__"}
	} else {
		filters["stream_error_kind"] = []string{*kind}
	}
	return filters
}

func attemptGroupRequestFilterKey(groupBy string) string {
	switch groupBy {
	case GroupAttemptTargetModel:
		return "attempt_target_model_id"
	case GroupEndpoint:
		return "endpoint_id"
	case GroupTerminalTarget:
		return "terminal_target_id"
	case GroupAPIFamily:
		return "api_family"
	case GroupAttemptTrigger:
		return "attempt_trigger"
	case GroupAttemptResult:
		return "attempt_result"
	default:
		return ""
	}
}

func attemptGroupFilters(params UsageErrorsParams, group ErrorsGroup) map[string][]string {
	filters := attemptProblemFilters(params)
	key := attemptGroupRequestFilterKey(group.EntityType)
	if key == "" {
		return filters
	}
	if group.EntityID == nil {
		filters[key] = []string{"__null__"}
	} else {
		filters[key] = []string{*group.EntityID}
	}
	return filters
}

package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

var failedAttemptRequestFilterValues = []string{
	"http_error",
	"stream_error",
	"transport_error",
	"client_disconnected",
	"unknown",
	"__null__",
}

func loadAttemptErrors(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, coverage Coverage, params UsageErrorsParams, queryContext string, referenceNow time.Time) (UsageErrorsResult, error) {
	result := UsageErrorsResult{
		GeneratedAt: referenceNow.UTC(), Coverage: coverage, Caliber: CaliberForScope(ScopeRouteAttempt),
		DatasetCoverage: DatasetCoverage{RequestLogs: &coverage}, Timeline: []ErrorsTimelinePoint{}, HTTPStatuses: []ErrorsHTTPStatus{}, StreamOutcomes: []ErrorsStreamOutcome{}, Groups: []ErrorsGroup{},
		RequestsContext: ErrorsRequestsContext{View: "attempts", QueryContext: queryContext, FinalFromTime: bounds.UsageFrom.UTC().Format(time.RFC3339), FinalToTime: bounds.UsageTo.UTC().Format(time.RFC3339), BaseRequestFilters: attemptBaseRequestFilters(params)},
	}
	clauses := []string{"profile_id = $1", "row_kind = 'upstream'", "created_at >= $2", "created_at < $3"}
	args := []any{profileID, bounds.UsageFrom.UTC(), bounds.UsageTo.UTC()}
	add := func(value any, template string) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(template, len(args)))
	}
	if params.AttemptTargetModelID != nil {
		add(strings.TrimSpace(*params.AttemptTargetModelID), "resolved_target_model_id = $%d")
	}
	if params.EndpointID != nil {
		add(*params.EndpointID, "endpoint_id = $%d")
	}
	if params.TerminalTargetID != nil {
		add(*params.TerminalTargetID, "connection_id = $%d")
	}
	if len(params.StatusCode) > 0 {
		placeholders := make([]string, 0, len(params.StatusCode))
		for _, statusCode := range params.StatusCode {
			args = append(args, statusCode)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		clauses = append(clauses, "upstream_status_code IN ("+strings.Join(placeholders, ",")+")")
	}
	rows, err := exec.Query(ctx, `SELECT created_at, resolved_target_model_id, endpoint_id, connection_id, api_family, attempt_trigger, attempt_result, upstream_status_code
		FROM request_logs WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at ASC, id ASC`, args...)
	if err != nil {
		return result, fmt.Errorf("load route-attempt errors: %w", err)
	}
	defer rows.Close()
	type groupAccumulator struct {
		count, failed int
		latest        time.Time
		label         string
		entityID      *string
	}
	groups := map[string]*groupAccumulator{}
	timeline := map[time.Time]*ErrorsTimelinePoint{}
	statusCounts := map[int]*ErrorsHTTPStatus{}
	for rows.Next() {
		var createdAt time.Time
		var target, apiFamily, trigger, attemptResult sql.NullString
		var endpointID, connectionID, statusCode sql.NullInt32
		if err := rows.Scan(&createdAt, &target, &endpointID, &connectionID, &apiFamily, &trigger, &attemptResult, &statusCode); err != nil {
			return result, err
		}
		result.Summary.RequestCount++
		failed := !attemptResult.Valid || (attemptResult.String != "completed" && attemptResult.String != "cancelled")
		if failed {
			result.Summary.FailedCount++
			result.Summary.HTTPErrorCount++
			bucket := createdAt.UTC().Truncate(time.Hour)
			point := timeline[bucket]
			if point == nil {
				point = &ErrorsTimelinePoint{BucketStart: bucket.Format(time.RFC3339)}
				timeline[bucket] = point
			}
			point.HTTPErrorCount++
			point.FailedCount++
			if statusCode.Valid {
				status := statusCounts[int(statusCode.Int32)]
				if status == nil {
					status = &ErrorsHTTPStatus{StatusCode: int(statusCode.Int32)}
					statusCounts[int(statusCode.Int32)] = status
				}
				status.Count++
				status.LastSeenAt = createdAt.UTC()
			}
		}
		key, label, entityID := attemptErrorGroupIdentity(params.GroupBy, target, endpointID, connectionID, apiFamily, trigger, attemptResult)
		group := groups[key]
		if group == nil {
			group = &groupAccumulator{label: label, entityID: entityID}
			groups[key] = group
		}
		group.count++
		if failed {
			group.failed++
		}
		if createdAt.After(group.latest) {
			group.latest = createdAt.UTC()
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Samples = ScopeSampleCounts{ObservationCount: result.Summary.RequestCount, LatencyMissingCount: result.Summary.RequestCount}
	for _, point := range timeline {
		result.Timeline = append(result.Timeline, *point)
	}
	sort.Slice(result.Timeline, func(i, j int) bool { return result.Timeline[i].BucketStart < result.Timeline[j].BucketStart })
	for _, item := range statusCounts {
		item.Denominator = result.Summary.FailedCount
		item.Percentage = percentageOf(item.Count, item.Denominator)
		item.RequestFilters = attemptHTTPStatusFilters(params, item.StatusCode)
		result.HTTPStatuses = append(result.HTTPStatuses, *item)
	}
	sort.Slice(result.HTTPStatuses, func(i, j int) bool { return result.HTTPStatuses[i].Count > result.HTTPStatuses[j].Count })
	for _, aggregate := range groups {
		item := ErrorsGroup{EntityType: params.GroupBy, EntityID: aggregate.entityID, Label: aggregate.label, ProblemCount: aggregate.failed, FailedCount: aggregate.failed, Denominator: aggregate.count, LastSeenAt: aggregate.latest}
		item.Percentage = percentageOf(item.ProblemCount, item.Denominator)
		item.RequestFilters = attemptGroupFilters(params, item)
		result.Groups = append(result.Groups, item)
	}
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i].ProblemCount > result.Groups[j].ProblemCount })
	return result, nil
}

func attemptErrorGroupIdentity(groupBy string, target sql.NullString, endpointID sql.NullInt32, connectionID sql.NullInt32, apiFamily sql.NullString, trigger sql.NullString, result sql.NullString) (string, string, *string) {
	stringIdentity := func(value sql.NullString, missingLabel string) (string, string, *string) {
		if value.Valid && strings.TrimSpace(value.String) != "" {
			resolved := strings.TrimSpace(value.String)
			return "value:" + resolved, resolved, &resolved
		}
		return "null", missingLabel, nil
	}
	intIdentity := func(value sql.NullInt32) (string, string, *string) {
		if value.Valid && value.Int32 > 0 {
			resolved := fmt.Sprintf("%d", value.Int32)
			return "value:" + resolved, resolved, &resolved
		}
		return "null", "unattributed", nil
	}
	switch groupBy {
	case GroupAttemptTargetModel:
		return stringIdentity(target, "unattributed")
	case GroupEndpoint:
		return intIdentity(endpointID)
	case GroupTerminalTarget:
		return intIdentity(connectionID)
	case GroupAPIFamily:
		return stringIdentity(apiFamily, "unknown")
	case GroupAttemptTrigger:
		return stringIdentity(trigger, "unknown")
	case GroupAttemptResult:
		return stringIdentity(result, "unknown")
	default:
		return "total", "total", nil
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
	if len(params.StatusCode) > 0 {
		values := make([]string, 0, len(params.StatusCode))
		for _, statusCode := range params.StatusCode {
			values = append(values, fmt.Sprintf("%d", statusCode))
		}
		filters["status_code"] = values
	}
	return filters
}

func attemptHTTPStatusFilters(params UsageErrorsParams, statusCode int) map[string][]string {
	filters := attemptBaseRequestFilters(params)
	filters["status_code"] = []string{fmt.Sprintf("%d", statusCode)}
	filters["attempt_result"] = append([]string(nil), failedAttemptRequestFilterValues...)
	return filters
}

func attemptGroupFilters(params UsageErrorsParams, group ErrorsGroup) map[string][]string {
	filters := attemptBaseRequestFilters(params)
	value := group.Label
	if group.EntityID == nil {
		value = "__null__"
	}
	switch group.EntityType {
	case GroupAttemptTargetModel:
		filters["attempt_target_model_id"] = []string{value}
	case GroupEndpoint:
		filters["endpoint_id"] = []string{value}
	case GroupTerminalTarget:
		filters["terminal_target_id"] = []string{value}
	case GroupAPIFamily:
		filters["api_family"] = []string{value}
	case GroupAttemptTrigger:
		filters["attempt_trigger"] = []string{value}
	case GroupAttemptResult:
		filters["attempt_result"] = []string{value}
	}
	if group.EntityType != GroupAttemptResult {
		filters["attempt_result"] = append([]string(nil), failedAttemptRequestFilterValues...)
	}
	return filters
}

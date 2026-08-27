package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

func loadAttemptErrors(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, coverage Coverage, params UsageErrorsParams, queryContext string, referenceNow time.Time) (UsageErrorsResult, error) {
	result := UsageErrorsResult{
		GeneratedAt: referenceNow.UTC(), Coverage: coverage, Caliber: CaliberForScope(ScopeRouteAttempt),
		DatasetCoverage: DatasetCoverage{RequestLogs: &coverage}, Timeline: []ErrorsTimelinePoint{}, HTTPStatuses: []ErrorsHTTPStatus{}, StreamOutcomes: []ErrorsStreamOutcome{}, Groups: []ErrorsGroup{},
		RequestsContext: ErrorsRequestsContext{View: "attempts", QueryContext: queryContext, FinalFromTime: bounds.UsageFrom.UTC().Format(time.RFC3339), FinalToTime: bounds.UsageTo.UTC().Format(time.RFC3339), BaseRequestFilters: map[string][]string{}},
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
	rows, err := exec.Query(ctx, `SELECT created_at, resolved_target_model_id, endpoint_id, connection_id, api_family, attempt_trigger, attempt_result, upstream_status_code
		FROM request_logs WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at ASC, id ASC`, args...)
	if err != nil {
		return result, fmt.Errorf("load route-attempt errors: %w", err)
	}
	defer rows.Close()
	type groupAccumulator struct {
		count, failed int
		latest        time.Time
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
		key := attemptSeriesGroupKey(params.GroupBy, target, endpointID, connectionID, apiFamily, trigger, attemptResult)
		group := groups[key]
		if group == nil {
			group = &groupAccumulator{}
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
		result.HTTPStatuses = append(result.HTTPStatuses, *item)
	}
	sort.Slice(result.HTTPStatuses, func(i, j int) bool { return result.HTTPStatuses[i].Count > result.HTTPStatuses[j].Count })
	for key, aggregate := range groups {
		entityID := key
		item := ErrorsGroup{EntityType: params.GroupBy, EntityID: &entityID, Label: key, ProblemCount: aggregate.failed, FailedCount: aggregate.failed, Denominator: aggregate.count, LastSeenAt: aggregate.latest}
		item.Percentage = percentageOf(item.ProblemCount, item.Denominator)
		result.Groups = append(result.Groups, item)
	}
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i].ProblemCount > result.Groups[j].ProblemCount })
	return result, nil
}

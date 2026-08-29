package stats

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Usage errors aggregate: summary, timeline, HTTP status ranking, stream
// outcome/kind ranking and entity groups over one finalized cohort. Every
// leaf/group carries a backend-built canonical final_* filter conjunction for
// the Requests deep link.

type UsageErrorsParams struct {
	GroupBy              string
	Scope                string
	APIFamily            *string
	ProxyAPIKeyID        *int
	EndpointID           *int
	IngressModelID       *string
	FinalTargetModelID   *string
	AttemptTargetModelID *string
	AttemptTrigger       []string
	AttemptResult        []string
	FinalResult          []string
	OutcomeDetail        []string
	StatusCode           []int
	StreamOutcome        []string
	StreamErrorKind      []string // "__null__" matches null kind
	TerminalTargetID     *int     // requires EndpointID
	Limit                int
}

type UsageErrorsResult struct {
	GeneratedAt     time.Time             `json:"generated_at"`
	Coverage        Coverage              `json:"coverage"`
	RequestsContext ErrorsRequestsContext `json:"requests_context"`
	Summary         ErrorsSummary         `json:"summary"`
	Timeline        []ErrorsTimelinePoint `json:"timeline"`
	HTTPStatuses    []ErrorsHTTPStatus    `json:"http_statuses"`
	StreamOutcomes  []ErrorsStreamOutcome `json:"stream_outcomes"`
	Groups          []ErrorsGroup         `json:"groups"`
	Other           ErrorsOther           `json:"other"`
	Caliber         ScopeCaliber          `json:"caliber"`
	DatasetCoverage DatasetCoverage       `json:"dataset_coverage"`
	Samples         ScopeSampleCounts     `json:"samples"`
}

type ErrorsRequestsContext struct {
	View               string              `json:"view"`
	QueryContext       string              `json:"query_context"`
	FinalFromTime      string              `json:"final_from_time"`
	FinalToTime        string              `json:"final_to_time"`
	BaseRequestFilters map[string][]string `json:"base_request_filters"`
}

type ErrorsSummary struct {
	RequestCount                 int `json:"request_count"`
	HTTPErrorCount               int `json:"http_error_count"`
	StreamErrorCount             int `json:"stream_error_count"`
	TransportErrorCount          int `json:"transport_error_count"`
	FailedCount                  int `json:"failed_count"`
	ClientDisconnectedCount      int `json:"client_disconnected_count"`
	DiagnosticStreamAnomalyCount int `json:"diagnostic_stream_anomaly_count"`
}

type ErrorsTimelinePoint struct {
	BucketStart             string `json:"bucket_start"`
	HTTPErrorCount          int    `json:"http_error_count"`
	StreamErrorCount        int    `json:"stream_error_count"`
	TransportErrorCount     int    `json:"transport_error_count"`
	FailedCount             int    `json:"failed_count"`
	ClientDisconnectedCount int    `json:"client_disconnected_count"`
}

type ErrorsHTTPStatus struct {
	StatusCode     int                 `json:"status_code"`
	Count          int                 `json:"count"`
	Denominator    int                 `json:"denominator"`
	Percentage     *float64            `json:"percentage"`
	LastSeenAt     time.Time           `json:"last_seen_at"`
	RequestFilters map[string][]string `json:"request_filters"`
}

type ErrorsStreamKind struct {
	StreamErrorKind *string             `json:"stream_error_kind"`
	Count           int                 `json:"count"`
	Denominator     int                 `json:"denominator"`
	Percentage      *float64            `json:"percentage"`
	RequestFilters  map[string][]string `json:"request_filters"`
}

type ErrorsStreamOutcome struct {
	StreamOutcome   string              `json:"stream_outcome"`
	Count           int                 `json:"count"`
	Denominator     int                 `json:"denominator"`
	Percentage      *float64            `json:"percentage"`
	LastSeenAt      time.Time           `json:"last_seen_at"`
	RequestFilters  map[string][]string `json:"request_filters"`
	ErrorKinds      []ErrorsStreamKind  `json:"error_kinds"`
	OtherErrorKinds ErrorsRemainder     `json:"other_error_kinds"`
}

type ErrorsGroup struct {
	EntityType              string              `json:"entity_type"`
	EntityID                *string             `json:"entity_id"`
	Label                   string              `json:"label"`
	Configured              *bool               `json:"configured"`
	ProblemCount            int                 `json:"problem_count"`
	FailedCount             int                 `json:"failed_count"`
	ClientDisconnectedCount int                 `json:"client_disconnected_count"`
	Denominator             int                 `json:"denominator"`
	Percentage              *float64            `json:"percentage"`
	LastSeenAt              time.Time           `json:"last_seen_at"`
	RequestFilters          map[string][]string `json:"request_filters"`
}

type ErrorsRemainder struct {
	Count          int                 `json:"count"`
	Denominator    int                 `json:"denominator"`
	Percentage     *float64            `json:"percentage"`
	RequestFilters map[string][]string `json:"request_filters"`
}

type ErrorsOther struct {
	HTTPStatuses   ErrorsRemainder `json:"http_statuses"`
	StreamOutcomes ErrorsRemainder `json:"stream_outcomes"`
	Groups         ErrorsRemainder `json:"groups"`
}

const finalResultSQL = `CASE WHEN status_code NOT BETWEEN 200 AND 299 THEN 'failed' WHEN stream_outcome = 'client_disconnected' THEN 'client_disconnected' WHEN stream_outcome IS NULL OR stream_outcome IN ('', 'not_streaming', 'completed') THEN 'completed' ELSE 'failed' END`

// errorFilterSQL builds the shared filtered cohort CTE fragment. The filter
// expressions mirror the classifier: outcome_detail is derived from the same
// CASE; pricing is not involved (final result only).
func errorFilterWhere(params UsageErrorsParams) (string, []any, error) {
	conditions := make([]string, 0, 8)
	args := make([]any, 0, 8)
	argIndex := 4 // $1 profile, $2 from, $3 to reserved
	next := func(value any) string {
		args = append(args, value)
		placeholder := fmt.Sprintf("$%d", argIndex)
		argIndex++
		return placeholder
	}
	if len(params.FinalResult) > 0 {
		for _, value := range params.FinalResult {
			trimmed := strings.TrimSpace(value)
			if trimmed != "completed" && trimmed != "failed" && trimmed != "client_disconnected" {
				return "", nil, &HTTPError{StatusCode: 422, Code: "filter_invalid", Detail: "invalid final_result: " + trimmed}
			}
		}
		placeholders := make([]string, 0, len(params.FinalResult))
		for _, value := range params.FinalResult {
			placeholders = append(placeholders, next(strings.TrimSpace(value)))
		}
		conditions = append(conditions, fmt.Sprintf("(%s) IN (%s)", finalResultSQL, strings.Join(placeholders, ", ")))
	}
	if len(params.OutcomeDetail) > 0 {
		for _, value := range params.OutcomeDetail {
			trimmed := strings.TrimSpace(value)
			if trimmed != "completed" && trimmed != "http_error" && trimmed != "stream_error" && trimmed != "client_disconnected" {
				return "", nil, &HTTPError{StatusCode: 422, Code: "filter_invalid", Detail: "invalid outcome_detail: " + trimmed}
			}
		}
		placeholders := make([]string, 0, len(params.OutcomeDetail))
		for _, value := range params.OutcomeDetail {
			placeholders = append(placeholders, next(strings.TrimSpace(value)))
		}
		conditions = append(conditions, fmt.Sprintf("(%s) IN (%s)", outcomeDetailSQL, strings.Join(placeholders, ", ")))
	}
	if len(params.StatusCode) > 0 {
		placeholders := make([]string, 0, len(params.StatusCode))
		for _, value := range params.StatusCode {
			placeholders = append(placeholders, next(value))
		}
		conditions = append(conditions, fmt.Sprintf("status_code IN (%s)", strings.Join(placeholders, ", ")))
	}
	if len(params.StreamOutcome) > 0 {
		outcomeConditions := make([]string, 0, len(params.StreamOutcome))
		validOutcomes := map[string]struct{}{
			"__null__": {}, "not_streaming": {}, "completed": {}, "gateway_timeout": {}, "provider_incomplete": {},
			"client_disconnected": {}, "upstream_read_error": {}, "upstream_ended_without_terminal": {}, "unknown": {},
		}
		for _, value := range params.StreamOutcome {
			trimmed := strings.TrimSpace(value)
			if _, ok := validOutcomes[trimmed]; !ok {
				return "", nil, &HTTPError{StatusCode: 422, Code: "filter_invalid", Detail: "invalid stream_outcome: " + trimmed}
			}
			if trimmed == "__null__" {
				outcomeConditions = append(outcomeConditions, "stream_outcome IS NULL")
			} else {
				outcomeConditions = append(outcomeConditions, fmt.Sprintf("stream_outcome = %s", next(trimmed)))
			}
		}
		conditions = append(conditions, "("+strings.Join(outcomeConditions, " OR ")+")")
	}
	if len(params.StreamErrorKind) > 0 {
		kindConditions := make([]string, 0, len(params.StreamErrorKind))
		for _, value := range params.StreamErrorKind {
			trimmed := strings.TrimSpace(value)
			if trimmed == "__null__" {
				kindConditions = append(kindConditions, "NULLIF(stream_error_kind, '') IS NULL")
			} else {
				kindConditions = append(kindConditions, fmt.Sprintf("NULLIF(stream_error_kind, '') = %s", next(trimmed)))
			}
		}
		conditions = append(conditions, "("+strings.Join(kindConditions, " OR ")+")")
	}
	if params.IngressModelID != nil && strings.TrimSpace(*params.IngressModelID) != "" {
		conditions = append(conditions, fmt.Sprintf("model_id = %s", next(strings.TrimSpace(*params.IngressModelID))))
	}
	if params.FinalTargetModelID != nil && strings.TrimSpace(*params.FinalTargetModelID) != "" {
		conditions = append(conditions, fmt.Sprintf("resolved_target_model_id = %s", next(strings.TrimSpace(*params.FinalTargetModelID))))
	}
	if params.EndpointID != nil {
		conditions = append(conditions, fmt.Sprintf("endpoint_id = %s", next(*params.EndpointID)))
	}
	if params.TerminalTargetID != nil {
		conditions = append(conditions, fmt.Sprintf("connection_id = %s", next(*params.TerminalTargetID)))
	}
	if params.APIFamily != nil && strings.TrimSpace(*params.APIFamily) != "" {
		conditions = append(conditions, fmt.Sprintf("api_family = %s", next(strings.TrimSpace(*params.APIFamily))))
	}
	if params.ProxyAPIKeyID != nil {
		conditions = append(conditions, fmt.Sprintf("proxy_api_key_id_snapshot = %s", next(*params.ProxyAPIKeyID)))
	}
	where := usageWindowPredicate
	if len(conditions) > 0 {
		where += " AND " + strings.Join(conditions, " AND ")
	}
	return where, args, nil
}

// LoadUsageErrors runs the three-statement error aggregate. Statement 1:
// filtered cohort summary + timeline. Statement 2: HTTP status ranking.
// Statement 3: stream outcome ranking with kind Top 5 and entity groups.
func LoadUsageErrors(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, usageCoverage Coverage, requestCoverage Coverage, params UsageErrorsParams, queryContext string, referenceNow time.Time) (UsageErrorsResult, error) {
	scope, err := NormalizeScope(params.Scope)
	if err != nil {
		return UsageErrorsResult{}, err
	}
	params.Scope = scope
	groupBy, err := ValidateGroupBy(scope, params.GroupBy)
	if err != nil {
		return UsageErrorsResult{}, err
	}
	if groupBy == GroupProxyAPIKey {
		return UsageErrorsResult{}, &HTTPError{StatusCode: 422, Code: "group_invalid", Detail: "group_by proxy_api_key is not supported by usage-errors"}
	}
	params.GroupBy = groupBy
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 50 {
		params.Limit = 50
	}
	if scope == ScopeRouteAttempt {
		return loadAttemptErrors(ctx, exec, profileID, bounds, requestCoverage, params, queryContext, referenceNow)
	}
	if params.GroupBy == "" {
		params.GroupBy = "none"
	}
	result := UsageErrorsResult{
		GeneratedAt:     referenceNow.UTC(),
		Coverage:        usageCoverage,
		Caliber:         CaliberForScope(scope),
		DatasetCoverage: ScopeCoverageFor(scope, &usageCoverage, &requestCoverage),
		RequestsContext: ErrorsRequestsContext{
			View:               "attempts",
			QueryContext:       queryContext,
			FinalFromTime:      bounds.UsageFrom.UTC().Format(time.RFC3339),
			FinalToTime:        bounds.UsageTo.UTC().Format(time.RFC3339),
			BaseRequestFilters: baseRequestFilters(params),
		},
		// A cohort with no errors is an empty ranking, not a missing one; the
		// response contract keeps every list a JSON array.
		Timeline:       []ErrorsTimelinePoint{},
		HTTPStatuses:   []ErrorsHTTPStatus{},
		StreamOutcomes: []ErrorsStreamOutcome{},
		Groups:         []ErrorsGroup{},
		Other: ErrorsOther{
			HTTPStatuses:   ErrorsRemainder{RequestFilters: map[string][]string{}},
			StreamOutcomes: ErrorsRemainder{RequestFilters: map[string][]string{}},
			Groups:         ErrorsRemainder{RequestFilters: map[string][]string{}},
		},
	}
	where, filterArgs, err := errorFilterWhere(params)
	if err != nil {
		return UsageErrorsResult{}, err
	}
	if scope == ScopeFinal {
		where += " AND resolved_target_model_id IS NOT NULL AND final_attempt_number IS NOT NULL"
	}
	args := append([]any{profileID, bounds.UsageFrom, bounds.UsageTo}, filterArgs...)

	// Statement 1: summary + timeline over the filtered cohort.
	rows, err := exec.Query(ctx, fmt.Sprintf(`
WITH classified AS (
	SELECT
		date_bin(interval '1 hour', created_at, $2) AS bucket_start,
		`+outcomeDetailSQL+` AS outcome_detail,
		stream_outcome,
		stream_error_kind,
		status_code,
		model_id,
		endpoint_id,
		connection_id,
		created_at
	FROM usage_request_events
	WHERE %s
)
SELECT
	(SELECT COUNT(*) FROM classified)::int,
	(SELECT COUNT(*) FROM classified WHERE outcome_detail = 'http_error')::int,
	(SELECT COUNT(*) FROM classified WHERE outcome_detail = 'stream_error')::int,
	(SELECT COUNT(*) FROM classified WHERE outcome_detail = 'client_disconnected')::int,
	(SELECT COUNT(*) FROM classified WHERE stream_outcome <> 'not_streaming' AND stream_outcome <> 'completed' AND stream_outcome <> '')::int,
	bucket_start,
	COUNT(*) FILTER (WHERE outcome_detail = 'http_error')::int,
	COUNT(*) FILTER (WHERE outcome_detail = 'stream_error')::int,
	COUNT(*) FILTER (WHERE outcome_detail = 'client_disconnected')::int
FROM classified
GROUP BY bucket_start
ORDER BY bucket_start ASC
LIMIT 400`, where), args...)
	if err != nil {
		return result, fmt.Errorf("load usage errors summary/timeline: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var point ErrorsTimelinePoint
		var bucket time.Time
		if err := rows.Scan(
			&result.Summary.RequestCount,
			&result.Summary.HTTPErrorCount,
			&result.Summary.StreamErrorCount,
			&result.Summary.ClientDisconnectedCount,
			&result.Summary.DiagnosticStreamAnomalyCount,
			&bucket,
			&point.HTTPErrorCount,
			&point.StreamErrorCount,
			&point.ClientDisconnectedCount,
		); err != nil {
			return result, err
		}
		result.Summary.FailedCount = result.Summary.HTTPErrorCount + result.Summary.StreamErrorCount
		point.FailedCount = point.HTTPErrorCount + point.StreamErrorCount
		point.BucketStart = bucket.UTC().Format(time.RFC3339)
		result.Timeline = append(result.Timeline, point)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	// Statement 2: HTTP status ranking over the http_error cohort.
	httpRows, err := exec.Query(ctx, fmt.Sprintf(`
WITH classified AS (
	SELECT status_code, created_at, model_id, endpoint_id, connection_id
	FROM usage_request_events
	WHERE %s AND `+outcomeDetailSQL+` = 'http_error'
), ranked AS (
	SELECT status_code, COUNT(*)::int AS item_count, MAX(created_at) AS last_seen_at
	FROM classified
	GROUP BY status_code
)
SELECT status_code, item_count, last_seen_at, SUM(item_count) OVER ()::int
FROM ranked
ORDER BY item_count DESC, status_code ASC
LIMIT %d`, where, params.Limit+1), args...)
	if err != nil {
		return result, fmt.Errorf("load usage errors http statuses: %w", err)
	}
	defer httpRows.Close()
	httpDenominator := 0
	for httpRows.Next() {
		var item ErrorsHTTPStatus
		if err := httpRows.Scan(&item.StatusCode, &item.Count, &item.LastSeenAt, &httpDenominator); err != nil {
			return result, err
		}
		item.LastSeenAt = item.LastSeenAt.UTC()
		result.HTTPStatuses = append(result.HTTPStatuses, item)
	}
	if err := httpRows.Err(); err != nil {
		return result, err
	}
	result.Other.HTTPStatuses.Denominator = httpDenominator
	httpListed := 0
	for index := range result.HTTPStatuses {
		if index < params.Limit {
			result.HTTPStatuses[index].Denominator = httpDenominator
			result.HTTPStatuses[index].Percentage = percentageOf(result.HTTPStatuses[index].Count, httpDenominator)
			result.HTTPStatuses[index].RequestFilters = httpStatusFilters(params, result.HTTPStatuses[index].StatusCode)
			httpListed += result.HTTPStatuses[index].Count
		}
	}
	result.Other.HTTPStatuses.Count = httpDenominator - httpListed
	result.Other.HTTPStatuses.Percentage = percentageOf(result.Other.HTTPStatuses.Count, httpDenominator)
	if len(result.HTTPStatuses) > params.Limit {
		result.HTTPStatuses = result.HTTPStatuses[:params.Limit]
	}
	if result.Other.HTTPStatuses.Count > 0 {
		result.Other.HTTPStatuses.RequestFilters = httpStatusRemainderFilters(params, result.HTTPStatuses)
	}

	// Statement 3: stream outcome ranking (abnormal outcomes) with kind Top 5,
	// plus entity groups by requested dimension.
	streamRows, err := exec.Query(ctx, fmt.Sprintf(`
WITH classified AS (
	SELECT stream_outcome, stream_error_kind, created_at, model_id, endpoint_id, connection_id
	FROM usage_request_events
	WHERE %s AND stream_outcome <> 'not_streaming' AND stream_outcome <> 'completed' AND stream_outcome <> ''
), ranked AS (
	SELECT stream_outcome, COUNT(*)::int AS item_count, MAX(created_at) AS last_seen_at
	FROM classified
	GROUP BY stream_outcome
)
SELECT stream_outcome, item_count, last_seen_at, SUM(item_count) OVER ()::int
FROM ranked
ORDER BY item_count DESC, stream_outcome ASC
LIMIT %d`, where, params.Limit+1), args...)
	if err != nil {
		return result, fmt.Errorf("load usage errors stream outcomes: %w", err)
	}
	defer streamRows.Close()
	streamDenominator := 0
	streamListed := 0
	for streamRows.Next() {
		var item ErrorsStreamOutcome
		var outcome string
		if err := streamRows.Scan(&outcome, &item.Count, &item.LastSeenAt, &streamDenominator); err != nil {
			return result, err
		}
		item.StreamOutcome = outcome
		item.LastSeenAt = item.LastSeenAt.UTC()
		if len(result.StreamOutcomes) < params.Limit {
			streamListed += item.Count
			result.StreamOutcomes = append(result.StreamOutcomes, item)
		}
	}
	if err := streamRows.Err(); err != nil {
		return result, err
	}
	result.Other.StreamOutcomes.Denominator = streamDenominator
	result.Other.StreamOutcomes.Count = streamDenominator - streamListed
	result.Other.StreamOutcomes.Percentage = percentageOf(result.Other.StreamOutcomes.Count, streamDenominator)
	if result.Other.StreamOutcomes.Count > 0 {
		result.Other.StreamOutcomes.RequestFilters = streamOutcomeRemainderFilters(params, result.StreamOutcomes)
	}

	// Kind Top 5 per listed outcome + other remainder.
	for outcomeIndex := range result.StreamOutcomes {
		outcome := result.StreamOutcomes[outcomeIndex].StreamOutcome
		kindPlaceholder := fmt.Sprintf("$%d", len(args)+1)
		kindArgs := append(append([]any(nil), args...), outcome)
		kindRows, err := exec.Query(ctx, fmt.Sprintf(`
WITH classified AS (
	SELECT NULLIF(stream_error_kind, '') AS stream_error_kind
	FROM usage_request_events
	WHERE %s AND stream_outcome = %s
)
SELECT stream_error_kind, COUNT(*)::int
FROM classified
GROUP BY stream_error_kind
ORDER BY COUNT(*) DESC, stream_error_kind ASC NULLS LAST
LIMIT 5`, where, kindPlaceholder), kindArgs...)
		if err != nil {
			return result, fmt.Errorf("load usage errors stream kinds: %w", err)
		}
		kindDenominator := result.StreamOutcomes[outcomeIndex].Count
		kindListed := 0
		result.StreamOutcomes[outcomeIndex].ErrorKinds = make([]ErrorsStreamKind, 0, 5)
		for kindRows.Next() {
			var kind ErrorsStreamKind
			var kindValue *string
			if err := kindRows.Scan(&kindValue, &kind.Count); err != nil {
				kindRows.Close()
				return result, err
			}
			kind.StreamErrorKind = kindValue
			kind.Denominator = kindDenominator
			kind.Percentage = percentageOf(kind.Count, kindDenominator)
			kind.RequestFilters = streamKindFilters(params, outcome, kindValue)
			kindListed += kind.Count
			result.StreamOutcomes[outcomeIndex].ErrorKinds = append(result.StreamOutcomes[outcomeIndex].ErrorKinds, kind)
		}
		kindRows.Close()
		result.StreamOutcomes[outcomeIndex].Denominator = streamDenominator
		result.StreamOutcomes[outcomeIndex].Percentage = percentageOf(result.StreamOutcomes[outcomeIndex].Count, streamDenominator)
		result.StreamOutcomes[outcomeIndex].RequestFilters = streamOutcomeFilters(params, outcome)
		result.StreamOutcomes[outcomeIndex].OtherErrorKinds = ErrorsRemainder{
			Count:       kindDenominator - kindListed,
			Denominator: kindDenominator,
			Percentage:  percentageOf(kindDenominator-kindListed, kindDenominator),
		}
		if result.StreamOutcomes[outcomeIndex].OtherErrorKinds.Count > 0 {
			result.StreamOutcomes[outcomeIndex].OtherErrorKinds.RequestFilters = streamKindRemainderFilters(
				params,
				outcome,
				result.StreamOutcomes[outcomeIndex].ErrorKinds,
			)
		}
	}

	// Entity groups by requested dimension.
	if params.GroupBy != "" && params.GroupBy != "none" {
		groupColumn := groupColumnFor(scope, params.GroupBy)
		groupRows, err := exec.Query(ctx, fmt.Sprintf(`
WITH classified AS (
	SELECT %[1]s AS entity_id, created_at, `+outcomeDetailSQL+` AS outcome_detail
	FROM usage_request_events
	WHERE %[2]s AND `+outcomeDetailSQL+` IN ('http_error', 'stream_error', 'client_disconnected')
), ranked AS (
	SELECT entity_id, COUNT(*)::int AS problem_count,
		COUNT(*) FILTER (WHERE outcome_detail = 'client_disconnected')::int AS client_disconnected_count,
		MAX(created_at) AS last_seen_at
	FROM classified
	GROUP BY entity_id
)
SELECT entity_id, problem_count, client_disconnected_count, last_seen_at,
	SUM(problem_count) OVER ()::int
FROM ranked
ORDER BY problem_count DESC, entity_id ASC
LIMIT %d`, groupColumn, where, params.Limit+1), args...)
		if err != nil {
			return result, fmt.Errorf("load usage errors groups: %w", err)
		}
		defer groupRows.Close()
		groupDenominator := 0
		groupListed := 0
		for groupRows.Next() {
			var item ErrorsGroup
			var entityID *string
			if err := groupRows.Scan(&entityID, &item.ProblemCount, &item.ClientDisconnectedCount, &item.LastSeenAt, &groupDenominator); err != nil {
				return result, err
			}
			if entityID == nil {
				item.EntityID = nil
				item.Label = "未归因"
			} else {
				item.EntityID = entityID
				item.Label = *entityID
			}
			item.EntityType = params.GroupBy
			item.LastSeenAt = item.LastSeenAt.UTC()
			item.FailedCount = item.ProblemCount - item.ClientDisconnectedCount
			if len(result.Groups) < params.Limit {
				groupListed += item.ProblemCount
				result.Groups = append(result.Groups, item)
			}
		}
		if err := groupRows.Err(); err != nil {
			return result, err
		}
		entityIDs := make([]string, 0, len(result.Groups))
		for _, item := range result.Groups {
			if item.EntityID != nil {
				entityIDs = append(entityIDs, *item.EntityID)
			}
		}
		labels, err := loadSeriesLabels(ctx, exec, profileID, bounds, scope, params.GroupBy, entityIDs)
		if err != nil {
			return result, err
		}
		result.Other.Groups.Denominator = groupDenominator
		result.Other.Groups.Count = groupDenominator - groupListed
		result.Other.Groups.Percentage = percentageOf(result.Other.Groups.Count, groupDenominator)
		for index := range result.Groups {
			if result.Groups[index].EntityID != nil {
				entityID := *result.Groups[index].EntityID
				if label := strings.TrimSpace(labels[entityID]); label != "" {
					result.Groups[index].Label = label
				} else {
					switch params.GroupBy {
					case GroupEndpoint:
						result.Groups[index].Label = "Endpoint #" + entityID
					case GroupTerminalTarget:
						result.Groups[index].Label = "Terminal Target #" + entityID
					case GroupProxyAPIKey:
						result.Groups[index].Label = "Proxy Key #" + entityID
					}
				}
			}
			result.Groups[index].Denominator = groupDenominator
			result.Groups[index].Percentage = percentageOf(result.Groups[index].ProblemCount, groupDenominator)
			result.Groups[index].RequestFilters = groupFilters(params, result.Groups[index])
		}
		if result.Other.Groups.Count > 0 {
			result.Other.Groups.RequestFilters = groupRemainderFilters(params, result.Groups)
		}
	}
	result.Samples = ScopeSampleCounts{ObservationCount: result.Summary.RequestCount, LatencyMissingCount: result.Summary.RequestCount}
	return result, nil
}

func percentageOf(count int, denominator int) *float64 {
	if denominator <= 0 {
		return nil
	}
	value := float64(count) * 100 / float64(denominator)
	return &value
}

// baseRequestFilters translates the active filters into canonical final_*.
func baseRequestFilters(params UsageErrorsParams) map[string][]string {
	filters := map[string][]string{}
	if len(params.FinalResult) > 0 {
		filters["final_result"] = params.FinalResult
	}
	if len(params.OutcomeDetail) > 0 {
		filters["outcome_detail"] = params.OutcomeDetail
	}
	if len(params.StatusCode) > 0 {
		values := make([]string, 0, len(params.StatusCode))
		for _, value := range params.StatusCode {
			values = append(values, fmt.Sprintf("%d", value))
		}
		filters["final_status_code"] = values
	}
	if len(params.StreamOutcome) > 0 {
		filters["final_stream_outcome"] = params.StreamOutcome
	}
	if len(params.StreamErrorKind) > 0 {
		filters["final_stream_error_kind"] = params.StreamErrorKind
	}
	if params.IngressModelID != nil && strings.TrimSpace(*params.IngressModelID) != "" {
		filters["ingress_model_id"] = []string{strings.TrimSpace(*params.IngressModelID)}
	}
	if params.FinalTargetModelID != nil && strings.TrimSpace(*params.FinalTargetModelID) != "" {
		filters["final_target_model_id"] = []string{strings.TrimSpace(*params.FinalTargetModelID)}
	}
	if params.EndpointID != nil {
		filters["final_endpoint_id"] = []string{fmt.Sprintf("%d", *params.EndpointID)}
	}
	if params.TerminalTargetID != nil {
		filters["final_terminal_target_id"] = []string{fmt.Sprintf("%d", *params.TerminalTargetID)}
	}
	if params.APIFamily != nil && strings.TrimSpace(*params.APIFamily) != "" {
		filters["api_family"] = []string{strings.TrimSpace(*params.APIFamily)}
	}
	if params.ProxyAPIKeyID != nil {
		filters["proxy_api_key_id"] = []string{fmt.Sprintf("%d", *params.ProxyAPIKeyID)}
	}
	return filters
}

func httpStatusFilters(params UsageErrorsParams, statusCode int) map[string][]string {
	filters := baseRequestFilters(params)
	filters["final_result"] = []string{"failed"}
	filters["outcome_detail"] = []string{"http_error"}
	filters["final_status_code"] = []string{fmt.Sprintf("%d", statusCode)}
	return filters
}

func streamOutcomeFilters(params UsageErrorsParams, outcome string) map[string][]string {
	filters := baseRequestFilters(params)
	filters["final_stream_outcome"] = []string{outcome}
	return filters
}

func streamKindFilters(params UsageErrorsParams, outcome string, kind *string) map[string][]string {
	filters := streamOutcomeFilters(params, outcome)
	if kind == nil {
		filters["final_stream_error_kind"] = []string{"__null__"}
	} else {
		filters["final_stream_error_kind"] = []string{*kind}
	}
	return filters
}

func finalizedExclusionFilter(facet string, visibleValues []string) []string {
	values := make([]string, 0, len(visibleValues)+1)
	values = append(values, facet)
	values = append(values, visibleValues...)
	return values
}

func httpStatusRemainderFilters(params UsageErrorsParams, visible []ErrorsHTTPStatus) map[string][]string {
	filters := baseRequestFilters(params)
	filters["final_result"] = []string{"failed"}
	filters["outcome_detail"] = []string{"http_error"}
	values := make([]string, 0, len(visible))
	for _, item := range visible {
		values = append(values, fmt.Sprintf("%d", item.StatusCode))
	}
	filters["final_exclude"] = finalizedExclusionFilter(FinalExclusionStatusCode, values)
	return filters
}

var abnormalFinalStreamOutcomes = []string{
	"gateway_timeout",
	"provider_incomplete",
	"client_disconnected",
	"upstream_read_error",
	"upstream_ended_without_terminal",
	"unknown",
}

func streamOutcomeRemainderFilters(params UsageErrorsParams, visible []ErrorsStreamOutcome) map[string][]string {
	filters := baseRequestFilters(params)
	allowed := make(map[string]struct{}, len(abnormalFinalStreamOutcomes))
	for _, value := range abnormalFinalStreamOutcomes {
		allowed[value] = struct{}{}
	}
	universe := append([]string(nil), abnormalFinalStreamOutcomes...)
	if len(params.StreamOutcome) > 0 {
		universe = universe[:0]
		seen := map[string]struct{}{}
		for _, raw := range params.StreamOutcome {
			value := strings.TrimSpace(raw)
			if _, ok := allowed[value]; !ok {
				continue
			}
			if _, duplicate := seen[value]; duplicate {
				continue
			}
			seen[value] = struct{}{}
			universe = append(universe, value)
		}
	}
	filters["final_stream_outcome"] = universe
	values := make([]string, 0, len(visible))
	for _, item := range visible {
		values = append(values, item.StreamOutcome)
	}
	filters["final_exclude"] = finalizedExclusionFilter(FinalExclusionStreamOutcome, values)
	return filters
}

func streamKindRemainderFilters(params UsageErrorsParams, outcome string, visible []ErrorsStreamKind) map[string][]string {
	filters := streamOutcomeFilters(params, outcome)
	values := make([]string, 0, len(visible))
	for _, item := range visible {
		if item.StreamErrorKind == nil || strings.TrimSpace(*item.StreamErrorKind) == "" {
			values = append(values, "__null__")
		} else {
			values = append(values, *item.StreamErrorKind)
		}
	}
	filters["final_exclude"] = finalizedExclusionFilter(FinalExclusionStreamErrorKind, values)
	return filters
}

func groupFilters(params UsageErrorsParams, group ErrorsGroup) map[string][]string {
	filters := baseRequestFilters(params)
	if _, ok := filters["final_result"]; !ok {
		filters["final_result"] = []string{"failed", "client_disconnected"}
	}
	if group.EntityID != nil {
		switch group.EntityType {
		case GroupAPIFamily:
			filters["api_family"] = []string{*group.EntityID}
		case GroupIngressModel:
			filters["ingress_model_id"] = []string{*group.EntityID}
		case GroupFinalTargetModel:
			filters["final_target_model_id"] = []string{*group.EntityID}
		case "endpoint":
			filters["final_endpoint_id"] = []string{*group.EntityID}
		case "terminal_target":
			filters["final_terminal_target_id"] = []string{*group.EntityID}
		}
	} else {
		switch group.EntityType {
		case GroupAPIFamily:
			filters["api_family"] = []string{"__null__"}
		case GroupIngressModel:
			filters["ingress_model_id"] = []string{"__null__"}
		case GroupFinalTargetModel:
			filters["final_target_model_id"] = []string{"__null__"}
		case "endpoint":
			filters["final_endpoint_id"] = []string{"__null__"}
		case "terminal_target":
			filters["final_terminal_target_id"] = []string{"__null__"}
		}
	}
	return filters
}

func groupExclusionFacet(groupBy string) string {
	switch groupBy {
	case GroupAPIFamily:
		return FinalExclusionAPIFamily
	case GroupIngressModel:
		return FinalExclusionIngressModel
	case GroupFinalTargetModel:
		return FinalExclusionFinalTargetModel
	case GroupEndpoint:
		return FinalExclusionFinalEndpoint
	case GroupTerminalTarget:
		return FinalExclusionFinalTerminalTarget
	default:
		return ""
	}
}

func groupRemainderFilters(params UsageErrorsParams, visible []ErrorsGroup) map[string][]string {
	filters := baseRequestFilters(params)
	if _, filtered := filters["final_result"]; !filtered {
		filters["final_result"] = []string{"failed", "client_disconnected"}
	}
	facet := groupExclusionFacet(params.GroupBy)
	if facet == "" {
		return filters
	}
	values := make([]string, 0, len(visible))
	for _, item := range visible {
		if item.EntityID == nil || strings.TrimSpace(*item.EntityID) == "" {
			values = append(values, "__null__")
		} else {
			values = append(values, *item.EntityID)
		}
	}
	filters["final_exclude"] = finalizedExclusionFilter(facet, values)
	return filters
}

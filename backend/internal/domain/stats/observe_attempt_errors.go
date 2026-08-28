package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type attemptErrorGroupAccumulator struct {
	key                string
	entityID           *string
	label              string
	problemCount       int
	failedCount        int
	clientDisconnected int
	lastSeenAt         time.Time
}

type attemptStreamAccumulator struct {
	outcome    string
	count      int
	lastSeenAt time.Time
	kinds      map[string]int
}

func loadAttemptErrors(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, coverage Coverage, params UsageErrorsParams, queryContext string, referenceNow time.Time) (UsageErrorsResult, error) {
	result := UsageErrorsResult{
		GeneratedAt: referenceNow.UTC(), Coverage: coverage, Caliber: CaliberForScope(ScopeRouteAttempt),
		DatasetCoverage: DatasetCoverage{RequestLogs: &coverage}, Timeline: []ErrorsTimelinePoint{}, HTTPStatuses: []ErrorsHTTPStatus{}, StreamOutcomes: []ErrorsStreamOutcome{}, Groups: []ErrorsGroup{},
		Other: ErrorsOther{
			HTTPStatuses:   ErrorsRemainder{RequestFilters: map[string][]string{}},
			StreamOutcomes: ErrorsRemainder{RequestFilters: map[string][]string{}},
			Groups:         ErrorsRemainder{RequestFilters: map[string][]string{}},
		},
		RequestsContext: ErrorsRequestsContext{
			View: "attempts", QueryContext: queryContext,
			FinalFromTime: bounds.UsageFrom.UTC().Format(time.RFC3339), FinalToTime: bounds.UsageTo.UTC().Format(time.RFC3339),
			BaseRequestFilters: attemptBaseRequestFilters(params),
		},
	}
	where, args, err := attemptErrorWhere(profileID, bounds, params)
	if err != nil {
		return result, err
	}
	rows, err := exec.Query(ctx, `SELECT created_at, resolved_target_model_id, endpoint_id, connection_id, api_family,
		attempt_trigger, attempt_result, upstream_status_code, stream_outcome, stream_error_kind, attempt_duration_ms
		FROM request_logs WHERE `+where+` ORDER BY created_at ASC, id ASC`, args...)
	if err != nil {
		return result, fmt.Errorf("load route-attempt errors: %w", err)
	}
	defer rows.Close()

	timeline := map[time.Time]*ErrorsTimelinePoint{}
	statusCounts := map[int]*ErrorsHTTPStatus{}
	streams := map[string]*attemptStreamAccumulator{}
	groups := map[string]*attemptErrorGroupAccumulator{}
	latencySamples := 0
	for rows.Next() {
		var createdAt time.Time
		var target, apiFamily, trigger, attemptResult, streamOutcome, streamErrorKind sql.NullString
		var endpointID, connectionID, statusCode, duration sql.NullInt32
		if err := rows.Scan(&createdAt, &target, &endpointID, &connectionID, &apiFamily, &trigger, &attemptResult, &statusCode, &streamOutcome, &streamErrorKind, &duration); err != nil {
			return result, err
		}
		createdAt = createdAt.UTC()
		result.Summary.RequestCount++
		if duration.Valid && duration.Int32 >= 0 {
			latencySamples++
		}
		class := classifyAttemptResult(attemptResult)
		problem := attemptClassIsProblem(class)
		switch class {
		case attemptClassHTTPError:
			result.Summary.HTTPErrorCount++
			result.Summary.FailedCount++
		case attemptClassStreamError:
			result.Summary.StreamErrorCount++
			result.Summary.FailedCount++
		case attemptClassTransportError:
			result.Summary.TransportErrorCount++
			result.Summary.FailedCount++
		case attemptClassClientDisconnected:
			result.Summary.ClientDisconnectedCount++
		case attemptClassUnknown:
			result.Summary.FailedCount++
		}
		if !problem {
			continue
		}

		bucket := createdAt.Truncate(time.Hour)
		point := timeline[bucket]
		if point == nil {
			point = &ErrorsTimelinePoint{BucketStart: bucket.Format(time.RFC3339)}
			timeline[bucket] = point
		}
		switch class {
		case attemptClassHTTPError:
			point.HTTPErrorCount++
			point.FailedCount++
		case attemptClassStreamError:
			point.StreamErrorCount++
			point.FailedCount++
		case attemptClassTransportError:
			point.TransportErrorCount++
			point.FailedCount++
		case attemptClassClientDisconnected:
			point.ClientDisconnectedCount++
		case attemptClassUnknown:
			point.FailedCount++
		}

		// Status ranking is a diagnostic facet over problem attempts. It does
		// not reclassify a stream, transport, or disconnect as an HTTP error.
		if statusCode.Valid {
			status := statusCounts[int(statusCode.Int32)]
			if status == nil {
				status = &ErrorsHTTPStatus{StatusCode: int(statusCode.Int32)}
				statusCounts[int(statusCode.Int32)] = status
			}
			status.Count++
			status.LastSeenAt = createdAt
		}

		if abnormalAttemptStreamOutcome(class, streamOutcome) {
			outcomeKey := attemptNullGroupKey
			if streamOutcome.Valid && strings.TrimSpace(streamOutcome.String) != "" {
				outcomeKey = strings.TrimSpace(streamOutcome.String)
			}
			stream := streams[outcomeKey]
			if stream == nil {
				stream = &attemptStreamAccumulator{outcome: outcomeKey, kinds: map[string]int{}}
				streams[outcomeKey] = stream
			}
			stream.count++
			stream.lastSeenAt = createdAt
			kindKey := attemptNullGroupKey
			if streamErrorKind.Valid && strings.TrimSpace(streamErrorKind.String) != "" {
				kindKey = strings.TrimSpace(streamErrorKind.String)
			}
			stream.kinds[kindKey]++
			result.Summary.DiagnosticStreamAnomalyCount++
		}

		groupKey, label, entityID := attemptErrorGroupIdentity(params.GroupBy, target, endpointID, connectionID, apiFamily, trigger, attemptResult)
		group := groups[groupKey]
		if group == nil {
			group = &attemptErrorGroupAccumulator{key: groupKey, entityID: entityID, label: label}
			groups[groupKey] = group
		}
		group.problemCount++
		if class == attemptClassClientDisconnected {
			group.clientDisconnected++
		} else {
			group.failedCount++
		}
		group.lastSeenAt = createdAt
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Samples = ScopeSampleCounts{
		ObservationCount: result.Summary.RequestCount, LatencySampleCount: latencySamples,
		LatencyMissingCount: result.Summary.RequestCount - latencySamples,
	}

	for _, point := range timeline {
		result.Timeline = append(result.Timeline, *point)
	}
	sort.Slice(result.Timeline, func(i, j int) bool { return result.Timeline[i].BucketStart < result.Timeline[j].BucketStart })
	populateAttemptStatusRanking(&result, statusCounts, params)
	populateAttemptStreamRanking(&result, streams, params)
	if params.GroupBy != GroupNone {
		if err := populateAttemptGroupRanking(ctx, exec, profileID, bounds, &result, groups, params); err != nil {
			return result, err
		}
	}
	return result, nil
}

func populateAttemptStatusRanking(result *UsageErrorsResult, counts map[int]*ErrorsHTTPStatus, params UsageErrorsParams) {
	items := make([]ErrorsHTTPStatus, 0, len(counts))
	denominator := 0
	for _, item := range counts {
		denominator += item.Count
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].StatusCode < items[j].StatusCode
	})
	visible := len(items)
	if visible > params.Limit {
		visible = params.Limit
	}
	listed := 0
	hiddenStatuses := make([]string, 0, len(items)-visible)
	for index := range items {
		if index < visible {
			items[index].Denominator = denominator
			items[index].Percentage = percentageOf(items[index].Count, denominator)
			items[index].RequestFilters = attemptHTTPStatusFilters(params, items[index].StatusCode)
			listed += items[index].Count
		} else {
			hiddenStatuses = append(hiddenStatuses, fmt.Sprintf("%d", items[index].StatusCode))
		}
	}
	result.HTTPStatuses = append(result.HTTPStatuses, items[:visible]...)
	result.Other.HTTPStatuses.Count = denominator - listed
	result.Other.HTTPStatuses.Denominator = denominator
	result.Other.HTTPStatuses.Percentage = percentageOf(denominator-listed, denominator)
	result.Other.HTTPStatuses.RequestFilters = attemptProblemFilters(params)
	if len(hiddenStatuses) > 0 {
		result.Other.HTTPStatuses.RequestFilters["status_code"] = hiddenStatuses
	}
}

func populateAttemptStreamRanking(result *UsageErrorsResult, streams map[string]*attemptStreamAccumulator, params UsageErrorsParams) {
	items := make([]*attemptStreamAccumulator, 0, len(streams))
	denominator := 0
	for _, item := range streams {
		denominator += item.count
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].outcome < items[j].outcome
	})
	visible := len(items)
	if visible > params.Limit {
		visible = params.Limit
	}
	listed := 0
	hiddenOutcomes := make([]string, 0, len(items)-visible)
	for index, aggregate := range items {
		if index >= visible {
			hiddenOutcomes = append(hiddenOutcomes, aggregate.outcome)
			continue
		}
		item := ErrorsStreamOutcome{
			StreamOutcome: aggregate.outcome, Count: aggregate.count, Denominator: denominator,
			Percentage: percentageOf(aggregate.count, denominator), LastSeenAt: aggregate.lastSeenAt,
			RequestFilters: attemptStreamOutcomeFilters(params, aggregate.outcome), ErrorKinds: []ErrorsStreamKind{},
			OtherErrorKinds: ErrorsRemainder{RequestFilters: map[string][]string{}},
		}
		listed += aggregate.count
		type kindCount struct {
			key   string
			count int
		}
		kinds := make([]kindCount, 0, len(aggregate.kinds))
		for key, count := range aggregate.kinds {
			kinds = append(kinds, kindCount{key: key, count: count})
		}
		sort.Slice(kinds, func(i, j int) bool {
			if kinds[i].count != kinds[j].count {
				return kinds[i].count > kinds[j].count
			}
			return kinds[i].key < kinds[j].key
		})
		kindVisible := len(kinds)
		if kindVisible > 5 {
			kindVisible = 5
		}
		kindListed := 0
		hiddenKinds := make([]string, 0, len(kinds)-kindVisible)
		for kindIndex, kind := range kinds {
			if kindIndex >= kindVisible {
				hiddenKinds = append(hiddenKinds, kind.key)
				continue
			}
			var value *string
			if kind.key != attemptNullGroupKey {
				resolved := kind.key
				value = &resolved
			}
			item.ErrorKinds = append(item.ErrorKinds, ErrorsStreamKind{
				StreamErrorKind: value, Count: kind.count, Denominator: aggregate.count,
				Percentage: percentageOf(kind.count, aggregate.count), RequestFilters: attemptStreamKindFilters(params, aggregate.outcome, value),
			})
			kindListed += kind.count
		}
		item.OtherErrorKinds.Count = aggregate.count - kindListed
		item.OtherErrorKinds.Denominator = aggregate.count
		item.OtherErrorKinds.Percentage = percentageOf(aggregate.count-kindListed, aggregate.count)
		item.OtherErrorKinds.RequestFilters = attemptStreamOutcomeFilters(params, aggregate.outcome)
		if len(hiddenKinds) > 0 {
			item.OtherErrorKinds.RequestFilters["stream_error_kind"] = hiddenKinds
		}
		result.StreamOutcomes = append(result.StreamOutcomes, item)
	}
	result.Other.StreamOutcomes.Count = denominator - listed
	result.Other.StreamOutcomes.Denominator = denominator
	result.Other.StreamOutcomes.Percentage = percentageOf(denominator-listed, denominator)
	result.Other.StreamOutcomes.RequestFilters = attemptProblemFilters(params)
	if len(hiddenOutcomes) > 0 {
		result.Other.StreamOutcomes.RequestFilters["stream_outcome"] = hiddenOutcomes
	}
}

func populateAttemptGroupRanking(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, result *UsageErrorsResult, groups map[string]*attemptErrorGroupAccumulator, params UsageErrorsParams) error {
	items := make([]*attemptErrorGroupAccumulator, 0, len(groups))
	denominator := 0
	for _, item := range groups {
		denominator += item.problemCount
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].problemCount != items[j].problemCount {
			return items[i].problemCount > items[j].problemCount
		}
		return items[i].key < items[j].key
	})
	visible := len(items)
	if visible > params.Limit {
		visible = params.Limit
	}
	labelIDs := make([]string, 0, visible)
	for _, item := range items[:visible] {
		if item.entityID != nil {
			labelIDs = append(labelIDs, *item.entityID)
		}
	}
	labels, err := loadSeriesLabels(ctx, exec, profileID, bounds, ScopeRouteAttempt, params.GroupBy, labelIDs)
	if err != nil {
		return err
	}
	listed := 0
	hiddenIDs := make([]string, 0, len(items)-visible)
	for index, aggregate := range items {
		item := ErrorsGroup{
			EntityType: params.GroupBy, EntityID: aggregate.entityID, Label: aggregate.label,
			ProblemCount: aggregate.problemCount, FailedCount: aggregate.failedCount,
			ClientDisconnectedCount: aggregate.clientDisconnected, Denominator: denominator,
			Percentage: percentageOf(aggregate.problemCount, denominator), LastSeenAt: aggregate.lastSeenAt,
		}
		if item.EntityID != nil {
			if label := strings.TrimSpace(labels[*item.EntityID]); label != "" {
				item.Label = label
			} else if params.GroupBy == GroupEndpoint {
				item.Label = "Endpoint #" + *item.EntityID
			} else if params.GroupBy == GroupTerminalTarget {
				item.Label = "Terminal Target #" + *item.EntityID
			}
		}
		if index < visible {
			item.RequestFilters = attemptGroupFilters(params, item)
			result.Groups = append(result.Groups, item)
			listed += item.ProblemCount
		} else if aggregate.entityID == nil {
			hiddenIDs = append(hiddenIDs, "__null__")
		} else {
			hiddenIDs = append(hiddenIDs, *aggregate.entityID)
		}
	}
	result.Other.Groups.Count = denominator - listed
	result.Other.Groups.Denominator = denominator
	result.Other.Groups.Percentage = percentageOf(denominator-listed, denominator)
	result.Other.Groups.RequestFilters = attemptProblemFilters(params)
	if len(hiddenIDs) > 0 {
		if key := attemptGroupRequestFilterKey(params.GroupBy); key != "" {
			result.Other.Groups.RequestFilters[key] = hiddenIDs
		}
	}
	return nil
}

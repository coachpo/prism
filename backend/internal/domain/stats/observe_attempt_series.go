package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type attemptSeriesAggregate struct {
	requestCount      int
	successCount      int
	httpErrorCount    int
	streamErrorCount  int
	transportCount    int
	unknownErrorCount int
	clientDisconnects int
	latencies         []int
}

const attemptNullGroupKey = "__null__"

func loadAttemptSeries(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, coverage Coverage, metric string, groupBy string, interval string, seriesLimit int, referenceNow time.Time) (UsageSeriesResult, error) {
	groupBy, err := ValidateGroupBy(ScopeRouteAttempt, groupBy)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	normalizedMetric, err := NormalizeMetric(ScopeRouteAttempt, metric)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	metric = normalizedMetric
	intervalName, bucketSize, err := ResolveSeriesInterval(interval, bounds.UsageFrom, bounds.UsageTo)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	if seriesLimit < 2 || seriesLimit > 6 {
		seriesLimit = 6
	}
	rows, err := exec.Query(ctx, `SELECT created_at, resolved_target_model_id, endpoint_id, connection_id, api_family, attempt_trigger, attempt_result, attempt_duration_ms
		FROM request_logs
		WHERE profile_id = $1 AND row_kind = 'upstream' AND created_at >= $2 AND created_at < $3
		ORDER BY created_at ASC, id ASC`, profileID, bounds.UsageFrom.UTC(), bounds.UsageTo.UTC())
	if err != nil {
		return UsageSeriesResult{}, fmt.Errorf("load attempt series rows: %w", err)
	}
	defer rows.Close()
	byEntity := map[string]map[time.Time]*attemptSeriesAggregate{}
	entityTotals := map[string]int{}
	latencySamples, latencyMissing, observations := 0, 0, 0
	for rows.Next() {
		var createdAt time.Time
		var target, apiFamily, trigger, result sql.NullString
		var endpointID, connectionID, duration sql.NullInt32
		if err := rows.Scan(&createdAt, &target, &endpointID, &connectionID, &apiFamily, &trigger, &result, &duration); err != nil {
			return UsageSeriesResult{}, err
		}
		key := attemptSeriesGroupKey(groupBy, target, endpointID, connectionID, apiFamily, trigger, result)
		bucket := dateBinUTC(createdAt, bounds.UsageFrom, bucketSize)
		if byEntity[key] == nil {
			byEntity[key] = map[time.Time]*attemptSeriesAggregate{}
		}
		if byEntity[key][bucket] == nil {
			byEntity[key][bucket] = &attemptSeriesAggregate{}
		}
		aggregate := byEntity[key][bucket]
		aggregate.requestCount++
		observations++
		entityTotals[key]++
		switch classifyAttemptResult(result) {
		case attemptClassCompleted:
			aggregate.successCount++
		case attemptClassHTTPError:
			aggregate.httpErrorCount++
		case attemptClassStreamError:
			aggregate.streamErrorCount++
		case attemptClassTransportError:
			aggregate.transportCount++
		case attemptClassClientDisconnected:
			aggregate.clientDisconnects++
		case attemptClassUnknown:
			aggregate.unknownErrorCount++
		}
		if duration.Valid && duration.Int32 >= 0 {
			aggregate.latencies = append(aggregate.latencies, int(duration.Int32))
			latencySamples++
		} else {
			latencyMissing++
		}
	}
	if err := rows.Err(); err != nil {
		return UsageSeriesResult{}, err
	}
	keys := make([]string, 0, len(entityTotals))
	for key := range entityTotals {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if entityTotals[keys[i]] != entityTotals[keys[j]] {
			return entityTotals[keys[i]] > entityTotals[keys[j]]
		}
		return keys[i] < keys[j]
	})
	visibleKeys := keys
	remainderKeys := []string{}
	truncated := false
	if groupBy != GroupNone {
		visibleSlots := seriesLimit - 1
		if len(keys) > visibleSlots {
			truncated = true
			visibleKeys = keys[:visibleSlots]
			remainderKeys = keys[visibleSlots:]
		}
	}
	labelIDs := make([]string, 0, len(visibleKeys))
	for _, key := range visibleKeys {
		if key != "total" && key != attemptNullGroupKey {
			labelIDs = append(labelIDs, key)
		}
	}
	labels, err := loadSeriesLabels(ctx, exec, profileID, bounds, ScopeRouteAttempt, groupBy, labelIDs)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	series := make([]SeriesItem, 0, len(visibleKeys)+1)
	for _, key := range visibleKeys {
		series = append(series, buildAttemptSeriesItem(groupBy, key, entityTotals[key], byEntity[key], labels))
	}
	if truncated {
		otherBuckets := map[time.Time]*attemptSeriesAggregate{}
		otherCount := 0
		for _, key := range remainderKeys {
			otherCount += entityTotals[key]
			for bucket, aggregate := range byEntity[key] {
				merged := otherBuckets[bucket]
				if merged == nil {
					merged = &attemptSeriesAggregate{}
					otherBuckets[bucket] = merged
				}
				mergeAttemptSeriesAggregate(merged, aggregate)
			}
		}
		other := buildAttemptSeriesItem(groupBy, "other", otherCount, otherBuckets, nil)
		other.Key = "other"
		other.Label = "Other"
		other.EntityID = nil
		other.Configured = nil
		series = append(series, other)
	}
	return UsageSeriesResult{
		GeneratedAt: referenceNow.UTC(), Coverage: coverage, Metric: metric, GroupBy: groupBy,
		SelectionBasis: "attempt_count", Interval: intervalName, SeriesLimit: seriesLimit, Truncated: truncated, Series: series,
		Caliber: CaliberForScope(ScopeRouteAttempt), DatasetCoverage: DatasetCoverage{RequestLogs: &coverage},
		Samples: ScopeSampleCounts{ObservationCount: observations, LatencySampleCount: latencySamples, LatencyMissingCount: latencyMissing},
	}, nil
}

func buildAttemptSeriesItem(groupBy string, key string, requestCount int, bucketMap map[time.Time]*attemptSeriesAggregate, labels map[string]string) SeriesItem {
	item := SeriesItem{Key: scopeSeriesKey(ScopeRouteAttempt, groupBy, key), Label: key, RequestCount: requestCount, Points: []SeriesPoint{}}
	if groupBy == GroupNone || key == "total" {
		item.Key = "total"
		item.Label = "Total"
	} else if key == attemptNullGroupKey {
		item.Label = attemptNullGroupLabel(groupBy)
	} else if key != "other" {
		entityKey := key
		item.EntityID = &entityKey
		item.Label = seriesLabel(labels, key)
		configured := true
		item.Configured = &configured
	}
	buckets := make([]time.Time, 0, len(bucketMap))
	for bucket := range bucketMap {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Before(buckets[j]) })
	for _, bucket := range buckets {
		aggregate := bucketMap[bucket]
		failed := aggregate.httpErrorCount + aggregate.streamErrorCount + aggregate.transportCount + aggregate.unknownErrorCount
		reconciliation := NewPricingReconciliation()
		FinalizePricingReconciliation(&reconciliation)
		point := SeriesPoint{
			BucketStart: bucket.UTC().Format(time.RFC3339), RequestCount: aggregate.requestCount,
			HTTPSuccessCount: aggregate.successCount + aggregate.streamErrorCount + aggregate.clientDisconnects,
			HTTPFailedCount:  aggregate.httpErrorCount, FailedCount: failed,
			ClientDisconnectedCount: aggregate.clientDisconnects,
			TTFTSampleCount:         len(aggregate.latencies), P50TTFTMS: percentileContInt(aggregate.latencies, 0.50), P95TTFTMS: percentileContInt(aggregate.latencies, 0.95),
			PricingReconciliation: reconciliation,
		}
		item.Points = append(item.Points, point)
	}
	return item
}

func mergeAttemptSeriesAggregate(destination *attemptSeriesAggregate, source *attemptSeriesAggregate) {
	destination.requestCount += source.requestCount
	destination.successCount += source.successCount
	destination.httpErrorCount += source.httpErrorCount
	destination.streamErrorCount += source.streamErrorCount
	destination.transportCount += source.transportCount
	destination.unknownErrorCount += source.unknownErrorCount
	destination.clientDisconnects += source.clientDisconnects
	destination.latencies = append(destination.latencies, source.latencies...)
}

func attemptNullGroupLabel(groupBy string) string {
	switch groupBy {
	case GroupEndpoint, GroupTerminalTarget, GroupAttemptTargetModel:
		return "Unattributed"
	default:
		return "Unknown"
	}
}

func attemptSeriesGroupKey(groupBy string, target sql.NullString, endpointID sql.NullInt32, connectionID sql.NullInt32, apiFamily sql.NullString, trigger sql.NullString, result sql.NullString) string {
	switch groupBy {
	case GroupAttemptTargetModel:
		if target.Valid && strings.TrimSpace(target.String) != "" {
			return strings.TrimSpace(target.String)
		}
		return attemptNullGroupKey
	case GroupEndpoint:
		if endpointID.Valid && endpointID.Int32 > 0 {
			return fmt.Sprintf("%d", endpointID.Int32)
		}
		return attemptNullGroupKey
	case GroupTerminalTarget:
		if connectionID.Valid && connectionID.Int32 > 0 {
			return fmt.Sprintf("%d", connectionID.Int32)
		}
		return attemptNullGroupKey
	case GroupAPIFamily:
		return coalesceSeriesString(apiFamily, attemptNullGroupKey)
	case GroupAttemptTrigger:
		return coalesceSeriesString(trigger, attemptNullGroupKey)
	case GroupAttemptResult:
		return coalesceSeriesString(result, attemptNullGroupKey)
	default:
		return "total"
	}
}

func coalesceSeriesString(value sql.NullString, fallback string) string {
	if value.Valid && strings.TrimSpace(value.String) != "" {
		return strings.TrimSpace(value.String)
	}
	return fallback
}

func scopeSeriesKey(scope string, groupBy string, key string) string {
	if groupBy == GroupNone {
		return "total"
	}
	return scope + ":" + groupBy + ":" + key
}

func dateBinUTC(value time.Time, origin time.Time, size time.Duration) time.Time {
	if size <= 0 {
		return value.UTC()
	}
	delta := value.UTC().Sub(origin.UTC())
	if delta < 0 {
		return origin.UTC()
	}
	return origin.UTC().Add((delta / size) * size)
}

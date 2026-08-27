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
	requestCount int
	successCount int
	failedCount  int
	latencies    []int
}

func loadAttemptSeries(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, coverage Coverage, metric string, groupBy string, interval string, seriesLimit int, referenceNow time.Time) (UsageSeriesResult, error) {
	groupBy, err := ValidateGroupBy(ScopeRouteAttempt, groupBy)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	switch strings.TrimSpace(metric) {
	case "", "attempts", "errors", "attempt_latency":
	default:
		return UsageSeriesResult{}, &HTTPError{StatusCode: 422, Code: "metric_invalid", Detail: fmt.Sprintf("metric %q not allowed for scope %q", metric, ScopeRouteAttempt)}
	}
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
		if result.Valid && result.String == "completed" {
			aggregate.successCount++
		} else if !result.Valid || result.String != "cancelled" {
			aggregate.failedCount++
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
	truncated := len(keys) > seriesLimit
	if truncated {
		keys = keys[:seriesLimit]
	}
	series := make([]SeriesItem, 0, len(keys))
	for _, key := range keys {
		entityKey := key
		item := SeriesItem{Key: scopeSeriesKey(ScopeRouteAttempt, groupBy, key), Label: key, RequestCount: entityTotals[key], Points: []SeriesPoint{}}
		if key != "total" && key != "unattributed" && key != "unknown" {
			item.EntityID = &entityKey
		}
		buckets := make([]time.Time, 0, len(byEntity[key]))
		for bucket := range byEntity[key] {
			buckets = append(buckets, bucket)
		}
		sort.Slice(buckets, func(i, j int) bool { return buckets[i].Before(buckets[j]) })
		for _, bucket := range buckets {
			aggregate := byEntity[key][bucket]
			point := SeriesPoint{
				BucketStart: bucket.UTC().Format(time.RFC3339), RequestCount: aggregate.requestCount,
				HTTPSuccessCount: aggregate.successCount, HTTPFailedCount: aggregate.failedCount, FailedCount: aggregate.failedCount,
				TTFTSampleCount: len(aggregate.latencies), P50TTFTMS: percentileContInt(aggregate.latencies, 0.50), P95TTFTMS: percentileContInt(aggregate.latencies, 0.95),
				PricingReconciliation: NewPricingReconciliation(),
			}
			item.Points = append(item.Points, point)
		}
		series = append(series, item)
	}
	return UsageSeriesResult{
		GeneratedAt: referenceNow.UTC(), Coverage: coverage, Metric: metric, GroupBy: groupBy,
		SelectionBasis: "attempt_count", Interval: intervalName, SeriesLimit: seriesLimit, Truncated: truncated, Series: series,
		Caliber: CaliberForScope(ScopeRouteAttempt), DatasetCoverage: DatasetCoverage{RequestLogs: &coverage},
		Samples: ScopeSampleCounts{ObservationCount: observations, LatencySampleCount: latencySamples, LatencyMissingCount: latencyMissing},
	}, nil
}

func attemptSeriesGroupKey(groupBy string, target sql.NullString, endpointID sql.NullInt32, connectionID sql.NullInt32, apiFamily sql.NullString, trigger sql.NullString, result sql.NullString) string {
	switch groupBy {
	case GroupAttemptTargetModel:
		if target.Valid && strings.TrimSpace(target.String) != "" {
			return strings.TrimSpace(target.String)
		}
		return "unattributed"
	case GroupEndpoint:
		if endpointID.Valid && endpointID.Int32 > 0 {
			return fmt.Sprintf("%d", endpointID.Int32)
		}
		return "unattributed"
	case GroupTerminalTarget:
		if connectionID.Valid && connectionID.Int32 > 0 {
			return fmt.Sprintf("%d", connectionID.Int32)
		}
		return "unattributed"
	case GroupAPIFamily:
		return stringValue(nullableString(apiFamily))
	case GroupAttemptTrigger:
		return coalesceSeriesString(trigger, "unknown")
	case GroupAttemptResult:
		return coalesceSeriesString(result, "unknown")
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

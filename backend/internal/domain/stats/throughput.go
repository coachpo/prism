package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ConnectionSuccessRateParams struct {
	ProfileID    int
	FromTime     *time.Time
	ToTime       *time.Time
	ReferenceNow time.Time
}

func GetConnectionSuccessRates(ctx context.Context, exec queryExecutor, params ConnectionSuccessRateParams) ([]ConnectionSuccessRate, error) {
	referenceNow := params.ReferenceNow.UTC()
	if referenceNow.IsZero() {
		referenceNow = time.Now().UTC()
	}
	preset := "24h"
	if params.FromTime != nil || params.ToTime != nil {
		preset = "custom"
	}
	bounds, coverage, err := ResolveDatasetCoverage(ctx, exec, "request_logs", preset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return nil, err
	}
	params.FromTime, params.ToTime = &bounds.UsageFrom, &bounds.UsageTo
	query, args := buildConnectionSuccessRatesQuery(params)
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query connection success rates for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	aggregates := map[int]*ConnectionSuccessRate{}
	for rows.Next() {
		var connectionID int
		var attemptResult sql.NullString
		var duration sql.NullInt32
		if err := rows.Scan(&connectionID, &attemptResult, &duration); err != nil {
			return nil, fmt.Errorf("scan connection success rate row: %w", err)
		}
		item := aggregates[connectionID]
		if item == nil {
			aggregates[connectionID] = &ConnectionSuccessRate{ConnectionID: connectionID}
			item = aggregates[connectionID]
		}
		item.TotalRequests++
		if attemptResult.Valid && attemptResult.String == "completed" {
			item.SuccessCount++
		}
		if duration.Valid {
			item.Samples.LatencySampleCount++
		} else {
			item.Samples.LatencyMissingCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection success rate rows for profile %d: %w", params.ProfileID, err)
	}
	items := make([]ConnectionSuccessRate, 0, len(aggregates))
	for _, item := range aggregates {
		item.ErrorCount = item.TotalRequests - item.SuccessCount
		rate := successRate(item.SuccessCount, item.TotalRequests)
		item.SuccessRate = &rate
		item.Caliber = CaliberForScope(ScopeRouteAttempt)
		item.Coverage = DatasetCoverage{RequestLogs: &coverage}
		item.Samples.ObservationCount = item.TotalRequests
		items = append(items, *item)
	}
	sort.Slice(items, func(i int, j int) bool { return items[i].ConnectionID < items[j].ConnectionID })
	return items, nil
}

func buildConnectionSuccessRatesQuery(params ConnectionSuccessRateParams) (string, []any) {
	clauses := []string{"profile_id = $1", "row_kind = 'upstream'", "connection_id > 0"}
	args := []any{params.ProfileID}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	return `SELECT connection_id, attempt_result, attempt_duration_ms FROM request_logs WHERE ` + strings.Join(clauses, " AND "), args
}

type ThroughputParams struct {
	ProfileID            int
	FromTime             *time.Time
	ToTime               *time.Time
	Preset               string
	ReferenceNow         time.Time
	IngressModelID       *string
	FinalTargetModelID   *string
	AttemptTargetModelID *string
	APIFamily            *string
	EndpointID           *int
	ConnectionID         *int
	Scope                string
}

func GetThroughput(ctx context.Context, exec queryExecutor, params ThroughputParams) (ThroughputStatsResponse, error) {
	scope, err := NormalizeScope(params.Scope)
	if err != nil {
		return ThroughputStatsResponse{}, err
	}
	referenceNow := params.ReferenceNow.UTC()
	if referenceNow.IsZero() {
		referenceNow = time.Now().UTC()
	}
	preset := params.Preset
	if params.FromTime != nil || params.ToTime != nil {
		preset = "custom"
	}
	domain := "usage_request_events"
	if scope == ScopeRouteAttempt {
		domain = "request_logs"
	}
	bounds, coverage, err := ResolveDatasetCoverage(ctx, exec, domain, preset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return ThroughputStatsResponse{}, err
	}
	params.FromTime, params.ToTime = &bounds.UsageFrom, &bounds.UsageTo
	var rows []time.Time
	if scope == ScopeRouteAttempt {
		rows, err = loadThroughputAttemptTimestamps(ctx, exec, params)
	} else {
		rows, err = loadThroughputUsageTimestamps(ctx, exec, params, scope)
	}
	if err != nil {
		return ThroughputStatsResponse{}, err
	}
	response := buildThroughputStats(rows, params.FromTime, params.ToTime)
	response.Caliber = CaliberForScope(scope)
	if scope == ScopeRouteAttempt {
		response.Coverage = DatasetCoverage{RequestLogs: &coverage}
	} else {
		response.Coverage = DatasetCoverage{UsageRequestEvents: &coverage}
	}
	response.Samples = ScopeSampleCounts{ObservationCount: len(rows)}
	return response, nil
}

func GetDashboardThroughput(ctx context.Context, exec queryExecutor, params ThroughputParams) (ThroughputStatsResponse, error) {
	params.Scope = ScopeIngress
	params.Preset = "custom"
	return GetThroughput(ctx, exec, params)
}

func buildThroughputStats(createdAtValues []time.Time, fromTime *time.Time, toTime *time.Time) ThroughputStatsResponse {
	bucketCounts := map[time.Time]int{}
	bucketOrder := make([]time.Time, 0)
	seenBuckets := map[time.Time]struct{}{}
	for _, createdAt := range createdAtValues {
		createdAt = createdAt.UTC()
		bucket := time.Date(createdAt.Year(), createdAt.Month(), createdAt.Day(), createdAt.Hour(), createdAt.Minute(), 0, 0, time.UTC)
		bucketCounts[bucket]++
		if _, ok := seenBuckets[bucket]; !ok {
			seenBuckets[bucket] = struct{}{}
			bucketOrder = append(bucketOrder, bucket)
		}
	}
	sort.Slice(bucketOrder, func(i int, j int) bool { return bucketOrder[i].Before(bucketOrder[j]) })
	timeWindowSeconds := 0.0
	if fromTime != nil && toTime != nil {
		timeWindowSeconds = toTime.UTC().Sub(fromTime.UTC()).Seconds()
		if timeWindowSeconds < 0 {
			timeWindowSeconds = 0
		}
	} else if len(bucketOrder) > 0 {
		timeWindowSeconds = bucketOrder[len(bucketOrder)-1].Sub(bucketOrder[0]).Seconds() + 60
	}
	if len(bucketOrder) == 0 {
		return ThroughputStatsResponse{
			AverageRPM:        0,
			PeakRPM:           0,
			CurrentRPM:        0,
			TotalRequests:     0,
			TimeWindowSeconds: roundFloat(timeWindowSeconds, 1),
			Buckets:           []ThroughputBucket{},
		}
	}
	totalRequests := 0
	buckets := make([]ThroughputBucket, 0, len(bucketOrder))
	peakRPM := 0.0
	currentRPM := 0.0
	for index, bucket := range bucketOrder {
		count := bucketCounts[bucket]
		rpm := roundFloat(float64(count), 3)
		totalRequests += count
		if rpm > peakRPM {
			peakRPM = rpm
		}
		if index == len(bucketOrder)-1 {
			currentRPM = rpm
		}
		buckets = append(buckets, ThroughputBucket{Timestamp: bucket, RequestCount: count, RPM: rpm})
	}
	timeWindowMinutes := 0.0
	if timeWindowSeconds > 0 {
		timeWindowMinutes = timeWindowSeconds / 60
	}
	averageRPM := 0.0
	if timeWindowMinutes > 0 {
		averageRPM = roundFloat(float64(totalRequests)/timeWindowMinutes, 3)
	}
	return ThroughputStatsResponse{
		AverageRPM:        averageRPM,
		PeakRPM:           roundFloat(peakRPM, 3),
		CurrentRPM:        roundFloat(currentRPM, 3),
		TotalRequests:     totalRequests,
		TimeWindowSeconds: roundFloat(timeWindowSeconds, 1),
		Buckets:           buckets,
	}
}

func loadThroughputAttemptTimestamps(ctx context.Context, exec queryExecutor, params ThroughputParams) ([]time.Time, error) {
	clauses := []string{"profile_id = $1", "row_kind = 'upstream'"}
	args := []any{params.ProfileID}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	if params.AttemptTargetModelID != nil && strings.TrimSpace(*params.AttemptTargetModelID) != "" {
		args = append(args, strings.TrimSpace(*params.AttemptTargetModelID))
		clauses = append(clauses, fmt.Sprintf("resolved_target_model_id = $%d", len(args)))
	}
	if params.APIFamily != nil && strings.TrimSpace(*params.APIFamily) != "" {
		args = append(args, strings.TrimSpace(*params.APIFamily))
		clauses = append(clauses, fmt.Sprintf("api_family = $%d", len(args)))
	}
	if params.EndpointID != nil {
		args = append(args, *params.EndpointID)
		clauses = append(clauses, fmt.Sprintf("endpoint_id = $%d", len(args)))
	}
	if params.ConnectionID != nil {
		args = append(args, *params.ConnectionID)
		clauses = append(clauses, fmt.Sprintf("connection_id = $%d", len(args)))
	}
	rows, err := exec.Query(ctx, `SELECT created_at FROM request_logs WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query throughput rows for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	items := make([]time.Time, 0)
	for rows.Next() {
		var createdAt time.Time
		if err := rows.Scan(&createdAt); err != nil {
			return nil, fmt.Errorf("scan throughput row: %w", err)
		}
		items = append(items, createdAt.UTC())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate throughput rows for profile %d: %w", params.ProfileID, err)
	}
	return items, nil
}

func loadThroughputUsageTimestamps(ctx context.Context, exec queryExecutor, params ThroughputParams, scope string) ([]time.Time, error) {
	clauses := []string{"profile_id = $1"}
	args := []any{params.ProfileID}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at < $%d", len(args)))
	}
	if params.APIFamily != nil {
		args = append(args, strings.TrimSpace(*params.APIFamily))
		clauses = append(clauses, fmt.Sprintf("api_family = $%d", len(args)))
	}
	if scope == ScopeIngress && params.IngressModelID != nil {
		args = append(args, strings.TrimSpace(*params.IngressModelID))
		clauses = append(clauses, fmt.Sprintf("model_id = $%d", len(args)))
	}
	if scope == ScopeFinal {
		clauses = append(clauses, "resolved_target_model_id IS NOT NULL", "final_attempt_number IS NOT NULL")
		if params.FinalTargetModelID != nil {
			args = append(args, strings.TrimSpace(*params.FinalTargetModelID))
			clauses = append(clauses, fmt.Sprintf("resolved_target_model_id = $%d", len(args)))
		}
	}
	if params.EndpointID != nil {
		args = append(args, *params.EndpointID)
		clauses = append(clauses, fmt.Sprintf("endpoint_id = $%d", len(args)))
	}
	if params.ConnectionID != nil {
		args = append(args, *params.ConnectionID)
		clauses = append(clauses, fmt.Sprintf("connection_id = $%d", len(args)))
	}
	rows, err := exec.Query(ctx, `SELECT created_at FROM usage_request_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query throughput usage rows: %w", err)
	}
	defer rows.Close()
	items := []time.Time{}
	for rows.Next() {
		var createdAt time.Time
		if err := rows.Scan(&createdAt); err != nil {
			return nil, err
		}
		items = append(items, createdAt.UTC())
	}
	return items, rows.Err()
}

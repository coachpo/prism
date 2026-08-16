package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type EndpointModelStatisticsParams struct {
	ProfileID  int
	EndpointID int
	Preset     string
	FromTime   *time.Time
	ToTime     *time.Time
}

type ModelMetricsParams struct {
	ProfileID          int
	ModelIDs           []string
	SummaryWindowHours int
	SpendingPreset     string
	ReferenceNow       time.Time
}

type ConnectionSuccessRateParams struct {
	ProfileID int
	FromTime  *time.Time
	ToTime    *time.Time
}

type ThroughputParams struct {
	ProfileID    int
	FromTime     *time.Time
	ToTime       *time.Time
	ModelID      *string
	APIFamily    *string
	EndpointID   *int
	ConnectionID *int
}

type SpendingParams struct {
	ProfileID    int
	Preset       string
	FromTime     *time.Time
	ToTime       *time.Time
	APIFamily    *string
	ModelID      *string
	EndpointID   *int
	ConnectionID *int
	GroupBy      string
	Limit        int
	Offset       int
	TopN         int
	ReferenceNow time.Time
}

type summaryRequestLogRow struct {
	CreatedAt       time.Time
	ModelID         string
	APIFamily       string
	EndpointBaseURL *string
	EndpointID      *int
	ConnectionID    *int
	StatusCode      int
	ResponseTimeMS  int
	InputTokens     *int
	OutputTokens    *int
	TotalTokens     *int
}

type spendingGroupAggregate struct {
	Key              string
	TotalCostMicros  int64
	TotalRequests    int
	PricedRequests   int
	UnpricedRequests int
	TotalTokens      int
}

type endpointModelAggregate struct {
	ModelID              string
	ModelLabel           string
	RequestCount         int
	SuccessCount         int
	FailedCount          int
	PricedRequestCount   int
	UnpricedRequestCount int
	TotalTokens          int
	TotalCostMicros      int64
	TTFTValues           []int
	OutputRateSum        float64
	EligibleOutputRates  int
}

type dashboardStatsSummaryGroup struct {
	group          StatGroup
	responseTimeMS []int
}

func GetStatsSummary(ctx context.Context, exec queryExecutor, params StatsSummaryParams) (StatsSummaryResponse, error) {
	rows, err := loadSummaryRequestLogRows(ctx, exec, params)
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	response := StatsSummaryResponse{Groups: []StatGroup{}}
	response.TotalRequests = len(rows)
	response.TotalInputTokens = 0
	response.TotalOutputTokens = 0
	response.TotalTokens = 0
	latencies := make([]int, 0, len(rows))
	groups := map[string]*StatGroup{}
	for _, row := range rows {
		if row.StatusCode >= 200 && row.StatusCode <= 299 {
			response.SuccessCount++
		}
		latencies = append(latencies, row.ResponseTimeMS)
		response.TotalInputTokens += intValue(row.InputTokens)
		response.TotalOutputTokens += intValue(row.OutputTokens)
		response.TotalTokens += intValue(row.TotalTokens)
		if normalizedGroupBy, ok := normalizedStatsSummaryGroupBy(params.GroupBy); ok {
			key := summaryGroupKey(normalizedGroupBy, row)
			group := groups[key]
			if group == nil {
				groups[key] = &StatGroup{Key: key}
				group = groups[key]
			}
			group.TotalRequests++
			if row.StatusCode >= 200 && row.StatusCode <= 299 {
				group.SuccessCount++
			}
			group.TotalTokens += intValue(row.TotalTokens)
			group.AvgResponseTimeMS += float64(row.ResponseTimeMS)
		}
	}
	response.ErrorCount = response.TotalRequests - response.SuccessCount
	response.SuccessRate = successRate(response.SuccessCount, response.TotalRequests)
	if response.TotalRequests > 0 {
		response.AvgResponseTimeMS = roundFloat(sumInts(latencies)/float64(len(latencies)), 1)
	}
	if p95 := percentileContInt(latencies, 0.95); p95 != nil {
		response.P95ResponseTimeMS = *p95
	}
	groupItems := make([]StatGroup, 0, len(groups))
	for _, group := range groups {
		group.ErrorCount = group.TotalRequests - group.SuccessCount
		if group.TotalRequests > 0 {
			group.AvgResponseTimeMS = roundFloat(group.AvgResponseTimeMS/float64(group.TotalRequests), 1)
		}
		groupItems = append(groupItems, *group)
	}
	sort.Slice(groupItems, func(i int, j int) bool {
		if groupItems[i].TotalRequests != groupItems[j].TotalRequests {
			return groupItems[i].TotalRequests > groupItems[j].TotalRequests
		}
		return groupItems[i].Key < groupItems[j].Key
	})
	response.Groups = groupItems
	return response, nil
}

func GetDashboardStatsSummary(ctx context.Context, exec queryExecutor, params StatsSummaryParams) (StatsSummaryResponse, error) {
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, params.FromTime, params.ToTime, params.APIFamily, params.ModelID, params.EndpointID, params.ConnectionID)
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	return buildDashboardStatsSummary(records, params), nil
}

func buildDashboardStatsSummary(records []usageEventRecord, params StatsSummaryParams) StatsSummaryResponse {
	response := StatsSummaryResponse{Groups: []StatGroup{}}
	response.TotalRequests = len(records)
	latencies := make([]int, 0, len(records))
	groups := map[string]*dashboardStatsSummaryGroup{}
	for _, record := range records {
		if record.SuccessFlag {
			response.SuccessCount++
		}
		if record.ResponseTimeMS != nil {
			latencies = append(latencies, *record.ResponseTimeMS)
		}
		response.TotalInputTokens += record.InputTokens
		response.TotalOutputTokens += record.OutputTokens
		response.TotalTokens += record.TotalTokens
		if normalizedGroupBy, ok := normalizedStatsSummaryGroupBy(params.GroupBy); ok {
			key := summaryUsageEventGroupKey(normalizedGroupBy, record)
			group := groups[key]
			if group == nil {
				groups[key] = &dashboardStatsSummaryGroup{group: StatGroup{Key: key}}
				group = groups[key]
			}
			group.group.TotalRequests++
			if record.SuccessFlag {
				group.group.SuccessCount++
			}
			group.group.TotalTokens += record.TotalTokens
			if record.ResponseTimeMS != nil {
				group.responseTimeMS = append(group.responseTimeMS, *record.ResponseTimeMS)
			}
		}
	}
	response.ErrorCount = response.TotalRequests - response.SuccessCount
	response.SuccessRate = successRate(response.SuccessCount, response.TotalRequests)
	if len(latencies) > 0 {
		response.AvgResponseTimeMS = roundFloat(sumInts(latencies)/float64(len(latencies)), 1)
	}
	if p95 := percentileContInt(latencies, 0.95); p95 != nil {
		response.P95ResponseTimeMS = *p95
	}
	groupItems := make([]StatGroup, 0, len(groups))
	for _, aggregate := range groups {
		item := aggregate.group
		item.ErrorCount = item.TotalRequests - item.SuccessCount
		if len(aggregate.responseTimeMS) > 0 {
			item.AvgResponseTimeMS = roundFloat(sumInts(aggregate.responseTimeMS)/float64(len(aggregate.responseTimeMS)), 1)
		}
		groupItems = append(groupItems, item)
	}
	sort.Slice(groupItems, func(i int, j int) bool {
		if groupItems[i].TotalRequests != groupItems[j].TotalRequests {
			return groupItems[i].TotalRequests > groupItems[j].TotalRequests
		}
		return groupItems[i].Key < groupItems[j].Key
	})
	response.Groups = groupItems
	return response
}

func summaryUsageEventGroupKey(groupBy string, record usageEventRecord) string {
	switch groupBy {
	case "model":
		return record.ModelID
	case "api_family":
		return record.APIFamily
	case "endpoint":
		return usageEventEndpointLabel(record)
	default:
		return ""
	}
}

func GetConnectionSuccessRates(ctx context.Context, exec queryExecutor, params ConnectionSuccessRateParams) ([]ConnectionSuccessRate, error) {
	query, args := buildConnectionSuccessRatesQuery(params)
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query connection success rates for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	aggregates := map[int]*ConnectionSuccessRate{}
	for rows.Next() {
		var connectionID int
		var scopedStatus sql.NullInt32
		if err := rows.Scan(&connectionID, &scopedStatus); err != nil {
			return nil, fmt.Errorf("scan connection success rate row: %w", err)
		}
		statusCode := intValue(nullableInt32(scopedStatus))
		item := aggregates[connectionID]
		if item == nil {
			aggregates[connectionID] = &ConnectionSuccessRate{ConnectionID: connectionID}
			item = aggregates[connectionID]
		}
		item.TotalRequests++
		if statusCode >= 200 && statusCode <= 299 {
			item.SuccessCount++
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
		items = append(items, *item)
	}
	sort.Slice(items, func(i int, j int) bool { return items[i].ConnectionID < items[j].ConnectionID })
	return items, nil
}

func GetThroughput(ctx context.Context, exec queryExecutor, params ThroughputParams) (ThroughputStatsResponse, error) {
	rows, err := loadThroughputTimestamps(ctx, exec, params)
	if err != nil {
		return ThroughputStatsResponse{}, err
	}
	return buildThroughputStats(rows, params.FromTime, params.ToTime), nil
}

func GetDashboardThroughput(ctx context.Context, exec queryExecutor, params ThroughputParams) (ThroughputStatsResponse, error) {
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, params.FromTime, params.ToTime, params.APIFamily, params.ModelID, params.EndpointID, params.ConnectionID)
	if err != nil {
		return ThroughputStatsResponse{}, err
	}
	createdAtValues := make([]time.Time, 0, len(records))
	for _, record := range records {
		createdAtValues = append(createdAtValues, record.CreatedAt)
	}
	return buildThroughputStats(createdAtValues, params.FromTime, params.ToTime), nil
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

func GetModelMetrics(ctx context.Context, exec queryExecutor, params ModelMetricsParams) (ModelMetricsBatchResponse, error) {
	uniqueModelIDs := dedupeNonEmptyStrings(params.ModelIDs)
	items := make([]ModelMetricsBatchItem, 0, len(uniqueModelIDs))
	if len(uniqueModelIDs) == 0 {
		return ModelMetricsBatchResponse{Items: items}, nil
	}
	summaryFrom := params.ReferenceNow.UTC().Add(-time.Duration(params.SummaryWindowHours) * time.Hour)
	summaryRows, err := loadModelMetricSummaryRows(ctx, exec, params.ProfileID, uniqueModelIDs, summaryFrom)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	spendingFrom, spendingTo := resolveTimePreset(params.SpendingPreset, nil, nil, params.ReferenceNow.UTC())
	spendingRows, err := loadUsageEventRecords(ctx, exec, params.ProfileID, spendingFrom, spendingTo, nil, nil, nil, nil)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	resultByModelID := map[string]ModelMetricsBatchItem{}
	for _, modelID := range uniqueModelIDs {
		resultByModelID[modelID] = ModelMetricsBatchItem{ModelID: modelID}
	}
	summaryByModel := map[string][]summaryRequestLogRow{}
	for _, row := range summaryRows {
		summaryByModel[row.ModelID] = append(summaryByModel[row.ModelID], row)
	}
	for modelID, rows := range summaryByModel {
		item := resultByModelID[modelID]
		latencies := make([]int, 0, len(rows))
		successCount := 0
		for _, row := range rows {
			latencies = append(latencies, row.ResponseTimeMS)
			if row.StatusCode >= 200 && row.StatusCode <= 299 {
				successCount++
			}
		}
		item.RequestCount24H = len(rows)
		item.SuccessRate = successRate(successCount, len(rows))
		if p95 := percentileContInt(latencies, 0.95); p95 != nil {
			item.P95LatencyMS = *p95
		}
		resultByModelID[modelID] = item
	}
	for _, row := range spendingRows {
		item, ok := resultByModelID[row.ModelID]
		if !ok || !row.SuccessFlag {
			continue
		}
		if row.TrustedKnownCost() {
			item.Spend30DMicros += row.TotalCostUserCurrencyMicros
		}
		resultByModelID[row.ModelID] = item
	}
	for _, modelID := range uniqueModelIDs {
		items = append(items, resultByModelID[modelID])
	}
	return ModelMetricsBatchResponse{Items: items}, nil
}

func GetEndpointModelStatistics(ctx context.Context, exec queryExecutor, params EndpointModelStatisticsParams, referenceNow time.Time) ([]EndpointModelStatistic, error) {
	endpointExists, historicalExists, err := endpointOrHistoricalUsageExists(ctx, exec, params.ProfileID, params.EndpointID)
	if err != nil {
		return nil, err
	}
	if !endpointExists && !historicalExists {
		return nil, &HTTPError{StatusCode: 404, Detail: "Endpoint not found"}
	}
	preset := params.Preset
	if params.FromTime != nil || params.ToTime != nil {
		preset = "custom"
	}
	startAt, endAt := resolveTimePreset(preset, params.FromTime, params.ToTime, referenceNow.UTC())
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, startAt, endAt, nil, nil, &params.EndpointID, nil)
	if err != nil {
		return nil, err
	}
	aggregates := map[string]*endpointModelAggregate{}
	for _, record := range records {
		group := aggregates[record.ModelID]
		modelLabel := record.ModelID
		if record.CurrentModelLabel != nil && strings.TrimSpace(*record.CurrentModelLabel) != "" {
			modelLabel = strings.TrimSpace(*record.CurrentModelLabel)
		}
		if group == nil {
			aggregates[record.ModelID] = &endpointModelAggregate{ModelID: record.ModelID, ModelLabel: modelLabel}
			group = aggregates[record.ModelID]
		}
		group.RequestCount++
		if record.SuccessFlag {
			group.SuccessCount++
			if record.Priced() {
				group.PricedRequestCount++
			} else {
				group.UnpricedRequestCount++
			}
		} else {
			group.FailedCount++
		}
		if record.TTFTMS != nil {
			group.TTFTValues = append(group.TTFTValues, *record.TTFTMS)
		}
		if outputRate := requestOutputRateTPS(record.OutputTokens, record.HasOutputTokens, record.TTFTMS, record.CompletionDurationMS); outputRate != nil {
			group.OutputRateSum += *outputRate
			group.EligibleOutputRates++
		}
		group.TotalTokens += record.TotalTokens
		if record.TrustedKnownCost() {
			group.TotalCostMicros += record.TotalCostUserCurrencyMicros
		}
	}
	items := make([]EndpointModelStatistic, 0, len(aggregates))
	for _, aggregate := range aggregates {
		items = append(items, EndpointModelStatistic{
			ModelID:              aggregate.ModelID,
			ModelLabel:           aggregate.ModelLabel,
			RequestCount:         aggregate.RequestCount,
			SuccessCount:         intPtr(aggregate.SuccessCount),
			FailedCount:          intPtr(aggregate.FailedCount),
			PricedRequestCount:   intPtr(aggregate.PricedRequestCount),
			UnpricedRequestCount: intPtr(aggregate.UnpricedRequestCount),
			SuccessRate:          successRate(aggregate.SuccessCount, aggregate.RequestCount),
			P50TTFTMS:            percentileContInt(aggregate.TTFTValues, 0.5),
			P95TTFTMS:            percentileContInt(aggregate.TTFTValues, 0.95),
			TotalTokens:          aggregate.TotalTokens,
			TotalCostMicros:      aggregate.TotalCostMicros,
			AvgOutputRateTPS:     averageOutputRatePointer(aggregate.OutputRateSum, aggregate.EligibleOutputRates),
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		if items[i].ModelLabel != items[j].ModelLabel {
			return items[i].ModelLabel < items[j].ModelLabel
		}
		return items[i].ModelID < items[j].ModelID
	})
	return items, nil
}

func GetSpending(ctx context.Context, exec queryExecutor, params SpendingParams) (SpendingReportResponse, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	topN := params.TopN
	if topN <= 0 {
		topN = 5
	}
	startAt, endAt := resolveTimePreset(params.Preset, params.FromTime, params.ToTime, params.ReferenceNow.UTC())
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, startAt, endAt, params.APIFamily, params.ModelID, params.EndpointID, params.ConnectionID)
	if err != nil {
		return SpendingReportResponse{}, err
	}
	successRecords := make([]usageEventRecord, 0)
	for _, record := range records {
		if record.SuccessFlag {
			successRecords = append(successRecords, record)
		}
	}
	response := SpendingReportResponse{Groups: []SpendingGroupRow{}, TopSpendingModels: []SpendingTopModel{}, TopSpendingEndpoints: []SpendingTopEndpoint{}, UnpricedBreakdown: map[string]int{}}
	for _, record := range successRecords {
		spend := int64(0)
		if record.TrustedKnownCost() {
			spend = record.TotalCostUserCurrencyMicros
		}
		response.Summary.TotalCostMicros += spend
		response.Summary.SuccessfulRequestCount++
		addSpendingSummaryTokenTotals(&response.Summary, record)
		if record.Priced() {
			response.Summary.PricedRequestCount++
		} else {
			response.Summary.UnpricedRequestCount++
			reason := "UNKNOWN"
			if record.UnpricedReason != nil && strings.TrimSpace(*record.UnpricedReason) != "" {
				reason = strings.TrimSpace(*record.UnpricedReason)
			}
			response.UnpricedBreakdown[reason]++
		}
	}
	if response.Summary.SuccessfulRequestCount > 0 {
		response.Summary.AvgCostPerSuccessfulRequestMicros = response.Summary.TotalCostMicros / int64(response.Summary.SuccessfulRequestCount)
	}
	groupBy := strings.TrimSpace(strings.ToLower(params.GroupBy))
	groupAggregates := map[string]*spendingGroupAggregate{}
	modelSpend := map[string]int64{}
	modelLabels := map[string]string{}
	endpointSpend := map[string]*SpendingTopEndpoint{}
	for _, record := range successRecords {
		spend := int64(0)
		if record.TrustedKnownCost() {
			spend = record.TotalCostUserCurrencyMicros
		}
		endpointLabel := usageEventEndpointLabel(record)
		groupKey := spendingGroupKey(groupBy, record, endpointLabel)
		groupDisplayKey := groupKey
		if groupBy == "endpoint" {
			groupKey = spendingEndpointKey(record.EndpointID, endpointLabel)
			groupDisplayKey = endpointLabel
		}
		if groupBy == "" || groupBy == "none" {
			groupKey = "all"
			groupDisplayKey = "all"
		}
		aggregate := groupAggregates[groupKey]
		if aggregate == nil {
			groupAggregates[groupKey] = &spendingGroupAggregate{Key: groupDisplayKey}
			aggregate = groupAggregates[groupKey]
		}
		aggregate.TotalCostMicros += spend
		aggregate.TotalRequests++
		if record.Priced() {
			aggregate.PricedRequests++
		} else {
			aggregate.UnpricedRequests++
		}
		aggregate.TotalTokens += record.TotalTokens
		modelLabel := strings.TrimSpace(record.ModelID)
		if record.CurrentModelLabel != nil && strings.TrimSpace(*record.CurrentModelLabel) != "" {
			modelLabel = strings.TrimSpace(*record.CurrentModelLabel)
		}
		if existingLabel, ok := modelLabels[record.ModelID]; !ok || existingLabel == strings.TrimSpace(record.ModelID) {
			modelLabels[record.ModelID] = modelLabel
		}
		modelSpend[record.ModelID] += spend
		endpointKey := spendingEndpointKey(record.EndpointID, endpointLabel)
		endpoint := endpointSpend[endpointKey]
		if endpoint == nil {
			endpointSpend[endpointKey] = &SpendingTopEndpoint{EndpointID: record.EndpointID, EndpointLabel: endpointLabel}
			endpoint = endpointSpend[endpointKey]
		}
		endpoint.TotalCostMicros += spend
	}
	groupItems := make([]SpendingGroupRow, 0)
	if groupBy == "" || groupBy == "none" {
		groupItems = append(groupItems, SpendingGroupRow{Key: "all", TotalCostMicros: response.Summary.TotalCostMicros, TotalRequests: response.Summary.SuccessfulRequestCount, PricedRequests: response.Summary.PricedRequestCount, UnpricedRequests: response.Summary.UnpricedRequestCount, TotalTokens: response.Summary.TotalTokens})
		response.GroupsTotal = 1
	} else {
		response.GroupsTotal = len(groupAggregates)
		orderedGroups := make([]SpendingGroupRow, 0, len(groupAggregates))
		for _, aggregate := range groupAggregates {
			orderedGroups = append(orderedGroups, SpendingGroupRow{Key: aggregate.Key, TotalCostMicros: aggregate.TotalCostMicros, TotalRequests: aggregate.TotalRequests, PricedRequests: aggregate.PricedRequests, UnpricedRequests: aggregate.UnpricedRequests, TotalTokens: aggregate.TotalTokens})
		}
		sort.Slice(orderedGroups, func(i int, j int) bool {
			if orderedGroups[i].TotalCostMicros != orderedGroups[j].TotalCostMicros {
				return orderedGroups[i].TotalCostMicros > orderedGroups[j].TotalCostMicros
			}
			return orderedGroups[i].Key < orderedGroups[j].Key
		})
		start := offset
		if start > len(orderedGroups) {
			start = len(orderedGroups)
		}
		end := start + limit
		if end > len(orderedGroups) {
			end = len(orderedGroups)
		}
		groupItems = orderedGroups[start:end]
	}
	response.Groups = groupItems
	modelItems := make([]SpendingTopModel, 0, len(modelSpend))
	for modelID, spend := range modelSpend {
		if spend <= 0 {
			continue
		}
		modelLabel := strings.TrimSpace(modelLabels[modelID])
		if modelLabel == "" {
			modelLabel = strings.TrimSpace(modelID)
		}
		modelItems = append(modelItems, SpendingTopModel{ModelID: modelID, ModelLabel: modelLabel, TotalCostMicros: spend})
	}
	sort.Slice(modelItems, func(i int, j int) bool {
		if modelItems[i].TotalCostMicros != modelItems[j].TotalCostMicros {
			return modelItems[i].TotalCostMicros > modelItems[j].TotalCostMicros
		}
		return modelItems[i].ModelID < modelItems[j].ModelID
	})
	if len(modelItems) > topN {
		modelItems = modelItems[:topN]
	}
	response.TopSpendingModels = modelItems
	endpointItems := make([]SpendingTopEndpoint, 0, len(endpointSpend))
	for _, item := range endpointSpend {
		if item.TotalCostMicros <= 0 {
			continue
		}
		endpointItems = append(endpointItems, *item)
	}
	sort.Slice(endpointItems, func(i int, j int) bool {
		if endpointItems[i].TotalCostMicros != endpointItems[j].TotalCostMicros {
			return endpointItems[i].TotalCostMicros > endpointItems[j].TotalCostMicros
		}
		if endpointIDOrMinusOne(endpointItems[i].EndpointID) != endpointIDOrMinusOne(endpointItems[j].EndpointID) {
			return endpointIDOrMinusOne(endpointItems[i].EndpointID) < endpointIDOrMinusOne(endpointItems[j].EndpointID)
		}
		return endpointItems[i].EndpointLabel < endpointItems[j].EndpointLabel
	})
	if len(endpointItems) > topN {
		endpointItems = endpointItems[:topN]
	}
	response.TopSpendingEndpoints = endpointItems
	response.ReportCurrencyCode, response.ReportCurrencySymbol, err = loadReportCurrencyPreferences(ctx, exec, params.ProfileID)
	if err != nil {
		return SpendingReportResponse{}, err
	}
	return response, nil
}

func addSpendingSummaryTokenTotals(summary *SpendingSummary, record usageEventRecord) {
	summary.TotalInputTokens += record.InputTokens
	summary.TotalOutputTokens += record.OutputTokens
	summary.TotalCacheReadInputTokens += record.CacheReadInputTokens
	summary.TotalCacheCreationInputTokens += record.CacheCreationInputTokens
	summary.TotalReasoningTokens += record.ReasoningTokens
	summary.TotalTokens += record.TotalTokens
}

func normalizedStatsSummaryGroupBy(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	switch strings.TrimSpace(strings.ToLower(*value)) {
	case "model", "api_family", "endpoint":
		return strings.TrimSpace(strings.ToLower(*value)), true
	default:
		return "", false
	}
}

func summaryGroupKey(groupBy string, row summaryRequestLogRow) string {
	switch groupBy {
	case "model":
		return row.ModelID
	case "api_family":
		return row.APIFamily
	case "endpoint":
		if row.EndpointBaseURL != nil && strings.TrimSpace(*row.EndpointBaseURL) != "" {
			return strings.TrimSpace(*row.EndpointBaseURL)
		}
		return "unknown"
	default:
		return ""
	}
}

func buildConnectionSuccessRatesQuery(params ConnectionSuccessRateParams) (string, []any) {
	clauses := []string{"profile_id = $1", "connection_id IS NOT NULL"}
	args := []any{params.ProfileID}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	return `SELECT connection_id, ` + scopedRequestLogStatusSQL + ` AS scoped_status FROM request_logs WHERE ` + strings.Join(clauses, " AND "), args
}

func loadSummaryRequestLogRows(ctx context.Context, exec queryExecutor, params StatsSummaryParams) ([]summaryRequestLogRow, error) {
	clauses := []string{"profile_id = $1"}
	args := []any{params.ProfileID}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	if params.ModelID != nil && strings.TrimSpace(*params.ModelID) != "" {
		args = append(args, strings.TrimSpace(*params.ModelID))
		clauses = append(clauses, fmt.Sprintf("model_id = $%d", len(args)))
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
	rows, err := exec.Query(ctx, `SELECT created_at, model_id, api_family, endpoint_base_url, endpoint_id, connection_id, `+scopedRequestLogStatusSQL+` AS scoped_status, `+scopedRequestLogDurationSQL+` AS scoped_duration_ms, input_tokens, output_tokens, total_tokens FROM request_logs WHERE `+strings.Join(clauses, " AND "), args...)
	if err != nil {
		return nil, fmt.Errorf("query request-log summary rows for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	items := make([]summaryRequestLogRow, 0)
	for rows.Next() {
		var endpointBaseURL sql.NullString
		var endpointID sql.NullInt32
		var connectionID sql.NullInt32
		var inputTokens sql.NullInt32
		var outputTokens sql.NullInt32
		var totalTokens sql.NullInt32
		var scopedStatus sql.NullInt32
		var scopedDuration sql.NullInt32
		var item summaryRequestLogRow
		if err := rows.Scan(&item.CreatedAt, &item.ModelID, &item.APIFamily, &endpointBaseURL, &endpointID, &connectionID, &scopedStatus, &scopedDuration, &inputTokens, &outputTokens, &totalTokens); err != nil {
			return nil, fmt.Errorf("scan request-log summary row: %w", err)
		}
		item.StatusCode = intValue(nullableInt32(scopedStatus))
		item.ResponseTimeMS = intValue(nullableInt32(scopedDuration))
		item.CreatedAt = item.CreatedAt.UTC()
		item.EndpointBaseURL = nullableString(endpointBaseURL)
		item.EndpointID = nullableInt32(endpointID)
		item.ConnectionID = nullableInt32(connectionID)
		item.InputTokens = nullableInt32(inputTokens)
		item.OutputTokens = nullableInt32(outputTokens)
		item.TotalTokens = nullableInt32(totalTokens)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate request-log summary rows for profile %d: %w", params.ProfileID, err)
	}
	return items, nil
}

func loadThroughputTimestamps(ctx context.Context, exec queryExecutor, params ThroughputParams) ([]time.Time, error) {
	clauses := []string{"profile_id = $1"}
	args := []any{params.ProfileID}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	if params.ModelID != nil && strings.TrimSpace(*params.ModelID) != "" {
		args = append(args, strings.TrimSpace(*params.ModelID))
		clauses = append(clauses, fmt.Sprintf("model_id = $%d", len(args)))
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

func loadModelMetricSummaryRows(ctx context.Context, exec queryExecutor, profileID int, modelIDs []string, fromTime time.Time) ([]summaryRequestLogRow, error) {
	if len(modelIDs) == 0 {
		return []summaryRequestLogRow{}, nil
	}
	placeholders := make([]string, 0, len(modelIDs))
	args := []any{profileID, fromTime.UTC()}
	for _, modelID := range modelIDs {
		args = append(args, modelID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := exec.Query(ctx, `SELECT created_at, model_id, api_family, endpoint_base_url, endpoint_id, connection_id, `+scopedRequestLogStatusSQL+` AS scoped_status, `+scopedRequestLogDurationSQL+` AS scoped_duration_ms, input_tokens, output_tokens, total_tokens FROM request_logs WHERE profile_id = $1 AND created_at >= $2 AND model_id IN (`+strings.Join(placeholders, ", ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query model metric summary rows for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]summaryRequestLogRow, 0)
	for rows.Next() {
		var endpointBaseURL sql.NullString
		var endpointID sql.NullInt32
		var connectionID sql.NullInt32
		var inputTokens sql.NullInt32
		var outputTokens sql.NullInt32
		var totalTokens sql.NullInt32
		var scopedStatus sql.NullInt32
		var scopedDuration sql.NullInt32
		var item summaryRequestLogRow
		if err := rows.Scan(&item.CreatedAt, &item.ModelID, &item.APIFamily, &endpointBaseURL, &endpointID, &connectionID, &scopedStatus, &scopedDuration, &inputTokens, &outputTokens, &totalTokens); err != nil {
			return nil, fmt.Errorf("scan model metric summary row: %w", err)
		}
		item.StatusCode = intValue(nullableInt32(scopedStatus))
		item.ResponseTimeMS = intValue(nullableInt32(scopedDuration))
		item.CreatedAt = item.CreatedAt.UTC()
		item.EndpointBaseURL = nullableString(endpointBaseURL)
		item.EndpointID = nullableInt32(endpointID)
		item.ConnectionID = nullableInt32(connectionID)
		item.InputTokens = nullableInt32(inputTokens)
		item.OutputTokens = nullableInt32(outputTokens)
		item.TotalTokens = nullableInt32(totalTokens)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model metric summary rows for profile %d: %w", profileID, err)
	}
	return items, nil
}

func endpointOrHistoricalUsageExists(ctx context.Context, exec queryExecutor, profileID int, endpointID int) (bool, bool, error) {
	var liveEndpointID int
	err := exec.QueryRow(ctx, `SELECT id FROM endpoints WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, endpointID).Scan(&liveEndpointID)
	liveExists := err == nil
	if err != nil && err != pgx.ErrNoRows {
		return false, false, fmt.Errorf("load endpoint %d for profile %d: %w", endpointID, profileID, err)
	}
	var historicalEndpointID int
	err = exec.QueryRow(ctx, `SELECT endpoint_id FROM usage_request_events WHERE profile_id = $1 AND endpoint_id = $2 LIMIT 1`, profileID, endpointID).Scan(&historicalEndpointID)
	historicalExists := err == nil
	if err != nil && err != pgx.ErrNoRows {
		return false, false, fmt.Errorf("check historical usage for endpoint %d in profile %d: %w", endpointID, profileID, err)
	}
	return liveExists, historicalExists, nil
}

func loadUsageEventRecords(ctx context.Context, exec queryExecutor, profileID int, startAt *time.Time, endAt *time.Time, apiFamily *string, modelID *string, endpointID *int, connectionID *int) ([]usageEventRecord, error) {
	clauses := []string{"usage_request_events.profile_id = $1"}
	args := []any{profileID}
	if endAt != nil {
		args = append(args, endAt.UTC())
		clauses = append(clauses, fmt.Sprintf("usage_request_events.created_at <= $%d", len(args)))
	}
	if startAt != nil {
		args = append(args, startAt.UTC())
		clauses = append(clauses, fmt.Sprintf("usage_request_events.created_at >= $%d", len(args)))
	}
	if apiFamily != nil && strings.TrimSpace(*apiFamily) != "" {
		args = append(args, strings.TrimSpace(*apiFamily))
		clauses = append(clauses, fmt.Sprintf("usage_request_events.api_family = $%d", len(args)))
	}
	if modelID != nil && strings.TrimSpace(*modelID) != "" {
		args = append(args, strings.TrimSpace(*modelID))
		clauses = append(clauses, fmt.Sprintf("usage_request_events.model_id = $%d", len(args)))
	}
	if endpointID != nil {
		args = append(args, *endpointID)
		clauses = append(clauses, fmt.Sprintf("usage_request_events.endpoint_id = $%d", len(args)))
	}
	if connectionID != nil {
		args = append(args, *connectionID)
		clauses = append(clauses, fmt.Sprintf("usage_request_events.connection_id = $%d", len(args)))
	}
	rows, err := exec.Query(ctx, `SELECT usage_request_events.id, usage_request_events.created_at, usage_request_events.profile_id, usage_request_events.ingress_request_id, usage_request_events.model_id, usage_request_events.resolved_target_model_id, usage_request_events.api_family, usage_request_events.endpoint_id, usage_request_events.endpoint_label_snapshot, usage_request_events.connection_id, usage_request_events.proxy_api_key_id_snapshot, usage_request_events.proxy_api_key_name_snapshot, usage_request_events.status_code, usage_request_events.success_flag, usage_request_events.pricing_status, usage_request_events.pricing_evidence_trust, usage_request_events.unpriced_reason, usage_request_events.input_tokens, usage_request_events.output_tokens, usage_request_events.total_tokens, usage_request_events.cache_read_input_tokens, usage_request_events.cache_creation_input_tokens, usage_request_events.reasoning_tokens, usage_request_events.total_cost_user_currency_micros, usage_request_events.attempt_count, usage_request_events.request_path, usage_request_events.response_time_ms, usage_request_events.ttft_ms, usage_request_events.completion_duration_ms, model_configs.display_name, endpoints.name, endpoints.base_url, proxy_api_keys.name, proxy_api_keys.key_prefix
		 FROM usage_request_events
		 LEFT JOIN model_configs ON model_configs.profile_id = usage_request_events.profile_id AND model_configs.model_id = usage_request_events.model_id
		 LEFT JOIN endpoints ON endpoints.profile_id = usage_request_events.profile_id AND endpoints.id = usage_request_events.endpoint_id
		 LEFT JOIN proxy_api_keys ON proxy_api_keys.id = usage_request_events.proxy_api_key_id_snapshot
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY usage_request_events.created_at DESC, usage_request_events.id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage events for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]usageEventRecord, 0)
	for rows.Next() {
		item, scanErr := scanUsageEventRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage events for profile %d: %w", profileID, err)
	}
	return items, nil
}

func spendingGroupKey(groupBy string, record usageEventRecord, endpointLabel string) string {
	switch groupBy {
	case "day":
		return record.CreatedAt.UTC().Format("2006-01-02")
	case "week":
		year, week := record.CreatedAt.UTC().ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "month":
		return record.CreatedAt.UTC().Format("2006-01")
	case "api_family":
		return record.APIFamily
	case "model":
		return record.ModelID
	case "endpoint":
		return endpointLabel
	case "model_endpoint":
		resolvedEndpointID := -1
		if record.EndpointID != nil {
			resolvedEndpointID = *record.EndpointID
		}
		return fmt.Sprintf("%s#%d", record.ModelID, resolvedEndpointID)
	default:
		return "all"
	}
}

func spendingEndpointKey(endpointID *int, endpointLabel string) string {
	resolvedEndpointID := -1
	if endpointID != nil {
		resolvedEndpointID = *endpointID
	}
	return fmt.Sprintf("%d\x00%s", resolvedEndpointID, endpointLabel)
}

func scanUsageEventRecord(scanner interface{ Scan(...any) error }) (usageEventRecord, error) {
	var resolvedTargetModelID sql.NullString
	var endpointID sql.NullInt32
	var endpointLabelSnapshot sql.NullString
	var connectionID sql.NullInt32
	var proxyAPIKeyID sql.NullInt32
	var proxyAPIKeyNameSnapshot sql.NullString
	var pricingStatus sql.NullString
	var pricingEvidenceTrust sql.NullString
	var unpricedReason sql.NullString
	var inputTokens sql.NullInt32
	var outputTokens sql.NullInt32
	var totalTokens sql.NullInt32
	var cacheReadInputTokens sql.NullInt32
	var cacheCreationInputTokens sql.NullInt32
	var reasoningTokens sql.NullInt32
	var totalCostUserCurrencyMicros sql.NullInt64
	var responseTimeMS sql.NullInt32
	var ttftMS sql.NullInt32
	var completionDurationMS sql.NullInt32
	var currentModelLabel sql.NullString
	var currentEndpointName sql.NullString
	var currentEndpointBaseURL sql.NullString
	var currentProxyAPIKeyName sql.NullString
	var currentProxyAPIKeyPrefix sql.NullString
	item := usageEventRecord{}
	if err := scanner.Scan(&item.ID, &item.CreatedAt, &item.ProfileID, &item.IngressRequestID, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &endpointID, &endpointLabelSnapshot, &connectionID, &proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &item.StatusCode, &item.SuccessFlag, &pricingStatus, &pricingEvidenceTrust, &unpricedReason, &inputTokens, &outputTokens, &totalTokens, &cacheReadInputTokens, &cacheCreationInputTokens, &reasoningTokens, &totalCostUserCurrencyMicros, &item.AttemptCount, &item.RequestPath, &responseTimeMS, &ttftMS, &completionDurationMS, &currentModelLabel, &currentEndpointName, &currentEndpointBaseURL, &currentProxyAPIKeyName, &currentProxyAPIKeyPrefix); err != nil {
		return usageEventRecord{}, fmt.Errorf("scan usage event: %w", err)
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	item.EndpointID = nullableInt32(endpointID)
	item.EndpointLabelSnapshot = stringValue(nullableString(endpointLabelSnapshot))
	item.ConnectionID = nullableInt32(connectionID)
	item.ProxyAPIKeyID = nullableInt32(proxyAPIKeyID)
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.PricingStatus = stringValue(nullableString(pricingStatus))
	item.PricingEvidenceTrust = stringValue(nullableString(pricingEvidenceTrust))
	item.UnpricedReason = nullableString(unpricedReason)
	item.InputTokens = intValue(nullableInt32(inputTokens))
	item.OutputTokens = intValue(nullableInt32(outputTokens))
	item.HasOutputTokens = outputTokens.Valid
	item.TotalTokens = intValue(nullableInt32(totalTokens))
	item.CacheReadInputTokens = intValue(nullableInt32(cacheReadInputTokens))
	item.CacheCreationInputTokens = intValue(nullableInt32(cacheCreationInputTokens))
	item.ReasoningTokens = intValue(nullableInt32(reasoningTokens))
	item.TotalCostUserCurrencyMicros = int64Value(nullableInt64(totalCostUserCurrencyMicros))
	item.HasTotalCostUserCurrencyMicros = totalCostUserCurrencyMicros.Valid
	item.ResponseTimeMS = nullableInt32(responseTimeMS)
	item.TTFTMS = nullableInt32(ttftMS)
	item.CompletionDurationMS = nullableInt32(completionDurationMS)
	item.CurrentModelLabel = nullableString(currentModelLabel)
	item.CurrentEndpointName = nullableString(currentEndpointName)
	item.CurrentEndpointBaseURL = nullableString(currentEndpointBaseURL)
	item.CurrentProxyAPIKeyName = nullableString(currentProxyAPIKeyName)
	item.CurrentProxyAPIKeyPrefix = nullableString(currentProxyAPIKeyPrefix)
	return item, nil
}

func averageOutputRatePointer(sum float64, count int) *float64 {
	if count <= 0 {
		return nil
	}
	resolved := roundFloat(sum/float64(count), 2)
	return &resolved
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		items = append(items, trimmed)
	}
	return items
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}

func sumInts(values []int) float64 {
	total := 0.0
	for _, value := range values {
		total += float64(value)
	}
	return total
}

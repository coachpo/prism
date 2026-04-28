package stats

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type DashboardAggregateSnapshot struct {
	ProfileID            int
	GeneratedAt          time.Time
	StatsSummary24H      StatsSummaryResponse
	APIFamilySummary24H  StatsSummaryResponse
	SpendingSummary30D   SpendingReportResponse
	Throughput24H        ThroughputStatsResponse
	UsageSnapshotPreset1 UsageSnapshotResponse
}

type DashboardAggregateStore struct {
	mu        sync.RWMutex
	snapshots map[int]DashboardAggregateSnapshot
}

func NewDashboardAggregateStore() *DashboardAggregateStore {
	return &DashboardAggregateStore{snapshots: map[int]DashboardAggregateSnapshot{}}
}

func (s *DashboardAggregateStore) LoadProfile(profileID int) (DashboardAggregateSnapshot, bool) {
	if s == nil || profileID <= 0 {
		return DashboardAggregateSnapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[profileID]
	return snapshot, ok
}

func (s *DashboardAggregateStore) StoreProfile(snapshot DashboardAggregateSnapshot) {
	if s == nil || snapshot.ProfileID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snapshot.ProfileID] = snapshot
}

func (s *DashboardAggregateStore) InvalidateProfile(profileID int) {
	if s == nil || profileID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.snapshots, profileID)
}

func (s *DashboardAggregateStore) InvalidateAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = map[int]DashboardAggregateSnapshot{}
}

func BuildDashboardAggregateSnapshot(ctx context.Context, exec queryExecutor, profileID int, referenceNow time.Time) (DashboardAggregateSnapshot, error) {
	generatedAt := referenceNow.UTC()
	windowStart24H := generatedAt.Add(-24 * time.Hour)
	apiFamilyGroupBy := "api_family"
	statsSummary, err := GetStatsSummary(ctx, exec, StatsSummaryParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	apiFamilySummary, err := GetStatsSummary(ctx, exec, StatsSummaryParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt, GroupBy: &apiFamilyGroupBy})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	spendingSummary, err := GetSpending(ctx, exec, SpendingParams{ProfileID: profileID, Preset: "last_30_days", ToTime: &generatedAt, GroupBy: "none", Limit: 50, Offset: 0, TopN: 5, ReferenceNow: generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	throughput, err := GetThroughput(ctx, exec, ThroughputParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	usageSnapshot, err := GetUsageSnapshot(ctx, exec, profileID, "1h", generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	return DashboardAggregateSnapshot{
		ProfileID:            profileID,
		GeneratedAt:          generatedAt,
		StatsSummary24H:      statsSummary,
		APIFamilySummary24H:  apiFamilySummary,
		SpendingSummary30D:   spendingSummary,
		Throughput24H:        throughput,
		UsageSnapshotPreset1: usageSnapshot,
	}, nil
}

type requestTrendPointStats struct {
	requestCount int
	successCount int
	failedCount  int
}

type tokenTrendPointStats struct {
	totalTokens     int
	inputTokens     int
	outputTokens    int
	cachedTokens    int
	reasoningTokens int
}

func GetUsageSnapshot(ctx context.Context, exec queryExecutor, profileID int, preset string, referenceNow time.Time) (UsageSnapshotResponse, error) {
	generatedAt := referenceNow.UTC()
	startAt, endAt := resolveTimePreset(preset, nil, &generatedAt, generatedAt)
	normalizedEndAt := generatedAt
	if endAt != nil {
		normalizedEndAt = endAt.UTC()
	}
	records, err := loadUsageEventRecords(ctx, exec, profileID, startAt, &normalizedEndAt, nil, nil, nil, nil)
	if err != nil {
		return UsageSnapshotResponse{}, err
	}
	events := buildSnapshotEvents(records)
	currencyCode, currencySymbol, err := loadReportCurrencyPreferences(ctx, exec, profileID)
	if err != nil {
		return UsageSnapshotResponse{}, err
	}
	totalRequests := len(events)
	successRequests := 0
	totalTokens := 0
	inputTokens := 0
	outputTokens := 0
	cachedTokens := 0
	reasoningTokens := 0
	var totalCostMicros int64
	for _, event := range events {
		if event.SuccessFlag {
			successRequests++
		}
		totalTokens += event.TotalTokens
		inputTokens += event.InputTokens
		outputTokens += event.OutputTokens
		cachedTokens += event.CachedTokens
		reasoningTokens += event.ReasoningTokens
		totalCostMicros += event.TotalCostMicros
	}
	failedRequests := totalRequests - successRequests
	effectiveStart := effectiveWindowStart(startAt, normalizedEndAt, events)
	windowMinutes := normalizedEndAt.Sub(effectiveStart).Minutes()
	if windowMinutes < 0 {
		windowMinutes = 0
	}
	rollingWindowStart := normalizedEndAt.Add(-rollingWindowMinutes * time.Minute)
	rollingRequestCount := 0
	rollingTokenCount := 0
	for _, event := range events {
		if !event.CreatedAt.Before(rollingWindowStart) {
			rollingRequestCount++
			rollingTokenCount += event.TotalTokens
		}
	}
	serviceHealthStart, _ := serviceHealthWindowBounds(normalizedEndAt)
	serviceHealthRecords, err := loadUsageEventRecords(ctx, exec, profileID, &serviceHealthStart, &normalizedEndAt, nil, nil, nil, nil)
	if err != nil {
		return UsageSnapshotResponse{}, err
	}
	serviceHealthEvents := buildSnapshotEvents(serviceHealthRecords)
	requestTrends := UsageRequestTrends{
		Hourly: buildRequestTrendSeries(events, startAt, normalizedEndAt, "hour"),
		Daily:  buildRequestTrendSeries(events, startAt, normalizedEndAt, "day"),
	}
	tokenUsageTrends := UsageTokenUsageTrends{
		Hourly: buildTokenTrendSeries(events, startAt, normalizedEndAt, "hour"),
		Daily:  buildTokenTrendSeries(events, startAt, normalizedEndAt, "day"),
	}
	tokenTypeBreakdown := UsageTokenTypeBreakdown{
		Hourly: buildTokenTypeBreakdown(events, startAt, normalizedEndAt, "hour"),
		Daily:  buildTokenTypeBreakdown(events, startAt, normalizedEndAt, "day"),
	}
	costOverview := buildCostOverview(events, startAt, normalizedEndAt)
	return UsageSnapshotResponse{
		GeneratedAt: generatedAt,
		TimeRange: UsageSnapshotTimeRange{
			Preset:  preset,
			StartAt: startAt,
			EndAt:   normalizedEndAt,
		},
		Currency: UsageSnapshotCurrency{
			Code:   currencyCode,
			Symbol: currencySymbol,
		},
		Overview: UsageSnapshotOverview{
			TotalRequests:        totalRequests,
			SuccessRequests:      successRequests,
			FailedRequests:       failedRequests,
			SuccessRate:          successRate(successRequests, totalRequests),
			TotalTokens:          totalTokens,
			InputTokens:          inputTokens,
			OutputTokens:         outputTokens,
			CachedTokens:         cachedTokens,
			ReasoningTokens:      reasoningTokens,
			AverageRPM:           dividePerMinute(totalRequests, windowMinutes),
			AverageTPM:           dividePerMinute(totalTokens, windowMinutes),
			TotalCostMicros:      totalCostMicros,
			RollingWindowMinutes: rollingWindowMinutes,
			RollingRequestCount:  rollingRequestCount,
			RollingTokenCount:    rollingTokenCount,
			RollingRPM:           roundFloat(float64(rollingRequestCount)/float64(rollingWindowMinutes), 3),
			RollingTPM:           roundFloat(float64(rollingTokenCount)/float64(rollingWindowMinutes), 3),
		},
		ServiceHealth:         buildServiceHealth(serviceHealthEvents, normalizedEndAt),
		RequestTrends:         requestTrends,
		TokenUsageTrends:      tokenUsageTrends,
		TokenTypeBreakdown:    tokenTypeBreakdown,
		CostOverview:          costOverview,
		EndpointStatistics:    buildUsageEndpointStatistics(events),
		ModelStatistics:       buildUsageModelStatistics(events),
		ProxyAPIKeyStatistics: buildProxyAPIKeyStatistics(events),
	}, nil
}

func buildSnapshotEvents(records []usageEventRecord) []snapshotEvent {
	events := make([]snapshotEvent, 0, len(records))
	for _, rawRecord := range records {
		record := normalizeUsageEventPricingCoherence(rawRecord)
		endpointLabel := resolveEndpointLabel(record.CurrentEndpointName, record.CurrentEndpointBaseURL, nil, record.EndpointID, "Unknown Endpoint")
		proxyAPIKeyLabel := record.ProxyAPIKeyNameSnapshot
		if proxyAPIKeyLabel == nil {
			proxyAPIKeyLabel = record.CurrentProxyAPIKeyName
		}
		proxyAPIKeyStatsLabel := "No proxy API key"
		if proxyAPIKeyLabel != nil && *proxyAPIKeyLabel != "" {
			proxyAPIKeyStatsLabel = *proxyAPIKeyLabel
		}
		modelLabel := record.ModelID
		if record.CurrentModelLabel != nil && *record.CurrentModelLabel != "" {
			modelLabel = *record.CurrentModelLabel
		}
		totalCostMicros := int64(0)
		if record.BillableFlag {
			totalCostMicros = record.TotalCostUserCurrencyMicros
		}
		events = append(events, snapshotEvent{
			APIFamily:             record.APIFamily,
			AttemptCount:          record.AttemptCount,
			BillableFlag:          record.BillableFlag,
			CachedTokens:          record.CacheReadInputTokens + record.CacheCreationInputTokens,
			ConnectionID:          record.ConnectionID,
			CreatedAt:             record.CreatedAt.UTC(),
			EndpointID:            record.EndpointID,
			EndpointLabel:         endpointLabel,
			IngressRequestID:      record.IngressRequestID,
			InputTokens:           record.InputTokens,
			ModelID:               record.ModelID,
			ModelLabel:            modelLabel,
			OutputTokens:          record.OutputTokens,
			PricedFlag:            record.PricedFlag,
			ProxyAPIKeyID:         record.ProxyAPIKeyID,
			ProxyAPIKeyLabel:      proxyAPIKeyLabel,
			ProxyAPIKeyStatsLabel: proxyAPIKeyStatsLabel,
			ProxyAPIKeyPrefix:     record.CurrentProxyAPIKeyPrefix,
			ReasoningTokens:       record.ReasoningTokens,
			RequestPath:           record.RequestPath,
			ResolvedTargetModelID: record.ResolvedTargetModelID,
			StatusCode:            record.StatusCode,
			SuccessFlag:           record.SuccessFlag,
			ResponseTimeMS:        record.ResponseTimeMS,
			TTFTMS:                record.TTFTMS,
			CompletionDurationMS:  record.CompletionDurationMS,
			HasOutputTokens:       record.HasOutputTokens,
			TotalCostMicros:       totalCostMicros,
			TotalTokens:           record.TotalTokens,
		})
	}
	return events
}

const (
	statsUnpricedReasonMissingPriceData = "MISSING_PRICE_DATA"
	statsDefaultOneToOneFXRate          = "1"
	statsDefaultOneToOneFXSource        = "DEFAULT_1_TO_1"
)

func normalizeUsageEventPricingCoherence(record usageEventRecord) usageEventRecord {
	record.UnpricedReason = normalizeOptionalString(record.UnpricedReason)
	if !record.SuccessFlag || !record.BillableFlag {
		return record
	}
	if record.UnpricedReason != nil {
		record.PricedFlag = false
		return record
	}
	if record.HasTotalCostUserCurrencyMicros {
		record.PricedFlag = true
		return record
	}
	if record.PricedFlag {
		record.PricedFlag = false
		record.UnpricedReason = statsStringPtr(statsUnpricedReasonMissingPriceData)
	}
	return record
}

func normalizeObservedSpendCoherence(success bool, pricedFlag *bool, unpricedReason *string, hasTotalCost bool) (*bool, *string) {
	normalizedReason := normalizeOptionalString(unpricedReason)
	if normalizedReason != nil {
		return statsBoolPtr(false), normalizedReason
	}
	if success && hasTotalCost {
		return statsBoolPtr(true), nil
	}
	if success && pricedFlag != nil && *pricedFlag && !hasTotalCost {
		return statsBoolPtr(false), statsStringPtr(statsUnpricedReasonMissingPriceData)
	}
	return pricedFlag, nil
}

func normalizeObservedFXCoherence(success bool, pricedFlag *bool, unpricedReason *string, hasTotalCost bool, currencyCodeOriginal *string, reportCurrencyCode *string, fxRateUsed *string, fxRateSource *string) (*string, *string) {
	normalizedRate := normalizeOptionalString(fxRateUsed)
	normalizedSource := normalizeOptionalString(fxRateSource)
	if !success || !hasTotalCost || pricedFlag == nil || !*pricedFlag || normalizeOptionalString(unpricedReason) != nil {
		return nil, nil
	}
	if normalizedRate != nil || normalizedSource != nil {
		return normalizedRate, normalizedSource
	}
	normalizedOriginalCurrency := normalizeOptionalString(currencyCodeOriginal)
	normalizedReportCurrency := normalizeOptionalString(reportCurrencyCode)
	if normalizedOriginalCurrency != nil && normalizedReportCurrency != nil && *normalizedOriginalCurrency == *normalizedReportCurrency {
		return statsStringPtr(statsDefaultOneToOneFXRate), statsStringPtr(statsDefaultOneToOneFXSource)
	}
	return nil, nil
}

func statsBoolPtr(value bool) *bool {
	resolved := value
	return &resolved
}

func statsStringPtr(value string) *string {
	resolved := value
	return &resolved
}

func buildServiceHealth(events []snapshotEvent, endAt time.Time) UsageServiceHealth {
	windowStart, windowEnd := serviceHealthWindowBounds(endAt)
	type cellAggregate struct {
		requestCount int
		successCount int
		failedCount  int
	}
	cellsByBucket := map[time.Time]*cellAggregate{}
	requestCount := 0
	successCount := 0
	failedCount := 0
	for _, event := range events {
		if event.CreatedAt.Before(windowStart) || !event.CreatedAt.Before(windowEnd) {
			continue
		}
		bucket := serviceHealthBucketFloor(event.CreatedAt)
		cell := cellsByBucket[bucket]
		if cell == nil {
			cellsByBucket[bucket] = &cellAggregate{}
			cell = cellsByBucket[bucket]
		}
		cell.requestCount++
		requestCount++
		if event.SuccessFlag {
			cell.successCount++
			successCount++
		} else {
			cell.failedCount++
			failedCount++
		}
	}
	cells := make([]UsageServiceHealthCell, 0, serviceHealthBucketCount)
	bucket := windowStart
	for i := 0; i < serviceHealthBucketCount; i++ {
		cell := cellsByBucket[bucket]
		requestCountValue := 0
		successCountValue := 0
		failedCountValue := 0
		if cell != nil {
			requestCountValue = cell.requestCount
			successCountValue = cell.successCount
			failedCountValue = cell.failedCount
		}
		cells = append(cells, UsageServiceHealthCell{
			BucketStart:            bucket,
			RequestCount:           requestCountValue,
			SuccessCount:           successCountValue,
			FailedCount:            failedCountValue,
			AvailabilityPercentage: nullableSuccessRate(successCountValue, requestCountValue),
			Status:                 serviceHealthStatus(requestCountValue, successCountValue, failedCountValue),
		})
		bucket = bucket.Add(time.Duration(serviceHealthIntervalMins) * time.Minute)
	}
	return UsageServiceHealth{
		AvailabilityPercentage: nullableSuccessRate(successCount, requestCount),
		RequestCount:           requestCount,
		SuccessCount:           successCount,
		FailedCount:            failedCount,
		IntervalMinutes:        serviceHealthIntervalMins,
		Cells:                  cells,
	}
}

func buildRequestTrendSeries(events []snapshotEvent, startAt *time.Time, endAt time.Time, granularity string) []UsageRequestTrendSeries {
	buckets := bucketRange(startAt, endAt, timeSliceFromEvents(events), granularity)
	bucketMinuteValue := bucketMinutes(granularity)
	overall := map[time.Time]*requestTrendPointStats{}
	modelTotals := map[string]int{}
	modelLabels := map[string]string{}
	byModel := map[string]map[time.Time]*requestTrendPointStats{}
	for _, event := range events {
		bucket := bucketFloor(event.CreatedAt, granularity)
		stat := overall[bucket]
		if stat == nil {
			overall[bucket] = &requestTrendPointStats{}
			stat = overall[bucket]
		}
		stat.requestCount++
		if event.SuccessFlag {
			stat.successCount++
		} else {
			stat.failedCount++
		}
		modelTotals[event.ModelID]++
		modelLabels[event.ModelID] = event.ModelLabel
		modelBucketStats := byModel[event.ModelID]
		if modelBucketStats == nil {
			byModel[event.ModelID] = map[time.Time]*requestTrendPointStats{}
			modelBucketStats = byModel[event.ModelID]
		}
		bucketStat := modelBucketStats[bucket]
		if bucketStat == nil {
			modelBucketStats[bucket] = &requestTrendPointStats{}
			bucketStat = modelBucketStats[bucket]
		}
		bucketStat.requestCount++
		if event.SuccessFlag {
			bucketStat.successCount++
		} else {
			bucketStat.failedCount++
		}
	}
	items := []UsageRequestTrendSeries{{
		Key:           "all",
		Label:         "All Models",
		TotalRequests: len(events),
		Points:        makeUsageRequestTrendPoints(buckets, overall, bucketMinuteValue),
	}}
	modelIDs := make([]string, 0, len(modelTotals))
	for modelID := range modelTotals {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Slice(modelIDs, func(i int, j int) bool {
		leftLabel := modelLabels[modelIDs[i]]
		rightLabel := modelLabels[modelIDs[j]]
		if leftLabel != rightLabel {
			return leftLabel < rightLabel
		}
		return modelIDs[i] < modelIDs[j]
	})
	for _, modelID := range modelIDs {
		items = append(items, UsageRequestTrendSeries{
			Key:           modelID,
			Label:         modelLabels[modelID],
			TotalRequests: modelTotals[modelID],
			Points:        makeUsageRequestTrendPoints(buckets, byModel[modelID], bucketMinuteValue),
		})
	}
	return items
}

func buildTokenTrendSeries(events []snapshotEvent, startAt *time.Time, endAt time.Time, granularity string) []UsageTokenTrendSeries {
	buckets := bucketRange(startAt, endAt, timeSliceFromEvents(events), granularity)
	bucketMinuteValue := bucketMinutes(granularity)
	overall := map[time.Time]*tokenTrendPointStats{}
	modelTotals := map[string]int{}
	modelLabels := map[string]string{}
	byModel := map[string]map[time.Time]*tokenTrendPointStats{}
	for _, event := range events {
		bucket := bucketFloor(event.CreatedAt, granularity)
		stat := overall[bucket]
		if stat == nil {
			overall[bucket] = &tokenTrendPointStats{}
			stat = overall[bucket]
		}
		stat.totalTokens += event.TotalTokens
		stat.inputTokens += event.InputTokens
		stat.outputTokens += event.OutputTokens
		stat.cachedTokens += event.CachedTokens
		stat.reasoningTokens += event.ReasoningTokens
		modelTotals[event.ModelID] += event.TotalTokens
		modelLabels[event.ModelID] = event.ModelLabel
		modelBucketStats := byModel[event.ModelID]
		if modelBucketStats == nil {
			byModel[event.ModelID] = map[time.Time]*tokenTrendPointStats{}
			modelBucketStats = byModel[event.ModelID]
		}
		bucketStat := modelBucketStats[bucket]
		if bucketStat == nil {
			modelBucketStats[bucket] = &tokenTrendPointStats{}
			bucketStat = modelBucketStats[bucket]
		}
		bucketStat.totalTokens += event.TotalTokens
		bucketStat.inputTokens += event.InputTokens
		bucketStat.outputTokens += event.OutputTokens
		bucketStat.cachedTokens += event.CachedTokens
		bucketStat.reasoningTokens += event.ReasoningTokens
	}
	items := []UsageTokenTrendSeries{{
		Key:         "all",
		Label:       "All Models",
		TotalTokens: totalTokensForEvents(events),
		Points:      makeUsageTokenTrendPoints(buckets, overall, bucketMinuteValue),
	}}
	modelIDs := make([]string, 0, len(modelTotals))
	for modelID := range modelTotals {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Slice(modelIDs, func(i int, j int) bool {
		leftLabel := modelLabels[modelIDs[i]]
		rightLabel := modelLabels[modelIDs[j]]
		if leftLabel != rightLabel {
			return leftLabel < rightLabel
		}
		return modelIDs[i] < modelIDs[j]
	})
	for _, modelID := range modelIDs {
		items = append(items, UsageTokenTrendSeries{
			Key:         modelID,
			Label:       modelLabels[modelID],
			TotalTokens: modelTotals[modelID],
			Points:      makeUsageTokenTrendPoints(buckets, byModel[modelID], bucketMinuteValue),
		})
	}
	return items
}

func buildTokenTypeBreakdown(events []snapshotEvent, startAt *time.Time, endAt time.Time, granularity string) []UsageTokenTypeBreakdownPoint {
	buckets := bucketRange(startAt, endAt, timeSliceFromEvents(events), granularity)
	type tokenBreakdown struct {
		inputTokens     int
		outputTokens    int
		cachedTokens    int
		reasoningTokens int
	}
	itemsByBucket := map[time.Time]*tokenBreakdown{}
	for _, event := range events {
		bucket := bucketFloor(event.CreatedAt, granularity)
		item := itemsByBucket[bucket]
		if item == nil {
			itemsByBucket[bucket] = &tokenBreakdown{}
			item = itemsByBucket[bucket]
		}
		item.inputTokens += event.InputTokens
		item.outputTokens += event.OutputTokens
		item.cachedTokens += event.CachedTokens
		item.reasoningTokens += event.ReasoningTokens
	}
	points := make([]UsageTokenTypeBreakdownPoint, 0, len(buckets))
	for _, bucket := range buckets {
		item := itemsByBucket[bucket]
		if item == nil {
			points = append(points, UsageTokenTypeBreakdownPoint{BucketStart: bucket})
			continue
		}
		points = append(points, UsageTokenTypeBreakdownPoint{BucketStart: bucket, InputTokens: item.inputTokens, OutputTokens: item.outputTokens, CachedTokens: item.cachedTokens, ReasoningTokens: item.reasoningTokens})
	}
	return points
}

func buildCostOverview(events []snapshotEvent, startAt *time.Time, endAt time.Time) UsageCostOverview {
	pricedRequestCount := 0
	unpricedRequestCount := 0
	for _, event := range events {
		if event.SuccessFlag && event.PricedFlag {
			pricedRequestCount++
		} else if event.SuccessFlag && !event.PricedFlag {
			unpricedRequestCount++
		}
	}
	buildPoints := func(granularity string) []UsageCostOverviewPoint {
		buckets := bucketRange(startAt, endAt, timeSliceFromEvents(events), granularity)
		totals := map[time.Time]int64{}
		for _, event := range events {
			bucket := bucketFloor(event.CreatedAt, granularity)
			totals[bucket] += event.TotalCostMicros
		}
		items := make([]UsageCostOverviewPoint, 0, len(buckets))
		for _, bucket := range buckets {
			items = append(items, UsageCostOverviewPoint{BucketStart: bucket, TotalCostMicros: totals[bucket]})
		}
		return items
	}
	var totalCostMicros int64
	for _, event := range events {
		totalCostMicros += event.TotalCostMicros
	}
	return UsageCostOverview{TotalCostMicros: totalCostMicros, PricedRequestCount: pricedRequestCount, UnpricedRequestCount: unpricedRequestCount, Hourly: buildPoints("hour"), Daily: buildPoints("day")}
}

func buildUsageEndpointStatistics(events []snapshotEvent) []UsageEndpointStatistic {
	type endpointAggregate struct {
		endpointID          *int
		endpointLabel       string
		requestCount        int
		successCount        int
		failedCount         int
		ttftValues          []int
		outputRateSum       float64
		eligibleOutputRates int
		totalTokens         int
		totalCostMicros     int64
	}
	groups := map[string]*endpointAggregate{}
	for _, event := range events {
		key := fmt.Sprintf("%d\x00%s", endpointIDOrMinusOne(event.EndpointID), event.EndpointLabel)
		group := groups[key]
		if group == nil {
			groups[key] = &endpointAggregate{endpointID: event.EndpointID, endpointLabel: event.EndpointLabel}
			group = groups[key]
		}
		group.requestCount++
		if event.SuccessFlag {
			group.successCount++
		} else {
			group.failedCount++
		}
		if event.TTFTMS != nil {
			group.ttftValues = append(group.ttftValues, *event.TTFTMS)
		}
		if outputRate := requestOutputRateTPS(event.OutputTokens, event.HasOutputTokens, event.TTFTMS, event.CompletionDurationMS); outputRate != nil {
			group.outputRateSum += *outputRate
			group.eligibleOutputRates++
		}
		group.totalTokens += event.TotalTokens
		group.totalCostMicros += event.TotalCostMicros
	}
	items := make([]UsageEndpointStatistic, 0, len(groups))
	for _, group := range groups {
		items = append(items, UsageEndpointStatistic{EndpointID: group.endpointID, EndpointLabel: group.endpointLabel, RequestCount: group.requestCount, SuccessRate: successRate(group.successCount, group.requestCount), P50TTFTMS: percentileContInt(group.ttftValues, 0.5), P95TTFTMS: percentileContInt(group.ttftValues, 0.95), AvgOutputRateTPS: averageOutputRatePointer(group.outputRateSum, group.eligibleOutputRates), TotalTokens: group.totalTokens, TotalCostMicros: group.totalCostMicros})
	}
	sort.Slice(items, func(i int, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		return items[i].EndpointLabel < items[j].EndpointLabel
	})
	return items
}

func buildUsageModelStatistics(events []snapshotEvent) []UsageModelStatistic {
	type modelAggregate struct {
		modelID              string
		modelLabel           string
		requestCount         int
		successCount         int
		failedCount          int
		pricedRequestCount   int
		unpricedRequestCount int
		inputTokens          int
		outputTokens         int
		cachedTokens         int
		reasoningTokens      int
		ttftValues           []int
		outputRateSum        float64
		eligibleOutputRates  int
		totalTokens          int
		totalCostMicros      int64
	}
	groups := map[string]*modelAggregate{}
	for _, event := range events {
		group := groups[event.ModelID]
		if group == nil {
			groups[event.ModelID] = &modelAggregate{modelID: event.ModelID, modelLabel: event.ModelLabel}
			group = groups[event.ModelID]
		}
		group.requestCount++
		if event.SuccessFlag {
			group.successCount++
			if event.PricedFlag {
				group.pricedRequestCount++
			} else {
				group.unpricedRequestCount++
			}
		} else {
			group.failedCount++
		}
		if event.TTFTMS != nil {
			group.ttftValues = append(group.ttftValues, *event.TTFTMS)
		}
		if outputRate := requestOutputRateTPS(event.OutputTokens, event.HasOutputTokens, event.TTFTMS, event.CompletionDurationMS); outputRate != nil {
			group.outputRateSum += *outputRate
			group.eligibleOutputRates++
		}
		group.inputTokens += event.InputTokens
		group.outputTokens += event.OutputTokens
		group.cachedTokens += event.CachedTokens
		group.reasoningTokens += event.ReasoningTokens
		group.totalTokens += event.TotalTokens
		group.totalCostMicros += event.TotalCostMicros
	}
	items := make([]UsageModelStatistic, 0, len(groups))
	for _, group := range groups {
		items = append(items, UsageModelStatistic{ModelID: group.modelID, ModelLabel: group.modelLabel, RequestCount: group.requestCount, SuccessCount: intPtr(group.successCount), FailedCount: intPtr(group.failedCount), PricedRequestCount: intPtr(group.pricedRequestCount), UnpricedRequestCount: intPtr(group.unpricedRequestCount), SuccessRate: successRate(group.successCount, group.requestCount), P50TTFTMS: percentileContInt(group.ttftValues, 0.5), P95TTFTMS: percentileContInt(group.ttftValues, 0.95), InputTokens: intPtr(group.inputTokens), OutputTokens: intPtr(group.outputTokens), CachedTokens: intPtr(group.cachedTokens), ReasoningTokens: intPtr(group.reasoningTokens), TotalTokens: group.totalTokens, TotalCostMicros: group.totalCostMicros, AvgOutputRateTPS: averageOutputRatePointer(group.outputRateSum, group.eligibleOutputRates)})
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
	return items
}

func buildProxyAPIKeyStatistics(events []snapshotEvent) []UsageProxyAPIKeyStatistic {
	type proxyAggregate struct {
		proxyAPIKeyID    *int
		proxyAPIKeyLabel string
		requestCount     int
		successCount     int
		failedCount      int
		totalTokens      int
		totalCostMicros  int64
	}
	groups := map[string]*proxyAggregate{}
	for _, event := range events {
		key := fmt.Sprintf("%d\x00%s\x00%s", endpointIDOrMinusOne(event.ProxyAPIKeyID), event.ProxyAPIKeyStatsLabel, valueOrEmpty(event.ProxyAPIKeyPrefix))
		group := groups[key]
		if group == nil {
			groups[key] = &proxyAggregate{proxyAPIKeyID: event.ProxyAPIKeyID, proxyAPIKeyLabel: event.ProxyAPIKeyStatsLabel}
			group = groups[key]
		}
		group.requestCount++
		if event.SuccessFlag {
			group.successCount++
		} else {
			group.failedCount++
		}
		group.totalTokens += event.TotalTokens
		group.totalCostMicros += event.TotalCostMicros
	}
	items := make([]UsageProxyAPIKeyStatistic, 0, len(groups))
	for _, group := range groups {
		items = append(items, UsageProxyAPIKeyStatistic{ProxyAPIKeyID: group.proxyAPIKeyID, ProxyAPIKeyLabel: group.proxyAPIKeyLabel, RequestCount: group.requestCount, SuccessRate: successRate(group.successCount, group.requestCount), TotalTokens: group.totalTokens, TotalCostMicros: group.totalCostMicros})
	}
	sort.Slice(items, func(i int, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		return items[i].ProxyAPIKeyLabel < items[j].ProxyAPIKeyLabel
	})
	return items
}

func makeUsageRequestTrendPoints(buckets []time.Time, stats map[time.Time]*requestTrendPointStats, bucketMinutes float64) []UsageRequestTrendPoint {
	points := make([]UsageRequestTrendPoint, 0, len(buckets))
	for _, bucket := range buckets {
		stat := stats[bucket]
		if stat == nil {
			points = append(points, UsageRequestTrendPoint{BucketStart: bucket})
			continue
		}
		points = append(points, UsageRequestTrendPoint{BucketStart: bucket, RequestCount: stat.requestCount, SuccessCount: stat.successCount, FailedCount: stat.failedCount, RPM: roundFloat(float64(stat.requestCount)/bucketMinutes, 3)})
	}
	return points
}

func makeUsageTokenTrendPoints(buckets []time.Time, stats map[time.Time]*tokenTrendPointStats, bucketMinutes float64) []UsageTokenTrendPoint {
	points := make([]UsageTokenTrendPoint, 0, len(buckets))
	for _, bucket := range buckets {
		stat := stats[bucket]
		if stat == nil {
			points = append(points, UsageTokenTrendPoint{BucketStart: bucket})
			continue
		}
		points = append(points, UsageTokenTrendPoint{BucketStart: bucket, TotalTokens: stat.totalTokens, InputTokens: stat.inputTokens, OutputTokens: stat.outputTokens, CachedTokens: stat.cachedTokens, ReasoningTokens: stat.reasoningTokens, TPM: roundFloat(float64(stat.totalTokens)/bucketMinutes, 3)})
	}
	return points
}

func dividePerMinute(total int, windowMinutes float64) float64 {
	if windowMinutes <= 0 {
		return 0
	}
	return roundFloat(float64(total)/windowMinutes, 3)
}

func nullableSuccessRate(successCount int, requestCount int) *float64 {
	if requestCount <= 0 {
		return nil
	}
	rate := successRate(successCount, requestCount)
	return &rate
}

func endpointIDOrMinusOne(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func totalTokensForEvents(events []snapshotEvent) int {
	total := 0
	for _, event := range events {
		total += event.TotalTokens
	}
	return total
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

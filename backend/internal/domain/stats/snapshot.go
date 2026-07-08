package stats

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"
)

type DashboardAggregateSnapshot struct {
	ProfileID                 int
	GeneratedAt               time.Time
	SnapshotRevision          string
	SourceWatermark           DashboardSnapshotSourceWatermark
	StatsSummary24H           StatsSummaryResponse
	APIFamilySummary24H       StatsSummaryResponse
	SpendingSummary30D        SpendingReportResponse
	Throughput24H             ThroughputStatsResponse
	UsageSnapshotPreset1      UsageSnapshotResponse
	StreamRequestCount24H     int
	UsageEventRequestCount24H int
	RoutingHealthMap          DashboardRoutingHealthMap
	TopologyGraph             DashboardTopologyGraph
	TotalModelCount           int
	ActiveModelCount          int
}

type DashboardSnapshot struct {
	GeneratedAt       time.Time                        `json:"generated_at"`
	SnapshotRevision  string                           `json:"snapshot_revision"`
	SourceWatermark   DashboardSnapshotSourceWatermark `json:"source_watermark"`
	Coverage24H       DashboardSnapshotCoverage        `json:"coverage_24h"`
	Coverage30D       DashboardSnapshotCoverage        `json:"coverage_30d"`
	Health            DashboardSnapshotHealth          `json:"health"`
	MetricSnapshot    DashboardMetricSnapshot          `json:"metric_snapshot"`
	APIFamilyRows     []StatGroup                      `json:"api_family_rows"`
	TopSpendingModels []SpendingTopModel               `json:"top_spending_models"`
	RoutingHealthMap  DashboardRoutingHealthMap        `json:"routing_health_map"`
	TopologyGraph     DashboardTopologyGraph           `json:"topology_graph"`
}

type DashboardSnapshotSourceWatermark struct {
	LatestUsageEventCreatedAt *time.Time `json:"latest_usage_event_created_at"`
	LatestUsageEventID        *int       `json:"latest_usage_event_id"`
}

type DashboardMetricSnapshot struct {
	ActiveModels           int     `json:"active_models"`
	AverageRPM             float64 `json:"average_rpm"`
	AverageRPMRequestTotal int     `json:"average_rpm_request_total"`
	AvgLatency             float64 `json:"avg_latency"`
	ErrorRate              float64 `json:"error_rate"`
	P95Latency             int     `json:"p95_latency"`
	PricedRequestCount     int     `json:"priced_request_count"`
	StreamShare            float64 `json:"stream_share"`
	SuccessRate            float64 `json:"success_rate"`
	TotalCost              int64   `json:"total_cost"`
	TotalModels            int     `json:"total_models"`
	TotalRequests          int     `json:"total_requests"`
	UnpricedRequestCount   int     `json:"unpriced_request_count"`
}

type DashboardRoutingHealthMap struct {
	Nodes                     []DashboardRoutingNode `json:"nodes"`
	Links                     []DashboardRoutingLink `json:"links"`
	EndpointCount             int                    `json:"endpointCount"`
	ModelCount                int                    `json:"modelCount"`
	ActiveConnectionTotal     int                    `json:"activeConnectionTotal"`
	ActiveTerminalTargetTotal int                    `json:"activeTerminalTargetTotal"`
	TrafficRequestTotal24H    int                    `json:"trafficRequestTotal24h"`
}

type DashboardRoutingNode struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Kind                      string   `json:"kind"`
	Label                     string   `json:"label"`
	Sublabel                  *string  `json:"sublabel"`
	EndpointID                *int     `json:"endpointId"`
	ModelID                   *string  `json:"modelId"`
	ModelConfigID             *int     `json:"modelConfigId"`
	ActiveConnectionCount     int      `json:"activeConnectionCount"`
	ActiveTerminalTargetCount int      `json:"activeTerminalTargetCount"`
	TrafficRequestCount24H    int      `json:"trafficRequestCount24h"`
	RequestCount24H           int      `json:"requestCount24h"`
	SuccessCount24H           int      `json:"successCount24h"`
	ErrorCount24H             int      `json:"errorCount24h"`
	SuccessRate24H            *float64 `json:"successRate24h"`
}

type DashboardRoutingLink struct {
	ID                        string   `json:"id"`
	SourceNodeID              string   `json:"sourceNodeId"`
	TargetNodeID              string   `json:"targetNodeId"`
	ModelID                   string   `json:"modelId"`
	ModelLabel                string   `json:"modelLabel"`
	ModelConfigID             int      `json:"modelConfigId"`
	EndpointID                int      `json:"endpointId"`
	EndpointLabel             string   `json:"endpointLabel"`
	ActiveConnectionCount     int      `json:"activeConnectionCount"`
	ActiveTerminalTargetCount int      `json:"activeTerminalTargetCount"`
	TrafficRequestCount24H    int      `json:"trafficRequestCount24h"`
	RequestCount24H           int      `json:"requestCount24h"`
	SuccessCount24H           int      `json:"successCount24h"`
	ErrorCount24H             int      `json:"errorCount24h"`
	SuccessRate24H            *float64 `json:"successRate24h"`
}

func cloneDashboardSnapshotSourceWatermark(value DashboardSnapshotSourceWatermark) DashboardSnapshotSourceWatermark {
	clone := DashboardSnapshotSourceWatermark{}
	if value.LatestUsageEventCreatedAt != nil {
		createdAt := value.LatestUsageEventCreatedAt.UTC()
		clone.LatestUsageEventCreatedAt = &createdAt
	}
	if value.LatestUsageEventID != nil {
		latestID := *value.LatestUsageEventID
		clone.LatestUsageEventID = &latestID
	}
	return clone
}

func NewDashboardSnapshot(aggregate DashboardAggregateSnapshot, referenceNow time.Time) DashboardSnapshot {
	generatedAt := aggregate.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = referenceNow.UTC()
	}
	apiFamilyRows := append([]StatGroup{}, aggregate.APIFamilySummary24H.Groups...)
	topSpendingModels := append([]SpendingTopModel{}, aggregate.SpendingSummary30D.TopSpendingModels...)
	return DashboardSnapshot{
		GeneratedAt:       generatedAt,
		SnapshotRevision:  aggregate.SnapshotRevision,
		SourceWatermark:   cloneDashboardSnapshotSourceWatermark(aggregate.SourceWatermark),
		Coverage24H:       DashboardSnapshotCoverage{From: generatedAt.Add(-24 * time.Hour), To: generatedAt},
		Coverage30D:       DashboardSnapshotCoverage{From: generatedAt.Add(-30 * 24 * time.Hour), To: generatedAt},
		Health:            NewDashboardSnapshotHealth(generatedAt, referenceNow),
		MetricSnapshot:    newDashboardMetricSnapshot(aggregate),
		APIFamilyRows:     apiFamilyRows,
		TopSpendingModels: topSpendingModels,
		RoutingHealthMap:  cloneDashboardRoutingHealthMap(aggregate.RoutingHealthMap),
		TopologyGraph:     cloneDashboardTopologyGraph(aggregate.TopologyGraph),
	}
}

func newDashboardMetricSnapshot(aggregate DashboardAggregateSnapshot) DashboardMetricSnapshot {
	streamShare := 0.0
	if aggregate.UsageEventRequestCount24H > 0 {
		streamShare = roundFloat((float64(aggregate.StreamRequestCount24H)/float64(aggregate.UsageEventRequestCount24H))*100, 2)
	}
	errorRate := 100 - aggregate.StatsSummary24H.SuccessRate
	if errorRate < 0 {
		errorRate = 0
	}
	return DashboardMetricSnapshot{
		ActiveModels:           aggregate.ActiveModelCount,
		AverageRPM:             aggregate.Throughput24H.AverageRPM,
		AverageRPMRequestTotal: aggregate.Throughput24H.TotalRequests,
		AvgLatency:             aggregate.StatsSummary24H.AvgResponseTimeMS,
		ErrorRate:              errorRate,
		P95Latency:             aggregate.StatsSummary24H.P95ResponseTimeMS,
		PricedRequestCount:     aggregate.SpendingSummary30D.Summary.PricedRequestCount,
		StreamShare:            streamShare,
		SuccessRate:            aggregate.StatsSummary24H.SuccessRate,
		TotalCost:              aggregate.SpendingSummary30D.Summary.TotalCostMicros,
		TotalModels:            aggregate.TotalModelCount,
		TotalRequests:          aggregate.StatsSummary24H.TotalRequests,
		UnpricedRequestCount:   aggregate.SpendingSummary30D.Summary.UnpricedRequestCount,
	}
}

type DashboardAggregateInvalidation struct {
	ProfileID int
	All       bool
}

type DashboardAggregateInvalidationListener func(DashboardAggregateInvalidation)

type DashboardAggregateStore struct {
	mu        sync.RWMutex
	snapshots map[int]DashboardAggregateSnapshot
	listeners []DashboardAggregateInvalidationListener
}

func NewDashboardAggregateStore() *DashboardAggregateStore {
	return &DashboardAggregateStore{snapshots: map[int]DashboardAggregateSnapshot{}}
}

func (s *DashboardAggregateStore) RegisterInvalidationListener(listener DashboardAggregateInvalidationListener) {
	if s == nil || listener == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
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

func (s *DashboardAggregateStore) LoadFreshProfile(profileID int, isFresh func(DashboardAggregateSnapshot) bool) (DashboardAggregateSnapshot, bool) {
	snapshot, ok := s.LoadProfile(profileID)
	if !ok {
		return DashboardAggregateSnapshot{}, false
	}
	if isFresh != nil && !isFresh(snapshot) {
		return DashboardAggregateSnapshot{}, false
	}
	return snapshot, true
}

func (s *DashboardAggregateStore) StoreProfile(snapshot DashboardAggregateSnapshot) {
	if s == nil || snapshot.ProfileID <= 0 {
		return
	}
	snapshot = ensureDashboardAggregateSnapshotRevision(snapshot)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snapshot.ProfileID] = snapshot
}

func ensureDashboardAggregateSnapshotRevision(snapshot DashboardAggregateSnapshot) DashboardAggregateSnapshot {
	if strings.TrimSpace(snapshot.SnapshotRevision) == "" {
		snapshot.SnapshotRevision = newDashboardSnapshotRevision(snapshot.GeneratedAt)
	}
	return snapshot
}

const dashboardRevisionAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var dashboardRevisionGenerator = struct {
	sync.Mutex
	lastTimestampMS uint64
	lastEntropy     [10]byte
}{}

func newDashboardSnapshotRevision(referenceNow time.Time) string {
	referenceNow = referenceNow.UTC()
	if referenceNow.IsZero() {
		referenceNow = time.Now().UTC()
	}
	timestampMS := uint64(referenceNow.UnixMilli())
	entropy := randomDashboardRevisionEntropy()

	dashboardRevisionGenerator.Lock()
	defer dashboardRevisionGenerator.Unlock()
	if timestampMS <= dashboardRevisionGenerator.lastTimestampMS {
		timestampMS = dashboardRevisionGenerator.lastTimestampMS
		entropy = dashboardRevisionGenerator.lastEntropy
		if !incrementDashboardRevisionEntropy(&entropy) {
			timestampMS++
			entropy = randomDashboardRevisionEntropy()
		}
	}
	dashboardRevisionGenerator.lastTimestampMS = timestampMS
	dashboardRevisionGenerator.lastEntropy = entropy
	return encodeDashboardRevisionULID(timestampMS, entropy)
}

func randomDashboardRevisionEntropy() [10]byte {
	var entropy [10]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic(fmt.Sprintf("generate dashboard snapshot revision entropy: %v", err))
	}
	return entropy
}

func incrementDashboardRevisionEntropy(entropy *[10]byte) bool {
	for index := len(entropy) - 1; index >= 0; index-- {
		entropy[index]++
		if entropy[index] != 0 {
			return true
		}
	}
	return false
}

func encodeDashboardRevisionULID(timestampMS uint64, entropy [10]byte) string {
	var payload [16]byte
	payload[0] = byte(timestampMS >> 40)
	payload[1] = byte(timestampMS >> 32)
	payload[2] = byte(timestampMS >> 24)
	payload[3] = byte(timestampMS >> 16)
	payload[4] = byte(timestampMS >> 8)
	payload[5] = byte(timestampMS)
	copy(payload[6:], entropy[:])

	value := new(big.Int).SetBytes(payload[:])
	base := big.NewInt(32)
	mod := new(big.Int)
	var encoded [26]byte
	for index := len(encoded) - 1; index >= 0; index-- {
		value.DivMod(value, base, mod)
		encoded[index] = dashboardRevisionAlphabet[mod.Int64()]
	}
	return string(encoded[:])
}

func (s *DashboardAggregateStore) InvalidateProfile(profileID int) {
	s.invalidateProfile(profileID, true)
}

func (s *DashboardAggregateStore) InvalidateProfileSilently(profileID int) {
	s.invalidateProfile(profileID, false)
}

func (s *DashboardAggregateStore) InvalidateAll() {
	s.invalidateAll(true)
}

func (s *DashboardAggregateStore) InvalidateAllSilently() {
	s.invalidateAll(false)
}

func (s *DashboardAggregateStore) invalidateProfile(profileID int, notify bool) {
	if s == nil || profileID <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.snapshots, profileID)
	listeners := s.invalidationListenersLocked(notify)
	s.mu.Unlock()
	notifyDashboardAggregateInvalidation(listeners, DashboardAggregateInvalidation{ProfileID: profileID})
}

func (s *DashboardAggregateStore) invalidateAll(notify bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.snapshots = map[int]DashboardAggregateSnapshot{}
	listeners := s.invalidationListenersLocked(notify)
	s.mu.Unlock()
	notifyDashboardAggregateInvalidation(listeners, DashboardAggregateInvalidation{All: true})
}

func (s *DashboardAggregateStore) invalidationListenersLocked(notify bool) []DashboardAggregateInvalidationListener {
	if !notify || len(s.listeners) == 0 {
		return nil
	}
	return append([]DashboardAggregateInvalidationListener{}, s.listeners...)
}

func notifyDashboardAggregateInvalidation(listeners []DashboardAggregateInvalidationListener, invalidation DashboardAggregateInvalidation) {
	for _, listener := range listeners {
		listener(invalidation)
	}
}

func BuildDashboardAggregateSnapshot(ctx context.Context, exec queryExecutor, profileID int, referenceNow time.Time) (DashboardAggregateSnapshot, error) {
	generatedAt := referenceNow.UTC()
	windowStart24H := generatedAt.Add(-24 * time.Hour)
	apiFamilyGroupBy := "api_family"
	models, err := loadDashboardSnapshotModels(ctx, exec, profileID)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	statsSummary, err := GetDashboardStatsSummary(ctx, exec, StatsSummaryParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	apiFamilySummary, err := GetDashboardStatsSummary(ctx, exec, StatsSummaryParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt, GroupBy: &apiFamilyGroupBy})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	spendingSummary, err := GetSpending(ctx, exec, SpendingParams{ProfileID: profileID, Preset: "last_30_days", ToTime: &generatedAt, GroupBy: "none", Limit: 50, Offset: 0, TopN: 5, ReferenceNow: generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	throughput, err := GetDashboardThroughput(ctx, exec, ThroughputParams{ProfileID: profileID, FromTime: &windowStart24H, ToTime: &generatedAt})
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	usageSnapshot, err := GetUsageSnapshot(ctx, exec, profileID, "1h", generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	streamRequestCount, usageEventRequestCount, err := loadDashboardStreamRequestCounts(ctx, exec, profileID, windowStart24H, generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	sourceWatermark, err := loadDashboardSnapshotSourceWatermark(ctx, exec, profileID, generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	routingHealthMap, err := buildDashboardRoutingHealthMap(ctx, exec, profileID, models, windowStart24H, generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	topologyGraph, err := buildDashboardTopologyGraph(ctx, exec, profileID, models, windowStart24H, generatedAt)
	if err != nil {
		return DashboardAggregateSnapshot{}, err
	}
	return DashboardAggregateSnapshot{
		ProfileID:                 profileID,
		GeneratedAt:               generatedAt,
		SnapshotRevision:          newDashboardSnapshotRevision(generatedAt),
		SourceWatermark:           sourceWatermark,
		StatsSummary24H:           statsSummary,
		APIFamilySummary24H:       apiFamilySummary,
		SpendingSummary30D:        spendingSummary,
		Throughput24H:             throughput,
		UsageSnapshotPreset1:      usageSnapshot,
		StreamRequestCount24H:     streamRequestCount,
		UsageEventRequestCount24H: usageEventRequestCount,
		RoutingHealthMap:          routingHealthMap,
		TopologyGraph:             topologyGraph,
		TotalModelCount:           len(models),
		ActiveModelCount:          countDashboardActiveModels(models),
	}, nil
}

func loadDashboardStreamRequestCounts(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time) (int, int, error) {
	var streamCount int64
	var totalCount int64
	if err := exec.QueryRow(ctx, `SELECT
		COUNT(*) FILTER (WHERE COALESCE(NULLIF(stream_outcome, ''), 'not_streaming') <> 'not_streaming'),
		COUNT(*)
		FROM usage_request_events
		WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3`, profileID, fromTime.UTC(), toTime.UTC()).Scan(&streamCount, &totalCount); err != nil {
		return 0, 0, fmt.Errorf("query dashboard usage-event stream request counts for profile %d: %w", profileID, err)
	}
	return int(streamCount), int(totalCount), nil
}

func loadDashboardSnapshotSourceWatermark(ctx context.Context, exec queryExecutor, profileID int, generatedAt time.Time) (DashboardSnapshotSourceWatermark, error) {
	var createdAt sql.NullTime
	var id sql.NullInt64
	if err := exec.QueryRow(ctx, `SELECT
		(SELECT created_at FROM usage_request_events WHERE profile_id = $1 AND created_at <= $2 ORDER BY created_at DESC, id DESC LIMIT 1),
		(SELECT id FROM usage_request_events WHERE profile_id = $1 AND created_at <= $2 ORDER BY created_at DESC, id DESC LIMIT 1)`, profileID, generatedAt.UTC()).Scan(&createdAt, &id); err != nil {
		return DashboardSnapshotSourceWatermark{}, fmt.Errorf("query dashboard source watermark for profile %d: %w", profileID, err)
	}
	watermark := DashboardSnapshotSourceWatermark{}
	if createdAt.Valid {
		latestUsageEventCreatedAt := createdAt.Time.UTC()
		watermark.LatestUsageEventCreatedAt = &latestUsageEventCreatedAt
	}
	if id.Valid {
		latestUsageEventID := int(id.Int64)
		watermark.LatestUsageEventID = &latestUsageEventID
	}
	return watermark, nil
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

type latencyTrendPointStats struct {
	values []int
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
		cachedTokens += cachedTokensForSnapshotEvent(event)
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
	requestTrends := UsageRequestTrends{
		Hourly: buildRequestTrendSeries(events, startAt, normalizedEndAt, "hour"),
		Daily:  buildRequestTrendSeries(events, startAt, normalizedEndAt, "day"),
	}
	latencyTrends := UsageLatencyTrends{
		Hourly: buildLatencyTrendSeries(events, startAt, normalizedEndAt, "hour"),
		Daily:  buildLatencyTrendSeries(events, startAt, normalizedEndAt, "day"),
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
		RequestTrends:         requestTrends,
		LatencyTrends:         latencyTrends,
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
		endpointLabel := strings.TrimSpace(record.EndpointLabelSnapshot)
		if endpointLabel == "" {
			endpointLabel = "Unknown Endpoint"
		}
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
			APIFamily:                record.APIFamily,
			AttemptCount:             record.AttemptCount,
			BillableFlag:             record.BillableFlag,
			CacheReadInputTokens:     record.CacheReadInputTokens,
			CacheCreationInputTokens: record.CacheCreationInputTokens,
			ConnectionID:             record.ConnectionID,
			CreatedAt:                record.CreatedAt.UTC(),
			EndpointID:               record.EndpointID,
			EndpointLabel:            endpointLabel,
			IngressRequestID:         record.IngressRequestID,
			InputTokens:              record.InputTokens,
			ModelID:                  record.ModelID,
			ModelLabel:               modelLabel,
			OutputTokens:             record.OutputTokens,
			PricedFlag:               record.PricedFlag,
			ProxyAPIKeyID:            record.ProxyAPIKeyID,
			ProxyAPIKeyLabel:         proxyAPIKeyLabel,
			ProxyAPIKeyStatsLabel:    proxyAPIKeyStatsLabel,
			ProxyAPIKeyPrefix:        record.CurrentProxyAPIKeyPrefix,
			ReasoningTokens:          record.ReasoningTokens,
			RequestPath:              record.RequestPath,
			ResolvedTargetModelID:    record.ResolvedTargetModelID,
			StatusCode:               record.StatusCode,
			SuccessFlag:              record.SuccessFlag,
			ResponseTimeMS:           record.ResponseTimeMS,
			TTFTMS:                   record.TTFTMS,
			CompletionDurationMS:     record.CompletionDurationMS,
			HasOutputTokens:          record.HasOutputTokens,
			TotalCostMicros:          totalCostMicros,
			TotalTokens:              record.TotalTokens,
		})
	}
	return events
}

func cachedTokensForSnapshotEvent(event snapshotEvent) int {
	return event.CacheReadInputTokens + event.CacheCreationInputTokens
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

func buildLatencyTrendSeries(events []snapshotEvent, startAt *time.Time, endAt time.Time, granularity string) []UsageLatencyTrendSeries {
	buckets := bucketRange(startAt, endAt, timeSliceFromEvents(events), granularity)
	overall := map[time.Time]*latencyTrendPointStats{}
	modelLabels := map[string]string{}
	byModel := map[string]map[time.Time]*latencyTrendPointStats{}
	for _, event := range events {
		if event.ResponseTimeMS == nil {
			continue
		}
		bucket := bucketFloor(event.CreatedAt, granularity)
		stat := overall[bucket]
		if stat == nil {
			overall[bucket] = &latencyTrendPointStats{}
			stat = overall[bucket]
		}
		stat.values = append(stat.values, *event.ResponseTimeMS)
		modelLabels[event.ModelID] = event.ModelLabel
		modelBucketStats := byModel[event.ModelID]
		if modelBucketStats == nil {
			byModel[event.ModelID] = map[time.Time]*latencyTrendPointStats{}
			modelBucketStats = byModel[event.ModelID]
		}
		bucketStat := modelBucketStats[bucket]
		if bucketStat == nil {
			modelBucketStats[bucket] = &latencyTrendPointStats{}
			bucketStat = modelBucketStats[bucket]
		}
		bucketStat.values = append(bucketStat.values, *event.ResponseTimeMS)
	}
	items := []UsageLatencyTrendSeries{{
		Key:    "all",
		Label:  "All Models",
		Points: makeUsageLatencyTrendPoints(buckets, overall),
	}}
	modelIDs := make([]string, 0, len(byModel))
	for modelID := range byModel {
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
		items = append(items, UsageLatencyTrendSeries{
			Key:    modelID,
			Label:  modelLabels[modelID],
			Points: makeUsageLatencyTrendPoints(buckets, byModel[modelID]),
		})
	}
	return items
}

func makeUsageLatencyTrendPoints(buckets []time.Time, stats map[time.Time]*latencyTrendPointStats) []UsageLatencyTrendPoint {
	points := make([]UsageLatencyTrendPoint, 0, len(buckets))
	for _, bucket := range buckets {
		stat := stats[bucket]
		if stat == nil {
			points = append(points, UsageLatencyTrendPoint{BucketStart: bucket})
			continue
		}
		// ponytail: Go-side percentile over loaded events, same as existing trends - move to SQL percentile_cont only when T5 fixes the load-all-events pattern wholesale.
		points = append(points, UsageLatencyTrendPoint{
			BucketStart: bucket,
			P50MS:       percentileContInt(stat.values, 0.5),
			P95MS:       percentileContInt(stat.values, 0.95),
		})
	}
	return points
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
		stat.cachedTokens += cachedTokensForSnapshotEvent(event)
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
		bucketStat.cachedTokens += cachedTokensForSnapshotEvent(event)
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
		item.cachedTokens += cachedTokensForSnapshotEvent(event)
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
		if items[i].EndpointLabel != items[j].EndpointLabel {
			return items[i].EndpointLabel < items[j].EndpointLabel
		}
		return endpointIDOrMinusOne(items[i].EndpointID) < endpointIDOrMinusOne(items[j].EndpointID)
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
		group.cachedTokens += cachedTokensForSnapshotEvent(event)
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

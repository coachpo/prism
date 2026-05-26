package contract_test

import (
	"context"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

var fixedS15Now = time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)

func s15AuditWindowQuery() string {
	values := url.Values{}
	values.Set("from", fixedS15Now.Add(-24*time.Hour).Format(time.RFC3339))
	values.Set("to", fixedS15Now.Add(time.Minute).Format(time.RFC3339))
	return values.Encode()
}

func assertErrorCode(t *testing.T, payload map[string]any, want string) {
	t.Helper()
	errorPayload := asMap(t, payload["error"])
	if errorPayload["code"] != want {
		t.Fatalf("expected error code %q, got %+v", want, payload)
	}
}

func assertDashboardSnapshotTopLevelShape(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"generated_at", "coverage_24h", "coverage_30d", "health", "metric_snapshot", "api_family_rows", "strategy_family_summary", "recent_requests", "top_spending_models", "routing_health_map"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected dashboard snapshot field %q, got %+v", key, payload)
		}
	}
	for _, legacyKey := range []string{"window", "covers", "freshness", "metrics"} {
		if _, ok := payload[legacyKey]; ok {
			t.Fatalf("dashboard snapshot must not expose legacy top-level %q, got %+v", legacyKey, payload)
		}
	}
}

func assertS15UsageSnapshotTokenTotals(t *testing.T, payload map[string]any, wantInput int, wantOutput int, wantTotal int, wantCached int, wantReasoning int) {
	t.Helper()
	overview := asMap(t, payload["overview"])
	if jsonInt(t, overview["input_tokens"]) != wantInput || jsonInt(t, overview["output_tokens"]) != wantOutput || jsonInt(t, overview["total_tokens"]) != wantTotal || jsonInt(t, overview["cached_tokens"]) != wantCached || jsonInt(t, overview["reasoning_tokens"]) != wantReasoning {
		t.Fatalf("expected overview input/output/total/cached/reasoning=%d/%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantTotal, wantCached, wantReasoning, overview)
	}
	trendTotals := s15AllModelHourlyTokenTrendTotals(t, payload)
	if trendTotals.inputTokens != wantInput || trendTotals.outputTokens != wantOutput || trendTotals.totalTokens != wantTotal || trendTotals.cachedTokens != wantCached || trendTotals.reasoningTokens != wantReasoning {
		t.Fatalf("expected token trend input/output/total/cached/reasoning=%d/%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantTotal, wantCached, wantReasoning, trendTotals)
	}
	breakdownTotals := s15HourlyTokenBreakdownTotals(t, payload)
	if breakdownTotals.inputTokens != wantInput || breakdownTotals.outputTokens != wantOutput || breakdownTotals.cachedTokens != wantCached || breakdownTotals.reasoningTokens != wantReasoning {
		t.Fatalf("expected token breakdown input/output/cached/reasoning=%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantCached, wantReasoning, breakdownTotals)
	}
	modelTotals := s15UsageModelTokenTotals(t, payload)
	if modelTotals.inputTokens != wantInput || modelTotals.outputTokens != wantOutput || modelTotals.totalTokens != wantTotal || modelTotals.cachedTokens != wantCached || modelTotals.reasoningTokens != wantReasoning {
		t.Fatalf("expected model stats input/output/total/cached/reasoning=%d/%d/%d/%d/%d, got %+v", wantInput, wantOutput, wantTotal, wantCached, wantReasoning, modelTotals)
	}
}

type s15UsageTokenTotals struct {
	inputTokens     int
	outputTokens    int
	totalTokens     int
	cachedTokens    int
	reasoningTokens int
}

func s15AllModelHourlyTokenTrendTotals(t *testing.T, payload map[string]any) s15UsageTokenTotals {
	t.Helper()
	trends := asMap(t, payload["token_usage_trends"])
	for _, rawSeries := range trends["hourly"].([]any) {
		series := asMap(t, rawSeries)
		if series["key"] != "all" {
			continue
		}
		totals := s15UsageTokenTotals{}
		for _, rawPoint := range series["points"].([]any) {
			point := asMap(t, rawPoint)
			totals.inputTokens += jsonInt(t, point["input_tokens"])
			totals.outputTokens += jsonInt(t, point["output_tokens"])
			totals.totalTokens += jsonInt(t, point["total_tokens"])
			totals.cachedTokens += jsonInt(t, point["cached_tokens"])
			totals.reasoningTokens += jsonInt(t, point["reasoning_tokens"])
		}
		return totals
	}
	return s15UsageTokenTotals{}
}

func s15HourlyTokenBreakdownTotals(t *testing.T, payload map[string]any) s15UsageTokenTotals {
	t.Helper()
	breakdown := asMap(t, payload["token_type_breakdown"])
	totals := s15UsageTokenTotals{}
	for _, rawPoint := range breakdown["hourly"].([]any) {
		point := asMap(t, rawPoint)
		totals.inputTokens += jsonInt(t, point["input_tokens"])
		totals.outputTokens += jsonInt(t, point["output_tokens"])
		totals.cachedTokens += jsonInt(t, point["cached_tokens"])
		totals.reasoningTokens += jsonInt(t, point["reasoning_tokens"])
	}
	return totals
}

func s15UsageModelTokenTotals(t *testing.T, payload map[string]any) s15UsageTokenTotals {
	t.Helper()
	totals := s15UsageTokenTotals{}
	for _, rawModel := range payload["model_statistics"].([]any) {
		model := asMap(t, rawModel)
		totals.inputTokens += jsonInt(t, model["input_tokens"])
		totals.outputTokens += jsonInt(t, model["output_tokens"])
		totals.totalTokens += jsonInt(t, model["total_tokens"])
		totals.cachedTokens += jsonInt(t, model["cached_tokens"])
		totals.reasoningTokens += jsonInt(t, model["reasoning_tokens"])
	}
	return totals
}

func TestUsageSnapshot(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Snapshot Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "snapshot-model", stringPtr("Snapshot Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Snapshot Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	proxyKeyID := insertContractProxyAPIKey(t, harness, "Snapshot Key")
	insertUsageEvent(t, harness, usageEventSeed{ID: 1, ProfileID: profileID, IngressRequestID: "snap-1", ModelID: "snapshot-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &connectionID, ProxyAPIKeyID: &proxyKeyID, ProxyAPIKeyNameSnapshot: stringPtr("Snapshot Key"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(38), CacheReadInputTokens: intPtr(5), CacheCreationInputTokens: intPtr(2), ReasoningTokens: intPtr(1), TotalCostUserCurrencyMicros: int64Ptr(2500), AttemptCount: 1, RequestPath: "/v1/chat/completions", ResponseTimeMS: intPtr(800), TTFTMS: intPtr(100), CompletionDurationMS: intPtr(1100), CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 2, ProfileID: profileID, IngressRequestID: "snap-2", ModelID: "snapshot-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &connectionID, StatusCode: 500, SuccessFlag: false, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), InputTokens: intPtr(15), OutputTokens: intPtr(25), TotalTokens: intPtr(40), AttemptCount: 1, RequestPath: "/v1/chat/completions", ResponseTimeMS: intPtr(900), CreatedAt: fixedS15Now.Add(-5 * time.Minute)})

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/usage-snapshot?preset=1h", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	overview := asMap(t, payload["overview"])
	if jsonInt(t, overview["total_requests"]) != 2 || jsonInt(t, overview["success_requests"]) != 1 || jsonInt(t, overview["failed_requests"]) != 1 || jsonInt(t, overview["total_tokens"]) != 78 {
		t.Fatalf("expected usage snapshot overview totals, got %+v", overview)
	}
	assertS15UsageSnapshotTokenTotals(t, payload, 25, 45, 78, 7, 1)
	if asMap(t, payload["currency"])["code"] != "USD" || asMap(t, payload["time_range"])["preset"] != "1h" {
		t.Fatalf("expected usage snapshot currency/time range payload, got %+v", payload)
	}
	endpointStats := payload["endpoint_statistics"].([]any)
	if len(endpointStats) != 1 {
		t.Fatalf("expected one endpoint statistic row, got %+v", payload)
	}
	endpointRow := asMap(t, endpointStats[0])
	if endpointRow["endpoint_label"] != "Snapshot Endpoint" || jsonInt(t, endpointRow["p50_ttft_ms"]) != 100 || math.Abs(endpointRow["avg_output_rate_tps"].(float64)-20.0) > 0.001 {
		t.Fatalf("expected snapshot endpoint statistics to preserve TTFT/output-rate aggregates, got %+v", endpointRow)
	}
	modelStats := payload["model_statistics"].([]any)
	if len(modelStats) != 1 {
		t.Fatalf("expected one model statistic row, got %+v", payload)
	}
	modelRow := asMap(t, modelStats[0])
	if modelRow["model_label"] != "Snapshot Model" || jsonInt(t, modelRow["priced_request_count"]) != 1 || jsonInt(t, modelRow["unpriced_request_count"]) != 0 || jsonInt(t, modelRow["p50_ttft_ms"]) != 100 || math.Abs(modelRow["avg_output_rate_tps"].(float64)-20.0) > 0.001 {
		t.Fatalf("expected snapshot model statistics to preserve priced counts and TTFT/output-rate aggregates, got %+v", modelRow)
	}
	proxyStats := payload["proxy_api_key_statistics"].([]any)
	foundSnapshotKey := false
	for _, raw := range proxyStats {
		if asMap(t, raw)["proxy_api_key_label"] == "Snapshot Key" {
			foundSnapshotKey = true
			break
		}
	}
	if !foundSnapshotKey {
		t.Fatalf("expected proxy-api-key statistics to include snapshot label, got %+v", payload)
	}
	costOverview := asMap(t, payload["cost_overview"])
	if jsonInt(t, costOverview["priced_request_count"]) != 1 || jsonInt(t, costOverview["unpriced_request_count"]) != 0 {
		t.Fatalf("expected cost overview priced/unpriced counts, got %+v", costOverview)
	}
}

func TestObservabilityUsageEventSeedPersistsMergedPersistenceSemantics(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertUsageEvent(t, harness, usageEventSeed{
		ID:                                3,
		ProfileID:                         profileID,
		IngressRequestID:                  "merged-persistence-usage",
		ModelID:                           "merged-persistence-model",
		APIFamily:                         "openai",
		StatusCode:                        200,
		SuccessFlag:                       true,
		BillableFlag:                      boolPtr(true),
		PricedFlag:                        boolPtr(true),
		InputTokens:                       intPtr(6),
		OutputTokens:                      intPtr(3),
		TotalTokens:                       intPtr(42),
		CacheReadInputTokens:              intPtr(4),
		CacheCreationInputTokens:          intPtr(2),
		ReasoningTokens:                   intPtr(1),
		InputCostMicros:                   int64Ptr(12),
		OutputCostMicros:                  int64Ptr(15),
		CacheReadInputCostMicros:          int64Ptr(44),
		CacheCreationInputCostMicros:      int64Ptr(26),
		ReasoningCostMicros:               int64Ptr(17),
		TotalCostOriginalMicros:           int64Ptr(114),
		TotalCostUserCurrencyMicros:       int64Ptr(114),
		CurrencyCodeOriginal:              stringPtr("USD"),
		ReportCurrencyCode:                stringPtr("USD"),
		ReportCurrencySymbol:              stringPtr("$"),
		FXRateUsed:                        stringPtr("1"),
		FXRateSource:                      stringPtr("DEFAULT_1_TO_1"),
		PricingSnapshotUnit:               stringPtr("PER_1M"),
		PricingSnapshotInput:              stringPtr("2"),
		PricingSnapshotOutput:             stringPtr("5"),
		PricingSnapshotCacheReadInput:     stringPtr("11"),
		PricingSnapshotCacheCreationInput: stringPtr("13"),
		PricingSnapshotReasoning:          stringPtr("17"),
		PricingConfigVersionUsed:          intPtr(7),
		AttemptCount:                      1,
		RequestPath:                       "/v1/chat/completions",
		ResponseTimeMS:                    intPtr(800),
		TTFTMS:                            intPtr(100),
		CompletionDurationMS:              intPtr(1100),
		CreatedAt:                         fixedS15Now.Add(-8 * time.Minute),
	})

	var inputTokens, outputTokens, totalTokens, cacheReadInputTokens, cacheCreationInputTokens, reasoningTokens int
	var inputCostMicros, outputCostMicros, cacheReadInputCostMicros, cacheCreationInputCostMicros, reasoningCostMicros, totalCostOriginalMicros, totalCostUserCurrencyMicros int64
	var pricingSnapshotUnit, pricingSnapshotInput, pricingSnapshotOutput, pricingSnapshotCacheReadInput, pricingSnapshotCacheCreationInput, pricingSnapshotReasoning string
	var pricingConfigVersionUsed int
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used FROM usage_request_events WHERE profile_id = $1 AND id = 3`,
		profileID,
	).Scan(&inputTokens, &outputTokens, &totalTokens, &cacheReadInputTokens, &cacheCreationInputTokens, &reasoningTokens, &inputCostMicros, &outputCostMicros, &cacheReadInputCostMicros, &cacheCreationInputCostMicros, &reasoningCostMicros, &totalCostOriginalMicros, &totalCostUserCurrencyMicros, &pricingSnapshotUnit, &pricingSnapshotInput, &pricingSnapshotOutput, &pricingSnapshotCacheReadInput, &pricingSnapshotCacheCreationInput, &pricingSnapshotReasoning, &pricingConfigVersionUsed); err != nil {
		t.Fatalf("load merged usage-event persistence row: %v", err)
	}
	if inputTokens != 6 || outputTokens != 3 || totalTokens != 42 || cacheReadInputTokens != 4 || cacheCreationInputTokens != 2 || reasoningTokens != 1 {
		t.Fatalf("expected canonical token components 6/3/42/4/2/1, got input=%d output=%d total=%d cache_read=%d cache_creation=%d reasoning=%d", inputTokens, outputTokens, totalTokens, cacheReadInputTokens, cacheCreationInputTokens, reasoningTokens)
	}
	if inputCostMicros != 12 || outputCostMicros != 15 || cacheReadInputCostMicros != 44 || cacheCreationInputCostMicros != 26 || reasoningCostMicros != 17 || totalCostOriginalMicros != 114 || totalCostUserCurrencyMicros != 114 {
		t.Fatalf("expected component costs and totals to persist, got input=%d output=%d cache_read=%d cache_creation=%d reasoning=%d total_original=%d total_user=%d", inputCostMicros, outputCostMicros, cacheReadInputCostMicros, cacheCreationInputCostMicros, reasoningCostMicros, totalCostOriginalMicros, totalCostUserCurrencyMicros)
	}
	if pricingSnapshotUnit != "PER_1M" || pricingSnapshotInput != "2" || pricingSnapshotOutput != "5" || pricingSnapshotCacheReadInput != "11" || pricingSnapshotCacheCreationInput != "13" || pricingSnapshotReasoning != "17" || pricingConfigVersionUsed != 7 {
		t.Fatalf("expected concrete pricing snapshot values, got unit=%q input=%q output=%q cache_read=%q cache_creation=%q reasoning=%q version=%d", pricingSnapshotUnit, pricingSnapshotInput, pricingSnapshotOutput, pricingSnapshotCacheReadInput, pricingSnapshotCacheCreationInput, pricingSnapshotReasoning, pricingConfigVersionUsed)
	}
}

func TestEndpointModelStatistics(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Endpoint Model Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "endpoint-model", stringPtr("Endpoint Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Endpoint Model Stats", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertUsageEvent(t, harness, usageEventSeed{ID: 10, ProfileID: profileID, IngressRequestID: "endpoint-1", ModelID: "endpoint-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &connectionID, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), OutputTokens: intPtr(20), TotalTokens: intPtr(20), TTFTMS: intPtr(100), CompletionDurationMS: intPtr(1000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-30 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 11, ProfileID: profileID, IngressRequestID: "endpoint-2", ModelID: "endpoint-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &connectionID, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(false), UnpricedReason: stringPtr("PRICING_DISABLED"), OutputTokens: intPtr(30), TotalTokens: intPtr(30), TTFTMS: intPtr(400), CompletionDurationMS: intPtr(1500), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-20 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 12, ProfileID: profileID, IngressRequestID: "endpoint-3", ModelID: "endpoint-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &connectionID, StatusCode: 500, SuccessFlag: false, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), TotalTokens: intPtr(0), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-10 * time.Minute)})

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/stats/endpoints/%d/models?preset=all", endpointID), nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload []map[string]any
	decodeJSONResponse(t, response, &payload)
	if len(payload) != 1 {
		t.Fatalf("expected one endpoint-model statistics row, got %+v", payload)
	}
	row := payload[0]
	if row["model_id"] != "endpoint-model" || row["model_label"] != "Endpoint Model" || jsonInt(t, row["request_count"]) != 3 || jsonInt(t, row["success_count"]) != 2 || jsonInt(t, row["failed_count"]) != 1 || jsonInt(t, row["priced_request_count"]) != 0 || jsonInt(t, row["unpriced_request_count"]) != 2 {
		t.Fatalf("unexpected endpoint-model statistics payload: %+v", row)
	}
	if jsonInt(t, row["p50_ttft_ms"]) != 250 || jsonInt(t, row["p95_ttft_ms"]) != 385 || math.Abs(row["avg_output_rate_tps"].(float64)-24.74) > 0.001 {
		t.Fatalf("expected TTFT percentiles and average output rate from seeded rows, got %+v", row)
	}
}

func TestStatsSummary(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 100, profileID, "summary-model-a", "openai", 12, 41, 200, 100, 10, 20, 30, fixedS15Now.Add(-55*time.Minute))
	insertRequestLogSummaryRow(t, harness, 101, profileID, "summary-model-a", "openai", 12, 41, 500, 300, 5, 10, 15, fixedS15Now.Add(-50*time.Minute))
	insertRequestLogSummaryRow(t, harness, 102, profileID, "summary-model-b", "anthropic", 13, 42, 200, 200, 8, 12, 20, fixedS15Now.Add(-45*time.Minute))

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/summary?group_by=api_family", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if jsonInt(t, payload["total_requests"]) != 3 || jsonInt(t, payload["success_count"]) != 2 || jsonInt(t, payload["error_count"]) != 1 {
		t.Fatalf("unexpected stats summary totals: %+v", payload)
	}
	groups := payload["groups"].([]any)
	if len(groups) != 2 || asMap(t, groups[0])["key"] != "openai" || asMap(t, groups[1])["key"] != "anthropic" {
		t.Fatalf("expected api-family groups in response, got %+v", payload)
	}
}

func TestManagementDashboardStatsReturnsCanonicalSnapshotWithoutWindow(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	for _, path := range []string{"/api/stats/dashboard", "/api/stats/dashboard?window=24h", "/api/stats/dashboard?window=all"} {
		response := harness.requestJSON(t, harness.client, http.MethodGet, path, nil, modelHeader(profileID))
		assertStatus(t, response, http.StatusOK)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		assertDashboardSnapshotTopLevelShape(t, payload)
	}
}

func TestManagementDashboardStatsSnapshotSections(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Dashboard Snapshot Strategy")
	modelInsertModel(t, harness, profileID, nil, "openai", "dashboard-model", stringPtr("Dashboard Model"), "native", &strategyID, true)
	insertRequestLogSummaryRow(t, harness, 100, profileID, "dashboard-model", "openai", 12, 41, 200, 100, 10, 20, 30, fixedS15Now.Add(-55*time.Minute))
	insertRequestLogSummaryRow(t, harness, 101, profileID, "dashboard-model", "openai", 12, 41, 500, 300, 5, 10, 15, fixedS15Now.Add(-50*time.Minute))
	insertUsageEvent(t, harness, usageEventSeed{ID: 30, ProfileID: profileID, IngressRequestID: "dashboard-spend-1", ModelID: "dashboard-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalCostUserCurrencyMicros: int64Ptr(2500), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/dashboard?window=24h", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	assertDashboardSnapshotTopLevelShape(t, payload)
	coverage24H := asMap(t, payload["coverage_24h"])
	coverage30D := asMap(t, payload["coverage_30d"])
	if coverage24H["from"] == nil || coverage24H["to"] == nil || coverage30D["from"] == nil || coverage30D["to"] == nil {
		t.Fatalf("expected dashboard coverage sections, got %+v", payload)
	}
	health := asMap(t, payload["health"])
	if jsonInt(t, health["lag_seconds"]) != 0 || health["stale"] != false || jsonInt(t, health["stale_after_seconds"]) != 120 {
		t.Fatalf("expected fresh dashboard health section, got %+v", health)
	}
	metricSnapshot := asMap(t, payload["metric_snapshot"])
	if jsonInt(t, metricSnapshot["total_requests"]) != 2 || jsonInt(t, metricSnapshot["total_cost"]) != 2500 || jsonInt(t, metricSnapshot["priced_request_count"]) != 1 {
		t.Fatalf("expected aggregate metric snapshot, got %+v", metricSnapshot)
	}
	if math.Abs(metricSnapshot["success_rate"].(float64)-50) > 0.001 || math.Abs(metricSnapshot["error_rate"].(float64)-50) > 0.001 {
		t.Fatalf("expected success/error rates from 24h summary, got %+v", metricSnapshot)
	}
	apiFamilyRows := payload["api_family_rows"].([]any)
	if len(apiFamilyRows) != 1 || asMap(t, apiFamilyRows[0])["key"] != "openai" || jsonInt(t, asMap(t, apiFamilyRows[0])["total_requests"]) != 2 {
		t.Fatalf("expected API-family rows from 24h summary, got %+v", payload["api_family_rows"])
	}
	topSpendingModels := payload["top_spending_models"].([]any)
	if len(topSpendingModels) != 1 {
		t.Fatalf("expected one top spending model row, got %+v", payload["top_spending_models"])
	}
	topSpendingModel := asMap(t, topSpendingModels[0])
	if topSpendingModel["model_id"] != "dashboard-model" || topSpendingModel["model_label"] != "Dashboard Model" || jsonInt(t, topSpendingModel["total_cost_micros"]) != 2500 {
		t.Fatalf("expected top spending models from 30d spending with canonical label, got %+v", payload["top_spending_models"])
	}
	routingHealthMap := asMap(t, payload["routing_health_map"])
	if len(routingHealthMap["nodes"].([]any)) != 0 || len(routingHealthMap["links"].([]any)) != 0 || jsonInt(t, routingHealthMap["endpointCount"]) != 0 || jsonInt(t, routingHealthMap["modelCount"]) != 0 {
		t.Fatalf("expected task-1 empty routing health map shell, got %+v", routingHealthMap)
	}
}

func TestManagementMetricsExposed(t *testing.T) {
	managementBranchSource := s15ReadBackendSource(t, "internal/platform/http/management_branch.go")
	dbSource := s15ReadBackendSource(t, "internal/platform/db/pools.go")
	if !strings.Contains(managementBranchSource, `router.Get("/metrics", platformdb.MetricsHandler(deps.DatabasePools))`) || !strings.Contains(managementBranchSource, "deps.DatabasePools != nil") {
		t.Fatalf("expected server to expose /metrics when database pools are configured")
	}
	for _, metric := range []string{"prism_db_pool_acquired_connections", "prism_db_pool_max_connections", "prism_db_pool_acquire_timeout_count"} {
		if !strings.Contains(dbSource, metric) {
			t.Fatalf("expected DB metrics handler to expose %s", metric)
		}
	}
}

func TestManagementGlobalLogRetentionJobStatusContract(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	jobID := createS15LogRetentionJob(t, harness, "request_logs", map[string]any{"delete_all": true}, "status")

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/management/jobs/"+jobID, nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	scope := asMap(t, payload["scope"])
	if payload["id"] != jobID || payload["type"] != "log_retention" || payload["progress"] == nil || scope["table"] != "request_logs" || scope["delete_all"] != true {
		t.Fatalf("expected global log-retention job status contract, got %+v", payload)
	}
}

func TestManagementDashboardHealthReportsSnapshotFreshness(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/dashboard", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	assertDashboardSnapshotTopLevelShape(t, payload)
	health := asMap(t, payload["health"])
	if jsonInt(t, health["lag_seconds"]) != 0 || health["stale"] != false || jsonInt(t, health["stale_after_seconds"]) != 120 {
		t.Fatalf("expected dashboard health to describe the aggregate snapshot freshness, got %+v", payload)
	}
}

func TestModelMetrics(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 200, profileID, "metrics-model", "openai", 12, 51, 200, 100, 0, 0, 0, fixedS15Now.Add(-2*time.Hour))
	insertRequestLogSummaryRow(t, harness, 201, profileID, "metrics-model", "openai", 12, 51, 500, 300, 0, 0, 0, fixedS15Now.Add(-90*time.Minute))
	insertUsageEvent(t, harness, usageEventSeed{ID: 20, ProfileID: profileID, IngressRequestID: "metrics-1", ModelID: "metrics-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalCostUserCurrencyMicros: int64Ptr(2500), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-24 * time.Hour)})

	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/stats/models/metrics", map[string]any{"model_ids": []string{"metrics-model"}, "summary_window_hours": 24, "spending_preset": "last_30_days"}, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one model metrics item, got %+v", payload)
	}
	item := asMap(t, items[0])
	if item["model_id"] != "metrics-model" || jsonInt(t, item["request_count_24h"]) != 2 || jsonInt(t, item["p95_latency_ms"]) != 290 || jsonInt(t, item["spend_30d_micros"]) != 2500 {
		t.Fatalf("unexpected model metrics payload: %+v", item)
	}
}

func TestConnectionSuccessRates(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 300, profileID, "connection-model", "openai", 12, 61, 200, 100, 0, 0, 0, fixedS15Now.Add(-40*time.Minute))
	insertRequestLogSummaryRow(t, harness, 301, profileID, "connection-model", "openai", 12, 61, 500, 100, 0, 0, 0, fixedS15Now.Add(-35*time.Minute))
	insertRequestLogSummaryRow(t, harness, 302, profileID, "connection-model", "openai", 12, 62, 200, 100, 0, 0, 0, fixedS15Now.Add(-30*time.Minute))

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/connection-success-rates", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload []map[string]any
	decodeJSONResponse(t, response, &payload)
	if len(payload) != 2 || jsonInt(t, payload[0]["connection_id"]) != 61 || jsonInt(t, payload[0]["total_requests"]) != 2 || jsonInt(t, payload[0]["success_count"]) != 1 {
		t.Fatalf("unexpected connection success rates payload: %+v", payload)
	}
}

func TestThroughput(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	fromTime := fixedS15Now.Add(-2 * time.Minute)
	toTime := fixedS15Now
	insertRequestLogSummaryRow(t, harness, 400, profileID, "throughput-model", "openai", 12, 71, 200, 100, 0, 0, 0, fixedS15Now.Add(-2*time.Minute))
	insertRequestLogSummaryRow(t, harness, 401, profileID, "throughput-model", "openai", 12, 71, 200, 100, 0, 0, 0, fixedS15Now.Add(-90*time.Second))
	insertRequestLogSummaryRow(t, harness, 402, profileID, "throughput-model", "openai", 12, 71, 200, 100, 0, 0, 0, fixedS15Now.Add(-30*time.Second))

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/stats/throughput?from_time=%s&to_time=%s", fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339)), nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if jsonInt(t, payload["total_requests"]) != 3 || len(payload["buckets"].([]any)) != 2 {
		t.Fatalf("unexpected throughput payload: %+v", payload)
	}
	if payload["average_rpm"].(float64) <= 0 || payload["peak_rpm"].(float64) <= 0 || payload["current_rpm"].(float64) <= 0 {
		t.Fatalf("expected positive throughput metrics, got %+v", payload)
	}
}

func TestSpending(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Spend Strategy")
	modelInsertModel(t, harness, profileID, nil, "openai", "spend-model", stringPtr("Spend Model"), "native", &strategyID, true)
	endpointA := modelInsertEndpoint(t, harness, profileID, "Spend Endpoint A", 0)
	endpointB := modelInsertEndpoint(t, harness, profileID, "Spend Endpoint B", 1)
	insertUsageEvent(t, harness, usageEventSeed{ID: 30, ProfileID: profileID, IngressRequestID: "spend-1", ModelID: "spend-model", APIFamily: "openai", EndpointID: &endpointA, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(38), CacheReadInputTokens: intPtr(5), CacheCreationInputTokens: intPtr(2), ReasoningTokens: intPtr(1), TotalCostUserCurrencyMicros: int64Ptr(5000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-4 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 31, ProfileID: profileID, IngressRequestID: "spend-2", ModelID: "spend-model", APIFamily: "openai", EndpointID: &endpointB, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(false), UnpricedReason: stringPtr("PRICING_DISABLED"), InputTokens: intPtr(3), OutputTokens: intPtr(4), TotalTokens: intPtr(12), CacheReadInputTokens: intPtr(2), CacheCreationInputTokens: intPtr(1), ReasoningTokens: intPtr(2), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-3 * time.Hour)})

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/spending?preset=all&group_by=model_endpoint&limit=50&offset=0", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	summary := asMap(t, payload["summary"])
	if jsonInt(t, summary["successful_request_count"]) != 2 || jsonInt(t, summary["priced_request_count"]) != 1 || jsonInt(t, summary["unpriced_request_count"]) != 1 || jsonInt(t, summary["total_cost_micros"]) != 5000 || jsonInt(t, payload["groups_total"]) != 2 {
		t.Fatalf("unexpected spending summary payload: %+v", payload)
	}
	if jsonInt(t, summary["total_input_tokens"]) != 13 || jsonInt(t, summary["total_output_tokens"]) != 24 || jsonInt(t, summary["total_cache_read_input_tokens"]) != 7 || jsonInt(t, summary["total_cache_creation_input_tokens"]) != 3 || jsonInt(t, summary["total_reasoning_tokens"]) != 3 || jsonInt(t, summary["total_tokens"]) != 50 {
		t.Fatalf("expected spending summary to preserve base and split token totals, got %+v", summary)
	}
	groups := payload["groups"].([]any)
	if len(groups) != 2 || !strings.Contains(asMap(t, groups[0])["key"].(string), "spend-model#") {
		t.Fatalf("expected grouped spending payload, got %+v", payload)
	}
	groupsByKey := make(map[string]map[string]any, len(groups))
	for _, raw := range groups {
		group := asMap(t, raw)
		groupsByKey[group["key"].(string)] = group
	}
	unpricedGroup := groupsByKey[fmt.Sprintf("spend-model#%d", endpointB)]
	if unpricedGroup == nil || jsonInt(t, unpricedGroup["total_cost_micros"]) != 0 || jsonInt(t, unpricedGroup["priced_requests"]) != 0 || jsonInt(t, unpricedGroup["unpriced_requests"]) != 1 {
		t.Fatalf("expected unpriced spend group to stay zero-cost while preserving request counts, got %+v", groupsByKey)
	}
	topSpendingModels := payload["top_spending_models"].([]any)
	if len(topSpendingModels) != 1 {
		t.Fatalf("expected one top spending model row, got %+v", payload["top_spending_models"])
	}
	topSpendingModel := asMap(t, topSpendingModels[0])
	if topSpendingModel["model_id"] != "spend-model" || topSpendingModel["model_label"] != "Spend Model" || jsonInt(t, topSpendingModel["total_cost_micros"]) != 5000 {
		t.Fatalf("expected top spending models to preserve canonical labels, got %+v", payload["top_spending_models"])
	}
	topEndpoints := payload["top_spending_endpoints"].([]any)
	if len(topEndpoints) == 0 || asMap(t, topEndpoints[0])["endpoint_label"] != "Spend Endpoint A" {
		t.Fatalf("expected top spending endpoints to use current endpoint labels, got %+v", payload)
	}
	if jsonInt(t, asMap(t, payload["unpriced_breakdown"])["PRICING_DISABLED"]) != 1 {
		t.Fatalf("expected canonical unpriced breakdown to count PRICING_DISABLED, got %+v", payload)
	}
}

func TestObservabilityTreatsSuccessfulMissingCostRowsAsUnpriced(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertUsageEvent(t, harness, usageEventSeed{ID: 35, ProfileID: profileID, IngressRequestID: "missing-cost-1", ModelID: "missing-cost-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(25), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-2 * time.Hour)})

	usageSnapshotResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/usage-snapshot?preset=all", nil, modelHeader(profileID))
	assertStatus(t, usageSnapshotResponse, http.StatusOK)
	var usageSnapshotPayload map[string]any
	decodeJSONResponse(t, usageSnapshotResponse, &usageSnapshotPayload)
	costOverview := asMap(t, usageSnapshotPayload["cost_overview"])
	if jsonInt(t, costOverview["priced_request_count"]) != 0 || jsonInt(t, costOverview["unpriced_request_count"]) != 1 || jsonInt(t, costOverview["total_cost_micros"]) != 0 {
		t.Fatalf("expected missing-cost usage snapshot to stay unpriced with zero cost, got %+v", usageSnapshotPayload)
	}
	modelStatistics := usageSnapshotPayload["model_statistics"].([]any)
	if len(modelStatistics) != 1 {
		t.Fatalf("expected one missing-cost model statistic row, got %+v", usageSnapshotPayload)
	}
	modelRow := asMap(t, modelStatistics[0])
	if jsonInt(t, modelRow["priced_request_count"]) != 0 || jsonInt(t, modelRow["unpriced_request_count"]) != 1 || jsonInt(t, modelRow["total_cost_micros"]) != 0 {
		t.Fatalf("expected missing-cost model statistics to count one unpriced request without inventing spend, got %+v", modelRow)
	}

	spendingResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/spending?preset=all&group_by=none&limit=50&offset=0", nil, modelHeader(profileID))
	assertStatus(t, spendingResponse, http.StatusOK)
	var spendingPayload map[string]any
	decodeJSONResponse(t, spendingResponse, &spendingPayload)
	summary := asMap(t, spendingPayload["summary"])
	if jsonInt(t, summary["successful_request_count"]) != 1 || jsonInt(t, summary["priced_request_count"]) != 0 || jsonInt(t, summary["unpriced_request_count"]) != 1 || jsonInt(t, summary["total_cost_micros"]) != 0 {
		t.Fatalf("expected missing-cost spending summary to stay unpriced with zero cost, got %+v", summary)
	}
	if jsonInt(t, asMap(t, spendingPayload["unpriced_breakdown"])["MISSING_PRICE_DATA"]) != 1 {
		t.Fatalf("expected missing-cost spending breakdown to count MISSING_PRICE_DATA, got %+v", spendingPayload)
	}
}

func TestStatsRetentionJobs(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 500, profileID, "retention-model", "openai", 12, 81, 200, 100, 0, 0, 0, fixedS15Now.Add(-48*time.Hour))
	insertRequestLogSummaryRow(t, harness, 501, profileID, "retention-model", "openai", 12, 81, 200, 100, 0, 0, 0, fixedS15Now.Add(-30*time.Minute))
	insertUsageEvent(t, harness, usageEventSeed{ID: 40, ProfileID: profileID, IngressRequestID: "stats-retention-old", ModelID: "retention-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 41, ProfileID: profileID, IngressRequestID: "stats-retention-new", ModelID: "retention-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	cutoff := fixedS15Now.Add(-24 * time.Hour).Format(time.RFC3339)
	requestLogJob := createS15LogRetentionJob(t, harness, "request_logs", map[string]any{"cutoff": cutoff}, "request-logs")
	usageJob := createS15LogRetentionJob(t, harness, "usage_request_events", map[string]any{"cutoff": cutoff}, "usage-events")
	if requestLogJob == usageJob || s15CountRows(t, harness, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1`, profileID) != 2 || s15CountRows(t, harness, `SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected global retention jobs to enqueue without inline stats deletion")
	}
}

func TestRequestLogsPartitionProfileScopedDuplicateID(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := s15InsertProfile(t, harness, "S15 Other Requests")
	requestID := 9100
	insertRequestLogSummaryRow(t, harness, requestID, profileID, "partition-old", "openai", 12, 91, 200, 111, 1, 2, 3, fixedS15Now.Add(-90*time.Minute))
	insertRequestLogSummaryRow(t, harness, requestID, profileID, "partition-new", "openai", 12, 91, 500, 222, 4, 5, 9, fixedS15Now.Add(-5*time.Minute))
	insertRequestLogSummaryRow(t, harness, requestID, otherProfileID, "partition-other", "openai", 12, 91, 200, 333, 7, 8, 15, fixedS15Now.Add(-1*time.Minute))

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/requests?limit=20", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	if jsonInt(t, listPayload["total"]) != 2 {
		t.Fatalf("expected profile-scoped request list over partitions, got %+v", listPayload)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/stats/requests/%d", requestID), nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	summary := asMap(t, detailPayload["summary"])
	if summary["model_id"] != "partition-new" || jsonInt(t, summary["status_code"]) != 500 || jsonInt(t, summary["response_time_ms"]) != 222 {
		t.Fatalf("expected newest duplicate request-log id for selected profile, got %+v", detailPayload)
	}
}

func TestAuditDetailMissingRequestLogWeakReference(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	requestCreatedAt := fixedS15Now.AddDate(0, 0, -2).Add(2 * time.Hour)
	auditCreatedAt := fixedS15Now.Add(-5 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 9200, profileID, "audit-missing-request", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	insertAuditLog(t, harness, auditLogSeed{ID: 9201, ProfileID: profileID, RequestLogID: intPtr(9200), RequestLogCreatedAt: timePtr(requestCreatedAt), IngressRequestID: stringPtr("weak-request-9200"), ModelID: "audit-missing-request", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: false, CreatedAt: auditCreatedAt})
	dropS15RequestLogPartition(t, harness, requestCreatedAt)

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs/9201", nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	if jsonInt(t, detailPayload["request_log_id"]) != 9200 || detailPayload["ingress_request_id"] != "weak-request-9200" || detailPayload["request_log_created_at"] == nil || detailPayload["request_log_missing"] != true {
		t.Fatalf("expected audit detail weak request link with missing state, got %+v", detailPayload)
	}
}

func TestAuditPartitionProfileScopedWeakRequestLinkList(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := s15InsertProfile(t, harness, "S15 Other Audit")
	requestCreatedAt := fixedS15Now.Add(-20 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 9300, profileID, "audit-partition", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	insertAuditLog(t, harness, auditLogSeed{ID: 9301, ProfileID: profileID, RequestLogID: intPtr(9300), RequestLogCreatedAt: timePtr(requestCreatedAt), IngressRequestID: stringPtr("weak-request-9300"), ModelID: "audit-partition", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	insertAuditLog(t, harness, auditLogSeed{ID: 9302, ProfileID: otherProfileID, RequestLogID: intPtr(9300), RequestLogCreatedAt: timePtr(requestCreatedAt), IngressRequestID: stringPtr("weak-request-other"), ModelID: "audit-partition", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-9 * time.Minute)})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	items := listPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected profile-scoped audit partition list, got %+v", listPayload)
	}
	item := asMap(t, items[0])
	if jsonInt(t, item["request_log_id"]) != 9300 || item["ingress_request_id"] != "weak-request-9300" || item["request_log_created_at"] == nil || item["request_log_missing"] != false {
		t.Fatalf("expected audit list weak request fields, got %+v", item)
	}
}

func TestAuditLogs(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	streamBody := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}}}\n\n"
	insertRequestLogSummaryRow(t, harness, 700, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-20*time.Minute))
	insertAuditLog(t, harness, auditLogSeed{ID: 800, ProfileID: profileID, RequestLogID: intPtr(700), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, RequestBody: stringPtr(strings.Repeat("a", 210)), ResponseHeaders: stringPtr(`{"x-request-id":"req_1"}`), ResponseBody: stringPtr(`{"ok":true}`), ResponseStatus: 200, AuditEnabledAtRequest: false, AuditCaptureBodiesAtRequest: true, CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 701, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-8*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 801, ProfileID: profileID, RequestLogID: intPtr(701), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, RequestBody: stringPtr(strings.Repeat("b", 210)), ResponseHeaders: stringPtr(`{"x-request-id":"req_2"}`), ResponseBody: stringPtr(`{"ok":true}`), ResponseStatus: 200, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: true, CreatedAt: fixedS15Now.Add(-5 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 702, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-6*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 802, ProfileID: profileID, RequestLogID: intPtr(702), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, ResponseStatus: 200, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: false, CreatedAt: fixedS15Now.Add(-4 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 703, profileID, "audit-model", "openai", 12, 91, 200, 100, 7, 13, 20, fixedS15Now.Add(-3*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 803, ProfileID: profileID, RequestLogID: intPtr(703), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, RequestBody: stringPtr(`{"model":"audit-model","stream":true}`), ResponseHeaders: stringPtr(`{"content-type":"text/event-stream"}`), ResponseBody: &streamBody, ResponseStatus: 200, IsStream: true, AuditEnabledAtRequest: true, AuditCaptureBodiesAtRequest: true, CreatedAt: fixedS15Now.Add(-3 * time.Minute)})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&request_log_id=700&limit=20", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusConflict)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	if listPayload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit list, got %+v", listPayload)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs/800", nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusConflict)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	if detailPayload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit detail, got %+v", detailPayload)
	}

	visibleListResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", nil, modelHeader(profileID))
	assertStatus(t, visibleListResponse, http.StatusOK)
	var visibleListPayload map[string]any
	decodeJSONResponse(t, visibleListResponse, &visibleListPayload)
	items := visibleListPayload["items"].([]any)
	if visibleListPayload["has_more"] != false || visibleListPayload["next_cursor"] != nil || len(items) != 3 {
		t.Fatalf("expected audit list to show only enabled rows, got %+v", visibleListPayload)
	}
	if jsonInt(t, asMap(t, items[0])["id"]) != 803 || jsonInt(t, asMap(t, items[1])["id"]) != 802 || jsonInt(t, asMap(t, items[2])["id"]) != 801 {
		t.Fatalf("expected streaming row to sort ahead of metadata-only and full-capture rows, got %+v", visibleListPayload)
	}
	streamListItem := asMap(t, items[0])
	if streamListItem["is_stream"] != true || streamListItem["response_body_stored"] != true || streamListItem["audit_capture_bodies_at_request"] != true {
		t.Fatalf("expected audit list streaming row to expose stored-body metadata, got %+v", streamListItem)
	}

	metadataDetailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs/802", nil, modelHeader(profileID))
	assertStatus(t, metadataDetailResponse, http.StatusOK)
	var metadataDetailPayload map[string]any
	decodeJSONResponse(t, metadataDetailResponse, &metadataDetailPayload)
	if metadataDetailPayload["request_body"] != nil || metadataDetailPayload["response_body"] != nil || metadataDetailPayload["request_body_stored"] != false || metadataDetailPayload["response_body_stored"] != false || metadataDetailPayload["audit_capture_bodies_at_request"] != false {
		t.Fatalf("expected metadata-only audit detail to be a first-class nil-body state, got %+v", metadataDetailPayload)
	}

	enabledDetailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs/801", nil, modelHeader(profileID))
	assertStatus(t, enabledDetailResponse, http.StatusOK)
	var enabledDetailPayload map[string]any
	decodeJSONResponse(t, enabledDetailResponse, &enabledDetailPayload)
	if enabledDetailPayload["request_body"] == nil || enabledDetailPayload["response_body"] == nil || enabledDetailPayload["request_body_stored"] != true || enabledDetailPayload["response_body_stored"] != true || enabledDetailPayload["audit_capture_bodies_at_request"] != true {
		t.Fatalf("expected audit detail to return full captured bodies for enabled requests, got %+v", enabledDetailPayload)
	}

	streamDetailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs/803", nil, modelHeader(profileID))
	assertStatus(t, streamDetailResponse, http.StatusOK)
	var streamDetailPayload map[string]any
	decodeJSONResponse(t, streamDetailResponse, &streamDetailPayload)
	if streamDetailPayload["is_stream"] != true || streamDetailPayload["response_body_stored"] != true || streamDetailPayload["audit_capture_bodies_at_request"] != true || streamDetailPayload["response_body"] != streamBody {
		t.Fatalf("expected streaming audit detail to return raw stored SSE body, got %+v", streamDetailPayload)
	}
	streamResponseBody := streamDetailPayload["response_body"].(string)
	if !strings.Contains(streamResponseBody, "event: response.created") || !strings.Contains(streamResponseBody, "event: response.completed") || strings.HasPrefix(strings.TrimSpace(streamResponseBody), "{") {
		t.Fatalf("expected streaming audit detail response body to preserve raw SSE framing, got %q", streamResponseBody)
	}
}

func TestAuditLogRetentionJob(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertAuditLog(t, harness, auditLogSeed{ID: 900, ProfileID: profileID, ModelID: "audit-retention", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertAuditLog(t, harness, auditLogSeed{ID: 901, ProfileID: profileID, ModelID: "audit-retention", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	retentionResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		"/api/settings/log-retention",
		map[string]any{"audit_logs_retention_days": 1},
		modelHeader(profileID),
	)
	assertStatus(t, retentionResponse, http.StatusOK)

	jobID := createS15LogRetentionJob(t, harness, "audit_logs", map[string]any{}, "audit-policy")
	if jobID == "" || s15CountRows(t, harness, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected audit log-retention job to enqueue without inline delete")
	}
}

func TestManagementLogRetentionJobCreateContract(t *testing.T) {
	harness := newS15ContractHarness(t)
	jobID := createS15LogRetentionJob(t, harness, "loadbalance_events", map[string]any{"delete_all": true}, "create")
	if jobID == "" {
		t.Fatal("expected log-retention job id")
	}
}

func TestManagementLogRetentionJobIdempotency(t *testing.T) {
	harness := newS15ContractHarness(t)
	first := createS15LogRetentionJob(t, harness, "audit_logs", map[string]any{"delete_all": true}, "idem")
	second := createS15LogRetentionJob(t, harness, "audit_logs", map[string]any{"delete_all": true}, "idem")
	if first != second {
		t.Fatalf("expected idempotent log-retention create to return same job, got %s and %s", first, second)
	}
}

func TestRequestLogDeletionDoesNotWidenAuditVisibility(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 710, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-12*time.Minute))
	insertAuditLog(t, harness, auditLogSeed{ID: 810, ProfileID: profileID, RequestLogID: intPtr(710), ModelID: "audit-model", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: false, CreatedAt: fixedS15Now.Add(-11 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 711, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-10*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 811, ProfileID: profileID, RequestLogID: intPtr(711), ModelID: "audit-model", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-9 * time.Minute)})

	beforeDelete := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", nil, modelHeader(profileID))
	assertStatus(t, beforeDelete, http.StatusOK)
	var beforeDeletePayload map[string]any
	decodeJSONResponse(t, beforeDelete, &beforeDeletePayload)
	if len(beforeDeletePayload["items"].([]any)) != 1 || jsonInt(t, asMap(t, beforeDeletePayload["items"].([]any)[0])["id"]) != 811 {
		t.Fatalf("expected only enabled audit row visible before request-log deletion, got %+v", beforeDeletePayload)
	}

	jobID := createS15LogRetentionJob(t, harness, "request_logs", map[string]any{"delete_all": true}, "request-log-visibility")
	if jobID == "" {
		t.Fatal("expected request-log retention job id")
	}
	if s15CountRows(t, harness, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected global request-log retention job to avoid inline parent request deletion")
	}
	if s15CountRows(t, harness, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1 AND request_log_id IS NULL`, profileID) != 0 {
		t.Fatalf("expected global request-log retention job to avoid inline audit orphaning")
	}

	afterJob := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", nil, modelHeader(profileID))
	assertStatus(t, afterJob, http.StatusOK)
	var afterJobPayload map[string]any
	decodeJSONResponse(t, afterJob, &afterJobPayload)
	if len(afterJobPayload["items"].([]any)) != 1 || jsonInt(t, asMap(t, afterJobPayload["items"].([]any)[0])["id"]) != 811 {
		t.Fatalf("expected queued request-log retention job not to widen audit visibility inline, got %+v", afterJobPayload)
	}
}

func TestManagementAuditListRequiresWindow(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?limit=20", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusBadRequest)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	assertErrorCode(t, payload, "audit_window_required")
}

func TestManagementAuditListRejectsOversizedWindow(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	from := fixedS15Now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	to := fixedS15Now.Format(time.RFC3339)
	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?from="+from+"&to="+to, nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusBadRequest)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	assertErrorCode(t, payload, "audit_window_too_large")
}

func TestManagementAuditRejectsUnsupportedFilters(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	cases := []string{
		"actor_id=operator",
		"from_time=" + fixedS15Now.Add(-time.Hour).Format(time.RFC3339),
		"to_time=" + fixedS15Now.Format(time.RFC3339),
	}
	for _, query := range cases {
		response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&"+query, nil, modelHeader(profileID))
		assertStatus(t, response, http.StatusBadRequest)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		assertErrorCode(t, payload, "audit_filter_unsupported")
	}
}

func TestManagementAuditCursorIntegrity(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertAuditLog(t, harness, auditLogSeed{ID: 820, ProfileID: profileID, ModelID: "audit-cursor", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-3 * time.Minute)})
	insertAuditLog(t, harness, auditLogSeed{ID: 821, ProfileID: profileID, ModelID: "audit-cursor", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-2 * time.Minute)})

	firstResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1", nil, modelHeader(profileID))
	assertStatus(t, firstResponse, http.StatusOK)
	var firstPayload map[string]any
	decodeJSONResponse(t, firstResponse, &firstPayload)
	if firstPayload["has_more"] != true || firstPayload["next_cursor"] == nil {
		t.Fatalf("expected first audit cursor page to include next_cursor, got %+v", firstPayload)
	}

	cursor := firstPayload["next_cursor"].(string)
	secondResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1&cursor="+cursor, nil, modelHeader(profileID))
	assertStatus(t, secondResponse, http.StatusOK)
	var secondPayload map[string]any
	decodeJSONResponse(t, secondResponse, &secondPayload)
	if jsonInt(t, asMap(t, secondPayload["items"].([]any)[0])["id"]) != 820 {
		t.Fatalf("expected keyset cursor page to continue after newest row, got %+v", secondPayload)
	}

	tamperedCursor := cursor[:len(cursor)-1] + "x"
	tamperedResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1&cursor="+tamperedCursor, nil, modelHeader(profileID))
	assertStatus(t, tamperedResponse, http.StatusBadRequest)
	var tamperedPayload map[string]any
	decodeJSONResponse(t, tamperedResponse, &tamperedPayload)
	assertErrorCode(t, tamperedPayload, "audit_cursor_invalid")
}

func TestLoadbalanceCurrentState(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Loadbalance Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-model", stringPtr("Loadbalance Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 2, LastFailureKind: stringPtr("transient_http"), LastCooldownSeconds: 60.0, MaxCooldownStrikes: 1, BanMode: "off", BlockedUntilAt: timePtr(fixedS15Now.Add(30 * time.Minute)), ProbeEligibleLogged: false, CircuitState: "open", LiveP95LatencyMS: intPtr(540), CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/current-state?model_config_id=%d", modelConfigID), nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one current-state item, got %+v", payload)
	}
	item := asMap(t, items[0])
	if jsonInt(t, item["connection_id"]) != connectionID || item["state"] != "blocked" || item["blocked_until_at"] == nil || jsonInt(t, item["live_p95_latency_ms"]) != 540 {
		t.Fatalf("unexpected loadbalance current-state payload: %+v", item)
	}
}

func TestObservabilityLoadbalanceProbeEligibleStateAndSummaryRemainCoherent(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Probe Eligible Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-probe-eligible-model", stringPtr("Loadbalance Probe Eligible Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Probe Eligible Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	probeEligibleAt := fixedS15Now.Add(-1 * time.Minute)
	failureKind := "transient_http"
	insertRuntimeState(t, harness, runtimeStateSeed{
		ProfileID:           profileID,
		ConnectionID:        connectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &failureKind,
		LastCooldownSeconds: 60.0,
		MaxCooldownStrikes:  1,
		BanMode:             "off",
		BlockedUntilAt:      timePtr(probeEligibleAt),
		ProbeAvailableAt:    timePtr(probeEligibleAt),
		ProbeEligibleLogged: false,
		CircuitState:        "open",
		CreatedAt:           fixedS15Now.Add(-10 * time.Minute),
		UpdatedAt:           fixedS15Now,
	})

	currentStateResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/current-state?model_config_id=%d", modelConfigID), nil, modelHeader(profileID))
	assertStatus(t, currentStateResponse, http.StatusOK)
	var currentStatePayload map[string]any
	decodeJSONResponse(t, currentStateResponse, &currentStatePayload)
	item := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if item["state"] != "probe_eligible" || item["probe_eligible_logged"] != false || item["blocked_until_at"] == nil || item["probe_available_at"] == nil {
		t.Fatalf("expected probe-eligible current-state payload, got %+v", item)
	}
	blockedUntilAtRaw, ok := item["blocked_until_at"].(string)
	if !ok {
		t.Fatalf("expected blocked_until_at string in probe-eligible payload, got %+v", item)
	}
	probeAvailableAtRaw, ok := item["probe_available_at"].(string)
	if !ok {
		t.Fatalf("expected probe_available_at string in probe-eligible payload, got %+v", item)
	}
	if blockedUntilAtRaw != probeAvailableAtRaw {
		t.Fatalf("expected probe-eligible blocked/probe timestamps to stay aligned, got %+v", item)
	}

	blockedUntilMono := float64(probeEligibleAt.UTC().UnixNano()) / float64(time.Second)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1150, ProfileID: profileID, ConnectionID: connectionID, EventType: "probe_eligible", FailureKind: &failureKind, ConsecutiveFailures: 1, CooldownSeconds: 60.0, BlockedUntilMono: &blockedUntilMono, ModelID: stringPtr("lb-probe-eligible-model"), EndpointID: &endpointID, VendorID: &vendorID, FailureThreshold: intPtr(1), BackoffMultiplier: float64Ptr(2.0), MaxCooldownSeconds: intPtr(900), MaxCooldownStrikes: intPtr(1), BanMode: stringPtr("off"), CreatedAt: fixedS15Now})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events?model_id=lb-probe-eligible-model&limit=20&offset=0", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	event := s15LoadbalanceEventByConnectionID(t, listPayload, connectionID)
	summary := asMap(t, event["summary"])
	if event["event_type"] != "probe_eligible" || summary["event"] != "Connection became probe eligible" || summary["cooldown"] != "60 seconds open interval completed" || !strings.Contains(summary["reason"].(string), "open interval") {
		t.Fatalf("expected probe-eligible event summary payload, got %+v", event)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/events/%d", jsonInt(t, event["id"])), nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	detailSummary := asMap(t, detailPayload["summary"])
	if detailPayload["event_type"] != "probe_eligible" || detailSummary["event"] != "Connection became probe eligible" {
		t.Fatalf("expected probe-eligible event detail payload, got %+v", detailPayload)
	}
	if got, ok := detailPayload["blocked_until_mono"].(float64); !ok || math.Abs(got-blockedUntilMono) > 0.001 {
		t.Fatalf("expected probe-eligible event detail blocked_until_mono %.6f, got %+v", blockedUntilMono, detailPayload)
	}
}

func TestLoadbalanceReset(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Reset Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-reset-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Reset Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 3, LastFailureKind: stringPtr("timeout"), LastCooldownSeconds: 90.0, MaxCooldownStrikes: 2, BanMode: "manual", ProbeEligibleLogged: false, CircuitState: "open", CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})
	for range 3 {
		harness.runtimeService.RuntimeState().ClaimRoundRobinCursor(profileID, modelConfigID, 4)
	}

	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["cleared"] != true {
		t.Fatalf("unexpected loadbalance reset payload: %+v", payload)
	}
	if _, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID); ok {
		t.Fatalf("expected loadbalance reset to clear local runtime state")
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor != 0 {
		t.Fatalf("expected loadbalance reset to clear local round-robin cursor, got %d", cursor)
	}
}

func TestLoadbalanceEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1000, ProfileID: profileID, ConnectionID: 1, EventType: "opened", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 2, CooldownSeconds: 60.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), VendorID: intPtr(1), FailureThreshold: intPtr(2), BackoffMultiplier: float64Ptr(2.5), MaxCooldownSeconds: intPtr(900), MaxCooldownStrikes: intPtr(0), BanMode: stringPtr("off"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1001, ProfileID: profileID, ConnectionID: 1, EventType: "banned", FailureKind: stringPtr("transient_http"), ConsecutiveFailures: 3, CooldownSeconds: 120.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), VendorID: intPtr(1), FailureThreshold: intPtr(2), BackoffMultiplier: float64Ptr(3.0), MaxCooldownSeconds: intPtr(1200), MaxCooldownStrikes: intPtr(3), BanMode: stringPtr("temporary"), BannedUntilAt: timePtr(fixedS15Now.Add(1 * time.Hour)), CreatedAt: fixedS15Now.Add(-1 * time.Minute)})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events?model_id=lb-events-model&limit=50&offset=0", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	items := listPayload["items"].([]any)
	if jsonInt(t, listPayload["total"]) != 2 || len(items) != 2 {
		t.Fatalf("expected loadbalance events list payload, got %+v", listPayload)
	}
	first := asMap(t, items[0])
	if jsonInt(t, first["id"]) != 1001 || asMap(t, first["summary"])["event"] != "Connection was banned" {
		t.Fatalf("expected newest banned event with derived summary, got %+v", first)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events/1001", nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	if jsonInt(t, detailPayload["failure_threshold"]) != 2 || jsonInt(t, detailPayload["max_cooldown_seconds"]) != 1200 || detailPayload["ban_mode"] != "temporary" {
		t.Fatalf("expected loadbalance event detail payload, got %+v", detailPayload)
	}
}

func TestLoadbalancePartitionProfileScopedEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := s15InsertProfile(t, harness, "S15 Other Loadbalance")
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1200, ProfileID: profileID, ConnectionID: 1, EventType: "opened", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 30.0, ModelID: stringPtr("lb-partition-model"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1201, ProfileID: otherProfileID, ConnectionID: 1, EventType: "banned", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 2, CooldownSeconds: 60.0, ModelID: stringPtr("lb-partition-model"), CreatedAt: fixedS15Now.Add(-1 * time.Minute)})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events?model_id=lb-partition-model&limit=20&offset=0", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	items := listPayload["items"].([]any)
	if jsonInt(t, listPayload["total"]) != 1 || len(items) != 1 || jsonInt(t, asMap(t, items[0])["id"]) != 1200 {
		t.Fatalf("expected profile-scoped loadbalance event list over partitions, got %+v", listPayload)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events/1200", nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	if detailPayload["event_type"] != "opened" || jsonInt(t, detailPayload["id"]) != 1200 {
		t.Fatalf("expected loadbalance partition detail for selected profile, got %+v", detailPayload)
	}
}

func TestLoadbalanceEventRetentionJob(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1100, ProfileID: profileID, ConnectionID: 1, EventType: "opened", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1101, ProfileID: profileID, ConnectionID: 1, EventType: "opened", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	jobID := createS15LogRetentionJob(t, harness, "loadbalance_events", map[string]any{"cutoff": fixedS15Now.Add(-24 * time.Hour).Format(time.RFC3339)}, "loadbalance-events")
	if jobID == "" || s15CountRows(t, harness, `SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected loadbalance event retention job to enqueue without inline delete")
	}
}

func TestLoadbalanceCurrentStateReflectsRuntimeOpenedTransition(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	suffix := randomSuffix()
	publicModelID := "s15-runtime-current-state-proxy-" + suffix
	targetModelID := "s15-runtime-current-state-native-" + suffix
	autoRecovery := mustModelJSON(t, map[string]any{
		"mode":         "enabled",
		"status_codes": []int{503},
		"cooldown":     map[string]any{"base_seconds": 60, "failure_threshold": 1, "backoff_multiplier": 2.0, "max_cooldown_seconds": 900},
		"ban":          map[string]any{"mode": "off", "max_cooldown_strikes_before_ban": 0, "ban_duration_seconds": 0},
	})
	strategyID := s15InsertRuntimeLoadbalanceStrategy(t, harness, profileID, "S15 Runtime Current State "+suffix, "fill-first", autoRecovery)
	targetModelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", targetModelID, stringPtr("S15 Runtime Current State Native"), "native", &strategyID, true)
	publicModelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", publicModelID, stringPtr("S15 Runtime Current State Proxy"), "proxy", nil, true)
	s15InsertProxyTarget(t, harness, publicModelConfigID, targetModelConfigID, 0)
	primaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"primary unavailable"}`))
	}))
	defer primaryUpstream.Close()
	secondaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-s15-current-state"}`))
	}))
	defer secondaryUpstream.Close()
	primaryEndpointID := s15InsertRuntimeEndpoint(t, harness, profileID, "S15 Runtime Current State Primary "+suffix, primaryUpstream.URL, "s15-primary-key", 0)
	secondaryEndpointID := s15InsertRuntimeEndpoint(t, harness, profileID, "S15 Runtime Current State Secondary "+suffix, secondaryUpstream.URL, "s15-secondary-key", 1)
	primaryConnectionID := modelInsertConnection(t, harness, profileID, targetModelConfigID, primaryEndpointID, 0, true, nil)
	secondaryConnectionID := modelInsertConnection(t, harness, profileID, targetModelConfigID, secondaryEndpointID, 1, true, nil)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	runtimeResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "opened transition"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, runtimeResponse, http.StatusOK)

	currentStateResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/current-state?model_config_id=%d", targetModelConfigID), nil, modelHeader(profileID))
	assertStatus(t, currentStateResponse, http.StatusOK)
	var currentStatePayload map[string]any
	decodeJSONResponse(t, currentStateResponse, &currentStatePayload)
	primaryItem := s15CurrentStateItemByConnectionID(t, currentStatePayload, primaryConnectionID)
	if primaryItem["state"] != "blocked" || primaryItem["circuit_state"] != "open" || jsonInt(t, primaryItem["consecutive_failures"]) != 1 || primaryItem["last_failure_kind"] != "transient_http" || primaryItem["blocked_until_at"] == nil {
		t.Fatalf("expected primary current-state payload to reflect opened transition, got %+v", primaryItem)
	}
	blockedUntilAtRaw, ok := primaryItem["blocked_until_at"].(string)
	if !ok {
		t.Fatalf("expected blocked_until_at string in current-state payload, got %+v", primaryItem)
	}
	blockedUntilAt, err := time.Parse(time.RFC3339Nano, blockedUntilAtRaw)
	if err != nil {
		t.Fatalf("parse blocked_until_at %q: %v", blockedUntilAtRaw, err)
	}
	secondaryItem := s15CurrentStateItemByConnectionID(t, currentStatePayload, secondaryConnectionID)
	if secondaryItem["state"] != "counting" || secondaryItem["circuit_state"] != "closed" || secondaryItem["last_live_success_at"] == nil || jsonInt(t, secondaryItem["live_p95_latency_ms"]) < 1 {
		t.Fatalf("expected winning current-state payload to reflect recovered counting state, got %+v", secondaryItem)
	}

	eventsPayload := requestS15LoadbalanceEventsUntil(t, harness, profileID, fmt.Sprintf("/api/loadbalance/events?model_id=%s&limit=20&offset=0", targetModelID), primaryConnectionID, "")
	openedEvent := s15LoadbalanceEventByConnectionID(t, eventsPayload, primaryConnectionID)
	if openedEvent["event_type"] != "opened" || openedEvent["failure_kind"] != "transient_http" || asMap(t, openedEvent["summary"])["event"] != "Connection opened its circuit" || asMap(t, openedEvent["summary"])["cooldown"] != "60 seconds" {
		t.Fatalf("expected opened event payload to reflect runtime failure semantics, got %+v", openedEvent)
	}
	blockedUntilMono, ok := openedEvent["blocked_until_mono"].(float64)
	if !ok {
		t.Fatalf("expected opened event payload to include blocked_until_mono, got %+v", openedEvent)
	}
	expectedBlockedUntilMono := float64(blockedUntilAt.UTC().UnixNano()) / float64(time.Second)
	if math.Abs(blockedUntilMono-expectedBlockedUntilMono) > 0.001 {
		t.Fatalf("expected opened event blocked_until_mono %.6f to match current-state blocked_until_at %.6f", blockedUntilMono, expectedBlockedUntilMono)
	}
	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/events/%d", jsonInt(t, openedEvent["id"])), nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	detailBlockedUntilMono, ok := detailPayload["blocked_until_mono"].(float64)
	if !ok {
		t.Fatalf("expected opened event detail payload to include blocked_until_mono, got %+v", detailPayload)
	}
	if math.Abs(detailBlockedUntilMono-expectedBlockedUntilMono) > 0.001 {
		t.Fatalf("expected opened event detail blocked_until_mono %.6f to match current-state blocked_until_at %.6f", detailBlockedUntilMono, expectedBlockedUntilMono)
	}
	if jsonInt(t, detailPayload["failure_threshold"]) != 1 || jsonInt(t, detailPayload["max_cooldown_seconds"]) != 900 || detailPayload["ban_mode"] != "off" || jsonInt(t, detailPayload["endpoint_id"]) != primaryEndpointID {
		t.Fatalf("expected opened event detail payload to match runtime policy, got %+v", detailPayload)
	}
}

func TestLoadbalanceEventsReflectRuntimeRecoveredTransition(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	suffix := randomSuffix()
	publicModelID := "s15-runtime-recovered-proxy-" + suffix
	targetModelID := "s15-runtime-recovered-native-" + suffix
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Runtime Recovered "+suffix)
	targetModelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", targetModelID, stringPtr("S15 Runtime Recovered Native"), "native", &strategyID, true)
	publicModelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", publicModelID, stringPtr("S15 Runtime Recovered Proxy"), "proxy", nil, true)
	s15InsertProxyTarget(t, harness, publicModelConfigID, targetModelConfigID, 0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-s15-recovered"}`))
	}))
	defer upstream.Close()
	endpointID := s15InsertRuntimeEndpoint(t, harness, profileID, "S15 Runtime Recovered Endpoint "+suffix, upstream.URL, "s15-recovered-key", 0)
	connectionID := modelInsertConnection(t, harness, profileID, targetModelConfigID, endpointID, 0, true, nil)
	priorFailureKind := "transient_http"
	insertRuntimeState(t, harness, runtimeStateSeed{
		ProfileID:           profileID,
		ConnectionID:        connectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &priorFailureKind,
		LastCooldownSeconds: 60,
		MaxCooldownStrikes:  1,
		BanMode:             "off",
		BlockedUntilAt:      timePtr(fixedS15Now.Add(-1 * time.Minute)),
		ProbeEligibleLogged: false,
		CircuitState:        "open",
		LiveP95LatencyMS:    intPtr(999),
		LastLiveFailureAt:   timePtr(fixedS15Now.Add(-2 * time.Minute)),
		CreatedAt:           fixedS15Now.Add(-10 * time.Minute),
		UpdatedAt:           fixedS15Now.Add(-2 * time.Minute),
	})
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	currentStateResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/current-state?model_config_id=%d", targetModelConfigID), nil, modelHeader(profileID))
	assertStatus(t, currentStateResponse, http.StatusOK)
	var currentStatePayload map[string]any
	decodeJSONResponse(t, currentStateResponse, &currentStatePayload)
	currentStateItem := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if currentStateItem["state"] != "probe_eligible" || currentStateItem["circuit_state"] != "open" || currentStateItem["probe_eligible_logged"] != false {
		t.Fatalf("expected expired open interval to surface probe_eligible current state before recovery, got %+v", currentStateItem)
	}

	runtimeResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "recovered transition"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, runtimeResponse, http.StatusOK)

	listPayload := requestS15LoadbalanceEventsUntil(t, harness, profileID, fmt.Sprintf("/api/loadbalance/events?model_id=%s&limit=20&offset=0", targetModelID), connectionID, "recovered")
	probeEligibleEvent := s15LoadbalanceEventByConnectionIDAndType(t, listPayload, connectionID, "probe_eligible")
	if probeEligibleEvent["failure_kind"] != "transient_http" || asMap(t, probeEligibleEvent["summary"])["event"] != "Connection became probe eligible" || !strings.Contains(asMap(t, probeEligibleEvent["summary"])["cooldown"].(string), "open interval completed") {
		t.Fatalf("expected probe_eligible event payload to reflect runtime probe semantics, got %+v", probeEligibleEvent)
	}
	recoveredEvent := s15LoadbalanceEventByConnectionIDAndType(t, listPayload, connectionID, "recovered")
	if recoveredEvent["failure_kind"] != "transient_http" || asMap(t, recoveredEvent["summary"])["event"] != "Connection recovered" || !strings.Contains(asMap(t, recoveredEvent["summary"])["cooldown"].(string), "Recovered after a 60 seconds open interval") {
		t.Fatalf("expected recovered event payload to reflect runtime recovery semantics, got %+v", recoveredEvent)
	}
	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/events/%d", jsonInt(t, recoveredEvent["id"])), nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	if jsonInt(t, detailPayload["failure_threshold"]) != 2 || jsonInt(t, detailPayload["max_cooldown_seconds"]) != 900 || detailPayload["ban_mode"] != "off" || jsonInt(t, detailPayload["connection_id"]) != connectionID {
		t.Fatalf("expected recovered event detail payload to match runtime recovery state, got %+v", detailPayload)
	}
}

func newS15ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "s15_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s15-contract-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}
	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s15-contract-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	s15PartitionHarness := &contractHarness{conn: conn, dsn: settings.DatabaseURL}
	ensureContractTestLogPartitions(t, s15PartitionHarness,
		contractTestLogPartitionFor("request_logs", fixedS15Now.AddDate(0, 0, -2)),
		contractTestLogPartitionFor("request_logs", fixedS15Now.AddDate(0, 0, -1)),
		contractTestLogPartitionFor("request_logs", fixedS15Now),
		contractTestLogPartitionFor("audit_logs", fixedS15Now.AddDate(0, 0, -2)),
		contractTestLogPartitionFor("audit_logs", fixedS15Now.AddDate(0, 0, -1)),
		contractTestLogPartitionFor("audit_logs", fixedS15Now),
		contractTestLogPartitionFor("usage_request_events", fixedS15Now.AddDate(0, 0, -2)),
		contractTestLogPartitionFor("usage_request_events", fixedS15Now.AddDate(0, 0, -1)),
		contractTestLogPartitionFor("usage_request_events", fixedS15Now),
		contractTestLogPartitionFor("loadbalance_events", fixedS15Now.AddDate(0, 0, -2)),
		contractTestLogPartitionFor("loadbalance_events", fixedS15Now.AddDate(0, 0, -1)),
		contractTestLogPartitionFor("loadbalance_events", fixedS15Now),
	)
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	telemetryPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create runtime telemetry pgx pool: %v", err)
	}
	t.Cleanup(telemetryPool.Close)
	feedbackPool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create runtime feedback pgx pool: %v", err)
	}
	t.Cleanup(feedbackPool.Close)
	runtimeCache := runtimeapi.NewSharedCacheWithOptions(runtimeapi.SharedCacheOptions{RefreshPool: pool, SecretEncryptionKey: settings.SecretEncryptionKey})
	if err := runtimeCache.Bootstrap(testContext); err != nil {
		t.Fatalf("bootstrap published runtime snapshot: %v", err)
	}
	runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
	auditService, err := managementaudit.NewService(settings, managementaudit.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
	if err != nil {
		t.Fatalf("build audit service: %v", err)
	}
	t.Cleanup(auditService.Close)
	settingsService, err := managementsettings.NewService(settings, managementsettings.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
	if err != nil {
		t.Fatalf("build settings service: %v", err)
	}
	t.Cleanup(settingsService.Close)
	loadbalanceService, err := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }, RuntimeState: runtimeState})
	if err != nil {
		t.Fatalf("build loadbalance service: %v", err)
	}
	t.Cleanup(loadbalanceService.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
	if err != nil {
		t.Fatalf("build stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: pool, TelemetryPool: telemetryPool, FeedbackPool: feedbackPool, Now: func() time.Time { return fixedS15Now }, Cache: runtimeCache, RuntimeState: runtimeState})
	if err != nil {
		t.Fatalf("build runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s15-contract-test", AuditService: auditService, LoadbalanceService: loadbalanceService, RuntimeService: runtimeService, RuntimeCache: runtimeCache, RuntimeState: runtimeState, SettingsService: settingsService, StatsService: statsService})
	if err != nil {
		t.Fatalf("build S15 handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: nil, runtimeService: runtimeService, runtimeCache: runtimeCache, url: server.URL}
}

type usageEventSeed struct {
	ID                                int
	ProfileID                         int
	IngressRequestID                  string
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	EndpointID                        *int
	ConnectionID                      *int
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
	StatusCode                        int
	SuccessFlag                       bool
	BillableFlag                      *bool
	PricedFlag                        *bool
	UnpricedReason                    *string
	InputTokens                       *int
	OutputTokens                      *int
	TotalTokens                       *int
	CacheReadInputTokens              *int
	CacheCreationInputTokens          *int
	ReasoningTokens                   *int
	InputCostMicros                   *int64
	OutputCostMicros                  *int64
	CacheReadInputCostMicros          *int64
	CacheCreationInputCostMicros      *int64
	ReasoningCostMicros               *int64
	TotalCostOriginalMicros           *int64
	TotalCostUserCurrencyMicros       *int64
	CurrencyCodeOriginal              *string
	ReportCurrencyCode                *string
	ReportCurrencySymbol              *string
	FXRateUsed                        *string
	FXRateSource                      *string
	PricingSnapshotUnit               *string
	PricingSnapshotInput              *string
	PricingSnapshotOutput             *string
	PricingSnapshotCacheReadInput     *string
	PricingSnapshotCacheCreationInput *string
	PricingSnapshotReasoning          *string
	PricingConfigVersionUsed          *int
	AttemptCount                      int
	RequestPath                       string
	ResponseTimeMS                    *int
	TTFTMS                            *int
	CompletionDurationMS              *int
	CreatedAt                         time.Time
}

type auditLogSeed struct {
	ID                          int
	ProfileID                   int
	RequestLogID                *int
	RequestLogCreatedAt         *time.Time
	IngressRequestID            *string
	ModelID                     string
	RequestHeaders              string
	RequestBody                 *string
	RequestBodyStored           *bool
	ResponseStatus              int
	ResponseHeaders             *string
	ResponseBody                *string
	ResponseBodyStored          *bool
	IsStream                    bool
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	CreatedAt                   time.Time
}

type runtimeStateSeed struct {
	ProfileID           int
	ConnectionID        int
	ConsecutiveFailures int
	LastFailureKind     *string
	LastCooldownSeconds float64
	MaxCooldownStrikes  int
	BanMode             string
	BannedUntilAt       *time.Time
	BlockedUntilAt      *time.Time
	ProbeAvailableAt    *time.Time
	ProbeEligibleLogged bool
	CircuitState        string
	LiveP95LatencyMS    *int
	LastLiveFailureAt   *time.Time
	LastLiveSuccessAt   *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type loadbalanceEventSeed struct {
	ID                  int64
	ProfileID           int
	ConnectionID        int
	EventType           string
	FailureKind         *string
	ConsecutiveFailures int
	CooldownSeconds     float64
	BlockedUntilMono    *float64
	ModelID             *string
	EndpointID          *int
	VendorID            *int
	FailureThreshold    *int
	BackoffMultiplier   *float64
	MaxCooldownSeconds  *int
	MaxCooldownStrikes  *int
	BanMode             *string
	BannedUntilAt       *time.Time
	CreatedAt           time.Time
}

func insertUsageEvent(t *testing.T, harness *contractHarness, seed usageEventSeed) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", seed.CreatedAt))
	if _, err := harness.conn.Exec(
		context.Background(),
		`INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, billable_flag, priced_flag, unpriced_reason) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46)`,
		seed.ID,
		seed.ProfileID,
		seed.IngressRequestID,
		seed.ModelID,
		nullableTestString(seed.ResolvedTargetModelID),
		seed.APIFamily,
		nullableTestInt(seed.EndpointID),
		nullableTestInt(seed.ConnectionID),
		nullableTestInt(seed.ProxyAPIKeyID),
		nullableTestString(seed.ProxyAPIKeyNameSnapshot),
		seed.StatusCode,
		seed.SuccessFlag,
		nullableTestInt(seed.InputTokens),
		nullableTestInt(seed.OutputTokens),
		nullableTestInt(seed.TotalTokens),
		nullableTestInt(seed.CacheReadInputTokens),
		nullableTestInt(seed.CacheCreationInputTokens),
		nullableTestInt(seed.ReasoningTokens),
		nullableTestInt64(seed.InputCostMicros),
		nullableTestInt64(seed.OutputCostMicros),
		nullableTestInt64(seed.CacheReadInputCostMicros),
		nullableTestInt64(seed.CacheCreationInputCostMicros),
		nullableTestInt64(seed.ReasoningCostMicros),
		nullableTestInt64(seed.TotalCostOriginalMicros),
		nullableTestInt64(seed.TotalCostUserCurrencyMicros),
		nullableTestString(seed.CurrencyCodeOriginal),
		nullableTestString(seed.ReportCurrencyCode),
		nullableTestString(seed.ReportCurrencySymbol),
		nullableTestString(seed.FXRateUsed),
		nullableTestString(seed.FXRateSource),
		nullableTestString(seed.PricingSnapshotUnit),
		nullableTestString(seed.PricingSnapshotInput),
		nullableTestString(seed.PricingSnapshotOutput),
		nullableTestString(seed.PricingSnapshotCacheReadInput),
		nullableTestString(seed.PricingSnapshotCacheCreationInput),
		nullableTestString(seed.PricingSnapshotReasoning),
		nullableTestInt(seed.PricingConfigVersionUsed),
		seed.AttemptCount,
		seed.RequestPath,
		seed.CreatedAt,
		nullableTestInt(seed.ResponseTimeMS),
		nullableTestInt(seed.CompletionDurationMS),
		nullableTestInt(seed.TTFTMS),
		nullableTestBool(seed.BillableFlag),
		nullableTestBool(seed.PricedFlag),
		nullableTestString(seed.UnpricedReason),
	); err != nil {
		t.Fatalf("insert usage event %d: %v", seed.ID, err)
	}
}

func insertRequestLogSummaryRow(t *testing.T, harness *contractHarness, id int, profileID int, modelID string, apiFamily string, endpointID int, connectionID int, statusCode int, responseTimeMS int, inputTokens int, outputTokens int, totalTokens int, createdAt time.Time) {
	t.Helper()
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, id, profileID, modelID, apiFamily, endpointID, connectionID, statusCode, responseTimeMS, inputTokens, outputTokens, totalTokens, createdAt, false)
}

func insertRequestLogSummaryRowWithAuditEnabled(t *testing.T, harness *contractHarness, id int, profileID int, modelID string, apiFamily string, endpointID int, connectionID int, statusCode int, responseTimeMS int, inputTokens int, outputTokens int, totalTokens int, createdAt time.Time, auditEnabledAtRequest bool) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", createdAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, connection_id, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, request_path, created_at, endpoint_base_url, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE, $9, $10, $11, $12, TRUE, TRUE, '/v1/chat/completions', $13, $14, $15, FALSE)`, id, profileID, modelID, apiFamily, endpointID, connectionID, statusCode, responseTimeMS, inputTokens, outputTokens, totalTokens, statusCode >= 200 && statusCode < 300, createdAt, fmt.Sprintf("https://endpoint-%d.invalid", endpointID), auditEnabledAtRequest); err != nil {
		t.Fatalf("insert request-log summary row %d: %v", id, err)
	}
}

func insertAuditLog(t *testing.T, harness *contractHarness, seed auditLogSeed) {
	t.Helper()
	requestBodyStored := seed.RequestBody != nil
	if seed.RequestBodyStored != nil {
		requestBodyStored = *seed.RequestBodyStored
	}
	responseBodyStored := seed.ResponseBody != nil
	if seed.ResponseBodyStored != nil {
		responseBodyStored = *seed.ResponseBodyStored
	}
	auditCaptureBodiesAtRequest := seed.AuditCaptureBodiesAtRequest
	if !auditCaptureBodiesAtRequest && (seed.RequestBody != nil || seed.ResponseBody != nil) {
		auditCaptureBodiesAtRequest = true
	}
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("audit_logs", seed.CreatedAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO audit_logs (id, profile_id, request_log_id, request_log_created_at, ingress_request_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_stored, response_status, response_headers, response_body, response_body_stored, is_stream, duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, $3, $4, $5, NULL, $6, NULL, NULL, 'https://audit.invalid', 'Audit endpoint', 'POST', 'https://audit.invalid/v1/chat/completions', $7, $8, $9, $10, $11, $12, $13, $14, 1234, $15, $16, $17)`, seed.ID, seed.ProfileID, nullableTestInt(seed.RequestLogID), nullableTestTime(seed.RequestLogCreatedAt), nullableTestString(seed.IngressRequestID), seed.ModelID, seed.RequestHeaders, nullableTestString(seed.RequestBody), requestBodyStored, seed.ResponseStatus, nullableTestString(seed.ResponseHeaders), nullableTestString(seed.ResponseBody), responseBodyStored, seed.IsStream, seed.AuditEnabledAtRequest, auditCaptureBodiesAtRequest, seed.CreatedAt); err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			t.Fatalf("insert audit log %d at %s: %s (%s)", seed.ID, seed.CreatedAt.UTC().Format(time.RFC3339), pgErr.Message, pgErr.Detail)
		}
		t.Fatalf("insert audit log %d at %s: %v", seed.ID, seed.CreatedAt.UTC().Format(time.RFC3339), err)
	}
}

func insertRuntimeState(t *testing.T, harness *contractHarness, seed runtimeStateSeed) {
	t.Helper()
	if harness.runtimeService == nil {
		t.Fatal("runtime service is required for local runtime state seeding")
	}
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT model_config_id FROM connections WHERE id = $1`, seed.ConnectionID).Scan(&modelConfigID); err != nil {
		t.Fatalf("load model config for connection %d: %v", seed.ConnectionID, err)
	}
	banMode := seed.BanMode
	if strings.TrimSpace(banMode) == "" {
		banMode = "off"
	}
	circuitState := seed.CircuitState
	if strings.TrimSpace(circuitState) == "" {
		circuitState = "closed"
	}
	harness.runtimeService.RuntimeState().SeedConnectionState(seed.ProfileID, modelConfigID, seed.ConnectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:        seed.ConnectionID,
		CircuitState:        circuitState,
		BanMode:             banMode,
		BannedUntilAt:       seed.BannedUntilAt,
		OpenUntilAt:         seed.BlockedUntilAt,
		ProbeAvailableAt:    seed.ProbeAvailableAt,
		WindowRequestCount:  4,
		InFlightNonStream:   1,
		ConsecutiveFailures: seed.ConsecutiveFailures,
		LastFailureKind:     seed.LastFailureKind,
		LastCooldownSeconds: seed.LastCooldownSeconds,
		MaxCooldownStrikes:  seed.MaxCooldownStrikes,
		ProbeEligibleLogged: seed.ProbeEligibleLogged,
		LiveP95LatencyMS:    seed.LiveP95LatencyMS,
		LastLiveFailureAt:   seed.LastLiveFailureAt,
		LastLiveSuccessAt:   seed.LastLiveSuccessAt,
	}, seed.CreatedAt, seed.UpdatedAt)
}

func insertLoadbalanceEvent(t *testing.T, harness *contractHarness, seed loadbalanceEventSeed) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("loadbalance_events", seed.CreatedAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_events (id, profile_id, connection_id, event_type, failure_kind, consecutive_failures, cooldown_seconds, blocked_until_mono, model_id, endpoint_id, vendor_id, failure_threshold, backoff_multiplier, max_cooldown_seconds, max_cooldown_strikes, ban_mode, banned_until_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`, seed.ID, seed.ProfileID, seed.ConnectionID, seed.EventType, nullableTestString(seed.FailureKind), seed.ConsecutiveFailures, seed.CooldownSeconds, nullableTestFloat64(seed.BlockedUntilMono), nullableTestString(seed.ModelID), nullableTestInt(seed.EndpointID), nullableTestInt(seed.VendorID), nullableTestInt(seed.FailureThreshold), nullableTestFloat64(seed.BackoffMultiplier), nullableTestInt(seed.MaxCooldownSeconds), nullableTestInt(seed.MaxCooldownStrikes), nullableTestString(seed.BanMode), nullableTestTime(seed.BannedUntilAt), seed.CreatedAt); err != nil {
		t.Fatalf("insert loadbalance event %d: %v", seed.ID, err)
	}
}

func s15InsertRuntimeLoadbalanceStrategy(t *testing.T, harness *contractHarness, profileID int, name string, legacyStrategyType string, autoRecovery string) int {
	t.Helper()
	now := fixedS15Now.UTC()
	var strategyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at) VALUES ($1, $2, 'legacy', $3, $4::jsonb, NULL, $5, $5) RETURNING id`, profileID, name, legacyStrategyType, autoRecovery, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert S15 runtime loadbalance strategy %q: %v", name, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return strategyID
}

func s15InsertRuntimeEndpoint(t *testing.T, harness *contractHarness, profileID int, name string, baseURL string, apiKey string, position int) int {
	t.Helper()
	now := fixedS15Now.UTC()
	var endpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, profileID, name, baseURL, apiKey, position, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert S15 runtime endpoint %q: %v", name, err)
	}
	return endpointID
}

func s15InsertProxyTarget(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetModelConfigID int, position int) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position, weight, target_priority) VALUES ($1, $2, $3, 1, $3)`, sourceModelConfigID, targetModelConfigID, position); err != nil {
		t.Fatalf("insert S15 proxy target %d -> %d: %v", sourceModelConfigID, targetModelConfigID, err)
	}
}

func s15CurrentStateItemByConnectionID(t *testing.T, payload map[string]any, connectionID int) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected current-state items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		if jsonInt(t, item["connection_id"]) == connectionID {
			return item
		}
	}
	t.Fatalf("expected current-state payload for connection %d, got %+v", connectionID, payload)
	return nil
}

func s15LoadbalanceEventByConnectionID(t *testing.T, payload map[string]any, connectionID int) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected loadbalance event items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		if jsonInt(t, item["connection_id"]) == connectionID {
			return item
		}
	}
	t.Fatalf("expected loadbalance event for connection %d, got %+v", connectionID, payload)
	return nil
}

func s15LoadbalanceEventByConnectionIDAndType(t *testing.T, payload map[string]any, connectionID int, eventType string) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected loadbalance event items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		if jsonInt(t, item["connection_id"]) == connectionID && item["event_type"] == eventType {
			return item
		}
	}
	t.Fatalf("expected loadbalance event for connection %d with type %q, got %+v", connectionID, eventType, payload)
	return nil
}

func requestS15LoadbalanceEventsUntil(t *testing.T, harness *contractHarness, profileID int, path string, connectionID int, eventType string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var payload map[string]any
	for {
		response := harness.requestJSON(t, harness.client, http.MethodGet, path, nil, modelHeader(profileID))
		assertStatus(t, response, http.StatusOK)
		decodeJSONResponse(t, response, &payload)
		if s15PayloadHasLoadbalanceEvent(payload, connectionID, eventType) {
			return payload
		}
		if time.Now().After(deadline) {
			return payload
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func s15PayloadHasLoadbalanceEvent(payload map[string]any, connectionID int, eventType string) bool {
	items, ok := payload["items"].([]any)
	if !ok {
		return false
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if intFromJSONNumber(item["connection_id"]) != connectionID {
			continue
		}
		if eventType == "" || item["event_type"] == eventType {
			return true
		}
	}
	return false
}

func intFromJSONNumber(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

func insertContractProxyAPIKey(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	var keyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO proxy_api_keys (name, key_prefix, key_hash, last_four, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, TRUE, $5, $5) RETURNING id`, name, "sk_test", strings.Repeat("a", 64), "1234", fixedS15Now).Scan(&keyID); err != nil {
		t.Fatalf("insert proxy api key %q: %v", name, err)
	}
	return keyID
}

func s15CountRows(t *testing.T, harness *contractHarness, query string, args ...any) int {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows with %q: %v", query, err)
	}
	return count
}

func s15InsertProfile(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 0, NULL, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func dropS15RequestLogPartition(t *testing.T, harness *contractHarness, createdAt time.Time) {
	t.Helper()
	day := utcContractPartitionDay(createdAt)
	partitionName := fmt.Sprintf("request_logs_p%s", day.Format("20060102"))
	if _, err := harness.conn.Exec(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS public.%s`, quoteIdentifier(partitionName))); err != nil {
		t.Fatalf("drop request log partition %s: %v", partitionName, err)
	}
}

func s15ReadBackendSource(t *testing.T, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s15BackendRoot(t), relative))
	if err != nil {
		t.Fatalf("read backend source %s: %v", relative, err)
	}
	return string(raw)
}

func s15BackendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func withHeader(headers map[string]string, key string, value string) map[string]string {
	merged := make(map[string]string, len(headers)+1)
	maps.Copy(merged, headers)
	merged[key] = value
	return merged
}

func createS15LogRetentionJob(t *testing.T, harness *contractHarness, tableName string, scope map[string]any, suffix string) string {
	t.Helper()
	body := map[string]any{"table": tableName, "reason": "retention cleanup"}
	maps.Copy(body, scope)
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/maintenance/log-retention/jobs", body, withHeader(map[string]string{}, "Idempotency-Key", "log-retention-"+suffix))
	assertStatus(t, response, http.StatusAccepted)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	jobID, ok := payload["job_id"].(string)
	statusURL, _ := payload["status_url"].(string)
	if !ok || jobID == "" || payload["state"] != "queued" || statusURL != "/api/management/jobs/"+jobID || response.Header.Get("Location") != statusURL {
		t.Fatalf("expected log-retention job response, got %+v with Location %q", payload, response.Header.Get("Location"))
	}
	payloadScope := asMap(t, payload["scope"])
	if payloadScope["table"] != tableName {
		t.Fatalf("expected log-retention scope for %s, got %+v", tableName, payload)
	}
	return jobID
}

func nullableTestInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTestBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTestTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullableTestFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}

func boolPtr(value bool) *bool {
	resolved := value
	return &resolved
}

func int64Ptr(value int64) *int64 {
	resolved := value
	return &resolved
}

func float64Ptr(value float64) *float64 {
	resolved := value
	return &resolved
}

func timePtr(value time.Time) *time.Time {
	resolved := value.UTC()
	return &resolved
}

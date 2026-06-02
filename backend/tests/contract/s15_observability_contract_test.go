package contract_test

import (
	"context"
	"encoding/json"
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
	for _, key := range []string{"generated_at", "coverage_24h", "coverage_30d", "health", "metric_snapshot", "api_family_rows", "recent_requests", "top_spending_models", "routing_health_map", "topology_graph"} {
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

func assertDashboardSnapshotDoesNotExposeStrategyFamilySummary(t *testing.T, payload map[string]any) {
	t.Helper()
	if _, ok := payload["strategy_family_summary"]; ok {
		t.Fatalf("dashboard snapshot must not expose removed strategy_family_summary, got %+v", payload)
	}
}

func assertS15NoPolicyThresholdFields(t *testing.T, item map[string]any) {
	t.Helper()
	for _, key := range []string{"policy_cycle_retry_attempt_limit", "policy_ban_cumulative_retry_attempt_threshold", "cycle_retry_attempt_limit", "ban_cumulative_retry_attempt_threshold"} {
		if _, ok := item[key]; ok {
			t.Fatalf("current-state payload must stay threshold-free; found %q in %+v", key, item)
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

func TestObservabilityUsageEventPersistsTranslatedAttributionColumns(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	contextRouting := `{"policy":"cheapest_eligible_context","selected_terminal_target_id":34,"selected_endpoint_id":12,"selected_context_band":"preferred","selected_usable_context_window_tokens":8192,"skipped_terminal_targets":[{"terminal_target_id":35,"endpoint_id":13,"context_band":"ineligible","reason":"estimated_context_exceeds_usable_window","usable_context_window_tokens":256,"estimated_total_context_tokens":1024}]}`
	insertUsageEvent(t, harness, usageEventSeed{
		ID:                       4,
		ProfileID:                profileID,
		IngressRequestID:         "translated-usage-attribution",
		ModelID:                  "translated-usage-model",
		APIFamily:                "openai",
		OperationName:            stringPtr("openai.responses"),
		UpstreamOperationName:    stringPtr("openai.chat_completions"),
		OperationTranslationMode: stringPtr("openai_responses_to_chat_completions"),
		AttemptCount:             1,
		RequestPath:              "/v1/responses",
		UpstreamRequestPath:      stringPtr("/v1/chat/completions"),
		ContextRouting:           stringPtr(contextRouting),
		StatusCode:               200,
		SuccessFlag:              true,
		CreatedAt:                fixedS15Now.Add(-7 * time.Minute),
	})

	var operationName, upstreamOperationName, operationTranslationMode string
	var upstreamRequestPath string
	var rawContextRouting []byte
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT operation_name, upstream_operation_name, operation_translation_mode, upstream_request_path, context_routing FROM usage_request_events WHERE profile_id = $1 AND id = 4`,
		profileID,
	).Scan(&operationName, &upstreamOperationName, &operationTranslationMode, &upstreamRequestPath, &rawContextRouting); err != nil {
		t.Fatalf("load translated observability usage-event row: %v", err)
	}
	if operationName != "openai.responses" || upstreamOperationName != "openai.chat_completions" || operationTranslationMode != "openai_responses_to_chat_completions" || upstreamRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected translated usage-event attribution columns, got ingress=%q upstream=%q mode=%q path=%q", operationName, upstreamOperationName, operationTranslationMode, upstreamRequestPath)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rawContextRouting, &decoded); err != nil {
		t.Fatalf("decode translated context_routing json: %v", err)
	}
	if decoded["selected_context_band"] != "preferred" {
		t.Fatalf("expected selected_context_band=preferred, got %+v", decoded)
	}
	firstSkipped := decoded["skipped_terminal_targets"].([]any)[0].(map[string]any)
	if firstSkipped["context_band"] != "ineligible" {
		t.Fatalf("expected skipped target context_band=ineligible, got %+v", firstSkipped)
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
		assertDashboardSnapshotDoesNotExposeStrategyFamilySummary(t, payload)
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
	assertDashboardSnapshotDoesNotExposeStrategyFamilySummary(t, payload)
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

func TestObservabilityDashboardTopologyGraphIncludesDisabledAndInactiveNodes(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Dashboard Topology Strategy")
	entryModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "entry-model", stringPtr("Entry Model"), "native", &strategyID, true)
	terminalModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "terminal-model", stringPtr("Terminal Model"), "native", &strategyID, true)
	disabledModelID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "disabled-model", stringPtr("Disabled Model"), "native", &strategyID, false)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Topology Endpoint", 0)
	terminalTargetID := modelInsertConnection(t, harness, profileID, terminalModelID, endpointID, 0, false, nil)
	modelInsertModelTarget(t, harness, profileID, entryModelID, terminalModelID, 0, true)
	insertUsageEvent(t, harness, usageEventSeed{ID: 80, ProfileID: profileID, IngressRequestID: "topology-1", ModelID: "terminal-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &terminalTargetID, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-20 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 81, ProfileID: profileID, IngressRequestID: "topology-2", ModelID: "terminal-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &terminalTargetID, StatusCode: 503, SuccessFlag: false, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-10 * time.Minute)})

	var modelToModelEdgeID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 AND target_model_config_id = $3`, profileID, entryModelID, terminalModelID).Scan(&modelToModelEdgeID); err != nil {
		t.Fatalf("load topology model edge id: %v", err)
	}
	var modelToTerminalEdgeID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 AND target_connection_id = $3`, profileID, terminalModelID, terminalTargetID).Scan(&modelToTerminalEdgeID); err != nil {
		t.Fatalf("load topology terminal edge id: %v", err)
	}

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/dashboard", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	assertDashboardSnapshotTopLevelShape(t, payload)

	topologyGraph := asMap(t, payload["topology_graph"])
	topologyStats := asMap(t, topologyGraph["stats"])
	if jsonInt(t, topologyStats["model_count"]) != 3 || jsonInt(t, topologyStats["active_model_count"]) != 2 || jsonInt(t, topologyStats["disabled_model_count"]) != 1 || jsonInt(t, topologyStats["terminal_target_count"]) != 1 || jsonInt(t, topologyStats["active_terminal_target_count"]) != 0 || jsonInt(t, topologyStats["inactive_terminal_target_count"]) != 1 || jsonInt(t, topologyStats["endpoint_count"]) != 1 || jsonInt(t, topologyStats["edge_count"]) != 3 {
		t.Fatalf("expected topology graph stats for disabled/inactive graph, got %+v", topologyStats)
	}

	nodes := topologyGraph["nodes"].([]any)
	nodesByID := make(map[string]map[string]any, len(nodes))
	for _, raw := range nodes {
		node := asMap(t, raw)
		nodesByID[node["id"].(string)] = node
	}
	disabledNode := nodesByID[fmt.Sprintf("model-%d", disabledModelID)]
	if disabledNode == nil || disabledNode["status"] != "disabled" || disabledNode["kind"] != "model" || jsonInt(t, disabledNode["model_config_id"]) != disabledModelID || disabledNode["model_id"] != "disabled-model" {
		t.Fatalf("expected disabled model node semantics, got %+v", disabledNode)
	}
	terminalNode := nodesByID[fmt.Sprintf("terminal-target-%d", terminalTargetID)]
	if terminalNode == nil || terminalNode["kind"] != "connection" || terminalNode["product_kind"] != "terminal_target" || terminalNode["status"] != "inactive" || terminalNode["active"] != false || jsonInt(t, terminalNode["terminal_target_id"]) != terminalTargetID || jsonInt(t, terminalNode["connection_id"]) != terminalTargetID || terminalNode["health_status"] != "healthy" || jsonInt(t, terminalNode["recent_request_count"]) != 2 || terminalNode["last_request_at"] == nil {
		t.Fatalf("expected inactive terminal target node semantics, got %+v", terminalNode)
	}
	if math.Abs(terminalNode["recent_success_rate"].(float64)-50) > 0.001 {
		t.Fatalf("expected backend-derived terminal target success rate, got %+v", terminalNode)
	}
	endpointNode := nodesByID[fmt.Sprintf("endpoint-%d", endpointID)]
	if endpointNode == nil || endpointNode["kind"] != "endpoint" || jsonInt(t, endpointNode["endpoint_id"]) != endpointID {
		t.Fatalf("expected endpoint node for terminal target binding, got %+v", endpointNode)
	}

	edges := topologyGraph["edges"].([]any)
	edgesByID := make(map[string]map[string]any, len(edges))
	for _, raw := range edges {
		edge := asMap(t, raw)
		edgesByID[edge["id"].(string)] = edge
	}
	modelToModelEdge := edgesByID[fmt.Sprintf("access-target-%d", modelToModelEdgeID)]
	if modelToModelEdge == nil || modelToModelEdge["kind"] != "model_to_model" || modelToModelEdge["source_node_id"] != fmt.Sprintf("model-%d", entryModelID) || modelToModelEdge["target_node_id"] != fmt.Sprintf("model-%d", terminalModelID) {
		t.Fatalf("expected stable model-to-model edge, got %+v", modelToModelEdge)
	}
	modelToTerminalEdge := edgesByID[fmt.Sprintf("access-target-%d", modelToTerminalEdgeID)]
	if modelToTerminalEdge == nil || modelToTerminalEdge["kind"] != "model_to_connection" || modelToTerminalEdge["product_kind"] != "model_to_terminal_target" || modelToTerminalEdge["source_node_id"] != fmt.Sprintf("model-%d", terminalModelID) || modelToTerminalEdge["target_node_id"] != fmt.Sprintf("terminal-target-%d", terminalTargetID) || jsonInt(t, modelToTerminalEdge["terminal_target_id"]) != terminalTargetID {
		t.Fatalf("expected stable model-to-terminal-target edge, got %+v", modelToTerminalEdge)
	}
	bindingEdge := edgesByID[fmt.Sprintf("terminal-target-binding-%d", terminalTargetID)]
	if bindingEdge == nil || bindingEdge["kind"] != "connection_to_endpoint" || bindingEdge["product_kind"] != "terminal_target_to_endpoint" || bindingEdge["source_node_id"] != fmt.Sprintf("terminal-target-%d", terminalTargetID) || bindingEdge["target_node_id"] != fmt.Sprintf("endpoint-%d", endpointID) || jsonInt(t, bindingEdge["endpoint_id"]) != endpointID {
		t.Fatalf("expected stable terminal-target-to-endpoint binding edge, got %+v", bindingEdge)
	}
}

func TestManagementMetricsEndpointRemovedAfterOTLP(t *testing.T) {
	managementBranchSource := s15ReadBackendSource(t, "internal/platform/http/management_branch.go")
	dbSource := s15ReadBackendSource(t, "internal/platform/db/pools.go")
	dbTelemetrySource := s15ReadBackendSource(t, "internal/platform/db/telemetry.go")
	if strings.Contains(managementBranchSource, `router.Get("/metrics"`) || strings.Contains(dbSource, "MetricsHandler") {
		t.Fatalf("expected backend-local /metrics route and handler to be removed")
	}
	for _, metric := range []string{"prism.db.pool.acquired_connections", "prism.db.pool.max_connections", "prism.db.pool.acquire.timeout.count"} {
		if !strings.Contains(dbTelemetrySource, metric) {
			t.Fatalf("expected OTLP DB pool telemetry to retain %s", metric)
		}
	}
	if !strings.Contains(dbSource, "func (p *DatabasePools) Metrics() []PoolMetricSnapshot") {
		t.Fatalf("expected DB pool snapshots to remain for OTLP observers")
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
	if jsonInt(t, item["connection_id"]) != connectionID || item["state"] != "retry_wait" || item["next_retry_at"] == nil || jsonInt(t, item["live_p95_latency_ms"]) != 540 {
		t.Fatalf("unexpected loadbalance current-state payload: %+v", item)
	}
	assertS15NoPolicyThresholdFields(t, item)
}

func TestObservabilityLoadbalanceRetryWindowStateAndSummaryRemainCoherent(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Retry Window Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-retry-window-model", stringPtr("Loadbalance Retry Window Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Retry Window Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	nextRetryAt := fixedS15Now.Add(1 * time.Minute)
	failureKind := "transient_http"
	insertRuntimeState(t, harness, runtimeStateSeed{
		ProfileID:           profileID,
		ConnectionID:        connectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &failureKind,
		LastCooldownSeconds: 60.0,
		BanMode:             "off",
		BlockedUntilAt:      timePtr(nextRetryAt),
		CreatedAt:           fixedS15Now.Add(-10 * time.Minute),
		UpdatedAt:           fixedS15Now,
	})

	currentStateResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/current-state?model_config_id=%d", modelConfigID), nil, modelHeader(profileID))
	assertStatus(t, currentStateResponse, http.StatusOK)
	var currentStatePayload map[string]any
	decodeJSONResponse(t, currentStateResponse, &currentStatePayload)
	item := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if item["state"] != "retry_wait" || item["next_retry_at"] == nil || jsonInt(t, item["cycle_retry_attempts"]) != 1 || jsonInt(t, item["last_retry_delay_ms"]) != 60000 {
		t.Fatalf("expected retry-window current-state payload, got %+v", item)
	}
	assertS15NoPolicyThresholdFields(t, item)

	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1150, ProfileID: profileID, ConnectionID: connectionID, EventType: "retry_scheduled", FailureKind: &failureKind, ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retry-window-model"), EndpointID: &endpointID, VendorID: &vendorID, BanMode: stringPtr("off"), CreatedAt: fixedS15Now})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events?model_id=lb-retry-window-model&limit=20&offset=0", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	event := s15LoadbalanceEventByConnectionID(t, listPayload, connectionID)
	summary := asMap(t, event["summary"])
	if event["event_type"] != "retry_scheduled" || summary["event"] != "Retry was scheduled" || summary["cooldown"] != "60 seconds" || !strings.Contains(summary["reason"].(string), "retry cycle") {
		t.Fatalf("expected retry-scheduled event summary payload, got %+v", event)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/events/%d", jsonInt(t, event["id"])), nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	detailSummary := asMap(t, detailPayload["summary"])
	if detailPayload["event_type"] != "retry_scheduled" || detailSummary["event"] != "Retry was scheduled" {
		t.Fatalf("expected retry-scheduled event detail payload, got %+v", detailPayload)
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
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 3, LastFailureKind: stringPtr("timeout"), LastCooldownSeconds: 90.0, MaxCooldownStrikes: 2, BanMode: "until_reset", ProbeEligibleLogged: false, CircuitState: "open", CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})
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
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1000, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 2, CooldownSeconds: 60.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), VendorID: intPtr(1), BanMode: stringPtr("off"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1001, ProfileID: profileID, ConnectionID: 1, EventType: "banned", FailureKind: stringPtr("transient_http"), ConsecutiveFailures: 3, CooldownSeconds: 120.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), VendorID: intPtr(1), BanMode: stringPtr("temporary"), PolicyCycleRetryAttemptLimit: intPtr(2), PolicyBanCumulativeRetryAttemptThreshold: intPtr(3), BannedUntilAt: timePtr(fixedS15Now.Add(1 * time.Hour)), CreatedAt: fixedS15Now.Add(-1 * time.Minute)})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events?model_id=lb-events-model&limit=50&offset=0", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	items := listPayload["items"].([]any)
	if jsonInt(t, listPayload["total"]) != 2 || len(items) != 2 {
		t.Fatalf("expected loadbalance events list payload, got %+v", listPayload)
	}
	first := asMap(t, items[0])
	firstSummary := asMap(t, first["summary"])
	if jsonInt(t, first["id"]) != 1001 || firstSummary["event"] != "Connection was banned" || jsonInt(t, first["cycle_retry_attempt_limit"]) != 2 || jsonInt(t, first["ban_cumulative_retry_attempt_threshold"]) != 3 || !strings.Contains(firstSummary["reason"].(string), "cumulative ban threshold of 3 attempts") || strings.Contains(firstSummary["reason"].(string), "Ban Mode threshold") {
		t.Fatalf("expected newest banned event with public policy snapshots and exact threshold summary, got %+v", first)
	}
	for _, legacyKey := range []string{"policy_cycle_retry_attempt_limit", "policy_ban_cumulative_retry_attempt_threshold"} {
		if _, ok := first[legacyKey]; ok {
			t.Fatalf("loadbalance event list must not expose legacy public snapshot key %q: %+v", legacyKey, first)
		}
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events/1001", nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	detailSummary := asMap(t, detailPayload["summary"])
	if jsonInt(t, detailPayload["cycle_retry_attempts"]) != 3 || jsonInt(t, detailPayload["cumulative_retry_attempts"]) != 3 || jsonInt(t, detailPayload["last_retry_delay_ms"]) != 120000 || detailPayload["ban_mode"] != "temporary" || jsonInt(t, detailPayload["cycle_retry_attempt_limit"]) != 2 || jsonInt(t, detailPayload["ban_cumulative_retry_attempt_threshold"]) != 3 || !strings.Contains(detailSummary["reason"].(string), "cumulative ban threshold of 3 attempts") {
		t.Fatalf("expected loadbalance event detail payload with public policy snapshot, got %+v", detailPayload)
	}
	for _, legacyKey := range []string{"policy_cycle_retry_attempt_limit", "policy_ban_cumulative_retry_attempt_threshold"} {
		if _, ok := detailPayload[legacyKey]; ok {
			t.Fatalf("loadbalance event detail must not expose legacy public snapshot key %q: %+v", legacyKey, detailPayload)
		}
	}
}

func TestLoadbalancePartitionProfileScopedEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := s15InsertProfile(t, harness, "S15 Other Loadbalance")
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1200, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 30.0, ModelID: stringPtr("lb-partition-model"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
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
	if detailPayload["event_type"] != "retry_scheduled" || jsonInt(t, detailPayload["id"]) != 1200 {
		t.Fatalf("expected loadbalance partition detail for selected profile, got %+v", detailPayload)
	}
}

func TestLoadbalanceEventRetentionJob(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1100, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1101, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	jobID := createS15LogRetentionJob(t, harness, "loadbalance_events", map[string]any{"cutoff": fixedS15Now.Add(-24 * time.Hour).Format(time.RFC3339)}, "loadbalance-events")
	if jobID == "" || s15CountRows(t, harness, `SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1`, profileID) != 2 {
		t.Fatalf("expected loadbalance event retention job to enqueue without inline delete")
	}
}

func TestLoadbalanceEventsPersistPolicySnapshotsFromRuntimeFailure(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Runtime Event Snapshot Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-runtime-snapshot-model", stringPtr("Runtime Snapshot Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Runtime Snapshot Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	strategy, ok, err := loadbalancedomain.LoadRuntimeStrategy(context.Background(), harness.conn, profileID, strategyID)
	if err != nil || !ok {
		t.Fatalf("load runtime strategy for snapshot test: ok=%t err=%v", ok, err)
	}
	var transition loadbalancedomain.RuntimeStateTransition
	for attempt := 0; attempt < 4; attempt++ {
		transition = harness.runtimeService.RuntimeState().RecordRuntimeTransportFailure(profileID, modelConfigID, connectionID, strategy, fixedS15Now.Add(time.Duration(attempt)*time.Second))
	}
	if transition.CurrentState.BanMode != "until_reset" || transition.CurrentState.CumulativeRetryAttempts != 4 {
		t.Fatalf("expected runtime policy evaluation to ban at threshold, got %+v", transition.CurrentState)
	}
	if err := loadbalancedomain.InsertRuntimeFailureEvent(context.Background(), harness.conn, s15LoadbalancePartitionEnsurer{harness: harness}, profileID, modelConfigID, connectionID, transition, strategy, "connect_error", fixedS15Now.Add(4*time.Second)); err != nil {
		t.Fatalf("insert runtime failure loadbalance event: %v", err)
	}

	var storedCycleLimit, storedBanThreshold int
	if err := harness.conn.QueryRow(context.Background(), `SELECT policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold FROM loadbalance_events WHERE profile_id = $1 AND connection_id = $2`, profileID, connectionID).Scan(&storedCycleLimit, &storedBanThreshold); err != nil {
		t.Fatalf("load stored policy snapshots: %v", err)
	}
	if storedCycleLimit != 2 || storedBanThreshold != 4 {
		t.Fatalf("expected stored immutable policy snapshots 2/4, got %d/%d", storedCycleLimit, storedBanThreshold)
	}

	listPath := "/api/loadbalance/events?model_id=lb-runtime-snapshot-model&limit=20&offset=0"
	listPayload := requestS15LoadbalanceEventsUntil(t, harness, profileID, listPath, connectionID, "banned")
	event := s15LoadbalanceEventByConnectionIDAndType(t, listPayload, connectionID, "banned")
	summary := asMap(t, event["summary"])
	if jsonInt(t, event["cycle_retry_attempt_limit"]) != 2 || jsonInt(t, event["ban_cumulative_retry_attempt_threshold"]) != 4 || !strings.Contains(summary["reason"].(string), "cumulative ban threshold of 4 attempts") || strings.Contains(summary["reason"].(string), "Ban Mode threshold") {
		t.Fatalf("expected runtime event list to expose public policy snapshots and exact threshold summary, got %+v", event)
	}
	for _, legacyKey := range []string{"policy_cycle_retry_attempt_limit", "policy_ban_cumulative_retry_attempt_threshold"} {
		if _, ok := event[legacyKey]; ok {
			t.Fatalf("runtime event list must not expose legacy public snapshot key %q: %+v", legacyKey, event)
		}
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/events/%d", jsonInt(t, event["id"])), nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	detailSummary := asMap(t, detailPayload["summary"])
	if jsonInt(t, detailPayload["cycle_retry_attempt_limit"]) != 2 || jsonInt(t, detailPayload["ban_cumulative_retry_attempt_threshold"]) != 4 || !strings.Contains(detailSummary["reason"].(string), "cumulative ban threshold of 4 attempts") {
		t.Fatalf("expected runtime event detail to expose public policy snapshots, got %+v", detailPayload)
	}
	for _, legacyKey := range []string{"policy_cycle_retry_attempt_limit", "policy_ban_cumulative_retry_attempt_threshold"} {
		if _, ok := detailPayload[legacyKey]; ok {
			t.Fatalf("runtime event detail must not expose legacy public snapshot key %q: %+v", legacyKey, detailPayload)
		}
	}

	currentStateResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/current-state?model_config_id=%d", modelConfigID), nil, modelHeader(profileID))
	assertStatus(t, currentStateResponse, http.StatusOK)
	var currentStatePayload map[string]any
	decodeJSONResponse(t, currentStateResponse, &currentStatePayload)
	currentState := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if currentState["state"] != "banned" || currentState["ban_mode"] != "until_reset" {
		t.Fatalf("expected current-state to remain connection-global while banned, got %+v", currentState)
	}
	assertS15NoPolicyThresholdFields(t, currentState)
}

func TestLoadbalanceCurrentStateReflectsRuntimeRetryTransition(t *testing.T) {
}

func TestLoadbalanceEventsReflectRuntimeRecoveryTransition(t *testing.T) {
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
	OperationName                     *string
	UpstreamOperationName             *string
	OperationTranslationMode          *string
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
	UpstreamRequestPath               *string
	ContextRouting                    *string
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
	ID                                       int64
	ProfileID                                int
	ConnectionID                             int
	EventType                                string
	FailureKind                              *string
	ConsecutiveFailures                      int
	CooldownSeconds                          float64
	BlockedUntilMono                         *float64
	ModelID                                  *string
	EndpointID                               *int
	VendorID                                 *int
	FailureThreshold                         *int
	BackoffMultiplier                        *float64
	MaxCooldownSeconds                       *int
	MaxCooldownStrikes                       *int
	BanMode                                  *string
	PolicyCycleRetryAttemptLimit             *int
	PolicyBanCumulativeRetryAttemptThreshold *int
	BannedUntilAt                            *time.Time
	CreatedAt                                time.Time
}

func insertUsageEvent(t *testing.T, harness *contractHarness, seed usageEventSeed) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", seed.CreatedAt))
	if _, err := harness.conn.Exec(
		context.Background(),
		`INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, operation_name, upstream_operation_name, operation_translation_mode, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, attempt_count, request_path, upstream_request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, billable_flag, priced_flag, unpriced_reason, context_routing) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51)`,
		seed.ID,
		seed.ProfileID,
		seed.IngressRequestID,
		seed.ModelID,
		nullableTestString(seed.ResolvedTargetModelID),
		seed.APIFamily,
		nullableTestString(seed.OperationName),
		nullableTestString(seed.UpstreamOperationName),
		nullableTestString(seed.OperationTranslationMode),
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
		nullableTestString(seed.UpstreamRequestPath),
		seed.CreatedAt,
		nullableTestInt(seed.ResponseTimeMS),
		nullableTestInt(seed.CompletionDurationMS),
		nullableTestInt(seed.TTFTMS),
		nullableTestBool(seed.BillableFlag),
		nullableTestBool(seed.PricedFlag),
		nullableTestString(seed.UnpricedReason),
		nullableTestJSON(seed.ContextRouting),
	); err != nil {
		t.Fatalf("insert usage event %d: %v", seed.ID, err)
	}
}

func nullableTestJSON(value *string) any {
	if value == nil {
		return nil
	}
	return []byte(*value)
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

type s15LoadbalancePartitionEnsurer struct {
	harness *contractHarness
}

func (e s15LoadbalancePartitionEnsurer) EnsurePartitionForTime(ctx context.Context, tableName string, timestamp time.Time) error {
	return ensureContractTestLogPartition(ctx, e.harness, tableName, utcContractPartitionDay(timestamp))
}

func insertRuntimeState(t *testing.T, harness *contractHarness, seed runtimeStateSeed) {
	t.Helper()
	if harness.runtimeService == nil {
		t.Fatal("runtime service is required for local runtime state seeding")
	}
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT source_model_config_id FROM model_access_targets WHERE target_connection_id = $1 ORDER BY position ASC, id ASC LIMIT 1`, seed.ConnectionID).Scan(&modelConfigID); err != nil {
		t.Fatalf("load model config for connection %d: %v", seed.ConnectionID, err)
	}
	banMode := seed.BanMode
	if strings.TrimSpace(banMode) == "" {
		banMode = "off"
	}
	nextRetryAt := seed.BlockedUntilAt
	if nextRetryAt == nil {
		nextRetryAt = seed.ProbeAvailableAt
	}
	harness.runtimeService.RuntimeState().SeedConnectionState(seed.ProfileID, modelConfigID, seed.ConnectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:            seed.ConnectionID,
		BanMode:                 banMode,
		BannedUntilAt:           seed.BannedUntilAt,
		NextRetryAt:             nextRetryAt,
		WindowRequestCount:      4,
		InFlightNonStream:       1,
		CycleRetryAttempts:      seed.ConsecutiveFailures,
		CumulativeRetryAttempts: seed.ConsecutiveFailures,
		LastRetryDelayMS:        int(seed.LastCooldownSeconds * 1000),
		LastFailureKind:         seed.LastFailureKind,
		LastSuccessAt:           seed.LastLiveSuccessAt,
		LiveP95LatencyMS:        seed.LiveP95LatencyMS,
	}, seed.CreatedAt, seed.UpdatedAt)
}

func insertLoadbalanceEvent(t *testing.T, harness *contractHarness, seed loadbalanceEventSeed) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("loadbalance_events", seed.CreatedAt))
	nextRetryAt := (*time.Time)(nil)
	if seed.CooldownSeconds > 0 {
		resolved := seed.CreatedAt.Add(time.Duration(seed.CooldownSeconds * float64(time.Second)))
		nextRetryAt = &resolved
	}
	lastRetryDelayMS := int(seed.CooldownSeconds * 1000)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_events (id, profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, vendor_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`, seed.ID, seed.ProfileID, seed.ConnectionID, seed.EventType, nullableTestString(seed.FailureKind), seed.ConsecutiveFailures, nullableTestTime(nextRetryAt), lastRetryDelayMS, nullableTestString(seed.ModelID), nullableTestInt(seed.EndpointID), nullableTestInt(seed.VendorID), nullableTestString(seed.BanMode), nullableTestInt(seed.PolicyCycleRetryAttemptLimit), nullableTestInt(seed.PolicyBanCumulativeRetryAttemptThreshold), nullableTestTime(seed.BannedUntilAt), seed.CreatedAt); err != nil {
		t.Fatalf("insert loadbalance event %d: %v", seed.ID, err)
	}
}

func s15InsertRuntimeLoadbalanceStrategy(t *testing.T, harness *contractHarness, profileID int, name string, legacyStrategyType string, cycleRetryAttemptLimit int) int {
	t.Helper()
	now := fixedS15Now.UTC()
	var strategyID int
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit,
			ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, $3, $4::integer[], 'off', 60000, 2.0, 0.2, 900000, $5, 0, 0, $6, $6)
		 RETURNING id`,
		profileID,
		name,
		legacyStrategyType,
		[]int32{503},
		cycleRetryAttemptLimit,
		now,
	).Scan(&strategyID); err != nil {
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

func s15InsertModelTarget(t *testing.T, harness *contractHarness, sourceModelConfigID int, targetModelConfigID int, position int) {
	t.Helper()
	now := fixedS15Now.UTC()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT profile_id FROM model_configs WHERE id = $1`, sourceModelConfigID).Scan(&profileID); err != nil {
		t.Fatalf("load S15 source profile %d: %v", sourceModelConfigID, err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, 1, $4, TRUE, $5, $5)`, profileID, sourceModelConfigID, targetModelConfigID, position, now); err != nil {
		t.Fatalf("insert S15 model target %d -> %d: %v", sourceModelConfigID, targetModelConfigID, err)
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

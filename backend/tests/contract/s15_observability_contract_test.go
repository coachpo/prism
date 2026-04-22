package contract_test

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

var fixedS15Now = time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)

func TestUsageSnapshot(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Snapshot Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "snapshot-model", stringPtr("Snapshot Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Snapshot Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	proxyKeyID := insertContractProxyAPIKey(t, harness, "Snapshot Key")
	insertUsageEvent(t, harness, usageEventSeed{ID: 1, ProfileID: profileID, IngressRequestID: "snap-1", ModelID: "snapshot-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &connectionID, ProxyAPIKeyID: &proxyKeyID, ProxyAPIKeyNameSnapshot: stringPtr("Snapshot Key"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(30), CacheReadInputTokens: intPtr(5), CacheCreationInputTokens: intPtr(2), ReasoningTokens: intPtr(1), TotalCostUserCurrencyMicros: int64Ptr(2500), AttemptCount: 1, RequestPath: "/v1/chat/completions", ResponseTimeMS: intPtr(800), TTFTMS: intPtr(100), CompletionDurationMS: intPtr(1100), CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 2, ProfileID: profileID, IngressRequestID: "snap-2", ModelID: "snapshot-model", APIFamily: "openai", EndpointID: &endpointID, ConnectionID: &connectionID, StatusCode: 500, SuccessFlag: false, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), InputTokens: intPtr(15), OutputTokens: intPtr(25), TotalTokens: intPtr(40), AttemptCount: 1, RequestPath: "/v1/chat/completions", ResponseTimeMS: intPtr(900), CreatedAt: fixedS15Now.Add(-5 * time.Minute)})

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/usage-snapshot?preset=1h", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	overview := asMap(t, payload["overview"])
	if jsonInt(t, overview["total_requests"]) != 2 || jsonInt(t, overview["success_requests"]) != 1 || jsonInt(t, overview["failed_requests"]) != 1 || jsonInt(t, overview["total_tokens"]) != 70 {
		t.Fatalf("expected usage snapshot overview totals, got %+v", overview)
	}
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
	if row["model_id"] != "endpoint-model" || row["model_label"] != "Endpoint Model" || jsonInt(t, row["request_count"]) != 3 || jsonInt(t, row["success_count"]) != 2 || jsonInt(t, row["failed_count"]) != 1 || jsonInt(t, row["priced_request_count"]) != 1 || jsonInt(t, row["unpriced_request_count"]) != 1 {
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
	endpointA := modelInsertEndpoint(t, harness, profileID, "Spend Endpoint A", 0)
	endpointB := modelInsertEndpoint(t, harness, profileID, "Spend Endpoint B", 1)
	insertUsageEvent(t, harness, usageEventSeed{ID: 30, ProfileID: profileID, IngressRequestID: "spend-1", ModelID: "spend-model", APIFamily: "openai", EndpointID: &endpointA, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(50), TotalCostUserCurrencyMicros: int64Ptr(5000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-4 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 31, ProfileID: profileID, IngressRequestID: "spend-2", ModelID: "spend-model", APIFamily: "openai", EndpointID: &endpointB, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(false), UnpricedReason: stringPtr("PRICING_DISABLED"), TotalTokens: intPtr(25), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-3 * time.Hour)})

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/spending?preset=all&group_by=model_endpoint&limit=50&offset=0", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	summary := asMap(t, payload["summary"])
	if jsonInt(t, summary["successful_request_count"]) != 2 || jsonInt(t, summary["priced_request_count"]) != 1 || jsonInt(t, summary["unpriced_request_count"]) != 1 || jsonInt(t, summary["total_cost_micros"]) != 5000 || jsonInt(t, payload["groups_total"]) != 2 {
		t.Fatalf("unexpected spending summary payload: %+v", payload)
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
	topEndpoints := payload["top_spending_endpoints"].([]any)
	if len(topEndpoints) == 0 || asMap(t, topEndpoints[0])["endpoint_label"] != "Spend Endpoint A" {
		t.Fatalf("expected top spending endpoints to use current endpoint labels, got %+v", payload)
	}
	if jsonInt(t, asMap(t, payload["unpriced_breakdown"])["PRICING_DISABLED"]) != 1 {
		t.Fatalf("expected canonical unpriced breakdown to count PRICING_DISABLED, got %+v", payload)
	}
}

func TestStatsDelete(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 500, profileID, "delete-model", "openai", 12, 81, 200, 100, 0, 0, 0, fixedS15Now.Add(-48*time.Hour))
	insertRequestLogSummaryRow(t, harness, 501, profileID, "delete-model", "openai", 12, 81, 200, 100, 0, 0, 0, fixedS15Now.Add(-30*time.Minute))
	insertUsageEvent(t, harness, usageEventSeed{ID: 40, ProfileID: profileID, IngressRequestID: "stats-delete-old", ModelID: "delete-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 41, ProfileID: profileID, IngressRequestID: "stats-delete-new", ModelID: "delete-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	deleteRequestLogs := harness.requestJSON(t, harness.client, http.MethodDelete, "/api/stats/requests?older_than_days=1", nil, modelHeader(profileID))
	assertStatus(t, deleteRequestLogs, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, deleteRequestLogs, &payload)
	if payload["accepted"] != true {
		t.Fatalf("expected stats request-log delete accepted response, got %+v", payload)
	}
	if s15CountRows(t, harness, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1`, profileID) != 1 {
		t.Fatalf("expected request-log delete to remove only old rows")
	}

	deleteStatistics := harness.requestJSON(t, harness.client, http.MethodDelete, "/api/stats/statistics?delete_all=true", nil, modelHeader(profileID))
	assertStatus(t, deleteStatistics, http.StatusOK)
	decodeJSONResponse(t, deleteStatistics, &payload)
	if payload["accepted"] != true || s15CountRows(t, harness, `SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1`, profileID) != 0 {
		t.Fatalf("expected statistics delete to clear usage events, got %+v", payload)
	}
}

func TestAuditLogs(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 700, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-20*time.Minute))
	insertAuditLog(t, harness, auditLogSeed{ID: 800, ProfileID: profileID, RequestLogID: intPtr(700), ModelID: "audit-model", RequestHeaders: `{"authorization":"Bearer [REDACTED]"}`, RequestBody: stringPtr(strings.Repeat("a", 210)), ResponseHeaders: stringPtr(`{"x-request-id":"req_1"}`), ResponseBody: stringPtr(`{"ok":true}`), ResponseStatus: 200, CreatedAt: fixedS15Now.Add(-10 * time.Minute)})

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs?request_log_id=700&limit=20", nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusConflict)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	if listPayload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit list, got %+v", listPayload)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/audit/logs/800", nil, modelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	if detailPayload["request_body"] == nil || detailPayload["response_body"] == nil {
		t.Fatalf("expected audit detail to return full captured bodies, got %+v", detailPayload)
	}
}

func TestAuditDelete(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertAuditLog(t, harness, auditLogSeed{ID: 900, ProfileID: profileID, ModelID: "audit-delete", RequestHeaders: `{}`, ResponseStatus: 200, CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertAuditLog(t, harness, auditLogSeed{ID: 901, ProfileID: profileID, ModelID: "audit-delete", RequestHeaders: `{}`, ResponseStatus: 200, CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	response := harness.requestJSON(t, harness.client, http.MethodDelete, "/api/audit/logs?older_than_days=1", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["accepted"] != true || s15CountRows(t, harness, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1`, profileID) != 1 {
		t.Fatalf("expected audit delete to remove only old rows, got %+v", payload)
	}
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

func TestLoadbalanceReset(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Reset Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-reset-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Reset Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 3, LastFailureKind: stringPtr("timeout"), LastCooldownSeconds: 90.0, MaxCooldownStrikes: 2, BanMode: "manual", ProbeEligibleLogged: false, CircuitState: "open", CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_round_robin_state (profile_id, model_config_id, next_cursor, created_at, updated_at) VALUES ($1, $2, 3, $3, $3)`, profileID, modelConfigID, fixedS15Now); err != nil {
		t.Fatalf("insert round-robin state: %v", err)
	}

	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["cleared"] != true {
		t.Fatalf("unexpected loadbalance reset payload: %+v", payload)
	}
	if s15CountRows(t, harness, `SELECT COUNT(*) FROM routing_connection_runtime_state WHERE profile_id = $1 AND connection_id = $2`, profileID, connectionID) != 0 || s15CountRows(t, harness, `SELECT COUNT(*) FROM loadbalance_round_robin_state WHERE profile_id = $1 AND model_config_id = $2`, profileID, modelConfigID) != 0 {
		t.Fatalf("expected loadbalance reset to clear runtime and round-robin state")
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

func TestLoadbalanceDeleteEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1100, ProfileID: profileID, ConnectionID: 1, EventType: "opened", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-delete-model"), CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1101, ProfileID: profileID, ConnectionID: 1, EventType: "opened", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-delete-model"), CreatedAt: fixedS15Now.Add(-30 * time.Minute)})

	response := harness.requestJSON(t, harness.client, http.MethodDelete, "/api/loadbalance/events?older_than_days=1", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["accepted"] != true || s15CountRows(t, harness, `SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1`, profileID) != 1 {
		t.Fatalf("expected loadbalance event delete to remove only old rows, got %+v", payload)
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

	eventsResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/events?model_id=%s&limit=20&offset=0", targetModelID), nil, modelHeader(profileID))
	assertStatus(t, eventsResponse, http.StatusOK)
	var eventsPayload map[string]any
	decodeJSONResponse(t, eventsResponse, &eventsPayload)
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

	runtimeResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "recovered transition"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, runtimeResponse, http.StatusOK)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/loadbalance/events?model_id=%s&limit=20&offset=0", targetModelID), nil, modelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listPayload map[string]any
	decodeJSONResponse(t, listResponse, &listPayload)
	recoveredEvent := s15LoadbalanceEventByConnectionID(t, listPayload, connectionID)
	if recoveredEvent["event_type"] != "recovered" || recoveredEvent["failure_kind"] != "transient_http" || asMap(t, recoveredEvent["summary"])["event"] != "Connection recovered" || !strings.Contains(asMap(t, recoveredEvent["summary"])["cooldown"].(string), "Recovered after a 60 seconds open interval") {
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
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	auditService, err := managementaudit.NewService(settings, managementaudit.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
	if err != nil {
		t.Fatalf("build audit service: %v", err)
	}
	t.Cleanup(auditService.Close)
	loadbalanceService, err := managementloadbalance.NewService(settings, managementloadbalance.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
	if err != nil {
		t.Fatalf("build loadbalance service: %v", err)
	}
	t.Cleanup(loadbalanceService.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
	if err != nil {
		t.Fatalf("build stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }})
	if err != nil {
		t.Fatalf("build runtime service: %v", err)
	}
	t.Cleanup(runtimeService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s15-contract-test", AuditService: auditService, LoadbalanceService: loadbalanceService, RuntimeService: runtimeService, StatsService: statsService})
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
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: nil, url: server.URL}
}

type usageEventSeed struct {
	ID                          int
	ProfileID                   int
	IngressRequestID            string
	ModelID                     string
	ResolvedTargetModelID       *string
	APIFamily                   string
	EndpointID                  *int
	ConnectionID                *int
	ProxyAPIKeyID               *int
	ProxyAPIKeyNameSnapshot     *string
	StatusCode                  int
	SuccessFlag                 bool
	BillableFlag                *bool
	PricedFlag                  *bool
	UnpricedReason              *string
	InputTokens                 *int
	OutputTokens                *int
	TotalTokens                 *int
	CacheReadInputTokens        *int
	CacheCreationInputTokens    *int
	ReasoningTokens             *int
	TotalCostUserCurrencyMicros *int64
	AttemptCount                int
	RequestPath                 string
	ResponseTimeMS              *int
	TTFTMS                      *int
	CompletionDurationMS        *int
	CreatedAt                   time.Time
}

type auditLogSeed struct {
	ID              int
	ProfileID       int
	RequestLogID    *int
	ModelID         string
	RequestHeaders  string
	RequestBody     *string
	ResponseStatus  int
	ResponseHeaders *string
	ResponseBody    *string
	CreatedAt       time.Time
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
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, total_cost_user_currency_micros, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, billable_flag, priced_flag, unpriced_reason) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28)`, seed.ID, seed.ProfileID, seed.IngressRequestID, seed.ModelID, nullableTestString(seed.ResolvedTargetModelID), seed.APIFamily, nullableTestInt(seed.EndpointID), nullableTestInt(seed.ConnectionID), nullableTestInt(seed.ProxyAPIKeyID), nullableTestString(seed.ProxyAPIKeyNameSnapshot), seed.StatusCode, seed.SuccessFlag, nullableTestInt(seed.InputTokens), nullableTestInt(seed.OutputTokens), nullableTestInt(seed.TotalTokens), nullableTestInt(seed.CacheReadInputTokens), nullableTestInt(seed.CacheCreationInputTokens), nullableTestInt(seed.ReasoningTokens), nullableTestInt64(seed.TotalCostUserCurrencyMicros), seed.AttemptCount, seed.RequestPath, seed.CreatedAt, nullableTestInt(seed.ResponseTimeMS), nullableTestInt(seed.CompletionDurationMS), nullableTestInt(seed.TTFTMS), nullableTestBool(seed.BillableFlag), nullableTestBool(seed.PricedFlag), nullableTestString(seed.UnpricedReason)); err != nil {
		t.Fatalf("insert usage event %d: %v", seed.ID, err)
	}
}

func insertRequestLogSummaryRow(t *testing.T, harness *contractHarness, id int, profileID int, modelID string, apiFamily string, endpointID int, connectionID int, statusCode int, responseTimeMS int, inputTokens int, outputTokens int, totalTokens int, createdAt time.Time) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, connection_id, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, request_path, created_at, endpoint_base_url) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE, $9, $10, $11, $12, TRUE, TRUE, '/v1/chat/completions', $13, $14)`, id, profileID, modelID, apiFamily, endpointID, connectionID, statusCode, responseTimeMS, inputTokens, outputTokens, totalTokens, statusCode >= 200 && statusCode < 300, createdAt, fmt.Sprintf("https://endpoint-%d.invalid", endpointID)); err != nil {
		t.Fatalf("insert request-log summary row %d: %v", id, err)
	}
}

func insertAuditLog(t *testing.T, harness *contractHarness, seed auditLogSeed) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO audit_logs (id, profile_id, request_log_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, response_status, response_headers, response_body, is_stream, duration_ms, created_at) VALUES ($1, $2, $3, NULL, $4, NULL, NULL, 'https://audit.invalid', 'Audit endpoint', 'POST', 'https://audit.invalid/v1/chat/completions', $5, $6, $7, $8, $9, FALSE, 1234, $10)`, seed.ID, seed.ProfileID, nullableTestInt(seed.RequestLogID), seed.ModelID, seed.RequestHeaders, nullableTestString(seed.RequestBody), seed.ResponseStatus, nullableTestString(seed.ResponseHeaders), nullableTestString(seed.ResponseBody), seed.CreatedAt); err != nil {
		t.Fatalf("insert audit log %d: %v", seed.ID, err)
	}
}

func insertRuntimeState(t *testing.T, harness *contractHarness, seed runtimeStateSeed) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO routing_connection_runtime_state (profile_id, connection_id, window_started_at, window_request_count, in_flight_non_stream, in_flight_stream, consecutive_failures, last_failure_kind, last_cooldown_seconds, max_cooldown_strikes, ban_mode, banned_until_at, open_until_at, probe_eligible_logged, circuit_state, probe_available_at, live_p95_latency_ms, last_live_failure_kind, last_live_failure_at, last_live_success_at, created_at, updated_at) VALUES ($1, $2, NULL, 4, 1, 0, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULL, $12, NULL, $13, $14, $15, $16)`, seed.ProfileID, seed.ConnectionID, seed.ConsecutiveFailures, nullableTestString(seed.LastFailureKind), seed.LastCooldownSeconds, seed.MaxCooldownStrikes, seed.BanMode, nullableTestTime(seed.BannedUntilAt), nullableTestTime(seed.BlockedUntilAt), seed.ProbeEligibleLogged, seed.CircuitState, nullableTestInt(seed.LiveP95LatencyMS), nullableTestTime(seed.LastLiveFailureAt), nullableTestTime(seed.LastLiveSuccessAt), seed.CreatedAt, seed.UpdatedAt); err != nil {
		t.Fatalf("insert runtime state for connection %d: %v", seed.ConnectionID, err)
	}
}

func insertLoadbalanceEvent(t *testing.T, harness *contractHarness, seed loadbalanceEventSeed) {
	t.Helper()
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
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position) VALUES ($1, $2, $3)`, sourceModelConfigID, targetModelConfigID, position); err != nil {
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

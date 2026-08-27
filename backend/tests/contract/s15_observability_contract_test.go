package contracttest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
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
	// The audit/stats surfaces use the flat management problem envelope:
	// {code, detail, params, details, request_id}.
	if payload["code"] != want {
		t.Fatalf("expected error code %q, got %+v", want, payload)
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

func assertJSONIntFields(t *testing.T, payload map[string]any, want map[string]int) {
	t.Helper()
	for key, expected := range want {
		if got := jsonInt(t, payload[key]); got != expected {
			t.Fatalf("expected %s=%d, got %+v", key, expected, payload)
		}
	}
}

func s15SumJSONInts(t *testing.T, items []any, keys ...string) map[string]any {
	t.Helper()
	totals := map[string]any{}
	for _, raw := range items {
		item := asMap(t, raw)
		for _, key := range keys {
			totals[key] = float64(intFromJSONNumber(totals[key]) + jsonInt(t, item[key]))
		}
	}
	return totals
}

func s15TokenTrendTotals(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	for _, raw := range asMap(t, payload["token_usage_trends"])["hourly"].([]any) {
		series := asMap(t, raw)
		if series["key"] == "all" {
			return s15SumJSONInts(t, series["points"].([]any), "input_tokens", "output_tokens", "total_tokens", "cached_tokens", "reasoning_tokens")
		}
	}
	t.Fatalf("expected aggregate token trend series, got %+v", payload["token_usage_trends"])
	return nil
}

func assertS15DashboardShape(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"api_family_rows", "coverage_24h", "coverage_30d", "generated_at", "health", "metric_snapshot", "routing_health_map", "snapshot_revision", "source_watermark", "top_spending_models", "caliber", "dataset_coverage", "samples", "routing_health_caliber"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected dashboard snapshot key %q, got %+v", key, payload)
		}
	}
	if len(payload) != 14 {
		t.Fatalf("expected canonical dashboard snapshot shape, got %+v", payload)
	}
	for _, key := range []string{"window", "covers", "freshness", "metrics", "recent_requests", "strategy_family_summary"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("expected dashboard snapshot to omit legacy key %q, got %+v", key, payload)
		}
	}
}

func assertS15EmptyRoutingHealthMap(t *testing.T, payload map[string]any) {
	t.Helper()
	routingHealthMap := asMap(t, payload["routing_health_map"])
	if len(routingHealthMap["nodes"].([]any)) != 0 || len(routingHealthMap["links"].([]any)) != 0 || jsonInt(t, routingHealthMap["endpointCount"]) != 0 || jsonInt(t, routingHealthMap["modelCount"]) != 0 {
		t.Fatalf("expected empty routing health map shell, got %+v", routingHealthMap)
	}
}

func s15LabelsByID(t *testing.T, items []any, idKey string, labelKey string) map[int]string {
	t.Helper()
	labels := make(map[int]string, len(items))
	for _, raw := range items {
		item := asMap(t, raw)
		labels[jsonInt(t, item[idKey])] = item[labelKey].(string)
	}
	return labels
}

func s15JSON[T any](t *testing.T, harness *contractHarness, profileID int, method string, path string, body any, want int) T {
	t.Helper()
	response := harness.requestJSON(t, harness.client, method, path, body, modelHeader(profileID))
	assertStatus(t, response, want)
	var payload T
	decodeJSONResponse(t, response, &payload)
	return payload
}

func s15GET[T any](t *testing.T, harness *contractHarness, profileID int, path string, want int) T {
	t.Helper()
	return s15JSON[T](t, harness, profileID, http.MethodGet, path, nil, want)
}

func newS15StatsService(t *testing.T, harness *contractHarness, snapshots *statsdomain.DashboardAggregateStore) *managementstats.Service {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), harness.dsn)
	if err != nil {
		t.Fatalf("create stats pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := managementstats.NewService(config.Settings{}, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }, DashboardSnapshots: snapshots})
	if err != nil {
		t.Fatalf("create stats service: %v", err)
	}
	t.Cleanup(service.Close)
	return service
}

func s15StatsServiceGET(t *testing.T, service *managementstats.Service, profileID int, path string) map[string]any {
	t.Helper()
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Profile-Id", fmt.Sprintf("%d", profileID))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s response: %v", path, err)
	}
	return payload
}

func TestUsageSnapshot(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Snapshot Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "snapshot-model", stringPtr("Snapshot Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Snapshot Endpoint")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	proxyKeyID := insertContractProxyAPIKey(t, harness, "Snapshot Key")
	insertUsageEvent(t, harness, usageEventSeed{ID: 1, ProfileID: profileID, IngressRequestID: "snap-1", ModelID: "snapshot-model", EndpointID: &endpointID, ConnectionID: &connectionID, ProxyAPIKeyID: &proxyKeyID, ProxyAPIKeyNameSnapshot: stringPtr("Snapshot Key"), InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(38), CacheReadInputTokens: intPtr(5), CacheCreationInputTokens: intPtr(2), ReasoningTokens: intPtr(1), TotalCostUserCurrencyMicros: int64Ptr(2500), ResponseTimeMS: intPtr(800), TTFTMS: intPtr(100), CompletionDurationMS: intPtr(1100), CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 2, ProfileID: profileID, IngressRequestID: "snap-2", ModelID: "snapshot-model", EndpointID: &endpointID, ConnectionID: &connectionID, StatusCode: http.StatusInternalServerError, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), InputTokens: intPtr(15), OutputTokens: intPtr(25), TotalTokens: intPtr(40), ResponseTimeMS: intPtr(900), CreatedAt: fixedS15Now.Add(-5 * time.Minute)})

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/usage-snapshot?preset=1h", http.StatusOK)
	for _, key := range []string{"generated_at", "time_range", "currency", "overview", "request_trends", "latency_trends", "token_usage_trends", "token_type_breakdown", "cost_overview", "endpoint_statistics", "model_statistics", "proxy_api_key_statistics"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected usage snapshot preset 1h to include %q, got %+v", key, payload)
		}
	}
	if _, ok := payload["service_health"]; ok {
		t.Fatalf("expected usage snapshot preset 1h to omit top-level service_health, got %+v", payload)
	}
	overview := asMap(t, payload["overview"])
	assertJSONIntFields(t, overview, map[string]int{"total_requests": 2, "success_requests": 1, "failed_requests": 1, "total_tokens": 78})
	assertJSONIntFields(t, s15TokenTrendTotals(t, payload), map[string]int{"input_tokens": 25, "output_tokens": 45, "total_tokens": 78, "cached_tokens": 7, "reasoning_tokens": 1})
	assertJSONIntFields(t, s15SumJSONInts(t, asMap(t, payload["token_type_breakdown"])["hourly"].([]any), "input_tokens", "output_tokens", "cached_tokens", "reasoning_tokens"), map[string]int{"input_tokens": 25, "output_tokens": 45, "cached_tokens": 7, "reasoning_tokens": 1})
	assertJSONIntFields(t, asMap(t, payload["cost_overview"]), map[string]int{"total_cost_micros": 2500, "priced_request_count": 1, "unpriced_request_count": 0})
	if timeRange := asMap(t, payload["time_range"]); timeRange["preset"] != "1h" || timeRange["start_at"] != "2026-04-19T11:00:00Z" || timeRange["end_at"] != "2026-04-19T12:00:00Z" {
		t.Fatalf("unexpected usage snapshot time range: %+v", timeRange)
	}
	if currency := asMap(t, payload["currency"]); currency["code"] != "USD" || currency["symbol"] != "$" {
		t.Fatalf("unexpected usage snapshot currency: %+v", currency)
	}
	endpointRow := asMap(t, payload["endpoint_statistics"].([]any)[0])
	modelRow := asMap(t, payload["model_statistics"].([]any)[0])
	assertJSONIntFields(t, endpointRow, map[string]int{"request_count": 2, "total_tokens": 78, "total_cost_micros": 2500, "p50_ttft_ms": 100, "p95_ttft_ms": 100})
	assertJSONIntFields(t, modelRow, map[string]int{"request_count": 2, "success_count": 1, "failed_count": 1, "input_tokens": 25, "output_tokens": 45, "total_tokens": 78, "cached_tokens": 7, "reasoning_tokens": 1, "priced_request_count": 1, "unpriced_request_count": 0, "total_cost_micros": 2500, "p50_ttft_ms": 100})
	if endpointRow["endpoint_label"] != "Snapshot Endpoint" || modelRow["model_label"] != "Snapshot Model" {
		t.Fatalf("unexpected usage snapshot labels: endpoint=%+v model=%+v", endpointRow, modelRow)
	}
	if math.Abs(endpointRow["avg_output_rate_tps"].(float64)-20) > 0.001 || math.Abs(modelRow["avg_output_rate_tps"].(float64)-20) > 0.001 {
		t.Fatalf("expected usage snapshot output-rate semantics, got endpoint=%+v model=%+v", endpointRow, modelRow)
	}
	proxyKeyLabels := map[string]bool{}
	for _, raw := range payload["proxy_api_key_statistics"].([]any) {
		proxyKeyLabels[asMap(t, raw)["proxy_api_key_label"].(string)] = true
	}
	if !proxyKeyLabels["No proxy API key"] || !proxyKeyLabels["Snapshot Key"] {
		t.Fatalf("expected snapshot proxy key statistics for stored and missing keys, got %+v", payload["proxy_api_key_statistics"])
	}
	directPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/usage-snapshot?preset=7d", http.StatusOK)
	if asMap(t, directPayload["time_range"])["preset"] != "7d" || directPayload["overview"] == nil || directPayload["model_statistics"] == nil || directPayload["service_health"] != nil {
		t.Fatalf("expected direct usage snapshot contract for 7d preset, got %+v", directPayload)
	}
}

func TestEndpointModelStatistics(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Endpoint Model Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "endpoint-model", stringPtr("Endpoint Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Endpoint Model Stats")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertUsageEvent(t, harness, usageEventSeed{ID: 10, ProfileID: profileID, IngressRequestID: "endpoint-1", ModelID: "endpoint-model", EndpointID: &endpointID, ConnectionID: &connectionID, OutputTokens: intPtr(20), TotalTokens: intPtr(20), TTFTMS: intPtr(100), CompletionDurationMS: intPtr(1000), CreatedAt: fixedS15Now.Add(-30 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 11, ProfileID: profileID, IngressRequestID: "endpoint-2", ModelID: "endpoint-model", EndpointID: &endpointID, ConnectionID: &connectionID, PricedFlag: boolPtr(false), UnpricedReason: stringPtr("PRICING_DISABLED"), OutputTokens: intPtr(30), TotalTokens: intPtr(30), TTFTMS: intPtr(400), CompletionDurationMS: intPtr(1500), CreatedAt: fixedS15Now.Add(-20 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 12, ProfileID: profileID, IngressRequestID: "endpoint-3", ModelID: "endpoint-model", EndpointID: &endpointID, ConnectionID: &connectionID, StatusCode: http.StatusInternalServerError, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), TotalTokens: intPtr(0), CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET resolved_target_model_id=model_id, final_attempt_number=1 WHERE id BETWEEN 10 AND 12`); err != nil {
		t.Fatalf("set endpoint final_execution identity: %v", err)
	}

	payload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/stats/endpoints/%d/models?preset=all&scope=final_execution", endpointID), http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one endpoint-model statistics row, got %+v", payload)
	}
	row := asMap(t, items[0])
	if row["model_id"] != "endpoint-model" {
		t.Fatalf("unexpected endpoint-model statistics payload: %+v", row)
	}
	assertJSONIntFields(t, row, map[string]int{
		"request_count": 3,
		"success_count": 2,
		"failed_count":  1,
	})
}

func TestStatsSummary(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	// /api/stats/summary reports the request-level granularity it advertises, so
	// it reads finalized usage events rather than per-attempt request_logs rows.
	// Seeding request_logs here would count attempts and reintroduce the basis
	// mismatch this endpoint was changed to remove.
	endpointA, connectionA := 12, 41
	endpointB, connectionB := 13, 42
	insertUsageEvent(t, harness, usageEventSeed{ID: 100, ProfileID: profileID, IngressRequestID: "summary-1", ModelID: "summary-model-a", APIFamily: "openai", EndpointID: &endpointA, ConnectionID: &connectionA, StatusCode: 200, SuccessFlag: true, ResponseTimeMS: intPtr(100), InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(30), CreatedAt: fixedS15Now.Add(-55 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 101, ProfileID: profileID, IngressRequestID: "summary-2", ModelID: "summary-model-a", APIFamily: "openai", EndpointID: &endpointA, ConnectionID: &connectionA, StatusCode: 500, SuccessFlag: false, ResponseTimeMS: intPtr(300), InputTokens: intPtr(5), OutputTokens: intPtr(10), TotalTokens: intPtr(15), CreatedAt: fixedS15Now.Add(-50 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 102, ProfileID: profileID, IngressRequestID: "summary-3", ModelID: "summary-model-b", APIFamily: "anthropic", EndpointID: &endpointB, ConnectionID: &connectionB, StatusCode: 200, SuccessFlag: true, ResponseTimeMS: intPtr(200), InputTokens: intPtr(8), OutputTokens: intPtr(12), TotalTokens: intPtr(20), CreatedAt: fixedS15Now.Add(-45 * time.Minute)})

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/summary?group_by=api_family", http.StatusOK)
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
	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/dashboard", http.StatusOK)
	assertS15DashboardShape(t, payload)
	s15GET[map[string]any](t, harness, profileID, "/api/stats/dashboard?window=24h", http.StatusUnprocessableEntity)
}

func TestManagementDashboardStatsSnapshotSections(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Dashboard Snapshot Strategy")
	modelInsertModel(t, harness, profileID, nil, "openai", "dashboard-model", stringPtr("Dashboard Model"), "native", &strategyID, true)
	insertUsageEvent(t, harness, usageEventSeed{ID: 30, ProfileID: profileID, IngressRequestID: "dashboard-spend-1", ModelID: "dashboard-model", InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(30), TotalCostUserCurrencyMicros: int64Ptr(2500), ResponseTimeMS: intPtr(100), CreatedAt: fixedS15Now.Add(-55 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 31, ProfileID: profileID, IngressRequestID: "dashboard-error-1", ModelID: "dashboard-model", StatusCode: http.StatusInternalServerError, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), InputTokens: intPtr(5), OutputTokens: intPtr(10), TotalTokens: intPtr(15), ResponseTimeMS: intPtr(300), CreatedAt: fixedS15Now.Add(-50 * time.Minute)})
	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/dashboard", http.StatusOK)
	assertS15DashboardShape(t, payload)
	for _, key := range []string{"coverage_24h", "coverage_30d"} {
		coverage := asMap(t, payload[key])
		if coverage["from"] == nil || coverage["to"] == nil {
			t.Fatalf("expected %s coverage bounds, got %+v", key, coverage)
		}
	}
	assertJSONIntFields(t, asMap(t, payload["metric_snapshot"]), map[string]int{"total_requests": 2, "total_models": 1, "active_models": 1, "avg_latency": 200, "p95_latency": 290, "success_rate": 50, "error_rate": 50, "priced_request_count": 1, "unpriced_request_count": 0, "total_cost": 2500})
	row := asMap(t, payload["api_family_rows"].([]any)[0])
	topModel := asMap(t, payload["top_spending_models"].([]any)[0])
	assertJSONIntFields(t, row, map[string]int{"total_requests": 2, "success_count": 1, "error_count": 1, "avg_response_time_ms": 200, "total_tokens": 45})
	if row["key"] != "openai" || topModel["model_id"] != "unattributed" || jsonInt(t, topModel["known_cost_micros"]) != 2500 {
		t.Fatalf("unexpected dashboard snapshot sections: rows=%+v top=%+v", row, topModel)
	}
	assertS15EmptyRoutingHealthMap(t, payload)
}

func TestManagementGlobalLogRetentionJobStatusContract(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	jobID := createS15LogRetentionJob(t, harness, "request_logs", map[string]any{"delete_all": true}, "status")

	payload := s15GET[map[string]any](t, harness, profileID, "/api/management/jobs/"+jobID+"?scope=global&type=log_retention", http.StatusOK)
	job := asMap(t, payload["job"])
	if job["id"] != jobID || job["type"] != "log_retention" || job["progress"] == nil || job["dataset"] != "request_logs" || job["mode"] != "delete_all" {
		t.Fatalf("expected global log-retention job status contract, got %+v", payload)
	}
}

func TestManagementDashboardHealthReportsSnapshotFreshness(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/dashboard", http.StatusOK)
	assertS15DashboardShape(t, payload)
	health := asMap(t, payload["health"])
	if health["stale"] != false {
		t.Fatalf("expected dashboard health to describe the aggregate snapshot freshness, got %+v", payload)
	}
	assertJSONIntFields(t, health, map[string]int{"lag_seconds": 0, "stale_after_seconds": 120})
}

func TestManagementDashboardStatsCacheFreshnessModes(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertUsageEvent(t, harness, usageEventSeed{ID: 33, ProfileID: profileID, IngressRequestID: "dashboard-cache-rebuild", ModelID: "dashboard-cache-model", CreatedAt: fixedS15Now.Add(-30 * time.Second)})
	snapshots := statsdomain.NewDashboardAggregateStore()
	service := newS15StatsService(t, harness, snapshots)

	snapshots.StoreProfile(statsdomain.DashboardAggregateSnapshot{ProfileID: profileID, GeneratedAt: fixedS15Now.Add(-statsdomain.DashboardStatsStaleAfter), StatsSummary24H: statsdomain.StatsSummaryResponse{TotalRequests: 7}})
	threshold := s15StatsServiceGET(t, service, profileID, "/stats/dashboard")
	if jsonInt(t, asMap(t, threshold["metric_snapshot"])["total_requests"]) != 7 {
		t.Fatalf("expected cached threshold snapshot to remain reusable, got %+v", threshold)
	}
	if health := asMap(t, threshold["health"]); health["stale"] != false || jsonInt(t, health["lag_seconds"]) != int(statsdomain.DashboardStatsStaleAfter/time.Second) {
		t.Fatalf("expected threshold dashboard health to stay fresh, got %+v", health)
	}

	snapshots.StoreProfile(statsdomain.DashboardAggregateSnapshot{ProfileID: profileID, GeneratedAt: fixedS15Now.Add(-statsdomain.DashboardStatsStaleAfter - time.Second), StatsSummary24H: statsdomain.StatsSummaryResponse{TotalRequests: 99}})
	rebuilt := s15StatsServiceGET(t, service, profileID, "/stats/dashboard")
	if jsonInt(t, asMap(t, rebuilt["metric_snapshot"])["total_requests"]) != 1 || jsonInt(t, asMap(t, rebuilt["health"])["lag_seconds"]) != 0 {
		t.Fatalf("expected stale cached dashboard snapshot to rebuild from usage events, got %+v", rebuilt)
	}

	snapshots.StoreProfile(statsdomain.DashboardAggregateSnapshot{ProfileID: 101, GeneratedAt: fixedS15Now})
	snapshots.StoreProfile(statsdomain.DashboardAggregateSnapshot{ProfileID: 202, GeneratedAt: fixedS15Now})
	service.InvalidateDashboardSnapshot(101)
	if _, ok := snapshots.LoadProfile(101); ok {
		t.Fatal("expected profile-specific dashboard snapshot invalidation to evict profile 101")
	}
	if _, ok := snapshots.LoadProfile(202); !ok {
		t.Fatal("expected profile-specific dashboard snapshot invalidation to preserve profile 202")
	}
	service.InvalidateAllDashboardSnapshots()
	if _, ok := snapshots.LoadProfile(202); ok {
		t.Fatal("expected global dashboard snapshot invalidation to evict remaining profiles")
	}
}

func TestModelMetrics(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 200, profileID, "metrics-model", "openai", 12, 51, 200, 100, 0, 0, 0, fixedS15Now.Add(-2*time.Hour))
	insertRequestLogSummaryRow(t, harness, 201, profileID, "metrics-model", "openai", 12, 51, 500, 300, 0, 0, 0, fixedS15Now.Add(-90*time.Minute))
	insertUsageEvent(t, harness, usageEventSeed{ID: 20, ProfileID: profileID, IngressRequestID: "metrics-1", ModelID: "metrics-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalCostUserCurrencyMicros: int64Ptr(2500), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-24 * time.Hour)})

	payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/stats/models/metrics", map[string]any{"model_ids": []string{"metrics-model"}, "summary_window_hours": 24, "spending_preset": "last_30_days"}, http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one model metrics item, got %+v", payload)
	}
	item := asMap(t, items[0])
	ingress := asMap(t, item["ingress"])
	if item["model_id"] != "metrics-model" || jsonInt(t, ingress["request_count"]) != 1 || jsonInt(t, ingress["known_cost_micros"]) != 2500 {
		t.Fatalf("unexpected model metrics payload: %+v", item)
	}
}

func TestConnectionSuccessRates(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 300, profileID, "connection-model", "openai", 12, 61, 200, 100, 0, 0, 0, fixedS15Now.Add(-40*time.Minute))
	insertRequestLogSummaryRow(t, harness, 301, profileID, "connection-model", "openai", 12, 61, 500, 100, 0, 0, 0, fixedS15Now.Add(-35*time.Minute))
	insertRequestLogSummaryRow(t, harness, 302, profileID, "connection-model", "openai", 12, 62, 200, 100, 0, 0, 0, fixedS15Now.Add(-30*time.Minute))
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET row_kind='upstream', url_scrub_provenance='runtime_scrubbed', resolved_target_model_id=model_id, ingress_request_id='connection-'||id, attempt_number=1, attempt_result=CASE WHEN legacy_status_code BETWEEN 200 AND 299 THEN 'completed' ELSE 'http_error' END, attempt_duration_ms=legacy_duration_ms, upstream_status_code=legacy_status_code, legacy_status_code=NULL, legacy_duration_ms=NULL WHERE id BETWEEN 300 AND 302`); err != nil {
		t.Fatalf("set connection route_attempt rows: %v", err)
	}

	payload := s15GET[[]map[string]any](t, harness, profileID, "/api/stats/connection-success-rates", http.StatusOK)
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
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET row_kind='upstream', url_scrub_provenance='runtime_scrubbed', resolved_target_model_id='throughput-model', ingress_request_id='throughput-'||id, attempt_number=1, attempt_result='completed', attempt_duration_ms=legacy_duration_ms, upstream_status_code=legacy_status_code, legacy_status_code=NULL, legacy_duration_ms=NULL WHERE id BETWEEN 400 AND 402`); err != nil {
		t.Fatalf("set route_attempt throughput rows: %v", err)
	}

	payload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/stats/throughput?scope=route_attempt&from_time=%s&to_time=%s", fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339)), http.StatusOK)
	if jsonInt(t, payload["total_requests"]) != 3 || len(payload["buckets"].([]any)) != 2 {
		t.Fatalf("unexpected throughput payload: %+v", payload)
	}
	if payload["average_rpm"].(float64) <= 0 || payload["peak_rpm"].(float64) <= 0 || payload["current_rpm"].(float64) <= 0 {
		t.Fatalf("expected positive throughput metrics, got %+v", payload)
	}
}

func TestEndpointLabelSnapshotUsageSnapshotSurvivesEndpointRenameAndDelete(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Endpoint Snapshot Usage Strategy")
	modelInsertModel(t, harness, profileID, nil, "openai", "snapshot-usage-model", stringPtr("Snapshot Usage Model"), "native", &strategyID, true)
	endpointRenamed := modelInsertEndpoint(t, harness, profileID, "Current Endpoint Before Rename")
	endpointDeleted := modelInsertEndpoint(t, harness, profileID, "Current Endpoint Before Delete")
	insertUsageEvent(t, harness, usageEventSeed{ID: 25, ProfileID: profileID, IngressRequestID: "snapshot-label-usage-renamed", ModelID: "snapshot-usage-model", APIFamily: "openai", EndpointID: &endpointRenamed, EndpointLabelSnapshot: stringPtr("Historical Renamed Label"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(11), TotalCostUserCurrencyMicros: int64Ptr(1100), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-20 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 26, ProfileID: profileID, IngressRequestID: "snapshot-label-usage-deleted", ModelID: "snapshot-usage-model", APIFamily: "openai", EndpointID: &endpointDeleted, EndpointLabelSnapshot: stringPtr("Historical Deleted Label"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(13), TotalCostUserCurrencyMicros: int64Ptr(1300), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	if _, err := harness.conn.Exec(context.Background(), `UPDATE endpoints SET name = 'Renamed Current Label' WHERE id = $1`, endpointRenamed); err != nil {
		t.Fatalf("rename endpoint %d: %v", endpointRenamed, err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM endpoints WHERE id = $1`, endpointDeleted); err != nil {
		t.Fatalf("delete endpoint %d: %v", endpointDeleted, err)
	}

	refreshS15ActualCoverage(t, harness, "usage_request_events")
	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/usage-snapshot?preset=all", http.StatusOK)
	labels := s15LabelsByID(t, payload["endpoint_statistics"].([]any), "endpoint_id", "endpoint_label")
	if labels[endpointRenamed] != "Historical Renamed Label" || labels[endpointDeleted] != "Historical Deleted Label" {
		t.Fatalf("expected usage snapshot endpoint labels from stored snapshots after rename/delete, got %+v", labels)
	}
}

func TestEndpointLabelSnapshotSpendingSurvivesEndpointRenameAndDelete(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Endpoint Snapshot Spending Strategy")
	modelInsertModel(t, harness, profileID, nil, "openai", "snapshot-spending-model", stringPtr("Snapshot Spending Model"), "native", &strategyID, true)
	endpointRenamed := modelInsertEndpoint(t, harness, profileID, "Spend Current Before Rename")
	endpointDeleted := modelInsertEndpoint(t, harness, profileID, "Spend Current Before Delete")
	insertUsageEvent(t, harness, usageEventSeed{ID: 27, ProfileID: profileID, IngressRequestID: "snapshot-label-spend-renamed", ModelID: "snapshot-spending-model", APIFamily: "openai", EndpointID: &endpointRenamed, EndpointLabelSnapshot: stringPtr("Historical Spend Renamed"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(10), TotalCostUserCurrencyMicros: int64Ptr(5000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-20 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 28, ProfileID: profileID, IngressRequestID: "snapshot-label-spend-deleted", ModelID: "snapshot-spending-model", APIFamily: "openai", EndpointID: &endpointDeleted, EndpointLabelSnapshot: stringPtr("Historical Spend Deleted"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(12), TotalCostUserCurrencyMicros: int64Ptr(4000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-10 * time.Minute)})
	if _, err := harness.conn.Exec(context.Background(), `UPDATE endpoints SET name = 'Spend Current After Rename' WHERE id = $1`, endpointRenamed); err != nil {
		t.Fatalf("rename endpoint %d: %v", endpointRenamed, err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM endpoints WHERE id = $1`, endpointDeleted); err != nil {
		t.Fatalf("delete endpoint %d: %v", endpointDeleted, err)
	}

	refreshS15ActualCoverage(t, harness, "usage_request_events")
	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/spending?scope=final_execution&preset=all&group_by=endpoint&limit=50&offset=0&top_n=5", http.StatusOK)
	groupLabels := map[string]bool{}
	for _, raw := range payload["groups"].([]any) {
		groupLabels[asMap(t, raw)["key"].(string)] = true
	}
	if !groupLabels["Historical Spend Renamed"] || !groupLabels["Historical Spend Deleted"] || groupLabels["Spend Current After Rename"] || groupLabels[fmt.Sprintf("Endpoint %d", endpointDeleted)] {
		t.Fatalf("expected spending endpoint groups from stored snapshots after rename/delete, got %+v", payload["groups"])
	}
	topLabels := s15LabelsByID(t, payload["top_spending_endpoints"].([]any), "endpoint_id", "endpoint_label")
	if topLabels[endpointRenamed] != "Historical Spend Renamed" || topLabels[endpointDeleted] != "Historical Spend Deleted" {
		t.Fatalf("expected top spending endpoint labels from stored snapshots, got %+v", topLabels)
	}
}

func TestEndpointLabelSnapshotTopEndpointDuplicateLabelsStayDistinct(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Endpoint Snapshot Duplicate Strategy")
	modelInsertModel(t, harness, profileID, nil, "openai", "snapshot-duplicate-model", stringPtr("Snapshot Duplicate Model"), "native", &strategyID, true)
	endpointA := modelInsertEndpoint(t, harness, profileID, "Duplicate Current A")
	endpointB := modelInsertEndpoint(t, harness, profileID, "Duplicate Current B")
	insertUsageEvent(t, harness, usageEventSeed{ID: 29, ProfileID: profileID, IngressRequestID: "snapshot-label-duplicate-a", ModelID: "snapshot-duplicate-model", APIFamily: "openai", EndpointID: &endpointA, EndpointLabelSnapshot: stringPtr("Shared Historical Label"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(10), TotalCostUserCurrencyMicros: int64Ptr(3000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-20 * time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 32, ProfileID: profileID, IngressRequestID: "snapshot-label-duplicate-b", ModelID: "snapshot-duplicate-model", APIFamily: "openai", EndpointID: &endpointB, EndpointLabelSnapshot: stringPtr("Shared Historical Label"), StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(12), TotalCostUserCurrencyMicros: int64Ptr(2000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-10 * time.Minute)})

	refreshS15ActualCoverage(t, harness, "usage_request_events")
	usagePayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/usage-snapshot?preset=all", http.StatusOK)
	statsByID := s15LabelsByID(t, usagePayload["endpoint_statistics"].([]any), "endpoint_id", "endpoint_label")
	if len(statsByID) != 2 || statsByID[endpointA] != "Shared Historical Label" || statsByID[endpointB] != "Shared Historical Label" {
		t.Fatalf("expected duplicate snapshot labels to remain distinct by endpoint id in usage snapshot, got %+v", usagePayload["endpoint_statistics"])
	}

	spendingPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/spending?scope=final_execution&preset=all&group_by=endpoint&limit=50&offset=0&top_n=5", http.StatusOK)
	topLabels := s15LabelsByID(t, spendingPayload["top_spending_endpoints"].([]any), "endpoint_id", "endpoint_label")
	if len(topLabels) != 2 || topLabels[endpointA] != "Shared Historical Label" || topLabels[endpointB] != "Shared Historical Label" {
		t.Fatalf("expected duplicate snapshot labels to remain distinct by endpoint id in top endpoints, got %+v", spendingPayload["top_spending_endpoints"])
	}
}

func TestSpending(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Spend Strategy")
	modelInsertModel(t, harness, profileID, nil, "openai", "spend-model", stringPtr("Spend Model"), "native", &strategyID, true)
	endpointA := modelInsertEndpoint(t, harness, profileID, "Spend Endpoint A")
	endpointB := modelInsertEndpoint(t, harness, profileID, "Spend Endpoint B")
	insertUsageEvent(t, harness, usageEventSeed{ID: 30, ProfileID: profileID, IngressRequestID: "spend-1", ModelID: "spend-model", APIFamily: "openai", EndpointID: &endpointA, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(38), CacheReadInputTokens: intPtr(5), CacheCreationInputTokens: intPtr(2), ReasoningTokens: intPtr(1), TotalCostUserCurrencyMicros: int64Ptr(5000), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-4 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 31, ProfileID: profileID, IngressRequestID: "spend-2", ModelID: "spend-model", APIFamily: "openai", EndpointID: &endpointB, StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(false), UnpricedReason: stringPtr("PRICING_DISABLED"), InputTokens: intPtr(3), OutputTokens: intPtr(4), TotalTokens: intPtr(12), CacheReadInputTokens: intPtr(2), CacheCreationInputTokens: intPtr(1), ReasoningTokens: intPtr(2), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: fixedS15Now.Add(-3 * time.Hour)})
	refreshS15ActualCoverage(t, harness, "usage_request_events")

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/spending?scope=ingress&preset=all&group_by=ingress_model_endpoint&limit=50&offset=0", http.StatusOK)
	summary := asMap(t, payload["summary"])
	if jsonInt(t, summary["successful_request_count"]) != 2 || jsonInt(t, summary["priced_request_count"]) != 1 || jsonInt(t, summary["unpriced_request_count"]) != 1 || jsonInt(t, summary["known_cost_micros"]) != 5000 || jsonInt(t, payload["groups_total"]) != 2 {
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
	if unpricedGroup == nil || unpricedGroup["known_cost_micros"] != nil || jsonInt(t, unpricedGroup["priced_requests"]) != 0 || jsonInt(t, unpricedGroup["unpriced_requests"]) != 1 {
		t.Fatalf("expected unpriced spend group to stay zero-cost while preserving request counts, got %+v", groupsByKey)
	}
	topSpendingModels := payload["top_spending_models"].([]any)
	if len(topSpendingModels) != 1 {
		t.Fatalf("expected one top spending model row, got %+v", payload["top_spending_models"])
	}
	topSpendingModel := asMap(t, topSpendingModels[0])
	if topSpendingModel["model_id"] != "spend-model" || topSpendingModel["model_label"] != "Spend Model" || jsonInt(t, topSpendingModel["known_cost_micros"]) != 5000 {
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

	refreshS15ActualCoverage(t, harness, "usage_request_events")
	usageSnapshotPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/usage-snapshot?preset=all", http.StatusOK)
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

	spendingPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/spending?preset=all&group_by=none&limit=50&offset=0", http.StatusOK)
	summary := asMap(t, spendingPayload["summary"])
	if jsonInt(t, summary["successful_request_count"]) != 1 || jsonInt(t, summary["priced_request_count"]) != 0 || jsonInt(t, summary["unpriced_request_count"]) != 1 || summary["known_cost_micros"] != nil {
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
	refreshS15ActualCoverage(t, harness, "request_logs", "usage_request_events")

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

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?limit=20", http.StatusOK)
	if jsonInt(t, listPayload["total"]) != 2 {
		t.Fatalf("expected profile-scoped request list over partitions, got %+v", listPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/stats/requests/%d", requestID), http.StatusOK)
	summary := asMap(t, detailPayload["summary"])
	if summary["ingress_model_id"] != "partition-new" || jsonInt(t, summary["legacy_status_code"]) != 500 || jsonInt(t, summary["legacy_duration_ms"]) != 222 {
		t.Fatalf("expected newest duplicate request-log id for Default profile, got %+v", detailPayload)
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

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/9201", http.StatusOK)
	if detailPayload["request_log_id"] != "9200" || detailPayload["ingress_request_id"] != "weak-request-9200" || detailPayload["request_log_created_at"] == nil || detailPayload["request_log_missing"] != true {
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

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected profile-scoped audit partition list, got %+v", listPayload)
	}
	item := asMap(t, items[0])
	if item["request_log_id"] != "9300" || item["ingress_request_id"] != "weak-request-9300" || item["request_log_created_at"] == nil || item["request_log_missing"] != false {
		t.Fatalf("expected audit list weak request fields, got %+v", item)
	}
}

func TestAuditListAndDetailPreserveBigIntRequestLogIDAsDecimalString(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	requestLogID := int(9007199254740997)
	requestCreatedAt := fixedS15Now.Add(-20 * time.Minute)
	auditCreatedAt := fixedS15Now.Add(-10 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, requestLogID, profileID, "audit-bigint", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	insertAuditLog(t, harness, auditLogSeed{
		ID:                          9401,
		ProfileID:                   profileID,
		RequestLogID:                intPtr(requestLogID),
		RequestLogCreatedAt:         timePtr(requestCreatedAt),
		IngressRequestID:            stringPtr("audit-bigint-ingress"),
		ModelID:                     "audit-bigint",
		RequestHeaders:              `{}`,
		ResponseStatus:              200,
		AuditEnabledAtRequest:       true,
		AuditCaptureBodiesAtRequest: false,
		CreatedAt:                   auditCreatedAt,
	})
	want := "9007199254740997"

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&request_log_id="+want+"&limit=20", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 1 || asMap(t, items[0])["request_log_id"] != want {
		t.Fatalf("expected audit list request_log_id %q as a decimal string, got %+v", want, listPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/9401", http.StatusOK)
	if detailPayload["request_log_id"] != want {
		t.Fatalf("expected audit detail request_log_id %q as a decimal string, got %T(%v)", want, detailPayload["request_log_id"], detailPayload["request_log_id"])
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

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&request_log_id=700&limit=20", http.StatusConflict)
	if listPayload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit list, got %+v", listPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/800", http.StatusConflict)
	if detailPayload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit detail, got %+v", detailPayload)
	}

	visibleListPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
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

	metadataDetailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/802", http.StatusOK)
	if metadataDetailPayload["request_body"] != nil || metadataDetailPayload["response_body"] != nil || metadataDetailPayload["request_body_stored"] != false || metadataDetailPayload["response_body_stored"] != false || metadataDetailPayload["audit_capture_bodies_at_request"] != false {
		t.Fatalf("expected metadata-only audit detail to be a first-class nil-body state, got %+v", metadataDetailPayload)
	}

	enabledDetailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/801", http.StatusOK)
	if enabledDetailPayload["request_body_base64"] == nil || enabledDetailPayload["response_body_base64"] == nil || enabledDetailPayload["request_body_stored"] != true || enabledDetailPayload["response_body_stored"] != true || enabledDetailPayload["audit_capture_bodies_at_request"] != true {
		t.Fatalf("expected audit detail to return full captured bodies for enabled requests, got %+v", enabledDetailPayload)
	}

	streamDetailPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs/803", http.StatusOK)
	if streamDetailPayload["is_stream"] != true || streamDetailPayload["response_body_stored"] != true || streamDetailPayload["audit_capture_bodies_at_request"] != true || streamDetailPayload["response_body_base64"] == nil {
		t.Fatalf("expected streaming audit detail to return raw stored SSE body, got %+v", streamDetailPayload)
	}
	streamResponseBody := mustDecodeBase64String(t, streamDetailPayload["response_body_base64"])
	if streamResponseBody != streamBody {
		t.Fatalf("expected streaming audit detail to return raw stored SSE body, got %q", streamResponseBody)
	}
	if !strings.Contains(streamResponseBody, "event: response.created") || !strings.Contains(streamResponseBody, "event: response.completed") || strings.HasPrefix(strings.TrimSpace(streamResponseBody), "{") {
		t.Fatalf("expected streaming audit detail response body to preserve raw SSE framing, got %q", streamResponseBody)
	}
}

func TestAuditLogRetentionJob(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertAuditLog(t, harness, auditLogSeed{ID: 900, ProfileID: profileID, ModelID: "audit-retention", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertAuditLog(t, harness, auditLogSeed{ID: 901, ProfileID: profileID, ModelID: "audit-retention", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-30 * time.Minute)})
	refreshS15ActualCoverage(t, harness, "audit_logs")

	// A manual audit purge job queues without inline deletion; the scheduled
	// policy flow is covered by the v2 planner tests.
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
	profileID := modelLoadDefaultProfileID(t, harness)
	// First acceptance creates the durable job; a lost-response retry with the
	// same operation id and request hash replays the recorded job (SPEC §6.4).
	operationID := "s15-idem-" + fmt.Sprintf("%d", time.Now().UnixNano())
	preflight := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/maintenance/log-retention/preflights", map[string]any{
		"kind":                 "manual_cleanup",
		"operation_id":         operationID,
		"preflight_attempt_id": "s15-idem-attempt",
		"dataset":              "audit_logs",
		"selection":            map[string]any{"mode": "delete_all"},
	}, map[string]string{}, http.StatusCreated)
	token := preflight["preflight_token"].(string)
	body := map[string]any{"operation_id": operationID, "preflight_token": token, "confirmation": map[string]any{"keyword": "DELETE"}}
	first := harness.requestJSON(t, harness.client, http.MethodPost, "/api/maintenance/log-retention/jobs", body, modelHeader(profileID))
	assertStatus(t, first, http.StatusAccepted)
	var firstPayload struct {
		Job map[string]any `json:"job"`
	}
	decodeJSONResponse(t, first, &firstPayload)
	firstID, _ := firstPayload.Job["id"].(string)
	second := harness.requestJSON(t, harness.client, http.MethodPost, "/api/maintenance/log-retention/jobs", body, modelHeader(profileID))
	assertStatus(t, second, http.StatusOK)
	var secondPayload struct {
		Replayed bool           `json:"replayed"`
		Job      map[string]any `json:"job"`
	}
	decodeJSONResponse(t, second, &secondPayload)
	secondID, _ := secondPayload.Job["id"].(string)
	if !secondPayload.Replayed || firstID == "" || secondID != firstID {
		t.Fatalf("expected idempotent replay to return the same job, got replayed=%v first=%s second=%s", secondPayload.Replayed, firstID, secondID)
	}
}

func TestRequestLogDeletionDoesNotWidenAuditVisibility(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertRequestLogSummaryRow(t, harness, 710, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-12*time.Minute))
	insertAuditLog(t, harness, auditLogSeed{ID: 810, ProfileID: profileID, RequestLogID: intPtr(710), ModelID: "audit-model", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: false, CreatedAt: fixedS15Now.Add(-11 * time.Minute)})
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 711, profileID, "audit-model", "openai", 12, 91, 200, 100, 0, 0, 0, fixedS15Now.Add(-10*time.Minute), true)
	insertAuditLog(t, harness, auditLogSeed{ID: 811, ProfileID: profileID, RequestLogID: intPtr(711), ModelID: "audit-model", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-9 * time.Minute)})
	refreshS15ActualCoverage(t, harness, "request_logs", "audit_logs")

	beforeDeletePayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
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

	afterJobPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=20", http.StatusOK)
	if len(afterJobPayload["items"].([]any)) != 1 || jsonInt(t, asMap(t, afterJobPayload["items"].([]any)[0])["id"]) != 811 {
		t.Fatalf("expected queued request-log retention job not to widen audit visibility inline, got %+v", afterJobPayload)
	}
}

func TestManagementAuditListRequiresWindow(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	payload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?limit=20", http.StatusBadRequest)
	assertErrorCode(t, payload, "audit_window_required")
}

func TestManagementAuditListRejectsOversizedWindow(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	from := fixedS15Now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	to := fixedS15Now.Format(time.RFC3339)
	payload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?from="+from+"&to="+to, http.StatusBadRequest)
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
		payload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&"+query, http.StatusBadRequest)
		assertErrorCode(t, payload, "audit_filter_unsupported")
	}
}

func TestManagementAuditCursorIntegrity(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertAuditLog(t, harness, auditLogSeed{ID: 820, ProfileID: profileID, ModelID: "audit-cursor", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-3 * time.Minute)})
	insertAuditLog(t, harness, auditLogSeed{ID: 821, ProfileID: profileID, ModelID: "audit-cursor", RequestHeaders: `{}`, ResponseStatus: 200, AuditEnabledAtRequest: true, CreatedAt: fixedS15Now.Add(-2 * time.Minute)})

	firstPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1", http.StatusOK)
	if firstPayload["has_more"] != true || firstPayload["next_cursor"] == nil {
		t.Fatalf("expected first audit cursor page to include next_cursor, got %+v", firstPayload)
	}

	cursor := firstPayload["next_cursor"].(string)
	secondPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1&cursor="+cursor, http.StatusOK)
	if jsonInt(t, asMap(t, secondPayload["items"].([]any)[0])["id"]) != 820 {
		t.Fatalf("expected keyset cursor page to continue after newest row, got %+v", secondPayload)
	}

	// Tamper with the first signature character, not the last one: a 32-byte
	// HMAC encodes to 43 base64 characters whose final character carries only
	// four significant bits, so replacing it decodes to the same signature
	// roughly one run in sixteen and the forgery would be accepted.
	signatureStart := strings.LastIndex(cursor, ".") + 1
	replacement := byte('x')
	if cursor[signatureStart] == replacement {
		replacement = 'y'
	}
	tamperedCursor := cursor[:signatureStart] + string(replacement) + cursor[signatureStart+1:]
	tamperedPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=1&cursor="+tamperedCursor, http.StatusBadRequest)
	assertErrorCode(t, tamperedPayload, "audit_cursor_invalid")
}

func TestLoadbalanceCurrentState(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Loadbalance Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-model", stringPtr("Loadbalance Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Endpoint")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 2, LastFailureKind: stringPtr("transient_http"), LastCooldownSeconds: 60.0, BanMode: "off", BlockedUntilAt: timePtr(fixedS15Now.Add(30 * time.Minute)), LastSuccessResponseHeadersLatencyMS: intPtr(540), CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})

	// Global view: no internal row-id input; the configured-target union is the
	// row set and completeness describes the process-local observation.
	payload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/current-state", http.StatusOK)
	if payload["scope"] != "process" || fmt.Sprint(payload["instance_id"]) == "" || fmt.Sprint(payload["configuration_revision"]) == "" {
		t.Fatalf("expected global current-state envelope, got %+v", payload)
	}
	completeness := asMap(t, payload["completeness"])
	if completeness["state"] != "ready" || completeness["complete"] != true || jsonInt(t, completeness["configured_target_count"]) != 1 || jsonInt(t, completeness["observed_target_count"]) != 1 || jsonInt(t, completeness["unobserved_target_count"]) != 0 {
		t.Fatalf("expected ready completeness with one observed target, got %+v", completeness)
	}
	item := s15CurrentStateItemByConnectionID(t, payload, connectionID)
	if item["observation_state"] != "observed" || item["state"] != "retry_wait" || item["next_retry_at"] == nil || jsonInt(t, item["cycle_retry_attempts"]) != 2 || jsonInt(t, item["last_success_response_headers_latency_ms"]) != 540 {
		t.Fatalf("unexpected loadbalance global current-state item: %+v", item)
	}
	model := asMap(t, item["model"])
	target := asMap(t, item["terminal_target"])
	if jsonInt(t, model["model_config_id"]) != modelConfigID || model["id"] != "lb-model" || jsonInt(t, target["id"]) != connectionID || item["available"] != false {
		t.Fatalf("unexpected global current-state identities: %+v", item)
	}
	assertS15NoPolicyThresholdFields(t, item)
}

func TestObservabilityLoadbalanceRetryWindowStateAndSummaryRemainCoherent(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Retry Window Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-retry-window-model", stringPtr("Loadbalance Retry Window Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Retry Window Endpoint")
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

	currentStatePayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/current-state", http.StatusOK)
	item := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if item["observation_state"] != "observed" || item["state"] != "retry_wait" || item["next_retry_at"] == nil || jsonInt(t, item["cycle_retry_attempts"]) != 1 || jsonInt(t, item["last_retry_delay_ms"]) != 60000 {
		t.Fatalf("expected retry-window current-state payload, got %+v", item)
	}
	assertS15NoPolicyThresholdFields(t, item)

	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1150, ProfileID: profileID, ConnectionID: connectionID, EventType: "retry_scheduled", FailureKind: &failureKind, ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retry-window-model"), EndpointID: &endpointID, BanMode: stringPtr("off"), CreatedAt: fixedS15Now.Add(-1 * time.Second)})

	queryContext := s15IssueEventsContext(t, harness, profileID, "24h")
	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&model_id=lb-retry-window-model&limit=20", http.StatusOK)
	event := s15LoadbalanceEventByConnectionID(t, listPayload, connectionID)
	summary := asMap(t, event["summary"])
	params := asMap(t, summary["params"])
	if event["event_type"] != "retry_scheduled" || summary["code"] != "loadbalance.retry_scheduled" || jsonInt(t, summary["version"]) != 1 || params["evidence_state"] != "complete" || params["failure_kind"] != "transient_http" || jsonInt(t, params["cycle_retry_attempts"]) != 1 {
		t.Fatalf("expected retry-scheduled V1 summary payload, got %+v", event)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/loadbalance/events/%s?query_context=%s", event["event_id"], url.QueryEscape(queryContext)), http.StatusOK)
	detailSummary := asMap(t, detailPayload["summary"])
	detailParams := asMap(t, detailSummary["params"])
	if detailPayload["event_type"] != "retry_scheduled" || detailSummary["code"] != "loadbalance.retry_scheduled" || detailParams["evidence_state"] != "complete" || jsonInt(t, detailParams["cycle_retry_attempts"]) != 1 {
		t.Fatalf("expected retry-scheduled event detail payload, got %+v", detailPayload)
	}
}

func TestLoadbalanceReset(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "S15 Reset Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "lb-reset-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Reset Endpoint")
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	insertRuntimeState(t, harness, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, ConsecutiveFailures: 3, LastFailureKind: stringPtr("timeout"), LastCooldownSeconds: 90.0, BanMode: "until_reset", CreatedAt: fixedS15Now.Add(-1 * time.Hour), UpdatedAt: fixedS15Now})
	for range 3 {
		harness.runtimeService.RuntimeState().ClaimRoundRobinCursor(profileID, modelConfigID, 4)
	}

	payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, http.StatusOK)
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["cleared"] != true {
		t.Fatalf("unexpected loadbalance reset payload: %+v", payload)
	}
	resetState := asMap(t, payload["state"])
	if resetState == nil {
		t.Fatalf("expected reset payload to include state DTO, got %+v", payload)
	}
	if resetState["state"] != "available" || jsonInt(t, resetState["cycle_retry_attempts"]) != 0 || jsonInt(t, resetState["cumulative_retry_attempts"]) != 0 || resetState["next_retry_at"] != nil || jsonInt(t, resetState["last_retry_delay_ms"]) != 0 || resetState["ban_mode"] != "off" || resetState["banned_until_at"] != nil || resetState["last_failure_kind"] != nil {
		t.Fatalf("expected reset to clear cooldown fields in state DTO, got %+v", resetState)
	}
	if _, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID); !ok {
		t.Fatalf("expected loadbalance reset to preserve local runtime state row")
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor == 0 {
		t.Fatalf("expected loadbalance reset to preserve round-robin cursor, got %d", cursor)
	}
	if _, exists := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID); !exists {
		t.Fatalf("expected narrow reset to preserve the connection state key")
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor == 0 {
		t.Fatalf("expected narrow reset to preserve the round-robin cursor, got 0")
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor != 3 {
		t.Fatalf("expected loadbalance reset to preserve round-robin cursor, got %d", cursor)
	}

	// Second reset has no cooldown fields left to clear: cleared=false with snapshot.
	secondPayload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, http.StatusOK)
	if jsonInt(t, secondPayload["connection_id"]) != connectionID || secondPayload["cleared"] != false || secondPayload["state"] == nil {
		t.Fatalf("expected no-op reset payload with cleared=false and state snapshot, got %+v", secondPayload)
	}

	// Unknown or cross-profile connection id returns 404.
	s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/current-state/999999/reset", nil, http.StatusNotFound)
}

func TestLoadbalanceEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1000, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 2, CooldownSeconds: 60.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), BanMode: stringPtr("off"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1001, ProfileID: profileID, ConnectionID: 1, EventType: "banned", FailureKind: stringPtr("transient_http"), ConsecutiveFailures: 3, CooldownSeconds: 120.0, ModelID: stringPtr("lb-events-model"), EndpointID: intPtr(12), BanMode: stringPtr("temporary"), PolicyCycleRetryAttemptLimit: intPtr(2), PolicyBanCumulativeRetryAttemptThreshold: intPtr(3), BannedUntilAt: timePtr(fixedS15Now.Add(1 * time.Hour)), CreatedAt: fixedS15Now.Add(-1 * time.Minute)})

	queryContext := s15IssueEventsContext(t, harness, profileID, "24h")
	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&model_id=lb-events-model&limit=50", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 2 || listPayload["has_more"] != false {
		t.Fatalf("expected loadbalance events list payload, got %+v", listPayload)
	}
	first := asMap(t, items[0])
	firstSummary := asMap(t, first["summary"])
	firstParams := asMap(t, firstSummary["params"])
	if first["event_id"] != "1001" || firstSummary["code"] != "loadbalance.banned" || jsonInt(t, firstParams["policy_ban_cumulative_retry_attempt_threshold"]) != 3 || jsonInt(t, first["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, first["policy_ban_cumulative_retry_attempt_threshold"]) != 3 || first["ban_mode"] != "temporary" || first["banned_until_at"] == nil {
		t.Fatalf("expected newest banned event with public policy snapshots and V1 summary, got %+v", first)
	}
	if firstParams["failure_kind"] != "transient_http" || firstParams["evidence_state"] != "complete" {
		t.Fatalf("expected complete banned summary params, got %+v", firstParams)
	}
	// The list DTO carries the policy snapshots as its own top-level typed
	// fields (SPEC §7.2); the summary params reconcile with them.
	if jsonInt(t, firstParams["cycle_retry_attempts"]) != jsonInt(t, first["cycle_retry_attempts"]) {
		t.Fatalf("summary params must reconcile with top-level facts: %+v vs %+v", firstParams, first)
	}

	// Bidirectional stable sort: desc is newest-first; asc restores the causal
	// order (older event first).
	ascPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&sort_order=asc&limit=50", http.StatusOK)
	ascItems := ascPayload["items"].([]any)
	if len(ascItems) != 2 || asMap(t, ascItems[0])["event_id"] != "1000" || asMap(t, ascItems[1])["event_id"] != "1001" {
		t.Fatalf("expected asc order 1000 -> 1001, got %+v", ascPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events/1001?query_context="+url.QueryEscape(queryContext), http.StatusOK)
	detailSummary := asMap(t, detailPayload["summary"])
	detailParams := asMap(t, detailSummary["params"])
	if jsonInt(t, detailPayload["cycle_retry_attempts"]) != 3 || jsonInt(t, detailPayload["cumulative_retry_attempts"]) != 3 || jsonInt(t, detailPayload["last_retry_delay_ms"]) != 120000 || detailPayload["ban_mode"] != "temporary" || jsonInt(t, detailPayload["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, detailPayload["policy_ban_cumulative_retry_attempt_threshold"]) != 3 || detailSummary["code"] != "loadbalance.banned" || detailParams["ban_mode"] != "temporary" || detailParams["banned_until_at"] == nil {
		t.Fatalf("expected loadbalance event detail payload with policy snapshot and V1 summary, got %+v", detailPayload)
	}
}

func TestLoadbalancePartitionProfileScopedEvents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := s15InsertProfile(t, harness, "S15 Other Loadbalance")
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1200, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 30.0, ModelID: stringPtr("lb-partition-model"), CreatedAt: fixedS15Now.Add(-2 * time.Minute)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1201, ProfileID: otherProfileID, ConnectionID: 1, EventType: "banned", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 2, CooldownSeconds: 60.0, ModelID: stringPtr("lb-partition-model"), CreatedAt: fixedS15Now.Add(-1 * time.Minute)})

	queryContext := s15IssueEventsContext(t, harness, profileID, "24h")
	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&limit=20", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 1 || asMap(t, items[0])["event_id"] != "1200" {
		t.Fatalf("expected profile-scoped loadbalance event list over partitions, got %+v", listPayload)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/events/1200?query_context="+url.QueryEscape(queryContext), http.StatusOK)
	if detailPayload["event_type"] != "retry_scheduled" || detailPayload["event_id"] != "1200" {
		t.Fatalf("expected loadbalance partition detail for Default profile, got %+v", detailPayload)
	}
}

func TestLoadbalanceEventRetentionJob(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1100, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-48 * time.Hour)})
	insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{ID: 1101, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", ConsecutiveFailures: 1, CooldownSeconds: 60.0, ModelID: stringPtr("lb-retention-model"), CreatedAt: fixedS15Now.Add(-30 * time.Minute)})
	refreshS15ActualCoverage(t, harness, "loadbalance_events")

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
	endpointID := modelInsertEndpoint(t, harness, profileID, "LB Runtime Snapshot Endpoint")
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
	if _, _, err := loadbalancedomain.InsertRuntimeFailureEvent(context.Background(), harness.conn, s15LoadbalancePartitionEnsurer{harness: harness}, profileID, modelConfigID, connectionID, transition, strategy, "connect_error", fixedS15Now.Add(4*time.Second)); err != nil {
		t.Fatalf("insert runtime failure loadbalance event: %v", err)
	}

	var storedCycleLimit, storedBanThreshold int
	if err := harness.conn.QueryRow(context.Background(), `SELECT policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold FROM loadbalance_events WHERE profile_id = $1 AND connection_id = $2`, profileID, connectionID).Scan(&storedCycleLimit, &storedBanThreshold); err != nil {
		t.Fatalf("load stored policy snapshots: %v", err)
	}
	if storedCycleLimit != 2 || storedBanThreshold != 4 {
		t.Fatalf("expected stored immutable policy snapshots 2/4, got %d/%d", storedCycleLimit, storedBanThreshold)
	}

	// A custom event window must cover events written at fixedS15Now+4s (the
	// s15 harness freezes the service clock at fixedS15Now).
	contextPayload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{
		"requested_preset": "custom",
		"custom_from_time": fixedS15Now.Add(-1 * time.Hour).Format(time.RFC3339),
		"custom_to_time":   fixedS15Now.Add(1 * time.Hour).Format(time.RFC3339),
	}, http.StatusOK)
	queryContext := fmt.Sprint(contextPayload["query_context"])

	listPath := "/api/loadbalance/events?query_context=" + url.QueryEscape(queryContext) + "&model_id=lb-runtime-snapshot-model&limit=20"
	listPayload := requestS15LoadbalanceEventsUntil(t, harness, profileID, listPath, connectionID, "banned")
	event := s15LoadbalanceEventByConnectionIDAndType(t, listPayload, connectionID, "banned")
	summary := asMap(t, event["summary"])
	params := asMap(t, summary["params"])
	if jsonInt(t, event["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, event["policy_ban_cumulative_retry_attempt_threshold"]) != 4 || jsonInt(t, params["policy_ban_cumulative_retry_attempt_threshold"]) != 4 || params["failure_kind"] != "connect_error" || params["evidence_state"] != "complete" {
		t.Fatalf("expected runtime event list to expose public policy snapshots and V1 summary, got %+v", event)
	}
	if summary["code"] != "loadbalance.banned" {
		t.Fatalf("expected banned V1 summary code, got %+v", summary)
	}

	detailPayload := s15GET[map[string]any](t, harness, profileID, fmt.Sprintf("/api/loadbalance/events/%s?query_context=%s", event["event_id"], url.QueryEscape(queryContext)), http.StatusOK)
	detailSummary := asMap(t, detailPayload["summary"])
	detailParams := asMap(t, detailSummary["params"])
	if jsonInt(t, detailPayload["policy_cycle_retry_attempt_limit"]) != 2 || jsonInt(t, detailPayload["policy_ban_cumulative_retry_attempt_threshold"]) != 4 || detailSummary["code"] != "loadbalance.banned" || detailParams["ban_mode"] != "until_reset" {
		t.Fatalf("expected runtime event detail to expose public policy snapshots and V1 summary, got %+v", detailPayload)
	}
	if detailParams["banned_until_at"] != nil {
		t.Fatalf("expected until_reset ban summary to carry null banned_until_at, got %+v", detailParams)
	}

	currentStatePayload := s15GET[map[string]any](t, harness, profileID, "/api/loadbalance/current-state", http.StatusOK)
	currentState := s15CurrentStateItemByConnectionID(t, currentStatePayload, connectionID)
	if currentState["state"] != "banned" || currentState["ban_mode"] != "until_reset" || currentState["observation_state"] != "observed" {
		t.Fatalf("expected current-state to remain connection-global while banned, got %+v", currentState)
	}
	assertS15NoPolicyThresholdFields(t, currentState)
}

func newS15ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "s15_contract", contractHarnessOptions{
		SecretEncryptionKey: "s15-contract-secret",
		Version:             "s15-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			ensureContractTestLogPartitions(t, harness,
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
			statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return fixedS15Now }, SecretEncryptionKey: settings.SecretEncryptionKey})
			if err != nil {
				t.Fatalf("build stats service: %v", err)
			}
			t.Cleanup(statsService.Close)
			runtimeService, err := runtimeapi.NewService(settings, runtimeapi.Options{ExecutionPool: pool, TelemetryPool: telemetryPool, FeedbackPool: feedbackPool, Now: func() time.Time { return fixedS15Now }, Cache: runtimeCache, RuntimeState: runtimeState})
			if err != nil {
				t.Fatalf("build runtime service: %v", err)
			}
			t.Cleanup(runtimeService.Close)
			harness.runtimeService = runtimeService
			harness.runtimeCache = runtimeCache
			return platformhttp.Dependencies{
				AuditService:       auditService,
				LoadbalanceService: loadbalanceService,
				RuntimeService:     runtimeService,
				RuntimeCache:       runtimeCache,
				RuntimeState:       runtimeState,
				SettingsService:    settingsService,
				StatsService:       statsService,
			}
		},
	})
}

type usageEventSeed struct {
	ID, ProfileID, StatusCode, AttemptCount int
	IngressRequestID, ModelID, APIFamily    string
	SuccessFlag                             bool
	EndpointID, ConnectionID                *int
	ProxyAPIKeyID                           *int
	InputTokens, OutputTokens, TotalTokens  *int
	CacheReadInputTokens                    *int
	CacheCreationInputTokens                *int
	ReasoningTokens                         *int
	ResponseTimeMS, TTFTMS                  *int
	CompletionDurationMS                    *int
	EndpointLabelSnapshot                   *string
	ProxyAPIKeyNameSnapshot                 *string
	UnpricedReason                          *string
	BillableFlag, PricedFlag                *bool
	TotalCostUserCurrencyMicros             *int64
	RequestPath                             string
	CreatedAt                               time.Time
}

type auditLogSeed struct {
	ID, ProfileID, ResponseStatus                  int
	ModelID, RequestHeaders                        string
	RequestLogID                                   *int
	RequestLogCreatedAt                            *time.Time
	IngressRequestID, RequestBody, ResponseHeaders *string
	ResponseBody                                   *string
	RequestBodyStored, ResponseBodyStored          *bool
	IsStream, AuditEnabledAtRequest                bool
	AuditCaptureBodiesAtRequest                    bool
	CreatedAt                                      time.Time
}

type runtimeStateSeed struct {
	ProfileID, ConnectionID, ConsecutiveFailures int
	LastFailureKind                              *string
	LastCooldownSeconds                          float64
	BanMode                                      string
	BlockedUntilAt                               *time.Time
	LastSuccessResponseHeadersLatencyMS          *int
	CreatedAt, UpdatedAt                         time.Time
}

type loadbalanceEventSeed struct {
	ID                                       int64
	ProfileID, ConnectionID                  int
	EventType                                string
	FailureKind, ModelID                     *string
	EndpointID                               *int
	ConsecutiveFailures                      int
	CooldownSeconds                          float64
	BanMode                                  *string
	PolicyCycleRetryAttemptLimit             *int
	PolicyBanCumulativeRetryAttemptThreshold *int
	BannedUntilAt                            *time.Time
	CreatedAt                                time.Time
}

func insertUsageEvent(t *testing.T, harness *contractHarness, seed usageEventSeed) {
	t.Helper()
	seed = coherentUsageEventSeed(seed)
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", seed.CreatedAt))
	attributionState := "unknown"
	if seed.ProxyAPIKeyID != nil {
		attributionState = "identified"
	}
	if _, err := harness.conn.Exec(
		context.Background(),
		`INSERT INTO usage_request_events (id, profile_id, ingress_request_id, model_id, api_family, endpoint_id, endpoint_label_snapshot, connection_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, pricing_status, pricing_evidence_trust, unpriced_reason, pricing_resolution_kind) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37)`,
		seed.ID,
		seed.ProfileID,
		seed.IngressRequestID,
		seed.ModelID,
		seed.APIFamily,
		nullableTestInt(seed.EndpointID),
		usageEventEndpointLabel(t, harness, seed),
		nullableTestInt(seed.ConnectionID),
		nullableTestInt(seed.ProxyAPIKeyID),
		nullableTestString(seed.ProxyAPIKeyNameSnapshot),
		attributionState,
		nil,
		seed.StatusCode,
		seed.SuccessFlag,
		nullableTestInt(seed.InputTokens),
		nullableTestInt(seed.OutputTokens),
		nullableTestInt(seed.TotalTokens),
		nullableTestInt(seed.CacheReadInputTokens),
		nullableTestInt(seed.CacheCreationInputTokens),
		nullableTestInt(seed.ReasoningTokens),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(usageEventComponentCost(seed)),
		nullableTestInt64(seed.TotalCostUserCurrencyMicros),
		nullableTestInt64(seed.TotalCostUserCurrencyMicros),
		seed.AttemptCount,
		seed.RequestPath,
		seed.CreatedAt,
		nullableTestInt(seed.ResponseTimeMS),
		nullableTestInt(seed.CompletionDurationMS),
		nullableTestInt(seed.TTFTMS),
		usageEventPricingStatus(seed),
		usageEventPricingTrust(seed),
		usageEventUnpricedReason(seed),
		usageEventResolutionKind(seed),
	); err != nil {
		t.Fatalf("insert usage event %d: %v", seed.ID, err)
	}
}

// usageEventPricingStatus projects the four-state status for fixture rows
// (SPEC 3.4): known non-2xx -> ineligible; 2xx priced seeds -> priced; 2xx
// with a typed usage reason -> unpriced; everything else stays unknown.
func usageEventPricingStatus(seed usageEventSeed) string {
	if seed.StatusCode != 0 && (seed.StatusCode < 200 || seed.StatusCode > 299) {
		return "ineligible"
	}
	if seed.PricedFlag != nil && *seed.PricedFlag {
		return "priced"
	}
	if seed.UnpricedReason != nil {
		return "unpriced"
	}
	return "unknown"
}

func usageEventPricingTrust(seed usageEventSeed) string {
	switch usageEventPricingStatus(seed) {
	case "priced":
		return "trusted"
	case "unpriced":
		// An unpriced row carrying any cost value is not canonical (the
		// all-or-none coherence predicate requires all costs absent for
		// unpriced); fixture rows keep legacy_untrusted instead of faking a
		// trusted snapshot.
		if seed.TotalCostUserCurrencyMicros != nil {
			return "legacy_untrusted"
		}
		return "trusted"
	default:
		return "legacy_untrusted"
	}
}

func usageEventUnpricedReason(seed usageEventSeed) *string {
	if usageEventPricingStatus(seed) != "unpriced" {
		return nil
	}
	return seed.UnpricedReason
}

// usageEventResolutionKind gives MISSING_PRICE_DATA fixture rows a typed
// resolution kind so the DB CHECK stays satisfied (SPEC 3.3).
func usageEventResolutionKind(seed usageEventSeed) *string {
	if seed.UnpricedReason != nil && *seed.UnpricedReason == "MISSING_PRICE_DATA" {
		return stringPtr("snapshot_incoherent")
	}
	return nil
}

// usageEventComponentCost gives priced fixtures an all-or-none cost set
// (SPEC 6.5): every component is present (0 when not seeded) so trusted
// priced rows satisfy the DB coherence CHECK.
func usageEventComponentCost(seed usageEventSeed) *int64 {
	if seed.PricedFlag == nil || !*seed.PricedFlag {
		return nil
	}
	if seed.TotalCostUserCurrencyMicros == nil {
		return int64Ptr(0)
	}
	return int64Ptr(0)
}

func coherentUsageEventSeed(seed usageEventSeed) usageEventSeed {
	if seed.APIFamily == "" {
		seed.APIFamily = "openai"
	}
	if seed.StatusCode == 0 {
		seed.StatusCode = http.StatusOK
	}
	if seed.StatusCode >= 200 && seed.StatusCode < 300 {
		seed.SuccessFlag = true
	}
	if seed.AttemptCount == 0 {
		seed.AttemptCount = 1
	}
	if seed.RequestPath == "" {
		seed.RequestPath = "/v1/chat/completions"
	}
	if seed.UnpricedReason != nil {
		trimmed := strings.TrimSpace(*seed.UnpricedReason)
		if trimmed == "" {
			seed.UnpricedReason = nil
		} else {
			seed.UnpricedReason = &trimmed
		}
	}
	if seed.SuccessFlag && seed.BillableFlag == nil {
		seed.BillableFlag = boolPtr(true)
	}
	if seed.SuccessFlag && seed.PricedFlag == nil {
		seed.PricedFlag = boolPtr(true)
	}
	if !seed.SuccessFlag || seed.BillableFlag == nil || !*seed.BillableFlag {
		return seed
	}
	if seed.UnpricedReason != nil {
		// Unpriced rows must be canonical: no cost values at all (the
		// all-or-none coherence predicate and trusted-requires-known checks
		// forbid unpriced rows carrying costs). The fixture drops them so the
		// insert is a coherent trusted snapshot; callers that need a cost
		// projection set it via explicit updates after seeding.
		seed.PricedFlag = boolPtr(false)
		seed.TotalCostUserCurrencyMicros = nil
		return seed
	}
	if seed.TotalCostUserCurrencyMicros != nil {
		seed.PricedFlag = boolPtr(true)
		return seed
	}
	if seed.PricedFlag != nil && *seed.PricedFlag {
		seed.PricedFlag = boolPtr(false)
		seed.UnpricedReason = stringPtr("MISSING_PRICE_DATA")
	}
	return seed
}

func usageEventEndpointLabel(t *testing.T, harness *contractHarness, seed usageEventSeed) string {
	t.Helper()
	if seed.EndpointLabelSnapshot != nil && strings.TrimSpace(*seed.EndpointLabelSnapshot) != "" {
		return strings.TrimSpace(*seed.EndpointLabelSnapshot)
	}
	if seed.EndpointID == nil {
		return "Unknown Endpoint"
	}
	var label string
	if err := harness.conn.QueryRow(context.Background(), `SELECT name FROM endpoints WHERE id = $1`, *seed.EndpointID).Scan(&label); err == nil && strings.TrimSpace(label) != "" {
		return label
	}
	return fmt.Sprintf("Endpoint %d", *seed.EndpointID)
}

func insertRequestLogSummaryRow(t *testing.T, harness *contractHarness, id int, profileID int, modelID string, apiFamily string, endpointID int, connectionID int, statusCode int, responseTimeMS int, inputTokens int, outputTokens int, totalTokens int, createdAt time.Time) {
	t.Helper()
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, id, profileID, modelID, apiFamily, endpointID, connectionID, statusCode, responseTimeMS, inputTokens, outputTokens, totalTokens, createdAt, false)
}

func insertRequestLogSummaryRowWithAuditEnabled(t *testing.T, harness *contractHarness, id int, profileID int, modelID string, apiFamily string, endpointID int, connectionID int, statusCode int, responseTimeMS int, inputTokens int, outputTokens int, totalTokens int, createdAt time.Time, auditEnabledAtRequest bool) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", createdAt))
	requestStatus := "unknown"
	requestTrust := "legacy_untrusted"
	if statusCode < 200 || statusCode > 299 {
		requestStatus = "ineligible"
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, connection_id, row_kind, url_scrub_provenance, legacy_status_code, legacy_duration_ms, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at, endpoint_base_url, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, $4, $5, $6, 'legacy_unknown', 'legacy_unknown', $7, $8, $7, $8, FALSE, $9, $10, $11, $12, $13, $14, '/v1/chat/completions', $15, $16, $17, FALSE)`, id, profileID, modelID, apiFamily, endpointID, connectionID, statusCode, responseTimeMS, inputTokens, outputTokens, totalTokens, statusCode >= 200 && statusCode < 300, requestStatus, requestTrust, createdAt, fmt.Sprintf("https://endpoint-%d.invalid", endpointID), auditEnabledAtRequest); err != nil {
		t.Fatalf("insert request-log summary row %d: %v", id, err)
	}
}

func insertAuditLog(t *testing.T, harness *contractHarness, seed auditLogSeed) {
	t.Helper()
	auditCaptureBodiesAtRequest := seed.AuditCaptureBodiesAtRequest
	if !auditCaptureBodiesAtRequest && (seed.RequestBody != nil || seed.ResponseBody != nil) {
		auditCaptureBodiesAtRequest = true
	}
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("audit_logs", seed.CreatedAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO audit_logs (id, profile_id, request_log_id, request_log_created_at, ingress_request_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_headers_scrub_provenance, request_headers_capture_status, request_body, request_body_encoding, request_body_capture_provenance, request_body_capture_end_state, request_body_capture_status, request_body_bytes_observed, request_body_bytes_stored, legacy_status_code, legacy_duration_ms, response_headers, response_headers_scrub_provenance, response_headers_capture_status, response_body, response_body_encoding, response_body_capture_provenance, response_body_capture_end_state, response_body_capture_status, response_body_bytes_observed, response_body_bytes_stored, row_kind, url_scrub_provenance, is_stream, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, $3, $4, $5, $6, NULL, NULL, 'https://audit.invalid', 'Audit endpoint', 'POST', 'https://audit.invalid/v1/chat/completions', $7::jsonb, 'legacy_unknown', CASE WHEN $7::jsonb IS NULL THEN 'not_requested' ELSE 'captured' END, $8::bytea, CASE WHEN $8::bytea IS NULL THEN NULL ELSE 'utf8' END, CASE WHEN $8::bytea IS NULL THEN 'not_applicable' ELSE 'legacy_text_transcoded' END, CASE WHEN $8::bytea IS NULL THEN NULL ELSE 'unknown' END, CASE WHEN $8::bytea IS NULL THEN 'not_requested' ELSE 'captured' END, CASE WHEN $8::bytea IS NULL THEN NULL ELSE octet_length($8::bytea) END, CASE WHEN $8::bytea IS NULL THEN NULL ELSE octet_length($8::bytea) END, $9, 1234, $10::jsonb, 'legacy_unknown', CASE WHEN $10::jsonb IS NULL THEN 'not_requested' ELSE 'captured' END, $11::bytea, CASE WHEN $11::bytea IS NULL THEN NULL ELSE 'utf8' END, CASE WHEN $11::bytea IS NULL THEN 'not_applicable' ELSE 'legacy_text_transcoded' END, CASE WHEN $11::bytea IS NULL THEN NULL ELSE 'unknown' END, CASE WHEN $11::bytea IS NULL THEN 'not_requested' ELSE 'captured' END, CASE WHEN $11::bytea IS NULL THEN NULL ELSE octet_length($11::bytea) END, CASE WHEN $11::bytea IS NULL THEN NULL ELSE octet_length($11::bytea) END, 'legacy_unknown', 'legacy_unknown', $12, $13, $14, $15)`, seed.ID, seed.ProfileID, nullableTestInt(seed.RequestLogID), nullableTestTime(seed.RequestLogCreatedAt), nullableTestString(seed.IngressRequestID), seed.ModelID, seed.RequestHeaders, nullableTestStringBytes(seed.RequestBody), seed.ResponseStatus, seed.ResponseHeaders, nullableTestStringBytes(seed.ResponseBody), seed.IsStream, seed.AuditEnabledAtRequest, auditCaptureBodiesAtRequest, seed.CreatedAt); err != nil {
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
	harness.runtimeService.RuntimeState().SeedConnectionState(seed.ProfileID, modelConfigID, seed.ConnectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:                        seed.ConnectionID,
		BanMode:                             banMode,
		NextRetryAt:                         seed.BlockedUntilAt,
		WindowRequestCount:                  4,
		InFlightNonStream:                   1,
		CycleRetryAttempts:                  seed.ConsecutiveFailures,
		CumulativeRetryAttempts:             seed.ConsecutiveFailures,
		LastRetryDelayMS:                    int(seed.LastCooldownSeconds * 1000),
		LastFailureKind:                     seed.LastFailureKind,
		LastSuccessResponseHeadersLatencyMS: seed.LastSuccessResponseHeadersLatencyMS,
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
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_events (id, profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`, seed.ID, seed.ProfileID, seed.ConnectionID, seed.EventType, nullableTestString(seed.FailureKind), seed.ConsecutiveFailures, nullableTestTime(nextRetryAt), lastRetryDelayMS, nullableTestString(seed.ModelID), nullableTestInt(seed.EndpointID), nullableTestString(seed.BanMode), nullableTestInt(seed.PolicyCycleRetryAttemptLimit), nullableTestInt(seed.PolicyBanCumulativeRetryAttemptThreshold), nullableTestTime(seed.BannedUntilAt), seed.CreatedAt); err != nil {
		t.Fatalf("insert loadbalance event %d: %v", seed.ID, err)
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
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) == connectionID {
			return item
		}
	}
	t.Fatalf("expected current-state payload for connection %d, got %+v", connectionID, payload)
	return nil
}

func s15IssueEventsContext(t *testing.T, harness *contractHarness, profileID int, preset string) string {
	t.Helper()
	payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": preset}, http.StatusOK)
	token, ok := payload["query_context"].(string)
	if !ok || token == "" {
		t.Fatalf("expected issued events query context, got %+v", payload)
	}
	return token
}

func s15LoadbalanceEventByConnectionID(t *testing.T, payload map[string]any, connectionID int) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected loadbalance event items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) == connectionID {
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
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) == connectionID && item["event_type"] == eventType {
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
		payload = s15GET[map[string]any](t, harness, profileID, path, http.StatusOK)
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
		target, ok := item["terminal_target"].(map[string]any)
		if !ok || intFromJSONNumber(target["id"]) != connectionID {
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

func withHeader(headers map[string]string, key string, value string) map[string]string {
	merged := make(map[string]string, len(headers)+1)
	maps.Copy(merged, headers)
	merged[key] = value
	return merged
}

func createS15LogRetentionJob(t *testing.T, harness *contractHarness, tableName string, scope map[string]any, suffix string) string {
	t.Helper()
	// Manual cleanup requires the fresh-preflight -> sealed-job flow (SPEC §6);
	// the old table/cutoff create route was removed.
	selection := map[string]any{}
	if cutoff, ok := scope["cutoff"].(string); ok {
		selection["mode"] = "cutoff"
		selection["cutoff"] = cutoff
	} else if deleteAll, ok := scope["delete_all"].(bool); ok && deleteAll {
		selection["mode"] = "delete_all"
	} else {
		selection["mode"] = "keep_days"
		selection["days"] = 7
	}
	operationID := "s15-purge-" + suffix + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	preflight := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/maintenance/log-retention/preflights", map[string]any{
		"kind":                 "manual_cleanup",
		"operation_id":         operationID,
		"preflight_attempt_id": "s15-attempt-" + suffix,
		"dataset":              tableName,
		"selection":            selection,
	}, map[string]string{}, http.StatusCreated)
	token, ok := preflight["preflight_token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected retention preflight token, got %+v", preflight)
	}
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/maintenance/log-retention/jobs", map[string]any{
		"operation_id":    operationID,
		"preflight_token": token,
		"confirmation":    map[string]any{"keyword": "DELETE"},
	}, withHeader(map[string]string{}, "Idempotency-Key", "log-retention-"+suffix))
	assertStatus(t, response, http.StatusAccepted)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	job, ok := payload["job"].(map[string]any)
	jobID, hasID := job["id"].(string)
	if !ok || !hasID || jobID == "" || job["state"] != "queued" || job["dataset"] != tableName {
		t.Fatalf("expected log-retention job response, got %+v", payload)
	}
	return jobID
}

func refreshS15ActualCoverage(t *testing.T, harness *contractHarness, datasets ...string) {
	t.Helper()
	for _, dataset := range datasets {
		source, err := statsdomain.LoadRetentionSourceProjection(context.Background(), harness.conn, dataset, fixedS15Now)
		if err != nil {
			t.Fatalf("load %s retention source for fixture refresh: %v", dataset, err)
		}
		if err := statsdomain.RefreshActualCoverageProjection(context.Background(), harness.conn, source, fixedS15Now); err != nil {
			t.Fatalf("refresh %s actual coverage for fixture: %v", dataset, err)
		}
	}
}

func nullableTestInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTestStringBytes(value *string) any {
	if value == nil {
		return nil
	}
	return []byte(*value)
}

func mustDecodeBase64String(t *testing.T, value any) string {
	t.Helper()
	encoded, ok := value.(string)
	if !ok {
		t.Fatalf("expected base64 string, got %T", value)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64 body: %v", err)
	}
	return string(decoded)
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

func timePtr(value time.Time) *time.Time {
	resolved := value.UTC()
	return &resolved
}

func TestAuditAnchorItemOutsideFirstPageIncludedExactlyOnce(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	requestCreatedAt := fixedS15Now.Add(-30 * time.Minute)
	insertRequestLogSummaryRowWithAuditEnabled(t, harness, 9400, profileID, "audit-anchor", "openai", 12, 91, 200, 100, 0, 0, 0, requestCreatedAt, true)
	// 25 audit rows for the same ingress: the anchored row (id 9401, oldest)
	// falls outside the first page of limit=5.
	for index := 0; index < 25; index++ {
		insertAuditLog(t, harness, auditLogSeed{
			ID:                    9401 + index,
			ProfileID:             profileID,
			RequestLogID:          intPtr(9400),
			RequestLogCreatedAt:   timePtr(requestCreatedAt),
			IngressRequestID:      stringPtr("anchor-ingress-9400"),
			ModelID:               "audit-anchor",
			RequestHeaders:        `{}`,
			ResponseStatus:        200,
			AuditEnabledAtRequest: true,
			CreatedAt:             fixedS15Now.Add(-time.Duration(25-index) * time.Minute),
		})
	}

	listPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=5&anchor_id=9401", http.StatusOK)
	items := listPayload["items"].([]any)
	if len(items) != 5 {
		t.Fatalf("expected first page of 5, got %d: %+v", len(items), listPayload)
	}
	anchorPayload, ok := listPayload["anchor_item"].(map[string]any)
	if !ok {
		t.Fatalf("expected anchor_item present in first response, got %+v", listPayload)
	}
	if jsonInt(t, anchorPayload["id"]) != 9401 {
		t.Fatalf("expected anchor_item id 9401, got %+v", anchorPayload)
	}
	// Exactly once: anchor is not duplicated inside items.
	for _, raw := range items {
		item := asMap(t, raw)
		if jsonInt(t, item["id"]) == 9401 {
			t.Fatalf("anchor id 9401 duplicated inside items: %+v", listPayload)
		}
	}

	// In-page anchor: no anchor_item, no duplication.
	inPagePayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=5&anchor_id=9425", http.StatusOK)
	if _, exists := inPagePayload["anchor_item"]; exists {
		t.Fatalf("in-page anchor must not emit anchor_item: %+v", inPagePayload)
	}

	// Unknown anchor id: no anchor_item, normal list.
	unknownPayload := s15GET[map[string]any](t, harness, profileID, "/api/audit/logs?"+s15AuditWindowQuery()+"&limit=5&anchor_id=999999", http.StatusOK)
	if _, exists := unknownPayload["anchor_item"]; exists {
		t.Fatalf("unknown anchor must not emit anchor_item: %+v", unknownPayload)
	}
}

func TestRequestLogsHalfOpenTimeBounds(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	// Row exactly at `to` must belong to the next window (half-open [from, to)).
	boundary := fixedS15Now.Add(-30 * time.Minute)
	insertRequestLogSummaryRow(t, harness, 9500, profileID, "half-open-model", "openai", 12, 91, 200, 100, 0, 0, 0, boundary.Add(-time.Minute))
	insertRequestLogSummaryRow(t, harness, 9501, profileID, "half-open-model", "openai", 12, 91, 200, 100, 0, 0, 0, boundary)
	insertRequestLogSummaryRow(t, harness, 9502, profileID, "half-open-model", "openai", 12, 91, 200, 100, 0, 0, 0, boundary.Add(time.Minute))

	windowPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?from_time="+url.QueryEscape(boundary.Add(-2*time.Minute).Format(time.RFC3339))+"&to_time="+url.QueryEscape(boundary.Format(time.RFC3339))+"&ingress_model_id=half-open-model&limit=50", http.StatusOK)
	items := windowPayload["items"].([]any)
	ids := map[string]bool{}
	for _, raw := range items {
		item := asMap(t, raw)
		ids[fmt.Sprint(item["request_log_id"])] = true
	}
	if !ids["9500"] {
		t.Fatalf("expected row before `to` inside window, got %+v", windowPayload)
	}
	if ids["9501"] || ids["9502"] {
		t.Fatalf("half-open bound violated: rows at/after `to` leaked into window: %+v", windowPayload)
	}

	nextPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?from_time="+url.QueryEscape(boundary.Format(time.RFC3339))+"&to_time="+url.QueryEscape(boundary.Add(2*time.Minute).Format(time.RFC3339))+"&ingress_model_id=half-open-model&limit=50", http.StatusOK)
	nextItems := nextPayload["items"].([]any)
	nextIDs := map[string]bool{}
	for _, raw := range nextItems {
		item := asMap(t, raw)
		nextIDs[fmt.Sprint(item["request_log_id"])] = true
	}
	if !nextIDs["9501"] || !nextIDs["9502"] {
		t.Fatalf("expected boundary row to appear exactly once in the next window, got %+v", nextPayload)
	}
}

func TestEndpointTerminalTargetStatisticsDrillDown(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	conn17 := intPtr(17)
	conn18 := intPtr(18)
	endpoint7 := intPtr(7)
	now := fixedS15Now.Add(-2 * time.Hour)
	// connection 17: 2xx success (priced) + 503 failure (ineligible) + stream anomaly
	insertUsageEvent(t, harness, usageEventSeed{ID: 9601, ProfileID: profileID, IngressRequestID: "tt-1", ModelID: "tt-model", APIFamily: "openai", EndpointID: endpoint7, ConnectionID: conn17, StatusCode: 200, SuccessFlag: true, EndpointLabelSnapshot: stringPtr("TT Endpoint"), OutputTokens: intPtr(100), TotalTokens: intPtr(500), TTFTMS: intPtr(120), CompletionDurationMS: intPtr(1000), BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalCostUserCurrencyMicros: int64Ptr(1200), RequestPath: "/v1/chat/completions", CreatedAt: now})
	insertUsageEvent(t, harness, usageEventSeed{ID: 9602, ProfileID: profileID, IngressRequestID: "tt-2", ModelID: "tt-model", APIFamily: "openai", EndpointID: endpoint7, ConnectionID: conn17, StatusCode: 503, SuccessFlag: false, EndpointLabelSnapshot: stringPtr("TT Endpoint"), OutputTokens: intPtr(0), TotalTokens: intPtr(10), TTFTMS: nil, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), RequestPath: "/v1/chat/completions", CreatedAt: now.Add(time.Minute)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 9603, ProfileID: profileID, IngressRequestID: "tt-3", ModelID: "tt-model", APIFamily: "openai", EndpointID: endpoint7, ConnectionID: conn17, StatusCode: 200, SuccessFlag: true, EndpointLabelSnapshot: stringPtr("TT Endpoint"), OutputTokens: intPtr(50), TotalTokens: intPtr(200), TTFTMS: intPtr(200), CompletionDurationMS: intPtr(800), BillableFlag: boolPtr(true), PricedFlag: boolPtr(false), UnpricedReason: stringPtr("MISSING_PRICE_DATA"), RequestPath: "/v1/chat/completions", CreatedAt: now.Add(2 * time.Minute)})
	// connection 18: client disconnected stream
	insertUsageEvent(t, harness, usageEventSeed{ID: 9604, ProfileID: profileID, IngressRequestID: "tt-4", ModelID: "tt-model", APIFamily: "openai", EndpointID: endpoint7, ConnectionID: conn18, StatusCode: 200, SuccessFlag: true, EndpointLabelSnapshot: stringPtr("TT Endpoint"), OutputTokens: intPtr(10), TotalTokens: intPtr(30), TTFTMS: intPtr(90), BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalCostUserCurrencyMicros: int64Ptr(300), RequestPath: "/v1/chat/completions", CreatedAt: now.Add(3 * time.Minute)})
	// pricing statuses
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET pricing_status = 'priced' WHERE id = 9601`); err != nil {
		t.Fatalf("set priced status: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET pricing_status = 'ineligible' WHERE id = 9602`); err != nil {
		t.Fatalf("set ineligible status: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET pricing_status = 'unpriced' WHERE id = 9603`); err != nil {
		t.Fatalf("set unpriced status: %v", err)
	}
	// stream anomaly: 2xx but non-terminal stream outcome
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET stream_outcome = 'upstream_read_error' WHERE id = 9603`); err != nil {
		t.Fatalf("set stream outcome: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET stream_outcome = CASE WHEN id = 9604 THEN 'client_disconnected' ELSE stream_outcome END, report_currency_code = 'USD', reporting_currency_epoch = CASE WHEN id = 9602 THEN NULL ELSE 1 END WHERE id BETWEEN 9601 AND 9604`); err != nil {
		t.Fatalf("set client disconnect: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET resolved_target_model_id='tt-model', final_attempt_number=1 WHERE id BETWEEN 9601 AND 9604`); err != nil {
		t.Fatalf("set terminal-target final_execution identity: %v", err)
	}
	// ban + admission-rejection loadbalance events for connection 17
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("loadbalance_events", now))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_events (profile_id, connection_id, endpoint_id, model_id, event_type, cycle_retry_attempts, cumulative_retry_attempts, last_retry_delay_ms, created_at) VALUES ($1, 17, 7, 'tt-model', 'banned', 0, 1, 0, $2), ($1, 17, 7, 'tt-model', 'banned', 0, 2, 0, $3), ($1, 17, 7, 'tt-model', 'admission_rejected', 0, 2, 0, $4)`, profileID, now, now.Add(time.Minute), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("insert loadbalance events: %v", err)
	}
	refreshS15ActualCoverage(t, harness, "usage_request_events", "loadbalance_events")
	usageSource, err := statsdomain.LoadRetentionSourceProjection(context.Background(), harness.conn, "usage_request_events", fixedS15Now)
	if err != nil {
		t.Fatalf("load usage owner metadata: %v", err)
	}

	terminalTargetURL := "/api/stats/endpoints/7/terminal-targets?preset=custom&from_time=" + url.QueryEscape(now.Format(time.RFC3339)) + "&to_time=" + url.QueryEscape(fixedS15Now.Format(time.RFC3339)) + "&limit=50"
	payload := s15GET[map[string]any](t, harness, profileID, terminalTargetURL, http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 terminal targets, got %+v", payload)
	}
	byConn := map[int]map[string]any{}
	for _, raw := range items {
		item := asMap(t, raw)
		byConn[jsonInt(t, item["connection_id"])] = item
	}
	first := byConn[17]
	if first == nil {
		t.Fatalf("expected connection 17, got %+v", payload)
	}
	if jsonInt(t, first["request_count"]) != 3 {
		t.Fatalf("expected 3 requests for conn 17, got %+v", first)
	}
	if jsonInt(t, first["http_success_count"]) != 2 || jsonInt(t, first["http_failed_count"]) != 1 {
		t.Fatalf("expected 2 success / 1 failed for conn 17, got %+v", first)
	}
	// final_failed counts http_error + stream_error only (client_disconnected separate)
	if jsonInt(t, first["final_failed_count"]) != 2 {
		t.Fatalf("expected final_failed 2 (503 + stream anomaly), got %+v", first)
	}
	if jsonInt(t, first["client_disconnected_count"]) != 0 {
		t.Fatalf("expected no client disconnects for conn 17, got %+v", first)
	}
	if jsonInt(t, first["ban_event_count"]) != 2 || jsonInt(t, first["admission_rejection_count"]) != 1 {
		t.Fatalf("expected ban 2 / admission 1 for conn 17, got %+v", first)
	}
	pricing := asMap(t, first["pricing_status_counts"])
	if jsonInt(t, pricing["priced"]) != 1 || jsonInt(t, pricing["ineligible"]) != 1 || jsonInt(t, pricing["unpriced"]) != 1 {
		t.Fatalf("expected pricing counts 1/1/1 for conn 17, got %+v", pricing)
	}
	reasons := first["unpriced_reason_counts"].(map[string]any)
	wantReasons := map[string]int{
		"PRICING_DISABLED":         0,
		"MISSING_TOKEN_USAGE":      0,
		"STREAM_USAGE_UNAVAILABLE": 0,
		"MISSING_PRICE_DATA":       1,
	}
	if len(reasons) != len(wantReasons) {
		t.Fatalf("expected exactly four canonical unpriced reasons, got %+v", reasons)
	}
	for reason, want := range wantReasons {
		if jsonInt(t, reasons[reason]) != want {
			t.Fatalf("expected %s reason count %d, got %+v", reason, want, reasons)
		}
	}
	if first["p50_latency_ms"] != nil {
		t.Fatalf("expected missing final-attempt latency without request rows, got %+v", first)
	}
	second := byConn[18]
	if second == nil {
		t.Fatalf("expected connection 18, got %+v", payload)
	}
	if jsonInt(t, second["client_disconnected_count"]) != 1 {
		t.Fatalf("expected 1 client disconnect for conn 18, got %+v", second)
	}
	if jsonInt(t, second["final_failed_count"]) != 0 {
		t.Fatalf("expected client disconnect not counted as final failed, got %+v", second)
	}
	secondReasons := asMap(t, second["unpriced_reason_counts"])
	if len(secondReasons) != len(wantReasons) {
		t.Fatalf("expected exactly four canonical zero-valued reasons, got %+v", secondReasons)
	}
	for reason := range wantReasons {
		if jsonInt(t, secondReasons[reason]) != 0 {
			t.Fatalf("expected %s reason count 0, got %+v", reason, secondReasons)
		}
	}
	coverage := asMap(t, payload["coverage"])
	if coverage["state"] != "known" || coverage["complete"] != true || coverage["source_revision"] != usageSource.SourceRevision || coverage["retention_epoch"] != usageSource.RetentionEpoch || coverage["retention_generation"] != usageSource.RetentionGeneration || coverage["purge_state"] != usageSource.PurgeState {
		t.Fatalf("expected exact usage owner coverage metadata, got %+v", coverage)
	}
	if _, exists := coverage["precision"]; exists || fmt.Sprint(first["coverage"]) != fmt.Sprint(coverage) {
		t.Fatalf("terminal-target coverage must match owner coverage without page precision: %+v", payload)
	}
	for _, item := range byConn {
		if item["event_coverage_complete"] != true {
			t.Fatalf("expected fresh event owner on every item, got %+v", item)
		}
	}
	epochPayload, legacyPayload := s15GET[map[string]any](t, harness, profileID, terminalTargetURL+"&cost_segment_key=e.1", http.StatusOK), s15GET[map[string]any](t, harness, profileID, terminalTargetURL+"&cost_segment_key=l.USD", http.StatusOK)
	epochItems, legacyItems := epochPayload["items"].([]any), legacyPayload["items"].([]any)
	if jsonInt(t, payload["total"]) != 2 || len(epochItems) != 2 || jsonInt(t, epochPayload["total"]) != 2 || jsonInt(t, asMap(t, epochItems[0])["connection_id"]) != 17 || jsonInt(t, asMap(t, epochItems[0])["request_count"]) != 2 || jsonInt(t, asMap(t, epochItems[1])["connection_id"]) != 18 || jsonInt(t, asMap(t, epochItems[1])["request_count"]) != 1 || len(legacyItems) != 1 || jsonInt(t, legacyPayload["total"]) != 1 || jsonInt(t, asMap(t, legacyItems[0])["connection_id"]) != 17 || jsonInt(t, asMap(t, legacyItems[0])["request_count"]) != 1 || jsonInt(t, asMap(t, legacyItems[0])["ban_event_count"]) != 2 || jsonInt(t, asMap(t, legacyItems[0])["pricing_status_counts"].(map[string]any)["ineligible"]) != 1 {
		t.Fatalf("expected exact epoch and legacy segment membership with independent events, got all=%+v epoch=%+v legacy=%+v", payload, epochPayload, legacyPayload)
	}
	for _, offset := range []int{2, 3} {
		outOfRange := s15GET[map[string]any](t, harness, profileID, terminalTargetURL+"&offset="+strconv.Itoa(offset), http.StatusOK)
		if len(outOfRange["items"].([]any)) != 0 || jsonInt(t, outOfRange["total"]) != 2 || jsonInt(t, outOfRange["limit"]) != 50 || jsonInt(t, outOfRange["offset"]) != offset || fmt.Sprint(outOfRange["coverage"]) != fmt.Sprint(payload["coverage"]) {
			t.Fatalf("offset %d must return an empty page without changing metadata: %+v", offset, outOfRange)
		}
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE retention_coverage_read_models SET dirty = TRUE, freshness = 'stale' WHERE dataset = 'loadbalance_events'`); err != nil {
		t.Fatalf("stale event owner: %v", err)
	}
	stalePayload := s15GET[map[string]any](t, harness, profileID, terminalTargetURL, http.StatusOK)
	if fmt.Sprint(stalePayload["coverage"]) != fmt.Sprint(payload["coverage"]) {
		t.Fatalf("event owner staleness must not change usage coverage: %+v", stalePayload)
	}
	for _, raw := range stalePayload["items"].([]any) {
		if item := asMap(t, raw); item["event_coverage_complete"] != false {
			t.Fatalf("expected stale event owner to fail closed, got %+v", item)
		}
	}
	oneSidedBounds := []struct{ query, wantFrom, wantTo string }{
		{"from_time=" + url.QueryEscape(now.Format(time.RFC3339)), now.Format(time.RFC3339), fixedS15Now.Format(time.RFC3339)},
		{"to_time=" + url.QueryEscape(fixedS15Now.Format(time.RFC3339)), fixedS15Now.Add(-time.Hour).Format(time.RFC3339), fixedS15Now.Format(time.RFC3339)},
	}
	for _, oneSided := range oneSidedBounds {
		oneSidedPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/endpoints/7/terminal-targets?"+oneSided.query+"&limit=50", http.StatusOK)
		oneSidedCoverage := asMap(t, oneSidedPayload["coverage"])
		if oneSidedCoverage["requested_from_time"] != oneSided.wantFrom || oneSidedCoverage["requested_to_time"] != oneSided.wantTo {
			t.Fatalf("one-sided bounds changed: %+v", oneSidedCoverage)
		}
	}
}

func TestLoadbalanceEventsBidirectionalKeyset(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-time.Minute)
	for index := 0; index < 5; index++ {
		insertLoadbalanceEvent(t, harness, loadbalanceEventSeed{
			ID: int64(9800 + index), ProfileID: profileID, ConnectionID: 1,
			EventType: "retry_scheduled", FailureKind: stringPtr("timeout"),
			ConsecutiveFailures: 1, CooldownSeconds: 60.0,
			ModelID: stringPtr("keyset-model"), EndpointID: intPtr(12), BanMode: stringPtr("off"),
			CreatedAt: now.Add(-time.Duration(index+1) * time.Minute),
		})
	}
	contextPayload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": "24h"}, http.StatusOK)
	queryContext := url.QueryEscape(fmt.Sprint(contextPayload["query_context"]))
	base := "/api/loadbalance/events?query_context=" + queryContext + "&model_id=keyset-model&sort_order=desc&limit=2"

	// The current registered event surface uses a signed forward cursor and
	// exposes event IDs as decimal strings.
	first := s15GET[map[string]any](t, harness, profileID, base, http.StatusOK)
	firstItems := first["items"].([]any)
	if len(firstItems) != 2 || asMap(t, firstItems[0])["event_id"] != "9800" || asMap(t, firstItems[1])["event_id"] != "9801" {
		t.Fatalf("expected newest-first page 9800/9801, got %+v", first)
	}
	if first["has_more"] != true || first["next_cursor"] == nil {
		t.Fatalf("expected a signed next cursor on the first page, got %+v", first)
	}

	second := s15GET[map[string]any](t, harness, profileID, base+"&cursor="+url.QueryEscape(fmt.Sprint(first["next_cursor"])), http.StatusOK)
	secondItems := second["items"].([]any)
	if len(secondItems) != 2 || asMap(t, secondItems[0])["event_id"] != "9802" || asMap(t, secondItems[1])["event_id"] != "9803" {
		t.Fatalf("expected cursor page 9802/9803, got %+v", second)
	}

	third := s15GET[map[string]any](t, harness, profileID, base+"&cursor="+url.QueryEscape(fmt.Sprint(second["next_cursor"])), http.StatusOK)
	thirdItems := third["items"].([]any)
	if len(thirdItems) != 1 || asMap(t, thirdItems[0])["event_id"] != "9804" || third["has_more"] != false {
		t.Fatalf("expected final cursor page 9804, got %+v", third)
	}
}

// TestS15PricingStatusRequestFilters verifies the four-state pricing_status
// filter and repeatable unpriced_reason OR semantics on the Requests browse
// endpoint (SPEC 9.2). The legacy priced=true|false filter is gone: unknown
// query keys are rejected by the strict list parser.
func TestS15PricingStatusRequestFilters(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now
	window := fmt.Sprintf("from_time=%s&to_time=%s", now.Add(-24*time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339))
	insertUsageEvent(t, harness, usageEventSeed{ID: 901, ProfileID: profileID, IngressRequestID: "ps-priced", ModelID: "ps-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), TotalTokens: intPtr(10), InputTokens: intPtr(5), OutputTokens: intPtr(5), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: now.Add(-1 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 902, ProfileID: profileID, IngressRequestID: "ps-unpriced", ModelID: "ps-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, PricedFlag: boolPtr(false), UnpricedReason: stringPtr("MISSING_TOKEN_USAGE"), TotalTokens: intPtr(0), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: now.Add(-2 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 903, ProfileID: profileID, IngressRequestID: "ps-ineligible", ModelID: "ps-model", APIFamily: "openai", StatusCode: 503, SuccessFlag: false, BillableFlag: boolPtr(false), PricedFlag: boolPtr(false), TotalTokens: intPtr(0), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: now.Add(-3 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 904, ProfileID: profileID, IngressRequestID: "ps-unknown", ModelID: "ps-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, PricedFlag: boolPtr(false), TotalTokens: intPtr(0), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: now.Add(-4 * time.Hour)})
	insertUsageEvent(t, harness, usageEventSeed{ID: 905, ProfileID: profileID, IngressRequestID: "ps-reason2", ModelID: "ps-model", APIFamily: "openai", StatusCode: 200, SuccessFlag: true, PricedFlag: boolPtr(false), UnpricedReason: stringPtr("STREAM_USAGE_UNAVAILABLE"), TotalTokens: intPtr(0), AttemptCount: 1, RequestPath: "/v1/chat/completions", CreatedAt: now.Add(-5 * time.Hour)})

	insertRequestLogRow := func(id int, ingress string, statusCode int, pricingStatus string, trust string, unpricedReason *string, createdAt time.Time) {
		t.Helper()
		ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", createdAt))
		var inputCost, outputCost, reasoningCost, cacheReadCost, cacheCreationCost, totalOriginal, totalUser any
		if pricingStatus == "priced" {
			inputCost, outputCost, reasoningCost, cacheReadCost, cacheCreationCost, totalOriginal, totalUser = int64(2500), int64(10000), int64(0), int64(0), int64(0), int64(12500), int64(12500)
		}
		if _, err := harness.conn.Exec(
			context.Background(),
			`INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, attempt_trigger, attempt_result, is_winner, upstream_status_code, attempt_duration_ms, status_code, response_time_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, unpriced_reason, request_path, created_at, input_cost_micros, output_cost_micros, reasoning_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, total_cost_original_micros, total_cost_user_currency_micros) VALUES ($1, $2, $3, $4, NULL, $5, 1, 'upstream', 'runtime_scrubbed', 'initial', CASE WHEN $6 BETWEEN 200 AND 299 THEN 'completed' ELSE 'http_error' END, TRUE, $6, 120, $6, 120, FALSE, $7, $8, $9, $10, '/v1/chat/completions', $11, $12, $13, $14, $15, $16, $17, $18)`,
			id, profileID, "ps-model", "openai", ingress, statusCode, statusCode >= 200 && statusCode < 300, pricingStatus, trust, unpricedReason, createdAt, inputCost, outputCost, reasoningCost, cacheReadCost, cacheCreationCost, totalOriginal, totalUser,
		); err != nil {
			t.Fatalf("insert request log %d: %v", id, err)
		}
	}
	insertRequestLogRow(1901, "ps-priced", 200, "priced", "trusted", nil, now.Add(-1*time.Hour))
	insertRequestLogRow(1902, "ps-unpriced", 200, "unpriced", "trusted", stringPtr("MISSING_TOKEN_USAGE"), now.Add(-2*time.Hour))
	insertRequestLogRow(1903, "ps-ineligible", 503, "ineligible", "trusted", nil, now.Add(-3*time.Hour))
	insertRequestLogRow(1904, "ps-unknown", 200, "unknown", "legacy_untrusted", nil, now.Add(-4*time.Hour))
	insertRequestLogRow(1905, "ps-reason2", 200, "unpriced", "trusted", stringPtr("STREAM_USAGE_UNAVAILABLE"), now.Add(-5*time.Hour))

	assertFilterIDs := func(query string, wantIDs ...int64) {
		t.Helper()
		payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?limit=100&"+window+"&"+query, http.StatusOK)
		items := payload["items"].([]any)
		t.Logf("query %q -> %d items", query, len(items))
		for _, item := range items {
			m := asMap(t, item)
			t.Logf("  item id=%v status=%v", m["id"], m["pricing_status"])
		}
		got := make(map[int64]bool)
		for _, item := range items {
			requestLogID, err := strconv.ParseInt(asMap(t, item)["request_log_id"].(string), 10, 64)
			if err != nil {
				t.Fatalf("parse request_log_id: %v", err)
			}
			got[requestLogID] = true
		}
		if len(got) != len(wantIDs) {
			t.Fatalf("query %q: expected %d rows (%v), got %d (%v)", query, len(wantIDs), wantIDs, len(got), got)
		}
		for _, id := range wantIDs {
			if !got[id] {
				t.Fatalf("query %q: expected id %d present, got %v", query, id, got)
			}
		}
	}

	allPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?limit=100&"+window, http.StatusOK)
	t.Logf("payload=%v", allPayload)
	for _, item := range allPayload["items"].([]any) {
		m := asMap(t, item)
		t.Logf("  all item id=%v status=%v reason=%v", m["id"], m["pricing_status"], m["unpriced_reason"])
	}
	assertFilterIDs("pricing_status=priced", 1901)
	assertFilterIDs("pricing_status=unpriced", 1902, 1905)
	assertFilterIDs("pricing_status=ineligible", 1903)
	assertFilterIDs("pricing_status=unknown", 1904)
	assertFilterIDs("pricing_status=unpriced&unpriced_reason=MISSING_TOKEN_USAGE", 1902)
	assertFilterIDs("pricing_status=unpriced&unpriced_reason=MISSING_TOKEN_USAGE&unpriced_reason=STREAM_USAGE_UNAVAILABLE", 1902, 1905)

	// Unknown four-state value is a typed 422; invalid reason remains a 400
	// value parse error.
	s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?pricing_status=maybe", http.StatusUnprocessableEntity)
	s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?unpriced_reason=INVALID_REASON", http.StatusBadRequest)
}

package contracttest

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

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

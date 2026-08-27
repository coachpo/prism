package contracttest

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestModelMetricsSeparatesIngressFinalExecutionAndRouteAttempt is the
// A -> B(503) -> C(200) acceptance fixture. One served-final cost is projected
// independently by ingress and final_execution; route_attempt never claims it.
func TestModelMetricsSeparatesIngressFinalExecutionAndRouteAttempt(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	createdAt := fixedS15Now.Add(-time.Hour)
	endpointB, endpointC, connectionB, connectionC := 701, 702, 801, 802
	insertRequestLogSummaryRow(t, harness, 9701, profileID, "A", "openai", endpointB, connectionB, http.StatusServiceUnavailable, 40, 0, 0, 0, createdAt)
	insertRequestLogSummaryRow(t, harness, 9702, profileID, "A", "openai", endpointC, connectionC, http.StatusOK, 120, 0, 0, 0, createdAt.Add(time.Second))
	for _, update := range []struct {
		id       int
		target   string
		attempt  int
		result   string
		winner   bool
		duration int
	}{
		{9701, "B", 1, "http_error", false, 40},
		{9702, "C", 2, "completed", true, 120},
	} {
		if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET
			resolved_target_model_id=$1, ingress_request_id='scope-chain', attempt_number=$2,
			attempt_result=$3, is_winner=$4, attempt_duration_ms=$5, upstream_status_code=legacy_status_code,
			legacy_status_code=NULL, legacy_duration_ms=NULL, row_kind='upstream', url_scrub_provenance='runtime_scrubbed'
			WHERE profile_id=$6 AND id=$7`, update.target, update.attempt, update.result, update.winner, update.duration, profileID, update.id); err != nil {
			t.Fatalf("update request attempt %d: %v", update.id, err)
		}
	}
	cost := int64(2500)
	insertUsageEvent(t, harness, usageEventSeed{ID: 9801, ProfileID: profileID, IngressRequestID: "scope-chain", ModelID: "A", APIFamily: "openai", StatusCode: http.StatusOK, SuccessFlag: true, EndpointID: &endpointC, ConnectionID: &connectionC, InputTokens: intPtr(10), OutputTokens: intPtr(20), TotalTokens: intPtr(30), TotalCostUserCurrencyMicros: &cost, BillableFlag: boolPtr(true), PricedFlag: boolPtr(true), AttemptCount: 2, RequestPath: "/v1/chat/completions", CreatedAt: createdAt.Add(time.Second), ResponseTimeMS: intPtr(200)})
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET resolved_target_model_id='C', final_attempt_number=2, stream_outcome='not_streaming' WHERE profile_id=$1 AND id=9801`, profileID); err != nil {
		t.Fatalf("update finalized usage event: %v", err)
	}

	payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, "/api/stats/models/metrics", map[string]any{"model_ids": []string{"A", "B", "C"}, "summary_window_hours": 24, "spending_preset": "last_30_days"}, http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %#v", items)
	}
	byID := map[string]map[string]any{}
	for _, raw := range items {
		item := asMap(t, raw)
		byID[item["model_id"].(string)] = item
	}
	if ingress := asMap(t, byID["A"]["ingress"]); jsonInt(t, ingress["request_count"]) != 1 || jsonInt(t, ingress["known_cost_micros"]) != int(cost) {
		t.Fatalf("A ingress = %#v", ingress)
	}
	if final := asMap(t, byID["C"]["final_execution"]); jsonInt(t, final["request_count"]) != 1 || jsonInt(t, final["p95_latency_ms"]) != 120 || jsonInt(t, final["known_cost_micros"]) != int(cost) {
		t.Fatalf("C final_execution = %#v", final)
	}
	if attempt := asMap(t, byID["B"]["route_attempt"]); jsonInt(t, attempt["request_count"]) != 1 || attempt["known_cost_micros"] != nil {
		t.Fatalf("B route_attempt = %#v", attempt)
	}
	if attempt := asMap(t, byID["C"]["route_attempt"]); jsonInt(t, attempt["request_count"]) != 1 || jsonInt(t, attempt["p95_latency_ms"]) != 120 || attempt["known_cost_micros"] != nil {
		t.Fatalf("C route_attempt = %#v", attempt)
	}
	coverage := asMap(t, payload["coverage"])
	if coverage["quality"] == nil || coverage["spending"] == nil {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestStatsSummaryRejectsRetiredAmbiguousKeys(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	for _, path := range []string{
		"/api/stats/summary?scope=ingress&group_by=model",
		"/api/stats/summary?scope=final_execution&model_id=C",
		"/api/stats/summary?scope=route_attempt&resolved_target_model_id=C",
	} {
		s15GET[map[string]any](t, harness, profileID, path, http.StatusUnprocessableEntity)
	}
}

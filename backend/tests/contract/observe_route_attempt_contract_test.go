package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func insertObserveAttemptRow(t *testing.T, harness *contractHarness, profileID int, sequence int, createdAt time.Time, endpointID int, result *string, statusCode *int, streamOutcome string) {
	t.Helper()
	if streamOutcome == "" {
		streamOutcome = "not_streaming"
	}
	_, err := harness.conn.Exec(context.Background(), `
		INSERT INTO request_logs (
			profile_id, model_id, resolved_target_model_id, api_family, endpoint_id,
			ingress_request_id, attempt_number, attempt_trigger, attempt_result,
			row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms,
			is_stream, stream_outcome, success_flag, pricing_status, pricing_evidence_trust,
			request_path, endpoint_description, created_at, audit_enabled_at_request, audit_capture_bodies_at_request
		) VALUES ($1, 'route-attempt-entry', 'route-attempt-target', 'openai', $2,
			$3, 1, 'initial', $4::varchar, 'upstream', 'runtime_scrubbed', $5, $6,
			$7, $8, $4::varchar = 'completed', 'ineligible', 'trusted', '/v1/chat/completions', $9, $10, FALSE, FALSE)`,
		profileID, endpointID, fmt.Sprintf("observe-attempt-%d", sequence), result, statusCode, 10+sequence,
		streamOutcome != "not_streaming", streamOutcome, fmt.Sprintf("Retained Endpoint %d", endpointID), createdAt.UTC(),
	)
	if err != nil {
		t.Fatalf("insert route-attempt row %d: %v", sequence, err)
	}
}

func observeAttemptContext(t *testing.T, harness *contractHarness, profileID int) string {
	t.Helper()
	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h&scope=route_attempt", nil, http.StatusOK)
	return payload["query_context"].(string)
}

func TestObserveRouteAttemptErrorsClassifyAndReplayWithoutNullMaps(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-2 * time.Minute)
	results := []*string{
		stringPtr("completed"), stringPtr("cancelled"), stringPtr("http_error"), stringPtr("stream_error"),
		stringPtr("transport_error"), stringPtr("client_disconnected"), stringPtr("unknown"), nil,
	}
	statuses := []*int{intPtr(200), nil, intPtr(503), intPtr(200), nil, intPtr(200), nil, nil}
	streams := []string{"not_streaming", "not_streaming", "not_streaming", "provider_incomplete", "not_streaming", "client_disconnected", "unknown", "not_streaming"}
	for index := range results {
		insertObserveAttemptRow(t, harness, profileID, index+1, now.Add(time.Duration(index)*time.Second), 800+index, results[index], statuses[index], streams[index])
	}

	token := observeAttemptContext(t, harness, profileID)
	scopedSummary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-summary?query_context="+token, nil, http.StatusOK)
	if asMap(t, scopedSummary["caliber"])["scope"] != "route_attempt" || scopedSummary["known_cost_micros"] != nil {
		t.Fatalf("route-attempt summary claimed the wrong caliber or cost: %+v", scopedSummary)
	}
	if segments, ok := scopedSummary["cost_segments"].([]any); !ok || len(segments) != 0 {
		t.Fatalf("route-attempt summary cost_segments must be a non-null empty list: %+v", scopedSummary["cost_segments"])
	}
	assertJSONIntFields(t, scopedSummary, map[string]int{
		"request_count": 8, "http_failed_count": 1, "stream_error_count": 1,
		"transport_error_count": 1, "client_disconnected_count": 1, "failed_count": 5,
	})
	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-errors?query_context="+token+"&group_by=attempt_result&limit=2", nil, http.StatusOK)
	summary := asMap(t, payload["summary"])
	assertJSONIntFields(t, summary, map[string]int{
		"request_count": 8, "http_error_count": 1, "stream_error_count": 1,
		"transport_error_count": 1, "client_disconnected_count": 1, "failed_count": 5,
	})
	requestsContext := asMap(t, payload["requests_context"])
	if requestsContext["view"] != "attempts" {
		t.Fatalf("expected attempts replay context, got %+v", requestsContext)
	}
	baseFilters := asMap(t, requestsContext["base_request_filters"])
	if values, ok := baseFilters["row_kind"].([]any); !ok || len(values) != 1 || values[0] != "upstream" {
		t.Fatalf("expected non-null upstream replay filter, got %+v", baseFilters)
	}
	for _, field := range []string{"http_statuses", "stream_outcomes", "groups"} {
		for _, raw := range payload[field].([]any) {
			if asMap(t, raw)["request_filters"] == nil {
				t.Fatalf("%s contains null request_filters: %+v", field, raw)
			}
		}
		if asMap(t, asMap(t, payload["other"])[field])["request_filters"] == nil {
			t.Fatalf("other.%s contains null request_filters", field)
		}
	}
}

func TestObserveRouteAttemptSeriesConservesOtherAndExactBoundary(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-2 * time.Minute)
	endpointA := modelInsertEndpoint(t, harness, profileID, "Attempt Endpoint A")
	endpointB := modelInsertEndpoint(t, harness, profileID, "Attempt Endpoint B")
	for index, endpointID := range []int{endpointA, endpointB} {
		insertObserveAttemptRow(t, harness, profileID, index+1, now.Add(time.Duration(index)*time.Second), endpointID, stringPtr("completed"), intPtr(200), "not_streaming")
	}
	token := observeAttemptContext(t, harness, profileID)
	exact := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-series?query_context="+token+"&metric=attempts&group_by=endpoint&series_limit=3", nil, http.StatusOK)
	if exact["truncated"] != false || len(exact["series"].([]any)) != 2 {
		t.Fatalf("exact visible boundary was reported truncated: %+v", exact)
	}
	for _, raw := range exact["series"].([]any) {
		item := asMap(t, raw)
		if item["label"] == fmt.Sprintf("%v", item["entity_id"]) {
			t.Fatalf("endpoint series exposed a bare id: %+v", item)
		}
	}

	for index, result := range []string{"http_error", "stream_error", "transport_error", "client_disconnected", "unknown"} {
		insertObserveAttemptRow(t, harness, profileID, index+10, now.Add(time.Duration(index+10)*time.Second), endpointA, stringPtr(result), intPtr(500+index), "not_streaming")
	}
	truncated := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-series?query_context="+token+"&metric=attempts&group_by=attempt_result&series_limit=3", nil, http.StatusOK)
	if truncated["truncated"] != true {
		t.Fatalf("expected a real remainder witness, got %+v", truncated)
	}
	total := 0
	foundOther := false
	for _, raw := range truncated["series"].([]any) {
		item := asMap(t, raw)
		total += jsonInt(t, item["request_count"])
		if item["key"] == "other" {
			foundOther = true
			if item["entity_id"] != nil {
				t.Fatalf("Other must not carry entity_id: %+v", item)
			}
		}
	}
	if total != 7 || !foundOther {
		t.Fatalf("route-attempt series did not conserve all 7 attempts: total=%d payload=%+v", total, truncated)
	}
}

func TestEndpointModelStatisticsPublishesCompleteFinalEnvelope(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Historical Endpoint Stats")
	seedObserveUsageRows(t, harness, profileID, []map[string]any{
		{"seq": 1, "model_id": "endpoint-stat-entry", "pricing_status": "priced", "total_cost_user_currency_micros": int64(1200000)},
		{"seq": 2, "model_id": "endpoint-stat-entry", "pricing_status": "priced", "total_cost_user_currency_micros": int64(0)},
	})
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events
		SET endpoint_id=$1, endpoint_label_snapshot='Retained Endpoint Stats', resolved_target_model_id='endpoint-stat-target', final_attempt_number=1
		WHERE profile_id=$2 AND model_id='endpoint-stat-entry'`, endpointID, profileID); err != nil {
		t.Fatalf("attribute endpoint usage: %v", err)
	}
	now := fixedS15Now.Add(-2 * time.Minute)
	for index, ingressID := range []string{"ingress-2-1", "ingress-2-2"} {
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (
			profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, ingress_request_id, attempt_number,
			attempt_trigger, attempt_result, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms,
			is_stream, stream_outcome, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at,
			audit_enabled_at_request, audit_capture_bodies_at_request)
			VALUES ($1, 'endpoint-stat-entry', 'endpoint-stat-target', 'openai', $2, $3, 1,
			'initial', 'completed', 'upstream', 'runtime_scrubbed', 200, $4,
			FALSE, 'not_streaming', TRUE, 'ineligible', 'trusted', '/v1/chat/completions', $5, FALSE, FALSE)`,
			profileID, endpointID, ingressID, 100+index*200, now.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatalf("insert final-attempt duration: %v", err)
		}
	}

	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		fmt.Sprintf("/api/stats/endpoints/%d/models?preset=24h&scope=final_execution", endpointID), nil, http.StatusOK)
	if payload["scope"] != "final_execution" || asMap(t, payload["caliber"])["cost_basis"] != "served_final_trusted_cost" {
		t.Fatalf("unexpected endpoint-model envelope: %+v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one target model item, got %+v", items)
	}
	item := asMap(t, items[0])
	assertJSONIntFields(t, item, map[string]int{
		"request_count": 2, "success_count": 2, "failed_count": 0,
		"priced_request_count": 2, "unpriced_request_count": 0,
		"p50_ttft_ms": 200, "p95_ttft_ms": 290, "total_tokens": 2000,
	})
	if item["known_cost_micros"] != float64(1200000) || item["avg_output_rate_tps"] == nil {
		t.Fatalf("cost/output-rate fields were not populated: %+v", item)
	}
	itemSamples := asMap(t, item["samples"])
	assertJSONIntFields(t, itemSamples, map[string]int{"latency_sample_count": 2, "cost_sample_count": 2})

	statsSummary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		fmt.Sprintf("/api/stats/summary?preset=24h&scope=final_execution&endpoint_id=%d", endpointID), nil, http.StatusOK)
	assertJSONIntFields(t, asMap(t, statsSummary["samples"]), map[string]int{
		"observation_count": 2, "latency_sample_count": 2, "cost_sample_count": 2, "cost_missing_count": 0,
	})
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodGet,
		"/api/stats/summary?scope=ingress&proxy_api_key_id=7", nil, modelHeader(profileID)),
		http.StatusUnprocessableEntity, `filter "proxy_api_key_id" is not supported by summary for scope "ingress"`)
}

func TestPublicStatsWindowsAreHalfOpen(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	to := fixedS15Now.Add(-time.Minute).UTC().Truncate(time.Second)
	from := to.Add(-time.Minute)
	seedObserveUsageRows(t, harness, profileID, []map[string]any{
		{"seq": 1, "created_at": to.Add(-time.Nanosecond), "pricing_status": "priced"},
		{"seq": 2, "created_at": to, "pricing_status": "priced"},
	})
	insertObserveAttemptRow(t, harness, profileID, 1, to.Add(-time.Nanosecond), 901, stringPtr("completed"), intPtr(200), "not_streaming")
	insertObserveAttemptRow(t, harness, profileID, 2, to, 901, stringPtr("completed"), intPtr(200), "not_streaming")
	window := "preset=custom&from_time=" + from.Format(time.RFC3339Nano) + "&to_time=" + to.Format(time.RFC3339Nano)

	summary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/summary?scope=ingress&"+window, nil, http.StatusOK)
	if jsonInt(t, summary["total_requests"]) != 1 {
		t.Fatalf("usage row exactly at to_time leaked into [from,to): %+v", summary)
	}
	throughput := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/throughput?scope=route_attempt&"+window, nil, http.StatusOK)
	if jsonInt(t, throughput["total_requests"]) != 1 {
		t.Fatalf("attempt row exactly at to_time leaked into [from,to): %+v", throughput)
	}
}

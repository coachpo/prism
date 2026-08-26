package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

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
	if summary["model_id"] != "partition-new" || jsonInt(t, summary["legacy_status_code"]) != 500 || jsonInt(t, summary["legacy_duration_ms"]) != 222 {
		t.Fatalf("expected newest duplicate request-log id for Default profile, got %+v", detailPayload)
	}
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

func TestRequestLogsHalfOpenTimeBounds(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	// Row exactly at `to` must belong to the next window (half-open [from, to)).
	boundary := fixedS15Now.Add(-30 * time.Minute)
	insertRequestLogSummaryRow(t, harness, 9500, profileID, "half-open-model", "openai", 12, 91, 200, 100, 0, 0, 0, boundary.Add(-time.Minute))
	insertRequestLogSummaryRow(t, harness, 9501, profileID, "half-open-model", "openai", 12, 91, 200, 100, 0, 0, 0, boundary)
	insertRequestLogSummaryRow(t, harness, 9502, profileID, "half-open-model", "openai", 12, 91, 200, 100, 0, 0, 0, boundary.Add(time.Minute))

	windowPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?from_time="+url.QueryEscape(boundary.Add(-2*time.Minute).Format(time.RFC3339))+"&to_time="+url.QueryEscape(boundary.Format(time.RFC3339))+"&model_id=half-open-model&limit=50", http.StatusOK)
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

	nextPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?from_time="+url.QueryEscape(boundary.Format(time.RFC3339))+"&to_time="+url.QueryEscape(boundary.Add(2*time.Minute).Format(time.RFC3339))+"&model_id=half-open-model&limit=50", http.StatusOK)
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

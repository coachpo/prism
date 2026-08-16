package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestAttemptViewServerSideSorting verifies the attempt-view sort grammar:
// sort_by/sort_order belong to the accepted query grammar (they are not
// unknown keys), the server reorders rows for every sortable key, rows with no
// value for the selected key sort last in both directions, and an unsupported
// sort is a typed 422 instead of a silent created_at fallback.
func TestAttemptViewServerSideSorting(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now
	window := fmt.Sprintf("from_time=%s&to_time=%s", now.Add(-24*time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339))

	insertSortRow := func(id int, statusCode int, ttftMS any, totalTokens any, costMicros any, createdAt time.Time) {
		t.Helper()
		ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", createdAt))
		pricingStatus, trust, attemptResult := "unknown", "legacy_untrusted", "http_error"
		// A priced row must carry the whole canonical cost tuple
		// (pricing_costs_coherence_check); an unpriced row carries none of it.
		var componentCost any
		if costMicros != nil {
			pricingStatus, trust, componentCost = "priced", "trusted", int64(0)
		}
		if statusCode >= 200 && statusCode < 300 {
			attemptResult = "completed"
		}
		if _, err := harness.conn.Exec(
			context.Background(),
			`INSERT INTO request_logs (id, profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, attempt_trigger, attempt_result, is_winner, upstream_status_code, attempt_duration_ms, status_code, response_time_ms, ttft_ms, is_stream, success_flag, total_tokens, pricing_status, pricing_evidence_trust, input_cost_micros, output_cost_micros, reasoning_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, request_path, created_at)
			 VALUES ($1, $2, 'sort-model', 'openai', $3, 1, 'upstream', 'runtime_scrubbed', 'initial', $4, TRUE, $5, 120, $5, 120, $6, FALSE, $7, $8, $9, $10, $11, $11, $11, $11, $11, $12, $12, '/v1/chat/completions', $13)`,
			id, profileID, fmt.Sprintf("sort-%d", id), attemptResult, statusCode, ttftMS, statusCode >= 200 && statusCode < 300, totalTokens, pricingStatus, trust, componentCost, costMicros, createdAt,
		); err != nil {
			t.Fatalf("insert sort row %d: %v", id, err)
		}
	}

	// Oldest row is the fastest-but-cheapest; the newest row carries no TTFT,
	// no tokens and no cost so the NULLS-last rule is observable.
	insertSortRow(3001, 200, 300, 50, int64(5000), now.Add(-5*time.Minute))
	insertSortRow(3002, 500, 100, 900, int64(100), now.Add(-4*time.Minute))
	insertSortRow(3003, 429, nil, nil, nil, now.Add(-3*time.Minute))

	assertOrder := func(query string, wantIDs ...string) {
		t.Helper()
		payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?limit=100&"+window+"&"+query, http.StatusOK)
		items := payload["items"].([]any)
		got := make([]string, 0, len(items))
		for _, item := range items {
			got = append(got, asMap(t, item)["request_log_id"].(string))
		}
		if len(got) != len(wantIDs) {
			t.Fatalf("query %q: expected %v, got %v", query, wantIDs, got)
		}
		for index, want := range wantIDs {
			if got[index] != want {
				t.Fatalf("query %q: expected %v, got %v", query, wantIDs, got)
			}
		}
	}

	// The attempt view is the failing surface when sort_by is not part of the
	// grammar: an explicit view=attempts query with the default sort must load.
	assertOrder("view=attempts&sort_by=created_at&sort_order=desc", "3003", "3002", "3001")
	assertOrder("", "3003", "3002", "3001")
	assertOrder("sort_by=created_at&sort_order=asc", "3001", "3002", "3003")

	// Rows without a value for the selected key stay last in both directions.
	assertOrder("view=attempts&sort_by=ttft_ms&sort_order=asc", "3002", "3001", "3003")
	assertOrder("view=attempts&sort_by=ttft_ms&sort_order=desc", "3001", "3002", "3003")
	assertOrder("view=attempts&sort_by=total_tokens&sort_order=desc", "3002", "3001", "3003")
	assertOrder("view=attempts&sort_by=total_cost_user_currency_micros&sort_order=desc", "3001", "3002", "3003")

	// display_status sorts on the row-scoped status, not the legacy column.
	assertOrder("view=attempts&sort_by=display_status&sort_order=desc", "3002", "3003", "3001")
	assertOrder("view=attempts&sort_by=display_status&sort_order=asc", "3001", "3003", "3002")

	// Unsupported sorts are typed rejections, never a quiet created_at fallback.
	s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=attempts&sort_by=endpoint_label", http.StatusUnprocessableEntity)
	s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=attempts&sort_by=ttft_ms&sort_order=sideways", http.StatusUnprocessableEntity)
}

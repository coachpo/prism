package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// seedObserveUsageRows inserts finalized usage events with explicit pricing
// facts so read models can be asserted without runtime execution.
func seedObserveUsageRows(t *testing.T, harness *contractHarness, profileID int, rows []map[string]any) {
	t.Helper()
	now := fixedS15Now.Add(-2 * time.Minute)
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", fixedS15Now))
	for _, row := range rows {
		createdAt := now
		if value, ok := row["created_at"].(time.Time); ok {
			createdAt = value
		}
		modelID := "observe-model"
		if value, ok := row["model_id"].(string); ok {
			modelID = value
		}
		apiFamily := "openai"
		if value, ok := row["api_family"].(string); ok {
			apiFamily = value
		}
		statusCode := 200
		if value, ok := row["status_code"].(int); ok {
			statusCode = value
		}
		streamOutcome := "not_streaming"
		if value, ok := row["stream_outcome"].(string); ok {
			streamOutcome = value
		}
		var streamErrorKind any
		if value, ok := row["stream_error_kind"]; ok {
			streamErrorKind = value
		}
		pricingStatus := "priced"
		if value, ok := row["pricing_status"].(string); ok {
			pricingStatus = value
		}
		pricingEvidenceTrust := "trusted"
		if value, ok := row["pricing_evidence_trust"].(string); ok {
			pricingEvidenceTrust = value
		}
		var pricingResolutionKind any
		if value, ok := row["pricing_resolution_kind"]; ok {
			pricingResolutionKind = value
		}
		var unpricedReason any
		if value, ok := row["unpriced_reason"]; ok {
			unpricedReason = value
		}
		ttft := 100
		if value, ok := row["ttft_ms"].(int); ok {
			ttft = value
		}
		operationName := any("openai.chat_completions")
		if value, ok := row["operation_name"]; ok {
			operationName = value
		}
		var inputTokens, cacheReadTokens, cacheCreationTokens any
		if value, ok := row["input_tokens"].(int); ok {
			inputTokens = value
		}
		if value, ok := row["cache_read_input_tokens"].(int); ok {
			cacheReadTokens = value
		}
		if value, ok := row["cache_creation_input_tokens"].(int); ok {
			cacheCreationTokens = value
		}
		totalTokens := 1000
		if value, ok := row["total_tokens"].(int); ok {
			totalTokens = value
		}
		outputTokens := 500
		if value, ok := row["output_tokens"].(int); ok {
			outputTokens = value
		}
		completionDuration := 2000
		cost := int64(1200000)
		if value, ok := row["total_cost_user_currency_micros"].(int64); ok {
			cost = value
		}
		reportCurrencyCode, reportCurrencySymbol, reportingCurrencyEpoch := any("USD"), any("$"), any(1)
		if value, ok := row["report_currency_code"]; ok {
			reportCurrencyCode = value
		}
		if value, ok := row["report_currency_symbol"]; ok {
			reportCurrencySymbol = value
		}
		if value, ok := row["reporting_currency_epoch"]; ok {
			reportingCurrencyEpoch = value
		}
		// pricing_costs_coherence_check requires all-or-none costs: only priced
		// rows may carry cost micros; ineligible/unpriced rows keep NULLs.
		var costValue any
		if pricingStatus == "priced" {
			costValue = cost
		}
		if _, err := harness.conn.Exec(context.Background(), `
			INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, operation_name, status_code, success_flag,
				ttft_ms, completion_duration_ms, output_tokens, total_tokens, input_tokens, cache_read_input_tokens, cache_creation_input_tokens,
				total_cost_user_currency_micros, report_currency_code, report_currency_symbol,
				stream_outcome, stream_error_kind, pricing_status, unpriced_reason, pricing_evidence_trust, reporting_currency_epoch, pricing_resolution_kind,
				input_cost_micros, output_cost_micros, reasoning_cost_micros,
				cache_read_input_cost_micros, cache_creation_input_cost_micros, total_cost_original_micros,
				output_rate_state, output_delivery_event_count, output_delivery_span_ms,
				attempt_count, request_path, endpoint_label_snapshot, created_at)
			VALUES ($1, $2, $3, $25, $21, $4, $5, $6, $7, $8, $9, $22, $23, $24, $15, $16, $17, $10, $11, $12, $13, $19, $18, $20,
				$15, $15, $15, $15, $15, $15,
				'measured', 2, $7::int - $6::int,
				1, '/v1/chat/completions', 'Observe Endpoint', $14)`,
			profileID,
			fmt.Sprintf("ingress-%d-%v", len(rows), row["seq"]),
			modelID,
			statusCode,
			statusCode >= 200 && statusCode <= 299,
			ttft,
			completionDuration,
			outputTokens,
			totalTokens,
			streamOutcome,
			streamErrorKind,
			pricingStatus,
			unpricedReason,
			createdAt,
			costValue,
			reportCurrencyCode,
			reportCurrencySymbol,
			reportingCurrencyEpoch,
			pricingEvidenceTrust,
			pricingResolutionKind,
			operationName,
			inputTokens,
			cacheReadTokens,
			cacheCreationTokens,
			apiFamily,
		); err != nil {
			t.Fatalf("seed usage row: %v", err)
		}
	}
}

func TestObserveQueryContextAndUsageSummary(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	// 24-row pricing fixture: 17 ineligible, 5 STREAM_USAGE_UNAVAILABLE,
	// 2 MISSING_TOKEN_USAGE, 0 PRICING_DISABLED, 0 MISSING_PRICE_DATA,
	// 18 priced (25 eligible = 18 priced + 7 unpriced), 0 unknown.
	rows := make([]map[string]any, 0, 42)
	for i := 0; i < 17; i++ {
		rows = append(rows, map[string]any{"seq": i, "status_code": 503, "pricing_status": "ineligible"})
	}
	for i := 0; i < 5; i++ {
		rows = append(rows, map[string]any{"seq": 100 + i, "status_code": 200, "pricing_status": "unpriced", "unpriced_reason": "STREAM_USAGE_UNAVAILABLE", "stream_outcome": "provider_incomplete", "ttft_ms": 1500})
	}
	for i := 0; i < 2; i++ {
		rows = append(rows, map[string]any{"seq": 200 + i, "status_code": 200, "pricing_status": "unpriced", "unpriced_reason": "MISSING_TOKEN_USAGE"})
	}
	for i := 0; i < 18; i++ {
		row := map[string]any{"seq": 300 + i, "status_code": 200, "pricing_status": "priced", "total_cost_user_currency_micros": int64(1200000)}
		if i >= 8 && i < 17 {
			row["ttft_ms"] = 201
		} else if i == 17 {
			row["ttft_ms"] = 300
		}
		rows = append(rows, row)
	}
	seedObserveUsageRows(t, harness, profileID, rows)

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token, ok := contextPayload["query_context"].(string)
	if !ok || token == "" {
		t.Fatalf("expected query_context token, got %+v", contextPayload)
	}
	usageBounds := asMap(t, contextPayload["usage_bounds"])
	if usageBounds["from_time"] == nil || usageBounds["to_time"] == nil {
		t.Fatalf("expected usage bounds, got %+v", contextPayload)
	}
	coverage := asMap(t, contextPayload["usage_coverage"])
	if coverage["requested_preset"] != "24h" || coverage["source"] != "raw" {
		t.Fatalf("expected 24h raw coverage, got %+v", coverage)
	}

	summary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-summary?query_context="+token, nil, http.StatusOK)
	if jsonInt(t, summary["request_count"]) != 42 {
		t.Fatalf("expected 42 requests, got %+v", summary)
	}
	if jsonInt(t, summary["http_failed_count"]) != 17 || jsonInt(t, summary["http_success_count"]) != 25 {
		t.Fatalf("expected 17 failed / 25 success, got %+v", summary)
	}
	if jsonInt(t, summary["failed_count"]) != 22 {
		t.Fatalf("expected failed_count 22 (17 http + 5 stream), got %+v", summary)
	}
	if jsonInt(t, summary["stream_error_count"]) != 5 || jsonInt(t, summary["client_disconnected_count"]) != 0 || jsonInt(t, summary["completed_count"]) != 20 {
		t.Fatalf("unexpected outcome counts, got %+v", summary)
	}
	reconciliation := asMap(t, summary["pricing_reconciliation"])
	if jsonInt(t, reconciliation["pricing_eligible_request_count"]) != 25 || jsonInt(t, reconciliation["pricing_ineligible_request_count"]) != 17 ||
		jsonInt(t, reconciliation["priced_request_count"]) != 18 || jsonInt(t, reconciliation["unpriced_request_count"]) != 7 || jsonInt(t, reconciliation["pricing_unknown_request_count"]) != 0 {
		t.Fatalf("unexpected pricing reconciliation, got %+v", reconciliation)
	}
	reasons := asMap(t, reconciliation["unpriced_reason_counts"])
	if jsonInt(t, reasons["STREAM_USAGE_UNAVAILABLE"]) != 5 || jsonInt(t, reasons["MISSING_TOKEN_USAGE"]) != 2 ||
		jsonInt(t, reasons["PRICING_DISABLED"]) != 0 || jsonInt(t, reasons["MISSING_PRICE_DATA"]) != 0 {
		t.Fatalf("unexpected reason counts, got %+v", reasons)
	}
	if reconciliation["pricing_coverage_state"] != "partial" {
		t.Fatalf("expected partial coverage, got %+v", reconciliation)
	}
	segments := summary["cost_segments"].([]any)
	if len(segments) != 1 {
		t.Fatalf("expected one cost segment, got %+v", summary["cost_segments"])
	}
	segment := asMap(t, segments[0])
	if segment["segment_key"] != "e.1" || segment["known_cost_micros"] != "21600000" {
		t.Fatalf("expected e.1 segment with 18 * 1200000 cost, got %+v", segment)
	}
	// TTFT sample counts: 25 eligible - 7 unpriced rows with ttft (5 stream + 2 missing usage have ttft set) ...
	// 18 priced (ttft 100) + 7 unpriced (ttft 1500 or missing) = 25 completed-only samples exclude stream_error rows.
	// completed rows: 18 priced + 2 missing-usage = 20 rows with ttft; stream_error rows excluded.
	if jsonInt(t, summary["ttft_sample_count"]) != 20 {
		t.Fatalf("expected 20 TTFT samples (completed only), got %+v", summary)
	}
	if p50, ok := summary["p50_ttft_ms"].(float64); !ok || p50 != 151 {
		t.Fatalf("expected rounded p50 ttft 151, got %+v", summary["p50_ttft_ms"])
	}
	if p95, ok := summary["p95_ttft_ms"].(float64); !ok || p95 != 206 {
		t.Fatalf("expected rounded p95 ttft 206, got %+v", summary["p95_ttft_ms"])
	}
}

func observeUsageSummarySegmentRows() []map[string]any {
	rows := make([]map[string]any, 0, 19)
	for index, symbol := range []string{"$", "US$", "A$", "B$", "C$", "D$", "E$", "F$", "G$"} {
		rows = append(rows, map[string]any{"seq": 900 + index, "pricing_status": "priced", "total_cost_user_currency_micros": int64(index + 1), "reporting_currency_epoch": 10, "report_currency_code": "USD", "report_currency_symbol": symbol})
	}
	return append(rows,
		map[string]any{"seq": 920, "pricing_status": "priced", "total_cost_user_currency_micros": int64(100), "reporting_currency_epoch": 2, "report_currency_code": "EUR", "report_currency_symbol": "€"},
		map[string]any{"seq": 921, "pricing_status": "priced", "total_cost_user_currency_micros": int64(200), "reporting_currency_epoch": 1, "report_currency_code": "JPY", "report_currency_symbol": "¥"},
		map[string]any{"seq": 922, "pricing_status": "unpriced", "unpriced_reason": "PRICING_DISABLED", "reporting_currency_epoch": nil, "report_currency_code": "GBP", "report_currency_symbol": "£"},
		map[string]any{"seq": 926, "pricing_status": "unpriced", "unpriced_reason": "MISSING_TOKEN_USAGE", "reporting_currency_epoch": nil, "report_currency_code": "GBP", "report_currency_symbol": "£"},
		map[string]any{"seq": 927, "pricing_status": "unpriced", "unpriced_reason": "STREAM_USAGE_UNAVAILABLE", "reporting_currency_epoch": nil, "report_currency_code": "GBP", "report_currency_symbol": "£"},
		map[string]any{"seq": 928, "pricing_status": "unpriced", "unpriced_reason": "MISSING_PRICE_DATA", "pricing_resolution_kind": "unsupported_unit", "reporting_currency_epoch": nil, "report_currency_code": "GBP", "report_currency_symbol": "£"},
		map[string]any{"seq": 929, "status_code": 503, "pricing_status": "ineligible", "reporting_currency_epoch": nil, "report_currency_code": "GBP", "report_currency_symbol": "£"},
		map[string]any{"seq": 923, "pricing_status": "priced", "total_cost_user_currency_micros": int64(500), "reporting_currency_epoch": nil, "report_currency_code": "US", "report_currency_symbol": nil},
		map[string]any{"seq": 924, "pricing_status": "unknown", "pricing_evidence_trust": "legacy_untrusted", "reporting_currency_epoch": nil, "report_currency_code": nil, "report_currency_symbol": nil},
		map[string]any{"seq": 925, "pricing_status": "priced", "total_cost_user_currency_micros": int64(900), "reporting_currency_epoch": 99, "created_at": fixedS15Now.Add(time.Minute)},
	)
}

func TestObserveUsageSummaryCanonicalCostSegments(t *testing.T) {
	harness := newS15ContractHarness(t)
	var hasSelectionState, hasTemplateKind bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='usage_request_events' AND column_name='pricing_selection_state'), EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='usage_request_events' AND column_name='pricing_template_kind')`).Scan(&hasSelectionState, &hasTemplateKind); err != nil {
		t.Fatalf("inspect usage pricing columns: %v", err)
	}
	t.Logf("usage pricing columns selection_state=%v template_kind=%v", hasSelectionState, hasTemplateKind)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedObserveUsageRows(t, harness, profileID, observeUsageSummarySegmentRows())

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	summary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-summary?query_context="+contextPayload["query_context"].(string), nil, http.StatusOK)
	segments := summary["cost_segments"].([]any)
	wantKeys := []string{"e.10", "e.2", "e.1", "l.GBP", "l.__unknown__"}
	wantStates := []string{"complete", "complete", "complete", "no_trusted_cost", "partial"}
	wantCosts := []any{"45", "100", "200", nil, nil}
	wantCounts := [][6]int{{9, 9, 0, 9, 0, 0}, {1, 1, 0, 1, 0, 0}, {1, 1, 0, 1, 0, 0}, {5, 4, 1, 0, 4, 0}, {2, 2, 0, 1, 0, 1}}
	wantReasons := [][4]int{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {1, 1, 1, 1}, {0, 0, 0, 0}}
	if len(segments) != len(wantKeys) || jsonInt(t, summary["request_count"]) != 18 {
		t.Fatalf("expected five window-scoped segments over 18 rows, got %+v", summary)
	}
	for index, key := range wantKeys {
		segment := asMap(t, segments[index])
		if segment["segment_key"] != key || segment["pricing_coverage_state"] != wantStates[index] || segment["known_cost_micros"] != wantCosts[index] {
			t.Fatalf("unexpected cost segment %d: %+v", index, segment)
		}
		assertJSONIntFields(t, segment, map[string]int{
			"request_count": wantCounts[index][0], "pricing_eligible_request_count": wantCounts[index][1],
			"pricing_ineligible_request_count": wantCounts[index][2], "priced_request_count": wantCounts[index][3],
			"unpriced_request_count": wantCounts[index][4], "pricing_unknown_request_count": wantCounts[index][5],
		})
		reasons := asMap(t, segment["unpriced_reason_counts"])
		if len(reasons) != 4 || jsonInt(t, reasons["PRICING_DISABLED"]) != wantReasons[index][0] || jsonInt(t, reasons["MISSING_TOKEN_USAGE"]) != wantReasons[index][1] || jsonInt(t, reasons["STREAM_USAGE_UNAVAILABLE"]) != wantReasons[index][2] || jsonInt(t, reasons["MISSING_PRICE_DATA"]) != wantReasons[index][3] {
			t.Fatalf("expected fixed four-reason counts for %s, got %+v", key, reasons)
		}
		if index > 0 {
			if _, exists := segment["sparkline"]; exists {
				t.Fatalf("expected only the first segment to own the full-window sparkline, got %+v", segments)
			}
		}
	}
	wantMetadata := [][4]any{{float64(10), "identified", "USD", "G$"}, {float64(2), "identified", "EUR", "€"}, {float64(1), "identified", "JPY", "¥"}, {nil, "legacy_unknown", "GBP", "£"}, {nil, "legacy_unknown", nil, nil}}
	for index, expected := range wantMetadata {
		segment := asMap(t, segments[index])
		if segment["reporting_currency_epoch"] != expected[0] || segment["currency_attribution"] != expected[1] || segment["currency_code"] != expected[2] || segment["display_symbol"] != expected[3] {
			t.Fatalf("unexpected currency metadata for %s: %+v", wantKeys[index], segment)
		}
	}
	epoch10 := asMap(t, segments[0])
	observed := epoch10["observed_symbols"].([]any)
	wantObserved := []string{"$", "US$", "A$", "B$", "C$", "D$", "E$", "F$"}
	if len(observed) != len(wantObserved) || jsonInt(t, epoch10["observed_symbol_count"]) != 9 || epoch10["observed_symbols_truncated"] != true {
		t.Fatalf("expected truncated first-seen symbol metadata, got %+v", epoch10)
	}
	for index, symbol := range wantObserved {
		if observed[index] != symbol {
			t.Fatalf("expected observed symbols %+v, got %+v", wantObserved, observed)
		}
	}
	unknownSegment := asMap(t, segments[4])
	if len(unknownSegment["observed_symbols"].([]any)) != 0 || jsonInt(t, unknownSegment["observed_symbol_count"]) != 0 || unknownSegment["observed_symbols_truncated"] != false {
		t.Fatalf("expected unknown segment to keep null currency/symbol facts, got %+v", unknownSegment)
	}
	if sparkline := asMap(t, epoch10["sparkline"]); jsonInt(t, asMap(t, sparkline["points"].([]any)[0])["request_count"]) != 18 || asMap(t, sparkline["points"].([]any)[0])["known_cost_micros"] != "845" {
		t.Fatalf("expected the existing full-window sparkline on the first segment, got %+v", sparkline)
	}
}

func TestObserveUsageSummaryCacheBasisPredicate(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	rows := []map[string]any{
		// Comparable: disjoint input/cache_read/cache_creation components.
		{"seq": 1, "operation_name": "anthropic.messages", "input_tokens": 200, "cache_read_input_tokens": 18000, "cache_creation_input_tokens": 0, "total_tokens": 18200, "output_tokens": 500},
		// count_tokens duplicates the total into cache_read: excluded.
		{"seq": 2, "operation_name": "gemini.count_tokens", "input_tokens": 41, "cache_read_input_tokens": 41, "cache_creation_input_tokens": 3, "total_tokens": 41, "output_tokens": 41},
		// Image generations never report cache components: excluded.
		{"seq": 3, "operation_name": "openai.images.generations", "input_tokens": 100, "output_tokens": 100, "total_tokens": 200},
		// Null operation_name is indeterminate: excluded, never a pass-through.
		{"seq": 4, "operation_name": nil, "input_tokens": 100, "cache_read_input_tokens": 50, "total_tokens": 150},
		// Missing input_tokens excludes even a measured cache_read.
		{"seq": 5, "operation_name": "anthropic.messages", "cache_read_input_tokens": 50, "total_tokens": 50},
		// Missing cache_read excludes even a measured input.
		{"seq": 6, "operation_name": "anthropic.messages", "input_tokens": 100, "total_tokens": 100},
	}
	seedObserveUsageRows(t, harness, profileID, rows)

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	summary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-summary?query_context="+contextPayload["query_context"].(string), nil, http.StatusOK)
	if jsonInt(t, summary["request_count"]) != 6 {
		t.Fatalf("expected 6 seeded requests, got %+v", summary)
	}
	// Only row 1 is cache-basis eligible; count_tokens, images, null
	// operation_name, and missing-component rows never enter the denominator.
	assertJSONIntFields(t, summary, map[string]int{"cache_basis_request_count": 1})
	if jsonInt(t, summary["cache_basis_input_tokens"]) != 200 {
		t.Fatalf("expected cache_basis_input_tokens 200, got %+v", summary["cache_basis_input_tokens"])
	}
	if jsonInt(t, summary["cache_basis_cache_read_tokens"]) != 18000 {
		t.Fatalf("expected cache_basis_cache_read_tokens 18000, got %+v", summary["cache_basis_cache_read_tokens"])
	}
	if jsonInt(t, summary["cache_basis_cache_creation_tokens"]) != 0 {
		t.Fatalf("expected cache_basis_cache_creation_tokens 0, got %+v", summary["cache_basis_cache_creation_tokens"])
	}
}

func TestObserveQueryContextValidation(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	// Custom > 30 days -> 422.
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/query-context?preset=custom&from_time=2026-01-01T00:00:00Z&to_time=2026-03-15T00:00:00Z", nil, modelHeader(profileID)), http.StatusUnprocessableEntity, "custom range cannot exceed 30 days")
	// Custom without bounds -> 422.
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/query-context?preset=custom", nil, modelHeader(profileID)), http.StatusUnprocessableEntity, "preset=custom requires from_time and to_time")
	// Invalid preset -> 422.
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/query-context?preset=3d", nil, modelHeader(profileID)), http.StatusUnprocessableEntity, `unknown preset "3d"`)
	// Unknown fragment without token -> 422.
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/usage-summary", nil, modelHeader(profileID)), http.StatusUnprocessableEntity, "query_context is required")
	// Tampered token -> 422.
	assertErrorResponse(t, harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/usage-summary?query_context=abc.def", nil, modelHeader(profileID)), http.StatusUnprocessableEntity, "invalid query_context")
}

func TestObserveQueryContextPresetConsistency(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedObserveUsageRows(t, harness, profileID, []map[string]any{{"seq": 1, "status_code": 200, "pricing_status": "priced"}})

	// Switching preset changes usage bounds; the token binds the effective
	// bounds so the same token stays consistent across fragments.
	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=1h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)
	summary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-summary?query_context="+token, nil, http.StatusOK)
	if jsonInt(t, summary["request_count"]) != 1 {
		t.Fatalf("expected 1 request in 1h window, got %+v", summary)
	}
}

func TestObserveUsageSeries(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	rows := make([]map[string]any, 0, 12)
	for i := 0; i < 6; i++ {
		rows = append(rows, map[string]any{"seq": 400 + i, "status_code": 200, "pricing_status": "priced", "model_id": "series-model-a", "ttft_ms": 100 + i})
	}
	for i := 0; i < 4; i++ {
		rows = append(rows, map[string]any{"seq": 500 + i, "status_code": 503, "pricing_status": "ineligible", "model_id": "series-model-b"})
	}
	seedObserveUsageRows(t, harness, profileID, rows)

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	series := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-series?query_context="+token+"&metric=requests&group_by=ingress_model&interval=auto", nil, http.StatusOK)
	if series["metric"] != "requests" || series["group_by"] != "ingress_model" {
		t.Fatalf("unexpected series root, got %+v", series)
	}
	items := series["series"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected two model series, got %+v", items)
	}
	first := asMap(t, items[0])
	if first["key"] != "ingress_model:series-model-a" {
		t.Fatalf("expected model series key, got %+v", first)
	}
	if jsonInt(t, first["request_count"]) != 6 {
		t.Fatalf("expected 6 requests for model a, got %+v", first)
	}
	points := first["points"].([]any)
	if len(points) == 0 {
		t.Fatalf("expected bucket points, got %+v", first)
	}
	point := asMap(t, points[0])
	if jsonInt(t, point["request_count"]) != 6 || jsonInt(t, point["http_success_count"]) != 6 {
		t.Fatalf("expected 6 success in bucket, got %+v", point)
	}
	reconciliation := asMap(t, point["pricing_reconciliation"])
	if jsonInt(t, reconciliation["priced_request_count"]) != 6 {
		t.Fatalf("expected 6 priced in bucket, got %+v", reconciliation)
	}
	second := asMap(t, items[1])
	if jsonInt(t, second["request_count"]) != 4 {
		t.Fatalf("expected 4 requests for model b, got %+v", second)
	}
	if jsonInt(t, asMap(t, second["points"].([]any)[0])["http_failed_count"]) != 4 {
		t.Fatalf("expected 4 http failures for model b, got %+v", second)
	}

	// total (group_by=none) shape
	totalSeries := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-series?query_context="+token+"&metric=requests&group_by=none&interval=auto", nil, http.StatusOK)
	totalItems := totalSeries["series"].([]any)
	if len(totalItems) != 1 || asMap(t, totalItems[0])["key"] != "total" {
		t.Fatalf("expected total series, got %+v", totalItems)
	}
	if jsonInt(t, asMap(t, totalItems[0])["request_count"]) != 10 {
		t.Fatalf("expected 10 total requests, got %+v", totalItems[0])
	}
}

func TestObserveUsageErrors(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	rows := make([]map[string]any, 0, 12)
	for i := 0; i < 3; i++ {
		rows = append(rows, map[string]any{"seq": 600 + i, "status_code": 503, "pricing_status": "ineligible", "model_id": "errors-model-a"})
	}
	for i := 0; i < 2; i++ {
		rows = append(rows, map[string]any{"seq": 700 + i, "status_code": 200, "pricing_status": "unpriced", "unpriced_reason": "STREAM_USAGE_UNAVAILABLE", "stream_outcome": "provider_incomplete", "stream_error_kind": "missing_terminal_event", "model_id": "errors-model-b"})
	}
	for i := 0; i < 1; i++ {
		rows = append(rows, map[string]any{"seq": 800 + i, "status_code": 200, "pricing_status": "unpriced", "unpriced_reason": "STREAM_USAGE_UNAVAILABLE", "stream_outcome": "client_disconnected", "model_id": "errors-model-b"})
	}
	seedObserveUsageRows(t, harness, profileID, rows)

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-errors?query_context="+token+"&group_by=ingress_model", nil, http.StatusOK)
	summary := asMap(t, payload["summary"])
	if jsonInt(t, summary["request_count"]) != 6 || jsonInt(t, summary["http_error_count"]) != 3 ||
		jsonInt(t, summary["stream_error_count"]) != 2 || jsonInt(t, summary["client_disconnected_count"]) != 1 ||
		jsonInt(t, summary["failed_count"]) != 5 {
		t.Fatalf("unexpected errors summary, got %+v", summary)
	}
	// Timeline: one bucket with the same decomposition.
	timeline := payload["timeline"].([]any)
	if len(timeline) != 1 {
		t.Fatalf("expected one timeline bucket, got %+v", timeline)
	}
	point := asMap(t, timeline[0])
	if jsonInt(t, point["http_error_count"]) != 3 || jsonInt(t, point["stream_error_count"]) != 2 || jsonInt(t, point["failed_count"]) != 5 || jsonInt(t, point["client_disconnected_count"]) != 1 {
		t.Fatalf("unexpected timeline point, got %+v", point)
	}
	// HTTP statuses: only 503 (http cohort = 3).
	statuses := payload["http_statuses"].([]any)
	if len(statuses) != 1 || jsonInt(t, asMap(t, statuses[0])["status_code"]) != 503 || jsonInt(t, asMap(t, statuses[0])["count"]) != 3 {
		t.Fatalf("unexpected http statuses, got %+v", statuses)
	}
	httpFilters := asMap(t, statuses[0])["request_filters"].(map[string]any)
	if httpFilters["final_status_code"].([]any)[0] != "503" || httpFilters["final_result"].([]any)[0] != "failed" {
		t.Fatalf("unexpected http leaf filters, got %+v", httpFilters)
	}
	// Stream outcomes: provider_incomplete (kind missing_terminal_event) + client_disconnected.
	outcomes := payload["stream_outcomes"].([]any)
	if len(outcomes) != 2 {
		t.Fatalf("expected two stream outcomes, got %+v", outcomes)
	}
	for _, outcomeValue := range outcomes {
		outcome := asMap(t, outcomeValue)
		switch outcome["stream_outcome"] {
		case "provider_incomplete":
			if jsonInt(t, outcome["count"]) != 2 {
				t.Fatalf("expected 2 provider_incomplete, got %+v", outcome)
			}
			kinds := outcome["error_kinds"].([]any)
			if len(kinds) != 1 || asMap(t, kinds[0])["stream_error_kind"] != "missing_terminal_event" {
				t.Fatalf("expected kind missing_terminal_event, got %+v", kinds)
			}
		case "client_disconnected":
			if jsonInt(t, outcome["count"]) != 1 {
				t.Fatalf("expected 1 client_disconnected, got %+v", outcome)
			}
		default:
			t.Fatalf("unexpected stream outcome %+v", outcome)
		}
	}
	// Groups by model.
	groups := payload["groups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("expected two model groups, got %+v", groups)
	}
	groupA := asMap(t, groups[0])
	if groupA["entity_id"] != "errors-model-a" || jsonInt(t, groupA["problem_count"]) != 3 || jsonInt(t, groupA["failed_count"]) != 3 {
		t.Fatalf("unexpected group A, got %+v", groupA)
	}
	groupB := asMap(t, groups[1])
	if groupB["entity_id"] != "errors-model-b" || jsonInt(t, groupB["problem_count"]) != 3 || jsonInt(t, groupB["failed_count"]) != 2 || jsonInt(t, groupB["client_disconnected_count"]) != 1 {
		t.Fatalf("unexpected group B, got %+v", groupB)
	}
	groupFilters := groupB["request_filters"].(map[string]any)
	if groupFilters["ingress_model_id"].([]any)[0] != "errors-model-b" {
		t.Fatalf("expected group model filter, got %+v", groupFilters)
	}
	// Requests context deep link.
	requestsContext := asMap(t, payload["requests_context"])
	if requestsContext["view"] != "attempts" || requestsContext["query_context"] != token {
		t.Fatalf("unexpected requests context, got %+v", requestsContext)
	}
}

func TestObserveRequestsFinalFiltersDeepLink(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	rows := []map[string]any{
		{"seq": 900, "status_code": 503, "pricing_status": "ineligible", "model_id": "final-model"},
		{"seq": 901, "status_code": 200, "pricing_status": "priced", "model_id": "final-model"},
	}
	seedObserveUsageRows(t, harness, profileID, rows)
	// Seed matching request_logs rows for the same ingresses so the retained
	// rows exist. Use distinct ingress ids from the usage rows.
	now := fixedS15Now.Add(-2 * time.Minute)
	for _, seq := range []string{"900", "901"} {
		if _, err := harness.conn.Exec(context.Background(), `
			INSERT INTO request_logs (profile_id, model_id, api_family, operation_name, ingress_request_id, attempt_number, upstream_status_code, response_time_ms, is_stream, success_flag, request_path, row_kind, url_scrub_provenance, pricing_status, pricing_evidence_trust, created_at)
			VALUES ($1, 'final-model', 'openai', 'openai.chat_completions', $2, 1, $3, 100, false, $4, '/v1/chat/completions', 'upstream', 'runtime_scrubbed', 'ineligible', 'trusted', $5)`,
			profileID, fmt.Sprintf("ingress-%d-%s", 2, seq), map[string]int{"900": 503, "901": 200}[seq], map[string]bool{"900": false, "901": true}[seq], now,
		); err != nil {
			t.Fatalf("seed request log: %v", err)
		}
	}

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)
	// final_result=failed deep link: only the 503 ingress's rows.
	payloadEnvelope := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/requests?view=attempts&query_context="+token+"&final_result=failed", nil, http.StatusOK)
	payload := payloadEnvelope["items"].([]any)
	if len(payload) != 1 {
		t.Fatalf("expected one retained row for failed cohort, got %+v", payloadEnvelope)
	}
	item := asMap(t, payload[0])
	if jsonInt(t, item["upstream_status_code"]) != 503 {
		t.Fatalf("expected 503 row, got %+v", item)
	}
	if item["row_kind"] != "upstream" {
		t.Fatalf("expected row_kind upstream, got %+v", item)
	}

	commaQuery := url.Values{
		"view":              {"attempts"},
		"query_context":     {token},
		"final_result":      {"failed,completed"},
		"final_endpoint_id": {"__null__"},
	}
	commaPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/requests?"+commaQuery.Encode(), nil, http.StatusOK)
	if items := commaPayload["items"].([]any); len(items) != 2 {
		t.Fatalf("expected same-key OR plus cross-key NULL filter to return two rows, got %+v", commaPayload)
	}
	repeatedQuery := url.Values{
		"view":              {"attempts"},
		"query_context":     {token},
		"final_result":      {"failed", "completed"},
		"final_endpoint_id": {"__null__"},
	}
	repeatedPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/requests?"+repeatedQuery.Encode(), nil, http.StatusOK)
	if len(repeatedPayload["items"].([]any)) != len(commaPayload["items"].([]any)) {
		t.Fatalf("expected repeated and comma selectors to be equivalent: comma=%+v repeated=%+v", commaPayload, repeatedPayload)
	}
	invalid := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/requests?view=attempts&query_context="+url.QueryEscape(token)+"&final_result=not-a-result", nil, modelHeader(profileID))
	assertStatus(t, invalid, http.StatusUnprocessableEntity)

	// Final filters without query_context -> 422 query_context_required.
	missingContext := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/requests?view=attempts&final_result=failed", nil, modelHeader(profileID))
	assertStatus(t, missingContext, http.StatusUnprocessableEntity)
	var missingContextPayload map[string]any
	decodeJSONResponse(t, missingContext, &missingContextPayload)
	if missingContextPayload["code"] != "query_context_required" {
		t.Fatalf("expected query_context_required, got %+v", missingContextPayload)
	}

	// Tampered token -> 422.
	badContext := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/requests?view=attempts&query_context=bad.token&final_result=failed", nil, modelHeader(profileID))
	assertStatus(t, badContext, http.StatusUnprocessableEntity)
	var badContextPayload map[string]any
	decodeJSONResponse(t, badContext, &badContextPayload)
	if badContextPayload["detail"] != "invalid query_context" {
		t.Fatalf("expected invalid query_context, got %+v", badContextPayload)
	}

	// Requests triage is ordinary state and must remain usable without a token.
	triage := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/requests?view=attempts&ingress_final_result=failed", nil, http.StatusOK)
	if items := triage["items"].([]any); len(items) != 1 {
		t.Fatalf("expected tokenless failed triage to select one row, got %+v", triage)
	}
}

func TestObserveIngressChains(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-5 * time.Minute)
	// Ingress A: two attempts (503 then 200) + one planning diagnostic row.
	// Ingress B: one upstream row.
	for _, row := range []struct {
		ingress string
		kind    string
		attempt int
		status  int
		success bool
		order   int
	}{
		{ingress: "chain-a", kind: "planning", attempt: 0, status: 503, success: false, order: 0},
		{ingress: "chain-a", kind: "upstream", attempt: 1, status: 503, success: false, order: 1},
		{ingress: "chain-a", kind: "upstream", attempt: 2, status: 200, success: true, order: 2},
		{ingress: "chain-b", kind: "upstream", attempt: 1, status: 200, success: true, order: 3},
	} {
		createdAt := now.Add(time.Duration(row.order) * time.Minute)
		if _, err := harness.conn.Exec(context.Background(), `
		INSERT INTO request_logs (profile_id, model_id, api_family, operation_name, ingress_request_id, attempt_number, status_code, upstream_status_code, gateway_status_code, response_time_ms, is_stream, success_flag, request_path, row_kind, url_scrub_provenance, pricing_status, pricing_evidence_trust, created_at)
			VALUES ($1, 'chain-model', 'openai', 'openai.chat_completions', $2, $3, $4, $5, $6, 100, false, $7, '/v1/chat/completions', $8, 'runtime_scrubbed', 'ineligible', 'trusted', $9)`,
			profileID, row.ingress, nullableTestAttempt(row.attempt), row.status, nullableTestStatus(row.kind, row.status), nullableTestGateway(row.kind, row.status), row.success, row.kind, createdAt,
		); err != nil {
			t.Fatalf("seed chain row: %v", err)
		}
	}
	// Finalized usage evidence for chain-a: expected attempt count 2.
	if _, err := harness.conn.Exec(context.Background(), `
		INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, operation_name, status_code, success_flag, attempt_count, request_path, endpoint_label_snapshot, pricing_status, pricing_evidence_trust, created_at)
		VALUES ($1, 'chain-a', 'chain-model', 'openai', 'openai.chat_completions', 200, true, 2, '/v1/chat/completions', 'Chain Endpoint', 'ineligible', 'trusted', $2)`,
		profileID, now,
	); err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/requests?view=ingress_chains&chain_limit=10", nil, http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected two chains, got %+v", payload)
	}
	// Newest first by first_at: chain-b was seeded last.
	chainB := asMap(t, items[0])
	if chainB["ingress_request_id"] != "chain-b" {
		t.Fatalf("expected chain-b first, got %+v", chainB)
	}
	chainA := asMap(t, items[1])
	if jsonInt(t, chainA["retained_upstream_attempt_count"]) != 2 || jsonInt(t, chainA["retained_request_log_row_count"]) != 3 {
		t.Fatalf("unexpected chain-a counts, got %+v", chainA)
	}
	if chainA["chain_complete"] != true {
		t.Fatalf("expected chain-complete true (2 expected = 2 retained upstream), got %+v", chainA)
	}
	rows := chainA["retained_rows"].([]any)
	if len(rows) != 3 {
		t.Fatalf("expected 3 retained rows for chain-a, got %+v", chainA)
	}
	firstRow := asMap(t, rows[0])
	if firstRow["row_kind"] != "planning" || firstRow["attempt_number"] != nil || firstRow["gateway_status_code"] != float64(503) {
		t.Fatalf("expected planning diagnostic row first with null attempt fields, got %+v", firstRow)
	}
	secondRow := asMap(t, rows[1])
	if secondRow["row_kind"] != "upstream" || jsonInt(t, secondRow["attempt_number"]) != 1 || secondRow["upstream_status_code"] != float64(503) {
		t.Fatalf("expected upstream attempt 1 row, got %+v", secondRow)
	}
	thirdRow := asMap(t, rows[2])
	if jsonInt(t, thirdRow["attempt_number"]) != 2 || thirdRow["upstream_status_code"] != float64(200) {
		t.Fatalf("expected upstream attempt 2 row, got %+v", thirdRow)
	}
	// Chain B: no finalized evidence -> chain_complete null.
	if chainB["chain_complete"] != nil {
		t.Fatalf("expected chain-b with null completeness, got %+v", chainB)
	}
	if jsonInt(t, chainB["retained_upstream_attempt_count"]) != 1 || jsonInt(t, chainB["retained_request_log_row_count"]) != 1 {
		t.Fatalf("unexpected chain-b counts, got %+v", chainB)
	}
}

func nullableTestStatus(kind string, status int) any {
	if kind == "upstream" {
		return status
	}
	return nil
}

func nullableTestGateway(kind string, status int) any {
	if kind == "planning" {
		return status
	}
	return nil
}

func nullableTestAttempt(attempt int) any {
	if attempt <= 0 {
		return nil
	}
	return attempt
}

func TestObserveRequestsExportCSV(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-2 * time.Minute)
	for _, row := range []struct {
		seq     int
		status  int
		success bool
		detail  string
	}{
		{seq: 1, status: 503, success: false, detail: "All connections failed"},
		{seq: 2, status: 200, success: true, detail: ""},
		{seq: 3, status: 400, success: false, detail: "Upstream returned HTTP 400"},
	} {
		if _, err := harness.conn.Exec(context.Background(), `
			INSERT INTO request_logs (profile_id, model_id, api_family, operation_name, ingress_request_id, attempt_number, status_code, response_time_ms, is_stream, success_flag, request_path, row_kind, error_code, error_detail, upstream_status_code, url_scrub_provenance, pricing_status, pricing_evidence_trust, created_at)
			VALUES ($1, 'export-model', 'openai', 'openai.chat_completions', $2, 1, $3, 100, false, $4, '/v1/chat/completions', 'upstream', $5, $6, $7, 'runtime_scrubbed', 'ineligible', 'trusted', $8)`,
			profileID, fmt.Sprintf("export-ingress-%d", row.seq), row.status, row.success, nullableExportCode(row.status), nullableTestString(stringPtr(row.detail)), nullableTestStatus("upstream", row.status), now,
		); err != nil {
			t.Fatalf("seed export row: %v", err)
		}
	}

	from := url.QueryEscape(fixedS15Now.Add(-1 * time.Hour).Format(time.RFC3339))
	to := url.QueryEscape(fixedS15Now.Format(time.RFC3339))
	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/stats/requests/export?from_time=%s&to_time=%s", from, to), nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	if response.Header.Get("X-Prism-Export-Row-Count") != "3" {
		t.Fatalf("expected 3 exported rows, got header %q", response.Header.Get("X-Prism-Export-Row-Count"))
	}
	if response.Header.Get("Digest") == "" || response.Header.Get("Content-Length") == "" {
		t.Fatalf("expected digest and content-length headers, got %+v", response.Header)
	}
	if response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("expected private no-store cache control, got %q", response.Header.Get("Cache-Control"))
	}
	body := readResponseBody(t, response)
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + 3 rows, got %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], "row_kind,request_log_id,ingress_request_id") {
		t.Fatalf("unexpected CSV header: %q", lines[0])
	}
	headerColumns := strings.Split(lines[0], ",")
	for _, column := range headerColumns {
		if column == "status_code" {
			t.Fatalf("export must not expose a mixed status_code column: %q", lines[0])
		}
	}
	for _, required := range []string{"upstream_status_code", "gateway_status_code", "legacy_status_code", "pricing_status"} {
		if !strings.Contains(lines[0], required) {
			t.Fatalf("expected %s in CSV header, got %q", required, lines[0])
		}
	}
	joined := strings.Join(lines[1:], "\n")
	if !strings.Contains(joined, "All connections failed") || !strings.Contains(joined, "upstream_http_400") {
		t.Fatalf("unexpected export rows: %q", joined)
	}

	// Pagination params are rejected.
	bad := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/stats/requests/export?from_time=%s&to_time=%s&offset=0", from, to), nil, modelHeader(profileID))
	assertErrorResponse(t, bad, http.StatusBadRequest, "export_pagination_unsupported")
	// Missing explicit range is rejected (structured envelope).
	noRange := harness.requestJSON(t, harness.client, http.MethodGet, "/api/stats/requests/export", nil, modelHeader(profileID))
	assertStatus(t, noRange, http.StatusUnprocessableEntity)
	var noRangePayload map[string]any
	decodeJSONResponse(t, noRange, &noRangePayload)
	if noRangePayload["code"] != "export_range_required" {
		t.Fatalf("expected export_range_required, got %+v", noRangePayload)
	}
}

func nullableExportCode(status int) any {
	if status == 503 {
		return "prism_routing_failure"
	}
	if status == 400 {
		return "upstream_http_400"
	}
	return nil
}

func TestObserveActivityFeed(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	rows := []map[string]any{
		{"seq": 950, "status_code": 200, "pricing_status": "priced", "model_id": "activity-a", "total_cost_user_currency_micros": int64(9300), "stream_outcome": ""},
		{"seq": 951, "status_code": 503, "pricing_status": "ineligible", "model_id": "activity-b", "stream_outcome": "completed"},
	}
	seedObserveUsageRows(t, harness, profileID, rows)
	// Give activity-a a resolved model different from requested to assert route_changed.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET resolved_target_model_id = CASE WHEN model_id = 'activity-a' THEN 'activity-actual' ELSE resolved_target_model_id END, routing_evidence_complete = CASE WHEN model_id = 'activity-a' THEN true ELSE routing_evidence_complete END WHERE profile_id = $1 AND model_id IN ('activity-a', 'activity-b')`, profileID); err != nil {
		t.Fatalf("set resolved model: %v", err)
	}

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	firstPage := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/observe-activity?query_context="+token+"&limit=1", nil, http.StatusOK)
	firstItems := firstPage["items"].([]any)
	if len(firstItems) != 1 || firstPage["has_more"] != true {
		t.Fatalf("expected one newest row and another page, got %+v", firstPage)
	}
	first := asMap(t, firstItems[0])
	if first["ingress_model_id"] != "activity-b" || jsonInt(t, first["status_code"]) != 503 {
		t.Fatalf("expected newest first (activity-b), got %+v", first)
	}
	if first["final_result"] != "failed" || first["outcome_detail"] != "http_error" {
		t.Fatalf("expected failed/http_error classification, got %+v", first)
	}
	if first["final_pricing_status"] != "ineligible" {
		t.Fatalf("expected ineligible pricing status, got %+v", first)
	}
	secondPage := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/observe-activity?query_context="+token+"&limit=1&before="+first["usage_event_id"].(string), nil, http.StatusOK)
	secondItems := secondPage["items"].([]any)
	if len(secondItems) != 1 || secondPage["has_more"] != false {
		t.Fatalf("expected exact-full terminal page, got %+v", secondPage)
	}
	second := asMap(t, secondItems[0])
	if second["ingress_model_id"] != "activity-a" || second["route_changed"] != true || first["is_stream"] != true || first["routing_evidence_complete"] != false || second["is_stream"] != false || second["routing_evidence_complete"] != true {
		t.Fatalf("expected route_changed for activity-a, got %+v", second)
	}
	if second["final_target_model_id"] != "activity-actual" {
		t.Fatalf("expected resolved model label, got %+v", second)
	}
	if second["known_cost_micros"] != "9300" || second["final_pricing_status"] != "priced" {
		t.Fatalf("expected priced cost facts, got %+v", second)
	}
	if expected := contextPayload["usage_coverage"]; !reflect.DeepEqual(firstPage["coverage"], expected) || !reflect.DeepEqual(secondPage["coverage"], expected) {
		t.Fatalf("expected every activity page to preserve frozen usage coverage, got first=%+v second=%+v want=%+v", firstPage["coverage"], secondPage["coverage"], expected)
	}
}

// TestObserveEmptyDatasetFragments pins the empty-domain contract: `all` over a
// domain with no retained rows freezes an empty half-open interval, and every
// fragment must answer it with an empty aggregate instead of a 422. The list
// fields stay JSON arrays because the Observe panels read `.length` on them.
func TestObserveEmptyDatasetFragments(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=all", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)
	usageBounds := asMap(t, contextPayload["usage_bounds"])
	if usageBounds["from_time"] != usageBounds["to_time"] {
		t.Fatalf("expected an empty half-open interval for an empty domain, got %+v", usageBounds)
	}
	summary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-summary?query_context="+token, nil, http.StatusOK)
	if segments, ok := summary["cost_segments"].([]any); !ok || len(segments) != 0 {
		t.Fatalf("expected an empty cost segment list, got %#v", summary["cost_segments"])
	}

	series := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-series?query_context="+token+"&metric=ttft&group_by=none&interval=auto", nil, http.StatusOK)
	items, ok := series["series"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("expected an empty series list, got %#v", series["series"])
	}

	errorsPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-errors?query_context="+token+"&group_by=ingress_model", nil, http.StatusOK)
	for _, key := range []string{"timeline", "http_statuses", "stream_outcomes", "groups"} {
		list, ok := errorsPayload[key].([]any)
		if !ok {
			t.Fatalf("expected usage-errors %q to be a JSON array, got %#v", key, errorsPayload[key])
		}
		if len(list) != 0 {
			t.Fatalf("expected usage-errors %q to be empty, got %+v", key, list)
		}
	}
	if gaps, ok := asMap(t, errorsPayload["coverage"])["gaps"].([]any); !ok {
		t.Fatalf("expected fragment coverage gaps to be a JSON array, got %#v", asMap(t, errorsPayload["coverage"])["gaps"])
	} else if len(gaps) != 0 {
		t.Fatalf("expected no coverage gaps for an empty domain, got %+v", gaps)
	}
	activity := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/observe-activity?query_context="+token, nil, http.StatusOK)
	expectedCoverage := contextPayload["usage_coverage"]
	for _, field := range []string{"retention_epoch", "retention_generation", "purge_state", "source_revision"} {
		if value, ok := asMap(t, expectedCoverage)[field].(string); !ok || value == "" {
			t.Fatalf("expected nonempty usage coverage owner field %q, got %+v", field, expectedCoverage)
		}
	}
	for name, fragment := range map[string]map[string]any{"summary": summary, "series": series, "errors": errorsPayload, "activity": activity} {
		if !reflect.DeepEqual(fragment["coverage"], expectedCoverage) {
			t.Fatalf("expected %s coverage to equal the signed usage projection, got %+v want %+v", name, fragment["coverage"], expectedCoverage)
		}
	}
}

// TestObserveUsageErrorsCleanCohort covers the common case behind the Analytics
// crash: a window that has traffic but no failures still returns arrays.
func TestObserveUsageErrorsCleanCohort(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedObserveUsageRows(t, harness, profileID, []map[string]any{{"seq": 970, "status_code": 200, "pricing_status": "priced", "model_id": "clean-model"}})

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)
	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/usage-errors?query_context="+token+"&group_by=ingress_model", nil, http.StatusOK)

	if jsonInt(t, asMap(t, payload["summary"])["failed_count"]) != 0 {
		t.Fatalf("expected a clean cohort, got %+v", payload["summary"])
	}
	for _, key := range []string{"http_statuses", "stream_outcomes", "groups"} {
		list, ok := payload[key].([]any)
		if !ok {
			t.Fatalf("expected usage-errors %q to be a JSON array, got %#v", key, payload[key])
		}
		if len(list) != 0 {
			t.Fatalf("expected usage-errors %q to be empty, got %+v", key, list)
		}
	}
	timeline, ok := payload["timeline"].([]any)
	if !ok || len(timeline) != 1 {
		t.Fatalf("expected one timeline bucket for the single request, got %#v", payload["timeline"])
	}
}

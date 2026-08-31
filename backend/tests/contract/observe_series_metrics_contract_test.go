package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// The main chart must carry the same honest output-rate and cache-basis
// components the Window KPI cards already publish, whatever metric is
// selected: the aggregate runs once per read and the six fields are part of
// every bucket row, so the frontend can project output rate and cache-read
// share without a second read model.

func seriesPointFields(t *testing.T, harness *contractHarness, profileID int, token string, metric string) map[string]any {
	t.Helper()
	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-series?query_context="+token+"&metric="+metric+"&group_by=none&interval=auto", nil, http.StatusOK)
	items := payload["series"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected the total series for metric %s, got %+v", metric, payload)
	}
	total := asMap(t, items[0])
	if total["key"] != "total" {
		t.Fatalf("expected the ungrouped total series, got %+v", total)
	}
	points := total["points"].([]any)
	if len(points) == 0 {
		t.Fatalf("expected bucket points, got %+v", payload)
	}
	return asMap(t, points[0])
}

// insertSeriesMetricsRow seeds one usage row with explicit operation_name,
// which the shared cache-basis eligibility predicate requires. The
// rateEvidence span seeds the measured output-delivery evidence; a nil span
// leaves the evidence columns NULL so the row reads as unknown.
func insertSeriesMetricsRow(t *testing.T, harness *contractHarness, profileID int, id int, createdAt time.Time,
	outputTokens, ttftMS, completionMS, inputTokens, cacheReadTokens, cacheCreationTokens *int, operationName string) {
	t.Helper()
	ingressID := fmt.Sprintf("series-metrics-%d", id)
	if _, err := harness.conn.Exec(context.Background(), `
	INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, operation_name, status_code, success_flag,
		attempt_count, request_path, pricing_status, pricing_evidence_trust,
		input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, ttft_ms, completion_duration_ms, output_rate_state, output_delivery_event_count, output_delivery_span_ms, endpoint_label_snapshot, created_at)
	VALUES ($1, $2, 'series-metrics-model', 'openai', $3, 200, true, 1, '/v1/chat/completions', 'ineligible', 'trusted',
		$4, $5, $6, $7, $8, $9, $10, CASE WHEN $11::int IS NOT NULL THEN 'measured' END, CASE WHEN $11::int IS NOT NULL THEN 2 END, $11, '', $12)`,
		profileID, ingressID, operationName,
		inputTokens, outputTokens, intPointerValue(outputTokens)+intPointerValue(inputTokens)+intPointerValue(cacheReadTokens)+intPointerValue(cacheCreationTokens),
		cacheReadTokens, cacheCreationTokens, ttftMS, completionMS, completionSpanEvidence(ttftMS, completionMS), createdAt,
	); err != nil {
		t.Fatalf("insert series metrics row %d: %v", id, err)
	}
}

// completionSpanEvidence projects the legacy TTFT/completion pair onto the
// measured delivery span so the fixture rows keep their historical rate
// semantics (span = completion - ttft).
func completionSpanEvidence(ttftMS, completionMS *int) *int {
	if ttftMS == nil || completionMS == nil {
		return nil
	}
	span := *completionMS - *ttftMS
	if span <= 0 {
		return nil
	}
	return &span
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func setSeriesOutputRateVerdict(t *testing.T, harness *contractHarness, profileID int, id int, state string, reason string) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `
		UPDATE usage_request_events
		SET output_rate_state = $1, output_rate_reason = $2
		WHERE profile_id = $3 AND ingress_request_id = $4`,
		state, reason, profileID, fmt.Sprintf("series-metrics-%d", id)); err != nil {
		t.Fatalf("set series output-rate verdict %d: %v", id, err)
	}
}

func assertJSONFloatNear(t *testing.T, value any, want float64) {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("expected JSON number near %v, got %#v", want, value)
	}
	if number < want-0.000001 || number > want+0.000001 {
		t.Fatalf("expected %v, got %v", want, number)
	}
}

// Per-request simple average: each measurable request contributes its own
// tok/s and the bucket mean is the unweighted mean of those values — never a
// token-weighted or duration-weighted blend.
func TestObserveUsageSeriesAveragesOutputRatePerRequest(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	now := fixedS15Now.Add(-2 * time.Minute)
	// Request A: 100 tokens over (1100-100)=1000ms → 100.0 tok/s.
	insertSeriesMetricsRow(t, harness, profileID, 1, now, intPtr(100), intPtr(100), intPtr(1100), intPtr(10), nil, nil, "openai.chat_completions")
	// Request B: 300 tokens over (2100-600)=1500ms → 200.0 tok/s.
	insertSeriesMetricsRow(t, harness, profileID, 2, now.Add(-time.Second), intPtr(300), intPtr(600), intPtr(2100), intPtr(20), nil, nil, "openai.chat_completions")
	// Not measurable: no completion duration, excluded from samples entirely.
	insertSeriesMetricsRow(t, harness, profileID, 3, now.Add(-3*time.Second), intPtr(999), intPtr(50), nil, intPtr(5), nil, nil, "openai.chat_completions")
	// GLM artifact: the old formula would produce 53,000 tok/s, but persisted
	// short-span evidence keeps it out of the mean.
	insertSeriesMetricsRow(t, harness, profileID, 4, now.Add(-4*time.Second), intPtr(53), intPtr(23495), intPtr(23496), intPtr(5), nil, nil, "openai.chat_completions")
	setSeriesOutputRateVerdict(t, harness, profileID, 4, "unmeasurable", "unmeasurable_output_span_below_threshold")
	// A non-streaming request is known not-applicable and still counts as a
	// request without becoming a rate sample.
	insertSeriesMetricsRow(t, harness, profileID, 5, now.Add(-5*time.Second), intPtr(25), nil, nil, intPtr(5), nil, nil, "openai.chat_completions")
	setSeriesOutputRateVerdict(t, harness, profileID, 5, "not_applicable", "not_applicable_non_stream")

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	point := seriesPointFields(t, harness, profileID, token, "output_rate")
	if got := jsonInt(t, point["output_rate_sample_count"]); got != 2 {
		t.Fatalf("expected 2 output-rate samples, got %+v", point)
	}
	assertJSONFloatNear(t, point["avg_output_rate_tps"], 150.0)
	// The unmeasured request still counts toward the bucket's request total.
	if got := jsonInt(t, point["request_count"]); got != 5 {
		t.Fatalf("expected 5 requests in the bucket, got %+v", point)
	}

	summary := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-summary?query_context="+token, nil, http.StatusOK)
	counts := asMap(t, summary["output_rate_state_counts"])
	for state, want := range map[string]int{"measured": 2, "unmeasurable": 1, "not_applicable": 1, "unknown": 1} {
		if got := jsonInt(t, counts[state]); got != want {
			t.Fatalf("expected %s output-rate count %d, got %d in %+v", state, want, got, counts)
		}
	}
	reasons := asMap(t, summary["output_rate_reason_counts"])
	if got := jsonInt(t, reasons["unmeasurable_output_span_below_threshold"]); got != 1 {
		t.Fatalf("expected one short-span reason, got %+v", reasons)
	}
	if got := jsonInt(t, reasons["not_applicable_non_stream"]); got != 1 {
		t.Fatalf("expected one non-stream reason, got %+v", reasons)
	}
}

// The cache basis keeps the usage-summary eligibility predicate and reports
// raw components, not a ratio: null sums mean no comparable rows, real zeros
// stay zeros, and ineligible operations never widen the basis.
func TestObserveUsageSeriesCarriesCacheBasisComponents(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	now := fixedS15Now.Add(-2 * time.Minute)
	// Comparable: input 400, cache-read 100, creation null (coalesces to 0).
	insertSeriesMetricsRow(t, harness, profileID, 1, now, intPtr(50), intPtr(100), intPtr(200), intPtr(400), intPtr(100), nil, "openai.chat_completions")
	// Not comparable: count_tokens operations are excluded by the predicate
	// (the registry's token-count operations are anthropic/gemini only).
	insertSeriesMetricsRow(t, harness, profileID, 2, now.Add(-time.Second), intPtr(10), nil, nil, intPtr(70), intPtr(70), nil, "anthropic.count_tokens")
	// Not comparable: cache_read is null, so the disjoint basis cannot hold.
	insertSeriesMetricsRow(t, harness, profileID, 3, now.Add(-2*time.Second), intPtr(20), nil, nil, intPtr(80), nil, intPtr(30), "openai.chat_completions")

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	point := seriesPointFields(t, harness, profileID, token, "cache_read_share")
	if got := jsonInt(t, point["cache_basis_request_count"]); got != 1 {
		t.Fatalf("expected 1 comparable request, got %+v", point)
	}
	if got := jsonInt(t, point["cache_basis_input_tokens"]); got != 400 {
		t.Fatalf("expected basis input 400, got %+v", point)
	}
	if got := jsonInt(t, point["cache_basis_cache_read_tokens"]); got != 100 {
		t.Fatalf("expected basis cache-read 100, got %+v", point)
	}
	// Creation is COALESCEd to a real zero inside an eligible basis; the sum
	// stays a measured 0, not a missing value.
	if got := jsonInt(t, point["cache_basis_cache_creation_tokens"]); got != 0 {
		t.Fatalf("expected basis cache-creation 0, got %+v", point)
	}
}

// A bucket without any eligible row publishes a zero count with null sums —
// the frontend's no-comparable state — and the six fields are stable across
// every metric because the aggregate does not branch on the requested metric.
func TestObserveUsageSeriesCacheBasisStaysHonestWithoutComparableRows(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	now := fixedS15Now.Add(-2 * time.Minute)
	// Only an ineligible operation, without stream timing: the whole bucket
	// has no comparable rows and no measurable output rate.
	insertSeriesMetricsRow(t, harness, profileID, 1, now, intPtr(10), nil, nil, intPtr(70), intPtr(70), nil, "gemini.count_tokens")

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	for _, metric := range []string{"requests", "errors", "ttft", "output_rate", "tokens", "cache_read_share", "cost"} {
		point := seriesPointFields(t, harness, profileID, token, metric)
		if got := jsonInt(t, point["cache_basis_request_count"]); got != 0 {
			t.Fatalf("metric %s: expected 0 comparable rows, got %+v", metric, point)
		}
		if point["cache_basis_input_tokens"] != nil || point["cache_basis_cache_read_tokens"] != nil || point["cache_basis_cache_creation_tokens"] != nil {
			t.Fatalf("metric %s: expected null basis sums without comparable rows, got %+v", metric, point)
		}
		if got := jsonInt(t, point["output_rate_sample_count"]); got != 0 {
			t.Fatalf("metric %s: expected 0 output-rate samples, got %+v", metric, point)
		}
		if point["avg_output_rate_tps"] != nil {
			t.Fatalf("metric %s: expected null average output rate without samples, got %+v", metric, point)
		}
	}
}

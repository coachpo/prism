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
// which the shared cache-basis eligibility predicate requires.
func insertSeriesMetricsRow(t *testing.T, harness *contractHarness, profileID int, id int, createdAt time.Time,
	outputTokens, ttftMS, completionMS, inputTokens, cacheReadTokens, cacheCreationTokens *int, operationName string) {
	t.Helper()
	ingressID := fmt.Sprintf("series-metrics-%d", id)
	if _, err := harness.conn.Exec(context.Background(), `
	INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, operation_name, status_code, success_flag,
		attempt_count, request_path, pricing_status, pricing_evidence_trust,
		input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, ttft_ms, completion_duration_ms, endpoint_label_snapshot, created_at)
	VALUES ($1, $2, 'series-metrics-model', 'openai', $3, 200, true, 1, '/v1/chat/completions', 'ineligible', 'trusted',
		$4, $5, $6, $7, $8, $9, $10, '', $11)`,
		profileID, ingressID, operationName,
		inputTokens, outputTokens, intPointerValue(outputTokens)+intPointerValue(inputTokens)+intPointerValue(cacheReadTokens)+intPointerValue(cacheCreationTokens),
		cacheReadTokens, cacheCreationTokens, ttftMS, completionMS, createdAt,
	); err != nil {
		t.Fatalf("insert series metrics row %d: %v", id, err)
	}
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
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

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	point := seriesPointFields(t, harness, profileID, token, "output_rate")
	if got := jsonInt(t, point["output_rate_sample_count"]); got != 2 {
		t.Fatalf("expected 2 output-rate samples, got %+v", point)
	}
	assertJSONFloatNear(t, point["avg_output_rate_tps"], 150.0)
	// The unmeasured request still counts toward the bucket's request total.
	if got := jsonInt(t, point["request_count"]); got != 3 {
		t.Fatalf("expected 3 requests in the bucket, got %+v", point)
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

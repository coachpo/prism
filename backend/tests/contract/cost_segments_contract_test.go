package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestCostSegmentsCatalogue verifies the canonical cost-segments catalogue:
// server-generated keys (e.N / l.AAA / l.__unknown__), coverage states, and
// bounded pagination.
func TestCostSegmentsCatalogue(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", now))

	// Segment e.2 (identified epoch): 1 priced trusted cost, 1 unpriced.
	for index := 0; index < 2; index++ {
		status := "priced"
		reason := "NULL"
		cost := int64(2500)
		if index == 1 {
			status = "unpriced"
			reason = "'PRICING_DISABLED'"
			cost = 0
		}
		var costArg any
		if cost > 0 {
			costArg = cost
		}
		query := fmt.Sprintf(`INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, unpriced_reason, report_currency_code, report_currency_symbol, reporting_currency_epoch, input_cost_micros, output_cost_micros, reasoning_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, created_at, proxy_api_key_attribution_state)
			VALUES ($1, 'seg-e2-%d', 'seg-model', 'openai', 'Seg', 200, TRUE, 1, '/v1/chat/completions', '%s', 'trusted', %s, 'USD', '$', 2, $4, $4, $4, $4, $4, $2, $2, $3, 'none')`, index, status, reason)
		if _, err := harness.conn.Exec(context.Background(), query, profileID, costArg, now.Add(time.Duration(index)*time.Minute), costArg); err != nil {
			t.Fatalf("seed e.2 segment row %d: %v", index, err)
		}
	}
	// Legacy segment l.EUR: unknown trust, canonical cost null.
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, report_currency_code, report_currency_symbol, created_at, proxy_api_key_attribution_state)
		VALUES ($1, 'seg-leur-1', 'seg-model', 'openai', 'Seg', 200, TRUE, 1, '/v1/chat/completions', 'unknown', 'legacy_untrusted', 'EUR', '€', $2, 'none')`,
		profileID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("seed l.EUR segment row: %v", err)
	}
	// Legacy l.__unknown__ row.
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, created_at, proxy_api_key_attribution_state)
		VALUES ($1, 'seg-unk-1', 'seg-model', 'openai', 'Seg', 200, TRUE, 1, '/v1/chat/completions', 'unknown', 'legacy_untrusted', $2, 'none')`,
		profileID, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("seed l.__unknown__ segment row: %v", err)
	}

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/cost-segments?limit=50", http.StatusOK)
	segments := payload["cost_segments"].([]any)
	if len(segments) != 3 {
		t.Fatalf("expected 3 cost segments, got %d", len(segments))
	}
	first := asMap(t, segments[0])
	if first["segment_key"] != "e.2" {
		t.Fatalf("expected identified epoch segment first, got %v", first["segment_key"])
	}
	if first["priced_request_count"] != float64(1) || first["unpriced_request_count"] != float64(1) {
		t.Fatalf("expected e.2 counts priced=1 unpriced=1, got %+v", first)
	}
	if first["pricing_coverage_state"] != "partial" {
		t.Fatalf("expected partial coverage for e.2, got %v", first["pricing_coverage_state"])
	}
	if first["known_cost_micros"] != "2500" {
		t.Fatalf("expected trusted known cost 2500, got %v", first["known_cost_micros"])
	}
	second := asMap(t, segments[1])
	if second["segment_key"] != "l.EUR" {
		t.Fatalf("expected legacy EUR segment second, got %v", second["segment_key"])
	}
	if second["known_cost_micros"] != nil {
		t.Fatalf("expected null known cost for legacy untrusted segment, got %v", second["known_cost_micros"])
	}
	third := asMap(t, segments[2])
	if third["segment_key"] != "l.__unknown__" {
		t.Fatalf("expected unknown segment last, got %v", third["segment_key"])
	}
	if third["known_cost_micros"] != nil {
		t.Fatalf("expected null known cost for unknown segment, got %v", third["known_cost_micros"])
	}
	if jsonInt(t, payload["cost_segments_total_count"]) != 3 || payload["cost_segments_next_cursor"] != nil {
		t.Fatalf("expected complete page metadata, got %+v", payload)
	}
}

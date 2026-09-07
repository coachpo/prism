package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestChainViewCountsChainRowsWithinTheWindowSlack pins the retained-row reach
// of the chain view. Counts and totals read the requested window widened by
// one day on each side, because the window has to constrain the scanned table
// itself for a partitioned read to prune. A chain whose retained rows straddle
// that boundary by more than the slack contributes only its in-reach rows, and
// this test is the reason that boundary is a decision rather than an accident.
func TestChainViewCountsChainRowsWithinTheWindowSlack(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	windowFrom := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	windowTo := windowFrom.Add(24 * time.Hour)
	inWindow := windowFrom.Add(12 * time.Hour)
	// One row inside the window, one twelve hours before it (inside the
	// slack), one three days before it (outside the slack).
	withinSlack := windowFrom.Add(-12 * time.Hour)
	beyondSlack := windowFrom.Add(-72 * time.Hour)

	partitions := []time.Time{inWindow, withinSlack, beyondSlack}
	for _, at := range partitions {
		ensureContractTestLogPartitions(t, harness,
			contractTestLogPartitionFor("request_logs", at),
			contractTestLogPartitionFor("usage_request_events", at),
		)
	}

	const ingressID = "chain-straddling-window"
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, stream_outcome, failover_occurred, created_at, ingress_started_at, ingress_completed_at, proxy_api_key_attribution_state)
		VALUES ($1, $2, 'chain-model', 'openai', 'Chain Endpoint', 200, TRUE, 3, '/v1/chat/completions', 'ineligible', 'trusted', 'not_streaming', FALSE, $3, $3, $3, 'none')`,
		profileID, ingressID, inWindow); err != nil {
		t.Fatalf("seed straddling chain usage event: %v", err)
	}
	for attempt, at := range []time.Time{inWindow, withinSlack, beyondSlack} {
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, attempt_trigger, attempt_result, is_winner, request_path, created_at)
			VALUES ($1, 'chain-model', 'openai', $2, $3, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', 'initial', 'completed', $4, '/v1/chat/completions', $5)`,
			profileID, ingressID, attempt+1, attempt == 0, at); err != nil {
			t.Fatalf("seed straddling chain row %d: %v", attempt+1, err)
		}
	}

	query := fmt.Sprintf("/api/stats/requests?view=ingress_chains&time_range=custom&from_time=%s&to_time=%s&chain_limit=20",
		windowFrom.Format(time.RFC3339), windowTo.Format(time.RFC3339))
	payload := s15GET[map[string]any](t, harness, profileID, query, http.StatusOK)

	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected the straddling chain to appear exactly once, got %d in %+v", len(items), payload)
	}
	item := asMap(t, items[0])
	if item["ingress_request_id"] != ingressID {
		t.Fatalf("expected the straddling chain, got %v", item["ingress_request_id"])
	}
	// Two of the three rows are in reach: the in-window row and the one
	// inside the slack. The row three days out is not.
	if got := jsonInt(t, item["retained_request_log_row_count"]); got != 2 {
		t.Fatalf("expected 2 retained rows within the window slack, got %d", got)
	}
	if got := jsonInt(t, payload["retained_request_log_row_total"]); got != 2 {
		t.Fatalf("expected the full-cohort row total to use the same reach, got %d", got)
	}
	if got := jsonInt(t, payload["retained_ingress_total"]); got != 1 {
		t.Fatalf("expected one retained ingress in the cohort, got %d", got)
	}
	// The chain is not complete: the usage event expects three attempts and
	// only two are in reach. The read path must say so rather than quietly
	// reporting a whole chain.
	if complete, ok := item["chain_complete"].(bool); !ok || complete {
		t.Fatalf("expected chain_complete=false for a chain reaching past the slack, got %v", item["chain_complete"])
	}
}

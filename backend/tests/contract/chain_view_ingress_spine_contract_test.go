package contracttest

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestChainViewOrdinaryStreamOutcomeMatchesEarlierAttempt(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	at := fixedS15Now.Add(-5 * time.Minute)
	ensureContractTestLogPartitions(t, harness,
		contractTestLogPartitionFor("request_logs", at),
		contractTestLogPartitionFor("usage_request_events", at),
	)
	const ingressID = "stream-recovered"
	seedChainIngress(t, harness, profileID, ingressID, at, 200, 2, true, "completed")
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs
		SET is_stream = TRUE, stream_outcome = CASE WHEN attempt_number = 1 THEN 'provider_incomplete' ELSE 'completed' END
		WHERE profile_id = $1 AND ingress_request_id = $2`, profileID, ingressID); err != nil {
		t.Fatalf("set retained stream outcomes: %v", err)
	}

	payload := s15GET[map[string]any](t, harness, profileID,
		"/api/stats/requests?view=ingress_chains&stream_outcome=provider_incomplete", http.StatusOK)
	if got := jsonInt(t, payload["retained_ingress_total"]); got != 1 {
		t.Fatalf("expected the earlier attempt to select one retained chain, got %d", got)
	}
	items := payload["items"].([]any)
	if len(items) != 1 || asMap(t, items[0])["ingress_request_id"] != ingressID {
		t.Fatalf("expected the recovered chain selected by its earlier attempt, got %+v", items)
	}
	item := asMap(t, items[0])
	if got := jsonInt(t, item["retained_request_log_row_count"]); got != 2 {
		t.Fatalf("expected both retained attempts in the selected chain, got %d", got)
	}
	if got := asMap(t, item["finalized_summary"])["final_result"]; got != "completed" {
		t.Fatalf("expected the independent finalized result to remain completed, got %v", got)
	}
}

func TestChainViewMixedEvidenceTimestampTiesPaginateWithoutGaps(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	at := fixedS15Now.Add(-5 * time.Minute)
	ensureContractTestLogPartitions(t, harness,
		contractTestLogPartitionFor("request_logs", at),
		contractTestLogPartitionFor("usage_request_events", at),
	)
	ids := []string{"tie-a", "tie-b", "tie-c", "tie-d", "tie-e"}
	for _, id := range ids {
		seedChainIngress(t, harness, profileID, id, at, 200, 1, false, "not_streaming")
	}
	// Both evidence sources share a timestamp, leaving ingress ID as the
	// only ordering key between finalized and retained-only chains.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET created_at = $1
		WHERE profile_id = $2 AND ingress_request_id = ANY($3)`, at, profileID, ids); err != nil {
		t.Fatalf("align retained chain timestamps: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM usage_request_events
		WHERE profile_id = $1 AND ingress_request_id IN ('tie-b', 'tie-d')`, profileID); err != nil {
		t.Fatalf("seed retained-only chains: %v", err)
	}
	for _, order := range []string{"asc", "desc"} {
		t.Run(order, func(t *testing.T) {
			query := url.Values{"view": {"ingress_chains"}, "chain_limit": {"1"}, "sort_order": {order}}
			for page := range ids {
				want := ids[page]
				if order == "desc" {
					want = ids[len(ids)-1-page]
				}
				payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?"+query.Encode(), http.StatusOK)
				if got := jsonInt(t, payload["retained_ingress_total"]); got != len(ids) {
					t.Fatalf("page %d: expected %d retained chains, got %d", page+1, len(ids), got)
				}
				items := payload["items"].([]any)
				if len(items) != 1 || asMap(t, items[0])["ingress_request_id"] != want {
					t.Fatalf("page %d: expected exactly %s, got %+v", page+1, want, items)
				}
				cursor, _ := payload["next_chain_cursor"].(string)
				if page == len(ids)-1 {
					if cursor != "" {
						t.Fatalf("expected the final mixed-evidence page to end pagination")
					}
				} else {
					if cursor == "" {
						t.Fatalf("page %d: missing continuation for remaining chains", page+1)
					}
					query.Set("chain_cursor", cursor)
				}
			}
		})
	}
}

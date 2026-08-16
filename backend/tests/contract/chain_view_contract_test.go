package contracttest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestChainViewServerSideCohortAndPagination verifies the ingress-chain view:
// row-scoped filters select the ingress cohort server-side before pagination,
// the outer page never splits an ingress, retained-row pages are bounded, and
// the finalized summary carries the authoritative final facts.
func TestChainViewServerSideCohortAndPagination(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-3 * time.Minute)
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("usage_request_events", now))

	// Ingress A: single success. Ingress B: 3 attempts with confirmed failover
	// ending 200. Ingress C: single 500 (failed).
	seedChainIngress(t, harness, profileID, "chain-a", now, 200, 1, false, "not_streaming")
	seedChainIngress(t, harness, profileID, "chain-b", now.Add(time.Minute), 200, 3, true, "not_streaming")
	seedChainIngress(t, harness, profileID, "chain-c", now.Add(2*time.Minute), 500, 1, false, "not_streaming")

	// Chain view default: all three ingresses, desc order.
	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&limit=50", http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("expected 3 chain items, got %d", len(items))
	}
	first := asMap(t, items[0])
	if first["ingress_request_id"] != "chain-c" {
		t.Fatalf("expected desc order to start with chain-c, got %v", first["ingress_request_id"])
	}
	summary := asMap(t, first["finalized_summary"])
	if summary["final_result"] != "failed" || jsonInt(t, summary["final_status_code"]) != 500 {
		t.Fatalf("expected chain-c finalized failed/500, got %+v", summary)
	}
	if jsonInt(t, first["retained_upstream_attempt_count"]) != 1 || jsonInt(t, first["retained_request_log_row_count"]) != 1 {
		t.Fatalf("expected chain-c single row, got %+v", first)
	}

	// Confirmed failover cohort selects chain-b only.
	failoverPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&confirmed_failover=true&limit=50", http.StatusOK)
	failoverItems := failoverPayload["items"].([]any)
	if len(failoverItems) != 1 {
		t.Fatalf("expected confirmed_failover cohort to select chain-b only, got %d", len(failoverItems))
	}
	failoverItem := asMap(t, failoverItems[0])
	if failoverItem["failover_occurred"] != true || jsonInt(t, failoverItem["expected_attempt_count"]) != 3 || jsonInt(t, failoverItem["retained_upstream_attempt_count"]) != 3 {
		t.Fatalf("expected chain-b failover item, got %+v", failoverItem)
	}
	retainedRows := asMap(t, failoverItems[0])["retained_rows"].([]any)
	if len(retainedRows) != 3 {
		t.Fatalf("expected chain-b to return all 3 retained rows, got %d", len(retainedRows))
	}

	// Final-result failed cohort selects chain-c.
	failedPayload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&ingress_final_result=failed&limit=50", http.StatusOK)
	failedItems := failedPayload["items"].([]any)
	if len(failedItems) != 1 || asMap(t, failedItems[0])["ingress_request_id"] != "chain-c" {
		t.Fatalf("expected final_result=failed cohort to select chain-c, got %d items", len(failedItems))
	}

	// Pagination: chain_limit=2 pages by ingress without splitting chain-b.
	page1 := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&chain_limit=2", http.StatusOK)
	page1Items := page1["items"].([]any)
	if len(page1Items) != 2 {
		t.Fatalf("expected 2 items on first chain page, got %d", len(page1Items))
	}
	if page1["has_more_chains"] != true || page1["next_chain_cursor"] == nil {
		t.Fatalf("expected has_more + cursor on first chain page, got %+v", page1)
	}
	cursor := page1["next_chain_cursor"].(string)
	page2 := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&chain_limit=2&chain_cursor="+cursor, http.StatusOK)
	page2Items := page2["items"].([]any)
	if len(page2Items) != 1 {
		t.Fatalf("expected 1 item on second chain page, got %d", len(page2Items))
	}
	secondItem := asMap(t, page2Items[0])
	if secondItem["ingress_request_id"] != "chain-a" || jsonInt(t, secondItem["retained_upstream_attempt_count"]) != 1 {
		t.Fatalf("expected chain-a on page 2, got %+v", secondItem)
	}

	// chain view rejects non-created_at sorts.
	s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&sort_by=ttft_ms", http.StatusUnprocessableEntity)
}

func TestChainViewNormalizesFinalizedTimestampsToUTC(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	location := time.FixedZone("FUN-009 offset", 3*60*60)
	startedAt := fixedS15Now.Add(-3 * time.Minute).In(location)
	completedAt := startedAt.Add(12 * time.Second)
	pricingEffectiveAt := startedAt.Add(-7 * 24 * time.Hour)
	seedChainTimestampFixture(t, harness, profileID, startedAt, completedAt, pricingEffectiveAt)
	previousLocal := time.Local
	time.Local = location
	defer func() { time.Local = previousLocal }()

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&ingress_request_id=chain-utc", http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one UTC chain item, got %+v", payload)
	}
	item := asMap(t, items[0])
	summary := asMap(t, item["finalized_summary"])
	row := asMap(t, item["retained_rows"].([]any)[0])
	for name, value := range map[string]any{
		"started_at":                   item["started_at"],
		"completed_at":                 item["completed_at"],
		"pricing_version_effective_at": summary["pricing_version_effective_at"],
		"retained_row_created_at":      row["created_at"],
	} {
		encoded, ok := value.(string)
		if !ok || !strings.HasSuffix(encoded, "Z") {
			t.Errorf("expected %s to use canonical UTC JSON, got %v", name, value)
		}
	}
}

func TestChainViewPreservesBigIntRequestLogIDsAsDecimalStrings(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedChainBigIntIDFixture(t, harness, profileID, fixedS15Now.Add(-4*time.Minute))

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&ingress_request_id=chain-bigint-ids", http.StatusOK)
	item := asMap(t, payload["items"].([]any)[0])
	summary := asMap(t, item["finalized_summary"])
	if got, ok := summary["request_log_id"].(string); !ok || got != "9007199254740997" {
		t.Fatalf("expected finalized request_log_id to preserve the final row BIGINT as a decimal string, got %T(%v)", summary["request_log_id"], summary["request_log_id"])
	}
	if summary["request_log_id"] == "9007199254740993" {
		t.Fatal("finalized request_log_id must not expose the usage-event id")
	}
	rows := item["retained_rows"].([]any)
	for index, want := range []string{"9007199254740995", "9007199254740997"} {
		row := asMap(t, rows[index])
		if got, ok := row["request_log_id"].(string); !ok || got != want {
			t.Fatalf("expected retained row %d request_log_id %q as a decimal string, got %T(%v)", index, want, row["request_log_id"], row["request_log_id"])
		}
	}
}

func seedChainTimestampFixture(t *testing.T, harness *contractHarness, profileID int, startedAt, completedAt, pricingEffectiveAt time.Time) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness,
		contractTestLogPartitionFor("request_logs", startedAt),
		contractTestLogPartitionFor("usage_request_events", startedAt),
	)
	seedChainIngress(t, harness, profileID, "chain-utc", startedAt, 200, 1, false, "not_streaming")
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events
		SET ingress_completed_at = $1, pricing_version_effective_at = $2
		WHERE profile_id = $3 AND ingress_request_id = 'chain-utc'`, completedAt, pricingEffectiveAt, profileID); err != nil {
		t.Fatalf("seed chain UTC finalized timestamps: %v", err)
	}
}

func seedChainBigIntIDFixture(t *testing.T, harness *contractHarness, profileID int, createdAt time.Time) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness,
		contractTestLogPartitionFor("request_logs", createdAt),
		contractTestLogPartitionFor("usage_request_events", createdAt),
	)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events
		(id, profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, final_attempt_number, request_path, pricing_status, pricing_evidence_trust, stream_outcome, created_at, ingress_started_at, ingress_completed_at, proxy_api_key_attribution_state)
		VALUES (9007199254740993, $1, 'chain-bigint-ids', 'chain-model', 'openai', 'Chain Endpoint', 200, TRUE, 2, 2, '/v1/chat/completions', 'ineligible', 'trusted', 'not_streaming', $2, $2, $2, 'none')`, profileID, createdAt); err != nil {
		t.Fatalf("seed BIGINT chain usage event: %v", err)
	}
	for index, id := range []int64{9007199254740995, 9007199254740997} {
		attempt := index + 1
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs
			(id, profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, attempt_trigger, attempt_result, is_winner, request_path, created_at)
			VALUES ($1, $2, 'chain-model', 'openai', 'chain-bigint-ids', $3, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', 'initial', 'completed', $4, '/v1/chat/completions', $5)`,
			id, profileID, attempt, attempt == 2, createdAt.Add(time.Duration(attempt)*time.Second)); err != nil {
			t.Fatalf("seed BIGINT chain request log %d: %v", attempt, err)
		}
	}
}

func seedChainIngress(t *testing.T, harness *contractHarness, profileID int, ingressID string, createdAt time.Time, statusCode int, attemptCount int, failover bool, streamOutcome string) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag, attempt_count, request_path, pricing_status, pricing_evidence_trust, stream_outcome, failover_occurred, created_at, ingress_started_at, ingress_completed_at, proxy_api_key_attribution_state)
		VALUES ($1, $2, 'chain-model', 'openai', 'Chain Endpoint', $3, $4, $5, '/v1/chat/completions', 'ineligible', 'trusted', $6, $7, $8, $8, $8, 'none')`,
		profileID, ingressID, statusCode, statusCode >= 200 && statusCode < 300, attemptCount, streamOutcome, failover, createdAt); err != nil {
		t.Fatalf("seed chain usage event %s: %v", ingressID, err)
	}
	for attempt := 1; attempt <= attemptCount; attempt++ {
		attemptStatus := statusCode
		attemptResult := "completed"
		trigger := "initial"
		winner := attempt == attemptCount
		if attempt < attemptCount && failover {
			attemptStatus = 503
			attemptResult = "http_error"
			trigger = "failover"
		}
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, attempt_trigger, attempt_result, is_winner, request_path, created_at)
			VALUES ($1, 'chain-model', 'openai', $2, $3, 'upstream', 'runtime_scrubbed', $4, 100, FALSE, $5, 'ineligible', 'trusted', $6, $7, $8, '/v1/chat/completions', $9)`,
			profileID, ingressID, attempt, attemptStatus, attemptStatus >= 200 && attemptStatus < 300, trigger, attemptResult, winner, createdAt.Add(time.Duration(attempt)*time.Second)); err != nil {
			t.Fatalf("seed chain request log %s/%d: %v", ingressID, attempt, err)
		}
	}
}

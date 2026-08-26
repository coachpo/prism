package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestChainViewServerSideCohortAndPagination verifies the ingress-chain view:
// row-scoped filters select the ingress cohort server-side before pagination,
// the outer page never splits an ingress, retained-row pages are bounded, and
// the finalized summary carries the authoritative final facts.
// TestChainViewOrdinarySetCoversRequestOnlyIngressesAndSkipsNullIngress
// verifies the ordinary ingress-set contract: chains whose retained request
// logs have no finalized usage evidence still appear with an unavailable
// finalized summary, rows with a NULL ingress_request_id never form a chain,
// and the full-cohort totals stay consistent with the visible set.
func TestChainViewOrdinarySetCoversRequestOnlyIngressesAndSkipsNullIngress(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := fixedS15Now.Add(-6 * time.Minute)
	ensureContractTestLogPartitions(t, harness,
		contractTestLogPartitionFor("request_logs", now),
		contractTestLogPartitionFor("usage_request_events", now),
	)

	// Ingress with both retained rows and finalized evidence.
	seedChainIngress(t, harness, profileID, "chain-both", now, 200, 2, false, "not_streaming")

	// Request-only ingress: retained rows without any usage event.
	requestOnlyAt := now.Add(time.Minute)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, attempt_trigger, attempt_result, is_winner, request_path, created_at)
		VALUES ($1, 'chain-model', 'openai', 'chain-request-only', 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', 'initial', 'completed', TRUE, '/v1/chat/completions', $2)`,
		profileID, requestOnlyAt); err != nil {
		t.Fatalf("seed request-only ingress row: %v", err)
	}

	// NULL-ingress diagnostic rows must not enter any chain or break totals.
	nullIngressAt := now.Add(2 * time.Minute)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at)
		VALUES ($1, 'chain-model', 'openai', NULL, NULL, 'admission', 'runtime_scrubbed', NULL, FALSE, FALSE, 'ineligible', 'trusted', '/v1/chat/completions', $2)`,
		profileID, nullIngressAt); err != nil {
		t.Fatalf("seed null-ingress row: %v", err)
	}

	payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&limit=50", http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected exactly two chains (null ingress excluded), got %d in %+v", len(items), payload)
	}

	// Desc order puts the newest finalized chain first.
	first := asMap(t, items[0])
	if first["ingress_request_id"] != "chain-request-only" {
		t.Fatalf("expected request-only chain to lead desc order, got %v", first["ingress_request_id"])
	}
	if first["finalized_summary"] != nil {
		t.Fatalf("expected request-only chain to carry no finalized summary, got %+v", first["finalized_summary"])
	}
	if first["finalized_evidence_state"] != "unavailable" || first["elapsed_evidence_state"] != "unavailable" || first["order_evidence_state"] != "retained_row_fallback" {
		t.Fatalf("expected unavailable finalized/elapsed evidence on request-only chain, got %+v", first)
	}
	if jsonInt(t, first["retained_request_log_row_count"]) != 1 {
		t.Fatalf("expected one retained row for request-only chain, got %+v", first)
	}

	second := asMap(t, items[1])
	if second["ingress_request_id"] != "chain-both" {
		t.Fatalf("expected finalized chain second, got %v", second["ingress_request_id"])
	}
	if asMap(t, second["finalized_summary"]) == nil {
		t.Fatalf("expected finalized summary for chain-both, got %+v", second)
	}

	// Full-cohort totals exclude the NULL-ingress row and match the page set.
	if jsonInt(t, payload["retained_ingress_total"]) != 2 {
		t.Fatalf("expected retained_ingress_total=2, got %+v", payload["retained_ingress_total"])
	}
	if jsonInt(t, payload["retained_request_log_row_total"]) != 3 {
		t.Fatalf("expected retained_request_log_row_total=3 (null row excluded), got %+v", payload["retained_request_log_row_total"])
	}

	// The outer cursor keeps working across mixed-evidence chains.
	page1 := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&chain_limit=1", http.StatusOK)
	cursor, ok := page1["next_chain_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("expected signed continuation cursor, got %+v", page1)
	}
	page2 := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&chain_limit=1&chain_cursor="+cursor, http.StatusOK)
	page2Items := page2["items"].([]any)
	if len(page2Items) != 1 || asMap(t, page2Items[0])["ingress_request_id"] != "chain-both" {
		t.Fatalf("expected chain-both on cursor page 2, got %+v", page2)
	}
}

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

func TestChainViewUsesPersistedCurrencyAttribution(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	createdAt := fixedS15Now.Add(-5 * time.Minute)
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", createdAt), contractTestLogPartitionFor("usage_request_events", createdAt))
	epoch := 7
	tests := []struct {
		name        string
		ingressID   string
		epoch       *int
		attribution string
		wantKey     string
	}{
		{name: "active identified", ingressID: "chain-currency-identified", epoch: &epoch, attribution: "identified", wantKey: "e.7"},
		{name: "identified without epoch", ingressID: "chain-currency-identified-code", epoch: nil, attribution: "identified", wantKey: "l.USD"},
		{name: "legacy unknown", ingressID: "chain-currency-legacy", epoch: &epoch, attribution: "legacy_unknown", wantKey: "e.7"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			seedChainIngress(t, harness, profileID, test.ingressID, createdAt.Add(time.Duration(index)*time.Second), 200, 1, false, "not_streaming")
			if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events
				SET reporting_currency_epoch = $1, report_currency_code = 'USD', report_currency_symbol = '$', currency_attribution = $2
				WHERE profile_id = $3 AND ingress_request_id = $4`, test.epoch, test.attribution, profileID, test.ingressID); err != nil {
				t.Fatalf("set finalized currency attribution: %v", err)
			}
			payload := s15GET[map[string]any](t, harness, profileID, "/api/stats/requests?view=ingress_chains&ingress_request_id="+test.ingressID, http.StatusOK)
			summary := asMap(t, asMap(t, payload["items"].([]any)[0])["finalized_summary"])
			if summary["currency_attribution"] != test.attribution || summary["cost_segment_key"] != test.wantKey {
				t.Fatalf("expected persisted attribution %q with segment %q, got %+v", test.attribution, test.wantKey, summary)
			}
		})
	}
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

func TestChainRowCursorPaginatesFullIngressCountsAndRejectsScopeChanges(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	ingressID := "chain-row-cursor"
	wantIDs := seedChainRowCursorFixture(t, harness, profileID, ingressID, fixedS15Now.Add(-4*time.Minute))
	// Finalized pricing selectors choose the ingress cohort. They must not be
	// folded into the retained-row status predicate: the finalized ingress is
	// unpriced while every retained row is independently ineligible.
	basePath := "/api/stats/requests?view=ingress_chains&ingress_request_id=" + ingressID + "&pricing_status=unpriced&unpriced_reason=MISSING_TOKEN_USAGE&status_code=503&chain_limit=1&chain_row_limit=1"

	var firstCursor string
	cursor := ""
	for pageIndex, wantID := range wantIDs {
		path := basePath
		if cursor != "" {
			path += "&row_cursor=" + cursor
		}
		payload := s15GET[map[string]any](t, harness, profileID, path, http.StatusOK)
		items := payload["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("page %d: expected one exact-ingress item, got %+v", pageIndex+1, payload)
		}
		item := asMap(t, items[0])
		for field, want := range map[string]int{
			"expected_attempt_count":          2,
			"expected_request_log_row_count":  3,
			"retained_upstream_attempt_count": 2,
			"retained_request_log_row_count":  3,
			"retained_row_count":              3,
			"retained_rows_loaded_count":      1,
			"matched_row_count":               2,
		} {
			if got := jsonInt(t, item[field]); got != want {
				t.Fatalf("page %d: expected stable %s=%d, got %d in %+v", pageIndex+1, field, want, got, item)
			}
		}
		if item["chain_complete"] != true {
			t.Fatalf("page %d: expected complete full-ingress evidence, got %+v", pageIndex+1, item)
		}
		rows := item["retained_rows"].([]any)
		if len(rows) != 1 {
			t.Fatalf("page %d: expected one retained row, got %+v", pageIndex+1, item)
		}
		row := asMap(t, rows[0])
		if got, ok := row["request_log_id"].(string); !ok || got != wantID {
			t.Fatalf("page %d: expected decimal-string request_log_id %q, got %T(%v)", pageIndex+1, wantID, row["request_log_id"], row["request_log_id"])
		}
		if wantMatched := pageIndex < 2; (row["matched_by_filter"] == true) != wantMatched {
			t.Fatalf("page %d: expected matched_by_filter=%t, got %+v", pageIndex+1, wantMatched, row["matched_by_filter"])
		}

		lastPage := pageIndex == len(wantIDs)-1
		if item["retained_rows_page_complete"] != lastPage {
			t.Fatalf("page %d: expected page_complete=%t, got %+v", pageIndex+1, lastPage, item)
		}
		if lastPage {
			if item["next_row_cursor"] != nil {
				t.Fatalf("page %d: expected terminal null cursor, got %+v", pageIndex+1, item["next_row_cursor"])
			}
			continue
		}
		var ok bool
		cursor, ok = item["next_row_cursor"].(string)
		if !ok || cursor == "" {
			t.Fatalf("page %d: expected signed continuation cursor, got %+v", pageIndex+1, item["next_row_cursor"])
		}
		if pageIndex == 0 {
			firstCursor = cursor
		}
	}

	cursorParts := strings.Split(firstCursor, ".")
	if len(cursorParts) != 2 || cursorParts[1] == "" {
		t.Fatalf("expected signed cursor envelope, got %q", firstCursor)
	}
	firstSignatureByte := byte('A')
	if cursorParts[1][0] == firstSignatureByte {
		firstSignatureByte = 'B'
	}
	tampered := cursorParts[0] + "." + string(firstSignatureByte) + cursorParts[1][1:]
	s15GET[map[string]any](t, harness, profileID, basePath+"&row_cursor="+tampered, http.StatusBadRequest)
	s15GET[map[string]any](t, harness, profileID, strings.Replace(basePath, ingressID, "chain-row-cursor-other", 1)+"&row_cursor="+firstCursor, http.StatusUnprocessableEntity)
	s15GET[map[string]any](t, harness, profileID, strings.Replace(basePath, "chain_row_limit=1", "chain_row_limit=2", 1)+"&row_cursor="+firstCursor, http.StatusUnprocessableEntity)
	s15GET[map[string]any](t, harness, profileID, basePath+"&chain_cursor=outer&row_cursor="+firstCursor, http.StatusUnprocessableEntity)
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

func seedChainRowCursorFixture(t *testing.T, harness *contractHarness, profileID int, ingressID string, createdAt time.Time) []string {
	t.Helper()
	ensureContractTestLogPartitions(t, harness,
		contractTestLogPartitionFor("request_logs", createdAt),
		contractTestLogPartitionFor("usage_request_events", createdAt),
	)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO usage_request_events
		(id, profile_id, ingress_request_id, model_id, api_family, endpoint_label_snapshot, status_code, success_flag,
		 attempt_count, expected_request_log_row_count, final_attempt_number, request_path, pricing_status,
		 pricing_evidence_trust, unpriced_reason, stream_outcome, created_at, ingress_started_at, ingress_completed_at,
		 proxy_api_key_attribution_state, routing_evidence_complete)
		VALUES (9007199254741101, $1, $2, 'chain-model', 'openai', 'Chain Endpoint', 200, TRUE,
		 2, 3, 2, '/v1/chat/completions', 'unpriced', 'trusted', 'MISSING_TOKEN_USAGE', 'not_streaming', $3, $3, $3, 'none', TRUE)`,
		profileID, ingressID, createdAt); err != nil {
		t.Fatalf("seed row-cursor usage event: %v", err)
	}
	ids := []int64{9007199254741103, 9007199254741105, 9007199254741107}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs
		(id, profile_id, model_id, api_family, ingress_request_id, row_kind, url_scrub_provenance,
		 gateway_status_code, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at)
		VALUES ($1, $2, 'chain-model', 'openai', $3, 'planning', 'runtime_scrubbed',
		 503, FALSE, FALSE, 'ineligible', 'trusted', '/v1/chat/completions', $4)`,
		ids[0], profileID, ingressID, createdAt.Add(time.Second)); err != nil {
		t.Fatalf("seed row-cursor planning row: %v", err)
	}
	for index, id := range ids[1:] {
		attempt := index + 1
		status := 503
		result := "http_error"
		winner := false
		if attempt == 2 {
			status = 200
			result = "completed"
			winner = true
		}
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs
			(id, profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance,
			 upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust,
			 attempt_trigger, attempt_result, is_winner, request_path, created_at)
			VALUES ($1, $2, 'chain-model', 'openai', $3, $4, 'upstream', 'runtime_scrubbed',
			 $5, 100, FALSE, $6, 'ineligible', 'trusted', 'initial', $7, $8,
			 '/v1/chat/completions', $9)`,
			id, profileID, ingressID, attempt, status, status == 200, result, winner, createdAt.Add(time.Duration(attempt+1)*time.Second)); err != nil {
			t.Fatalf("seed row-cursor upstream request log %d: %v", attempt, err)
		}
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, fmt.Sprintf("%d", id))
	}
	return result
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

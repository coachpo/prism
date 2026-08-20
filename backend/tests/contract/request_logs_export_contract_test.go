package contracttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRequestLogCSVExportContract verifies the full filtered CSV export:
// server-side rows > page size, RFC 4180 quoting, formula neutralisation,
// digest/headers, 31-day bound, and the 100k rejection without partial files.
func TestRequestLogCSVExportContract(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))
	var clientRuleID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, 'CSV export', '^Codex-Parity/', TRUE, FALSE, $2, $2) RETURNING id`, profileID, now).Scan(&clientRuleID); err != nil {
		t.Fatalf("seed CSV client rule: %v", err)
	}

	// Seed 3 rows including a formula-injection fixture and a binary-ish path.
	for index := 0; index < 3; index++ {
		ingress := "export-failover"
		if index == 2 {
			ingress = "export-decoy"
		}
		errorDetail := "\"safe detail\""
		if index >= 1 {
			errorDetail = "=HYPERLINK(\"http://evil\")"
		}
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (
			profile_id, model_id, resolved_target_model_id, api_family, operation_name, ingress_request_id, attempt_number, attempt_trigger, attempt_result, is_winner, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, stream_outcome, ttft_ms, completion_duration_ms, success_flag, input_tokens, output_tokens, total_tokens, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_status, unpriced_reason, pricing_evidence_trust, pricing_template_id_used, pricing_template_name_snapshot, pricing_template_revision_id_used, pricing_config_version_used, reporting_currency_epoch, metadata_redacted_fields, metadata_truncated_fields, request_path, error_detail, endpoint_id, connection_id, caller_user_agent, created_at)
			VALUES ($1, CASE WHEN $2 = 2 THEN 'other-model' ELSE 'export-model' END, CASE WHEN $2 = 0 THEN 'export-other-target' ELSE 'export-target' END, 'openai', 'openai.chat_completions', $3, CASE WHEN $2 = 1 THEN 2 ELSE 1 END, CASE WHEN $2 = 1 THEN 'failover' ELSE 'initial' END, CASE WHEN $2 = 0 THEN 'http_error' ELSE 'completed' END, $2 <> 0, 'upstream', 'runtime_scrubbed', CASE WHEN $2 = 0 THEN 503 ELSE 200 END, 11 * ($2 + 1), $2 = 1, CASE WHEN $2 = 1 THEN 'completed' ELSE 'not_streaming' END, CASE WHEN $2 = 1 THEN 7 END, CASE WHEN $2 = 1 THEN 19 END, $2 <> 0, CASE WHEN $2 = 1 THEN 5 END, CASE WHEN $2 = 1 THEN 10 END, CASE WHEN $2 = 1 THEN 15 END, CASE WHEN $2 = 1 THEN 'EUR' END, 'USD', '$', CASE WHEN $2 = 1 THEN '2' END, CASE WHEN $2 = 1 THEN 'TEST' END, CASE WHEN $2 = 2 THEN 'unpriced' ELSE 'ineligible' END, CASE WHEN $2 = 2 THEN 'MISSING_TOKEN_USAGE' END, 'trusted', CASE WHEN $2 = 1 THEN 42 END, CASE WHEN $2 = 1 THEN 'Export price' END, CASE WHEN $2 = 1 THEN 77 END, CASE WHEN $2 = 1 THEN 3 END, CASE WHEN $2 = 1 THEN 4 END, CASE WHEN $2 = 1 THEN ARRAY['authorization']::text[] ELSE ARRAY[]::text[] END, CASE WHEN $2 = 1 THEN ARRAY['error_detail']::text[] ELSE ARRAY[]::text[] END, '/v1/chat/completions', $4, CASE WHEN $2 = 2 THEN 9912 ELSE 9911 END, CASE WHEN $2 = 2 THEN 7712 ELSE 7711 END, CASE WHEN $2 = 2 THEN 'Other-Client/1.0' ELSE 'Codex-Parity/1.0' END, $5)`,
			profileID, index, ingress, errorDetail, now.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatalf("seed export row %d: %v", index, err)
		}
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET pricing_template_kind = 'tiered', pricing_selection_state = 'selected', pricing_card_role = 'tier_above', pricing_selector_threshold_tokens = 272000, pricing_selector_basis_tokens = 272001 WHERE profile_id = $1 AND ingress_request_id = 'export-failover' AND attempt_number = 2`, profileID); err != nil {
		t.Fatalf("seed CSV card evidence: %v", err)
	}

	from := now.Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	to := now.Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	path := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", from, to)
	response := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected export 200, got %d", response.StatusCode)
	}
	rawBody, readErr := ioReadAll(response)
	if readErr != nil {
		t.Fatalf("read export body: %v", readErr)
	}
	response.Body = ioNopCloser(bytes.NewReader(rawBody))
	body := string(rawBody)
	assertPrivateObservabilityHeaders(t, response.Header)
	if response.Header.Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("expected text/csv content type, got %q", response.Header.Get("Content-Type"))
	}
	if response.Header.Get("X-Prism-Export-Row-Count") != "3" {
		t.Fatalf("expected 3 exported rows, got %q", response.Header.Get("X-Prism-Export-Row-Count"))
	}
	digestHeader := response.Header.Get("Digest")
	if !strings.HasPrefix(digestHeader, "sha-256=") {
		t.Fatalf("expected sha-256 digest header, got %q", digestHeader)
	}
	expectedDigest := sha256.Sum256([]byte(body))
	if digestHeader != "sha-256="+hex.EncodeToString(expectedDigest[:]) {
		t.Fatalf("digest header does not match body bytes")
	}
	// Formula injection: the =HYPERLINK path must be apostrophe-neutralised.
	if strings.Contains(body, "\n=HYPERLINK") || strings.Contains(body, "=HYPERLINK(") && !strings.Contains(body, "'=HYPERLINK(") {
		t.Fatalf("formula injection not neutralised in export: %q", body)
	}
	if !strings.Contains(body, "'=HYPERLINK(") {
		t.Fatalf("expected apostrophe-neutralised formula cell, got %q", body)
	}
	// Header row + 3 rows.
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + 3 rows, got %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], "row_kind,request_log_id") {
		t.Fatalf("expected CSV header, got %q", lines[0])
	}
	records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("parse export CSV: %v", err)
	}
	wantHeader := "row_kind,request_log_id,ingress_request_id,attempt_number,attempt_trigger,attempt_result,is_winner,created_at,model_id,resolved_target_model_id,api_family,operation_name,endpoint_id,terminal_target_id,upstream_status_code,gateway_status_code,legacy_status_code,error_source,error_code,failure_stage,error_detail,stream_error_detail,stream_outcome,stream_error_kind,attempt_duration_ms,legacy_duration_ms,ttft_ms,total_duration_ms,input_tokens,output_tokens,total_tokens,cache_read_input_tokens,cache_creation_input_tokens,reasoning_tokens,total_cost_user_currency_micros,currency_code_original,report_currency_code,report_currency_symbol,fx_rate_used,fx_rate_source,pricing_status,unpriced_reason,pricing_resolution_kind,missing_price_components,pricing_evidence_trust,pricing_template_id_used,pricing_template_name_snapshot,pricing_template_revision_id_used,pricing_config_version_used,pricing_version_effective_at,reporting_currency_epoch,metadata_redacted_fields,metadata_truncated_fields,pricing_template_kind,pricing_selection_state,pricing_card_role,pricing_selector_threshold_tokens,pricing_selector_basis_tokens,pricing_schedule_decided_at,pricing_schedule_timezone,pricing_schedule_local_weekday,pricing_schedule_local_minute,pricing_schedule_digest"
	if strings.Join(records[0], ",") != wantHeader {
		t.Fatalf("CSV header drifted: %q", strings.Join(records[0], ","))
	}
	columns := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		columns[name] = index
	}
	var failover []string
	for _, record := range records[1:] {
		if record[columns["attempt_trigger"]] == "failover" {
			failover = record
		}
	}
	if failover == nil || failover[columns["ttft_ms"]] != "7" || failover[columns["total_duration_ms"]] != "19" || failover[columns["currency_code_original"]] != "EUR" || failover[columns["report_currency_code"]] != "USD" || failover[columns["report_currency_symbol"]] != "$" || failover[columns["pricing_template_kind"]] != "tiered" || failover[columns["pricing_selection_state"]] != "selected" || failover[columns["pricing_card_role"]] != "tier_above" || failover[columns["pricing_selector_threshold_tokens"]] != "272000" || failover[columns["pricing_selector_basis_tokens"]] != "272001" {
		t.Fatalf("expected exact stream/failover database fields in CSV, got %+v", failover)
	}
	base, combined := fmt.Sprintf("view=attempts&from_time=%s&to_time=%s", from, to), fmt.Sprintf("client_rule_id=%d&model_id=export-model&resolved_target_model_id=export-target&endpoint_id=9911&terminal_target_id=7711&status_family=2xx&status_code=200&pricing_status=ineligible&error_text=HYPERLINK", clientRuleID)
	checks := []struct {
		filter string
		want   int
	}{{"ingress_request_id=export-failover", 2}, {fmt.Sprintf("client_rule_id=%d", clientRuleID), 2}, {"model_id=export-model", 2}, {"resolved_target_model_id=export-target", 2}, {"endpoint_id=9911", 2}, {"terminal_target_id=7711", 2}, {"status_family=2xx", 2}, {"status_code=200", 2}, {"pricing_status=ineligible", 2}, {"unpriced_reason=MISSING_TOKEN_USAGE", 1}, {"error_text=HYPERLINK", 2}, {combined, 1}}
	var winner map[string]any
	for _, check := range checks {
		query := base + "&" + check.filter
		list := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/requests?"+query, nil, http.StatusOK)
		filtered := harness.requestJSONRaw(t, harness.client, http.MethodGet, "/api/stats/requests/export?"+query, "", modelHeader(profileID))
		filteredBody, filteredReadErr := ioReadAll(filtered)
		filteredRecords, filteredParseErr := csv.NewReader(bytes.NewReader(filteredBody)).ReadAll()
		items := list["items"].([]any)
		jsonIDs := make(map[string]bool, len(items))
		for _, item := range items {
			jsonIDs[asMap(t, item)["request_log_id"].(string)] = true
		}
		if filteredReadErr != nil || filteredParseErr != nil || filtered.StatusCode != http.StatusOK || filtered.Header.Get("X-Prism-Export-Row-Count") != fmt.Sprintf("%d", check.want) || len(items) != check.want || len(filteredRecords) != check.want+1 {
			t.Fatalf("filter %q JSON/CSV cardinality mismatch: list=%+v csv=%+v status=%d count=%q read=%v parse=%v", check.filter, list, filteredRecords, filtered.StatusCode, filtered.Header.Get("X-Prism-Export-Row-Count"), filteredReadErr, filteredParseErr)
		}
		for _, record := range filteredRecords[1:] {
			if !jsonIDs[record[columns["request_log_id"]]] {
				t.Fatalf("filter %q JSON/CSV ID mismatch: list=%+v csv=%+v", check.filter, list, filteredRecords)
			}
		}
		if check.filter == combined {
			winner = asMap(t, items[0])
		}
	}
	if jsonInt(t, winner["ttft_ms"]) != 7 || jsonInt(t, winner["completion_duration_ms"]) != 19 || winner["report_currency_symbol"] != failover[columns["report_currency_symbol"]] {
		t.Fatalf("expected JSON/CSV stream-duration and currency parity, JSON=%+v CSV=%+v", winner, failover)
	}
	missingPayload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, path+"&ingress_final_result=failed", nil, modelHeader(profileID), http.StatusUnprocessableEntity)
	if missingPayload["code"] != "query_context_required" {
		t.Fatalf("expected export query_context_required parity, got %+v", missingPayload)
	}

	// Pagination keys rejected.
	pageResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path+"&limit=10", "", modelHeader(profileID))
	if pageResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected export to reject pagination keys, got %d", pageResponse.StatusCode)
	}

	// Range beyond 31 days rejected (non-exact selectors).
	oldFrom := now.Add(-40 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	widePath := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", oldFrom, to)
	wideResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, widePath, "", modelHeader(profileID))
	if wideResponse.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected export range 422, got %d", wideResponse.StatusCode)
	}
}

// TestRequestLogCSVExportTooLarge verifies the 100,000-row cap rejects with a
// typed 422 and produces no CSV body.
func TestRequestLogCSVExportTooLarge(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))

	// Seed 100,001 rows (well below the cap for a unit-ish contract test? No:
	// the cap test needs >100k rows; seed them in one batch.)
	const rows = 100001
	batch := 5000
	inserted := 0
	for inserted < rows {
		count := batch
		if inserted+count > rows {
			count = rows - inserted
		}
		values := make([]string, 0, count)
		for index := 0; index < count; index++ {
			values = append(values, fmt.Sprintf("(%d, 'export-bulk', 'openai', 'bulk-%d', 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', now() + interval '1 second')", profileID, inserted+index))
		}
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at) VALUES `+strings.Join(values, ",")); err != nil {
			t.Fatalf("seed bulk export rows at %d: %v", inserted, err)
		}
		inserted += count
	}

	from := now.Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	to := now.Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	path := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", from, to)
	response := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 100k export to reject with 422, got %d", response.StatusCode)
	}
	body := readResponseBody(t, response)
	if !strings.Contains(body, "request_export_too_large") {
		t.Fatalf("expected typed request_export_too_large rejection, got %q", body)
	}
	if response.Header.Get("X-Prism-Export-Row-Count") != "" {
		t.Fatalf("expected no success export headers on rejection, got %q", response.Header.Get("X-Prism-Export-Row-Count"))
	}
}

// TestRequestLogCSVExportSnapshotStableUnderConcurrentInserts verifies the
// REPEATABLE READ snapshot keeps the exported row count stable while inserts
// land concurrently (Requests SPEC §6.8).
func TestRequestLogCSVExportSnapshotStableUnderConcurrentInserts(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	now := time.Now().UTC()
	// Seed rows span ten minutes; ensure partitions for every boundary they
	// can cross (a run near midnight UTC would otherwise fail).
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now.Add(10*time.Minute)))

	// Seed a base row set.
	for index := 0; index < 10; index++ {
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at)
			VALUES ($1, 'snap-model', 'openai', $2, 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', $3)`,
			profileID, fmt.Sprintf("snap-%d", index), now.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatalf("seed snapshot row %d: %v", index, err)
		}
	}

	// First export: its READ ONLY REPEATABLE READ snapshot opens at request
	// dispatch and freezes the count before any concurrent insert lands.
	from := now.Add(-time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	to := now.Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	path := fmt.Sprintf("/api/stats/requests/export?view=attempts&from_time=%s&to_time=%s", from, to)
	firstResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if firstResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected first export 200, got %d", firstResponse.StatusCode)
	}
	firstBody, err := ioReadAll(firstResponse)
	if err != nil {
		t.Fatalf("read first export body: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(firstBody)), "\n"); len(lines) != 11 {
		t.Fatalf("expected exactly 11 lines (header + 10 rows) in the first snapshot, got %d", len(lines))
	}

	// Concurrent inserts now land strictly after the first snapshot froze.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		counter := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, success_flag, pricing_status, pricing_evidence_trust, request_path, created_at)
				VALUES ($1, 'snap-model', 'openai', $2, 1, 'upstream', 'runtime_scrubbed', 200, 100, FALSE, TRUE, 'ineligible', 'trusted', '/v1/chat/completions', now())`,
				profileID, fmt.Sprintf("snap-concurrent-%d", counter)); err != nil {
				t.Errorf("concurrent insert: %v", err)
				return
			}
			counter++
			time.Sleep(2 * time.Millisecond)
		}
	}()
	defer func() {
		close(stop)
		<-done
	}()

	// Second export while inserts run: its own snapshot is stable; the
	// frozen count can only grow between snapshots, never shrink.
	secondResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if secondResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected second export 200 under concurrent inserts, got %d", secondResponse.StatusCode)
	}
	secondBody, err := ioReadAll(secondResponse)
	if err != nil {
		t.Fatalf("read second export body: %v", err)
	}
	secondLines := strings.Split(strings.TrimSpace(string(secondBody)), "\n")
	if len(secondLines) < 11 {
		t.Fatalf("expected second export to contain at least the seeded 10 rows, got %d lines", len(secondLines))
	}

	thirdResponse := harness.requestJSONRaw(t, harness.client, http.MethodGet, path, "", modelHeader(profileID))
	if thirdResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected third export 200, got %d", thirdResponse.StatusCode)
	}
	thirdBody, err := ioReadAll(thirdResponse)
	if err != nil {
		t.Fatalf("read third export body: %v", err)
	}
	thirdLines := strings.Split(strings.TrimSpace(string(thirdBody)), "\n")
	if len(thirdLines) < len(secondLines) {
		t.Fatalf("expected third export count to be monotonic (got %d < %d): RR snapshot must never see rows shrink", len(thirdLines), len(secondLines))
	}
}

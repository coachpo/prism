package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"testing"
	"time"
)

func TestObserveFinalizedErrorsOtherRemaindersReplayExactly(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	rows := []map[string]any{
		{"seq": 2100, "model_id": "errors-openai", "api_family": "openai", "operation_name": "openai.chat_completions", "status_code": 500, "pricing_status": "ineligible"},
		{"seq": 2101, "model_id": "errors-openai", "api_family": "openai", "operation_name": "openai.chat_completions", "status_code": 500, "pricing_status": "ineligible"},
		{"seq": 2102, "model_id": "errors-openai", "api_family": "openai", "operation_name": "openai.chat_completions", "status_code": 500, "pricing_status": "ineligible"},
		{"seq": 2200, "model_id": "errors-anthropic", "api_family": "anthropic", "operation_name": "anthropic.messages", "status_code": 501, "pricing_status": "ineligible"},
		{"seq": 2201, "model_id": "errors-anthropic", "api_family": "anthropic", "operation_name": "anthropic.messages", "status_code": 501, "pricing_status": "ineligible"},
		{"seq": 2300, "model_id": "errors-gemini", "api_family": "gemini", "operation_name": "gemini.generate_content", "status_code": 502, "pricing_status": "ineligible"},
	}
	seedObserveUsageRows(t, harness, profileID, rows)
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", fixedS15Now))
	createdAt := fixedS15Now.Add(-2 * time.Minute)
	for _, row := range rows {
		seq := row["seq"].(int)
		status := row["status_code"].(int)
		family := row["api_family"].(string)
		operation := row["operation_name"].(string)
		modelID := row["model_id"].(string)
		ingressID := fmt.Sprintf("ingress-%d-%d", len(rows), seq)
		if _, err := harness.conn.Exec(context.Background(), `
			INSERT INTO request_logs (profile_id, model_id, api_family, operation_name, ingress_request_id,
				attempt_number, attempt_result, upstream_status_code, response_time_ms, is_stream, success_flag,
				request_path, row_kind, url_scrub_provenance, pricing_status, pricing_evidence_trust, created_at)
			VALUES ($1, $2, $3, $4, $5, 1, 'http_error', $6, 100, false, false,
				'/v1/test', 'upstream', 'runtime_scrubbed', 'ineligible', 'trusted', $7)`,
			profileID, modelID, family, operation, ingressID, status, createdAt,
		); err != nil {
			t.Fatalf("seed request row: %v", err)
		}
	}
	if _, err := harness.conn.Exec(context.Background(), `
		UPDATE usage_request_events
		SET resolved_target_model_id = model_id || '-target', final_attempt_number = 1
		WHERE profile_id = $1 AND ingress_request_id LIKE 'ingress-6-%'`, profileID); err != nil {
		t.Fatalf("seed final execution identity: %v", err)
	}

	for _, scope := range []string{"ingress", "final_execution"} {
		t.Run(scope, func(t *testing.T) {
			contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
				"/api/stats/query-context?preset=24h&scope="+scope, nil, http.StatusOK)
			token := contextPayload["query_context"].(string)

			statusPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
				"/api/stats/usage-errors?query_context="+url.QueryEscape(token)+"&group_by=none&limit=2", nil, http.StatusOK)
			statusOther := asMap(t, asMap(t, statusPayload["other"])["http_statuses"])
			if jsonInt(t, statusOther["count"]) != 1 {
				t.Fatalf("status Other = %+v", statusOther)
			}
			assertErrorsOtherReplay(t, harness, profileID, token, statusOther, 1, []string{"gemini"})

			groupPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
				"/api/stats/usage-errors?query_context="+url.QueryEscape(token)+"&group_by=api_family&limit=2", nil, http.StatusOK)
			groups := groupPayload["groups"].([]any)
			if len(groups) != 2 || asMap(t, groups[0])["entity_type"] != "api_family" {
				t.Fatalf("api_family groups = %+v", groups)
			}
			groupOther := asMap(t, asMap(t, groupPayload["other"])["groups"])
			if jsonInt(t, groupOther["count"]) != 1 {
				t.Fatalf("group Other = %+v", groupOther)
			}
			assertErrorsOtherReplay(t, harness, profileID, token, groupOther, 1, []string{"gemini"})
		})
	}
}

func assertErrorsOtherReplay(t *testing.T, harness *contractHarness, profileID int, token string, remainder map[string]any, wantTotal int, wantFamilies []string) {
	t.Helper()
	filters := asMap(t, remainder["request_filters"])
	values := url.Values{"view": {"attempts"}, "limit": {"100"}, "query_context": {token}}
	for key, raw := range filters {
		for _, value := range raw.([]any) {
			values.Add(key, value.(string))
		}
	}
	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/requests?"+values.Encode(), nil, http.StatusOK)
	if jsonInt(t, payload["total"]) != wantTotal {
		t.Fatalf("replay total = %v, filters=%+v payload=%+v", payload["total"], filters, payload)
	}
	families := make([]string, 0, len(payload["items"].([]any)))
	for _, raw := range payload["items"].([]any) {
		families = append(families, asMap(t, raw)["api_family"].(string))
	}
	sort.Strings(families)
	sort.Strings(wantFamilies)
	if fmt.Sprint(families) != fmt.Sprint(wantFamilies) {
		t.Fatalf("replay families = %v, want %v", families, wantFamilies)
	}
}

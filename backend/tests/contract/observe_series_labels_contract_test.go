package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// The main-chart legend is the only place an operator can tell two exits
// apart, so a grouped series must carry the entity's name and its id. Raw
// `connection_id` / `endpoint_id` text reads as an unlabelled number.
func TestObserveUsageSeriesResolvesGroupedEntityLabels(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	endpointID := modelInsertEndpoint(t, harness, profileID, "Label Endpoint")
	connectionID := insertLabelledConnection(t, harness, profileID, endpointID, "Label Target")

	// Same base as the other observe fixtures: the harness already owns the
	// usage partition for this day.
	now := fixedS15Now.Add(-2 * time.Minute)
	for index := 0; index < 3; index++ {
		if _, err := harness.conn.Exec(context.Background(), `
			INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, operation_name, status_code, success_flag,
				attempt_count, request_path, endpoint_id, connection_id, endpoint_label_snapshot, pricing_status, pricing_evidence_trust,
				input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, ttft_ms, completion_duration_ms, created_at)
			VALUES ($1, $2, 'label-model', 'openai', 'openai.chat_completions', 200, true, 1, '/v1/chat/completions', $3, $4, 'Retained Endpoint Label', 'ineligible', 'trusted',
				400, 100, 600, 100, 0, 100, 1100, $5)`,
			profileID, fmt.Sprintf("label-ingress-%d", index), endpointID, connectionID, now.Add(time.Duration(index)*time.Second),
		); err != nil {
			t.Fatalf("seed labelled usage row: %v", err)
		}
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET resolved_target_model_id=model_id, final_attempt_number=1 WHERE model_id='label-model'`); err != nil {
		t.Fatalf("seed final_execution identity: %v", err)
	}

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h&scope=final_execution", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	target := firstSeriesItem(t, harness, profileID, token, "terminal_target")
	if target["key"] != fmt.Sprintf("terminal_target:%d", connectionID) {
		t.Fatalf("expected terminal-target series key, got %+v", target)
	}
	// The live connection name wins, matching the terminal-target drill-down.
	if target["label"] != "Label Target" {
		t.Fatalf("expected resolved terminal-target label, got %+v", target)
	}
	if target["entity_id"] != fmt.Sprintf("%d", connectionID) {
		t.Fatalf("expected terminal-target entity_id, got %+v", target)
	}
	assertGroupedSeriesMetricFields(t, target, 3, 100, 1200, 300)

	endpoint := firstSeriesItem(t, harness, profileID, token, "endpoint")
	if endpoint["key"] != fmt.Sprintf("endpoint:%d", endpointID) {
		t.Fatalf("expected endpoint series key, got %+v", endpoint)
	}
	// Endpoint labels come from the retained snapshot, never the mutable
	// endpoints.name, so history cannot be relabelled by a later rename.
	if endpoint["label"] != "Retained Endpoint Label" {
		t.Fatalf("expected retained endpoint label, got %+v", endpoint)
	}
	if endpoint["entity_id"] != fmt.Sprintf("%d", endpointID) {
		t.Fatalf("expected endpoint entity_id, got %+v", endpoint)
	}
	assertGroupedSeriesMetricFields(t, endpoint, 3, 100, 1200, 300)

	model := firstSeriesItem(t, harness, profileID, token, "final_target_model")
	if model["key"] != "final_target_model:label-model" || model["entity_id"] != "label-model" {
		t.Fatalf("expected model series identity, got %+v", model)
	}
	assertGroupedSeriesMetricFields(t, model, 3, 100, 1200, 300)

	// Ungrouped stays one series named for what it is, not the SQL remainder
	// bucket it shares a code path with.
	total := firstSeriesItem(t, harness, profileID, token, "none")
	if total["key"] != "total" || total["label"] != "Total" || total["entity_id"] != nil {
		t.Fatalf("expected unlabelled total series, got %+v", total)
	}
	assertGroupedSeriesMetricFields(t, total, 3, 100, 1200, 300)
}

func assertGroupedSeriesMetricFields(t *testing.T, item map[string]any, wantCount int, wantRate float64, wantInput int, wantRead int) {
	t.Helper()
	points := item["points"].([]any)
	if len(points) != 1 {
		t.Fatalf("expected one grouped metric bucket, got %+v", item)
	}
	point := asMap(t, points[0])
	if got := jsonInt(t, point["output_rate_sample_count"]); got != wantCount {
		t.Fatalf("expected %d output-rate samples, got %+v", wantCount, point)
	}
	assertJSONFloatNear(t, point["avg_output_rate_tps"], wantRate)
	if got := jsonInt(t, point["cache_basis_request_count"]); got != wantCount {
		t.Fatalf("expected %d cache-basis requests, got %+v", wantCount, point)
	}
	if got := jsonInt(t, point["cache_basis_input_tokens"]); got != wantInput {
		t.Fatalf("expected basis input %d, got %+v", wantInput, point)
	}
	if got := jsonInt(t, point["cache_basis_cache_read_tokens"]); got != wantRead {
		t.Fatalf("expected basis cache read %d, got %+v", wantRead, point)
	}
	if got := jsonInt(t, point["cache_basis_cache_creation_tokens"]); got != 0 {
		t.Fatalf("expected measured zero cache creation, got %+v", point)
	}
}

func firstSeriesItem(t *testing.T, harness *contractHarness, profileID int, token string, groupBy string) map[string]any {
	t.Helper()
	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-series?query_context="+token+"&metric=requests&group_by="+groupBy+"&interval=auto", nil, http.StatusOK)
	items := payload["series"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected at least one %s series, got %+v", groupBy, payload)
	}
	return asMap(t, items[0])
}

func insertLabelledConnection(t *testing.T, harness *contractHarness, profileID int, endpointID int, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `
		INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream,
			openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at)
		VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, $3, NULL, NULL, 'healthy', NULL, NULL, $4, $4) RETURNING id`,
		profileID, endpointID, name, now,
	).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection %q: %v", name, err)
	}
	return connectionID
}

// A request that fails before routing settles records the outcome with no
// endpoint and no connection. Those rows are real traffic, so the grouped main
// chart has to keep counting them without letting a NULL become an entity: the
// operator sees the same request total whichever grouping is selected.
func TestObserveUsageSeriesFoldsUnattributedRequestsIntoOther(t *testing.T) {
	harness := newS15ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	endpointID := modelInsertEndpoint(t, harness, profileID, "Attributed Endpoint")
	connectionID := insertLabelledConnection(t, harness, profileID, endpointID, "Attributed Target")

	now := fixedS15Now.Add(-2 * time.Minute)
	for index := 0; index < 3; index++ {
		if _, err := harness.conn.Exec(context.Background(), `
		INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, operation_name, status_code, success_flag,
			attempt_count, request_path, endpoint_id, connection_id, endpoint_label_snapshot, pricing_status, pricing_evidence_trust, created_at)
		VALUES ($1, $2, 'attributed-model', 'openai', 'openai.chat_completions', 200, true, 1, '/v1/chat/completions', $3, $4, 'Attributed Endpoint Label', 'ineligible', 'trusted', $5)`,
			profileID, fmt.Sprintf("attributed-ingress-%d", index), endpointID, connectionID, now.Add(time.Duration(index)*time.Second),
		); err != nil {
			t.Fatalf("seed attributed usage row: %v", err)
		}
	}
	// No exit was selected, so both grouping columns are NULL on these rows.
	for index := 0; index < 2; index++ {
		if _, err := harness.conn.Exec(context.Background(), `
		INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, operation_name, status_code, success_flag,
			attempt_count, request_path, endpoint_id, connection_id, endpoint_label_snapshot, pricing_status, pricing_evidence_trust, created_at)
		VALUES ($1, $2, 'attributed-model', 'openai', 'openai.chat_completions', 502, false, 1, '/v1/chat/completions', NULL, NULL, '', 'ineligible', 'trusted', $3)`,
			profileID, fmt.Sprintf("unattributed-ingress-%d", index), now.Add(time.Duration(index)*time.Second),
		); err != nil {
			t.Fatalf("seed unattributed usage row: %v", err)
		}
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE usage_request_events SET resolved_target_model_id=model_id, final_attempt_number=1 WHERE model_id='attributed-model'`); err != nil {
		t.Fatalf("seed grouped final_execution identity: %v", err)
	}

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h&scope=final_execution", nil, http.StatusOK)
	token := contextPayload["query_context"].(string)

	for _, groupBy := range []string{"endpoint", "terminal_target"} {
		entityKey := fmt.Sprintf("endpoint:%d", endpointID)
		if groupBy == "terminal_target" {
			entityKey = fmt.Sprintf("terminal_target:%d", connectionID)
		}
		series := seriesItems(t, harness, profileID, token, groupBy)

		totalRequests := 0
		byKey := map[string]map[string]any{}
		for _, item := range series {
			entry := asMap(t, item)
			key, _ := entry["key"].(string)
			byKey[key] = entry
			totalRequests += int(entry["request_count"].(float64))
			// A NULL group value must never be published as an entity: an
			// empty id would render as a nameless exit in the legend.
			if key != "other" {
				if entityID, ok := entry["entity_id"].(string); !ok || entityID == "" {
					t.Fatalf("group_by=%s produced an entity series without an id: %+v", groupBy, entry)
				}
			}
		}
		if totalRequests != 5 {
			t.Fatalf("group_by=%s lost requests: want 5, got %d (%+v)", groupBy, totalRequests, series)
		}
		if byKey[entityKey] == nil {
			t.Fatalf("group_by=%s dropped the attributed entity series %q, got %+v", groupBy, entityKey, series)
		}
		if got := int(byKey[entityKey]["request_count"].(float64)); got != 3 {
			t.Fatalf("group_by=%s attributed series counted %d requests, want 3", groupBy, got)
		}
		other := byKey["other"]
		if other == nil {
			t.Fatalf("group_by=%s dropped the unattributed rows instead of folding them into Other, got %+v", groupBy, series)
		}
		if got := int(other["request_count"].(float64)); got != 2 {
			t.Fatalf("group_by=%s Other counted %d requests, want the 2 unattributed rows", groupBy, got)
		}
		// Other is the re-aggregated remainder, not an entity.
		if other["entity_id"] != nil {
			t.Fatalf("group_by=%s gave Other an entity id: %+v", groupBy, other)
		}
		if other["label"] != "Other" {
			t.Fatalf("group_by=%s mislabelled the remainder series: %+v", groupBy, other)
		}
	}
}

func seriesItems(t *testing.T, harness *contractHarness, profileID int, token string, groupBy string) []any {
	t.Helper()
	payload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet,
		"/api/stats/usage-series?query_context="+token+"&metric=cost&group_by="+groupBy+"&interval=auto", nil, http.StatusOK)
	items, ok := payload["series"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected at least one %s series, got %+v", groupBy, payload)
	}
	return items
}

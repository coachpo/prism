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
			attempt_count, request_path, endpoint_id, connection_id, endpoint_label_snapshot, pricing_status, pricing_evidence_trust, created_at)
		VALUES ($1, $2, 'label-model', 'openai', 'openai.chat_completions', 200, true, 1, '/v1/chat/completions', $3, $4, 'Retained Endpoint Label', 'ineligible', 'trusted', $5)`,
			profileID, fmt.Sprintf("label-ingress-%d", index), endpointID, connectionID, now.Add(time.Duration(index)*time.Second),
		); err != nil {
			t.Fatalf("seed labelled usage row: %v", err)
		}
	}

	contextPayload := modelJSON[map[string]any](t, harness, profileID, http.MethodGet, "/api/stats/query-context?preset=24h", nil, http.StatusOK)
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

	// Ungrouped stays one series named for what it is, not the SQL remainder
	// bucket it shares a code path with.
	total := firstSeriesItem(t, harness, profileID, token, "none")
	if total["key"] != "total" || total["label"] != "Total" || total["entity_id"] != nil {
		t.Fatalf("expected unlabelled total series, got %+v", total)
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

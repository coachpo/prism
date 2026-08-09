package runtimetest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// seedProxyKeyAttributedRequests seeds retained request rows with proxy key
// snapshot identities: one current key (id 201, name "current-key"), one
// deleted key (id 202, snapshot name "deleted-key") and unkeyed rows.
func seedProxyKeyAttributedRequests(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Now().UTC()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", now))

	base := `INSERT INTO request_logs (id, profile_id, model_id, api_family, status_code, response_time_ms, is_stream, success_flag, request_path, created_at, ingress_request_id, attempt_number, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request) VALUES ($1, $2, 'model-a', 'openai', 200, 10, FALSE, TRUE, '/v1/chat/completions', $3, $4, 1, $5, $6, $7, $8)`
	rows := []struct {
		id         int
		createdAt  time.Time
		ingress    string
		keyID      any
		keyName    any
		state      string
		enforced   any
	}{
		{id: 300, createdAt: now.Add(-1 * time.Hour), ingress: "ingress-current-1", keyID: 201, keyName: "current-key", state: "identified", enforced: false},
		{id: 301, createdAt: now.Add(-2 * time.Hour), ingress: "ingress-current-2", keyID: 201, keyName: "current-key", state: "identified", enforced: true},
		{id: 302, createdAt: now.Add(-3 * time.Hour), ingress: "ingress-deleted-1", keyID: 202, keyName: "deleted-key", state: "identified", enforced: false},
		{id: 303, createdAt: now.Add(-4 * time.Hour), ingress: "ingress-deleted-2", keyID: 202, keyName: "deleted-key-renamed", state: "identified", enforced: false},
		{id: 304, createdAt: now.Add(-5 * time.Hour), ingress: "ingress-none-1", keyID: nil, keyName: nil, state: "none", enforced: false},
		{id: 305, createdAt: now.Add(-6 * time.Hour), ingress: "ingress-unknown-1", keyID: nil, keyName: "orphan-name", state: "unknown", enforced: nil},
	}
	for _, row := range rows {
		if _, err := harness.conn.Exec(context.Background(), base, row.id, profileID, row.createdAt, row.ingress, row.keyID, row.keyName, row.state, row.enforced); err != nil {
			t.Fatalf("seed proxy-key attributed request log %d: %v", row.id, err)
		}
	}
}

// TestRequestLogsProxyAPIKeyIDParserRejectsInvalidValues covers the typed 422
// contract: empty, non-integer, zero, negative, overflow and duplicate values
// never normalize to no filter.
func TestRequestLogsProxyAPIKeyIDParserRejectsInvalidValues(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)

	for _, raw := range []string{"", "abc", "0", "-1", "999999999999999999999", "1.5", "1e3"} {
		query := "?limit=50&offset=0&proxy_api_key_id=" + raw
		response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests"+query, nil, runtimeModelHeader(profileID))
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("proxy_api_key_id=%q: expected 422, got %d", raw, response.StatusCode)
		}
		assertStatus(t, response, http.StatusUnprocessableEntity)
		assertStructuredErrorCode(t, response, "invalid_proxy_api_key_id")
	}

	// Duplicate parameter values are rejected (never silently take the first).
	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0&proxy_api_key_id=1&proxy_api_key_id=2", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusUnprocessableEntity)
	assertStructuredErrorCode(t, response, "invalid_proxy_api_key_id")
}

// TestRequestLogsProxyAPIKeyIDFilterMatchesBeforePagination verifies the
// filter is server-applied before COUNT/pagination and composes with other
// ordinary filters.
func TestRequestLogsProxyAPIKeyIDFilterMatchesBeforePagination(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedProxyKeyAttributedRequests(t, harness, profileID)

	// Current key: two rows.
	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=1&offset=0&proxy_api_key_id=201", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["total"] != float64(2) {
		t.Fatalf("expected total=2 for current key filter, got %+v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected limit=1 to page one item, got %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["proxy_api_key_id"] != float64(201) {
		t.Fatalf("expected item to carry proxy_api_key_id=201, got %+v", first)
	}
	if first["proxy_api_key_name_snapshot"] != "current-key" {
		t.Fatalf("expected item to carry name snapshot, got %+v", first)
	}
	if first["proxy_api_key_attribution_state"] != "identified" {
		t.Fatalf("expected item attribution state identified, got %+v", first)
	}
	if first["proxy_api_key_auth_enforced_at_request"] != false {
		t.Fatalf("expected item auth_enforced=false for the permissive row, got %+v", first)
	}

	// Deleted key: historical snapshot identity still matches.
	deletedResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0&proxy_api_key_id=202", nil, runtimeModelHeader(profileID))
	assertStatus(t, deletedResponse, http.StatusOK)
	decodeJSONResponse(t, deletedResponse, &payload)
	if payload["total"] != float64(2) {
		t.Fatalf("expected total=2 for deleted key filter via snapshots, got %+v", payload)
	}
	deletedItems := payload["items"].([]any)
	for _, rawItem := range deletedItems {
		item := rawItem.(map[string]any)
		if item["proxy_api_key_id"] != float64(202) {
			t.Fatalf("expected deleted-key filter to return only snapshot-matched rows, got %+v", item)
		}
	}

	// AND composition: key + status filter narrows.
	composedResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0&proxy_api_key_id=201&status_family=4xx", nil, runtimeModelHeader(profileID))
	assertStatus(t, composedResponse, http.StatusOK)
	decodeJSONResponse(t, composedResponse, &payload)
	if payload["total"] != float64(0) {
		t.Fatalf("expected key+status AND composition to return 0 rows, got %+v", payload)
	}

	// Unknown key id: empty result, not an error and not a dropped filter.
	unknownResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0&proxy_api_key_id=999", nil, runtimeModelHeader(profileID))
	assertStatus(t, unknownResponse, http.StatusOK)
	decodeJSONResponse(t, unknownResponse, &payload)
	if payload["total"] != float64(0) {
		t.Fatalf("expected unknown key id to return empty cohort, got %+v", payload)
	}
}

// TestRequestLogsProxyAPIKeyFilterOptionsContract covers the searchable bounded
// option source: current + historical union, deleted fallback, selected_id and
// limit cap.
func TestRequestLogsProxyAPIKeyFilterOptionsContract(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedProxyKeyAttributedRequests(t, harness, profileID)

	// Current configured keys are part of the option source even without rows
	// in the window (no proxy_api_keys rows were seeded in this harness, so
	// only snapshot-derived options appear).
	response := harness.requestJSON(t, http.MethodGet, "/api/stats/request-filter-options/proxy-api-keys?limit=50", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected two snapshot-derived options (201, 202), got %+v", payload)
	}
	byID := map[float64]map[string]any{}
	for _, rawItem := range items {
		item := rawItem.(map[string]any)
		byID[item["proxy_api_key_id"].(float64)] = item
	}
	if item := byID[202]; item["configured"] != false || item["proxy_api_key_name"] != "deleted-key" {
		t.Fatalf("expected deleted option to use latest in-window snapshot with configured=false, got %+v", item)
	}
	if _, ok := payload["resolved_from_time"].(string); !ok {
		t.Fatalf("expected resolved_from_time in options payload, got %+v", payload)
	}

	// q search: name substring match.
	searchResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/request-filter-options/proxy-api-keys?q=deleted", nil, runtimeModelHeader(profileID))
	assertStatus(t, searchResponse, http.StatusOK)
	decodeJSONResponse(t, searchResponse, &payload)
	searchItems := payload["items"].([]any)
	if len(searchItems) != 1 || searchItems[0].(map[string]any)["proxy_api_key_id"] != float64(202) {
		t.Fatalf("expected q=deleted to match the deleted option, got %+v", payload)
	}

	// selected_id returns the option separately even when outside the page.
	selectedResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/request-filter-options/proxy-api-keys?selected_id=202&limit=1", nil, runtimeModelHeader(profileID))
	assertStatus(t, selectedResponse, http.StatusOK)
	decodeJSONResponse(t, selectedResponse, &payload)
	selected := payload["selected"].(map[string]any)
	if selected["proxy_api_key_id"] != float64(202) || selected["configured"] != false {
		t.Fatalf("expected selected deleted option, got %+v", payload)
	}

	// selected_id of a key with no surviving snapshot renders #<id>.
	ghostResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/request-filter-options/proxy-api-keys?selected_id=777", nil, runtimeModelHeader(profileID))
	assertStatus(t, ghostResponse, http.StatusOK)
	decodeJSONResponse(t, ghostResponse, &payload)
	ghost := payload["selected"].(map[string]any)
	if ghost["proxy_api_key_name"] != "#777" || ghost["configured"] != false {
		t.Fatalf("expected ghost selected option to render #777 with configured=false, got %+v", payload)
	}

	// Invalid selected_id is a typed 422.
	badSelectedResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/request-filter-options/proxy-api-keys?selected_id=abc", nil, runtimeModelHeader(profileID))
	assertStatus(t, badSelectedResponse, http.StatusUnprocessableEntity)
}

// TestRequestLogsIngressChainViewEXISTSSemantics verifies view=ingress_chains:
// outer ingress selection with a parameterized EXISTS match, bounded full
// retained chains, per-row matched markers and the deep-link shape.
func TestRequestLogsIngressChainViewEXISTSSemantics(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedProxyKeyAttributedRequests(t, harness, profileID)

	// Key filter selects ingress groups via EXISTS; each chain keeps the full
	// retained rows and marks which rows matched.
	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?view=ingress_chains&proxy_api_key_id=201&chain_limit=50", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["view"] != "ingress_chains" {
		t.Fatalf("expected ingress_chains view marker, got %+v", payload)
	}
	if payload["retained_ingress_total"] != float64(2) {
		t.Fatalf("expected retained_ingress_total=2 for key 201, got %+v", payload)
	}
	chains := payload["items"].([]any)
	if len(chains) != 2 {
		t.Fatalf("expected two ingress chains for key 201, got %+v", payload)
	}
	chainByID := map[string]map[string]any{}
	for _, rawChain := range chains {
		chain := rawChain.(map[string]any)
		chainByID[chain["ingress_request_id"].(string)] = chain
	}
	for _, ingress := range []string{"ingress-current-1", "ingress-current-2"} {
		chain := chainByID[ingress]
		rows := chain["rows"].([]any)
		if len(rows) != 1 {
			t.Fatalf("expected one retained row in chain %s, got %+v", ingress, chain)
		}
		row := rows[0].(map[string]any)
		if row["matched_by_filter"] != true {
			t.Fatalf("expected chain row to be marked matched, got %+v", row)
		}
		if chain["retained_row_count"] != float64(1) || chain["matched_row_count"] != float64(1) {
			t.Fatalf("expected chain counts 1/1, got %+v", chain)
		}
	}

	// A chain containing a non-matching sibling row keeps it with
	// matched_by_filter=false. Seed a sibling under the same ingress as the
	// deleted key and filter for the current key.
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, status_code, response_time_ms, is_stream, success_flag, request_path, created_at, ingress_request_id, attempt_number, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request) VALUES (306, $1, 'model-a', 'openai', 500, 10, FALSE, FALSE, '/v1/chat/completions', $2, 'ingress-current-1', 2, NULL, NULL, 'none', FALSE)`, profileID, now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("seed sibling chain row: %v", err)
	}
	siblingResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?view=ingress_chains&proxy_api_key_id=201&chain_limit=50", nil, runtimeModelHeader(profileID))
	assertStatus(t, siblingResponse, http.StatusOK)
	decodeJSONResponse(t, siblingResponse, &payload)
	siblingChains := payload["items"].([]any)
	var siblingChain map[string]any
	for _, rawChain := range siblingChains {
		chain := rawChain.(map[string]any)
		if chain["ingress_request_id"] == "ingress-current-1" {
			siblingChain = chain
		}
	}
	if siblingChain == nil {
		t.Fatalf("expected ingress-current-1 chain after sibling seed, got %+v", payload)
	}
	rows := siblingChain["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("expected bounded full retained chain of 2 rows, got %+v", siblingChain)
	}
	if siblingChain["retained_row_count"] != float64(2) || siblingChain["matched_row_count"] != float64(1) {
		t.Fatalf("expected chain counts 2/1, got %+v", siblingChain)
	}
	matchedCount := 0
	for _, rawRow := range rows {
		row := rawRow.(map[string]any)
		if row["matched_by_filter"] == true {
			matchedCount++
		}
	}
	if matchedCount != 1 {
		t.Fatalf("expected exactly one matched row in chain, got %+v", rows)
	}

	// Invalid view values are rejected.
	badViewResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?view=other", nil, runtimeModelHeader(profileID))
	assertStatus(t, badViewResponse, http.StatusBadRequest)
}

// TestRequestLogsDeepLinkRoundTripsProxyAPIKeyID verifies the ledger deep-link
// URL contract survives the request parser.
func TestRequestLogsDeepLinkRoundTripsProxyAPIKeyID(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedProxyKeyAttributedRequests(t, harness, profileID)

	response := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests?proxy_api_key_id=%d&from_time=%s&to_time=%s", 201, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339)), nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["total"] != float64(2) {
		t.Fatalf("expected deep-link window to match both current-key rows, got %+v", payload)
	}
}

func assertStructuredErrorCode(t *testing.T, response *http.Response, wantCode string) {
	t.Helper()
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeJSONResponse(t, response, &payload)
	if payload.Error.Code != wantCode {
		t.Fatalf("expected structured error code %q, got %+v", wantCode, payload)
	}
}

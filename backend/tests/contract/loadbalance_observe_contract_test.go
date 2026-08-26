package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func observeIssueEventsContext(t *testing.T, harness *contractHarness, profileID int, preset string) string {
	t.Helper()
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": preset}, modelHeader(profileID), http.StatusOK)
	token, ok := payload["query_context"].(string)
	if !ok || token == "" {
		t.Fatalf("expected issued events query context, got %+v", payload)
	}
	return token
}

func observeInsertEvent(t *testing.T, harness *contractHarness, seed observeEventSeed) {
	t.Helper()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("loadbalance_events", seed.CreatedAt))
	nextRetryAt := (*time.Time)(nil)
	if seed.CooldownSeconds > 0 {
		resolved := seed.CreatedAt.Add(time.Duration(seed.CooldownSeconds * float64(time.Second)))
		nextRetryAt = &resolved
	}
	lastRetryDelayMS := int(seed.CooldownSeconds * 1000)
	var bannedUntilAt, lastSuccessAt *time.Time
	if !seed.BannedUntilAt.IsZero() {
		bannedUntilAt = &seed.BannedUntilAt
	}
	if !seed.LastSuccessAt.IsZero() {
		lastSuccessAt = &seed.LastSuccessAt
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_events (id, profile_id, connection_id, event_type, failure_kind, admission_reason, model_config_id, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, banned_until_at, last_success_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		seed.ID, seed.ProfileID, seed.ConnectionID, seed.EventType, nullableTestString(seed.FailureKind), nullableTestString(seed.AdmissionReason), nullableTestInt(seed.ModelConfigID), seed.ConsecutiveFailures, nullableTestTime(nextRetryAt), lastRetryDelayMS, nullableTestString(seed.ModelID), nullableTestInt(seed.EndpointID), nullableTestString(seed.BanMode), nullableTestInt(seed.PolicyCycleRetryAttemptLimit), nullableTestInt(seed.PolicyBanCumulativeRetryAttemptThreshold), nullableTestTime(bannedUntilAt), nullableTestTime(lastSuccessAt), seed.CreatedAt); err != nil {
		t.Fatalf("insert observe loadbalance event %d: %v", seed.ID, err)
	}
}

type observeEventSeed struct {
	ID, ProfileID, ConnectionID, ConsecutiveFailures int
	EventType                                        string
	FailureKind, AdmissionReason, ModelID, BanMode   *string
	ModelConfigID, EndpointID                        *int
	PolicyCycleRetryAttemptLimit                     *int
	PolicyBanCumulativeRetryAttemptThreshold         *int
	CooldownSeconds                                  float64
	BannedUntilAt, LastSuccessAt, CreatedAt          time.Time
}

func TestGlobalCurrentStateContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	// no_config: before any models exist the completeness is a non-error
	// no_config state, not an error and not "healthy".
	empty := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state", nil, modelHeader(profileID), http.StatusOK)
	if empty["scope"] != "process" {
		t.Fatalf("unexpected empty current-state payload %+v", empty)
	}
	emptyCompleteness := asMap(t, empty["completeness"])
	if emptyCompleteness["state"] != "no_config" || emptyCompleteness["complete"] != true || jsonInt(t, emptyCompleteness["configured_target_count"]) != 0 {
		t.Fatalf("expected no_config completeness, got %+v", emptyCompleteness)
	}

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Observe Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "observe-model", stringPtr("Observe Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Observe Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)

	// partial: one configured target, none observed yet (the runtime store is
	// process-local and empty); the row must be unobserved with null fields,
	// never faked as available.
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state", nil, modelHeader(profileID), http.StatusOK)
	completeness := asMap(t, payload["completeness"])
	if completeness["state"] != "unobserved" || completeness["complete"] != false || jsonInt(t, completeness["configured_target_count"]) != 1 || jsonInt(t, completeness["observed_target_count"]) != 0 {
		t.Fatalf("expected unobserved completeness, got %+v", completeness)
	}
	item := currentStateItemByTarget(t, payload, connectionID)
	if item["observation_state"] != "unobserved" || item["state"] != nil || item["available"] != nil || item["cycle_retry_attempts"] != nil {
		t.Fatalf("expected unobserved item with null runtime fields, got %+v", item)
	}

	// Object filters narrow the cohort but not the completeness denominator
	// beyond the cohort: filtering by the single model keeps one configured row.
	modelFiltered := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state?model_id=observe-model", nil, modelHeader(profileID), http.StatusOK)
	if len(modelFiltered["items"].([]any)) != 1 {
		t.Fatalf("expected model-filtered current-state rows, got %+v", modelFiltered)
	}

	// unobserved state filter only matches rows without a state key.
	unobservedOnly := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state?state=unobserved", nil, modelHeader(profileID), http.StatusOK)
	if len(unobservedOnly["items"].([]any)) != 1 {
		t.Fatalf("expected unobserved filter to keep the unobserved row, got %+v", unobservedOnly)
	}
	availableOnly := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state?state=available", nil, modelHeader(profileID), http.StatusOK)
	if len(availableOnly["items"].([]any)) != 0 {
		t.Fatalf("expected no available rows, got %+v", availableOnly)
	}

	// Observed rows expose runtime fields; the state key comes from the
	// process-local store only.
	nowAt := time.Now().UTC()
	harness.runtimeService.RuntimeState().SeedConnectionState(profileID, modelConfigID, connectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:            connectionID,
		CycleRetryAttempts:      2,
		CumulativeRetryAttempts: 5,
		BanMode:                 "temporary",
		BannedUntilAt:           timePtr(nowAt.Add(30 * time.Minute)),
	}, nowAt.Add(-1*time.Hour), nowAt)

	ready := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state", nil, modelHeader(profileID), http.StatusOK)
	completeness = asMap(t, ready["completeness"])
	if completeness["state"] != "ready" || completeness["complete"] != true || jsonInt(t, completeness["observed_target_count"]) != 1 {
		t.Fatalf("expected ready completeness, got %+v", completeness)
	}
	item = currentStateItemByTarget(t, ready, connectionID)
	if item["observation_state"] != "observed" || item["state"] != "banned" || item["available"] != false || jsonInt(t, item["cycle_retry_attempts"]) != 2 {
		t.Fatalf("expected observed banned item, got %+v", item)
	}

	// Cursor pagination with stable configuration identity and a stale 409
	// after the configuration revision changes.
	modelInsertModel(t, harness, profileID, &vendorID, "openai", "observe-model-b", stringPtr("B Model"), "native", &strategyID, true)
	secondEndpointID := modelInsertEndpoint(t, harness, profileID, "Observe Endpoint B", 0)
	modelInsertConnection(t, harness, profileID, modelConfigID, secondEndpointID, 1, true, nil)

	page := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state?limit=1", nil, modelHeader(profileID), http.StatusOK)
	if page["has_more"] != true || page["next_cursor"] == nil {
		t.Fatalf("expected first current-state page with cursor, got %+v", page)
	}
	cursorValue := fmt.Sprint(page["next_cursor"])
	secondPage := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/current-state?limit=1&cursor="+url.QueryEscape(cursorValue), nil, modelHeader(profileID), http.StatusOK)
	if len(secondPage["items"].([]any)) != 1 || secondPage["has_more"] != false {
		t.Fatalf("expected final current-state page, got %+v", secondPage)
	}

	// A payload edit bumps the configuration revision: the old cursor is stale.
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/loadbalance/strategies/%d", strategyID), map[string]any{
		"name": "Observe Strategy", "legacy_strategy_type": "fill-first", "retry_base_delay_ms": 61000,
	}, modelHeader(profileID), http.StatusOK)
	stale := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/current-state?limit=1&cursor="+url.QueryEscape(cursorValue), nil, modelHeader(profileID))
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("expected stale current-state cursor 409, got %d", stale.StatusCode)
	}
	var stalePayload struct {
		Code string `json:"code"`
	}
	decodeContractResponse(t, stale, &stalePayload)
	if stalePayload.Code != "current_state_cursor_stale" {
		t.Fatalf("expected current_state_cursor_stale code, got %+v", stalePayload)
	}

	// no-store cache control.
	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/current-state", nil, modelHeader(profileID))
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("expected current-state Cache-Control no-store, got %q", response.Header.Get("Cache-Control"))
	}
}

func currentStateItemByTarget(t *testing.T, payload map[string]any, targetID int) map[string]any {
	t.Helper()
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("expected current-state items array, got %+v", payload)
	}
	for _, raw := range items {
		item := asMap(t, raw)
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) == targetID {
			return item
		}
	}
	t.Fatalf("expected current-state item for target %d, got %+v", targetID, payload)
	return nil
}

func TestEventsTimelineKeysetTieBreakContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	nowAt := time.Now().UTC()
	// The canonical same-second fixture: 9 -> 10 -> 11 in causal order.
	observeInsertEvent(t, harness, observeEventSeed{ID: 9, ProfileID: profileID, ConnectionID: 1, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 30, ModelID: stringPtr("tie-model"), CreatedAt: nowAt.Add(-2 * time.Minute)})
	observeInsertEvent(t, harness, observeEventSeed{ID: 10, ProfileID: profileID, ConnectionID: 1, EventType: "unbanned", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 0, ModelID: stringPtr("tie-model"), BanMode: stringPtr("temporary"), CreatedAt: nowAt.Add(-2 * time.Minute)})
	observeInsertEvent(t, harness, observeEventSeed{ID: 11, ProfileID: profileID, ConnectionID: 1, EventType: "recovered", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 0, ModelID: stringPtr("tie-model"), LastSuccessAt: nowAt.Add(-2 * time.Minute), CreatedAt: nowAt.Add(-2 * time.Minute)})

	queryContext := observeIssueEventsContext(t, harness, profileID, "24h")

	asc := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&sort_order=asc&limit=10", nil, modelHeader(profileID), http.StatusOK)
	ascItems := asc["items"].([]any)
	if len(ascItems) != 3 || asMap(t, ascItems[0])["event_id"] != "9" || asMap(t, ascItems[1])["event_id"] != "10" || asMap(t, ascItems[2])["event_id"] != "11" {
		t.Fatalf("expected asc tie-break 9 -> 10 -> 11, got %+v", asc)
	}

	desc := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&sort_order=desc&limit=10", nil, modelHeader(profileID), http.StatusOK)
	descItems := desc["items"].([]any)
	if len(descItems) != 3 || asMap(t, descItems[0])["event_id"] != "11" || asMap(t, descItems[1])["event_id"] != "10" || asMap(t, descItems[2])["event_id"] != "9" {
		t.Fatalf("expected desc tie-break 11 -> 10 -> 9, got %+v", desc)
	}

	// Keyset pagination preserves the tie-break across pages (asc).
	page1 := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&sort_order=asc&limit=2", nil, modelHeader(profileID), http.StatusOK)
	page1Items := page1["items"].([]any)
	if len(page1Items) != 2 || page1["has_more"] != true || page1["next_cursor"] == nil {
		t.Fatalf("expected asc page one, got %+v", page1)
	}
	page2 := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&sort_order=asc&limit=2&cursor="+url.QueryEscape(fmt.Sprint(page1["next_cursor"])), nil, modelHeader(profileID), http.StatusOK)
	page2Items := page2["items"].([]any)
	if len(page2Items) != 1 || asMap(t, page2Items[0])["event_id"] != "11" || page2["has_more"] != false {
		t.Fatalf("expected asc page two to continue the tie-break, got %+v", page2)
	}

	// Cursor scope mismatch (different limit) is a typed 422.
	mismatch := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&sort_order=asc&limit=3&cursor="+url.QueryEscape(fmt.Sprint(page1["next_cursor"])), nil, modelHeader(profileID))
	if mismatch.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected event cursor scope mismatch 422, got %d", mismatch.StatusCode)
	}

	// Filters are server-before-pagination: event_type narrows the timeline.
	bannedOnly := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&event_type=unbanned&limit=10", nil, modelHeader(profileID), http.StatusOK)
	if len(bannedOnly["items"].([]any)) != 1 || asMap(t, bannedOnly["items"].([]any)[0])["event_id"] != "10" {
		t.Fatalf("expected unbanned filter to return only event 10, got %+v", bannedOnly)
	}

	// Invalid context is a typed 422; detail out of bounds is a 404.
	invalid := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events?query_context=forged", nil, modelHeader(profileID))
	if invalid.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected invalid query context 422, got %d", invalid.StatusCode)
	}
	// A context whose window excludes the events makes detail 404.
	customPayload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{
		"requested_preset": "custom",
		"custom_from_time": nowAt.Add(-6 * time.Hour).Format(time.RFC3339),
		"custom_to_time":   nowAt.Add(-5 * time.Hour).Format(time.RFC3339),
	}, modelHeader(profileID), http.StatusOK)
	oldWindow := fmt.Sprint(customPayload["query_context"])
	detail404 := harness.requestJSON(t, harness.client, http.MethodGet, "/api/loadbalance/events/9?query_context="+url.QueryEscape(oldWindow), nil, modelHeader(profileID))
	if detail404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected out-of-bounds event detail 404, got %d", detail404.StatusCode)
	}
	detailOK := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events/9?query_context="+url.QueryEscape(queryContext), nil, modelHeader(profileID), http.StatusOK)
	if detailOK["event_id"] != "9" || detailOK["event_type"] != "retry_scheduled" {
		t.Fatalf("expected in-bounds event detail, got %+v", detailOK)
	}
	// Detail summary V1 for retry_scheduled.
	summary := asMap(t, detailOK["summary"])
	params := asMap(t, summary["params"])
	if summary["code"] != "loadbalance.retry_scheduled" || params["evidence_state"] != "complete" || params["failure_kind"] != "timeout" || jsonInt(t, params["cycle_retry_attempts"]) != 1 || params["next_retry_at"] == nil {
		t.Fatalf("expected retry_scheduled V1 summary, got %+v", detailOK)
	}
}

func TestEventsAdmissionReasonAndModelIdentityContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	nowAt := time.Now().UTC()

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Observe Admission Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "admission-model", stringPtr("Admission Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Admission Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)

	// A legacy admission row: no admission_reason, stale failure_kind was
	// normalized by the migration; evidence is legacy_incomplete.
	observeInsertEvent(t, harness, observeEventSeed{ID: 2000, ProfileID: profileID, ConnectionID: connectionID, EventType: "admission_rejected", ConsecutiveFailures: 0, ModelID: stringPtr("admission-model"), ModelConfigID: &modelConfigID, EndpointID: &endpointID, CreatedAt: nowAt.Add(-2 * time.Minute)})

	// New writer: admission reason comes from the decision site, failure_kind
	// stays null, and model_config_id + public model_id are persisted together.
	state := loadbalancedomain.RuntimeConnectionState{ConnectionID: connectionID}
	if err := loadbalancedomain.InsertRuntimeAdmissionRejectedEvent(context.Background(), harness.conn, s15LoadbalancePartitionEnsurer{harness: harness}, profileID, modelConfigID, connectionID, "qps_limit", state, nowAt.Add(-1*time.Minute)); err != nil {
		t.Fatalf("insert runtime admission rejected event: %v", err)
	}

	queryContext := observeIssueEventsContext(t, harness, profileID, "24h")
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&admission_reason=qps_limit&limit=10", nil, modelHeader(profileID), http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one qps_limit admission event, got %+v", payload)
	}
	event := asMap(t, items[0])
	if event["event_type"] != "admission_rejected" || event["admission_reason"] != "qps_limit" || event["failure_kind"] != nil {
		t.Fatalf("expected admission event with reason and null failure kind, got %+v", event)
	}
	summary := asMap(t, event["summary"])
	params := asMap(t, summary["params"])
	if summary["code"] != "loadbalance.admission_rejected" || params["admission_reason"] != "qps_limit" || params["evidence_state"] != "complete" || params["failure_kind"] != nil {
		t.Fatalf("expected admission V1 summary, got %+v", event)
	}
	model := asMap(t, event["model"])
	if jsonInt(t, model["model_config_id"]) != modelConfigID || model["id"] != "admission-model" || model["attribution"] != "identified" || model["configured"] != true {
		t.Fatalf("expected identified configured model projection, got %+v", model)
	}
	target := asMap(t, event["terminal_target"])
	if jsonInt(t, target["id"]) != connectionID || jsonInt(t, target["owner_model_config_id"]) != modelConfigID {
		t.Fatalf("expected terminal target with owner model, got %+v", target)
	}

	// Legacy admission row: legacy_incomplete evidence, no reason guessing.
	all := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&event_type=admission_rejected&limit=10", nil, modelHeader(profileID), http.StatusOK)
	allItems := all["items"].([]any)
	if len(allItems) != 2 {
		t.Fatalf("expected both admission events, got %+v", all)
	}
	for _, raw := range allItems {
		item := asMap(t, raw)
		itemSummary := asMap(t, item["summary"])
		itemParams := asMap(t, itemSummary["params"])
		if item["event_id"] == "2000" {
			if itemParams["evidence_state"] != "legacy_incomplete" || itemParams["admission_reason"] != nil {
				t.Fatalf("expected legacy admission evidence legacy_incomplete, got %+v", item)
			}
		} else if itemParams["evidence_state"] != "complete" {
			t.Fatalf("expected new admission evidence complete, got %+v", item)
		}
	}

	// Deleted model: numeric identity persists, configured=false, no link.
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM model_configs WHERE id = $1`, modelConfigID); err != nil {
		t.Fatalf("delete model for identity contract: %v", err)
	}
	detail := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events/2000?query_context="+url.QueryEscape(queryContext), nil, modelHeader(profileID), http.StatusOK)
	detailModel := asMap(t, detail["model"])
	if detailModel["configured"] != false || detailModel["attribution"] != "identified" {
		t.Fatalf("expected deleted model with identified + configured=false, got %+v", detailModel)
	}
}

func putRetentionPolicy(t *testing.T, harness *contractHarness, profileID int, policies map[string]any) {
	t.Helper()
	canonicalPolicies := map[string]any{
		"request_logs_retention_days":       nil,
		"statistics_retention_days":         nil,
		"audit_logs_retention_days":         nil,
		"loadbalance_events_retention_days": nil,
	}
	for key, value := range policies {
		canonicalPolicies[key] = value
	}
	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/settings/log-retention", nil, modelHeader(profileID))
	assertStatus(t, getResponse, http.StatusOK)
	var parsed struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(readResponseBody(t, getResponse)), &parsed); err != nil {
		t.Fatalf("decode log-retention response: %v", err)
	}
	body := map[string]any{
		"operation_id":      "lb-retention-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"expected_revision": parsed.Revision,
		"policies":          canonicalPolicies,
	}
	preflight := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/maintenance/log-retention/preflights", map[string]any{
		"kind":                       "policy_change",
		"operation_id":               body["operation_id"],
		"preflight_attempt_id":       "lb-retention-attempt",
		"expected_settings_revision": parsed.Revision,
		"policies":                   canonicalPolicies,
	}, modelHeader(profileID), http.StatusCreated)
	token, ok := preflight["preflight_token"].(string)
	if !ok || token == "" {
		t.Fatalf("expected preflight token, got %+v", preflight)
	}
	body["preflight_token"] = token
	body["confirmation"] = map[string]any{"keyword": "DELETE"}
	putResponse := harness.requestJSON(t, harness.client, http.MethodPut, "/api/settings/log-retention", body, modelHeader(profileID))
	assertStatus(t, putResponse, http.StatusOK)
}

func TestEventsRequestContextHandoffContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	nowAt := time.Now().UTC()

	// Configure a 30-day request retention so the ±15 minute window survives
	// clipping (v2 contract: destructive NULL->N needs a fresh preflight).
	putRetentionPolicy(t, harness, profileID, map[string]any{"request_logs_retention_days": 30})

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Observe Handoff Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "handoff-model", stringPtr("Handoff Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Handoff Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)

	observeInsertEvent(t, harness, observeEventSeed{ID: 3000, ProfileID: profileID, ConnectionID: connectionID, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 30, ModelID: stringPtr("handoff-model"), ModelConfigID: &modelConfigID, EndpointID: &endpointID, CreatedAt: nowAt.Add(-2 * time.Minute)})

	queryContext := observeIssueEventsContext(t, harness, profileID, "24h")
	detail := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events/3000?query_context="+url.QueryEscape(queryContext), nil, modelHeader(profileID), http.StatusOK)
	filtersRaw, ok := detail["request_context_filters"].(map[string]any)
	if !ok {
		t.Fatalf("expected request context filters, got %+v", detail)
	}
	if filtersRaw["schema_version"] != float64(1) || filtersRaw["kind"] != "contextual_window" || filtersRaw["correlation"] != "not_exact" {
		t.Fatalf("unexpected request context filters %+v", filtersRaw)
	}
	fromTime, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(filtersRaw["from_time"]))
	toTime, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(filtersRaw["to_time"]))
	eventAt := nowAt.Add(-2 * time.Minute)
	if fromTime.Before(eventAt.Add(-16*time.Minute)) || fromTime.After(eventAt.Add(-14*time.Minute)) {
		t.Fatalf("expected event minus 15 minutes as the window start, got %s", fromTime)
	}
	if toTime.Before(nowAt) || toTime.After(eventAt.Add(16*time.Minute)) {
		t.Fatalf("expected the window end to be event plus 15 minutes clipped to now, got %s", toTime)
	}
	if filtersRaw["model_id"] != "handoff-model" || filtersRaw["terminal_target_id"] != float64(connectionID) || filtersRaw["endpoint_id"] != float64(endpointID) {
		t.Fatalf("expected typed object filters, got %+v", filtersRaw)
	}
	if detail["request_context_unavailable_reason"] != nil {
		t.Fatalf("expected no unavailable reason with a retained window, got %+v", detail)
	}

	// With no request-log retention overlap the handoff is disabled with the
	// typed reason and null filters.
	putRetentionPolicy(t, harness, profileID, map[string]any{"request_logs_retention_days": 1})
	nowAt = time.Now().UTC()
	// The event lies outside the 1-day request retention but inside a valid
	// 2-day custom event window, so detail is reachable while the handoff has
	// no retention overlap.
	observeInsertEvent(t, harness, observeEventSeed{ID: 3001, ProfileID: profileID, ConnectionID: connectionID, EventType: "retry_scheduled", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, CooldownSeconds: 30, ModelID: stringPtr("handoff-model"), ModelConfigID: &modelConfigID, EndpointID: &endpointID, CreatedAt: nowAt.Add(-25 * 24 * time.Hour)})
	oldContext := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{
		"requested_preset": "custom",
		"custom_from_time": nowAt.Add(-26 * 24 * time.Hour).Format(time.RFC3339),
		"custom_to_time":   nowAt.Add(-24 * 24 * time.Hour).Format(time.RFC3339),
	}, modelHeader(profileID), http.StatusOK)
	oldWindow := fmt.Sprint(oldContext["query_context"])
	detailOld := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events/3001?query_context="+url.QueryEscape(oldWindow), nil, modelHeader(profileID), http.StatusOK)
	if detailOld["request_context_filters"] != nil || detailOld["request_context_unavailable_reason"] != "request_retention_no_overlap" {
		t.Fatalf("expected retention-clipped handoff disabled, got %+v", detailOld)
	}
}

func observeSetRetentionPurgeState(t *testing.T, harness *contractHarness, dataset string, state string) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE log_retention_policy_resources SET purge_state = $1 WHERE dataset = $2`, state, dataset); err != nil {
		t.Fatalf("set %s purge state to %s: %v", dataset, state, err)
	}
}

// TestEventsUnnamedTerminalTargetProjectionContract pins the NULL/blank
// connection-name contract: an existing terminal target with no usable name
// still loads and projects as configured with the #<connection_id> fallback
// label and a resolvable owner, while a missing connection row stays
// configured=false without an owner.
func TestEventsUnnamedTerminalTargetProjectionContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	nowAt := time.Now().UTC()

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Observe Unnamed Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "unnamed-model", stringPtr("Unnamed Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Unnamed Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)

	observeInsertEvent(t, harness, observeEventSeed{ID: 4000, ProfileID: profileID, ConnectionID: connectionID, EventType: "banned", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, ModelID: stringPtr("unnamed-model"), ModelConfigID: &modelConfigID, EndpointID: &endpointID, CreatedAt: nowAt.Add(-2 * time.Minute)})
	missingConnectionID := connectionID + 987654
	observeInsertEvent(t, harness, observeEventSeed{ID: 4001, ProfileID: profileID, ConnectionID: missingConnectionID, EventType: "banned", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, ModelID: stringPtr("unnamed-model"), ModelConfigID: &modelConfigID, EndpointID: &endpointID, CreatedAt: nowAt.Add(-2 * time.Minute)})

	// connections.name is legally NULL; the timeline must still load instead
	// of failing the label scan.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET name = NULL WHERE id = $1`, connectionID); err != nil {
		t.Fatalf("clear connection name: %v", err)
	}

	assertConfiguredUnnamedTarget := func(item map[string]any) {
		t.Helper()
		target := asMap(t, item["terminal_target"])
		if jsonInt(t, target["id"]) != connectionID || target["label"] != fmt.Sprintf("#%d", connectionID) || target["configured"] != true || jsonInt(t, target["owner_model_config_id"]) != modelConfigID {
			t.Fatalf("expected configured unnamed target projection, got %+v", target)
		}
	}

	queryContext := observeIssueEventsContext(t, harness, profileID, "24h")
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events?query_context="+url.QueryEscape(queryContext)+"&limit=10", nil, modelHeader(profileID), http.StatusOK)
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected both events on the unnamed-target timeline, got %+v", payload)
	}
	itemByID := map[string]map[string]any{}
	for _, raw := range items {
		item := asMap(t, raw)
		itemByID[fmt.Sprint(item["event_id"])] = item
	}
	assertConfiguredUnnamedTarget(itemByID["4000"])
	missingTarget := asMap(t, itemByID["4001"]["terminal_target"])
	if jsonInt(t, missingTarget["id"]) != missingConnectionID || missingTarget["label"] != fmt.Sprintf("#%d", missingConnectionID) || missingTarget["configured"] != false || missingTarget["owner_model_config_id"] != nil {
		t.Fatalf("expected missing target configured=false without owner, got %+v", missingTarget)
	}

	detail := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events/4000?query_context="+url.QueryEscape(queryContext), nil, modelHeader(profileID), http.StatusOK)
	assertConfiguredUnnamedTarget(detail)

	// A whitespace-only name is equally unusable and keeps the same contract.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET name = '   ' WHERE id = $1`, connectionID); err != nil {
		t.Fatalf("blank connection name: %v", err)
	}
	blankDetail := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/loadbalance/events/4000?query_context="+url.QueryEscape(queryContext), nil, modelHeader(profileID), http.StatusOK)
	assertConfiguredUnnamedTarget(blankDetail)
}

// TestEventsRequestLogPurgeInProgressContract pins the shared request-log
// retention-floor branch: while the request-logs source is running or awaiting
// recovery, both the events list and a hit detail return the typed 503 with
// the existing Request Logs purge detail instead of an opaque 500.
func TestEventsRequestLogPurgeInProgressContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	nowAt := time.Now().UTC()

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Observe Purge Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "purge-model", stringPtr("Purge Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Purge Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)

	observeInsertEvent(t, harness, observeEventSeed{ID: 4100, ProfileID: profileID, ConnectionID: connectionID, EventType: "banned", FailureKind: stringPtr("timeout"), ConsecutiveFailures: 1, ModelID: stringPtr("purge-model"), ModelConfigID: &modelConfigID, EndpointID: &endpointID, CreatedAt: nowAt.Add(-2 * time.Minute)})

	queryContext := observeIssueEventsContext(t, harness, profileID, "24h")
	listPath := "/api/loadbalance/events?query_context=" + url.QueryEscape(queryContext)
	detailPath := "/api/loadbalance/events/4100?query_context=" + url.QueryEscape(queryContext)

	assertRequestLogPurgeUnavailable := func(response *http.Response) {
		t.Helper()
		var payload struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		decodeContractResponse(t, response, &payload)
		if payload.Code != "request_log_purge_in_progress" || payload.Detail != "request logs are temporarily unavailable while retention cleanup is publishing" {
			t.Fatalf("expected typed request log purge payload, got %+v", payload)
		}
	}

	for _, purgeState := range []string{"running", "recovery_required"} {
		observeSetRetentionPurgeState(t, harness, "request_logs", purgeState)
		listResponse := harness.requestJSON(t, harness.client, http.MethodGet, listPath, nil, modelHeader(profileID))
		if listResponse.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected request-log purge list 503 in state %s, got %d body %s", purgeState, listResponse.StatusCode, readResponseBody(t, listResponse))
		}
		assertRequestLogPurgeUnavailable(listResponse)
		detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, detailPath, nil, modelHeader(profileID))
		if detailResponse.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected request-log purge detail 503 in state %s, got %d body %s", purgeState, detailResponse.StatusCode, readResponseBody(t, detailResponse))
		}
		assertRequestLogPurgeUnavailable(detailResponse)
	}
}

// TestEventsAllPresetEventsPurgeInProgressContract pins preset=all issuance:
// while the loadbalance-events source is running or awaiting recovery, issuing
// the all-history context returns the typed 503 with the existing Events purge
// detail, and idle issuance keeps working.
func TestEventsAllPresetEventsPurgeInProgressContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	idleIssue := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": "all"}, modelHeader(profileID), http.StatusOK)
	if token, ok := idleIssue["query_context"].(string); !ok || token == "" {
		t.Fatalf("expected idle preset=all issuance to keep working, got %+v", idleIssue)
	}

	assertEventsPurgeUnavailable := func(response *http.Response) {
		t.Helper()
		var payload struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		decodeContractResponse(t, response, &payload)
		if payload.Code != "event_purge_in_progress" || payload.Detail != "events are temporarily unavailable while retention cleanup is publishing" {
			t.Fatalf("expected typed events purge payload, got %+v", payload)
		}
	}

	for _, purgeState := range []string{"running", "recovery_required"} {
		observeSetRetentionPurgeState(t, harness, "loadbalance_events", purgeState)
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": "all"}, modelHeader(profileID))
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected events purge preset=all 503 in state %s, got %d body %s", purgeState, response.StatusCode, readResponseBody(t, response))
		}
		assertEventsPurgeUnavailable(response)
	}

	// Restoring idle restores normal issuance; only the purge branch changed.
	observeSetRetentionPurgeState(t, harness, "loadbalance_events", "idle")
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/loadbalance/events/query-context", map[string]any{"requested_preset": "all"}, modelHeader(profileID), http.StatusOK)
}

var _ = json.Marshal

func TestNarrowCooldownResetContract(t *testing.T) {
	harness := newLoadbalanceContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Observe Narrow Reset Strategy")
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "narrow-reset-model", stringPtr("Narrow Reset Model"), "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Narrow Reset Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)

	nowAt := time.Now().UTC()
	// Seed a full observation: cooldown fields that must clear plus preserved
	// fields (QPS window/count, in-flight, last success, latency).
	latency := 812
	harness.runtimeService.RuntimeState().SeedConnectionState(profileID, modelConfigID, connectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:                        connectionID,
		WindowStartedAt:                     timePtr(nowAt.Add(-30 * time.Second)),
		WindowRequestCount:                  7,
		InFlightNonStream:                   2,
		InFlightStream:                      1,
		CycleRetryAttempts:                  3,
		CumulativeRetryAttempts:             9,
		NextRetryAt:                         timePtr(nowAt.Add(2 * time.Minute)),
		LastRetryDelayMS:                    240000,
		BanMode:                             "temporary",
		BannedUntilAt:                       timePtr(nowAt.Add(30 * time.Minute)),
		LastFailureKind:                     stringPtr("timeout"),
		LastSuccessAt:                       timePtr(nowAt.Add(-1 * time.Hour)),
		LastSuccessResponseHeadersLatencyMS: &latency,
	}, nowAt.Add(-2*time.Hour), nowAt)
	for range 3 {
		harness.runtimeService.RuntimeState().ClaimRoundRobinCursor(profileID, modelConfigID, 4)
	}

	payload := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, http.StatusOK)
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["cleared"] != true {
		t.Fatalf("expected narrow reset cleared=true, got %+v", payload)
	}
	state, ok := payload["state"].(map[string]any)
	if !ok {
		t.Fatalf("expected full post-reset snapshot, got %+v", payload)
	}
	// Cleared cooldown fields.
	if state["state"] != "available" || state["ban_mode"] != "off" || state["banned_until_at"] != nil || state["next_retry_at"] != nil || state["last_failure_kind"] != nil || jsonInt(t, state["cycle_retry_attempts"]) != 0 || jsonInt(t, state["cumulative_retry_attempts"]) != 0 || jsonInt(t, state["last_retry_delay_ms"]) != 0 {
		t.Fatalf("expected cleared cooldown fields, got %+v", state)
	}
	// Preserved fields.
	if jsonInt(t, state["window_request_count"]) != 7 || jsonInt(t, state["in_flight_non_stream"]) != 2 || jsonInt(t, state["in_flight_stream"]) != 1 || state["last_success_at"] == nil || jsonInt(t, state["last_success_response_headers_latency_ms"]) != 812 {
		t.Fatalf("expected QPS/in-flight/last-success/latency preserved, got %+v", state)
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, modelConfigID, 4); cursor == 0 {
		t.Fatalf("expected round-robin cursor preserved by narrow reset")
	}
	// The connection state key survives with the cleared snapshot.
	observed, exists := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	if !exists || observed.CycleRetryAttempts != 0 || observed.BanMode != "off" {
		t.Fatalf("expected state key to survive narrow reset, got %+v exists=%t", observed, exists)
	}

	// Idempotent no-op: a second reset with nothing to clear returns
	// cleared=false with the unchanged snapshot, never an error.
	again := s15JSON[map[string]any](t, harness, profileID, http.MethodPost, fmt.Sprintf("/api/loadbalance/current-state/%d/reset", connectionID), nil, http.StatusOK)
	if again["cleared"] != false || again["state"] == nil {
		t.Fatalf("expected cleared=false no-op with snapshot, got %+v", again)
	}

	// Unknown target id is a 404.
	missing := harness.requestJSON(t, harness.client, http.MethodPost, "/api/loadbalance/current-state/999999/reset", nil, modelHeader(profileID))
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("expected unknown target reset 404, got %d", missing.StatusCode)
	}
}

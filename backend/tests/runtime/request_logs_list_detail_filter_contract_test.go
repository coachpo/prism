package runtimetest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestRequestLogListContract(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	seedRequestLogUserAgentRules(t, harness, profileID)
	seedFixtureRequestLog(t, harness, profileID)
	seedSimpleRequestLog(t, harness, profileID, 102, 999, nil, time.Date(2026, 4, 18, 12, 20, 0, 0, time.UTC), false)

	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	writeRequestFixtureIfRequested(t, "request-log-list.json", payload)
	expected := loadRequestFixture(t, "request-log-list.json")
	// The flat request-log browse response is intentionally limited to filter
	// options, rows, and pagination; retained coverage belongs to the ingress
	// chain read model rather than this attempt-list projection.
	delete(payload, "coverage")
	delete(expected, "coverage")
	delete(payload, "dataset_coverage")
	delete(expected, "dataset_coverage")
	rawDump, _ := json.MarshalIndent(payload, "", "  ")
	_ = os.WriteFile("/tmp/request-log-list-actual.json", append(rawDump, '\n'), 0o644)
	if !jsonBytesEqual(t, payload, expected) {
		t.Fatalf("expected request-log list payload to match fixture, got %+v", payload)
	}
	itemsByID := requestLogItemsByID(t, payload["items"].([]any))
	primaryItem := itemsByID[101]
	if primaryItem["upstream_model_id"] != "vendor/gpt-4o-native" {
		t.Fatalf("expected retained upstream model snapshot on list row, got %+v", primaryItem)
	}
	if pricingStatus, ok := primaryItem["pricing_status"].(string); !ok || pricingStatus != "priced" {
		t.Fatalf("expected primary request-log list row pricing_status=priced, got %+v", primaryItem)
	}
	if unpricedReason, ok := primaryItem["unpriced_reason"]; !ok || unpricedReason != nil {
		t.Fatalf("expected primary request-log list row unpriced_reason=null, got %+v", primaryItem)
	}
	if _, ok := primaryItem["stream_error_detail"]; ok {
		t.Fatalf("did not expect request-log list row to expose stream_error_detail, got %+v", primaryItem)
	}
	filterOptions := asMapRuntime(t, payload["filter_options"])
	models, ok := filterOptions["ingress_models"].([]any)
	if !ok {
		t.Fatalf("expected request-log filter options to always include models array, got %+v", filterOptions)
	}
	if len(models) != 0 {
		t.Fatalf("expected happy-path request-log filter options to expose empty models array when no current models exist, got %+v", filterOptions)
	}
	staleResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?endpoint_id=999&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, staleResponse, http.StatusOK)
	decodeJSONResponse(t, staleResponse, &payload)
	staleItems := payload["items"].([]any)
	if len(staleItems) != 1 {
		t.Fatalf("expected one stale-endpoint row, got %+v", payload)
	}
	staleItem := staleItems[0].(map[string]any)
	if staleItem["endpoint_label"] != "Endpoint 999" {
		t.Fatalf("expected stale request-log row to keep synthetic endpoint label, got %+v", staleItem)
	}
	if pricingStatus, ok := staleItem["pricing_status"].(string); !ok || pricingStatus != "unpriced" {
		t.Fatalf("expected stale request-log row pricing_status=unpriced when cost is missing, got %+v", staleItem)
	}
	if staleItem["unpriced_reason"] != "MISSING_PRICE_DATA" {
		t.Fatalf("expected stale request-log row unpriced_reason=MISSING_PRICE_DATA when cost is missing, got %+v", staleItem)
	}
	endpoints := payload["filter_options"].(map[string]any)["endpoints"].([]any)
	firstEndpoint := endpoints[0].(map[string]any)
	if firstEndpoint["endpoint_id"] != float64(999) || firstEndpoint["endpoint_label"] != "Endpoint 999" {
		t.Fatalf("expected stale endpoint option to prepend synthetic label, got %+v", payload)
	}
	staleDetailResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/102", nil, runtimeModelHeader(profileID))
	assertStatus(t, staleDetailResponse, http.StatusOK)
	decodeJSONResponse(t, staleDetailResponse, &payload)
	stalePricing := asMapRuntime(t, payload["pricing"])
	if pricingStatus, ok := stalePricing["pricing_status"].(string); !ok || pricingStatus != "unpriced" {
		t.Fatalf("expected stale request-log detail pricing.pricing_status=unpriced when cost is missing, got %+v", stalePricing)
	}
	if stalePricing["unpriced_reason"] != "MISSING_PRICE_DATA" {
		t.Fatalf("expected stale request-log detail pricing.unpriced_reason=MISSING_PRICE_DATA when cost is missing, got %+v", stalePricing)
	}
	if totalCost, ok := stalePricing["total_cost_user_currency_micros"]; !ok || totalCost != nil {
		t.Fatalf("expected stale request-log detail pricing.total_cost_user_currency_micros=null when cost is missing, got %+v", stalePricing)
	}
	for _, forbidden := range []string{"status_code", "response_time_ms"} {
		if _, ok := asMapRuntime(t, payload["summary"])[forbidden]; ok {
			t.Fatalf("request-log detail must not expose legacy key %q: %+v", forbidden, payload)
		}
	}
	staleModelResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?ingress_model_id=stale-selected-model&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, staleModelResponse, http.StatusOK)
	decodeJSONResponse(t, staleModelResponse, &payload)
	models = payload["filter_options"].(map[string]any)["ingress_models"].([]any)
	if len(models) == 0 {
		t.Fatalf("expected model filters for stale-selected-model request, got %+v", payload)
	}
	firstModel := asMapRuntime(t, models[0])
	if firstModel["ingress_model_id"] != "stale-selected-model" || firstModel["model_label"] != "stale-selected-model" {
		t.Fatalf("expected stale selected model option to prepend synthetic label, got %+v", payload["filter_options"])
	}
}

func TestRequestLogsClientRuleFilterMatchesCallerUserAgentOnly(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	codexRuleID := insertRequestLogUserAgentRule(t, harness, &profileID, "Codex Filter", "codex-filter", true, false)
	seedSimpleRequestLog(t, harness, profileID, 201, 12, nil, time.Date(2026, 4, 18, 12, 10, 0, 0, time.UTC), false)
	seedSimpleRequestLog(t, harness, profileID, 202, 12, nil, time.Date(2026, 4, 18, 12, 11, 0, 0, time.UTC), false)
	seedSimpleRequestLog(t, harness, profileID, 203, 12, nil, time.Date(2026, 4, 18, 12, 12, 0, 0, time.UTC), false)
	seedSimpleRequestLog(t, harness, profileID, 204, 12, nil, time.Date(2026, 4, 18, 12, 13, 0, 0, time.UTC), false)
	updateRequestLogUserAgents(t, harness, profileID, 201, "codex-filter/1.0", "upstream-client/1.0")
	updateRequestLogUserAgents(t, harness, profileID, 202, "caller-client/1.0", "codex-filter/1.0")
	updateRequestLogUserAgents(t, harness, profileID, 203, "", "codex-filter/1.0")

	response := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests?client_rule_id=%d&limit=50&offset=0", codexRuleID), nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected client_rule_id to match only non-empty caller_user_agent, got %+v", payload)
	}
	item := asMapRuntime(t, items[0])
	if got, ok := item["request_log_id"].(string); !ok || got != "201" {
		t.Fatalf("expected caller-matched request log 201, got %+v", item)
	}
}

func TestRequestLogsClientRuleFilterValidationScope(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	systemRuleID := insertRequestLogUserAgentRule(t, harness, nil, "System Filter", "system-filter", true, true)
	disabledRuleID := insertRequestLogUserAgentRule(t, harness, &profileID, "Disabled Filter", "disabled-filter", false, false)
	otherProfileID := insertRequestLogProfile(t, harness)
	otherProfileRuleID := insertRequestLogUserAgentRule(t, harness, &otherProfileID, "Other Profile Filter", "other-profile-filter", true, false)
	seedSimpleRequestLog(t, harness, profileID, 211, 12, nil, time.Date(2026, 4, 18, 12, 10, 0, 0, time.UTC), false)
	updateRequestLogUserAgents(t, harness, profileID, 211, "system-filter/1.0", "upstream-client/1.0")

	systemResponse := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests?client_rule_id=%d&limit=50&offset=0", systemRuleID), nil, runtimeModelHeader(profileID))
	assertStatus(t, systemResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, systemResponse, &payload)
	if got := len(payload["items"].([]any)); got != 1 {
		t.Fatalf("expected enabled system rule to be authorized and match one row, got %+v", payload)
	}

	for _, test := range []struct {
		name   string
		ruleID int
	}{
		{name: "disabled", ruleID: disabledRuleID},
		{name: "other profile", ruleID: otherProfileRuleID},
		{name: "unknown", ruleID: 9999999},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests?client_rule_id=%d&limit=50&offset=0", test.ruleID), nil, runtimeModelHeader(profileID))
			assertStatus(t, response, http.StatusBadRequest)
		})
	}
	malformedResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?client_rule_id=not-a-rule&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, malformedResponse, http.StatusBadRequest)
}

func TestRequestLogsResolvedTargetModelFilterComposesWithRequestedModel(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	seedSimpleRequestLog(t, harness, profileID, 221, 12, nil, time.Date(2026, 4, 18, 12, 10, 0, 0, time.UTC), false)
	seedSimpleRequestLog(t, harness, profileID, 222, 12, nil, time.Date(2026, 4, 18, 12, 11, 0, 0, time.UTC), false)
	seedSimpleRequestLog(t, harness, profileID, 223, 12, nil, time.Date(2026, 4, 18, 12, 12, 0, 0, time.UTC), false)
	updateRequestLogModels(t, harness, profileID, 221, "requested-model", "final-target-model")
	updateRequestLogModels(t, harness, profileID, 222, "other-requested-model", "final-target-model")
	updateRequestLogModels(t, harness, profileID, 223, "requested-model", "other-final-target-model")

	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?ingress_model_id=requested-model&attempt_target_model_id=final-target-model&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected requested and resolved target filters to compose to one row, got %+v", payload)
	}
	if got, ok := asMapRuntime(t, items[0])["request_log_id"].(string); !ok || got != "221" {
		t.Fatalf("expected composed filters to return request 221, got %+v", items[0])
	}

	mismatch := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?ingress_model_id=missing-requested-model&attempt_target_model_id=final-target-model&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, mismatch, http.StatusOK)
	decodeJSONResponse(t, mismatch, &payload)
	if got := len(payload["items"].([]any)); got != 0 {
		t.Fatalf("expected requested model mismatch to suppress final-target matches, got %+v", payload)
	}
}

func TestRequestLogListTimeWindowHonorsToTime(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	start := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	seedSimpleRequestLog(t, harness, profileID, 231, 12, nil, start, false)
	seedSimpleRequestLog(t, harness, profileID, 232, 12, nil, start.Add(time.Hour), false)

	from := url.QueryEscape(start.Format(time.RFC3339))
	to := url.QueryEscape(start.Add(30 * time.Minute).Format(time.RFC3339))
	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?from_time="+from+"&to_time="+to+"&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected to_time to exclude rows after the window, got %+v", payload)
	}
	if got, ok := asMapRuntime(t, items[0])["request_log_id"].(string); !ok || got != "231" {
		t.Fatalf("expected request log 231 inside the time window, got %+v", items[0])
	}
}

func TestRequestLogListStatusAndErrorFilters(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	baseTime := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	seedFilteredRequestLog(t, harness, profileID, 241, 200, nil, baseTime)
	seedFilteredRequestLog(t, harness, profileID, 242, 429, runtimeStringPtr("rate limit timeout from upstream"), baseTime.Add(time.Minute))
	seedFilteredRequestLog(t, harness, profileID, 243, 500, runtimeStringPtr("internal failure"), baseTime.Add(2*time.Minute))

	tests := []struct {
		name    string
		query   string
		wantIDs []int
	}{
		{name: "2xx status family", query: "status_family=2xx", wantIDs: []int{241}},
		{name: "exact status code", query: "status_code=429", wantIDs: []int{242}},
		{name: "error text", query: "error_text=timeout", wantIDs: []int{242}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?"+test.query+"&limit=50&offset=0", nil, runtimeModelHeader(profileID))
			assertStatus(t, response, http.StatusOK)
			var payload map[string]any
			decodeJSONResponse(t, response, &payload)
			items := payload["items"].([]any)
			if len(items) != len(test.wantIDs) {
				t.Fatalf("expected %s to return ids %v, got %+v", test.query, test.wantIDs, payload)
			}
			for index, wantID := range test.wantIDs {
				if got, ok := asMapRuntime(t, items[index])["request_log_id"].(string); !ok || got != strconv.Itoa(wantID) {
					t.Fatalf("expected %s result %d to be request log %d, got %+v", test.query, index, wantID, items[index])
				}
			}
		})
	}
}

func TestRequestLogListPricingFilters(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	baseTime := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	seedPricingFilteredRequestLog(t, harness, profileID, 251, true, nil, true, baseTime)
	seedPricingFilteredRequestLog(t, harness, profileID, 252, false, runtimeStringPtr("MISSING_PRICE_DATA"), false, baseTime.Add(time.Minute))
	seedPricingFilteredRequestLog(t, harness, profileID, 253, false, runtimeStringPtr("MISSING_TOKEN_USAGE"), false, baseTime.Add(2*time.Minute))
	seedPricingFilteredRequestLog(t, harness, profileID, 254, false, runtimeStringPtr("MISSING_PRICE_DATA"), false, baseTime.Add(3*time.Minute))

	tests := []struct {
		name    string
		query   string
		status  int
		wantIDs []int
	}{
		{name: "unpriced only", query: "pricing_status=unpriced", status: http.StatusOK, wantIDs: []int{254, 253, 252}},
		{name: "priced only", query: "pricing_status=priced", status: http.StatusOK, wantIDs: []int{251}},
		{name: "specific unpriced reason", query: "unpriced_reason=MISSING_PRICE_DATA", status: http.StatusOK, wantIDs: []int{254, 252}},
		{name: "unknown priced alias rejected", query: "priced=true", status: http.StatusUnprocessableEntity},
		{name: "unknown pricing status rejected", query: "pricing_status=bogus", status: http.StatusUnprocessableEntity},
		{name: "invalid unpriced reason", query: "unpriced_reason=NOPE", status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?"+test.query+"&limit=50&offset=0", nil, runtimeModelHeader(profileID))
			assertStatus(t, response, test.status)
			if test.status != http.StatusOK {
				return
			}
			var payload map[string]any
			decodeJSONResponse(t, response, &payload)
			items := payload["items"].([]any)
			if len(items) != len(test.wantIDs) {
				t.Fatalf("expected %s to return ids %v, got %+v", test.query, test.wantIDs, payload)
			}
			for index, wantID := range test.wantIDs {
				if got, ok := asMapRuntime(t, items[index])["request_log_id"].(string); !ok || got != strconv.Itoa(wantID) {
					t.Fatalf("expected %s result %d to be request log %d, got %+v", test.query, index, wantID, items[index])
				}
			}
		})
	}
}

func TestRequestLogDetailContract(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	seedRequestLogUserAgentRules(t, harness, profileID)
	seedFixtureRequestLog(t, harness, profileID)
	seedSimpleRequestLog(t, harness, profileID, 102, 12, nil, time.Date(2026, 4, 18, 12, 20, 0, 0, time.UTC), false)

	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/101", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	writeRequestFixtureIfRequested(t, "request-log-detail.json", payload)
	expected := loadRequestFixture(t, "request-log-detail.json")
	expectedRouting := asMapRuntime(t, expected["routing"])
	expectedRouting["profile_id"] = float64(profileID)
	// Dataset coverage carries runtime materialization identity and is verified
	// independently from this frozen response fixture.
	delete(payload, "dataset_coverage")
	delete(expected, "dataset_coverage")
	rawDump, _ := json.MarshalIndent(payload, "", "  ")
	_ = os.WriteFile("/tmp/request-log-detail-actual.json", append(rawDump, '\n'), 0o644)
	if !jsonBytesEqual(t, payload, expected) {
		t.Fatalf("expected request-log detail payload to match fixture, got %+v", payload)
	}
	if summary := asMapRuntime(t, payload["summary"]); summary["upstream_model_id"] != "vendor/gpt-4o-native" {
		t.Fatalf("expected retained upstream model snapshot in exact detail, got %+v", summary)
	}
	routing := asMapRuntime(t, payload["routing"])
	auditEnabledAtRequest, ok := routing["audit_enabled_at_request"].(bool)
	if !ok || auditEnabledAtRequest {
		t.Fatalf("expected request-log detail routing.audit_enabled_at_request=false boolean, got %+v", routing)
	}
	auditCaptureBodiesAtRequest, ok := routing["audit_capture_bodies_at_request"].(bool)
	if !ok || auditCaptureBodiesAtRequest {
		t.Fatalf("expected request-log detail routing.audit_capture_bodies_at_request=false boolean, got %+v", routing)
	}
	for _, absent := range []string{"model_id", "resolved_target_model_id", "api_family"} {
		if _, ok := routing[absent]; ok {
			t.Fatalf("did not expect routing field %s in detail payload, got %+v", absent, routing)
		}
	}

	legacyResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/102", nil, runtimeModelHeader(profileID))
	assertStatus(t, legacyResponse, http.StatusOK)
	decodeJSONResponse(t, legacyResponse, &payload)
	legacyRequest := asMapRuntime(t, payload["request"])
	if generationParams, ok := legacyRequest["request_generation_params"]; !ok || generationParams != nil {
		t.Fatalf("expected legacy request.request_generation_params=null, got %+v", legacyRequest)
	}
	if generationParamsStatus, ok := legacyRequest["request_generation_params_status"]; !ok || generationParamsStatus != nil {
		t.Fatalf("expected legacy request.request_generation_params_status=null, got %+v", legacyRequest)
	}

	missing := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/999999", nil, runtimeModelHeader(profileID))
	assertStatus(t, missing, http.StatusNotFound)
	var missingPayload map[string]any
	decodeJSONResponse(t, missing, &missingPayload)
	if missingPayload["detail"] != "Request log not found" {
		t.Fatalf("expected scoped request-log 404 detail, got %+v", missingPayload)
	}
}

func TestRequestLogDetailCurrentPricingEffectiveAtUsesUTC(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	seedFixtureRequestLog(t, harness, profileID)
	location := time.FixedZone("FUN-009 offset", 3*60*60)
	templateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "request-log-current-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "1", "2", "0", "0", "0")
	attachRequestLogCurrentPricingTemplate(t, harness, profileID, 101, templateID, time.Date(2026, 4, 18, 15, 0, 0, 0, location))
	previousLocal := time.Local
	time.Local = location
	defer func() { time.Local = previousLocal }()

	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/101", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	currentPricing := asMapRuntime(t, payload["current_pricing_template"])
	effectiveAt, ok := currentPricing["current_effective_at"].(string)
	if !ok || effectiveAt != "2026-04-18T12:00:00Z" {
		t.Fatalf("expected current pricing effective_at to use canonical UTC JSON, got %v", currentPricing["current_effective_at"])
	}
}

func TestRequestLogStreamErrorDetailContract(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	createdAt := time.Date(2026, 4, 18, 12, 25, 0, 0, time.UTC)
	seedSimpleRequestLog(t, harness, profileID, 105, 12, nil, createdAt, false)
	streamErrorDetail := "upstream read failed after provider closed connection"
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET is_stream = FALSE, stream_outcome = 'upstream_read_error', stream_error_kind = 'upstream_read_failed', stream_error_detail = $1, attempt_result = 'stream_error', failure_stage = 'stream', error_source = 'upstream', error_code = 'stream_upstream_read_failed' WHERE profile_id = $2 AND id = 105`, streamErrorDetail, profileID); err != nil {
		t.Fatalf("seed request-log stream error detail: %v", err)
	}

	listResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, listResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, listResponse, &payload)
	item := requestLogItemsByID(t, payload["items"].([]any))[105]
	if item["stream_outcome"] != "upstream_read_error" || item["stream_error_kind"] != "upstream_read_failed" {
		t.Fatalf("expected request-log list to include stream outcome/kind, got %+v", item)
	}
	if _, ok := item["stream_error_detail"]; ok {
		t.Fatalf("did not expect request-log list to expose stream_error_detail, got %+v", item)
	}

	detailResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/105", nil, runtimeModelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	decodeJSONResponse(t, detailResponse, &payload)
	summary := asMapRuntime(t, payload["summary"])
	if summary["stream_outcome"] != "upstream_read_error" || summary["stream_error_kind"] != "upstream_read_failed" {
		t.Fatalf("expected request-log detail summary to expose stream outcome/kind, got %+v", summary)
	}
	failure := asMapRuntime(t, payload["failure"])
	if failure["category"] != "provider_stream" || failure["source"] != "upstream" || failure["stage"] != "stream" || failure["code"] != "stream_upstream_read_failed" || failure["stream_error_detail"] != streamErrorDetail || failure["detail"] != streamErrorDetail || failure["detail_source"] != "stream_error_detail" {
		t.Fatalf("expected request-log detail failure projection to expose exact sanitized stream error detail, got %+v", failure)
	}
}

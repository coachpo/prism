package runtimetest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type requestLogContractHarness struct {
	databaseName string
	client       *http.Client
	conn         *pgx.Conn
	server       *httptest.Server
	url          string
}

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
	expected := loadRequestFixture(t, "request-log-list.json")
	if !jsonBytesEqual(t, payload, expected) {
		t.Fatalf("expected request-log list payload to match fixture, got %+v", payload)
	}
	itemsByID := requestLogItemsByID(t, payload["items"].([]any))
	primaryItem := itemsByID[101]
	if pricedFlag, ok := primaryItem["priced_flag"].(bool); !ok || !pricedFlag {
		t.Fatalf("expected primary request-log list row priced_flag=true, got %+v", primaryItem)
	}
	if unpricedReason, ok := primaryItem["unpriced_reason"]; !ok || unpricedReason != nil {
		t.Fatalf("expected primary request-log list row unpriced_reason=null, got %+v", primaryItem)
	}
	if _, ok := primaryItem["stream_error_detail"]; ok {
		t.Fatalf("did not expect request-log list row to expose stream_error_detail, got %+v", primaryItem)
	}
	filterOptions := asMapRuntime(t, payload["filter_options"])
	models, ok := filterOptions["models"].([]any)
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
	if pricedFlag, ok := staleItem["priced_flag"].(bool); !ok || pricedFlag {
		t.Fatalf("expected stale request-log row priced_flag=false when cost is missing, got %+v", staleItem)
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
	staleUsage := asMapRuntime(t, payload["usage"])
	if pricedFlag, ok := staleUsage["priced_flag"].(bool); !ok || pricedFlag {
		t.Fatalf("expected stale request-log detail usage.priced_flag=false when cost is missing, got %+v", staleUsage)
	}
	if staleUsage["unpriced_reason"] != "MISSING_PRICE_DATA" {
		t.Fatalf("expected stale request-log detail usage.unpriced_reason=MISSING_PRICE_DATA when cost is missing, got %+v", staleUsage)
	}
	staleCosting := asMapRuntime(t, payload["costing"])
	if totalCost, ok := staleCosting["total_cost_user_currency_micros"]; !ok || totalCost != nil {
		t.Fatalf("expected stale request-log detail costing.total_cost_user_currency_micros=null when cost is missing, got %+v", staleCosting)
	}
	staleModelResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?model_id=stale-selected-model&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, staleModelResponse, http.StatusOK)
	decodeJSONResponse(t, staleModelResponse, &payload)
	models = payload["filter_options"].(map[string]any)["models"].([]any)
	if len(models) == 0 {
		t.Fatalf("expected model filters for stale-selected-model request, got %+v", payload)
	}
	firstModel := asMapRuntime(t, models[0])
	if firstModel["model_id"] != "stale-selected-model" || firstModel["model_label"] != "stale-selected-model" {
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
	if got := jsonInt(t, item["id"]); got != 201 {
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

	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?model_id=requested-model&resolved_target_model_id=final-target-model&limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected requested and resolved target filters to compose to one row, got %+v", payload)
	}
	if got := jsonInt(t, asMapRuntime(t, items[0])["id"]); got != 221 {
		t.Fatalf("expected composed filters to return request 221, got %+v", items[0])
	}

	mismatch := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?model_id=missing-requested-model&resolved_target_model_id=final-target-model&limit=50&offset=0", nil, runtimeModelHeader(profileID))
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
	if got := jsonInt(t, asMapRuntime(t, items[0])["id"]); got != 231 {
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
				if got := jsonInt(t, asMapRuntime(t, items[index])["id"]); got != wantID {
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
		{name: "unpriced only", query: "priced=false", status: http.StatusOK, wantIDs: []int{254, 253, 252}},
		{name: "priced only", query: "priced=true", status: http.StatusOK, wantIDs: []int{251}},
		{name: "specific unpriced reason", query: "unpriced_reason=MISSING_PRICE_DATA", status: http.StatusOK, wantIDs: []int{254, 252}},
		{name: "invalid priced", query: "priced=notabool", status: http.StatusBadRequest},
		{name: "non-lowercase priced", query: "priced=TRUE", status: http.StatusBadRequest},
		{name: "numeric priced", query: "priced=1", status: http.StatusBadRequest},
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
				if got := jsonInt(t, asMapRuntime(t, items[index])["id"]); got != wantID {
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
	expected := loadRequestFixture(t, "request-log-detail.json")
	expectedRouting := asMapRuntime(t, expected["routing"])
	expectedRouting["profile_id"] = float64(profileID)
	if !jsonBytesEqual(t, payload, expected) {
		t.Fatalf("expected request-log detail payload to match fixture, got %+v", payload)
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

func TestRequestLogStreamErrorDetailContract(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	createdAt := time.Date(2026, 4, 18, 12, 25, 0, 0, time.UTC)
	seedSimpleRequestLog(t, harness, profileID, 105, 12, nil, createdAt, false)
	streamErrorDetail := "upstream read failed after provider closed connection"
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET is_stream = FALSE, stream_outcome = 'upstream_read_error', stream_error_kind = 'upstream_read_failed', stream_error_detail = $1 WHERE profile_id = $2 AND id = 105`, streamErrorDetail, profileID); err != nil {
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
	if summary["stream_outcome"] != "upstream_read_error" || summary["stream_error_kind"] != "upstream_read_failed" || summary["stream_error_detail"] != streamErrorDetail {
		t.Fatalf("expected request-log detail summary to expose exact sanitized stream error detail, got %+v", summary)
	}
}

func TestRuntimeRequestLogPersistsAuditEnabledSnapshot(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	publicModelID := "audit-public-" + randomSuffix()
	targetModelID := "audit-target-" + randomSuffix()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   publicModelID,
		TargetModelID:   targetModelID,
		EndpointBaseURL: harness.upstream.baseURL("/audit-enabled"),
		EndpointAPIKey:  "runtime-audit-key",
	})
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "persist audit snapshot"}}, "model": route.PublicModelID},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	var auditEnabledAtRequest bool
	var auditCaptureBodiesAtRequest bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT audit_enabled_at_request, audit_capture_bodies_at_request FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&auditEnabledAtRequest, &auditCaptureBodiesAtRequest); err != nil {
		t.Fatalf("load persisted runtime audit snapshot: %v", err)
	}
	if auditEnabledAtRequest || auditCaptureBodiesAtRequest {
		t.Fatalf("expected runtime request log to persist absent audit family settings as false/false, got enabled=%v capture=%v", auditEnabledAtRequest, auditCaptureBodiesAtRequest)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func TestRuntimeRequestLogPersistsAPIFamilyAuditSettingsSnapshot(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "openai", true, false)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "anthropic", false, false)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "gemini", true, true)

	openAIRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "audit-openai-public-" + randomSuffix(),
		TargetModelID:   "audit-openai-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/audit-family/openai"),
		EndpointAPIKey:  "runtime-audit-openai-key",
	})
	anthropicUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "msg-audit-family", "type": "message", "role": "assistant", "content": []map[string]any{}, "usage": map[string]any{"input_tokens": 4, "output_tokens": 2}})
	anthropicRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "anthropic",
		PublicModelID:   "audit-anthropic-public-" + randomSuffix(),
		TargetModelID:   "audit-anthropic-target-" + randomSuffix(),
		EndpointBaseURL: anthropicUpstream.baseURL("/audit-family/anthropic"),
		EndpointAPIKey:  "runtime-audit-anthropic-key",
	})
	geminiUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"candidates": []map[string]any{{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}}}, "usageMetadata": map[string]any{"promptTokenCount": 3, "candidatesTokenCount": 2, "totalTokenCount": 5}})
	geminiRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "audit-gemini-public-" + randomSuffix(),
		TargetModelID:   "audit-gemini-target-" + randomSuffix(),
		EndpointBaseURL: geminiUpstream.baseURL("/audit-family/gemini"),
		EndpointAPIKey:  "runtime-audit-gemini-key",
	})

	openAIResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "persist openai audit policy"}},
		"model":    openAIRoute.PublicModelID,
	}, nil)
	assertStatus(t, openAIResponse, http.StatusOK)
	anthropicResponse := harness.requestJSON(t, http.MethodPost, "/v1/messages", map[string]any{
		"model":      anthropicRoute.PublicModelID,
		"max_tokens": 16,
		"messages":   []map[string]any{{"role": "user", "content": "persist anthropic audit policy"}},
	}, nil)
	assertStatus(t, anthropicResponse, http.StatusOK)
	geminiResponse := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/v1beta/models/%s:generateContent", geminiRoute.PublicModelID), map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "persist gemini audit policy"}}}},
	}, nil)
	assertStatus(t, geminiResponse, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 3, UsageEvents: 3, OutboxRows: 0}, 5*time.Second)

	assertRuntimeRequestLogAuditSnapshot(t, harness, profileID, openAIRoute.PublicModelID, true, false)
	assertRuntimeRequestLogAuditSnapshot(t, harness, profileID, anthropicRoute.PublicModelID, false, false)
	assertRuntimeRequestLogAuditSnapshot(t, harness, profileID, geminiRoute.PublicModelID, true, true)
	assertRuntimeAuditLogSnapshot(t, harness, profileID, openAIRoute.PublicModelID, true, false)
	assertRuntimeAuditLogSnapshot(t, harness, profileID, geminiRoute.PublicModelID, true, true)
}

func TestRuntimeRequestLogsPreserveRequestedAndResolvedModelIdentity(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-runtime-requested-resolved-identity-" + suffix,
		"object": "chat.completion",
		"usage": map[string]any{
			"prompt_tokens":     8,
			"completion_tokens": 5,
			"total_tokens":      13,
		},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "requested-resolved-public-" + suffix,
		TargetModelID:   "requested-resolved-target-" + suffix,
		EndpointBaseURL: upstream.baseURL("/request-logs/requested-resolved"),
		EndpointAPIKey:  "runtime-requested-resolved-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "preserve requested and resolved identity"}},
			"model":    route.PublicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	if got := requestModelID(t, upstream.lastRequest(t).Body); got != route.TargetModelID {
		t.Fatalf("expected upstream request model %q, got %q", route.TargetModelID, got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, route.TargetModelID)
}

func TestRuntimeRequestLogsPreserveRequestedPublicAndResolvedNativeIdentityForResponses(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "resp-runtime-requested-resolved-identity-" + suffix,
		"object": "response",
		"usage": map[string]any{
			"input_tokens":  8,
			"output_tokens": 5,
			"total_tokens":  13,
		},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "requested-resolved-public-responses-" + suffix,
		TargetModelID:   "requested-resolved-target-responses-" + suffix,
		EndpointBaseURL: upstream.baseURL("/request-logs/requested-resolved-responses"),
		EndpointAPIKey:  "runtime-requested-resolved-key-responses",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/responses",
		map[string]any{
			"input": "preserve requested and resolved identity in responses",
			"model": route.PublicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	if got := requestModelID(t, upstream.lastRequest(t).Body); got != route.TargetModelID {
		t.Fatalf("expected upstream request model %q, got %q", route.TargetModelID, got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, route.TargetModelID)
}

func TestRuntimeRequestLogsSkipCrossFamilyProxyTargets(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	anthropicUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "msg-cross-family-anthropic-" + suffix, "type": "message"})
	openAIUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-cross-family-openai-" + suffix,
		"object": "chat.completion",
		"usage": map[string]any{
			"prompt_tokens":     4,
			"completion_tokens": 3,
			"total_tokens":      7,
		},
	})

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	anthropicStrategyID := harness.seedLegacyStrategy(t, profileID, "request-logs-cross-family-anthropic-"+suffix, "round-robin")
	openAIStrategyID := harness.seedLegacyStrategy(t, profileID, "request-logs-cross-family-openai-"+suffix, "round-robin")
	anthropicTargetModelID := "cross-family-anthropic-target-" + suffix
	openAITargetModelID := "cross-family-openai-target-" + suffix
	publicModelID := "cross-family-public-" + suffix
	anthropicTargetConfigID := harness.seedModel(t, profileID, "anthropic", anthropicTargetModelID, "native", &anthropicStrategyID)
	openAITargetConfigID := harness.seedModel(t, profileID, "openai", openAITargetModelID, "native", &openAIStrategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, anthropicTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, openAITargetConfigID, 1)
	anthropicEndpointID := harness.seedEndpoint(t, profileID, "request-logs-cross-family-anthropic-endpoint-"+suffix, anthropicUpstream.baseURL("/request-logs/cross-family/anthropic"), "runtime-cross-family-anthropic-key")
	openAIEndpointID := harness.seedEndpoint(t, profileID, "request-logs-cross-family-openai-endpoint-"+suffix, openAIUpstream.baseURL("/request-logs/cross-family/openai"), "runtime-cross-family-openai-key")
	harness.seedConnection(t, profileID, anthropicTargetConfigID, anthropicEndpointID, "request-logs-cross-family-anthropic-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, openAITargetConfigID, openAIEndpointID, "request-logs-cross-family-openai-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "skip cross-family proxy targets"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	if got := len(anthropicUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected cross-family anthropic target to be skipped, got %d upstream requests", got)
	}
	if got := len(openAIUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected valid same-family target to receive exactly one upstream request, got %d", got)
	}
	if got := requestModelID(t, openAIUpstream.lastRequest(t).Body); got != openAITargetModelID {
		t.Fatalf("expected same-family upstream request model %q, got %q", openAITargetModelID, got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, publicModelID, openAITargetModelID)
}

func TestRuntimeRequestLogPreservesUnpricedPricingPathways(t *testing.T) {
	baseUsage := map[string]any{
		"prompt_tokens":     10,
		"completion_tokens": 6,
		"total_tokens":      16,
	}
	componentUsage := map[string]any{
		"prompt_tokens":     10,
		"completion_tokens": 6,
		"total_tokens":      16,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 4,
		},
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": 3,
		},
	}

	type pricingTemplateSpec struct {
		currencyCode  string
		inputPrice    string
		outputPrice   string
		cachedInput   string
		cacheCreation string
		reasoning     string
	}

	loadPayload := func(t *testing.T, harness *runtimeHarness, profileID int, path string) map[string]any {
		t.Helper()
		response := harness.requestJSON(t, http.MethodGet, path, nil, runtimeModelHeader(profileID))
		assertStatus(t, response, http.StatusOK)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		return payload
	}

	loadSingleListItem := func(t *testing.T, harness *runtimeHarness, profileID int) map[string]any {
		t.Helper()
		payload := loadPayload(t, harness, profileID, "/api/stats/requests?limit=50&offset=0")
		items, ok := payload["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("expected one request-log list row, got %+v", payload)
		}
		return asMapRuntime(t, items[0])
	}

	tests := []struct {
		name           string
		usage          map[string]any
		requestContent string
		template       func(runtimeReportCurrencySnapshot) *pricingTemplateSpec
		attachTemplate func(*testing.T, *runtimeHarness, int, runtimeReportCurrencySnapshot, seededRuntimeRoute, string)
		want           func(runtimeReportCurrencySnapshot) runtimePersistedPricingRow
		assert         func(*testing.T, *runtimeHarness, int)
	}{
		{
			name:           "optional component prices without component usage counters",
			usage:          baseUsage,
			requestContent: "price omitted optional counters",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "2", outputPrice: "5", cachedInput: "11", cacheCreation: "13", reasoning: "17"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantPricedRow(func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
					row.InputCostMicros = runtimeNullInt64(20)
					row.OutputCostMicros = runtimeNullInt64(30)
					row.CacheReadInputCostMicros = runtimeNullInt64(0)
					row.CacheCreationInputCostMicros = runtimeNullInt64(0)
					row.ReasoningCostMicros = runtimeNullInt64(0)
					row.TotalCostOriginalMicros = runtimeNullInt64(50)
					row.TotalCostUserCurrencyMicros = runtimeNullInt64(50)
					row.CurrencyCodeOriginal = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencyCode = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencySymbol = runtimeNullString(reportCurrency.Symbol)
					row.PricingSnapshotInput = runtimeNullString("2")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("11")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("13")
					row.PricingSnapshotReasoning = runtimeNullString("17")
				})
			},
			assert: func(t *testing.T, harness *runtimeHarness, profileID int) {
				t.Helper()
				streamRow := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
				if streamRow.StreamOutcome != "not_streaming" || streamRow.StreamErrorKind.Valid || streamRow.StreamErrorDetail.Valid || !streamRow.TotalCostUserCurrencyMicros.Valid || streamRow.TotalCostUserCurrencyMicros.Int64 != 50 || !streamRow.CompletionDurationMS.Valid {
					t.Fatalf("expected non-stream request log to persist not_streaming while preserving pricing/timing, got %+v", streamRow)
				}
			},
		},
		{
			name:           "priced zero distinct from unpriced",
			usage:          componentUsage,
			requestContent: "keep priced zero distinct from unpriced",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "0", outputPrice: "0", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantPricedRow(func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(6)
					row.OutputTokens = runtimeNullInt64(3)
					row.TotalTokens = runtimeNullInt64(16)
					row.CacheReadInputTokens = runtimeNullInt64(4)
					row.ReasoningTokens = runtimeNullInt64(3)
					row.InputCostMicros = runtimeNullInt64(0)
					row.OutputCostMicros = runtimeNullInt64(0)
					row.CacheReadInputCostMicros = runtimeNullInt64(0)
					row.CacheCreationInputCostMicros = runtimeNullInt64(0)
					row.ReasoningCostMicros = runtimeNullInt64(0)
					row.TotalCostOriginalMicros = runtimeNullInt64(0)
					row.TotalCostUserCurrencyMicros = runtimeNullInt64(0)
					row.CurrencyCodeOriginal = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencyCode = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencySymbol = runtimeNullString(reportCurrency.Symbol)
					row.PricingSnapshotInput = runtimeNullString("0")
					row.PricingSnapshotOutput = runtimeNullString("0")
					row.PricingSnapshotCacheReadInput = runtimeNullString("0")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("0")
					row.PricingSnapshotReasoning = runtimeNullString("0")
				})
			},
			assert: func(t *testing.T, harness *runtimeHarness, profileID int) {
				t.Helper()
				listItem := loadSingleListItem(t, harness, profileID)
				if pricedFlag, ok := listItem["priced_flag"].(bool); !ok || !pricedFlag {
					t.Fatalf("expected priced-zero request-log list row priced_flag=true, got %+v", listItem)
				}
				if unpricedReason, ok := listItem["unpriced_reason"]; !ok || unpricedReason != nil {
					t.Fatalf("expected priced-zero request-log list row unpriced_reason=null, got %+v", listItem)
				}
				if jsonInt(t, listItem["total_cost_user_currency_micros"]) != 0 {
					t.Fatalf("expected priced-zero request-log list row total_cost_user_currency_micros=0, got %+v", listItem)
				}

				detailPayload := loadLatestRuntimeRequestLogDetailPayload(t, harness, profileID)
				usage := asMapRuntime(t, detailPayload["usage"])
				if pricedFlag, ok := usage["priced_flag"].(bool); !ok || !pricedFlag {
					t.Fatalf("expected priced-zero request-log detail usage.priced_flag=true, got %+v", usage)
				}
				if unpricedReason, ok := usage["unpriced_reason"]; !ok || unpricedReason != nil {
					t.Fatalf("expected priced-zero request-log detail usage.unpriced_reason=null, got %+v", usage)
				}
				costing := asMapRuntime(t, detailPayload["costing"])
				if jsonInt(t, costing["total_cost_user_currency_micros"]) != 0 {
					t.Fatalf("expected priced-zero request-log detail costing.total_cost_user_currency_micros=0, got %+v", costing)
				}
				if costing["fx_rate_used"] != "1" || costing["fx_rate_source"] != "DEFAULT_1_TO_1" {
					t.Fatalf("expected priced-zero request-log detail costing fx provenance to stay explicit, got %+v", costing)
				}

				spendingPayload := loadPayload(t, harness, profileID, "/api/stats/spending?preset=1h&group_by=none&limit=50&offset=0")
				summary := asMapRuntime(t, spendingPayload["summary"])
				if jsonInt(t, summary["successful_request_count"]) != 1 || jsonInt(t, summary["priced_request_count"]) != 1 || jsonInt(t, summary["unpriced_request_count"]) != 0 || jsonInt(t, summary["total_cost_micros"]) != 0 {
					t.Fatalf("expected priced-zero spending summary to stay priced with zero cost, got %+v", summary)
				}
				unpricedBreakdown := asMapRuntime(t, spendingPayload["unpriced_breakdown"])
				if len(unpricedBreakdown) != 0 {
					t.Fatalf("expected priced-zero spending breakdown to stay empty, got %+v", unpricedBreakdown)
				}
			},
		},
		{
			name:           "management normalized component prices",
			usage:          componentUsage,
			requestContent: "price management-normalized optional defaults",
			attachTemplate: func(t *testing.T, harness *runtimeHarness, profileID int, reportCurrency runtimeReportCurrencySnapshot, route seededRuntimeRoute, suffix string) {
				t.Helper()
				createResponse := harness.requestJSON(t, http.MethodPost, "/api/pricing-templates", map[string]any{
					"name":                  "Runtime Management Normalized Components " + suffix,
					"pricing_currency_code": reportCurrency.Code,
					"input_price":           "2",
					"output_price":          "5",
					"cached_input_price":    "   ",
					"cache_creation_price":  nil,
					"reasoning_price":       "   ",
				}, runtimeModelHeader(profileID))
				assertStatus(t, createResponse, http.StatusCreated)
				var createdTemplate map[string]any
				decodeJSONResponse(t, createResponse, &createdTemplate)
				pricingTemplateID := jsonInt(t, createdTemplate["id"])
				if createdTemplate["cached_input_price"] != "0" || createdTemplate["cache_creation_price"] != "0" || createdTemplate["reasoning_price"] != "0" {
					t.Fatalf("expected management-created blank/null component prices to normalize to zero strings, got %+v", createdTemplate)
				}
				attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantPricedRow(func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(6)
					row.OutputTokens = runtimeNullInt64(3)
					row.TotalTokens = runtimeNullInt64(16)
					row.CacheReadInputTokens = runtimeNullInt64(4)
					row.ReasoningTokens = runtimeNullInt64(3)
					row.InputCostMicros = runtimeNullInt64(12)
					row.OutputCostMicros = runtimeNullInt64(15)
					row.CacheReadInputCostMicros = runtimeNullInt64(0)
					row.CacheCreationInputCostMicros = runtimeNullInt64(0)
					row.ReasoningCostMicros = runtimeNullInt64(0)
					row.TotalCostOriginalMicros = runtimeNullInt64(27)
					row.TotalCostUserCurrencyMicros = runtimeNullInt64(27)
					row.CurrencyCodeOriginal = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencyCode = runtimeNullString(reportCurrency.Code)
					row.ReportCurrencySymbol = runtimeNullString(reportCurrency.Symbol)
					row.PricingSnapshotInput = runtimeNullString("2")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("0")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("0")
					row.PricingSnapshotReasoning = runtimeNullString("0")
				})
			},
		},
		{
			name:  "pricing disabled",
			usage: baseUsage,
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("PRICING_DISABLED", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
				})
			},
		},
		{
			name:  "invalid required price",
			usage: baseUsage,
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "not-a-decimal", outputPrice: "5", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_PRICE_DATA", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
					row.PricingSnapshotUnit = runtimeNullString("PER_1M")
					row.PricingSnapshotInput = runtimeNullString("not-a-decimal")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("0")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("0")
					row.PricingSnapshotReasoning = runtimeNullString("0")
					row.PricingConfigVersionUsed = runtimeNullInt64(1)
				})
			},
		},
		{
			name:  "missing fx",
			usage: baseUsage,
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				missingFXCurrencyCode := "EUR"
				if reportCurrency.Code == missingFXCurrencyCode {
					missingFXCurrencyCode = "USD"
				}
				return &pricingTemplateSpec{currencyCode: missingFXCurrencyCode, inputPrice: "2", outputPrice: "5", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_PRICE_DATA", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(6)
					row.TotalTokens = runtimeNullInt64(16)
					row.PricingSnapshotUnit = runtimeNullString("PER_1M")
					row.PricingSnapshotInput = runtimeNullString("2")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("0")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("0")
					row.PricingSnapshotReasoning = runtimeNullString("0")
					row.PricingConfigVersionUsed = runtimeNullInt64(1)
				})
			},
		},
		{
			name: "missing required usage",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "2", outputPrice: "5", cachedInput: "0", cacheCreation: "0", reasoning: "0"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_TOKEN_USAGE", func(row *runtimePersistedPricingRow) {
					row.PricingSnapshotUnit = runtimeNullString("PER_1M")
					row.PricingSnapshotInput = runtimeNullString("2")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("0")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("0")
					row.PricingSnapshotReasoning = runtimeNullString("0")
					row.PricingConfigVersionUsed = runtimeNullInt64(1)
				})
			},
		},
		{
			name: "degraded component pricing",
			usage: map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 6,
				"total_tokens":      16,
				"completion_tokens_details": map[string]any{
					"reasoning_tokens": 3,
				},
			},
			requestContent: "degrade invalid reasoning pricing",
			template: func(reportCurrency runtimeReportCurrencySnapshot) *pricingTemplateSpec {
				return &pricingTemplateSpec{inputPrice: "2", outputPrice: "5", cachedInput: "11", cacheCreation: "13", reasoning: "not-a-decimal"}
			},
			want: func(reportCurrency runtimeReportCurrencySnapshot) runtimePersistedPricingRow {
				return wantUnpricedRow("MISSING_PRICE_DATA", func(row *runtimePersistedPricingRow) {
					row.InputTokens = runtimeNullInt64(10)
					row.OutputTokens = runtimeNullInt64(3)
					row.TotalTokens = runtimeNullInt64(16)
					row.ReasoningTokens = runtimeNullInt64(3)
					row.PricingSnapshotUnit = runtimeNullString("PER_1M")
					row.PricingSnapshotInput = runtimeNullString("2")
					row.PricingSnapshotOutput = runtimeNullString("5")
					row.PricingSnapshotCacheReadInput = runtimeNullString("11")
					row.PricingSnapshotCacheCreationInput = runtimeNullString("13")
					row.PricingSnapshotReasoning = runtimeNullString("not-a-decimal")
					row.PricingConfigVersionUsed = runtimeNullInt64(1)
				})
			},
			assert: func(t *testing.T, harness *runtimeHarness, profileID int) {
				t.Helper()
				listItem := loadSingleListItem(t, harness, profileID)
				if pricedFlag, ok := listItem["priced_flag"].(bool); !ok || pricedFlag {
					t.Fatalf("expected degraded request-log list row priced_flag=false, got %+v", listItem)
				}
				if listItem["unpriced_reason"] != "MISSING_PRICE_DATA" {
					t.Fatalf("expected degraded request-log list row unpriced_reason=MISSING_PRICE_DATA, got %+v", listItem)
				}
				if totalCost, ok := listItem["total_cost_user_currency_micros"]; !ok || totalCost != nil {
					t.Fatalf("expected degraded request-log list row total_cost_user_currency_micros=null, got %+v", listItem)
				}

				detailPayload := loadLatestRuntimeRequestLogDetailPayload(t, harness, profileID)
				usage := asMapRuntime(t, detailPayload["usage"])
				if pricedFlag, ok := usage["priced_flag"].(bool); !ok || pricedFlag {
					t.Fatalf("expected degraded request-log detail usage.priced_flag=false, got %+v", usage)
				}
				if usage["unpriced_reason"] != "MISSING_PRICE_DATA" || jsonInt(t, usage["reasoning_tokens"]) != 3 {
					t.Fatalf("expected degraded request-log detail usage payload to expose missing-price reasoning tokens, got %+v", usage)
				}
				costing := asMapRuntime(t, detailPayload["costing"])
				if totalCost, ok := costing["total_cost_user_currency_micros"]; !ok || totalCost != nil {
					t.Fatalf("expected degraded request-log detail costing.total_cost_user_currency_micros=null, got %+v", costing)
				}

				usageSnapshotPayload := loadPayload(t, harness, profileID, "/api/stats/usage-snapshot?preset=1h")
				overview := asMapRuntime(t, usageSnapshotPayload["overview"])
				if jsonInt(t, overview["success_requests"]) != 1 || jsonInt(t, overview["reasoning_tokens"]) != 3 || jsonInt(t, overview["total_cost_micros"]) != 0 {
					t.Fatalf("expected degraded usage snapshot overview to keep reasoning tokens but zero cost, got %+v", overview)
				}
				costOverview := asMapRuntime(t, usageSnapshotPayload["cost_overview"])
				if jsonInt(t, costOverview["priced_request_count"]) != 0 || jsonInt(t, costOverview["unpriced_request_count"]) != 1 {
					t.Fatalf("expected degraded usage snapshot cost overview to count one unpriced request, got %+v", costOverview)
				}
				modelStatistics, ok := usageSnapshotPayload["model_statistics"].([]any)
				if !ok || len(modelStatistics) != 1 {
					t.Fatalf("expected one degraded usage snapshot model row, got %+v", usageSnapshotPayload)
				}
				modelRow := asMapRuntime(t, modelStatistics[0])
				if jsonInt(t, modelRow["priced_request_count"]) != 0 || jsonInt(t, modelRow["unpriced_request_count"]) != 1 || jsonInt(t, modelRow["total_cost_micros"]) != 0 {
					t.Fatalf("expected degraded usage snapshot model statistics to preserve unpriced counts, got %+v", modelRow)
				}

				spendingPayload := loadPayload(t, harness, profileID, "/api/stats/spending?preset=1h&group_by=none&limit=50&offset=0")
				summary := asMapRuntime(t, spendingPayload["summary"])
				if jsonInt(t, summary["successful_request_count"]) != 1 || jsonInt(t, summary["priced_request_count"]) != 0 || jsonInt(t, summary["unpriced_request_count"]) != 1 || jsonInt(t, summary["total_reasoning_tokens"]) != 3 || jsonInt(t, summary["total_cost_micros"]) != 0 {
					t.Fatalf("expected degraded spending summary to stay unpriced with zero cost, got %+v", summary)
				}
				unpricedBreakdown := asMapRuntime(t, spendingPayload["unpriced_breakdown"])
				if jsonInt(t, unpricedBreakdown["MISSING_PRICE_DATA"]) != 1 {
					t.Fatalf("expected degraded spending breakdown to count MISSING_PRICE_DATA, got %+v", unpricedBreakdown)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			reportCurrency := loadRuntimeReportCurrencySnapshot(t, harness.conn, profileID)
			suffix := randomSuffix()
			slug := strings.ReplaceAll(test.name, " ", "-")
			responseBody := map[string]any{
				"id":     "chatcmpl-runtime-pricing-" + slug + "-" + suffix,
				"object": "chat.completion",
			}
			if test.usage != nil {
				responseBody["usage"] = test.usage
			}
			upstream := newScriptedUpstream(t, http.StatusOK, responseBody)
			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:       profileID,
				APIFamily:       "openai",
				PublicModelID:   slug + "-public-" + suffix,
				TargetModelID:   slug + "-target-" + suffix,
				EndpointBaseURL: upstream.baseURL("/request-logs/pricing/" + slug),
				EndpointAPIKey:  "runtime-pricing-" + slug + "-key",
			})

			switch {
			case test.attachTemplate != nil:
				test.attachTemplate(t, harness, profileID, reportCurrency, route, suffix)
			case test.template != nil:
				spec := test.template(reportCurrency)
				currencyCode := spec.currencyCode
				if currencyCode == "" {
					currencyCode = reportCurrency.Code
				}
				pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-pricing-"+slug+"-"+suffix, currencyCode, spec.inputPrice, spec.outputPrice, spec.cachedInput, spec.cacheCreation, spec.reasoning)
				attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)
			}

			requestContent := test.requestContent
			if requestContent == "" {
				requestContent = "preserve " + test.name
			}
			response := harness.requestJSON(
				t,
				http.MethodPost,
				"/v1/chat/completions",
				map[string]any{
					"messages": []map[string]any{{"role": "user", "content": requestContent}},
					"model":    route.PublicModelID,
				},
				nil,
			)
			assertStatus(t, response, http.StatusOK)
			if got := len(upstream.requestsSnapshot()); got != 1 {
				t.Fatalf("expected %s request to hit upstream exactly once, got %d", test.name, got)
			}
			waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
			assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
			assertLatestRuntimePricingRows(t, harness.conn, profileID, test.want(reportCurrency), test.name)
			if test.assert != nil {
				test.assert(t, harness, profileID)
			}
		})
	}
}

func TestRuntimeUsageEventEndpointLabelSnapshotUsesSelectedEndpointIdentity(t *testing.T) {
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:  1,
			PollInterval: 25 * time.Millisecond,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "runtime-usage-label-public-" + suffix
	targetModelID := "runtime-usage-label-target-" + suffix
	endpointName := "  Runtime Snapshot Endpoint " + suffix + "  "
	mutatedEndpointName := "mutated endpoint name " + suffix
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-usage-label-" + suffix})
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-usage-label-strategy-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, profileID, endpointName, upstream.baseURL("/request-logs/usage-label/snapshot"), "runtime-usage-label-key")
	connectionID := harness.seedConnection(t, profileID, targetModelConfigID, endpointID, "runtime-usage-label-connection-"+suffix, nil, nil, 0)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "persist endpoint label snapshot from request-time identity"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE endpoints SET name = $2, updated_at = $3 WHERE id = $1`, endpointID, mutatedEndpointName, time.Now().UTC()); err != nil {
		t.Fatalf("mutate endpoint label before durable telemetry materialization: %v", err)
	}

	gate.Release()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	row := loadLatestRuntimeUsageEndpointLabelSnapshot(t, harness.conn, profileID)
	if !row.EndpointID.Valid || int(row.EndpointID.Int64) != endpointID || !row.ConnectionID.Valid || int(row.ConnectionID.Int64) != connectionID || row.EndpointLabelSnapshot != strings.TrimSpace(endpointName) {
		t.Fatalf("expected usage-event label snapshot from request-time selected endpoint identity, got %+v", row)
	}
}

func TestRuntimeUsageEventEndpointLabelSnapshotFallsBackToBaseURL(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "runtime-usage-label-base-url-public-" + suffix
	targetModelID := "runtime-usage-label-base-url-target-" + suffix
	endpointBaseURL := harness.upstream.baseURL("/request-logs/usage-label/base-url")
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-usage-label-base-url-strategy-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, profileID, "   ", endpointBaseURL, "runtime-usage-label-base-url-key")
	harness.seedConnection(t, profileID, targetModelConfigID, endpointID, "runtime-usage-label-base-url-connection-"+suffix, nil, nil, 0)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "persist endpoint label base URL fallback"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	row := loadLatestRuntimeUsageEndpointLabelSnapshot(t, harness.conn, profileID)
	if row.EndpointLabelSnapshot != endpointBaseURL {
		t.Fatalf("expected usage-event label snapshot to fall back to endpoint base URL %q, got %+v", endpointBaseURL, row)
	}
}

func TestRuntimeUsageEventEndpointLabelSnapshotForSelectedEndpoint(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "gpt-4o-runtime-usage-label-unknown-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-usage-label-unknown-strategy-"+suffix, "fill-first")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	smallEndpointID := harness.seedEndpoint(t, profileID, "runtime-usage-label-unknown-small-"+suffix, harness.upstream.baseURL("/request-logs/usage-label/unknown/small"), "runtime-usage-label-unknown-small-key")
	largeEndpointID := harness.seedEndpoint(t, profileID, "runtime-usage-label-unknown-large-"+suffix, harness.upstream.baseURL("/request-logs/usage-label/unknown/large"), "runtime-usage-label-unknown-large-key")
	smallConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, smallEndpointID, "runtime-usage-label-unknown-small-connection-"+suffix, nil, nil, 0)
	largeConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, largeEndpointID, "runtime-usage-label-unknown-large-connection-"+suffix, nil, nil, 1)
	_ = largeConnectionID
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages":              []map[string]any{{"role": "user", "content": "force no endpoint attribution"}},
		"model":                 publicModelID,
		"max_completion_tokens": 600,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	row := loadLatestRuntimeUsageEndpointLabelSnapshot(t, harness.conn, profileID)
	if !row.EndpointID.Valid || int(row.EndpointID.Int64) != smallEndpointID || !row.ConnectionID.Valid || int(row.ConnectionID.Int64) != smallConnectionID {
		t.Fatalf("expected usage event to persist selected endpoint/connection, got %+v", row)
	}
	if row.EndpointLabelSnapshot != "runtime-usage-label-unknown-small-"+suffix {
		t.Fatalf("expected selected endpoint label snapshot, got %+v", row)
	}
}

func TestRuntimeRequestLogPersistsFailoverAttemptRowsAndSingleUsageEvent(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "runtime-request-log-fill-first-" + suffix
	targetModelID := "runtime-request-log-target-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-request-log-secondary"})
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-request-log-fill-first-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "runtime-request-log-primary-endpoint-"+suffix, primaryUpstream.baseURL("/request-logs/fill-first/primary"), "runtime-request-log-primary-key")
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "runtime-request-log-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/request-logs/fill-first/secondary"), "runtime-request-log-secondary-key")
	primaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "runtime-request-log-primary-connection-"+suffix, nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "runtime-request-log-secondary-connection-"+suffix, nil, nil, 1)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "persist failover attempt counts"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected primary upstream to receive one failover attempt, got %d requests", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected secondary upstream to receive one failover attempt, got %d requests", got)
	}
	assertLatestRuntimeAttemptSequence(t, harness.conn, profileID, []runtimeRequestLogAttempt{{
		AttemptNumber: 1,
		ConnectionID:  primaryConnectionID,
		EndpointID:    primaryEndpointID,
		StatusCode:    http.StatusServiceUnavailable,
		SuccessFlag:   false,
	}, {
		AttemptNumber: 2,
		ConnectionID:  secondaryConnectionID,
		EndpointID:    secondaryEndpointID,
		StatusCode:    http.StatusOK,
		SuccessFlag:   true,
	}})
}

func TestRuntimeRequestLogPersistsStreamedResponsesUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998}}}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":5}}}}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "stream-public-" + randomSuffix(),
		TargetModelID:   "stream-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-stream-key",
	})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/responses",
		map[string]any{
			"model": route.PublicModelID,
			"input": []map[string]any{{
				"type": "message",
				"role": "user",
				"content": []map[string]any{{
					"type": "input_text",
					"text": "你好",
				}},
			}},
			"stream": true,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:          runtimeNullInt64(5),
		OutputTokens:         runtimeNullInt64(8),
		TotalTokens:          runtimeNullInt64(20),
		CacheReadInputTokens: runtimeNullInt64(2),
		ReasoningTokens:      runtimeNullInt64(5),
	})
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || row.StreamErrorKind.Valid || row.StreamErrorDetail.Valid || !row.TotalCostUserCurrencyMicros.Valid || row.TotalCostUserCurrencyMicros.Int64 != 50 || !row.PricedFlag.Valid || !row.PricedFlag.Bool || row.UnpricedReason.Valid || !row.CompletionDurationMS.Valid {
		t.Fatalf("expected completed streamed request log to persist priced stream telemetry, got %+v", row)
	}
	usageEventRow := loadLatestRuntimeUsageEventStreamTelemetryRow(t, harness.conn, profileID)
	if usageEventRow.StreamOutcome != "completed" || usageEventRow.StreamErrorKind.Valid || !usageEventRow.TotalCostUserCurrencyMicros.Valid || usageEventRow.TotalCostUserCurrencyMicros.Int64 != 50 || !usageEventRow.PricedFlag.Valid || !usageEventRow.PricedFlag.Bool || usageEventRow.UnpricedReason.Valid || !usageEventRow.CompletionDurationMS.Valid {
		t.Fatalf("expected completed streamed usage event to persist priced stream telemetry, got %+v", usageEventRow)
	}
}

func TestRuntimeRequestLogPersistsProviderIncompleteStreamOutcome(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		_, _ = io.WriteString(w, "event: response.incomplete\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "stream-incomplete-public-" + randomSuffix(), TargetModelID: "stream-incomplete-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-stream-incomplete-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-incomplete-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "provider incomplete stream", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "provider_incomplete" || row.StreamErrorKind.Valid || row.StreamErrorDetail.Valid || !row.CompletionDurationMS.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected provider-incomplete stream telemetry with terminal duration and unavailable usage reason, got %+v", row)
	}
}

func TestRuntimeRequestLogPersistsStreamEOFWithoutTerminalOutcome(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "stream-missing-terminal-public-" + randomSuffix(), TargetModelID: "stream-missing-terminal-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-stream-missing-terminal-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-missing-terminal-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "missing terminal stream", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "upstream_ended_without_terminal" || !row.StreamErrorKind.Valid || row.StreamErrorKind.String != "missing_terminal_event" || row.StreamErrorDetail.Valid || row.InputTokens.Valid || row.OutputTokens.Valid || row.TotalTokens.Valid || row.TotalCostUserCurrencyMicros.Valid || row.CompletionDurationMS.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected missing-terminal stream telemetry with null usage/cost and unavailable reason, got %+v", row)
	}
}

func TestRuntimeRequestLogCompletedStreamWithMissingUsageKeepsMissingTokenUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":null}}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "stream-missing-usage-public-" + randomSuffix(), TargetModelID: "stream-missing-usage-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-stream-missing-usage-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-missing-usage-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "completed stream without usage", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || row.StreamErrorKind.Valid || row.TotalCostUserCurrencyMicros.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "MISSING_TOKEN_USAGE" {
		t.Fatalf("expected completed missing-usage stream to keep missing-token reason, got %+v", row)
	}
}

func TestRuntimeRequestLogUsageDiscardInvalidUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":    "chatcmpl-invalid-usage-" + suffix,
		"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 19},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "invalid-usage-public-" + suffix, TargetModelID: "invalid-usage-target-" + suffix, EndpointBaseURL: upstream.baseURL("/request-logs/invalid-usage"), EndpointAPIKey: "runtime-invalid-usage-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-invalid-usage-pricing-"+suffix, loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "invalid usage should discard"}}}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	assertRuntimePricingRowDiscardedUsage(t, loadLatestRuntimePricingRowForTable(t, harness.conn, profileID, "request_logs"), "MISSING_TOKEN_USAGE")
	assertRuntimePricingRowDiscardedUsage(t, loadLatestRuntimePricingRowForTable(t, harness.conn, profileID, "usage_request_events"), "MISSING_TOKEN_USAGE")
}

func TestRuntimeRequestLogMissingTerminalUsageDiscard(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}},\"delta\":\"partial\"}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "missing-terminal-usage-public-" + randomSuffix(), TargetModelID: "missing-terminal-usage-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-missing-terminal-usage-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-missing-terminal-usage-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "missing terminal with usage", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "upstream_ended_without_terminal" || !row.StreamErrorKind.Valid || row.StreamErrorKind.String != "missing_terminal_event" || row.InputTokens.Valid || row.OutputTokens.Valid || row.TotalTokens.Valid || row.TotalCostUserCurrencyMicros.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected missing-terminal usage to be discarded in request log, got %+v", row)
	}
	usageEventRow := loadLatestRuntimeUsageEventStreamTelemetryRow(t, harness.conn, profileID)
	if usageEventRow.StreamOutcome != "upstream_ended_without_terminal" || usageEventRow.InputTokens.Valid || usageEventRow.OutputTokens.Valid || usageEventRow.TotalTokens.Valid || usageEventRow.TotalCostUserCurrencyMicros.Valid || !usageEventRow.UnpricedReason.Valid || usageEventRow.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected missing-terminal usage to be discarded in usage event, got %+v", usageEventRow)
	}
}

func TestRuntimeAnthropicStreamRequestLogPersistsSplitUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":7,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":3,\"output_tokens\":1}}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":13}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "anthropic",
		PublicModelID:   "stream-anthropic-public-" + randomSuffix(),
		TargetModelID:   "stream-anthropic-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-anthropic-stream-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/messages",
		map[string]any{
			"model":      route.PublicModelID,
			"max_tokens": 16,
			"stream":     true,
			"messages":   []map[string]any{{"role": "user", "content": "你好"}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:              runtimeNullInt64(7),
		OutputTokens:             runtimeNullInt64(13),
		TotalTokens:              runtimeNullInt64(25),
		CacheReadInputTokens:     runtimeNullInt64(2),
		CacheCreationInputTokens: runtimeNullInt64(3),
	})
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
}

func TestRuntimeRequestLogPersistsStreamedGeminiUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好\"}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好！有什么我可以帮你的吗？\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":20,\"cachedContentTokenCount\":3,\"thoughtsTokenCount\":5}}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "stream-gemini-public-" + randomSuffix(),
		TargetModelID:   "stream-gemini-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-gemini-stream-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID),
		map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": "你好"}},
			}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:          runtimeNullInt64(4),
		OutputTokens:         runtimeNullInt64(8),
		TotalTokens:          runtimeNullInt64(20),
		CacheReadInputTokens: runtimeNullInt64(3),
		ReasoningTokens:      runtimeNullInt64(5),
	})
	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || row.StreamErrorKind.Valid || row.StreamErrorDetail.Valid || !row.CompletionDurationMS.Valid {
		t.Fatalf("expected Gemini streamGenerateContent response to persist completed stream telemetry, got %+v", row)
	}
	usageEventRow := loadLatestRuntimeUsageEventStreamTelemetryRow(t, harness.conn, profileID)
	if usageEventRow.StreamOutcome != "completed" || usageEventRow.StreamErrorKind.Valid || !usageEventRow.CompletionDurationMS.Valid {
		t.Fatalf("expected Gemini streamGenerateContent usage event to persist completed stream telemetry, got %+v", usageEventRow)
	}
}

func TestRuntimeRequestLogPersistsGeminiStreamGenerateContentUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好\"}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好！有什么我可以帮你的吗？\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":99,\"cachedContentTokenCount\":3,\"thoughtsTokenCount\":5}}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "stream-gemini-authoritative-public-" + randomSuffix(),
		TargetModelID:   "stream-gemini-authoritative-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-gemini-stream-authoritative-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID),
		map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": "你好"}},
			}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:          runtimeNullInt64(4),
		OutputTokens:         runtimeNullInt64(8),
		TotalTokens:          runtimeNullInt64(99),
		CacheReadInputTokens: runtimeNullInt64(3),
		ReasoningTokens:      runtimeNullInt64(5),
	})
}

type runtimeDurableHistoryCounts struct {
	RequestLogs       int
	UsageEvents       int
	AuditLogs         int
	LoadbalanceEvents int
}

type runtimeUsageEndpointLabelSnapshotRow struct {
	EndpointID            sql.NullInt64
	ConnectionID          sql.NullInt64
	EndpointLabelSnapshot string
}

func loadLatestRuntimeUsageEndpointLabelSnapshot(t *testing.T, conn *pgx.Conn, profileID int) runtimeUsageEndpointLabelSnapshotRow {
	t.Helper()
	var row runtimeUsageEndpointLabelSnapshotRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT endpoint_id, connection_id, endpoint_label_snapshot FROM usage_request_events WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&row.EndpointID, &row.ConnectionID, &row.EndpointLabelSnapshot); err != nil {
		t.Fatalf("load latest runtime usage endpoint label snapshot: %v", err)
	}
	return row
}

func assertRuntimeDurableHistoryCounts(t *testing.T, conn *pgx.Conn, profileID int, want runtimeDurableHistoryCounts) {
	t.Helper()
	var got runtimeDurableHistoryCounts
	if err := conn.QueryRow(
		context.Background(),
		`SELECT
			(SELECT COUNT(*) FROM request_logs WHERE profile_id = $1),
			(SELECT COUNT(*) FROM usage_request_events WHERE profile_id = $1),
			(SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1),
			(SELECT COUNT(*) FROM loadbalance_events WHERE profile_id = $1)`,
		profileID,
	).Scan(&got.RequestLogs, &got.UsageEvents, &got.AuditLogs, &got.LoadbalanceEvents); err != nil {
		t.Fatalf("load durable history counts: %v", err)
	}
	if got != want {
		t.Fatalf("expected durable history counts %+v, got %+v", want, got)
	}
}

func assertRuntimeRetainedStatsAPIs(t *testing.T, harness *runtimeHarness, profileID int) {
	t.Helper()
	tests := []struct {
		name   string
		path   string
		assert func(map[string]any)
	}{
		{
			name: "request history",
			path: "/api/stats/requests?limit=50&offset=0",
			assert: func(payload map[string]any) {
				items, ok := payload["items"].([]any)
				if !ok || len(items) != 2 {
					t.Fatalf("expected retained request history to return two durable rows, got %+v", payload)
				}
			},
		},
		{
			name: "spending",
			path: "/api/stats/spending?preset=1h&group_by=none&limit=50&offset=0",
			assert: func(payload map[string]any) {
				if _, ok := payload["summary"].(map[string]any); !ok {
					t.Fatalf("expected retained spending API summary, got %+v", payload)
				}
			},
		},
		{
			name: "usage snapshot",
			path: "/api/stats/usage-snapshot?preset=1h",
			assert: func(payload map[string]any) {
				if _, ok := payload["overview"].(map[string]any); !ok {
					t.Fatalf("expected retained usage snapshot overview, got %+v", payload)
				}
			},
		},
		{
			name: "dashboard aggregate",
			path: "/api/stats/dashboard?window=24h",
			assert: func(payload map[string]any) {
				if _, ok := payload["metric_snapshot"].(map[string]any); !ok {
					t.Fatalf("expected retained dashboard metric snapshot, got %+v", payload)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := harness.requestJSON(t, http.MethodGet, test.path, nil, runtimeModelHeader(profileID))
			assertStatus(t, response, http.StatusOK)
			var payload map[string]any
			decodeJSONResponse(t, response, &payload)
			test.assert(payload)
		})
	}
}

type runtimePersistedStreamTelemetryRow struct {
	StreamOutcome               string
	StreamErrorKind             sql.NullString
	StreamErrorDetail           sql.NullString
	InputTokens                 sql.NullInt64
	OutputTokens                sql.NullInt64
	TotalTokens                 sql.NullInt64
	TotalCostUserCurrencyMicros sql.NullInt64
	PricedFlag                  sql.NullBool
	UnpricedReason              sql.NullString
	CompletionDurationMS        sql.NullInt64
}

type runtimeReportCurrencySnapshot struct {
	Code   string
	Symbol string
}

func loadRuntimeReportCurrencySnapshot(t *testing.T, conn *pgx.Conn, profileID int) runtimeReportCurrencySnapshot {
	t.Helper()
	var snapshot runtimeReportCurrencySnapshot
	if err := conn.QueryRow(
		context.Background(),
		`SELECT report_currency_code, report_currency_symbol FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`,
		profileID,
	).Scan(&snapshot.Code, &snapshot.Symbol); err != nil {
		t.Fatalf("load runtime report currency snapshot: %v", err)
	}
	return snapshot
}

func loadRuntimeReportCurrencyCode(t *testing.T, conn *pgx.Conn, profileID int) string {
	return loadRuntimeReportCurrencySnapshot(t, conn, profileID).Code
}

func loadLatestRuntimeRequestLogStreamTelemetryRow(t *testing.T, conn *pgx.Conn, profileID int) runtimePersistedStreamTelemetryRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var row runtimePersistedStreamTelemetryRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT stream_outcome, stream_error_kind, stream_error_detail, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, priced_flag, unpriced_reason, completion_duration_ms FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&row.StreamOutcome, &row.StreamErrorKind, &row.StreamErrorDetail, &row.InputTokens, &row.OutputTokens, &row.TotalTokens, &row.TotalCostUserCurrencyMicros, &row.PricedFlag, &row.UnpricedReason, &row.CompletionDurationMS); err != nil {
		t.Fatalf("load latest runtime request-log stream telemetry row: %v", err)
	}
	return row
}

func loadLatestRuntimeUsageEventStreamTelemetryRow(t *testing.T, conn *pgx.Conn, profileID int) runtimePersistedStreamTelemetryRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var row runtimePersistedStreamTelemetryRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT stream_outcome, stream_error_kind, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, priced_flag, unpriced_reason, completion_duration_ms FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&row.StreamOutcome, &row.StreamErrorKind, &row.InputTokens, &row.OutputTokens, &row.TotalTokens, &row.TotalCostUserCurrencyMicros, &row.PricedFlag, &row.UnpricedReason, &row.CompletionDurationMS); err != nil {
		t.Fatalf("load latest runtime usage-event stream telemetry row: %v", err)
	}
	return row
}

type runtimePersistedUsageRow struct {
	InputTokens              sql.NullInt64
	OutputTokens             sql.NullInt64
	TotalTokens              sql.NullInt64
	CacheReadInputTokens     sql.NullInt64
	CacheCreationInputTokens sql.NullInt64
	ReasoningTokens          sql.NullInt64
}

func assertLatestRequestLogUsage(t *testing.T, conn *pgx.Conn, profileID int, expectStream bool, wantInput int64, wantOutput int64, wantTotal int64) {
	t.Helper()
	assertLatestRuntimeUsageRows(t, conn, profileID, expectStream, runtimePersistedUsageRow{
		InputTokens:  runtimeNullInt64(wantInput),
		OutputTokens: runtimeNullInt64(wantOutput),
		TotalTokens:  runtimeNullInt64(wantTotal),
	})
}

func assertLatestRuntimeUsageRows(t *testing.T, conn *pgx.Conn, profileID int, expectStream bool, want runtimePersistedUsageRow) {
	t.Helper()
	waitForRuntimeTelemetryCounts(t, conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var isStream bool
	requestLogRow := runtimePersistedUsageRow{}
	if err := conn.QueryRow(
		context.Background(),
		`SELECT is_stream, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&isStream, &requestLogRow.InputTokens, &requestLogRow.OutputTokens, &requestLogRow.TotalTokens, &requestLogRow.CacheReadInputTokens, &requestLogRow.CacheCreationInputTokens, &requestLogRow.ReasoningTokens); err != nil {
		t.Fatalf("load latest request_logs usage row: %v", err)
	}
	if isStream != expectStream {
		t.Fatalf("expected request_logs is_stream=%t, got %t", expectStream, isStream)
	}
	if requestLogRow != want {
		t.Fatalf("expected request_logs canonical usage row %+v, got %+v", want, requestLogRow)
	}
	usageEventRow := runtimePersistedUsageRow{}
	if err := conn.QueryRow(
		context.Background(),
		`SELECT input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&usageEventRow.InputTokens, &usageEventRow.OutputTokens, &usageEventRow.TotalTokens, &usageEventRow.CacheReadInputTokens, &usageEventRow.CacheCreationInputTokens, &usageEventRow.ReasoningTokens); err != nil {
		t.Fatalf("load latest usage_request_events usage row: %v", err)
	}
	if usageEventRow != want {
		t.Fatalf("expected usage_request_events canonical usage row %+v, got %+v", want, usageEventRow)
	}
}

func runtimeNullInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}

func runtimeNullBool(value bool) sql.NullBool {
	return sql.NullBool{Bool: value, Valid: true}
}

func runtimeNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}

type runtimeRequestLogAttempt struct {
	AttemptNumber int
	ConnectionID  int
	EndpointID    int
	StatusCode    int
	SuccessFlag   bool
}

type runtimePersistedPricingRow struct {
	AttemptMetric                     int
	BillableFlag                      sql.NullBool
	PricedFlag                        sql.NullBool
	UnpricedReason                    sql.NullString
	InputTokens                       sql.NullInt64
	OutputTokens                      sql.NullInt64
	TotalTokens                       sql.NullInt64
	CacheReadInputTokens              sql.NullInt64
	CacheCreationInputTokens          sql.NullInt64
	ReasoningTokens                   sql.NullInt64
	InputCostMicros                   sql.NullInt64
	OutputCostMicros                  sql.NullInt64
	CacheReadInputCostMicros          sql.NullInt64
	CacheCreationInputCostMicros      sql.NullInt64
	ReasoningCostMicros               sql.NullInt64
	TotalCostOriginalMicros           sql.NullInt64
	TotalCostUserCurrencyMicros       sql.NullInt64
	CurrencyCodeOriginal              sql.NullString
	ReportCurrencyCode                sql.NullString
	ReportCurrencySymbol              sql.NullString
	FXRateUsed                        sql.NullString
	FXRateSource                      sql.NullString
	PricingSnapshotUnit               sql.NullString
	PricingSnapshotInput              sql.NullString
	PricingSnapshotOutput             sql.NullString
	PricingSnapshotCacheReadInput     sql.NullString
	PricingSnapshotCacheCreationInput sql.NullString
	PricingSnapshotReasoning          sql.NullString
	PricingConfigVersionUsed          sql.NullInt64
}

func wantRuntimePricingRow(mutate func(*runtimePersistedPricingRow)) runtimePersistedPricingRow {
	row := runtimePersistedPricingRow{
		AttemptMetric: 1,
		BillableFlag:  runtimeNullBool(true),
	}
	if mutate != nil {
		mutate(&row)
	}
	return row
}

func wantPricedRow(mutate func(*runtimePersistedPricingRow)) runtimePersistedPricingRow {
	return wantRuntimePricingRow(func(row *runtimePersistedPricingRow) {
		row.PricedFlag = runtimeNullBool(true)
		row.FXRateUsed = runtimeNullString("1")
		row.FXRateSource = runtimeNullString("DEFAULT_1_TO_1")
		row.PricingSnapshotUnit = runtimeNullString("PER_1M")
		row.PricingConfigVersionUsed = runtimeNullInt64(1)
		if mutate != nil {
			mutate(row)
		}
	})
}

func wantUnpricedRow(reason string, mutate func(*runtimePersistedPricingRow)) runtimePersistedPricingRow {
	return wantRuntimePricingRow(func(row *runtimePersistedPricingRow) {
		row.PricedFlag = runtimeNullBool(false)
		row.UnpricedReason = runtimeNullString(reason)
		if mutate != nil {
			mutate(row)
		}
	})
}

func assertRuntimePricingRowDiscardedUsage(t *testing.T, row runtimePersistedPricingRow, wantReason string) {
	t.Helper()
	if row.InputTokens.Valid || row.OutputTokens.Valid || row.TotalTokens.Valid || row.CacheReadInputTokens.Valid || row.CacheCreationInputTokens.Valid || row.ReasoningTokens.Valid {
		t.Fatalf("expected token usage to be discarded, got %+v", row)
	}
	if row.InputCostMicros.Valid || row.OutputCostMicros.Valid || row.CacheReadInputCostMicros.Valid || row.CacheCreationInputCostMicros.Valid || row.ReasoningCostMicros.Valid || row.TotalCostOriginalMicros.Valid || row.TotalCostUserCurrencyMicros.Valid {
		t.Fatalf("expected discarded usage to skip pricing costs, got %+v", row)
	}
	if !row.BillableFlag.Valid || !row.BillableFlag.Bool || row.PricedFlag.Valid && row.PricedFlag.Bool || !row.UnpricedReason.Valid || row.UnpricedReason.String != wantReason {
		t.Fatalf("expected discarded usage to be billable but unpriced as %s, got %+v", wantReason, row)
	}
}

func loadLatestRuntimeIngressRequestID(t *testing.T, conn *pgx.Conn, profileID int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var ingressRequestID string
		err := conn.QueryRow(
			context.Background(),
			`SELECT ingress_request_id FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
			profileID,
		).Scan(&ingressRequestID)
		if err == nil {
			return ingressRequestID
		}
		time.Sleep(25 * time.Millisecond)
	}
	var ingressRequestID string
	if err := conn.QueryRow(
		context.Background(),
		`SELECT ingress_request_id FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&ingressRequestID); err != nil {
		t.Fatalf("load latest runtime ingress request id: %v", err)
	}
	return ingressRequestID
}

const runtimePersistedPricingRowSelectColumns = `billable_flag, priced_flag, unpriced_reason, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used`

func loadLatestRuntimePricingRowForTable(t *testing.T, conn *pgx.Conn, profileID int, tableName string) runtimePersistedPricingRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	attemptMetricColumn := "attempt_number"
	orderBy := "attempt_number DESC, id DESC"
	switch tableName {
	case "request_logs":
	case "usage_request_events":
		attemptMetricColumn = "attempt_count"
		orderBy = "id DESC"
	default:
		t.Fatalf("unsupported runtime pricing table %q", tableName)
	}
	query := fmt.Sprintf(
		`SELECT %s, %s FROM %s WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY %s LIMIT 1`,
		attemptMetricColumn,
		runtimePersistedPricingRowSelectColumns,
		tableName,
		orderBy,
	)
	var row runtimePersistedPricingRow
	if err := conn.QueryRow(context.Background(), query, profileID, ingressRequestID).Scan(
		&row.AttemptMetric,
		&row.BillableFlag,
		&row.PricedFlag,
		&row.UnpricedReason,
		&row.InputTokens,
		&row.OutputTokens,
		&row.TotalTokens,
		&row.CacheReadInputTokens,
		&row.CacheCreationInputTokens,
		&row.ReasoningTokens,
		&row.InputCostMicros,
		&row.OutputCostMicros,
		&row.CacheReadInputCostMicros,
		&row.CacheCreationInputCostMicros,
		&row.ReasoningCostMicros,
		&row.TotalCostOriginalMicros,
		&row.TotalCostUserCurrencyMicros,
		&row.CurrencyCodeOriginal,
		&row.ReportCurrencyCode,
		&row.ReportCurrencySymbol,
		&row.FXRateUsed,
		&row.FXRateSource,
		&row.PricingSnapshotUnit,
		&row.PricingSnapshotInput,
		&row.PricingSnapshotOutput,
		&row.PricingSnapshotCacheReadInput,
		&row.PricingSnapshotCacheCreationInput,
		&row.PricingSnapshotReasoning,
		&row.PricingConfigVersionUsed,
	); err != nil {
		t.Fatalf("load latest runtime pricing row from %s: %v", tableName, err)
	}
	return row
}

func assertLatestRuntimePricingRows(t *testing.T, conn *pgx.Conn, profileID int, want runtimePersistedPricingRow, label string) {
	t.Helper()
	requestLogRow := loadLatestRuntimePricingRowForTable(t, conn, profileID, "request_logs")
	if requestLogRow != want {
		t.Fatalf("expected %s request_logs pricing row %+v, got %+v", label, want, requestLogRow)
	}
	usageEventRow := loadLatestRuntimePricingRowForTable(t, conn, profileID, "usage_request_events")
	if usageEventRow != want {
		t.Fatalf("expected %s usage_request_events pricing row %+v, got %+v", label, want, usageEventRow)
	}
	if requestLogRow != usageEventRow {
		t.Fatalf("expected %s request_logs and usage_request_events rows to agree, got request_logs=%+v usage_request_events=%+v", label, requestLogRow, usageEventRow)
	}
}

func assertLatestRuntimeWinningRequestLogTiming(t *testing.T, conn *pgx.Conn, profileID int, expectTTFT bool) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	var ttftMs sql.NullInt64
	var completionDurationMs sql.NullInt64
	if err := conn.QueryRow(
		context.Background(),
		`SELECT ttft_ms, completion_duration_ms FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&ttftMs, &completionDurationMs); err != nil {
		t.Fatalf("load runtime winning request-log timing: %v", err)
	}
	if expectTTFT {
		if !ttftMs.Valid || ttftMs.Int64 < 1 {
			t.Fatalf("expected streamed winning request log to persist positive ttft_ms, got %+v", ttftMs)
		}
		if !completionDurationMs.Valid || completionDurationMs.Int64 < ttftMs.Int64 {
			t.Fatalf("expected streamed winning request log to persist completion_duration_ms >= ttft_ms, got ttft=%+v completion=%+v", ttftMs, completionDurationMs)
		}
		return
	}
	if ttftMs.Valid {
		t.Fatalf("expected streamed winning request log without meaningful payload to keep ttft_ms NULL, got %+v", ttftMs)
	}
	if !completionDurationMs.Valid || completionDurationMs.Int64 < 1 {
		t.Fatalf("expected streamed winning request log to persist positive completion_duration_ms, got %+v", completionDurationMs)
	}
}

func assertLatestRuntimeAttemptCounts(t *testing.T, conn *pgx.Conn, profileID int, wantRequestLogAttempt int, wantUsageEventAttempt int) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	rows, err := conn.Query(
		context.Background(),
		`SELECT attempt_number FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number ASC, id ASC`,
		profileID,
		ingressRequestID,
	)
	if err != nil {
		t.Fatalf("query runtime request-log attempts: %v", err)
	}
	defer rows.Close()

	attemptNumbers := make([]int, 0)
	for rows.Next() {
		var attemptNumber int
		if err := rows.Scan(&attemptNumber); err != nil {
			t.Fatalf("scan runtime request-log attempt number: %v", err)
		}
		attemptNumbers = append(attemptNumbers, attemptNumber)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime request-log attempt numbers: %v", err)
	}
	if len(attemptNumbers) != wantRequestLogAttempt {
		t.Fatalf("expected %d request_logs rows for ingress_request_id %q, got %v", wantRequestLogAttempt, ingressRequestID, attemptNumbers)
	}
	for index, attemptNumber := range attemptNumbers {
		wantAttemptNumber := index + 1
		if attemptNumber != wantAttemptNumber {
			t.Fatalf("expected request_logs attempt_number sequence %d..%d, got %v", 1, wantRequestLogAttempt, attemptNumbers)
		}
	}

	var usageEventCount int
	var usageEventAttempt int
	if err := conn.QueryRow(
		context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(attempt_count), 0) FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2`,
		profileID,
		ingressRequestID,
	).Scan(&usageEventCount, &usageEventAttempt); err != nil {
		t.Fatalf("load runtime usage-event attempt count: %v", err)
	}
	if usageEventCount != 1 {
		t.Fatalf("expected exactly 1 usage_request_events row for ingress_request_id %q, got %d", ingressRequestID, usageEventCount)
	}
	if usageEventAttempt != wantUsageEventAttempt {
		t.Fatalf("expected usage_request_events attempt_count=%d, got %d", wantUsageEventAttempt, usageEventAttempt)
	}
}

func assertLatestRuntimeModelIdentity(t *testing.T, conn *pgx.Conn, profileID int, wantModelID string, wantResolvedTargetModelID string) {
	t.Helper()
	assertLatestRuntimeModelIdentityState(t, conn, profileID, wantModelID, sql.NullString{String: wantResolvedTargetModelID, Valid: true})
}

func assertLatestRuntimeModelIdentityNull(t *testing.T, conn *pgx.Conn, profileID int, wantModelID string) {
	t.Helper()
	assertLatestRuntimeModelIdentityState(t, conn, profileID, wantModelID, sql.NullString{})
}

func assertLatestRuntimeModelIdentityState(t *testing.T, conn *pgx.Conn, profileID int, wantModelID string, wantResolvedTargetModelID sql.NullString) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	var requestLogModelID string
	var requestLogResolvedTargetModelID sql.NullString
	if err := conn.QueryRow(
		context.Background(),
		`SELECT model_id, resolved_target_model_id FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&requestLogModelID, &requestLogResolvedTargetModelID); err != nil {
		t.Fatalf("load runtime request-log model identity: %v", err)
	}
	if requestLogModelID != wantModelID || requestLogResolvedTargetModelID != wantResolvedTargetModelID {
		t.Fatalf("expected request_logs identity requested=%q resolved=%+v, got requested=%q resolved=%+v", wantModelID, wantResolvedTargetModelID, requestLogModelID, requestLogResolvedTargetModelID)
	}

	var usageEventModelID string
	var usageEventResolvedTargetModelID sql.NullString
	if err := conn.QueryRow(
		context.Background(),
		`SELECT model_id, resolved_target_model_id FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&usageEventModelID, &usageEventResolvedTargetModelID); err != nil {
		t.Fatalf("load runtime usage-event model identity: %v", err)
	}
	if usageEventModelID != wantModelID || usageEventResolvedTargetModelID != wantResolvedTargetModelID {
		t.Fatalf("expected usage_request_events identity requested=%q resolved=%+v, got requested=%q resolved=%+v", wantModelID, wantResolvedTargetModelID, usageEventModelID, usageEventResolvedTargetModelID)
	}
}

func assertLatestRuntimeAttemptSequence(t *testing.T, conn *pgx.Conn, profileID int, want []runtimeRequestLogAttempt) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	rows, err := conn.Query(
		context.Background(),
		`SELECT attempt_number, connection_id, endpoint_id, status_code, success_flag FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number ASC, id ASC`,
		profileID,
		ingressRequestID,
	)
	if err != nil {
		t.Fatalf("query runtime request-log attempt sequence: %v", err)
	}
	defer rows.Close()

	got := make([]runtimeRequestLogAttempt, 0, len(want))
	for rows.Next() {
		var attempt runtimeRequestLogAttempt
		if err := rows.Scan(&attempt.AttemptNumber, &attempt.ConnectionID, &attempt.EndpointID, &attempt.StatusCode, &attempt.SuccessFlag); err != nil {
			t.Fatalf("scan runtime request-log attempt sequence: %v", err)
		}
		got = append(got, attempt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate runtime request-log attempt sequence: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d request-log attempt rows for ingress_request_id %q, got %+v", len(want), ingressRequestID, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected request-log attempt sequence %+v for ingress_request_id %q, got %+v", want, ingressRequestID, got)
		}
	}
}

func TestAuditLogsRejectDisabledRequestSnapshot(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	seedSimpleRequestLog(t, harness, profileID, 104, 12, nil, time.Date(2026, 4, 18, 12, 50, 0, 0, time.UTC), false)
	seedRuntimeAuditLog(t, harness, 804, profileID, 104, time.Date(2026, 4, 18, 12, 55, 0, 0, time.UTC))

	from := url.QueryEscape(time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC).Format(time.RFC3339))
	to := url.QueryEscape(time.Date(2026, 4, 18, 13, 0, 0, 0, time.UTC).Format(time.RFC3339))
	response := harness.requestJSON(t, http.MethodGet, "/api/audit/logs?from="+from+"&to="+to+"&request_log_id=104&limit=20", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusConflict)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit list, got %+v", payload)
	}

	detailResponse := harness.requestJSON(t, http.MethodGet, "/api/audit/logs/804", nil, runtimeModelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusConflict)
	decodeJSONResponse(t, detailResponse, &payload)
	if payload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit detail, got %+v", payload)
	}
}

func TestRequestLogCurrentModelEnrichmentContract(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	seedRequestLogUserAgentRules(t, harness, profileID)
	seedRequestLogModels(t, harness, profileID)
	seedFixtureRequestLog(t, harness, profileID)
	seedSimpleRequestLog(t, harness, profileID, 103, 12, nil, time.Date(2026, 4, 18, 12, 40, 0, 0, time.UTC), false)

	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)

	filterOptions := asMapRuntime(t, payload["filter_options"])
	models := filterOptions["models"].([]any)
	if !jsonBytesEqual(t, models, []any{
		map[string]any{"model_id": "gpt-4o-native", "model_label": "GPT-4o Native"},
		map[string]any{"model_id": "gpt-4o", "model_label": "GPT-4o Proxy"},
	}) {
		t.Fatalf("expected current model filter options to expose display-name enrichment, got %+v", models)
	}

	itemsByID := requestLogItemsByID(t, payload["items"].([]any))
	fixtureItem := itemsByID[101]
	if fixtureItem["model_label"] != "GPT-4o Proxy" || fixtureItem["resolved_target_model_label"] != "GPT-4o Native" {
		t.Fatalf("expected fixture request log to use current model display-name enrichment, got %+v", fixtureItem)
	}
	if _, ok := fixtureItem["is_proxy_origin"]; ok {
		t.Fatalf("did not expect proxy-origin field in fixture request log payload, got %+v", fixtureItem)
	}
	directItem := itemsByID[103]
	if directItem["model_label"] != "GPT-4o Proxy" || directItem["resolved_target_model_label"] != "GPT-4o Proxy" {
		t.Fatalf("expected current direct row to expose requested and final-target labels, got %+v", directItem)
	}

	detailResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/101", nil, runtimeModelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	decodeJSONResponse(t, detailResponse, &payload)
	summary := asMapRuntime(t, payload["summary"])
	if summary["model_label"] != "GPT-4o Proxy" || summary["resolved_target_model_label"] != "GPT-4o Native" {
		t.Fatalf("expected detail summary to use current model enrichment, got %+v", summary)
	}
	if _, ok := summary["is_proxy_origin"]; ok {
		t.Fatalf("did not expect proxy-origin field in detail summary, got %+v", summary)
	}
	routing := asMapRuntime(t, payload["routing"])
	if _, ok := routing["connection_id"]; ok {
		t.Fatalf("did not expect routing field connection_id in enriched detail payload, got %+v", routing)
	}
	for _, absent := range []string{"model_id", "resolved_target_model_id", "api_family"} {
		if _, ok := routing[absent]; ok {
			t.Fatalf("did not expect routing field %s in enriched detail payload, got %+v", absent, routing)
		}
	}
}

func newRequestLogContractHarness(t *testing.T) *requestLogContractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName := "s15_runtime_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s15-runtime-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}
	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "s15-runtime-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	statsService, err := managementstats.NewService(settings, managementstats.Options{Pool: pool, Now: func() time.Time { return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatalf("build stats service: %v", err)
	}
	t.Cleanup(statsService.Close)
	auditService, err := managementaudit.NewService(settings, managementaudit.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build audit service: %v", err)
	}
	t.Cleanup(auditService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s15-runtime-test", AuditService: auditService, StatsService: statsService})
	if err != nil {
		t.Fatalf("build runtime request-log handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &requestLogContractHarness{databaseName: databaseName, client: client, conn: conn, server: server, url: server.URL}
}

func (h *requestLogContractHarness) requestJSON(t *testing.T, method string, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, h.url+path, requestBody)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("perform request %s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func loadRuntimeDefaultProfileID(t *testing.T, harness *requestLogContractHarness) int {
	t.Helper()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func runtimeModelHeader(profileID int) map[string]string {
	return map[string]string{"X-Profile-Id": fmt.Sprintf("%d", profileID)}
}

func seedRuntimeAuditFamilySetting(t *testing.T, harness *runtimeHarness, profileID int, apiFamily string, auditEnabled bool, auditCaptureBodies bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(
		context.Background(),
		`INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $5)
		 ON CONFLICT (profile_id, api_family) DO UPDATE SET audit_enabled = EXCLUDED.audit_enabled, audit_capture_bodies = EXCLUDED.audit_capture_bodies, updated_at = EXCLUDED.updated_at`,
		profileID,
		apiFamily,
		auditEnabled,
		auditCaptureBodies,
		now,
	); err != nil {
		t.Fatalf("seed runtime audit setting %s: %v", apiFamily, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
}

func assertRuntimeRequestLogAuditSnapshot(t *testing.T, harness *runtimeHarness, profileID int, modelID string, wantAuditEnabled bool, wantAuditCaptureBodies bool) {
	t.Helper()
	var auditEnabledAtRequest bool
	var auditCaptureBodiesAtRequest bool
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT audit_enabled_at_request, audit_capture_bodies_at_request FROM request_logs WHERE profile_id = $1 AND model_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		modelID,
	).Scan(&auditEnabledAtRequest, &auditCaptureBodiesAtRequest); err != nil {
		t.Fatalf("load request-log audit snapshot for %s: %v", modelID, err)
	}
	if auditEnabledAtRequest != wantAuditEnabled || auditCaptureBodiesAtRequest != wantAuditCaptureBodies {
		t.Fatalf("expected request-log audit snapshot for %s to be enabled=%v capture=%v, got enabled=%v capture=%v", modelID, wantAuditEnabled, wantAuditCaptureBodies, auditEnabledAtRequest, auditCaptureBodiesAtRequest)
	}
}

func assertRuntimeAuditLogSnapshot(t *testing.T, harness *runtimeHarness, profileID int, modelID string, wantAuditEnabled bool, wantAuditCaptureBodies bool) {
	t.Helper()
	var auditEnabledAtRequest bool
	var auditCaptureBodiesAtRequest bool
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT audit_enabled_at_request, audit_capture_bodies_at_request FROM audit_logs WHERE profile_id = $1 AND model_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		modelID,
	).Scan(&auditEnabledAtRequest, &auditCaptureBodiesAtRequest); err != nil {
		t.Fatalf("load audit-log audit snapshot for %s: %v", modelID, err)
	}
	if auditEnabledAtRequest != wantAuditEnabled || auditCaptureBodiesAtRequest != wantAuditCaptureBodies {
		t.Fatalf("expected audit-log audit snapshot for %s to be enabled=%v capture=%v, got enabled=%v capture=%v", modelID, wantAuditEnabled, wantAuditCaptureBodies, auditEnabledAtRequest, auditCaptureBodiesAtRequest)
	}
}

func seedRequestLogEndpoints(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoints (id, profile_id, name, base_url, api_key, config_revision, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, 1, $6, $6), ($7, $2, $8, $9, $10, 1, $6, $6)`, 12, profileID, "Primary OpenAI", "https://api.openai.com", "fixture-key", now, 13, "Primary Anthropic", "https://api.anthropic.com", "fixture-key"); err != nil {
		t.Fatalf("seed request-log endpoints: %v", err)
	}
}

func seedRequestLogUserAgentRules(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, TRUE, FALSE, $4, $4), ($1, $5, $6, TRUE, FALSE, $4, $4)`, profileID, "Codex", "codex", now, "OpenAI SDK", "openai/python"); err != nil {
		t.Fatalf("seed request-log user-agent rules: %v", err)
	}
}

func insertRequestLogUserAgentRule(t *testing.T, harness *requestLogContractHarness, profileID *int, name string, pattern string, enabled bool, isSystem bool) int {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	var profileValue any
	if profileID != nil {
		profileValue = *profileID
	}
	var ruleID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, profileValue, name, pattern, enabled, isSystem, now).Scan(&ruleID); err != nil {
		t.Fatalf("insert request-log user-agent rule %q: %v", name, err)
	}
	return ruleID
}

func insertRequestLogProfile(t *testing.T, harness *requestLogContractHarness) int {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COALESCE(MAX(id), 0) + 1 FROM profiles`).Scan(&profileID); err != nil {
		t.Fatalf("choose request-log profile id: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO profiles (id, name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, NULL, FALSE, FALSE, TRUE, 1, $3, $3)`, profileID, "request-log-other-profile-"+randomSuffix(), now); err != nil {
		t.Fatalf("insert request-log profile: %v", err)
	}
	return profileID
}

func updateRequestLogUserAgents(t *testing.T, harness *requestLogContractHarness, profileID int, requestLogID int, callerUserAgent string, upstreamUserAgent string) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET caller_user_agent = $1, upstream_user_agent = $2 WHERE profile_id = $3 AND id = $4`, callerUserAgent, upstreamUserAgent, profileID, requestLogID); err != nil {
		t.Fatalf("update request-log user agents for %d: %v", requestLogID, err)
	}
}

func updateRequestLogModels(t *testing.T, harness *requestLogContractHarness, profileID int, requestLogID int, modelID string, resolvedTargetModelID string) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET model_id = $1, resolved_target_model_id = $2 WHERE profile_id = $3 AND id = $4`, modelID, resolvedTargetModelID, profileID, requestLogID); err != nil {
		t.Fatalf("update request-log models for %d: %v", requestLogID, err)
	}
}

func seedFixtureRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	createdAt := time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, resolved_target_model_id, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, reasoning_tokens, input_cost_micros, output_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_reasoning, cache_read_input_tokens, cache_creation_input_tokens, cache_read_input_cost_micros, cache_creation_input_cost_micros, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_config_version_used, request_path, error_detail, endpoint_description, created_at, caller_user_agent, upstream_user_agent, completion_duration_ms, ttft_ms, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, NULL, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, NULL, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, NULL, $44, $45, $46, $47, $48, $49, $50, $51)`, 101, profileID, "gpt-4o", "openai", "gpt-4o-native", 12, 34, "ingress_req_42", 2, "req_upstream_abc123", "https://api.openai.com", 200, 1234, false, 15, 42, 57, true, true, true, 0, 500, 750, 0, 1250, 1250, "USD", "USD", "$", "1", "DEFAULT_1_TO_1", "1M tokens", "2.500000", "10.000000", "0.000000", 0, 0, 0, 0, "1.250000", "0.000000", 1, "/v1/chat/completions", "Primary production key", createdAt, "codex/1.0", "OpenAI/Python 1.0", 914, 320, false, false); err != nil {
		t.Fatalf("seed fixture request log: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE request_logs SET operation_name = 'openai.chat_completions', upstream_operation_name = 'openai.responses', operation_translation_mode = 'openai_chat_completions_to_responses', upstream_request_path = '/v1/responses', request_generation_params = $1::jsonb, request_generation_params_status = 'complete', selected_terminal_target_id = 34 WHERE profile_id = $2 AND id = 101`, `{"provider":"openai","temperature":0.7,"top_p":0.9,"max_output_tokens":1024,"max_output_tokens_source":"max_completion_tokens","reasoning":{"effort":"low","source_field":"reasoning_effort"}}`, profileID); err != nil {
		t.Fatalf("seed fixture request generation params: %v", err)
	}
}

func seedSimpleRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, endpointID int, endpointBaseURL *string, createdAt time.Time, auditEnabledAtRequest bool) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	var historicalBaseURL any
	if endpointBaseURL != nil {
		historicalBaseURL = *endpointBaseURL
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, unpriced_reason, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'gpt-4o', 'gpt-4o', 'openai', $3, NULL, $4, 1, $5, 200, 120, FALSE, TRUE, TRUE, FALSE, 'MISSING_PRICE_DATA', '/v1/chat/completions', $6, $7, FALSE)`, id, profileID, endpointID, fmt.Sprintf("req-%d", id), historicalBaseURL, createdAt, auditEnabledAtRequest); err != nil {
		t.Fatalf("seed simple request log %d: %v", id, err)
	}
}

func seedFilteredRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, statusCode int, errorDetail *string, createdAt time.Time) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	success := statusCode >= 200 && statusCode < 300
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, error_detail, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'gpt-4o', 'gpt-4o', 'openai', 12, NULL, $3, 1, 'https://api.openai.com', $4, 120, FALSE, $5, TRUE, TRUE, '/v1/chat/completions', $6, $7, FALSE, FALSE)`, id, profileID, fmt.Sprintf("filtered-req-%d", id), statusCode, success, nullableTestString(errorDetail), createdAt); err != nil {
		t.Fatalf("seed filtered request log %d: %v", id, err)
	}
}

func seedPricingFilteredRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, priced bool, unpricedReason *string, hasCost bool, createdAt time.Time) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("request_logs", createdAt))
	var cost any
	if hasCost {
		cost = int64(1000)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, unpriced_reason, total_cost_user_currency_micros, request_path, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, 'gpt-4o', 'gpt-4o', 'openai', 12, NULL, $3, 1, 'https://api.openai.com', 200, 120, FALSE, TRUE, TRUE, $4, $5, $6, '/v1/chat/completions', $7, FALSE, FALSE)`, id, profileID, fmt.Sprintf("pricing-filtered-req-%d", id), priced, nullableTestString(unpricedReason), cost, createdAt); err != nil {
		t.Fatalf("seed pricing-filtered request log %d: %v", id, err)
	}
}

func seedRuntimeAuditLog(t *testing.T, harness *requestLogContractHarness, auditLogID int, profileID int, requestLogID int, createdAt time.Time) {
	t.Helper()
	ensureRuntimeTestLogPartitions(t, harness.databaseName, runtimeTestLogPartitionFor("audit_logs", createdAt))
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO audit_logs (id, profile_id, request_log_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_stored, response_status, response_headers, response_body, response_body_stored, is_stream, duration_ms, audit_enabled_at_request, audit_capture_bodies_at_request, created_at) VALUES ($1, $2, $3, 'gpt-4o', NULL, NULL, 'https://audit.invalid', 'Audit endpoint', 'POST', 'https://audit.invalid/v1/chat/completions', '{"authorization":"Bearer [REDACTED]"}', '{"messages":[{"role":"user","content":"hidden"}]}', TRUE, 200, '{"x-request-id":"req-hidden"}', '{"ok":true}', TRUE, FALSE, 1234, FALSE, TRUE, $4)`, auditLogID, profileID, requestLogID, createdAt); err != nil {
		t.Fatalf("seed runtime audit log %d: %v", auditLogID, err)
	}
}

func seedRequestLogModels(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	var strategyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'round-robin', ARRAY[403,422,429,500,502,503,504,529], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $3, $3) RETURNING id`, profileID, "request-log-current-models", now).Scan(&strategyID); err != nil {
		t.Fatalf("insert current request-log strategy: %v", err)
	}
	var nativeModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $3, $4, 'dual_native', TRUE, $5, $5) RETURNING id`, profileID, "gpt-4o-native", "GPT-4o Native", strategyID, now).Scan(&nativeModelID); err != nil {
		t.Fatalf("insert current native request-log model: %v", err)
	}
	var proxyModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', $2, $3, $4, 'dual_native', TRUE, $5, $5) RETURNING id`, profileID, "gpt-4o", "GPT-4o Proxy", strategyID, now).Scan(&proxyModelID); err != nil {
		t.Fatalf("insert current proxy request-log model: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, 0, TRUE, $4, $4)`, profileID, proxyModelID, nativeModelID, now); err != nil {
		t.Fatalf("insert request-log access target: %v", err)
	}
}

func insertRuntimePricingTemplate(t *testing.T, conn *pgx.Conn, profileID int, name string, pricingCurrencyCode string, inputPrice string, outputPrice string, cachedInputPrice string, cacheCreationPrice string, reasoningPrice string) int {
	t.Helper()
	now := time.Now().UTC()
	var templateID int
	if err := conn.QueryRow(
		context.Background(),
		`INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, NULL, 'PER_1M', $3, $4, $5, $6, $7, $8, 1, $9, $9) RETURNING id`,
		profileID,
		name,
		pricingCurrencyCode,
		inputPrice,
		outputPrice,
		cachedInputPrice,
		cacheCreationPrice,
		reasoningPrice,
		now,
	).Scan(&templateID); err != nil {
		t.Fatalf("insert runtime pricing template %q: %v", name, err)
	}
	return templateID
}

func attachRuntimeConnectionPricingTemplate(t *testing.T, harness *runtimeHarness, connectionID int, pricingTemplateID int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE connections SET pricing_template_id = $2, updated_at = $3 WHERE id = $1`,
		connectionID,
		pricingTemplateID,
		now,
	); err != nil {
		t.Fatalf("attach pricing template %d to runtime connection %d: %v", pricingTemplateID, connectionID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{harness.profileIDForConnection(t, connectionID)}})
}

func requestLogItemsByID(t *testing.T, rawItems []any) map[int]map[string]any {
	t.Helper()
	itemsByID := make(map[int]map[string]any, len(rawItems))
	for _, rawItem := range rawItems {
		item := asMapRuntime(t, rawItem)
		id, ok := item["id"].(float64)
		if !ok {
			t.Fatalf("expected request-log item id number, got %+v", item)
		}
		itemsByID[int(id)] = item
	}
	return itemsByID
}

func loadRequestFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve runtime request-log test file path")
	}
	fixturePath := filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "requests", name)
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode fixture %s: %v", fixturePath, err)
	}
	return payload
}

func jsonBytesEqual(t *testing.T, left any, right any) bool {
	t.Helper()
	leftRaw, err := json.Marshal(left)
	if err != nil {
		t.Fatalf("marshal left payload: %v", err)
	}
	rightRaw, err := json.Marshal(right)
	if err != nil {
		t.Fatalf("marshal right payload: %v", err)
	}
	return bytes.Equal(leftRaw, rightRaw)
}

func asMapRuntime(t *testing.T, raw any) map[string]any {
	t.Helper()
	item, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T %+v", raw, raw)
	}
	return item
}

func loadLatestRuntimeRequestLogDetailPayload(t *testing.T, harness *runtimeHarness, profileID int) map[string]any {
	t.Helper()
	var requestLogID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&requestLogID); err != nil {
		t.Fatalf("load latest facade request log id: %v", err)
	}
	response := harness.requestJSON(t, http.MethodGet, fmt.Sprintf("/api/stats/requests/%d", requestLogID), nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

package runtimetest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	safediag "github.com/coachpo/prism/backend/internal/domain/safediag"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

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

func TestRuntimePlanningFailureWritesSafeDiagnostics(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "runtime-planning-failure-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-planning-failure-"+suffix, "fill-first")
	_ = strategyID
	// A model with no enabled access targets produces a planning failure 503.
	harness.seedModel(t, profileID, "openai", modelID, "native", nil)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "planning failure diagnostics"}},
			"model":    modelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusServiceUnavailable)

	var rowKind, errorSource, errorCode, failureStage string
	var gatewayStatusCode *int
	var errorDetail *string
	var upstreamStatusCode *int
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := harness.conn.QueryRow(context.Background(), `
			SELECT row_kind, error_source, error_code, failure_stage, gateway_status_code, error_detail, upstream_status_code
			FROM request_logs
			WHERE profile_id = $1 AND model_id = $2
			ORDER BY id DESC LIMIT 1`,
			profileID, modelID,
		).Scan(&rowKind, &errorSource, &errorCode, &failureStage, &gatewayStatusCode, &errorDetail, &upstreamStatusCode)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("load planning failure request-log row: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if rowKind != "planning" {
		t.Fatalf("expected row_kind planning, got %q", rowKind)
	}
	if errorSource != "prism" || failureStage != "routing" {
		t.Fatalf("expected prism/routing failure source/stage, got %q/%q", errorSource, failureStage)
	}
	if errorCode == "" {
		t.Fatal("expected non-empty stable error_code for planning failure")
	}
	if errorDetail == nil || strings.TrimSpace(*errorDetail) == "" {
		t.Fatalf("expected non-empty safe error_detail, got %v", errorDetail)
	}
	if gatewayStatusCode == nil || *gatewayStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected gateway_status_code 503, got %v", gatewayStatusCode)
	}
	if upstreamStatusCode != nil {
		t.Fatalf("expected upstream_status_code null for planning row, got %v", upstreamStatusCode)
	}
	// error_text search must hit the persisted safe diagnostics.
	search := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?error_text="+url.QueryEscape(*errorDetail), nil, runtimeModelHeader(profileID))
	assertStatus(t, search, http.StatusOK)
}

func TestRuntimeRoutingScheduleClosedWritesSafeDiagnostics(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "runtime-schedule-closed-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-schedule-closed-"+suffix, "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "proxy", &strategyID)
	endpointID := harness.seedEndpoint(t, profileID, "runtime-schedule-closed-endpoint-"+suffix, "https://runtime-schedule-closed.invalid", "runtime-schedule-closed-key")
	connectionID := harness.seedConnectionWithOpenAITextCapability(t, profileID, modelConfigID, endpointID, "runtime-schedule-closed-connection-"+suffix, nil, nil, 0, runtimeStringPtr("dual_native"))
	harness.updateConnectionRoutingSchedule(t, profileID, connectionID, "UTC", [][3]int{{1 << (closedWindowISODay() % 7), 0, 1440}})
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "schedule closed diagnostics"}},
			"model":    modelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusServiceUnavailable)

	var errorCode string
	var errorDetail *string
	var upstreamStatusCode *int
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := harness.conn.QueryRow(context.Background(), `
			SELECT error_code, error_detail, upstream_status_code
			FROM request_logs
			WHERE profile_id = $1 AND model_id = $2
			ORDER BY id DESC LIMIT 1`,
			profileID, modelID,
		).Scan(&errorCode, &errorDetail, &upstreamStatusCode)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("load routing-schedule-closed request-log row: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if errorCode != "terminal_target_schedule_closed" {
		t.Fatalf("expected persisted terminal_target_schedule_closed, got %q", errorCode)
	}
	if errorDetail == nil || !strings.Contains(*errorDetail, "outside their configured routing window") {
		t.Fatalf("expected routing-window detail in request_logs, got %v", errorDetail)
	}
	if upstreamStatusCode != nil {
		t.Fatalf("expected upstream_status_code null for planning row, got %v", upstreamStatusCode)
	}
	// error_text predicate covers the stable code: the requests page search
	// for the schedule code must hit this row.
	search := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?error_text="+url.QueryEscape("terminal_target_schedule_closed"), nil, runtimeModelHeader(profileID))
	assertStatus(t, search, http.StatusOK)
}

func TestRuntimeUpstreamFailureWritesSafeDiagnostics(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "runtime-upstream-failure-" + suffix
	targetModelID := "runtime-upstream-failure-target-" + suffix
	failingUpstream := newScriptedUpstream(t, http.StatusForbidden, map[string]any{"error": "policy denied"})
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-upstream-failure-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, profileID, "runtime-upstream-failure-endpoint-"+suffix, failingUpstream.baseURL("/request-logs/upstream-failure"), "runtime-upstream-failure-key", 0)
	harness.seedConnection(t, profileID, targetModelConfigID, endpointID, "runtime-upstream-failure-connection-"+suffix, nil, nil, 0)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "upstream failure diagnostics"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusForbidden)

	var rowKind, errorSource, errorCode, failureStage string
	var upstreamStatusCode *int
	var errorDetail *string
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := harness.conn.QueryRow(context.Background(), `
			SELECT row_kind, error_source, error_code, failure_stage, upstream_status_code, error_detail
			FROM request_logs
			WHERE profile_id = $1 AND model_id = $2 AND row_kind = 'upstream'
			ORDER BY id DESC LIMIT 1`,
			profileID, publicModelID,
		).Scan(&rowKind, &errorSource, &errorCode, &failureStage, &upstreamStatusCode, &errorDetail)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("load upstream failure request-log row: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if rowKind != "upstream" {
		t.Fatalf("expected row_kind upstream, got %q", rowKind)
	}
	if errorSource != "upstream" || failureStage != "upstream_response" {
		t.Fatalf("expected upstream/upstream_response, got %q/%q", errorSource, failureStage)
	}
	if errorCode != "upstream_http_403" {
		t.Fatalf("expected stable code upstream_http_403, got %q", errorCode)
	}
	if upstreamStatusCode == nil || *upstreamStatusCode != http.StatusForbidden {
		t.Fatalf("expected upstream_status_code 403, got %v", upstreamStatusCode)
	}
	if errorDetail == nil || strings.TrimSpace(*errorDetail) == "" {
		t.Fatalf("expected non-empty safe error_detail, got %v", errorDetail)
	}
}

func TestRuntimeAuditHeaderScrubPersistsRedactedOnly(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "openai", true, false)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "sid=leak-secret-value")
		w.Header().Set("X-Upstream-Trace", "Bearer sk-upstream-leak-98765")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-audit-scrub","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(upstream.Close)

	publicModelID := "audit-scrub-public-" + randomSuffix()
	targetModelID := "audit-scrub-target-" + randomSuffix()
	harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   publicModelID,
		TargetModelID:   targetModelID,
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-audit-scrub-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "scrub me"}}, "model": publicModelID},
		map[string]string{
			"Cookie":        "session=leak-cookie-value",
			"X-Token":       "leak-x-token-value",
			"X-Trace-Id":    "token=leak-inline-value",
			"X-Project":     "prism",
			"Authorization": "Bearer sk-leak-auth-value",
			"X-Api-Key":     "leak-api-key-value",
		},
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	var requestHeaders string
	var responseHeaders *string
	if err := harness.conn.QueryRow(context.Background(), `SELECT request_headers, response_headers FROM audit_logs WHERE profile_id = $1 AND model_id = $2 ORDER BY id DESC LIMIT 1`, profileID, publicModelID).Scan(&requestHeaders, &responseHeaders); err != nil {
		t.Fatalf("load audit headers: %v", err)
	}
	if responseHeaders == nil {
		t.Fatalf("expected response headers persisted for audit row")
	}
	for _, forbidden := range []string{"leak-cookie-value", "leak-x-token-value", "sk-leak-auth-value", "leak-api-key-value", "sk-upstream-leak-98765", "sid=leak-secret-value"} {
		if strings.Contains(requestHeaders, forbidden) || strings.Contains(*responseHeaders, forbidden) {
			t.Errorf("audit headers leak forbidden value %q (request=%q response=%q)", forbidden, requestHeaders, *responseHeaders)
		}
	}
	// The persisted header block is a JSON array of {name, value} entries;
	// sensitive names must carry the exact redaction marker, safe values must
	// survive the value scrubber verbatim.
	auditHeaderEntryValues := func(raw string) map[string]string {
		t.Helper()
		var entries []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			t.Fatalf("decode persisted audit header block %q: %v", raw, err)
		}
		values := map[string]string{}
		for _, entry := range entries {
			values[entry.Name] = entry.Value
		}
		return values
	}
	requestValues := auditHeaderEntryValues(requestHeaders)
	responseValues := auditHeaderEntryValues(*responseHeaders)
	for _, name := range []string{"cookie", "x-token", "x-trace-id", "set-cookie", "x-upstream-trace"} {
		value, present := requestValues[name]
		if !present {
			value, present = responseValues[name]
		}
		if !present || value != safediag.RedactedMarker {
			t.Errorf("audit headers missing redaction sentinel for %q (request=%q response=%q)", name, requestHeaders, *responseHeaders)
		}
	}
	if requestValues["x-project"] != "prism" {
		t.Errorf("non-sensitive header x-project should be preserved verbatim: %q", requestHeaders)
	}
}

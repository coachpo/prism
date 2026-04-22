package runtime_test

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
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type requestLogContractHarness struct {
	client *http.Client
	conn   *pgx.Conn
	server *httptest.Server
	url    string
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
	if pricedFlag, ok := staleItem["priced_flag"].(bool); !ok || !pricedFlag {
		t.Fatalf("expected stale request-log row priced_flag=true, got %+v", staleItem)
	}
	if unpricedReason, ok := staleItem["unpriced_reason"]; !ok || unpricedReason != nil {
		t.Fatalf("expected stale request-log row unpriced_reason=null, got %+v", staleItem)
	}
	endpoints := payload["filter_options"].(map[string]any)["endpoints"].([]any)
	firstEndpoint := endpoints[0].(map[string]any)
	if firstEndpoint["endpoint_id"] != float64(999) || firstEndpoint["endpoint_label"] != "Endpoint 999" {
		t.Fatalf("expected stale endpoint option to prepend synthetic label, got %+v", payload)
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

func TestRequestLogDetailContract(t *testing.T) {
	harness := newRequestLogContractHarness(t)
	profileID := loadRuntimeDefaultProfileID(t, harness)
	seedRequestLogEndpoints(t, harness, profileID)
	seedRequestLogUserAgentRules(t, harness, profileID)
	seedFixtureRequestLog(t, harness, profileID)

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
	for _, absent := range []string{"model_id", "resolved_target_model_id", "api_family", "vendor_id", "vendor_key", "vendor_name"} {
		if _, ok := routing[absent]; ok {
			t.Fatalf("did not expect routing field %s in detail payload, got %+v", absent, routing)
		}
	}

	missing := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/999999", nil, runtimeModelHeader(profileID))
	assertStatus(t, missing, http.StatusNotFound)
	var missingPayload map[string]any
	decodeJSONResponse(t, missing, &missingPayload)
	if missingPayload["detail"] != "Request log not found" {
		t.Fatalf("expected scoped request-log 404 detail, got %+v", missingPayload)
	}
}

func TestRuntimeRequestLogPersistsAuditEnabledSnapshot(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	vendorID := loadVendorIDByKey(t, harness.conn, "openai")
	if _, err := harness.conn.Exec(context.Background(), `UPDATE vendors SET audit_enabled = TRUE WHERE id = $1`, vendorID); err != nil {
		t.Fatalf("enable audit for runtime vendor: %v", err)
	}
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
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET vendor_id = $1 WHERE profile_id = $2 AND model_id = ANY($3::text[])`, vendorID, profileID, []string{route.PublicModelID, route.TargetModelID}); err != nil {
		t.Fatalf("attach runtime models to audit-enabled vendor: %v", err)
	}

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "persist audit snapshot"}}, "model": route.PublicModelID},
		nil,
	)
	assertStatus(t, response, http.StatusOK)

	var auditEnabledAtRequest bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT audit_enabled_at_request FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&auditEnabledAtRequest); err != nil {
		t.Fatalf("load persisted runtime audit snapshot: %v", err)
	}
	if !auditEnabledAtRequest {
		t.Fatalf("expected runtime request log to persist audit_enabled_at_request=true for audit-enabled executed vendor")
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
}

func TestRuntimeRequestLogPersistsOptionalPricingWithoutOptionalUsageCounters(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	var reportCurrencyCode string
	var reportCurrencySymbol string
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT report_currency_code, report_currency_symbol FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`,
		profileID,
	).Scan(&reportCurrencyCode, &reportCurrencySymbol); err != nil {
		t.Fatalf("load runtime report currency snapshot: %v", err)
	}

	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-runtime-pricing-optional-usage-" + suffix,
		"object": "chat.completion",
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 6,
			"total_tokens":      16,
		},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "priced-optional-usage-public-" + suffix,
		TargetModelID:   "priced-optional-usage-target-" + suffix,
		EndpointBaseURL: upstream.baseURL("/request-logs/pricing/optional-usage"),
		EndpointAPIKey:  "runtime-priced-optional-usage-key",
	})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-pricing-template-"+suffix, reportCurrencyCode, "2", "5", "11", "13", "17")
	attachRuntimeConnectionPricingTemplate(t, harness.conn, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "price omitted optional counters"}},
			"model":    route.PublicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected optional-pricing runtime request to hit upstream exactly once, got %d", got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)

	want := runtimePersistedPricingRow{
		AttemptMetric:                     1,
		BillableFlag:                      sql.NullBool{Bool: true, Valid: true},
		PricedFlag:                        sql.NullBool{Bool: true, Valid: true},
		InputTokens:                       sql.NullInt64{Int64: 10, Valid: true},
		OutputTokens:                      sql.NullInt64{Int64: 6, Valid: true},
		TotalTokens:                       sql.NullInt64{Int64: 16, Valid: true},
		InputCostMicros:                   sql.NullInt64{Int64: 20, Valid: true},
		OutputCostMicros:                  sql.NullInt64{Int64: 30, Valid: true},
		CacheReadInputCostMicros:          sql.NullInt64{Int64: 0, Valid: true},
		CacheCreationInputCostMicros:      sql.NullInt64{Int64: 0, Valid: true},
		ReasoningCostMicros:               sql.NullInt64{Int64: 0, Valid: true},
		TotalCostOriginalMicros:           sql.NullInt64{Int64: 50, Valid: true},
		TotalCostUserCurrencyMicros:       sql.NullInt64{Int64: 50, Valid: true},
		CurrencyCodeOriginal:              sql.NullString{String: reportCurrencyCode, Valid: true},
		ReportCurrencyCode:                sql.NullString{String: reportCurrencyCode, Valid: true},
		ReportCurrencySymbol:              sql.NullString{String: reportCurrencySymbol, Valid: true},
		FXRateUsed:                        sql.NullString{String: "1", Valid: true},
		FXRateSource:                      sql.NullString{String: "DEFAULT_1_TO_1", Valid: true},
		PricingSnapshotUnit:               sql.NullString{String: "PER_1M", Valid: true},
		PricingSnapshotInput:              sql.NullString{String: "2", Valid: true},
		PricingSnapshotOutput:             sql.NullString{String: "5", Valid: true},
		PricingSnapshotCacheReadInput:     sql.NullString{String: "11", Valid: true},
		PricingSnapshotCacheCreationInput: sql.NullString{String: "13", Valid: true},
		PricingSnapshotReasoning:          sql.NullString{String: "17", Valid: true},
		PricingConfigVersionUsed:          sql.NullInt64{Int64: 1, Valid: true},
	}
	requestLogRow := loadLatestRuntimeRequestLogPricingRow(t, harness.conn, profileID)
	if requestLogRow != want {
		t.Fatalf("expected winning request_logs pricing row %+v, got %+v", want, requestLogRow)
	}
	usageEventRow := loadLatestRuntimeUsageEventPricingRow(t, harness.conn, profileID)
	if usageEventRow != want {
		t.Fatalf("expected usage_request_events pricing row %+v, got %+v", want, usageEventRow)
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
	primaryEndpointID := harness.seedEndpoint(t, profileID, "runtime-request-log-primary-endpoint-"+suffix, primaryUpstream.baseURL("/request-logs/fill-first/primary"), "runtime-request-log-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "runtime-request-log-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/request-logs/fill-first/secondary"), "runtime-request-log-secondary-key", 1)
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
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20,\"output_tokens_details\":{\"reasoning_tokens\":0}}}}\n\n")
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

	var isStream bool
	var inputTokens sql.NullInt64
	var outputTokens sql.NullInt64
	var totalTokens sql.NullInt64
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT is_stream, input_tokens, output_tokens, total_tokens FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&isStream, &inputTokens, &outputTokens, &totalTokens); err != nil {
		t.Fatalf("load persisted streamed request log usage: %v", err)
	}
	if !isStream {
		t.Fatalf("expected streamed responses request to persist is_stream=true")
	}
	if !inputTokens.Valid || inputTokens.Int64 != 7 || !outputTokens.Valid || outputTokens.Int64 != 13 || !totalTokens.Valid || totalTokens.Int64 != 20 {
		t.Fatalf("expected streamed responses usage to persist 7/13/20, got input=%+v output=%+v total=%+v", inputTokens, outputTokens, totalTokens)
	}
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
}

func TestRuntimeRequestLogPersistsStreamedAnthropicUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":7,\"output_tokens\":1}}}\n\n")
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
	assertLatestRequestLogUsage(t, harness.conn, profileID, true, 7, 13, 20)
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
}

func TestRuntimeRequestLogPersistsStreamedGeminiUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好\"}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好！有什么我可以帮你的吗？\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":20}}\n\n")
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
		fmt.Sprintf("/v1beta/models/%s:generateContent?alt=sse", route.PublicModelID),
		map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": "你好"}},
			}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertLatestRequestLogUsage(t, harness.conn, profileID, false, 7, 13, 20)
}

func assertLatestRequestLogUsage(t *testing.T, conn *pgx.Conn, profileID int, expectStream bool, wantInput int64, wantOutput int64, wantTotal int64) {
	t.Helper()
	var isStream bool
	var inputTokens sql.NullInt64
	var outputTokens sql.NullInt64
	var totalTokens sql.NullInt64
	if err := conn.QueryRow(
		context.Background(),
		`SELECT is_stream, input_tokens, output_tokens, total_tokens FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&isStream, &inputTokens, &outputTokens, &totalTokens); err != nil {
		t.Fatalf("load latest streamed request log usage: %v", err)
	}
	if isStream != expectStream {
		t.Fatalf("expected request log is_stream=%t, got %t", expectStream, isStream)
	}
	if !inputTokens.Valid || inputTokens.Int64 != wantInput || !outputTokens.Valid || outputTokens.Int64 != wantOutput || !totalTokens.Valid || totalTokens.Int64 != wantTotal {
		t.Fatalf("expected streamed usage %d/%d/%d, got input=%+v output=%+v total=%+v", wantInput, wantOutput, wantTotal, inputTokens, outputTokens, totalTokens)
	}
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

func loadLatestRuntimeIngressRequestID(t *testing.T, conn *pgx.Conn, profileID int) string {
	t.Helper()
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

func loadLatestRuntimeRequestLogPricingRow(t *testing.T, conn *pgx.Conn, profileID int) runtimePersistedPricingRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var row runtimePersistedPricingRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT attempt_number, billable_flag, priced_flag, unpriced_reason, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(
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
		t.Fatalf("load latest runtime request-log pricing row: %v", err)
	}
	return row
}

func loadLatestRuntimeUsageEventPricingRow(t *testing.T, conn *pgx.Conn, profileID int) runtimePersistedPricingRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var row runtimePersistedPricingRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT attempt_count, billable_flag, priced_flag, unpriced_reason, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(
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
		t.Fatalf("load latest runtime usage-event pricing row: %v", err)
	}
	return row
}

func assertLatestRuntimeWinningRequestLogFields(t *testing.T, conn *pgx.Conn, profileID int, wantAttemptNumber int, wantUsageValid bool, wantTimingValid bool, wantStream bool) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	var attemptNumber int
	var isStream bool
	var inputTokens sql.NullInt64
	var outputTokens sql.NullInt64
	var totalTokens sql.NullInt64
	var ttftMs sql.NullInt64
	var completionDurationMs sql.NullInt64
	if err := conn.QueryRow(
		context.Background(),
		`SELECT attempt_number, is_stream, input_tokens, output_tokens, total_tokens, ttft_ms, completion_duration_ms FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&attemptNumber, &isStream, &inputTokens, &outputTokens, &totalTokens, &ttftMs, &completionDurationMs); err != nil {
		t.Fatalf("load runtime winning request-log row: %v", err)
	}
	if attemptNumber != wantAttemptNumber {
		t.Fatalf("expected final request-log attempt_number=%d, got %d", wantAttemptNumber, attemptNumber)
	}
	if isStream != wantStream {
		t.Fatalf("expected final request-log is_stream=%t, got %t", wantStream, isStream)
	}
	if inputTokens.Valid != wantUsageValid || outputTokens.Valid != wantUsageValid || totalTokens.Valid != wantUsageValid {
		t.Fatalf("expected final request-log usage validity=%t, got input=%+v output=%+v total=%+v", wantUsageValid, inputTokens, outputTokens, totalTokens)
	}
	if ttftMs.Valid != wantTimingValid || completionDurationMs.Valid != wantTimingValid {
		t.Fatalf("expected final request-log timing validity=%t, got ttft=%+v completion=%+v", wantTimingValid, ttftMs, completionDurationMs)
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

	response := harness.requestJSON(t, http.MethodGet, "/api/audit/logs?request_log_id=104&limit=20", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusConflict)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if payload["detail"] != "Audit capture unavailable for this request" {
		t.Fatalf("expected disabled request snapshot to reject audit list, got %+v", payload)
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
	if fixtureItem["model_label"] != "GPT-4o Proxy" || fixtureItem["resolved_target_model_label"] != "GPT-4o Native" || fixtureItem["is_proxy_origin"] != true {
		t.Fatalf("expected fixture request log to use current model display-name enrichment, got %+v", fixtureItem)
	}
	proxyOnlyItem := itemsByID[103]
	if proxyOnlyItem["model_label"] != "GPT-4o Proxy" || proxyOnlyItem["resolved_target_model_label"] != nil || proxyOnlyItem["is_proxy_origin"] != true {
		t.Fatalf("expected current proxy model row to remain proxy-origin without resolved-target divergence, got %+v", proxyOnlyItem)
	}

	detailResponse := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/101", nil, runtimeModelHeader(profileID))
	assertStatus(t, detailResponse, http.StatusOK)
	decodeJSONResponse(t, detailResponse, &payload)
	summary := asMapRuntime(t, payload["summary"])
	if summary["model_label"] != "GPT-4o Proxy" || summary["resolved_target_model_label"] != "GPT-4o Native" || summary["is_proxy_origin"] != true {
		t.Fatalf("expected detail summary to use current model enrichment, got %+v", summary)
	}
	routing := asMapRuntime(t, payload["routing"])
	for _, absent := range []string{"model_id", "resolved_target_model_id", "api_family", "vendor_id", "vendor_key", "vendor_name"} {
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
	t.Cleanup(func() { conn.Close(context.Background()) })
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
	return &requestLogContractHarness{client: client, conn: conn, server: server, url: server.URL}
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

func seedRequestLogEndpoints(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoints (id, profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7), ($8, $2, $9, $10, $11, $12, $7, $7)`, 12, profileID, "Primary OpenAI", "https://api.openai.com", "fixture-key", 0, now, 13, "Primary Anthropic", "https://api.anthropic.com", "fixture-key", 1); err != nil {
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

func seedFixtureRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	createdAt := time.Date(2026, 4, 18, 12, 34, 56, 0, time.UTC)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, vendor_id, vendor_key, vendor_name, resolved_target_model_id, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, reasoning_tokens, input_cost_micros, output_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_reasoning, cache_read_input_tokens, cache_creation_input_tokens, cache_read_input_cost_micros, cache_creation_input_cost_micros, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_config_version_used, request_path, error_detail, endpoint_description, created_at, caller_user_agent, upstream_user_agent, completion_duration_ms, ttft_ms, audit_enabled_at_request) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, NULL, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, NULL, $24, $25, $26, $27, $28, $29, $30, $31, $32, NULL, NULL, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, NULL, $45, $46, $47, $48, $49, $50, $51)`, 101, profileID, "gpt-4o", "openai", 1, "openai", "OpenAI", "gpt-4o-native", 12, 34, "ingress_req_42", 2, "req_upstream_abc123", "https://api.openai.com", 200, 1234, false, 15, 42, 57, true, true, true, 0, 500, 750, 0, 1250, 1250, "USD", "USD", "$", "1M tokens", "2.500000", "10.000000", "0.000000", 0, 0, 0, 0, "1.250000", "0.000000", 1, "/v1/chat/completions", "Primary production key", createdAt, "codex/1.0", "OpenAI/Python 1.0", 914, 320, false); err != nil {
		t.Fatalf("seed fixture request log: %v", err)
	}
}

func seedSimpleRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, endpointID int, endpointBaseURL *string, createdAt time.Time, auditEnabledAtRequest bool) {
	t.Helper()
	var historicalBaseURL any
	if endpointBaseURL != nil {
		historicalBaseURL = *endpointBaseURL
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, created_at, audit_enabled_at_request) VALUES ($1, $2, 'gpt-4o', 'openai', $3, NULL, $4, 1, $5, 200, 120, FALSE, TRUE, TRUE, TRUE, '/v1/chat/completions', $6, $7)`, id, profileID, endpointID, fmt.Sprintf("req-%d", id), historicalBaseURL, createdAt, auditEnabledAtRequest); err != nil {
		t.Fatalf("seed simple request log %d: %v", id, err)
	}
}

func seedRequestLogModels(t *testing.T, harness *requestLogContractHarness, profileID int) {
	t.Helper()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	openAIVendorID := loadRequestLogVendorIDByKey(t, harness, "openai")
	autoRecovery := `{"enabled":true,"check_interval_seconds":300,"max_retries":3}`
	var strategyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at) VALUES ($1, $2, 'legacy', 'round-robin', $3::jsonb, NULL, $4, $4) RETURNING id`, profileID, "request-log-current-models", autoRecovery, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert current request-log strategy: %v", err)
	}
	var nativeModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, 'openai', $3, $4, 'native', $5, TRUE, $6, $6) RETURNING id`, profileID, openAIVendorID, "gpt-4o-native", "GPT-4o Native", strategyID, now).Scan(&nativeModelID); err != nil {
		t.Fatalf("insert current native request-log model: %v", err)
	}
	var proxyModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, 'openai', $3, $4, 'proxy', NULL, TRUE, $5, $5) RETURNING id`, profileID, openAIVendorID, "gpt-4o", "GPT-4o Proxy", now).Scan(&proxyModelID); err != nil {
		t.Fatalf("insert current proxy request-log model: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position) VALUES ($1, $2, 0)`, proxyModelID, nativeModelID); err != nil {
		t.Fatalf("insert request-log proxy target: %v", err)
	}
}

func loadRequestLogVendorIDByKey(t *testing.T, harness *requestLogContractHarness, key string) int {
	t.Helper()
	return loadVendorIDByKey(t, harness.conn, key)
}

func loadVendorIDByKey(t *testing.T, conn *pgx.Conn, key string) int {
	t.Helper()
	var vendorID int
	if err := conn.QueryRow(context.Background(), `SELECT id FROM vendors WHERE key = $1 LIMIT 1`, key).Scan(&vendorID); err != nil {
		t.Fatalf("load vendor %q for request-log contract test: %v", key, err)
	}
	return vendorID
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

func attachRuntimeConnectionPricingTemplate(t *testing.T, conn *pgx.Conn, connectionID int, pricingTemplateID int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := conn.Exec(
		context.Background(),
		`UPDATE connections SET pricing_template_id = $2, updated_at = $3 WHERE id = $1`,
		connectionID,
		pricingTemplateID,
		now,
	); err != nil {
		t.Fatalf("attach pricing template %d to runtime connection %d: %v", pricingTemplateID, connectionID, err)
	}
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

func readRuntimeResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return string(raw)
}

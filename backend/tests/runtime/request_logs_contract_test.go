package runtime_test

import (
	"bytes"
	"context"
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

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
	seedSimpleRequestLog(t, harness, profileID, 102, 999, nil, time.Date(2026, 4, 18, 12, 20, 0, 0, time.UTC))

	response := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?limit=50&offset=0", nil, runtimeModelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	expected := loadRequestFixture(t, "request-log-list.json")
	if payload["limit"] != expected["limit"] || payload["offset"] != expected["offset"] {
		t.Fatalf("expected request-log envelope pagination to match fixture, got %+v", payload)
	}
	if !jsonBytesEqual(t, payload["filter_options"], expected["filter_options"]) {
		t.Fatalf("expected request-log filter options to match fixture, got %+v", payload["filter_options"])
	}
	items := payload["items"].([]any)
	expectedItem := expected["items"].([]any)[0].(map[string]any)
	if len(items) == 0 {
		t.Fatalf("expected at least one request-log row, got %+v", payload)
	}
	item := items[0].(map[string]any)
	for _, key := range []string{"id", "created_at", "model_id", "resolved_target_model_id", "api_family", "vendor_id", "vendor_key", "vendor_name", "endpoint_id", "endpoint_label", "connection_id", "status_code", "response_time_ms", "ttft_ms", "completion_duration_ms", "is_stream", "output_tokens", "total_tokens", "total_cost_user_currency_micros", "report_currency_symbol", "caller_client_display", "upstream_client_display"} {
		if item[key] != expectedItem[key] {
			t.Fatalf("expected request-log field %s to match fixture, got %+v", key, item)
		}
	}
	if _, ok := item["user_agent_overridden"].(bool); !ok {
		t.Fatalf("expected request-log user_agent_overridden boolean field, got %+v", item)
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
	summary := asMapRuntime(t, payload["summary"])
	expectedSummary := expected["summary"].(map[string]any)
	for _, key := range []string{"id", "created_at", "model_id", "resolved_target_model_id", "api_family", "vendor_id", "vendor_key", "vendor_name", "status_code", "response_time_ms", "ttft_ms", "completion_duration_ms", "is_stream"} {
		if summary[key] != expectedSummary[key] {
			t.Fatalf("expected request-log detail summary field %s to match fixture, got %+v", key, summary)
		}
	}
	request := asMapRuntime(t, payload["request"])
	if request["request_path"] != "/v1/chat/completions" || request["ingress_request_id"] != "ingress_req_42" || request["attempt_number"] != float64(2) || request["provider_correlation_id"] != "req_upstream_abc123" || request["caller_client_display"] != "Codex" || request["upstream_client_display"] != "OpenAI SDK" {
		t.Fatalf("unexpected request-log detail request payload: %+v", request)
	}
	if _, ok := request["user_agent_overridden"].(bool); !ok {
		t.Fatalf("expected request-log detail user_agent_overridden boolean field, got %+v", request)
	}
	routing := asMapRuntime(t, payload["routing"])
	if routing["profile_id"] != float64(profileID) || routing["endpoint_id"] != float64(12) || routing["connection_id"] != float64(34) || routing["audit_enabled_at_request"] != false {
		t.Fatalf("unexpected request-log detail routing payload: %+v", routing)
	}
	usage := asMapRuntime(t, payload["usage"])
	if usage["input_tokens"] != float64(15) || usage["output_tokens"] != float64(42) || usage["total_tokens"] != float64(57) || usage["success_flag"] != true {
		t.Fatalf("unexpected request-log detail usage payload: %+v", usage)
	}
	costing := asMapRuntime(t, payload["costing"])
	if costing["total_cost_user_currency_micros"] != float64(1250) || costing["report_currency_symbol"] != "$" {
		t.Fatalf("unexpected request-log detail costing payload: %+v", costing)
	}
	pricing := asMapRuntime(t, payload["pricing"])
	if pricing["pricing_snapshot_unit"] != "1M tokens" || pricing["pricing_snapshot_input"] != "2.500000" || pricing["pricing_config_version_used"] != float64(1) {
		t.Fatalf("unexpected request-log detail pricing payload: %+v", pricing)
	}

	missing := harness.requestJSON(t, http.MethodGet, "/api/stats/requests/999999", nil, runtimeModelHeader(profileID))
	assertStatus(t, missing, http.StatusNotFound)
	var missingPayload map[string]any
	decodeJSONResponse(t, missing, &missingPayload)
	if missingPayload["detail"] != "Request log not found" {
		t.Fatalf("expected scoped request-log 404 detail, got %+v", missingPayload)
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
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "s15-runtime-test", StatsService: statsService})
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

func seedSimpleRequestLog(t *testing.T, harness *requestLogContractHarness, profileID int, id int, endpointID int, endpointBaseURL *string, createdAt time.Time) {
	t.Helper()
	var historicalBaseURL any
	if endpointBaseURL != nil {
		historicalBaseURL = *endpointBaseURL
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (id, profile_id, model_id, api_family, endpoint_id, connection_id, ingress_request_id, attempt_number, endpoint_base_url, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, request_path, created_at) VALUES ($1, $2, 'gpt-4o', 'openai', $3, NULL, $4, 1, $5, 200, 120, FALSE, TRUE, TRUE, TRUE, '/v1/chat/completions', $6)`, id, profileID, endpointID, fmt.Sprintf("req-%d", id), historicalBaseURL, createdAt); err != nil {
		t.Fatalf("seed simple request log %d: %v", id, err)
	}
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

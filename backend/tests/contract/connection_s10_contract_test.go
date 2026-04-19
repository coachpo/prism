package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

type scriptedUpstreamResponse struct {
	statusCode int
	payload    any
}

type recordedUpstreamRequest struct {
	Path    string
	Headers http.Header
	Body    map[string]any
}

type scriptedUpstream struct {
	mu        sync.Mutex
	responses []scriptedUpstreamResponse
	requests  []recordedUpstreamRequest
	server    *httptest.Server
}

type connectionHealthSnapshot struct {
	HealthStatus    string
	HealthDetail    *string
	LastHealthCheck *time.Time
}

func TestConnectionHealthChecks(t *testing.T) {
	upstream := newScriptedUpstream(t)
	upstream.queueJSON(http.StatusOK, map[string]any{"ok": true})
	upstream.queueJSON(http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "invalid api key"}})
	checkedAt := time.Date(2026, time.April, 19, 12, 34, 56, 0, time.UTC)
	harness := newConnectionHealthContractHarness(t, upstream, checkedAt)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S10 Health Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-health-model", nil, "native", &strategyID, true)
	endpointID := insertContractEndpointWithBaseURL(t, harness, defaultProfileID, "Health Check Endpoint", upstream.server.URL, "health-policy-endpoint-key", 0)
	connectionID := insertContractConnectionWithState(t, harness, defaultProfileID, modelConfigID, endpointID, nil, 0, true, map[string]string{"X-Allow-Smoke": "still-here", "X-Correlation-ID": "blocked-after-merge"}, nil, "unknown", nil, nil)

	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/connections/%d/health-check", connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["health_status"] != "unhealthy" {
		t.Fatalf("expected persisted health check payload for connection %d, got %+v", connectionID, payload)
	}
	if payload["detail"] != "Authentication failed (HTTP 401): invalid api key" {
		t.Fatalf("expected upstream auth failure detail, got %+v", payload)
	}
	if jsonInt(t, payload["response_time_ms"]) <= 0 {
		t.Fatalf("expected positive response_time_ms, got %+v", payload)
	}
	responseCheckedAt := parseRFC3339Time(t, payload["checked_at"].(string))
	if !responseCheckedAt.Equal(checkedAt) {
		t.Fatalf("expected checked_at %s, got %s", checkedAt, responseCheckedAt)
	}
	snapshot := loadConnectionHealthSnapshot(t, harness, connectionID)
	if snapshot.HealthStatus != "unhealthy" || snapshot.HealthDetail == nil || *snapshot.HealthDetail != "Authentication failed (HTTP 401): invalid api key" || snapshot.LastHealthCheck == nil || !snapshot.LastHealthCheck.Equal(checkedAt) {
		t.Fatalf("expected persisted health state, got %+v", snapshot)
	}
	requests := upstream.snapshotRequests()
	if len(requests) != 2 {
		t.Fatalf("expected two upstream health probes, got %+v", requests)
	}

	for index, request := range requests {
		if request.Path != "/v1/responses" {
			t.Fatalf("expected health probe %d to use /v1/responses, got %+v", index, request)
		}
		if request.Headers.Get("Authorization") != "Bearer health-policy-endpoint-key" {
			t.Fatalf("expected Authorization header on probe %d, got %+v", index, request.Headers)
		}
		if request.Headers.Get("X-Allow-Smoke") != "still-here" {
			t.Fatalf("expected allowed custom header on probe %d, got %+v", index, request.Headers)
		}
		if request.Headers.Get("X-Correlation-Id") != "" {
			t.Fatalf("expected blocked correlation header on probe %d, got %+v", index, request.Headers)
		}
		if request.Body["model"] != "s10-health-model" {
			t.Fatalf("expected model id in probe body %d, got %+v", index, request.Body)
		}
	}
}

func TestConnectionHealthCheckPreview(t *testing.T) {
	upstream := newScriptedUpstream(t)
	upstream.queueJSON(http.StatusOK, map[string]any{"ok": true})
	upstream.queueJSON(http.StatusTooManyRequests, map[string]any{"error": map[string]any{"message": "rate limited"}})
	checkedAt := time.Date(2026, time.April, 19, 15, 0, 0, 0, time.UTC)
	harness := newConnectionHealthContractHarness(t, upstream, checkedAt)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S10 Preview Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-preview-model", nil, "native", &strategyID, true)
	existingEndpointID := insertContractEndpointWithBaseURL(t, harness, defaultProfileID, "Existing Preview Endpoint", "https://existing-preview.invalid", "existing-preview-key", 0)
	oldCheckedAt := checkedAt.Add(-2 * time.Hour)
	connectionID := insertContractConnectionWithState(t, harness, defaultProfileID, modelConfigID, existingEndpointID, nil, 0, true, nil, nil, "healthy", stringPtr("existing health state"), &oldCheckedAt)
	endpointsBefore := countProfileEndpoints(t, harness, defaultProfileID)

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/health-check-preview", modelConfigID), map[string]any{
		"endpoint_create": map[string]any{"name": "Preview Inline Endpoint", "base_url": upstream.server.URL, "api_key": "preview-inline-key"},
		"custom_headers":  map[string]string{"X-Allow-Preview": "preview-ok", "X-Request-ID": "blocked-request-id"},
	}, modelHeader(defaultProfileID))
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["health_status"] != "healthy" || previewPayload["detail"] != "Rate limited (connection works)" {
		t.Fatalf("expected preview health payload to stay healthy on 429, got %+v", previewPayload)
	}
	if jsonInt(t, previewPayload["response_time_ms"]) <= 0 {
		t.Fatalf("expected positive preview response_time_ms, got %+v", previewPayload)
	}
	if _, ok := previewPayload["connection_id"]; ok {
		t.Fatalf("did not expect preview payload to include connection_id, got %+v", previewPayload)
	}

	endpointsAfter := countProfileEndpoints(t, harness, defaultProfileID)
	if endpointsAfter != endpointsBefore {
		t.Fatalf("expected preview to avoid persisting inline endpoints, got before=%d after=%d", endpointsBefore, endpointsAfter)
	}
	snapshot := loadConnectionHealthSnapshot(t, harness, connectionID)
	if snapshot.HealthStatus != "healthy" || snapshot.HealthDetail == nil || *snapshot.HealthDetail != "existing health state" || snapshot.LastHealthCheck == nil || !snapshot.LastHealthCheck.Equal(oldCheckedAt) {
		t.Fatalf("expected preview to avoid mutating persisted connection state, got %+v", snapshot)
	}
	requests := upstream.snapshotRequests()
	if len(requests) != 2 {
		t.Fatalf("expected preview route to perform the same two-step probe, got %+v", requests)
	}
	if requests[0].Headers.Get("Authorization") != "Bearer preview-inline-key" || requests[0].Headers.Get("X-Allow-Preview") != "preview-ok" || requests[0].Headers.Get("X-Request-Id") != "" {
		t.Fatalf("expected preview headers to use inline endpoint secret and block request-id, got %+v", requests[0].Headers)
	}
}

func TestConnectionPricingTemplates(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := insertContractProfile(t, harness, "S10 Other Pricing Profile")
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S10 Pricing Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-pricing-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Pricing Assignment Endpoint", 0)
	connectionID := insertContractConnectionWithState(t, harness, defaultProfileID, modelConfigID, endpointID, nil, 0, true, nil, nil, "unknown", nil, nil)

	pricingTemplateID := insertContractPricingTemplate(t, harness, defaultProfileID, "S10 Assigned Template")
	otherProfileTemplateID := insertContractPricingTemplate(t, harness, otherProfileID, "S10 Other Profile Template")

	assignResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/connections/%d/pricing-template", connectionID), map[string]any{"pricing_template_id": pricingTemplateID}, modelHeader(defaultProfileID))
	assertStatus(t, assignResponse, http.StatusOK)
	var assignedPayload map[string]any
	decodeJSONResponse(t, assignResponse, &assignedPayload)
	if jsonInt(t, assignedPayload["pricing_template_id"]) != pricingTemplateID || jsonInt(t, asMap(t, assignedPayload["pricing_template"])["id"]) != pricingTemplateID {
		t.Fatalf("expected pricing template assignment payload, got %+v", assignedPayload)
	}

	clearResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/connections/%d/pricing-template", connectionID), map[string]any{"pricing_template_id": nil}, modelHeader(defaultProfileID))
	assertStatus(t, clearResponse, http.StatusOK)
	var clearedPayload map[string]any
	decodeJSONResponse(t, clearResponse, &clearedPayload)
	if clearedPayload["pricing_template_id"] != nil || clearedPayload["pricing_template"] != nil {
		t.Fatalf("expected clear pricing template assignment payload, got %+v", clearedPayload)
	}

	wrongProfileResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/connections/%d/pricing-template", connectionID), map[string]any{"pricing_template_id": otherProfileTemplateID}, modelHeader(defaultProfileID))
	assertErrorResponse(t, wrongProfileResponse, http.StatusNotFound, "Pricing template not found")
}

func TestPricingTemplateManagementCRUD(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := insertContractProfile(t, harness, "S10 Other CRUD Profile")
	existingTemplateID := insertContractPricingTemplate(t, harness, defaultProfileID, "S10 Existing Template")
	_ = insertContractPricingTemplate(t, harness, otherProfileID, "S10 Other Profile Template")

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/pricing-templates", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed) != 1 || jsonInt(t, listed[0]["id"]) != existingTemplateID || listed[0]["name"] != "S10 Existing Template" {
		t.Fatalf("expected pricing template list for effective profile only, got %+v", listed)
	}

	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/pricing-templates/%d", existingTemplateID), nil, modelHeader(defaultProfileID))
	assertStatus(t, getResponse, http.StatusOK)
	var existing map[string]any
	decodeJSONResponse(t, getResponse, &existing)
	if jsonInt(t, existing["profile_id"]) != defaultProfileID || existing["pricing_unit"] != "PER_1M" {
		t.Fatalf("expected pricing template payload for profile %d, got %+v", defaultProfileID, existing)
	}

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{"name": "S10 Created Template", "description": "created via contract", "pricing_currency_code": "usd", "input_price": "1.25", "output_price": "2.50", "cached_input_price": "0.10", "cache_creation_price": nil, "reasoning_price": nil}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	createdID := jsonInt(t, created["id"])
	if created["name"] != "S10 Created Template" || created["pricing_currency_code"] != "USD" || created["version"] != float64(1) {
		t.Fatalf("expected created pricing template payload, got %+v", created)
	}

	updateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": created["updated_at"], "description": "updated via contract", "input_price": "3.75", "reasoning_price": "0.50"}, modelHeader(defaultProfileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updateResponse, &updated)
	if updated["description"] != "updated via contract" || updated["input_price"] != "3.75" || updated["reasoning_price"] != "0.50" || updated["version"] != float64(2) {
		t.Fatalf("expected updated pricing template payload, got %+v", updated)
	}

	staleUpdate := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": created["updated_at"], "name": "Stale Update"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, staleUpdate, http.StatusConflict, "Pricing template has changed. Please refresh and retry.")

	deleteResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/pricing-templates/%d", createdID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteResponse, http.StatusOK)
	var deleted map[string]any
	decodeJSONResponse(t, deleteResponse, &deleted)
	if deleted["deleted"] != true {
		t.Fatalf("expected deleted response payload, got %+v", deleted)
	}

	missingAfterDelete := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/pricing-templates/%d", createdID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, missingAfterDelete, http.StatusNotFound, "Pricing template not found")
}

func TestPricingTemplateDeleteConflict(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S10 Pricing Delete Conflict Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-pricing-delete-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Pricing Delete Conflict Endpoint", 0)
	templateID := insertContractPricingTemplate(t, harness, defaultProfileID, "S10 Delete Conflict Template")
	_ = insertContractConnectionWithState(t, harness, defaultProfileID, modelConfigID, endpointID, &templateID, 0, true, nil, stringPtr("Conflict Connection"), "healthy", nil, nil)

	blockedDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/pricing-templates/%d", templateID), nil, modelHeader(defaultProfileID))
	assertStatus(t, blockedDelete, http.StatusConflict)
	var payload map[string]any
	decodeJSONResponse(t, blockedDelete, &payload)
	detail := asMap(t, payload["detail"])
	if detail["message"] != "Cannot delete pricing template that is referenced by connections" {
		t.Fatalf("expected delete conflict message, got %+v", payload)
	}
	connections := detail["connections"].([]any)
	if len(connections) != 1 || jsonInt(t, asMap(t, connections[0])["model_config_id"]) != modelConfigID || jsonInt(t, asMap(t, connections[0])["endpoint_id"]) != endpointID {
		t.Fatalf("expected delete conflict dependency payload, got %+v", payload)
	}
}

func TestPricingTemplateConnections(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)

	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S10 Usage Strategy")
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-usage-model-a", nil, "native", &strategyID, true)
	modelBID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-usage-model-b", nil, "native", &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "Usage Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "Usage Endpoint B", 1)
	templateID := insertContractPricingTemplate(t, harness, defaultProfileID, "S10 Usage Template")
	connectionAID := insertContractConnectionWithState(t, harness, defaultProfileID, modelAID, endpointAID, &templateID, 0, true, nil, stringPtr("Template Connection A"), "healthy", nil, nil)
	connectionBID := insertContractConnectionWithState(t, harness, defaultProfileID, modelBID, endpointBID, &templateID, 1, true, nil, stringPtr("Template Connection B"), "healthy", nil, nil)
	_ = insertContractConnectionWithState(t, harness, defaultProfileID, modelBID, endpointBID, nil, 2, true, nil, stringPtr("Unassigned Connection"), "healthy", nil, nil)

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/pricing-templates/%d/connections", templateID), nil, modelHeader(defaultProfileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if jsonInt(t, payload["template_id"]) != templateID {
		t.Fatalf("expected template_id %d, got %+v", templateID, payload)
	}
	items := payload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected two usage rows, got %+v", payload)
	}

	first := asMap(t, items[0])
	second := asMap(t, items[1])
	assertPricingTemplateUsageItem(t, first, connectionAID, "Template Connection A", modelAID, "s10-usage-model-a", endpointAID, "Usage Endpoint A")
	assertPricingTemplateUsageItem(t, second, connectionBID, "Template Connection B", modelBID, "s10-usage-model-b", endpointBID, "Usage Endpoint B")
}

func newScriptedUpstream(t *testing.T) *scriptedUpstream {
	t.Helper()
	upstream := &scriptedUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.handle))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (s *scriptedUpstream) queueJSON(statusCode int, payload any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, scriptedUpstreamResponse{statusCode: statusCode, payload: payload})
}

func (s *scriptedUpstream) snapshotRequests() []recordedUpstreamRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]recordedUpstreamRequest, len(s.requests))
	copy(items, s.requests)
	return items
}

func (s *scriptedUpstream) handle(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()
	s.mu.Lock()
	s.requests = append(s.requests, recordedUpstreamRequest{Path: r.URL.Path, Headers: r.Header.Clone(), Body: body})
	response := scriptedUpstreamResponse{statusCode: http.StatusOK, payload: map[string]any{"ok": true}}
	if len(s.responses) > 0 {
		response = s.responses[0]
		s.responses = s.responses[1:]
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.statusCode)
	_ = json.NewEncoder(w).Encode(response.payload)
}

func newConnectionHealthContractHarness(t *testing.T, upstream *scriptedUpstream, checkedAt time.Time) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "connection_health_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "connection-health-contract-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}
	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "connection-health-contract-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool, Now: func() time.Time { return checkedAt }, HTTPClient: upstream.server.Client()})
	if err != nil {
		t.Fatalf("build connections service: %v", err)
	}
	t.Cleanup(connectionsService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "connection-health-contract-test", ConnectionsService: connectionsService})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

	client := server.Client()
	client.Jar = jar
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: nil, url: server.URL}
}

func insertContractEndpointWithBaseURL(t *testing.T, harness *contractHarness, profileID int, name string, baseURL string, apiKey string, position int) int {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, profileID, name, baseURL, apiKey, position, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", name, err)
	}
	return endpointID
}

func insertContractConnectionWithState(t *testing.T, harness *contractHarness, profileID int, modelConfigID int, endpointID int, pricingTemplateID *int, priority int, isActive bool, customHeaders map[string]string, name *string, healthStatus string, healthDetail *string, lastHealthCheck *time.Time) int {
	t.Helper()
	now := time.Now().UTC()
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, model_config_id, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, $4, NULL, NULL, NULL, NULL, $5, $6, $7, NULL, $8, $9, $10, $11, $12, $12) RETURNING id`, profileID, modelConfigID, endpointID, nullableTestInt(pricingTemplateID), isActive, priority, name, headersValue, healthStatus, healthDetail, lastHealthCheck, now).Scan(&connectionID); err != nil {

		t.Fatalf("insert contract connection for model %d endpoint %d: %v", modelConfigID, endpointID, err)
	}
	return connectionID
}

func loadConnectionHealthSnapshot(t *testing.T, harness *contractHarness, connectionID int) connectionHealthSnapshot {
	t.Helper()
	var detail sqlNullString
	var checkedAt sqlNullTime
	snapshot := connectionHealthSnapshot{}
	if err := harness.conn.QueryRow(context.Background(), `SELECT health_status, health_detail, last_health_check FROM connections WHERE id = $1`, connectionID).Scan(&snapshot.HealthStatus, &detail, &checkedAt); err != nil {
		t.Fatalf("load connection health snapshot for %d: %v", connectionID, err)
	}
	snapshot.HealthDetail = detail.ptr()
	snapshot.LastHealthCheck = checkedAt.ptr()
	return snapshot
}

func countProfileEndpoints(t *testing.T, harness *contractHarness, profileID int) int {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM endpoints WHERE profile_id = $1`, profileID).Scan(&count); err != nil {
		t.Fatalf("count endpoints for profile %d: %v", profileID, err)
	}

	return count
}

func parseRFC3339Time(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse RFC3339 time %q: %v", value, err)
	}
	return parsed.UTC()
}

func assertPricingTemplateUsageItem(t *testing.T, payload map[string]any, connectionID int, connectionName string, modelConfigID int, modelID string, endpointID int, endpointName string) {
	t.Helper()
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["connection_name"] != connectionName || jsonInt(t, payload["model_config_id"]) != modelConfigID || payload["model_id"] != modelID || jsonInt(t, payload["endpoint_id"]) != endpointID || payload["endpoint_name"] != endpointName {
		t.Fatalf("unexpected pricing template usage row: %+v", payload)
	}
}

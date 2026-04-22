package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestModelCRUD(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Legacy Strategy")

	missingHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, nil)
	assertErrorResponse(t, missingHeader, http.StatusBadRequest, fmt.Sprintf("%s header is required", profiledomain.ProfileIDHeader))

	nativeCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":               vendorID,
			"api_family":              "openai",
			"model_id":                "s8-native-model",
			"display_name":            "S8 Native Model",
			"model_type":              "native",
			"loadbalance_strategy_id": strategyID,
			"is_enabled":              true,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, nativeCreate, http.StatusCreated)
	var nativePayload map[string]any
	decodeJSONResponse(t, nativeCreate, &nativePayload)
	nativeID := jsonInt(t, nativePayload["id"])
	if nativePayload["model_id"] != "s8-native-model" || nativePayload["model_type"] != "native" || nativePayload["is_enabled"] != true {
		t.Fatalf("expected native create payload, got %+v", nativePayload)
	}
	if got := asMap(t, nativePayload["loadbalance_strategy"])["strategy_type"]; got != "legacy" {
		t.Fatalf("expected legacy strategy summary on native create, got %+v", nativePayload)
	}
	if connections, ok := nativePayload["connections"].([]any); !ok || len(connections) != 0 {
		t.Fatalf("expected native create response to include empty connections, got %+v", nativePayload)
	}

	duplicateCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":               vendorID,
			"api_family":              "openai",
			"model_id":                "s8-native-model",
			"model_type":              "native",
			"loadbalance_strategy_id": strategyID,
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, duplicateCreate, http.StatusConflict, "Model ID 's8-native-model' already exists")

	proxyCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":    vendorID,
			"api_family":   "openai",
			"model_id":     "s8-proxy-model",
			"display_name": "S8 Proxy Model",
			"model_type":   "proxy",
			"proxy_targets": []map[string]any{{
				"target_model_id": "s8-native-model",
				"position":        0,
			}},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, proxyCreate, http.StatusCreated)
	var proxyPayload map[string]any
	decodeJSONResponse(t, proxyCreate, &proxyPayload)
	proxyID := jsonInt(t, proxyPayload["id"])
	assertProxyTargets(t, proxyPayload, []string{"s8-native-model"})

	selfTargetUpdate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", proxyID),
		map[string]any{"proxy_targets": []map[string]any{{"target_model_id": "s8-proxy-model", "position": 0}}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, selfTargetUpdate, http.StatusBadRequest, "Proxy model cannot target itself")

	chainedProxyCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":     vendorID,
			"api_family":    "openai",
			"model_id":      "s8-chained-proxy",
			"model_type":    "proxy",
			"proxy_targets": []map[string]any{{"target_model_id": "s8-proxy-model", "position": 0}},
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, chainedProxyCreate, http.StatusBadRequest, "Target model 's8-proxy-model' is not a native model (chained proxies not allowed)")

	missingTargetCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":     vendorID,
			"api_family":    "openai",
			"model_id":      "s8-missing-target-proxy",
			"model_type":    "proxy",
			"proxy_targets": []map[string]any{{"target_model_id": "does-not-exist", "position": 0}},
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, missingTargetCreate, http.StatusBadRequest, "Target model 'does-not-exist' not found")

	wrongFamilyProxy := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":     vendorID,
			"api_family":    "anthropic",
			"model_id":      "s8-family-mismatch-proxy",
			"model_type":    "proxy",
			"proxy_targets": []map[string]any{{"target_model_id": "s8-native-model", "position": 0}},
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, wrongFamilyProxy, http.StatusBadRequest, "Proxy targets must use the same api_family as the proxy model")

	firstEndpointID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 Detail Endpoint A", 0)
	secondEndpointID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 Detail Endpoint B", 1)
	modelInsertConnection(t, harness, defaultProfileID, nativeID, firstEndpointID, 5, true, nil)
	secondConnectionID := modelInsertConnection(t, harness, defaultProfileID, nativeID, secondEndpointID, 1, false, map[string]string{"x-test": "1"})
	modelInsertFXRateSetting(t, harness, defaultProfileID, "s8-native-model", firstEndpointID, "1.250000")

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d", nativeID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detailPayload map[string]any
	decodeJSONResponse(t, detailResponse, &detailPayload)
	assertConnectionOrder(t, detailPayload, secondConnectionID)

	convertNativeWithReferrer := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", nativeID),
		map[string]any{
			"model_type":    "proxy",
			"proxy_targets": []map[string]any{{"target_model_id": "s8-native-model", "position": 0}},
		},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, convertNativeWithReferrer, http.StatusBadRequest, "Cannot convert native model to proxy while proxy models [s8-proxy-model] point to it")

	renameNative := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", nativeID),
		map[string]any{
			"model_id":                "s8-native-model-renamed",
			"display_name":            nil,
			"loadbalance_strategy_id": strategyID,
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, renameNative, http.StatusOK)
	var renamedPayload map[string]any
	decodeJSONResponse(t, renameNative, &renamedPayload)
	if renamedPayload["model_id"] != "s8-native-model-renamed" || renamedPayload["display_name"] != "s8-native-model-renamed" {
		t.Fatalf("expected rename payload to resync display_name, got %+v", renamedPayload)
	}
	assertProxyTargetModelID(t, harness, proxyID, defaultProfileID, "s8-native-model-renamed")
	if got := modelLoadFXRateModelID(t, harness, defaultProfileID, firstEndpointID); got != "s8-native-model-renamed" {
		t.Fatalf("expected fx-rate setting model_id to sync on rename, got %q", got)
	}

	deleteNativeWhileReferenced := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", nativeID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, deleteNativeWhileReferenced, http.StatusBadRequest, "Cannot delete: proxy models [s8-proxy-model] point to this model")

	deleteProxy := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", proxyID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteProxy, http.StatusOK)
	deleteNative := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", nativeID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteNative, http.StatusOK)

	listAfterDelete := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, modelHeader(defaultProfileID))
	assertStatus(t, listAfterDelete, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listAfterDelete, &listed)
	if modelListContainsID(listed, nativeID) || modelListContainsID(listed, proxyID) {
		t.Fatalf("expected deleted models to be absent from list, got %+v", listed)
	}
}

func TestModelsByEndpoints(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Helper Strategy")
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "helper-a", stringPtr("Helper A"), "native", &strategyID, true)
	modelBID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "helper-b", nil, "native", &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 Helper Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 Helper Endpoint B", 1)
	modelInsertConnection(t, harness, defaultProfileID, modelBID, endpointAID, 2, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelBID, endpointBID, 1, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointBID, 0, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointBID, 3, false, nil)
	modelInsertRequestLog(t, harness, defaultProfileID, "helper-a", "openai", 200, "helper-a-success")
	modelInsertRequestLog(t, harness, defaultProfileID, "helper-a", "openai", 500, "helper-a-failure")

	helperResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models/by-endpoints",
		map[string]any{"endpoint_ids": []int{endpointBID, 999999, endpointAID}},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, helperResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, helperResponse, &payload)
	items := payload["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("expected helper route to preserve three endpoint envelopes, got %+v", payload)
	}
	assertEndpointModelsBatchItem(t, asMap(t, items[0]), endpointBID, []string{"helper-a", "helper-b"})
	assertEndpointModelsBatchItem(t, asMap(t, items[1]), 999999, nil)
	assertEndpointModelsBatchItem(t, asMap(t, items[2]), endpointAID, []string{"helper-b"})

	firstModels := asMap(t, items[0])["models"].([]any)
	assertModelListItemCounts(t, asMap(t, firstModels[0]), modelAID, 2, 1, 2, 50)
	assertModelListItemCounts(t, asMap(t, firstModels[1]), modelBID, 1, 1, 0, nil)

	emptyInput := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models/by-endpoints",
		map[string]any{"endpoint_ids": []int{}},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, emptyInput, http.StatusOK)
	var emptyPayload map[string]any
	decodeJSONResponse(t, emptyInput, &emptyPayload)
	if items, ok := emptyPayload["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("expected empty endpoint_ids to return an empty items list, got %+v", emptyPayload)
	}
}

func TestModelsByEndpoint(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Endpoint Strategy")
	modelZID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "z-model", nil, "native", &strategyID, true)
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "a-model", stringPtr("Model A"), "native", &strategyID, false)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 By Endpoint", 0)
	modelInsertConnection(t, harness, defaultProfileID, modelZID, endpointID, 5, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointID, 1, false, nil)

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/by-endpoint/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, response, http.StatusOK)
	var models []map[string]any
	decodeJSONResponse(t, response, &models)
	if len(models) != 2 {
		t.Fatalf("expected by-endpoint helper to return two models, got %+v", models)
	}
	if models[0]["model_id"] != "a-model" || models[1]["model_id"] != "z-model" {
		t.Fatalf("expected by-endpoint helper to sort by model_id, got %+v", models)
	}
	assertModelListItemCounts(t, models[0], modelAID, 1, 0, 0, nil)
	assertModelListItemCounts(t, models[1], modelZID, 1, 1, 0, nil)
}

func newModelContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "model_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "model-contract-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "model-contract-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build models service: %v", err)
	}
	t.Cleanup(modelsService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "model-contract-test", ModelsService: modelsService})
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

func modelHeader(profileID int) map[string]string {
	return map[string]string{profiledomain.ProfileIDHeader: fmt.Sprintf("%d", profileID)}
}

func modelLoadDefaultProfileID(t *testing.T, harness *contractHarness) int {
	t.Helper()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func modelLoadVendorIDByKey(t *testing.T, harness *contractHarness, key string) int {
	t.Helper()
	var vendorID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM vendors WHERE key = $1 LIMIT 1`, key).Scan(&vendorID); err != nil {
		t.Fatalf("load vendor %q: %v", key, err)
	}
	return vendorID
}

func modelInsertLoadbalanceStrategy(t *testing.T, harness *contractHarness, profileID int, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var strategyID int
	autoRecovery := mustModelJSON(t, map[string]any{
		"mode":         "enabled",
		"status_codes": []int{408, 409, 425, 429, 500, 502, 503, 504},
		"cooldown":     map[string]any{"base_seconds": 60, "failure_threshold": 2, "backoff_multiplier": 2.0, "max_cooldown_seconds": 900, "jitter_ratio": 0.2},
		"ban":          map[string]any{"mode": "off"},
	})
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8) RETURNING id`, profileID, name, "legacy", "single", autoRecovery, nil, now, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
}

func modelInsertModel(t *testing.T, harness *contractHarness, profileID int, vendorID *int, apiFamily string, modelID string, displayName *string, modelType string, strategyID *int, isEnabled bool) int {
	t.Helper()
	now := time.Now().UTC()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`, profileID, nullableTestInt(vendorID), apiFamily, modelID, displayName, modelType, nullableTestInt(strategyID), isEnabled, now, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model %q: %v", modelID, err)
	}
	return modelConfigID
}

func modelInsertEndpoint(t *testing.T, harness *contractHarness, profileID int, name string, position int) int {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, profileID, name, fmt.Sprintf("https://%s.invalid", strings.ToLower(strings.ReplaceAll(name, " ", "-"))), "plain-api-key", position, now, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", name, err)
	}
	return endpointID
}

func modelInsertConnection(t *testing.T, harness *contractHarness, profileID int, modelConfigID int, endpointID int, priority int, isActive bool, customHeaders map[string]string) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, model_config_id, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`, profileID, modelConfigID, endpointID, isActive, priority, fmt.Sprintf("conn-%d", priority), nil, headersValue, "healthy", nil, nil, now, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection for model %d endpoint %d: %v", modelConfigID, endpointID, err)
	}
	return connectionID
}

func modelInsertRequestLog(t *testing.T, harness *contractHarness, profileID int, modelID string, apiFamily string, statusCode int, requestID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, status_code, response_time_ms, is_stream, success_flag, billable_flag, priced_flag, unpriced_reason, request_path, created_at) VALUES ($1, $2, $3, $4, NULL, $5, $6, FALSE, $7, $8, $9, NULL, $10, $11)`, profileID, modelID, apiFamily, requestID, statusCode, 120, statusCode >= 200 && statusCode < 300, true, true, "/v1/chat/completions", now); err != nil {
		t.Fatalf("insert request log %q: %v", requestID, err)
	}
}

func modelInsertFXRateSetting(t *testing.T, harness *contractHarness, profileID int, modelID string, endpointID int, fxRate string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`, profileID, modelID, endpointID, fxRate, now, now); err != nil {
		t.Fatalf("insert endpoint fx rate setting: %v", err)
	}
}

func modelLoadFXRateModelID(t *testing.T, harness *contractHarness, profileID int, endpointID int) string {
	t.Helper()
	var modelID string
	if err := harness.conn.QueryRow(context.Background(), `SELECT model_id FROM endpoint_fx_rate_settings WHERE profile_id = $1 AND endpoint_id = $2 LIMIT 1`, profileID, endpointID).Scan(&modelID); err != nil {
		t.Fatalf("load endpoint fx rate setting: %v", err)
	}
	return modelID
}

func assertProxyTargets(t *testing.T, payload map[string]any, want []string) {
	t.Helper()
	proxyTargets, ok := payload["proxy_targets"].([]any)
	if !ok || len(proxyTargets) != len(want) {
		t.Fatalf("expected proxy_targets %v, got %+v", want, payload)
	}
	for index, raw := range proxyTargets {
		proxyTarget := asMap(t, raw)
		if proxyTarget["target_model_id"] != want[index] {
			t.Fatalf("expected proxy target %q at index %d, got %+v", want[index], index, proxyTarget)
		}
	}
}

func assertProxyTargetModelID(t *testing.T, harness *contractHarness, modelConfigID int, profileID int, wantTargetModelID string) {
	t.Helper()
	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d", modelConfigID), nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	assertProxyTargets(t, payload, []string{wantTargetModelID})
}

func assertConnectionOrder(t *testing.T, payload map[string]any, expectedFirstConnectionID int) {
	t.Helper()
	connections, ok := payload["connections"].([]any)
	if !ok || len(connections) < 2 {
		t.Fatalf("expected model detail response with ordered connections, got %+v", payload)
	}
	firstConnection := asMap(t, connections[0])
	if jsonInt(t, firstConnection["id"]) != expectedFirstConnectionID || jsonInt(t, firstConnection["priority"]) != 1 {
		t.Fatalf("expected first connection %d with priority ordering, got %+v", expectedFirstConnectionID, firstConnection)
	}
}

func assertEndpointModelsBatchItem(t *testing.T, item map[string]any, wantEndpointID int, wantModelIDs []string) {
	t.Helper()
	if jsonInt(t, item["endpoint_id"]) != wantEndpointID {
		t.Fatalf("expected endpoint_id %d, got %+v", wantEndpointID, item)
	}
	models, ok := item["models"].([]any)
	if !ok {
		t.Fatalf("expected models list, got %+v", item)
	}
	if len(models) != len(wantModelIDs) {
		t.Fatalf("expected model ids %v, got %+v", wantModelIDs, item)
	}
	for index, wantModelID := range wantModelIDs {
		model := asMap(t, models[index])
		if model["model_id"] != wantModelID {
			t.Fatalf("expected model_id %q at index %d, got %+v", wantModelID, index, model)
		}
	}
}

func assertModelListItemCounts(t *testing.T, item map[string]any, wantID int, wantConnectionCount int, wantActiveCount int, wantHealthTotal int, wantHealthRate any) {
	t.Helper()
	if jsonInt(t, item["id"]) != wantID || jsonInt(t, item["connection_count"]) != wantConnectionCount || jsonInt(t, item["active_connection_count"]) != wantActiveCount || jsonInt(t, item["health_total_requests"]) != wantHealthTotal {
		t.Fatalf("unexpected model helper row counts: %+v", item)
	}
	if wantHealthRate == nil {
		if item["health_success_rate"] != nil {
			t.Fatalf("expected nil health_success_rate, got %+v", item)
		}
		return
	}
	if item["health_success_rate"] != float64(wantHealthRate.(int)) {
		t.Fatalf("expected health_success_rate %v, got %+v", wantHealthRate, item)
	}
}

func modelListContainsID(items []map[string]any, wantID int) bool {
	for _, item := range items {
		value, ok := item["id"].(float64)
		if ok && int(value) == wantID {
			return true
		}
	}
	return false
}

func nullableTestInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func mustModelJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(raw)
}

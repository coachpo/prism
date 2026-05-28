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
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestModelCRUD(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Access Strategy")
	targetModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s8-target-model", stringPtr("S8 Target Model"), &strategyID, true)
	otherFamilyModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "anthropic", "s8-anthropic-target", stringPtr("S8 Anthropic Target"), &strategyID, true)
	_ = otherFamilyModelID

	missingHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, nil)
	assertErrorResponseCode(t, missingHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderMissing, fmt.Sprintf("%s header is required", profiledomain.ProfileIDHeader))

	legacyShape := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "legacy-shape", "model_type": "native", "loadbalance_strategy_id": strategyID},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, legacyShape, http.StatusBadRequest)

	missingStrategy := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "missing-strategy", "access_targets": []map[string]any{modelAccessTarget("model", "s8-target-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, missingStrategy, http.StatusBadRequest, "loadbalance_strategy_id is required")

	createSelfTarget := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "s8-create-self-target", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "s8-create-self-target", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, createSelfTarget, http.StatusBadRequest, "Model access target cannot target itself")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{
			"vendor_id":               vendorID,
			"api_family":              "openai",
			"model_id":                "s8-access-model",
			"display_name":            "S8 Access Model",
			"loadbalance_strategy_id": strategyID,
			"is_enabled":              true,
			"access_targets":          []map[string]any{modelAccessTarget("model", "s8-target-model", nil, 0, true)},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createPayload map[string]any
	decodeJSONResponse(t, createResponse, &createPayload)
	sourceModelConfigID := jsonInt(t, createPayload["id"])
	assertNoLegacyModelFields(t, createPayload)
	assertAccessTargets(t, createPayload, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s8-target-model", Position: 0, IsEnabled: true}})
	if got := asMap(t, createPayload["loadbalance_strategy"])["legacy_strategy_type"]; got != "single" {
		t.Fatalf("expected legacy strategy summary on model create, got %+v", createPayload)
	}

	duplicateCreate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/models",
		map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "s8-access-model", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "s8-target-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, duplicateCreate, http.StatusConflict, "Model ID 's8-access-model' already exists")

	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 Access Endpoint", 0)
	connectionID := modelInsertStandaloneConnection(t, harness, defaultProfileID, "openai", endpointID, 3, true, nil)
	updateResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{
			"display_name": nil,
			"access_targets": []map[string]any{
				modelAccessTarget("connection", "", &connectionID, 0, true),
				modelAccessTarget("model", "s8-target-model", nil, 1, false),
			},
		},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, updateResponse, http.StatusOK)
	var updatePayload map[string]any
	decodeJSONResponse(t, updateResponse, &updatePayload)
	assertNoLegacyModelFields(t, updatePayload)
	assertAccessTargets(t, updatePayload, []expectedAccessTarget{{TargetType: "connection", ConnectionID: connectionID, Position: 0, IsEnabled: true}, {TargetType: "model", TargetModelID: "s8-target-model", Position: 1, IsEnabled: false}})
	if updatePayload["display_name"] != "s8-access-model" {
		t.Fatalf("expected nil display_name update to reset to model_id, got %+v", updatePayload)
	}

	zeroEnabledTargets := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-target-model", nil, 0, false)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, zeroEnabledTargets, http.StatusBadRequest, "enabled models must include at least one enabled access target")

	wrongFamilyTarget := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-anthropic-target", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, wrongFamilyTarget, http.StatusBadRequest, "Model access targets must use the same api_family as the source model")

	selfTarget := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-access-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, selfTarget, http.StatusBadRequest, "Model access target cannot target itself")

	deleteReferencedTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", targetModelID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, deleteReferencedTarget, http.StatusConflict, "Cannot delete: models [s8-access-model] target this model")

	cycleUpdate := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", targetModelID),
		map[string]any{"access_targets": []map[string]any{modelAccessTarget("model", "s8-access-model", nil, 0, true)}},
		modelHeader(defaultProfileID),
	)
	assertErrorResponse(t, cycleUpdate, http.StatusBadRequest, "access_targets cannot introduce a model target cycle")

	renameResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPut,
		fmt.Sprintf("/api/models/%d", sourceModelConfigID),
		map[string]any{"model_id": "s8-access-model-renamed", "display_name": nil},
		modelHeader(defaultProfileID),
	)
	assertStatus(t, renameResponse, http.StatusOK)
	var renamedPayload map[string]any
	decodeJSONResponse(t, renameResponse, &renamedPayload)
	if renamedPayload["model_id"] != "s8-access-model-renamed" || renamedPayload["display_name"] != "s8-access-model-renamed" {
		t.Fatalf("expected rename payload to resync display_name, got %+v", renamedPayload)
	}

	deleteSource := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", sourceModelConfigID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteSource, http.StatusOK)
	assertNoSourceAccessTargets(t, harness, sourceModelConfigID)
	deleteTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", targetModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteTarget, http.StatusOK)
}

func TestDeleteReferencedModel(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Delete Referenced Strategy")
	targetID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "delete-target", nil, &strategyID, true)
	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "delete-source", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "delete-target", nil, 0, true)}}, modelHeader(profileID))
	assertStatus(t, createResponse, http.StatusCreated)
	deleteReferenced := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", targetID), nil, modelHeader(profileID))
	assertErrorResponse(t, deleteReferenced, http.StatusConflict, "Cannot delete: models [delete-source] target this model")
}

func TestWrongFamilyTarget(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Wrong Family Strategy")
	modelInsertModel(t, harness, profileID, &vendorID, "anthropic", "wrong-family-target", nil, &strategyID, true)
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "wrong-family-source", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("model", "wrong-family-target", nil, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, response, http.StatusBadRequest, "Model access targets must use the same api_family as the source model")
}

func TestWrongProfileTarget(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := modelInsertProfile(t, harness, "Other Profile")
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Wrong Profile Strategy")
	otherEndpointID := modelInsertEndpoint(t, harness, otherProfileID, "Other Profile Endpoint", 0)
	otherConnectionID := modelInsertStandaloneConnection(t, harness, otherProfileID, "openai", otherEndpointID, 0, true, nil)
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models", map[string]any{"vendor_id": vendorID, "api_family": "openai", "model_id": "wrong-profile-source", "loadbalance_strategy_id": strategyID, "access_targets": []map[string]any{modelAccessTarget("connection", "", &otherConnectionID, 0, true)}}, modelHeader(profileID))
	assertErrorResponse(t, response, http.StatusBadRequest, fmt.Sprintf("Target connection %d not found", otherConnectionID))
}

func TestModelsByEndpoints(t *testing.T) {
	harness := newModelContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S8 Helper Strategy")
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "helper-a", stringPtr("Helper A"), &strategyID, true)
	modelBID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "helper-b", nil, &strategyID, true)
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
	modelZID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "z-model", nil, &strategyID, true)
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "a-model", stringPtr("Model A"), &strategyID, true)
	facadeID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "facade-model", stringPtr("Facade Model"), &strategyID, true)
	disabledFacadeID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "disabled-facade", nil, &strategyID, false)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "S8 By Endpoint", 0)
	modelInsertConnection(t, harness, defaultProfileID, modelZID, endpointID, 5, true, nil)
	modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointID, 1, false, nil)
	modelInsertModelTarget(t, harness, defaultProfileID, facadeID, modelZID, 0, true)
	modelInsertModelTarget(t, harness, defaultProfileID, disabledFacadeID, modelZID, 0, true)

	response := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/by-endpoint/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, response, http.StatusOK)
	var models []map[string]any
	decodeJSONResponse(t, response, &models)
	if len(models) != 3 {
		t.Fatalf("expected by-endpoint helper to return three enabled reachable models, got %+v", models)
	}
	if models[0]["model_id"] != "a-model" || models[1]["model_id"] != "facade-model" || models[2]["model_id"] != "z-model" {
		t.Fatalf("expected by-endpoint helper to sort by model_id, got %+v", models)
	}
	assertModelListItemCounts(t, models[0], modelAID, 1, 0, 0, nil)
	assertModelListItemCounts(t, models[1], facadeID, 1, 1, 0, nil)
	assertModelListItemCounts(t, models[2], modelZID, 1, 1, 0, nil)
}

func TestEndpointBatchDedupe(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Endpoint Batch Dedupe Strategy")
	terminalID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "batch-terminal", nil, &strategyID, true)
	facadeID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "batch-facade", nil, &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Endpoint Batch Dedupe", 0)
	connectionID := modelInsertConnection(t, harness, profileID, terminalID, endpointID, 0, true, nil)
	modelInsertModelTarget(t, harness, profileID, facadeID, terminalID, 0, true)
	modelInsertConnectionTarget(t, harness, profileID, facadeID, connectionID, 1, true)

	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models/by-endpoints", map[string]any{"endpoint_ids": []int{endpointID}}, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items := payload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one endpoint envelope, got %+v", payload)
	}
	item := asMap(t, items[0])
	assertEndpointModelsBatchItem(t, item, endpointID, []string{"batch-facade", "batch-terminal"})
	models := item["models"].([]any)
	assertModelListItemCounts(t, asMap(t, models[0]), facadeID, 1, 1, 0, nil)
	assertModelListItemCounts(t, asMap(t, models[1]), terminalID, 1, 1, 0, nil)
}

func TestReachableConnectionCount(t *testing.T) {
	harness := newModelContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Reachable Count Strategy")
	terminalID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "count-terminal", nil, &strategyID, true)
	facadeID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "count-facade", nil, &strategyID, true)
	recursiveFacadeID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "count-recursive-facade", nil, &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, profileID, "Reachable Count Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, profileID, "Reachable Count Endpoint B", 1)
	modelInsertConnection(t, harness, profileID, terminalID, endpointAID, 0, true, nil)
	modelInsertConnection(t, harness, profileID, terminalID, endpointAID, 1, false, nil)
	modelInsertConnection(t, harness, profileID, terminalID, endpointBID, 2, true, nil)
	inactiveTargetConnectionID := modelInsertStandaloneConnection(t, harness, profileID, "openai", endpointAID, 3, true, nil)
	modelInsertModelTarget(t, harness, profileID, facadeID, terminalID, 0, true)
	modelInsertConnectionTarget(t, harness, profileID, facadeID, inactiveTargetConnectionID, 1, false)
	modelInsertModelTarget(t, harness, profileID, recursiveFacadeID, facadeID, 0, true)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var models []any
	decodeJSONResponse(t, response, &models)
	assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-terminal"), terminalID, 3, 2, 0, nil)
	assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-facade"), facadeID, 3, 2, 0, nil)
	assertModelListItemCounts(t, findModelListItemByModelID(t, models, "count-recursive-facade"), recursiveFacadeID, 3, 2, 0, nil)
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

func modelInsertProfile(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, NULL, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
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
	if err := harness.conn.QueryRow(
		context.Background(),
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, retry_max_attempts,
			ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, 'single', $3::integer[], 'off', 60000, 2.0, 0.2, 900000, 3, 0, $4, $4)
		 RETURNING id`,
		profileID,
		name,
		[]int32{403, 422, 429, 500, 502, 503, 504, 529},
		now,
	).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
}

func modelInsertModel(t *testing.T, harness *contractHarness, profileID int, vendorID *int, apiFamily string, modelID string, displayName *string, args ...any) int {
	t.Helper()
	strategyID, isEnabled := parseModelInsertArgs(t, args)
	now := time.Now().UTC()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, profileID, nullableTestInt(vendorID), apiFamily, modelID, displayName, nullableTestInt(strategyID), isEnabled, now, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model %q: %v", modelID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return modelConfigID
}

func parseModelInsertArgs(t *testing.T, args []any) (*int, bool) {
	t.Helper()
	if len(args) == 2 {
		return modelStrategyArg(t, args[0]), args[1].(bool)
	}
	if len(args) == 3 {
		return modelStrategyArg(t, args[1]), args[2].(bool)
	}
	t.Fatalf("unexpected modelInsertModel args: %d", len(args))
	return nil, false
}

func modelStrategyArg(t *testing.T, value any) *int {
	t.Helper()
	if value == nil {
		return nil
	}
	strategyID, ok := value.(*int)
	if !ok {
		t.Fatalf("expected *int strategy arg, got %T", value)
	}
	return strategyID
}

func modelInsertEndpoint(t *testing.T, harness *contractHarness, profileID int, name string, position int) int {
	t.Helper()
	now := time.Now().UTC()
	var endpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, profileID, name, fmt.Sprintf("https://%s.invalid", strings.ToLower(strings.ReplaceAll(name, " ", "-"))), "plain-api-key", position, now, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", name, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return endpointID
}

func modelInsertConnection(t *testing.T, harness *contractHarness, profileID int, modelConfigID int, endpointID int, priority int, isActive bool, customHeaders map[string]string) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	var apiFamily string
	if err := harness.conn.QueryRow(context.Background(), `SELECT api_family FROM model_configs WHERE id = $1 AND profile_id = $2`, modelConfigID, profileID).Scan(&apiFamily); err != nil {
		t.Fatalf("load model %d api family: %v", modelConfigID, err)
	}
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`, profileID, apiFamily, endpointID, isActive, priority, fmt.Sprintf("conn-%d", priority), nil, headersValue, "healthy", nil, nil, now, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection for model %d endpoint %d: %v", modelConfigID, endpointID, err)
	}
	modelInsertConnectionTarget(t, harness, profileID, modelConfigID, connectionID, priority, true)
	return connectionID
}

func modelInsertModelTarget(t *testing.T, harness *contractHarness, profileID int, sourceModelConfigID int, targetModelConfigID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, 1, $4, $5, $6, $6)`, profileID, sourceModelConfigID, targetModelConfigID, position, isEnabled, now); err != nil {
		t.Fatalf("insert model target for model %d target %d: %v", sourceModelConfigID, targetModelConfigID, err)
	}
}

func modelInsertConnectionTarget(t *testing.T, harness *contractHarness, profileID int, sourceModelConfigID int, connectionID int, position int, isEnabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, connectionID, position, isEnabled, now); err != nil {
		t.Fatalf("insert connection target for model %d connection %d: %v", sourceModelConfigID, connectionID, err)
	}
}

func modelInsertStandaloneConnection(t *testing.T, harness *contractHarness, profileID int, apiFamily string, endpointID int, priority int, isActive bool, customHeaders map[string]string) int {
	t.Helper()
	now := time.Now().UTC()
	var connectionID int
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, NULL, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`, profileID, apiFamily, endpointID, isActive, priority, fmt.Sprintf("conn-%d", priority), nil, headersValue, "healthy", nil, nil, now, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert standalone connection endpoint %d: %v", endpointID, err)
	}
	return connectionID
}

func modelInsertRequestLog(t *testing.T, harness *contractHarness, profileID int, modelID string, apiFamily string, statusCode int, requestID string) {
	t.Helper()
	now := time.Now().UTC()
	ensureContractTestLogPartitions(t, harness, contractTestLogPartitionFor("request_logs", now))
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

type expectedAccessTarget struct {
	TargetType    string
	TargetModelID string
	ConnectionID  int
	Position      int
	IsEnabled     bool
}

func modelAccessTarget(targetType string, targetModelID string, connectionID *int, position int, isEnabled bool) map[string]any {
	item := map[string]any{"target_type": targetType, "position": position, "is_enabled": isEnabled}
	if targetType == "model" {
		item["target_model_id"] = targetModelID
	}
	if targetType == "connection" && connectionID != nil {
		item["connection_id"] = *connectionID
	}
	return item
}

func assertNoLegacyModelFields(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, key := range []string{"model_type", "proxy_selection_strategy", "proxy_targets", "connections"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("expected payload to omit legacy field %s, got %+v", key, payload)
		}
	}
}

func assertAccessTargets(t *testing.T, payload map[string]any, want []expectedAccessTarget) {
	t.Helper()
	items, ok := payload["access_targets"].([]any)
	if !ok || len(items) != len(want) {
		t.Fatalf("expected access_targets %v, got %+v", want, payload)
	}
	for index, raw := range items {
		item := asMap(t, raw)
		expected := want[index]
		if item["target_type"] != expected.TargetType || jsonInt(t, item["position"]) != expected.Position || item["is_enabled"] != expected.IsEnabled {
			t.Fatalf("unexpected access target at %d: got %+v want %+v", index, item, expected)
		}
		if expected.TargetType == "model" {
			if item["target_model_id"] != expected.TargetModelID {
				t.Fatalf("expected model target %q at %d, got %+v", expected.TargetModelID, index, item)
			}
			if targetModel := asMap(t, item["target_model"]); targetModel["model_id"] != expected.TargetModelID {
				t.Fatalf("expected hydrated target_model %q at %d, got %+v", expected.TargetModelID, index, item)
			}
			continue
		}
		if jsonInt(t, item["connection_id"]) != expected.ConnectionID {
			t.Fatalf("expected connection target %d at %d, got %+v", expected.ConnectionID, index, item)
		}
		connection := asMap(t, item["connection"])
		if jsonInt(t, connection["id"]) != expected.ConnectionID {
			t.Fatalf("expected hydrated connection %d at %d, got %+v", expected.ConnectionID, index, item)
		}
	}
}

func modelLoadConnectionTargetID(t *testing.T, harness *contractHarness, sourceModelConfigID int, connectionID int) int {
	t.Helper()
	var targetID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_access_targets WHERE source_model_config_id = $1 AND target_connection_id = $2 LIMIT 1`, sourceModelConfigID, connectionID).Scan(&targetID); err != nil {
		t.Fatalf("load connection target %d for model %d: %v", connectionID, sourceModelConfigID, err)
	}
	return targetID
}

func assertNoSourceAccessTargets(t *testing.T, harness *contractHarness, sourceModelConfigID int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1`, sourceModelConfigID).Scan(&count); err != nil {
		t.Fatalf("count source access targets for model %d: %v", sourceModelConfigID, err)
	}
	if count != 0 {
		t.Fatalf("expected source access targets for model %d to be deleted, got %d", sourceModelConfigID, count)
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

func findModelListItemByModelID(t *testing.T, items []any, modelID string) map[string]any {
	t.Helper()
	for _, raw := range items {
		item := asMap(t, raw)
		if item["model_id"] == modelID {
			return item
		}
	}
	t.Fatalf("expected model_id %q in %+v", modelID, items)
	return nil
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

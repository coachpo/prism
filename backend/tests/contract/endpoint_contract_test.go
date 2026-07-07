package contract_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestEndpointCRUD(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	missingHeader := harness.requestJSON(t, harness.client, http.MethodGet, "/api/endpoints", nil, nil)
	assertErrorResponseCode(t, missingHeader, http.StatusBadRequest, profiledomain.ScopeErrorCodeHeaderMissing, fmt.Sprintf("%s header is required", profiledomain.ProfileIDHeader))

	createPrimary := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Primary OpenAI", "base_url": "https://api.openai.com/", "api_key": "sk-primary"}, modelHeader(defaultProfileID))
	assertStatus(t, createPrimary, http.StatusCreated)
	var primary map[string]any
	decodeJSONResponse(t, createPrimary, &primary)
	primaryID := jsonInt(t, primary["id"])
	if primary["name"] != "Primary OpenAI" || primary["base_url"] != "https://api.openai.com" {
		t.Fatalf("expected normalized endpoint create payload, got %+v", primary)
	}
	if primary["has_api_key"] != true || primary["masked_api_key"] != "********" || jsonInt(t, primary["position"]) != 0 {
		t.Fatalf("expected endpoint secret summary on create, got %+v", primary)
	}

	createDependent := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Dependent Endpoint", "base_url": "https://dependent.invalid", "api_key": "sk-dependent"}, modelHeader(defaultProfileID))
	assertStatus(t, createDependent, http.StatusCreated)
	var dependent map[string]any
	decodeJSONResponse(t, createDependent, &dependent)
	dependentID := jsonInt(t, dependent["id"])

	createSpare := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Spare Endpoint", "base_url": "https://spare.invalid", "api_key": "sk-spare"}, modelHeader(defaultProfileID))
	assertStatus(t, createSpare, http.StatusCreated)
	var spare map[string]any
	decodeJSONResponse(t, createSpare, &spare)
	spareID := jsonInt(t, spare["id"])

	updatePrimary := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/endpoints/%d", primaryID), map[string]any{"name": "Primary Updated", "base_url": "https://api.openai.com/v2/", "api_key": "sk-updated"}, modelHeader(defaultProfileID))
	assertStatus(t, updatePrimary, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updatePrimary, &updated)
	if updated["name"] != "Primary Updated" || updated["base_url"] != "https://api.openai.com/v2" {
		t.Fatalf("expected endpoint update payload to persist changes, got %+v", updated)
	}

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Endpoint CRUD Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-endpoint-crud-model", nil, "native", &strategyID, true)
	dependentConnectionID := modelInsertConnection(t, harness, defaultProfileID, modelConfigID, dependentID, 0, true, nil)

	blockedDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", dependentID), nil, modelHeader(defaultProfileID))
	assertStatus(t, blockedDelete, http.StatusConflict)
	var blockedPayload map[string]any
	decodeJSONResponse(t, blockedDelete, &blockedPayload)
	detail := asMap(t, blockedPayload["detail"])
	if detail["message"] != "Cannot delete endpoint that is referenced by connections" {
		t.Fatalf("expected structured endpoint delete conflict, got %+v", blockedPayload)
	}
	connections := detail["connections"].([]any)
	if len(connections) != 1 || jsonInt(t, asMap(t, connections[0])["connection_id"]) != dependentConnectionID {
		t.Fatalf("expected delete conflict to expose dependent connection rows, got %+v", blockedPayload)
	}

	disableDependentModel := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{"is_enabled": false}, modelHeader(defaultProfileID))
	assertStatus(t, disableDependentModel, http.StatusOK)
	dependentTargetID := modelLoadConnectionTargetID(t, harness, modelConfigID, dependentConnectionID)
	deleteTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", modelConfigID, dependentTargetID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteTarget, http.StatusOK)
	assertStoredConnectionCount(t, harness, dependentConnectionID, 0)
	deleteDependent := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", dependentID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteDependent, http.StatusOK)
	var deletedPayload map[string]any
	decodeJSONResponse(t, deleteDependent, &deletedPayload)
	if deletedPayload["deleted"] != true {
		t.Fatalf("expected endpoint delete success payload, got %+v", deletedPayload)
	}

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/endpoints", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var endpoints []map[string]any
	decodeJSONResponse(t, listResponse, &endpoints)
	if len(endpoints) < 2 {
		t.Fatalf("expected enough endpoints to verify delete compaction, got %+v", endpoints)
	}
	if jsonInt(t, endpoints[0]["id"]) != primaryID || jsonInt(t, endpoints[0]["position"]) != 0 {
		t.Fatalf("expected primary endpoint to remain first after compaction, got %+v", endpoints)
	}
	if jsonInt(t, endpoints[1]["id"]) != spareID || jsonInt(t, endpoints[1]["position"]) != 1 {
		t.Fatalf("expected later endpoints to compact after delete, got %+v", endpoints)
	}
}

func TestEndpointDeletePreservesReusableEndpointSemantics(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Task 5 Endpoint Reuse Strategy")
	firstOwnerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-endpoint-owner-a", nil, "native", &strategyID, true)
	secondOwnerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-endpoint-owner-b", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Task 5 Reusable Endpoint", 0)
	firstConnectionID := modelInsertConnection(t, harness, profileID, firstOwnerID, endpointID, 0, true, nil)
	secondConnectionID := modelInsertConnection(t, harness, profileID, secondOwnerID, endpointID, 0, true, nil)

	blockedDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(profileID))
	assertStatus(t, blockedDelete, http.StatusConflict)
	var blockedPayload map[string]any
	decodeJSONResponse(t, blockedDelete, &blockedPayload)
	blockedConnections := asMap(t, blockedPayload["detail"])["connections"].([]any)
	assertEndpointDeleteConflictConnection(t, blockedConnections, firstConnectionID, firstOwnerID, "task5-endpoint-owner-a")
	assertEndpointDeleteConflictConnection(t, blockedConnections, secondConnectionID, secondOwnerID, "task5-endpoint-owner-b")

	deleteFirstOwner := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", firstOwnerID), nil, modelHeader(profileID))
	assertStatus(t, deleteFirstOwner, http.StatusOK)
	assertStoredConnectionCount(t, harness, firstConnectionID, 0)
	stillBlocked := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(profileID))
	assertStatus(t, stillBlocked, http.StatusConflict)
	decodeJSONResponse(t, stillBlocked, &blockedPayload)
	remainingConnections := asMap(t, blockedPayload["detail"])["connections"].([]any)
	if len(remainingConnections) != 1 {
		t.Fatalf("expected one remaining endpoint usage row, got %+v", blockedPayload)
	}
	assertEndpointDeleteConflictConnection(t, remainingConnections, secondConnectionID, secondOwnerID, "task5-endpoint-owner-b")

	deleteSecondOwner := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d", secondOwnerID), nil, modelHeader(profileID))
	assertStatus(t, deleteSecondOwner, http.StatusOK)
	assertStoredConnectionCount(t, harness, secondConnectionID, 0)
	deleteEndpoint := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(profileID))
	assertStatus(t, deleteEndpoint, http.StatusOK)
	assertEndpointCount(t, harness, endpointID, 0)
}

func TestEndpointDuplicate(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	sourceID := modelInsertEndpoint(t, harness, defaultProfileID, "Duplicate Me", 0)
	_ = modelInsertEndpoint(t, harness, defaultProfileID, "Duplicate Me copy", 1)
	_ = modelInsertEndpoint(t, harness, defaultProfileID, "Duplicate Me copy 2", 2)

	duplicateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/duplicate", sourceID), nil, modelHeader(defaultProfileID))
	assertStatus(t, duplicateResponse, http.StatusCreated)
	var duplicate map[string]any
	decodeJSONResponse(t, duplicateResponse, &duplicate)
	if duplicate["name"] != "Duplicate Me copy 3" || jsonInt(t, duplicate["position"]) != 3 {
		t.Fatalf("expected duplicate endpoint name deconfliction and append position, got %+v", duplicate)
	}
	if duplicate["base_url"] != "https://duplicate-me.invalid" || duplicate["masked_api_key"] != "********" {
		t.Fatalf("expected duplicate to preserve endpoint credentials and base_url, got %+v", duplicate)
	}
}

func TestEndpointReorder(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "Endpoint B", 1)
	endpointCID := modelInsertEndpoint(t, harness, defaultProfileID, "Endpoint C", 2)

	reorderResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/endpoints/%d/position", endpointCID), map[string]any{"to_index": 0}, modelHeader(defaultProfileID))
	assertStatus(t, reorderResponse, http.StatusOK)
	var reordered []map[string]any
	decodeJSONResponse(t, reorderResponse, &reordered)
	if jsonInt(t, reordered[0]["id"]) != endpointCID || jsonInt(t, reordered[0]["position"]) != 0 {
		t.Fatalf("expected moved endpoint to land first, got %+v", reordered)
	}
	if jsonInt(t, reordered[1]["id"]) != endpointAID || jsonInt(t, reordered[1]["position"]) != 1 || jsonInt(t, reordered[2]["id"]) != endpointBID || jsonInt(t, reordered[2]["position"]) != 2 {
		t.Fatalf("expected reorder to rewrite contiguous endpoint positions, got %+v", reordered)
	}

	noOpResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/endpoints/%d/position", endpointCID), map[string]any{"to_index": 0}, modelHeader(defaultProfileID))
	assertStatus(t, noOpResponse, http.StatusOK)
	var noOp []map[string]any
	decodeJSONResponse(t, noOpResponse, &noOp)
	if jsonInt(t, noOp[0]["id"]) != endpointCID || jsonInt(t, noOp[1]["id"]) != endpointAID || jsonInt(t, noOp[2]["id"]) != endpointBID {
		t.Fatalf("expected no-op reorder to preserve ordered list, got %+v", noOp)
	}

	outOfRange := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/endpoints/%d/position", endpointCID), map[string]any{"to_index": 9}, modelHeader(defaultProfileID))
	assertErrorResponse(t, outOfRange, http.StatusUnprocessableEntity, "to_index must be between 0 and 2")
}

func TestEndpointConnections(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := insertContractProfile(t, harness, "S9 Helper Profile")
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	defaultStrategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Endpoint Helper Strategy")
	otherStrategyID := modelInsertLoadbalanceStrategy(t, harness, otherProfileID, "S9 Other Profile Strategy")
	defaultModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "endpoint-helper-default", nil, "native", &defaultStrategyID, true)
	otherModelID := modelInsertModel(t, harness, otherProfileID, &vendorID, "openai", "endpoint-helper-other", nil, "native", &otherStrategyID, true)
	defaultEndpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Helper Endpoint Default", 0)
	otherEndpointID := modelInsertEndpoint(t, harness, otherProfileID, "Helper Endpoint Other", 0)
	firstConnectionID := modelInsertConnection(t, harness, defaultProfileID, defaultModelID, defaultEndpointID, 0, true, nil)
	secondConnectionID := modelInsertConnection(t, harness, defaultProfileID, defaultModelID, defaultEndpointID, 1, false, nil)
	_ = modelInsertConnection(t, harness, otherProfileID, otherModelID, otherEndpointID, 0, true, nil)

	helperResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/endpoints/connections", nil, modelHeader(defaultProfileID))
	assertStatus(t, helperResponse, http.StatusOK)
	var helperPayload map[string]any
	decodeJSONResponse(t, helperResponse, &helperPayload)
	items := helperPayload["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected helper dropdown to stay profile-scoped, got %+v", helperPayload)
	}
	if jsonInt(t, asMap(t, items[0])["id"]) != firstConnectionID || jsonInt(t, asMap(t, items[0])["endpoint_id"]) != defaultEndpointID {
		t.Fatalf("expected dropdown item shape for first connection, got %+v", helperPayload)
	}
	if jsonInt(t, asMap(t, items[1])["id"]) != secondConnectionID || jsonInt(t, asMap(t, items[1])["endpoint_id"]) != defaultEndpointID {
		t.Fatalf("expected dropdown items ordered by connection id, got %+v", helperPayload)
	}
}

func assertEndpointDeleteConflictConnection(t *testing.T, connections []any, connectionID int, modelConfigID int, modelID string) {
	t.Helper()
	for _, raw := range connections {
		item := asMap(t, raw)
		if jsonInt(t, item["connection_id"]) != connectionID {
			continue
		}
		if jsonInt(t, item["model_config_id"]) != modelConfigID || item["model_id"] != modelID {
			t.Fatalf("unexpected endpoint delete usage for connection %d: %+v", connectionID, item)
		}
		return
	}
	t.Fatalf("expected endpoint delete conflict to include connection %d, got %+v", connectionID, connections)
}

func newEndpointConnectionContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "endpoint_connection_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "endpoint-connection-contract-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "endpoint-connection-contract-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	endpointsService, err := managementendpoints.NewService(settings, managementendpoints.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build endpoints service: %v", err)
	}
	t.Cleanup(endpointsService.Close)
	connectionsService, err := managementconnections.NewService(settings, managementconnections.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build connections service: %v", err)
	}
	t.Cleanup(connectionsService.Close)
	modelsService, err := managementmodels.NewService(settings, managementmodels.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build models service: %v", err)
	}
	t.Cleanup(modelsService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "endpoint-connection-contract-test", EndpointsService: endpointsService, ConnectionsService: connectionsService, ModelsService: modelsService})
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
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, server: server, service: nil, url: server.URL}
}

func insertContractProfile(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, $2, FALSE, FALSE, TRUE, 0, NULL, $3, $3) RETURNING id`, name, nil, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

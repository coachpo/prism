package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

func TestEndpointCRUD(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	createPrimary := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Primary OpenAI", "base_url": "https://api.openai.com/", "api_key": "sk-primary"}, nil)
	assertStatus(t, createPrimary, http.StatusCreated)
	var primary map[string]any
	decodeJSONResponse(t, createPrimary, &primary)
	primaryID := jsonInt(t, primary["id"])
	if jsonInt(t, primary["profile_id"]) != defaultProfileID {
		t.Fatalf("expected missing profile header to create in Default profile %d, got %+v", defaultProfileID, primary)
	}
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
	if updated["name"] != "Primary Updated" || updated["base_url"] != "https://api.openai.com/v2" || updated["masked_api_key"] != "********" {
		t.Fatalf("expected endpoint update payload to persist changes, got %+v", updated)
	}

	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Endpoint CRUD Strategy")
	boundaryModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-endpoint-boundary-model", nil, "native", &strategyID, true)
	boundaryConnectionID := seedEndpointBoundaryConnectionUsage(t, harness, defaultProfileID, boundaryModelID, primaryID)
	beforeBoundary := loadEndpointConnectionBoundaryState(t, harness, boundaryModelID, boundaryConnectionID)

	boundaryUpdate := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/endpoints/%d", primaryID), map[string]any{"base_url": "https://api.openai.com/v3/", "api_key": "sk-boundary-rotated"}, modelHeader(defaultProfileID))
	assertStatus(t, boundaryUpdate, http.StatusOK)
	decodeJSONResponse(t, boundaryUpdate, &updated)
	if updated["base_url"] != "https://api.openai.com/v3" || updated["masked_api_key"] != "********" {
		t.Fatalf("expected boundary endpoint update to keep masked credentials, got %+v", updated)
	}
	afterBoundary := loadEndpointConnectionBoundaryState(t, harness, boundaryModelID, boundaryConnectionID)
	if beforeBoundary != afterBoundary {
		t.Fatalf("endpoint update changed dependent routing/pricing/health state\nbefore=%+v\nafter=%+v", beforeBoundary, afterBoundary)
	}

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
	if jsonInt(t, endpoints[0]["id"]) != primaryID || jsonInt(t, endpoints[0]["position"]) != 0 || endpoints[0]["masked_api_key"] != "********" {
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

type endpointConnectionBoundaryState struct {
	EndpointID        int
	PricingTemplateID int
	QPSLimit          int
	MaxNonStream      int
	MaxStream         int
	Priority          int
	AuthType          string
	CustomHeaders     string
	HealthStatus      string
	HealthDetail      string
	LastHealthAt      time.Time
	TargetPosition    int
	TargetEnabled     bool
}

func seedEndpointBoundaryConnectionUsage(t *testing.T, harness *contractHarness, profileID int, modelConfigID int, endpointID int) int {
	t.Helper()
	now := time.Now().UTC()
	var pricingTemplateID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, 'Endpoint Boundary Pricing', NULL, 'PER_1M', 'USD', '1', '2', '0', '0', '0', 1, $2, $2) RETURNING id`, profileID, now).Scan(&pricingTemplateID); err != nil {
		t.Fatalf("insert boundary pricing template: %v", err)
	}
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 5, true, nil)
	lastHealthAt := now.Add(-time.Hour)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET pricing_template_id = $2, qps_limit = 7, max_in_flight_non_stream = 3, max_in_flight_stream = 2, priority = 42, name = 'boundary-terminal', auth_type = 'bearer', custom_headers = $3, health_status = 'degraded', health_detail = 'sticky health detail', last_health_check = $4 WHERE id = $1`, connectionID, pricingTemplateID, `{"X-Boundary":"kept"}`, lastHealthAt); err != nil {
		t.Fatalf("update boundary connection state: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_access_targets SET position = 5, is_enabled = FALSE WHERE source_model_config_id = $1 AND target_connection_id = $2`, modelConfigID, connectionID); err != nil {
		t.Fatalf("update boundary access target state: %v", err)
	}
	return connectionID
}

func loadEndpointConnectionBoundaryState(t *testing.T, harness *contractHarness, modelConfigID int, connectionID int) endpointConnectionBoundaryState {
	t.Helper()
	var state endpointConnectionBoundaryState
	if err := harness.conn.QueryRow(context.Background(), `SELECT connections.endpoint_id, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, connections.priority, COALESCE(connections.auth_type, ''), COALESCE(connections.custom_headers::text, ''), connections.health_status, COALESCE(connections.health_detail, ''), COALESCE(connections.last_health_check, '0001-01-01 00:00:00+00'::timestamptz), model_access_targets.position, model_access_targets.is_enabled FROM connections JOIN model_access_targets ON model_access_targets.target_connection_id = connections.id WHERE model_access_targets.source_model_config_id = $1 AND connections.id = $2`, modelConfigID, connectionID).Scan(&state.EndpointID, &state.PricingTemplateID, &state.QPSLimit, &state.MaxNonStream, &state.MaxStream, &state.Priority, &state.AuthType, &state.CustomHeaders, &state.HealthStatus, &state.HealthDetail, &state.LastHealthAt, &state.TargetPosition, &state.TargetEnabled); err != nil {
		t.Fatalf("load endpoint connection boundary state: %v", err)
	}
	return state
}

func newEndpointConnectionContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	return newContractHarnessFor(t, "endpoint_connection_contract", contractHarnessOptions{
		SecretEncryptionKey: "endpoint-connection-contract-secret",
		Version:             "endpoint-connection-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
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
			return platformhttp.Dependencies{
				EndpointsService:   endpointsService,
				ConnectionsService: connectionsService,
				ModelsService:      modelsService,
			}
		},
	})
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

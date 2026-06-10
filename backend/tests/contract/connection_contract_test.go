package contract_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestConnectionStandaloneMutationRejections(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Task 4 Public Connection Rejection Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "task4-public-connection-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Public Rejection Connection Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, defaultProfileID, ownerModelID, endpointID, 0, true, map[string]string{"x-test": "1"})
	pricingTemplateID := insertContractPricingTemplate(t, harness, defaultProfileID, "Task 4 Public Connection Pricing")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/connections", map[string]any{"api_family": "openai", "endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Rejected Public Mutation"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, createResponse, http.StatusBadRequest, connectionOwnerScopedMutationDetail())
	assertConnectionNameCount(t, harness, defaultProfileID, "Rejected Public Mutation", 0)

	for _, testCase := range []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodPut, path: fmt.Sprintf("/api/connections/%d", connectionID), body: map[string]any{"name": "Rejected Put"}},
		{method: http.MethodPatch, path: fmt.Sprintf("/api/connections/%d", connectionID), body: map[string]any{"name": "Rejected Patch"}},
		{method: http.MethodDelete, path: fmt.Sprintf("/api/connections/%d", connectionID)},
		{method: http.MethodPut, path: fmt.Sprintf("/api/connections/%d/pricing-template", connectionID), body: map[string]any{"pricing_template_id": pricingTemplateID}},
		{method: http.MethodPost, path: fmt.Sprintf("/api/connections/%d/health-check", connectionID)},
	} {
		response := harness.requestJSON(t, harness.client, testCase.method, testCase.path, testCase.body, modelHeader(defaultProfileID))
		assertErrorResponse(t, response, http.StatusBadRequest, connectionOwnerScopedMutationDetail())
	}
	assertStoredConnectionCount(t, harness, connectionID, 1)
}

func TestConnectionReadSurfacesHideOwnerlessConnections(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Task 4 Private Connection Read Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "task4-private-read-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Private Connection Read Endpoint", 0)
	ownedConnectionID := modelInsertConnection(t, harness, defaultProfileID, ownerModelID, endpointID, 0, true, map[string]string{"x-owner": "1"})
	ownerlessConnectionID := modelInsertStandaloneConnection(t, harness, defaultProfileID, "openai", endpointID, 1, true, map[string]string{"x-orphan": "1"})
	ownerTargetID := modelLoadConnectionTargetID(t, harness, ownerModelID, ownedConnectionID)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/connections", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed) != 1 || jsonInt(t, listed[0]["id"]) != ownedConnectionID || jsonInt(t, listed[0]["model_config_id"]) != ownerModelID {
		t.Fatalf("expected /api/connections to return only owner-backed private connections, got %+v", listed)
	}
	if jsonInt(t, listed[0]["endpoint_id"]) != endpointID || asMap(t, listed[0]["custom_headers"])["x-owner"] != "1" {
		t.Fatalf("expected /api/connections to keep the owned connection payload intact, got %+v", listed[0])
	}

	getOwnedResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d", ownedConnectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, getOwnedResponse, http.StatusOK)
	var fetched map[string]any
	decodeJSONResponse(t, getOwnedResponse, &fetched)
	if jsonInt(t, fetched["id"]) != ownedConnectionID || jsonInt(t, fetched["model_config_id"]) != ownerModelID || jsonInt(t, fetched["endpoint_id"]) != endpointID || asMap(t, fetched["custom_headers"])["x-owner"] != "1" {
		t.Fatalf("expected owner-backed private connection payload, got %+v", fetched)
	}

	getOwnerlessResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d", ownerlessConnectionID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, getOwnerlessResponse, http.StatusNotFound, "Connection not found")

	referencesResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/references", ownedConnectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, referencesResponse, http.StatusOK)
	var references map[string]any
	decodeJSONResponse(t, referencesResponse, &references)
	items := references["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected exactly one owner reference for a private connection, got %+v", references)
	}
	ownerReference := asMap(t, items[0])
	if jsonInt(t, ownerReference["target_id"]) != ownerTargetID || jsonInt(t, ownerReference["model_config_id"]) != ownerModelID || ownerReference["model_id"] != "task4-private-read-owner" {
		t.Fatalf("expected /references to report the direct owner model only, got %+v", references)
	}

	ownerRouteResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/owner", ownedConnectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, ownerRouteResponse, http.StatusNotFound)

	ownerlessReferencesResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/references", ownerlessConnectionID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, ownerlessReferencesResponse, http.StatusNotFound, "Connection not found")
	assertStoredConnectionCount(t, harness, ownerlessConnectionID, 1)
}

func TestTargetRouteCRUD(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Target Route Strategy")
	sourceModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-target-route-source", nil, "native", &strategyID, true)
	targetModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-target-route-model", nil, "native", &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "Target Route Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "Target Route Endpoint B", 1)

	createConnection := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", sourceModelID), map[string]any{"endpoint_id": endpointAID, "openai_text_capability": "responses_only", "is_active": true, "name": "Owner Route Connection"}, modelHeader(defaultProfileID))
	assertStatus(t, createConnection, http.StatusCreated)
	var connectionPayload map[string]any
	decodeJSONResponse(t, createConnection, &connectionPayload)
	connectionID := jsonInt(t, connectionPayload["id"])
	connectionTargetID := modelLoadConnectionTargetID(t, harness, sourceModelID, connectionID)

	createModelTarget := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", sourceModelID), map[string]any{"target_type": "model", "target_model_id": "s9-target-route-model", "position": 1, "is_enabled": false}, modelHeader(defaultProfileID))
	assertStatus(t, createModelTarget, http.StatusCreated)
	var targets []map[string]any
	decodeJSONResponse(t, createModelTarget, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "connection", ConnectionID: connectionID, Position: 0, IsEnabled: true}, {TargetType: "model", TargetModelID: "s9-target-route-model", Position: 1, IsEnabled: false}})
	modelTargetID := jsonInt(t, targets[1]["id"])

	updateConnection := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", sourceModelID, connectionID), map[string]any{"endpoint_id": endpointBID, "name": "Owner Route Connection Updated", "is_active": false, "custom_headers": map[string]string{"x-owner": "2"}}, modelHeader(defaultProfileID))
	assertStatus(t, updateConnection, http.StatusOK)
	var updatedConnection map[string]any
	decodeJSONResponse(t, updateConnection, &updatedConnection)
	if jsonInt(t, updatedConnection["id"]) != connectionID || jsonInt(t, updatedConnection["model_config_id"]) != sourceModelID || jsonInt(t, updatedConnection["endpoint_id"]) != endpointBID || updatedConnection["is_active"] != false || asMap(t, updatedConnection["custom_headers"])["x-owner"] != "2" {
		t.Fatalf("expected owner-scoped update to preserve owner target and update connection fields, got %+v", updatedConnection)
	}
	assertConnectionOwnerTarget(t, harness, sourceModelID, connectionID, 0, true)

	changeFamily := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", sourceModelID, connectionID), map[string]any{"api_family": "anthropic"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, changeFamily, http.StatusBadRequest, "api_family must match owner model api_family")
	assertStoredConnectionAPIFamily(t, harness, connectionID, "openai")

	enableModelTarget := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, modelTargetID), map[string]any{"is_enabled": true}, modelHeader(defaultProfileID))
	assertStatus(t, enableModelTarget, http.StatusOK)
	reorderResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d/position", sourceModelID, modelTargetID), map[string]any{"to_index": 0}, modelHeader(defaultProfileID))
	assertStatus(t, reorderResponse, http.StatusOK)
	decodeJSONResponse(t, reorderResponse, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s9-target-route-model", Position: 0, IsEnabled: true}, {TargetType: "connection", ConnectionID: connectionID, Position: 1, IsEnabled: true}})
	connectionTargetID = jsonInt(t, targets[1]["id"])

	toggleConnectionTarget := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, connectionTargetID), map[string]any{"is_enabled": false}, modelHeader(defaultProfileID))
	assertStatus(t, toggleConnectionTarget, http.StatusOK)
	decodeJSONResponse(t, toggleConnectionTarget, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s9-target-route-model", Position: 0, IsEnabled: true}, {TargetType: "connection", ConnectionID: connectionID, Position: 1, IsEnabled: false}})
	_ = targetModelID
}

func TestDeleteReferencedConnection(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Delete Referenced Connection Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-delete-referenced-connection", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Delete Referenced Endpoint", 0)

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "is_active": true, "name": "Delete Referenced Private"}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])

	referencesResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/references", connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, referencesResponse, http.StatusOK)
	var references map[string]any
	decodeJSONResponse(t, referencesResponse, &references)
	items := references["items"].([]any)
	if len(items) != 1 || jsonInt(t, asMap(t, items[0])["model_config_id"]) != modelConfigID || asMap(t, items[0])["model_id"] != "s9-delete-referenced-connection" {
		t.Fatalf("expected connection references to expose the single owner model, got %+v", references)
	}

	standaloneDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/connections/%d", connectionID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, standaloneDelete, http.StatusBadRequest, connectionOwnerScopedMutationDetail())

	ownerDeleteLastTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, ownerDeleteLastTarget, http.StatusBadRequest, "enabled models must include at least one enabled access target")
	assertStoredConnectionCount(t, harness, connectionID, 1)
	assertModelConnectionTargetCount(t, harness, modelConfigID, 1)

	disableModel := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{"is_enabled": false}, modelHeader(defaultProfileID))
	assertStatus(t, disableModel, http.StatusOK)

	ownerDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, ownerDelete, http.StatusOK)
	assertStoredConnectionCount(t, harness, connectionID, 0)
	assertModelConnectionTargetCount(t, harness, modelConfigID, 0)
}

func TestConnectionReferencesReportSingleOwner(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Task 5 Reference Strategy")
	ownerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-reference-owner", nil, "native", &strategyID, true)
	facadeID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-reference-facade", nil, "native", &strategyID, true)
	otherOwnerID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "task5-reference-other-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, profileID, "Task 5 Reference Endpoint", 0)
	connectionID := modelInsertConnection(t, harness, profileID, ownerID, endpointID, 0, true, nil)
	otherConnectionID := modelInsertConnection(t, harness, profileID, otherOwnerID, endpointID, 0, true, nil)
	modelInsertModelTarget(t, harness, profileID, facadeID, ownerID, 0, true)
	ownerTargetID := modelLoadConnectionTargetID(t, harness, ownerID, connectionID)

	referencesResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/references", connectionID), nil, modelHeader(profileID))
	assertStatus(t, referencesResponse, http.StatusOK)
	var references map[string]any
	decodeJSONResponse(t, referencesResponse, &references)
	items := references["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected exactly one owner reference, got %+v", references)
	}
	ownerReference := asMap(t, items[0])
	if jsonInt(t, ownerReference["target_id"]) != ownerTargetID || jsonInt(t, ownerReference["model_config_id"]) != ownerID || ownerReference["model_id"] != "task5-reference-owner" {
		t.Fatalf("expected direct owner reference only, got %+v", references)
	}

	otherReferencesResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/references", otherConnectionID), nil, modelHeader(profileID))
	assertStatus(t, otherReferencesResponse, http.StatusOK)
	decodeJSONResponse(t, otherReferencesResponse, &references)
	items = references["items"].([]any)
	if len(items) != 1 || jsonInt(t, asMap(t, items[0])["model_config_id"]) != otherOwnerID || asMap(t, items[0])["model_id"] != "task5-reference-other-owner" {
		t.Fatalf("expected second private connection to report its own single owner, got %+v", references)
	}
}

func TestTargetDeleteOwnedConnectionDeletesConnectionNoOrphan(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Task 4 Target Delete Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "task4-target-delete-owner", nil, "native", &strategyID, true)
	targetModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "task4-target-delete-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Task 4 Target Delete Endpoint", 0)

	createConnection := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "is_active": true, "name": "Target Delete Private"}, modelHeader(defaultProfileID))
	assertStatus(t, createConnection, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createConnection, &created)
	connectionID := jsonInt(t, created["id"])
	connectionTargetID := modelLoadConnectionTargetID(t, harness, ownerModelID, connectionID)

	createModelTarget := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", ownerModelID), map[string]any{"target_type": "model", "target_model_id": "task4-target-delete-model", "position": 1, "is_enabled": true}, modelHeader(defaultProfileID))
	assertStatus(t, createModelTarget, http.StatusCreated)

	deleteConnectionTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", ownerModelID, connectionTargetID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteConnectionTarget, http.StatusOK)
	var targets []map[string]any
	decodeJSONResponse(t, deleteConnectionTarget, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "model", TargetModelID: "task4-target-delete-model", Position: 0, IsEnabled: true}})
	assertStoredConnectionCount(t, harness, connectionID, 0)
	assertModelConnectionTargetCount(t, harness, ownerModelID, 0)
	_ = targetModelID
}

func TestModelAPIFamilyChangeRejectsPrivateConnections(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Task 4 Family Change Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "task4-family-change-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Task 4 Family Change Endpoint", 0)
	createConnection := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Family Change Private"}, modelHeader(defaultProfileID))
	assertStatus(t, createConnection, http.StatusCreated)

	changeFamily := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{"api_family": "anthropic"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, changeFamily, http.StatusConflict, "Cannot change api_family while private connections exist")
	assertStoredModelAPIFamily(t, harness, modelConfigID, "openai")
}

func TestModelScopedConnectionCreateCreatesOwnerTarget(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Model Scoped Create Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-model-scoped-owner", nil, "native", &strategyID, true)
	otherModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-model-scoped-other", nil, "native", &strategyID, true)
	ownerEndpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Model Scoped Owner Endpoint", 0)
	otherEndpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Model Scoped Other Endpoint", 1)
	standaloneEndpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Model Scoped Standalone Endpoint", 2)
	standaloneConnectionID := modelInsertStandaloneConnection(t, harness, defaultProfileID, "openai", standaloneEndpointID, 0, true, nil)

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": ownerEndpointID, "openai_text_capability": "responses_only", "is_active": true, "name": "Owner Private Connection", "custom_headers": map[string]string{"x-owner": "1"}, "qps_limit": 2}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if jsonInt(t, created["model_config_id"]) != ownerModelID || created["api_family"] != "openai" || jsonInt(t, created["endpoint_id"]) != ownerEndpointID || jsonInt(t, created["priority"]) != 0 || created["health_status"] != "unknown" {
		t.Fatalf("expected owner-scoped created connection payload, got %+v", created)
	}
	if asMap(t, created["custom_headers"])["x-owner"] != "1" || jsonInt(t, asMap(t, created["endpoint"])["id"]) != ownerEndpointID || created["openai_probe_endpoint_variant"] != "responses_minimal" {
		t.Fatalf("expected owner-scoped create to hydrate endpoint, headers, and OpenAI probe default, got %+v", created)
	}
	assertConnectionOwnerTarget(t, harness, ownerModelID, connectionID, 0, true)
	assertStoredConnectionAPIFamily(t, harness, connectionID, "openai")

	otherCreateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", otherModelID), map[string]any{"api_family": "openai", "endpoint_id": otherEndpointID, "openai_text_capability": "responses_only", "name": "Other Private Connection"}, modelHeader(defaultProfileID))
	assertStatus(t, otherCreateResponse, http.StatusCreated)
	var otherCreated map[string]any
	decodeJSONResponse(t, otherCreateResponse, &otherCreated)
	otherConnectionID := jsonInt(t, otherCreated["id"])

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/connections", ownerModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed) != 1 || jsonInt(t, listed[0]["id"]) != connectionID || jsonInt(t, listed[0]["model_config_id"]) != ownerModelID {
		t.Fatalf("expected owner model list to contain only its private connection, excluding standalone %d and other-owned %d, got %+v", standaloneConnectionID, otherConnectionID, listed)
	}
}

func TestModelConnectionContextCapabilityOverrides(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Connection Context Capability Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "connection-context-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Context Capability Endpoint", 0)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET context_window_tokens = 128000, default_output_token_reserve = 2048, max_context_utilization = 0.80 WHERE id = $1`, ownerModelID); err != nil {
		t.Fatalf("seed owner model context capabilities: %v", err)
	}

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Inherited Capability Connection"}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if jsonInt(t, created["context_window_tokens"]) != 128000 || jsonInt(t, created["default_output_token_reserve"]) != 2048 || jsonFloat(t, created["max_context_utilization"]) != 0.8 {
		t.Fatalf("expected owner defaults to hydrate created connection, got %+v", created)
	}
	assertContextCapabilityOverridesPayload(t, created, nil, nil, nil)
	assertStoredConnectionContextCapabilities(t, harness, connectionID, intPtr(128000), false, 2048, false, 0.8, false)

	sameAsOwnerOverrideResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"context_window_tokens": 128000, "default_output_token_reserve": 2048, "max_context_utilization": 0.80}, modelHeader(defaultProfileID))
	assertStatus(t, sameAsOwnerOverrideResponse, http.StatusOK)
	var sameAsOwnerOverride map[string]any
	decodeJSONResponse(t, sameAsOwnerOverrideResponse, &sameAsOwnerOverride)
	if jsonInt(t, sameAsOwnerOverride["context_window_tokens"]) != 128000 || jsonInt(t, sameAsOwnerOverride["default_output_token_reserve"]) != 2048 || jsonFloat(t, sameAsOwnerOverride["max_context_utilization"]) != 0.8 {
		t.Fatalf("expected explicit same-as-owner overrides to keep effective values unchanged, got %+v", sameAsOwnerOverride)
	}
	assertContextCapabilityOverridesPayload(t, sameAsOwnerOverride, intPtr(128000), intPtr(2048), float64Ptr(0.8))
	assertStoredConnectionContextCapabilities(t, harness, connectionID, intPtr(128000), true, 2048, true, 0.8, true)

	resetContextWindowResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"context_window_tokens": nil}, modelHeader(defaultProfileID))
	assertStatus(t, resetContextWindowResponse, http.StatusOK)
	var resetContextWindow map[string]any
	decodeJSONResponse(t, resetContextWindowResponse, &resetContextWindow)
	if jsonInt(t, resetContextWindow["context_window_tokens"]) != 128000 || jsonInt(t, resetContextWindow["default_output_token_reserve"]) != 2048 || jsonFloat(t, resetContextWindow["max_context_utilization"]) != 0.8 {
		t.Fatalf("expected reset-to-owner to preserve effective values while clearing only one override flag, got %+v", resetContextWindow)
	}
	assertContextCapabilityOverridesPayload(t, resetContextWindow, nil, intPtr(2048), float64Ptr(0.8))
	assertStoredConnectionContextCapabilities(t, harness, connectionID, intPtr(128000), false, 2048, true, 0.8, true)

	unrelatedUpdateResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"name": "Inherited Capability Connection Renamed"}, modelHeader(defaultProfileID))
	assertStatus(t, unrelatedUpdateResponse, http.StatusOK)
	var unrelatedUpdate map[string]any
	decodeJSONResponse(t, unrelatedUpdateResponse, &unrelatedUpdate)
	assertContextCapabilityOverridesPayload(t, unrelatedUpdate, nil, intPtr(2048), float64Ptr(0.8))
	assertStoredConnectionContextCapabilities(t, harness, connectionID, intPtr(128000), false, 2048, true, 0.8, true)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/connections", ownerModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed) != 1 || jsonInt(t, listed[0]["id"]) != connectionID {
		t.Fatalf("expected owner-scoped connection list with one item, got %+v", listed)
	}
	assertContextCapabilityOverridesPayload(t, listed[0], nil, intPtr(2048), float64Ptr(0.8))

	invalidReserve := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "default_output_token_reserve": 0}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalidReserve, http.StatusUnprocessableEntity, "default_output_token_reserve must be greater than or equal to 1 when provided")

	invalidUtilization := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "max_context_utilization": 1.1}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalidUtilization, http.StatusUnprocessableEntity, "max_context_utilization must be greater than 0 and less than or equal to 1 when provided")
}

func TestModelDetailConnectionContextCapabilityResponses(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Model Detail Connection Capability Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "model-detail-connection-context-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Model Detail Connection Capability Endpoint", 0)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET context_window_tokens = 32000, default_output_token_reserve = 1024, max_context_utilization = 0.60 WHERE id = $1`, ownerModelID); err != nil {
		t.Fatalf("seed owner model detail context capabilities: %v", err)
	}

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Detail Capability Connection", "context_window_tokens": 64000, "default_output_token_reserve": 1024}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d", ownerModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detail map[string]any
	decodeJSONResponse(t, detailResponse, &detail)
	accessTargets := detail["access_targets"].([]any)
	if len(accessTargets) != 1 {
		t.Fatalf("expected one model detail access target, got %+v", detail)
	}
	assertNestedConnectionContextCapabilityPayload(t, asMap(t, accessTargets[0]), connectionID, intPtr(64000), 1024, 0.6, intPtr(64000), intPtr(1024), nil)

	targetsResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/targets", ownerModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, targetsResponse, http.StatusOK)
	var targets []map[string]any
	decodeJSONResponse(t, targetsResponse, &targets)
	if len(targets) != 1 {
		t.Fatalf("expected one target-list access target, got %+v", targets)
	}
	assertNestedConnectionContextCapabilityPayload(t, targets[0], connectionID, intPtr(64000), 1024, 0.6, intPtr(64000), intPtr(1024), nil)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/models", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	var ownerItem map[string]any
	for _, item := range listed {
		if jsonInt(t, item["id"]) == ownerModelID {
			ownerItem = item
			break
		}
	}
	if ownerItem == nil {
		t.Fatalf("expected /api/models list to include owner model %d, got %+v", ownerModelID, listed)
	}
	listedTargets := ownerItem["access_targets"].([]any)
	if len(listedTargets) != 1 {
		t.Fatalf("expected one listed access target for owner model, got %+v", ownerItem)
	}
	assertNestedConnectionContextCapabilityPayload(t, asMap(t, listedTargets[0]), connectionID, intPtr(64000), 1024, 0.6, intPtr(64000), intPtr(1024), nil)
}

func TestModelScopedConnectionCreateRejectsConflictingAPIFamily(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Conflicting Family Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-conflicting-family-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Conflicting Family Endpoint", 0)

	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"api_family": "anthropic", "endpoint_id": endpointID, "name": "Conflicting Family Connection"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, response, http.StatusBadRequest, "api_family must match owner model api_family")
	assertConnectionNameCount(t, harness, defaultProfileID, "Conflicting Family Connection", 0)
	assertModelConnectionTargetCount(t, harness, modelConfigID, 0)
}

func TestModelScopedConnectionCreateRollsBackWhenOwnerTargetInsertFails(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Rollback Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-rollback-target-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Rollback Target Endpoint", 0)

	if _, err := harness.conn.Exec(context.Background(), `CREATE OR REPLACE FUNCTION task3_fail_model_access_target_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced owner target insert failure'; END; $$`); err != nil {
		t.Fatalf("install access target failure function: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `CREATE TRIGGER task3_fail_model_access_target_insert BEFORE INSERT ON model_access_targets FOR EACH ROW EXECUTE FUNCTION task3_fail_model_access_target_insert()`); err != nil {
		t.Fatalf("install access target failure trigger: %v", err)
	}

	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Rollback Connection"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, response, http.StatusInternalServerError, "Internal server error")
	assertConnectionNameCount(t, harness, defaultProfileID, "Rollback Connection", 0)
}

func TestLegacyModelConnectionAuxiliaryRoutesRejectWithOwnerGuidance(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Legacy Route Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-legacy-route-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Legacy Route Endpoint", 0)
	createConnection := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Legacy Route Private"}, modelHeader(defaultProfileID))
	assertStatus(t, createConnection, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createConnection, &created)
	connectionID := jsonInt(t, created["id"])

	for _, testCase := range []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodPut, path: fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), body: map[string]any{"name": "Legacy Put"}},
		{method: http.MethodPut, path: fmt.Sprintf("/api/models/%d/connections/%d/pricing-template", modelConfigID, connectionID), body: map[string]any{"pricing_template_id": nil}},
		{method: http.MethodPost, path: fmt.Sprintf("/api/models/%d/connections/%d/health-check", modelConfigID, connectionID)},
		{method: http.MethodPatch, path: fmt.Sprintf("/api/models/%d/connections/%d/priority", modelConfigID, connectionID), body: map[string]any{"to_index": 0}},
	} {
		response := harness.requestJSON(t, harness.client, testCase.method, testCase.path, testCase.body, modelHeader(defaultProfileID))
		assertErrorResponse(t, response, http.StatusBadRequest, connectionOwnerScopedMutationDetail())
	}

	legacyPreview := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/health-check-preview", modelConfigID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only"}, modelHeader(defaultProfileID))
	assertStatus(t, legacyPreview, http.StatusNotFound)
}

func TestModelConnectionsBatch(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Batch Strategy")
	modelAID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-batch-a", nil, "native", &strategyID, true)
	modelBID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-batch-b", nil, "native", &strategyID, true)
	modelCID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-batch-c", nil, "native", &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "Batch Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "Batch Endpoint B", 1)
	connectionBSlowID := modelInsertConnection(t, harness, defaultProfileID, modelBID, endpointAID, 2, true, map[string]string{"x-b": "1"})
	connectionBFastID := modelInsertConnection(t, harness, defaultProfileID, modelBID, endpointBID, 0, false, nil)
	connectionAID := modelInsertConnection(t, harness, defaultProfileID, modelAID, endpointAID, 1, true, nil)

	batchResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models/connections/batch", map[string]any{"model_config_ids": []int{modelBID, modelAID, modelBID, modelCID}}, modelHeader(defaultProfileID))
	assertStatus(t, batchResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, batchResponse, &payload)
	items := payload["items"].([]any)
	if len(items) != 3 || jsonInt(t, asMap(t, items[0])["model_config_id"]) != modelBID || jsonInt(t, asMap(t, items[1])["model_config_id"]) != modelAID || jsonInt(t, asMap(t, items[2])["model_config_id"]) != modelCID {
		t.Fatalf("expected batch helper to preserve first-seen de-duped model order, got %+v", payload)
	}

	modelBConnections := asMap(t, items[0])["connections"].([]any)
	if len(modelBConnections) != 2 || jsonInt(t, asMap(t, modelBConnections[0])["id"]) != connectionBFastID || jsonInt(t, asMap(t, modelBConnections[0])["priority"]) != 0 || jsonInt(t, asMap(t, modelBConnections[1])["id"]) != connectionBSlowID || jsonInt(t, asMap(t, modelBConnections[1])["priority"]) != 2 {
		t.Fatalf("expected batch helper to keep connection ordering stable per model, got %+v", payload)
	}
	if asMap(t, modelBConnections[0])["endpoint"] == nil || jsonInt(t, asMap(t, asMap(t, modelBConnections[0])["endpoint"])["id"]) != endpointBID {
		t.Fatalf("expected batch helper to include nested endpoint payloads, got %+v", payload)
	}
	if asMap(t, modelBConnections[1])["custom_headers"].(map[string]any)["x-b"] != "1" {
		t.Fatalf("expected batch helper to include parsed custom headers, got %+v", payload)
	}

	modelAConnections := asMap(t, items[1])["connections"].([]any)
	if len(modelAConnections) != 1 || jsonInt(t, asMap(t, modelAConnections[0])["id"]) != connectionAID {
		t.Fatalf("expected batch helper to include ordered model A connections, got %+v", payload)
	}
	modelCConnections := asMap(t, items[2])["connections"].([]any)
	if len(modelCConnections) != 0 {
		t.Fatalf("expected batch helper to return empty connection lists for models without connections, got %+v", payload)
	}
}

func assertTargetRouteOrder(t *testing.T, targets []map[string]any, want []expectedAccessTarget) {
	t.Helper()
	if len(targets) != len(want) {
		t.Fatalf("expected targets %v, got %+v", want, targets)
	}
	for index, target := range targets {
		expected := want[index]
		if target["target_type"] != expected.TargetType || jsonInt(t, target["position"]) != expected.Position || target["is_enabled"] != expected.IsEnabled {
			t.Fatalf("unexpected target at %d: got %+v want %+v", index, target, expected)
		}
		if expected.TargetType == "model" {
			if target["target_model_id"] != expected.TargetModelID || asMap(t, target["target_model"])["model_id"] != expected.TargetModelID {
				t.Fatalf("expected model target %q at %d, got %+v", expected.TargetModelID, index, target)
			}
			continue
		}
		if jsonInt(t, target["connection_id"]) != expected.ConnectionID || jsonInt(t, asMap(t, target["connection"])["id"]) != expected.ConnectionID {
			t.Fatalf("expected connection target %d at %d, got %+v", expected.ConnectionID, index, target)
		}
	}
}

func assertConnectionOwnerTarget(t *testing.T, harness *contractHarness, modelConfigID int, connectionID int, wantPosition int, wantEnabled bool) {
	t.Helper()
	var position int
	var enabled bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT position, is_enabled FROM model_access_targets WHERE source_model_config_id = $1 AND target_connection_id = $2`, modelConfigID, connectionID).Scan(&position, &enabled); err != nil {
		t.Fatalf("load owner target for model %d connection %d: %v", modelConfigID, connectionID, err)
	}
	if position != wantPosition || enabled != wantEnabled {
		t.Fatalf("expected owner target position=%d enabled=%v, got position=%d enabled=%v", wantPosition, wantEnabled, position, enabled)
	}
}

func assertStoredConnectionAPIFamily(t *testing.T, harness *contractHarness, connectionID int, wantAPIFamily string) {
	t.Helper()
	var apiFamily string
	if err := harness.conn.QueryRow(context.Background(), `SELECT api_family FROM connections WHERE id = $1`, connectionID).Scan(&apiFamily); err != nil {
		t.Fatalf("load connection %d api_family: %v", connectionID, err)
	}
	if apiFamily != wantAPIFamily {
		t.Fatalf("expected connection %d api_family %q, got %q", connectionID, wantAPIFamily, apiFamily)
	}
}

func connectionOwnerScopedMutationDetail() string {
	return "terminal target mutations must use owner-scoped routes under /api/models/{model_config_id}/connections"
}

func assertStoredConnectionCount(t *testing.T, harness *contractHarness, connectionID int, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM connections WHERE id = $1`, connectionID).Scan(&count); err != nil {
		t.Fatalf("count connection %d: %v", connectionID, err)
	}
	if count != want {
		t.Fatalf("expected connection %d count %d, got %d", connectionID, want, count)
	}
}

func assertStoredModelAPIFamily(t *testing.T, harness *contractHarness, modelConfigID int, wantAPIFamily string) {
	t.Helper()
	var apiFamily string
	if err := harness.conn.QueryRow(context.Background(), `SELECT api_family FROM model_configs WHERE id = $1`, modelConfigID).Scan(&apiFamily); err != nil {
		t.Fatalf("load model %d api_family: %v", modelConfigID, err)
	}
	if apiFamily != wantAPIFamily {
		t.Fatalf("expected model %d api_family %q, got %q", modelConfigID, wantAPIFamily, apiFamily)
	}
}

func assertStoredConnectionContextCapabilities(t *testing.T, harness *contractHarness, connectionID int, wantContextWindowTokens *int, wantContextWindowTokensOverridden bool, wantDefaultOutputTokenReserve int, wantDefaultOutputTokenReserveOverridden bool, wantMaxContextUtilization float64, wantMaxContextUtilizationOverridden bool) {
	t.Helper()
	var contextWindowTokens sql.NullInt32
	var contextWindowTokensOverridden bool
	var defaultOutputTokenReserve int
	var defaultOutputTokenReserveOverridden bool
	var maxContextUtilization float64
	var maxContextUtilizationOverridden bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT context_window_tokens, context_window_tokens_overridden, default_output_token_reserve, default_output_token_reserve_overridden, max_context_utilization, max_context_utilization_overridden FROM connections WHERE id = $1`, connectionID).Scan(&contextWindowTokens, &contextWindowTokensOverridden, &defaultOutputTokenReserve, &defaultOutputTokenReserveOverridden, &maxContextUtilization, &maxContextUtilizationOverridden); err != nil {
		t.Fatalf("load connection %d context capabilities: %v", connectionID, err)
	}
	if wantContextWindowTokens == nil {
		if contextWindowTokens.Valid {
			t.Fatalf("expected connection %d context_window_tokens to be NULL, got %d", connectionID, contextWindowTokens.Int32)
		}
	} else if !contextWindowTokens.Valid || int(contextWindowTokens.Int32) != *wantContextWindowTokens {
		t.Fatalf("expected connection %d context_window_tokens %d, got %+v", connectionID, *wantContextWindowTokens, contextWindowTokens)
	}
	if contextWindowTokensOverridden != wantContextWindowTokensOverridden {
		t.Fatalf("expected connection %d context_window_tokens_overridden=%t, got %t", connectionID, wantContextWindowTokensOverridden, contextWindowTokensOverridden)
	}
	if defaultOutputTokenReserve != wantDefaultOutputTokenReserve || maxContextUtilization != wantMaxContextUtilization {
		t.Fatalf("expected connection %d reserve/utilization %d/%0.2f, got %d/%0.2f", connectionID, wantDefaultOutputTokenReserve, wantMaxContextUtilization, defaultOutputTokenReserve, maxContextUtilization)
	}
	if defaultOutputTokenReserveOverridden != wantDefaultOutputTokenReserveOverridden {
		t.Fatalf("expected connection %d default_output_token_reserve_overridden=%t, got %t", connectionID, wantDefaultOutputTokenReserveOverridden, defaultOutputTokenReserveOverridden)
	}
	if maxContextUtilizationOverridden != wantMaxContextUtilizationOverridden {
		t.Fatalf("expected connection %d max_context_utilization_overridden=%t, got %t", connectionID, wantMaxContextUtilizationOverridden, maxContextUtilizationOverridden)
	}
}

func assertContextCapabilityOverridesPayload(t *testing.T, payload map[string]any, wantContextWindowTokens *int, wantDefaultOutputTokenReserve *int, wantMaxContextUtilization *float64) {
	t.Helper()
	rawOverrides, ok := payload["context_capability_overrides"]
	if !ok {
		t.Fatalf("expected context_capability_overrides object, got %+v", payload)
	}
	overrides := asMap(t, rawOverrides)
	assertOptionalOverrideIntField(t, overrides, "context_window_tokens", wantContextWindowTokens)
	assertOptionalOverrideIntField(t, overrides, "default_output_token_reserve", wantDefaultOutputTokenReserve)
	assertOptionalOverrideFloatField(t, overrides, "max_context_utilization", wantMaxContextUtilization)
}

func assertNestedConnectionContextCapabilityPayload(t *testing.T, target map[string]any, connectionID int, wantEffectiveContextWindowTokens *int, wantEffectiveDefaultOutputTokenReserve int, wantEffectiveMaxContextUtilization float64, wantContextWindowTokens *int, wantDefaultOutputTokenReserve *int, wantMaxContextUtilization *float64) {
	t.Helper()
	connection := asMap(t, target["connection"])
	terminalTarget := asMap(t, target["terminal_target"])
	if jsonInt(t, connection["id"]) != connectionID || jsonInt(t, terminalTarget["id"]) != connectionID {
		t.Fatalf("expected nested connection and terminal_target id %d, got %+v", connectionID, target)
	}
	if wantEffectiveContextWindowTokens != nil {
		if jsonInt(t, connection["context_window_tokens"]) != *wantEffectiveContextWindowTokens || jsonInt(t, terminalTarget["context_window_tokens"]) != *wantEffectiveContextWindowTokens {
			t.Fatalf("expected effective context_window_tokens %d, got connection=%+v terminal_target=%+v", *wantEffectiveContextWindowTokens, connection, terminalTarget)
		}
	} else if connection["context_window_tokens"] != nil || terminalTarget["context_window_tokens"] != nil {
		t.Fatalf("expected effective context_window_tokens to be null, got connection=%+v terminal_target=%+v", connection, terminalTarget)
	}
	if jsonInt(t, connection["default_output_token_reserve"]) != wantEffectiveDefaultOutputTokenReserve || jsonInt(t, terminalTarget["default_output_token_reserve"]) != wantEffectiveDefaultOutputTokenReserve {
		t.Fatalf("expected effective default_output_token_reserve %d, got connection=%+v terminal_target=%+v", wantEffectiveDefaultOutputTokenReserve, connection, terminalTarget)
	}
	if jsonFloat(t, connection["max_context_utilization"]) != wantEffectiveMaxContextUtilization || jsonFloat(t, terminalTarget["max_context_utilization"]) != wantEffectiveMaxContextUtilization {
		t.Fatalf("expected effective max_context_utilization %0.2f, got connection=%+v terminal_target=%+v", wantEffectiveMaxContextUtilization, connection, terminalTarget)
	}
	assertContextCapabilityOverridesPayload(t, connection, wantContextWindowTokens, wantDefaultOutputTokenReserve, wantMaxContextUtilization)
	assertContextCapabilityOverridesPayload(t, terminalTarget, wantContextWindowTokens, wantDefaultOutputTokenReserve, wantMaxContextUtilization)
}

func assertOptionalOverrideIntField(t *testing.T, payload map[string]any, fieldName string, want *int) {
	t.Helper()
	value, ok := payload[fieldName]
	if !ok {
		t.Fatalf("expected override field %q in %+v", fieldName, payload)
	}
	if want == nil {
		if value != nil {
			t.Fatalf("expected override field %q to be null, got %+v", fieldName, value)
		}
		return
	}
	if jsonInt(t, value) != *want {
		t.Fatalf("expected override field %q=%d, got %+v", fieldName, *want, value)
	}
}

func assertOptionalOverrideFloatField(t *testing.T, payload map[string]any, fieldName string, want *float64) {
	t.Helper()
	value, ok := payload[fieldName]
	if !ok {
		t.Fatalf("expected override field %q in %+v", fieldName, payload)
	}
	if want == nil {
		if value != nil {
			t.Fatalf("expected override field %q to be null, got %+v", fieldName, value)
		}
		return
	}
	if jsonFloat(t, value) != *want {
		t.Fatalf("expected override field %q=%0.2f, got %+v", fieldName, *want, value)
	}
}

func assertConnectionNameCount(t *testing.T, harness *contractHarness, profileID int, name string, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM connections WHERE profile_id = $1 AND name = $2`, profileID, name).Scan(&count); err != nil {
		t.Fatalf("count connections named %q: %v", name, err)
	}
	if count != want {
		t.Fatalf("expected %d connections named %q, got %d", want, name, count)
	}
}

func assertModelConnectionTargetCount(t *testing.T, harness *contractHarness, modelConfigID int, want int) {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE source_model_config_id = $1 AND target_connection_id IS NOT NULL`, modelConfigID).Scan(&count); err != nil {
		t.Fatalf("count connection targets for model %d: %v", modelConfigID, err)
	}
	if count != want {
		t.Fatalf("expected %d connection targets for model %d, got %d", want, modelConfigID, count)
	}
}

func insertContractPricingTemplate(t *testing.T, harness *contractHarness, profileID int, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var templateID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, NULL, 'PER_1M', 'USD', '1', '2', '0', '0', '0', 1, $3, $3) RETURNING id`, profileID, name, now).Scan(&templateID); err != nil {
		t.Fatalf("insert pricing template %q: %v", name, err)
	}
	return templateID
}

func assertPricingTemplatePayloadPrices(t *testing.T, payload map[string]any, inputPrice string, outputPrice string, cachedInputPrice string, cacheCreationPrice string, reasoningPrice string) {
	t.Helper()
	if payload["input_price"] != inputPrice || payload["output_price"] != outputPrice || payload["cached_input_price"] != cachedInputPrice || payload["cache_creation_price"] != cacheCreationPrice || payload["reasoning_price"] != reasoningPrice {
		t.Fatalf("expected pricing fields input=%q output=%q cached_input=%q cache_creation=%q reasoning=%q, got %+v", inputPrice, outputPrice, cachedInputPrice, cacheCreationPrice, reasoningPrice, payload)
	}
}

func assertPricingTemplateStoredPrices(t *testing.T, harness *contractHarness, profileID int, name string, inputPrice string, outputPrice string, cachedInputPrice string, cacheCreationPrice string, reasoningPrice string) {
	t.Helper()
	var gotInputPrice string
	var gotOutputPrice string
	var gotCachedInputPrice string
	var gotCacheCreationPrice string
	var gotReasoningPrice string
	if err := harness.conn.QueryRow(context.Background(), `SELECT input_price, output_price, cached_input_price, cache_creation_price, reasoning_price FROM pricing_templates WHERE profile_id = $1 AND name = $2`, profileID, name).Scan(&gotInputPrice, &gotOutputPrice, &gotCachedInputPrice, &gotCacheCreationPrice, &gotReasoningPrice); err != nil {
		t.Fatalf("load pricing template %q prices: %v", name, err)
	}
	if gotInputPrice != inputPrice || gotOutputPrice != outputPrice || gotCachedInputPrice != cachedInputPrice || gotCacheCreationPrice != cacheCreationPrice || gotReasoningPrice != reasoningPrice {
		t.Fatalf("expected stored pricing fields input=%q output=%q cached_input=%q cache_creation=%q reasoning=%q, got input=%q output=%q cached_input=%q cache_creation=%q reasoning=%q", inputPrice, outputPrice, cachedInputPrice, cacheCreationPrice, reasoningPrice, gotInputPrice, gotOutputPrice, gotCachedInputPrice, gotCacheCreationPrice, gotReasoningPrice)
	}
}

func TestConnectionPreferredContext(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Preferred Connection Context Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "preferred-connection-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Preferred Connection Endpoint", 0)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET max_context_utilization = 0.80, preferred_context_utilization_threshold = 0.70 WHERE id = $1`, ownerModelID); err != nil {
		t.Fatalf("seed owner preferred context values: %v", err)
	}

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Preferred Context Connection"}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if jsonFloat(t, created["preferred_context_utilization_threshold"]) != 0.7 {
		t.Fatalf("expected inherited preferred_context_utilization_threshold=0.7, got %+v", created)
	}
	assertOptionalOverrideFloatField(t, asMap(t, created["context_capability_overrides"]), "preferred_context_utilization_threshold", nil)
	assertStoredConnectionPreferredContextThreshold(t, harness, connectionID, float64Ptr(0.7), false)

	overrideResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"preferred_context_utilization_threshold": 0.70}, modelHeader(defaultProfileID))
	assertStatus(t, overrideResponse, http.StatusOK)
	var overridden map[string]any
	decodeJSONResponse(t, overrideResponse, &overridden)
	if jsonFloat(t, overridden["preferred_context_utilization_threshold"]) != 0.7 {
		t.Fatalf("expected explicit same-as-owner preferred override to keep effective value, got %+v", overridden)
	}
	assertOptionalOverrideFloatField(t, asMap(t, overridden["context_capability_overrides"]), "preferred_context_utilization_threshold", float64Ptr(0.7))
	assertStoredConnectionPreferredContextThreshold(t, harness, connectionID, float64Ptr(0.7), true)

	resetResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"preferred_context_utilization_threshold": nil}, modelHeader(defaultProfileID))
	assertStatus(t, resetResponse, http.StatusOK)
	var reset map[string]any
	decodeJSONResponse(t, resetResponse, &reset)
	if jsonFloat(t, reset["preferred_context_utilization_threshold"]) != 0.7 {
		t.Fatalf("expected reset preferred_context_utilization_threshold to inherit owner value, got %+v", reset)
	}
	assertOptionalOverrideFloatField(t, asMap(t, reset["context_capability_overrides"]), "preferred_context_utilization_threshold", nil)
	assertStoredConnectionPreferredContextThreshold(t, harness, connectionID, float64Ptr(0.7), false)

	invalidZero := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "preferred_context_utilization_threshold": 0}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalidZero, http.StatusUnprocessableEntity, "preferred_context_utilization_threshold must be greater than 0 and less than or equal to 1 when provided")

	invalidHigh := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "preferred_context_utilization_threshold": 1.1}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalidHigh, http.StatusUnprocessableEntity, "preferred_context_utilization_threshold must be greater than 0 and less than or equal to 1 when provided")

	invalidCrossField := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "max_context_utilization": 0.60, "preferred_context_utilization_threshold": 0.70}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalidCrossField, http.StatusUnprocessableEntity, "preferred_context_utilization_threshold must be less than or equal to max_context_utilization when provided")

	invalidLowerMax := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"max_context_utilization": 0.60}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalidLowerMax, http.StatusUnprocessableEntity, "preferred_context_utilization_threshold must be less than or equal to max_context_utilization when provided")
}

func TestConnectionCapabilitySnapshotsExposePreferredThresholdAndOpenAITextCapability(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Connection Capability Snapshot Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "connection-capability-snapshot-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Capability Snapshot Endpoint", 0)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET max_context_utilization = 0.80, preferred_context_utilization_threshold = 0.70 WHERE id = $1`, ownerModelID); err != nil {
		t.Fatalf("seed owner model capability snapshot defaults: %v", err)
	}

	missingCapability := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "name": "Missing Capability Connection"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, missingCapability, http.StatusUnprocessableEntity, "openai_text_capability is required for OpenAI-family connections")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Capability Snapshot Connection"}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if jsonFloat(t, created["preferred_context_utilization_threshold"]) != 0.7 || created["openai_probe_endpoint_variant"] != "responses_minimal" || created["openai_text_capability"] != "responses_only" {
		t.Fatalf("expected created connection to expose inherited preferred threshold and explicit OpenAI text capability, got %+v", created)
	}
	if _, ok := created["openai_upstream_operation"]; ok {
		t.Fatalf("created connection must not expose removed openai_upstream_operation, got %+v", created)
	}

	updateResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"openai_probe_endpoint_variant": "chat_completions_reasoning_none"}, modelHeader(defaultProfileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updateResponse, &updated)
	if jsonFloat(t, updated["preferred_context_utilization_threshold"]) != 0.7 || updated["openai_probe_endpoint_variant"] != "chat_completions_reasoning_none" || updated["openai_text_capability"] != "responses_only" {
		t.Fatalf("expected updated connection to preserve text capability independently of probe variant, got %+v", updated)
	}
	if _, ok := updated["openai_upstream_operation"]; ok {
		t.Fatalf("updated connection must not expose removed openai_upstream_operation, got %+v", updated)
	}

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d", ownerModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detail map[string]any
	decodeJSONResponse(t, detailResponse, &detail)
	accessTargets := detail["access_targets"].([]any)
	if len(accessTargets) != 1 {
		t.Fatalf("expected one access target in model detail, got %+v", detail)
	}
	target := asMap(t, accessTargets[0])
	for _, nested := range []struct {
		name    string
		payload map[string]any
	}{
		{name: "connection", payload: asMap(t, target["connection"])},
		{name: "terminal_target", payload: asMap(t, target["terminal_target"])},
	} {
		if jsonInt(t, nested.payload["id"]) != connectionID {
			t.Fatalf("expected nested %s id %d, got %+v", nested.name, connectionID, nested.payload)
		}
		if jsonFloat(t, nested.payload["preferred_context_utilization_threshold"]) != 0.7 {
			t.Fatalf("expected nested %s preferred_context_utilization_threshold=0.7, got %+v", nested.name, nested.payload)
		}
		if nested.payload["openai_probe_endpoint_variant"] != "chat_completions_reasoning_none" || nested.payload["openai_text_capability"] != "responses_only" {
			t.Fatalf("expected nested %s to expose OpenAI text capability independently of probe variant, got %+v", nested.name, nested.payload)
		}
		if _, ok := nested.payload["openai_upstream_operation"]; ok {
			t.Fatalf("nested %s must not expose removed openai_upstream_operation, got %+v", nested.name, nested.payload)
		}
	}
}

func TestConnectionProbeEndpointVariantRejectsNonOpenAIFamily(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "anthropic")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Connection Non-OpenAI Probe Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "anthropic", "connection-non-openai-probe-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Non-OpenAI Probe Endpoint", 0)

	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "openai_probe_endpoint_variant": "responses_minimal"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, response, http.StatusUnprocessableEntity, "openai_probe_endpoint_variant is only supported for OpenAI-family connections")
}

func assertStoredConnectionPreferredContextThreshold(t *testing.T, harness *contractHarness, connectionID int, want *float64, wantOverridden bool) {
	t.Helper()
	var preferred sql.NullFloat64
	var overridden bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT preferred_context_utilization_threshold, preferred_context_utilization_threshold_overridden FROM connections WHERE id = $1`, connectionID).Scan(&preferred, &overridden); err != nil {
		t.Fatalf("load connection %d preferred_context_utilization_threshold: %v", connectionID, err)
	}
	if want == nil {
		if preferred.Valid {
			t.Fatalf("expected connection %d preferred_context_utilization_threshold NULL, got %0.2f", connectionID, preferred.Float64)
		}
	} else if !preferred.Valid || preferred.Float64 != *want {
		t.Fatalf("expected connection %d preferred_context_utilization_threshold %0.2f, got %+v", connectionID, *want, preferred)
	}
	if overridden != wantOverridden {
		t.Fatalf("expected connection %d preferred_context_utilization_threshold_overridden=%t, got %t", connectionID, wantOverridden, overridden)
	}
}

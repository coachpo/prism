package contract_test

import (
	"context"
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

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": ownerEndpointID, "openai_text_capability": "responses_only", "is_active": true, "name": "Owner Private Connection"}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if jsonInt(t, created["model_config_id"]) != ownerModelID || created["api_family"] != "openai" || jsonInt(t, created["endpoint_id"]) != ownerEndpointID || jsonInt(t, created["priority"]) != 0 || created["health_status"] != "unknown" {
		t.Fatalf("expected owner-scoped created connection payload, got %+v", created)
	}
	if jsonInt(t, asMap(t, created["endpoint"])["id"]) != ownerEndpointID {
		t.Fatalf("expected owner-scoped create to hydrate endpoint, got %+v", created)
	}
	assertConnectionOwnerTarget(t, harness, ownerModelID, connectionID, 0, true)
	assertStoredConnectionAPIFamily(t, harness, connectionID, "openai")

	otherCreateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", otherModelID), map[string]any{"endpoint_id": otherEndpointID, "openai_text_capability": "responses_only", "name": "Other Private Connection"}, modelHeader(defaultProfileID))
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
		{method: http.MethodPatch, path: fmt.Sprintf("/api/models/%d/connections/%d/priority", modelConfigID, connectionID), body: map[string]any{"to_index": 0}},
	} {
		response := harness.requestJSON(t, harness.client, testCase.method, testCase.path, testCase.body, modelHeader(defaultProfileID))
		assertErrorResponse(t, response, http.StatusBadRequest, connectionOwnerScopedMutationDetail())
	}
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

func TestConnectionCapabilitySnapshotsExposeOpenAITextCapability(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Connection Capability Snapshot Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "connection-capability-snapshot-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Capability Snapshot Endpoint", 0)

	missingCapability := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "name": "Missing Capability Connection"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, missingCapability, http.StatusUnprocessableEntity, "openai_text_capability is required for OpenAI-family connections")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "name": "Capability Snapshot Connection"}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if created["openai_text_capability"] != "responses_only" {
		t.Fatalf("expected created connection to expose explicit OpenAI text capability, got %+v", created)
	}
	if _, ok := created["openai_probe_endpoint_variant"]; ok {
		t.Fatalf("created connection must not expose removed openai_probe_endpoint_variant, got %+v", created)
	}
	if _, ok := created["openai_upstream_operation"]; ok {
		t.Fatalf("created connection must not expose removed openai_upstream_operation, got %+v", created)
	}

	removedProbeUpdateResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"openai_probe_endpoint_variant": "chat_completions_reasoning_none"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, removedProbeUpdateResponse, http.StatusBadRequest, "Invalid request body")

	updateResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID), map[string]any{"openai_text_capability": "chat_completions_only"}, modelHeader(defaultProfileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updateResponse, &updated)
	if updated["openai_text_capability"] != "chat_completions_only" {
		t.Fatalf("expected updated connection to expose text capability, got %+v", updated)
	}
	if _, ok := updated["openai_probe_endpoint_variant"]; ok {
		t.Fatalf("updated connection must not expose removed openai_probe_endpoint_variant, got %+v", updated)
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
		if nested.payload["openai_text_capability"] != "chat_completions_only" {
			t.Fatalf("expected nested %s to expose OpenAI text capability, got %+v", nested.name, nested.payload)
		}
		if _, ok := nested.payload["openai_probe_endpoint_variant"]; ok {
			t.Fatalf("nested %s must not expose removed openai_probe_endpoint_variant, got %+v", nested.name, nested.payload)
		}
		if _, ok := nested.payload["openai_upstream_operation"]; ok {
			t.Fatalf("nested %s must not expose removed openai_upstream_operation, got %+v", nested.name, nested.payload)
		}
	}
}

func TestConnectionProbeEndpointVariantIsRemovedFromWrites(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Connection Removed Probe Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "connection-removed-probe-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Removed Probe Endpoint", 0)

	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "responses_only", "openai_probe_endpoint_variant": "responses_minimal"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, response, http.StatusBadRequest, "Invalid request body")
}

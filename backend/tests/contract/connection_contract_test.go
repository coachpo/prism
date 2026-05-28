package contract_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestConnectionStandaloneCRUD(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Endpoint B", 1)
	pricingTemplateID := insertContractPricingTemplate(t, harness, defaultProfileID, "S9 Connection Pricing")

	priorityOnCreate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/connections", map[string]any{"api_family": "openai", "endpoint_id": endpointAID, "priority": 0}, modelHeader(defaultProfileID))
	assertErrorResponse(t, priorityOnCreate, http.StatusUnprocessableEntity, "priority is not allowed on create")
	missingFamily := harness.requestJSON(t, harness.client, http.MethodPost, "/api/connections", map[string]any{"endpoint_id": endpointAID}, modelHeader(defaultProfileID))
	assertErrorResponse(t, missingFamily, http.StatusBadRequest, "api_family is required")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/connections", map[string]any{"api_family": "openai", "endpoint_id": endpointAID, "is_active": true, "name": "Primary Connection", "auth_type": "openai", "custom_headers": map[string]string{"x-test": "1"}, "qps_limit": 3}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if _, ok := created["model_config_id"]; ok {
		t.Fatalf("standalone connection payload must not expose model ownership, got %+v", created)
	}
	if created["api_family"] != "openai" || jsonInt(t, created["endpoint_id"]) != endpointAID || jsonInt(t, created["priority"]) != 0 || created["health_status"] != "unknown" {
		t.Fatalf("expected created standalone connection payload, got %+v", created)
	}
	if asMap(t, created["custom_headers"])["x-test"] != "1" || created["openai_probe_endpoint_variant"] != "responses_minimal" {
		t.Fatalf("expected created connection to preserve headers and default OpenAI probe variant, got %+v", created)
	}

	inlineCreate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/connections", map[string]any{"api_family": "openai", "endpoint_create": map[string]any{"name": "Inline Endpoint", "base_url": "https://inline.invalid/", "api_key": "sk-inline"}, "is_active": true, "name": "Inline Connection", "openai_probe_endpoint_variant": "chat_completions_minimal"}, modelHeader(defaultProfileID))
	assertStatus(t, inlineCreate, http.StatusCreated)
	var inlineCreated map[string]any
	decodeJSONResponse(t, inlineCreate, &inlineCreated)
	inlineConnectionID := jsonInt(t, inlineCreated["id"])
	if created["id"] == inlineCreated["id"] || asMap(t, inlineCreated["endpoint"])["name"] != "Inline Endpoint" || inlineCreated["openai_probe_endpoint_variant"] != "chat_completions_minimal" {
		t.Fatalf("expected inline standalone connection creation payload, got %+v", inlineCreated)
	}

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/connections", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed) != 2 || jsonInt(t, listed[0]["id"]) != connectionID || jsonInt(t, listed[1]["id"]) != inlineConnectionID {
		t.Fatalf("expected standalone connection list ordered by id, got %+v", listed)
	}

	pricingResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/connections/%d/pricing-template", connectionID), map[string]any{"pricing_template_id": pricingTemplateID}, modelHeader(defaultProfileID))
	assertStatus(t, pricingResponse, http.StatusOK)
	var priced map[string]any
	decodeJSONResponse(t, pricingResponse, &priced)
	if jsonInt(t, priced["pricing_template_id"]) != pricingTemplateID || jsonInt(t, asMap(t, priced["pricing_template"])["id"]) != pricingTemplateID {
		t.Fatalf("expected pricing-template assignment payload, got %+v", priced)
	}

	priorityOnUpdate := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/connections/%d", connectionID), map[string]any{"priority": 0}, modelHeader(defaultProfileID))
	assertErrorResponse(t, priorityOnUpdate, http.StatusUnprocessableEntity, "priority is not allowed on update")
	updateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/connections/%d", connectionID), map[string]any{"endpoint_id": endpointBID, "is_active": false, "custom_headers": map[string]string{}, "openai_probe_endpoint_variant": "responses_reasoning_none"}, modelHeader(defaultProfileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updateResponse, &updated)
	if jsonInt(t, updated["endpoint_id"]) != endpointBID || updated["is_active"] != false || updated["custom_headers"] != nil || updated["openai_probe_endpoint_variant"] != "responses_reasoning_none" {
		t.Fatalf("expected connection update to move endpoints and clear empty headers, got %+v", updated)
	}

	deleteInline := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/connections/%d", inlineConnectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteInline, http.StatusOK)
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
	connectionAID := modelInsertStandaloneConnection(t, harness, defaultProfileID, "openai", endpointAID, 0, true, nil)
	connectionBID := modelInsertStandaloneConnection(t, harness, defaultProfileID, "openai", endpointBID, 0, true, nil)

	createConnectionTarget := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", sourceModelID), map[string]any{"target_type": "connection", "connection_id": connectionAID, "position": 0, "is_enabled": true}, modelHeader(defaultProfileID))
	assertStatus(t, createConnectionTarget, http.StatusCreated)
	var targets []map[string]any
	decodeJSONResponse(t, createConnectionTarget, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "connection", ConnectionID: connectionAID, Position: 0, IsEnabled: true}})
	connectionTargetID := jsonInt(t, targets[0]["id"])

	createModelTarget := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", sourceModelID), map[string]any{"target_type": "model", "target_model_id": "s9-target-route-model", "position": 1, "is_enabled": false}, modelHeader(defaultProfileID))
	assertStatus(t, createModelTarget, http.StatusCreated)
	decodeJSONResponse(t, createModelTarget, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "connection", ConnectionID: connectionAID, Position: 0, IsEnabled: true}, {TargetType: "model", TargetModelID: "s9-target-route-model", Position: 1, IsEnabled: false}})
	connectionTargetID = jsonInt(t, targets[0]["id"])
	modelTargetID := jsonInt(t, targets[1]["id"])

	updateConnectionTarget := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, connectionTargetID), map[string]any{"connection_id": connectionBID, "is_enabled": true}, modelHeader(defaultProfileID))
	assertStatus(t, updateConnectionTarget, http.StatusOK)
	decodeJSONResponse(t, updateConnectionTarget, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "connection", ConnectionID: connectionBID, Position: 0, IsEnabled: true}, {TargetType: "model", TargetModelID: "s9-target-route-model", Position: 1, IsEnabled: false}})
	connectionTargetID = jsonInt(t, targets[0]["id"])
	modelTargetID = jsonInt(t, targets[1]["id"])

	reorderResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/targets/%d/position", sourceModelID, modelTargetID), map[string]any{"to_index": 0}, modelHeader(defaultProfileID))
	assertStatus(t, reorderResponse, http.StatusOK)
	decodeJSONResponse(t, reorderResponse, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "model", TargetModelID: "s9-target-route-model", Position: 0, IsEnabled: false}, {TargetType: "connection", ConnectionID: connectionBID, Position: 1, IsEnabled: true}})
	connectionTargetID = jsonInt(t, targets[1]["id"])

	deleteModelTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, jsonInt(t, targets[0]["id"])), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteModelTarget, http.StatusOK)
	decodeJSONResponse(t, deleteModelTarget, &targets)
	assertTargetRouteOrder(t, targets, []expectedAccessTarget{{TargetType: "connection", ConnectionID: connectionBID, Position: 0, IsEnabled: true}})
	connectionTargetID = jsonInt(t, targets[0]["id"])

	deleteLastEnabled := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", sourceModelID, connectionTargetID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, deleteLastEnabled, http.StatusBadRequest, "enabled models must include at least one enabled access target")
	_ = targetModelID
}

func TestDeleteReferencedConnection(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Delete Referenced Connection Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-delete-referenced-connection", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Delete Referenced Endpoint", 0)
	connectionID := modelInsertStandaloneConnection(t, harness, defaultProfileID, "openai", endpointID, 0, true, nil)

	attachResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/targets", modelConfigID), map[string]any{"target_type": "connection", "connection_id": connectionID, "position": 0, "is_enabled": true}, modelHeader(defaultProfileID))
	assertStatus(t, attachResponse, http.StatusCreated)
	var targets []map[string]any
	decodeJSONResponse(t, attachResponse, &targets)
	targetID := jsonInt(t, targets[0]["id"])

	referencesResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/references", connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, referencesResponse, http.StatusOK)
	var references map[string]any
	decodeJSONResponse(t, referencesResponse, &references)
	items := references["items"].([]any)
	if len(items) != 1 || jsonInt(t, asMap(t, items[0])["model_config_id"]) != modelConfigID || asMap(t, items[0])["model_id"] != "s9-delete-referenced-connection" {
		t.Fatalf("expected connection references to expose attached model target, got %+v", references)
	}

	deleteReferenced := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/connections/%d", connectionID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, deleteReferenced, http.StatusConflict, "Cannot delete: models [s9-delete-referenced-connection] target this connection")

	detachResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", modelConfigID, targetID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, detachResponse, http.StatusBadRequest, "enabled models must include at least one enabled access target")
	disableModel := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{"is_enabled": false}, modelHeader(defaultProfileID))
	assertStatus(t, disableModel, http.StatusOK)
	targetID = modelLoadConnectionTargetID(t, harness, modelConfigID, connectionID)
	detachResponse = harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", modelConfigID, targetID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detachResponse, http.StatusOK)
	deleteDetached := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/connections/%d", connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteDetached, http.StatusOK)
}

func TestLegacyModelOwnedConnectionRoute(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Legacy Route Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-legacy-route-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Legacy Route Endpoint", 0)
	connectionID := modelInsertStandaloneConnection(t, harness, defaultProfileID, "openai", endpointID, 0, true, nil)

	legacyList := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/connections", modelConfigID), nil, modelHeader(defaultProfileID))
	assertStatus(t, legacyList, http.StatusNotFound)
	legacyCreate := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_id": endpointID}, modelHeader(defaultProfileID))
	assertStatus(t, legacyCreate, http.StatusNotFound)
	legacyReorder := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d/priority", modelConfigID, connectionID), map[string]any{"to_index": 0}, modelHeader(defaultProfileID))
	assertStatus(t, legacyReorder, http.StatusNotFound)
	legacyPreview := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections/health-check-preview", modelConfigID), map[string]any{"endpoint_id": endpointID}, modelHeader(defaultProfileID))
	assertStatus(t, legacyPreview, http.StatusNotFound)
	legacyOwner := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/owner", connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, legacyOwner, http.StatusNotFound)
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


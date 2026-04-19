package contract_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestConnectionOwners(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S9 Connection Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s9-connection-model", nil, "native", &strategyID, true)
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Endpoint A", 0)
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "Connection Endpoint B", 1)
	pricingTemplateID := insertContractPricingTemplate(t, harness, defaultProfileID, "S9 Connection Pricing")

	priorityOnCreate := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_id": endpointAID, "priority": 0}, modelHeader(defaultProfileID))
	assertErrorResponse(t, priorityOnCreate, http.StatusUnprocessableEntity, "priority is not allowed on create")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_id": endpointAID, "is_active": true, "name": "Primary Connection", "auth_type": "openai", "custom_headers": map[string]string{"x-test": "1"}, "qps_limit": 3}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	if jsonInt(t, created["endpoint_id"]) != endpointAID || jsonInt(t, created["priority"]) != 0 || created["health_status"] != "unknown" {
		t.Fatalf("expected created connection payload with appended priority, got %+v", created)
	}
	if asMap(t, created["custom_headers"])["x-test"] != "1" || created["openai_probe_endpoint_variant"] != "responses_minimal" {
		t.Fatalf("expected created connection to preserve headers and default OpenAI probe variant, got %+v", created)
	}

	inlineCreate := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{"endpoint_create": map[string]any{"name": "Inline Endpoint", "base_url": "https://inline.invalid/", "api_key": "sk-inline"}, "is_active": true, "name": "Inline Connection", "openai_probe_endpoint_variant": "chat_completions_minimal"}, modelHeader(defaultProfileID))
	assertStatus(t, inlineCreate, http.StatusCreated)
	var inlineCreated map[string]any
	decodeJSONResponse(t, inlineCreate, &inlineCreated)
	inlineConnectionID := jsonInt(t, inlineCreated["id"])
	if jsonInt(t, inlineCreated["priority"]) != 1 || created["id"] == inlineCreated["id"] {
		t.Fatalf("expected inline connection create to append after the first connection, got %+v", inlineCreated)
	}
	if asMap(t, inlineCreated["endpoint"])["name"] != "Inline Endpoint" || inlineCreated["openai_probe_endpoint_variant"] != "chat_completions_minimal" {
		t.Fatalf("expected inline endpoint creation payload, got %+v", inlineCreated)
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

	reorderResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d/priority", modelConfigID, inlineConnectionID), map[string]any{"to_index": 0}, modelHeader(defaultProfileID))
	assertStatus(t, reorderResponse, http.StatusOK)
	var reordered []map[string]any
	decodeJSONResponse(t, reorderResponse, &reordered)
	if jsonInt(t, reordered[0]["id"]) != inlineConnectionID || jsonInt(t, reordered[0]["priority"]) != 0 || jsonInt(t, reordered[1]["id"]) != connectionID || jsonInt(t, reordered[1]["priority"]) != 1 {
		t.Fatalf("expected connection reorder to rewrite contiguous priorities, got %+v", reordered)
	}

	deleteInline := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/connections/%d", inlineConnectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteInline, http.StatusOK)
	listAfterDelete := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/connections", modelConfigID), nil, modelHeader(defaultProfileID))
	assertStatus(t, listAfterDelete, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listAfterDelete, &listed)
	if len(listed) != 1 || jsonInt(t, listed[0]["id"]) != connectionID || jsonInt(t, listed[0]["priority"]) != 0 {
		t.Fatalf("expected delete to compact remaining connection priorities, got %+v", listed)
	}

	ownerResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/owner", connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, ownerResponse, http.StatusOK)
	var owner map[string]any
	decodeJSONResponse(t, ownerResponse, &owner)
	if jsonInt(t, owner["connection_id"]) != connectionID || jsonInt(t, owner["model_config_id"]) != modelConfigID || owner["model_id"] != "s9-connection-model" || jsonInt(t, owner["endpoint_id"]) != endpointBID || owner["endpoint_name"] != "Connection Endpoint B" || owner["endpoint_base_url"] != "https://connection-endpoint-b.invalid" {
		t.Fatalf("expected owner lookup payload shape, got %+v", owner)
	}

	dropConnectionEndpointConstraint(t, harness)
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM endpoints WHERE id = $1`, endpointBID); err != nil {
		t.Fatalf("delete endpoint after dropping connection fk: %v", err)
	}
	missingOwner := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d/owner", connectionID), nil, modelHeader(defaultProfileID))
	assertErrorResponse(t, missingOwner, http.StatusBadRequest, "Connection endpoint is missing")
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

func insertContractPricingTemplate(t *testing.T, harness *contractHarness, profileID int, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var templateID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, NULL, 'PER_1M', 'USD', '1', '2', NULL, NULL, NULL, 1, $3, $3) RETURNING id`, profileID, name, now).Scan(&templateID); err != nil {
		t.Fatalf("insert pricing template %q: %v", name, err)
	}
	return templateID
}

func dropConnectionEndpointConstraint(t *testing.T, harness *contractHarness) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `ALTER TABLE connections DROP CONSTRAINT connections_endpoint_id_fkey`); err != nil {
		t.Fatalf("drop connections_endpoint_id_fkey: %v", err)
	}
}

package contracttest

import (
	"fmt"
	"net/http"
	"testing"
)

func TestConnectionCustomRequestParametersCreateSemantics(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Custom Request Parameters Create Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "custom-request-params-create-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Custom Request Parameters Create Endpoint")

	for _, testCase := range []struct {
		name         string
		body         map[string]any
		wantStatus   int
		wantNull     bool
		wantProvider string
	}{
		{
			name:       "missing field normalizes to null",
			body:       map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native"},
			wantStatus: http.StatusCreated,
			wantNull:   true,
		},
		{
			name:       "null field normalizes to null",
			body:       map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "custom_request_parameters": nil},
			wantStatus: http.StatusCreated,
			wantNull:   true,
		},
		{
			name:       "empty object normalizes to null",
			body:       map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "custom_request_parameters": map[string]any{}},
			wantStatus: http.StatusCreated,
			wantNull:   true,
		},
		{
			name:         "valid object round-trips",
			body:         map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "custom_request_parameters": map[string]any{"provider": map[string]any{"only": []string{"deepinfra/turbo"}, "allow_fallbacks": false}, "temperature": nil}},
			wantStatus:   http.StatusCreated,
			wantNull:     false,
			wantProvider: "deepinfra/turbo",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), testCase.body, modelHeader(defaultProfileID))
			assertStatus(t, response, testCase.wantStatus)
			if testCase.wantStatus != http.StatusCreated {
				return
			}
			var payload map[string]any
			decodeJSONResponse(t, response, &payload)
			connectionID := jsonInt(t, payload["id"])
			if testCase.wantNull {
				if value, present := payload["custom_request_parameters"]; !present || value != nil {
					t.Fatalf("expected custom_request_parameters to be present and null, got %v present=%v", value, present)
				}
			} else {
				params := asMap(t, payload["custom_request_parameters"])
				provider := asMap(t, params["provider"])
				if jsonStringList(t, provider["only"])[0] != testCase.wantProvider || provider["allow_fallbacks"] != false {
					t.Fatalf("unexpected custom request parameters round-trip: %+v", payload["custom_request_parameters"])
				}
				if value, present := params["temperature"]; !present || value != nil {
					t.Fatalf("expected literal null temperature to round-trip, got %v present=%v", value, present)
				}
			}

			fetched := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d/connections", ownerModelID), nil, modelHeader(defaultProfileID))
			assertStatus(t, fetched, http.StatusOK)
			var list []map[string]any
			decodeJSONResponse(t, fetched, &list)
			for _, item := range list {
				if jsonInt(t, item["id"]) == connectionID {
					if testCase.wantNull && item["custom_request_parameters"] != nil {
						t.Fatalf("expected stable null on list read, got %+v", item["custom_request_parameters"])
					}
					if !testCase.wantNull && item["custom_request_parameters"] == nil {
						t.Fatalf("expected object on list read, got nil")
					}
				}
			}
		})
	}
}

func TestConnectionCustomRequestParametersValidationErrors(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Custom Request Parameters Validation Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "custom-request-params-validation-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Custom Request Parameters Validation Endpoint")

	createBase := func(fieldValue any) map[string]any {
		return map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "custom_request_parameters": fieldValue}
	}

	tests := []struct {
		name       string
		fieldValue any
		wantStatus int
		wantDetail string
		wantField  string
		wantPath   string
		wantReason string
		wantLimit  any
	}{
		{name: "array root", fieldValue: []any{1, 2}, wantStatus: http.StatusUnprocessableEntity, wantDetail: "Invalid custom request parameters", wantField: "custom_request_parameters", wantPath: "custom_request_parameters", wantReason: "not_object"},
		{name: "string root", fieldValue: "not-an-object", wantStatus: http.StatusUnprocessableEntity, wantDetail: "Invalid custom request parameters", wantField: "custom_request_parameters", wantPath: "custom_request_parameters", wantReason: "not_object"},
		{name: "protected model", fieldValue: map[string]any{"model": "x"}, wantStatus: http.StatusUnprocessableEntity, wantDetail: "Invalid custom request parameters", wantField: "custom_request_parameters", wantPath: "custom_request_parameters.model", wantReason: "protected_field"},
		{name: "protected stream", fieldValue: map[string]any{"stream": true}, wantStatus: http.StatusUnprocessableEntity, wantDetail: "Invalid custom request parameters", wantField: "custom_request_parameters", wantPath: "custom_request_parameters.stream", wantReason: "protected_field"},
		{name: "protected messages", fieldValue: map[string]any{"messages": []any{}}, wantStatus: http.StatusUnprocessableEntity, wantDetail: "Invalid custom request parameters", wantField: "custom_request_parameters", wantPath: "custom_request_parameters.messages", wantReason: "protected_field"},
		{name: "protected systemInstruction", fieldValue: map[string]any{"systemInstruction": "x"}, wantStatus: http.StatusUnprocessableEntity, wantDetail: "Invalid custom request parameters", wantField: "custom_request_parameters", wantPath: "custom_request_parameters.systemInstruction", wantReason: "protected_field"},
		{name: "number out of range", fieldValue: map[string]any{"n": 9007199254740992}, wantStatus: http.StatusUnprocessableEntity, wantDetail: "Invalid custom request parameters", wantField: "custom_request_parameters", wantPath: "custom_request_parameters.n", wantReason: "number_out_of_range"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), createBase(testCase.fieldValue), modelHeader(defaultProfileID))
			assertErrorResponse(t, response, testCase.wantStatus, testCase.wantDetail)
			payload := decodeErrorResponseMap(t, response)
			if payload["field"] != testCase.wantField || payload["path"] != testCase.wantPath || payload["reason"] != testCase.wantReason {
				t.Fatalf("unexpected validation envelope: %+v", payload)
			}
			if testCase.wantLimit != nil && jsonInt(t, payload["limit"]) != testCase.wantLimit {
				t.Fatalf("unexpected limit %v, want %v", payload["limit"], testCase.wantLimit)
			}
		})
	}

	// Malformed JSON for the field fails the whole body as 400.
	rawBody := `{"endpoint_id": ` + fmt.Sprint(endpointID) + `, "openai_text_capability": "dual_native", "custom_request_parameters": {"provider": }`
	response := harness.requestJSONRaw(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), rawBody, modelHeader(defaultProfileID))
	assertErrorResponse(t, response, http.StatusBadRequest, "Invalid request body")

	// Unknown top-level fields still fail as 400.
	unknown := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "custom_request_parameters_typo": map[string]any{"a": 1}}, modelHeader(defaultProfileID))
	assertErrorResponse(t, unknown, http.StatusBadRequest, "Invalid request body")

	// Failed creates must not leave partial rows.
	assertConnectionNameCount(t, harness, defaultProfileID, "custom-request-params-validation-owner", 0)
}

func TestConnectionCustomRequestParametersPatchSemantics(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Custom Request Parameters Patch Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "custom-request-params-patch-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Custom Request Parameters Patch Endpoint")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "custom_request_parameters": map[string]any{"provider": map[string]any{"only": []string{"deepinfra/turbo"}}, "temperature": 0.5}}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	connectionID := jsonInt(t, created["id"])
	patchPath := fmt.Sprintf("/api/models/%d/connections/%d", ownerModelID, connectionID)
	readPath := fmt.Sprintf("/api/connections/%d", connectionID)

	readParams := func() map[string]any {
		t.Helper()
		response := harness.requestJSON(t, harness.client, http.MethodGet, readPath, nil, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusOK)
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		if payload["custom_request_parameters"] == nil {
			return map[string]any{}
		}
		return asMap(t, payload["custom_request_parameters"])
	}

	// PATCH with missing field keeps the current value.
	keep := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath, map[string]any{"name": "renamed"}, modelHeader(defaultProfileID))
	assertStatus(t, keep, http.StatusOK)
	if params := readParams(); jsonStringList(t, asMap(t, params["provider"])["only"])[0] != "deepinfra/turbo" {
		t.Fatalf("expected PATCH omission to keep current parameters, got %+v", params)
	}

	// PATCH with null clears.
	clear := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath, map[string]any{"custom_request_parameters": nil}, modelHeader(defaultProfileID))
	assertStatus(t, clear, http.StatusOK)
	if params := readParams(); len(params) != 0 {
		t.Fatalf("expected PATCH null to clear parameters, got %+v", params)
	}

	// PATCH with {} clears.
	clearEmpty := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath, map[string]any{"custom_request_parameters": map[string]any{}}, modelHeader(defaultProfileID))
	assertStatus(t, clearEmpty, http.StatusOK)
	if params := readParams(); len(params) != 0 {
		t.Fatalf("expected PATCH empty object to clear parameters, got %+v", params)
	}

	// PATCH with a valid object replaces wholesale (old member must not survive).
	replace := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath, map[string]any{"custom_request_parameters": map[string]any{"provider": map[string]any{"order": []string{"deepinfra/turbo"}, "allow_fallbacks": false}}}, modelHeader(defaultProfileID))
	assertStatus(t, replace, http.StatusOK)
	replaced := readParams()
	provider := asMap(t, replaced["provider"])
	if jsonStringList(t, provider["order"])[0] != "deepinfra/turbo" || provider["allow_fallbacks"] != false {
		t.Fatalf("expected PATCH object to replace wholesale, got %+v", replaced)
	}
	if _, hasTemperature := replaced["temperature"]; hasTemperature {
		t.Fatalf("expected old temperature member to be removed by whole-value replace, got %+v", replaced)
	}

	// Invalid PATCH fails atomically and keeps the stored value.
	invalid := harness.requestJSON(t, harness.client, http.MethodPatch, patchPath, map[string]any{"name": "should-not-persist", "custom_request_parameters": []any{1}}, modelHeader(defaultProfileID))
	assertErrorResponse(t, invalid, http.StatusUnprocessableEntity, "Invalid custom request parameters")
	afterInvalid := readParams()
	afterInvalidProvider := asMap(t, afterInvalid["provider"])
	if jsonStringList(t, afterInvalidProvider["order"])[0] != "deepinfra/turbo" {
		t.Fatalf("expected failed PATCH to keep the stored parameters, got %+v", afterInvalid)
	}
	readBack := harness.requestJSON(t, harness.client, http.MethodGet, readPath, nil, modelHeader(defaultProfileID))
	var readPayload map[string]any
	decodeJSONResponse(t, readBack, &readPayload)
	if readPayload["name"] == "should-not-persist" {
		t.Fatalf("expected failed PATCH not to persist unrelated fields, got %+v", readPayload["name"])
	}
}

func TestConnectionCustomRequestParametersNestedTargetSummary(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Custom Request Parameters Nested Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "custom-request-params-nested-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Custom Request Parameters Nested Endpoint")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "custom_request_parameters": map[string]any{"provider": map[string]any{"only": []string{"google-vertex/us-east5"}}}}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)

	// Nested model access-target read exposes connectionTargetSummary with the field.
	modelsResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/models/%d", ownerModelID), nil, modelHeader(defaultProfileID))
	assertStatus(t, modelsResponse, http.StatusOK)
	var modelPayload map[string]any
	decodeJSONResponse(t, modelsResponse, &modelPayload)
	found := false
	for _, rawTarget := range modelPayload["access_targets"].([]any) {
		target := asMap(t, rawTarget)
		if target["target_type"] == "connection" {
			connection := asMap(t, target["connection"])
			if connection == nil {
				t.Fatalf("expected nested connection target summary, got %+v", target)
			}
			params := asMap(t, connection["custom_request_parameters"])
			provider := asMap(t, params["provider"])
			if jsonStringList(t, provider["only"])[0] != "google-vertex/us-east5" {
				t.Fatalf("expected nested target summary to expose custom request parameters, got %+v", connection["custom_request_parameters"])
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a connection access target in model read, got %+v", modelPayload["access_targets"])
	}
}

func TestModelConnectionsBatchExposesCustomRequestParameters(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Custom Request Parameters Batch Strategy")
	ownerModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "custom-request-params-batch-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Custom Request Parameters Batch Endpoint")

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", ownerModelID), map[string]any{"endpoint_id": endpointID, "openai_text_capability": "dual_native", "custom_request_parameters": map[string]any{"provider": map[string]any{"only": []string{"deepinfra/turbo"}}}}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)

	batchResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models/connections/batch", map[string]any{"model_config_ids": []int{ownerModelID}}, modelHeader(defaultProfileID))
	assertStatus(t, batchResponse, http.StatusOK)
	var batch map[string]any
	decodeJSONResponse(t, batchResponse, &batch)
	items := batch["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one batch item, got %+v", batch)
	}
	item := asMap(t, items[0])
	connections := item["connections"].([]any)
	if len(connections) != 1 {
		t.Fatalf("expected one connection in batch item, got %+v", item)
	}
	connection := asMap(t, connections[0])
	params := asMap(t, connection["custom_request_parameters"])
	provider := asMap(t, params["provider"])
	if jsonStringList(t, provider["only"])[0] != "deepinfra/turbo" {
		t.Fatalf("expected batch read to expose custom request parameters, got %+v", connection["custom_request_parameters"])
	}
}

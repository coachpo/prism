package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestConnectionPricingTemplates(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	otherProfileID := insertContractProfile(t, harness, "S10 Other Pricing Profile")
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S10 Pricing Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-pricing-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Pricing Assignment Endpoint")
	connectionID := insertContractConnectionWithState(t, harness, defaultProfileID, modelConfigID, endpointID, nil, 0, true, nil, nil, "unknown", nil, nil)

	pricingTemplateID := insertContractPricingTemplate(t, harness, defaultProfileID, "S10 Assigned Template")
	otherProfileTemplateID := insertContractPricingTemplate(t, harness, otherProfileID, "S10 Other Profile Template")

	connectionRead := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/connections/%d", connectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, connectionRead, http.StatusOK)
	var connectionPayload map[string]any
	decodeJSONResponse(t, connectionRead, &connectionPayload)
	expectedUpdatedAt := connectionPayload["updated_at"]

	assignResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), map[string]any{"pricing_template_id": pricingTemplateID, "expected_connection_updated_at": expectedUpdatedAt, "expected_pricing_template_id": nil}, modelHeader(defaultProfileID))
	assertStatus(t, assignResponse, http.StatusOK)
	assignedPayload := connectionMutationConnection(t, assignResponse)
	if jsonInt(t, assignedPayload["pricing_template_id"]) != pricingTemplateID || jsonInt(t, asMap(t, assignedPayload["pricing_template"])["id"]) != pricingTemplateID {
		t.Fatalf("expected pricing template assignment payload, got %+v", assignedPayload)
	}

	clearResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), map[string]any{"pricing_template_id": nil, "expected_connection_updated_at": assignedPayload["updated_at"], "expected_pricing_template_id": pricingTemplateID}, modelHeader(defaultProfileID))
	assertStatus(t, clearResponse, http.StatusOK)
	clearedPayload := connectionMutationConnection(t, clearResponse)
	if clearedPayload["pricing_template_id"] != nil || clearedPayload["pricing_template"] != nil {
		t.Fatalf("expected clear pricing template assignment payload, got %+v", clearedPayload)
	}

	wrongProfileResponse := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), map[string]any{"pricing_template_id": otherProfileTemplateID, "expected_connection_updated_at": clearedPayload["updated_at"], "expected_pricing_template_id": nil}, modelHeader(defaultProfileID))
	assertErrorResponse(t, wrongProfileResponse, http.StatusNotFound, "Pricing template not found")

	// CAS drift: stale expected values must be rejected with 409.
	staleAssign := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), map[string]any{"pricing_template_id": pricingTemplateID, "expected_connection_updated_at": expectedUpdatedAt, "expected_pricing_template_id": nil}, modelHeader(defaultProfileID))
	assertStatus(t, staleAssign, http.StatusConflict)
	// Missing CAS fields must be a field-level 422.
	missingCAS := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), map[string]any{"pricing_template_id": pricingTemplateID}, modelHeader(defaultProfileID))
	assertStatus(t, missingCAS, http.StatusUnprocessableEntity)
	// A present-but-null expected_connection_updated_at carries no usable expectation and must conflict.
	nullExpectedUpdatedAt := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), map[string]any{"pricing_template_id": pricingTemplateID, "expected_connection_updated_at": nil, "expected_pricing_template_id": nil}, modelHeader(defaultProfileID))
	assertStatus(t, nullExpectedUpdatedAt, http.StatusConflict)
}

func TestConnectionS10PricingTemplateCRUD(t *testing.T) {
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
	assertPricingTemplatePayloadPrices(t, listed[0], "1", "2", "0", "0", "0")
	if listed[0]["tier"] != nil {
		t.Fatalf("expected unconfigured template tier to be explicit null, got %+v", listed[0]["tier"])
	}

	getResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/pricing-templates/%d", existingTemplateID), nil, modelHeader(defaultProfileID))
	assertStatus(t, getResponse, http.StatusOK)
	var existing map[string]any
	decodeJSONResponse(t, getResponse, &existing)
	if jsonInt(t, existing["profile_id"]) != defaultProfileID || existing["pricing_unit"] != "PER_1M" {
		t.Fatalf("expected pricing template payload for profile %d, got %+v", defaultProfileID, existing)
	}
	assertPricingTemplatePayloadPrices(t, existing, "1", "2", "0", "0", "0")
	if existing["tier"] != nil {
		t.Fatalf("expected unconfigured template tier to be explicit null, got %+v", existing["tier"])
	}

	// Blank specialty prices are field-level 422 (never normalized to zero);
	// explicit JSON null means unconfigured (SPEC 4.1).
	blankResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{"name": "S10 Blank Price Rejected", "template_kind": "standard", "card": map[string]any{"input_price": "1.25", "output_price": "2.50", "cached_input_price": "0.10", "cache_creation_price": "   ", "reasoning_price": "0"}}, modelHeader(defaultProfileID))
	assertStatus(t, blankResponse, http.StatusUnprocessableEntity)

	legacyFieldResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{"name": "S10 Legacy Field Rejected", "template_kind": "standard", "pricing_currency_code": "usd", "card": map[string]any{"input_price": "1.25", "output_price": "2.50", "cached_input_price": "0.10", "cache_creation_price": "0", "reasoning_price": "0"}}, modelHeader(defaultProfileID))
	assertStatus(t, legacyFieldResponse, http.StatusUnprocessableEntity)

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{"name": "S10 Created Template", "description": "created via contract", "template_kind": "standard", "card": map[string]any{"input_price": "1.25", "output_price": "2.50", "cached_input_price": "0.10", "cache_creation_price": "0", "reasoning_price": nil}}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	createdID := jsonInt(t, created["id"])
	if created["name"] != "S10 Created Template" || created["pricing_currency_code"] != "USD" || created["version"] != float64(1) {
		t.Fatalf("expected created pricing template payload, got %+v", created)
	}
	assertPricingTemplatePayloadPrices(t, created, "1.25", "2.5", "0.1", "0", nil)

	updateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": created["updated_at"], "description": "updated via contract", "template_kind": "standard", "card": map[string]any{"input_price": "3.75", "output_price": "2.5", "cached_input_price": "0.1", "cache_creation_price": "0", "reasoning_price": "0.50"}}, modelHeader(defaultProfileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updateResponse, &updated)
	if updated["description"] != "updated via contract" || updated["version"] != float64(2) {
		t.Fatalf("expected updated pricing template payload, got %+v", updated)
	}
	assertPricingTemplatePayloadPrices(t, updated, "3.75", "2.5", "0.1", "0", "0.5")

	nullKindResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": updated["updated_at"], "template_kind": nil}, modelHeader(defaultProfileID))
	assertStatus(t, nullKindResponse, http.StatusUnprocessableEntity)
	nullCASResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": nil, "template_kind": "tiered", "base_card": map[string]any{"input_price": "3.75", "output_price": "2.5", "cached_input_price": "0.1", "cache_creation_price": "0", "reasoning_price": "0.5"}, "tier": map[string]any{"input_tokens_above": 1, "card": map[string]any{"input_price": "4", "output_price": "18", "cached_input_price": "0.2", "cache_creation_price": "5", "reasoning_price": "6"}}}, modelHeader(defaultProfileID))
	assertStatus(t, nullCASResponse, http.StatusUnprocessableEntity)

	// A tier is one singular, complete five-component mirror. Its threshold
	// uses strict greater-than semantics at runtime; CRUD only stores the
	// normalized immutable card.
	tierUpdateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{
		"expected_updated_at": updated["updated_at"],
		"template_kind":       "tiered",
		"base_card":           map[string]any{"input_price": "3.75", "output_price": "2.5", "cached_input_price": "0.1", "cache_creation_price": "0", "reasoning_price": "0.50"},
		"tier":                map[string]any{"input_tokens_above": 272000, "card": map[string]any{"input_price": "4", "output_price": "18", "cached_input_price": "0.2", "cache_creation_price": "5", "reasoning_price": "6"}},
	}, modelHeader(defaultProfileID))
	assertStatus(t, tierUpdateResponse, http.StatusOK)
	var tiered map[string]any
	decodeJSONResponse(t, tierUpdateResponse, &tiered)
	tier := asMap(t, tiered["tier"])
	tierCard := asMap(t, tier["card"])
	if jsonInt(t, tier["input_tokens_above"]) != 272000 || tierCard["input_price"] != "4" || tierCard["output_price"] != "18" || tierCard["reasoning_price"] != "6" {
		t.Fatalf("expected normalized typed tier pricing card, got %+v", tiered)
	}

	// Explicit null specials become unconfigured (NULL), not zero. Clearing
	// the tier in the same replacement keeps the parity invariant valid.
	zeroPricesResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": tiered["updated_at"], "template_kind": "standard", "card": map[string]any{"input_price": "3.75", "output_price": "2.5", "cached_input_price": nil, "cache_creation_price": nil, "reasoning_price": nil}}, modelHeader(defaultProfileID))
	assertStatus(t, zeroPricesResponse, http.StatusOK)
	var zeroed map[string]any
	decodeJSONResponse(t, zeroPricesResponse, &zeroed)
	if zeroed["version"] != float64(4) {
		t.Fatalf("expected null pricing fields and tier clear to bump version, got %+v", zeroed)
	}
	assertPricingTemplatePayloadPrices(t, zeroed, "3.75", "2.5", nil, nil, nil)
	if zeroed["tier"] != nil {
		t.Fatalf("expected explicit null tier after clearing, got %+v", zeroed["tier"])
	}
	assertPricingTemplateStoredPrices(t, harness, defaultProfileID, "S10 Created Template", "3.75", "2.5", nil, nil, nil)

	staleUpdate := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": created["updated_at"], "name": "Stale Update"}, modelHeader(defaultProfileID))
	assertErrorResponse(t, staleUpdate, http.StatusConflict, "Pricing template has changed. Please refresh and retry.")

	deleteResponse := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/pricing-templates/%d", createdID), nil, modelHeader(defaultProfileID))
	assertStatus(t, deleteResponse, http.StatusOK)
	var deleted map[string]any
	decodeJSONResponse(t, deleteResponse, &deleted)
	if deleted["deleted"] != true {
		t.Fatalf("expected deleted response payload, got %+v", deleted)
	}

	// Tombstones stay addressable by ID but do not occupy the active authoring
	// namespace: the same canonical name can be recreated with a new revision.
	recreatedResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{
		"name":          "S10 Created Template",
		"template_kind": "standard",
		"card":          map[string]any{"input_price": "1", "output_price": "2", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"},
	}, modelHeader(defaultProfileID))
	assertStatus(t, recreatedResponse, http.StatusCreated)
	var recreated map[string]any
	decodeJSONResponse(t, recreatedResponse, &recreated)
	if jsonInt(t, recreated["id"]) == createdID {
		t.Fatalf("expected recreated template to have a new logical identity, got %+v", recreated)
	}
}

func TestConnectionS10PeakValleyPricingTemplateCRUD(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	create := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{
		"name": "S10 Peak Valley Template", "template_kind": "peak_valley",
		"peak_card":    map[string]any{"input_price": "10", "output_price": "20", "cached_input_price": "1", "cache_creation_price": "2", "reasoning_price": "3"},
		"offpeak_card": map[string]any{"input_price": "1", "output_price": "2", "cached_input_price": "1", "cache_creation_price": "2", "reasoning_price": "3"},
		"schedule":     map[string]any{"timezone": "Asia/Shanghai", "windows": []map[string]any{{"weekday_mask": 127, "start_minute": 0, "end_minute": 1440}}},
	}, modelHeader(profileID))
	assertStatus(t, create, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, create, &created)
	if created["template_kind"] != "peak_valley" || created["card"] != nil || created["base_card"] != nil {
		t.Fatalf("expected mutually exclusive peak_valley response, got %+v", created)
	}
	if asMap(t, created["peak_card"])["input_price"] != "10" || asMap(t, created["offpeak_card"])["input_price"] != "1" {
		t.Fatalf("expected complete peak/offpeak cards, got %+v", created)
	}
	schedule := asMap(t, created["schedule"])
	if schedule["timezone"] != "Asia/Shanghai" || len(schedule["windows"].([]any)) != 1 {
		t.Fatalf("expected authored schedule, got %+v", created)
	}
	bad := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{"name": "S10 Peak Empty", "template_kind": "peak_valley", "peak_card": map[string]any{"input_price": "1", "output_price": "2", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"}, "offpeak_card": map[string]any{"input_price": "1", "output_price": "2", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"}, "schedule": map[string]any{"timezone": "UTC", "windows": []any{}}}, modelHeader(profileID))
	assertStatus(t, bad, http.StatusUnprocessableEntity)
}

func TestConnectionS10PricingTemplateMetadataOnlyUpdateKeepsVersion(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	createResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates", map[string]any{"name": "S10 Metadata Only Update", "template_kind": "standard", "card": map[string]any{"input_price": "1.10", "output_price": "2.20", "cached_input_price": "3.30", "cache_creation_price": "4.40", "reasoning_price": "5.50"}}, modelHeader(defaultProfileID))
	assertStatus(t, createResponse, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, createResponse, &created)
	createdID := jsonInt(t, created["id"])
	assertPricingTemplatePayloadPrices(t, created, "1.1", "2.2", "3.3", "4.4", "5.5")

	// A metadata-only update (no price keys) must not create a revision.
	updateResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": created["updated_at"], "description": "metadata only"}, modelHeader(defaultProfileID))
	assertStatus(t, updateResponse, http.StatusOK)
	var updated map[string]any
	decodeJSONResponse(t, updateResponse, &updated)
	if updated["version"] != float64(1) {
		t.Fatalf("expected metadata-only update to keep version 1, got %+v", updated)
	}
	assertPricingTemplatePayloadPrices(t, updated, "1.1", "2.2", "3.3", "4.4", "5.5")
	assertPricingTemplateStoredPrices(t, harness, defaultProfileID, "S10 Metadata Only Update", "1.1", "2.2", "3.3", "4.4", "5.5")

	// Canonicalization: "2.20" (equivalent to stored "2.2") is a no-op
	// (SPEC 4.2) and must not create a revision.
	canonicalResponse := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", createdID), map[string]any{"expected_updated_at": updated["updated_at"], "template_kind": "standard", "card": map[string]any{"input_price": "1.1", "output_price": "2.20", "cached_input_price": "3.3", "cache_creation_price": "4.4", "reasoning_price": "5.5"}}, modelHeader(defaultProfileID))
	assertStatus(t, canonicalResponse, http.StatusOK)
	var canonical map[string]any
	decodeJSONResponse(t, canonicalResponse, &canonical)
	if canonical["version"] != float64(1) {
		t.Fatalf("expected canonical-equivalent re-submission to keep version 1, got %+v", canonical)
	}
}

func TestPricingTemplateDeleteConflict(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "S10 Pricing Delete Conflict Strategy")
	modelConfigID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "s10-pricing-delete-model", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Pricing Delete Conflict Endpoint")
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
	endpointAID := modelInsertEndpoint(t, harness, defaultProfileID, "Usage Endpoint A")
	endpointBID := modelInsertEndpoint(t, harness, defaultProfileID, "Usage Endpoint B")
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

func TestPricingTemplateImportUpsertValidationAndUnknownFields(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)

	payload := map[string]any{
		"schema_version": 2,
		"mode":           "upsert_by_name",
		"templates": []map[string]any{
			{"name": " gpt-4o ", "template_kind": "standard", "card": map[string]any{"input_price": "2.5", "output_price": "10", "cached_input_price": "1.25", "cache_creation_price": "0", "reasoning_price": "0"}, "description": " flagship "},
			{"name": "gpt-4o-mini", "template_kind": "standard", "card": map[string]any{"input_price": "0.15", "output_price": "0.60", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"}},
		},
	}

	var imported struct {
		Created     int      `json:"created"`
		Updated     int      `json:"updated"`
		Skipped     []string `json:"skipped"`
		Errors      []any    `json:"errors"`
		PreviewHash string   `json:"preview_hash"`
		Committable bool     `json:"committable"`
	}

	// Preview-only import reports the impact without writing.
	createdPreview := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import", payload, modelHeader(profileID))
	assertStatus(t, createdPreview, http.StatusOK)
	decodeJSONResponse(t, createdPreview, &imported)
	if imported.Created != 2 || imported.Updated != 0 || len(imported.Skipped) != 0 || len(imported.Errors) != 0 || imported.PreviewHash == "" || !imported.Committable {
		t.Fatalf("unexpected created import preview: %+v", imported)
	}
	assertPricingTemplateCount(t, harness, profileID, 0)

	// Commit with the preview hash writes the batch atomically.
	commitBody := map[string]any{"schema_version": 2, "mode": payload["mode"], "templates": payload["templates"], "preview_hash": imported.PreviewHash}
	commitResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import/commit", commitBody, modelHeader(profileID))
	assertStatus(t, commitResponse, http.StatusOK)
	decodeJSONResponse(t, commitResponse, &imported)
	if imported.Created != 2 || imported.Updated != 0 || len(imported.Skipped) != 0 || len(imported.Errors) != 0 {
		t.Fatalf("unexpected created import commit: %+v", imported)
	}
	assertPricingTemplateCount(t, harness, profileID, 2)

	// Re-preview: identical prices classify as no-ops (no revision created,
	// SPEC 4.2 canonical-equivalence).
	updatedPreview := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import", payload, modelHeader(profileID))
	assertStatus(t, updatedPreview, http.StatusOK)
	decodeJSONResponse(t, updatedPreview, &imported)
	if imported.Created != 0 || imported.Updated != 0 || imported.PreviewHash == "" {
		t.Fatalf("unexpected no-op import preview: %+v", imported)
	}
	commitBody2 := map[string]any{"schema_version": 2, "mode": payload["mode"], "templates": payload["templates"], "preview_hash": imported.PreviewHash}
	updatedResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import/commit", commitBody2, modelHeader(profileID))
	assertStatus(t, updatedResponse, http.StatusOK)
	decodeJSONResponse(t, updatedResponse, &imported)
	if imported.Created != 0 || imported.Updated != 0 || len(imported.Skipped) != 0 || len(imported.Errors) != 0 {
		t.Fatalf("unexpected no-op import commit: %+v", imported)
	}
	assertPricingTemplateCount(t, harness, profileID, 2)

	// A real price change through the two-phase flow bumps the version.
	changedPayload := map[string]any{
		"schema_version": 2,
		"mode":           "upsert_by_name",
		"templates": []map[string]any{
			{"name": "gpt-4o", "template_kind": "standard", "card": map[string]any{"input_price": "2.5", "output_price": "11", "cached_input_price": "1.25", "cache_creation_price": "0", "reasoning_price": "0"}, "description": " flagship "},
			{"name": "gpt-4o-mini", "template_kind": "standard", "card": map[string]any{"input_price": "0.15", "output_price": "0.60", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"}},
		},
	}
	changePreview := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import", changedPayload, modelHeader(profileID))
	assertStatus(t, changePreview, http.StatusOK)
	decodeJSONResponse(t, changePreview, &imported)
	if imported.Created != 0 || imported.Updated != 1 {
		t.Fatalf("expected one update in change preview (gpt-4o-mini is a no-op), got %+v", imported)
	}
	changeCommit := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import/commit", map[string]any{"schema_version": 2, "mode": "upsert_by_name", "templates": changedPayload["templates"], "preview_hash": imported.PreviewHash}, modelHeader(profileID))
	assertStatus(t, changeCommit, http.StatusOK)
	decodeJSONResponse(t, changeCommit, &imported)
	if imported.Created != 0 || imported.Updated != 1 {
		t.Fatalf("expected one update in change commit, got %+v", imported)
	}
	assertPricingTemplateCount(t, harness, profileID, 2)

	// A stale commit (wrong hash) fails closed without writing.
	staleCommit := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import/commit", map[string]any{"schema_version": 2, "mode": payload["mode"], "templates": payload["templates"], "preview_hash": "not-the-hash"}, modelHeader(profileID))
	assertStatus(t, staleCommit, http.StatusConflict)
	assertPricingTemplateCount(t, harness, profileID, 2)

	invalid := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import", map[string]any{
		"schema_version": 2,
		"mode":           "upsert_by_name",
		"templates": []map[string]any{
			{"name": "bad-row-kept-out", "template_kind": "standard", "card": map[string]any{"input_price": "1", "output_price": "2", "cached_input_price": "1.2.3", "cache_creation_price": "0", "reasoning_price": "0"}},
			{"name": "bad-price", "template_kind": "standard", "card": map[string]any{"input_price": "-1", "output_price": "2", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"}},
		},
	}, modelHeader(profileID))
	assertStatus(t, invalid, http.StatusOK)
	decodeJSONResponse(t, invalid, &imported)
	if imported.Created != 0 || imported.Updated != 0 || len(imported.Errors) != 2 || imported.Committable {
		t.Fatalf("expected per-row import preview errors, got %+v", imported)
	}
	assertPricingTemplateCount(t, harness, profileID, 2)

	// Legacy authoring fields inside import rows fail closed per row.
	legacyRow := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import", map[string]any{
		"schema_version": 2,
		"mode":           "upsert_by_name",
		"templates": []map[string]any{
			{"name": "legacy-row", "template_kind": "standard", "pricing_currency_code": "USD", "card": map[string]any{"input_price": "1", "output_price": "2", "cached_input_price": "0", "cache_creation_price": "0", "reasoning_price": "0"}},
		},
	}, modelHeader(profileID))
	assertStatus(t, legacyRow, http.StatusOK)
	decodeJSONResponse(t, legacyRow, &imported)
	if len(imported.Errors) != 1 || imported.Committable {
		t.Fatalf("expected legacy-field import preview error, got %+v", imported)
	}
	assertPricingTemplateCount(t, harness, profileID, 2)

	unknown := harness.requestJSON(t, harness.client, http.MethodPost, "/api/pricing-templates/import", map[string]any{
		"schema_version": 2,
		"mode":           "upsert_by_name",
		"templates":      []map[string]any{},
		"surprise":       true,
	}, modelHeader(profileID))
	assertStatus(t, unknown, http.StatusBadRequest)
}

func insertContractConnectionWithState(t *testing.T, harness *contractHarness, profileID int, modelConfigID int, endpointID int, pricingTemplateID *int, priority int, isActive bool, customHeaders map[string]string, name *string, healthStatus string, healthDetail *string, lastHealthAt *time.Time) int {
	t.Helper()
	now := time.Now().UTC()
	var apiFamily string
	if err := harness.conn.QueryRow(context.Background(), `SELECT api_family FROM model_configs WHERE id = $1 AND profile_id = $2`, modelConfigID, profileID).Scan(&apiFamily); err != nil {
		t.Fatalf("load model %d api family: %v", modelConfigID, err)
	}
	var headersValue any
	if customHeaders != nil {
		headersValue = mustModelJSON(t, customHeaders)
	}
	var connectionID int
	var openAITextCapability any
	if apiFamily == "openai" {
		openAITextCapability = "dual_native"
	}
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, $4, NULL, NULL, NULL, $13, $5, $6, $7, NULL, $8, $9, $10, $11, $12, $12) RETURNING id`, profileID, apiFamily, endpointID, nullableTestInt(pricingTemplateID), isActive, priority, name, headersValue, healthStatus, healthDetail, lastHealthAt, now, openAITextCapability).Scan(&connectionID); err != nil {
		t.Fatalf("insert contract connection for model %d endpoint %d: %v", modelConfigID, endpointID, err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, TRUE, $5, $5)`, profileID, modelConfigID, connectionID, priority, now); err != nil {
		t.Fatalf("insert contract connection access target for model %d endpoint %d: %v", modelConfigID, endpointID, err)
	}
	return connectionID
}

func assertPricingTemplateUsageItem(t *testing.T, payload map[string]any, connectionID int, connectionName string, modelConfigID int, modelID string, endpointID int, endpointName string) {
	t.Helper()
	if jsonInt(t, payload["connection_id"]) != connectionID || payload["connection_name"] != connectionName || jsonInt(t, payload["model_config_id"]) != modelConfigID || payload["model_id"] != modelID || jsonInt(t, payload["endpoint_id"]) != endpointID || payload["endpoint_name"] != endpointName {
		t.Fatalf("unexpected pricing template usage row: %+v", payload)
	}
}

func assertPricingTemplateCount(t *testing.T, harness *contractHarness, profileID int, want int) {
	t.Helper()
	var got int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM pricing_templates WHERE profile_id = $1`, profileID).Scan(&got); err != nil {
		t.Fatalf("count pricing templates: %v", err)
	}
	if got != want {
		t.Fatalf("expected %d pricing templates, got %d", want, got)
	}
}

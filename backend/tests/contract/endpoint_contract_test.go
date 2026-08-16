package contracttest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

func TestEndpointCRUDSecretMetadata(t *testing.T) {
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
	assertEndpointSecretSurface(t, "create", primary, true, "fp_v1_", 1)
	assertNoMaskedOrPosition(t, primary)

	// Blank key create: no fingerprint/time, revision 1.
	createBlank := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Blank Key Endpoint", "base_url": "https://blank.invalid"}, nil)
	assertStatus(t, createBlank, http.StatusCreated)
	var blank map[string]any
	decodeJSONResponse(t, createBlank, &blank)
	if blank["has_api_key"] != false || blank["api_key_fingerprint"] != nil || blank["api_key_updated_at"] != nil {
		t.Fatalf("expected blank key create to expose no secret metadata, got %+v", blank)
	}
	if jsonInt(t, blank["config_revision"]) != 1 {
		t.Fatalf("expected blank key create revision 1, got %+v", blank)
	}

	// Update with the same key: identity unchanged, fingerprint/time/revision preserved.
	sameKeyUpdate := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/endpoints/%d", primaryID), map[string]any{"name": "Primary Renamed", "api_key": "sk-primary"}, modelHeader(defaultProfileID))
	assertStatus(t, sameKeyUpdate, http.StatusOK)
	var sameKey map[string]any
	decodeJSONResponse(t, sameKeyUpdate, &sameKey)
	if sameKey["name"] != "Primary Renamed" {
		t.Fatalf("expected name rename, got %+v", sameKey)
	}
	if sameKey["api_key_fingerprint"] != primary["api_key_fingerprint"] {
		t.Fatalf("expected same-key update to preserve fingerprint, got %+v", sameKey)
	}
	if jsonInt(t, sameKey["config_revision"]) != 1 {
		t.Fatalf("expected same-key update to preserve revision 1, got %+v", sameKey)
	}

	// Blank key on update preserves the existing key identity.
	blankKeyUpdate := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/endpoints/%d", primaryID), map[string]any{"base_url": "https://api.openai.com/v2/"}, modelHeader(defaultProfileID))
	assertStatus(t, blankKeyUpdate, http.StatusOK)
	var blankKey map[string]any
	decodeJSONResponse(t, blankKeyUpdate, &blankKey)
	if blankKey["base_url"] != "https://api.openai.com/v2" {
		t.Fatalf("expected base URL update, got %+v", blankKey)
	}
	if blankKey["api_key_fingerprint"] != primary["api_key_fingerprint"] || jsonInt(t, blankKey["config_revision"]) != 2 {
		t.Fatalf("expected URL change to keep key identity and bump revision, got %+v", blankKey)
	}
	if blankKey["api_key_updated_at"] == nil {
		t.Fatalf("expected key time to survive URL-only update, got %+v", blankKey)
	}

	// Rotate to a different key: new fingerprint, new key time, revision bump.
	rotate := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/endpoints/%d", primaryID), map[string]any{"api_key": "sk-rotated"}, modelHeader(defaultProfileID))
	assertStatus(t, rotate, http.StatusOK)
	var rotated map[string]any
	decodeJSONResponse(t, rotate, &rotated)
	if rotated["api_key_fingerprint"] == primary["api_key_fingerprint"] {
		t.Fatalf("expected rotated key to expose a new fingerprint, got %+v", rotated)
	}
	if jsonInt(t, rotated["config_revision"]) != 3 {
		t.Fatalf("expected key rotation to bump revision, got %+v", rotated)
	}

	// No-op update preserves everything including updated_at and revision.
	noOp := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/endpoints/%d", primaryID), map[string]any{"name": "Primary Renamed", "base_url": "https://api.openai.com/v2", "api_key": "sk-rotated"}, modelHeader(defaultProfileID))
	assertStatus(t, noOp, http.StatusOK)
	var noOpPayload map[string]any
	decodeJSONResponse(t, noOp, &noOpPayload)
	if noOpPayload["updated_at"] != rotated["updated_at"] || jsonInt(t, noOpPayload["config_revision"]) != 3 {
		t.Fatalf("expected no-op update to preserve timestamps and revision, got %+v", noOpPayload)
	}
}

func TestEndpointCreateValidation(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)

	// Name too long (129 code points) -> typed 422 field error.
	overlongName := strings.Repeat("界", 129)
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": overlongName, "base_url": "https://valid.invalid"}, nil)
	assertStatus(t, response, http.StatusUnprocessableEntity)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	fields := asMap(t, asMap(t, payload["detail"])["fields"])
	if fields["name"] != "name_too_long" {
		t.Fatalf("expected name_too_long field code, got %+v", payload)
	}

	// Query string rejected -> base_url_invalid.
	queryResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Query Endpoint", "base_url": "https://valid.invalid?x=1"}, nil)
	assertStatus(t, queryResponse, http.StatusUnprocessableEntity)
	decodeJSONResponse(t, queryResponse, &payload)
	fields = asMap(t, asMap(t, payload["detail"])["fields"])
	if fields["base_url"] != "base_url_invalid" {
		t.Fatalf("expected base_url_invalid field code for query, got %+v", payload)
	}

	// 512-code-point URL boundary accepted; 513 rejected.
	boundaryURL := "https://api.example.com/" + strings.Repeat("a", 512-24)
	boundaryResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Boundary Endpoint", "base_url": boundaryURL}, nil)
	assertStatus(t, boundaryResponse, http.StatusCreated)
	tooLongURL := "https://api.example.com/" + strings.Repeat("a", 513-24)
	tooLongResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": "Too Long Endpoint", "base_url": tooLongURL}, nil)
	assertStatus(t, tooLongResponse, http.StatusUnprocessableEntity)
	decodeJSONResponse(t, tooLongResponse, &payload)
	fields = asMap(t, asMap(t, payload["detail"])["fields"])
	if fields["base_url"] != "base_url_too_long" {
		t.Fatalf("expected base_url_too_long field code, got %+v", payload)
	}
}

func TestEndpointListOrderAndDTOSurface(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	// Names chosen to prove lower(name) ASC, name ASC, id ASC ordering.
	for _, name := range []string{"Zulu Endpoint", "alpha Endpoint", "Alpha Endpoint", "bravo Endpoint"} {
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": name, "base_url": "https://" + strings.ToLower(name[:1]) + ".invalid", "api_key": "sk-" + strings.ToLower(name[:1])}, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusCreated)
	}

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/endpoints", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var endpoints []map[string]any
	decodeJSONResponse(t, listResponse, &endpoints)
	if len(endpoints) != 4 {
		t.Fatalf("expected 4 endpoints, got %+v", endpoints)
	}
	// lower("alpha Endpoint") == lower("Alpha Endpoint") -> name ASC tie-break.
	if endpoints[0]["name"] != "Alpha Endpoint" || endpoints[1]["name"] != "alpha Endpoint" || endpoints[2]["name"] != "bravo Endpoint" || endpoints[3]["name"] != "Zulu Endpoint" {
		t.Fatalf("expected deterministic lower-name order, got %+v", endpoints)
	}
	for _, endpoint := range endpoints {
		assertNoMaskedOrPosition(t, endpoint)
	}
}

func TestEndpointReferencesBatch(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Batch Ref Strategy")
	modelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "batch-ref-model", nil, "native", &strategyID, true)

	referencedID := modelInsertEndpoint(t, harness, defaultProfileID, "Referenced Endpoint")
	unreferencedID := modelInsertEndpoint(t, harness, defaultProfileID, "Unreferenced Endpoint")
	firstConnectionID := modelInsertConnection(t, harness, defaultProfileID, modelID, referencedID, 0, true, nil)
	secondConnectionID := modelInsertConnection(t, harness, defaultProfileID, modelID, referencedID, 1, false, nil)

	batchResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": []int{unreferencedID, referencedID}}, modelHeader(defaultProfileID))
	assertStatus(t, batchResponse, http.StatusOK)
	var batch map[string]any
	decodeJSONResponse(t, batchResponse, &batch)
	items := batch["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected two batch items in input order, got %+v", batch)
	}
	firstItem := asMap(t, items[0])
	if jsonInt(t, firstItem["endpoint_id"]) != unreferencedID {
		t.Fatalf("expected input-order items, got %+v", batch)
	}
	firstSummary := asMap(t, firstItem["summary"])
	if jsonInt(t, firstSummary["direct_reference_count"]) != 0 || jsonInt(t, firstSummary["referencing_model_count"]) != 0 || jsonInt(t, firstSummary["enabled_reference_count"]) != 0 || jsonInt(t, firstSummary["orphan_reference_count"]) != 0 {
		t.Fatalf("expected explicit zero summary, got %+v", firstItem)
	}
	secondSummary := asMap(t, asMap(t, items[1])["summary"])
	if jsonInt(t, secondSummary["direct_reference_count"]) != 2 || jsonInt(t, secondSummary["referencing_model_count"]) != 1 || jsonInt(t, secondSummary["enabled_reference_count"]) != 1 || jsonInt(t, secondSummary["orphan_reference_count"]) != 0 {
		t.Fatalf("expected 2 direct refs / 1 model / 1 enabled, got %+v", secondSummary)
	}
	_ = firstConnectionID
	_ = secondConnectionID

	// Missing ID -> typed 404 with missing_endpoint_ids.
	missing := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": []int{99999}}, modelHeader(defaultProfileID))
	assertStatus(t, missing, http.StatusNotFound)
	var missingPayload map[string]any
	decodeJSONResponse(t, missing, &missingPayload)
	detail := asMap(t, missingPayload["detail"])
	if detail["code"] != "endpoint_not_found" || len(detail["missing_endpoint_ids"].([]any)) != 1 {
		t.Fatalf("expected typed missing endpoint detail, got %+v", missingPayload)
	}

	// Empty/duplicate/over-100 -> 422.
	for name, ids := range map[string][]int{"empty": {}, "duplicate": {referencedID, referencedID}} {
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": ids}, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusUnprocessableEntity)
		_ = name
	}
	overLimit := make([]int, 101)
	for index := range overLimit {
		overLimit[index] = index + 1
	}
	overResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": overLimit}, modelHeader(defaultProfileID))
	assertStatus(t, overResponse, http.StatusUnprocessableEntity)
}

func TestEndpointReferenceDetailPaginationAndStale(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Detail Pagination Strategy")
	firstModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "detail-model-a", nil, "native", &strategyID, true)
	secondModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "detail-model-b", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Detail Endpoint")
	// Two owners produce two direct references.
	firstConnectionID := modelInsertConnection(t, harness, defaultProfileID, firstModelID, endpointID, 0, true, nil)
	secondConnectionID := modelInsertConnection(t, harness, defaultProfileID, secondModelID, endpointID, 1, true, nil)

	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/references", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detail map[string]any
	decodeJSONResponse(t, detailResponse, &detail)
	summary := asMap(t, detail["summary"])
	if jsonInt(t, summary["direct_reference_count"]) != 2 {
		t.Fatalf("expected 2 direct references, got %+v", detail)
	}
	page := asMap(t, detail["reference_page"])
	if jsonInt(t, page["total_count"]) != 2 || len(page["items"].([]any)) != 2 {
		t.Fatalf("expected bounded first page with both items, got %+v", page)
	}
	if page["next_cursor"] != nil {
		t.Fatalf("expected no cursor on final page, got %+v", page)
	}
	if page["reference_snapshot_hash"] == "" {
		t.Fatalf("expected opaque snapshot hash, got %+v", page)
	}
	hash := page["reference_snapshot_hash"].(string)
	items := page["items"].([]any)
	firstItem := asMap(t, items[0])
	if firstItem["kind"] != "owned_terminal_target" || firstItem["owner_model"] == nil {
		t.Fatalf("expected owned reference with owner model, got %+v", firstItem)
	}
	owner := asMap(t, firstItem["owner_model"])
	if owner["model_id"] != "detail-model-a" || firstItem["openai_text_capability"] == nil {
		t.Fatalf("expected strict-equality model metadata, got %+v", firstItem)
	}
	if firstItem["enabled"] != true || len(firstItem["inactive_reasons"].([]any)) != 0 {
		t.Fatalf("expected enabled owned reference, got %+v", firstItem)
	}
	_ = firstConnectionID
	_ = secondConnectionID

	// A mutation between pages must produce a typed stale 409 on continuation.
	cursorResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/references?limit=1", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, cursorResponse, http.StatusOK)
	var cursorDetail map[string]any
	decodeJSONResponse(t, cursorResponse, &cursorDetail)
	cursorPage := asMap(t, cursorDetail["reference_page"])
	if cursorPage["next_cursor"] == nil {
		t.Fatalf("expected non-null cursor when rows remain, got %+v", cursorDetail)
	}
	cursor := cursorPage["next_cursor"].(string)
	if cursorPage["reference_snapshot_hash"].(string) != hash {
		t.Fatalf("expected identical snapshot hash across pages of the same snapshot")
	}

	// Insert a new connection (mutation) -> continuation is stale.
	thirdConnectionID := modelInsertConnection(t, harness, defaultProfileID, firstModelID, endpointID, 2, true, nil)
	staleResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/references?limit=1&cursor=%s", endpointID, cursor), nil, modelHeader(defaultProfileID))
	assertStatus(t, staleResponse, http.StatusConflict)
	var stalePayload map[string]any
	decodeJSONResponse(t, staleResponse, &stalePayload)
	if asMap(t, stalePayload["detail"])["code"] != "reference_snapshot_stale" {
		t.Fatalf("expected reference_snapshot_stale, got %+v", stalePayload)
	}
	_ = thirdConnectionID

	// Invalid cursor shape -> typed 422.
	invalidCursor := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/references?limit=1&cursor=not-a-cursor", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, invalidCursor, http.StatusUnprocessableEntity)
	decodeJSONResponse(t, invalidCursor, &stalePayload)
	if asMap(t, stalePayload["detail"])["code"] != "reference_cursor_invalid" {
		t.Fatalf("expected reference_cursor_invalid, got %+v", stalePayload)
	}
}

func TestEndpointDeleteBlockedAndEligible(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Delete Block Strategy")
	ownerID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "delete-block-owner", nil, "native", &strategyID, true)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Delete Block Endpoint")
	connectionID := modelInsertConnection(t, harness, defaultProfileID, ownerID, endpointID, 0, true, nil)

	// Disabled connection still blocks deletion (direct reference truth).
	blocked := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, blocked, http.StatusConflict)
	var blockedPayload map[string]any
	decodeJSONResponse(t, blocked, &blockedPayload)
	detail := asMap(t, blockedPayload["detail"])
	if detail["code"] != "endpoint_in_use" {
		t.Fatalf("expected typed endpoint_in_use detail, got %+v", blockedPayload)
	}
	blockedSummary := asMap(t, detail["summary"])
	if jsonInt(t, blockedSummary["direct_reference_count"]) != 1 || jsonInt(t, blockedSummary["enabled_reference_count"]) != 1 {
		t.Fatalf("expected canonical blocker summary, got %+v", detail)
	}
	blockedPage := asMap(t, detail["reference_page"])
	if jsonInt(t, blockedPage["total_count"]) != 1 || len(blockedPage["items"].([]any)) != 1 {
		t.Fatalf("expected bounded first blocker page, got %+v", detail)
	}
	if detail["references_url"] != fmt.Sprintf("/api/endpoints/%d/references", endpointID) {
		t.Fatalf("expected references_url, got %+v", detail)
	}
	blockedItem := asMap(t, blockedPage["items"].([]any)[0])
	if jsonInt(t, blockedItem["connection_id"]) != connectionID || blockedItem["kind"] != "owned_terminal_target" {
		t.Fatalf("expected owned blocker item, got %+v", blockedItem)
	}

	// Remove the owner via the model route, then deletion is eligible.
	// Disable the model first: deleting the last enabled target of an enabled
	// model is rejected by the model domain invariant.
	disableOwner := harness.requestJSON(t, harness.client, http.MethodPut, fmt.Sprintf("/api/models/%d", ownerID), map[string]any{"is_enabled": false}, modelHeader(defaultProfileID))
	assertStatus(t, disableOwner, http.StatusOK)
	targetID := modelLoadConnectionTargetID(t, harness, ownerID, connectionID)
	removeTarget := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/models/%d/targets/%d", ownerID, targetID), nil, modelHeader(defaultProfileID))
	assertStatus(t, removeTarget, http.StatusOK)
	assertStoredConnectionCount(t, harness, connectionID, 0)

	eligible := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, eligible, http.StatusOK)
	var deleted map[string]any
	decodeJSONResponse(t, eligible, &deleted)
	if deleted["deleted"] != true {
		t.Fatalf("expected deleted:true, got %+v", deleted)
	}
	assertEndpointCount(t, harness, endpointID, 0)
}

func TestEndpointDeleteRacePreflightZeroThenReference(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Delete Race Strategy")
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Race Endpoint")

	// Preflight: zero references.
	preflight := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/references", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, preflight, http.StatusOK)
	var preflightPayload map[string]any
	decodeJSONResponse(t, preflight, &preflightPayload)
	if jsonInt(t, asMap(t, preflightPayload["summary"])["direct_reference_count"]) != 0 {
		t.Fatalf("expected zero preflight, got %+v", preflightPayload)
	}

	// A reference is inserted before DELETE; lock-time recompute must 409.
	ownerID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "race-owner", nil, "native", &strategyID, true)
	modelInsertConnection(t, harness, defaultProfileID, ownerID, endpointID, 0, true, nil)
	raceDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, raceDelete, http.StatusConflict)
	var racePayload map[string]any
	decodeJSONResponse(t, raceDelete, &racePayload)
	detail := asMap(t, racePayload["detail"])
	if detail["code"] != "endpoint_in_use" {
		t.Fatalf("expected lock-time race to return endpoint_in_use, got %+v", racePayload)
	}
	if jsonInt(t, asMap(t, detail["summary"])["direct_reference_count"]) != 1 {
		t.Fatalf("expected canonical lock-time summary, got %+v", detail)
	}
}

func TestEndpointOrphanCleanup(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Orphan Endpoint")
	now := time.Now().UTC()
	var orphanConnectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, 'orphan-terminal', NULL, NULL, 'healthy', NULL, NULL, $3, $3) RETURNING id`, defaultProfileID, endpointID, now).Scan(&orphanConnectionID); err != nil {
		t.Fatalf("insert orphan connection: %v", err)
	}

	// Orphan appears in detail as kind=orphan_connection and blocks delete.
	detailResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/references", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, detailResponse, http.StatusOK)
	var detail map[string]any
	decodeJSONResponse(t, detailResponse, &detail)
	summary := asMap(t, detail["summary"])
	if jsonInt(t, summary["direct_reference_count"]) != 1 || jsonInt(t, summary["orphan_reference_count"]) != 1 || jsonInt(t, summary["referencing_model_count"]) != 0 {
		t.Fatalf("expected orphan reference summary, got %+v", detail)
	}
	page := asMap(t, detail["reference_page"])
	if jsonInt(t, page["total_count"]) != 1 || len(page["items"].([]any)) != 1 {
		t.Fatalf("expected one orphan item in page, got %+v", page)
	}
	orphanItem := asMap(t, page["items"].([]any)[0])
	if orphanItem["kind"] != "orphan_connection" || orphanItem["owner_model"] != nil || orphanItem["access_target"] != nil {
		t.Fatalf("expected orphan item shape, got %+v", orphanItem)
	}
	if orphanItem["inactive_reasons"].([]any)[0] != "orphaned" {
		t.Fatalf("expected orphaned reason, got %+v", orphanItem)
	}

	blocked := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, blocked, http.StatusConflict)

	// Cleanup succeeds.
	cleanup := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d/orphan-connections/%d", endpointID, orphanConnectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, cleanup, http.StatusOK)
	var cleaned map[string]any
	decodeJSONResponse(t, cleanup, &cleaned)
	if cleaned["deleted"] != true || jsonInt(t, cleaned["connection_id"]) != orphanConnectionID {
		t.Fatalf("expected orphan cleanup success, got %+v", cleaned)
	}
	assertStoredConnectionCount(t, harness, orphanConnectionID, 0)

	// Now deletion is eligible.
	eligible := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, eligible, http.StatusOK)
}

func TestEndpointOrphanCleanupRaceOwnerAttach(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Orphan Race Strategy")
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Orphan Race Endpoint")
	now := time.Now().UTC()
	var orphanConnectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, 'race-orphan', NULL, NULL, 'healthy', NULL, NULL, $3, $3) RETURNING id`, defaultProfileID, endpointID, now).Scan(&orphanConnectionID); err != nil {
		t.Fatalf("insert race orphan connection: %v", err)
	}

	// Simulate an owner attach racing the cleanup: an access target pointing at
	// the orphan row appears after the preflight (external/historical write).
	ownerID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "orphan-race-owner", nil, "native", &strategyID, true)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, defaultProfileID, ownerID, orphanConnectionID, now); err != nil {
		t.Fatalf("attach racing owner: %v", err)
	}

	raceCleanup := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d/orphan-connections/%d", endpointID, orphanConnectionID), nil, modelHeader(defaultProfileID))
	assertStatus(t, raceCleanup, http.StatusConflict)
	var racePayload map[string]any
	decodeJSONResponse(t, raceCleanup, &racePayload)
	detail := asMap(t, racePayload["detail"])
	if detail["code"] != "connection_not_orphaned" {
		t.Fatalf("expected connection_not_orphaned, got %+v", racePayload)
	}
	item := asMap(t, detail["item"])
	if item["kind"] != "owned_terminal_target" || jsonInt(t, item["connection_id"]) != orphanConnectionID {
		t.Fatalf("expected owned item in conflict detail, got %+v", detail)
	}
	assertStoredConnectionCount(t, harness, orphanConnectionID, 1)
}

func TestEndpointReferenceIntegrityErrorFailsClosed(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "Integrity Endpoint")
	now := time.Now().UTC()
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'dual_native', TRUE, 0, 'dup-owner-connection', NULL, NULL, 'healthy', NULL, NULL, $3, $3) RETURNING id`, defaultProfileID, endpointID, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert integrity connection: %v", err)
	}
	// Two owners for the same connection (corruption). The partial unique
	// owner index normally prevents this; drop it temporarily to simulate
	// external/historical corruption and restore it afterwards.
	if _, err := harness.conn.Exec(context.Background(), `DROP INDEX uq_model_access_targets_connection_owner`); err != nil {
		t.Fatalf("drop owner uniqueness index: %v", err)
	}
	t.Cleanup(func() {
		if _, err := harness.conn.Exec(context.Background(), `DELETE FROM model_access_targets WHERE target_connection_id = $1`, connectionID); err != nil {
			t.Fatalf("remove corrupt owner rows: %v", err)
		}
		if _, err := harness.conn.Exec(context.Background(), `CREATE UNIQUE INDEX uq_model_access_targets_connection_owner ON model_access_targets USING btree (target_connection_id) WHERE (target_connection_id IS NOT NULL)`); err != nil {
			t.Fatalf("restore owner uniqueness index: %v", err)
		}
	})
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, defaultProfileID, "Integrity Strategy")
	firstModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "integrity-model-a", nil, "native", &strategyID, true)
	secondModelID := modelInsertModel(t, harness, defaultProfileID, &vendorID, "openai", "integrity-model-b", nil, "native", &strategyID, true)
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4), ($1, $5, 'connection', $3, 1, TRUE, $4, $4)`, defaultProfileID, firstModelID, connectionID, now, secondModelID); err != nil {
		t.Fatalf("insert duplicate owners: %v", err)
	}

	// Batch read fails closed with typed 409.
	batch := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints/references/batch", map[string]any{"endpoint_ids": []int{endpointID}}, modelHeader(defaultProfileID))
	assertStatus(t, batch, http.StatusConflict)
	var payload map[string]any
	decodeJSONResponse(t, batch, &payload)
	detail := asMap(t, payload["detail"])
	if detail["code"] != "reference_integrity_error" {
		t.Fatalf("expected reference_integrity_error, got %+v", payload)
	}
	affected := detail["affected_connection_ids"].([]any)
	if len(affected) != 1 || jsonInt(t, affected[0]) != connectionID {
		t.Fatalf("expected affected connection ids, got %+v", detail)
	}

	// Single detail also fails closed.
	single := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/endpoints/%d/references", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, single, http.StatusConflict)

	// DELETE must not proceed.
	blockedDelete := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/endpoints/%d", endpointID), nil, modelHeader(defaultProfileID))
	assertStatus(t, blockedDelete, http.StatusConflict)
	assertEndpointCount(t, harness, endpointID, 1)
}

func TestEndpointPositionRouteAbsent(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	endpointID := modelInsertEndpoint(t, harness, defaultProfileID, "No Position Endpoint")

	response := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/endpoints/%d/position", endpointID), map[string]any{"to_index": 0}, modelHeader(defaultProfileID))
	assertStatus(t, response, http.StatusNotFound)
}

func TestEndpointVerifyClassificationAndZeroSideEffects(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)

	// Provider test servers: one per family, each route maps to an outcome.
	openaiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer openaiServer.Close()
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.URL.Query().Get("limit") == "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-3-5-sonnet"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer anthropicServer.Close()
	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" && r.URL.Query().Get("pageSize") == "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-2.0-flash"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer geminiServer.Close()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-verify-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer authServer.Close()

	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`internal boom`))
	}))
	defer failureServer.Close()

	createEndpoint := func(name string, baseURL string, apiKey string) int {
		response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/endpoints", map[string]any{"name": name, "base_url": baseURL, "api_key": apiKey}, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusCreated)
		var endpoint map[string]any
		decodeJSONResponse(t, response, &endpoint)
		return jsonInt(t, endpoint["id"])
	}

	openaiEndpointID := createEndpoint("Verify OpenAI", openaiServer.URL, "sk-verify-key")
	anthropicEndpointID := createEndpoint("Verify Anthropic", anthropicServer.URL, "sk-anthropic")
	geminiEndpointID := createEndpoint("Verify Gemini", geminiServer.URL, "sk-gemini")
	failureEndpointID := createEndpoint("Verify Failure", failureServer.URL, "sk-fail")

	beforeCounts := countVerifySideEffectRows(t, harness)

	// Verified outcomes for all three families.
	for _, test := range []struct {
		endpointID int
		family     string
	}{
		{openaiEndpointID, "openai"},
		{anthropicEndpointID, "anthropic"},
		{geminiEndpointID, "gemini"},
	} {
		response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/verify", test.endpointID), map[string]any{"api_family": test.family, "expected_config_revision": 1}, modelHeader(defaultProfileID))
		assertStatus(t, response, http.StatusOK)
		var verify map[string]any
		decodeJSONResponse(t, response, &verify)
		if verify["outcome"] != "verified" || verify["is_current"] != true || jsonInt(t, verify["config_revision"]) != 1 {
			t.Fatalf("expected verified current outcome, got %+v", verify)
		}
		if verify["api_key_fingerprint"] == nil {
			t.Fatalf("expected fingerprint in verify response, got %+v", verify)
		}
	}

	// Authentication failure.
	authEndpointID := createEndpoint("Verify Auth", authServer.URL, "sk-wrong-key")
	authResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/verify", authEndpointID), map[string]any{"api_family": "openai", "expected_config_revision": 1}, modelHeader(defaultProfileID))
	assertStatus(t, authResponse, http.StatusOK)
	var authVerify map[string]any
	decodeJSONResponse(t, authResponse, &authVerify)
	if authVerify["outcome"] != "authentication_failed" || jsonInt(t, authVerify["upstream_status"]) != http.StatusUnauthorized {
		t.Fatalf("expected authentication_failed outcome, got %+v", authVerify)
	}

	// Upstream unavailable (5xx).
	failureResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/verify", failureEndpointID), map[string]any{"api_family": "openai", "expected_config_revision": 1}, modelHeader(defaultProfileID))
	assertStatus(t, failureResponse, http.StatusOK)
	var failureVerify map[string]any
	decodeJSONResponse(t, failureResponse, &failureVerify)
	if failureVerify["outcome"] != "upstream_unavailable" {
		t.Fatalf("expected upstream_unavailable outcome, got %+v", failureVerify)
	}

	// Stale revision: no probe, typed 409 before any outbound I/O.
	staleResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/verify", openaiEndpointID), map[string]any{"api_family": "openai", "expected_config_revision": 99}, modelHeader(defaultProfileID))
	assertStatus(t, staleResponse, http.StatusConflict)
	var stalePayload map[string]any
	decodeJSONResponse(t, staleResponse, &stalePayload)
	detail := asMap(t, stalePayload["detail"])
	if detail["code"] != "endpoint_config_changed" {
		t.Fatalf("expected endpoint_config_changed, got %+v", stalePayload)
	}
	if asMap(t, detail["endpoint"])["id"] == nil {
		t.Fatalf("expected current Endpoint DTO in conflict detail, got %+v", detail)
	}

	// Invalid family -> 422.
	invalidFamily := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/verify", openaiEndpointID), map[string]any{"api_family": "unknown", "expected_config_revision": 1}, modelHeader(defaultProfileID))
	assertStatus(t, invalidFamily, http.StatusUnprocessableEntity)

	// Zero side effects: no request/usage/audit/loadbalance writes, and the
	// Endpoint row is untouched (fingerprint/time/revision preserved).
	afterCounts := countVerifySideEffectRows(t, harness)
	for label := range beforeCounts {
		if beforeCounts[label] != afterCounts[label] {
			t.Fatalf("verify produced %s side effects: before=%d after=%d", label, beforeCounts[label], afterCounts[label])
		}
	}
	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/endpoints", nil, modelHeader(defaultProfileID))
	assertStatus(t, listResponse, http.StatusOK)
	var endpoints []map[string]any
	decodeJSONResponse(t, listResponse, &endpoints)
	for _, endpoint := range endpoints {
		if jsonInt(t, endpoint["id"]) == openaiEndpointID {
			if jsonInt(t, endpoint["config_revision"]) != 1 {
				t.Fatalf("expected verify to leave revision untouched, got %+v", endpoint)
			}
		}
	}
}

func TestEndpointDuplicateCopiesKeyIdentity(t *testing.T) {
	harness := newEndpointConnectionContractHarness(t)
	defaultProfileID := modelLoadDefaultProfileID(t, harness)
	sourceID := modelInsertEndpoint(t, harness, defaultProfileID, "Duplicate Me")

	duplicateResponse := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/endpoints/%d/duplicate", sourceID), nil, modelHeader(defaultProfileID))
	assertStatus(t, duplicateResponse, http.StatusCreated)
	var duplicate map[string]any
	decodeJSONResponse(t, duplicateResponse, &duplicate)
	if duplicate["name"] != "Duplicate Me copy" {
		t.Fatalf("expected duplicate name deconfliction, got %+v", duplicate)
	}
	if duplicate["api_key_fingerprint"] == nil || duplicate["api_key_updated_at"] == nil {
		t.Fatalf("expected duplicate to carry key identity with fresh key time, got %+v", duplicate)
	}
	if jsonInt(t, duplicate["config_revision"]) != 1 {
		t.Fatalf("expected duplicate revision 1, got %+v", duplicate)
	}
	assertNoMaskedOrPosition(t, duplicate)
}

func assertEndpointSecretSurface(t *testing.T, label string, endpoint map[string]any, hasKey bool, fingerprintPrefix string, revision int) {
	t.Helper()
	if endpoint["has_api_key"] != hasKey {
		t.Fatalf("%s: expected has_api_key=%v, got %+v", label, hasKey, endpoint)
	}
	if hasKey {
		fingerprint, ok := endpoint["api_key_fingerprint"].(string)
		if !ok || !strings.HasPrefix(fingerprint, fingerprintPrefix) {
			t.Fatalf("%s: expected fingerprint prefix %q, got %+v", label, fingerprintPrefix, endpoint)
		}
		if endpoint["api_key_updated_at"] == nil {
			t.Fatalf("%s: expected key time for keyed Endpoint, got %+v", label, endpoint)
		}
	} else if endpoint["api_key_fingerprint"] != nil || endpoint["api_key_updated_at"] != nil {
		t.Fatalf("%s: expected null secret metadata for keyless Endpoint, got %+v", label, endpoint)
	}
	if jsonInt(t, endpoint["config_revision"]) != revision {
		t.Fatalf("%s: expected config_revision %d, got %+v", label, revision, endpoint)
	}
}

func assertNoMaskedOrPosition(t *testing.T, endpoint map[string]any) {
	t.Helper()
	if _, exists := endpoint["masked_api_key"]; exists {
		t.Fatalf("expected no masked_api_key in Endpoint DTO, got %+v", endpoint)
	}
	if _, exists := endpoint["position"]; exists {
		t.Fatalf("expected no position in Endpoint DTO, got %+v", endpoint)
	}
}

func countVerifySideEffectRows(t *testing.T, harness *contractHarness) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for label, table := range map[string]string{
		"request_logs":         "request_logs",
		"usage_request_events": "usage_request_events",
		"audit_logs":           "audit_logs",
		"loadbalance_events":   "loadbalance_events",
	} {
		var count int
		if err := harness.conn.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[label] = count
	}
	return counts
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
			modelsService.SetTerminalTargetCreator(connectionsService)
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

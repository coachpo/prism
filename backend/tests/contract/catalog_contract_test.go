package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/jackc/pgx/v5/pgxpool"
)

// catalogFixtureCatalog mirrors the published models.dev shapes closely
// enough to exercise every mapping rule: unique matches, cross-provider
// ambiguity, an OpenAI single context tier with duplicate legacy evidence,
// audio costs, explicit zero prices, nullable efforts, a full five-component
// card, an offering that only an aggregator-style provider carries (so the
// api_family auto-match finds nothing and a human must pick coordinates), and a
// source decimal that is valid catalog data but too long for Prism storage.
const catalogFixtureCatalog = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "gpt-contract": {
        "id": "gpt-contract",
        "name": "GPT Contract",
        "description": "contract fixture model",
        "family": "gpt-contract",
        "attachment": false,
        "reasoning": true,
        "reasoning_options": [{"type": "effort", "values": [null, "low", "medium", "high"]}],
        "tool_call": true,
        "structured_output": true,
        "temperature": true,
        "release_date": "2026-01-15",
        "last_updated": "2026-02-20",
        "modalities": {"input": ["text"], "output": ["text"]},
        "open_weights": false,
        "limit": {"context": 400000, "output": 32768},
        "cost": {"input": 2.5, "output": 10, "cache_read": 0}
      },
      "gpt-five-part": {
        "id": "gpt-five-part",
        "name": "GPT Five Part",
        "release_date": "2026-05",
        "last_updated": "2026-06",
        "open_weights": false,
        "limit": {"context": 200000, "output": 16384},
        "cost": {"input": 1.25, "output": 10, "cache_read": 0, "cache_write": 1.5, "reasoning": 12.5}
      },
      "gpt-long": {
        "id": "gpt-long",
        "name": "GPT Long",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "limit": {"context": 400000, "output": 32768},
        "cost": {
          "input": 30, "output": 180,
          "tiers": [{"input": 60, "output": 270, "tier": {"type": "context", "size": 272000}}],
          "context_over_200k": {"input": 60, "output": 270}
        }
      },
      "gpt-audio": {
        "id": "gpt-audio",
        "name": "GPT Audio",
        "release_date": "2026-03",
        "last_updated": "2026-03",
        "open_weights": false,
        "cost": {"input": 5, "output": 20, "input_audio": 10, "output_audio": 40}
      }
    }
  },
  "azure": {
    "id": "azure",
    "name": "Azure",
    "models": {
      "shared-model": {
        "id": "shared-model",
        "name": "Shared Azure",
        "release_date": "2026-04",
        "last_updated": "2026-04",
        "open_weights": false,
        "cost": {"input": 1, "output": 2}
      }
    }
  },
  "chutes": {
    "id": "chutes",
    "name": "Chutes",
    "models": {
      "schema-edge": {
        "id": "schema-edge",
        "name": "Schema Edge",
        "cost": {"input": 0.0245, "output": 0.0978, "cache_read": 2.4499999999999995e-3}
      }
    }
  },
  "codex": {
    "id": "codex",
    "name": "Codex",
    "models": {
      "gpt-5.6-luna": {
        "id": "gpt-5.6-luna",
        "name": "GPT 5.6 Luna",
        "release_date": "2026-07",
        "last_updated": "2026-07",
        "open_weights": false,
        "cost": {"input": 0.5, "output": 4, "cache_read": 0.05}
      }
    }
  }
}`

func catalogAssertErrorContains(t *testing.T, response *http.Response, wantStatus int, wantFragment string) {
	t.Helper()
	assertStatus(t, response, wantStatus)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	detail, ok := payload["detail"].(string)
	if !ok {
		t.Fatalf("expected error detail string, got %+v", payload)
	}
	if !strings.Contains(detail, wantFragment) {
		t.Fatalf("expected error detail containing %q, got %+v", wantFragment, payload)
	}
}

func newCatalogContractHarness(t *testing.T, modelLockedObservers ...func(int)) *contractHarness {
	t.Helper()
	if len(modelLockedObservers) > 1 {
		t.Fatal("catalog contract harness accepts at most one model-locked observer")
	}
	var modelLockedObserver func(int)
	if len(modelLockedObservers) == 1 {
		modelLockedObserver = modelLockedObservers[0]
	}
	catalogServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"catalog-contract-1"`)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, catalogFixtureCatalog)
	}))
	t.Cleanup(catalogServer.Close)
	catalogClient, clientErr := modelsdev.NewClient(modelsdev.ClientOptions{BaseURL: catalogServer.URL, HTTPClient: catalogServer.Client()})
	if clientErr != nil {
		t.Fatalf("build catalog client: %v", clientErr)
	}
	return newContractHarnessFor(t, "catalog_contract", contractHarnessOptions{
		SecretEncryptionKey: "catalog-contract-secret",
		Version:             "catalog-contract-test",
		DependenciesBuilder: func(t *testing.T, testContext context.Context, harness *contractHarness, settings config.Settings, pool *pgxpool.Pool) platformhttp.Dependencies {
			t.Helper()
			endpointsService, err := managementendpoints.NewService(settings, managementendpoints.Options{Pool: pool})
			if err != nil {
				t.Fatalf("build endpoints service: %v", err)
			}
			t.Cleanup(endpointsService.Close)
			connectionsService, connectionsErr := managementconnections.NewService(settings, managementconnections.Options{Pool: pool, Catalog: catalogClient})
			if connectionsErr != nil {
				t.Fatalf("build connections service: %v", connectionsErr)
			}
			t.Cleanup(connectionsService.Close)
			modelsService, modelsErr := managementmodels.NewService(settings, managementmodels.Options{Pool: pool, Catalog: catalogClient, CatalogWriteModelLocked: modelLockedObserver})
			if modelsErr != nil {
				t.Fatalf("build models service: %v", modelsErr)
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

func catalogCreateOpenAIModel(t *testing.T, harness *contractHarness, strategyID int, modelID string) map[string]any {
	t.Helper()
	body := map[string]any{
		"api_family":              "openai",
		"model_id":                modelID,
		"loadbalance_strategy_id": strategyID,
		"openai_accepted_format":  "dual_native",
		"display_name":            nil,
		"is_enabled":              false,
	}
	return requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models", body, nil, http.StatusCreated)["model"].(map[string]any)
}

// catalogBindBody carries the Prism identity every bind must confirm: the
// write transaction re-verifies both fields under the model row lock.
func catalogBindBody(prismModelID string, overrides map[string]any) map[string]any {
	payload := map[string]any{
		"expected_prism_model_id": prismModelID,
		"expected_api_family":     "openai",
	}
	for key, value := range overrides {
		payload[key] = value
	}
	return payload
}

// catalogRefreshCommitBody echoes the preview's coordinate and updated_at
// token plus the catalog revision the preview was read against.
func catalogRefreshCommitBody(preview map[string]any, revision string) map[string]any {
	return map[string]any{
		"expected_provider_id":        preview["provider_id"],
		"expected_catalog_model_id":   preview["catalog_model_id"],
		"expected_binding_updated_at": preview["binding_updated_at"],
		"expected_catalog_revision":   revision,
	}
}

// catalogUnbindBody carries the binding snapshot the operator confirmed.
func catalogUnbindBody(bound map[string]any) map[string]any {
	return map[string]any{
		"expected_provider_id":        bound["provider_id"],
		"expected_catalog_model_id":   bound["catalog_model_id"],
		"expected_binding_updated_at": bound["updated_at"],
	}
}

func TestModelCatalogBindingAndOverrideContracts(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-contract")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	// C1/C2: unbound read carries the auto-match hint once the catalog is cached.
	unbound := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	if unbound["bound"] != false {
		t.Fatalf("expected unbound payload, got %+v", unbound)
	}

	// Unique exact match previews committable.
	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	if matchPreview["committable"] != true || matchPreview["provider_id"] != "openai" || matchPreview["reason"] != "unique_match" {
		t.Fatalf("unexpected match preview %+v", matchPreview)
	}
	wrongMethod := modelResponse(t, harness, profileID, http.MethodGet, catalogPath+"/match-preview", nil)
	assertStatus(t, wrongMethod, http.StatusMethodNotAllowed)
	revision := matchPreview["catalog_revision"].(string)
	candidates := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath+"/candidates?scope=all&limit=20", nil, nil, http.StatusOK)
	if jsonInt(t, candidates["total"]) < 1 || len(candidates["items"].([]any)) < 1 {
		t.Fatalf("catalog candidates must remain available: %+v", candidates)
	}
	// Every candidate page publishes the snapshot revision it was computed
	// from; this endpoint returns an already-validated snapshot, so it
	// deliberately does not fabricate a fresh/stale enum.
	if candidates["catalog_revision"] != revision || candidates["fetched_at"] == nil {
		t.Fatalf("candidate page must carry snapshot revision evidence: %+v", candidates)
	}

	// Bind without coordinates applies the unique match; display_name stays nil.
	bound := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-contract", map[string]any{"expected_catalog_revision": revision}), nil, http.StatusOK)
	if bound["bound"] != true || bound["match_source"] != "unique_match" || bound["provider_id"] != "openai" || bound["catalog_model_id"] != "gpt-contract" {
		t.Fatalf("unexpected bound payload %+v", bound)
	}
	source := bound["source"].(map[string]any)
	if source["name"] != "GPT Contract" || source["description"] != "contract fixture model" {
		t.Fatalf("source snapshot incomplete: %+v", source)
	}
	if cached := source["modalities_input"].([]any); len(cached) != 1 || cached[0] != "text" {
		t.Fatalf("modalities must survive as arrays: %+v", source["modalities_input"])
	}
	if limitContext, ok := source["limit_context"].(float64); !ok || limitContext != 400000 {
		t.Fatalf("limit_context must survive verbatim: %v", source["limit_context"])
	}
	effective := bound["effective"].(map[string]any)
	if effective["name"] != "GPT Contract" {
		t.Fatalf("effective defaults to source: %+v", effective)
	}

	detail := requestJSONStatus[map[string]any](t, harness, http.MethodGet, fmt.Sprintf("/api/models/%d", modelConfigID), nil, nil, http.StatusOK)
	if detail["catalog"] == nil {
		t.Fatalf("model detail must embed catalog: %+v", detail)
	}
	// Source name never overwrites display_name: it keeps the create-time
	// resync value even though the bound catalog name differs in casing.
	if detail["display_name"] != "gpt-contract" {
		t.Fatalf("catalog binding must not change display_name: %+v", detail["display_name"])
	}
	boundCatalog := detail["catalog"].(map[string]any)
	if boundCatalog["source"].(map[string]any)["name"] != "GPT Contract" {
		t.Fatalf("source name should differ from display_name: %+v", boundCatalog["source"])
	}

	// Ambiguous ids cannot auto-bind but can bind explicitly.
	ambiguousPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/"+fmt.Sprint(modelConfigID)+"/catalog/match-preview", nil, nil, http.StatusOK)

	manualBody := func(overrides map[string]any) map[string]any {
		payload := catalogBindBody("gpt-contract", map[string]any{"provider_id": "azure", "catalog_model_id": "shared-model", "expected_catalog_revision": revision})
		for key, value := range overrides {
			payload[key] = value
		}
		return payload
	}
	assertStatus(t, modelResponse(t, harness, profileID, http.MethodPost, catalogPath+"/bind", manualBody(nil)), http.StatusOK)
	rebound := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	if rebound["match_source"] != "manual" || rebound["provider_id"] != "azure" {
		t.Fatalf("explicit binding must persist: %+v", rebound)
	}
	if _, hasOverrides := rebound["override"]; !hasOverrides {
		t.Fatalf("override key must always serialize: %+v", rebound)
	}

	// Stale revisions cannot commit.
	stale := modelResponse(t, harness, profileID, http.MethodPost, catalogPath+"/bind", manualBody(map[string]any{"expected_catalog_revision": "\"outdated\""}))
	catalogAssertErrorContains(t, stale, http.StatusConflict, "models_dev_catalog_stale")

	// Per-field overrides merge over source and survive refreshes. The raw
	// body bypasses ensureOpenAIAcceptedFormat, which decorates plain
	// model-path PUT payloads and is not part of this contract.
	overridden := requestJSONStatus[map[string]any](t, harness, http.MethodPut, catalogPath+"/override",
		json.RawMessage(`{"name":"Operator Name","limit_context":999999,"status":null}`), nil, http.StatusOK)
	if _, mutated := overridden["openai_accepted_format"]; mutated {
		t.Fatalf("override payload must not be decorated: %+v", overridden)
	}
	overridePayload := overridden["override"].(map[string]any)
	if overridePayload == nil || overridePayload["name"] != "Operator Name" {
		t.Fatalf("override must store operator values: %+v", overridePayload)
	}
	// The model is bound to azure/shared-model at this point; its source has
	// no description, so an absent source field must stay absent while the
	// overridden name merges over it.
	effectiveAfterOverride := overridden["effective"].(map[string]any)
	if effectiveAfterOverride["name"] != "Operator Name" {
		t.Fatalf("override must merge over source: %+v", effectiveAfterOverride)
	}
	if effectiveAfterOverride["description"] != nil || effectiveAfterOverride["release_date"] != "2026-04" {
		t.Fatalf("unoverridden fields must fall through to source: %+v", effectiveAfterOverride)
	}

	// Refresh preview publishes the local CAS snapshot; the commit must echo
	// coordinate, token, and revision back or it cannot land.
	refreshPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/refresh/preview", map[string]any{}, nil, http.StatusOK)
	if refreshPreview["binding_updated_at"] == nil || refreshPreview["provider_id"] != "azure" || refreshPreview["catalog_model_id"] != "shared-model" {
		t.Fatalf("refresh preview must publish the binding CAS snapshot: %+v", refreshPreview)
	}
	refreshed := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/refresh/commit", catalogRefreshCommitBody(refreshPreview, revision), nil, http.StatusOK)
	if refreshed["override"] == nil || refreshed["override"].(map[string]any)["name"] != "Operator Name" {
		t.Fatalf("refresh must preserve manual overrides: %+v", refreshed)
	}
	if refreshed["fetched_at"] == nil || refreshed["catalog_revision"] != revision {
		t.Fatalf("refresh must advance the fetch stamp: %+v", refreshed)
	}

	// Null restores one field to source; bulk delete clears everything.
	restoredField := requestJSONStatus[map[string]any](t, harness, http.MethodPut, catalogPath+"/override", json.RawMessage(`{"name":null}`), nil, http.StatusOK)
	if restoredField["effective"].(map[string]any)["name"] != "Shared Azure" {
		t.Fatalf("null override must restore the azure source name: %+v", restoredField)
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodDelete, catalogPath+"/override", nil, nil, http.StatusOK)
	cleared := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	if cleared["override"] != nil {
		t.Fatalf("bulk clear must remove all overrides: %+v", cleared)
	}

	// Unbind returns to the unbound shape without touching runtime identity.
	// The delete carries the binding snapshot the operator saw; without it the
	// request rejects before touching the row, and the same snapshot stays
	// idempotent once the row is already gone.
	boundBeforeUnbind := requestJSONStatus[map[string]any](t, harness, http.MethodGet, catalogPath, nil, nil, http.StatusOK)
	missingSnapshot := modelResponse(t, harness, profileID, http.MethodDelete, catalogPath, nil)
	catalogAssertErrorContains(t, missingSnapshot, http.StatusUnprocessableEntity, "unbind requires the binding coordinate")
	unboundAgain := requestJSONStatus[map[string]any](t, harness, http.MethodDelete, catalogPath, catalogUnbindBody(boundBeforeUnbind), nil, http.StatusOK)
	if unboundAgain["bound"] != false {
		t.Fatalf("unbind must return an unbound payload: %+v", unboundAgain)
	}
	unboundTwice := requestJSONStatus[map[string]any](t, harness, http.MethodDelete, catalogPath, catalogUnbindBody(boundBeforeUnbind), nil, http.StatusOK)
	if unboundTwice["bound"] != false {
		t.Fatalf("repeat unbind must stay idempotent: %+v", unboundTwice)
	}
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM model_catalog_bindings WHERE model_config_id = $1`, modelConfigID, 0)
	if strings.Contains(ambiguousPreview["reason"].(string), "unique") && ambiguousPreview["committable"] != true {
		t.Fatalf("fixture unique match preview drifted: %+v", ambiguousPreview)
	}
}

func TestCatalogPricingImportAtomicAssignmentContracts(t *testing.T) {
	harness := newCatalogContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Catalog Pricing Strategy")
	model := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-long")
	modelConfigID := jsonInt(t, model["id"])
	catalogPath := fmt.Sprintf("/api/models/%d/catalog", modelConfigID)

	matchPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/match-preview", map[string]any{}, nil, http.StatusOK)
	revision := matchPreview["catalog_revision"].(string)
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, catalogPath+"/bind", catalogBindBody("gpt-long", map[string]any{"expected_catalog_revision": revision}), nil, http.StatusOK)

	terminalTarget := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/connections", modelConfigID), map[string]any{
		"endpoint_create":        map[string]any{"name": "Catalog Endpoint", "base_url": "https://catalog.example/v1", "api_key": "sk-catalog"},
		"name":                   "Catalog Target",
		"is_active":              true,
		"openai_text_capability": "dual_native",
	}, nil, http.StatusCreated)["connection"].(map[string]any)
	connectionID := jsonInt(t, terminalTarget["id"])

	previewPath := "/api/pricing-templates/catalog/preview"
	commitPath := "/api/pricing-templates/catalog/commit"

	previewRequest := func(connections []int) map[string]any {
		return map[string]any{"model_config_id": modelConfigID, "connection_ids": connections}
	}
	preview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, previewPath, previewRequest([]int{connectionID}), nil, http.StatusOK)
	if preview["committable"] != true || preview["action"] != "create" {
		t.Fatalf("first import must offer creation: %+v", preview)
	}
	plan := preview["plan"].(map[string]any)
	if plan["template_kind"] != "tiered" {
		t.Fatalf("single context tier must map to tiered: %+v", plan)
	}
	if threshold, ok := plan["tier_input_tokens_above"].(float64); !ok || threshold != 272000 {
		t.Fatalf("tier size must land verbatim in input_tokens_above: %v", plan["tier_input_tokens_above"])
	}
	baseCard := plan["cards"].(map[string]any)["tier_base"].(map[string]any)
	if baseCard["input_price"] != "30" || baseCard["cached_input_price"] != nil {
		t.Fatalf("base card mapping drifted: %+v", baseCard)
	}
	targets := preview["targets"].([]any)
	if len(targets) != 1 || asMap(t, targets[0])["connection_id"].(float64) != float64(connectionID) {
		t.Fatalf("preview must echo target CAS state: %+v", targets)
	}

	previewHash := preview["preview_hash"].(string)
	expectedRevision := preview["catalog_revision"].(string)

	// Stale catalog revision cannot commit.
	staleResponse := modelResponse(t, harness, profileID, http.MethodPost, commitPath, map[string]any{
		"schema_version": 1, "model_config_id": modelConfigID, "connection_ids": []int{connectionID},
		"preview_hash": previewHash, "expected_catalog_revision": "\"older-revision\"",
	})
	catalogAssertErrorContains(t, staleResponse, http.StatusConflict, "models_dev_catalog_stale")

	committed := requestJSONStatus[map[string]any](t, harness, http.MethodPost, commitPath, map[string]any{
		"schema_version": 1, "model_config_id": modelConfigID, "connection_ids": []int{connectionID},
		"preview_hash": previewHash, "expected_catalog_revision": expectedRevision,
	}, nil, http.StatusOK)
	if committed["created"] != true || committed["assigned_connection_ids"].([]any)[0].(float64) != float64(connectionID) {
		t.Fatalf("unexpected commit result: %+v", committed)
	}
	templateID := jsonInt(t, committed["template_id"])

	var linkProvider, linkModel string
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT catalog_provider_id, catalog_model_id FROM pricing_templates WHERE id = $1`, templateID).Scan(&linkProvider, &linkModel); err != nil {
		t.Fatalf("load template link columns: %v", err)
	}
	if linkProvider != "openai" || linkModel != "gpt-long" {
		t.Fatalf("template must carry offering coordinates: %q/%q", linkProvider, linkModel)
	}
	var revisionSource string
	var catalogRevision *string
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT revision_source, catalog_revision FROM pricing_template_revisions WHERE id = $1`, committed["revision_id"]).Scan(&revisionSource, &catalogRevision); err != nil {
		t.Fatalf("load revision source evidence: %v", err)
	}
	if revisionSource != "catalog" || catalogRevision == nil || *catalogRevision != expectedRevision {
		t.Fatalf("import revision must carry source evidence: %q %v", revisionSource, catalogRevision)
	}
	var assignedTemplate *int
	var targetUpdatedAt time.Time
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT pricing_template_id, updated_at FROM connections WHERE id = $1`, connectionID).Scan(&assignedTemplate, &targetUpdatedAt); err != nil {
		t.Fatalf("load terminal target: %v", err)
	}
	if assignedTemplate == nil || *assignedTemplate != templateID {
		t.Fatalf("terminal target must reference the imported template: %v", assignedTemplate)
	}

	// Re-import with unchanged state reuses the linked template.
	reusedPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, previewPath, previewRequest([]int{connectionID}), nil, http.StatusOK)
	if reusedPreview["action"] != "reuse" {
		t.Fatalf("unchanged re-import must reuse: %+v", reusedPreview)
	}

	// Manual drift requires explicit confirmation.
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/pricing-templates/%d", templateID), map[string]any{
		"base_card": map[string]any{"input_price": "99", "output_price": "199", "cached_input_price": nil, "cache_creation_price": nil, "reasoning_price": nil},
		"tier": map[string]any{
			"input_tokens_above": 272000,
			"card":               map[string]any{"input_price": "60", "output_price": "270", "cached_input_price": nil, "cache_creation_price": nil, "reasoning_price": nil},
		},
	}, nil, http.StatusOK)
	driftedPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, previewPath, previewRequest([]int{connectionID}), nil, http.StatusOK)
	if driftedPreview["drift"] != true || driftedPreview["action"] != "drift" {
		t.Fatalf("manual drift must surface: %+v", driftedPreview)
	}
	unconfirmed := modelResponse(t, harness, profileID, http.MethodPost, commitPath, map[string]any{
		"schema_version": 1, "model_config_id": modelConfigID, "connection_ids": []int{connectionID},
		"preview_hash": driftedPreview["preview_hash"].(string), "expected_catalog_revision": driftedPreview["catalog_revision"].(string),
	})
	catalogAssertErrorContains(t, unconfirmed, http.StatusConflict, "models_dev_pricing_drift_unconfirmed")
	confirmed := requestJSONStatus[map[string]any](t, harness, http.MethodPost, commitPath, map[string]any{
		"schema_version": 1, "model_config_id": modelConfigID, "connection_ids": []int{connectionID},
		"preview_hash": driftedPreview["preview_hash"].(string), "expected_catalog_revision": driftedPreview["catalog_revision"].(string),
		"confirm_drift": true,
	}, nil, http.StatusOK)
	if confirmed["updated"] != true || confirmed["drift_confirmed"] != true {
		t.Fatalf("confirmed drift must append an import revision: %+v", confirmed)
	}
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM pricing_template_revisions WHERE template_id = $1 AND revision_source = 'catalog'`, templateID, 2)

	// A concurrent Terminal Target change between preview and commit aborts
	// the whole transaction: no new revision, no assignment.
	freshPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, previewPath, previewRequest([]int{connectionID}), nil, http.StatusOK)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET name = name WHERE id = $1 AND updated_at = updated_at`, connectionID); err != nil {
		t.Fatalf("touch connection: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET updated_at = now() WHERE id = $1`, connectionID); err != nil {
		t.Fatalf("bump connection updated_at: %v", err)
	}
	conflict := modelResponse(t, harness, profileID, http.MethodPost, commitPath, map[string]any{
		"schema_version": 1, "model_config_id": modelConfigID, "connection_ids": []int{connectionID},
		"preview_hash": freshPreview["preview_hash"].(string), "expected_catalog_revision": freshPreview["catalog_revision"].(string),
	})
	// The replay identity covers every target's CAS columns, so an external
	// touch surfaces as a stale preview; either way the commit is rejected
	// before any write lands.
	catalogAssertErrorContains(t, conflict, http.StatusConflict, "models_dev_pricing_preview_stale")
	var revisionCount int
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pricing_template_revisions WHERE template_id = $1`, templateID).Scan(&revisionCount); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	// The failed commit must leave zero new rows behind: revision count and
	// the current revision's version both stay where the confirmed drift left
	// them.
	var versionAfterDrift int
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT revisions.version FROM pricing_templates AS templates
		 JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
		 WHERE templates.id = $1`, templateID).Scan(&versionAfterDrift); err != nil {
		t.Fatalf("load current revision version: %v", err)
	}
	if revisionCount != versionAfterDrift {
		t.Fatalf("failed commit moved bookkeeping: revisions=%d version=%d", revisionCount, versionAfterDrift)
	}

	// Incompatible prices stay zero-write with a stable reason.
	audioModel := catalogCreateOpenAIModel(t, harness, strategyID, "gpt-audio")
	audioModelID := jsonInt(t, audioModel["id"])
	audioBind := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/bind", audioModelID), catalogBindBody("gpt-audio", map[string]any{"provider_id": "openai", "catalog_model_id": "gpt-audio", "expected_catalog_revision": revision}), nil, http.StatusOK)
	if audioBind["bound"] != true {
		t.Fatalf("audio model must bind: %+v", audioBind)
	}
	audioPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, previewPath, map[string]any{"model_config_id": audioModelID}, nil, http.StatusOK)
	audioPlan := audioPreview["plan"].(map[string]any)
	if audioPreview["committable"] != false {
		t.Fatalf("audio costs must fail closed: %+v", audioPreview)
	}
	incompatibilities := audioPlan["incompatibilities"].([]any)
	foundAudioReason := false
	for _, item := range incompatibilities {
		if asMap(t, item)["reason"] == "audio_cost_present" {
			foundAudioReason = true
		}
	}
	if !foundAudioReason {
		t.Fatalf("stable audio reason missing: %+v", incompatibilities)
	}
	audioCommit := modelResponse(t, harness, profileID, http.MethodPost, commitPath, map[string]any{
		"schema_version": 1, "model_config_id": audioModelID, "connection_ids": []int{},
		"preview_hash": audioPreview["preview_hash"].(string), "expected_catalog_revision": audioPreview["catalog_revision"].(string),
	})
	catalogAssertErrorContains(t, audioCommit, http.StatusUnprocessableEntity, "models_dev_pricing_incompatible")
	var audioTemplates int
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pricing_templates WHERE catalog_provider_id = 'openai' AND catalog_model_id = 'gpt-audio' AND deleted_at IS NULL`).Scan(&audioTemplates); err != nil {
		t.Fatalf("count audio templates: %v", err)
	}
	if audioTemplates != 0 {
		t.Fatalf("incompatible import must not create templates, found %d", audioTemplates)
	}

	// A valid catalog decimal that exceeds Prism's 20-character storage
	// boundary remains previewable as source evidence, but commit rejects it
	// before creating a revision or changing any requested target assignment.
	schemaEdgeModel := catalogCreateOpenAIModel(t, harness, strategyID, "schema-edge")
	schemaEdgeModelID := jsonInt(t, schemaEdgeModel["id"])
	schemaEdgeBind := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/bind", schemaEdgeModelID), catalogBindBody("schema-edge", map[string]any{
		"provider_id": "chutes", "catalog_model_id": "schema-edge", "expected_catalog_revision": revision,
	}), nil, http.StatusOK)
	if schemaEdgeBind["bound"] != true {
		t.Fatalf("schema-edge model must bind: %+v", schemaEdgeBind)
	}

	var revisionsBefore int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM pricing_template_revisions`).Scan(&revisionsBefore); err != nil {
		t.Fatalf("count revisions before incompatible import: %v", err)
	}
	var assignedTemplateBefore *int
	var targetUpdatedAtBefore time.Time
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT pricing_template_id, updated_at FROM connections WHERE id = $1`, connectionID).Scan(&assignedTemplateBefore, &targetUpdatedAtBefore); err != nil {
		t.Fatalf("load target before incompatible import: %v", err)
	}

	schemaEdgePreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, previewPath, map[string]any{
		"model_config_id": schemaEdgeModelID, "connection_ids": []int{connectionID},
	}, nil, http.StatusOK)
	if schemaEdgePreview["committable"] != false {
		t.Fatalf("unrepresentable catalog price must fail closed: %+v", schemaEdgePreview)
	}
	schemaEdgePlan := asMap(t, schemaEdgePreview["plan"])
	schemaEdgeCards := asMap(t, schemaEdgePlan["cards"])
	schemaEdgeStandardCard := asMap(t, schemaEdgeCards["standard"])
	if schemaEdgeStandardCard["cached_input_price"] != "0.0024499999999999995" {
		t.Fatalf("preview must preserve the exact catalog decimal: %+v", schemaEdgeStandardCard)
	}
	schemaEdgeIncompatibilities := schemaEdgePlan["incompatibilities"].([]any)
	if len(schemaEdgeIncompatibilities) != 1 {
		t.Fatalf("expected one exact storage incompatibility, got %+v", schemaEdgeIncompatibilities)
	}
	schemaEdgeIncompatibility := asMap(t, schemaEdgeIncompatibilities[0])
	if schemaEdgeIncompatibility["field"] != "cost.cache_read" || schemaEdgeIncompatibility["reason"] != "price_not_representable" {
		t.Fatalf("unrepresentable price reason drifted: %+v", schemaEdgeIncompatibility)
	}

	schemaEdgeCommit := modelResponse(t, harness, profileID, http.MethodPost, commitPath, map[string]any{
		"schema_version": 1, "model_config_id": schemaEdgeModelID, "connection_ids": []int{connectionID},
		"preview_hash": schemaEdgePreview["preview_hash"].(string), "expected_catalog_revision": schemaEdgePreview["catalog_revision"].(string),
	})
	catalogAssertErrorContains(t, schemaEdgeCommit, http.StatusUnprocessableEntity, "models_dev_pricing_incompatible")
	assertCountQuery(t, harness, `SELECT COUNT(*) FROM pricing_templates WHERE profile_id = $1 AND catalog_provider_id = 'chutes' AND catalog_model_id = 'schema-edge' AND deleted_at IS NULL`, profileID, 0)
	var revisionsAfter int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM pricing_template_revisions`).Scan(&revisionsAfter); err != nil {
		t.Fatalf("count revisions after incompatible import: %v", err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("incompatible import created revisions: before=%d after=%d", revisionsBefore, revisionsAfter)
	}
	var assignedTemplateAfter *int
	var targetUpdatedAtAfter time.Time
	if err := harness.conn.QueryRow(context.Background(),
		`SELECT pricing_template_id, updated_at FROM connections WHERE id = $1`, connectionID).Scan(&assignedTemplateAfter, &targetUpdatedAtAfter); err != nil {
		t.Fatalf("load target after incompatible import: %v", err)
	}
	if assignedTemplateBefore == nil || assignedTemplateAfter == nil || *assignedTemplateAfter != *assignedTemplateBefore || !targetUpdatedAtAfter.Equal(targetUpdatedAtBefore) {
		t.Fatalf("incompatible import changed target assignment: before=%v/%s after=%v/%s", assignedTemplateBefore, targetUpdatedAtBefore, assignedTemplateAfter, targetUpdatedAtAfter)
	}
}

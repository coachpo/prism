package contracttest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/jackc/pgx/v5/pgxpool"
)

// exportCompleteCard prices every one of the five components with reasoning
// equal to output, the only shape both client files can express losslessly.
const exportCompleteCard = `{
	"input_price": "3",
	"output_price": "15",
	"cached_input_price": "0.3",
	"cache_creation_price": "3.75",
	"reasoning_price": "15"
}`

func newExportContractHarness(t *testing.T, catalogHandler http.HandlerFunc) *contractHarness {
	t.Helper()
	catalogServer := httptest.NewTLSServer(catalogHandler)
	t.Cleanup(catalogServer.Close)
	catalogClient, clientErr := modelsdev.NewClient(modelsdev.ClientOptions{BaseURL: catalogServer.URL, HTTPClient: catalogServer.Client()})
	if clientErr != nil {
		t.Fatalf("build catalog client: %v", clientErr)
	}
	return newContractHarnessFor(t, "export_contract", contractHarnessOptions{
		SecretEncryptionKey: "export-contract-secret",
		Version:             "export-contract-test",
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
			modelsService, modelsErr := managementmodels.NewService(settings, managementmodels.Options{Pool: pool, Catalog: catalogClient})
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

// exportFixtureCatalog carries one OpenAI offering whose metadata backs the
// bound fixture model.
const exportFixtureCatalog = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "gpt-export": {
        "id": "gpt-export",
        "name": "GPT Export",
        "family": "gpt-export",
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 400000, "output": 32768},
        "reasoning_options": [{"type": "effort", "values": ["low", "medium", "high"]}],
        "cost": {"input": 2.5, "output": 10}
      }
    }
  }
}`

func exportServingCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", `"export-contract-1"`)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, exportFixtureCatalog)
}

func exportFailingCatalog(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "catalog unavailable", http.StatusBadGateway)
}

// exportSeedModel composite-creates an enabled dual_native model together
// with its first active Terminal Target on a keyed endpoint bound to a
// complete standard price template.
func exportSeedModel(t *testing.T, harness *contractHarness, modelID string) (modelConfigID int, connectionID int) {
	t.Helper()
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Export Strategy "+modelID)

	template := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/pricing-templates", map[string]any{
		"name":          "Export Standard " + modelID,
		"template_kind": "standard",
		"card":          jsonDecodeRaw(t, exportCompleteCard),
	}, nil, http.StatusCreated)
	templateID := jsonInt(t, template["id"])

	payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models", map[string]any{
		"api_family":              "openai",
		"model_id":                modelID,
		"loadbalance_strategy_id": strategyID,
		"openai_accepted_format":  "dual_native",
		"is_enabled":              true,
		"initial_terminal_target": map[string]any{
			"endpoint_create":        map[string]any{"name": "Export Endpoint", "base_url": "https://export.example/v1", "api_key": "sk-export-live-key"},
			"name":                   "Export Target",
			"is_active":              true,
			"pricing_template_id":    templateID,
			"openai_text_capability": "dual_native",
		},
	}, nil, http.StatusCreated)["model"].(map[string]any)
	modelConfigID = jsonInt(t, payload["id"])
	targets := payload["access_targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("seeded model must carry exactly one target: %+v", targets)
	}
	connection := asMap(t, asMap(t, targets[0])["connection"])
	return modelConfigID, jsonInt(t, connection["id"])
}

func jsonDecodeRaw(t *testing.T, payload string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("decode fixture card: %v", err)
	}
	return decoded
}

func exportFetchSource(t *testing.T, harness *contractHarness, platform string) map[string]any {
	t.Helper()
	return requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/models/exports/"+platform+"/source", nil, nil, http.StatusOK)
}

func exportRequestWithHeaders(t *testing.T, harness *contractHarness, method string, path string, body any, status int) (map[string]any, http.Header) {
	t.Helper()
	response := harness.requestJSON(t, harness.client, method, path, body, nil)
	assertStatus(t, response, status)
	headers := response.Header.Clone()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload, headers
}

func TestModelExportSourceAndRenderContracts(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export")

	// Bind the catalog offering so enrichment has live data.
	bindRevision := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/match-preview", modelConfigID), map[string]any{}, nil, http.StatusOK)["catalog_revision"].(string)
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/bind", modelConfigID), map[string]any{"expected_catalog_revision": bindRevision}, nil, http.StatusOK)

	source, sourceHeaders := exportRequestWithHeaders(t, harness, http.MethodGet, "/api/models/exports/pi/source", nil, http.StatusOK)
	if got := sourceHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("source cache policy = %q, want private, no-store", got)
	}
	if source["platform"] != "pi" {
		t.Fatalf("source platform drifted: %+v", source["platform"])
	}
	digest := source["source_digest"].(string)
	if len(digest) != 64 {
		t.Fatalf("source_digest must be sha256 hex: %q", digest)
	}
	models := source["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("exactly one seeded model expected: %+v", models)
	}
	row := asMap(t, models[0])
	if row["model_config_id"].(float64) != float64(modelConfigID) || row["default_selected"] != true || row["selectable"] != true {
		t.Fatalf("selection truth mismatch: %+v", row)
	}
	enrichment := row["enrichment"].(map[string]any)
	if enrichment["available"] != true {
		t.Fatalf("serving catalog must mark enrichment available: %+v", enrichment)
	}
	completeness := row["platform_completeness"].(map[string]any)
	fields := completeness["metadata_fields"].(map[string]any)
	for _, key := range []string{"name", "reasoning", "contextWindow", "maxTokens", "thinkingLevelMap"} {
		if fields[key] != true {
			t.Fatalf("pi field %s must project as known: %+v", key, fields)
		}
	}
	priceRisk := row["price_risk"].(map[string]any)
	if priceRisk["exportable"] != true {
		t.Fatalf("complete five-component USD PER_1M price must export: %+v", priceRisk)
	}

	renderRequest := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"provider_id":            "custom-prism",
		"credential":             map[string]any{"include": true, "api_key": " proxy-key "},
	}
	rendered, renderHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/pi/render", renderRequest, http.StatusOK)
	if got := renderHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("render cache policy = %q, want private, no-store", got)
	}
	content := rendered["content"].(string)
	if !strings.HasSuffix(content, "\n") || strings.HasSuffix(content, "\n\n") {
		t.Fatalf("rendered content must end with exactly one newline")
	}
	sum := sha256.Sum256([]byte(content))
	if hex.EncodeToString(sum[:]) != rendered["content_sha256"] {
		t.Fatalf("content sha256 must match bytes exactly")
	}
	if rendered["file_name"] != "prism-pi-models.json" {
		t.Fatalf("pi file name must be fixed: %+v", rendered["file_name"])
	}
	if rendered["mime_type"] != "application/json;charset=utf-8" {
		t.Fatalf("pi MIME must be fixed: %+v", rendered["mime_type"])
	}
	if strings.Contains(content, "https://export.example") || strings.Contains(content, "sk-export-live-key") {
		t.Fatalf("render leaked an upstream endpoint URL or stored endpoint key: %s", content)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		t.Fatalf("rendered content must parse: %v", err)
	}
	provider := document["providers"].(map[string]any)["custom-prism"].(map[string]any)
	providerModels := provider["models"].([]any)[0].(map[string]any)
	if providerModels["api"] != "openai-responses" {
		t.Fatalf("dual_native pins Responses: %+v", providerModels)
	}
	if providerModels["baseUrl"] != "https://prism-client.example/v1" || provider["apiKey"] != "proxy-key" {
		t.Fatalf("base URL and credential must come only from final operator input: %+v", provider)
	}
	if cost, ok := providerModels["cost"].(map[string]any); !ok || cost["input"].(float64) != 3 {
		t.Fatalf("complete flat price must render: %+v", providerModels["cost"])
	}

	// Deterministic replay: identical body renders byte-identical output.
	reRendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", renderRequest, nil, http.StatusOK)
	if reRendered["content"] != content || reRendered["content_sha256"] != rendered["content_sha256"] {
		t.Fatalf("render replay must be deterministic")
	}

	// Digest drift fails closed with the stable code before any rendering.
	staleBody := map[string]any{
		"expected_source_digest": strings.Repeat("0", 64),
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
	}
	stale, staleHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/pi/render", staleBody, http.StatusConflict)
	if stale["code"] != "export_source_stale" {
		t.Fatalf("drift must return export_source_stale: %+v", stale)
	}
	if got := staleHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("render error cache policy = %q, want private, no-store", got)
	}

	// Unknown ids fail the whole request with 422.
	unknown := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{999999},
		"base_url":               "https://prism-client.example",
	}
	unknownResponse := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", unknown, nil, http.StatusUnprocessableEntity)
	if unknownResponse["code"] != "export_model_unselectable" {
		t.Fatalf("unknown id must fail closed: %+v", unknownResponse)
	}

	// A deprecated pre-release candidate field remains accepted-but-ignored only
	// so the frozen verification adapter can prove it never becomes truth.
	tampered := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"provider_id":            "custom-prism",
		"credential":             map[string]any{"include": true, "api_key": " proxy-key "},
		"enrichment_candidates": map[int]any{
			modelConfigID: map[string]any{
				"metadata": map[string]any{"name": "tampered-client-name"},
				"derived":  map[string]any{"thinkingLevelMap": map[string]any{"high": "tampered"}},
			},
		},
	}
	tamperedRender, compatibilityHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/pi/render", tampered, http.StatusOK)
	if got := compatibilityHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("compatibility render cache policy = %q, want private, no-store", got)
	}
	if strings.Contains(tamperedRender["content"].(string), "tampered") {
		t.Fatalf("request-carried candidate influenced rendered content: %s", tamperedRender["content"])
	}
	if tamperedRender["content"] != content || tamperedRender["content_sha256"] != rendered["content_sha256"] {
		t.Fatalf("ignored candidate changed deterministic server-owned output")
	}

	invalidEnhancement := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"enhancements": map[int]any{
			modelConfigID: map[string]any{"fields": map[string]any{"reasoning": "yes"}},
		},
	}, nil, http.StatusUnprocessableEntity)
	if invalidEnhancement["code"] != "target_schema_invalid" || invalidEnhancement["field"] != "reasoning" {
		t.Fatalf("invalid target enhancement must return the stable schema error: %+v", invalidEnhancement)
	}
}

func TestModelExportOpenCodeRenderContract(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export")

	bindRevision := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/match-preview", modelConfigID), map[string]any{}, nil, http.StatusOK)["catalog_revision"].(string)
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/bind", modelConfigID), map[string]any{"expected_catalog_revision": bindRevision}, nil, http.StatusOK)

	source := exportFetchSource(t, harness, "opencode")
	digest := source["source_digest"].(string)

	rendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
	}, nil, http.StatusOK)
	var document map[string]any
	if err := json.Unmarshal([]byte(rendered["content"].(string)), &document); err != nil {
		t.Fatalf("opencode content must parse: %v", err)
	}
	provider := document["provider"].(map[string]any)["prism"].(map[string]any)
	env := provider["env"].([]any)
	if len(env) != 1 || env[0] != "PRISM_API_KEY" {
		t.Fatalf("provider env slot must wire PRISM_API_KEY: %+v", env)
	}
	if _, hasPlaintext := provider["options"].(map[string]any)["apiKey"]; hasPlaintext {
		t.Fatalf("no plaintext key may appear when credential.include is false")
	}
	modelEntry := provider["models"].(map[string]any)["gpt-export"].(map[string]any)
	protocolSlot := modelEntry["provider"].(map[string]any)
	if protocolSlot["npm"] != "@ai-sdk/openai" || protocolSlot["api"] != "https://prism-client.example/v1" {
		t.Fatalf("per-model protocol slot drifted: %+v", protocolSlot)
	}
	if _, exists := modelEntry["variants"]; exists {
		t.Fatalf("models.dev reasoning options must not synthesize OpenCode variants: %+v", modelEntry)
	}
	if _, exists := document["model"]; exists {
		t.Fatalf("OpenCode must not invent a default model: %+v", document["model"])
	}
	if rendered["file_name"] != "opencode-prism.json" {
		t.Fatalf("OpenCode file name must be fixed: %+v", rendered["file_name"])
	}

	withDefault := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/opencode/render", map[string]any{
		"expected_source_digest":  digest,
		"model_config_ids":        []int{modelConfigID},
		"base_url":                "https://prism-client.example",
		"default_model_config_id": modelConfigID,
	}, nil, http.StatusOK)
	var defaultDocument map[string]any
	if err := json.Unmarshal([]byte(withDefault["content"].(string)), &defaultDocument); err != nil {
		t.Fatalf("decode explicit default content: %v", err)
	}
	if defaultDocument["model"] != "prism/gpt-export" {
		t.Fatalf("explicit selected default model must render: %+v", defaultDocument["model"])
	}
}

func TestModelExportCredentialNeverReadsStoredEndpointKeys(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-secret-boundary")

	source := exportFetchSource(t, harness, "pi")
	digest := source["source_digest"].(string)
	targetRow := asMap(t, asMap(t, source["models"].([]any)[0])["targets"].([]any)[0])
	if _, exists := targetRow["has_api_key"]; exists {
		t.Fatalf("source must not expose stored-key presence: %+v", targetRow)
	}
	if _, exists := targetRow["endpoint_base_url"]; exists {
		t.Fatalf("source must not expose upstream endpoint URLs: %+v", targetRow)
	}

	emptyManual := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"credential":             map[string]any{"include": true, "api_key": "   "},
	}, nil, http.StatusOK)
	emptyContent := emptyManual["content"].(string)
	if !strings.Contains(emptyContent, `"apiKey": ""`) {
		t.Fatalf("include=true must preserve the explicitly confirmed trimmed empty string: %s", emptyContent)
	}
	if strings.Contains(emptyContent, "sk-export-live-key") || strings.Contains(emptyContent, "https://export.example") {
		t.Fatalf("explicit empty key must never fall back to stored endpoint data: %s", emptyContent)
	}

	legacy := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"include_api_keys":       true,
	}, nil)
	if legacy.StatusCode != http.StatusBadRequest {
		body := readResponseBody(t, legacy)
		t.Fatalf("legacy stored-key mode status = %d, want 400: %s", legacy.StatusCode, body)
	}
	_ = legacy.Body.Close()
}

func TestModelExportSourceSurvivesCatalogOutage(t *testing.T) {
	var catalogAvailable atomic.Bool
	catalogAvailable.Store(true)
	harness := newExportContractHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if catalogAvailable.Load() {
			exportServingCatalog(w, r)
			return
		}
		exportFailingCatalog(w, r)
	})
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export")

	bindRevision := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/match-preview", modelConfigID), map[string]any{}, nil, http.StatusOK)["catalog_revision"].(string)
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/bind", modelConfigID), map[string]any{"expected_catalog_revision": bindRevision}, nil, http.StatusOK)
	servingSource := exportFetchSource(t, harness, "pi")
	if asMap(t, asMap(t, servingSource["models"].([]any)[0])["enrichment"])["available"] != true {
		t.Fatalf("serving catalog must establish an enriched source before outage")
	}

	catalogAvailable.Store(false)

	source := exportFetchSource(t, harness, "pi")
	row := asMap(t, source["models"].([]any)[0])
	enrichment := row["enrichment"].(map[string]any)
	if enrichment["available"] != false {
		t.Fatalf("failed fetch must keep enrichment unavailable: %+v", enrichment)
	}
	if row["selectable"] != true || row["default_selected"] != true {
		t.Fatalf("core export must not depend on models.dev availability")
	}
	if row["enrichment_candidate"] != nil {
		t.Fatalf("unavailable enrichment must not fabricate candidates")
	}
	warnings, _ := row["warnings"].([]any)
	foundEnrichmentWarning := false
	for _, warning := range warnings {
		foundEnrichmentWarning = foundEnrichmentWarning || warning == "enrichment_unavailable"
	}
	if !foundEnrichmentWarning {
		t.Fatalf("bound catalog outage warnings = %v, want enrichment_unavailable", warnings)
	}
	rendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": source["source_digest"],
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
	}, nil, http.StatusOK)
	if !strings.Contains(rendered["content"].(string), `"id": "gpt-export"`) {
		t.Fatalf("stored truth still renders without enrichment: %s", rendered["content"])
	}
}

func TestModelExportEnabledModelWithoutRouteIsNotSelectable(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog)
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Export No Route Strategy")
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models", map[string]any{
		"api_family":              "openai",
		"model_id":                "gpt-no-route",
		"loadbalance_strategy_id": strategyID,
		"openai_accepted_format":  "dual_native",
		"is_enabled":              true,
		"initial_terminal_target": map[string]any{
			"endpoint_create": map[string]any{
				"name": "Export No Route Endpoint", "base_url": "https://upstream.example/v1", "api_key": "stored-upstream-key",
			},
			"name": "Export No Route Target", "is_active": true, "openai_text_capability": "dual_native",
		},
	}, nil, http.StatusCreated)["model"].(map[string]any)
	modelConfigID := jsonInt(t, payload["id"])
	connectionID := jsonInt(t, asMap(t, asMap(t, payload["access_targets"].([]any)[0])["connection"])["id"])
	requestJSONStatus[map[string]any](t, harness, http.MethodPatch,
		fmt.Sprintf("/api/models/%d/connections/%d", modelConfigID, connectionID), map[string]any{"is_active": false}, nil, http.StatusOK)

	source := exportFetchSource(t, harness, "pi")
	row := asMap(t, source["models"].([]any)[0])
	if row["selectable"] == true || row["default_selected"] == true {
		t.Fatalf("enabled model without a reachable Terminal Target must not be selected: %+v", row)
	}
	if row["unselectable_reason"] != "no_reachable_terminal_target" {
		t.Fatalf("unselectable reason = %+v", row["unselectable_reason"])
	}
}

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
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/jackc/pgx/v5/pgxpool"
)

// exportCompleteCard prices every one of the five components with reasoning
// equal to output, the only shape the Pi client file can express losslessly.
const exportCompleteCard = `{
	"input_price": "3",
	"output_price": "15",
	"cached_input_price": "0.3",
	"cache_creation_price": "3.75",
	"reasoning_price": "15"
}`

// exportFixtureCatalog is the unrelated models.dev catalog fixture: the
// models service requires a working models.dev client to construct, even
// though these Pi-only export tests never exercise its bind/enrichment flow.
const exportFixtureCatalog = `{
  "openai": {
    "id": "openai",
    "name": "OpenAI",
    "models": {
      "gpt-export": {
        "id": "gpt-export",
        "name": "GPT Export (models.dev)",
        "family": "gpt-export",
        "reasoning": true,
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

// piFixtureCatalog carries one exact-id-matching pi.dev provider/model whose
// safe leaves back the bound fixture model, plus a decoy model_id under a
// different provider that must never leak in as a candidate.
const piFixtureCatalog = `{
  "openai": {
    "gpt-export": {
      "id": "gpt-export",
      "name": "GPT Export",
      "api": "openai-responses",
      "provider": "openai",
      "baseUrl": "https://api.openai.example/v1",
      "reasoning": true,
      "input": ["text", "image"],
      "contextWindow": 400000,
      "maxTokens": 32768,
      "thinkingLevelMap": {"low": "low", "medium": "medium", "high": "high"},
      "compat": {"supportsTemperature": true},
      "cost": {"input": 2.5, "output": 10}
    },
    "gpt-decoy": {
      "id": "gpt-decoy",
      "api": "openai-responses",
      "provider": "openai"
    },
    "gpt-multi": {
      "id": "gpt-multi",
      "name": "GPT Multi",
      "api": "openai-responses",
      "provider": "openai",
      "reasoning": true,
      "contextWindow": 300000
    },
    "gpt-export-lite": {
      "id": "gpt-export-lite",
      "name": "GPT Export Lite",
      "api": "openai-responses",
      "provider": "openai",
      "baseUrl": "https://api.openai.example/v1",
      "reasoning": false,
      "input": ["text"],
      "contextWindow": 131072,
      "maxTokens": 16384,
      "cost": {"input": 1, "output": 4},
      "headers": {"x-tracking": "drop-me"}
    },
    "gpt-export-chat": {
      "id": "gpt-export-chat",
      "name": "GPT Export Chat",
      "api": "openai-completions",
      "provider": "openai"
    },
    "GPT-EXPORT-Case": {
      "id": "GPT-EXPORT-Case",
      "api": "openai-responses",
      "provider": "openai"
    }
  },
  "openrouter": {
    "gpt-multi": {
      "id": "gpt-multi",
      "name": "GPT Multi",
      "api": "openai-responses",
      "provider": "openrouter",
      "reasoning": true,
      "contextWindow": 300000
    }
  }
}`

func piServingCatalogHandler(w http.ResponseWriter, _ *http.Request) {
	body := []byte(piFixtureCatalog)
	sum := sha256.Sum256(body)
	w.Header().Set("ETag", `"pi-contract-1"`)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Pi-Model-Catalog-Revision", "sha256-"+hex.EncodeToString(sum[:]))
	w.Header().Set("X-Pi-Model-Catalog-Minimum-Version", "0.1.0")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func piFailingCatalogHandler(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "pi catalog unavailable", http.StatusBadGateway)
}

func newExportContractHarness(t *testing.T, catalogHandler http.HandlerFunc, piCatalogHandler http.HandlerFunc) *contractHarness {
	t.Helper()
	catalogServer := httptest.NewTLSServer(catalogHandler)
	t.Cleanup(catalogServer.Close)
	catalogClient, clientErr := modelsdev.NewClient(modelsdev.ClientOptions{BaseURL: catalogServer.URL, HTTPClient: catalogServer.Client()})
	if clientErr != nil {
		t.Fatalf("build catalog client: %v", clientErr)
	}
	var piCatalogClient *pidev.Client
	if piCatalogHandler != nil {
		piCatalogServer := httptest.NewTLSServer(piCatalogHandler)
		t.Cleanup(piCatalogServer.Close)
		var piClientErr error
		piCatalogClient, piClientErr = pidev.NewClient(pidev.ClientOptions{BaseURL: piCatalogServer.URL, HTTPClient: piCatalogServer.Client()})
		if piClientErr != nil {
			t.Fatalf("build pi catalog client: %v", piClientErr)
		}
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
			modelsService, modelsErr := managementmodels.NewService(settings, managementmodels.Options{Pool: pool, Catalog: catalogClient, PiCatalog: piCatalogClient})
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

// exportSeedModel composite-creates an enabled model together with its first
// active Terminal Target on a keyed endpoint bound to a complete standard
// price template. acceptedFormat controls the final Pi API mapping.
func exportSeedModel(t *testing.T, harness *contractHarness, modelID string, apiFamily string, acceptedFormat string) (modelConfigID int, connectionID int) {
	t.Helper()
	profileID := modelLoadDefaultProfileID(t, harness)
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "Export Strategy "+modelID+"-"+apiFamily+"-"+acceptedFormat)

	template := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/pricing-templates", map[string]any{
		"name":          "Export Standard " + modelID + "-" + apiFamily + "-" + acceptedFormat,
		"template_kind": "standard",
		"card":          jsonDecodeRaw(t, exportCompleteCard),
	}, nil, http.StatusCreated)
	templateID := jsonInt(t, template["id"])

	createBody := map[string]any{
		"api_family":              apiFamily,
		"model_id":                modelID,
		"loadbalance_strategy_id": strategyID,
		"is_enabled":              true,
		"initial_terminal_target": map[string]any{
			"endpoint_create":     map[string]any{"name": "Export Endpoint " + modelID, "base_url": "https://export.example/v1", "api_key": "sk-export-live-key"},
			"name":                "Export Target",
			"is_active":           true,
			"pricing_template_id": templateID,
		},
	}
	if apiFamily == "openai" {
		createBody["openai_accepted_format"] = acceptedFormat
		initial := createBody["initial_terminal_target"].(map[string]any)
		initial["openai_text_capability"] = acceptedFormat
	}
	payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models", createBody, nil, http.StatusCreated)["model"].(map[string]any)
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

func exportFetchSource(t *testing.T, harness *contractHarness) map[string]any {
	t.Helper()
	return requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/models/exports/pi/source", nil, nil, http.StatusOK)
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

// exportSourceRow locates one model_config_id's row in a /export/source response.
func exportSourceRow(t *testing.T, source map[string]any, modelConfigID int) map[string]any {
	t.Helper()
	for _, raw := range source["models"].([]any) {
		row := asMap(t, raw)
		if int(row["model_config_id"].(float64)) == modelConfigID {
			return row
		}
	}
	t.Fatalf("model %d not present in source response: %+v", modelConfigID, source["models"])
	return nil
}

// exportBindPi binds a model to its single exact pi.dev candidate.
func exportBindPi(t *testing.T, harness *contractHarness, modelConfigID int) map[string]any {
	t.Helper()
	source := exportFetchSource(t, harness)
	row := exportSourceRow(t, source, modelConfigID)
	if row["candidate_status"] != "single" {
		t.Fatalf("fixture must offer exactly one pi candidate before auto-bind: %+v", row)
	}
	catalogRevision := source["catalog"].(map[string]any)["revision"].(string)
	bound, headers := exportRequestWithHeaders(t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"expected_catalog_revision": catalogRevision,
		"expected_prism_model_id":   row["model_id"],
		"expected_pi_api":           row["pi_api"],
	}, http.StatusOK)
	if got := headers.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Pi binding cache policy = %q, want private, no-store", got)
	}
	if bound["bound"] != true {
		t.Fatalf("bind must report bound=true: %+v", bound)
	}
	return bound
}

func exportPiBind(t *testing.T, harness *contractHarness, modelConfigID int, body map[string]any, status int) map[string]any {
	t.Helper()
	response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), body, nil)
	assertStatus(t, response, status)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

func piRefreshCommitBody(preview map[string]any) map[string]any {
	return map[string]any{
		"expected_provider_id":        preview["provider_id"],
		"expected_catalog_model_id":   preview["catalog_model_id"],
		"expected_api":                preview["api"],
		"expected_binding_updated_at": preview["binding_updated_at"],
		"expected_catalog_revision":   preview["catalog_revision"],
	}
}

func piSelectionAssertion(bound map[string]any) map[string]any {
	return map[string]any{
		"provider_id": bound["provider_id"],
		"model_id":    bound["catalog_model_id"],
		"api":         bound["api"],
	}
}

func TestModelExportPiRefreshPreviewRejectsInvalidBodies(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	path := "/api/models/1/pi/refresh/preview"
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "unknown field", body: `{"unexpected":true}`, status: http.StatusBadRequest},
		{name: "trailing document", body: `{} {}`, status: http.StatusBadRequest},
		{name: "body limit", body: `{"unexpected":"` + strings.Repeat("x", 1<<20) + `"}`, status: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := harness.requestJSONRaw(t, harness.client, http.MethodPost, path, test.body, nil)
			assertStatus(t, response, test.status)
		})
	}
}

func TestModelExportSourceAndRenderContracts(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")
	digestBeforeModelsDevBind := exportFetchSource(t, harness)["source_digest"].(string)

	// A retained models.dev binding remains usable by its own model-detail
	// surface but is not a Pi-export metadata source.
	modelsDevPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/match-preview", modelConfigID), map[string]any{}, nil, http.StatusOK)
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/catalog/bind", modelConfigID), catalogBindBody("gpt-export", map[string]any{
		"expected_catalog_revision": modelsDevPreview["catalog_revision"],
	}), nil, http.StatusOK)

	source, sourceHeaders := exportRequestWithHeaders(t, harness, http.MethodGet, "/api/models/exports/pi/source", nil, http.StatusOK)
	if source["source_digest"] != digestBeforeModelsDevBind {
		t.Fatalf("models.dev binding must not change Pi export source_digest")
	}
	if got := sourceHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("source cache policy = %q, want private, no-store", got)
	}
	if source["target_version"] != "0.84.3" {
		t.Fatalf("source target_version drifted: %+v", source["target_version"])
	}
	row := exportSourceRow(t, source, modelConfigID)
	mergedBeforePi := asMap(t, row["merged_metadata"])
	if _, leaked := mergedBeforePi["reasoning"]; leaked {
		t.Fatalf("models.dev binding metadata must not enrich Pi export: %+v", mergedBeforePi)
	}
	if row["selectable"] != true {
		t.Fatalf("selection truth mismatch: %+v", row)
	}
	if row["candidate_status"] != "single" {
		t.Fatalf("exact single pi.dev candidate expected: %+v", row)
	}
	if row["pi_binding_status"] != "unbound" || row["pi_selected"] != nil {
		t.Fatalf("model must start unbound: %+v", row)
	}
	candidates := row["pi_candidates"].([]any)
	if len(candidates) != 1 {
		t.Fatalf("exactly one pi candidate expected: %+v", candidates)
	}
	candidate := asMap(t, candidates[0])
	if candidate["provider_id"] != "openai" || candidate["model_id"] != "gpt-export" || candidate["api"] != "openai-responses" {
		t.Fatalf("candidate coordinate drifted: %+v", candidate)
	}
	completeness := row["completeness"].(map[string]any)
	fields := completeness["metadata_fields"].(map[string]any)
	// "name" is Prism's own truth (display_name, defaulting to model_id) and
	// stays known independent of any pi.dev binding; the pi.dev-only-derived
	// fields must not be known until a binding actually exists.
	if fields["name"] != true {
		t.Fatalf("Prism's own display name must always project as known: %+v", fields)
	}
	for _, key := range []string{"reasoning", "contextWindow", "maxTokens"} {
		if fields[key] != false {
			t.Fatalf("unbound model must not project pi candidate fields as known yet: %+v", fields)
		}
	}
	priceRisk := row["price_risk"].(map[string]any)
	if priceRisk["exportable"] != true {
		t.Fatalf("complete five-component USD PER_1M price must export: %+v", priceRisk)
	}

	// Bind, then re-fetch source: the binding becomes the render authority and
	// its safe fields now feed the Pi completeness projection.
	bound := exportBindPi(t, harness, modelConfigID)
	if bound["bind_source"] != "single_candidate" {
		t.Fatalf("auto-applied single candidate must record bind_source: %+v", bound)
	}

	source = exportFetchSource(t, harness)
	digest := source["source_digest"].(string)
	if len(digest) != 64 {
		t.Fatalf("source_digest must be sha256 hex: %q", digest)
	}
	ignoredProfileSource := requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/models/exports/pi/source", nil, map[string]string{"X-Profile-Id": "999999"}, http.StatusOK)
	if ignoredProfileSource["source_digest"] != digest {
		t.Fatalf("Pi export must stay pinned to Default profile when a legacy profile header is supplied")
	}
	row = exportSourceRow(t, source, modelConfigID)
	if row["pi_binding_status"] != "bound" {
		t.Fatalf("bound model must report bound status: %+v", row)
	}
	if row["pi_binding_renderable"] != true {
		t.Fatalf("identity/API-compatible binding must be renderable: %+v", row)
	}
	selected := asMap(t, row["pi_selected"])
	if selected["provider_id"] != "openai" || selected["model_id"] != "gpt-export" || selected["api"] != "openai-responses" {
		t.Fatalf("pi_selected must carry the bound coordinate: %+v", selected)
	}
	bindingSource := asMap(t, row["pi_binding_source"])
	bindingEffective := asMap(t, row["pi_binding_effective"])
	if bindingSource["context_window"].(float64) != 400000 || bindingEffective["context_window"].(float64) != 400000 {
		t.Fatalf("source must publish frozen source/effective binding metadata: source=%+v effective=%+v", bindingSource, bindingEffective)
	}
	dropped := row["pi_binding_dropped_fields"].([]any)
	if len(dropped) != 1 || dropped[0] != "compat.supportsTemperature" {
		t.Fatalf("source must publish stable sanitized-field evidence: %+v", dropped)
	}
	completeness = row["completeness"].(map[string]any)
	fields = completeness["metadata_fields"].(map[string]any)
	for _, key := range []string{"name", "reasoning", "contextWindow", "maxTokens", "input"} {
		if fields[key] != true {
			t.Fatalf("bound pi field %s must project as known: %+v", key, fields)
		}
	}

	renderRequest := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"provider_id":            "custom-prism",
		"credential":             map[string]any{"include": true, "api_key": " proxy-key "},
		"selections":             map[string]any{fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound)},
	}
	rendered, renderHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/pi/render", renderRequest, http.StatusOK)
	if got := renderHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("render cache policy = %q, want private, no-store", got)
	}
	if _, exists := rendered["catalog"]; exists {
		t.Fatalf("render must not fabricate live catalog evidence it never checked: %+v", rendered["catalog"])
	}
	rawRenderRequest, err := json.Marshal(renderRequest)
	if err != nil {
		t.Fatalf("marshal render request for trailing-document boundary: %v", err)
	}
	trailing := harness.requestJSONRaw(t, harness.client, http.MethodPost, "/api/models/exports/pi/render", string(rawRenderRequest)+` {}`, nil)
	if trailing.StatusCode != http.StatusBadRequest {
		t.Fatalf("render body with a trailing JSON document status = %d, want 400", trailing.StatusCode)
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
	modelResults := rendered["model_results"].([]any)
	if len(modelResults) != 1 || asMap(t, modelResults[0])["model_config_id"] != float64(modelConfigID) {
		t.Fatalf("render must publish one ordered result per selected model: %+v", modelResults)
	}
	if strings.Contains(content, "https://export.example") || strings.Contains(content, "sk-export-live-key") || strings.Contains(content, "api.openai.example") {
		t.Fatalf("render leaked an upstream endpoint URL, stored endpoint key, or pi.dev baseUrl: %s", content)
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
	if providerModels["reasoning"] != true || providerModels["contextWindow"].(float64) != 400000 {
		t.Fatalf("bound safe pi.dev leaves must render: %+v", providerModels)
	}
	if _, hasHeaders := providerModels["headers"]; hasHeaders {
		t.Fatalf("pi.dev headers must never enter the rendered document: %+v", providerModels)
	}

	// Deterministic replay: identical body renders byte-identical output.
	reRendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", renderRequest, map[string]string{"X-Profile-Id": "999999"}, http.StatusOK)
	if reRendered["content"] != content || reRendered["content_sha256"] != rendered["content_sha256"] {
		t.Fatalf("render replay must be deterministic")
	}

	// Digest drift fails closed with the stable code before any rendering.
	staleBody := map[string]any{
		"expected_source_digest": strings.Repeat("0", 64),
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections":             map[string]any{fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound)},
	}
	stale, staleHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/exports/pi/render", staleBody, http.StatusConflict)
	if stale["detail"] != "export_source_stale" {
		t.Fatalf("drift must return export_source_stale: %+v", stale)
	}
	if stale["code"] != "export_source_stale" {
		t.Fatalf("drift must expose the stable export_source_stale code: %+v", stale)
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
	if !strings.Contains(fmt.Sprint(unknownResponse["detail"]), "not exportable") {
		t.Fatalf("unknown id must fail closed: %+v", unknownResponse)
	}

	// A render selections assertion that names a different coordinate than the
	// one actually bound must fail 422, never silently substitute.
	wrongAssertion := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections": map[string]any{fmt.Sprintf("%d", modelConfigID): map[string]any{
			"provider_id": "openai", "model_id": "gpt-decoy", "api": "openai-responses",
		}},
	}
	wrongResponse := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", wrongAssertion, nil, http.StatusUnprocessableEntity)
	if wrongResponse["code"] != "candidate_invalid" || wrongResponse["model_config_id"] != float64(modelConfigID) {
		t.Fatalf("mismatched selection assertion must expose candidate_invalid for the model: %+v", wrongResponse)
	}

	// A render request that omits the selections assertion entirely for a
	// bound model must also fail closed, never silently trust the binding.
	omittedAssertion := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
	}
	omittedResponse := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", omittedAssertion, nil, http.StatusUnprocessableEntity)
	if omittedResponse["code"] != "candidate_unselected" || omittedResponse["model_config_id"] != float64(modelConfigID) {
		t.Fatalf("omitted selection assertion must expose candidate_unselected for the model: %+v", omittedResponse)
	}

	// Assertions for models outside model_config_ids are not candidates for
	// this render and must be rejected instead of ignored.
	extraAssertion := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections": map[string]any{
			fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound),
			"999999":                         piSelectionAssertion(bound),
		},
	}
	extraResponse := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", extraAssertion, nil, http.StatusUnprocessableEntity)
	if extraResponse["code"] != "candidate_invalid" || extraResponse["model_config_id"] != float64(999999) {
		t.Fatalf("extra selection assertion must expose candidate_invalid for the extra model: %+v", extraResponse)
	}

	// Retained raw binding evidence remains manageable after a Prism identity
	// edit, but the explicit compatibility flag and render both fail closed.
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET model_id = 'gpt-export-renamed' WHERE id = $1`, modelConfigID); err != nil {
		t.Fatalf("mutate model identity for binding-health check: %v", err)
	}
	driftedSource := exportFetchSource(t, harness)
	driftedRow := exportSourceRow(t, driftedSource, modelConfigID)
	if driftedRow["pi_binding_renderable"] != false || driftedRow["pi_binding_status"] != "bound_drifted" {
		t.Fatalf("identity-drifted binding must be visible but non-renderable: %+v", driftedRow)
	}
	if asMap(t, driftedRow["pi_selected"])["model_id"] != "gpt-export" || driftedRow["pi_binding_source"] == nil {
		t.Fatalf("identity drift must preserve raw coordinate and source evidence: %+v", driftedRow)
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": driftedSource["source_digest"],
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections":             map[string]any{fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound)},
	}, nil, http.StatusUnprocessableEntity)

	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET model_id = 'gpt-export', openai_accepted_format = 'chat_completions_only' WHERE id = $1`, modelConfigID); err != nil {
		t.Fatalf("mutate final Pi API for binding-health check: %v", err)
	}
	apiDriftSource := exportFetchSource(t, harness)
	apiDriftRow := exportSourceRow(t, apiDriftSource, modelConfigID)
	if apiDriftRow["pi_binding_renderable"] != false || asMap(t, apiDriftRow["pi_selected"])["api"] != "openai-responses" {
		t.Fatalf("API-drifted binding must retain raw evidence but become non-renderable: %+v", apiDriftRow)
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusConflict)
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{"name": "must reject"}, nil, http.StatusConflict)
}

func TestModelExportPiRefreshAndOverride(t *testing.T) {
	var catalogGeneration atomic.Int32
	catalogGeneration.Store(1)
	harness := newExportContractHarness(t, exportServingCatalog, func(w http.ResponseWriter, r *http.Request) {
		if catalogGeneration.Load() == 1 {
			piServingCatalogHandler(w, r)
			return
		}
		if catalogGeneration.Load() > 2 {
			piFailingCatalogHandler(w, r)
			return
		}
		// Second generation changes the bound candidate's safe fields so a
		// refresh has something to preview and commit.
		updatedCatalog := strings.Replace(piFixtureCatalog, `"contextWindow": 400000`, `"contextWindow": 800000`, 1)
		updatedCatalog = strings.Replace(updatedCatalog, `"compat": {"supportsTemperature": true},`, `"compat": {"supportsTemperature": true}, "headers": {"x-upstream-only": "ignored"},`, 1)
		body := []byte(updatedCatalog)
		sum := sha256.Sum256(body)
		w.Header().Set("ETag", `"pi-contract-2"`)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Pi-Model-Catalog-Revision", "sha256-"+hex.EncodeToString(sum[:]))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")

	bound := exportBindPi(t, harness, modelConfigID)
	firstDigest := exportFetchSource(t, harness)["source_digest"].(string)

	// Override the name; the effective value must win over the bound source.
	overridden := requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{
		"name": "Operator Renamed",
	}, nil, http.StatusOK)
	if overridden["effective"].(map[string]any)["name"] != "Operator Renamed" {
		t.Fatalf("override must win in the effective projection: %+v", overridden["effective"])
	}
	if overridden["source"].(map[string]any)["name"] != "GPT Export" {
		t.Fatalf("override must never mutate the frozen source snapshot: %+v", overridden["source"])
	}

	// An override that violates the Pi schema fails 422 and writes nothing.
	invalidOverride := requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{
		"reasoning": "yes",
	}, nil, http.StatusUnprocessableEntity)
	if invalidOverride["code"] == "" && invalidOverride["detail"] == nil {
		t.Fatalf("schema-invalid override must fail closed: %+v", invalidOverride)
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{
		"compat": map[string]any{"allowedFallbackModels": []string{"other-provider/model"}},
	}, nil, http.StatusUnprocessableEntity)
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{
		"max_tokens": 0,
	}, nil, http.StatusUnprocessableEntity)

	overrideSource := exportFetchSource(t, harness)
	overrideDigest := overrideSource["source_digest"].(string)
	if overrideDigest == firstDigest {
		t.Fatalf("an override must move the source digest")
	}
	overrideRow := exportSourceRow(t, overrideSource, modelConfigID)
	if asMap(t, overrideRow["pi_binding_override"])["name"] != "Operator Renamed" || asMap(t, overrideRow["pi_binding_effective"])["name"] != "Operator Renamed" {
		t.Fatalf("source must publish distinct override and effective projections: %+v", overrideRow)
	}

	// Clearing the override restores the source name.
	cleared := requestJSONStatus[map[string]any](t, harness, http.MethodDelete, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), nil, nil, http.StatusOK)
	if cleared["effective"].(map[string]any)["name"] != "GPT Export" {
		t.Fatalf("clearing overrides must restore the source value: %+v", cleared["effective"])
	}

	// Refresh preview against the unchanged catalog reports no drift.
	quietPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusOK)
	if quietPreview["changed"] != false {
		t.Fatalf("unchanged catalog must preview no changes: %+v", quietPreview)
	}
	frozenDigest := exportFetchSource(t, harness)["source_digest"].(string)

	// Advance the fixture catalog, then preview and commit the refresh.
	catalogGeneration.Store(2)
	preview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusOK)
	if preview["changed"] != true {
		t.Fatalf("advanced catalog must preview a change: %+v", preview)
	}
	hasDroppedFieldsDiff := false
	for _, rawChange := range preview["changes"].([]any) {
		if asMap(t, rawChange)["field"] == "dropped_fields" {
			hasDroppedFieldsDiff = true
		}
	}
	if !hasDroppedFieldsDiff {
		t.Fatalf("refresh preview must surface changed dropped-field evidence: %+v", preview["changes"])
	}
	nextRevision := preview["catalog_revision"].(string)
	if nextRevision == bound["catalog_revision"] {
		t.Fatalf("advanced catalog must carry a new revision")
	}

	// Repeating bind for the existing coordinate is idempotent: it must not
	// smuggle the new live source fields/revision around explicit refresh.
	sameCoordinate := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"expected_catalog_revision": nextRevision,
		"expected_prism_model_id":   "gpt-export",
		"expected_pi_api":           "openai-responses",
	}, nil, http.StatusOK)
	if sameCoordinate["catalog_revision"] != bound["catalog_revision"] || sameCoordinate["source"].(map[string]any)["context_window"].(float64) != 400000 {
		t.Fatalf("same-coordinate bind must preserve frozen source and revision: %+v", sameCoordinate)
	}
	driftSource := exportFetchSource(t, harness)
	if driftSource["source_digest"] != frozenDigest {
		t.Fatalf("same-coordinate bind/live drift must not change frozen render digest")
	}
	driftRow := exportSourceRow(t, driftSource, modelConfigID)
	if driftRow["pi_binding_status"] != "bound_drifted" || driftRow["pi_binding_renderable"] != true {
		t.Fatalf("live metadata drift must invite refresh without blocking frozen render: %+v", driftRow)
	}
	catalogGeneration.Store(3)
	staleLKGSource := exportFetchSource(t, harness)
	staleLKGRow := exportSourceRow(t, staleLKGSource, modelConfigID)
	if staleLKGSource["catalog"].(map[string]any)["status"] != "stale" || staleLKGRow["pi_binding_status"] != "bound" {
		t.Fatalf("stale LKG may show stale candidates but must not assert binding drift: source=%+v row=%+v", staleLKGSource["catalog"], staleLKGRow)
	}
	catalogGeneration.Store(2)

	// A commit against a stale (superseded) revision fails closed and writes
	// nothing: the 409 status itself is the assertion.
	staleCatalogCommit := piRefreshCommitBody(quietPreview)
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/commit", modelConfigID), staleCatalogCommit, nil, http.StatusConflict)

	// A metadata write after preview invalidates the binding CAS token even
	// when the remote catalog revision and coordinate are unchanged.
	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{
		"name": "Survives Refresh",
	}, nil, http.StatusOK)
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/commit", modelConfigID), piRefreshCommitBody(preview), nil, http.StatusConflict)

	currentPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusOK)
	committed := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/commit", modelConfigID), piRefreshCommitBody(currentPreview), nil, http.StatusOK)
	if committed["catalog_revision"] != nextRevision {
		t.Fatalf("commit must persist the previewed revision: %+v", committed)
	}
	if committed["source"].(map[string]any)["context_window"].(float64) != 800000 {
		t.Fatalf("commit must replace source fields from the refreshed catalog: %+v", committed["source"])
	}
	if committed["override"].(map[string]any)["name"] != "Survives Refresh" || committed["effective"].(map[string]any)["name"] != "Survives Refresh" {
		t.Fatalf("refresh must preserve manual overrides: %+v", committed)
	}
	if dropped := committed["dropped_fields"].([]any); len(dropped) != 2 || dropped[0] != "compat.supportsTemperature" || dropped[1] != "headers" {
		t.Fatalf("refresh must preserve refreshed dropped-field evidence: %+v", committed)
	}
	refreshedSource := exportFetchSource(t, harness)
	if refreshedSource["source_digest"] == frozenDigest || exportSourceRow(t, refreshedSource, modelConfigID)["pi_binding_status"] != "bound" {
		t.Fatalf("committed refresh must move the digest and clear live drift: %+v", refreshedSource)
	}

	// Unbind removes the row entirely.
	unbound := requestJSONStatus[map[string]any](t, harness, http.MethodDelete, fmt.Sprintf("/api/models/%d/pi", modelConfigID), nil, nil, http.StatusOK)
	if unbound["bound"] != false {
		t.Fatalf("unbind must report bound=false: %+v", unbound)
	}
	sourceAfterUnbind := exportFetchSource(t, harness)
	rowAfterUnbind := exportSourceRow(t, sourceAfterUnbind, modelConfigID)
	if rowAfterUnbind["pi_binding_status"] != "unbound" || rowAfterUnbind["pi_selected"] != nil {
		t.Fatalf("unbound model must report no selection: %+v", rowAfterUnbind)
	}

	// Render after unbinding is blocked: nothing is bound to trust.
	renderAfterUnbind := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": sourceAfterUnbind["source_digest"],
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
	}, nil, http.StatusUnprocessableEntity)
	if renderAfterUnbind == nil {
		t.Fatalf("render must block an unbound model")
	}

	// Refreshing or overriding an unbound model fails closed rather than
	// silently creating a binding.
	refreshUnbound := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusConflict)
	if refreshUnbound == nil {
		t.Fatalf("refresh preview on an unbound model must fail closed")
	}
}

// TestModelExportPiMultipleCandidatesRequireExplicitBindAndRebindMovesDigest
// covers C4/C5 end to end against a genuinely ambiguous exact-id match: two
// providers both publish "gpt-multi" under openai-responses. Binding without
// coordinates must reject with the candidate list; binding to one, then
// rebinding to the other, must change the source digest each time.
func TestModelExportPiMultipleCandidatesRequireExplicitBindAndRebindMovesDigest(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-multi", "openai", "dual_native")

	source := exportFetchSource(t, harness)
	row := exportSourceRow(t, source, modelConfigID)
	if row["candidate_status"] != "multiple" {
		t.Fatalf("fixture must offer two candidates for gpt-multi: %+v", row)
	}
	candidates := row["pi_candidates"].([]any)
	if len(candidates) != 2 {
		t.Fatalf("expected exactly two candidates: %+v", candidates)
	}
	templateProjection := func(candidate map[string]any) string {
		projection := map[string]any{}
		for _, field := range []string{"name", "reasoning", "input", "context_window", "max_tokens", "thinking_level_map", "compat", "dropped_fields"} {
			if value, present := candidate[field]; present {
				projection[field] = value
			}
		}
		encoded, err := json.Marshal(projection)
		if err != nil {
			t.Fatalf("encode candidate template projection: %v", err)
		}
		return string(encoded)
	}
	if templateProjection(asMap(t, candidates[0])) != templateProjection(asMap(t, candidates[1])) {
		t.Fatalf("fixture candidates must have identical sanitized templates: %+v", candidates)
	}
	catalogRevision := source["catalog"].(map[string]any)["revision"].(string)

	// Binding without explicit coordinates must reject with the candidate
	// evidence, never auto-merge or pick by lexical/provider order.
	ambiguous := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"expected_catalog_revision": catalogRevision,
		"expected_prism_model_id":   row["model_id"],
		"expected_pi_api":           row["pi_api"],
	}, nil, http.StatusConflict)
	if candList, ok := ambiguous["candidates"].([]any); !ok || len(candList) != 2 {
		t.Fatalf("ambiguous bind must return the full candidate list as evidence: %+v", ambiguous)
	}

	// Explicit bind to the openai-hosted candidate.
	firstBind := exportPiBind(t, harness, modelConfigID, map[string]any{
		"provider_id":               "openai",
		"catalog_model_id":          "gpt-multi",
		"expected_catalog_revision": catalogRevision,
		"expected_prism_model_id":   row["model_id"],
		"expected_pi_api":           row["pi_api"],
	}, http.StatusOK)
	if firstBind["bind_source"] != "manual" || firstBind["provider_id"] != "openai" {
		t.Fatalf("explicit disambiguation among multiple candidates must record manual bind_source: %+v", firstBind)
	}
	if firstBind["prism_model_id_at_bind"] != row["model_id"] {
		t.Fatalf("explicit bind must freeze the confirmed Prism identity alongside the coordinate: %+v", firstBind)
	}
	firstDigest := exportFetchSource(t, harness)["source_digest"].(string)
	firstPreview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusOK)

	// Rebinding to the other exact-id candidate must clear inherited
	// assumptions (a fresh coordinate) and move the digest again.
	secondBind := exportPiBind(t, harness, modelConfigID, map[string]any{
		"provider_id":               "openrouter",
		"catalog_model_id":          "gpt-multi",
		"expected_catalog_revision": catalogRevision,
		"expected_prism_model_id":   row["model_id"],
		"expected_pi_api":           row["pi_api"],
	}, http.StatusOK)
	if secondBind["provider_id"] != "openrouter" {
		t.Fatalf("rebind must switch the persisted coordinate to the newly chosen candidate: %+v", secondBind)
	}
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/commit", modelConfigID), piRefreshCommitBody(firstPreview), nil, http.StatusConflict)
	secondDigest := exportFetchSource(t, harness)["source_digest"].(string)
	if secondDigest == firstDigest {
		t.Fatalf("rebinding to a different exact-id candidate must move the source digest")
	}

	// A render selections assertion still naming the old (pre-rebind)
	// coordinate must now be rejected as non-current.
	staleAssertion := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": secondDigest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections": map[string]any{
			fmt.Sprintf("%d", modelConfigID): map[string]any{
				"provider_id": "openai", "model_id": "gpt-multi", "api": "openai-responses",
			},
		},
	}, nil, http.StatusUnprocessableEntity)
	if staleAssertion == nil {
		t.Fatalf("a selection assertion naming the pre-rebind coordinate must fail closed")
	}
}

func TestModelExportCredentialNeverReadsStoredEndpointKeys(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")
	bound := exportBindPi(t, harness, modelConfigID)

	source := exportFetchSource(t, harness)
	digest := source["source_digest"].(string)
	targetRow := asMap(t, asMap(t, exportSourceRow(t, source, modelConfigID))["targets"].([]any)[0])
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
		"selections":             map[string]any{fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound)},
	}, nil, http.StatusUnprocessableEntity)
	if emptyManual["code"] != "credential_api_key_required" {
		t.Fatalf("Pi-incompatible empty included key must expose credential_api_key_required: %+v", emptyManual)
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
	harness := newExportContractHarness(t, exportServingCatalog, func(w http.ResponseWriter, r *http.Request) {
		if catalogAvailable.Load() {
			piServingCatalogHandler(w, r)
			return
		}
		piFailingCatalogHandler(w, r)
	})
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")
	catalogAvailable.Store(false)
	_, unavailableHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/search", modelConfigID), map[string]any{"model_id_query": "gpt-export"}, http.StatusServiceUnavailable)
	if got := unavailableHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("unavailable search cache policy = %q, want private, no-store", got)
	}
	catalogAvailable.Store(true)
	bound := exportBindPi(t, harness, modelConfigID)

	catalogAvailable.Store(false)

	// A bound model's render authority is the persisted row, not live catalog
	// reachability: source keeps reporting the binding as healthy evidence
	// (benefit of the doubt) using the last-known-good catalog for candidate
	// discovery, and render - which never touches the network at all - is
	// entirely unaffected by the outage.
	source := exportFetchSource(t, harness)
	if source["catalog"].(map[string]any)["status"] != "stale" {
		t.Fatalf("a failed fetch with a cached catalog must report stale, not fresh: %+v", source["catalog"])
	}
	row := exportSourceRow(t, source, modelConfigID)
	if row["candidate_status"] != "single" {
		t.Fatalf("LKG catalog data must still back candidate discovery during an outage: %+v", row)
	}
	if row["pi_binding_status"] != "bound" {
		t.Fatalf("outage must not flag a healthy binding as drifted: %+v", row)
	}
	selected := asMap(t, row["pi_selected"])
	if selected["provider_id"] != "openai" || selected["model_id"] != "gpt-export" {
		t.Fatalf("bound coordinate must survive a catalog outage: %+v", selected)
	}

	search, searchHeaders := exportPiSearch(t, harness, modelConfigID, map[string]any{"model_id_query": "gpt-export"})
	if search["catalog"].(map[string]any)["status"] != "stale" {
		t.Fatalf("outage search must label LKG evidence stale: %+v", search["catalog"])
	}
	if got := searchHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("stale search cache policy = %q, want private, no-store", got)
	}
	beforeRejectedBind := source["source_digest"].(string)
	rejected := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-export-lite",
		"expected_catalog_revision": search["catalog"].(map[string]any)["revision"],
		"expected_prism_model_id":   "gpt-export", "expected_pi_api": "openai-responses",
	}, nil, http.StatusBadGateway)
	if !strings.Contains(fmt.Sprint(rejected), "pi_catalog_unavailable") {
		t.Fatalf("stale LKG bind must require a fresh fetch: %+v", rejected)
	}
	afterRejectedBind := exportFetchSource(t, harness)
	if afterRejectedBind["source_digest"] != beforeRejectedBind || asMap(t, exportSourceRow(t, afterRejectedBind, modelConfigID)["pi_selected"])["model_id"] != "gpt-export" {
		t.Fatalf("stale LKG bind rejection changed frozen truth: %+v", afterRejectedBind)
	}

	rendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": source["source_digest"],
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections":             map[string]any{fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound)},
	}, nil, http.StatusOK)
	if !strings.Contains(rendered["content"].(string), `"id": "gpt-export"`) {
		t.Fatalf("stored truth still renders during a catalog outage: %s", rendered["content"])
	}
}

func TestModelExportRenderUsesFrozenBindingWithoutCatalogClientOrSnapshot(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, nil)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-offline", "openai", "dual_native")
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_pi_catalog_bindings (
		model_config_id, provider_id, catalog_model_id, api, prism_model_id_at_bind, bind_source, catalog_revision, fetched_at,
		source_name, source_reasoning, source_input, source_context_window, source_max_tokens, source_dropped_fields, updated_at
	) VALUES ($1, 'openai', 'gpt-offline', 'openai-responses', 'gpt-offline', 'manual', 'sha256-offline-fixture', $2,
		'GPT Offline', TRUE, '["text"]'::jsonb, 200000, 16384, '[]'::jsonb, $2)`, modelConfigID, now); err != nil {
		t.Fatalf("seed frozen Pi binding: %v", err)
	}

	source := exportFetchSource(t, harness)
	row := exportSourceRow(t, source, modelConfigID)
	if source["catalog"].(map[string]any)["status"] != "unavailable" || row["candidate_status"] != "catalog_unavailable" || row["pi_binding_renderable"] != true {
		t.Fatalf("frozen binding must remain published without a catalog client: source=%+v row=%+v", source["catalog"], row)
	}
	if candidates, ok := row["pi_candidates"].([]any); !ok || len(candidates) != 0 {
		t.Fatalf("catalog-unavailable candidate evidence must be an empty array: %+v", row["pi_candidates"])
	}
	rendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": source["source_digest"],
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections": map[string]any{fmt.Sprintf("%d", modelConfigID): map[string]any{
			"provider_id": "openai", "model_id": "gpt-offline", "api": "openai-responses",
		}},
	}, nil, http.StatusOK)
	if !strings.Contains(rendered["content"].(string), `"id": "gpt-offline"`) {
		t.Fatalf("frozen binding did not render without catalog state: %s", rendered["content"])
	}
}

func TestModelExportEnabledModelWithoutRouteIsNotSelectable(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
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

	source := exportFetchSource(t, harness)
	row := exportSourceRow(t, source, modelConfigID)
	if row["selectable"] == true {
		t.Fatalf("enabled model without a reachable Terminal Target must not be selectable: %+v", row)
	}
	if row["unselectable_reason"] != "no_reachable_terminal_target" {
		t.Fatalf("unselectable reason = %+v", row["unselectable_reason"])
	}
}

func exportErrorText(payload map[string]any) string {
	parts := make([]string, 0, 2)
	for _, field := range []string{"detail", "title"} {
		if value, ok := payload[field]; ok && value != nil {
			parts = append(parts, fmt.Sprint(value))
		}
	}
	return strings.Join(parts, " ")
}

func exportPiSearch(t *testing.T, harness *contractHarness, modelConfigID int, body map[string]any) (map[string]any, http.Header) {
	t.Helper()
	return exportRequestWithHeaders(t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/search", modelConfigID), body, http.StatusOK)
}

func TestModelExportPiCatalogSearchContract(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")
	baselineDigest := exportFetchSource(t, harness)["source_digest"].(string)

	payload, headers := exportPiSearch(t, harness, modelConfigID, map[string]any{"model_id_query": "GPT-EXPORT"})
	if got := headers.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("search cache policy = %q, want private, no-store", got)
	}
	if payload["api"] != "openai-responses" {
		t.Fatalf("search must be scoped to the model's current final Pi API: %+v", payload["api"])
	}
	if payload["selected"] != false {
		t.Fatalf("a search must never report a selection: %+v", payload)
	}
	if got := int(payload["limit"].(float64)); got != 20 {
		t.Fatalf("default page bound = %d, want 20", got)
	}
	if got := int(payload["offset"].(float64)); got != 0 {
		t.Fatalf("default offset = %d, want 0", got)
	}
	catalogEvidence := asMap(t, payload["catalog"])
	if revision, ok := catalogEvidence["revision"].(string); !ok || !strings.HasPrefix(revision, "sha256-") {
		t.Fatalf("search must publish the trusted body-SHA revision: %+v", catalogEvidence)
	}
	if payload["fetched_at"] == nil || payload["checked_at"] == nil {
		t.Fatalf("search must publish both fetch and revalidation stamps: %+v", payload)
	}
	identity := asMap(t, payload["export_identity"])
	if identity["model_id"] != "gpt-export" || identity["api"] != "openai-responses" {
		t.Fatalf("export identity must stay Prism-authored: %+v", identity)
	}
	if identity["provider_id_source"] != "operator_input" {
		t.Fatalf("exported provider key must be declared operator input: %+v", identity)
	}
	var lite map[string]any
	for _, raw := range payload["results"].([]any) {
		candidate := asMap(t, raw)
		if candidate["model_id"] == "gpt-export-lite" {
			lite = candidate
			break
		}
	}
	if lite == nil {
		t.Fatalf("same-API safe candidate missing from search response: %+v", payload["results"])
	}
	dropped, _ := lite["dropped_fields"].([]any)
	foundHeaders := false
	for _, item := range dropped {
		if item == "headers" {
			foundHeaders = true
		}
	}
	if !foundHeaders {
		t.Fatalf("gpt-export-lite must report its dropped headers path: %+v", lite)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode search payload: %v", err)
	}
	for _, forbidden := range []string{"api.openai.example", "x-tracking", "\"cost\"", "drop-me"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("search leaked unsafe directory field %q: %s", forbidden, encoded)
		}
	}

	for _, body := range []map[string]any{{}, {"model_id_query": "   "}, {"model_id_query": strings.Repeat("g", 201)}, {"model_id_query": "gpt", "offset": -1}} {
		response := harness.requestJSON(t, harness.client, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/search", modelConfigID), body, nil)
		assertStatus(t, response, http.StatusUnprocessableEntity)
		_ = response.Body.Close()
	}
	if after := exportFetchSource(t, harness)["source_digest"].(string); after != baselineDigest {
		t.Fatalf("directory search must never write binding state: digest %s -> %s", baselineDigest, after)
	}

	// Offset paging windows the same ranked hit set: page two must not repeat
	// page one, and an offset at total returns an empty page without error.
	total := int(payload["total"].(float64))
	if total > 1 {
		pageTwo := exportPiSearchOrFail(t, harness, modelConfigID, map[string]any{
			"model_id_query": "GPT-EXPORT", "limit": 1, "offset": 1,
		})
		if int(pageTwo["offset"].(float64)) != 1 || int(pageTwo["total"].(float64)) != total {
			t.Fatalf("offset page must echo its window and the full total: %+v", pageTwo)
		}
		firstID := asMap(t, payload["results"].([]any)[0])["model_id"]
		secondID := asMap(t, pageTwo["results"].([]any)[0])["model_id"]
		if firstID == secondID {
			t.Fatalf("offset=1 must skip the first ranked hit: %v vs %v", firstID, secondID)
		}
	}
	beyond := exportPiSearchOrFail(t, harness, modelConfigID, map[string]any{
		"model_id_query": "GPT-EXPORT", "limit": 5, "offset": total,
	})
	if len(beyond["results"].([]any)) != 0 || int(beyond["returned"].(float64)) != 0 {
		t.Fatalf("offset at total must return an empty page: %+v", beyond)
	}
}

func exportPiSearchOrFail(t *testing.T, harness *contractHarness, modelConfigID int, body map[string]any) map[string]any {
	t.Helper()
	payload, _ := exportPiSearch(t, harness, modelConfigID, body)
	return payload
}

func TestModelExportPiCrossDirectoryBindRenderAndIdentityDrift(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	const prismModelID = "codex/gpt-export"
	modelConfigID, _ := exportSeedModel(t, harness, prismModelID, "openai", "dual_native")

	source := exportFetchSource(t, harness)
	row := exportSourceRow(t, source, modelConfigID)
	if row["candidate_status"] != "not_in_catalog" {
		t.Fatalf("a namespaced Prism id must stay not_in_catalog for the default flow: %+v", row)
	}
	if candidates := row["pi_candidates"].([]any); len(candidates) != 0 {
		t.Fatalf("the default exact flow must not fuzzy-match a namespaced id: %+v", candidates)
	}
	if row["pi_binding_status"] != "unbound" || row["pi_binding_renderable"] != false {
		t.Fatalf("fresh model must start unbound: %+v", row)
	}
	revision := source["catalog"].(map[string]any)["revision"].(string)

	implicit := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"expected_catalog_revision": revision,
		"expected_prism_model_id":   prismModelID,
		"expected_pi_api":           "openai-responses",
	}, nil, http.StatusUnprocessableEntity)
	if !strings.Contains(exportErrorText(implicit), "pi_candidate_not_in_catalog") {
		t.Fatalf("zero exact candidates must reject with pi_candidate_not_in_catalog: %+v", implicit)
	}

	page := exportPiSearchOrFail(t, harness, modelConfigID, map[string]any{"model_id_query": "gpt-export"})
	if len(page["results"].([]any)) == 0 {
		t.Fatalf("directory search must offer same-API coordinates for a not_in_catalog model: %+v", page)
	}

	bound := exportPiBind(t, harness, modelConfigID, map[string]any{
		"provider_id":               "openai",
		"catalog_model_id":          "gpt-export",
		"expected_catalog_revision": revision,
		"expected_prism_model_id":   prismModelID,
		"expected_pi_api":           "openai-responses",
	}, http.StatusOK)
	if bound["bind_source"] != "manual" {
		t.Fatalf("an explicit coordinate bind must record manual bind_source: %+v", bound)
	}
	if bound["catalog_model_id"] != "gpt-export" || bound["prism_model_id_at_bind"] != prismModelID {
		t.Fatalf("directory id and frozen Prism identity must stay distinct: %+v", bound)
	}

	overridden := requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{
		"name":           "Operator Chosen Name",
		"context_window": 123456,
	}, nil, http.StatusOK)
	if asMap(t, overridden["override"])["name"] != "Operator Chosen Name" {
		t.Fatalf("override must persist across a cross-directory bind: %+v", overridden)
	}
	updatedAtBeforeIdentityReconfirm := overridden["updated_at"]

	boundSource := exportFetchSource(t, harness)
	boundRow := exportSourceRow(t, boundSource, modelConfigID)
	if boundRow["candidate_status"] != "not_in_catalog" {
		t.Fatalf("cross-directory binding must not rewrite live candidate evidence: %+v", boundRow)
	}
	if boundRow["pi_binding_status"] != "bound" || boundRow["pi_binding_renderable"] != true {
		t.Fatalf("a cross-directory binding with a matching identity snapshot must render: %+v", boundRow)
	}
	if boundRow["pi_binding_prism_model_id"] != prismModelID {
		t.Fatalf("source must publish the frozen Prism identity: %+v", boundRow)
	}
	selectedCoordinate := asMap(t, boundRow["pi_selected"])
	if selectedCoordinate["model_id"] != "gpt-export" {
		t.Fatalf("pi_selected must keep the directory coordinate: %+v", boundRow["pi_selected"])
	}
	renderDigest := boundSource["source_digest"].(string)

	rendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": renderDigest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"provider_id":            "prism-home",
		"selections": map[string]any{
			fmt.Sprintf("%d", modelConfigID): map[string]any{
				"provider_id": "openai", "model_id": "gpt-export", "api": "openai-responses",
			},
		},
	}, nil, http.StatusOK)
	content := rendered["content"].(string)
	if !strings.Contains(content, `"id": "codex/gpt-export"`) {
		t.Fatalf("exported model id must be the Prism model_id: %s", content)
	}
	if strings.Contains(content, `"id": "gpt-export"`) {
		t.Fatalf("directory model id must never become the exported model id: %s", content)
	}
	if !strings.Contains(content, `"prism-home"`) {
		t.Fatalf("exported provider key must come from operator input: %s", content)
	}
	if !strings.Contains(content, `"api": "openai-responses"`) {
		t.Fatalf("exported api must stay Prism's own mapping: %s", content)
	}
	if !strings.Contains(content, `"contextWindow": 123456`) {
		t.Fatalf("override must reach the rendered file: %s", content)
	}

	for _, forbidden := range []string{"api.openai.example", "x-tracking"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("directory field %q leaked into the export: %s", forbidden, content)
		}
	}

	requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d", modelConfigID), map[string]any{
		"model_id": "renamed/gpt-export",
	}, nil, http.StatusOK)
	driftedSource := exportFetchSource(t, harness)
	driftedRow := exportSourceRow(t, driftedSource, modelConfigID)
	if driftedRow["pi_binding_renderable"] != false {
		t.Fatalf("Prism identity drift must clear renderability: %+v", driftedRow)
	}
	if driftedRow["pi_binding_status"] != "bound_drifted" {
		t.Fatalf("Prism identity drift must surface as bound_drifted: %+v", driftedRow)
	}
	if asMap(t, driftedRow["pi_selected"])["model_id"] != "gpt-export" {
		t.Fatalf("drift must preserve the raw coordinate for rebind: %+v", driftedRow["pi_selected"])
	}
	driftDigest := driftedSource["source_digest"].(string)
	unrenderable := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/exports/pi/render", map[string]any{
		"expected_source_digest": driftDigest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections": map[string]any{
			fmt.Sprintf("%d", modelConfigID): map[string]any{
				"provider_id": "openai", "model_id": "gpt-export", "api": "openai-responses",
			},
		},
	}, nil, http.StatusUnprocessableEntity)
	if code, _ := unrenderable["code"].(string); code != "candidate_invalid" {
		t.Fatalf("drifted binding must reject render as candidate_invalid: %+v", unrenderable)
	}
	preview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusConflict)
	if !strings.Contains(exportErrorText(preview), "pi_binding_model_drifted") {
		t.Fatalf("drifted binding must block refresh preview: %+v", preview)
	}
	if preview["bound_prism_model_id"] != prismModelID || preview["catalog_model_id"] != "gpt-export" {
		t.Fatalf("drift error must separate frozen Prism identity from directory id: %+v", preview)
	}
	overrideBlocked := requestJSONStatus[map[string]any](t, harness, http.MethodPut, fmt.Sprintf("/api/models/%d/pi/override", modelConfigID), map[string]any{"max_tokens": 4096}, nil, http.StatusConflict)
	if !strings.Contains(exportErrorText(overrideBlocked), "pi_binding_model_drifted") {
		t.Fatalf("drifted binding must block override writes: %+v", overrideBlocked)
	}

	reconfirmed := exportPiBind(t, harness, modelConfigID, map[string]any{
		"provider_id":               "openai",
		"catalog_model_id":          "gpt-export",
		"expected_catalog_revision": revision,
		"expected_prism_model_id":   "renamed/gpt-export",
		"expected_pi_api":           "openai-responses",
	}, http.StatusOK)
	if reconfirmed["prism_model_id_at_bind"] != "renamed/gpt-export" {
		t.Fatalf("identity re-confirmation must move only the snapshot: %+v", reconfirmed)
	}
	if reconfirmed["bind_source"] != "manual" || reconfirmed["catalog_revision"] != revision {
		t.Fatalf("re-confirmation must preserve bind source and frozen revision: %+v", reconfirmed)
	}
	if reconfirmed["fetched_at"] != bound["fetched_at"] || reconfirmed["updated_at"] == updatedAtBeforeIdentityReconfirm {
		t.Fatalf("re-confirmation must preserve fetched_at and advance updated_at: before=%v after=%+v", updatedAtBeforeIdentityReconfirm, reconfirmed)
	}
	if asMap(t, reconfirmed["source"])["context_window"] != float64(400000) {
		t.Fatalf("re-confirmation must preserve the frozen source snapshot: %+v", reconfirmed["source"])
	}
	if asMap(t, reconfirmed["override"])["name"] != "Operator Chosen Name" {
		t.Fatalf("re-confirmation must preserve operator overrides: %+v", reconfirmed["override"])
	}
	if asMap(t, reconfirmed["effective"])["name"] != "Operator Chosen Name" {
		t.Fatalf("effective value must still prefer the override: %+v", reconfirmed["effective"])
	}
	restoredSource := exportFetchSource(t, harness)
	restoredRow := exportSourceRow(t, restoredSource, modelConfigID)
	if restoredRow["pi_binding_renderable"] != true {
		t.Fatalf("re-confirmed binding must render again: %+v", restoredRow)
	}
	if restoredDigest := restoredSource["source_digest"].(string); restoredDigest == driftDigest || restoredDigest == renderDigest {
		t.Fatalf("identity re-confirmation must move the digest exactly once: %s", restoredDigest)
	}

	unchanged := exportPiBind(t, harness, modelConfigID, map[string]any{
		"provider_id":               "openai",
		"catalog_model_id":          "gpt-export",
		"expected_catalog_revision": revision,
		"expected_prism_model_id":   "renamed/gpt-export",
		"expected_pi_api":           "openai-responses",
	}, http.StatusOK)
	if unchanged["updated_at"] != reconfirmed["updated_at"] {
		t.Fatalf("same-coordinate same-identity bind must not advance updated_at: %v vs %v", unchanged["updated_at"], reconfirmed["updated_at"])
	}
	if after := exportFetchSource(t, harness)["source_digest"].(string); after != restoredSource["source_digest"] {
		t.Fatalf("same-coordinate same-identity bind must not move the digest")
	}
}

func TestModelExportPiBindRejectsCrossAPIUnknownAndStaleWithZeroWrites(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")
	source := exportFetchSource(t, harness)
	revision := source["catalog"].(map[string]any)["revision"].(string)
	baselineDigest := source["source_digest"].(string)

	reject := func(body map[string]any, status int, needle string) {
		t.Helper()
		payload := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), body, nil, status)
		if !strings.Contains(fmt.Sprintf("%v", payload), needle) {
			t.Fatalf("bind rejection %q missing from %+v", needle, payload)
		}
		if after := exportFetchSource(t, harness)["source_digest"].(string); after != baselineDigest {
			t.Fatalf("rejected bind must write nothing: digest moved %s -> %s", baselineDigest, after)
		}
	}

	reject(map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-export-chat",
		"expected_catalog_revision": revision, "expected_prism_model_id": "gpt-export", "expected_pi_api": "openai-responses",
	}, http.StatusUnprocessableEntity, "pi_candidate_api_mismatch")
	reject(map[string]any{
		"provider_id": "not-a-provider", "catalog_model_id": "gpt-export",
		"expected_catalog_revision": revision, "expected_prism_model_id": "gpt-export", "expected_pi_api": "openai-responses",
	}, http.StatusUnprocessableEntity, "pi_candidate_unknown")
	reject(map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-export-typo",
		"expected_catalog_revision": revision, "expected_prism_model_id": "gpt-export", "expected_pi_api": "openai-responses",
	}, http.StatusUnprocessableEntity, "pi_candidate_unknown")
	reject(map[string]any{
		"catalog_model_id":          "gpt-export",
		"expected_catalog_revision": revision, "expected_prism_model_id": "gpt-export", "expected_pi_api": "openai-responses",
	}, http.StatusUnprocessableEntity, "must be provided together")
	reject(map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-export",
		"expected_catalog_revision": revision, "expected_prism_model_id": "some-other-model", "expected_pi_api": "openai-responses",
	}, http.StatusConflict, "pi_model_changed")
	reject(map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-export",
		"expected_catalog_revision": revision, "expected_prism_model_id": "gpt-export", "expected_pi_api": "anthropic-messages",
	}, http.StatusConflict, "pi_model_changed")
	reject(map[string]any{
		"provider_id": "openai", "catalog_model_id": "gpt-export",
		"expected_catalog_revision": "sha256-stale", "expected_prism_model_id": "gpt-export", "expected_pi_api": "openai-responses",
	}, http.StatusConflict, "pi_catalog_stale")
	reject(map[string]any{"provider_id": "openai", "catalog_model_id": "gpt-export", "expected_catalog_revision": revision, "expected_pi_api": "openai-responses"},
		http.StatusUnprocessableEntity, "expected_prism_model_id")
	reject(map[string]any{"provider_id": "openai", "catalog_model_id": "gpt-export", "expected_catalog_revision": revision, "expected_prism_model_id": "gpt-export"},
		http.StatusUnprocessableEntity, "expected_pi_api")
	reject(map[string]any{"provider_id": "openai", "catalog_model_id": "gpt-export", "expected_prism_model_id": "gpt-export", "expected_pi_api": "openai-responses"},
		http.StatusUnprocessableEntity, "expected_catalog_revision")

	imageOnlyID, _ := exportSeedModel(t, harness, "gpt-image-only", "openai", "dual_native")
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET openai_accepted_format = NULL, openai_image_operations = 'generations' WHERE id = $1`, imageOnlyID); err != nil {
		t.Fatalf("make seeded model image-only: %v", err)
	}
	noAPI := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/search", imageOnlyID), map[string]any{"model_id_query": "gpt"}, nil, http.StatusUnprocessableEntity)
	if !strings.Contains(fmt.Sprintf("%v", noAPI), "pi_api_unsupported") {
		t.Fatalf("undeterminable final Pi API must reject search: %+v", noAPI)
	}
	noBind := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", imageOnlyID), map[string]any{
		"expected_catalog_revision": revision, "expected_prism_model_id": "gpt-image-only", "expected_pi_api": "openai-responses",
	}, nil, http.StatusUnprocessableEntity)
	if !strings.Contains(fmt.Sprintf("%v", noBind), "pi_api_unsupported") {
		t.Fatalf("undeterminable final Pi API must reject bind: %+v", noBind)
	}
}

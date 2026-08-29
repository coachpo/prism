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
      "name": "GPT Multi (OpenAI)",
      "api": "openai-responses",
      "provider": "openai",
      "reasoning": true,
      "contextWindow": 300000
    }
  },
  "openrouter": {
    "gpt-multi": {
      "id": "gpt-multi",
      "name": "GPT Multi (OpenRouter)",
      "api": "openai-responses",
      "provider": "openrouter",
      "reasoning": false,
      "contextWindow": 128000
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
	piCatalogServer := httptest.NewTLSServer(piCatalogHandler)
	t.Cleanup(piCatalogServer.Close)
	piCatalogClient, piClientErr := pidev.NewClient(pidev.ClientOptions{BaseURL: piCatalogServer.URL, HTTPClient: piCatalogServer.Client()})
	if piClientErr != nil {
		t.Fatalf("build pi catalog client: %v", piClientErr)
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
	return requestJSONStatus[map[string]any](t, harness, http.MethodGet, "/api/models/export/source", nil, nil, http.StatusOK)
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

// exportBindPi binds a model to its single exact pi.dev candidate and returns
// the accepted coordinate for building a render selections assertion.
func exportBindPi(t *testing.T, harness *contractHarness, modelConfigID int) map[string]any {
	t.Helper()
	source := exportFetchSource(t, harness)
	row := exportSourceRow(t, source, modelConfigID)
	if row["candidate_status"] != "single" {
		t.Fatalf("fixture must offer exactly one pi candidate before auto-bind: %+v", row)
	}
	catalogRevision := source["catalog"].(map[string]any)["revision"].(string)
	bound := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"expected_catalog_revision": catalogRevision,
	}, nil, http.StatusOK)
	if bound["bound"] != true {
		t.Fatalf("bind must report bound=true: %+v", bound)
	}
	return bound
}

func piSelectionAssertion(bound map[string]any) map[string]any {
	return map[string]any{
		"provider_id": bound["provider_id"],
		"model_id":    bound["catalog_model_id"],
		"api":         bound["api"],
	}
}

func TestModelExportSourceAndRenderContracts(t *testing.T) {
	harness := newExportContractHarness(t, exportServingCatalog, piServingCatalogHandler)
	modelConfigID, _ := exportSeedModel(t, harness, "gpt-export", "openai", "dual_native")

	source, sourceHeaders := exportRequestWithHeaders(t, harness, http.MethodGet, "/api/models/export/source", nil, http.StatusOK)
	if got := sourceHeaders.Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("source cache policy = %q, want private, no-store", got)
	}
	if source["target_version"] != "0.84.3" {
		t.Fatalf("source target_version drifted: %+v", source["target_version"])
	}
	row := exportSourceRow(t, source, modelConfigID)
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
	completeness := row["platform_completeness"].(map[string]any)
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
	// its safe fields now feed platform_completeness.
	bound := exportBindPi(t, harness, modelConfigID)
	if bound["bind_source"] != "single_candidate" {
		t.Fatalf("auto-applied single candidate must record bind_source: %+v", bound)
	}

	source = exportFetchSource(t, harness)
	digest := source["source_digest"].(string)
	if len(digest) != 64 {
		t.Fatalf("source_digest must be sha256 hex: %q", digest)
	}
	row = exportSourceRow(t, source, modelConfigID)
	if row["pi_binding_status"] != "bound" {
		t.Fatalf("bound model must report bound status: %+v", row)
	}
	selected := asMap(t, row["pi_selected"])
	if selected["provider_id"] != "openai" || selected["model_id"] != "gpt-export" || selected["api"] != "openai-responses" {
		t.Fatalf("pi_selected must carry the bound coordinate: %+v", selected)
	}
	completeness = row["platform_completeness"].(map[string]any)
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
	rendered, renderHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/export/render", renderRequest, http.StatusOK)
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
	reRendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", renderRequest, nil, http.StatusOK)
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
	stale, staleHeaders := exportRequestWithHeaders(t, harness, http.MethodPost, "/api/models/export/render", staleBody, http.StatusConflict)
	if stale["detail"] != "export_source_stale" {
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
	unknownResponse := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", unknown, nil, http.StatusUnprocessableEntity)
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
	wrongResponse := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", wrongAssertion, nil, http.StatusUnprocessableEntity)
	if !strings.Contains(fmt.Sprint(wrongResponse["detail"]), "is not a current Pi candidate") {
		t.Fatalf("mismatched selection assertion must report the stable candidate_invalid detail: %+v", wrongResponse)
	}

	// A render request that omits the selections assertion entirely for a
	// bound model must also fail closed, never silently trust the binding.
	omittedAssertion := map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
	}
	omittedResponse := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", omittedAssertion, nil, http.StatusUnprocessableEntity)
	if omittedResponse == nil {
		t.Fatalf("omitted selection assertion must fail closed")
	}
}

func TestModelExportPiRefreshAndOverride(t *testing.T) {
	var catalogGeneration atomic.Int32
	catalogGeneration.Store(1)
	harness := newExportContractHarness(t, exportServingCatalog, func(w http.ResponseWriter, r *http.Request) {
		if catalogGeneration.Load() == 1 {
			piServingCatalogHandler(w, r)
			return
		}
		// Second generation changes the bound candidate's safe fields so a
		// refresh has something to preview and commit.
		body := []byte(strings.Replace(piFixtureCatalog, `"contextWindow": 400000`, `"contextWindow": 800000`, 1))
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

	overrideDigest := exportFetchSource(t, harness)["source_digest"].(string)
	if overrideDigest == firstDigest {
		t.Fatalf("an override must move the source digest")
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

	// Advance the fixture catalog, then preview and commit the refresh.
	catalogGeneration.Store(2)
	preview := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/preview", modelConfigID), map[string]any{}, nil, http.StatusOK)
	if preview["changed"] != true {
		t.Fatalf("advanced catalog must preview a change: %+v", preview)
	}
	nextRevision := preview["catalog_revision"].(string)
	if nextRevision == bound["catalog_revision"] {
		t.Fatalf("advanced catalog must carry a new revision")
	}

	// A commit against a stale (superseded) revision fails closed and writes
	// nothing: the 409 status itself is the assertion.
	requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/commit", modelConfigID), map[string]any{
		"expected_catalog_revision": bound["catalog_revision"],
	}, nil, http.StatusConflict)

	committed := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/refresh/commit", modelConfigID), map[string]any{
		"expected_catalog_revision": nextRevision,
	}, nil, http.StatusOK)
	if committed["catalog_revision"] != nextRevision {
		t.Fatalf("commit must persist the previewed revision: %+v", committed)
	}
	if committed["source"].(map[string]any)["context_window"].(float64) != 800000 {
		t.Fatalf("commit must replace source fields from the refreshed catalog: %+v", committed["source"])
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
	renderAfterUnbind := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", map[string]any{
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
	catalogRevision := source["catalog"].(map[string]any)["revision"].(string)

	// Binding without explicit coordinates must reject with the candidate
	// evidence, never auto-merge or pick by lexical/provider order.
	ambiguous := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"expected_catalog_revision": catalogRevision,
	}, nil, http.StatusConflict)
	if candList, ok := ambiguous["candidates"].([]any); !ok || len(candList) != 2 {
		t.Fatalf("ambiguous bind must return the full candidate list as evidence: %+v", ambiguous)
	}

	// Explicit bind to the openai-hosted candidate.
	firstBind := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"provider_id":               "openai",
		"catalog_model_id":          "gpt-multi",
		"expected_catalog_revision": catalogRevision,
	}, nil, http.StatusOK)
	if firstBind["bind_source"] != "manual" || firstBind["provider_id"] != "openai" {
		t.Fatalf("explicit disambiguation among multiple candidates must record manual bind_source: %+v", firstBind)
	}
	firstDigest := exportFetchSource(t, harness)["source_digest"].(string)

	// Rebinding to the other exact-id candidate must clear inherited
	// assumptions (a fresh coordinate) and move the digest again.
	secondBind := requestJSONStatus[map[string]any](t, harness, http.MethodPost, fmt.Sprintf("/api/models/%d/pi/bind", modelConfigID), map[string]any{
		"provider_id":               "openrouter",
		"catalog_model_id":          "gpt-multi",
		"expected_catalog_revision": catalogRevision,
	}, nil, http.StatusOK)
	if secondBind["provider_id"] != "openrouter" {
		t.Fatalf("rebind must switch the persisted coordinate to the newly chosen candidate: %+v", secondBind)
	}
	secondDigest := exportFetchSource(t, harness)["source_digest"].(string)
	if secondDigest == firstDigest {
		t.Fatalf("rebinding to a different exact-id candidate must move the source digest")
	}

	// A render selections assertion still naming the old (pre-rebind)
	// coordinate must now be rejected as non-current.
	staleAssertion := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", map[string]any{
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

	emptyManual := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", map[string]any{
		"expected_source_digest": digest,
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"credential":             map[string]any{"include": true, "api_key": "   "},
		"selections":             map[string]any{fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound)},
	}, nil, http.StatusOK)
	emptyContent := emptyManual["content"].(string)
	if !strings.Contains(emptyContent, `"apiKey": ""`) {
		t.Fatalf("include=true must preserve the explicitly confirmed trimmed empty string: %s", emptyContent)
	}
	if strings.Contains(emptyContent, "sk-export-live-key") || strings.Contains(emptyContent, "https://export.example") {
		t.Fatalf("explicit empty key must never fall back to stored endpoint data: %s", emptyContent)
	}

	legacy := harness.requestJSON(t, harness.client, http.MethodPost, "/api/models/export/render", map[string]any{
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

	rendered := requestJSONStatus[map[string]any](t, harness, http.MethodPost, "/api/models/export/render", map[string]any{
		"expected_source_digest": source["source_digest"],
		"model_config_ids":       []int{modelConfigID},
		"base_url":               "https://prism-client.example",
		"selections":             map[string]any{fmt.Sprintf("%d", modelConfigID): piSelectionAssertion(bound)},
	}, nil, http.StatusOK)
	if !strings.Contains(rendered["content"].(string), `"id": "gpt-export"`) {
		t.Fatalf("stored truth still renders during a catalog outage: %s", rendered["content"])
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

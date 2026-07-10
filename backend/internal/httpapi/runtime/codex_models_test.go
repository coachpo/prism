package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

func TestCodexModelsCatalogKnownModelsSatisfyClientContract(t *testing.T) {
	snapshot := modelsCatalogTestSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-5.5"},
		runtimeModelRecord{ID: 2, APIFamily: "OPENAI", ModelID: "gpt-5.4"},
		runtimeModelRecord{ID: 3, APIFamily: " openai ", ModelID: "gpt-5.4-mini"},
		runtimeModelRecord{ID: 4, APIFamily: "openai", ModelID: "gpt-5.6-luna"},
		runtimeModelRecord{ID: 5, APIFamily: "openai", ModelID: "gpt-5.6-sol"},
		runtimeModelRecord{ID: 6, APIFamily: "openai", ModelID: "gpt-5.6-terra"},
		runtimeModelRecord{ID: 7, APIFamily: "anthropic", ModelID: "claude-ignored"},
		runtimeModelRecord{ID: 8, APIFamily: "openai", ModelID: " "},
	)

	response := requestModelsCatalog(t, snapshot, "/v1/models?client_version=0.143.0", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected Codex catalog status 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Codex catalog JSON content type, got %q", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode Codex catalog response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected Codex catalog top level to contain only models, got %+v", payload)
	}
	rawModels, ok := payload["models"].([]any)
	if !ok {
		t.Fatalf("expected Codex catalog models array, got %+v", payload["models"])
	}
	if len(rawModels) != 6 {
		t.Fatalf("expected six known Codex models, got %d: %+v", len(rawModels), rawModels)
	}

	requiredKeys := []string{
		"slug",
		"display_name",
		"supported_reasoning_levels",
		"shell_type",
		"visibility",
		"supported_in_api",
		"priority",
		"base_instructions",
		"supports_reasoning_summaries",
		"support_verbosity",
		"truncation_policy",
		"supports_parallel_tool_calls",
		"experimental_supported_tools",
	}
	validEfforts := map[string]struct{}{
		"none":    {},
		"minimal": {},
		"low":     {},
		"medium":  {},
		"high":    {},
		"xhigh":   {},
		"max":     {},
		"ultra":   {},
	}
	validShellTypes := map[string]struct{}{
		"default":       {},
		"local":         {},
		"unified_exec":  {},
		"disabled":      {},
		"shell_command": {},
	}
	validVisibilities := map[string]struct{}{
		"list": {},
		"hide": {},
		"none": {},
	}

	seen := make(map[string]struct{}, len(rawModels))
	templates, _ := loadCodexTemplates()
	var previous map[string]any
	for _, rawModel := range rawModels {
		model, ok := rawModel.(map[string]any)
		if !ok {
			t.Fatalf("expected Codex model object, got %T", rawModel)
		}
		slug, _ := model["slug"].(string)
		seen[slug] = struct{}{}
		for _, key := range requiredKeys {
			if _, ok := model[key]; !ok {
				t.Fatalf("expected Codex model %q to contain required key %q", slug, key)
			}
		}
		if displayName, _ := model["display_name"].(string); displayName == "" {
			t.Fatalf("expected Codex model %q to have a display name", slug)
		}
		if baseInstructions, _ := model["base_instructions"].(string); baseInstructions == "" {
			t.Fatalf("expected Codex model %q to have base instructions", slug)
		}
		if _, ok := model["supported_in_api"].(bool); !ok {
			t.Fatalf("expected Codex model %q supported_in_api to be boolean", slug)
		}
		for _, key := range []string{"supports_reasoning_summaries", "support_verbosity", "supports_parallel_tool_calls"} {
			if _, ok := model[key].(bool); !ok {
				t.Fatalf("expected Codex model %q %s to be boolean", slug, key)
			}
		}
		if _, ok := model["priority"].(float64); !ok {
			t.Fatalf("expected Codex model %q priority to be numeric", slug)
		}
		if _, ok := validShellTypes[model["shell_type"].(string)]; !ok {
			t.Fatalf("expected Codex model %q shell_type to be valid, got %q", slug, model["shell_type"])
		}
		if _, ok := validVisibilities[model["visibility"].(string)]; !ok {
			t.Fatalf("expected Codex model %q visibility to be valid, got %q", slug, model["visibility"])
		}

		truncation, ok := model["truncation_policy"].(map[string]any)
		if !ok {
			t.Fatalf("expected Codex model %q truncation_policy object, got %T", slug, model["truncation_policy"])
		}
		if mode, _ := truncation["mode"].(string); mode != "bytes" && mode != "tokens" {
			t.Fatalf("expected Codex model %q truncation mode to be valid, got %q", slug, mode)
		}
		if _, ok := truncation["limit"].(float64); !ok {
			t.Fatalf("expected Codex model %q truncation limit to be numeric", slug)
		}

		reasoningLevels, ok := model["supported_reasoning_levels"].([]any)
		if !ok || len(reasoningLevels) == 0 {
			t.Fatalf("expected Codex model %q reasoning levels, got %+v", slug, model["supported_reasoning_levels"])
		}
		for _, rawLevel := range reasoningLevels {
			level, ok := rawLevel.(map[string]any)
			if !ok {
				t.Fatalf("expected Codex model %q reasoning level object, got %T", slug, rawLevel)
			}
			effort, effortOK := level["effort"].(string)
			description, descriptionOK := level["description"].(string)
			if !effortOK || !descriptionOK || description == "" {
				t.Fatalf("expected Codex model %q reasoning level fields, got %+v", slug, level)
			}
			if _, ok := validEfforts[effort]; !ok {
				t.Fatalf("expected Codex model %q reasoning effort to be valid, got %q", slug, effort)
			}
		}
		if _, ok := model["experimental_supported_tools"].([]any); !ok {
			t.Fatalf("expected Codex model %q experimental_supported_tools array, got %T", slug, model["experimental_supported_tools"])
		}
		if expected := cloneCodexModelTemplate(templates[slug]); !reflect.DeepEqual(model, expected) {
			t.Fatalf("expected Codex model %q to preserve its template exactly", slug)
		}

		if previous != nil {
			previousPriority := previous["priority"].(float64)
			priority := model["priority"].(float64)
			previousSlug := previous["slug"].(string)
			if priority < previousPriority || (priority == previousPriority && slug < previousSlug) {
				t.Fatalf("expected deterministic priority/slug ordering, got %q after %q", slug, previousSlug)
			}
		}
		previous = model
	}

	for _, slug := range []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.5", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra"} {
		if _, ok := seen[slug]; !ok {
			t.Fatalf("expected Codex catalog to contain %q, got %+v", slug, seen)
		}
	}
}

func TestCodexModelsCatalogSynthesizesUnknownModelsWithoutPollutingTemplates(t *testing.T) {
	snapshot := modelsCatalogTestSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-5.5"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "a-future-model"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "z-future-model"},
	)

	first := requestModelsCatalog(t, snapshot, "/v1/models?client_version=", "")
	models := decodeModelsCatalog(t, first)
	bySlug := modelsBySlug(t, models)
	known := requireCatalogModel(t, bySlug, "gpt-5.5")
	aFuture := requireCatalogModel(t, bySlug, "a-future-model")
	zFuture := requireCatalogModel(t, bySlug, "z-future-model")
	knownPriority := requireCatalogPriority(t, known)
	aPriority := requireCatalogPriority(t, aFuture)
	zPriority := requireCatalogPriority(t, zFuture)
	templates, maxPriority := loadCodexTemplates()
	if aPriority != float64(maxPriority+100) || zPriority != float64(maxPriority+200) {
		t.Fatalf("expected exact unknown priorities after template maximum %d, got a=%v z=%v", maxPriority, aPriority, zPriority)
	}
	if knownPriority >= aPriority {
		t.Fatalf("expected known template priority before unknown models, got known=%v a=%v", knownPriority, aPriority)
	}
	for _, slug := range []string{"a-future-model", "z-future-model"} {
		model := requireCatalogModel(t, bySlug, slug)
		if model["slug"] != slug || model["display_name"] != slug {
			t.Fatalf("expected synthesized identifiers for %q, got %+v", slug, model)
		}
		if model["description"] != "" {
			t.Fatalf("expected synthesized description for %q to be empty, got %#v", slug, model["description"])
		}
		if baseInstructions, _ := model["base_instructions"].(string); baseInstructions == "" {
			t.Fatalf("expected synthesized model %q to inherit base instructions", slug)
		}
		expected := cloneCodexModelTemplate(templates["gpt-5.5"])
		expected["slug"] = slug
		expected["display_name"] = slug
		expected["description"] = ""
		expected["priority"] = model["priority"]
		if !reflect.DeepEqual(model, expected) {
			t.Fatalf("expected synthesized model %q to differ from gpt-5.5 only by allowed overrides", slug)
		}
	}

	snapshot.ModelsByID = map[string]runtimeModelRecord{
		"gpt-5.5": {ID: 4, APIFamily: "openai", ModelID: "gpt-5.5"},
	}
	second := requestModelsCatalog(t, snapshot, "/v1/models?client_version=0.143.0", "")
	knownModels := decodeModelsCatalog(t, second)
	if len(knownModels) != 1 || knownModels[0]["slug"] != "gpt-5.5" {
		t.Fatalf("expected fallback synthesis not to mutate gpt-5.5 template, got %+v", knownModels)
	}
}

func TestOpenAIModelsHandlerPreservesLegacyBytesAndBranchesToCodexCatalog(t *testing.T) {
	createdAt := time.Unix(1_700_000_000, 0).UTC()
	snapshot := modelsCatalogTestSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-5.5", CreatedAt: createdAt},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "gpt-5.4", CreatedAt: createdAt.Add(time.Second)},
	)

	legacy := requestModelsCatalog(t, snapshot, "/v1/models", "")
	const expectedLegacy = "{\"object\":\"list\",\"data\":[{\"id\":\"gpt-5.4\",\"object\":\"model\",\"created\":1700000001,\"owned_by\":\"prism\"},{\"id\":\"gpt-5.5\",\"object\":\"model\",\"created\":1700000000,\"owned_by\":\"prism\"}]}\n"
	if got := legacy.Body.String(); got != expectedLegacy {
		t.Fatalf("expected legacy OpenAI response bytes to remain unchanged,\nwant: %q\n got: %q", expectedLegacy, got)
	}

	codex := requestModelsCatalog(t, snapshot, "/v1/models?client_version=0.143.0", "")
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(codex.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode Codex response: %v", err)
	}
	if len(payload) != 1 || payload["models"] == nil {
		t.Fatalf("expected client_version request to return only models, got %+v", payload)
	}
}

func TestCodexModelsCatalogFiltersByAcceptedOpenAIFormatWithoutChangingPlainList(t *testing.T) {
	tests := []struct {
		modelID   string
		format    string
		wantCodex bool
	}{
		{modelID: "gpt-5.5", format: providerauth.OpenAITextCapabilityResponsesOnly, wantCodex: true},
		{modelID: "gpt-5.4", format: providerauth.OpenAITextCapabilityChatCompletionsOnly, wantCodex: false},
		{modelID: "gpt-5.4-mini", format: providerauth.OpenAITextCapabilityDualNative, wantCodex: true},
	}
	models := make([]runtimeModelRecord, 0, len(tests))
	for index, test := range tests {
		format := test.format
		models = append(models, runtimeModelRecord{
			ID:                   index + 1,
			APIFamily:            "openai",
			ModelID:              test.modelID,
			OpenAIAcceptedFormat: &format,
		})
	}
	snapshot := modelsCatalogTestSnapshot(models...)
	codexModels := modelsBySlug(t, buildCodexModelsCatalogResponse(snapshot).Models)
	plainModels := buildOpenAIModelsListResponse(snapshot)
	plainIDs := make(map[string]struct{}, len(plainModels.Data))
	for _, model := range plainModels.Data {
		plainIDs[model.ID] = struct{}{}
	}

	for _, test := range tests {
		_, codexListed := codexModels[test.modelID]
		if codexListed != test.wantCodex {
			t.Errorf("Codex listed %q = %t, want %t for format %q", test.modelID, codexListed, test.wantCodex, test.format)
		}
		if _, plainListed := plainIDs[test.modelID]; !plainListed {
			t.Errorf("plain OpenAI list omitted %q with format %q", test.modelID, test.format)
		}
	}
}

func TestCodexModelsCatalogETagAndEmptyResponse(t *testing.T) {
	snapshot := modelsCatalogTestSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-5.5"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "gpt-5.4"},
	)

	first := requestModelsCatalog(t, snapshot, "/v1/models?client_version=0.143.0", "")
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("expected initial Codex catalog response with ETag, status=%d etag=%q body=%s", first.Code, etag, first.Body.String())
	}
	wantETag := fmt.Sprintf(`W/"%x"`, sha256.Sum256(first.Body.Bytes()))
	if etag != wantETag {
		t.Fatalf("expected content-derived weak ETag %q, got %q", wantETag, etag)
	}
	for _, path := range []string{
		"/v1/models?client_version=",
		"/v1/models?client_version=arbitrary-client",
	} {
		repeated := requestModelsCatalog(t, snapshot, path, "")
		if repeated.Body.String() != first.Body.String() || repeated.Header().Get("ETag") != etag {
			t.Fatalf("expected client_version value to be ignored for %q, body_equal=%t etag=%q", path, repeated.Body.String() == first.Body.String(), repeated.Header().Get("ETag"))
		}
	}
	reordered := modelsCatalogTestSnapshot(
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "gpt-5.4"},
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-5.5"},
	)
	reorderedResponse := requestModelsCatalog(t, reordered, "/v1/models?client_version=0.143.0", "")
	if reorderedResponse.Body.String() != first.Body.String() || reorderedResponse.Header().Get("ETag") != etag {
		t.Fatalf("expected catalog bytes and ETag to be independent of snapshot map insertion order")
	}

	notModified := requestModelsCatalog(t, snapshot, "/v1/models?client_version=0.143.0", etag)
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("expected matching ETag to return 304, got %d: %s", notModified.Code, notModified.Body.String())
	}
	if notModified.Body.Len() != 0 {
		t.Fatalf("expected 304 response to have an empty body, got %q", notModified.Body.String())
	}
	if got := notModified.Header().Get("ETag"); got != etag {
		t.Fatalf("expected 304 response to preserve ETag %q, got %q", etag, got)
	}

	snapshot.ModelsByID["my-future-model"] = runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "my-future-model"}
	changed := requestModelsCatalog(t, snapshot, "/v1/models?client_version=0.143.0", "")
	if changedETag := changed.Header().Get("ETag"); changedETag == "" || changedETag == etag {
		t.Fatalf("expected model-set change to produce a different ETag, before=%q after=%q", etag, changedETag)
	}

	empty := requestModelsCatalog(t, &planningSnapshot{ModelsByID: map[string]runtimeModelRecord{}}, "/v1/models?client_version=0.143.0", "")
	if empty.Code != http.StatusOK || empty.Body.String() != "{\"models\":[]}" {
		t.Fatalf("expected empty Codex catalog response, status=%d body=%q", empty.Code, empty.Body.String())
	}
}

func requestModelsCatalog(t *testing.T, snapshot *planningSnapshot, path string, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	cache := NewSharedCache()
	cache.published.Store(&publishedRuntimeSnapshot{
		Generation:    1,
		PublishedAt:   time.Unix(1_700_000_000, 0).UTC(),
		ActiveProfile: profiledomain.Profile{ID: profiledomain.DefaultProfileID, Name: "Default", IsActive: true},
		PlanningByProfileID: map[int]*planningSnapshot{
			profiledomain.DefaultProfileID: snapshot,
		},
	})
	service := &Service{cache: cache}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}
	response := httptest.NewRecorder()
	service.handleOpenAIModelsList(response, request)
	return response
}

func modelsCatalogTestSnapshot(models ...runtimeModelRecord) *planningSnapshot {
	byID := make(map[string]runtimeModelRecord, len(models))
	for _, model := range models {
		byID[model.ModelID] = model
	}
	return &planningSnapshot{ModelsByID: byID}
}

func decodeModelsCatalog(t *testing.T, response *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("expected Codex catalog status 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode Codex catalog response: %v", err)
	}
	return payload.Models
}

func modelsBySlug(t *testing.T, models []map[string]any) map[string]map[string]any {
	t.Helper()
	bySlug := make(map[string]map[string]any, len(models))
	for _, model := range models {
		slug, ok := model["slug"].(string)
		if !ok || slug == "" {
			t.Fatalf("expected Codex model slug, got %+v", model)
		}
		bySlug[slug] = model
	}
	return bySlug
}

func requireCatalogModel(t *testing.T, bySlug map[string]map[string]any, slug string) map[string]any {
	t.Helper()
	model, ok := bySlug[slug]
	if !ok {
		t.Fatalf("expected Codex catalog model %q, got %+v", slug, bySlug)
	}
	return model
}

func requireCatalogPriority(t *testing.T, model map[string]any) float64 {
	t.Helper()
	priority, ok := model["priority"].(float64)
	if !ok {
		t.Fatalf("expected Codex model priority, got %+v", model)
	}
	return priority
}

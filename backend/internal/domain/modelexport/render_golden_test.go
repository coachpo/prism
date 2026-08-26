package modelexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

// fixtureFacts covers one model per family plus a tiered-price model so both
// renderers exercise protocol mapping, price gates, and enrichment replay.
func fixtureFacts() SourceFacts {
	prismMetadataGPT := map[string]json.RawMessage{
		MetaReasoning: rawValue(true),
	}
	prismMetadataClaude := map[string]json.RawMessage{
		MetaName:          rawValue("Claude Opus 4.8"),
		MetaContextWindow: rawValue(200000),
	}
	return SourceFacts{
		Platform: PlatformPi,
		Models: []ModelFact{
			{
				ModelConfigID: 3, ModelID: "gpt-5.6-sol", APIFamily: "openai",
				IsEnabled: true, Selectable: true,
				OpenAIAcceptedFormat: ptrString("dual_native"),
				CatalogBinding:       CatalogEvidence{Bound: true, ProviderID: "openai", CatalogModelID: "gpt-5.6-sol", CatalogRevision: `"rev-1"`},
				Enrichment:           EnrichmentEvidence{OfferingProviderID: "openai", OfferingModelID: "gpt-5.6-sol"},
				PrismMetadata:        prismMetadataGPT,
				Targets: []TargetFact{{
					TerminalTargetID: 11, Position: 0, EndpointID: 21,
					EndpointName: "primary",
					Pricing: &TargetPriceSnapshot{
						Kind: pricingkind.Standard, Card: completeCardPtr(),
						CurrencyCode: "USD", PricingUnit: "PER_1M",
					},
				}},
			},
			{
				ModelConfigID: 5, ModelID: "claude-opus-4-8", APIFamily: "anthropic",
				IsEnabled: true, Selectable: true,
				CatalogBinding: CatalogEvidence{Bound: false},
				PrismMetadata:  prismMetadataClaude,
				Targets: []TargetFact{{
					TerminalTargetID: 12, Position: 0, EndpointID: 22,
					EndpointName: "primary",
					Pricing: &TargetPriceSnapshot{
						Kind: pricingkind.Tiered,
						BaseCard: &PriceCardSnapshot{
							InputPrice: "5", OutputPrice: "25",
							CachedInputPrice: ptrString("0.5"), CacheCreationPrice: ptrString("6.25"),
							ReasoningPrice: ptrString("25"),
						},
						AboveCard: &PriceCardSnapshot{
							InputPrice: "10", OutputPrice: "37.5",
							CachedInputPrice: ptrString("1"), CacheCreationPrice: ptrString("12.5"),
							ReasoningPrice: ptrString("37.5"),
						},
						TierThreshold: ptrInt(200000),
						CurrencyCode:  "USD", PricingUnit: "PER_1M",
					},
				}},
			},
			{
				ModelConfigID: 8, ModelID: "gemini-4-pro", APIFamily: "gemini",
				IsEnabled: true, Selectable: true,
				CatalogBinding: CatalogEvidence{Bound: true, ProviderID: "google", CatalogModelID: "gemini-4-pro", CatalogRevision: `"rev-2"`, HasOverrides: true},
				Enrichment:     EnrichmentEvidence{OfferingProviderID: "google", OfferingModelID: "gemini-4-pro"},
				PrismMetadata:  map[string]json.RawMessage{},
				Targets: []TargetFact{{
					TerminalTargetID: 13, Position: 0, EndpointID: 23,
					EndpointName: "google",
					Pricing: &TargetPriceSnapshot{
						Kind: pricingkind.PeakValley, CurrencyCode: "USD", PricingUnit: "PER_1M",
					},
				}},
			},
			{
				ModelConfigID: 9, ModelID: "glm-5.2", APIFamily: "openai",
				IsEnabled: true, Selectable: true,
				OpenAIAcceptedFormat: ptrString("chat_completions_only"),
				CatalogBinding:       CatalogEvidence{Bound: true, ProviderID: "zai", CatalogModelID: "glm-5.2", CatalogRevision: `"rev-3"`},
				Enrichment:           EnrichmentEvidence{OfferingProviderID: "zai", OfferingModelID: "glm-5.2"},
				PrismMetadata:        map[string]json.RawMessage{},
				Targets: []TargetFact{{
					TerminalTargetID: 14, Position: 0, EndpointID: 24,
					EndpointName: "zai",
					Pricing: &TargetPriceSnapshot{
						Kind: pricingkind.Standard, Card: incompleteCardPtr(),
						CurrencyCode: "USD", PricingUnit: "PER_1M",
					},
				}},
			},
		},
	}
}

func completeCardPtr() *PriceCardSnapshot {
	return &PriceCardSnapshot{
		InputPrice: "3", OutputPrice: "15",
		CachedInputPrice: ptrString("0.3"), CacheCreationPrice: ptrString("3.75"),
		ReasoningPrice: ptrString("15"),
	}
}

func incompleteCardPtr() *PriceCardSnapshot {
	return &PriceCardSnapshot{
		InputPrice: "1", OutputPrice: "4",
		CachedInputPrice: nil, CacheCreationPrice: nil, ReasoningPrice: nil,
	}
}

func fixtureEnrichment(platform Platform) map[int]PlatformCandidate {
	gpt := PlatformCandidate{
		Metadata: NewMetadataLayer(map[string]json.RawMessage{
			MetaContextWindow:   rawValue(1050000),
			MetaMaxOutputTokens: rawValue(128000),
			MetaModalitiesInput: rawValue([]string{"text", "image"}),
		}),
		DerivedFields: map[string]json.RawMessage{},
	}
	if platform == PlatformPi {
		gpt.DerivedFields["thinkingLevelMap"] = rawValue(map[string]*string{
			"off": nil, "minimal": nil, "low": ptrString("low"), "medium": ptrString("medium"),
			"high": ptrString("high"), "xhigh": nil, "max": ptrString("max"),
		})
	}
	glm := PlatformCandidate{
		DerivedFields: map[string]json.RawMessage{},
	}
	if platform == PlatformOpenCode {
		glm.DerivedFields["interleaved"] = rawValue(map[string]string{"field": "reasoning_content"})
	}
	return map[int]PlatformCandidate{
		3: gpt,
		8: {
			Metadata: NewMetadataLayer(map[string]json.RawMessage{
				MetaName:             rawValue("Gemini 4 Pro"),
				MetaModalitiesOutput: rawValue([]string{"text"}),
			}),
			DerivedFields: map[string]json.RawMessage{},
		},
		9: glm,
	}
}

func TestRenderPiGolden(t *testing.T) {
	facts := fixtureFacts()
	facts.Platform = PlatformPi
	result, err := RenderPi(PiInput{
		Facts:      facts,
		Selection:  []int{3, 5, 8, 9},
		Enrichment: fixtureEnrichment(PlatformPi),
		BaseURL:    "https://prism.example",
	})
	if err != nil {
		t.Fatalf("RenderPi: %v", err)
	}
	verifyGolden(t, "pi_models.golden.json", result)
}

func TestRenderPiWithEmbeddedKeyGolden(t *testing.T) {
	facts := fixtureFacts()
	facts.Platform = PlatformPi
	result, err := RenderPi(PiInput{
		Facts:         facts,
		Selection:     []int{3},
		Enrichment:    fixtureEnrichment(PlatformPi),
		BaseURL:       "https://prism.example",
		IncludeAPIKey: true,
		APIKey:        "proxy-key",
	})
	if err != nil {
		t.Fatalf("RenderPi keyed: %v", err)
	}
	if !strings.Contains(result.Content, `"apiKey": "proxy-key"`) {
		t.Fatalf("embedded provider key missing:\n%s", result.Content)
	}
	verifyGolden(t, "pi_models_keyed.golden.json", result)
}

func TestRenderCredentialPresenceDistinguishesEmptyFromOmitted(t *testing.T) {
	facts := fixtureFacts()
	facts.Models = facts.Models[:1]
	piIncluded, err := RenderPi(PiInput{
		Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example", IncludeAPIKey: true, APIKey: "",
	})
	if err != nil {
		t.Fatalf("RenderPi included empty key: %v", err)
	}
	if !strings.Contains(piIncluded.Content, `"apiKey": ""`) {
		t.Fatalf("Pi must preserve an explicitly included empty key: %s", piIncluded.Content)
	}
	piOmitted, err := RenderPi(PiInput{Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example"})
	if err != nil {
		t.Fatalf("RenderPi omitted key: %v", err)
	}
	if strings.Contains(piOmitted.Content, `"apiKey"`) {
		t.Fatalf("Pi must omit the key slot when include=false: %s", piOmitted.Content)
	}

	facts.Platform = PlatformOpenCode
	ocIncluded, err := RenderOpenCode(OpenCodeInput{
		Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example", IncludeAPIKey: true, APIKey: "",
	})
	if err != nil {
		t.Fatalf("RenderOpenCode included empty key: %v", err)
	}
	if !strings.Contains(ocIncluded.Content, `"apiKey": ""`) {
		t.Fatalf("OpenCode must preserve an explicitly included empty key: %s", ocIncluded.Content)
	}
	ocOmitted, err := RenderOpenCode(OpenCodeInput{Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example"})
	if err != nil {
		t.Fatalf("RenderOpenCode omitted key: %v", err)
	}
	if strings.Contains(ocOmitted.Content, `"apiKey"`) {
		t.Fatalf("OpenCode must omit the key slot when include=false: %s", ocOmitted.Content)
	}
}

func TestRenderOpenCodeGolden(t *testing.T) {
	facts := fixtureFacts()
	facts.Platform = PlatformOpenCode
	defaultModel := 3
	result, err := RenderOpenCode(OpenCodeInput{
		Facts:      facts,
		Selection:  []int{3, 5, 8, 9},
		Enrichment: fixtureEnrichment(PlatformOpenCode),
		Enhancements: map[int]ManualEnhancement{3: {Fields: rawValue(map[string]any{
			"variants": map[string]any{
				"minimal": map[string]any{"reasoningEffort": "minimal", "reasoningSummary": "auto", "include": []string{"reasoning.encrypted_content"}},
				"high":    map[string]any{"reasoningEffort": "high", "reasoningSummary": "auto", "include": []string{"reasoning.encrypted_content"}},
			},
		})}},
		DefaultModel: &defaultModel,
		BaseURL:      "https://prism.example",
	})
	if err != nil {
		t.Fatalf("RenderOpenCode: %v", err)
	}
	if !strings.Contains(result.Content, `"PRISM_API_KEY"`) {
		t.Fatalf("opencode document must wire PRISM_API_KEY env slot:\n%s", result.Content)
	}
	verifyGolden(t, "opencode_config.golden.json", result)
}

func TestRenderOpenCodeDoesNotInventDefaultOrVariants(t *testing.T) {
	facts := fixtureFacts()
	facts.Platform = PlatformOpenCode
	result, err := RenderOpenCode(OpenCodeInput{
		Facts:      facts,
		Selection:  []int{3},
		Enrichment: fixtureEnrichment(PlatformOpenCode),
		BaseURL:    "https://prism.example",
	})
	if err != nil {
		t.Fatalf("RenderOpenCode: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(result.Content), &document); err != nil {
		t.Fatalf("decode OpenCode document: %v", err)
	}
	if _, exists := document["model"]; exists {
		t.Fatalf("OpenCode default must be omitted unless explicitly selected")
	}
	provider := document["provider"].(map[string]any)["prism"].(map[string]any)
	model := provider["models"].(map[string]any)["gpt-5.6-sol"].(map[string]any)
	if _, exists := model["variants"]; exists {
		t.Fatalf("models.dev reasoning options must not synthesize OpenCode variants")
	}
}

func TestRenderOpenCodeProjectsOnlySupportedCatalogMetadata(t *testing.T) {
	fact := ModelFact{
		ModelConfigID:        1,
		ModelID:              "metadata-model",
		APIFamily:            "openai",
		IsEnabled:            true,
		Selectable:           true,
		OpenAIAcceptedFormat: ptrString("responses_only"),
		PrismMetadata: map[string]json.RawMessage{
			MetaName:             rawValue("Metadata Model"),
			MetaDescription:      rawValue("not a target field"),
			MetaFamily:           rawValue("gpt"),
			MetaReleaseDate:      rawValue("2026-08-26"),
			MetaKnowledge:        rawValue("2026-01"),
			MetaStatus:           rawValue("active"),
			MetaAttachment:       rawValue(true),
			MetaReasoning:        rawValue(false),
			MetaTemperature:      rawValue(true),
			MetaToolCall:         rawValue(true),
			MetaContextWindow:    rawValue(200000),
			MetaMaxInputTokens:   rawValue(180000),
			MetaMaxOutputTokens:  rawValue(20000),
			MetaModalitiesInput:  rawValue([]string{"text", "image"}),
			MetaModalitiesOutput: rawValue([]string{"text"}),
		},
		Targets: []TargetFact{{TerminalTargetID: 1, Position: 0, EndpointID: 1, EndpointName: "primary"}},
	}
	result, err := RenderOpenCode(OpenCodeInput{
		Facts:     SourceFacts{Platform: PlatformOpenCode, Models: []ModelFact{fact}},
		Selection: []int{1}, BaseURL: "https://prism.example",
	})
	if err != nil {
		t.Fatalf("RenderOpenCode metadata: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(result.Content), &document); err != nil {
		t.Fatalf("decode OpenCode metadata document: %v", err)
	}
	provider := document["provider"].(map[string]any)["prism"].(map[string]any)
	model := provider["models"].(map[string]any)["metadata-model"].(map[string]any)
	for key, want := range map[string]any{
		"name": "Metadata Model", "family": "gpt", "release_date": "2026-08-26",
		"attachment": true, "reasoning": false, "temperature": true, "tool_call": true,
	} {
		if got := model[key]; got != want {
			t.Fatalf("OpenCode metadata %s = %#v, want %#v", key, got, want)
		}
	}
	limit := model["limit"].(map[string]any)
	if limit["context"] != float64(200000) || limit["input"] != float64(180000) || limit["output"] != float64(20000) {
		t.Fatalf("OpenCode limit projection = %+v", limit)
	}
	for _, omitted := range []string{"description", "knowledge", "status"} {
		if _, exists := model[omitted]; exists {
			t.Fatalf("OpenCode must omit unsupported catalog field %q: %+v", omitted, model)
		}
	}
}

func TestRenderResultsAreDeterministicAcrossRuns(t *testing.T) {
	facts := fixtureFacts()
	facts.Platform = PlatformPi
	first, err := RenderPi(PiInput{Facts: facts, Selection: []int{3, 5}, Enrichment: fixtureEnrichment(PlatformPi), BaseURL: "https://prism.example"})
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := RenderPi(PiInput{Facts: facts, Selection: []int{3, 5}, Enrichment: fixtureEnrichment(PlatformPi), BaseURL: "https://prism.example"})
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if first.Content != second.Content || first.ContentSHA256 != second.ContentSHA256 {
		t.Fatalf("renders must be byte-stable")
	}
	sum := sha256.Sum256([]byte(first.Content))
	if hex.EncodeToString(sum[:]) != first.ContentSHA256 {
		t.Fatalf("sha256 must match content bytes exactly")
	}
	if !strings.HasSuffix(first.Content, "\n") || strings.HasSuffix(first.Content, "\n\n") {
		t.Fatalf("deterministic JSON must end with exactly one newline")
	}
}

func verifyGolden(t *testing.T, name string, result *RenderResult) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("PRISM_UPDATE_MODELEXPORT_GOLDENS") != "" {
		if err := os.WriteFile(path, []byte(result.Content), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set PRISM_UPDATE_MODELEXPORT_GOLDENS=1 to regenerate)", name, err)
	}
	want := string(raw)
	if want != result.Content {
		t.Fatalf("golden drift in %s:\n--- got ---\n%s\n--- want ---\n%s", name, result.Content, want)
	}
	var parsed any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("generated content must stay valid JSON: %v", err)
	}
}

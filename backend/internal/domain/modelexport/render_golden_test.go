package modelexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

func fixtureFacts() SourceFacts {
	dualNative := "dual_native"
	chatOnly := "chat_completions_only"
	return SourceFacts{
		TargetVersion: PiTargetVersion,
		Models: []ModelFact{
			{
				ModelConfigID: 3, ModelID: "gpt-5.6-sol", APIFamily: "openai",
				IsEnabled: true, Selectable: true, OpenAIAcceptedFormat: &dualNative,
				PiSelected: &SelectedCoordinate{ProviderID: "openai", ModelID: "gpt-5.6-sol", API: piAPIResponses, CatalogRevision: "sha256-gpt"},
				PiTemplate: PiTemplate{
					Metadata: NewMetadataLayer(map[string]json.RawMessage{
						MetaContextWindow:   rawValue(1050000),
						MetaMaxOutputTokens: rawValue(128000),
						MetaModalitiesInput: rawValue([]string{"text", "image"}),
					}),
					DerivedFields: map[string]json.RawMessage{
						"thinkingLevelMap": rawValue(map[string]*string{
							"off": nil, "minimal": nil, "low": ptrString("low"), "medium": ptrString("medium"),
							"high": ptrString("high"), "xhigh": nil, "max": ptrString("max"),
						}),
					},
				},
				PrismMetadata: map[string]json.RawMessage{MetaReasoning: rawValue(true)},
				Targets: []TargetFact{{
					TerminalTargetID: 11, Position: 0, EndpointID: 21, EndpointName: "primary",
					Pricing: &TargetPriceSnapshot{
						Kind: pricingkind.Standard, Card: completeCardPtr(),
						CurrencyCode: "USD", PricingUnit: "PER_1M",
					},
				}},
			},
			{
				ModelConfigID: 5, ModelID: "claude-opus-4-8", APIFamily: "anthropic",
				IsEnabled: true, Selectable: true,
				PiSelected: &SelectedCoordinate{ProviderID: "anthropic", ModelID: "claude-opus-4-8", API: piAPIAnthropic, CatalogRevision: "sha256-claude"},
				PrismMetadata: map[string]json.RawMessage{
					MetaName: rawValue("Claude Opus 4.8"), MetaContextWindow: rawValue(200000),
				},
				Targets: []TargetFact{{
					TerminalTargetID: 12, Position: 0, EndpointID: 22, EndpointName: "primary",
					Pricing: &TargetPriceSnapshot{
						Kind: pricingkind.Tiered,
						BaseCard: &PriceCardSnapshot{
							InputPrice: "5", OutputPrice: "25", CachedInputPrice: ptrString("0.5"),
							CacheCreationPrice: ptrString("6.25"), ReasoningPrice: ptrString("25"),
						},
						AboveCard: &PriceCardSnapshot{
							InputPrice: "10", OutputPrice: "37.5", CachedInputPrice: ptrString("1"),
							CacheCreationPrice: ptrString("12.5"), ReasoningPrice: ptrString("37.5"),
						},
						TierThreshold: ptrInt(200000), CurrencyCode: "USD", PricingUnit: "PER_1M",
					},
				}},
			},
			{
				ModelConfigID: 8, ModelID: "gemini-4-pro", APIFamily: "gemini",
				IsEnabled: true, Selectable: true,
				PiSelected: &SelectedCoordinate{ProviderID: "google", ModelID: "gemini-4-pro", API: piAPIGemini, CatalogRevision: "sha256-gemini"},
				PiTemplate: PiTemplate{Metadata: NewMetadataLayer(map[string]json.RawMessage{
					MetaName: rawValue("Gemini 4 Pro"),
				})},
				PrismMetadata: map[string]json.RawMessage{},
				Targets: []TargetFact{{
					TerminalTargetID: 13, Position: 0, EndpointID: 23, EndpointName: "google",
					Pricing: &TargetPriceSnapshot{Kind: pricingkind.PeakValley, CurrencyCode: "USD", PricingUnit: "PER_1M"},
				}},
			},
			{
				ModelConfigID: 9, ModelID: "glm-5.2", APIFamily: "openai",
				IsEnabled: true, Selectable: true, OpenAIAcceptedFormat: &chatOnly,
				PiSelected: &SelectedCoordinate{ProviderID: "zai", ModelID: "glm-5.2", API: piAPIOpenAIChat, CatalogRevision: "sha256-glm"},
				PiTemplate: PiTemplate{DerivedFields: map[string]json.RawMessage{
					"compat": rawValue(map[string]any{
						"chatTemplateArgs": map[string]any{
							"enabled": map[string]any{"$var": "thinking.enabled", "omitWhenOff": true},
							"effort":  map[string]any{"$var": "thinking.effort"},
						},
					}),
				}},
				PrismMetadata: map[string]json.RawMessage{},
				Targets: []TargetFact{{
					TerminalTargetID: 14, Position: 0, EndpointID: 24, EndpointName: "zai",
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
		InputPrice: "3", OutputPrice: "15", CachedInputPrice: ptrString("0.3"),
		CacheCreationPrice: ptrString("3.75"), ReasoningPrice: ptrString("15"),
	}
}

func incompleteCardPtr() *PriceCardSnapshot {
	return &PriceCardSnapshot{InputPrice: "1", OutputPrice: "4"}
}

func TestRenderPiGolden(t *testing.T) {
	result, err := RenderPi(PiInput{
		Facts: fixtureFacts(), Selection: []int{3, 5, 8, 9}, BaseURL: "https://prism.example",
	})
	if err != nil {
		t.Fatalf("RenderPi: %v", err)
	}
	if len(result.ModelResults) != 4 {
		t.Fatalf("model results = %d, want one entry per selected model", len(result.ModelResults))
	}
	for index, modelConfigID := range []int{3, 5, 8, 9} {
		if result.ModelResults[index].ModelConfigID != modelConfigID {
			t.Fatalf("model result %d id = %d, want %d", index, result.ModelResults[index].ModelConfigID, modelConfigID)
		}
	}
	verifyGolden(t, "pi_models.golden.json", result)
}

func TestRenderPiWithEmbeddedKeyGolden(t *testing.T) {
	result, err := RenderPi(PiInput{
		Facts: fixtureFacts(), Selection: []int{3, 5, 8, 9}, BaseURL: "https://prism.example",
		IncludeAPIKey: true, APIKey: "proxy-key",
	})
	if err != nil {
		t.Fatalf("RenderPi keyed: %v", err)
	}
	verifyGolden(t, "pi_models_keyed.golden.json", result)
}

func TestRenderCredentialPresenceDistinguishesOmittedEmptyAndIncluded(t *testing.T) {
	facts := fixtureFacts()
	inputs := []struct {
		name       string
		include    bool
		key        string
		wantNeedle string
		wantKey    bool
	}{
		{name: "omitted", wantKey: false},
		{name: "included value", include: true, key: "proxy-key", wantNeedle: `"apiKey": "proxy-key"`, wantKey: true},
	}
	for _, test := range inputs {
		t.Run(test.name, func(t *testing.T) {
			result, err := RenderPi(PiInput{
				Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example",
				IncludeAPIKey: test.include, APIKey: test.key,
			})
			if err != nil {
				t.Fatalf("RenderPi: %v", err)
			}
			hasKey := strings.Contains(result.Content, `"apiKey"`)
			if hasKey != test.wantKey || (test.wantNeedle != "" && !strings.Contains(result.Content, test.wantNeedle)) {
				t.Fatalf("credential presence mismatch: %s", result.Content)
			}
		})
	}
	if _, err := RenderPi(PiInput{
		Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example", IncludeAPIKey: true,
	}); err == nil {
		t.Fatalf("an explicitly included empty key must be rejected because Pi 0.84.3 cannot load it")
	}
}

func TestPiDocumentSchemaRejectsEmptyProviderKey(t *testing.T) {
	document := map[string]any{"providers": map[string]any{"prism": map[string]any{
		"name": "Prism", "apiKey": "", "models": []any{map[string]any{
			"id": "m", "api": piAPIResponses,
		}},
	}}}
	if err := validatePiDocument(document); err == nil {
		t.Fatalf("Pi 0.84.3 provider schema must reject an empty apiKey")
	}
}

func TestPiAPIAndGatewayBaseURLMapping(t *testing.T) {
	chatOnly := "chat_completions_only"
	responsesOnly := "responses_only"
	tests := []struct {
		name     string
		family   string
		format   *string
		wantAPI  string
		wantBase string
	}{
		{name: "responses", family: "openai", format: &responsesOnly, wantAPI: piAPIResponses, wantBase: "https://prism.example/v1"},
		{name: "chat", family: "openai", format: &chatOnly, wantAPI: piAPIOpenAIChat, wantBase: "https://prism.example/v1"},
		{name: "anthropic", family: "anthropic", wantAPI: piAPIAnthropic, wantBase: "https://prism.example"},
		{name: "gemini", family: "gemini", wantAPI: piAPIGemini, wantBase: "https://prism.example/v1beta"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PiAPIForModel(test.family, test.format); got != test.wantAPI {
				t.Fatalf("api = %q, want %q", got, test.wantAPI)
			}
			if got := clientBaseURL("https://prism.example/", test.family); got != test.wantBase {
				t.Fatalf("base URL = %q, want %q", got, test.wantBase)
			}
		})
	}
}

func TestRenderUsesPersistedPiTemplateWithoutLiveCandidate(t *testing.T) {
	facts := fixtureFacts()
	facts.Models = facts.Models[:1]
	facts.Models[0].PiCandidates = []PiCandidate{{
		ProviderID: "different-live-provider", ModelID: "gpt-5.6-sol", API: piAPIResponses,
	}}
	result, err := RenderPi(PiInput{Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example"})
	if err != nil {
		t.Fatalf("a frozen binding must render independently of current live candidates: %v", err)
	}
	if !strings.Contains(result.Content, `"contextWindow": 1050000`) {
		t.Fatalf("render did not replay the persisted template: %s", result.Content)
	}
}

func TestRenderRejectsMissingOrIdentityDriftedPiBinding(t *testing.T) {
	facts := fixtureFacts()
	facts.Models = facts.Models[:1]
	facts.Models[0].PiSelected = nil
	_, err := RenderPi(PiInput{Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example"})
	var unselected *ErrCandidateUnselected
	if !errors.As(err, &unselected) {
		t.Fatalf("unbound render error = %v, want ErrCandidateUnselected", err)
	}

	facts = fixtureFacts()
	facts.Models = facts.Models[:1]
	facts.Models[0].PiSelected.ModelID = "old-id"
	_, err = RenderPi(PiInput{Facts: facts, Selection: []int{3}, BaseURL: "https://prism.example"})
	var invalid *ErrCandidateInvalid
	if !errors.As(err, &invalid) {
		t.Fatalf("drifted render error = %v, want ErrCandidateInvalid", err)
	}
}

func TestRenderResultsAreDeterministicAcrossRuns(t *testing.T) {
	input := PiInput{Facts: fixtureFacts(), Selection: []int{3, 5}, BaseURL: "https://prism.example"}
	first, err := RenderPi(input)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	second, err := RenderPi(input)
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
	if first.FileName != PiFileName || first.MIMEType != "application/json;charset=utf-8" {
		t.Fatalf("download metadata = %q %q", first.FileName, first.MIMEType)
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
		t.Fatalf("read golden %s: %v", name, err)
	}
	if want := string(raw); want != result.Content {
		t.Fatalf("golden drift in %s:\n--- got ---\n%s\n--- want ---\n%s", name, result.Content, want)
	}
	var parsed any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("generated content must stay valid JSON: %v", err)
	}
}

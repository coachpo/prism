package modelexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func openCodeFixture() OpenCodeSourceFacts {
	result := OpenCodeSourceFacts{TargetVersion: OpenCodeTargetVersion}
	for _, pi := range fixtureFacts().Models {
		result.Models = append(result.Models, OpenCodeModelFact{
			ModelConfigID: pi.ModelConfigID, ModelID: pi.ModelID, APIFamily: pi.APIFamily,
			OpenAIAcceptedFormat: pi.OpenAIAcceptedFormat, IsEnabled: true, Selectable: true,
			SourceMetadata: map[string]json.RawMessage{"name": rawValue("Catalog " + pi.ModelID), "limit_context": rawValue(200000), "limit_output": rawValue(8192), "reasoning": rawValue(true), "tool_call": rawValue(true)},
			Targets:        pi.Targets,
		})
	}
	result.Models[0].ModelID = "organization/gpt-5.6-sol"
	result.Models[0].DisplayName = ptrString("Prism Chat")
	result.Models[0].OverrideMetadata = map[string]json.RawMessage{"reasoning": rawValue(false), "limit_input": rawValue(0), "modalities_input": rawValue([]string{"text", "image"})}
	return result
}

func openCodeInput() OpenCodeInput {
	return OpenCodeInput{Facts: openCodeFixture(), Selection: []int{3, 5, 8, 9}, BaseURL: "https://prism.example"}
}

func TestOpenCodeNativeSDKMapping(t *testing.T) {
	tests := []struct{ family, mode, sdk, path string }{
		{"openai", "chat_completions_only", "@ai-sdk/openai-compatible", "/v1"},
		{"openai", "responses_only", "@ai-sdk/openai", "/v1"},
		{"openai", "dual_native", "@ai-sdk/openai", "/v1"},
		{"anthropic", "", "@ai-sdk/anthropic", "/v1"},
		{"gemini", "", "@ai-sdk/google", "/v1beta"},
	}
	for _, test := range tests {
		if OpenCodeSDKForModel(test.family, &test.mode) != test.sdk || OpenCodeAPIPathForModel(test.family) != test.path {
			t.Fatalf("incorrect native SDK mapping for %s/%s", test.family, test.mode)
		}
	}
}

func TestRenderOpenCodeGolden(t *testing.T) {
	input := openCodeInput()
	result, err := RenderOpenCode(input)
	if err != nil {
		t.Fatal(err)
	}
	verifyGolden(t, "opencode_prism.golden.json", result)
	input.Selection = []int{9, 8, 5, 3, 3, 9}
	reordered, err := RenderOpenCode(input)
	if err != nil || result.Content != reordered.Content {
		t.Fatalf("selection order/deduplication changed bytes: %v", err)
	}
	sum := sha256.Sum256([]byte(result.Content))
	if result.ContentSHA256 != hex.EncodeToString(sum[:]) || result.FileName != OpenCodeFileName || result.MIMEType != "application/json;charset=utf-8" || !strings.HasSuffix(result.Content, "\n") || strings.HasSuffix(result.Content, "\n\n") {
		t.Fatal("download bytes and metadata disagree")
	}
}

func TestRenderOpenCodeFinalCredentialAndLiteralSubstitutions(t *testing.T) {
	input := openCodeInput()
	input.ProviderID, input.IncludeAPIKey, input.APIKey = "prism-home_2", true, "  literal-{env:KEY}-{file:absent}  "
	input.Facts.Models[0].ModelID = "org/{env:MODEL}"
	input.Facts.Models[0].DisplayName = ptrString("Literal {file:absent}")
	result, err := RenderOpenCode(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "{env:") || strings.Contains(result.Content, "{file:") {
		t.Fatal("raw config substitution would change a literal value")
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(result.Content), &document); err != nil {
		t.Fatal(err)
	}
	provider := document["provider"].(map[string]any)[input.ProviderID].(map[string]any)
	if provider["options"].(map[string]any)["apiKey"] != strings.TrimSpace(input.APIKey) || provider["env"] != nil {
		t.Fatal("final credentials changed or env mode leaked")
	}
	if provider["models"].(map[string]any)[input.Facts.Models[0].ModelID].(map[string]any)["name"] != *input.Facts.Models[0].DisplayName {
		t.Fatal("literal model identity or metadata changed")
	}
}

func TestRenderOpenCodeRejectsInvalidTargetAndSelection(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*OpenCodeInput)
	}{
		{"builtin provider", func(i *OpenCodeInput) { i.ProviderID = "openai" }},
		{"provider slash", func(i *OpenCodeInput) { i.ProviderID = "prism/a" }},
		{"empty suffix", func(i *OpenCodeInput) { i.ProviderID = "prism-" }},
		{"blank included key", func(i *OpenCodeInput) { i.IncludeAPIKey = true; i.APIKey = " \n " }},
		{"key without mode", func(i *OpenCodeInput) { i.APIKey = "unexpected" }},
		{"origin credentials", func(i *OpenCodeInput) { i.BaseURL = "https://key@prism.example" }},
		{"origin query", func(i *OpenCodeInput) { i.BaseURL = "https://prism.example?key=a" }},
		{"origin fragment", func(i *OpenCodeInput) { i.BaseURL = "https://prism.example#key" }},
		{"origin path", func(i *OpenCodeInput) { i.BaseURL = "https://prism.example/v1" }},
		{"origin scheme", func(i *OpenCodeInput) { i.BaseURL = "file:///tmp/config" }},
		{"version", func(i *OpenCodeInput) { i.Facts.TargetVersion = "dev" }},
		{"empty selection", func(i *OpenCodeInput) { i.Selection = nil }},
		{"unknown selection", func(i *OpenCodeInput) { i.Selection = []int{999} }},
		{"disabled", func(i *OpenCodeInput) { i.Facts.Models[0].IsEnabled = false }},
		{"unroutable", func(i *OpenCodeInput) { i.Facts.Models[0].Selectable = false }},
		{"missing native mode", func(i *OpenCodeInput) { i.Facts.Models[0].OpenAIAcceptedFormat = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := openCodeInput()
			test.mutate(&input)
			if _, err := RenderOpenCode(input); err == nil {
				t.Fatal("invalid target or selection rendered")
			}
		})
	}
}

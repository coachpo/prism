package pidev

import (
	"reflect"
	"testing"
)

// These allowlists are pinned to Pi 0.84.3's API-specific compat interfaces
// in packages/ai/src/types.ts and the models.json loader schema in
// packages/coding-agent/src/core/model-config.ts. Provider routing and
// server-side fallback fields are deliberately narrower than Pi itself.
func TestAllowedCompatFieldsAreAPISpecificAndStable(t *testing.T) {
	tests := map[string][]string{
		APIOpenAICompletions: {
			"cacheControlFormat", "chatTemplateArgs", "chatTemplateKwargs", "deferredToolsMode",
			"maxTokensField", "requiresAssistantAfterToolResult", "requiresReasoningContentOnAssistantMessages",
			"requiresThinkingAsText", "requiresToolResultName", "sendSessionAffinityHeaders",
			"sessionAffinityFormat", "supportsDeveloperRole", "supportsFinishReason",
			"supportsLongCacheRetention", "supportsOpenAIGrammarTools", "supportsReasoningEffort",
			"supportsStore", "supportsStrictMode", "supportsThinkingTokenBudget",
			"supportsUsageInStreaming", "thinkingFormat", "thinkingTokenBudgetField", "zaiToolStream",
		},
		APIOpenAIResponses: {
			"sessionAffinityFormat", "supportsAdditionalTools", "supportsDeveloperRole",
			"supportsExplicitPromptCacheMode", "supportsLongCacheRetention",
			"supportsOpenAIGrammarTools", "supportsStrictMode", "supportsToolSearch",
		},
		APIAnthropicMessages: {
			"allowEmptySignature", "forceAdaptiveThinking", "sendSessionAffinityHeaders",
			"supportsCacheControlOnTools", "supportsEagerToolInputStreaming", "supportsLongCacheRetention",
			"supportsStrictTools", "supportsTemperature", "supportsToolReferences",
		},
		APIGoogleGenerative: {},
	}
	for api, want := range tests {
		if got := AllowedCompatFields(api); !reflect.DeepEqual(got, want) {
			t.Fatalf("AllowedCompatFields(%q) = %#v, want %#v", api, got, want)
		}
	}
}

func TestParseCatalogSanitizesCompatAndReportsDroppedPaths(t *testing.T) {
	body := `{
		"chat": {"m": {
			"id":"m", "api":"openai-completions", "provider":"chat",
			"compat": {
				"supportsStore": false,
				"zaiToolStream": true,
				"supportsExplicitPromptCacheMode": true,
				"openRouterRouting": {"only":["provider-a"]},
				"futureField": true
			},
			"headers": {"x-routing-key":"unsafe"},
			"samplingParams": {"temperature":0.1}
		}}
	}`
	providers, err := parseCatalog([]byte(body))
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	model := providers["chat"].Models["m"]
	wantCompat := map[string]any{"supportsStore": false, "zaiToolStream": true}
	if !reflect.DeepEqual(model.Compat, wantCompat) {
		t.Fatalf("Compat = %#v, want %#v", model.Compat, wantCompat)
	}
	wantDropped := []string{
		"compat.futureField",
		"compat.openRouterRouting",
		"compat.supportsExplicitPromptCacheMode",
		"headers",
		"samplingParams",
	}
	if !reflect.DeepEqual(model.DroppedFields, wantDropped) {
		t.Fatalf("DroppedFields = %#v, want %#v", model.DroppedFields, wantDropped)
	}
}

func TestSanitizeCompatUsesConcreteModelAPI(t *testing.T) {
	raw := map[string]any{
		"supportsDeveloperRole":           true,
		"supportsExplicitPromptCacheMode": true,
		"zaiToolStream":                   true,
	}
	clean, dropped, err := SanitizeCompat(APIOpenAIResponses, raw)
	if err != nil {
		t.Fatalf("SanitizeCompat: %v", err)
	}
	if _, ok := clean["supportsExplicitPromptCacheMode"]; !ok {
		t.Fatalf("Responses-safe field was dropped: %#v", clean)
	}
	if !reflect.DeepEqual(dropped, []string{"compat.zaiToolStream"}) {
		t.Fatalf("dropped = %#v", dropped)
	}

	clean, dropped, err = SanitizeCompat(APIAnthropicMessages, map[string]any{
		"supportsTemperature":   false,
		"allowedFallbackModels": []any{map[string]any{"provider": "other", "model": "m"}},
	})
	if err != nil {
		t.Fatalf("SanitizeCompat anthropic: %v", err)
	}
	if !reflect.DeepEqual(clean, map[string]any{"supportsTemperature": false}) ||
		!reflect.DeepEqual(dropped, []string{"compat.allowedFallbackModels"}) {
		t.Fatalf("anthropic clean=%#v dropped=%#v", clean, dropped)
	}
}

func TestValidateCompatRejectsRoutingFallbackAndCrossAPIFields(t *testing.T) {
	for name, test := range map[string]struct {
		api   string
		value map[string]any
	}{
		"routing":   {APIOpenAICompletions, map[string]any{"openRouterRouting": map[string]any{}}},
		"fallback":  {APIAnthropicMessages, map[string]any{"allowedFallbackModels": []any{}}},
		"cross api": {APIOpenAIResponses, map[string]any{"zaiToolStream": true}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCompat(test.api, test.value); err == nil {
				t.Fatalf("ValidateCompat(%q, %#v) must reject", test.api, test.value)
			}
		})
	}
}

func TestSanitizeCompatFailsClosedOnInvalidAllowedValue(t *testing.T) {
	if _, _, err := SanitizeCompat(APIOpenAICompletions, map[string]any{"supportsStore": "false"}); err == nil {
		t.Fatalf("an allowed key with the wrong type must fail catalog validation")
	}
}

func TestValidateCompatPinsPiThinkingVariables(t *testing.T) {
	for _, variable := range []string{"thinking.enabled", "thinking.effort", "thinking.budget"} {
		if err := ValidateCompat(APIOpenAICompletions, map[string]any{
			"chatTemplateArgs": map[string]any{
				"thinking": map[string]any{"$var": variable},
			},
		}); err != nil {
			t.Fatalf("Pi 0.84.3 variable %q was rejected: %v", variable, err)
		}
	}
}

func TestSanitizeCompatRejectsNestedCredentialShapedKeys(t *testing.T) {
	_, _, err := SanitizeCompat(APIOpenAICompletions, map[string]any{
		"chatTemplateKwargs": map[string]any{"api_key": "must-not-persist"},
	})
	if err == nil {
		t.Fatalf("credential-shaped compat key must fail before persistence")
	}
}

package providercompat

import "testing"

func TestOpenAITextCapabilityHelpers(t *testing.T) {
	tests := []struct {
		name              string
		capability        string
		ingressOperation  string
		wantSupported     bool
		wantNative        bool
		wantMode          string
		wantModeSupported bool
	}{
		{name: "responses only supports responses native", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationResponses, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "chat only supports chat native", capability: OpenAITextCapabilityChatCompletionsOnly, ingressOperation: OpenAIUpstreamOperationChatCompletions, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "responses only translates chat ingress", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationChatCompletions, wantSupported: true, wantMode: OpenAITextTranslationModeChatToResponses, wantModeSupported: true},
		{name: "chat only translates responses ingress", capability: OpenAITextCapabilityChatCompletionsOnly, ingressOperation: OpenAIUpstreamOperationResponses, wantSupported: true, wantMode: OpenAITextTranslationModeResponsesToChat, wantModeSupported: true},
		{name: "dual native keeps chat native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationChatCompletions, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "dual native keeps responses native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationResponses, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "responses adjunct input tokens stays native on responses only", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationResponsesInputTokens, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "responses adjunct input tokens stays native on dual native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationResponsesInputTokens, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "responses adjunct input tokens rejects chat only", capability: OpenAITextCapabilityChatCompletionsOnly, ingressOperation: OpenAIUpstreamOperationResponsesInputTokens, wantSupported: true, wantMode: "", wantModeSupported: false},
		{name: "responses adjunct stays native on responses only", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationResponsesCompact, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "responses adjunct compact stays native on dual native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationResponsesCompact, wantSupported: true, wantNative: true, wantMode: OpenAITextTranslationModeNone, wantModeSupported: true},
		{name: "responses adjunct requires responses support", capability: OpenAITextCapabilityChatCompletionsOnly, ingressOperation: OpenAIUpstreamOperationResponsesCompact, wantSupported: true},
		{name: "invalid capability unsupported", capability: "unknown", ingressOperation: OpenAIUpstreamOperationResponses},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsSupportedOpenAITextCapability(test.capability); got != test.wantSupported {
				t.Fatalf("expected supported=%v, got %v", test.wantSupported, got)
			}
			if got := OpenAITextCapabilitySupportsNativeOperation(test.capability, test.ingressOperation); got != test.wantNative {
				t.Fatalf("expected native=%v, got %v", test.wantNative, got)
			}
			mode, ok := OpenAITextSiblingTranslationMode(test.capability, test.ingressOperation)
			if ok != test.wantModeSupported || mode != test.wantMode {
				t.Fatalf("expected mode=%q supported=%v, got mode=%q supported=%v", test.wantMode, test.wantModeSupported, mode, ok)
			}
		})
	}
}

func TestResolveAuthProfileAndControlledHeaders(t *testing.T) {
	authType := " anthropic "
	profile, err := ResolveAuthProfile(&authType, APIFamilyOpenAI)
	if err != nil {
		t.Fatalf("resolve auth profile: %v", err)
	}
	if profile.AuthHeader != "x-api-key" || profile.AuthPrefix != "" || profile.ExtraHeaders["anthropic-version"] != "2023-06-01" {
		t.Fatalf("unexpected auth profile: %+v", profile)
	}

	profile.ExtraHeaders["anthropic-version"] = "mutated"
	freshAuthType := "anthropic"
	fresh, err := ResolveAuthProfile(&freshAuthType, APIFamilyOpenAI)
	if err != nil {
		t.Fatalf("resolve fresh auth profile: %v", err)
	}
	if fresh.ExtraHeaders["anthropic-version"] != "2023-06-01" {
		t.Fatalf("expected auth profile extra headers to be cloned, got %+v", fresh.ExtraHeaders)
	}
	controlled := fresh.ControlledHeaderNames()
	if _, ok := controlled["x-api-key"]; !ok {
		t.Fatalf("expected auth header to be controlled, got %+v", controlled)
	}
	if _, ok := controlled["anthropic-version"]; !ok {
		t.Fatalf("expected extra header to be controlled, got %+v", controlled)
	}
	unknownAuthType := "unknown"
	if _, err := ResolveAuthProfile(&unknownAuthType, APIFamilyOpenAI); err == nil || err.Error() != "unsupported auth_type: unknown" {
		t.Fatalf("expected unsupported auth_type error, got %v", err)
	}
}

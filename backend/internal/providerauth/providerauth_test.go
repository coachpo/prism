package providerauth

import "testing"

func TestOpenAITextModesStrictEquality(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "dual native equals itself", left: OpenAITextCapabilityDualNative, right: OpenAITextCapabilityDualNative, want: true},
		{name: "chat only equals itself", left: OpenAITextCapabilityChatCompletionsOnly, right: OpenAITextCapabilityChatCompletionsOnly, want: true},
		{name: "responses only equals itself", left: OpenAITextCapabilityResponsesOnly, right: OpenAITextCapabilityResponsesOnly, want: true},
		{name: "dual native never equals chat only", left: OpenAITextCapabilityDualNative, right: OpenAITextCapabilityChatCompletionsOnly, want: false},
		{name: "dual native never equals responses only", left: OpenAITextCapabilityDualNative, right: OpenAITextCapabilityResponsesOnly, want: false},
		{name: "chat only never equals responses only", left: OpenAITextCapabilityChatCompletionsOnly, right: OpenAITextCapabilityResponsesOnly, want: false},
		{name: "unknown mode never equals", left: "legacy", right: "legacy", want: false},
		{name: "empty never equals", left: "", right: OpenAITextCapabilityDualNative, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := OpenAITextModesEqual(test.left, test.right); got != test.want {
				t.Fatalf("expected OpenAITextModesEqual(%q, %q) = %v, got %v", test.left, test.right, test.want, got)
			}
		})
	}
}

func TestOpenAITextModesMatchPointers(t *testing.T) {
	dual := OpenAITextCapabilityDualNative
	responses := OpenAITextCapabilityResponsesOnly
	tests := []struct {
		name  string
		left  *string
		right *string
		want  bool
	}{
		{name: "both nil match", left: nil, right: nil, want: true},
		{name: "one-sided nil never matches", left: &dual, right: nil, want: false},
		{name: "one-sided nil never matches reversed", left: nil, right: &dual, want: false},
		{name: "equal values match", left: &dual, right: &dual, want: true},
		{name: "different modes never match", left: &dual, right: &responses, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := OpenAITextModesMatch(test.left, test.right); got != test.want {
				t.Fatalf("expected OpenAITextModesMatch = %v, got %v", test.want, got)
			}
		})
	}
}

func TestOpenAITextCapabilityHelpers(t *testing.T) {
	tests := []struct {
		name             string
		capability       string
		ingressOperation string
		wantSupported    bool
		wantNative       bool
	}{
		{name: "responses only supports responses native", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationResponses, wantSupported: true, wantNative: true},
		{name: "chat only supports chat native", capability: OpenAITextCapabilityChatCompletionsOnly, ingressOperation: OpenAIUpstreamOperationChatCompletions, wantSupported: true, wantNative: true},
		{name: "responses only rejects chat ingress", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationChatCompletions, wantSupported: true},
		{name: "chat only rejects responses ingress", capability: OpenAITextCapabilityChatCompletionsOnly, ingressOperation: OpenAIUpstreamOperationResponses, wantSupported: true},
		{name: "dual native keeps chat native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationChatCompletions, wantSupported: true, wantNative: true},
		{name: "dual native keeps responses native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationResponses, wantSupported: true, wantNative: true},
		{name: "responses adjunct input tokens stays native on responses only", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationResponsesInputTokens, wantSupported: true, wantNative: true},
		{name: "responses adjunct input tokens stays native on dual native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationResponsesInputTokens, wantSupported: true, wantNative: true},
		{name: "responses adjunct input tokens rejects chat only", capability: OpenAITextCapabilityChatCompletionsOnly, ingressOperation: OpenAIUpstreamOperationResponsesInputTokens, wantSupported: true},
		{name: "responses adjunct stays native on responses only", capability: OpenAITextCapabilityResponsesOnly, ingressOperation: OpenAIUpstreamOperationResponsesCompact, wantSupported: true, wantNative: true},
		{name: "responses adjunct compact stays native on dual native", capability: OpenAITextCapabilityDualNative, ingressOperation: OpenAIUpstreamOperationResponsesCompact, wantSupported: true, wantNative: true},
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

package providercompat

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeConnectionOpenAIProbeEndpointVariant(t *testing.T) {
	got, err := NormalizeConnectionOpenAIProbeEndpointVariant(APIFamilyOpenAI, nil)
	if err != nil || got == nil || *got != DefaultOpenAIProbeEndpointVariant {
		t.Fatalf("expected default OpenAI probe variant, got value=%#v err=%v", got, err)
	}

	chatVariant := " chat_completions_minimal "
	got, err = NormalizeConnectionOpenAIProbeEndpointVariant(APIFamilyOpenAI, &chatVariant)
	if err != nil || got == nil || *got != OpenAIProbeEndpointVariantChatCompletionsMinimal {
		t.Fatalf("expected trimmed chat probe variant, got value=%#v err=%v", got, err)
	}

	upperVariant := "RESPONSES_MINIMAL"
	if _, err := NormalizeConnectionOpenAIProbeEndpointVariant(APIFamilyOpenAI, &upperVariant); !errors.Is(err, ErrOpenAIProbeEndpointVariantInvalid) {
		t.Fatalf("expected case-sensitive management variant rejection, got %v", err)
	}

	unsupportedVariant := "responses_minimal"
	if _, err := NormalizeConnectionOpenAIProbeEndpointVariant(APIFamilyGemini, &unsupportedVariant); !errors.Is(err, ErrOpenAIProbeEndpointVariantUnsupported) {
		t.Fatalf("expected non-OpenAI management variant rejection, got %v", err)
	}

	blankVariant := " \t "
	got, err = NormalizeConnectionOpenAIProbeEndpointVariant(APIFamilyAnthropic, &blankVariant)
	if err != nil || got != nil {
		t.Fatalf("expected blank non-OpenAI management variant to be ignored, got value=%#v err=%v", got, err)
	}
}

func TestNormalizeImportedOpenAIProbeEndpointVariant(t *testing.T) {
	got, err := NormalizeImportedOpenAIProbeEndpointVariant(APIFamilyOpenAI, nil)
	if err != nil || got != nil {
		t.Fatalf("expected nil imported variant to stay nil, got value=%#v err=%v", got, err)
	}

	upperVariant := " RESPONSES_MINIMAL "
	got, err = NormalizeImportedOpenAIProbeEndpointVariant(APIFamilyOpenAI, &upperVariant)
	if err != nil || got == nil || *got != OpenAIProbeEndpointVariantResponsesMinimal {
		t.Fatalf("expected case-insensitive imported variant normalization, got value=%#v err=%v", got, err)
	}

	blankVariant := " "
	if _, err := NormalizeImportedOpenAIProbeEndpointVariant(APIFamilyOpenAI, &blankVariant); !errors.Is(err, ErrOpenAIProbeEndpointVariantInvalid) {
		t.Fatalf("expected blank imported OpenAI variant rejection, got %v", err)
	}
	if _, err := NormalizeImportedOpenAIProbeEndpointVariant(APIFamilyGemini, &blankVariant); !errors.Is(err, ErrOpenAIProbeEndpointVariantUnsupported) {
		t.Fatalf("expected provided non-OpenAI imported variant rejection, got %v", err)
	}
}

func TestDeriveOpenAIUpstreamOperation(t *testing.T) {
	tests := []struct {
		name       string
		apiFamily  string
		variant    string
		useVariant bool
		want       string
		wantNil    bool
	}{
		{name: "nil defaults to responses", apiFamily: APIFamilyOpenAI, want: OpenAIUpstreamOperationResponses},
		{name: "blank defaults to responses", apiFamily: APIFamilyOpenAI, variant: " \t", useVariant: true, want: OpenAIUpstreamOperationResponses},
		{name: "chat variant maps to chat", apiFamily: " OpenAI ", variant: OpenAIProbeEndpointVariantChatCompletionsReasoningNone, useVariant: true, want: OpenAIUpstreamOperationChatCompletions},
		{name: "unknown openai variant falls back to responses", apiFamily: APIFamilyOpenAI, variant: "unknown", useVariant: true, want: OpenAIUpstreamOperationResponses},
		{name: "non openai has no upstream operation", apiFamily: APIFamilyAnthropic, variant: OpenAIProbeEndpointVariantChatCompletionsMinimal, useVariant: true, wantNil: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var variant *string
			if test.useVariant {
				variant = &test.variant
			}
			got := DeriveOpenAIUpstreamOperation(test.apiFamily, variant)
			if test.wantNil {
				if got != nil {
					t.Fatalf("expected nil upstream operation, got %q", *got)
				}
				return
			}
			if got == nil || *got != test.want {
				t.Fatalf("expected upstream operation %q, got %+v", test.want, got)
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

func TestBuildHealthProbeRequest(t *testing.T) {
	responsesVariant := OpenAIProbeEndpointVariantResponsesReasoningNone
	request, err := BuildHealthProbeRequest(APIFamilyOpenAI, "gpt-test", &responsesVariant)
	if err != nil {
		t.Fatalf("build OpenAI health probe: %v", err)
	}
	if request.Path != "/v1/responses" {
		t.Fatalf("expected OpenAI responses probe path, got %q", request.Path)
	}
	if request.Body["model"] != "gpt-test" || request.Body["max_output_tokens"] != 1 {
		t.Fatalf("unexpected OpenAI responses body: %+v", request.Body)
	}
	if !reflect.DeepEqual(request.Body["reasoning"], map[string]any{"effort": "none"}) {
		t.Fatalf("expected reasoning none body, got %+v", request.Body)
	}

	chatVariant := OpenAIProbeEndpointVariantChatCompletionsReasoningNone
	request, err = BuildHealthProbeRequest(APIFamilyOpenAI, "gpt-chat", &chatVariant)
	if err != nil {
		t.Fatalf("build OpenAI chat health probe: %v", err)
	}
	if request.Path != "/v1/chat/completions" || request.Body["reasoning_effort"] != "none" {
		t.Fatalf("unexpected OpenAI chat probe request: %+v", request)
	}

	request, err = BuildHealthProbeRequest(APIFamilyAnthropic, "claude", nil)
	if err != nil || request.Path != "/v1/messages" || request.Body["max_tokens"] != 1 {
		t.Fatalf("unexpected Anthropic probe request: %+v err=%v", request, err)
	}

	request, err = BuildHealthProbeRequest(APIFamilyGemini, "gemini-pro", nil)
	if err != nil || request.Path != "/v1beta/models/gemini-pro:generateContent" {
		t.Fatalf("unexpected Gemini probe request: %+v err=%v", request, err)
	}

	if _, err = BuildHealthProbeRequest("unknown", "model", nil); err == nil || err.Error() != `unsupported api_family "unknown" for health check` {
		t.Fatalf("expected unsupported api_family error, got %v", err)
	}
}

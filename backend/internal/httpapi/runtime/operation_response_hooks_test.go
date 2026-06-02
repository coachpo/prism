package runtime

import (
	"bytes"
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNonStreamResponseHooksByOperation(t *testing.T) {
	tests := []struct {
		name         string
		requestPath  string
		payload      string
		wantProvider string
		wantKind     operationResponseKind
		wantUsage    responseUsage
	}{
		{
			name:         "openai chat normalizes nested cached and reasoning usage",
			requestPath:  "/v1/chat/completions",
			payload:      `{"id":"chatcmpl-hook","usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`,
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantUsage:    generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
		},
		{
			name:         "openai responses normalizes nested cached and reasoning usage",
			requestPath:  "/v1/responses",
			payload:      `{"response":{"id":"resp-hook","usage":{"input_tokens":9,"output_tokens":7,"total_tokens":16,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":5}}}}`,
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantUsage:    generationResponseHookTestUsageWithCacheAndReasoning(7, 2, 16, 2, 5),
		},
		{
			name:         "anthropic count tokens uses token-count parser",
			requestPath:  "/v1/messages/count_tokens",
			payload:      `{"input_tokens":23,"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}`,
			wantProvider: "anthropic",
			wantKind:     operationResponseKindTokenCount,
			wantUsage:    tokenCountResponseHookTestUsage(23),
		},
		{
			name:         "gemini count tokens uses token-count parser",
			requestPath:  "/v1beta/models/gemini-2.5-pro:countTokens",
			payload:      `{"totalTokens":34,"usageMetadata":{"promptTokenCount":999,"candidatesTokenCount":999,"totalTokenCount":1998}}`,
			wantProvider: "gemini",
			wantKind:     operationResponseKindTokenCount,
			wantUsage:    tokenCountResponseHookTestUsage(34),
		},
		{
			name:         "openai image generation reserves media seam without text usage",
			requestPath:  "/v1/images/generations",
			payload:      `{"created":1700000000,"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998},"data":[{"url":"https://example.test/image.png"}]}`,
			wantProvider: "openai",
			wantKind:     operationResponseKindMedia,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			hooks, ok := ResponseHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected response hooks for %s", operation.Name)
			}
			if hooks.Provider != test.wantProvider || hooks.Kind != test.wantKind {
				t.Fatalf("expected %s/%s hooks, got %+v", test.wantProvider, test.wantKind, hooks)
			}
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(test.payload), "application/json", fixedResponseHookTestNow, false)
			if err != nil {
				t.Fatalf("capture non-stream response: %v", err)
			}
			if forwarded.String() != test.payload {
				t.Fatalf("expected response body to pass through unchanged, got %q", forwarded.String())
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected extracted usage %+v, got %+v", test.wantUsage, got)
			}
		})
	}
}

func TestGeminiGenerateContentNormalizesUsageMetadataDisjointSplits(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent").Operation
	payload := `{"responseId":"gemini-hook","usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":17,"totalTokenCount":99,"cachedContentTokenCount":4,"thoughtsTokenCount":6}}`
	var forwarded bytes.Buffer
	capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(payload), "application/json", fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("capture Gemini non-stream usage: %v", err)
	}
	if forwarded.String() != payload {
		t.Fatalf("expected response body to pass through unchanged, got %q", forwarded.String())
	}
	wantUsage := responseUsage{InputTokens: intPtr(7), OutputTokens: intPtr(11), TotalTokens: intPtr(99), CacheReadInputTokens: intPtr(4), ReasoningTokens: intPtr(6)}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected Gemini disjoint usage with provider total: want %+v got %+v", wantUsage, got)
	}
}

func TestMediaResponsesDoNotCaptureUsagePayloads(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
	}{
		{name: "image generation", requestPath: "/v1/images/generations"},
		{name: "image edit", requestPath: "/v1/images/edits"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			payload := `{"created":1700000000,"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998,"prompt_tokens_details":{"cached_tokens":777},"completion_tokens_details":{"reasoning_tokens":666}},"usageMetadata":{"promptTokenCount":999,"candidatesTokenCount":999,"totalTokenCount":1998,"cachedContentTokenCount":777,"thoughtsTokenCount":666},"data":[{"url":"https://example.test/image.png"}]}`
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(payload), "application/json", fixedResponseHookTestNow, false)
			if err != nil {
				t.Fatalf("capture media response: %v", err)
			}
			if forwarded.String() != payload {
				t.Fatalf("expected media body to pass through unchanged, got %q", forwarded.String())
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, responseUsage{}) {
				t.Fatalf("expected media response to capture no usage, got %+v", got)
			}
		})
	}
}

func TestAnthropicMessagesNonStreamUsagePreservesCacheSplits(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/messages").Operation
	payload := `{"id":"msg-anthropic-usage","type":"message","content":[{"type":"thinking","thinking":"do not synthesize reasoning"}],"usage":{"input_tokens":7,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":13,"output_tokens_details":{"reasoning_tokens":99}}}`
	var forwarded bytes.Buffer
	capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(payload), "application/json", fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("capture Anthropic non-stream usage: %v", err)
	}
	if forwarded.String() != payload {
		t.Fatalf("expected response body to pass through unchanged, got %q", forwarded.String())
	}
	wantUsage := responseUsage{InputTokens: intPtr(7), OutputTokens: intPtr(13), TotalTokens: intPtr(25), CacheReadInputTokens: intPtr(2), CacheCreationInputTokens: intPtr(3)}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected Anthropic cache split usage without reasoning synthesis: want %+v got %+v", wantUsage, got)
	}
}

func TestCountTokensResponsesDoNotUseGenerationUsage(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		payload     string
		wantUsage   responseUsage
	}{
		{
			name:        "anthropic count ignores generation usage object",
			requestPath: "/v1/messages/count_tokens",
			payload:     `{"input_tokens":11,"usage":{"prompt_tokens":101,"completion_tokens":202,"total_tokens":303,"completion_tokens_details":{"reasoning_tokens":404}}}`,
			wantUsage:   tokenCountResponseHookTestUsage(11),
		},
		{
			name:        "gemini count ignores generation usage metadata",
			requestPath: "/v1beta/models/gemini-2.5-pro:countTokens",
			payload:     `{"totalTokens":17,"usageMetadata":{"promptTokenCount":101,"candidatesTokenCount":202,"totalTokenCount":303,"cachedContentTokenCount":404,"thoughtsTokenCount":505}}`,
			wantUsage:   tokenCountResponseHookTestUsage(17),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(test.payload), "application/json", fixedResponseHookTestNow, false)
			if err != nil {
				t.Fatalf("capture token-count response: %v", err)
			}
			if forwarded.String() != test.payload {
				t.Fatalf("expected count-token body to pass through unchanged, got %q", forwarded.String())
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected count-only usage %+v, got %+v", test.wantUsage, got)
			}
		})
	}
}

func TestRuntimeUsageNormalizationKeepsProviderTotalPrecedence(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	payload := `{"id":"chatcmpl-total-precedence","usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":99}}`
	var forwarded bytes.Buffer
	capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(payload), "application/json", fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("capture provider-total usage: %v", err)
	}
	want := generationResponseHookTestUsage(7, 13, 99)
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected provider total to win without recomputation: want %+v got %+v", want, got)
	}
}

func TestRuntimeUsageNormalizationDerivesMissingTotalOnlyWhenAllowed(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/messages").Operation
	payload := `{"id":"msg-derived-total","usage":{"input_tokens":7,"output_tokens":13}}`
	var forwarded bytes.Buffer
	capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(payload), "application/json", fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("capture derived-total usage: %v", err)
	}
	want := generationResponseHookTestUsage(7, 13, 20)
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected missing total to derive from base input/output: want %+v got %+v", want, got)
	}
}

func TestRuntimeInvalidUsageDiscard(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		payload     string
	}{
		{
			name:        "negative provider token count",
			requestPath: "/v1/chat/completions",
			payload:     `{"id":"chatcmpl-negative","usage":{"prompt_tokens":7,"completion_tokens":-1,"total_tokens":6}}`,
		},
		{
			name:        "provider total underflows parent input and output",
			requestPath: "/v1/chat/completions",
			payload:     `{"id":"chatcmpl-underflow","usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":19}}`,
		},
		{
			name:        "cache split underflows input parent",
			requestPath: "/v1/chat/completions",
			payload:     `{"id":"chatcmpl-cache-underflow","usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20,"prompt_tokens_details":{"cached_tokens":8}}}`,
		},
		{
			name:        "reasoning split underflows output parent",
			requestPath: "/v1/responses",
			payload:     `{"response":{"id":"resp-reasoning-underflow","usage":{"input_tokens":7,"output_tokens":13,"total_tokens":20,"output_tokens_details":{"reasoning_tokens":14}}}}`,
		},
		{
			name:        "gemini total underflows prompt and candidate counts",
			requestPath: "/v1beta/models/gemini-2.5-pro:generateContent",
			payload:     `{"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":19}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(test.payload), "application/json", fixedResponseHookTestNow, false)
			if err != nil {
				t.Fatalf("capture invalid usage: %v", err)
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, responseUsage{}) {
				t.Fatalf("expected invalid usage to be discarded, got %+v", got)
			}
		})
	}
}

func TestGeminiUsageMetadataInvalidSplitsDiscard(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "cache read exceeds prompt parent", payload: `{"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":20,"cachedContentTokenCount":8}}`},
		{name: "thoughts exceed candidate parent", payload: `{"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":20,"thoughtsTokenCount":14}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent").Operation
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, TranslationModeNone, &forwarded, strings.NewReader(test.payload), "application/json", fixedResponseHookTestNow, false)
			if err != nil {
				t.Fatalf("capture invalid Gemini usage: %v", err)
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, responseUsage{}) {
				t.Fatalf("expected invalid Gemini usage to be discarded, got %+v", got)
			}
		})
	}
}

func TestRuntimeMissingTerminalUsageDiscard(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	stream := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}},\"delta\":\"partial\"}\n\n"
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("proxy missing-terminal stream: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal {
		t.Fatalf("expected missing terminal stream outcome, got %+v", capture)
	}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, responseUsage{}) {
		t.Fatalf("expected usage observed before missing terminal to be discarded, got %+v", got)
	}
	if len(capture.Body) > 0 {
		t.Fatalf("expected missing-terminal stream to omit reconstructed usage body, got %q", string(capture.Body))
	}
}

func fixedResponseHookTestNow() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}

func generationResponseHookTestUsage(input int, output int, total int) responseUsage {
	inputTokens := input
	outputTokens := output
	totalTokens := total
	return responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens}
}

func generationResponseHookTestUsageWithCacheAndReasoning(input int, output int, total int, cacheRead int, reasoning int) responseUsage {
	usage := generationResponseHookTestUsage(input, output, total)
	cacheReadTokens := cacheRead
	reasoningTokens := reasoning
	usage.CacheReadInputTokens = &cacheReadTokens
	usage.ReasoningTokens = &reasoningTokens
	return usage
}

func tokenCountResponseHookTestUsage(count int) responseUsage {
	inputTokens := count
	totalTokens := count
	return responseUsage{InputTokens: &inputTokens, TotalTokens: &totalTokens}
}

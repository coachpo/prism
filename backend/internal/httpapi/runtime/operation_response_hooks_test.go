package runtime

import (
	"bytes"
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
			name:         "openai responses keeps text-generation usage",
			requestPath:  "/v1/responses",
			payload:      `{"response":{"id":"resp-hook","usage":{"input_tokens":5,"output_tokens":8,"total_tokens":13}}}`,
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantUsage:    generationResponseHookTestUsage(5, 8, 13),
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
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, &forwarded, strings.NewReader(test.payload), "application/json", fixedResponseHookTestNow, false)
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

func TestCountTokenResponsesDoNotUseGenerationUsage(t *testing.T) {
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
			payload:     `{"totalTokens":17,"usageMetadata":{"promptTokenCount":101,"candidatesTokenCount":202,"totalTokenCount":303,"cachedContentTokenCount":404}}`,
			wantUsage:   tokenCountResponseHookTestUsage(17),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, &forwarded, strings.NewReader(test.payload), "application/json", fixedResponseHookTestNow, false)
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

func fixedResponseHookTestNow() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}

func generationResponseHookTestUsage(input int, output int, total int) responseUsage {
	inputTokens := input
	outputTokens := output
	totalTokens := total
	return responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens}
}

func tokenCountResponseHookTestUsage(count int) responseUsage {
	inputTokens := count
	totalTokens := count
	return responseUsage{InputTokens: &inputTokens, TotalTokens: &totalTokens}
}

package runtime

import (
	"net/http"
	"testing"
)

func TestRuntimeOperationHookResidency(t *testing.T) {
	tests := []struct {
		name                           string
		requestPath                    string
		hookCollectionID               string
		provider                       string
		rawBody                        []byte
		wantRequestGenerationExtractor bool
		wantStreamingObserver          bool
		wantRequestStream              bool
		wantGenerationStatus           string
		wantResponseKind               operationResponseKind
		wantUsageRule                  runtimeUsageNormalizationRule
		wantMediaHooks                 bool
		wantMediaKind                  operationMediaRequestKind
		wantStreamHooks                bool
	}{
		{
			name:                           "openai chat completions text generation",
			requestPath:                    "/v1/chat/completions",
			hookCollectionID:               "openai.chat_completions",
			provider:                       "openai",
			rawBody:                        []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hidden"}],"temperature":0.7,"stream":true}`),
			wantRequestGenerationExtractor: true,
			wantRequestStream:              true,
			wantGenerationStatus:           requestGenerationParamsStatusComplete,
			wantResponseKind:               operationResponseKindTextGeneration,
			wantUsageRule:                  runtimeUsageRuleOpenAIChatCompletions,
			wantStreamHooks:                true,
		},
		{
			name:                           "openai responses text generation",
			requestPath:                    "/v1/responses",
			hookCollectionID:               "openai.responses",
			provider:                       "openai",
			rawBody:                        []byte(`{"model":"gpt-4o","input":"hidden","temperature":0.4,"stream":true}`),
			wantRequestGenerationExtractor: true,
			wantRequestStream:              true,
			wantGenerationStatus:           requestGenerationParamsStatusComplete,
			wantResponseKind:               operationResponseKindTextGeneration,
			wantUsageRule:                  runtimeUsageRuleOpenAIResponses,
			wantStreamHooks:                true,
		},
		{
			name:                 "openai responses input tokens token count",
			requestPath:          "/v1/responses/input_tokens",
			hookCollectionID:     runtimeHookCollectionOpenAIResponsesInputTokens,
			provider:             "openai",
			rawBody:              []byte(`{"model":"gpt-4o","input":"hidden","stream":true}`),
			wantRequestStream:    false,
			wantGenerationStatus: requestGenerationParamsStatusMissing,
			wantResponseKind:     operationResponseKindTokenCount,
		},
		{
			name:                 "openai responses compact text adjunct",
			requestPath:          "/v1/responses/compact",
			hookCollectionID:     runtimeHookCollectionOpenAIResponsesCompact,
			provider:             "openai",
			rawBody:              []byte(`{"model":"gpt-4o","input":"hidden","stream":true}`),
			wantRequestStream:    false,
			wantGenerationStatus: requestGenerationParamsStatusMissing,
			wantResponseKind:     operationResponseKindTextGeneration,
			wantUsageRule:        runtimeUsageRuleOpenAIResponses,
		},
		{
			name:                 "openai image generations media",
			requestPath:          "/v1/images/generations",
			hookCollectionID:     runtimeHookCollectionOpenAIImagesGeneration,
			provider:             "openai",
			rawBody:              []byte(`{"model":"gpt-image-1","prompt":"hidden","temperature":0.7,"stream":true}`),
			wantRequestStream:    false,
			wantGenerationStatus: requestGenerationParamsStatusMissing,
			wantResponseKind:     operationResponseKindMedia,
			wantMediaHooks:       true,
			wantMediaKind:        operationMediaRequestKindImageGeneration,
		},
		{
			name:                 "openai image edits media",
			requestPath:          "/v1/images/edits",
			hookCollectionID:     runtimeHookCollectionOpenAIImagesEdit,
			provider:             "openai",
			rawBody:              []byte(`{"model":"gpt-image-1","prompt":"hidden","temperature":0.7,"stream":true}`),
			wantRequestStream:    false,
			wantGenerationStatus: requestGenerationParamsStatusMissing,
			wantResponseKind:     operationResponseKindMedia,
			wantMediaHooks:       true,
			wantMediaKind:        operationMediaRequestKindImageEdit,
		},
		{
			name:                           "anthropic messages text generation",
			requestPath:                    "/v1/messages",
			hookCollectionID:               "anthropic.messages",
			provider:                       "anthropic",
			rawBody:                        []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hidden"}],"temperature":0.6,"stream":true}`),
			wantRequestGenerationExtractor: true,
			wantRequestStream:              true,
			wantGenerationStatus:           requestGenerationParamsStatusComplete,
			wantResponseKind:               operationResponseKindTextGeneration,
			wantUsageRule:                  runtimeUsageRuleAnthropicMessages,
			wantStreamHooks:                true,
		},
		{
			name:                 "anthropic count tokens",
			requestPath:          "/v1/messages/count_tokens",
			hookCollectionID:     runtimeHookCollectionAnthropicCountTokens,
			provider:             "anthropic",
			rawBody:              []byte(`{"model":"claude-3-5-sonnet","messages":[],"temperature":0.7,"max_tokens":123,"stream":true}`),
			wantRequestStream:    false,
			wantGenerationStatus: requestGenerationParamsStatusMissing,
			wantResponseKind:     operationResponseKindTokenCount,
		},
		{
			name:                           "gemini generate content text generation",
			requestPath:                    "/v1beta/models/gemini-2.5-pro:generateContent",
			hookCollectionID:               "gemini.generate_content",
			provider:                       "gemini",
			rawBody:                        []byte(`{"contents":[{"parts":[{"text":"hidden"}]}],"generationConfig":{"temperature":0.3}}`),
			wantRequestGenerationExtractor: true,
			wantRequestStream:              false,
			wantGenerationStatus:           requestGenerationParamsStatusComplete,
			wantResponseKind:               operationResponseKindTextGeneration,
			wantUsageRule:                  runtimeUsageRuleGeminiGenerateContent,
		},
		{
			name:                           "gemini stream generate content",
			requestPath:                    "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			hookCollectionID:               "gemini.stream_generate_content",
			provider:                       "gemini",
			rawBody:                        []byte(`{"contents":[{"parts":[{"text":"hidden"}]}],"generationConfig":{"temperature":0.5}}`),
			wantRequestGenerationExtractor: true,
			wantStreamingObserver:          true,
			wantRequestStream:              true,
			wantGenerationStatus:           requestGenerationParamsStatusComplete,
			wantResponseKind:               operationResponseKindTextGeneration,
			wantUsageRule:                  runtimeUsageRuleGeminiStreamGenerateContent,
			wantStreamHooks:                true,
		},
		{
			name:                 "gemini count tokens",
			requestPath:          "/v1beta/models/gemini-2.5-pro:countTokens",
			hookCollectionID:     runtimeHookCollectionGeminiCountTokens,
			provider:             "gemini",
			rawBody:              []byte(`{"contents":[],"generationConfig":{"temperature":0.2,"maxOutputTokens":5},"stream":true}`),
			wantRequestStream:    false,
			wantGenerationStatus: requestGenerationParamsStatusMissing,
			wantResponseKind:     operationResponseKindTokenCount,
		},
	}

	seen := map[string]struct{}{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			seen[operation.Name] = struct{}{}
			assertResolvedHookCollection(t, operation, test.hookCollectionID)
			assertRequestHookResidency(t, operation, test.provider, test.rawBody, test.requestPath, test.wantRequestGenerationExtractor, test.wantStreamingObserver, test.wantRequestStream, test.wantGenerationStatus)
			assertResponseHookResidency(t, operation, test.provider, test.wantResponseKind, test.wantUsageRule)
			assertMediaHookResidency(t, operation, test.provider, test.wantMediaHooks, test.wantMediaKind)
			assertStreamHookResidency(t, operation, test.provider, test.wantRequestStream, test.wantStreamHooks, test.wantUsageRule)
		})
	}
	assertAllRuntimeOperationsCoveredByHookResidency(t, seen)
}

func assertResolvedHookCollection(t *testing.T, operation RuntimeOperation, want string) {
	t.Helper()
	if operation.HookCollectionID != want {
		t.Fatalf("expected %s to use hook collection %q, got %q", operation.Name, want, operation.HookCollectionID)
	}
}

func assertRequestHookResidency(t *testing.T, operation RuntimeOperation, provider string, rawBody []byte, requestPath string, wantExtractor bool, wantObserver bool, wantStream bool, wantStatus string) {
	t.Helper()
	hooks, ok := requestHooksForOperation(operation)
	if !ok {
		t.Fatalf("expected request hooks for %s", operation.Name)
	}
	if hooks.Provider != provider {
		t.Fatalf("expected %s request hooks for %s, got %+v", provider, operation.Name, hooks)
	}
	if (hooks.ExtractBufferedGenerationParams != nil) != wantExtractor {
		t.Fatalf("expected request generation extractor=%v for %s, got %+v", wantExtractor, operation.Name, hooks)
	}
	if (hooks.NewGenerationParamsStreamingObserver != nil) != wantObserver {
		t.Fatalf("expected streaming observer=%v for %s, got %+v", wantObserver, operation.Name, hooks)
	}
	if got := requestWantsStreamForOperation(operation, rawBody, requestPath); got != wantStream {
		t.Fatalf("expected request stream=%v for %s, got %v", wantStream, operation.Name, got)
	}
	if _, ok := newRequestGenerationParamsStreamingObserver(operation); ok != wantObserver {
		t.Fatalf("expected observer constructor result=%v for %s, got %v", wantObserver, operation.Name, ok)
	}
	snapshot := extractBufferedRequestGenerationParams(operation, rawBody)
	if snapshot.Status != wantStatus {
		t.Fatalf("expected generation status %q for %s, got %+v", wantStatus, operation.Name, snapshot)
	}
	if wantStatus == requestGenerationParamsStatusComplete && (snapshot.Params == nil || snapshot.Params.Provider != provider) {
		t.Fatalf("expected %s generation params for %s, got %+v", provider, operation.Name, snapshot)
	}
	if wantStatus == requestGenerationParamsStatusMissing && snapshot.Params != nil {
		t.Fatalf("expected no generation params for %s, got %+v", operation.Name, snapshot)
	}
}

func assertResponseHookResidency(t *testing.T, operation RuntimeOperation, provider string, wantKind operationResponseKind, wantUsageRule runtimeUsageNormalizationRule) {
	t.Helper()
	hooks, ok := responseHooksForOperation(operation)
	if !ok {
		t.Fatalf("expected response hooks for %s", operation.Name)
	}
	if hooks.Provider != provider || hooks.Kind != wantKind {
		t.Fatalf("expected %s/%s response hooks for %s, got %+v", provider, wantKind, operation.Name, hooks)
	}
	if hooks.ParseNonStreamResponse == nil {
		t.Fatalf("expected non-stream response parser for %s", operation.Name)
	}
	if wantKind == operationResponseKindTextGeneration {
		if hooks.UsageRule != wantUsageRule {
			t.Fatalf("expected response usage rule %+v for %s, got %+v", wantUsageRule, operation.Name, hooks.UsageRule)
		}
	} else if hooks.UsageRule.configured() {
		t.Fatalf("expected %s response hooks to keep usage normalization outside generation seam, got %+v", operation.Name, hooks.UsageRule)
	}
}

func assertMediaHookResidency(t *testing.T, operation RuntimeOperation, provider string, wantMedia bool, wantKind operationMediaRequestKind) {
	t.Helper()
	hooks, ok := mediaHooksForOperation(operation)
	if ok != wantMedia {
		t.Fatalf("expected media hooks=%v for %s, got %v", wantMedia, operation.Name, ok)
	}
	if !wantMedia {
		return
	}
	if hooks.Provider != provider || hooks.RequestKind != wantKind {
		t.Fatalf("expected %s/%s media hooks for %s, got %+v", provider, wantKind, operation.Name, hooks)
	}
	if hooks.ExtractModel == nil || hooks.RewriteModel == nil {
		t.Fatalf("expected model extract/rewrite media hooks for %s, got %+v", operation.Name, hooks)
	}
}

func assertStreamHookResidency(t *testing.T, operation RuntimeOperation, provider string, isStreamingRequest bool, wantStream bool, wantUsageRule runtimeUsageNormalizationRule) {
	t.Helper()
	hooks, ok := streamHooksForProxyResponse(operation, isStreamingRequest)
	if ok != (isStreamingRequest && wantStream) {
		t.Fatalf("expected request stream hook=%v for %s, got %v", isStreamingRequest && wantStream, operation.Name, ok)
	}
	forcedHooks, forcedOK := streamHooksForProxyResponse(operation, true)
	if forcedOK != wantStream {
		t.Fatalf("expected forced stream hook=%v for %s, got %v", wantStream, operation.Name, forcedOK)
	}
	if !wantStream {
		return
	}
	selected := forcedHooks
	if ok {
		selected = hooks
	}
	if selected.Provider != provider || selected.Kind != operationResponseKindTextGeneration {
		t.Fatalf("expected %s text stream hooks for %s, got %+v", provider, operation.Name, selected)
	}
	if selected.UsageRule != wantUsageRule {
		t.Fatalf("expected stream usage rule %+v for %s, got %+v", wantUsageRule, operation.Name, selected.UsageRule)
	}
	if selected.MergeUsage == nil {
		t.Fatalf("expected stream usage merger for %s", operation.Name)
	}
}

func assertAllRuntimeOperationsCoveredByHookResidency(t *testing.T, seen map[string]struct{}) {
	t.Helper()
	catalog := RuntimeOperationCatalog()
	if len(seen) != len(catalog) {
		t.Fatalf("expected hook-residency coverage for %d operations, got %d", len(catalog), len(seen))
	}
	for _, operation := range catalog {
		if _, ok := seen[operation.Name]; !ok {
			t.Fatalf("missing hook-residency coverage for %s", operation.Name)
		}
	}
}

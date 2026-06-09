package runtime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRequestGenerationParamsByOperation(t *testing.T) {
	tests := []struct {
		name, requestPath string
		rawBody           []byte
		want              requestGenerationParamsSnapshot
	}{
		{"openai chat completions", "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hidden"}],"temperature":0.7,"top_p":0.9,"max_completion_tokens":1024,"reasoning_effort":"low"}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", Temperature: floatPtr(0.7), TopP: floatPtr(0.9), MaxOutputTokens: intPtr(1024), MaxOutputTokensSource: stringPtr("max_completion_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("low"), SourceField: stringPtr("reasoning_effort")}}}},
		{"openai responses", "/v1/responses", []byte(`{"model":"gpt-4o","input":"hidden","temperature":0.4,"top_p":0.8,"max_output_tokens":256,"reasoning":{"effort":"medium"}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", Temperature: floatPtr(0.4), TopP: floatPtr(0.8), MaxOutputTokens: intPtr(256), MaxOutputTokensSource: stringPtr("max_output_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("medium"), SourceField: stringPtr("reasoning.effort")}}}},
		{"openai image operation skipped", "/v1/images/generations", []byte(`{"model":"gpt-image-1","temperature":0.7,"top_p":0.9,"max_tokens":64}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMissing}},
		{"anthropic messages", "/v1/messages", []byte(`{"model":"claude","messages":[{"role":"user","content":"hidden"}],"temperature":0.6,"top_p":0.95,"max_tokens":512,"thinking":{"type":"enabled","budget_tokens":2048},"output_config":{"effort":"high"}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "anthropic", Temperature: floatPtr(0.6), TopP: floatPtr(0.95), MaxOutputTokens: intPtr(512), MaxOutputTokensSource: stringPtr("max_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("high"), Mode: stringPtr("enabled"), BudgetTokens: intPtr(2048), SourceField: stringPtr("output_config.effort")}}}},
		{"gemini generate content", "/v1beta/models/gemini:generateContent", []byte(`{"contents":[{"parts":[{"text":"hidden"}]}],"generationConfig":{"temperature":0.3,"topP":0.7,"topK":40,"maxOutputTokens":777,"thinkingConfig":{"thinkingBudget":123,"thinkingLevel":"high","includeThoughts":true}}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "gemini", Temperature: floatPtr(0.3), TopP: floatPtr(0.7), TopK: intPtr(40), MaxOutputTokens: intPtr(777), MaxOutputTokensSource: stringPtr("generationConfig.maxOutputTokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("high"), BudgetTokens: intPtr(123), IncludeThoughts: boolPtr(true), SourceField: stringPtr("generationConfig.thinkingConfig")}}}},
		{"gemini malformed", "/v1beta/models/gemini:generateContent", []byte(`{"generationConfig":`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMalformed}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operationMatch := mustResolveRequestGenerationOperation(t, test.requestPath)
			original := append([]byte(nil), test.rawBody...)
			got := extractBufferedRequestGenerationParams(operationMatch.Operation, test.rawBody)
			if !reflect.DeepEqual(got, test.want) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(test.want)
				t.Fatalf("expected %s, got %s", wantJSON, gotJSON)
			}
			if !bytes.Equal(test.rawBody, original) {
				t.Fatal("buffered extraction mutated raw request bytes")
			}
		})
	}
}

func TestEstimateOpenAIChatCompletionsRequestTokens(t *testing.T) {
	contextWindowTokens := 10_000
	estimation, err := estimateOpenAIChatCompletionsRequestTokens([]byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"You are terse."},{"role":"user","content":[{"type":"text","text":"Summarize this request."}]}],"response_format":{"type":"json_schema","json_schema":{"name":"summary","schema":{"type":"object"}}},"max_completion_tokens":512}`), requestContextEstimationOptions{ContextWindowTokens: &contextWindowTokens})
	if err != nil {
		t.Fatalf("estimate chat request tokens: %v", err)
	}
	if estimation == nil {
		t.Fatal("expected chat estimation metadata")
	}
	if estimation.Method != openAIChatContextEstimationMethod {
		t.Fatalf("expected method %q, got %+v", openAIChatContextEstimationMethod, estimation)
	}
	if estimation.ReservedOutputTokens != 512 {
		t.Fatalf("expected explicit chat output reserve 512, got %+v", estimation)
	}
	if estimation.UsableContextWindowTokens == nil || *estimation.UsableContextWindowTokens != 9000 {
		t.Fatalf("expected default usable chat context 9000, got %+v", estimation)
	}
	if estimation.EstimatedInputTokens <= 0 {
		t.Fatalf("expected positive chat input estimate, got %+v", estimation)
	}
	if estimation.EstimatedTotalContextTokens != estimation.EstimatedInputTokens+512 {
		t.Fatalf("expected chat total context to include explicit reserve, got %+v", estimation)
	}
}

func TestEstimateOpenAIResponsesRequestTokens(t *testing.T) {
	defaultOutputReserve := 2048
	contextWindowTokens := 20_000
	maxContextUtilization := 0.75
	estimation, err := estimateOpenAIResponsesRequestTokens([]byte(`{"model":"gpt-4o","instructions":"Be concise.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Summarize this request."}]}],"text":{"format":{"type":"text"}}}`), requestContextEstimationOptions{DefaultOutputTokenReserve: &defaultOutputReserve, ContextWindowTokens: &contextWindowTokens, MaxContextUtilization: &maxContextUtilization})
	if err != nil {
		t.Fatalf("estimate responses request tokens: %v", err)
	}
	if estimation == nil {
		t.Fatal("expected responses estimation metadata")
	}
	if estimation.Method != openAIResponsesContextEstimationMethod {
		t.Fatalf("expected method %q, got %+v", openAIResponsesContextEstimationMethod, estimation)
	}
	if estimation.ReservedOutputTokens != defaultOutputReserve {
		t.Fatalf("expected default output reserve %d, got %+v", defaultOutputReserve, estimation)
	}
	if estimation.UsableContextWindowTokens == nil || *estimation.UsableContextWindowTokens != 15_000 {
		t.Fatalf("expected override usable context 15000, got %+v", estimation)
	}
	if estimation.EstimatedInputTokens <= 0 {
		t.Fatalf("expected positive responses input estimate, got %+v", estimation)
	}
	if estimation.EstimatedTotalContextTokens != estimation.EstimatedInputTokens+defaultOutputReserve {
		t.Fatalf("expected responses total context to include default reserve, got %+v", estimation)
	}
}

func TestRequestGenerationParamsContextFitHelper(t *testing.T) {
	estimation := &requestContextEstimation{EstimatedTotalContextTokens: 600}
	if !estimation.fitsUsableContextWindowTokens(600) {
		t.Fatal("expected equal estimated and usable context tokens to fit")
	}
	if estimation.fitsUsableContextWindowTokens(599) {
		t.Fatal("expected estimated tokens above usable context window to be rejected")
	}
	var missing *requestContextEstimation
	if missing.fitsUsableContextWindowTokens(600) {
		t.Fatal("expected missing estimation to never fit")
	}
	if estimation.fitsUsableContextWindowTokens(0) {
		t.Fatal("expected unavailable usable context metadata to never fit")
	}
}

func TestGeminiStreamingObserverByOperation(t *testing.T) {
	streamOperation := mustResolveRequestGenerationOperation(t, "/v1beta/models/gemini:streamGenerateContent").Operation
	generateOperation := mustResolveRequestGenerationOperation(t, "/v1beta/models/gemini:generateContent").Operation
	streamFlagBody := []byte(`{"stream":true}`)
	if !canStreamIncomingRequestBody(requestPlan{APIFamily: "gemini"}, streamOperation) {
		t.Fatal("expected streamGenerateContent operation to allow streaming request-generation observation")
	}
	if canStreamIncomingRequestBody(requestPlan{APIFamily: "gemini"}, generateOperation) {
		t.Fatal("expected non-stream generateContent operation to use buffered request-generation extraction")
	}
	if requestWantsStreamForOperation(generateOperation, streamFlagBody, "/v1beta/models/gemini:generateContent") {
		t.Fatal("expected generateContent to ignore body stream:true for runtime stream classification")
	}
	if !requestWantsStreamForOperation(streamOperation, nil, "/v1beta/models/gemini:streamGenerateContent") {
		t.Fatal("expected streamGenerateContent path to force runtime stream classification")
	}
	if _, ok := newRequestGenerationParamsStreamingObserver(generateOperation); ok {
		t.Fatal("expected generateContent to have no streaming request-generation observer hook")
	}
	observer, ok := newRequestGenerationParamsStreamingObserver(streamOperation)
	if !ok {
		t.Fatal("expected streamGenerateContent to provide a streaming request-generation observer hook")
	}
	for _, chunk := range []string{`{"contents":[`, `{"parts":[{"text":"hidden"}]}`, `],"generationConfig":{"temperature":0.5,"topP":0.8,"topK":32,"maxOutputTokens":99,"thinkingConfig":{"thinkingBudget":11,"thinkingLevel":"low","includeThoughts":false}}}`} {
		observer.Observe([]byte(chunk))
	}
	observer.Finish()
	snapshot := observer.Snapshot()
	if snapshot.Status != requestGenerationParamsStatusComplete || snapshot.Params == nil || snapshot.Params.MaxOutputTokens == nil || *snapshot.Params.MaxOutputTokens != 99 {
		t.Fatalf("expected operation-selected Gemini streaming params, got %+v", snapshot)
	}
}

func TestCountTokenHooksDoNotUseGenerationParsers(t *testing.T) {
	tests := []struct {
		name             string
		requestPath      string
		rawBody          []byte
		hookCollectionID string
		provider         string
	}{
		{
			name:             "anthropic count tokens",
			requestPath:      "/v1/messages/count_tokens",
			rawBody:          []byte(`{"model":"claude-3-5-sonnet","messages":[],"temperature":0.7,"top_p":0.8,"max_tokens":123,"thinking":{"type":"enabled","budget_tokens":456},"stream":true}`),
			hookCollectionID: runtimeHookCollectionAnthropicCountTokens,
			provider:         "anthropic",
		},
		{
			name:             "gemini count tokens",
			requestPath:      "/v1beta/models/gemini-2.5-pro:countTokens",
			rawBody:          []byte(`{"contents":[],"generationConfig":{"temperature":0.2,"topP":0.3,"topK":4,"maxOutputTokens":5,"thinkingConfig":{"thinkingBudget":6,"thinkingLevel":"low","includeThoughts":true}},"stream":true}`),
			hookCollectionID: runtimeHookCollectionGeminiCountTokens,
			provider:         "gemini",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRequestGenerationOperation(t, test.requestPath).Operation
			if operation.HookCollectionID != test.hookCollectionID {
				t.Fatalf("expected hook collection %q, got %q", test.hookCollectionID, operation.HookCollectionID)
			}
			hooks, ok := requestHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected dedicated request hooks for %s", operation.Name)
			}
			if hooks.Provider != test.provider {
				t.Fatalf("expected provider %q, got %q", test.provider, hooks.Provider)
			}
			responseHooks, ok := responseHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected dedicated response hooks for %s", operation.Name)
			}
			if responseHooks.Provider != test.provider || responseHooks.Kind != operationResponseKindTokenCount {
				t.Fatalf("expected %s token-count response hooks, got %+v", test.provider, responseHooks)
			}
			if hooks.ExtractBufferedGenerationParams != nil {
				t.Fatal("expected count-token hook to omit generation-param extraction")
			}
			if hooks.NewGenerationParamsStreamingObserver != nil {
				t.Fatal("expected count-token hook to omit generation streaming observer")
			}
			if requestWantsStreamForOperation(operation, test.rawBody, test.requestPath) {
				t.Fatal("expected count-token hook to ignore generation-style stream:true")
			}
			if canStreamIncomingRequestBody(requestPlan{APIFamily: operation.APIFamily}, operation) {
				t.Fatal("expected count-token operation to reject streaming request-body semantics")
			}
			snapshot := extractBufferedRequestGenerationParams(operation, test.rawBody)
			if snapshot.Status != requestGenerationParamsStatusMissing || snapshot.Params != nil {
				t.Fatalf("expected count-token generation snapshot to stay missing, got %+v", snapshot)
			}
		})
	}
}

func mustResolveRequestGenerationOperation(t *testing.T, requestPath string) RuntimeOperationMatch {
	t.Helper()
	operationMatch, ok := ResolveRuntimeOperation(http.MethodPost, requestPath)
	if !ok {
		t.Fatalf("expected runtime operation for %s", requestPath)
	}
	return operationMatch
}

func TestGeminiGenerationParamsStreamingObserverExtractsAcrossSmallChunks(t *testing.T) {
	observer := newGeminiGenerationParamsStreamingObserver()
	for _, chunk := range []string{`{"contents":[`, `{"parts":[{"text":"hidden"}]}`, `],"generationConfig":{"temperature":0.5,"topP":0.8,"topK":32,"maxOutputTokens":99,"thinkingConfig":{"thinkingBudget":11,"thinkingLevel":"low","includeThoughts":false}}}`} {
		observer.Observe([]byte(chunk))
	}
	observer.Finish()
	snapshot := observer.Snapshot()
	if snapshot.Status != requestGenerationParamsStatusComplete || snapshot.Params == nil || snapshot.Params.MaxOutputTokens == nil || *snapshot.Params.MaxOutputTokens != 99 {
		t.Fatalf("expected complete Gemini streaming params, got %+v", snapshot)
	}
	if snapshot.Params.MaxOutputTokensSource == nil || *snapshot.Params.MaxOutputTokensSource != "generationConfig.maxOutputTokens" {
		t.Fatalf("expected max token source, got %+v", snapshot.Params)
	}
	if snapshot.Params.Reasoning == nil || snapshot.Params.Reasoning.SourceField == nil || *snapshot.Params.Reasoning.SourceField != "generationConfig.thinkingConfig" {
		t.Fatalf("expected reasoning source, got %+v", snapshot.Params)
	}
	raw, _ := json.Marshal(snapshot.Params)
	for _, forbidden := range []string{"hidden", "contents", "parts", "text"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("retained forbidden payload %q in %s", forbidden, raw)
		}
	}
}

func TestGeminiGenerationParamsStreamingObserverHandlesConfigBeforeAndAfterContents(t *testing.T) {
	for _, body := range []string{`{"generationConfig":{"temperature":0.1},"contents":[{"parts":[{"text":"hidden"}]}]}`, `{"contents":[{"parts":[{"text":"hidden"}]}],"generationConfig":{"temperature":0.1}}`} {
		observer := newGeminiGenerationParamsStreamingObserver()
		_, _ = observer.Write([]byte(body))
		observer.Finish()
		snapshot := observer.Snapshot()
		if snapshot.Status != requestGenerationParamsStatusComplete || snapshot.Params.Temperature == nil || *snapshot.Params.Temperature != 0.1 {
			t.Fatalf("expected order-independent extraction, got %+v", snapshot)
		}
	}
}

func TestGeminiGenerationParamsStreamingObserverReportsTerminalStatuses(t *testing.T) {
	tests := []struct {
		name, body string
		limit      int
		finish     bool
		want       string
	}{{"incomplete", `{"generationConfig":{"temperature":0.1}}`, 0, false, requestGenerationParamsStatusIncomplete}, {"malformed", `{"generationConfig":`, 0, true, requestGenerationParamsStatusMalformed}, {"missing", `{"contents":[{"parts":[{"text":"hidden"}]}]}`, 0, true, requestGenerationParamsStatusMissing}, {"large skipped content still parses", `{"contents":"` + strings.Repeat("x", 128) + `","generationConfig":{"temperature":0.1}}`, 32, true, requestGenerationParamsStatusComplete}, {"oversize captured scalar", `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"` + strings.Repeat("x", 128) + `"}}}`, 32, true, requestGenerationParamsStatusSkippedOversize}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observer := newGeminiGenerationParamsStreamingObserver(test.limit)
			observer.Observe([]byte(test.body))
			if test.finish {
				observer.Finish()
			}
			if got := observer.Snapshot().Status; got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func floatPtr(value float64) *float64 { return &value }

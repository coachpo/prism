package runtime

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestExtractBufferedRequestGenerationParamsProviderMappings(t *testing.T) {
	tests := []struct {
		name, apiFamily, requestPath string
		rawBody                      []byte
		want                         requestGenerationParamsSnapshot
	}{
		{"openai chat non stream", "openai", "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hidden"}],"temperature":0.7,"top_p":0.9,"max_completion_tokens":1024,"reasoning_effort":"low"}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", Temperature: floatPtr(0.7), TopP: floatPtr(0.9), MaxOutputTokens: intPtr(1024), MaxOutputTokensSource: stringPtr("max_completion_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("low"), SourceField: stringPtr("reasoning_effort")}}}},
		{"openai chat stream", "openai", "/v1/chat/completions", []byte(`{"model":"gpt-4o","stream":true,"temperature":0.2,"top_p":1,"max_tokens":64,"messages":[{"role":"user","content":"hidden"}]}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", Temperature: floatPtr(0.2), TopP: floatPtr(1), MaxOutputTokens: intPtr(64), MaxOutputTokensSource: stringPtr("max_tokens")}}},
		{"openai responses", "openai", "/v1/responses", []byte(`{"model":"gpt-4o","input":"hidden","temperature":0.4,"top_p":0.8,"max_output_tokens":256,"reasoning":{"effort":"medium"}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "openai", Temperature: floatPtr(0.4), TopP: floatPtr(0.8), MaxOutputTokens: intPtr(256), MaxOutputTokensSource: stringPtr("max_output_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("medium"), SourceField: stringPtr("reasoning.effort")}}}},
		{"openai responses malformed", "openai", "/v1/responses", []byte(`{"model":"gpt-4o","input":`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMalformed}},
		{"anthropic", "anthropic", "/v1/messages", []byte(`{"model":"claude","messages":[{"role":"user","content":"hidden"}],"temperature":0.6,"top_p":0.95,"max_tokens":512,"thinking":{"type":"enabled","budget_tokens":2048},"output_config":{"effort":"high"}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "anthropic", Temperature: floatPtr(0.6), TopP: floatPtr(0.95), MaxOutputTokens: intPtr(512), MaxOutputTokensSource: stringPtr("max_tokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("high"), Mode: stringPtr("enabled"), BudgetTokens: intPtr(2048), SourceField: stringPtr("output_config.effort")}}}},
		{"gemini", "gemini", "/v1beta/models/gemini:generateContent", []byte(`{"contents":[{"parts":[{"text":"hidden"}]}],"generationConfig":{"temperature":0.3,"topP":0.7,"topK":40,"maxOutputTokens":777,"thinkingConfig":{"thinkingBudget":123,"thinkingLevel":"high","includeThoughts":true}}}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusComplete, Params: &requestGenerationParams{Provider: "gemini", Temperature: floatPtr(0.3), TopP: floatPtr(0.7), TopK: intPtr(40), MaxOutputTokens: intPtr(777), MaxOutputTokensSource: stringPtr("generationConfig.maxOutputTokens"), Reasoning: &requestGenerationReasoningParams{Effort: stringPtr("high"), BudgetTokens: intPtr(123), IncludeThoughts: boolPtr(true), SourceField: stringPtr("generationConfig.thinkingConfig")}}}},
		{"missing", "openai", "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hidden"}]}`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMissing, Params: &requestGenerationParams{Provider: "openai"}}},
		{"malformed", "gemini", "/v1beta/models/gemini:generateContent", []byte(`{"generationConfig":`), requestGenerationParamsSnapshot{Status: requestGenerationParamsStatusMalformed}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := append([]byte(nil), test.rawBody...)
			got := extractBufferedRequestGenerationParams(test.apiFamily, test.requestPath, test.rawBody)
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

package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/anthropic"
	"github.com/coachpo/prism/backend/internal/gateway/provider/gemini"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

func TestProviderAdapterCompileTimeConformance(t *testing.T) {
	var _ provider.ProviderAdapter = openai.Adapter{}
	var _ provider.ProviderAdapter = anthropic.Adapter{}
	var _ provider.ProviderAdapter = gemini.Adapter{}
}

func TestProviderAdaptersExposeCurrentBehavior(t *testing.T) {
	tests := []struct {
		name             string
		adapter          provider.ProviderAdapter
		operation        provider.Operation
		wantResponseKind string
		wantStream       bool
	}{
		{
			name:             "openai chat completions",
			adapter:          openai.New(),
			operation:        provider.Operation{Name: "openai.chat_completions"},
			wantResponseKind: "text_generation",
			wantStream:       true,
		},
		{
			name:             "openai responses input tokens",
			adapter:          openai.New(),
			operation:        provider.Operation{Name: openai.OperationResponsesInputTokens},
			wantResponseKind: "token_count",
		},
		{
			name:             "openai responses compact",
			adapter:          openai.New(),
			operation:        provider.Operation{Name: openai.OperationResponsesCompact},
			wantResponseKind: "text_generation",
		},
		{
			name:             "anthropic messages",
			adapter:          anthropic.New(),
			operation:        provider.Operation{Name: "anthropic.messages"},
			wantResponseKind: "text_generation",
			wantStream:       true,
		},
		{
			name:             "anthropic count tokens",
			adapter:          anthropic.New(),
			operation:        provider.Operation{Name: "anthropic.count_tokens"},
			wantResponseKind: "token_count",
		},
		{
			name:             "gemini count tokens",
			adapter:          gemini.New(),
			operation:        provider.Operation{Name: "gemini.count_tokens"},
			wantResponseKind: "token_count",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			behavior, ok := test.adapter.CurrentBehavior(context.Background(), test.operation)
			if !ok {
				t.Fatalf("expected current behavior for %s", test.operation.Name)
			}
			if behavior.APIFamily != test.adapter.APIFamily() {
				t.Fatalf("expected api family %q, got %+v", test.adapter.APIFamily(), behavior)
			}
			if !behavior.HasRequest || !behavior.HasResponse {
				t.Fatalf("expected request and response behavior for %s, got %+v", test.operation.Name, behavior)
			}
			if behavior.Response.Kind != test.wantResponseKind {
				t.Fatalf("expected response kind %q, got %+v", test.wantResponseKind, behavior.Response)
			}
			if behavior.HasStream != test.wantStream {
				t.Fatalf("expected stream behavior %v, got %+v", test.wantStream, behavior)
			}
		})
	}
}

func TestOpenAIAdapterClassifiesOverflow(t *testing.T) {
	adapter := openai.New()
	classification := adapter.ClassifyOverflow(context.Background(), provider.UpstreamResponse{
		StatusCode: 400,
		Body:       []byte(`{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`),
	})
	if !classification.Promotable || classification.ErrorCode != "context_length_exceeded" {
		t.Fatalf("expected promotable overflow classification, got %+v", classification)
	}
}

func TestOpenAIAdapterBuildsExactTextUpstreamRequests(t *testing.T) {
	adapter := openai.New()
	tests := []struct {
		name          string
		operationName string
		rawBody       []byte
		wantPath      string
		wantBodyModel string
	}{
		{name: "chat completions", operationName: openai.OperationChatCompletions, rawBody: []byte(`{"model":"public-chat","messages":[]}`), wantPath: "/v1/chat/completions", wantBodyModel: "target-model"},
		{name: "responses", operationName: openai.OperationResponses, rawBody: []byte(`{"model":"public-responses","input":"hello"}`), wantPath: "/v1/responses", wantBodyModel: "target-model"},
		{name: "input tokens", operationName: openai.OperationResponsesInputTokens, rawBody: []byte(`{"model":"public-responses","input":"hello"}`), wantPath: "/v1/responses/input_tokens", wantBodyModel: "target-model"},
		{name: "compact", operationName: openai.OperationResponsesCompact, rawBody: []byte(`{"model":"public-responses","input":"hello"}`), wantPath: "/v1/responses/compact", wantBodyModel: "target-model"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream, err := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
				Operation:     provider.Operation{Name: test.operationName},
				RawBody:       test.rawBody,
				TargetModelID: "target-model",
			})
			if err != nil {
				t.Fatalf("build text upstream request: %v", err)
			}
			if upstream.Method != http.MethodPost || upstream.Path != test.wantPath {
				t.Fatalf("expected POST %s, got %+v", test.wantPath, upstream)
			}
			if got := adapterTestBodyModel(t, upstream.Body); got != test.wantBodyModel {
				t.Fatalf("expected model %q, got %q", test.wantBodyModel, got)
			}
		})
	}
}

func TestOpenAIAdapterExtractsNativeUsage(t *testing.T) {
	adapter := openai.New()
	tests := []struct {
		name       string
		operation  string
		body       string
		wantInput  int
		wantOutput int
		wantTotal  int
	}{
		{name: "chat completions", operation: openai.OperationChatCompletions, body: `{"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`, wantInput: 7, wantOutput: 13, wantTotal: 20},
		{name: "responses", operation: openai.OperationResponses, body: `{"usage":{"input_tokens":11,"output_tokens":17,"total_tokens":28}}`, wantInput: 11, wantOutput: 17, wantTotal: 28},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, err := adapter.ExtractUsage(context.Background(), provider.UpstreamResponse{Operation: provider.Operation{Name: test.operation}, Body: []byte(test.body)})
			if err != nil || usage.InputTokens == nil || usage.OutputTokens == nil || usage.TotalTokens == nil || *usage.InputTokens != test.wantInput || *usage.OutputTokens != test.wantOutput || *usage.TotalTokens != test.wantTotal {
				t.Fatalf("extract native usage: usage=%+v err=%v", usage, err)
			}
		})
	}
}

func adapterTestBodyModel(t *testing.T, body []byte) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	model, _ := payload["model"].(string)
	return model
}

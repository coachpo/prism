package provider_test

import (
	"context"
	"encoding/json"
	"errors"
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
		wantMedia        bool
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
			name:             "openai image edit",
			adapter:          openai.New(),
			operation:        provider.Operation{Name: "openai.images.edits"},
			wantResponseKind: "media",
			wantMedia:        true,
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
			if behavior.HasMedia != test.wantMedia {
				t.Fatalf("expected media behavior %v, got %+v", test.wantMedia, behavior)
			}
		})
	}
}

func TestOpenAIAdapterBridgesTranslationAndOverflowClassifiers(t *testing.T) {
	adapter := openai.New()
	request := provider.ConversionRequest{
		Operation:     provider.Operation{Name: "openai.responses"},
		Mode:          provider.TranslationModeOpenAIResponsesToChatCompletions,
		RawBody:       []byte(`{"model":"gpt-test","input":"hello"}`),
		TargetModelID: "gpt-upstream",
	}
	capability, err := adapter.ConversionCapability(context.Background(), request)
	if err != nil {
		t.Fatalf("classify conversion: %v", err)
	}
	if !capability.RequestSupported || !capability.ResponseSupported || !capability.StreamSupported {
		t.Fatalf("expected safe conversion capability, got %+v", capability)
	}

	translated, err := adapter.TranslateRequest(context.Background(), request)
	if err != nil {
		t.Fatalf("translate request: %v", err)
	}
	if translated.Path != "/v1/chat/completions" || len(translated.Body) == 0 {
		t.Fatalf("unexpected translated request: %+v", translated)
	}

	classification := adapter.ClassifyOverflow(context.Background(), provider.UpstreamResponse{
		StatusCode:      400,
		Body:            []byte(`{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`),
		TranslationMode: provider.TranslationModeNone,
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

func TestOpenAIAdapterRejectsUnsupportedTextConversionWithTypedError(t *testing.T) {
	adapter := openai.New()
	_, err := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
		Operation:       provider.Operation{Name: openai.OperationResponses},
		RawBody:         []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"}}`),
		TargetModelID:   "chat-target",
		TranslationMode: provider.TranslationModeOpenAIResponsesToChatCompletions,
	})
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected provider adapter error, got %v", err)
	}
	if adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Code != "openai_request_translation_unsupported" || adapterErr.Fields["unsupported_reason"] != "responses_text" {
		t.Fatalf("expected typed unsupported conversion error, got %+v", adapterErr)
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

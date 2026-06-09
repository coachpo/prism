package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

const updateOpenAITranslationGoldensEnv = "PRISM_UPDATE_OPENAI_TRANSLATION_GOLDENS"

func TestOpenAITranslationGoldenRequests(t *testing.T) {
	adapter := openai.New()
	tests := []struct {
		name       string
		operation  string
		mode       provider.TranslationMode
		target     string
		raw        []byte
		wantPath   string
		goldenFile string
	}{
		{
			name:       "responses request to chat upstream",
			operation:  openai.OperationResponses,
			mode:       provider.TranslationModeOpenAIResponsesToChatCompletions,
			target:     "chat-target",
			raw:        []byte(`{"model":"responses-public","instructions":"system note","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"max_output_tokens":64,"temperature":0.2}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_to_chat.json",
		},
		{
			name:       "chat request to responses upstream",
			operation:  openai.OperationChatCompletions,
			mode:       provider.TranslationModeOpenAIChatCompletionsToResponses,
			target:     "responses-target",
			raw:        []byte(`{"model":"chat-public","messages":[{"role":"system","content":"system note"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"max_completion_tokens":32,"reasoning_effort":"low"}`),
			wantPath:   "/v1/responses",
			goldenFile: "request_chat_to_responses.json",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream, err := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
				Operation:       provider.Operation{Name: test.operation, APIFamily: provider.APIFamilyOpenAI},
				RawBody:         test.raw,
				TargetModelID:   test.target,
				TranslationMode: test.mode,
			})
			if err != nil {
				t.Fatalf("build translated request: %v", err)
			}
			if upstream.Method != http.MethodPost || upstream.Path != test.wantPath {
				t.Fatalf("expected POST %s, got %+v", test.wantPath, upstream)
			}
			assertGoldenJSON(t, test.goldenFile, upstream.Body)
		})
	}
}

func TestOpenAITranslationGoldenResponses(t *testing.T) {
	adapter := openai.New()
	tests := []struct {
		name         string
		mode         provider.TranslationMode
		requested    string
		raw          []byte
		goldenFile   string
		wantUsage    responseUsage
		wantRuleName string
	}{
		{
			name:         "responses upstream to chat client",
			mode:         provider.TranslationModeOpenAIChatCompletionsToResponses,
			raw:          []byte(`{"id":"resp_123","created_at":1700000000,"model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}}`),
			goldenFile:   "response_responses_to_chat.json",
			wantUsage:    generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantRuleName: runtimeUsageRuleName(runtimeUsageRuleOpenAIResponses),
		},
		{
			name:         "chat upstream to responses client",
			mode:         provider.TranslationModeOpenAIResponsesToChatCompletions,
			requested:    "responses-public",
			raw:          []byte(`{"id":"chatcmpl_123","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`),
			goldenFile:   "response_chat_to_responses.json",
			wantUsage:    generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantRuleName: runtimeUsageRuleName(runtimeUsageRuleOpenAIChatCompletions),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := adapter.AdaptNonStreamResponse(context.Background(), provider.UpstreamResponse{
				StatusCode:       http.StatusOK,
				Body:             test.raw,
				TranslationMode:  test.mode,
				RequestedModelID: test.requested,
				Operation:        provider.Operation{APIFamily: provider.APIFamilyOpenAI},
			})
			if err != nil {
				t.Fatalf("adapt translated response: %v", err)
			}
			assertGoldenJSON(t, test.goldenFile, response.Body)
			if got, _ := responseUsageFromProviderEnvelope(response.Usage); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected usage %+v, got %+v", test.wantUsage, got)
			}
			if response.Usage.NormalizationRule != test.wantRuleName {
				t.Fatalf("expected normalization rule %q, got %+v", test.wantRuleName, response.Usage)
			}
		})
	}
}

func TestOpenAITranslationGoldenStreams(t *testing.T) {
	tests := []struct {
		name        string
		ingressPath string
		mode        TranslationMode
		requested   string
		stream      string
		goldenFile  string
		wantUsage   responseUsage
	}{
		{
			name:        "responses stream to chat client",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.created\n" +
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
				"event: response.output_item.added\n" +
				"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
				"event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"hello \"}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello \"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":6,\"total_tokens\":16,\"input_tokens_details\":{\"cached_tokens\":4},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n",
			goldenFile: "stream_responses_to_chat.sse",
			wantUsage:  generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
		},
		{
			name:        "chat stream to responses client",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			requested:   "responses-public",
			stream: "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
				"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
				"data: [DONE]\n\n",
			goldenFile: "stream_chat_to_responses.sse",
			wantUsage:  generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.ingressPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, test.mode, test.requested, context.Background(), &forwarded, strings.NewReader(test.stream), fixedResponseHookTestNow, true)
			if err != nil {
				t.Fatalf("translate stream: %v", err)
			}
			assertGoldenText(t, test.goldenFile, forwarded.Bytes())
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected stream usage %+v, got %+v", test.wantUsage, got)
			}
		})
	}
}
func TestOpenAITranslationGoldenRejectedShapes(t *testing.T) {
	adapter := openai.New()
	_, requestErr := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
		Operation:       provider.Operation{Name: openai.OperationResponses, APIFamily: provider.APIFamilyOpenAI},
		RawBody:         []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"}}`),
		TargetModelID:   "chat-target",
		TranslationMode: provider.TranslationModeOpenAIResponsesToChatCompletions,
	})
	assertGoldenAdapterError(t, "rejected_request_responses_text.json", requestErr)

	_, responseErr := adapter.AdaptNonStreamResponse(context.Background(), provider.UpstreamResponse{
		StatusCode:       http.StatusOK,
		Body:             []byte(`{"id":"chatcmpl_fn","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`),
		TranslationMode:  provider.TranslationModeOpenAIResponsesToChatCompletions,
		RequestedModelID: "responses-public",
		Operation:        provider.Operation{APIFamily: provider.APIFamilyOpenAI},
	})
	assertGoldenAdapterError(t, "rejected_response_chat_tool_calls.json", responseErr)

	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	_, streamErr := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIResponsesToChatCompletions, "responses-public", context.Background(), &forwarded, strings.NewReader("data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n"), fixedResponseHookTestNow, true)
	assertGoldenDomainError(t, "rejected_stream_chat_tool_call.json", streamErr)
}

func TestOpenAITranslationGoldenAdjunctOperationsRejectAccidentalConversion(t *testing.T) {
	adapter := openai.New()
	for _, operationName := range []string{openai.OperationResponsesInputTokens, openai.OperationResponsesCompact} {
		t.Run(operationName, func(t *testing.T) {
			_, err := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
				Operation:       provider.Operation{Name: operationName, APIFamily: provider.APIFamilyOpenAI},
				RawBody:         []byte(`{"model":"responses-public","input":"hello"}`),
				TargetModelID:   "chat-target",
				TranslationMode: provider.TranslationModeOpenAIResponsesToChatCompletions,
			})
			assertGoldenAdapterError(t, "rejected_adjunct_conversion.json", err)
		})
	}
}
func assertGoldenJSON(t *testing.T, name string, actual []byte) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(actual, &decoded); err != nil {
		t.Fatalf("decode actual JSON for %s: %v", name, err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("canonicalize actual JSON for %s: %v", name, err)
	}
	assertGoldenText(t, name, canonical)
}

func assertGoldenAdapterError(t *testing.T, name string, err error) {
	t.Helper()
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected adapter error for %s, got %v", name, err)
	}
	payload := map[string]any{"http_status": adapterErr.HTTPStatus, "code": adapterErr.Code, "detail": adapterErr.Detail}
	maps.Copy(payload, adapterErr.Fields)
	actual, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		t.Fatalf("marshal adapter error payload: %v", marshalErr)
	}
	assertGoldenText(t, name, actual)
}

func assertGoldenDomainError(t *testing.T, name string, err error) {
	t.Helper()
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error for %s, got %v", name, err)
	}
	payload := map[string]any{"http_status": domainErr.StatusCode, "code": domainErr.ErrorCode, "detail": domainErr.Detail}
	maps.Copy(payload, domainErr.Fields)
	actual, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		t.Fatalf("marshal domain error payload: %v", marshalErr)
	}
	assertGoldenText(t, name, actual)
}
func assertGoldenText(t *testing.T, name string, actual []byte) {
	t.Helper()
	path := openAITranslationGoldenPath(name)
	if os.Getenv(updateOpenAITranslationGoldensEnv) == "1" {
		if err := os.WriteFile(path, append(bytes.TrimRight(actual, "\n"), '\n'), 0o644); err != nil {
			t.Fatalf("update golden %s: %v", path, err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	expected = bytes.TrimRight(expected, "\n")
	actual = bytes.TrimRight(actual, "\n")
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden %s mismatch\nexpected: %s\nactual:   %s", name, expected, actual)
	}
}

func openAITranslationGoldenPath(name string) string {
	return filepath.Join("testdata", "openai_translation", name)
}

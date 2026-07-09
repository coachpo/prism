package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

const updateOpenAITranslationGoldensEnv = "PRISM_UPDATE_OPENAI_TRANSLATION_GOLDENS"

func TestOpenAITranslationGoldenRequests(t *testing.T) {
	tests := []struct {
		name       string
		mode       TranslationMode
		target     string
		raw        []byte
		wantPath   string
		goldenFile string
		wantLoss   []string
		assert     func(*testing.T, map[string]any, *runtimeTranslationLossDecision)
	}{
		{
			name:       "responses request to chat upstream",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "chat-target",
			raw:        []byte(`{"model":"responses-public","instructions":"system note","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"max_output_tokens":64,"temperature":0.2}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_to_chat.json",
		},
		{
			name:       "responses reasoning request to chat upstream",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "chat-target",
			raw:        []byte(`{"model":"responses-public","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"reasoning":{"effort":"medium"},"max_output_tokens":32}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_reasoning_to_chat.json",
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				if got := stringValue(payload["reasoning_effort"]); got != "medium" {
					t.Fatalf("expected reasoning_effort=medium, got %q", got)
				}
				if got := intValue(intPointerFromAny(payload["max_tokens"])); got != 32 {
					t.Fatalf("expected max_tokens=32, got %+v", payload["max_tokens"])
				}
				if _, ok := payload["max_completion_tokens"]; ok {
					t.Fatalf("expected max_completion_tokens to be absent for non-o-series target, got %+v", payload)
				}
			},
		},
		{
			name:       "responses request to chat o series target",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "o3-mini",
			raw:        []byte(`{"model":"responses-public","input":"hello","max_output_tokens":64}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_o_series_to_chat.json",
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				if got := intValue(intPointerFromAny(payload["max_completion_tokens"])); got != 64 {
					t.Fatalf("expected max_completion_tokens=64, got %+v", payload["max_completion_tokens"])
				}
				if _, ok := payload["max_tokens"]; ok {
					t.Fatalf("expected max_tokens to be absent for o-series target, got %+v", payload)
				}
			},
		},
		{
			name:       "responses chat token alias passes when canonical absent",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "deepseek-v4-pro",
			raw:        []byte(`{"model":"responses-public","input":"hello","max_tokens":77}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_alias_to_chat.json",
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				if got := intValue(intPointerFromAny(payload["max_tokens"])); got != 77 {
					t.Fatalf("expected max_tokens=77, got %+v", payload)
				}
			},
		},
		{
			name:       "responses request passes chat fields and merges stream options",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "deepseek-v4-pro",
			raw:        []byte(`{"model":"responses-public","input":"hello","stream":true,"stream_options":{"include_usage":false,"chunking":"line"},"frequency_penalty":0.2,"presence_penalty":0.3,"logit_bias":{"42":-1},"logprobs":true,"top_logprobs":2,"n":1,"stop":["\n\n"],"response_format":{"type":"json_object"}}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_passthrough_to_chat.json",
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				for _, field := range []string{"frequency_penalty", "presence_penalty", "logit_bias", "logprobs", "top_logprobs", "n", "stop", "response_format"} {
					if _, ok := payload[field]; !ok {
						t.Fatalf("expected passthrough field %q, got %+v", field, payload)
					}
				}
				streamOptions := payload["stream_options"].(map[string]any)
				if got, _ := streamOptions["include_usage"].(bool); !got {
					t.Fatalf("expected stream_options.include_usage=true, got %+v", streamOptions)
				}
				if got := stringValue(streamOptions["chunking"]); got != "line" {
					t.Fatalf("expected stream_options.chunking=line, got %+v", streamOptions)
				}
			},
		},
		{
			name:       "responses request rich content and tools to chat upstream",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "chat-target",
			raw:        []byte(`{"model":"responses-public","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Need a tool."}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{ \"b\": 2, \"a\": 1 }"},{"type":"function_call_output","call_id":"call_1","output":{"ok":true}},{"type":"message","role":"user","content":[{"type":"input_text","text":"see this"},{"type":"input_image","image_url":"data:image/png;base64,abc"},{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}}]}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"lookup"},"parallel_tool_calls":true,"reasoning":{"effort":"high"}}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_rich_to_chat.json",
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				if payload["parallel_tool_calls"] != true || stringValue(payload["reasoning_effort"]) != "high" {
					t.Fatalf("expected tool/reasoning fields, got %+v", payload)
				}
				messages := payload["messages"].([]any)
				if len(messages) != 3 {
					t.Fatalf("expected assistant, tool, and user messages, got %+v", messages)
				}
				assistant := messages[0].(map[string]any)
				if assistant["reasoning_content"] != "Need a tool." {
					t.Fatalf("expected assistant reasoning_content, got %+v", assistant)
				}
				toolCalls := assistant["tool_calls"].([]any)
				if len(toolCalls) != 1 || toolCalls[0].(map[string]any)["function"].(map[string]any)["arguments"] != `{"a":1,"b":2}` {
					t.Fatalf("expected assistant tool call with canonical args, got %+v", assistant)
				}
				userContent := messages[2].(map[string]any)["content"].([]any)
				if len(userContent) != 3 || userContent[1].(map[string]any)["type"] != "image_url" || userContent[2].(map[string]any)["type"] != "input_audio" {
					t.Fatalf("expected rich user content, got %+v", userContent)
				}
			},
		},
		{
			name:       "responses request lossy include drop to chat upstream",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "chat-target",
			raw:        []byte(`{"model":"responses-public","input":"hello","include":["file_search_call.results"],"text":{"format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}},"verbosity":"low"},"reasoning":{"effort":"medium","encrypted_content":"opaque"},"stream":true}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_lossy_to_chat.json",
			wantLoss:   []string{"responses_include", "responses_text.verbosity", "responses_reasoning.encrypted_content"},
			assert: func(t *testing.T, payload map[string]any, loss *runtimeTranslationLossDecision) {
				t.Helper()
				if loss == nil || !loss.Lossy {
					t.Fatalf("expected lossy translation decision, got %+v", loss)
				}
				if _, ok := payload["include"]; ok {
					t.Fatalf("expected include to drop, got %+v", payload)
				}
				if _, ok := payload["text"]; ok {
					t.Fatalf("expected text object to drop, got %+v", payload)
				}
				responseFormat := payload["response_format"].(map[string]any)
				if got := stringValue(responseFormat["type"]); got != "json_schema" {
					t.Fatalf("expected json_schema response_format, got %+v", responseFormat)
				}
			},
		},
		{
			name:       "responses request drops codex metadata fields",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "chat-target",
			raw:        []byte(`{"model":"responses-public","input":[{"role":"developer","content":[{"type":"input_text","text":"system"}]},{"role":"user","content":[{"type":"input_text","text":"hello"}]}],"client_metadata":{"session_id":"session-1","thread_id":"thread-1"},"prompt_cache_key":"session-1","include":["reasoning.encrypted_content"],"text":{"verbosity":"low"},"stream":true}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_codex_to_chat.json",
			wantLoss:   []string{"responses_client_metadata", "responses_prompt_cache_key", "responses_include", "responses_text.verbosity"},
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				messages := payload["messages"].([]any)
				if got := stringValue(messages[0].(map[string]any)["role"]); got != "system" {
					t.Fatalf("expected developer message to translate to system, got %q", got)
				}
				if got := stringValue(messages[1].(map[string]any)["role"]); got != "user" {
					t.Fatalf("expected second message role user, got %q", got)
				}
				for _, field := range []string{"client_metadata", "prompt_cache_key", "include", "text"} {
					if _, ok := payload[field]; ok {
						t.Fatalf("expected %s to drop, got %+v", field, payload)
					}
				}
			},
		},
		{
			name:       "responses request records dropped tool metadata",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			target:     "chat-target",
			raw:        []byte(`{"model":"responses-public","input":"hello","tools":[{"type":"image_generation","name":"draw"}],"tool_choice":{"type":"function","name":"draw"},"parallel_tool_calls":true}`),
			wantPath:   "/v1/chat/completions",
			goldenFile: "request_responses_tools_drop_to_chat.json",
			wantLoss:   []string{"responses_tools.0", "responses_tool_choice", "responses_parallel_tool_calls"},
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				if _, ok := payload["tools"]; ok {
					t.Fatalf("expected unsupported responses tools to drop, got %+v", payload)
				}
			},
		},
		{
			name:       "chat request to responses upstream",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			target:     "responses-target",
			raw:        []byte(`{"model":"chat-public","messages":[{"role":"system","content":"system note"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"max_completion_tokens":32,"reasoning_effort":"low"}`),
			wantPath:   "/v1/responses",
			goldenFile: "request_chat_to_responses.json",
		},
		{
			name:       "chat request drops approved metadata to responses upstream",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			target:     "responses-target",
			raw:        []byte(`{"model":"chat-public","stream":true,"messages":[{"role":"system","content":"system note"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"max_completion_tokens":32,"reasoning_effort":"low","logprobs":true,"top_logprobs":2,"stream_options":{"include_usage":false},"response_format":{"type":"json_schema","json_schema":{"name":"summary","schema":{"type":"object"}}}}`),
			wantPath:   "/v1/responses",
			goldenFile: "request_chat_lossy_to_responses.json",
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				if got := stringValue(payload["instructions"]); got != "system note" {
					t.Fatalf("expected translated instructions, got %q", got)
				}
				if _, ok := payload["logprobs"]; ok {
					t.Fatalf("expected logprobs to drop, got %+v", payload)
				}
				if _, ok := payload["top_logprobs"]; ok {
					t.Fatalf("expected top_logprobs to drop, got %+v", payload)
				}
				if _, ok := payload["stream_options"]; ok {
					t.Fatalf("expected stream_options to drop, got %+v", payload)
				}
				if got, ok := payload["stream"].(bool); !ok || !got {
					t.Fatalf("expected stream=true, got %+v", payload["stream"])
				}
			},
		},
		{
			name:       "chat request tools to responses upstream",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			target:     "responses-target",
			raw:        []byte(`{"model":"chat-public","messages":[{"role":"assistant","reasoning_content":"Need lookup","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{ \"q\": \"x\" }"}}]},{"role":"tool","tool_call_id":"call_1","content":"done"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`),
			wantPath:   "/v1/responses",
			goldenFile: "request_chat_tools_to_responses.json",
			assert: func(t *testing.T, payload map[string]any, _ *runtimeTranslationLossDecision) {
				t.Helper()
				input := payload["input"].([]any)
				if input[0].(map[string]any)["type"] != "reasoning" || input[1].(map[string]any)["type"] != "function_call" || input[2].(map[string]any)["type"] != "function_call_output" {
					t.Fatalf("expected reasoning/function_call/function_call_output sequence, got %+v", input)
				}
				if payload["tools"].([]any)[0].(map[string]any)["name"] != "lookup" {
					t.Fatalf("expected responses tool definition, got %+v", payload)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, body, loss, err := translateOpenAIRequestWithLoss(test.raw, test.mode, test.target)
			if err != nil {
				t.Fatalf("translate request: %v", err)
			}
			if path != test.wantPath {
				t.Fatalf("expected path %q, got %q", test.wantPath, path)
			}
			assertGoldenJSON(t, test.goldenFile, body)
			payload := decodeTranslationTestPayload(t, body)
			assertStringSliceContainsAll(t, nilStringSlice(loss), test.wantLoss, "dropped fields")
			if test.assert != nil {
				test.assert(t, payload, loss)
			}
		})
	}
}

func TestOpenAITranslationGoldenRequestCapabilityMatrix(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	inputTokensOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses/input_tokens").Operation
	chatOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	compactOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses/compact").Operation

	t.Run("resolve wire format compatibility", func(t *testing.T) {
		tests := []struct {
			name           string
			operation      RuntimeOperation
			acceptedFormat *string
			capability     *string
			want           TranslationMode
			wantOK         bool
		}{
			{name: "chat native", operation: chatOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeNone, wantOK: true},
			{name: "responses native", operation: responsesOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone, wantOK: true},
			{name: "responses to chat", operation: responsesOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeOpenAIResponsesToChatCompletions, wantOK: true},
			{name: "chat to responses", operation: chatOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), want: TranslationModeOpenAIChatCompletionsToResponses, wantOK: true},
			{name: "dual native responses", operation: responsesOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityDualNative), capability: stringPtr(providerauth.OpenAITextCapabilityDualNative), want: TranslationModeNone, wantOK: true},
			{name: "missing accepted format", operation: responsesOperation, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone},
			{name: "missing capability", operation: responsesOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone},
			{name: "input tokens native", operation: inputTokensOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone, wantOK: true},
			{name: "input tokens chat only cannot run adjunct", operation: inputTokensOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeNone, wantOK: false},
			{name: "responses adjunct native", operation: compactOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone, wantOK: true},
			{name: "chat only cannot run responses adjunct", operation: compactOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeNone},
			{name: "responses caller rejected by chat only model", operation: responsesOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone},
			{name: "chat caller rejected by responses only model", operation: chatOperation, acceptedFormat: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeNone},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got, ok := resolveTranslationMode(test.operation, test.acceptedFormat, test.capability)
				if got != test.want || ok != test.wantOK {
					t.Fatalf("expected mode %q ok=%v, got mode %q ok=%v", test.want, test.wantOK, got, ok)
				}
			})
		}
	})

	t.Run("classify request capability", func(t *testing.T) {
		tests := []struct {
			name              string
			operation         RuntimeOperation
			mode              TranslationMode
			raw               []byte
			wantRequestClass  openAITranslationCapabilityClass
			wantResponseClass openAITranslationCapabilityClass
			wantStreamClass   openAITranslationCapabilityClass
			wantReason        string
			wantSupported     bool
		}{
			{name: "responses text request safe", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","input":"hello"}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantSupported: true},
			{name: "chat text request safe", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}]}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantSupported: true},
			{name: "chat multi choice rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"n":2}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_multi_choice"},
			{name: "chat response format rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"response_format":{"type":"text"}}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_response_format"},
			{name: "chat tool choice rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","function":{"name":"lookup"}}]}}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_tool_choice"},
			{name: "chat unknown field rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"parallel_tool_calls":true}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_unknown_field"},
			{name: "chat modalities rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"modalities":["text"]}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_modalities"},
			{name: "chat prediction rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"prediction":{"type":"content"}}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_prediction"},
			{name: "responses previous response id with residual input safe", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","input":"hello","previous_response_id":"resp_123"}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantSupported: true},
			{name: "chat structured output safe", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"response_format":{"type":"json_schema","json_schema":{"name":"summary","schema":{"type":"object"}}}}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantSupported: true},
			{name: "responses reasoning state drops with residual input safe", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","input":"hello","reasoning":{"encrypted_content":"state"}}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantSupported: true},
			{name: "responses stateful continuation without runnable input rejected", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","previous_response_id":"resp_123"}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "responses_stateful_continuation_without_runnable_input"},
			{name: "chat audio rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"audio":{"format":"wav"}}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_audio"},
			{name: "chat stream tools safe", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","stream":true,"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantSupported: true},
			{name: "responses stream tools safe", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","stream":true,"input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantSupported: true},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				capability := classifyOpenAITranslationCapability(test.operation, test.raw, test.mode)
				if capability.RequestClass != test.wantRequestClass || capability.ResponseClass != test.wantResponseClass || capability.StreamClass != test.wantStreamClass {
					t.Fatalf("expected classes request=%s response=%s stream=%s, got %+v", test.wantRequestClass, test.wantResponseClass, test.wantStreamClass, capability)
				}
				if capability.UnsupportedReason != test.wantReason {
					t.Fatalf("expected unsupported reason %q, got %+v", test.wantReason, capability)
				}
				if capability.supported() != test.wantSupported {
					t.Fatalf("expected supported=%v, got %+v", test.wantSupported, capability)
				}
				if test.wantSupported {
					return
				}
				rejection := capability.rejection()
				if rejection == nil {
					t.Fatal("expected rejected capability to produce domain error")
				}
				if rejection.StatusCode != http.StatusBadRequest || rejection.ErrorCode != openAIRequestTranslationUnsupportedErrorCode {
					t.Fatalf("expected pinned rejection contract, got %+v", rejection)
				}
				if got := stringValue(rejection.Fields["unsupported_reason"]); got != test.wantReason {
					t.Fatalf("expected rejection reason %q, got %+v", test.wantReason, rejection.Fields)
				}
			})
		}
	})
}

func TestOpenAITranslationGoldenBridgePlanning(t *testing.T) {
	tests := []struct {
		name           string
		requestPath    string
		connection     runtimeConnection
		targetModel    string
		raw            []byte
		wantTranslated bool
		wantMode       TranslationMode
		wantPath       string
		wantModel      string
		wantErr        string
		assert         func(*testing.T, codingAgentFormatPlan)
	}{
		{
			name:           "responses to chat request plan",
			requestPath:    "/v1/responses",
			connection:     runtimeConnection{OpenAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)},
			targetModel:    "chat-target-model",
			raw:            []byte(`{"model":"responses-public","input":"hello","max_output_tokens":24}`),
			wantTranslated: true,
			wantMode:       TranslationModeOpenAIResponsesToChatCompletions,
			wantPath:       "/v1/chat/completions",
			wantModel:      "chat-target-model",
		},
		{
			name:           "responses include survives bridge by dropping unsupported field",
			requestPath:    "/v1/responses",
			connection:     runtimeConnection{OpenAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)},
			targetModel:    "chat-target-model",
			raw:            []byte(`{"model":"responses-public","input":"hello","include":["file_search_call.results"]}`),
			wantTranslated: true,
			wantMode:       TranslationModeOpenAIResponsesToChatCompletions,
			wantPath:       "/v1/chat/completions",
			wantModel:      "chat-target-model",
			assert: func(t *testing.T, plan codingAgentFormatPlan) {
				t.Helper()
				payload := decodeTranslationTestPayload(t, plan.UpstreamBody)
				if _, ok := payload["include"]; ok {
					t.Fatalf("expected include to drop from translated body, got %s", string(plan.UpstreamBody))
				}
			},
		},
		{
			name:           "chat to responses request plan",
			requestPath:    "/v1/chat/completions",
			connection:     runtimeConnection{OpenAITextCapability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)},
			targetModel:    "responses-target-model",
			raw:            []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":32}`),
			wantTranslated: true,
			wantMode:       TranslationModeOpenAIChatCompletionsToResponses,
			wantPath:       "/v1/responses",
			wantModel:      "responses-target-model",
		},
		{
			name:           "bridge rejects unsupported responses text format",
			requestPath:    "/v1/responses",
			connection:     runtimeConnection{OpenAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)},
			targetModel:    "chat-target-model",
			raw:            []byte(`{"model":"responses-public","input":"hello","text":{"format":{"type":"text"}}}`),
			wantTranslated: true,
			wantErr:        "responses_text_format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			plan, translated, err := planCodingAgentFormatRequest(operation, test.raw, test.targetModel, test.connection)
			if translated != test.wantTranslated {
				t.Fatalf("expected translated=%v, got %v", test.wantTranslated, translated)
			}
			if test.wantErr != "" {
				assertDomainErrorReason(t, err, http.StatusBadRequest, openAIRequestTranslationUnsupportedErrorCode, test.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("plan coding agent format request: %v", err)
			}
			if plan.TranslationMode != test.wantMode {
				t.Fatalf("expected mode %q, got %q", test.wantMode, plan.TranslationMode)
			}
			if plan.UpstreamRequestPath != test.wantPath {
				t.Fatalf("expected path %q, got %q", test.wantPath, plan.UpstreamRequestPath)
			}
			if got := extractModelFromBody(plan.UpstreamBody); got != test.wantModel {
				t.Fatalf("expected target model %q, got %q in %s", test.wantModel, got, string(plan.UpstreamBody))
			}
			if test.assert != nil {
				test.assert(t, plan)
			}
		})
	}
}

type goldenRequestPlanModelSpec struct {
	id             int
	modelID        string
	acceptedFormat *string
}

type goldenRequestPlanRouteSpec struct {
	source   string
	target   string
	position int
}

type goldenRequestPlanConnectionSpec struct {
	modelID      string
	connectionID int
	targetID     int
	position     int
	capability   *string
}

func TestOpenAITranslationGoldenRequestPlanSelection(t *testing.T) {
	responsesOnly := providerauth.OpenAITextCapabilityResponsesOnly
	tests := []struct {
		name        string
		requestPath string
		rawBody     []byte
		models      []goldenRequestPlanModelSpec
		routes      []goldenRequestPlanRouteSpec
		connections []goldenRequestPlanConnectionSpec
		wantErr     string
		assert      func(*testing.T, requestPlan)
	}{
		{
			name:        "responses ingress translates chat only target",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"responses-public","input":"hello"}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "responses-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "responses-public", connectionID: 2821, targetID: 9821, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}, {modelID: "responses-public", connectionID: 2822, targetID: 9822, position: 1, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/chat/completions" {
					t.Fatalf("expected translated chat path, got %q", plan.EffectiveRequestPath)
				}
				if len(plan.TerminalAttempts) == 0 || plan.TerminalAttempts[0].Connection.ID != 2821 {
					t.Fatalf("expected first ordered chat-only target 2821, got %+v", plan.TerminalAttempts)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIResponsesToChatCompletions {
					t.Fatalf("expected responses-to-chat translation, got %+v", plan.TerminalAttempts[0])
				}
			},
		},
		{
			name:        "chat ingress translates responses only target",
			requestPath: "/v1/chat/completions",
			rawBody:     []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}]}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "chat-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "chat-public", connectionID: 2822, targetID: 9822, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)}, {modelID: "chat-public", connectionID: 2823, targetID: 9823, position: 1, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}, {modelID: "chat-public", connectionID: 2824, targetID: 9824, position: 2, capability: stringPtr(providerauth.OpenAITextCapabilityDualNative)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/responses" {
					t.Fatalf("expected translated responses path, got %q", plan.EffectiveRequestPath)
				}
				if len(plan.TerminalAttempts) == 0 || plan.TerminalAttempts[0].Connection.ID != 2822 {
					t.Fatalf("expected first ordered responses-only target 2822, got %+v", plan.TerminalAttempts)
				}
				if plan.SelectedTerminalTargetID == nil || *plan.SelectedTerminalTargetID != 2822 {
					t.Fatalf("expected selected terminal target 2822, got %+v", plan.SelectedTerminalTargetID)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIChatCompletionsToResponses {
					t.Fatalf("expected chat-to-responses translation, got %+v", plan.TerminalAttempts[0])
				}
			},
		},
		{
			name:        "selected dual native chat target stays native",
			requestPath: "/v1/chat/completions",
			rawBody:     []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}]}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "chat-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "chat-public", connectionID: 2825, targetID: 9825, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityDualNative)}, {modelID: "chat-public", connectionID: 2826, targetID: 9826, position: 1, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/chat/completions" {
					t.Fatalf("expected native chat path, got %q", plan.EffectiveRequestPath)
				}
				if len(plan.TerminalAttempts) == 0 || plan.TerminalAttempts[0].Connection.ID != 2825 {
					t.Fatalf("expected dual-native target 2825, got %+v", plan.TerminalAttempts)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeNone {
					t.Fatalf("expected dual-native target to stay native, got %+v", plan.TerminalAttempts[0])
				}
			},
		},
		{
			name:        "selected responses only target stays native",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"responses-public","input":"hello","text":{"format":{"type":"text"}}}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "responses-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "responses-public", connectionID: 2812, targetID: 9812, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)}, {modelID: "responses-public", connectionID: 2811, targetID: 9811, position: 1, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/responses" {
					t.Fatalf("expected native responses path, got %q", plan.EffectiveRequestPath)
				}
				if plan.TerminalAttempts[0].Connection.ID != 2812 {
					t.Fatalf("expected native responses target 2812, got %+v", plan.TerminalAttempts)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeNone {
					t.Fatalf("expected no translation, got %+v", plan.TerminalAttempts[0])
				}
			},
		},
		{
			name:        "model target policy first translated child precedes native sibling",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"responses-public","input":"hello"}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "responses-public"}, {id: 2, modelID: "chat-child"}},
			routes:      []goldenRequestPlanRouteSpec{{source: "responses-public", target: "chat-child", position: 0}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "chat-child", connectionID: 2821, targetID: 9821, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}, {modelID: "responses-public", connectionID: 2822, targetID: 9822, position: 1, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2821 {
					t.Fatalf("expected translated child terminal target 2821, got %+v", plan.TerminalAttempts)
				}
				if plan.EffectiveRequestPath != "/v1/chat/completions" {
					t.Fatalf("expected translated child path, got %q", plan.EffectiveRequestPath)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIResponsesToChatCompletions {
					t.Fatalf("expected responses-to-chat translation, got %+v", plan.TerminalAttempts[0])
				}
			},
		},
		{
			name:        "model target policy first rejects unsupported translated child before native sibling",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"}}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "responses-public"}, {id: 2, modelID: "chat-child"}},
			routes:      []goldenRequestPlanRouteSpec{{source: "responses-public", target: "chat-child", position: 0}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "chat-child", connectionID: 2821, targetID: 9821, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}, {modelID: "responses-public", connectionID: 2822, targetID: 9822, position: 1, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)}},
			wantErr:     "responses_text_format",
		},
		{
			name:        "model accepted format responses include drops for chat only fallback",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"gpt-5.4-mini","input":"hello","include":["file_search_call.results"],"text":{"format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}},"verbosity":"low"},"reasoning":{"effort":"medium","encrypted_content":"opaque"}}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "gpt-5.4-mini", acceptedFormat: &responsesOnly}, {id: 2, modelID: "deepseek-v4-pro"}},
			routes:      []goldenRequestPlanRouteSpec{{source: "gpt-5.4-mini", target: "deepseek-v4-pro", position: 0}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "deepseek-v4-pro", connectionID: 2851, targetID: 9851, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/chat/completions" {
					t.Fatalf("expected translated chat path, got %q", plan.EffectiveRequestPath)
				}
				if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2851 {
					t.Fatalf("expected deepseek chat-only target 2851, got %+v", plan.TerminalAttempts)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIResponsesToChatCompletions {
					t.Fatalf("expected responses-to-chat translation, got %+v", plan.TerminalAttempts[0])
				}
				if got := extractModelFromBody(plan.UpstreamBody); got != "deepseek-v4-pro" {
					t.Fatalf("expected translated model deepseek-v4-pro, got %q in %s", got, string(plan.UpstreamBody))
				}
				payload := decodeTranslationTestPayload(t, plan.UpstreamBody)
				if _, ok := payload["include"]; ok {
					t.Fatalf("expected include to drop, got %+v", payload)
				}
				if _, ok := payload["text"]; ok {
					t.Fatalf("expected text wrapper to drop, got %+v", payload)
				}
			},
		},
		{
			name:        "responses to chat uses selected target for max output token field",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"o3-mini","input":"hello","max_output_tokens":64}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "o3-mini", acceptedFormat: &responsesOnly}, {id: 2, modelID: "deepseek-v4-pro"}},
			routes:      []goldenRequestPlanRouteSpec{{source: "o3-mini", target: "deepseek-v4-pro", position: 0}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "deepseek-v4-pro", connectionID: 2852, targetID: 9852, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/chat/completions" {
					t.Fatalf("expected translated chat path, got %q", plan.EffectiveRequestPath)
				}
				if got := extractModelFromBody(plan.UpstreamBody); got != "deepseek-v4-pro" {
					t.Fatalf("expected translated model deepseek-v4-pro, got %q in %s", got, string(plan.UpstreamBody))
				}
				payload := decodeTranslationTestPayload(t, plan.UpstreamBody)
				if got := intValue(intPointerFromAny(payload["max_tokens"])); got != 64 {
					t.Fatalf("expected selected non-o-series target to receive max_tokens=64, got %+v", payload["max_tokens"])
				}
				if _, ok := payload["max_completion_tokens"]; ok {
					t.Fatalf("expected max_completion_tokens to be absent, got %+v", payload)
				}
			},
		},
		{
			name:        "responses unsupported tools record lossy drops",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"responses-public","input":"hello","tools":[{"type":"image_generation","name":"draw"}],"tool_choice":{"type":"function","name":"draw"},"parallel_tool_calls":true}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "responses-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "responses-public", connectionID: 2861, targetID: 9861, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				payload := decodeTranslationTestPayload(t, plan.UpstreamBody)
				if _, ok := payload["tools"]; ok {
					t.Fatalf("expected unsupported responses tools to drop, got %+v", payload)
				}
			},
		},
		{
			name:        "adapter gated responses ingress can use chat only target",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"responses-public","input":"hello"}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "responses-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "responses-public", connectionID: 2831, targetID: 9831, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/chat/completions" {
					t.Fatalf("expected translated chat path, got %q", plan.EffectiveRequestPath)
				}
				if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2831 {
					t.Fatalf("expected translated terminal target 2831, got %+v", plan.TerminalAttempts)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIResponsesToChatCompletions {
					t.Fatalf("expected responses-to-chat translation, got %+v", plan.TerminalAttempts[0])
				}
				if got := extractModelFromBody(plan.UpstreamBody); got != "responses-public" {
					t.Fatalf("expected translated body model responses-public, got %q in %s", got, string(plan.UpstreamBody))
				}
			},
		},
		{
			name:        "adapter gated chat ingress can use responses only target",
			requestPath: "/v1/chat/completions",
			rawBody:     []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":32}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "chat-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "chat-public", connectionID: 2832, targetID: 9832, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)}},
			assert: func(t *testing.T, plan requestPlan) {
				t.Helper()
				if plan.EffectiveRequestPath != "/v1/responses" {
					t.Fatalf("expected translated responses path, got %q", plan.EffectiveRequestPath)
				}
				if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2832 {
					t.Fatalf("expected translated terminal target 2832, got %+v", plan.TerminalAttempts)
				}
				if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIChatCompletionsToResponses {
					t.Fatalf("expected chat-to-responses translation, got %+v", plan.TerminalAttempts[0])
				}
				if got := extractModelFromBody(plan.UpstreamBody); got != "chat-public" {
					t.Fatalf("expected translated body model chat-public, got %q in %s", got, string(plan.UpstreamBody))
				}
			},
		},
		{
			name:        "adapter gated rejects unsupported translated openai text shape",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"responses-public","input":"hello","text":{"format":{"type":"text"}}}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "responses-public"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "responses-public", connectionID: 2841, targetID: 9841, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}},
			wantErr:     "responses_text_format",
		},
		{
			name:        "translation unsupported stays hard rejection with estimate present",
			requestPath: "/v1/responses",
			rawBody:     []byte(`{"model":"gpt-4o","input":"hello","text":{"format":{"type":"text"}}}`),
			models:      []goldenRequestPlanModelSpec{{id: 1, modelID: "gpt-4o"}},
			connections: []goldenRequestPlanConnectionSpec{{modelID: "gpt-4o", connectionID: 2991, targetID: 9991, position: 0, capability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}},
			wantErr:     "responses_text_format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newRequestPlanUnitService()
			snapshot := newGoldenRequestPlanSnapshot(test.models, test.routes, test.connections)
			request := httptest.NewRequest(http.MethodPost, test.requestPath, nil)
			operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
			plan, err := service.buildRequestPlanFromSnapshot(request, test.rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
			if test.wantErr != "" {
				assertDomainErrorReason(t, err, http.StatusBadRequest, openAIRequestTranslationUnsupportedErrorCode, test.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("build request plan: %v", err)
			}
			test.assert(t, plan)
		})
	}
}

func TestOpenAITranslationGoldenResponses(t *testing.T) {
	tests := []struct {
		name       string
		mode       TranslationMode
		requested  string
		raw        []byte
		goldenFile string
		wantUsage  responseUsage
		wantRule   runtimeUsageNormalizationRule
		assert     func(*testing.T, map[string]any)
	}{
		{
			name:       "responses upstream to chat client",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			raw:        []byte(`{"id":"resp_123","created_at":1700000000,"model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}}`),
			goldenFile: "response_responses_to_chat.json",
			wantUsage:  generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantRule:   runtimeUsageRuleOpenAIResponses,
			assert: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if got := stringValue(payload["object"]); got != "chat.completion" {
					t.Fatalf("expected chat.completion object, got %q", got)
				}
			},
		},
		{
			name:       "responses upstream to chat client with requested model",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			requested:  "chat-public",
			raw:        []byte(`{"id":"resp_123","object":"response","created_at":1700000000,"model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`),
			goldenFile: "response_responses_to_chat_requested_direct.json",
			assert: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if _, ok := payload["output"]; ok {
					t.Fatalf("expected translated chat payload to omit raw responses output, got %+v", payload)
				}
				if got := stringValue(payload["model"]); got != "chat-public" {
					t.Fatalf("expected requested public model, got %q", got)
				}
			},
		},
		{
			name:       "responses upstream to chat client ignores null error",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			requested:  "chat-public",
			raw:        []byte(`{"id":"resp_null_error","object":"response","created_at":1700000000,"model":"responses-target","error":null,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16}}`),
			goldenFile: "response_responses_to_chat_null_error.json",
			wantUsage:  generationResponseHookTestUsage(10, 6, 16),
			wantRule:   runtimeUsageRuleOpenAIResponses,
		},
		{
			name:       "chat upstream to responses client",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			requested:  "responses-public",
			raw:        []byte(`{"id":"chatcmpl_123","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`),
			goldenFile: "response_chat_to_responses_direct.json",
			wantUsage:  generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantRule:   runtimeUsageRuleOpenAIChatCompletions,
			assert: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if got := stringValue(payload["object"]); got != "response" {
					t.Fatalf("expected response object, got %q", got)
				}
			},
		},
		{
			name:       "chat upstream to responses client requested equals resolved",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			requested:  "chat-target",
			raw:        []byte(`{"id":"chatcmpl_123","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`),
			goldenFile: "response_chat_to_responses_requested_equal.json",
			wantUsage:  generationResponseHookTestUsage(10, 6, 16),
			wantRule:   runtimeUsageRuleOpenAIChatCompletions,
		},
		{
			name:       "chat upstream to responses client preserves error payload",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			requested:  "responses-public",
			raw:        []byte(`{"id":"chatcmpl_123","created":1700000001,"model":"chat-target","error":{"message":"upstream failed","type":"server_error","code":"bad_gateway"},"choices":[{"index":0,"message":{"role":"assistant","content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`),
			goldenFile: "response_chat_to_responses_error.json",
			wantUsage:  generationResponseHookTestUsage(10, 6, 16),
			wantRule:   runtimeUsageRuleOpenAIChatCompletions,
		},
		{
			name:       "responses refusal to chat client",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			raw:        []byte(`{"id":"resp_refusal","model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"I cannot help with that."}]}]}`),
			goldenFile: "response_responses_refusal_to_chat.json",
		},
		{
			name:       "chat refusal to responses client",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			requested:  "responses-public",
			raw:        []byte(`{"id":"chatcmpl_refusal","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","refusal":"I cannot help with that."},"finish_reason":"stop"}]}`),
			goldenFile: "response_chat_refusal_to_responses.json",
		},
		{
			name:       "chat response incomplete and tool output to responses client",
			mode:       TranslationModeOpenAIResponsesToChatCompletions,
			requested:  "responses-public",
			raw:        []byte(`{"id":"chatcmpl_1","created":123,"model":"chat-target","choices":[{"message":{"role":"assistant","reasoning_content":"Need lookup","content":"<think>hidden</think>visible","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{ \"b\": 2, \"a\": 1 }"}}]},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":7,"total_tokens":17,"completion_tokens_details":{"reasoning_tokens":2}}}`),
			goldenFile: "response_chat_incomplete_to_responses.json",
			wantUsage: func() responseUsage {
				usage := generationResponseHookTestUsage(10, 5, 17)
				reasoningTokens := 2
				usage.ReasoningTokens = &reasoningTokens
				return usage
			}(),
			wantRule: runtimeUsageRuleOpenAIChatCompletions,
		},
		{
			name:       "responses response reasoning refusal and tool call to chat client",
			mode:       TranslationModeOpenAIChatCompletionsToResponses,
			requested:  "chat-public",
			raw:        []byte(`{"id":"resp_1","model":"responses-target","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Need lookup"}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"},{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"No."}]}]}`),
			goldenFile: "response_responses_reasoning_to_chat.json",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, usage, usageRule, err := translateOpenAIResponse(test.raw, test.mode, test.requested)
			if err != nil {
				t.Fatalf("translate response: %v", err)
			}
			assertGoldenJSON(t, test.goldenFile, body)
			assertResponseUsageEqual(t, test.wantUsage, usage)
			if !reflect.DeepEqual(usageRule, test.wantRule) {
				t.Fatalf("expected usage rule %+v, got %+v", test.wantRule, usageRule)
			}
			if test.assert != nil {
				test.assert(t, decodeTranslationTestPayload(t, body))
			}
		})
	}
}

func TestOpenAITranslationGoldenNonStreamHooksAndMetadata(t *testing.T) {
	t.Run("translated non stream hook preserves canonical usage and raw audit", func(t *testing.T) {
		tests := []struct {
			name         string
			ingressPath  string
			mode         TranslationMode
			payload      string
			wantContains string
			wantUsage    responseUsage
		}{
			{
				name:         "responses ingress from translated chat upstream",
				ingressPath:  "/v1/responses",
				mode:         TranslationModeOpenAIResponsesToChatCompletions,
				payload:      `{"id":"chatcmpl-hook","model":"responses-target","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`,
				wantContains: `"output"`,
				wantUsage:    generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			},
			{
				name:         "chat ingress from translated responses upstream",
				ingressPath:  "/v1/chat/completions",
				mode:         TranslationModeOpenAIChatCompletionsToResponses,
				payload:      `{"id":"resp-hook","object":"response","model":"chat-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}}`,
				wantContains: `"choices"`,
				wantUsage:    generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				operation := mustResolveRuntimeOperation(t, http.MethodPost, test.ingressPath).Operation
				var forwarded bytes.Buffer
				capture, err := proxyNonEventResponseAndCaptureByOperation(operation, test.mode, &forwarded, strings.NewReader(test.payload), "application/json", fixedResponseHookTestNow, true)
				if err != nil {
					t.Fatalf("capture translated non-stream response: %v", err)
				}
				if forwarded.String() == test.payload || !strings.Contains(forwarded.String(), test.wantContains) {
					t.Fatalf("expected translated payload containing %q, got %q", test.wantContains, forwarded.String())
				}
				if string(capture.AuditBody) != test.payload {
					t.Fatalf("expected raw upstream audit body, got %q", string(capture.AuditBody))
				}
				if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
					t.Fatalf("expected canonical usage %+v, got %+v", test.wantUsage, got)
				}
			})
		}
	})

	t.Run("translated non stream tool context uses request body", func(t *testing.T) {
		operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
		connection := runtimeConnection{OpenAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly)}
		rawRequest := []byte(`{"model":"responses-public","input":"use tools","tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`)
		plan, translated, err := planCodingAgentFormatRequest(operation, rawRequest, "chat-target", connection)
		if err != nil {
			t.Fatalf("plan responses-to-chat tool request: %v", err)
		}
		if !translated || plan.ToolContext == nil {
			t.Fatalf("expected translated plan with tool context, translated=%v context=%v", translated, plan.ToolContext)
		}
		namespaceChatName := plan.ToolContext.ChatNameForResponseFunction("_search_emails", "mcp__apps__gmail")
		upstreamRaw := []byte(fmt.Sprintf(`{"id":"chatcmpl_tools","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_custom","type":"function","function":{"name":"exec","arguments":%q}},{"id":"call_namespace","type":"function","function":{"name":%q,"arguments":%q}},{"id":"call_search","type":"function","function":{"name":"tool_search","arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`, `{"input":"ls -la"}`, namespaceChatName, `{"query":"from:alerts"}`, `{"query":"gmail","limit":3}`))
		metadata := runtimeFinalResponseTranslationMetadata{RequestedModelID: "responses-public", ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient}
		var forwarded bytes.Buffer
		capture, err := proxyNonEventResponseAndCaptureForFinalAttemptWithRequestBody(metadata, rawRequest, &forwarded, bytes.NewReader(upstreamRaw), fixedResponseHookTestNow, true)
		if err != nil {
			t.Fatalf("translate chat tool response with request context: %v", err)
		}
		assertGoldenJSON(t, "response_chat_tools_to_responses.json", forwarded.Bytes())
		if got := capture.extractedUsage(); !reflect.DeepEqual(got, generationResponseHookTestUsage(12, 5, 17)) {
			t.Fatalf("expected canonical chat usage, got %+v", got)
		}
		if strings.Contains(forwarded.String(), "chat-target") {
			t.Fatalf("expected translated response not to leak resolved target model, got %s", forwarded.String())
		}
	})

	t.Run("final response translation metadata drives non stream serialization", func(t *testing.T) {
		assertFinalResponseTranslationDirectionValues(t)
		service := &Service{now: fixedResponseHookTestNow}
		connection := runtimeConnection{ID: 77, Endpoint: runtimeEndpoint{ID: 7007}}
		tests := []struct {
			name        string
			requestPath string
			requested   string
			clientOp    string
			upstreamOp  string
			upstreamReq string
			direction   runtimeFinalResponseTranslationDirection
			rawBody     []byte
			goldenFile  string
		}{
			{
				name:        "chat upstream to responses client",
				requestPath: "/v1/responses",
				requested:   "responses-public",
				clientOp:    openAIUpstreamOperationResponses,
				upstreamOp:  openAIUpstreamOperationChatCompletions,
				upstreamReq: "/v1/chat/completions",
				direction:   runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient,
				rawBody:     []byte(`{"id":"chatcmpl_meta","object":"chat.completion","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`),
				goldenFile:  "response_chat_to_responses_metadata.json",
			},
			{
				name:        "responses upstream to chat client",
				requestPath: "/v1/chat/completions",
				requested:   "chat-public",
				clientOp:    openAIUpstreamOperationChatCompletions,
				upstreamOp:  openAIUpstreamOperationResponses,
				upstreamReq: "/v1/responses",
				direction:   runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient,
				rawBody:     []byte(`{"id":"resp_meta","object":"response","created_at":1700000001,"model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16}}`),
				goldenFile:  "response_responses_to_chat_requested_metadata.json",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
				plan := requestPlan{
					RequestedModelID: test.requested,
					RuntimeOperation: operation,
					TerminalAttempts: []runtimeTerminalAttempt{{Connection: connection, TranslationMode: TranslationModeNone}},
				}
				execution := executionResult{
					Response:   &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}},
					Connection: connection,
					FinalResponseTranslation: &runtimeFinalResponseTranslationMetadata{
						TranslationMode:              TranslationModeNone,
						RequestedModelID:             test.requested,
						ClientOperationName:          test.clientOp,
						SelectedTerminalTargetID:     &connection.ID,
						UpstreamOperationName:        test.upstreamOp,
						UpstreamRequestPath:          test.upstreamReq,
						ResponseTranslationDirection: test.direction,
					},
				}
				recorder := httptest.NewRecorder()
				writer := newRuntimeDeferredCommitWriter(recorder)

				capture, err := service.writeBufferedNonStreamResponse(writer, plan, execution, *execution.FinalResponseTranslation, test.rawBody)
				if err != nil {
					t.Fatalf("write translated non-stream response: %v", err)
				}
				writer.Commit()

				assertGoldenJSON(t, test.goldenFile, recorder.Body.Bytes())
				if got := capture.extractedUsage().TotalTokens; got == nil || *got != 16 {
					t.Fatalf("expected translated capture usage, got %+v", capture.extractedUsage())
				}
			})
		}
	})

	t.Run("final response translation falls back to final attempt metadata", func(t *testing.T) {
		service := &Service{now: fixedResponseHookTestNow}
		operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
		connection := runtimeConnection{ID: 91, Endpoint: runtimeEndpoint{ID: 911}}
		plan := requestPlan{
			RequestedModelID: "chat-public",
			RuntimeOperation: operation,
			TerminalAttempts: []runtimeTerminalAttempt{{Connection: connection, TranslationMode: TranslationModeOpenAIChatCompletionsToResponses}},
		}
		execution := executionResult{
			Response:   &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}},
			Connection: connection,
			Attempts: []executionAttempt{{
				Connection:               connection,
				OperationTranslationMode: TranslationModeOpenAIChatCompletionsToResponses,
				UpstreamOperationName:    openAIUpstreamOperationResponses,
				UpstreamRequestPath:      "/v1/responses",
			}},
			FinalResponseTranslation: &runtimeFinalResponseTranslationMetadata{
				TranslationMode:              TranslationModeNone,
				RequestedModelID:             "chat-public",
				ClientOperationName:          openAIUpstreamOperationChatCompletions,
				SelectedTerminalTargetID:     &connection.ID,
				UpstreamOperationName:        openAIUpstreamOperationChatCompletions,
				UpstreamRequestPath:          "/v1/chat/completions",
				ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionNone,
			},
		}

		finalResponseTranslation := finalResponseTranslationForSerialization(plan, execution)
		recorder := httptest.NewRecorder()
		writer := newRuntimeDeferredCommitWriter(recorder)
		rawBody := []byte(`{"id":"resp_live_like","object":"response","created_at":1700000001,"model":"gpt-5.5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"translated from final attempt"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16}}`)
		capture, err := service.writeBufferedNonStreamResponse(writer, plan, execution, finalResponseTranslation, rawBody)
		if err != nil {
			t.Fatalf("write translated response from final attempt metadata: %v", err)
		}
		writer.Commit()

		payload := decodeTranslationTestPayload(t, recorder.Body.Bytes())
		if got := stringValue(payload["object"]); got != "chat.completion" {
			t.Fatalf("expected final attempt metadata to translate responses body to chat completion, got %q", got)
		}
		if got := stringValue(payload["model"]); got != "chat-public" {
			t.Fatalf("expected requested model in translated body, got %q", got)
		}
		if _, ok := payload["choices"]; !ok {
			t.Fatalf("expected translated chat response to contain choices, got %+v", payload)
		}
		if got := capture.extractedUsage().TotalTokens; got == nil || *got != 16 {
			t.Fatalf("expected translated capture usage from upstream body, got %+v", capture.extractedUsage())
		}
	})

	t.Run("translated response rejects unsupported shape", func(t *testing.T) {
		tests := []struct {
			name   string
			mode   TranslationMode
			raw    []byte
			reason string
		}{
			{name: "responses unsupported output type", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"id":"resp_bad","output":[{"type":"unknown"}]}`), reason: "responses_output_type"},
			{name: "chat empty choices", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"id":"chatcmpl_empty","choices":[]}`), reason: "chat_choices"},
			{name: "chat malformed choice", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"id":"chatcmpl_bad","choices":["bad"]}`), reason: "chat_choice"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				_, _, _, err := translateOpenAIResponse(test.raw, test.mode, "responses-public")
				assertDomainErrorReason(t, err, http.StatusBadGateway, openAIResponseTranslationUnsupportedErrorCode, test.reason)
			})
		}
	})

	t.Run("translated non stream hook rejects unsupported shape", func(t *testing.T) {
		operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
		var forwarded bytes.Buffer
		capture, err := proxyTranslatedOpenAINonEventResponseAndCapture(TranslationModeOpenAIResponsesToChatCompletions, "responses-public", &forwarded, strings.NewReader(`{"id":"chatcmpl_empty","choices":[]}`), fixedResponseHookTestNow, true)
		if !reflect.DeepEqual(capture, runtimeResponseCapture{}) {
			t.Fatalf("expected no capture on unsupported translated response, got %+v", capture)
		}
		assertDomainErrorReason(t, err, http.StatusBadGateway, openAIResponseTranslationUnsupportedErrorCode, "chat_choices")
		if forwarded.Len() != 0 || strings.TrimSpace(operation.Name) != openAIUpstreamOperationResponses {
			t.Fatalf("expected unsupported response hook to avoid forwarding bytes for %s, got %q", operation.Name, forwarded.String())
		}
	})

	t.Run("translated response headers drop unsafe entity metadata", func(t *testing.T) {
		filtered := filterTranslatedResponseHeaders(http.Header{
			"Content-Encoding": []string{"gzip"},
			"Content-Length":   []string{"999"},
			"Digest":           []string{"sha-256=raw"},
			"ETag":             []string{`"raw"`},
			"X-Request-Id":     []string{"req_translated"},
		})
		if filtered.Get("Content-Encoding") != "" || filtered.Get("Content-Length") != "" || filtered.Get("Digest") != "" || filtered.Get("ETag") != "" {
			t.Fatalf("expected unsafe entity metadata to drop, got %v", filtered)
		}
		if filtered.Get("X-Request-Id") != "req_translated" {
			t.Fatalf("expected correlation header to survive, got %v", filtered)
		}
	})
}

func TestOpenAITranslationGoldenStreams(t *testing.T) {
	tests := []struct {
		name         string
		ingressPath  string
		mode         TranslationMode
		requested    string
		stream       string
		goldenFile   string
		wantUsage    responseUsage
		wantOutcome  string
		wantErrKind  *string
		captureAudit bool
		assert       func(*testing.T, runtimeResponseCapture, string)
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
			goldenFile:   "stream_responses_to_chat.sse",
			wantUsage:    generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantOutcome:  runtimeStreamOutcomeCompleted,
			captureAudit: true,
			assert: func(t *testing.T, capture runtimeResponseCapture, body string) {
				t.Helper()
				if capture.FirstMeaningfulPayloadAt == nil || capture.CompletedAt == nil {
					t.Fatalf("expected ttft and completion timestamps, got %+v", capture)
				}
				if !strings.Contains(body, "data: [DONE]") {
					t.Fatalf("expected DONE sentinel, got %q", body)
				}
			},
		},
		{
			name:        "responses stream accepts in progress and lifecycle events",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.created\n" +
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_text_lifecycle\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
				"event: response.in_progress\n" +
				"data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_text_lifecycle\",\"model\":\"responses-target\"}}\n\n" +
				"event: response.output_item.added\n" +
				"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
				"event: response.content_part.added\n" +
				"data: {\"type\":\"response.content_part.added\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"\"}}\n\n" +
				"event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"hello lifecycle\"}\n\n" +
				"event: response.output_text.done\n" +
				"data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"text\":\"hello lifecycle\"}\n\n" +
				"event: response.content_part.done\n" +
				"data: {\"type\":\"response.content_part.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"text\",\"text\":\"hello lifecycle\"}}\n\n" +
				"event: response.output_item.done\n" +
				"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello lifecycle\"}]}}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_text_lifecycle\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello lifecycle\"}]}]}}\n\n",
			goldenFile:   "stream_responses_lifecycle_to_chat.sse",
			wantOutcome:  runtimeStreamOutcomeCompleted,
			captureAudit: true,
		},
		{
			name:        "responses stream requested model preserved",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			requested:   "responses-public",
			stream: "event: response.created\n" +
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_public_model\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
				"event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"public model\"}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_public_model\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"public model\"}]}]}}\n\n",
			goldenFile:  "stream_responses_requested_model_to_chat.sse",
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "responses stream handles failed terminal event",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			requested:   "responses-public",
			stream: "event: response.created\n" +
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_failed\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
				"event: response.failed\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failed\",\"model\":\"responses-target\",\"status\":\"failed\",\"error\":{\"message\":\"upstream failed\",\"type\":\"server_error\",\"code\":\"bad_gateway\"}}}\n\n",
			goldenFile:  "stream_responses_failed_to_chat.sse",
			wantOutcome: runtimeStreamOutcomeProviderIncomplete,
		},
		{
			name:        "responses stream accepts reasoning deltas",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			requested:   "chat-public",
			stream: "event: response.reasoning_text.delta\n" +
				"data: {\"type\":\"response.reasoning_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"delta\":\"plan\"}\n\n" +
				"event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"output_index\":1,\"item_id\":\"msg_1\",\"delta\":\"answer\"}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_reasoning\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"plan\"}]},{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}]}}\n\n",
			goldenFile:  "stream_responses_reasoning_to_chat.sse",
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "responses stream accepts function call deltas",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			requested:   "chat-public",
			stream: "event: response.created\n" +
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_tools\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
				"event: response.output_item.added\n" +
				"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"\"}}\n\n" +
				"event: response.function_call_arguments.delta\n" +
				"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"delta\":\"{\\\"q\\\":\"}\n\n" +
				"event: response.function_call_arguments.delta\n" +
				"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"delta\":\"\\\"x\\\"}\"}\n\n" +
				"event: response.output_item.done\n" +
				"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}}\n\n" +
				"event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tools\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}]}}\n\n",
			goldenFile:  "stream_responses_tools_to_chat.sse",
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "responses stream preserves incomplete classification",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"partial\"}\n\n" +
				"event: response.incomplete\n" +
				"data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_incomplete\",\"model\":\"responses-target\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"partial\"}]}]}}\n\n",
			goldenFile:  "stream_responses_incomplete_to_chat.sse",
			wantOutcome: runtimeStreamOutcomeProviderIncomplete,
			assert: func(t *testing.T, capture runtimeResponseCapture, _ string) {
				t.Helper()
				if capture.CompletedAt == nil {
					t.Fatalf("expected completed timestamp for incomplete stream, got %+v", capture)
				}
				if got := capture.extractedUsage(); !reflect.DeepEqual(got, responseUsage{}) {
					t.Fatalf("expected incomplete stream to discard usage, got %+v", got)
				}
			},
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
			goldenFile:  "stream_chat_to_responses.sse",
			wantUsage:   generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "chat stream to responses captures terminal choice usage",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			requested:   "responses-public",
			stream: "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
				"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
				"data: [DONE]\n\n",
			goldenFile:  "stream_chat_to_responses.sse",
			wantUsage:   generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "chat stream to responses requested equals resolved",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			requested:   "chat-target",
			stream: "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
				"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
				"data: [DONE]\n\n",
			goldenFile:  "stream_chat_requested_equal_to_responses.sse",
			wantUsage:   generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3),
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "chat stream accepts reasoning and think deltas",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			requested:   "responses-public",
			stream: "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"plan\"}}]}\n\n" +
				"data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"<thi\"}}]}\n\n" +
				"data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"nk>hidden</think>answer\"}}]}\n\n" +
				"data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: [DONE]\n\n",
			goldenFile:  "stream_chat_reasoning_to_responses.sse",
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "chat stream preserves error payload",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			requested:   "chat-public",
			stream: "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-public\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
				"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-public\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
				"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-public\",\"error\":{\"message\":\"upstream failed\",\"type\":\"server_error\",\"code\":\"bad_gateway\"},\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16}}\n\n" +
				"data: [DONE]\n\n",
			goldenFile:  "stream_chat_error_to_responses.sse",
			wantUsage:   generationResponseHookTestUsage(10, 6, 16),
			wantOutcome: runtimeStreamOutcomeCompleted,
		},
		{
			name:        "chat stream preserves missing terminal classification",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			requested:   "responses-public",
			stream:      "data: {\"id\":\"chatcmpl_missing_terminal\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"}}]}\n\n",
			goldenFile:  "stream_chat_missing_terminal_to_responses.sse",
			wantOutcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal,
			wantErrKind: ptr(runtimeStreamErrorKindMissingTerminalEvent),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.ingressPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, test.mode, test.requested, context.Background(), &forwarded, strings.NewReader(test.stream), fixedResponseHookTestNow, test.captureAudit)
			if err != nil {
				t.Fatalf("translate stream: %v", err)
			}
			assertGoldenText(t, test.goldenFile, forwarded.Bytes())
			if !reflect.DeepEqual(capture.extractedUsage(), test.wantUsage) {
				t.Fatalf("expected usage %+v, got %+v", test.wantUsage, capture.extractedUsage())
			}
			if capture.StreamOutcome != test.wantOutcome {
				t.Fatalf("expected outcome %q, got %+v", test.wantOutcome, capture)
			}
			if !reflect.DeepEqual(capture.StreamErrorKind, test.wantErrKind) {
				t.Fatalf("expected stream error kind %+v, got %+v", test.wantErrKind, capture.StreamErrorKind)
			}
			if test.captureAudit && string(capture.AuditBody) != test.stream {
				t.Fatalf("expected raw upstream stream audit body, got %q", string(capture.AuditBody))
			}
			if test.assert != nil {
				test.assert(t, capture, forwarded.String())
			}
		})
	}
}

func TestOpenAITranslationGoldenStreamMetadataAndLiveProxy(t *testing.T) {
	t.Run("translated stream rejects unsupported shape", func(t *testing.T) {
		tests := []struct {
			name        string
			ingressPath string
			mode        TranslationMode
			stream      string
			reason      string
		}{
			{
				name:        "responses output item done unsupported type",
				ingressPath: "/v1/chat/completions",
				mode:        TranslationModeOpenAIChatCompletionsToResponses,
				stream:      "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"img_1\",\"type\":\"image_generation_call\"}}\n\n",
				reason:      "responses_stream_output_item",
			},
			{
				name:        "responses output item done message unsupported content part",
				ingressPath: "/v1/chat/completions",
				mode:        TranslationModeOpenAIChatCompletionsToResponses,
				stream:      "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_image\",\"image_url\":\"https://example.invalid/image.png\"}]}}\n\n",
				reason:      "responses_stream_content_part_type",
			},
			{
				name:        "responses content part malformed part",
				ingressPath: "/v1/chat/completions",
				mode:        TranslationModeOpenAIChatCompletionsToResponses,
				stream:      "event: response.content_part.added\ndata: {\"type\":\"response.content_part.added\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":\"not-object\"}\n\n",
				reason:      "responses_stream_content_part",
			},
			{
				name:        "chat upstream choice missing delta and message",
				ingressPath: "/v1/responses",
				mode:        TranslationModeOpenAIResponsesToChatCompletions,
				stream:      "data: {\"id\":\"chatcmpl_bad\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0}]}\n\ndata: [DONE]\n\n",
				reason:      "chat_choice_stream",
			},
			{
				name:        "chat upstream malformed tool arguments payload",
				ingressPath: "/v1/responses",
				mode:        TranslationModeOpenAIResponsesToChatCompletions,
				stream:      "data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_custom\",\"type\":\"function\",\"function\":{\"name\":\"exec\",\"arguments\":\"{\"input\":\"ls -la\"}\"}}]}}]}\n\ndata: [DONE]\n\n",
				reason:      "chat_stream_payload",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				operation := mustResolveRuntimeOperation(t, http.MethodPost, test.ingressPath).Operation
				var forwarded bytes.Buffer
				capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, test.mode, "responses-public", context.Background(), &forwarded, strings.NewReader(test.stream), fixedResponseHookTestNow, true)
				if err == nil {
					t.Fatalf("expected unsupported stream translation error, got capture %+v and body %q", capture, forwarded.String())
				}
				assertDomainErrorReason(t, err, http.StatusBadGateway, openAIStreamTranslationUnsupportedErrorCode, test.reason)
			})
		}
	})

	t.Run("final response translation metadata drives stream serialization", func(t *testing.T) {
		service := &Service{now: fixedResponseHookTestNow}
		operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
		connection := runtimeConnection{ID: 88, Endpoint: runtimeEndpoint{ID: 8008}}
		plan := requestPlan{
			RequestedModelID:   "responses-public",
			RuntimeOperation:   operation,
			IsStreamingRequest: true,
			TerminalAttempts:   []runtimeTerminalAttempt{{Connection: connection, TranslationMode: TranslationModeNone}},
		}
		stream := "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16}}\n\n" +
			"data: [DONE]\n\n"
		execution := executionResult{
			Response:   &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))},
			Connection: connection,
			FinalResponseTranslation: &runtimeFinalResponseTranslationMetadata{
				TranslationMode:              TranslationModeNone,
				RequestedModelID:             "responses-public",
				ClientOperationName:          openAIUpstreamOperationResponses,
				SelectedTerminalTargetID:     &connection.ID,
				UpstreamOperationName:        openAIUpstreamOperationChatCompletions,
				UpstreamRequestPath:          "/v1/chat/completions",
				ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient,
			},
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		recorder := httptest.NewRecorder()

		service.writeProxyResponse(recorder, request, plan, execution, service.nowUTC())

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected translated stream status 200, got %d body %s", recorder.Code, recorder.Body.String())
		}
		assertGoldenText(t, "stream_chat_to_responses_metadata.sse", recorder.Body.Bytes())
	})

	t.Run("final response translation stream serialization uses request tool context", func(t *testing.T) {
		service := &Service{now: fixedResponseHookTestNow}
		operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
		connection := runtimeConnection{ID: 89, Endpoint: runtimeEndpoint{ID: 8009}}
		rawRequest := []byte(`{"model":"responses-public","stream":true,"input":"use tools","tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`)
		upstreamRequest := []byte("{\"messages\":[{\"content\":\"use tools\",\"role\":\"user\"}],\"model\":\"chat-target\",\"stream\":true,\"stream_options\":{\"include_usage\":true},\"tools\":[{\"type\":\"function\",\"function\":{\"name\":\"exec\",\"description\":\"Original tool definition:\\n```json\\n{\\\"name\\\":\\\"exec\\\",\\\"type\\\":\\\"custom\\\"}\\n```\",\"parameters\":{\"type\":\"object\",\"properties\":{\"input\":{\"type\":\"string\"}},\"required\":[\"input\"]}}},{\"type\":\"function\",\"function\":{\"name\":\"tool_search\",\"parameters\":{\"type\":\"object\",\"properties\":{\"query\":{\"type\":\"string\"},\"limit\":{\"type\":\"integer\"}},\"required\":[\"query\"]}}},{\"type\":\"function\",\"function\":{\"name\":\"mcp__apps__gmail___search_emails\",\"parameters\":{\"type\":\"object\"}}}]}")
		plan := requestPlan{
			RequestedModelID:   "responses-public",
			RuntimeOperation:   operation,
			UpstreamBody:       upstreamRequest,
			IsStreamingRequest: true,
			TerminalAttempts:   []runtimeTerminalAttempt{{Connection: connection, TranslationMode: TranslationModeNone}},
		}
		stream := "data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_custom\",\"type\":\"function\",\"function\":{\"name\":\"exec\",\"arguments\":\"{\\\"input\\\":\\\"ls -la\\\"}\"}},{\"index\":1,\"id\":\"call_search\",\"type\":\"function\",\"function\":{\"name\":\"tool_search\",\"arguments\":\"{\\\"query\\\":\\\"gmail\\\",\\\"limit\\\":3}\"}},{\"index\":2,\"id\":\"call_namespace\",\"type\":\"function\",\"function\":{\"name\":\"mcp__apps__gmail___search_emails\",\"arguments\":\"{\\\"query\\\":\\\"from:alerts\\\"}\"}}]}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n"
		execution := executionResult{
			Response:   &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))},
			Connection: connection,
			FinalResponseTranslation: &runtimeFinalResponseTranslationMetadata{
				TranslationMode:              TranslationModeNone,
				RequestedModelID:             "responses-public",
				ClientOperationName:          openAIUpstreamOperationResponses,
				SelectedTerminalTargetID:     &connection.ID,
				UpstreamOperationName:        openAIUpstreamOperationChatCompletions,
				UpstreamRequestPath:          "/v1/chat/completions",
				ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient,
			},
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(rawRequest)))
		recorder := httptest.NewRecorder()

		service.writeProxyResponse(recorder, request, plan, execution, service.nowUTC())

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected translated stream status 200, got %d body %s", recorder.Code, recorder.Body.String())
		}
		assertGoldenText(t, "stream_chat_tools_to_responses_service.sse", recorder.Body.Bytes())
	})

	t.Run("direct translated stream tool deltas use request context", func(t *testing.T) {
		rawRequest := []byte(`{"model":"responses-public","input":"use tools","tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`)
		stream := "data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_namespace\",\"type\":\"function\",\"function\":{\"name\":\"mcp__apps__gmail___search_emails\",\"arguments\":\"{\\\"query\\\":\\\"from:alerts\\\"}\"}},{\"index\":0,\"id\":\"call_custom\",\"type\":\"function\",\"function\":{\"name\":\"exec\",\"arguments\":\"{\\\"input\\\":\\\"ls -la\\\"}\"}},{\"index\":2,\"id\":\"call_search\",\"type\":\"function\",\"function\":{\"name\":\"tool_search\",\"arguments\":\"{\\\"query\\\":\\\"gmail\\\",\\\"limit\\\":3}\"}}]}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n"
		operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
		metadata := runtimeFinalResponseTranslationMetadata{RequestedModelID: "responses-public", ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient}
		var forwarded bytes.Buffer
		capture, err := proxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBody(operation, metadata, rawRequest, context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
		if err != nil {
			t.Fatalf("translate chat stream tool deltas: %v", err)
		}
		if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
			t.Fatalf("expected completed stream outcome, got %+v", capture)
		}
		assertGoldenText(t, "stream_chat_tools_to_responses_direct.sse", forwarded.Bytes())
	})

	t.Run("handleStreamingProxy reconstructs completed output", func(t *testing.T) {
		transport := &responsesToolsStreamRoundTripper{}
		client := &http.Client{Transport: transport}
		service := newResponsesToolsStreamService(client)
		rawRequest := `{"model":"responses-public","stream":true,"input":"use tools","tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(rawRequest))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		service.handleStreamingProxy(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected live stream status 200, got %d body %s", recorder.Code, recorder.Body.String())
		}
		if transport.calls.Load() != 1 {
			t.Fatalf("expected one upstream request, got %d", transport.calls.Load())
		}
		assertResponsesToolsUpstreamChatRequest(t, transport.requestPath, transport.requestBody, "responses-public")
		assertGoldenText(t, "stream_chat_tools_to_responses_service.sse", recorder.Body.Bytes())
	})

	t.Run("handleStreamingProxy atlas message payload reconstructs completed output", func(t *testing.T) {
		transport := &responsesToolsStreamRoundTripper{messageToolPayload: true}
		client := &http.Client{Transport: transport}
		service := newResponsesToolsStreamServiceForModel(client, "qa-stream-1781714246")
		rawRequest := `{"model":"qa-stream-1781714246","input":"use tools","stream":true,"tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(rawRequest))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		service.handleStreamingProxy(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected Atlas live stream status 200, got %d body %s", recorder.Code, recorder.Body.String())
		}
		if transport.calls.Load() != 1 {
			t.Fatalf("expected one upstream request, got %d", transport.calls.Load())
		}
		assertResponsesToolsUpstreamChatRequest(t, transport.requestPath, transport.requestBody, "qa-stream-1781714246")
		assertGoldenText(t, "stream_chat_tools_to_responses_atlas.sse", recorder.Body.Bytes())
	})

	t.Run("handleStreamingProxy rejects malformed arguments payload", func(t *testing.T) {
		transport := &responsesToolsStreamRoundTripper{malformedArgumentsPayload: true}
		client := &http.Client{Transport: transport}
		service := newResponsesToolsStreamServiceForModel(client, "qa-stream-1781714246")
		rawRequest := `{"model":"qa-stream-1781714246","input":"use tools","stream":true,"tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(rawRequest))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		service.handleStreamingProxy(recorder, request)

		if recorder.Code != http.StatusBadGateway {
			t.Fatalf("expected malformed live stream status 502, got %d body %s", recorder.Code, recorder.Body.String())
		}
		if transport.calls.Load() != 1 {
			t.Fatalf("expected one upstream request, got %d", transport.calls.Load())
		}
		assertResponsesToolsUpstreamChatRequest(t, transport.requestPath, transport.requestBody, "qa-stream-1781714246")
		if !strings.Contains(recorder.Body.String(), openAIStreamTranslationUnsupportedErrorCode) || !strings.Contains(recorder.Body.String(), "chat_stream_payload") {
			t.Fatalf("expected strict malformed stream rejection body, got %s", recorder.Body.String())
		}
	})
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
		Body:             []byte(`{"id":"chatcmpl_empty","choices":[]}`),
		TranslationMode:  provider.TranslationModeOpenAIResponsesToChatCompletions,
		RequestedModelID: "responses-public",
		Operation:        provider.Operation{APIFamily: provider.APIFamilyOpenAI},
	})
	assertGoldenAdapterError(t, "rejected_response_chat_tool_calls.json", responseErr)

	tests := []struct {
		name   string
		mode   TranslationMode
		raw    []byte
		reason string
	}{
		{name: "chat multi choice", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"n":2}`), reason: "chat_multi_choice"},
		{name: "chat response format", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"response_format":{"type":"text"}}`), reason: "chat_response_format"},
		{name: "chat tool choice", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":{"type":"custom","name":"lookup"}}`), reason: "chat_tool_choice"},
		{name: "chat unknown field", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"functions":[{"name":"legacy"}]}`), reason: "chat_unknown_field"},
		{name: "responses stateful continuation without input", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","previous_response_id":"resp_123"}`), reason: "responses_stateful_continuation_without_runnable_input"},
		{name: "chat audio", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"audio":{"format":"wav"}}`), reason: "chat_audio"},
		{name: "chat modalities", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"modalities":["text"]}`), reason: "chat_modalities"},
		{name: "chat prediction", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"prediction":{"type":"content"}}`), reason: "chat_prediction"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := translateOpenAIRequest(test.raw, test.mode, "target-model")
			assertDomainErrorReason(t, err, http.StatusBadRequest, openAIRequestTranslationUnsupportedErrorCode, test.reason)
		})
	}
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

func newGoldenRequestPlanSnapshot(models []goldenRequestPlanModelSpec, routes []goldenRequestPlanRouteSpec, connections []goldenRequestPlanConnectionSpec) *planningSnapshot {
	records := make([]runtimeModelRecord, 0, len(models))
	for _, model := range models {
		records = append(records, runtimeModelRecord{
			ID:                   model.id,
			APIFamily:            "openai",
			ModelID:              model.modelID,
			OpenAIAcceptedFormat: model.acceptedFormat,
		})
	}
	snapshot := newRequestPlanSnapshot(records...)
	for _, model := range models {
		record := snapshot.ModelsByID[model.modelID]
		snapshot.AccessTargetsBySourceModelID[record.ID] = nil
	}
	for _, route := range routes {
		addRequestPlanModelTargetAtPosition(snapshot, route.source, route.target, route.position)
	}
	for _, connection := range connections {
		model := snapshot.ModelsByID[connection.modelID]
		addRequestPlanConnectionTargetWithOptions(snapshot, model, connection.connectionID, connection.targetID, connection.position, requestPlanConnectionTargetOptions{
			openAITextCapability: connection.capability,
		})
	}
	return snapshot
}

func decodeTranslationTestPayload(t *testing.T, rawBody []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode translated payload: %v", err)
	}
	return payload
}

type translatedSSEEvent struct {
	event   string
	payload map[string]any
}

func parseTranslatedSSEEvents(t *testing.T, body string) []translatedSSEEvent {
	t.Helper()
	chunks := strings.Split(body, "\n\n")
	events := make([]translatedSSEEvent, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var eventName string
		var dataLines []string
		for _, line := range strings.Split(chunk, "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if len(dataLines) == 0 {
			continue
		}
		data := strings.Join(dataLines, "\n")
		if strings.TrimSpace(data) == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("decode translated SSE event %q payload: %v", eventName, err)
		}
		events = append(events, translatedSSEEvent{event: eventName, payload: payload})
	}
	return events
}

func translatedSSEEventPayload(t *testing.T, events []translatedSSEEvent, eventName string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event.event == eventName {
			return event.payload
		}
	}
	t.Fatalf("expected translated SSE event %q, got %+v", eventName, events)
	return nil
}

func assertResponsesToolsUpstreamChatRequest(t *testing.T, requestPath string, requestBody []byte, expectedModel string) {
	t.Helper()
	if requestPath != "/v1/chat/completions" {
		t.Fatalf("expected upstream chat completions path, got %q", requestPath)
	}
	payload := decodeTranslationTestPayload(t, requestBody)
	if payload["stream"] != true || stringValue(payload["model"]) != expectedModel {
		t.Fatalf("expected translated streaming chat request, got %+v", payload)
	}
	if includeUsage := nestedValue(payload, "stream_options", "include_usage"); includeUsage != true {
		t.Fatalf("expected stream_options.include_usage=true, got %+v body %s", includeUsage, string(requestBody))
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 3 {
		t.Fatalf("expected three translated chat tools, got %+v", payload["tools"])
	}
	toolNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolPayload, ok := tool.(map[string]any)
		if !ok {
			t.Fatalf("expected translated tool object, got %+v", tool)
		}
		toolNames = append(toolNames, stringValue(nestedValue(toolPayload, "function", "name")))
	}
	if strings.Join(toolNames, ",") != "exec,tool_search,mcp__apps__gmail___search_emails" {
		t.Fatalf("expected translated tool names, got %+v body %s", toolNames, string(requestBody))
	}
}

type responsesToolsStreamRoundTripper struct {
	calls                     atomic.Int32
	requestPath               string
	requestBody               []byte
	messageToolPayload        bool
	malformedArgumentsPayload bool
}

func (transport *responsesToolsStreamRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	transport.requestPath = request.URL.Path
	if request.Body != nil {
		body, _ := io.ReadAll(request.Body)
		transport.requestBody = body
	}
	toolPayloadKey := "delta"
	if transport.messageToolPayload {
		toolPayloadKey = "message"
	}
	arguments := map[string]string{
		"custom":    `"{\"input\":\"ls -la\"}"`,
		"search":    `"{\"query\":\"gmail\",\"limit\":3}"`,
		"namespace": `"{\"query\":\"from:alerts\"}"`,
	}
	if transport.malformedArgumentsPayload {
		arguments = map[string]string{
			"custom":    `"{"input":"ls -la"}"`,
			"search":    `"{"query":"gmail","limit":3}"`,
			"namespace": `"{"query":"from:alerts"}"`,
		}
	}
	stream := "data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"" + toolPayloadKey + "\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_custom\",\"type\":\"function\",\"function\":{\"name\":\"exec\",\"arguments\":" + arguments["custom"] + "}},{\"index\":1,\"id\":\"call_search\",\"type\":\"function\",\"function\":{\"name\":\"tool_search\",\"arguments\":" + arguments["search"] + "}},{\"index\":2,\"id\":\"call_namespace\",\"type\":\"function\",\"function\":{\"name\":\"mcp__apps__gmail___search_emails\",\"arguments\":" + arguments["namespace"] + "}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}, nil
}

func newResponsesToolsStreamService(client *http.Client) *Service {
	return newResponsesToolsStreamServiceForModel(client, "responses-public")
}

func newResponsesToolsStreamServiceForModel(client *http.Client, modelID string) *Service {
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: modelID})
	model := snapshot.ModelsByID[modelID]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2891, 9891, 0, requestPlanConnectionTargetOptions{
		openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly),
	})
	connection := snapshot.TerminalTargetsByID[2891]
	connection.Endpoint.BaseURL = "https://upstream.example"
	connection.UpstreamAuth = &runtimeConnectionUpstreamAuthSnapshot{
		AuthHeader:            "Authorization",
		AuthValue:             "Bearer redacted",
		ControlledHeaderNames: map[string]struct{}{"authorization": {}},
	}
	snapshot.TerminalTargetsByID[2891] = connection
	cache := NewSharedCache()
	cache.published.Store(&publishedRuntimeSnapshot{
		Generation:    1,
		PublishedAt:   time.Unix(1_700_000_000, 0).UTC(),
		ActiveProfile: profiledomain.Profile{ID: requestPlanTestProfileID, Name: "runtime-test", IsActive: true},
		PlanningByProfileID: map[int]*planningSnapshot{
			requestPlanTestProfileID: snapshot,
		},
	})
	return &Service{
		httpClient:                   client,
		staticRuntimeProxyConfig:     RuntimeProxyConfigSnapshot{HTTPClient: client},
		cache:                        cache,
		runtimeState:                 loadbalancedomain.NewLocalRuntimeStateStore(),
		requireDurableSuccessHandoff: false,
		now:                          fixedResponseHookTestNow,
	}
}

func nilStringSlice(loss *runtimeTranslationLossDecision) []string {
	if loss == nil {
		return nil
	}
	return loss.DroppedFields
}

func assertStringSliceContainsAll(t *testing.T, got []string, want []string, label string) {
	t.Helper()
	if len(want) == 0 {
		return
	}
	values := map[string]struct{}{}
	for _, value := range got {
		values[value] = struct{}{}
	}
	for _, value := range want {
		if _, ok := values[value]; !ok {
			t.Fatalf("expected %s to contain %q, got %+v", label, value, got)
		}
	}
}

func assertDomainErrorReason(t *testing.T, err error, status int, code string, reason string) {
	t.Helper()
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != status || domainErr.ErrorCode != code {
		t.Fatalf("expected status=%d code=%q, got %+v", status, code, domainErr)
	}
	if reason != "" {
		if got := stringValue(domainErr.Fields["unsupported_reason"]); got != reason {
			t.Fatalf("expected unsupported reason %q, got %+v", reason, domainErr.Fields)
		}
	}
}

func assertResponseUsageEqual(t *testing.T, want responseUsage, got responseUsage) {
	t.Helper()
	gotView := []int{
		intValue(got.InputTokens),
		intValue(got.OutputTokens),
		intValue(got.TotalTokens),
		intValue(got.CacheReadInputTokens),
		intValue(got.CacheCreationInputTokens),
		intValue(got.ReasoningTokens),
	}
	wantView := []int{
		intValue(want.InputTokens),
		intValue(want.OutputTokens),
		intValue(want.TotalTokens),
		intValue(want.CacheReadInputTokens),
		intValue(want.CacheCreationInputTokens),
		intValue(want.ReasoningTokens),
	}
	if !reflect.DeepEqual(wantView, gotView) || want.discarded != got.discarded {
		t.Fatalf("expected usage ints %+v discarded=%v, got %+v discarded=%v", wantView, want.discarded, gotView, got.discarded)
	}
}

func assertFinalResponseTranslationDirectionValues(t *testing.T) {
	t.Helper()
	values := map[runtimeFinalResponseTranslationDirection]string{
		runtimeFinalResponseTranslationDirectionNone:                          "none",
		runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient: "responses_upstream_to_chat_client",
		runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient: "chat_upstream_to_responses_client",
	}
	for direction, expected := range values {
		if string(direction) != expected {
			t.Fatalf("expected final response translation direction %q, got %q", expected, direction)
		}
	}
}

func ptr[T any](value T) *T {
	return &value
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

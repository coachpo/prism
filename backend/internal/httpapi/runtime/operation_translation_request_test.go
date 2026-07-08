package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coachpo/prism/backend/internal/providercompat"
)

func TestResolveOpenAIWireFormatCompatibilityMatrix(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	inputTokensOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses/input_tokens").Operation
	chatOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	compactOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses/compact").Operation
	tests := []struct {
		name           string
		operation      RuntimeOperation
		acceptedFormat *string
		capability     *string
		want           TranslationMode
		wantOK         bool
	}{
		{name: "chat native", operation: chatOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly), capability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeNone, wantOK: true},
		{name: "responses native", operation: responsesOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone, wantOK: true},
		{name: "responses to chat", operation: responsesOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeOpenAIResponsesToChatCompletions, wantOK: true},
		{name: "chat to responses", operation: chatOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly), capability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), want: TranslationModeOpenAIChatCompletionsToResponses, wantOK: true},
		{name: "dual native responses", operation: responsesOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityDualNative), capability: stringPtr(providercompat.OpenAITextCapabilityDualNative), want: TranslationModeNone, wantOK: true},
		{name: "missing accepted format", operation: responsesOperation, capability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone},
		{name: "missing capability", operation: responsesOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone},
		{name: "input tokens native", operation: inputTokensOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone, wantOK: true},
		{name: "input tokens chat only cannot run adjunct", operation: inputTokensOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeNone, wantOK: false},
		{name: "responses adjunct native", operation: compactOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), want: TranslationModeNone, wantOK: true},
		{name: "chat only cannot run responses adjunct", operation: compactOperation, acceptedFormat: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly), capability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly), want: TranslationModeNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := resolveTranslationMode(test.operation, test.acceptedFormat, test.capability)
			if got != test.want || ok != test.wantOK {
				t.Fatalf("expected mode %q ok=%v, got mode %q ok=%v", test.want, test.wantOK, got, ok)
			}
		})
	}
}

func TestResolveOpenAIWireFormatRejectsModelContractMismatch(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	chatOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	tests := []struct {
		name           string
		operation      RuntimeOperation
		acceptedFormat string
		capability     string
	}{
		{name: "responses caller rejected by chat-only model", operation: responsesOperation, acceptedFormat: providercompat.OpenAITextCapabilityChatCompletionsOnly, capability: providercompat.OpenAITextCapabilityResponsesOnly},
		{name: "chat caller rejected by responses-only model", operation: chatOperation, acceptedFormat: providercompat.OpenAITextCapabilityResponsesOnly, capability: providercompat.OpenAITextCapabilityChatCompletionsOnly},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := resolveTranslationMode(test.operation, stringPtr(test.acceptedFormat), stringPtr(test.capability))
			if got != TranslationModeNone || ok {
				t.Fatalf("expected model contract mismatch rejection, got mode %q ok=%v", got, ok)
			}
		})
	}
}

func TestClassifyOpenAITranslationCapability(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	chatOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
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
			if !test.wantSupported {
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
			}
		})
	}
}

func TestTranslateOpenAIResponsesToChatRequest(t *testing.T) {
	rawBody := []byte(`{"model":"responses-public","instructions":"system note","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"max_output_tokens":64,"temperature":0.2}`)

	path, translated, err := translateOpenAIRequest(rawBody, TranslationModeOpenAIResponsesToChatCompletions, "chat-target")
	if err != nil {
		t.Fatalf("translate responses to chat request: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("expected translated path /v1/chat/completions, got %q", path)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["model"]); got != "chat-target" {
		t.Fatalf("expected translated model chat-target, got %q", got)
	}
	if got := stringValue(payload["reasoning_effort"]); got != "" {
		t.Fatalf("expected no reasoning effort in base translation test, got %q", got)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected system and user chat messages, got %+v", payload["messages"])
	}
	if got := stringValue(messages[0].(map[string]any)["role"]); got != "system" {
		t.Fatalf("expected leading system message, got %q", got)
	}
	if got := stringValue(messages[1].(map[string]any)["role"]); got != "user" {
		t.Fatalf("expected user message, got %q", got)
	}
	if got := stringValue(messages[1].(map[string]any)["content"]); got != "hello" {
		t.Fatalf("expected translated user text hello, got %q", got)
	}
}

func TestTranslateOpenAIResponsesReasoningRequest(t *testing.T) {
	rawBody := []byte(`{"model":"responses-public","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"reasoning":{"effort":"medium"},"max_output_tokens":32}`)

	_, translated, err := translateOpenAIRequest(rawBody, TranslationModeOpenAIResponsesToChatCompletions, "chat-target")
	if err != nil {
		t.Fatalf("translate responses reasoning request: %v", err)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["reasoning_effort"]); got != "medium" {
		t.Fatalf("expected reasoning_effort=medium, got %q", got)
	}
	if got := intValue(intPointerFromAny(payload["max_tokens"])); got != 32 {
		t.Fatalf("expected max_tokens=32, got %+v", payload["max_tokens"])
	}
	if _, ok := payload["max_completion_tokens"]; ok {
		t.Fatalf("expected max_completion_tokens to be absent for non-o-series target, got %s", string(translated))
	}
}

func TestTranslateResponsesToChatDropsInclude(t *testing.T) {
	rawBody := []byte(`{"model":"responses-public","input":"hello","include":["file_search_call.results"],"text":{"format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}},"verbosity":"low"},"reasoning":{"effort":"medium","encrypted_content":"opaque"},"stream":true}`)

	path, translated, err := translateOpenAIRequest(rawBody, TranslationModeOpenAIResponsesToChatCompletions, "chat-target")
	if err != nil {
		t.Fatalf("translate lossy responses request: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("expected translated path /v1/chat/completions, got %q", path)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if _, ok := payload["include"]; ok {
		t.Fatalf("expected include to drop, got %s", string(translated))
	}
	if _, ok := payload["text"]; ok {
		t.Fatalf("expected text object to drop after mapping format, got %s", string(translated))
	}
	if got := stringValue(payload["reasoning_effort"]); got != "medium" {
		t.Fatalf("expected reasoning_effort=medium, got %q", got)
	}
	responseFormat := payload["response_format"].(map[string]any)
	if got := stringValue(responseFormat["type"]); got != "json_schema" {
		t.Fatalf("expected json_schema response_format, got %+v", responseFormat)
	}
	streamOptions := payload["stream_options"].(map[string]any)
	if got, _ := streamOptions["include_usage"].(bool); !got {
		t.Fatalf("expected stream_options.include_usage=true, got %+v", streamOptions)
	}
}

func TestTranslateResponsesToChatRejectsStatefulContinuationWithoutRunnableInput(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "previous response only", raw: []byte(`{"model":"responses-public","previous_response_id":"resp_123"}`)},
		{name: "conversation only", raw: []byte(`{"model":"responses-public","conversation":"conv_123"}`)},
		{name: "state with empty input", raw: []byte(`{"model":"responses-public","previous_response_id":"resp_123","input":[]}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := translateOpenAIRequest(test.raw, TranslationModeOpenAIResponsesToChatCompletions, "chat-target")
			var domainErr *domainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("expected domain error, got %v", err)
			}
			if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "responses_stateful_continuation_without_runnable_input" {
				t.Fatalf("expected continuity rejection, got %+v", domainErr.Fields)
			}
		})
	}
}

func TestTranslateResponsesToChatRejectsUnsupportedTextFormat(t *testing.T) {
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":{"type":"text"}}}`)

	_, _, err := translateOpenAIRequest(rawBody, TranslationModeOpenAIResponsesToChatCompletions, "chat-target")
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "responses_text_format" {
		t.Fatalf("expected responses_text_format, got %+v", domainErr.Fields)
	}
}

func TestTranslateChatToResponsesDropsApprovedMetadata(t *testing.T) {
	rawBody := []byte(`{"model":"chat-public","stream":true,"messages":[{"role":"system","content":"system note"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"max_completion_tokens":32,"reasoning_effort":"low","logprobs":true,"top_logprobs":2,"stream_options":{"include_usage":false},"response_format":{"type":"json_schema","json_schema":{"name":"summary","schema":{"type":"object"}}}}`)

	path, translated, err := translateOpenAIRequest(rawBody, TranslationModeOpenAIChatCompletionsToResponses, "responses-target")
	if err != nil {
		t.Fatalf("translate chat to responses request: %v", err)
	}
	if path != "/v1/responses" {
		t.Fatalf("expected translated path /v1/responses, got %q", path)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["model"]); got != "responses-target" {
		t.Fatalf("expected translated model responses-target, got %q", got)
	}
	if got := stringValue(payload["instructions"]); got != "system note" {
		t.Fatalf("expected translated instructions, got %q", got)
	}
	if got := intValue(intPointerFromAny(payload["max_output_tokens"])); got != 32 {
		t.Fatalf("expected max_output_tokens=32, got %+v", payload["max_output_tokens"])
	}
	if got := stringValue(payload["reasoning"].(map[string]any)["effort"]); got != "low" {
		t.Fatalf("expected reasoning.effort=low, got %q", got)
	}
	if _, ok := payload["logprobs"]; ok {
		t.Fatalf("expected logprobs to drop, got %s", string(translated))
	}
	if _, ok := payload["top_logprobs"]; ok {
		t.Fatalf("expected top_logprobs to drop, got %s", string(translated))
	}
	if _, ok := payload["stream_options"]; ok {
		t.Fatalf("expected stream_options to drop, got %s", string(translated))
	}
	if got, ok := payload["stream"].(bool); !ok || !got {
		t.Fatalf("expected stream to pass through, got %+v", payload["stream"])
	}
	input := payload["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("expected one translated user message, got %+v", input)
	}
	if got := stringValue(input[0].(map[string]any)["type"]); got != "message" {
		t.Fatalf("expected translated message item, got %q", got)
	}
	responseFormat := payload["response_format"].(map[string]any)
	if got := stringValue(responseFormat["type"]); got != "json_schema" {
		t.Fatalf("expected json_schema response_format, got %+v", responseFormat)
	}
}

func TestTranslateOpenAIRequestRejectsUnsupportedShape(t *testing.T) {
	tests := []struct {
		name   string
		mode   TranslationMode
		raw    []byte
		reason string
	}{
		{name: "chat multi-choice", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"n":2}`), reason: "chat_multi_choice"},
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
			var domainErr *domainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("expected domain error, got %v", err)
			}
			if domainErr.StatusCode != http.StatusBadRequest || domainErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode || domainErr.Detail != openAIRequestTranslationUnsupportedDetail {
				t.Fatalf("expected pinned translation 400 contract, got %+v", domainErr)
			}
			if got := stringValue(domainErr.Fields["unsupported_reason"]); got != test.reason {
				t.Fatalf("expected unsupported reason %q, got %+v", test.reason, domainErr.Fields)
			}
		})
	}
}

func TestPlanCodingAgentFormatRequestResponsesToChat(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	connection := runtimeConnection{
		OpenAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly),
	}
	rawBody := []byte(`{"model":"responses-public","input":"hello","max_output_tokens":24}`)

	plan, translated, err := planCodingAgentFormatRequest(operation, rawBody, "chat-target-model", connection)
	if err != nil {
		t.Fatalf("plan bridge responses-to-chat request: %v", err)
	}
	if !translated {
		t.Fatal("expected bridge to translate responses request for chat target")
	}
	if plan.TranslationMode != TranslationModeOpenAIResponsesToChatCompletions {
		t.Fatalf("expected responses-to-chat mode, got %q", plan.TranslationMode)
	}
	if plan.UpstreamRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected translated path /v1/chat/completions, got %q", plan.UpstreamRequestPath)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "chat-target-model" {
		t.Fatalf("expected translated target model chat-target-model, got %q", got)
	}
}

func TestPlanCodingAgentFormatAllowsResponsesIncludeForChatOnlyTarget(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	connection := runtimeConnection{
		OpenAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly),
	}
	rawBody := []byte(`{"model":"responses-public","input":"hello","include":["file_search_call.results"]}`)

	plan, translated, err := planCodingAgentFormatRequest(operation, rawBody, "chat-target-model", connection)
	if err != nil {
		t.Fatalf("plan bridge responses include request: %v", err)
	}
	if !translated {
		t.Fatal("expected bridge to identify translation target")
	}
	payload := decodeTranslationTestPayload(t, plan.UpstreamBody)
	if _, ok := payload["include"]; ok {
		t.Fatalf("expected include to drop from translated body, got %s", string(plan.UpstreamBody))
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "chat-target-model" {
		t.Fatalf("expected translated target model chat-target-model, got %q", got)
	}
}

func TestPlanCodingAgentFormatRequestChatToResponses(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	connection := runtimeConnection{
		OpenAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly),
	}
	rawBody := []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":32}`)

	plan, translated, err := planCodingAgentFormatRequest(operation, rawBody, "responses-target-model", connection)
	if err != nil {
		t.Fatalf("plan bridge chat-to-responses request: %v", err)
	}
	if !translated {
		t.Fatal("expected bridge to translate chat request for responses target")
	}
	if plan.TranslationMode != TranslationModeOpenAIChatCompletionsToResponses {
		t.Fatalf("expected chat-to-responses mode, got %q", plan.TranslationMode)
	}
	if plan.UpstreamRequestPath != "/v1/responses" {
		t.Fatalf("expected translated path /v1/responses, got %q", plan.UpstreamRequestPath)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "responses-target-model" {
		t.Fatalf("expected translated target model responses-target-model, got %q", got)
	}
}

func TestPlanCodingAgentFormatRequestRejectsUnsupportedShape(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	connection := runtimeConnection{
		OpenAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly),
	}
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":{"type":"text"}}}`)

	_, translated, err := planCodingAgentFormatRequest(operation, rawBody, "chat-target-model", connection)
	if !translated {
		t.Fatal("expected bridge to identify translation target")
	}
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != http.StatusBadRequest || domainErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode || domainErr.Detail != openAIRequestTranslationUnsupportedDetail {
		t.Fatalf("expected pinned translation 400 contract, got %+v", domainErr)
	}
	if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "responses_text_format" {
		t.Fatalf("expected unsupported reason responses_text_format, got %+v", domainErr.Fields)
	}
}

func TestBuildRequestPlan_ResponsesIngressTranslatesChatOnlyTarget(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_821, 9_821, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_822, 9_822, 1, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello"}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build translated responses request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected responses-to-chat path from first ordered target, got path=%q attempts=%+v", plan.EffectiveRequestPath, plan.TerminalAttempts)
	}
	if len(plan.TerminalAttempts) == 0 || plan.TerminalAttempts[0].Connection.ID != 2_821 {
		t.Fatalf("expected first ordered chat-only target 2821, got %+v", plan.TerminalAttempts)
	}
	if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIResponsesToChatCompletions {
		t.Fatalf("expected responses-to-chat translation, got path=%q attempts=%+v", plan.EffectiveRequestPath, plan.TerminalAttempts)
	}
}

func TestBuildRequestPlan_ChatIngressTranslatesResponsesOnlyTarget(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "chat-public"})
	model := snapshot.ModelsByID["chat-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_822, 9_822, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly)})
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_823, 9_823, 1, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_824, 9_824, 2, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityDualNative)})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}]}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build translated chat request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/responses" {
		t.Fatalf("expected chat-to-responses path from first ordered target, got path=%q attempts=%+v", plan.EffectiveRequestPath, plan.TerminalAttempts)
	}
	if len(plan.TerminalAttempts) == 0 || plan.TerminalAttempts[0].Connection.ID != 2_822 {
		t.Fatalf("expected first ordered responses-only target 2822, got %+v", plan.TerminalAttempts)
	}
	if plan.SelectedTerminalTargetID == nil || *plan.SelectedTerminalTargetID != 2_822 {
		t.Fatalf("expected selected terminal target 2822, got %+v", plan.SelectedTerminalTargetID)
	}
	if plan.TerminalAttempts[0].TranslationMode != TranslationModeOpenAIChatCompletionsToResponses {
		t.Fatalf("expected chat-to-responses translation, got path=%q attempts=%+v", plan.EffectiveRequestPath, plan.TerminalAttempts)
	}
}

func TestBuildRequestPlan_SelectedDualNativeChatTargetStaysNative(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "chat-public"})
	model := snapshot.ModelsByID["chat-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_825, 9_825, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityDualNative)})
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_826, 9_826, 1, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}]}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build native dual chat request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected native chat path, got %q", plan.EffectiveRequestPath)
	}
	if len(plan.TerminalAttempts) == 0 || plan.TerminalAttempts[0].Connection.ID != 2_825 {
		t.Fatalf("expected first ordered dual-native target 2825, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeNone {
		t.Fatalf("expected selected dual-native chat target to stay native, got %q", got)
	}
}

func TestBuildRequestPlan_SelectedResponsesOnlyTargetStaysNative(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_812, 9_812, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly)})
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_811, 9_811, 1, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":{"type":"text"}}}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build selected responses-only request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/responses" {
		t.Fatalf("expected native responses path, got %q", plan.EffectiveRequestPath)
	}
	if plan.TerminalAttempts[0].Connection.ID != 2_812 {
		t.Fatalf("expected native responses-capable connection 2812, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeNone {
		t.Fatalf("expected selected responses-only target to use translation mode none, got %q", got)
	}
}

func TestBuildRequestPlan_ModelTargetPolicyFirstTranslatedChildPrecedesNativeOpenAISibling(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "chat-child"},
	)
	public := snapshot.ModelsByID["responses-public"]
	child := snapshot.ModelsByID["chat-child"]
	snapshot.AccessTargetsBySourceModelID[public.ID] = nil
	snapshot.AccessTargetsBySourceModelID[child.ID] = nil
	addRequestPlanModelTargetAtPosition(snapshot, "responses-public", "chat-child", 0)
	addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_821, 9_821, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	addRequestPlanConnectionTargetWithOptions(snapshot, public, 2_822, 9_822, 1, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello"}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build policy-first translated child request plan: %v", err)
	}
	if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2_821 {
		t.Fatalf("expected first policy child terminal target 2821, got %+v", plan.TerminalAttempts)
	}
	if got := plan.EffectiveRequestPath; got != "/v1/chat/completions" {
		t.Fatalf("expected translated child path /v1/chat/completions, got %q", got)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeOpenAIResponsesToChatCompletions {
		t.Fatalf("expected responses-to-chat translation, got %q", got)
	}
}

func TestBuildRequestPlan_ModelTargetPolicyFirstRejectsUnsupportedTranslatedChildBeforeNativeSibling(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "chat-child"},
	)
	public := snapshot.ModelsByID["responses-public"]
	child := snapshot.ModelsByID["chat-child"]
	snapshot.AccessTargetsBySourceModelID[public.ID] = nil
	snapshot.AccessTargetsBySourceModelID[child.ID] = nil
	addRequestPlanModelTargetAtPosition(snapshot, "responses-public", "chat-child", 0)
	addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_821, 9_821, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	addRequestPlanConnectionTargetWithOptions(snapshot, public, 2_822, 9_822, 1, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"}}`)

	_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected selected translated child to hard-reject unsupported shape, got %v", err)
	}
	if domainErr.StatusCode != http.StatusBadRequest || domainErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode || domainErr.Detail != openAIRequestTranslationUnsupportedDetail {
		t.Fatalf("expected pinned translation unsupported error, got %+v", domainErr)
	}
	if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "responses_text_format" {
		t.Fatalf("expected unsupported reason responses_text_format, got %+v", domainErr.Fields)
	}
}

func TestBuildRequestPlan_ModelAcceptedFormatResponsesIncludeDropsForChatOnlyFallback(t *testing.T) {
	service := newRequestPlanUnitService()
	responsesOnly := providercompat.OpenAITextCapabilityResponsesOnly
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-5.4-mini", OpenAIAcceptedFormat: &responsesOnly},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "deepseek-v4-pro"},
	)
	source := snapshot.ModelsByID["gpt-5.4-mini"]
	target := snapshot.ModelsByID["deepseek-v4-pro"]
	snapshot.AccessTargetsBySourceModelID[source.ID] = nil
	snapshot.AccessTargetsBySourceModelID[target.ID] = nil
	addRequestPlanModelTargetAtPosition(snapshot, source.ModelID, target.ModelID, 0)
	addRequestPlanConnectionTargetWithOptions(snapshot, target, 2_851, 9_851, 0, requestPlanConnectionTargetOptions{
		openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly),
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"gpt-5.4-mini","input":"hello","include":["file_search_call.results"],"text":{"format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}},"verbosity":"low"},"reasoning":{"effort":"medium","encrypted_content":"opaque"}}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build deployed responses include fallback request plan: %v", err)
	}
	if got := plan.EffectiveRequestPath; got != "/v1/chat/completions" {
		t.Fatalf("expected translated Chat path, got %q", got)
	}
	if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2_851 {
		t.Fatalf("expected deepseek chat-only terminal target 2851, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeOpenAIResponsesToChatCompletions {
		t.Fatalf("expected responses-to-chat translation, got %q", got)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "deepseek-v4-pro" {
		t.Fatalf("expected translated upstream body model deepseek-v4-pro, got %q in %s", got, string(plan.UpstreamBody))
	}
}

func TestBuildRequestPlan_ResponsesToChatUsesSelectedTargetForMaxOutputTokenField(t *testing.T) {
	service := newRequestPlanUnitService()
	responsesOnly := providercompat.OpenAITextCapabilityResponsesOnly
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "o3-mini", OpenAIAcceptedFormat: &responsesOnly},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "deepseek-v4-pro"},
	)
	source := snapshot.ModelsByID["o3-mini"]
	target := snapshot.ModelsByID["deepseek-v4-pro"]
	snapshot.AccessTargetsBySourceModelID[source.ID] = nil
	snapshot.AccessTargetsBySourceModelID[target.ID] = nil
	addRequestPlanModelTargetAtPosition(snapshot, source.ModelID, target.ModelID, 0)
	addRequestPlanConnectionTargetWithOptions(snapshot, target, 2_852, 9_852, 0, requestPlanConnectionTargetOptions{
		openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly),
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"o3-mini","input":"hello","max_output_tokens":64}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build target-model token-field request plan: %v", err)
	}
	if got := plan.EffectiveRequestPath; got != "/v1/chat/completions" {
		t.Fatalf("expected translated Chat path, got %q", got)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "deepseek-v4-pro" {
		t.Fatalf("expected translated upstream body model deepseek-v4-pro, got %q in %s", got, string(plan.UpstreamBody))
	}
	payload := decodeTranslationTestPayload(t, plan.UpstreamBody)
	if got := intValue(intPointerFromAny(payload["max_tokens"])); got != 64 {
		t.Fatalf("expected selected non-o-series target to receive max_tokens=64, got %+v in %s", payload["max_tokens"], string(plan.UpstreamBody))
	}
	if _, ok := payload["max_completion_tokens"]; ok {
		t.Fatalf("expected max_completion_tokens to be absent for selected non-o-series target, got %s", string(plan.UpstreamBody))
	}
}

func TestBuildRequestPlan_ResponsesUnsupportedToolsRecordLossyDrops(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_861, 9_861, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","tools":[{"type":"image_generation","name":"draw"}],"tool_choice":{"type":"function","name":"draw"},"parallel_tool_calls":true}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build lossy unsupported responses tools plan: %v", err)
	}
	payload := decodeTranslationTestPayload(t, plan.UpstreamBody)
	if _, ok := payload["tools"]; ok {
		t.Fatalf("expected unsupported responses tools to drop from translated body, got %s", string(plan.UpstreamBody))
	}
}

func TestBuildRequestPlan_AdapterGatedResponsesIngressCanUseChatOnlyTarget(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_831, 9_831, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello"}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build adapter-gated translated responses request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected translated chat path, got %q", plan.EffectiveRequestPath)
	}
	if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2_831 {
		t.Fatalf("expected translated terminal target 2831, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeOpenAIResponsesToChatCompletions {
		t.Fatalf("expected responses-to-chat translation mode, got %q", got)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "responses-public" {
		t.Fatalf("expected translated upstream body model responses-public, got %q in %s", got, string(plan.UpstreamBody))
	}
}

func TestBuildRequestPlan_AdapterGatedChatIngressCanUseResponsesOnlyTarget(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "chat-public"})
	model := snapshot.ModelsByID["chat-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_832, 9_832, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityResponsesOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":32}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build adapter-gated translated chat request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/responses" {
		t.Fatalf("expected translated responses path, got %q", plan.EffectiveRequestPath)
	}
	if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 2_832 {
		t.Fatalf("expected translated terminal target 2832, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeOpenAIChatCompletionsToResponses {
		t.Fatalf("expected chat-to-responses translation mode, got %q", got)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "chat-public" {
		t.Fatalf("expected translated upstream body model chat-public, got %q in %s", got, string(plan.UpstreamBody))
	}
}

func TestBuildRequestPlan_AdapterGatedRejectsUnsupportedTranslatedOpenAITextShape(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_841, 9_841, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":{"type":"text"}}}`)

	_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != http.StatusBadRequest || domainErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode || domainErr.Detail != openAIRequestTranslationUnsupportedDetail {
		t.Fatalf("expected adapter-backed translation unsupported error, got %+v", domainErr)
	}
	if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "responses_text_format" {
		t.Fatalf("expected unsupported reason responses_text_format, got %+v", domainErr.Fields)
	}
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

func TestBuildRequestPlan_TranslationUnsupportedStaysHardRejectionWithEstimatePresent(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o"})
	model := snapshot.ModelsByID["gpt-4o"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_991, 9_991, 0, requestPlanConnectionTargetOptions{
		openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly),
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"gpt-4o","input":"hello","text":{"format":{"type":"text"}}}`)

	_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != http.StatusBadRequest || domainErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode {
		t.Fatalf("expected translation unsupported hard rejection, got %+v", domainErr)
	}
}

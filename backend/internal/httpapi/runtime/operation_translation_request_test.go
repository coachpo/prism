package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveTranslationMode(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	chatOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	tests := []struct {
		name      string
		operation RuntimeOperation
		upstream  *string
		variant   *string
		want      TranslationMode
	}{
		{name: "responses to chat", operation: responsesOperation, upstream: stringPtr(openAIUpstreamOperationChatCompletions), variant: stringPtr("chat_completions_reasoning_none"), want: TranslationModeOpenAIResponsesToChatCompletions},
		{name: "chat to responses", operation: chatOperation, upstream: stringPtr(openAIUpstreamOperationResponses), variant: stringPtr("responses_reasoning_none"), want: TranslationModeOpenAIChatCompletionsToResponses},
		{name: "same dialect", operation: responsesOperation, upstream: stringPtr(openAIUpstreamOperationResponses), variant: stringPtr("responses_reasoning_none"), want: TranslationModeNone},
		{name: "missing variant", operation: responsesOperation, upstream: stringPtr(openAIUpstreamOperationChatCompletions), want: TranslationModeNone},
		{name: "unsupported upstream", operation: responsesOperation, upstream: stringPtr("anthropic.messages"), variant: stringPtr("chat_completions_reasoning_none"), want: TranslationModeNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveTranslationMode(test.operation, test.upstream, test.variant); got != test.want {
				t.Fatalf("expected translation mode %q, got %q", test.want, got)
			}
		})
	}
}

func TestTranslationCapability(t *testing.T) {
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
		{name: "responses previous response id rejected", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","input":"hello","previous_response_id":"resp_123"}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "responses_previous_response_id"},
		{name: "chat structured output rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[],"response_format":{"type":"json_schema"}}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_response_format"},
		{name: "responses reasoning state rejected", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","input":"hello","reasoning":{"encrypted_content":"state"}}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "responses_reasoning_encrypted_content"},
		{name: "chat audio rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"audio":{"format":"wav"}}`), wantRequestClass: openAITranslationCapabilityReject, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilitySafe, wantReason: "chat_audio"},
		{name: "chat stream tools rejected", operation: chatOperation, mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","stream":true,"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilityReject, wantReason: "chat_stream_tools"},
		{name: "responses stream tools rejected", operation: responsesOperation, mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","stream":true,"input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`), wantRequestClass: openAITranslationCapabilitySafe, wantResponseClass: openAITranslationCapabilitySafe, wantStreamClass: openAITranslationCapabilityReject, wantReason: "responses_stream_tools"},
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
	if got := stringValue(messages[1].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]); got != "hello" {
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
	if got := intValue(intPointerFromAny(payload["max_completion_tokens"])); got != 32 {
		t.Fatalf("expected max_completion_tokens=32, got %+v", payload["max_completion_tokens"])
	}
}

func TestTranslateOpenAIChatToResponsesRequest(t *testing.T) {
	rawBody := []byte(`{"model":"chat-public","messages":[{"role":"system","content":"system note"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"max_completion_tokens":32,"reasoning_effort":"low"}`)

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
	input := payload["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("expected one translated user message, got %+v", input)
	}
	if got := stringValue(input[0].(map[string]any)["type"]); got != "message" {
		t.Fatalf("expected translated message item, got %q", got)
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
		{name: "chat tools", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`), reason: "chat_tools"},
		{name: "chat image", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"model":"chat-public","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/image.png"}}]}]}`), reason: "chat_image_part"},
		{name: "responses previous_response_id", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","previous_response_id":"resp_123","input":"hello"}`), reason: "responses_previous_response_id"},
		{name: "responses tools", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","input":"hello","tools":[{"type":"function","name":"lookup"}]}`), reason: "responses_tools"},
		{name: "responses image", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"model":"responses-public","input":[{"type":"input_image","image_url":"https://example.invalid/image.png"}]}`), reason: "responses_input_image"},
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

func TestCodingAgentFormatBridgePlanRequestResponsesToChat(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	bridge := NewCodingAgentFormatBridge("safe_only")
	connection := runtimeConnection{
		OpenAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"),
		OpenAIUpstreamOperation:    stringPtr(openAIUpstreamOperationChatCompletions),
	}
	rawBody := []byte(`{"model":"responses-public","input":"hello","max_output_tokens":24}`)

	plan, translated, err := bridge.PlanRequest(operation, rawBody, "chat-target-model", connection)
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

func TestCodingAgentFormatBridgePlanRequestChatToResponses(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	bridge := NewCodingAgentFormatBridge("safe_only")
	connection := runtimeConnection{
		OpenAIProbeEndpointVariant: stringPtr("responses_reasoning_none"),
		OpenAIUpstreamOperation:    stringPtr(openAIUpstreamOperationResponses),
	}
	rawBody := []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":32}`)

	plan, translated, err := bridge.PlanRequest(operation, rawBody, "responses-target-model", connection)
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

func TestCodingAgentFormatBridgePlanRequestRejectsUnsupportedShape(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	bridge := NewCodingAgentFormatBridge("safe_only")
	connection := runtimeConnection{
		OpenAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"),
		OpenAIUpstreamOperation:    stringPtr(openAIUpstreamOperationChatCompletions),
	}
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"}}`)

	_, translated, err := bridge.PlanRequest(operation, rawBody, "chat-target-model", connection)
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
	if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "responses_text" {
		t.Fatalf("expected unsupported reason responses_text, got %+v", domainErr.Fields)
	}
}

func TestBuildRequestPlan_NonNativeOpenAITargetIsNotSelectedByGenericPlanner(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_821, 9_821, 0, requestPlanConnectionTargetOptions{openAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationChatCompletions)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello"}`)

	_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "No eligible targets available for model 'responses-public'.")
}

func TestBuildRequestPlan_NativeOpenAITargetWinsOverNonNative(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_811, 9_811, 0, requestPlanConnectionTargetOptions{openAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationChatCompletions)})
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_812, 9_812, 1, requestPlanConnectionTargetOptions{openAIProbeEndpointVariant: stringPtr("responses_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationResponses)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"}}`)

	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build native-support request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/responses" {
		t.Fatalf("expected native responses path, got %q", plan.EffectiveRequestPath)
	}
	if plan.TerminalAttempts[0].Connection.ID != 2_812 {
		t.Fatalf("expected native responses-capable connection 2812, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeNone {
		t.Fatalf("expected native support to use translation mode none, got %q", got)
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

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
			eligibility := requestTranslationEligibilityFromCapability(capability)
			if eligibility.Supported != test.wantSupported {
				t.Fatalf("expected supported=%v, got %+v", test.wantSupported, eligibility)
			}
			if !test.wantSupported {
				if eligibility.Rejection == nil {
					t.Fatal("expected rejected capability to produce domain error")
				}
				if eligibility.Rejection.StatusCode != http.StatusBadRequest || eligibility.Rejection.ErrorCode != openAIRequestTranslationUnsupportedErrorCode {
					t.Fatalf("expected pinned rejection contract, got %+v", eligibility.Rejection)
				}
				if got := stringValue(eligibility.Rejection.Fields["unsupported_reason"]); got != test.wantReason {
					t.Fatalf("expected rejection reason %q, got %+v", test.wantReason, eligibility.Rejection.Fields)
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

func TestBuildRequestPlan_TranslationEligibilityResponsesTranslationEligible(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "chat-target-model"},
	)
	addRequestPlanProxyTarget(snapshot, "responses-public", "chat-target-model")
	child := snapshot.ModelsByID["chat-target-model"]
	snapshot.AccessTargetsBySourceModelID[child.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_801, 9_801, 0, requestPlanConnectionTargetOptions{openAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationChatCompletions)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","max_output_tokens":24}`)
	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build translated responses request plan: %v", err)
	}
	if plan.EffectiveRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected translated effective request path, got %q", plan.EffectiveRequestPath)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "chat-target-model" {
		t.Fatalf("expected translated target model chat-target-model, got %q", got)
	}
	if got := plan.TerminalAttempts[0].TranslationMode; got != TranslationModeOpenAIResponsesToChatCompletions {
		t.Fatalf("expected responses-to-chat translation mode, got %q", got)
	}
}

func TestBuildRequestPlan_TranslationEligibilityNativeSupportWins(t *testing.T) {
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

func TestBuildRequestPlan_TranslationEligibilityRejectsUnsupportedShapeWithoutTransport(t *testing.T) {
	service := newRequestPlanUnitService()
	transport := &ingressRoundTripRecorder{}
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"})
	model := snapshot.ModelsByID["responses-public"]
	snapshot.AccessTargetsBySourceModelID[model.ID] = nil
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_821, 9_821, 0, requestPlanConnectionTargetOptions{openAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationChatCompletions)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"}}`)
	_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{HTTPClient: &http.Client{Transport: transport}}, operationMatch, requestPlanTestProfileID, snapshot)
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
	if got := transport.calls.Load(); got != 0 {
		t.Fatalf("expected unsupported translated shape to avoid transport calls, got %d", got)
	}
}

func TestBuildRequestPlan_TranslationPreservesResponsesIngressEstimation(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "chat-target-model"},
	)
	child := snapshot.ModelsByID["chat-target-model"]
	setRequestPlanStrategyType(snapshot, child, "cheapest_eligible_context")
	addRequestPlanProxyTarget(snapshot, "responses-public", "chat-target-model")
	snapshot.AccessTargetsBySourceModelID[child.ID] = nil
	contextWindowTokens := 8_192
	addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_831, 9_831, 0, requestPlanConnectionTargetOptions{contextWindowTokens: &contextWindowTokens, maxContextUtilization: 1.0, openAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationChatCompletions)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","max_output_tokens":24}`)
	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build translated responses request plan: %v", err)
	}
	if plan.RuntimeOperation.Name != openAIUpstreamOperationResponses {
		t.Fatalf("expected ingress operation %q, got %q", openAIUpstreamOperationResponses, plan.RuntimeOperation.Name)
	}
	if plan.EffectiveRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected translated effective request path, got %q", plan.EffectiveRequestPath)
	}
	if plan.RequestContextEstimation == nil || plan.RequestContextEstimation.Method != openAIResponsesContextEstimationMethod {
		t.Fatalf("expected ingress responses estimation method %q, got %+v", openAIResponsesContextEstimationMethod, plan.RequestContextEstimation)
	}
	if plan.RequestContextEstimation.ReservedOutputTokens != 24 {
		t.Fatalf("expected ingress responses reserved output tokens 24, got %+v", plan.RequestContextEstimation)
	}
	if plan.ContextRouting == nil || plan.ContextRouting.EstimationMethod == nil || *plan.ContextRouting.EstimationMethod != openAIResponsesContextEstimationMethod {
		t.Fatalf("expected ingress responses context-routing metadata, got %+v", plan.ContextRouting)
	}
	if plan.SelectedTerminalTargetID == nil || *plan.SelectedTerminalTargetID != 2_831 {
		t.Fatalf("expected selected terminal target 2831, got %+v", plan.SelectedTerminalTargetID)
	}
	if plan.RequestGenerationParams.Status != requestGenerationParamsStatusComplete || plan.RequestGenerationParams.Params == nil || plan.RequestGenerationParams.Params.MaxOutputTokensSource == nil || *plan.RequestGenerationParams.Params.MaxOutputTokensSource != "max_output_tokens" {
		t.Fatalf("expected ingress responses request-generation params, got %+v", plan.RequestGenerationParams)
	}
}

func TestBuildRequestPlan_TranslationPreservesChatIngressEstimation(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "chat-public"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "responses-target-model"},
	)
	child := snapshot.ModelsByID["responses-target-model"]
	setRequestPlanStrategyType(snapshot, child, "cheapest_eligible_context")
	addRequestPlanProxyTarget(snapshot, "chat-public", "responses-target-model")
	snapshot.AccessTargetsBySourceModelID[child.ID] = nil
	contextWindowTokens := 8_192
	addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_832, 9_832, 0, requestPlanConnectionTargetOptions{contextWindowTokens: &contextWindowTokens, maxContextUtilization: 1.0, openAIProbeEndpointVariant: stringPtr("responses_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationResponses)})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"chat-public","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":32,"reasoning_effort":"low"}`)
	plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build translated chat request plan: %v", err)
	}
	if plan.RuntimeOperation.Name != openAIUpstreamOperationChatCompletions {
		t.Fatalf("expected ingress operation %q, got %q", openAIUpstreamOperationChatCompletions, plan.RuntimeOperation.Name)
	}
	if plan.EffectiveRequestPath != "/v1/responses" {
		t.Fatalf("expected translated effective request path, got %q", plan.EffectiveRequestPath)
	}
	if plan.RequestContextEstimation == nil || plan.RequestContextEstimation.Method != openAIChatContextEstimationMethod {
		t.Fatalf("expected ingress chat estimation method %q, got %+v", openAIChatContextEstimationMethod, plan.RequestContextEstimation)
	}
	if plan.RequestContextEstimation.ReservedOutputTokens != 32 {
		t.Fatalf("expected ingress chat reserved output tokens 32, got %+v", plan.RequestContextEstimation)
	}
	if plan.ContextRouting == nil || plan.ContextRouting.EstimationMethod == nil || *plan.ContextRouting.EstimationMethod != openAIChatContextEstimationMethod {
		t.Fatalf("expected ingress chat context-routing metadata, got %+v", plan.ContextRouting)
	}
	if plan.SelectedTerminalTargetID == nil || *plan.SelectedTerminalTargetID != 2_832 {
		t.Fatalf("expected selected terminal target 2832, got %+v", plan.SelectedTerminalTargetID)
	}
	if plan.RequestGenerationParams.Status != requestGenerationParamsStatusComplete || plan.RequestGenerationParams.Params == nil || plan.RequestGenerationParams.Params.MaxOutputTokensSource == nil || *plan.RequestGenerationParams.Params.MaxOutputTokensSource != "max_completion_tokens" {
		t.Fatalf("expected ingress chat request-generation params, got %+v", plan.RequestGenerationParams)
	}
}

func TestBuildRequestPlan_TranslationUnsupportedPreservesResponsesIngressEstimation(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "responses-public"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "chat-target-model"},
	)
	child := snapshot.ModelsByID["chat-target-model"]
	setRequestPlanStrategyType(snapshot, child, "cheapest_eligible_context")
	addRequestPlanProxyTarget(snapshot, "responses-public", "chat-target-model")
	snapshot.AccessTargetsBySourceModelID[child.ID] = nil
	contextWindowTokens := 8_192
	addRequestPlanConnectionTargetWithOptions(snapshot, child, 2_841, 9_841, 0, requestPlanConnectionTargetOptions{contextWindowTokens: &contextWindowTokens, maxContextUtilization: 1.0, openAIProbeEndpointVariant: stringPtr("chat_completions_reasoning_none"), openAIUpstreamOperation: stringPtr(openAIUpstreamOperationChatCompletions)})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"responses-public","input":"hello","text":{"format":"json_schema"},"max_output_tokens":48}`)
	_, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != http.StatusBadRequest || domainErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode {
		t.Fatalf("expected translated unsupported-shape 400, got %+v", domainErr)
	}
	if domainErr.SelectedTerminalTargetID == nil || *domainErr.SelectedTerminalTargetID != 2_841 {
		t.Fatalf("expected selected terminal target 2841 on translated rejection, got %+v", domainErr.SelectedTerminalTargetID)
	}
	if domainErr.ContextRouting == nil || domainErr.ContextRouting.EstimationMethod == nil || *domainErr.ContextRouting.EstimationMethod != openAIResponsesContextEstimationMethod {
		t.Fatalf("expected ingress responses context-routing metadata on translated rejection, got %+v", domainErr.ContextRouting)
	}
	if domainErr.ContextRouting.SelectedTerminalTargetID == nil || *domainErr.ContextRouting.SelectedTerminalTargetID != 2_841 {
		t.Fatalf("expected context-routing selected terminal target 2841 on translated rejection, got %+v", domainErr.ContextRouting)
	}
	if domainErr.ContextRouting.ReservedOutputTokens == nil || *domainErr.ContextRouting.ReservedOutputTokens != 48 {
		t.Fatalf("expected translated rejection to keep reserved output tokens 48, got %+v", domainErr.ContextRouting)
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

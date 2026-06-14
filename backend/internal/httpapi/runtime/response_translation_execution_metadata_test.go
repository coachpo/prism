package runtime

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFinalResponseTranslationMetadataDrivesNonStreamSerialization(t *testing.T) {
	service := &Service{now: fixedResponseHookTestNow}
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	connection := runtimeConnection{ID: 77, Endpoint: runtimeEndpoint{ID: 7007}}
	plan := requestPlan{
		RequestedModelID: "responses-public",
		RuntimeOperation: operation,
		TerminalAttempts: []runtimeTerminalAttempt{{Connection: connection, TranslationMode: TranslationModeNone}},
	}
	execution := executionResult{
		Response:   &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}},
		Connection: connection,
		FinalResponseTranslation: &runtimeFinalResponseTranslationMetadata{
			TranslationMode:          TranslationModeOpenAIResponsesToChatCompletions,
			RequestedModelID:         "responses-public",
			SelectedTerminalTargetID: intPtr(connection.ID),
			UpstreamOperationName:    openAIUpstreamOperationChatCompletions,
			UpstreamRequestPath:      "/v1/chat/completions",
		},
	}
	rawBody := []byte(`{"id":"chatcmpl_meta","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`)
	responseRecorder := httptest.NewRecorder()
	proxyWriter := newRuntimeDeferredCommitWriter(responseRecorder)

	capture, err := service.writeBufferedNonStreamResponse(proxyWriter, plan, execution, *execution.FinalResponseTranslation, rawBody)
	if err != nil {
		t.Fatalf("write translated non-stream response: %v", err)
	}
	proxyWriter.Commit()

	payload := decodeTranslationTestPayload(t, responseRecorder.Body.Bytes())
	if got := stringValue(payload["object"]); got != "response" {
		t.Fatalf("expected explicit metadata to translate chat response to responses payload, got object %q body %s", got, responseRecorder.Body.String())
	}
	if got := stringValue(payload["model"]); got != "responses-public" {
		t.Fatalf("expected requested public model from final metadata, got %q", got)
	}
	if got := capture.extractedUsage().TotalTokens; got == nil || *got != 16 {
		t.Fatalf("expected translated capture usage from upstream body, got %+v", capture.extractedUsage())
	}
}

func TestFinalResponseTranslationMetadataDrivesStreamSerialization(t *testing.T) {
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
			TranslationMode:          TranslationModeOpenAIResponsesToChatCompletions,
			RequestedModelID:         "responses-public",
			SelectedTerminalTargetID: intPtr(connection.ID),
			UpstreamOperationName:    openAIUpstreamOperationChatCompletions,
			UpstreamRequestPath:      "/v1/chat/completions",
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	responseRecorder := httptest.NewRecorder()

	service.writeProxyResponse(responseRecorder, request, plan, execution, service.nowUTC())

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("expected translated stream status 200, got %d body %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	events := parseTranslatedSSEEvents(t, responseRecorder.Body.String())
	created := translatedSSEEventPayload(t, events, "response.created")
	createdResponse := created["response"].(map[string]any)
	if got := stringValue(createdResponse["model"]); got != "responses-public" {
		t.Fatalf("expected stream translation to use requested public model from final metadata, got %q body %s", got, responseRecorder.Body.String())
	}
	if strings.Contains(responseRecorder.Body.String(), "chat.completion.chunk") {
		t.Fatalf("expected translated responses stream, got raw chat stream %s", responseRecorder.Body.String())
	}
}

func TestPreDispatchPromotionMergePreservesFinalTranslationIntent(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	sourceConnection := runtimeConnection{ID: 11, Endpoint: runtimeEndpoint{ID: 111}}
	promotedConnection := runtimeConnection{ID: 22, Endpoint: runtimeEndpoint{ID: 222}}
	sourcePlan := requestPlan{
		RequestedModelID:         "responses-public",
		RuntimeOperation:         operation,
		SelectedTerminalTargetID: intPtr(sourceConnection.ID),
		RequestContextEstimation: &requestContextEstimation{Method: openAIResponsesContextEstimationMethod, EstimatedInputTokens: 2000, ReservedOutputTokens: 100, EstimatedTotalContextTokens: 2100},
		ContextRouting:           &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: intPtr(sourceConnection.ID)},
	}
	promotedPlan := requestPlan{
		RequestedModelID:         "promoted-model",
		RuntimeOperation:         operation,
		SelectedTerminalTargetID: intPtr(promotedConnection.ID),
		TerminalAttempts: []runtimeTerminalAttempt{{
			TargetModel:          runtimeModelRecord{ModelID: "chat-target", APIFamily: "openai"},
			Connection:           promotedConnection,
			TranslationMode:      TranslationModeOpenAIResponsesToChatCompletions,
			EffectiveRequestPath: "/v1/chat/completions",
		}},
		ContextRouting: &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: intPtr(promotedConnection.ID)},
	}

	merged := mergePreDispatchContextOverflowPromotedPlan(sourcePlan, promotedPlan, 1800, 4096)
	state := requestExecutionState{launchedAttempts: 1}
	execution := state.result(merged, executionOutcome{
		TerminalAttempt: merged.TerminalAttempts[0],
		Connection:      promotedConnection,
		Response:        &http.Response{StatusCode: http.StatusOK},
		Launched:        true,
	})

	if merged.RequestedModelID != "responses-public" {
		t.Fatalf("expected client requested model to survive merge, got %q", merged.RequestedModelID)
	}
	metadata := execution.FinalResponseTranslation
	if metadata == nil {
		t.Fatal("expected explicit final response translation metadata")
	}
	if metadata.TranslationMode != TranslationModeOpenAIResponsesToChatCompletions || metadata.RequestedModelID != "responses-public" || intValue(metadata.SelectedTerminalTargetID) != promotedConnection.ID || metadata.UpstreamOperationName != openAIUpstreamOperationChatCompletions || metadata.UpstreamRequestPath != "/v1/chat/completions" {
		t.Fatalf("expected promoted final translation intent with public model preserved, got %+v", metadata)
	}
}

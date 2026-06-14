package runtime

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFinalResponseTranslationMetadataDrivesNonStreamSerialization(t *testing.T) {
	assertFinalResponseTranslationDirectionValues(t)
	service := &Service{now: fixedResponseHookTestNow}
	connection := runtimeConnection{ID: 77, Endpoint: runtimeEndpoint{ID: 7007}}
	tests := []struct {
		name                 string
		requestPath          string
		requestedModel       string
		clientOperationName  string
		upstreamOperation    string
		upstreamRequestPath  string
		direction            runtimeFinalResponseTranslationDirection
		rawBody              []byte
		wantObject           string
		wantEnvelopeKey      string
		forbiddenRawFragment string
	}{
		{
			name:                 "chat upstream to responses client",
			requestPath:          "/v1/responses",
			requestedModel:       "responses-public",
			clientOperationName:  openAIUpstreamOperationResponses,
			upstreamOperation:    openAIUpstreamOperationChatCompletions,
			upstreamRequestPath:  "/v1/chat/completions",
			direction:            runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient,
			rawBody:              []byte(`{"id":"chatcmpl_meta","object":"chat.completion","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`),
			wantObject:           "response",
			wantEnvelopeKey:      "output",
			forbiddenRawFragment: `"object":"chat.completion"`,
		},
		{
			name:                 "responses upstream to chat client",
			requestPath:          "/v1/chat/completions",
			requestedModel:       "chat-public",
			clientOperationName:  openAIUpstreamOperationChatCompletions,
			upstreamOperation:    openAIUpstreamOperationResponses,
			upstreamRequestPath:  "/v1/responses",
			direction:            runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient,
			rawBody:              []byte(`{"id":"resp_meta","object":"response","created_at":1700000001,"model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16}}`),
			wantObject:           "chat.completion",
			wantEnvelopeKey:      "choices",
			forbiddenRawFragment: `"object":"response"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			plan := requestPlan{
				RequestedModelID: test.requestedModel,
				RuntimeOperation: operation,
				TerminalAttempts: []runtimeTerminalAttempt{{Connection: connection, TranslationMode: TranslationModeNone}},
			}
			execution := executionResult{
				Response:   &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}},
				Connection: connection,
				FinalResponseTranslation: &runtimeFinalResponseTranslationMetadata{
					TranslationMode:              TranslationModeNone,
					RequestedModelID:             test.requestedModel,
					ClientOperationName:          test.clientOperationName,
					SelectedTerminalTargetID:     &connection.ID,
					UpstreamOperationName:        test.upstreamOperation,
					UpstreamRequestPath:          test.upstreamRequestPath,
					ResponseTranslationDirection: test.direction,
				},
			}
			responseRecorder := httptest.NewRecorder()
			proxyWriter := newRuntimeDeferredCommitWriter(responseRecorder)

			capture, err := service.writeBufferedNonStreamResponse(proxyWriter, plan, execution, *execution.FinalResponseTranslation, test.rawBody)
			if err != nil {
				t.Fatalf("write translated non-stream response: %v", err)
			}
			proxyWriter.Commit()

			payload := decodeTranslationTestPayload(t, responseRecorder.Body.Bytes())
			if got := stringValue(payload["object"]); got != test.wantObject {
				t.Fatalf("expected explicit metadata to translate non-stream payload to object %q, got object %q body %s", test.wantObject, got, responseRecorder.Body.String())
			}
			if got := stringValue(payload["model"]); got != test.requestedModel {
				t.Fatalf("expected requested public model from final metadata, got %q", got)
			}
			if _, ok := payload[test.wantEnvelopeKey]; !ok {
				t.Fatalf("expected translated payload to contain %q envelope, got %+v", test.wantEnvelopeKey, payload)
			}
			if strings.Contains(responseRecorder.Body.String(), test.forbiddenRawFragment) {
				t.Fatalf("expected translated payload to avoid raw upstream object leak %s, got %s", test.forbiddenRawFragment, responseRecorder.Body.String())
			}
			if got := capture.extractedUsage().TotalTokens; got == nil || *got != 16 {
				t.Fatalf("expected translated capture usage from upstream body, got %+v", capture.extractedUsage())
			}
		})
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
		SelectedTerminalTargetID: &sourceConnection.ID,
		RequestContextEstimation: &requestContextEstimation{Method: openAIResponsesContextEstimationMethod, EstimatedInputTokens: 2000, ReservedOutputTokens: 100, EstimatedTotalContextTokens: 2100},
		ContextRouting:           &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: &sourceConnection.ID},
	}
	promotedPlan := requestPlan{
		RequestedModelID:         "promoted-model",
		RuntimeOperation:         operation,
		SelectedTerminalTargetID: &promotedConnection.ID,
		TerminalAttempts: []runtimeTerminalAttempt{{
			TargetModel:          runtimeModelRecord{ModelID: "chat-target", APIFamily: "openai"},
			Connection:           promotedConnection,
			TranslationMode:      TranslationModeOpenAIResponsesToChatCompletions,
			EffectiveRequestPath: "/v1/chat/completions",
		}},
		ContextRouting: &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: &promotedConnection.ID},
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
	if metadata.TranslationMode != TranslationModeOpenAIResponsesToChatCompletions || metadata.RequestedModelID != "responses-public" || metadata.ClientOperationName != openAIUpstreamOperationResponses || intValue(metadata.SelectedTerminalTargetID) != promotedConnection.ID || metadata.UpstreamOperationName != openAIUpstreamOperationChatCompletions || metadata.UpstreamRequestPath != "/v1/chat/completions" || metadata.ResponseTranslationDirection != runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient {
		t.Fatalf("expected promoted final translation intent with public model preserved, got %+v", metadata)
	}
}

func TestProviderFallbackPromotionMergePreservesChatToResponsesFinalTranslationIntent(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	sourceResolvedTargetModelID := "chat-source-model"
	promotedResolvedTargetModelID := "responses-promoted-model"
	sourceConnection := runtimeConnection{ID: 31, Endpoint: runtimeEndpoint{ID: 311}}
	promotedConnection := runtimeConnection{ID: 42, Endpoint: runtimeEndpoint{ID: 422}}
	sourcePlan := requestPlan{
		RequestedModelID:         "chat-public",
		ResolvedTargetModelID:    &sourceResolvedTargetModelID,
		RuntimeOperation:         operation,
		SelectedTerminalTargetID: &sourceConnection.ID,
		RequestContextEstimation: &requestContextEstimation{Method: openAIChatContextEstimationMethod, EstimatedInputTokens: 5000, ReservedOutputTokens: 1000, EstimatedTotalContextTokens: 6000},
		ContextRouting:           &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: &sourceConnection.ID},
	}
	promotedPlan := requestPlan{
		RequestedModelID:         "responses-promoted-public",
		ResolvedTargetModelID:    &promotedResolvedTargetModelID,
		RuntimeOperation:         operation,
		SelectedTerminalTargetID: &promotedConnection.ID,
		TerminalAttempts: []runtimeTerminalAttempt{{
			TargetModel:          runtimeModelRecord{ModelID: promotedResolvedTargetModelID, APIFamily: "openai"},
			Connection:           promotedConnection,
			TranslationMode:      TranslationModeOpenAIChatCompletionsToResponses,
			EffectiveRequestPath: "/v1/responses",
		}},
		ContextRouting: &runtimeContextRoutingDecision{Policy: "cheapest_eligible_context", SelectedTerminalTargetID: &promotedConnection.ID},
	}
	sourceExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusBadRequest},
		Connection:            sourceConnection,
		ResolvedTargetModelID: &sourceResolvedTargetModelID,
		AttemptCount:          1,
		Attempts:              []executionAttempt{{Connection: sourceConnection, ResolvedTargetModelID: sourceResolvedTargetModelID, StatusCode: http.StatusBadRequest}},
	}
	promotedExecution := executionResult{
		Response:              &http.Response{StatusCode: http.StatusOK},
		Connection:            promotedConnection,
		ResolvedTargetModelID: &promotedResolvedTargetModelID,
		AttemptCount:          1,
		Attempts:              []executionAttempt{{Connection: promotedConnection, ResolvedTargetModelID: promotedResolvedTargetModelID, StatusCode: http.StatusOK, OperationTranslationMode: TranslationModeNone}},
		FinalResponseTranslation: &runtimeFinalResponseTranslationMetadata{
			TranslationMode:              TranslationModeNone,
			ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient,
		},
	}

	finalPlan := mergeContextOverflowPromotedPlan(sourcePlan, promotedPlan, sourceExecution, promotedExecution, cliProxyAPIOverflowClassification{Promotable: true, ErrorCode: "context_length_exceeded", Classifier: cliProxyAPIOverflowClassifierErrorCode})
	finalExecution := mergeContextOverflowPromotedExecution(finalPlan, sourceExecution, promotedExecution)

	metadata := finalExecution.FinalResponseTranslation
	if metadata == nil {
		t.Fatal("expected promoted provider fallback to keep final response translation metadata")
	}
	if metadata.RequestedModelID != "chat-public" || metadata.ClientOperationName != openAIUpstreamOperationChatCompletions || metadata.UpstreamOperationName != openAIUpstreamOperationResponses || metadata.UpstreamRequestPath != "/v1/responses" || metadata.ResponseTranslationDirection != runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient {
		t.Fatalf("expected Chat client / Responses upstream final translation metadata, got %+v", metadata)
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

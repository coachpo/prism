package runtime

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/providercompat"
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

func TestFinalResponseTranslationStreamSerializationUsesRequestToolContext(t *testing.T) {
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
	responseRecorder := httptest.NewRecorder()

	service.writeProxyResponse(responseRecorder, request, plan, execution, service.nowUTC())

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("expected translated stream status 200, got %d body %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	completed := translatedSSEEventPayload(t, parseTranslatedSSEEvents(t, responseRecorder.Body.String()), "response.completed")
	response := completed["response"].(map[string]any)
	output := response["output"].([]any)
	if len(output) != 3 {
		t.Fatalf("expected live stream path to reconstruct three tool outputs, got %+v body %s", output, responseRecorder.Body.String())
	}
	custom := output[0].(map[string]any)
	if custom["type"] != "custom_tool_call" || custom["name"] != "exec" || custom["input"] != "ls -la" {
		t.Fatalf("expected custom tool reconstruction through service stream path, got %+v", custom)
	}
	toolSearch := output[1].(map[string]any)
	if toolSearch["type"] != "tool_search_call" || toolSearch["execution"] != "client" || stringValue(nestedValue(toolSearch, "arguments", "query")) != "gmail" {
		t.Fatalf("expected tool_search reconstruction through service stream path, got %+v", toolSearch)
	}
	namespace := output[2].(map[string]any)
	if namespace["type"] != "function_call" || namespace["name"] != "_search_emails" || namespace["namespace"] != "mcp__apps__gmail" {
		t.Fatalf("expected namespace tool reconstruction through service stream path, got %+v", namespace)
	}
	if len(output) == 1 && stringValue(output[0].(map[string]any)["type"]) == "message" {
		t.Fatalf("expected service stream path not to fall back to an empty assistant message, got %+v", output)
	}
}

func TestHandleStreamingProxyResponsesToolsStreamReconstructsCompletedOutput(t *testing.T) {
	transport := &responsesToolsStreamRoundTripper{}
	client := &http.Client{Transport: transport}
	service := newResponsesToolsStreamService(client)
	rawRequest := `{"model":"responses-public","stream":true,"input":"use tools","tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(rawRequest))
	request.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()

	service.handleStreamingProxy(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("expected live stream status 200, got %d body %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	if transport.calls.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", transport.calls.Load())
	}
	assertResponsesToolsUpstreamChatRequest(t, transport.requestPath, transport.requestBody, "responses-public")
	assertResponsesToolsCompletedOutput(t, responseRecorder.Body.String())
}

func TestHandleStreamingProxyAtlasResponsesToolsStreamReconstructsMessageToolPayload(t *testing.T) {
	transport := &responsesToolsStreamRoundTripper{messageToolPayload: true}
	client := &http.Client{Transport: transport}
	service := newResponsesToolsStreamServiceForModel(client, "qa-stream-1781714246")
	rawRequest := `{"model":"qa-stream-1781714246","input":"use tools","stream":true,"tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(rawRequest))
	request.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()

	service.handleStreamingProxy(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("expected Atlas live stream status 200, got %d body %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	if transport.calls.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", transport.calls.Load())
	}
	assertResponsesToolsUpstreamChatRequest(t, transport.requestPath, transport.requestBody, "qa-stream-1781714246")
	assertResponsesToolsCompletedOutput(t, responseRecorder.Body.String())
}

func TestHandleStreamingProxyAtlasResponsesToolsStreamRejectsMalformedArgumentsPayload(t *testing.T) {
	transport := &responsesToolsStreamRoundTripper{malformedArgumentsPayload: true}
	client := &http.Client{Transport: transport}
	service := newResponsesToolsStreamServiceForModel(client, "qa-stream-1781714246")
	rawRequest := `{"model":"qa-stream-1781714246","input":"use tools","stream":true,"tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(rawRequest))
	request.Header.Set("Content-Type", "application/json")
	responseRecorder := httptest.NewRecorder()

	service.handleStreamingProxy(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadGateway {
		t.Fatalf("expected malformed live stream status 502, got %d body %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	if transport.calls.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", transport.calls.Load())
	}
	assertResponsesToolsUpstreamChatRequest(t, transport.requestPath, transport.requestBody, "qa-stream-1781714246")
	if !strings.Contains(responseRecorder.Body.String(), openAIStreamTranslationUnsupportedErrorCode) || !strings.Contains(responseRecorder.Body.String(), "chat_stream_payload") {
		t.Fatalf("expected strict malformed stream rejection body, got %s", responseRecorder.Body.String())
	}
}

func assertResponsesToolsCompletedOutput(t *testing.T, streamBody string) {
	t.Helper()
	completed := translatedSSEEventPayload(t, parseTranslatedSSEEvents(t, streamBody), "response.completed")
	response := completed["response"].(map[string]any)
	output := response["output"].([]any)
	if len(output) == 1 && stringValue(output[0].(map[string]any)["type"]) == "message" {
		t.Fatalf("expected live path not to fall back to an empty assistant message, got %+v body %s", output, streamBody)
	}
	if len(output) != 3 {
		t.Fatalf("expected handleStreamingProxy to reconstruct three tool outputs, got %+v body %s", output, streamBody)
	}
	custom := output[0].(map[string]any)
	if custom["type"] != "custom_tool_call" || custom["name"] != "exec" || custom["input"] != "ls -la" {
		t.Fatalf("expected custom tool reconstruction through live path, got %+v", custom)
	}
	toolSearch := output[1].(map[string]any)
	if toolSearch["type"] != "tool_search_call" || toolSearch["execution"] != "client" || stringValue(nestedValue(toolSearch, "arguments", "query")) != "gmail" {
		t.Fatalf("expected tool_search reconstruction through live path, got %+v", toolSearch)
	}
	namespace := output[2].(map[string]any)
	if namespace["type"] != "function_call" || namespace["name"] != "_search_emails" || namespace["namespace"] != "mcp__apps__gmail" {
		t.Fatalf("expected namespace tool reconstruction through live path, got %+v", namespace)
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
	addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_891, 9_891, 0, requestPlanConnectionTargetOptions{
		openAITextCapability: stringPtr(providercompat.OpenAITextCapabilityChatCompletionsOnly),
	})
	connection := snapshot.TerminalTargetsByID[2_891]
	connection.Endpoint.BaseURL = "https://upstream.example"
	connection.UpstreamAuth = &runtimeConnectionUpstreamAuthSnapshot{
		AuthHeader:            "Authorization",
		AuthValue:             "Bearer redacted",
		ControlledHeaderNames: map[string]struct{}{"authorization": {}},
	}
	snapshot.TerminalTargetsByID[2_891] = connection
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

func assertResponsesToolsUpstreamChatRequest(t *testing.T, requestPath string, requestBody []byte, expectedModel string) {
	t.Helper()
	if requestPath != "/v1/chat/completions" {
		t.Fatalf("expected upstream chat completions path, got %q", requestPath)
	}
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(requestBody)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode upstream chat request: %v body %s", err, string(requestBody))
	}
	if payload["stream"] != true || stringValue(payload["model"]) != expectedModel {
		t.Fatalf("expected translated streaming chat request for responses-public, got %+v", payload)
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

func TestFinalResponseTranslationForSerializationFallsBackToFinalAttemptMetadata(t *testing.T) {
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
			Connection:                  connection,
			OperationTranslationMode:    TranslationModeOpenAIChatCompletionsToResponses,
			UpstreamOperationName:       openAIUpstreamOperationResponses,
			UpstreamRequestPath:         "/v1/responses",
			AuditEnabledAtRequest:       false,
			AuditCaptureBodiesAtRequest: false,
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
	responseRecorder := httptest.NewRecorder()
	proxyWriter := newRuntimeDeferredCommitWriter(responseRecorder)
	rawBody := []byte(`{"id":"resp_live_like","object":"response","created_at":1700000001,"model":"gpt-5.5","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"translated from final attempt"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16}}`)
	capture, err := service.writeBufferedNonStreamResponse(proxyWriter, plan, execution, finalResponseTranslation, rawBody)
	if err != nil {
		t.Fatalf("write translated response from final attempt metadata: %v", err)
	}
	proxyWriter.Commit()

	payload := decodeTranslationTestPayload(t, responseRecorder.Body.Bytes())
	if got := stringValue(payload["object"]); got != "chat.completion" {
		t.Fatalf("expected final attempt metadata to translate Responses body to Chat Completions, got object %q body %s", got, responseRecorder.Body.String())
	}
	if got := stringValue(payload["model"]); got != "chat-public" {
		t.Fatalf("expected requested model in translated body, got %q", got)
	}
	if _, ok := payload["choices"]; !ok {
		t.Fatalf("expected translated Chat response to contain choices, got %+v", payload)
	}
	if _, ok := payload["output"]; ok {
		t.Fatalf("expected translated Chat response not to expose raw Responses output, got %s", responseRecorder.Body.String())
	}
	if got := capture.extractedUsage().TotalTokens; got == nil || *got != 16 {
		t.Fatalf("expected translated capture usage from upstream body, got %+v", capture.extractedUsage())
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

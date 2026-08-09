package runtimetest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// SPEC §14.3 black-box matrix: the shared LLM planning change must keep
// operation-native wire behavior across OpenAI Chat/Responses, Anthropic and
// Gemini in streaming and non-streaming shapes while upstream selection follows
// the authored mixed (position, id) order.

type mixedOrderMatrixCase struct {
	name                  string
	apiFamily             string
	operationName         string
	requestPath           func(publicModelID string) string
	requestBody           func(publicModelID string, ignoredBodyModel string) any
	responseContentType   string
	responseBody          string
	responseContains      string
	wantUpstreamSuffix    string // path suffix after the endpoint prefix
	wantTerminalBodyModel bool   // body-bound ops rewrite model on terminal hit
}

type mixedOrderMatrixSeed struct {
	profileID     int
	apiFamily     string
	publicModelID string
	childModelID  string
	terminalFirst bool
	terminalURL   string
	childURL      string
	terminalKey   string
	childKey      string
	strategyType  string
}

func mixedOrderMatrixCases() []mixedOrderMatrixCase {
	chatStreamBody := "data: {\"id\":\"chatcmpl-mixed-order\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: [DONE]\n\n"
	responsesStreamBody := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}}}\n\n"
	anthropicStreamBody := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial anthropic\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	geminiStreamBody := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial gemini\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":11,\"totalTokenCount\":18}}\n\n"

	return []mixedOrderMatrixCase{
		{
			name:          "OpenAIChatCompletions",
			apiFamily:     "openai",
			operationName: "openai.chat_completions",
			requestPath:   func(string) string { return "/v1/chat/completions" },
			requestBody: func(publicModelID string, _ string) any {
				return map[string]any{"model": publicModelID, "messages": []map[string]any{{"role": "user", "content": "mixed order chat"}}}
			},
			responseBody:          `{"id":"mixed-order-chat","usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`,
			responseContains:      "mixed-order-chat",
			wantUpstreamSuffix:    "/v1/chat/completions",
			wantTerminalBodyModel: true,
		},
		{
			name:          "OpenAIChatCompletionsStream",
			apiFamily:     "openai",
			operationName: "openai.chat_completions",
			requestPath:   func(string) string { return "/v1/chat/completions" },
			requestBody: func(publicModelID string, _ string) any {
				return map[string]any{"model": publicModelID, "messages": []map[string]any{{"role": "user", "content": "mixed order chat stream"}}, "stream": true}
			},
			responseContentType:   "text/event-stream",
			responseBody:          chatStreamBody,
			responseContains:      "partial",
			wantUpstreamSuffix:    "/v1/chat/completions",
			wantTerminalBodyModel: true,
		},
		{
			name:          "OpenAIResponses",
			apiFamily:     "openai",
			operationName: "openai.responses",
			requestPath:   func(string) string { return "/v1/responses" },
			requestBody: func(publicModelID string, _ string) any {
				return map[string]any{"model": publicModelID, "input": "mixed order responses"}
			},
			responseBody:          `{"id":"mixed-order-responses","response":{"usage":{"input_tokens":19,"output_tokens":23,"total_tokens":42}}}`,
			responseContains:      "mixed-order-responses",
			wantUpstreamSuffix:    "/v1/responses",
			wantTerminalBodyModel: true,
		},
		{
			name:          "OpenAIResponsesStream",
			apiFamily:     "openai",
			operationName: "openai.responses",
			requestPath:   func(string) string { return "/v1/responses" },
			requestBody: func(publicModelID string, _ string) any {
				return map[string]any{"model": publicModelID, "input": "mixed order responses stream", "stream": true}
			},
			responseContentType:   "text/event-stream",
			responseBody:          responsesStreamBody,
			responseContains:      "partial",
			wantUpstreamSuffix:    "/v1/responses",
			wantTerminalBodyModel: true,
		},
		{
			name:          "AnthropicMessages",
			apiFamily:     "anthropic",
			operationName: "anthropic.messages",
			requestPath:   func(string) string { return "/v1/messages" },
			requestBody: func(publicModelID string, _ string) any {
				return map[string]any{"model": publicModelID, "messages": []map[string]any{{"role": "user", "content": "mixed order anthropic"}}, "max_tokens": 64}
			},
			responseBody:          `{"id":"mixed-order-anthropic","type":"message","usage":{"input_tokens":5,"output_tokens":8}}`,
			responseContains:      "mixed-order-anthropic",
			wantUpstreamSuffix:    "/v1/messages",
			wantTerminalBodyModel: true,
		},
		{
			name:          "AnthropicMessagesStream",
			apiFamily:     "anthropic",
			operationName: "anthropic.messages",
			requestPath:   func(string) string { return "/v1/messages" },
			requestBody: func(publicModelID string, _ string) any {
				return map[string]any{"model": publicModelID, "messages": []map[string]any{{"role": "user", "content": "mixed order anthropic stream"}}, "max_tokens": 64, "stream": true}
			},
			responseContentType:   "text/event-stream",
			responseBody:          anthropicStreamBody,
			responseContains:      "partial anthropic",
			wantUpstreamSuffix:    "/v1/messages",
			wantTerminalBodyModel: true,
		},
		{
			name:          "GeminiGenerateContent",
			apiFamily:     "gemini",
			operationName: "gemini.generate_content",
			requestPath: func(publicModelID string) string {
				return fmt.Sprintf("/v1beta/models/%s:generateContent", publicModelID)
			},
			requestBody: func(_ string, ignoredBodyModel string) any {
				return routeMatrixGeminiBody(ignoredBodyModel, "mixed order gemini generate", 0.44)
			},
			responseBody:       `{"responseId":"mixed-order-gemini-generate","usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":17,"totalTokenCount":28}}`,
			responseContains:   "mixed-order-gemini-generate",
			wantUpstreamSuffix: ":generateContent",
		},
		{
			name:          "GeminiStreamGenerateContent",
			apiFamily:     "gemini",
			operationName: "gemini.stream_generate_content",
			requestPath: func(publicModelID string) string {
				return fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", publicModelID)
			},
			requestBody: func(_ string, ignoredBodyModel string) any {
				return routeMatrixGeminiBody(ignoredBodyModel, "mixed order gemini stream", 0.55)
			},
			responseContentType: "text/event-stream",
			responseBody:        geminiStreamBody,
			responseContains:    "partial gemini",
			wantUpstreamSuffix:  ":streamGenerateContent",
		},
	}
}

func containsRuntimeTestString(value string, contains string) bool {
	return strings.Contains(value, contains)
}

func seedMixedOrderMatrixRoute(t *testing.T, harness *runtimeHarness, seed mixedOrderMatrixSeed) (publicModelID string, childModelID string, terminalConnectionID int) {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, seed.profileID, "mixed-order-matrix-"+randomSuffix(), seed.strategyType)
	publicModelConfigID := harness.seedModel(t, seed.profileID, seed.apiFamily, seed.publicModelID, "proxy", &strategyID)
	childModelConfigID := harness.seedModel(t, seed.profileID, seed.apiFamily, seed.childModelID, "native", &strategyID)
	terminalEndpointID := harness.seedEndpoint(t, seed.profileID, "mixed-order-terminal-endpoint-"+randomSuffix(), seed.terminalURL, seed.terminalKey)
	childEndpointID := harness.seedEndpoint(t, seed.profileID, "mixed-order-child-endpoint-"+randomSuffix(), seed.childURL, seed.childKey)
	if seed.terminalFirst {
		terminalConnectionID = harness.seedConnection(t, seed.profileID, publicModelConfigID, terminalEndpointID, "mixed-order-router-terminal-"+randomSuffix(), nil, nil, 0)
		harness.seedConnection(t, seed.profileID, childModelConfigID, childEndpointID, "mixed-order-child-terminal-"+randomSuffix(), nil, nil, 0)
		harness.seedProxyTargetAtPosition(t, publicModelConfigID, childModelConfigID, 1)
	} else {
		harness.seedConnection(t, seed.profileID, childModelConfigID, childEndpointID, "mixed-order-child-terminal-"+randomSuffix(), nil, nil, 0)
		harness.seedProxyTargetAtPosition(t, publicModelConfigID, childModelConfigID, 0)
		terminalConnectionID = harness.seedConnection(t, seed.profileID, publicModelConfigID, terminalEndpointID, "mixed-order-router-terminal-"+randomSuffix(), nil, nil, 1)
	}
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{seed.profileID}})
	return seed.publicModelID, seed.childModelID, terminalConnectionID
}

func runMixedOrderMatrixOrder(t *testing.T, terminalFirst bool) {
	for _, test := range mixedOrderMatrixCases() {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			terminalUpstream := newRouteMatrixUpstream(t, test.responseContentType, []byte(test.responseBody))
			childUpstream := newRouteMatrixUpstream(t, test.responseContentType, []byte(test.responseBody))
			suffix := randomSuffix()
			publicModelID := "mixed-order-public-" + suffix
			childModelID := "mixed-order-child-" + suffix
			ignoredBodyModel := "mixed-order-ignored-body-" + suffix
			publicModelID, childModelID, _ = seedMixedOrderMatrixRoute(t, harness, mixedOrderMatrixSeed{
				profileID:     profileID,
				apiFamily:     test.apiFamily,
				publicModelID: publicModelID,
				childModelID:  childModelID,
				terminalFirst: terminalFirst,
				terminalURL:   terminalUpstream.baseURL("/mixed-order/terminal"),
				childURL:      childUpstream.baseURL("/mixed-order/child"),
				terminalKey:   "mixed-order-terminal-key",
				childKey:      "mixed-order-child-key",
				strategyType:  "fill-first",
			})

			requestPath := test.requestPath(publicModelID)
			response := harness.requestJSON(t, http.MethodPost, requestPath, test.requestBody(publicModelID, ignoredBodyModel), nil)
			assertStatus(t, response, http.StatusOK)
			responseBody := readResponseBody(t, response)
			if !containsRuntimeTestString(responseBody, test.responseContains) {
				t.Fatalf("expected downstream response to contain %q, got %q", test.responseContains, responseBody)
			}

			var hitTerminalUpstream bool
			if terminalFirst {
				requests := terminalUpstream.requestsSnapshot()
				if len(requests) != 1 {
					t.Fatalf("expected mixed order [terminal@0, model@1] to hit the terminal upstream once, got %d requests", len(requests))
				}
				if len(childUpstream.requestsSnapshot()) != 0 {
					t.Fatalf("expected mixed order [terminal@0, model@1] to skip the child upstream")
				}
				hitTerminalUpstream = true
			} else {
				requests := childUpstream.requestsSnapshot()
				if len(requests) != 1 {
					t.Fatalf("expected mixed order [model@0, terminal@1] to hit the child upstream once, got %d requests", len(requests))
				}
				if len(terminalUpstream.requestsSnapshot()) != 0 {
					t.Fatalf("expected mixed order [model@0, terminal@1] to skip the terminal upstream")
				}
				hitTerminalUpstream = false
			}

			upstream := terminalUpstream
			wantModelID := publicModelID
			if !hitTerminalUpstream {
				upstream = childUpstream
				wantModelID = childModelID
			}
			upstreamRequest := upstream.lastRequest(t)
			if upstreamRequest.Method != http.MethodPost || !containsRuntimeTestString(upstreamRequest.Path, test.wantUpstreamSuffix) {
				t.Fatalf("expected operation-native upstream path %q, got %s %s", test.wantUpstreamSuffix, upstreamRequest.Method, upstreamRequest.Path)
			}
			if test.wantTerminalBodyModel {
				if got := requestModelID(t, upstreamRequest.Body); got != wantModelID {
					t.Fatalf("expected upstream body model %q, got %q in %s", wantModelID, got, string(upstreamRequest.Body))
				}
			} else if got := requestModelID(t, upstreamRequest.Body); got != ignoredBodyModel {
				t.Fatalf("expected path-bound operation to leave body model %q untouched, got %q", ignoredBodyModel, got)
			}
			waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
		})
	}
}

func TestRuntimeMixedOrderOperationMatrixTerminalFirst(t *testing.T) {
	runMixedOrderMatrixOrder(t, true)
}

func TestRuntimeMixedOrderOperationMatrixModelFirst(t *testing.T) {
	runMixedOrderMatrixOrder(t, false)
}

func TestRuntimeMixedOrderPlanningRejectionHasNoUpstreamSideEffect(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	terminalUpstream := newArrivalRecordingUpstream(t)
	defer terminalUpstream.close()
	childUpstream := newArrivalRecordingUpstream(t)
	defer childUpstream.close()
	suffix := randomSuffix()
	publicModelID := "mixed-order-reject-public-" + suffix
	childModelID := "mixed-order-reject-child-" + suffix
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "mixed-order-reject-"+suffix, "single")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	childModelConfigID := harness.seedModel(t, profileID, "openai", childModelID, "native", &strategyID)
	harness.seedEndpoint(t, profileID, "mixed-order-reject-terminal-endpoint-"+suffix, terminalUpstream.baseURL("/mixed-order/reject-terminal"), "reject-terminal-key")
	harness.seedEndpoint(t, profileID, "mixed-order-reject-child-endpoint-"+suffix, childUpstream.baseURL("/mixed-order/reject-child"), "reject-child-key")
	// Child model has no terminal leaves; the model peer at position 0 is the
	// only row `single` may consider. The terminal peer at position 1 must NOT
	// act as a fallback tier.
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, childModelConfigID, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(publicModelID, "single zero-leaf reject"), nil)
	assertStatus(t, response, http.StatusServiceUnavailable)
	terminalUpstream.assertNotStartedWithin(t, 2*time.Second)
	childUpstream.assertNotStartedWithin(t, 2*time.Second)
}

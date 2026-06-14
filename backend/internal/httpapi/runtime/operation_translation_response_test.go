package runtime

import (
	"bytes"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestTranslateOpenAIResponsesToChatResponse(t *testing.T) {
	rawBody := []byte(`{"id":"resp_123","created_at":1700000000,"model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}}`)

	translated, usage, usageRule, err := translateOpenAIResponsesUpstreamToChatClientResponseWithRequestedModel(rawBody, "")
	if err != nil {
		t.Fatalf("translate responses to chat response: %v", err)
	}
	wantUsage := generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3)
	if !reflect.DeepEqual(usage, wantUsage) {
		t.Fatalf("expected canonical responses usage %+v, got %+v", wantUsage, usage)
	}
	if !reflect.DeepEqual(usageRule, runtimeUsageRuleOpenAIResponses) {
		t.Fatalf("expected responses usage rule %+v, got %+v", runtimeUsageRuleOpenAIResponses, usageRule)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["object"]); got != "chat.completion" {
		t.Fatalf("expected chat completion object, got %q", got)
	}
	if got := intValue(intPointerFromAny(payload["created"])); got != 1700000000 {
		t.Fatalf("expected created timestamp 1700000000, got %+v", payload["created"])
	}
	choices := payload["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("expected one translated choice, got %+v", payload["choices"])
	}
	choice := choices[0].(map[string]any)
	if got := stringValue(choice["finish_reason"]); got != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", got)
	}
	message := choice["message"].(map[string]any)
	if got := stringValue(message["role"]); got != "assistant" {
		t.Fatalf("expected assistant role, got %q", got)
	}
	if got := stringValue(message["content"]); got != "hello" {
		t.Fatalf("expected translated assistant content hello, got %q", got)
	}
	usagePayload := payload["usage"].(map[string]any)
	if got := intValue(intPointerFromAny(usagePayload["prompt_tokens"])); got != 10 {
		t.Fatalf("expected prompt_tokens=10, got %+v", usagePayload)
	}
	if got := intValue(intPointerFromAny(usagePayload["completion_tokens"])); got != 6 {
		t.Fatalf("expected completion_tokens=6, got %+v", usagePayload)
	}
	if got := intValue(intPointerFromAny(nestedValue(usagePayload, "prompt_tokens_details", "cached_tokens"))); got != 4 {
		t.Fatalf("expected cached prompt detail=4, got %+v", usagePayload)
	}
	if got := intValue(intPointerFromAny(nestedValue(usagePayload, "completion_tokens_details", "reasoning_tokens"))); got != 3 {
		t.Fatalf("expected reasoning completion detail=3, got %+v", usagePayload)
	}
}

func TestTranslateOpenAIResponsesToChatResponseWithRequestedModel(t *testing.T) {
	rawBody := []byte(`{"id":"resp_123","object":"response","created_at":1700000000,"model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`)

	translated, _, _, err := translateOpenAIResponsesUpstreamToChatClientResponseWithRequestedModel(rawBody, "chat-public")
	if err != nil {
		t.Fatalf("translate responses to chat response with requested model: %v", err)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["object"]); got != "chat.completion" {
		t.Fatalf("expected chat completion object, got %q", got)
	}
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("expected translated Chat choices envelope, got %+v", payload["choices"])
	}
	if _, ok := payload["output"]; ok {
		t.Fatalf("expected translated Chat payload to omit raw Responses output, got %+v", payload)
	}
	if strings.Contains(string(translated), `"object":"response"`) {
		t.Fatalf("expected translated Chat payload to avoid raw Responses object leak, got %s", string(translated))
	}
	if got := stringValue(payload["model"]); got != "chat-public" {
		t.Fatalf("expected translated chat model to normalize to requested public model, got %q", got)
	}
	if got := stringValue(payload["model"]); got == "responses-target" {
		t.Fatalf("expected translated chat model to avoid leaking resolved target model, got %q", got)
	}
}

func TestTranslateOpenAIChatToResponsesResponse(t *testing.T) {
	rawBody := []byte(`{"id":"chatcmpl_123","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`)

	translated, usage, usageRule, err := translateOpenAIChatUpstreamToResponsesClientResponseWithRequestedModel(rawBody, "responses-public")
	if err != nil {
		t.Fatalf("translate chat to responses response: %v", err)
	}
	wantUsage := generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3)
	if !reflect.DeepEqual(usage, wantUsage) {
		t.Fatalf("expected canonical chat usage %+v, got %+v", wantUsage, usage)
	}
	if !reflect.DeepEqual(usageRule, runtimeUsageRuleOpenAIChatCompletions) {
		t.Fatalf("expected chat usage rule %+v, got %+v", runtimeUsageRuleOpenAIChatCompletions, usageRule)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["model"]); got != "responses-public" {
		t.Fatalf("expected translated responses model to normalize to requested public model, got %q", got)
	}
	if got := stringValue(payload["model"]); got == "chat-target" {
		t.Fatalf("expected translated responses model to avoid leaking resolved target model, got %q", got)
	}
	if got := stringValue(payload["object"]); got != "response" {
		t.Fatalf("expected response object, got %q", got)
	}
	if got := stringValue(payload["status"]); got != "completed" {
		t.Fatalf("expected completed status, got %q", got)
	}
	if got := intValue(intPointerFromAny(payload["created_at"])); got != 1700000001 {
		t.Fatalf("expected created_at 1700000001, got %+v", payload["created_at"])
	}
	output := payload["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("expected translated message output item, got %+v", output)
	}
	message := output[0].(map[string]any)
	if got := stringValue(message["type"]); got != "message" {
		t.Fatalf("expected first output item to be message, got %q", got)
	}
	parts := message["content"].([]any)
	if len(parts) != 1 || stringValue(parts[0].(map[string]any)["type"]) != "output_text" || stringValue(parts[0].(map[string]any)["text"]) != "thinking" {
		t.Fatalf("expected translated output_text content, got %+v", message["content"])
	}
	usagePayload := payload["usage"].(map[string]any)
	if got := intValue(intPointerFromAny(usagePayload["input_tokens"])); got != 10 {
		t.Fatalf("expected input_tokens=10, got %+v", usagePayload)
	}
	if got := intValue(intPointerFromAny(usagePayload["output_tokens"])); got != 6 {
		t.Fatalf("expected output_tokens=6, got %+v", usagePayload)
	}
	if got := intValue(intPointerFromAny(nestedValue(usagePayload, "input_tokens_details", "cached_tokens"))); got != 4 {
		t.Fatalf("expected cached input detail=4, got %+v", usagePayload)
	}
	if got := intValue(intPointerFromAny(nestedValue(usagePayload, "output_tokens_details", "reasoning_tokens"))); got != 3 {
		t.Fatalf("expected reasoning output detail=3, got %+v", usagePayload)
	}
}

func TestTranslateOpenAIResponseRefusal(t *testing.T) {
	responsesRaw := []byte(`{"id":"resp_refusal","model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"I cannot help with that."}]}]}`)
	chatBody, _, _, err := translateOpenAIResponsesUpstreamToChatClientResponseWithRequestedModel(responsesRaw, "")
	if err != nil {
		t.Fatalf("translate responses refusal to chat: %v", err)
	}
	chatPayload := decodeTranslationTestPayload(t, chatBody)
	chatMessage := chatPayload["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if got := stringValue(chatMessage["refusal"]); got != "I cannot help with that." {
		t.Fatalf("expected chat refusal, got %+v", chatMessage)
	}

	chatRaw := []byte(`{"id":"chatcmpl_refusal","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","refusal":"I cannot help with that."},"finish_reason":"stop"}]}`)
	responsesBody, _, _, err := translateOpenAIChatUpstreamToResponsesClientResponseWithRequestedModel(chatRaw, "responses-public")
	if err != nil {
		t.Fatalf("translate chat refusal to responses: %v", err)
	}
	responsesPayload := decodeTranslationTestPayload(t, responsesBody)
	parts := responsesPayload["output"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 1 || stringValue(parts[0].(map[string]any)["type"]) != "refusal" || stringValue(parts[0].(map[string]any)["refusal"]) != "I cannot help with that." {
		t.Fatalf("expected responses refusal content, got %+v", parts)
	}
}

func TestTranslateOpenAIResponseRejectsUnsupportedShape(t *testing.T) {
	tests := []struct {
		name   string
		mode   TranslationMode
		raw    []byte
		reason string
	}{
		{name: "responses function call", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"id":"resp_fn","output":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}]}`), reason: "responses_function_call"},
		{name: "chat tool calls", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"id":"chatcmpl_fn","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), reason: "chat_tool_calls"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := translateOpenAIResponse(test.raw, test.mode, "responses-public")
			var domainErr *domainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("expected domain error, got %v", err)
			}
			if domainErr.StatusCode != http.StatusBadGateway || domainErr.ErrorCode != openAIResponseTranslationUnsupportedErrorCode || domainErr.Detail != openAIResponseTranslationUnsupportedDetail {
				t.Fatalf("expected pinned response translation 502 contract, got %+v", domainErr)
			}
			if got := stringValue(domainErr.Fields["unsupported_reason"]); got != test.reason {
				t.Fatalf("expected unsupported reason %q, got %+v", test.reason, domainErr.Fields)
			}
		})
	}
}

func TestTranslateOpenAIChatToResponsesResponseRequestedEqualsResolved(t *testing.T) {
	rawBody := []byte(`{"id":"chatcmpl_123","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`)

	translated, _, _, err := translateOpenAIChatUpstreamToResponsesClientResponseWithRequestedModel(rawBody, "chat-target")
	if err != nil {
		t.Fatalf("translate chat to responses response: %v", err)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["model"]); got != "chat-target" {
		t.Fatalf("expected translated responses model to keep equal requested/resolved model, got %q", got)
	}
	if got := stringValue(payload["model"]); got == "responses-public" {
		t.Fatalf("expected translated responses equal-identity model to avoid alias swap, got %q", got)
	}
}

func TestTranslateOpenAIChatToResponsesResponsePreservesErrorPayload(t *testing.T) {
	rawBody := []byte(`{"id":"chatcmpl_123","created":1700000001,"model":"chat-target","error":{"message":"upstream failed","type":"server_error","code":"bad_gateway"},"choices":[{"index":0,"message":{"role":"assistant","content":"thinking"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`)

	translated, _, _, err := translateOpenAIChatUpstreamToResponsesClientResponseWithRequestedModel(rawBody, "responses-public")
	if err != nil {
		t.Fatalf("translate chat to responses response with error payload: %v", err)
	}
	payload := decodeTranslationTestPayload(t, translated)
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected translated responses payload to preserve error object, got %+v", payload)
	}
	if got := stringValue(errorPayload["message"]); got != "upstream failed" {
		t.Fatalf("expected preserved error message, got %q", got)
	}
	if got := stringValue(payload["model"]); got != "responses-public" {
		t.Fatalf("expected translated responses model to normalize to requested public model during error payload preservation, got %q", got)
	}
	if got := stringValue(payload["model"]); got == "chat-target" {
		t.Fatalf("expected translated responses error payload to avoid leaking resolved target model, got %q", got)
	}
}

func TestOperationResponseHooks_TranslatedOpenAINonStreamPreservesCanonicalUsageAndRawAudit(t *testing.T) {
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
				t.Fatalf("expected translated client payload containing %q, got %q", test.wantContains, forwarded.String())
			}
			if test.ingressPath == "/v1/responses" {
				if !strings.Contains(forwarded.String(), `"model":"responses-target"`) {
					t.Fatalf("expected equal requested/resolved responses payload to keep model responses-target, got %q", forwarded.String())
				}
				if strings.Contains(forwarded.String(), `"model":"responses-public"`) {
					t.Fatalf("expected equal requested/resolved responses payload to avoid swapping to public alias, got %q", forwarded.String())
				}
			}
			if test.ingressPath == "/v1/chat/completions" {
				if !strings.Contains(forwarded.String(), `"object":"chat.completion"`) {
					t.Fatalf("expected translated Chat payload object, got %q", forwarded.String())
				}
				if strings.Contains(forwarded.String(), `"object":"response"`) {
					t.Fatalf("expected translated Chat payload to avoid raw Responses object leak, got %q", forwarded.String())
				}
				if !strings.Contains(forwarded.String(), `"model":"chat-target"`) {
					t.Fatalf("expected equal requested/resolved chat payload to keep model chat-target, got %q", forwarded.String())
				}
				if strings.Contains(forwarded.String(), `"model":"chat-public"`) {
					t.Fatalf("expected equal requested/resolved chat payload to avoid swapping to public alias, got %q", forwarded.String())
				}
			}
			if string(capture.AuditBody) != test.payload {
				t.Fatalf("expected raw upstream audit body, got %q", string(capture.AuditBody))
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected canonical usage %+v, got %+v", test.wantUsage, got)
			}
		})
	}
}

func TestOperationResponseHooks_TranslatedOpenAIUnsupportedShapeReturnsDomainError(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyTranslatedOpenAINonEventResponseAndCapture(TranslationModeOpenAIResponsesToChatCompletions, "responses-public", &forwarded, strings.NewReader(`{"id":"chatcmpl_fn","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), fixedResponseHookTestNow, true)
	if !reflect.DeepEqual(capture, runtimeResponseCapture{}) {
		t.Fatalf("expected no capture on unsupported translated response, got %+v", capture)
	}
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	if domainErr.StatusCode != http.StatusBadGateway || domainErr.ErrorCode != openAIResponseTranslationUnsupportedErrorCode {
		t.Fatalf("expected response translation unsupported error, got %+v", domainErr)
	}
	if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "chat_tool_calls" {
		t.Fatalf("expected unsupported reason chat_tool_calls, got %+v", domainErr.Fields)
	}
	if forwarded.Len() != 0 || strings.TrimSpace(operation.Name) != openAIUpstreamOperationResponses {
		t.Fatalf("expected unsupported response hook to avoid forwarding bytes for %s, got %q", operation.Name, forwarded.String())
	}
}

func TestOperationResponseHooks_TranslatedHeaderSafety(t *testing.T) {
	filtered := filterTranslatedResponseHeaders(http.Header{
		"Content-Encoding": []string{"gzip"},
		"Content-Length":   []string{"999"},
		"Digest":           []string{"sha-256=raw"},
		"ETag":             []string{`"raw"`},
		"X-Request-Id":     []string{"req_translated"},
	})
	if filtered.Get("Content-Encoding") != "" || filtered.Get("Content-Length") != "" || filtered.Get("Digest") != "" || filtered.Get("ETag") != "" {
		t.Fatalf("expected translated response headers to drop unsafe entity metadata, got %v", filtered)
	}
	if filtered.Get("X-Request-Id") != "req_translated" {
		t.Fatalf("expected safe correlation header to survive translation filtering, got %v", filtered)
	}
}

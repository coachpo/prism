package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/providerauth"
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

func TestTranslateOpenAIResponsesToChatResponseIgnoresNullErrorField(t *testing.T) {
	rawBody := []byte(`{"id":"resp_null_error","object":"response","created_at":1700000000,"model":"responses-target","error":null,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16}}`)

	translated, usage, _, err := translateOpenAIResponsesUpstreamToChatClientResponseWithRequestedModel(rawBody, "chat-public")
	if err != nil {
		t.Fatalf("translate responses to chat response with null error: %v", err)
	}
	payload := decodeTranslationTestPayload(t, translated)
	if got := stringValue(payload["object"]); got != "chat.completion" {
		t.Fatalf("expected null error Responses payload to translate to Chat object, got %q body %s", got, string(translated))
	}
	if _, ok := payload["choices"]; !ok {
		t.Fatalf("expected translated Chat payload to contain choices, got %+v", payload)
	}
	if _, ok := payload["output"]; ok {
		t.Fatalf("expected translated Chat payload to omit raw Responses output, got %s", string(translated))
	}
	if got := stringValue(payload["model"]); got != "chat-public" {
		t.Fatalf("expected translated chat model to normalize to requested public model, got %q", got)
	}
	if got := usage.TotalTokens; got == nil || *got != 16 {
		t.Fatalf("expected usage to be extracted from Responses body, got %+v", usage)
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

func TestTranslateOpenAIChatToResponsesResponseWithToolContext(t *testing.T) {
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	connection := runtimeConnection{
		OpenAITextCapability: stringPtr(providerauth.OpenAITextCapabilityChatCompletionsOnly),
	}
	rawRequest := []byte(`{"model":"responses-public","input":"use tools","tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`)
	plan, translated, err := planCodingAgentFormatRequest(operation, rawRequest, "chat-target", connection)
	if err != nil {
		t.Fatalf("plan responses-to-chat tool request: %v", err)
	}
	if !translated || plan.ToolContext == nil {
		t.Fatalf("expected translated plan with request-scoped tool context, translated=%v context=%v", translated, plan.ToolContext)
	}
	namespaceChatName := plan.ToolContext.ChatNameForResponseFunction("_search_emails", "mcp__apps__gmail")
	upstreamRaw := []byte(fmt.Sprintf(`{"id":"chatcmpl_tools","created":1700000001,"model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_custom","type":"function","function":{"name":"exec","arguments":%q}},{"id":"call_namespace","type":"function","function":{"name":%q,"arguments":%q}},{"id":"call_search","type":"function","function":{"name":"tool_search","arguments":%q}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`, `{"input":"ls -la"}`, namespaceChatName, `{"query":"from:alerts"}`, `{"query":"gmail","limit":3}`))
	metadata := runtimeFinalResponseTranslationMetadata{RequestedModelID: "responses-public", ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient}
	var forwarded bytes.Buffer
	capture, err := proxyNonEventResponseAndCaptureForFinalAttemptWithRequestBody(metadata, rawRequest, &forwarded, bytes.NewReader(upstreamRaw), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate chat tool response with request context: %v", err)
	}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, generationResponseHookTestUsage(12, 5, 17)) {
		t.Fatalf("expected canonical chat usage, got %+v", got)
	}
	payload := decodeTranslationTestPayload(t, forwarded.Bytes())
	if got := stringValue(payload["model"]); got != "responses-public" {
		t.Fatalf("expected requested public responses model, got %q", got)
	}
	output := payload["output"].([]any)
	if len(output) != 3 {
		t.Fatalf("expected three reconstructed tool output items, got %+v", output)
	}
	custom := output[0].(map[string]any)
	if custom["type"] != "custom_tool_call" || custom["name"] != "exec" || custom["input"] != "ls -la" {
		t.Fatalf("expected custom tool reconstruction, got %+v", custom)
	}
	namespace := output[1].(map[string]any)
	if namespace["type"] != "function_call" || namespace["name"] != "_search_emails" || namespace["namespace"] != "mcp__apps__gmail" {
		t.Fatalf("expected namespace tool reconstruction, got %+v", namespace)
	}
	toolSearch := output[2].(map[string]any)
	if toolSearch["type"] != "tool_search_call" || toolSearch["execution"] != "client" || stringValue(nestedValue(toolSearch, "arguments", "query")) != "gmail" {
		t.Fatalf("expected tool_search reconstruction, got %+v", toolSearch)
	}
	if strings.Contains(forwarded.String(), "chat-target") {
		t.Fatalf("expected translated response not to leak resolved target model, got %s", forwarded.String())
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
		{name: "responses unsupported output type", mode: TranslationModeOpenAIChatCompletionsToResponses, raw: []byte(`{"id":"resp_bad","output":[{"type":"unknown"}]}`), reason: "responses_output_type"},
		{name: "chat empty choices", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"id":"chatcmpl_empty","choices":[]}`), reason: "chat_choices"},
		{name: "chat malformed choice", mode: TranslationModeOpenAIResponsesToChatCompletions, raw: []byte(`{"id":"chatcmpl_bad","choices":["bad"]}`), reason: "chat_choice"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := translateOpenAIResponse(test.raw, test.mode, "responses-public")
			var domainErr *domainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("expected domain error, got %v", err)
			}
			if domainErr.StatusCode != http.StatusBadGateway || domainErr.ErrorCode != openAIResponseTranslationUnsupportedErrorCode || domainErr.Detail != "Prism cannot translate this OpenAI response shape for the selected target." {
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
	capture, err := proxyTranslatedOpenAINonEventResponseAndCapture(TranslationModeOpenAIResponsesToChatCompletions, "responses-public", &forwarded, strings.NewReader(`{"id":"chatcmpl_empty","choices":[]}`), fixedResponseHookTestNow, true)
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
	if got := stringValue(domainErr.Fields["unsupported_reason"]); got != "chat_choices" {
		t.Fatalf("expected unsupported reason chat_choices, got %+v", domainErr.Fields)
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

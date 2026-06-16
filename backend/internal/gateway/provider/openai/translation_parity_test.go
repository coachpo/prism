package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

func TestTranslateResponsesToChatRequestToolsReasoningAndRichContent(t *testing.T) {
	raw := []byte(`{"model":"responses-public","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Need a tool."}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{ \"b\": 2, \"a\": 1 }"},{"type":"function_call_output","call_id":"call_1","output":{"ok":true}},{"type":"message","role":"user","content":[{"type":"input_text","text":"see this"},{"type":"input_image","image_url":"data:image/png;base64,abc"},{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}}]}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"lookup"},"parallel_tool_calls":true,"reasoning":{"effort":"high"}}`)
	_, body, err := translateRequest(raw, provider.TranslationModeOpenAIResponsesToChatCompletions, "chat-target")
	if err != nil {
		t.Fatalf("translate rich responses request: %v", err)
	}
	payload := decodeOpenAIParityPayload(t, body)
	if payload["parallel_tool_calls"] != true || stringValue(payload["reasoning_effort"]) != "high" {
		t.Fatalf("expected tool/reasoning fields, got %+v", payload)
	}
	messages := payload["messages"].([]any)
	assistant := messages[0].(map[string]any)
	if assistant["reasoning_content"] != "Need a tool." || assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"] != `{"a":1,"b":2}` {
		t.Fatalf("expected assistant tool call with reasoning and canonical args, got %+v", assistant)
	}
	userContent := messages[2].(map[string]any)["content"].([]any)
	if len(userContent) != 3 || userContent[1].(map[string]any)["type"] != "image_url" || userContent[2].(map[string]any)["type"] != "input_audio" {
		t.Fatalf("expected rich chat content, got %+v", userContent)
	}
}

func TestTranslateChatToResponsesRequestToolsAndDeterministicNRejection(t *testing.T) {
	raw := []byte(`{"model":"chat-public","messages":[{"role":"assistant","reasoning_content":"Need lookup","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{ \"q\": \"x\" }"}}]},{"role":"tool","tool_call_id":"call_1","content":"done"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}},"parallel_tool_calls":false}`)
	_, body, err := translateRequest(raw, provider.TranslationModeOpenAIChatCompletionsToResponses, "responses-target")
	if err != nil {
		t.Fatalf("translate chat tool request: %v", err)
	}
	payload := decodeOpenAIParityPayload(t, body)
	input := payload["input"].([]any)
	if input[0].(map[string]any)["type"] != "reasoning" || input[1].(map[string]any)["type"] != "function_call" || input[2].(map[string]any)["type"] != "function_call_output" {
		t.Fatalf("expected reasoning, function call, and output items, got %+v", input)
	}
	if payload["parallel_tool_calls"] != false || payload["tools"].([]any)[0].(map[string]any)["name"] != "lookup" {
		t.Fatalf("expected responses tool fields, got %+v", payload)
	}

	_, _, err = translateRequest([]byte(`{"model":"chat-public","messages":[],"n":2}`), provider.TranslationModeOpenAIChatCompletionsToResponses, "responses-target")
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Fields["unsupported_reason"] != "chat_multi_choice" {
		t.Fatalf("expected deterministic n>1 rejection, got %+v", err)
	}
}

func TestTranslateNonStreamResponsesToolsReasoningInlineThinkAndIncomplete(t *testing.T) {
	chatRaw := []byte(`{"id":"chatcmpl_1","created":123,"model":"chat-target","choices":[{"message":{"role":"assistant","reasoning_content":"Need lookup","content":"<think>hidden</think>visible","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{ \"b\": 2, \"a\": 1 }"}}]},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":7,"total_tokens":17,"completion_tokens_details":{"reasoning_tokens":2}}}`)
	body, usage, _, err := translateResponse(chatRaw, provider.TranslationModeOpenAIResponsesToChatCompletions, "responses-public")
	if err != nil {
		t.Fatalf("translate chat response: %v", err)
	}
	payload := decodeOpenAIParityPayload(t, body)
	if payload["status"] != "incomplete" || payload["incomplete_details"].(map[string]any)["reason"] != "max_output_tokens" {
		t.Fatalf("expected incomplete responses mapping, got %+v", payload)
	}
	output := payload["output"].([]any)
	if output[0].(map[string]any)["type"] != "reasoning" || output[1].(map[string]any)["content"].([]any)[0].(map[string]any)["text"] != "visible" || output[2].(map[string]any)["type"] != "function_call" {
		t.Fatalf("expected reasoning, visible text, and tool call output, got %+v", output)
	}
	if usage.ReasoningTokens == nil || *usage.ReasoningTokens != 2 {
		t.Fatalf("expected reasoning usage, got %+v", usage)
	}

	responsesRaw := []byte(`{"id":"resp_1","model":"responses-target","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"Need lookup"}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"},{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"No."}]}]}`)
	chatBody, _, _, err := translateResponse(responsesRaw, provider.TranslationModeOpenAIChatCompletionsToResponses, "chat-public")
	if err != nil {
		t.Fatalf("translate responses response: %v", err)
	}
	chatPayload := decodeOpenAIParityPayload(t, chatBody)
	message := chatPayload["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["reasoning_content"] != "Need lookup" || message["refusal"] != "No." || len(message["tool_calls"].([]any)) != 1 {
		t.Fatalf("expected chat reasoning, refusal, and tool call, got %+v", message)
	}
}

func decodeOpenAIParityPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

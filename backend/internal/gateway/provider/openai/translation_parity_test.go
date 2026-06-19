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

func TestTranslateResponsesToChatRequestMapsMaxOutputTokensByTargetModelFamily(t *testing.T) {
	tests := []struct {
		name           string
		targetModelID  string
		raw            []byte
		wantTokenField string
		absentField    string
		wantValue      int
	}{
		{name: "o-series target", targetModelID: "o3-mini", raw: []byte(`{"model":"responses-public","input":"hello","max_output_tokens":64}`), wantTokenField: "max_completion_tokens", absentField: "max_tokens", wantValue: 64},
		{name: "non-o-series target", targetModelID: "deepseek-v4-pro", raw: []byte(`{"model":"responses-public","input":"hello","max_output_tokens":64}`), wantTokenField: "max_tokens", absentField: "max_completion_tokens", wantValue: 64},
		{name: "canonical max output wins over chat alias", targetModelID: "deepseek-v4-pro", raw: []byte(`{"model":"responses-public","input":"hello","max_output_tokens":64,"max_tokens":200000}`), wantTokenField: "max_tokens", absentField: "max_completion_tokens", wantValue: 64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, body, err := translateRequest(test.raw, provider.TranslationModeOpenAIResponsesToChatCompletions, test.targetModelID)
			if err != nil {
				t.Fatalf("translate max output tokens request: %v", err)
			}

			payload := decodeOpenAIParityPayload(t, body)
			if got := intPointerFromAny(payload[test.wantTokenField]); got == nil || *got != test.wantValue {
				t.Fatalf("expected %s=%d, got %+v in %+v", test.wantTokenField, test.wantValue, payload[test.wantTokenField], payload)
			}
			if _, ok := payload[test.absentField]; ok {
				t.Fatalf("expected %s to be absent, got %+v", test.absentField, payload)
			}
		})
	}
}

func TestTranslateResponsesToChatRequestPassesChatTokenAliasWhenCanonicalAbsent(t *testing.T) {
	raw := []byte(`{"model":"responses-public","input":"hello","max_tokens":77}`)

	_, body, err := translateRequest(raw, provider.TranslationModeOpenAIResponsesToChatCompletions, "deepseek-v4-pro")
	if err != nil {
		t.Fatalf("translate chat token alias request: %v", err)
	}

	payload := decodeOpenAIParityPayload(t, body)
	if got := intPointerFromAny(payload["max_tokens"]); got == nil || *got != 77 {
		t.Fatalf("expected max_tokens=77, got %+v", payload)
	}
}

func TestTranslateResponsesToChatRequestPassesChatFieldsAndMergesStreamOptions(t *testing.T) {
	raw := []byte(`{"model":"responses-public","input":"hello","stream":true,"stream_options":{"include_usage":false,"chunking":"line"},"frequency_penalty":0.2,"presence_penalty":0.3,"logit_bias":{"42":-1},"logprobs":true,"top_logprobs":2,"n":1,"stop":["\n\n"],"response_format":{"type":"json_object"}}`)

	_, body, err := translateRequest(raw, provider.TranslationModeOpenAIResponsesToChatCompletions, "deepseek-v4-pro")
	if err != nil {
		t.Fatalf("translate chat passthrough request: %v", err)
	}

	payload := decodeOpenAIParityPayload(t, body)
	for _, field := range []string{"frequency_penalty", "presence_penalty", "logit_bias", "logprobs", "top_logprobs", "n", "stop", "response_format"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("expected passthrough field %q, got %+v", field, payload)
		}
	}
	streamOptions := payload["stream_options"].(map[string]any)
	if got, _ := streamOptions["include_usage"].(bool); !got {
		t.Fatalf("expected stream_options.include_usage=true, got %+v", streamOptions)
	}
	if got := stringValue(streamOptions["chunking"]); got != "line" {
		t.Fatalf("expected stream_options.chunking=line to survive merge, got %+v", streamOptions)
	}
}

func TestTranslateChatToResponsesRequestToolsAndDeterministicNRejection(t *testing.T) {
	raw := []byte(`{"model":"chat-public","messages":[{"role":"assistant","reasoning_content":"Need lookup","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{ \"q\": \"x\" }"}}]},{"role":"tool","tool_call_id":"call_1","content":"done"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`)
	_, body, err := translateRequest(raw, provider.TranslationModeOpenAIChatCompletionsToResponses, "responses-target")
	if err != nil {
		t.Fatalf("translate chat tool request: %v", err)
	}
	payload := decodeOpenAIParityPayload(t, body)
	input := payload["input"].([]any)
	if input[0].(map[string]any)["type"] != "reasoning" || input[1].(map[string]any)["type"] != "function_call" || input[2].(map[string]any)["type"] != "function_call_output" {
		t.Fatalf("expected reasoning, function call, and output items, got %+v", input)
	}
	if payload["tools"].([]any)[0].(map[string]any)["name"] != "lookup" {
		t.Fatalf("expected responses tool fields, got %+v", payload)
	}

	_, _, err = translateRequest([]byte(`{"model":"chat-public","messages":[],"n":2}`), provider.TranslationModeOpenAIChatCompletionsToResponses, "responses-target")
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Fields["unsupported_reason"] != "chat_multi_choice" {
		t.Fatalf("expected deterministic n>1 rejection, got %+v", err)
	}

	_, _, err = translateRequest([]byte(`{"model":"chat-public","messages":[],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","function":{"name":"lookup"}}]}}`), provider.TranslationModeOpenAIChatCompletionsToResponses, "responses-target")
	adapterErr = nil
	if !errors.As(err, &adapterErr) || adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Fields["unsupported_reason"] != "chat_tool_choice" {
		t.Fatalf("expected non-representable tool_choice rejection, got %+v", err)
	}
}

func TestTranslateResponsesToChatRequestRecordsDroppedToolMetadata(t *testing.T) {
	raw := []byte(`{"model":"responses-public","input":"hello","tools":[{"type":"image_generation","name":"draw"}],"tool_choice":{"type":"function","name":"draw"},"parallel_tool_calls":true}`)
	_, body, loss, err := translateRequestWithLoss(raw, provider.TranslationModeOpenAIResponsesToChatCompletions, "chat-target")
	if err != nil {
		t.Fatalf("translate lossy responses tools request: %v", err)
	}
	payload := decodeOpenAIParityPayload(t, body)
	if _, ok := payload["tools"]; ok {
		t.Fatalf("expected unsupported responses tool to drop, got %+v", payload)
	}
	assertOpenAIStringSliceContainsAll(t, loss.DroppedFields, []string{"responses_tools.0", "responses_tool_choice", "responses_parallel_tool_calls"}, "dropped fields")
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

func assertOpenAIStringSliceContainsAll(t *testing.T, got []string, want []string, label string) {
	t.Helper()
	values := map[string]struct{}{}
	for _, value := range got {
		values[value] = struct{}{}
	}
	for _, value := range want {
		if _, ok := values[value]; !ok {
			t.Fatalf("expected %s to contain %q, got %+v", label, value, got)
		}
	}
}

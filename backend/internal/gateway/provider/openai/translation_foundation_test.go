package openai

import "testing"

func TestToolContextReconstructsNamespaceCustomAndToolSearch(t *testing.T) {
	payload := map[string]any{
		"tools": []any{
			map[string]any{"type": "custom", "name": "exec"},
			map[string]any{"type": "tool_search"},
		},
		"input": []any{
			map[string]any{
				"type": "tool_search_output",
				"tools": []any{
					map[string]any{
						"type": "namespace",
						"name": "mcp__apps__gmail",
						"tools": []any{
							map[string]any{"type": "function", "name": "_search_emails", "parameters": map[string]any{"type": "object"}},
						},
					},
				},
			},
		},
	}

	context := BuildToolContextFromResponsesPayload(payload)
	if got := len(context.ChatTools()); got != 3 {
		t.Fatalf("expected three chat tools, got %d", got)
	}
	if !context.IsCustomToolChatName("exec") {
		t.Fatalf("expected exec to be tracked as custom tool")
	}
	chatName := context.ChatNameForResponseFunction("_search_emails", "mcp__apps__gmail")
	spec, ok := context.LookupChatName(chatName)
	if !ok || spec.Kind != ToolKindNamespace || spec.Name != "_search_emails" || spec.Namespace != "mcp__apps__gmail" {
		t.Fatalf("expected namespace spec reconstruction, got spec=%+v ok=%v", spec, ok)
	}

	custom := ResponseToolCallItemFromChatName("ctc_call_1", "completed", "call_1", "exec", `{"input":"ls -la"}`, "Need shell", context)
	if custom["type"] != "custom_tool_call" || custom["input"] != "ls -la" || custom["reasoning_content"] != "Need shell" {
		t.Fatalf("expected restored custom tool call, got %+v", custom)
	}
	toolSearch := ResponseToolCallItemFromChatName("fc_call_2", "completed", "call_2", "tool_search", `{"query":"gmail","limit":10}`, "", context)
	if toolSearch["type"] != "tool_search_call" || toolSearch["execution"] != "client" {
		t.Fatalf("expected restored tool search call, got %+v", toolSearch)
	}
}

func TestReasoningAndInlineThinkHelpers(t *testing.T) {
	reasoning := ExtractReasoningFieldText(map[string]any{"reasoning_details": []any{map[string]any{"text": "Need context"}, map[string]any{"summary": "Then answer"}}})
	if reasoning == nil || reasoning.Text != "Need context\n\nThen answer" {
		t.Fatalf("expected reasoning details extraction, got %+v", reasoning)
	}
	think, answer, ok := splitLeadingThinkBlock("\n <think>\nplan\n</think>\n\nanswer")
	if !ok || think != "plan" || answer != "answer" {
		t.Fatalf("expected leading think split, got think=%q answer=%q ok=%v", think, answer, ok)
	}

	state := NewInlineThinkState()
	decision, _, text := state.Push("<thi")
	if decision != InlineThinkNeedMore || text != "" {
		t.Fatalf("expected partial think tag to need more, got decision=%s text=%q", decision, text)
	}
	decision, think, text = state.Push("nk>hidden</think>visible")
	if decision != InlineThinkReasoningDecision || think != "hidden" || text != "visible" {
		t.Fatalf("expected completed think block, got decision=%s think=%q text=%q", decision, think, text)
	}
}

func TestContentErrorAndStreamStateHelpers(t *testing.T) {
	content := ResponsesContentPartsToChatContent([]ResponsesContentPart{
		{Kind: ResponsesContentPartInputText, Text: "hello"},
		{Kind: ResponsesContentPartOutputText, Text: "world"},
	})
	if content != "hello\nworld" {
		t.Fatalf("expected text-only content collapse, got %#v", content)
	}
	withFile := ResponsesContentPartsToChatContent([]ResponsesContentPart{{Kind: ResponsesContentPartInputText, Text: "read"}, {Kind: ResponsesContentPartInputFile, File: map[string]any{"file_id": "file_1"}}})
	if parts, ok := withFile.([]any); !ok || len(parts) != 2 {
		t.Fatalf("expected non-text content parts, got %#v", withFile)
	}

	arguments := canonicalToolArguments(map[string]any{"b": float64(2), "a": float64(1)})
	if arguments != `{"a":1,"b":2}` {
		t.Fatalf("expected canonical arguments, got %q", arguments)
	}
	errorObject := NormalizedOpenAIErrorObject(map[string]any{"base_resp": map[string]any{"status_code": float64(2013), "status_msg": "invalid params"}})
	errorPayload := errorObject["error"].(map[string]any)
	if errorPayload["message"] != "invalid params" || errorPayload["type"] != "upstream_error" || errorPayload["code"] != float64(2013) {
		t.Fatalf("expected normalized upstream error, got %+v", errorObject)
	}

	streamState := NewChatToResponsesState(nil)
	if streamState.AllocateOutputIndex() != 0 || streamState.AllocateOutputIndex() != 1 {
		t.Fatalf("expected monotonic output indexes")
	}
	streamState.Tools[0] = &ToolCallState{}
	streamState.Tools[0].ApplyDelta("call_1", "lookup", `{ "b": 2,`, "Need data")
	streamState.Tools[0].ApplyDelta("", "", ` "a": 1 }`, "")
	if !streamState.HasSubstantiveOutput() || streamState.Tools[0].CanonicalArguments() != `{"a":1,"b":2}` {
		t.Fatalf("expected substantive canonical tool state, got %+v args=%q", streamState.Tools[0], streamState.Tools[0].CanonicalArguments())
	}
}

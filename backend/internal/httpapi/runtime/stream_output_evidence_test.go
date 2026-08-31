package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestOpenAIChatCompletionsOutputEventAllowlist proves only visible text and
// tool-output increments qualify as output evidence: usage chunks, terminal
// finish-reason chunks, reasoning deltas, and control payloads never count.
func TestOpenAIChatCompletionsOutputEventAllowlist(t *testing.T) {
	tests := []struct {
		name          string
		payload       string
		wantVisible   bool
		wantReasoning bool
	}{
		{name: "content delta counts", payload: `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`, wantVisible: true},
		{name: "empty content delta does not count", payload: `{"choices":[{"index":0,"delta":{"content":""}}]}`},
		{name: "tool call arguments count", payload: `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"f","arguments":"{\"x\":"}}]}}]}`, wantVisible: true},
		{name: "legacy function call arguments count", payload: `{"choices":[{"index":0,"delta":{"function_call":{"name":"f","arguments":"{\"x\":"}}}]}`, wantVisible: true},
		{name: "tool call id-only update does not count", payload: `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function"}]}}]}`},
		{name: "finish reason chunk does not count", payload: `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`},
		{name: "usage-only final chunk does not count", payload: `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`},
		{name: "reasoning_content delta is separate evidence", payload: `{"choices":[{"index":0,"delta":{"reasoning_content":"thinking"}}]}`, wantReasoning: true},
		{name: "role delta does not count", payload: `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := decodeJSONObjectForTest(t, test.payload)
			got := collectOpenAIChatCompletionsOutputEvent("", payload)
			if got.VisibleOutput != test.wantVisible || got.ReasoningObserved != test.wantReasoning {
				t.Fatalf("collectOpenAIChatCompletionsOutputEvent = %+v, want visible=%v reasoning=%v", got, test.wantVisible, test.wantReasoning)
			}
		})
	}
}

// TestOpenAIResponsesOutputEventAllowlist proves the Responses allowlist counts
// text and function-argument deltas and excludes reasoning and terminal
// events.
func TestOpenAIResponsesOutputEventAllowlist(t *testing.T) {
	tests := []struct {
		name          string
		event         string
		payload       string
		wantVisible   bool
		wantReasoning bool
	}{
		{name: "output text delta counts", event: "response.output_text.delta", payload: `{"type":"response.output_text.delta","delta":"hi"}`, wantVisible: true},
		{name: "function call arguments delta counts", event: "response.function_call_arguments.delta", payload: `{"type":"response.function_call_arguments.delta","delta":"{\"x\":"}`, wantVisible: true},
		{name: "custom tool input delta counts", event: "response.custom_tool_call_input.delta", payload: `{"type":"response.custom_tool_call_input.delta","delta":"abc"}`, wantVisible: true},
		{name: "reasoning summary delta is separate evidence", event: "response.reasoning_summary_text.delta", payload: `{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`, wantReasoning: true},
		{name: "reasoning text delta is separate evidence", event: "response.reasoning_text.delta", payload: `{"type":"response.reasoning_text.delta","delta":"thinking"}`, wantReasoning: true},
		{name: "terminal completed event does not count", event: "response.completed", payload: `{"type":"response.completed","response":{}}`},
		{name: "item lifecycle events do not count", event: "response.output_item.added", payload: `{"type":"response.output_item.added","item":{}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := decodeJSONObjectForTest(t, test.payload)
			got := collectOpenAIResponsesOutputEvent(test.event, payload)
			if got.VisibleOutput != test.wantVisible || got.ReasoningObserved != test.wantReasoning {
				t.Fatalf("collectOpenAIResponsesOutputEvent = %+v, want visible=%v reasoning=%v", got, test.wantVisible, test.wantReasoning)
			}
		})
	}
}

// TestAnthropicMessagesOutputEventAllowlist proves text_delta and
// input_json_delta count while thinking, signature, and lifecycle events do
// not.
func TestAnthropicMessagesOutputEventAllowlist(t *testing.T) {
	tests := []struct {
		name          string
		event         string
		payload       string
		wantVisible   bool
		wantReasoning bool
	}{
		{name: "text delta counts", event: "content_block_delta", payload: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`, wantVisible: true},
		{name: "input json delta counts", event: "content_block_delta", payload: `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}`, wantVisible: true},
		{name: "thinking delta is separate evidence", event: "content_block_delta", payload: `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`, wantReasoning: true},
		{name: "thinking block start is separate evidence", event: "content_block_start", payload: `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`, wantReasoning: true},
		{name: "signature delta does not count", event: "content_block_delta", payload: `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`},
		{name: "message delta usage does not count", event: "message_delta", payload: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`},
		{name: "message start does not count", event: "message_start", payload: `{"type":"message_start","message":{}}`},
		{name: "message stop terminal does not count", event: "message_stop", payload: `{"type":"message_stop"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := decodeJSONObjectForTest(t, test.payload)
			got := collectAnthropicMessagesOutputEvent(test.event, payload)
			if got.VisibleOutput != test.wantVisible || got.ReasoningObserved != test.wantReasoning {
				t.Fatalf("collectAnthropicMessagesOutputEvent = %+v, want visible=%v reasoning=%v", got, test.wantVisible, test.wantReasoning)
			}
		})
	}
}

// TestGeminiStreamGenerateContentOutputEventAllowlist proves candidate text
// and functionCall parts count while thought parts and usage frames do not.
func TestGeminiStreamGenerateContentOutputEventAllowlist(t *testing.T) {
	tests := []struct {
		name          string
		payload       string
		wantVisible   bool
		wantReasoning bool
	}{
		{name: "text part counts", payload: `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`, wantVisible: true},
		{name: "functionCall part counts", payload: `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","args":{}}}]}}]}`, wantVisible: true},
		{name: "thought part is separate evidence", payload: `{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]}}]}`, wantReasoning: true},
		{name: "usage terminal frame does not count", payload: `{"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":5,"totalTokenCount":8}}`},
		{name: "empty parts do not count", payload: `{"candidates":[{"content":{"parts":[{}]}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := decodeJSONObjectForTest(t, test.payload)
			got := collectGeminiStreamGenerateContentOutputEvent("", payload)
			if got.VisibleOutput != test.wantVisible || got.ReasoningObserved != test.wantReasoning {
				t.Fatalf("collectGeminiStreamGenerateContentOutputEvent = %+v, want visible=%v reasoning=%v", got, test.wantVisible, test.wantReasoning)
			}
		})
	}
}

// TestSSECaptureOutputEvidenceClassification drives the full SSE capture with
// a scripted clock and proves the classification outcomes: measured,
// single-event, short-span burst, missing usage, and incomplete streams.
func TestSSECaptureOutputEvidenceClassification(t *testing.T) {
	hooks, ok := streamHooksForOperation(newRuntimeOperation("openai.chat_completions", "openai", "/v1/chat/completions", staticRuntimeOperationPath("/v1/chat/completions"), false, RuntimeOperationModelBindingBody))
	if !ok {
		t.Fatal("expected openai.chat_completions stream hooks")
	}
	type chunk struct {
		body string
		at   time.Time
	}
	run := func(name string, chunks []chunk) runtimeResponseCapture {
		t.Helper()
		capture := sseCompletedResponseCapture{streamHooks: hooks}
		for _, c := range chunks {
			for _, line := range strings.SplitAfter(c.body, "\n") {
				if line == "" {
					continue
				}
				capture.consumeLine([]byte(line), c.at)
			}
			capture.finishEvent(c.at)
		}
		outcome := classifySSEStreamOutcome(nil, capture.terminalSignal, nil, nil)
		return capture.runtimeResponseCapture(outcome)
	}

	t.Run("measured at exactly 50ms", func(t *testing.T) {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		captured := run("measured", []chunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n", at: base},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n", at: base.Add(50 * time.Millisecond)},
			{body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n", at: base.Add(60 * time.Millisecond)},
			{body: "data: [DONE]\n\n", at: base.Add(60 * time.Millisecond)},
		})
		assertOutputDelivery(t, captured, "measured", "", 2, 50)
		if captured.Usage.OutputTokens == nil || *captured.Usage.OutputTokens != 2 {
			t.Fatalf("expected output tokens 2, got %v", captured.Usage.OutputTokens)
		}
	})

	t.Run("unmeasurable at 49ms", func(t *testing.T) {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		captured := run("short", []chunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n", at: base},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n", at: base.Add(49 * time.Millisecond)},
			{body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n", at: base.Add(49 * time.Millisecond)},
			{body: "data: [DONE]\n\n", at: base.Add(49 * time.Millisecond)},
		})
		assertOutputDelivery(t, captured, "unmeasurable", "unmeasurable_output_span_below_threshold", 2, 49)
	})

	t.Run("51ms stays measured", func(t *testing.T) {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		captured := run("just-over", []chunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n", at: base},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n", at: base.Add(51 * time.Millisecond)},
			{body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n", at: base.Add(51 * time.Millisecond)},
			{body: "data: [DONE]\n\n", at: base.Add(51 * time.Millisecond)},
		})
		assertOutputDelivery(t, captured, "measured", "", 2, 51)
	})

	t.Run("single visible event is unmeasurable", func(t *testing.T) {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		captured := run("single", []chunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"only\"}}]}\n\n", at: base},
			{body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":5,\"total_tokens\":6}}\n\n", at: base.Add(time.Second)},
			{body: "data: [DONE]\n\n", at: base.Add(time.Second)},
		})
		assertOutputDelivery(t, captured, "unmeasurable", "unmeasurable_single_output_event", 1, 0)
	})

	t.Run("gemini single frame", func(t *testing.T) {
		// Gemini path-streaming delivers one content frame: a single output
		// event with no progressive span to measure.
		geminiHooks, ok := streamHooksForOperation(newRuntimeOperation("gemini.stream_generate_content", "gemini", "/v1beta/models/{model}:streamGenerateContent", geminiRuntimeOperationPath(":streamGenerateContent"), true, RuntimeOperationModelBindingPath))
		if !ok {
			t.Fatal("expected gemini.stream_generate_content stream hooks")
		}
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		capture := sseCompletedResponseCapture{streamHooks: geminiHooks}
		capture.consumeLine([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"one frame\"}],\"role\":\"model\"}}]}\n"), base)
		capture.finishEvent(base)
		capture.consumeLine([]byte("data: {\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":9,\"totalTokenCount\":12}}\n"), base.Add(time.Second))
		capture.finishEvent(base.Add(time.Second))
		captured := capture.runtimeResponseCapture(classifySSEStreamOutcome(nil, capture.terminalSignal, nil, nil))
		if captured.StreamOutcome != runtimeStreamOutcomeCompleted {
			t.Fatalf("expected completed outcome, got %q", captured.StreamOutcome)
		}
		assertOutputDelivery(t, captured, "unmeasurable", "unmeasurable_single_output_event", 1, 0)
	})

	t.Run("missing usage is unmeasurable", func(t *testing.T) {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		captured := run("no-usage", []chunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n", at: base},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n", at: base.Add(80 * time.Millisecond)},
			{body: "data: [DONE]\n\n", at: base.Add(80 * time.Millisecond)},
		})
		assertOutputDelivery(t, captured, "unmeasurable", "unmeasurable_missing_output_usage", 2, 80)
	})

	t.Run("split reasoning tokens keep the base-output numerator aligned", func(t *testing.T) {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		captured := run("reasoning", []chunk{
			{body: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n", at: base},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n", at: base},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n", at: base.Add(80 * time.Millisecond)},
			{body: "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":9,\"total_tokens\":10,\"completion_tokens_details\":{\"reasoning_tokens\":5}}}\n\n", at: base.Add(90 * time.Millisecond)},
			{body: "data: [DONE]\n\n", at: base.Add(90 * time.Millisecond)},
		})
		assertOutputDelivery(t, captured, "measured", "", 2, 80)
		if captured.Usage.OutputTokens == nil || *captured.Usage.OutputTokens != 4 {
			t.Fatalf("expected canonical base output 4 after reasoning split, got %v", captured.Usage.OutputTokens)
		}
	})

	t.Run("anthropic thinking without a usage split is unaligned", func(t *testing.T) {
		anthropicHooks, ok := streamHooksForOperation(newRuntimeOperation("anthropic.messages", "anthropic", "/v1/messages", staticRuntimeOperationPath("/v1/messages"), false, RuntimeOperationModelBindingBody))
		if !ok {
			t.Fatal("expected anthropic.messages stream hooks")
		}
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		capture := sseCompletedResponseCapture{streamHooks: anthropicHooks}
		feed := func(event string, payload string, at time.Time) {
			capture.consumeLine([]byte("event: "+event+"\n"), at)
			capture.consumeLine([]byte("data: "+payload+"\n"), at)
			capture.finishEvent(at)
		}
		feed("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":2}}}`, base)
		feed("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hidden"}}`, base)
		feed("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"a"}}`, base.Add(20*time.Millisecond))
		feed("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"b"}}`, base.Add(80*time.Millisecond))
		feed("message_delta", `{"type":"message_delta","usage":{"output_tokens":7}}`, base.Add(90*time.Millisecond))
		feed("message_stop", `{"type":"message_stop"}`, base.Add(90*time.Millisecond))
		captured := capture.runtimeResponseCapture(classifySSEStreamOutcome(nil, capture.terminalSignal, nil, nil))
		assertOutputDelivery(t, captured, "unmeasurable", "unmeasurable_reasoning_tokens_unaligned", 2, 60)
	})

	t.Run("incomplete stream keeps facts without a rate", func(t *testing.T) {
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		capture := sseCompletedResponseCapture{streamHooks: hooks}
		capture.consumeLine([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"), base)
		capture.finishEvent(base)
		capture.consumeLine([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n"), base.Add(80*time.Millisecond))
		capture.finishEvent(base.Add(80 * time.Millisecond))
		// Stream ends without a terminal event.
		captured := capture.runtimeResponseCapture(classifySSEStreamOutcome(nil, capture.terminalSignal, nil, nil))
		assertOutputDelivery(t, captured, "unmeasurable", "unmeasurable_incomplete_stream", 2, 80)
	})
}

// TestNonStreamAndImageClassifications proves the not_applicable verdicts:
// buffered text responses and image operations never become rate samples.
func TestNonStreamAndImageClassifications(t *testing.T) {
	t.Run("non-stream text is not applicable", func(t *testing.T) {
		captured := classifyOutputDelivery(operationResponseKindTextGeneration, runtimeStreamOutcomeNotStreaming, responseUsage{}, outputDeliveryEvidence{})
		assertOutputDelivery(t, runtimeResponseCapture{OutputDelivery: captured, StreamOutcome: runtimeStreamOutcomeNotStreaming}, "not_applicable", "not_applicable_non_stream", 0, 0)
	})

	t.Run("image operation is not applicable even when streaming", func(t *testing.T) {
		captured := classifyOutputDelivery(operationResponseKindImageGeneration, runtimeStreamOutcomeCompleted, responseUsage{OutputTokens: intPtr(1)}, outputDeliveryEvidence{})
		assertOutputDelivery(t, runtimeResponseCapture{OutputDelivery: captured}, "not_applicable", "not_applicable_image_operation", 0, 0)
	})

	t.Run("token count operation is not applicable", func(t *testing.T) {
		captured := classifyOutputDelivery(operationResponseKindTokenCount, runtimeStreamOutcomeNotStreaming, responseUsage{}, outputDeliveryEvidence{})
		assertOutputDelivery(t, runtimeResponseCapture{OutputDelivery: captured}, "not_applicable", "not_applicable_non_text_operation", 0, 0)
	})
}

// TestProxyEventStreamCaptureEndToEnd feeds the GLM-style burst fixture
// through the public capture entrypoint and proves the artifact becomes null
// while the request facts remain.
func TestProxyEventStreamCaptureEndToEnd(t *testing.T) {
	operation := newRuntimeOperation("openai.chat_completions", "openai", "/v1/chat/completions", staticRuntimeOperationPath("/v1/chat/completions"), false, RuntimeOperationModelBindingBody)
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":53,\"total_tokens\":63}}\n\n" +
		"data: [DONE]\n\n"
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := 0
	now := func() time.Time {
		clock++
		// Every line is observed inside the same millisecond: a buffered burst.
		return base.Add(time.Duration(clock) * time.Microsecond)
	}
	capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, nil, &bytes.Buffer{}, strings.NewReader(stream), now, false)
	if err != nil {
		t.Fatalf("capture stream: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed outcome, got %q", capture.StreamOutcome)
	}
	assertOutputDelivery(t, capture, "unmeasurable", "unmeasurable_output_span_below_threshold", 2, 0)
	if capture.OutputDelivery.SpanMS == nil || *capture.OutputDelivery.SpanMS != 0 {
		t.Fatalf("expected the sub-millisecond observed span to persist as 0ms, got %v", capture.OutputDelivery.SpanMS)
	}
	if capture.Usage.OutputTokens == nil || *capture.Usage.OutputTokens != 53 {
		t.Fatalf("expected the 53-token usage fact preserved, got %v", capture.Usage.OutputTokens)
	}
}

func assertOutputDelivery(t *testing.T, capture runtimeResponseCapture, state string, reason string, eventCount int, spanMS int) {
	t.Helper()
	measurement := capture.OutputDelivery
	if measurement.State != state {
		t.Fatalf("expected state %q, got %q", state, measurement.State)
	}
	if reason == "" {
		if measurement.Reason != nil {
			t.Fatalf("expected no reason for state %q, got %q", state, *measurement.Reason)
		}
	} else if measurement.Reason == nil || *measurement.Reason != reason {
		t.Fatalf("expected reason %q, got %v", reason, measurement.Reason)
	}
	if eventCount > 0 {
		if measurement.EventCount == nil || *measurement.EventCount != eventCount {
			t.Fatalf("expected event count %d, got %v", eventCount, measurement.EventCount)
		}
	}
	if spanMS > 0 {
		if measurement.SpanMS == nil || *measurement.SpanMS != spanMS {
			t.Fatalf("expected span %dms, got %v", spanMS, measurement.SpanMS)
		}
	}
}

func decodeJSONObjectForTest(t *testing.T, payload string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		t.Fatalf("decode payload %s: %v", payload, err)
	}
	return value
}

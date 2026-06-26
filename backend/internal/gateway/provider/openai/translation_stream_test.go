package openai

import (
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

func TestChatStreamReasoningContentTranslatesToResponsesReasoning(t *testing.T) {
	translator, err := NewStreamTranslatorWithToolContext(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses-public", nil)
	if err != nil {
		t.Fatalf("new stream translator: %v", err)
	}
	frames, err := translator.ConsumeEvent("", map[string]any{"id": "chatcmpl_reasoning", "model": "chat-target", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": "plan"}}}})
	if err != nil {
		t.Fatalf("consume reasoning delta: %v", err)
	}
	done, err := translator.ConsumeDone()
	if err != nil {
		t.Fatalf("consume done: %v", err)
	}
	body := string(joinStreamFrames(append(frames, done...)))
	if !strings.Contains(body, "response.reasoning_text.delta") || !strings.Contains(body, `"type":"reasoning"`) || strings.Contains(body, `"type":"output_text","text":"plan"`) {
		t.Fatalf("expected reasoning delta/final item without visible text leak, got %s", body)
	}
}

func TestChatStreamLeadingThinkTranslatesToReasoningAndVisibleText(t *testing.T) {
	translator, err := NewStreamTranslatorWithToolContext(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses-public", nil)
	if err != nil {
		t.Fatalf("new stream translator: %v", err)
	}
	var frames [][]byte
	for _, content := range []string{"<thi", "nk>hidden</think>", "visible"} {
		chunk, err := translator.ConsumeEvent("", map[string]any{"id": "chatcmpl_think", "model": "chat-target", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}}}})
		if err != nil {
			t.Fatalf("consume content delta %q: %v", content, err)
		}
		frames = append(frames, chunk...)
	}
	done, err := translator.ConsumeDone()
	if err != nil {
		t.Fatalf("consume done: %v", err)
	}
	body := string(joinStreamFrames(append(frames, done...)))
	if !strings.Contains(body, "response.reasoning_text.delta") || !strings.Contains(body, `"delta":"hidden"`) || !strings.Contains(body, `"delta":"visible"`) || strings.Contains(body, "<think>") {
		t.Fatalf("expected split think reasoning and visible answer only, got %s", body)
	}
}

func TestResponsesStreamReasoningDeltaTranslatesToChatReasoningContent(t *testing.T) {
	translator, err := NewStreamTranslatorWithToolContext(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat-public", nil)
	if err != nil {
		t.Fatalf("new stream translator: %v", err)
	}
	frames, err := translator.ConsumeEvent("response.reasoning_text.delta", map[string]any{"type": "response.reasoning_text.delta", "output_index": 0, "item_id": "rs_1", "delta": "plan"})
	if err != nil {
		t.Fatalf("consume responses reasoning delta: %v", err)
	}
	done, err := translator.ConsumeEvent("response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":    "resp_reasoning",
			"model": "responses-target",
			"output": []any{
				map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "plan"}}},
				map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "answer"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("consume completed: %v", err)
	}
	body := string(joinStreamFrames(append(frames, done...)))
	if !strings.Contains(body, `"reasoning_content":"plan"`) || !strings.Contains(body, `"content":"answer"`) || strings.Contains(body, "response.reasoning_text.delta") {
		t.Fatalf("expected chat reasoning_content and visible answer without raw Responses event, got %s", body)
	}
}

func joinStreamFrames(frames [][]byte) []byte {
	var joined []byte
	for _, frame := range frames {
		joined = append(joined, frame...)
	}
	return joined
}

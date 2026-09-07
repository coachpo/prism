package runtimetest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

const opencodeToolMarker = "PRISM_CONTROLLED_TOOL_RESULT_1827"
const opencodeFinalMarker = "PRISM_CONTROLLED_FINAL_1827"

type opencodeExchange struct {
	Path          string          `json:"path"`
	Authorization string          `json:"authorization,omitempty"`
	APIKey        string          `json:"api_key,omitempty"`
	GoogleKey     string          `json:"google_key,omitempty"`
	Body          json.RawMessage `json:"body"`
	ToolResult    bool            `json:"tool_result"`
}

type opencodeUpstream struct {
	mu        sync.Mutex
	exchanges []opencodeExchange
	toolFile  string
}

func (u *opencodeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	continuation := strings.Contains(string(body), opencodeToolMarker)
	u.mu.Lock()
	u.exchanges = append(u.exchanges, opencodeExchange{Path: r.URL.RequestURI(), Authorization: r.Header.Get("Authorization"), APIKey: r.Header.Get("X-Api-Key"), GoogleKey: r.Header.Get("X-Goog-Api-Key"), Body: body, ToolResult: continuation})
	u.mu.Unlock()
	var request map[string]any
	_ = json.Unmarshal(body, &request)
	model, _ := request["model"].(string)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	args, _ := json.Marshal(map[string]string{"filePath": u.toolFile})
	switch {
	case strings.Contains(r.URL.Path, "chat/completions"):
		writeOpenCodeChat(w, model, string(args), continuation)
	case strings.HasSuffix(r.URL.Path, "responses"):
		writeOpenCodeResponses(w, string(args), continuation)
	case strings.HasSuffix(r.URL.Path, "messages"):
		writeOpenCodeAnthropic(w, model, string(args), continuation)
	case strings.Contains(r.URL.Path, ":streamGenerateContent"):
		parts := []any{map[string]any{"text": "before-tool"}, map[string]any{"functionCall": map[string]any{"name": "read", "args": map[string]string{"filePath": u.toolFile}}}}
		if continuation {
			parts = []any{map[string]any{"text": opencodeFinalMarker}}
		}
		opencodeEvent(w, "", map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}}, "usageMetadata": map[string]int{"promptTokenCount": 20, "candidatesTokenCount": 10, "totalTokenCount": 30}})
	default:
		http.Error(w, "unexpected controlled upstream path", http.StatusBadRequest)
	}
}

func opencodeEvent(w http.ResponseWriter, name string, payload any) {
	raw, _ := json.Marshal(payload)
	if name != "" {
		fmt.Fprintf(w, "event: %s\n", name)
	}
	fmt.Fprintf(w, "data: %s\n\n", raw)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func writeOpenCodeChat(w http.ResponseWriter, model, args string, final bool) {
	chunk := func(delta map[string]any, reason any) {
		opencodeEvent(w, "", map[string]any{"id": "chat_fixture", "object": "chat.completion.chunk", "created": 1, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": reason}}})
	}
	text := "before-tool"
	if final {
		text = opencodeFinalMarker
	}
	chunk(map[string]any{"role": "assistant", "content": text}, nil)
	reason := "stop"
	if !final {
		chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_fixture", "type": "function", "function": map[string]any{"name": "read", "arguments": args}}}}, nil)
		reason = "tool_calls"
	}
	chunk(map[string]any{}, reason)
	opencodeEvent(w, "", map[string]any{"id": "chat_fixture", "object": "chat.completion.chunk", "created": 1, "model": model, "choices": []any{}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeOpenCodeAnthropic(w http.ResponseWriter, model, args string, final bool) {
	opencodeEvent(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 20, "output_tokens": 0}}})
	text := "before-tool"
	if final {
		text = opencodeFinalMarker
	}
	opencodeEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	opencodeEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	opencodeEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	reason := "end_turn"
	if !final {
		opencodeEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "tool_use", "id": "toolu_fixture", "name": "read", "input": map[string]any{}}})
		opencodeEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": args}})
		opencodeEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})
		reason = "tool_use"
	}
	opencodeEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 10}})
	opencodeEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

func writeOpenCodeResponses(w http.ResponseWriter, args string, final bool) {
	event := func(typ string, fields map[string]any) { fields["type"] = typ; opencodeEvent(w, typ, fields) }
	response := map[string]any{"id": "resp_fixture", "object": "response", "created_at": 1, "model": "upstream-responses", "status": "in_progress"}
	event("response.created", map[string]any{"response": response})
	text := "before-tool"
	if final {
		text = opencodeFinalMarker
	}
	message := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
	event("response.output_item.added", map[string]any{"output_index": 0, "item": message})
	event("response.content_part.added", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
	event("response.output_text.delta", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "delta": text})
	event("response.output_text.done", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "text": text})
	message["status"] = "completed"
	message["content"] = []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}
	event("response.output_item.done", map[string]any{"output_index": 0, "item": message})
	output := []any{message}
	if !final {
		call := map[string]any{"id": "fc_fixture", "type": "function_call", "call_id": "call_fixture", "name": "read", "arguments": "", "status": "in_progress"}
		event("response.output_item.added", map[string]any{"output_index": 1, "item": call})
		event("response.function_call_arguments.delta", map[string]any{"item_id": "fc_fixture", "output_index": 1, "delta": args})
		event("response.function_call_arguments.done", map[string]any{"item_id": "fc_fixture", "output_index": 1, "arguments": args})
		call["arguments"] = args
		call["status"] = "completed"
		event("response.output_item.done", map[string]any{"output_index": 1, "item": call})
		output = append(output, call)
	}
	response["status"] = "completed"
	response["output"] = output
	response["usage"] = map[string]any{"input_tokens": 20, "output_tokens": 10, "total_tokens": 30, "input_tokens_details": map[string]int{"cached_tokens": 0}, "output_tokens_details": map[string]int{"reasoning_tokens": 0}}
	event("response.completed", map[string]any{"response": response})
}

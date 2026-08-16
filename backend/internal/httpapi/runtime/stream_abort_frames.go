package runtime

import (
	"encoding/json"
	"io"
	"net/http"
)

// Once the response head is on the wire the status code is spent, so a stream
// that dies mid-flight can only be reported inside the stream itself. Without a
// frame here the handler returns normally, net/http appends the terminating
// chunk, and a truncated answer reaches the client as a syntactically perfect
// success — indistinguishable from the model finishing its turn.
type runtimeStreamAbortReason struct {
	Code    string
	Message string
}

// Only outcomes where Prism knows the byte stream was cut short qualify.
// upstream_ended_without_terminal is deliberately excluded: there Prism relayed
// everything the upstream sent, and providers that legitimately omit the [DONE]
// sentinel would be handed a fabricated error.
func runtimeStreamAbortReasonFor(outcome string) (runtimeStreamAbortReason, bool) {
	switch outcome {
	case runtimeStreamOutcomeUpstreamReadError:
		return runtimeStreamAbortReason{
			Code:    "upstream_stream_interrupted",
			Message: "Upstream stream ended before completion; this response is truncated.",
		}, true
	case runtimeStreamOutcomeGatewayTimeout:
		return runtimeStreamAbortReason{
			Code:    "gateway_stream_timeout",
			Message: "Gateway ended the stream before completion; this response is truncated.",
		}, true
	default:
		return runtimeStreamAbortReason{}, false
	}
}

// writeRuntimeStreamAbortFrame emits one terminal frame in the shape the client's
// own SDK already treats as an error. It never writes a [DONE] sentinel and never
// synthesises a finish_reason, because either would re-disguise the truncation as
// a clean completion. The reason text is fixed; upstream error content is not
// echoed here (it is preserved in the request log instead).
func writeRuntimeStreamAbortFrame(w io.Writer, operation RuntimeOperation, reason runtimeStreamAbortReason) {
	hooks, ok := streamHooksForOperation(operation)
	if !ok {
		return
	}
	var payload any
	switch {
	case hooks.Provider == "anthropic":
		payload = map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "api_error", "message": reason.Message},
		}
	case hooks.Provider == "gemini":
		payload = map[string]any{
			"error": map[string]any{
				"code":    http.StatusBadGateway,
				"status":  "UNAVAILABLE",
				"message": reason.Message,
			},
		}
	default:
		payload = map[string]any{
			"error": map[string]any{
				"type":    "upstream_error",
				"code":    reason.Code,
				"message": reason.Message,
			},
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	// Anthropic and the OpenAI Responses API both carry a named SSE event type;
	// chat.completions, images and Gemini read bare data frames.
	frame := make([]byte, 0, len(encoded)+32)
	if hooks.Provider == "anthropic" || operationStreamUsesNamedEvents(operation) {
		frame = append(frame, "event: error\n"...)
	}
	frame = append(frame, "data: "...)
	frame = append(frame, encoded...)
	frame = append(frame, "\n\n"...)
	if _, err := w.Write(frame); err != nil {
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func operationStreamUsesNamedEvents(operation RuntimeOperation) bool {
	collectionID := operation.HookCollectionID
	if collectionID == "" {
		collectionID = operation.Name
	}
	return collectionID == "openai.responses"
}

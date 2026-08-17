package runtime

// Request stream helpers resolve caller stream intent for runtime telemetry.
// Path-native Gemini streaming is authoritative even when the body omits a
// stream flag. Body-bound operations use the provider-native boolean only.
//
// Malformed or empty bodies do not become streaming requests here; body
// validation belongs to the operation planner. These helpers only answer the
// classification question needed by request hooks and persisted stream state.
//
// Keeping path and body intent in one seam prevents a provider adapter from
// inventing a second streaming grammar.
//
// `:streamGenerateContent` is a path contract, not a hint derived from the
// request body. The body flag remains meaningful for OpenAI Chat Completions
// and other body-bound operations, where the registered operation hook owns
// the final stream classification.
//
// A false result here does not reject the request. It only selects the
// buffered response path; malformed-body and unsupported-operation errors are
// raised by their owning planners.
//
// This separation keeps request stream intent independent from response SSE
// terminal classification.
// No helper reads a clock, database, or provider catalog.
//
// The returned boolean is consumed by the ingress fast-path decision and by
// telemetry's stream outcome projection. It is not persisted as a separate
// client intent field.
//
// Keeping the body decoder here intentionally accepts only a JSON boolean;
// strings, numbers, and malformed objects remain non-streaming until the
// operation-specific request hook reports otherwise.
//
// This module has no side effects.
// It is safe to call during the ingress probe and final plan phases.
// The probe phase may receive a nil body and must remain non-rejecting.
// The final phase owns malformed-body rejection after buffering.
//
// These phase distinctions are part of Gemini path-streaming behavior.
//
//
// The helper is intentionally independent from provider adapters.
// It answers one ingress question for every registered operation.
//
// The operation hook may later refine generation parameters, but it does not
// change this path/body stream intent rule.
//
// No response bytes are inspected here.
// No audit row is created here.
// No runtime state is mutated here.
// Its output is a pure classification value.
// Callers decide buffering and response handling after this answer.
// The helper never invents a provider operation.
// It remains safe for direct unit-level classification.
//
//
import (
	"encoding/json"
	"strings"
)

func requestWantsStream(rawBody []byte, requestPath string) bool {
	if strings.Contains(strings.TrimSpace(requestPath), ":streamGenerateContent") {
		return true
	}
	return requestBodyWantsStream(rawBody)
}

func requestBodyWantsStream(rawBody []byte) bool {
	if len(rawBody) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return false
	}
	stream, ok := payload["stream"].(bool)
	return ok && stream
}

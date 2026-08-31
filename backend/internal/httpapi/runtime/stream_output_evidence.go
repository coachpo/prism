package runtime

import (
	"strings"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	anthropicprovider "github.com/coachpo/prism/backend/internal/gateway/provider/anthropic"
)

// Output-rate measurability policy (product policy, deliberately not a schema
// constraint): a streaming request is rate-measurable only when its response
// completed and delivered at least two visible text/tool-output events whose
// first-to-last span reaches this floor. Buffered-burst upstreams (one burst
// under the floor after a long wait), single-event streams, and reasoning-only
// delivery stay unmeasured so they cannot pollute the tok/s average.
const outputRateMinDeliverySpanMS = 50

// outputDeliveryEvidence accumulates the observed visible-output facts of one
// response. Only operation-allowlisted visible text/tool increments are
// observed; usage, terminal, control, reasoning, and image payloads never
// count as output evidence.
type outputDeliveryEvidence struct {
	firstOutputAt *time.Time
	lastOutputAt  *time.Time
	eventCount    int
	// reasoningObserved is kept apart from visible-output event timing. It
	// matters only when the provider usage has no independent reasoning-token
	// component; when reasoning is split, OutputTokens is already the aligned
	// base-output numerator.
	reasoningObserved bool
}

func (e *outputDeliveryEvidence) observe(observedAt time.Time) {
	if e.firstOutputAt == nil {
		first := observedAt
		e.firstOutputAt = &first
	}
	last := observedAt
	e.lastOutputAt = &last
	e.eventCount++
}

// spanMilliseconds projects the first-to-last visible-output span. It reports
// false when fewer than two events were observed: a single event has no
// progressive span to measure.
func (e outputDeliveryEvidence) spanMilliseconds() (int, bool) {
	if e.eventCount < 2 || e.firstOutputAt == nil || e.lastOutputAt == nil {
		return 0, false
	}
	duration := e.lastOutputAt.Sub(*e.firstOutputAt)
	if duration <= 0 {
		return 0, true
	}
	// Preserve the observed millisecond fact. Unlike request-duration fields,
	// a sub-millisecond output span remains 0ms rather than being promoted to
	// 1ms; it is still safely rejected by the 50ms writer policy.
	return int(duration / time.Millisecond), true
}

// outputDeliveryMeasurement is the classified output-rate evidence of one
// response. State and reason are persisted verbatim on request_logs (final
// attempt row) and usage_request_events; the count and span keep the request
// facts even when the rate itself is not measurable.
type outputDeliveryMeasurement struct {
	State      string
	Reason     *string
	EventCount *int
	SpanMS     *int
}

func measuredOutputDelivery(evidence outputDeliveryEvidence, spanMS int) outputDeliveryMeasurement {
	return outputDeliveryMeasurement{State: statsdomain.OutputRateStateMeasured, EventCount: intPtr(evidence.eventCount), SpanMS: intPtr(spanMS)}
}

func unmeasurableOutputDelivery(reason string, eventCount *int, spanMS *int) outputDeliveryMeasurement {
	return outputDeliveryMeasurement{State: statsdomain.OutputRateStateUnmeasurable, Reason: stringPtr(reason), EventCount: eventCount, SpanMS: spanMS}
}

func notApplicableOutputDelivery(reason string) outputDeliveryMeasurement {
	return outputDeliveryMeasurement{State: statsdomain.OutputRateStateNotApplicable, Reason: stringPtr(reason)}
}

// outputDeliveryForFailure classifies current synthetic failures that never
// entered an SSE capture. They are known non-delivery cases, not legacy
// unknowns: text operations are non-streaming for this attempt, while Images
// and token-count operations retain their operation-specific not-applicable
// reason.
func outputDeliveryForFailure(operation RuntimeOperation) outputDeliveryMeasurement {
	hooks, ok := responseHooksForOperation(operation)
	if !ok {
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableNonTextOperation)
	}
	switch hooks.Kind {
	case operationResponseKindImageGeneration:
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableImageOperation)
	case operationResponseKindTextGeneration:
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableNonStream)
	default:
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableNonTextOperation)
	}
}

// classifyOutputDelivery projects one response onto the four-state output-rate
// domain. The classification is writer policy: it runs once per request at
// capture time and its verdict is persisted, never re-derived on read.
func classifyOutputDelivery(kind operationResponseKind, streamOutcome string, usage responseUsage, evidence outputDeliveryEvidence) outputDeliveryMeasurement {
	switch kind {
	case operationResponseKindImageGeneration:
		// Images never participate in the text tok/s metric.
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableImageOperation)
	case operationResponseKindTokenCount:
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableNonTextOperation)
	case operationResponseKindTextGeneration:
		// Progressive-delivery measurement below.
	default:
		// Operations without response hooks (e.g. model listing) have no
		// output-token caliber at all.
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableNonTextOperation)
	}
	if !runtimeStreamOutcomeIsStreaming(streamOutcome) {
		// Buffered non-event delivery has no progressive timeline to measure.
		return notApplicableOutputDelivery(statsdomain.OutputRateReasonNotApplicableNonStream)
	}
	if streamOutcome != runtimeStreamOutcomeCompleted {
		// Incomplete delivery (client disconnect, provider incomplete, upstream
		// error, missing terminal) keeps its delivery facts but no rate.
		eventCount, spanMS := evidenceFacts(evidence)
		return unmeasurableOutputDelivery(statsdomain.OutputRateReasonUnmeasurableIncompleteStream, eventCount, spanMS)
	}
	if usage.OutputTokens == nil {
		// Without the finalized output-token numerator the rate is undefined
		// even when delivery evidence exists.
		eventCount, spanMS := evidenceFacts(evidence)
		return unmeasurableOutputDelivery(statsdomain.OutputRateReasonUnmeasurableMissingOutputUsage, eventCount, spanMS)
	}
	if evidence.eventCount == 0 {
		return unmeasurableOutputDelivery(statsdomain.OutputRateReasonUnmeasurableNoOutputEvents, nil, nil)
	}
	if evidence.eventCount == 1 {
		return unmeasurableOutputDelivery(statsdomain.OutputRateReasonUnmeasurableSingleOutputEvent, intPtr(evidence.eventCount), nil)
	}
	span, ok := evidence.spanMilliseconds()
	if !ok {
		return unmeasurableOutputDelivery(statsdomain.OutputRateReasonUnmeasurableNoOutputEvents, intPtr(evidence.eventCount), nil)
	}
	if span < outputRateMinDeliverySpanMS {
		return unmeasurableOutputDelivery(statsdomain.OutputRateReasonUnmeasurableSpanBelowThreshold, intPtr(evidence.eventCount), intPtr(span))
	}
	if evidence.reasoningObserved && (usage.ReasoningTokens == nil || *usage.ReasoningTokens <= 0) {
		// A reasoning event without a separately reported reasoning-token
		// component means the provider's output-token numerator may include
		// hidden/thinking output that the visible span excludes. Providers that
		// do report reasoning separately are safe because canonical
		// OutputTokens has already subtracted that component.
		return unmeasurableOutputDelivery(statsdomain.OutputRateReasonUnmeasurableReasoningUnaligned, intPtr(evidence.eventCount), intPtr(span))
	}
	return measuredOutputDelivery(evidence, span)
}

func evidenceFacts(evidence outputDeliveryEvidence) (*int, *int) {
	eventCount := intPtr(evidence.eventCount)
	var spanMS *int
	if span, ok := evidence.spanMilliseconds(); ok {
		spanMS = intPtr(span)
	}
	return eventCount, spanMS
}

// hasNonEmptyStreamString reports whether the JSON value is a non-blank
// string. Empty or whitespace-only deltas carry no visible output.
func hasNonEmptyStreamString(value any) bool {
	return strings.TrimSpace(stringValue(value)) != ""
}

// ---- Per-operation visible-output allowlists ----
//
// Each collector answers exactly one question: does this stream payload carry
// a visible text or tool-output increment? Usage chunks, terminal events,
// control/bookkeeping payloads, reasoning deltas, and image payloads are
// excluded so they can never inflate the event count or stretch the span.

func collectOpenAIChatCompletionsOutputEvent(_ string, payload map[string]any) operationStreamOutputObservation {
	observation := operationStreamOutputObservation{}
	choices, ok := payload["choices"].([]any)
	if !ok {
		return observation
	}
	for _, choice := range choices {
		choicePayload, ok := choice.(map[string]any)
		if !ok {
			continue
		}
		delta, ok := choicePayload["delta"].(map[string]any)
		if !ok {
			continue
		}
		if hasNonEmptyStreamString(delta["reasoning_content"]) || hasNonEmptyStreamString(delta["reasoning"]) {
			observation.ReasoningObserved = true
		}
		if hasNonEmptyStreamString(delta["content"]) {
			observation.VisibleOutput = true
		}
		// Legacy Chat function calls stream their argument bytes under the
		// singular function_call field.
		if functionCall, ok := delta["function_call"].(map[string]any); ok && hasNonEmptyStreamString(functionCall["arguments"]) {
			observation.VisibleOutput = true
		}
		if toolCalls, ok := delta["tool_calls"].([]any); ok {
			for _, toolCall := range toolCalls {
				toolPayload, ok := toolCall.(map[string]any)
				if !ok {
					continue
				}
				// Tool-output increments stream through the function
				// arguments; finish_reason, ids, and indexes are control.
				if hasNonEmptyStreamString(toolPayload["arguments"]) {
					observation.VisibleOutput = true
				}
				if function, ok := toolPayload["function"].(map[string]any); ok {
					if hasNonEmptyStreamString(function["arguments"]) {
						observation.VisibleOutput = true
					}
				}
			}
		}
	}
	return observation
}

func collectOpenAIResponsesOutputEvent(event string, payload map[string]any) operationStreamOutputObservation {
	switch responsesStreamEventType(event, payload) {
	case "response.output_text.delta",
		"response.function_call_arguments.delta",
		"response.custom_tool_call_input.delta":
		return operationStreamOutputObservation{VisibleOutput: hasNonEmptyStreamString(payload["delta"])}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return operationStreamOutputObservation{ReasoningObserved: hasNonEmptyStreamString(payload["delta"])}
	default:
		// Reasoning summary/text deltas, output-item lifecycle events, and the
		// terminal response.completed event are never output evidence.
		return operationStreamOutputObservation{}
	}
}

func responsesStreamEventType(event string, payload map[string]any) string {
	if trimmed := strings.TrimSpace(event); trimmed != "" {
		return trimmed
	}
	eventType, _ := payload["type"].(string)
	return eventType
}

func collectAnthropicMessagesOutputEvent(event string, payload map[string]any) operationStreamOutputObservation {
	if anthropicprovider.IsMessagesStreamEvent(event, payload, "content_block_start") {
		block, _ := payload["content_block"].(map[string]any)
		blockType, _ := block["type"].(string)
		if blockType == "thinking" || blockType == "redacted_thinking" {
			return operationStreamOutputObservation{ReasoningObserved: true}
		}
	}
	if !anthropicprovider.IsMessagesStreamEvent(event, payload, "content_block_delta") {
		return operationStreamOutputObservation{}
	}
	delta, ok := payload["delta"].(map[string]any)
	if !ok {
		return operationStreamOutputObservation{}
	}
	switch deltaType, _ := delta["type"].(string); deltaType {
	case "text_delta":
		return operationStreamOutputObservation{VisibleOutput: hasNonEmptyStreamString(delta["text"])}
	case "input_json_delta":
		return operationStreamOutputObservation{VisibleOutput: hasNonEmptyStreamString(delta["partial_json"])}
	case "thinking_delta":
		return operationStreamOutputObservation{ReasoningObserved: hasNonEmptyStreamString(delta["thinking"])}
	default:
		// thinking_delta and signature_delta are reasoning/control evidence.
		return operationStreamOutputObservation{}
	}
}

func collectGeminiStreamGenerateContentOutputEvent(_ string, payload map[string]any) operationStreamOutputObservation {
	observation := operationStreamOutputObservation{}
	candidates, ok := payload["candidates"].([]any)
	if !ok {
		return observation
	}
	for _, candidate := range candidates {
		candidatePayload, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		content, ok := candidatePayload["content"].(map[string]any)
		if !ok {
			continue
		}
		parts, ok := content["parts"].([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			partPayload, ok := part.(map[string]any)
			if !ok {
				continue
			}
			// Thought summaries are reasoning evidence, not visible output.
			if thought, _ := partPayload["thought"].(bool); thought {
				if hasNonEmptyStreamString(partPayload["text"]) {
					observation.ReasoningObserved = true
				}
				continue
			}
			if hasNonEmptyStreamString(partPayload["text"]) {
				observation.VisibleOutput = true
			}
			// A functionCall part is the model's tool output for this frame.
			if _, ok := partPayload["functionCall"].(map[string]any); ok {
				observation.VisibleOutput = true
			}
		}
	}
	return observation
}

package stats

import (
	"encoding/json"
	"strings"
)

// Output-rate evidence is the persisted measurability projection for one
// request's progressive visible-output delivery. The runtime writer classifies
// each request once and writes the same state, reason, event count, and
// delivery span to the final attempt row of request_logs and to
// usage_request_events; every read surface derives the authoritative tok/s
// only from measured evidence (state=measured plus output_tokens and
// output_delivery_span_ms). Historical rows keep NULL evidence and every
// reader projects them as unknown, so old rate artifacts can never re-enter
// an average.
const (
	OutputRateStateMeasured      = "measured"
	OutputRateStateUnmeasurable  = "unmeasurable"
	OutputRateStateNotApplicable = "not_applicable"
	OutputRateStateUnknown       = "unknown"
)

// Output-rate reason codes. Measured rows carry no reason: the state itself is
// the explanation. Reasons describe why a request has no authoritative rate
// and are product policy, safe to extend without schema changes.
const (
	OutputRateReasonNotApplicableImageOperation    = "not_applicable_image_operation"
	OutputRateReasonNotApplicableNonStream         = "not_applicable_non_stream"
	OutputRateReasonNotApplicableNonTextOperation  = "not_applicable_non_text_operation"
	OutputRateReasonUnmeasurableIncompleteStream   = "unmeasurable_incomplete_stream"
	OutputRateReasonUnmeasurableMissingOutputUsage = "unmeasurable_missing_output_usage"
	OutputRateReasonUnmeasurableNoOutputEvents     = "unmeasurable_no_output_events"
	OutputRateReasonUnmeasurableSingleOutputEvent  = "unmeasurable_single_output_event"
	OutputRateReasonUnmeasurableSpanBelowThreshold = "unmeasurable_output_span_below_threshold"
	OutputRateReasonUnmeasurableReasoningUnaligned = "unmeasurable_reasoning_tokens_unaligned"
	OutputRateReasonUnmeasurableNonSuccessStatus   = "unmeasurable_non_success_status"
	OutputRateReasonUnknownMissingEvidence         = "unknown_missing_evidence"
	OutputRateReasonUnknownInconsistentEvidence    = "unknown_inconsistent_evidence"
)

// NormalizeOutputRateState projects a persisted state onto the four-state
// domain. NULL evidence columns (historical rows) read as unknown so no read
// surface can mistake missing evidence for a measurement.
func NormalizeOutputRateState(state string) string {
	switch state {
	case OutputRateStateMeasured, OutputRateStateUnmeasurable, OutputRateStateNotApplicable, OutputRateStateUnknown:
		return state
	default:
		return OutputRateStateUnknown
	}
}

// OutputRateTPSFromEvidence derives the authoritative per-request tok/s from
// persisted measured evidence only. The numerator is the finalized
// output_tokens fact and the denominator is the writer-recorded first-to-last
// visible-output delivery span; unmeasured rows return nil and never enter an
// average. A measured zero stays a real zero.
func OutputRateTPSFromEvidence(outputTokens int, hasOutputTokens bool, state string, deliverySpanMS *int) *float64 {
	if !hasOutputTokens || outputTokens < 0 || NormalizeOutputRateState(state) != OutputRateStateMeasured {
		return nil
	}
	if deliverySpanMS == nil || *deliverySpanMS <= 0 {
		return nil
	}
	resolved := roundFloat((float64(outputTokens)*1000)/float64(*deliverySpanMS), 2)
	return &resolved
}

// decodeOutputRateReasonCounts parses the window's jsonb_object_agg of
// output-rate reasons into the API map. An empty aggregate decodes to an
// empty map so the JSON payload always carries the object shape.
func decodeOutputRateReasonCounts(raw []byte) map[string]int {
	counts := map[string]int{}
	if len(raw) == 0 {
		return counts
	}
	var decoded map[string]int
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return counts
	}
	for reason, count := range decoded {
		if strings.TrimSpace(reason) == "" {
			continue
		}
		counts[reason] = count
	}
	return counts
}

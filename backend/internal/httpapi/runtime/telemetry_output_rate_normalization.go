package runtime

import (
	"strings"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

// normalizeRuntimeOutputRateEvidence projects envelopes that predate the
// output-rate evidence columns onto conservative unknown evidence, mirroring
// the currency-attribution precedent. It also fails closed on partial or
// contradictory payloads: only identical, internally valid evidence on the
// usage event and final request-log row survives. Intermediate attempt rows
// never carry the per-request verdict.
func normalizeRuntimeOutputRateEvidence(envelope *runtimeTelemetryEnvelope) {
	if envelope == nil {
		return
	}
	for index := 0; index+1 < len(envelope.RequestLogs); index++ {
		clearRequestLogOutputRateEvidence(&envelope.RequestLogs[index])
	}
	usage := usageEventOutputDelivery(envelope.UsageEvent)
	if len(envelope.RequestLogs) == 0 {
		if outputDeliveryMeasurementEmpty(usage) {
			setUsageEventOutputRateUnknown(&envelope.UsageEvent, statsdomain.OutputRateReasonUnknownMissingEvidence)
		} else {
			// A per-request usage verdict without its final request-log peer is
			// structurally asymmetric even when the usage-side fields look valid.
			setUsageEventOutputRateUnknown(&envelope.UsageEvent, statsdomain.OutputRateReasonUnknownInconsistentEvidence)
		}
		return
	}

	final := &envelope.RequestLogs[len(envelope.RequestLogs)-1]
	request := requestLogOutputDelivery(*final)
	if outputDeliveryMeasurementEmpty(usage) && outputDeliveryMeasurementEmpty(request) {
		setOutputRateUnknown(final, &envelope.UsageEvent, statsdomain.OutputRateReasonUnknownMissingEvidence)
		return
	}
	if !outputDeliveryMeasurementValid(usage, envelope.UsageEvent.OutputTokens) ||
		!outputDeliveryMeasurementValid(request, final.OutputTokens) ||
		!outputDeliveryMeasurementEqual(usage, request) ||
		(usage.State == statsdomain.OutputRateStateMeasured && !optionalIntEqual(envelope.UsageEvent.OutputTokens, final.OutputTokens)) {
		setOutputRateUnknown(final, &envelope.UsageEvent, statsdomain.OutputRateReasonUnknownInconsistentEvidence)
	}
}

func usageEventOutputDelivery(event usageEventInsert) outputDeliveryMeasurement {
	return outputDeliveryMeasurement{
		State:      strings.TrimSpace(event.OutputRateState),
		Reason:     event.OutputRateReason,
		EventCount: event.OutputDeliveryEventCount,
		SpanMS:     event.OutputDeliverySpanMS,
	}
}

func requestLogOutputDelivery(row requestLogInsert) outputDeliveryMeasurement {
	return outputDeliveryMeasurement{
		State:      strings.TrimSpace(row.OutputRateState),
		Reason:     row.OutputRateReason,
		EventCount: row.OutputDeliveryEventCount,
		SpanMS:     row.OutputDeliverySpanMS,
	}
}

func outputDeliveryMeasurementEmpty(measurement outputDeliveryMeasurement) bool {
	return strings.TrimSpace(measurement.State) == "" && measurement.Reason == nil && measurement.EventCount == nil && measurement.SpanMS == nil
}

func outputDeliveryMeasurementValid(measurement outputDeliveryMeasurement, outputTokens *int) bool {
	reasonPresent := measurement.Reason != nil && strings.TrimSpace(*measurement.Reason) != ""
	switch strings.TrimSpace(measurement.State) {
	case statsdomain.OutputRateStateMeasured:
		return !reasonPresent && measurement.Reason == nil && outputTokens != nil && *outputTokens >= 0 &&
			measurement.EventCount != nil && *measurement.EventCount >= 2 &&
			measurement.SpanMS != nil && *measurement.SpanMS > 0
	case statsdomain.OutputRateStateUnmeasurable:
		return reasonPresent && optionalNonNegativeInt(measurement.EventCount) && optionalNonNegativeInt(measurement.SpanMS)
	case statsdomain.OutputRateStateNotApplicable, statsdomain.OutputRateStateUnknown:
		return reasonPresent && measurement.EventCount == nil && measurement.SpanMS == nil
	default:
		return false
	}
}

func optionalNonNegativeInt(value *int) bool {
	return value == nil || *value >= 0
}

func outputDeliveryMeasurementEqual(left outputDeliveryMeasurement, right outputDeliveryMeasurement) bool {
	return strings.TrimSpace(left.State) == strings.TrimSpace(right.State) &&
		optionalStringEqual(left.Reason, right.Reason) &&
		optionalIntEqual(left.EventCount, right.EventCount) &&
		optionalIntEqual(left.SpanMS, right.SpanMS)
}

func optionalStringEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalIntEqual(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func clearRequestLogOutputRateEvidence(row *requestLogInsert) {
	if row == nil {
		return
	}
	row.OutputRateState = ""
	row.OutputRateReason = nil
	row.OutputDeliveryEventCount = nil
	row.OutputDeliverySpanMS = nil
}

func setUsageEventOutputRateUnknown(event *usageEventInsert, reason string) {
	if event == nil {
		return
	}
	event.OutputRateState = statsdomain.OutputRateStateUnknown
	event.OutputRateReason = stringPtr(reason)
	event.OutputDeliveryEventCount = nil
	event.OutputDeliverySpanMS = nil
}

func setOutputRateUnknown(request *requestLogInsert, usage *usageEventInsert, reason string) {
	clearRequestLogOutputRateEvidence(request)
	request.OutputRateState = statsdomain.OutputRateStateUnknown
	request.OutputRateReason = stringPtr(reason)
	setUsageEventOutputRateUnknown(usage, reason)
}

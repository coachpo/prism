package runtime

import "testing"

func TestNormalizeRuntimeOutputRateEvidenceFailsClosed(t *testing.T) {
	measured := outputDeliveryMeasurement{
		State:      "measured",
		EventCount: intPtr(2),
		SpanMS:     intPtr(80),
	}

	tests := []struct {
		name       string
		request    outputDeliveryMeasurement
		usage      outputDeliveryMeasurement
		requestOut *int
		usageOut   *int
		wantReason string
	}{
		{name: "legacy payload missing both sides", wantReason: "unknown_missing_evidence"},
		{name: "usage-only evidence", usage: measured, usageOut: intPtr(4), requestOut: intPtr(4), wantReason: "unknown_inconsistent_evidence"},
		{name: "request-only evidence", request: measured, usageOut: intPtr(4), requestOut: intPtr(4), wantReason: "unknown_inconsistent_evidence"},
		{name: "mismatched span", request: measured, usage: outputDeliveryMeasurement{State: "measured", EventCount: intPtr(2), SpanMS: intPtr(90)}, usageOut: intPtr(4), requestOut: intPtr(4), wantReason: "unknown_inconsistent_evidence"},
		{name: "mismatched measured numerator", request: measured, usage: measured, requestOut: intPtr(9), usageOut: intPtr(4), wantReason: "unknown_inconsistent_evidence"},
		{name: "invalid measured numerator", request: measured, usage: measured, wantReason: "unknown_inconsistent_evidence"},
		{name: "negative measured numerator", request: measured, usage: measured, requestOut: intPtr(-1), usageOut: intPtr(-1), wantReason: "unknown_inconsistent_evidence"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := runtimeTelemetryEnvelope{
				RequestLogs: []requestLogInsert{
					{OutputRateState: "measured", OutputDeliveryEventCount: intPtr(9), OutputDeliverySpanMS: intPtr(900), OutputTokens: intPtr(9)},
					requestLogInsertFromOutputDelivery(test.request, test.requestOut),
				},
				UsageEvent: usageEventInsertFromOutputDelivery(test.usage, test.usageOut),
			}
			normalizeRuntimeOutputRateEvidence(&envelope)
			if envelope.RequestLogs[0].OutputRateState != "" || envelope.RequestLogs[0].OutputDeliveryEventCount != nil || envelope.RequestLogs[0].OutputDeliverySpanMS != nil {
				t.Fatalf("intermediate evidence was not cleared: %+v", envelope.RequestLogs[0])
			}
			assertNormalizedUnknownOutputRate(t, envelope, test.wantReason)
		})
	}

	t.Run("identical valid evidence survives", func(t *testing.T) {
		envelope := runtimeTelemetryEnvelope{
			RequestLogs: []requestLogInsert{requestLogInsertFromOutputDelivery(measured, intPtr(4))},
			UsageEvent:  usageEventInsertFromOutputDelivery(measured, intPtr(4)),
		}
		normalizeRuntimeOutputRateEvidence(&envelope)
		request := requestLogOutputDelivery(envelope.RequestLogs[0])
		usage := usageEventOutputDelivery(envelope.UsageEvent)
		if !outputDeliveryMeasurementEqual(request, measured) || !outputDeliveryMeasurementEqual(usage, measured) {
			t.Fatalf("valid identical evidence changed: request=%+v usage=%+v", request, usage)
		}
	})

	t.Run("usage evidence without a request-log peer degrades", func(t *testing.T) {
		envelope := runtimeTelemetryEnvelope{UsageEvent: usageEventInsertFromOutputDelivery(measured, intPtr(4))}
		normalizeRuntimeOutputRateEvidence(&envelope)
		if envelope.UsageEvent.OutputRateState != "unknown" || envelope.UsageEvent.OutputRateReason == nil || *envelope.UsageEvent.OutputRateReason != "unknown_inconsistent_evidence" {
			t.Fatalf("expected orphan usage evidence to degrade, got %q/%v", envelope.UsageEvent.OutputRateState, envelope.UsageEvent.OutputRateReason)
		}
	})
}

func requestLogInsertFromOutputDelivery(measurement outputDeliveryMeasurement, outputTokens *int) requestLogInsert {
	return requestLogInsert{
		OutputTokens:             outputTokens,
		OutputRateState:          measurement.State,
		OutputRateReason:         measurement.Reason,
		OutputDeliveryEventCount: measurement.EventCount,
		OutputDeliverySpanMS:     measurement.SpanMS,
	}
}

func usageEventInsertFromOutputDelivery(measurement outputDeliveryMeasurement, outputTokens *int) usageEventInsert {
	return usageEventInsert{
		OutputTokens:             outputTokens,
		OutputRateState:          measurement.State,
		OutputRateReason:         measurement.Reason,
		OutputDeliveryEventCount: measurement.EventCount,
		OutputDeliverySpanMS:     measurement.SpanMS,
	}
}

func assertNormalizedUnknownOutputRate(t *testing.T, envelope runtimeTelemetryEnvelope, reason string) {
	t.Helper()
	final := envelope.RequestLogs[len(envelope.RequestLogs)-1]
	if final.OutputRateState != "unknown" || final.OutputRateReason == nil || *final.OutputRateReason != reason {
		t.Fatalf("unexpected normalized request evidence: %q/%v", final.OutputRateState, final.OutputRateReason)
	}
	usage := envelope.UsageEvent
	if usage.OutputRateState != final.OutputRateState || !optionalStringEqual(usage.OutputRateReason, final.OutputRateReason) {
		t.Fatalf("normalized evidence is asymmetric: request=%q/%v usage=%q/%v", final.OutputRateState, final.OutputRateReason, usage.OutputRateState, usage.OutputRateReason)
	}
	if final.OutputDeliveryEventCount != nil || final.OutputDeliverySpanMS != nil || usage.OutputDeliveryEventCount != nil || usage.OutputDeliverySpanMS != nil {
		t.Fatalf("unknown evidence retained delivery facts: request=%v/%v usage=%v/%v", final.OutputDeliveryEventCount, final.OutputDeliverySpanMS, usage.OutputDeliveryEventCount, usage.OutputDeliverySpanMS)
	}
}

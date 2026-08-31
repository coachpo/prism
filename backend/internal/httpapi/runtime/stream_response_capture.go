package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

func proxyEventStreamAndCaptureCompletedResponse(operation RuntimeOperation, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	streamHooks, ok := streamHooksForOperation(operation)
	if !ok {
		return runtimeResponseCapture{}, fmt.Errorf("stream hooks not configured for operation %s", operation.Name)
	}
	reader := bufio.NewReader(src)
	capture := sseCompletedResponseCapture{streamHooks: streamHooks}
	auditBuffer, _ := newBoundedAuditWriter(captureAuditBody)

	captureResult := func(classification sseStreamClassification) runtimeResponseCapture {
		responseCapture := capture.runtimeResponseCapture(classification)
		if captureAuditBody {
			responseCapture.AuditBody, responseCapture.AuditBodyObserved, responseCapture.AuditBodyStored, responseCapture.AuditBodyTruncated = auditBuffer.snapshot()
		}
		return responseCapture
	}

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			capture.consumeLine(line, now())
			written, writeErr := dst.Write(line)
			if captureAuditBody && written > 0 {
				if written > len(line) {
					written = len(line)
				}
				_, _ = auditBuffer.Write(line[:written])
			}
			if writeErr != nil {
				capture.finishEvent(now())
				return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, writeErr)), writeErr
			}
		}
		if err == nil {
			continue
		}
		capture.finishEvent(now())
		if errors.Is(err, io.EOF) {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, nil)), nil
		}
		return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, err, nil)), err
	}
}

type sseTerminalSignal uint8

const (
	sseTerminalSignalNone sseTerminalSignal = iota
	sseTerminalSignalCompleted
	sseTerminalSignalProviderIncomplete
)

type sseStreamClassification struct {
	outcome string
	kind    *string
	detail  *string
}

type sseCompletedResponseCapture struct {
	streamHooks       operationStreamHooks
	currentEvent      string
	currentDataLines  []string
	completedResponse []byte
	usage             responseUsage
	firstPayloadAt    *time.Time
	completedAt       *time.Time
	terminalSignal    sseTerminalSignal
	outputDelivery    outputDeliveryEvidence
}

func (capture *sseCompletedResponseCapture) runtimeResponseCapture(classification sseStreamClassification) runtimeResponseCapture {
	// The output-rate verdict is classified here once per response: the
	// canonicalized usage supplies the numerator evidence and the observed
	// outputDelivery evidence supplies the delivery facts.
	outcome := strings.TrimSpace(classification.outcome)
	if outcome == "" {
		outcome = runtimeStreamOutcomeUnknown
	}
	usage := capture.usage.canonicalizedForRuntimeUsage(capture.streamHooks.UsageRule)
	body := capture.completedResponse
	if outcome != runtimeStreamOutcomeCompleted {
		usage = responseUsage{}
		body = nil
	}
	return runtimeResponseCapture{
		Body:                     body,
		Usage:                    usage,
		UsageRule:                capture.streamHooks.UsageRule,
		UsageSource:              runtimeUsageSourceFromUsage(usage, outcome),
		FirstMeaningfulPayloadAt: capture.firstPayloadAt,
		CompletedAt:              capture.completedAt,
		StreamOutcome:            outcome,
		StreamErrorKind:          classification.kind,
		StreamErrorDetail:        classification.detail,
		OutputDelivery:           classifyOutputDelivery(capture.streamHooks.Kind, outcome, usage, capture.outputDelivery),
	}
}

func (capture *sseCompletedResponseCapture) consumeLine(line []byte, observedAt time.Time) {
	trimmed := strings.TrimRight(string(line), "\r\n")
	if trimmed == "" {
		capture.finishEvent(observedAt)
		return
	}
	if strings.HasPrefix(trimmed, "event:") {
		capture.currentEvent = trimSSEFieldValue(strings.TrimPrefix(trimmed, "event:"))
		return
	}
	if strings.HasPrefix(trimmed, "data:") {
		capture.currentDataLines = append(capture.currentDataLines, trimSSEFieldValue(strings.TrimPrefix(trimmed, "data:")))
	}
}

func (capture *sseCompletedResponseCapture) finishEvent(observedAt time.Time) {
	if len(capture.currentDataLines) > 0 {
		capture.consumePayload([]byte(strings.Join(capture.currentDataLines, "\n")), observedAt)
	}
	capture.currentEvent = ""
	capture.currentDataLines = nil
}

func (capture *sseCompletedResponseCapture) consumePayload(payloadBytes []byte, observedAt time.Time) {
	if strings.TrimSpace(string(payloadBytes)) == "[DONE]" {
		if capture.streamHooks.CompleteOnDoneSentinel {
			completedAt := observedAt
			capture.completedAt = &completedAt
			capture.terminalSignal = sseTerminalSignalCompleted
		}
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return
	}
	if capture.firstPayloadAt == nil && payloadHasMeaningfulStreamContent(payload) {
		firstPayloadAt := observedAt
		capture.firstPayloadAt = &firstPayloadAt
	}
	if terminalSignal := capture.streamHooks.terminalSignal(capture.currentEvent, payload); terminalSignal != sseTerminalSignalNone {
		completedAt := observedAt
		capture.completedAt = &completedAt
		capture.terminalSignal = terminalSignal
	}
	capture.streamHooks.mergeUsage(&capture.usage, capture.currentEvent, payload)
	observation := capture.streamHooks.collectOutputEvent(capture.currentEvent, payload)
	if observation.ReasoningObserved {
		capture.outputDelivery.reasoningObserved = true
	}
	if observation.VisibleOutput {
		capture.outputDelivery.observe(observedAt)
	}
	if usage := capture.usage.canonicalizedForRuntimeUsage(capture.streamHooks.UsageRule); usage.hasValues() {
		if usageBody := buildUsageBodyFromResponseUsage(usage); len(usageBody) > 0 {
			capture.completedResponse = usageBody
		}
	}
}

func trimSSEFieldValue(value string) string {
	return strings.TrimLeft(value, " ")
}

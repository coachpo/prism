package runtime

import (
	"io"
	"strings"
	"time"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

const (
	runtimeStreamOutcomeNotStreaming                 = "not_streaming"
	runtimeStreamOutcomeCompleted                    = "completed"
	runtimeStreamOutcomeProviderIncomplete           = "provider_incomplete"
	runtimeStreamOutcomeClientDisconnected           = "client_disconnected"
	runtimeStreamOutcomeGatewayTimeout               = "gateway_timeout"
	runtimeStreamOutcomeUpstreamReadError            = "upstream_read_error"
	runtimeStreamOutcomeUpstreamEndedWithoutTerminal = "upstream_ended_without_terminal"
	runtimeStreamOutcomeUnknown                      = "unknown"

	runtimeStreamErrorKindClientWriteFailed      = "client_write_failed"
	runtimeStreamErrorKindRequestContextCanceled = "request_context_canceled"
	runtimeStreamErrorKindGatewayStreamDeadline  = "gateway_stream_deadline_exceeded"
	runtimeStreamErrorKindUpstreamReadFailed     = "upstream_read_failed"
	runtimeStreamErrorKindMissingTerminalEvent   = "missing_terminal_event"
	runtimeStreamErrorDetailMaxLength            = 512
)

type runtimeResponseCapture struct {
	Body                     []byte
	AuditBody                []byte
	AuditBodyObserved        int64
	AuditBodyStored          int64
	AuditBodyTruncated       bool
	Usage                    responseUsage
	UsageRule                runtimeUsageNormalizationRule
	UsageSource              gatewaycore.UsageSource
	FirstMeaningfulPayloadAt *time.Time
	CompletedAt              *time.Time
	StreamOutcome            string
	StreamErrorKind          *string
	StreamErrorDetail        *string
}

func (capture runtimeResponseCapture) extractedUsage() responseUsage {
	if capture.Usage.hasValues() || capture.Usage.discarded {
		if capture.UsageRule.configured() {
			return capture.Usage.canonicalizedForRuntimeUsage(capture.UsageRule)
		}
		return capture.Usage.normalized()
	}
	return extractResponseUsage(capture.Body, capture.UsageRule).normalized()
}

func proxyNonEventResponseAndCaptureUsage(hooks operationResponseHooks, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if !responseMayContainJSONUsage(contentType) {
		auditBuffer, auditWriter := newBoundedAuditWriter(captureAuditBody)
		writers := []io.Writer{dst}
		if auditWriter != nil {
			writers = append(writers, auditWriter)
		}
		_, err := io.Copy(io.MultiWriter(writers...), src)
		completedAt := now()
		capture := runtimeResponseCapture{CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming, UsageRule: hooks.UsageRule, UsageSource: gatewaycore.UsageSourceMissing}
		if captureAuditBody {
			capture.AuditBody, capture.AuditBodyObserved, capture.AuditBodyStored, capture.AuditBodyTruncated = auditBuffer.snapshot()
		}
		return capture, err
	}
	capture := newStreamedResponseUsageCapture(hooks.UsageRule)
	auditBuffer, auditWriter := newBoundedAuditWriter(captureAuditBody)
	writers := []io.Writer{dst, capture}
	if auditWriter != nil {
		writers = append(writers, auditWriter)
	}
	_, copyErr := io.Copy(io.MultiWriter(writers...), src)
	completedAt := now()
	responseCapture := capture.runtimeResponseCapture(completedAt, captureAuditBody, nil)
	if captureAuditBody {
		responseCapture.AuditBody, responseCapture.AuditBodyObserved, responseCapture.AuditBodyStored, responseCapture.AuditBodyTruncated = auditBuffer.snapshot()
	}
	return responseCapture, copyErr
}

func responseMayContainJSONUsage(contentType string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	return trimmed == "" || strings.Contains(trimmed, "json")
}

func proxyNonEventResponseAndCaptureWithoutUsage(hooks operationResponseHooks, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	auditBuffer, auditWriter := newBoundedAuditWriter(captureAuditBody)
	writers := []io.Writer{dst}
	if auditWriter != nil {
		writers = append(writers, auditWriter)
	}
	_, err := io.Copy(io.MultiWriter(writers...), src)
	completedAt := now()
	capture := runtimeResponseCapture{CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming}
	if captureAuditBody {
		capture.AuditBody, capture.AuditBodyObserved, capture.AuditBodyStored, capture.AuditBodyTruncated = auditBuffer.snapshot()
	}
	return capture, err
}

type streamedResponseUsageCapture struct {
	parser *streamedResponseUsageParser
}

func newStreamedResponseUsageCapture(rule runtimeUsageNormalizationRule) *streamedResponseUsageCapture {
	return &streamedResponseUsageCapture{parser: newStreamedResponseUsageParser(rule)}
}

func (capture *streamedResponseUsageCapture) Write(payload []byte) (int, error) {
	capture.parser.consume(payload)
	return len(payload), nil
}

func (capture *streamedResponseUsageCapture) runtimeResponseCapture(completedAt time.Time, captureAuditBody bool, auditBody []byte) runtimeResponseCapture {
	usage := capture.parser.extractedUsage()
	responseCapture := runtimeResponseCapture{
		Body:          buildUsageBodyFromResponseUsage(usage),
		Usage:         usage,
		UsageRule:     capture.parser.rule,
		UsageSource:   runtimeUsageSourceFromUsage(usage, runtimeStreamOutcomeNotStreaming),
		CompletedAt:   &completedAt,
		StreamOutcome: runtimeStreamOutcomeNotStreaming,
	}
	if captureAuditBody {
		responseCapture.AuditBody = append([]byte(nil), auditBody...)
	}
	return responseCapture
}

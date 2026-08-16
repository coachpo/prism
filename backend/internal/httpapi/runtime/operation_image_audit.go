package runtime

import (
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

// Audit-body redaction for the image operations.
//
// The capture counters (observed, stored, truncated) keep describing the bytes
// seen on the wire: they are capture facts, and redaction is a separate
// transformation applied on the way to persistence. Only the persisted body is
// rewritten.

const runtimeImageEventStreamContentType = "text/event-stream"

// runtimeOperationCapturesImageBodies reports whether an operation's audit
// bodies can carry base64 image payloads. It reads the response hook kind so
// the registry stays the single source of truth for what an operation is.
func runtimeOperationCapturesImageBodies(operation RuntimeOperation) bool {
	hooks, ok := responseHooksForOperation(operation)
	return ok && hooks.Kind == operationResponseKindImageGeneration
}

func runtimeCapturedAuditRequestBodyForOperation(operation RuntimeOperation, enabled bool, body []byte) *string {
	if !enabled || len(body) == 0 {
		return nil
	}
	if !runtimeOperationCapturesImageBodies(operation) {
		return runtimeCapturedAuditBody(enabled, body)
	}
	return auditBodyPointer(openai.RedactImageRequestAuditBody(body))
}

// runtimeCapturedAuditResponseBodyForOperation redacts a captured image
// response. Streaming and non-streaming responses take different shapes — an
// SSE frame sequence versus one JSON document — so the stream outcome selects
// the redaction mode rather than a response header that is not carried this far.
func runtimeCapturedAuditResponseBodyForOperation(operation RuntimeOperation, enabled bool, body []byte, streamOutcome string) *string {
	if !enabled || len(body) == 0 {
		return nil
	}
	if !runtimeOperationCapturesImageBodies(operation) {
		return runtimeCapturedAuditBody(enabled, body)
	}
	contentType := ""
	if runtimeStreamOutcomeIsStreaming(streamOutcome) {
		contentType = runtimeImageEventStreamContentType
	}
	return auditBodyPointer(openai.RedactImageResponseAuditBody(body, contentType))
}

func auditBodyPointer(body []byte) *string {
	if len(body) == 0 {
		return nil
	}
	resolved := string(body)
	return &resolved
}

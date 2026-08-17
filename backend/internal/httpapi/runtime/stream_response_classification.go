package runtime

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
)

func classifySSEStreamOutcome(ctx context.Context, terminal sseTerminalSignal, upstreamErr error, writeErr error) sseStreamClassification {
	if writeErr != nil {
		return sseStreamClassification{outcome: runtimeStreamOutcomeClientDisconnected, kind: stringPtr(runtimeStreamErrorKindClientWriteFailed), detail: sanitizedStreamErrorDetail(writeErr)}
	}
	if ctx != nil && ctx.Err() != nil {
		// A client that goes away cancels the inbound request context
		// (context.Canceled). A budget the gateway itself imposed expires it
		// (context.DeadlineExceeded). Blaming the client for both is what made
		// gateway-side truncation unattributable in the request log.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return sseStreamClassification{outcome: runtimeStreamOutcomeGatewayTimeout, kind: stringPtr(runtimeStreamErrorKindGatewayStreamDeadline), detail: sanitizedStreamErrorDetail(ctx.Err())}
		}
		return sseStreamClassification{outcome: runtimeStreamOutcomeClientDisconnected, kind: stringPtr(runtimeStreamErrorKindRequestContextCanceled), detail: sanitizedStreamErrorDetail(ctx.Err())}
	}
	if upstreamErr != nil && !errors.Is(upstreamErr, io.EOF) {
		return sseStreamClassification{outcome: runtimeStreamOutcomeUpstreamReadError, kind: stringPtr(runtimeStreamErrorKindUpstreamReadFailed), detail: sanitizedStreamErrorDetail(upstreamErr)}
	}
	switch terminal {
	case sseTerminalSignalProviderIncomplete:
		return sseStreamClassification{outcome: runtimeStreamOutcomeProviderIncomplete}
	case sseTerminalSignalCompleted:
		return sseStreamClassification{outcome: runtimeStreamOutcomeCompleted}
	case sseTerminalSignalNone:
		return sseStreamClassification{outcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, kind: stringPtr(runtimeStreamErrorKindMissingTerminalEvent)}
	default:
		return sseStreamClassification{outcome: runtimeStreamOutcomeUnknown}
	}
}

var runtimeStreamErrorAuthorizationBearerPattern = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization)\b\s*[:=]\s*Bearer\s+[A-Za-z0-9._~+/=-]+`)

var runtimeStreamErrorSensitiveFragmentPattern = regexp.MustCompile(`(?i)\b(x-api-key|api[-_ ]?key|api[-_ ]?token|token|secret|password|cookie)\b\s*[:=]\s*("[^"]*"|'[^']*'|\S+)`)

var runtimeStreamErrorBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)

var runtimeStreamErrorJSONObjectPattern = regexp.MustCompile(`\{[^{}]{0,200}\}`)

var runtimeStreamErrorJSONArrayPattern = regexp.MustCompile(`\[[^\[\]]{0,200}\]`)

func sanitizedStreamErrorDetail(err error) *string {
	if err == nil {
		return nil
	}
	detail := strings.Join(strings.Fields(err.Error()), " ")
	if detail == "" {
		return nil
	}
	detail = runtimeStreamErrorAuthorizationBearerPattern.ReplaceAllString(detail, "$1=[REDACTED]")
	detail = runtimeStreamErrorSensitiveFragmentPattern.ReplaceAllString(detail, "$1=[REDACTED]")
	detail = runtimeStreamErrorBearerPattern.ReplaceAllString(detail, "Bearer [REDACTED]")
	detail = runtimeStreamErrorJSONObjectPattern.ReplaceAllString(detail, "[REDACTED]")
	detail = runtimeStreamErrorJSONArrayPattern.ReplaceAllString(detail, "[REDACTED]")
	detail = strings.TrimSpace(strings.Join(strings.Fields(detail), " "))
	if detail == "" {
		return nil
	}
	if len(detail) > runtimeStreamErrorDetailMaxLength {
		detail = detail[:runtimeStreamErrorDetailMaxLength]
	}
	return &detail
}

func payloadHasMeaningfulStreamContent(payload map[string]any) bool {
	return payloadContainsMeaningfulValue(payload)
}

func payloadContainsMeaningfulValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			switch key {
			case "usage", "usageMetadata", "type", "id", "model", "role", "index", "stop_reason", "stop_sequence", "finishReason":
				continue
			case "text", "delta", "output_text", "partial_json", "arguments", "reasoning", "thinking":
				if strings.TrimSpace(stringValue(nested)) != "" {
					return true
				}
			}
			if payloadContainsMeaningfulValue(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if payloadContainsMeaningfulValue(nested) {
				return true
			}
		}
	case string:
		return strings.TrimSpace(typed) != ""
	}
	return false
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

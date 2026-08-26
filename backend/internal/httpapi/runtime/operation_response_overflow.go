package runtime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

type cliProxyAPIOverflowClassification struct {
	Promotable bool
	ErrorCode  string
	Classifier string
}

type cliProxyAPIOverflowEvidence struct {
	code    *string
	message *string
}

const (
	cliProxyAPIOverflowClassifierErrorCode   = "error_code"
	cliProxyAPIOverflowClassifierMessageText = "message_text"
)

func classifyCLIProxyAPIOverflowResponse(statusCode int, rawBody []byte) cliProxyAPIOverflowClassification {
	if !cliProxyAPIOverflowStatusAllowed(statusCode) {
		return cliProxyAPIOverflowClassification{}
	}
	payload, ok := decodeCLIProxyAPIOverflowPayload(rawBody)
	if !ok {
		return cliProxyAPIOverflowClassification{}
	}
	evidence, ok := extractCLIProxyAPIOverflowEvidence(payload)
	if !ok {
		return cliProxyAPIOverflowClassification{}
	}
	if evidence.code != nil {
		code := strings.ToLower(strings.TrimSpace(*evidence.code))
		if code == "context_length_exceeded" || code == "context_too_large" {
			return cliProxyAPIOverflowClassification{Promotable: true, ErrorCode: code, Classifier: cliProxyAPIOverflowClassifierErrorCode}
		}
		return cliProxyAPIOverflowClassification{}
	}
	message := normalizedCLIProxyAPIOverflowMessage(evidence.message)
	if message == "" || cliProxyAPIOverflowMessageRejected(message) {
		return cliProxyAPIOverflowClassification{}
	}
	if cliProxyAPIOverflowMessageSignalsOverflow(message) {
		return cliProxyAPIOverflowClassification{Promotable: true, Classifier: cliProxyAPIOverflowClassifierMessageText}
	}
	return cliProxyAPIOverflowClassification{}
}

func cliProxyAPIOverflowStatusAllowed(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func nonStreamResponseRequiresBufferedInspection(statusCode int) bool {
	return cliProxyAPIOverflowStatusAllowed(statusCode)
}

func decodeCLIProxyAPIOverflowPayload(rawBody []byte) (map[string]any, bool) {
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return nil, false
	}
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}
	return payload, true
}

func extractCLIProxyAPIOverflowEvidence(payload map[string]any) (cliProxyAPIOverflowEvidence, bool) {
	if payload == nil {
		return cliProxyAPIOverflowEvidence{}, false
	}
	errorPayload, hasErrorObject := payload["error"].(map[string]any)
	if hasErrorObject {
		return extractCLIProxyAPIOverflowEvidenceFromErrorObject(errorPayload, payload)
	}
	return extractCLIProxyAPIOverflowEvidenceFromFlatPayload(payload)
}

func extractCLIProxyAPIOverflowEvidenceFromErrorObject(errorPayload map[string]any, payload map[string]any) (cliProxyAPIOverflowEvidence, bool) {
	if errorPayload == nil {
		return cliProxyAPIOverflowEvidence{}, false
	}
	evidence := cliProxyAPIOverflowEvidence{code: trimmedStringFromAny(errorPayload["code"])}
	if message := trimmedStringFromAny(firstValue(errorPayload, "message", "detail")); message != nil {
		evidence.message = message
	} else {
		evidence.message = trimmedStringFromAny(firstValue(payload, "message", "detail"))
	}
	return evidence, evidence.code != nil || evidence.message != nil
}

func extractCLIProxyAPIOverflowEvidenceFromFlatPayload(payload map[string]any) (cliProxyAPIOverflowEvidence, bool) {
	evidence := cliProxyAPIOverflowEvidence{
		code:    trimmedStringFromAny(payload["code"]),
		message: trimmedStringFromAny(firstValue(payload, "detail", "message", "error")),
	}
	return evidence, evidence.code != nil || evidence.message != nil
}

func normalizedCLIProxyAPIOverflowMessage(message *string) string {
	if message == nil {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(*message), " "))
}

func cliProxyAPIOverflowMessageSignalsOverflow(message string) bool {
	return containsCLIProxyAPIOverflowFragment(message,
		"maximum context length",
		"max context length",
		"context length",
		"context window",
		"too many tokens",
	) && containsCLIProxyAPIOverflowFragment(message,
		"exceeded",
		"exceeds",
		"too large",
		"too long",
		"over limit",
		"over the limit",
	)
}

func cliProxyAPIOverflowMessageRejected(message string) bool {
	return containsCLIProxyAPIOverflowFragment(message,
		"model_not_found",
		"model not found",
		"unknown model",
		"unknown provider",
		"does not exist",
		"invalid_api_key",
		"invalid api key",
		"incorrect api key",
		"authentication",
		"unauthorized",
		"permission denied",
		"forbidden",
		"insufficient_quota",
		"insufficient quota",
		"quota exceeded",
		"credit balance",
		"balance",
		"billing",
		"hard limit",
		"rate_limit",
		"rate limit",
		"too many requests",
		"tokens per minute",
		"per minute",
		"retry after",
		"server overloaded",
		"overloaded",
		"capacity exceeded",
		"capacity exhausted",
		"temporarily unavailable",
		"try again later",
		"moderation",
		"safety",
	)
}

func containsCLIProxyAPIOverflowFragment(message string, fragments ...string) bool {
	for _, fragment := range fragments {
		if fragment != "" && strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

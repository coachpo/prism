package openai

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Audit redaction for the image operations.
//
// Image requests and responses carry base64 image payloads that must never
// reach the audit tables: a single response can be tens of megabytes, and the
// bytes have no diagnostic value. Redaction keeps the surrounding metadata
// (model, prompt, size, quality, usage) so an audited image call stays
// explainable, and replaces only the payload fields.
//
// These are pure byte-to-byte functions. The image operations bind their model
// from the JSON body like the text operations do, so no adapter-level media
// request type is needed.

const (
	redactedImagePayload  = "[redacted image bytes]"
	redactedImageRequest  = `{"body":"[redacted image request]"}`
	redactedImageResponse = `{"body":"[redacted image response]"}`

	// dataURLPrefix marks an inline base64 image reference. Plain https URLs
	// and uploaded file ids stay readable because they are short and are what
	// makes an audited edit request reproducible.
	dataURLPrefix = "data:"
)

// RedactImageRequestAuditBody removes inline image payloads from an image
// request body. Generations bodies carry no image input and pass through with
// their metadata intact; edits bodies carry `images[]` and `mask` references
// that may be inline data URLs.
func RedactImageRequestAuditBody(rawBody []byte) []byte {
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return nil
	}
	return redactImageJSONDocument(rawBody, redactedImageRequest)
}

// RedactImageResponseAuditBody removes generated image payloads from an image
// response body. Server-sent event streams are redacted event by event so the
// event names, indices and terminal usage object survive; everything else is
// treated as a single JSON document.
func RedactImageResponseAuditBody(rawBody []byte, contentType string) []byte {
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return nil
	}
	if isEventStreamContentType(contentType) {
		return redactImageEventStream(rawBody)
	}
	return redactImageJSONDocument(rawBody, redactedImageResponse)
}

func isEventStreamContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream")
}

// redactImageEventStream rewrites the `data:` payload of every SSE line and
// leaves the frame structure untouched. A line whose payload does not parse is
// replaced wholesale rather than passed through, so a truncated final event can
// never leak a partial base64 blob.
func redactImageEventStream(rawBody []byte) []byte {
	lines := bytes.Split(rawBody, []byte("\n"))
	for index, line := range lines {
		trimmedRight := bytes.TrimRight(line, "\r")
		carriageReturn := len(line) != len(trimmedRight)
		if !bytes.HasPrefix(trimmedRight, []byte("data:")) {
			continue
		}
		payload := bytes.TrimPrefix(trimmedRight, []byte("data:"))
		leadingSpace := len(payload) > 0 && payload[0] == ' '
		payload = bytes.TrimSpace(payload)
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		rebuilt := []byte("data:")
		if leadingSpace {
			rebuilt = append(rebuilt, ' ')
		}
		rebuilt = append(rebuilt, redactImageJSONDocument(payload, redactedImageResponse)...)
		if carriageReturn {
			rebuilt = append(rebuilt, '\r')
		}
		lines[index] = rebuilt
	}
	return bytes.Join(lines, []byte("\n"))
}

// redactImageJSONDocument parses one JSON document and rewrites every image
// payload it contains. An unparseable document is replaced by the fallback so a
// truncated or non-JSON body never reaches the audit tables unredacted.
func redactImageJSONDocument(rawBody []byte, fallback string) []byte {
	var payload any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return []byte(fallback)
	}
	redacted, err := json.Marshal(redactImageJSONValue(payload, ""))
	if err != nil {
		return []byte(fallback)
	}
	return redacted
}

func redactImageJSONValue(value any, key string) any {
	if redactsImageKey(key) {
		return redactedImagePayload
	}
	if isImageReferenceKey(key) {
		if reference, ok := value.(string); ok && strings.HasPrefix(strings.TrimSpace(reference), dataURLPrefix) {
			return redactedImagePayload
		}
	}
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			redacted[childKey] = redactImageJSONValue(childValue, childKey)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for index, childValue := range typed {
			// Array elements inherit the key of the array itself so that
			// `images: ["data:..."]` is redacted the same way a single value is.
			redacted[index] = redactImageJSONValue(childValue, key)
		}
		return redacted
	default:
		return value
	}
}

// redactsImageKey lists the fields whose value is always raw image bytes.
func redactsImageKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "b64_json", "image", "mask":
		return true
	default:
		return false
	}
}

// isImageReferenceKey lists the fields that hold an image reference which may
// be either a short URL or file id (kept) or an inline data URL (redacted).
func isImageReferenceKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "image_url", "url":
		return true
	default:
		return false
	}
}

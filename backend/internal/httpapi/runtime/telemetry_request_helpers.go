package runtime

import (
	"encoding/json"
	"strings"
)

func requestWantsStream(rawBody []byte, requestPath string) bool {
	if strings.Contains(strings.TrimSpace(requestPath), ":streamGenerateContent") {
		return true
	}
	return requestBodyWantsStream(rawBody)
}

func requestBodyWantsStream(rawBody []byte) bool {
	if len(rawBody) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return false
	}
	stream, ok := payload["stream"].(bool)
	return ok && stream
}

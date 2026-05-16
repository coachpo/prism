package sidecars

import "strings"

func sidecarTextLooksSensitive(value string) bool {
	normalized := strings.ToLower(value)
	markers := []string{"authorization", "bearer ", "cookie", "set-cookie", "x-api-key", "apikey", "api_key", "access_token", "refresh_token", "chatgpt-account-id", "account_id", "account id", "\"body\"", "body:", "\"headers\"", "headers:", "raw-", "secret"}
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return strings.Contains(normalized, "@")
}

func isSensitiveHeaderName(name string) bool {
	normalized := normalizedSnapshotKey(name)
	return strings.Contains(normalized, "cookie") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "auth") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "xapikey") ||
		strings.Contains(normalized, "oauth") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential")
}

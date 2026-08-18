package runtime

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

func runtimeAuditRequestURL(requestURL string, request *http.Request) string {
	trimmed := strings.TrimSpace(requestURL)
	if trimmed != "" {
		return trimmed
	}
	if request == nil || request.URL == nil {
		return ""
	}
	return request.URL.String()
}

// marshalAuditHeaders serializes scrubbed canonical request header entries.
// Every value is irreversibly scrubbed with the fixed-bottom-line matcher
// (Requests SPEC §5.5) and the request-time effective Header Blocklist before
// it may enter telemetry; pre-scrub values never reach the outbox, staging, DB,
// logs, or traces.
func marshalAuditHeaders(headers map[string]string, extraRules []safediag.SensitiveNameRule) string {
	entries := scrubAuditHeaderMap(headers, extraRules)
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func marshalAuditHTTPHeaders(headers http.Header, extraRules []safediag.SensitiveNameRule) *string {
	entries := scrubAuditHTTPHeaderEntries(headers, extraRules)
	if entries == nil {
		return nil
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil
	}
	resolved := string(encoded)
	return &resolved
}

// scrubAuditHeaderMap returns canonical lowercased-name entries with every
// value redacted when its name is sensitive; non-sensitive values keep their
// original bytes (they were already sanitized by buildUpstreamHeaders).
func scrubAuditHeaderMap(headers map[string]string, extraRules []safediag.SensitiveNameRule) []auditHeaderEntry {
	if len(headers) == 0 {
		return []auditHeaderEntry{}
	}
	matcher := safediag.NewSensitiveNameMatcher(extraRules...)
	entries := make([]auditHeaderEntry, 0, len(headers))
	for key, value := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
		entry := auditHeaderEntry{Name: normalizedKey, Value: scrubbed.Value}
		if matcher.IsSensitiveName(normalizedKey) {
			entry.Value = safediag.RedactedMarker
		} else {
			// Non-sensitive names still carry caller-controlled values: the
			// fixed-bottom-line value scrubber removes embedded credentials
			// (Bearer/sk-/token= fragments) while preserving safe text.
			scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxAuditHeaderValueBytes})
			entry.Value = scrubbed.Value
		}
		entries = append(entries, entry)
	}
	sortAuditHeaderEntries(entries)
	return entries
}

func scrubAuditHTTPHeaderEntries(headers http.Header, extraRules []safediag.SensitiveNameRule) []auditHeaderEntry {
	if len(headers) == 0 {
		return nil
	}
	matcher := safediag.NewSensitiveNameMatcher(extraRules...)
	entries := make([]auditHeaderEntry, 0, len(headers))
	for key, values := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		for _, value := range values {
			scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
			entry := auditHeaderEntry{Name: normalizedKey, Value: scrubbed.Value}
			if matcher.IsSensitiveName(normalizedKey) {
				entry.Value = safediag.RedactedMarker
			} else {
				scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxAuditHeaderValueBytes})
				entry.Value = scrubbed.Value
			}
			entries = append(entries, entry)
		}
	}
	sortAuditHeaderEntries(entries)
	return entries
}

type auditHeaderEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// sortAuditHeaderEntries orders entries by lowercase name then original value
// ordinal (stable), preserving duplicate values.
func sortAuditHeaderEntries(entries []auditHeaderEntry) {
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].Name != entries[right].Name {
			return entries[left].Name < entries[right].Name
		}
		return entries[left].Value < entries[right].Value
	})
}

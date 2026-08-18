package startup

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

const legacyAuditBodyCapBytes = 4 * 1024 * 1024

// legacyBodyBytes converts a legacy TEXT body to a BYTEA prefix capped at
// 4 MiB per body and (for request bodies) the shared 12 MiB ingress budget.
func legacyBodyBytes(body *string, budgetRemaining *int64) ([]byte, int64, int64, bool) {
	if body == nil {
		return nil, 0, 0, false
	}
	raw := []byte(*body)
	observed := int64(len(raw))
	stored := observed
	if stored > int64(legacyAuditBodyCapBytes) {
		stored = int64(legacyAuditBodyCapBytes)
	}
	if budgetRemaining != nil && stored > *budgetRemaining {
		stored = *budgetRemaining
	}
	if budgetRemaining != nil {
		*budgetRemaining -= stored
	}
	if stored == 0 {
		return nil, observed, 0, observed > 0
	}
	return raw[:stored], observed, stored, stored < observed
}

func legacyBodyEncoding(observed int64) *string {
	if observed <= 0 {
		return nil
	}
	encoding := "utf8"
	return &encoding
}

func legacyEndState(observed int64) *string {
	if observed <= 0 {
		return nil
	}
	state := "unknown"
	return &state
}

func legacyBodyCaptureStatus(observed int64, stored int64, truncated bool) string {
	switch {
	case observed == 0:
		return "legacy_unknown"
	case stored == 0:
		return "omitted_ingress_budget"
	case truncated:
		return "truncated"
	default:
		return "captured"
	}
}

func legacyBodyLimitReason(observed int64, stored int64) string {
	switch {
	case observed == 0:
		return "none"
	case stored == 0:
		return "ingress_budget"
	case stored < observed:
		return "body_cap"
	default:
		return "none"
	}
}

func legacyAllValuesRedactedHeaders(raw string) string {
	if raw == "" {
		return "[]"
	}
	// Legacy header TEXT has no verifiable request-time scrub snapshot: every
	// value is replaced with the fixed legacy marker. Only parsed names stay.
	entries := parseLegacyHeaderEntries(raw)
	if len(entries) == 0 {
		return "[]"
	}
	encoded, _ := json.Marshal(entries)
	return string(encoded)
}

func legacyAllValuesRedactedHeadersOptional(raw *string) *string {
	if raw == nil {
		return nil
	}
	resolved := legacyAllValuesRedactedHeaders(*raw)
	return &resolved
}

func parseLegacyHeaderEntries(raw string) []map[string]string {
	var entries []map[string]string
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		for name := range parsed {
			entries = append(entries, map[string]string{"name": name, "value": legacyRedactedHeaderValue})
		}
		return entries
	}
	// Plain-text "Name: value" lines.
	for _, line := range splitLegacyHeaderLines(raw) {
		for index := 0; index < len(line); index++ {
			if line[index] == ':' {
				name := trimLegacySpace(line[:index])
				if name != "" {
					entries = append(entries, map[string]string{"name": name, "value": legacyRedactedHeaderValue})
				}
				break
			}
		}
	}
	return entries
}

func splitLegacyHeaderLines(raw string) []string {
	var lines []string
	start := 0
	for index := 0; index <= len(raw); index++ {
		if index == len(raw) || raw[index] == '\n' {
			lines = append(lines, raw[start:index])
			start = index + 1
		}
	}
	return lines
}

func trimLegacySpace(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\r') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}

// scrubLegacyMetadata applies the fixed §4.3 value scrubber and caps to a
// legacy ordinary metadata value.
func scrubLegacyMetadata(value *string) *string {
	if value == nil {
		return nil
	}
	scrubbed := safediag.ScrubValue(*value, safediag.ScrubOptions{MaxBytes: safediag.MaxLabelBytes})
	if stringsLegacyTrim(scrubbed.Value) == "" {
		return nil
	}
	return &scrubbed.Value
}

func stringsLegacyTrim(value string) string {
	return trimLegacySpace(value)
}

// legacyPathOnly strips query/fragment from a legacy request path so only
// the path portion enters request_logs.request_path (§4.3 scrubbed URL rule).
func legacyPathOnly(path string) string {
	for index := 0; index < len(path); index++ {
		if path[index] == '?' || path[index] == '#' {
			return path[:index]
		}
	}
	return path
}

func derefLegacyString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalLegacyString(value string) *string {
	if stringsLegacyTrim(value) == "" {
		return nil
	}
	return &value
}

func legacyMetadataRedactedFields(fields ...*string) []string {
	var redacted []string
	for _, field := range fields {
		if field != nil {
			redacted = append(redacted, "caller_user_agent")
			break
		}
	}
	return redacted
}

func legacyMetadataTruncatedFields(fields ...*string) []string {
	return []string{}
}

// legacyPricingStatus is the conservative four-state backfill for legacy
// rows: non-2xx -> ineligible, 2xx with reason -> unpriced, else unknown
// (Pricing SPEC §3.4 conservative projection).
func legacyPricingStatus(success bool, unpricedReason *string) string {
	if !success {
		return "ineligible"
	}
	if unpricedReason != nil && stringsLegacyTrim(*unpricedReason) != "" {
		return "unpriced"
	}
	return "unknown"
}

func legacyProxyKeyAttribution(proxyAPIKeyID *int) string {
	if proxyAPIKeyID == nil {
		return "none"
	}
	return "identified"
}

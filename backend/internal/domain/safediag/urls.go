package safediag

import (
	"net/url"
	"strings"
)

// URLScrubProvenance describes how a URL was sanitized.
type URLScrubProvenance string

const (
	URLScrubProvenanceRuntimeScrubbed  URLScrubProvenance = "runtime_scrubbed"
	URLScrubProvenanceLegacyRescrubbed URLScrubProvenance = "legacy_rescrubbed"
	URLScrubProvenanceLegacyUnknown    URLScrubProvenance = "legacy_unknown"
	URLScrubProvenanceNotApplicable    URLScrubProvenance = "not_applicable"
)

// ScrubRequestURL sanitizes a request URL for persistence: only normalized
// scheme/host/path plus query parameter names are retained; every query value
// is unconditionally replaced with [REDACTED]; userinfo and fragment are
// removed entirely. The result is capped at MaxRequestURLBytes and
// MaxRequestURLCodePoints.
func ScrubRequestURL(rawURL string) (string, bool) {
	scrubbed := scrubURL(rawURL, true)
	scrubbed, truncated := capURL(scrubbed, MaxRequestURLBytes, MaxRequestURLCodePoints)
	return scrubbed, truncated
}

// ScrubEndpointBaseURL sanitizes an endpoint base URL: only scheme/host/base
// path are retained; userinfo, query, and fragment are removed. The result is
// capped at MaxEndpointBaseURLBytes and MaxEndpointBaseURLCodePoints.
func ScrubEndpointBaseURL(rawURL string) (string, bool) {
	scrubbed := scrubURL(rawURL, false)
	scrubbed, truncated := capURL(scrubbed, MaxEndpointBaseURLBytes, MaxEndpointBaseURLCodePoints)
	return scrubbed, truncated
}

// ScrubRequestPath cleans a request path for request_logs.request_path: it
// strips control characters and caps at MaxRequestPathBytes and
// MaxRequestPathCodePoints. It never returns a raw string with control chars.
func ScrubRequestPath(path string) (string, bool) {
	cleaned := stripControlCharacters(path)
	cleaned = foldWhitespace(cleaned)
	cleaned, truncated := TruncateUTF8(cleaned, MaxRequestPathBytes)
	if !truncated {
		var wasTruncated bool
		cleaned, wasTruncated = TruncateCodePoints(cleaned, MaxRequestPathCodePoints)
		truncated = wasTruncated
	}
	return strings.TrimSpace(cleaned), truncated
}

func scrubURL(rawURL string, keepQueryNames bool) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" {
		// Parse failure: keep only a control-character-cleaned path (no query,
		// no fragment, no userinfo).
		pathOnly := trimmed
		if idx := strings.IndexAny(pathOnly, "?#"); idx >= 0 {
			pathOnly = pathOnly[:idx]
		}
		if atIdx := strings.LastIndex(pathOnly, "@"); atIdx >= 0 {
			pathOnly = pathOnly[atIdx+1:]
		}
		return stripControlCharacters(pathOnly)
	}
	var builder strings.Builder
	builder.WriteString(parsed.Scheme)
	builder.WriteString("://")
	if parsed.Host != "" {
		builder.WriteString(parsed.Host)
	}
	if parsed.Path != "" {
		builder.WriteString(parsed.Path)
	}
	if keepQueryNames && parsed.RawQuery != "" {
		builder.WriteString("?")
		names := make([]string, 0)
		for _, pair := range strings.Split(parsed.RawQuery, "&") {
			name := pair
			if idx := strings.IndexByte(pair, '='); idx >= 0 {
				name = pair[:idx]
			}
			if name == "" {
				continue
			}
			names = append(names, name+"="+RedactedMarker)
		}
		builder.WriteString(strings.Join(names, "&"))
	}
	// userinfo and fragment are dropped by construction.
	return builder.String()
}

func capURL(scrubbed string, maxBytes int, maxCodePoints int) (string, bool) {
	if scrubbed == "" {
		return "", false
	}
	truncated := false
	if len(scrubbed) > maxBytes {
		scrubbed, _ = TruncateUTF8(scrubbed, maxBytes)
		truncated = true
	}
	if utf8RuneCount(scrubbed) > maxCodePoints {
		scrubbed, _ = TruncateCodePoints(scrubbed, maxCodePoints)
		truncated = true
	}
	return scrubbed, truncated
}

func utf8RuneCount(s string) int {
	return len([]rune(s))
}

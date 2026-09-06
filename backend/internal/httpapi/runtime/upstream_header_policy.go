package runtime

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
)

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailers":            {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
}

// clientAuthHeaders are the caller's own credentials. Prism authenticates to
// the upstream with the connection's key, so forwarding these would leak a
// client credential to a third party and can collide with the header Prism
// sets. The provider auth profile's ControlledHeaderNames only covers the
// active api family (an Anthropic connection controls x-api-key, not
// authorization), so this full set is checked separately.
var clientAuthHeaders = map[string]struct{}{
	"authorization":  {},
	"x-api-key":      {},
	"x-goog-api-key": {},
}

// transportControlledHeaderNames are decided by Prism's own transport and the
// response decoding path, never by the caller. content-length is recomputed
// from the body Prism actually sends. accept-encoding must not be forwarded
// because the runtime transport runs with DisableCompression: a forwarded
// "gzip" would return a compressed body that the streaming parsers cannot read,
// breaking usage, token, and output-rate extraction.
var transportControlledHeaderNames = map[string]struct{}{
	"accept-encoding": {},
	"content-length":  {},
}

// forwardableClientHeaders forwards the caller's headers verbatim except for
// the ones that cannot survive a hop.
//
// This is a blocklist, and that is the point. Prism's goal at this boundary is
// that the upstream sees what the client actually sent, because a request
// stripped down to a handful of protocol headers is itself an anomaly: no real
// client sends an empty User-Agent and nothing else. Reproducing the caller's
// headers is what keeps the gateway from announcing itself. Withheld here are
// only the headers that are wrong to copy rather than merely revealing:
//
//   - hop-by-hop headers and host, which describe this connection, not the next
//     one (RFC 7230 §6.1)
//   - transport-controlled headers, which Prism recomputes
//   - the caller's own credentials, which must never reach an upstream Prism
//     authenticates by itself
//   - headers the provider auth profile controls, which Prism sets downstream
//
// Everything else, including User-Agent, tracing headers, and whatever headers
// a given IDE invents, is the operator's call through the Header Blocklist.
func forwardableClientHeaders(clientHeaders map[string]string, proxyControlledHeaders map[string]struct{}) map[string]string {
	forwarded := make(map[string]string, len(clientHeaders))
	for key, value := range clientHeaders {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		if _, blocked := transportControlledHeaderNames[keyLower]; blocked {
			continue
		}
		if _, blocked := clientAuthHeaders[keyLower]; blocked {
			continue
		}
		if _, blocked := proxyControlledHeaders[keyLower]; blocked {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(value)
		if !ok {
			continue
		}
		forwarded[key] = normalizedValue
	}
	return forwarded
}

// upstreamResponseHeaderNames is the set of upstream response headers allowed
// back to the caller. Upstream session state (set-cookie), identity and
// version exposure (server, x-powered-by, vendor-private x-* headers) and
// upstream CORS decisions (access-control-*) are not relayed: Prism owns this
// response.
var upstreamResponseHeaderNames = map[string]struct{}{
	"content-type":        {},
	"content-length":      {},
	"content-encoding":    {},
	"content-disposition": {},
	"cache-control":       {},
	"date":                {},
	"etag":                {},
	"last-modified":       {},
	"vary":                {},
	"retry-after":         {},
	"request-id":          {},
}

// upstreamResponseHeaderPrefixes keeps the rate-limit headers callers need
// for backpressure.
var upstreamResponseHeaderPrefixes = []string{"x-ratelimit-", "anthropic-ratelimit-", "openai-"}

func copyUpstreamResponseHeaders(target http.Header, source http.Header) {
	for key, values := range filterUpstreamResponseHeaders(source) {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func filterUpstreamResponseHeaders(source http.Header) http.Header {
	filtered := make(http.Header)
	for key, values := range source {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		if !upstreamResponseHeaderAllowed(keyLower) {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	return filtered
}

func upstreamResponseHeaderAllowed(keyLower string) bool {
	if _, allowed := upstreamResponseHeaderNames[keyLower]; allowed {
		return true
	}
	for _, prefix := range upstreamResponseHeaderPrefixes {
		if strings.HasPrefix(keyLower, prefix) {
			return true
		}
	}
	return false
}

type headerBlocklistRule struct {
	MatchType string
	Pattern   string
}

// auditableAttemptHeaders is what the audit trail records for one attempt: the
// headers the client actually sent, unioned with the headers Prism actually
// forwarded.
//
// Recording only the forwarded set would let the outbound filter erase its own
// evidence: a header that was dropped would leave no trace at all, so the
// operator could not answer "what did my client send, and what did Prism
// actually pass on?" from one record. Client-only entries are the ones that
// were seen and withheld; forwarded-only entries are what Prism added (provider
// auth, connection custom headers). Sensitive values are replaced downstream by
// the audit scrubber, which already derives its rules from the same blocklist.
func auditableAttemptHeaders(clientHeaders map[string]string, forwardedHeaders map[string]string) map[string]string {
	audited := make(map[string]string, len(clientHeaders)+len(forwardedHeaders))
	maps.Copy(audited, clientHeaders)
	maps.Copy(audited, forwardedHeaders)
	return audited
}

func (s *Service) buildUpstreamHeaders(connection runtimeConnection, apiFamily string, clientHeaders map[string]string, rules []headerBlocklistRule, stripBodyDependentHeaders bool) (map[string]string, error) {
	_ = apiFamily
	compiledAuth := connection.UpstreamAuth
	if compiledAuth == nil {
		return nil, fmt.Errorf("runtime upstream auth snapshot unavailable for connection %d", connection.ID)
	}
	proxyControlledHeaders := compiledAuth.ControlledHeaderNames

	headers := forwardableClientHeaders(clientHeaders, proxyControlledHeaders)
	headers = sanitizeHeaders(headers, rules)
	headers[compiledAuth.AuthHeader] = compiledAuth.AuthValue
	maps.Copy(headers, compiledAuth.ExtraHeaders)
	for key, rawValue := range connection.CustomHeaders {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, protected := proxyControlledHeaders[keyLower]; protected {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(fmt.Sprint(rawValue))
		if !ok {
			continue
		}
		// A custom header replaces the forwarded header of the same name, and
		// header names are case-insensitive while map keys are not: the caller's
		// "User-Agent" and a connection's "user-agent" are one header arriving
		// under two keys. Both would survive into doUpstreamRequest, where
		// Header.Set canonicalizes each in map iteration order and the last
		// write wins at random. Drop every case variant so the declared value is
		// the only one left.
		for existing := range headers {
			if strings.EqualFold(strings.TrimSpace(existing), keyLower) {
				delete(headers, existing)
			}
		}
		headers[key] = normalizedValue
	}

	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, protected := proxyControlledHeaders[keyLower]; protected || !headerIsBlocked(key, rules) {
			sanitized[key] = value
		}
	}
	if stripBodyDependentHeaders {
		// The merged body is re-encoded uncompressed JSON with a freshly
		// computed Content-Length; stale body-dependent headers from the
		// client, provider auth extras, or Connection custom_headers must not
		// reach the captured upstream.
		for key := range sanitized {
			keyLower := strings.ToLower(strings.TrimSpace(key))
			if _, bodyDependent := bodyDependentHeaders[keyLower]; bodyDependent {
				delete(sanitized, key)
			}
		}
	}
	return sanitized, nil
}

// bodyDependentHeaders are invalidated whenever Prism re-encodes an upstream
// request body for custom request parameters: Content-Length is recomputed
// from the new body and the rest describe digests or encodings of the old
// bytes.
var bodyDependentHeaders = map[string]struct{}{
	"content-length":   {},
	"content-encoding": {},
	"content-md5":      {},
	"digest":           {},
	"content-digest":   {},
}

func flattenHeaders(header http.Header) map[string]string {
	flattened := make(map[string]string, len(header))
	for key, values := range header {
		flattened[key] = strings.Join(values, ", ")
	}
	return flattened
}

func normalizeHeaderValue(value string) (string, bool) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", false
	}
	for _, character := range normalized {
		if character < 0x20 || character == 0x7f {
			return "", false
		}
	}
	return normalized, true
}

func sanitizeHeaders(headers map[string]string, rules []headerBlocklistRule) map[string]string {
	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		if headerIsBlocked(key, rules) {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}

func headerIsBlocked(name string, rules []headerBlocklistRule) bool {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	for _, rule := range rules {
		switch rule.MatchType {
		case "exact":
			if normalizedName == rule.Pattern {
				return true
			}
		case "prefix":
			if strings.HasPrefix(normalizedName, rule.Pattern) {
				return true
			}
		}
	}
	return false
}

func parseCustomHeaders(value sql.NullString) map[string]any {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return map[string]any{}
	}
	if parsed == nil {
		return map[string]any{}
	}
	return parsed
}

func copyResponseHeaders(target http.Header, source http.Header) {
	for key, values := range filterResponseHeaders(source) {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func filterResponseHeaders(source http.Header) http.Header {
	filtered := make(http.Header)
	for key, values := range source {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	return filtered
}

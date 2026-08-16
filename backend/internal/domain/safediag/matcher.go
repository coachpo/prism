package safediag

import "strings"

// SensitiveNameRule is an extra exact/prefix header name rule contributed by
// the request-time effective Header Blocklist. It can only make scrubbing
// stricter; it can never weaken the fixed bottom line.
type SensitiveNameRule struct {
	MatchType string // "exact" | "prefix"
	Pattern   string
}

// fixedExactNames are the immutable exact sensitive header names.
var fixedExactNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
	"x-goog-api-key":      {},
	"api-key":             {},
	"apikey":              {},
	"openai-api-key":      {},
	"anthropic-api-key":   {},
	// Trace/token carrier headers are treated as sensitive names: operators
	// routinely embed Bearer tokens in trace values, and the fixed bottom
	// line must never persist a caller-controlled trace credential.
	"x-trace-id":       {},
	"x-upstream-trace": {},
	// Non-secret exception: the value of this header is never a credential,
	// and it must not be redacted as a secret header name.
	"access-control-allow-credentials": {},
}

// fixedFragments are the immutable case-insensitive sensitive name fragments.
var fixedFragments = []string{
	"api-key",
	"api_key",
	"token",
	"secret",
	"credential",
	"password",
	"passwd",
	"private-key",
	"private_key",
	"session",
	"jwt",
}

// sensitiveNameMatcher implements the fixed bottom-line sensitive header name
// rules plus request-time extra rules. It is shared by runtime diagnostics,
// audit header scrubbing, legacy backfill, and the browser mask mirror.
type sensitiveNameMatcher struct {
	fixedExact    map[string]struct{}
	fixedFragment []string
	extraExact    map[string]struct{}
	extraPrefix   []string
}

// NewSensitiveNameMatcher builds the immutable matcher. extraRules are the
// request-time effective Header Blocklist rules (case-insensitive normalized);
// they are optional and never weaken the fixed rules.
func NewSensitiveNameMatcher(extraRules ...SensitiveNameRule) *sensitiveNameMatcher {
	matcher := &sensitiveNameMatcher{
		fixedExact:    make(map[string]struct{}, len(fixedExactNames)+4),
		fixedFragment: append([]string(nil), fixedFragments...),
		extraExact:    map[string]struct{}{},
		extraPrefix:   []string{},
	}
	for name := range fixedExactNames {
		matcher.fixedExact[name] = struct{}{}
	}
	for _, rule := range extraRules {
		pattern := strings.ToLower(strings.TrimSpace(rule.Pattern))
		if pattern == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rule.MatchType)) {
		case "exact":
			matcher.extraExact[pattern] = struct{}{}
		case "prefix":
			matcher.extraPrefix = append(matcher.extraPrefix, pattern)
		}
	}
	return matcher
}

// IsSensitiveName reports whether a header name must have its value redacted.
// The name is normalized to lowercase before matching. The non-secret
// exception is checked before fragments so that
// access-control-allow-credentials (which contains the fragment
// "credential") is never redacted.
func (matcher *sensitiveNameMatcher) IsSensitiveName(name string) bool {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	if normalizedName == "access-control-allow-credentials" {
		return false
	}
	if _, fixed := matcher.fixedExact[normalizedName]; fixed {
		return true
	}
	for _, fragment := range matcher.fixedFragment {
		if strings.Contains(normalizedName, fragment) {
			return true
		}
	}
	if _, extra := matcher.extraExact[normalizedName]; extra {
		return true
	}
	for _, prefix := range matcher.extraPrefix {
		if strings.HasPrefix(normalizedName, prefix) {
			return true
		}
	}
	return false
}

// FixedSensitiveNameList returns the fixed exact names for documentation and
// test parity with the frontend mask mirror.
func FixedSensitiveNameList() []string {
	return []string{
		"authorization",
		"proxy-authorization",
		"cookie",
		"set-cookie",
		"x-api-key",
		"x-goog-api-key",
		"api-key",
		"apikey",
		"openai-api-key",
		"anthropic-api-key",
	}
}

// FixedSensitiveFragments returns the fixed case-insensitive fragments for
// documentation and test parity with the frontend mask mirror.
func FixedSensitiveFragments() []string {
	return append([]string(nil), fixedFragments...)
}

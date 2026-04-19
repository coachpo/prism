package runtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func mustRuntimeJSON(t *testing.T, value any) []byte {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal runtime fixture: %v", err)
	}
	return raw
}

func TestResolveFailoverStatusCodes(t *testing.T) {
	adaptive := runtimeStrategyRecord{
		StrategyType:     "adaptive",
		RoutingPolicyRaw: mustRuntimeJSON(t, map[string]any{"circuit_breaker": map[string]any{"failure_status_codes": []int{408, 429}}}),
	}
	if got := resolveFailoverStatusCodes(adaptive); !reflect.DeepEqual(got, []int{408, 429}) {
		t.Fatalf("expected adaptive failover codes, got %v", got)
	}

	legacy := runtimeStrategyRecord{
		StrategyType:    "legacy",
		AutoRecoveryRaw: mustRuntimeJSON(t, map[string]any{"status_codes": []int{500, 503}}),
	}
	if got := resolveFailoverStatusCodes(legacy); !reflect.DeepEqual(got, []int{500, 503}) {
		t.Fatalf("expected legacy failover codes, got %v", got)
	}

	invalid := runtimeStrategyRecord{StrategyType: "adaptive", RoutingPolicyRaw: []byte("{")}
	if got := resolveFailoverStatusCodes(invalid); !reflect.DeepEqual(got, defaultFailoverStatusCodes) {
		t.Fatalf("expected default failover codes on invalid payload, got %v", got)
	}
}

func TestModelResolutionAndRewriteHelpers(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	if got, err := resolveModelID(rawBody, "/v1/chat/completions"); err != nil || got != "gpt-4o" {
		t.Fatalf("expected body model id, got model=%q err=%v", got, err)
	}
	if got, err := resolveModelID(nil, "/v1beta/models/gemini-2.5-pro:generateContent"); err != nil || got != "gemini-2.5-pro" {
		t.Fatalf("expected path model id, got model=%q err=%v", got, err)
	}
	if got := extractModelFromPath("/v1beta/models/gemini-2.5-pro:generateContent"); got != "gemini-2.5-pro" {
		t.Fatalf("expected path extraction to return Gemini model id, got %q", got)
	}

	rewrittenBody := rewriteModelInBody(rawBody, "gpt-4o-mini")
	if got := extractModelFromBody(rewrittenBody); got != "gpt-4o-mini" {
		t.Fatalf("expected rewritten model id in body, got %q", got)
	}
	if got := rewriteModelInPath("/v1beta/models/gemini-1.5-pro:generateContent", "gemini-1.5-pro", "gemini-2.5-pro"); got != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected rewritten Gemini path, got %q", got)
	}
	if _, err := resolveModelID([]byte(`{"messages":[]}`), "/v1/chat/completions"); err == nil {
		t.Fatal("expected missing model id to fail")
	}
}

func TestValidatePathCompatibilityAndHeaderHelpers(t *testing.T) {
	if err := validatePathCompatibility("openai", "/v1/chat/completions"); err != nil {
		t.Fatalf("expected OpenAI generic path to be valid, got %v", err)
	}
	if err := validatePathCompatibility("anthropic", "/v1/messages"); err != nil {
		t.Fatalf("expected Anthropic messages path to be valid, got %v", err)
	}
	if err := validatePathCompatibility("gemini", "/v1beta/models/gemini-2.5-pro:generateContent"); err != nil {
		t.Fatalf("expected Gemini native path to be valid, got %v", err)
	}

	err := validatePathCompatibility("openai", "/v1beta/models/gemini-2.5-pro:generateContent")
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr.StatusCode != http.StatusBadRequest || !strings.Contains(domainErr.Detail, "incompatible") {
		t.Fatalf("expected incompatibility domain error, got %v", err)
	}

	if got, ok := normalizeHeaderValue("  keep  "); !ok || got != "keep" {
		t.Fatalf("expected normalized header value, got value=%q ok=%v", got, ok)
	}
	if _, ok := normalizeHeaderValue("bad\nvalue"); ok {
		t.Fatal("expected control-character header value to be rejected")
	}

	rules := []headerBlocklistRule{{MatchType: "exact", Pattern: "x-remove"}, {MatchType: "prefix", Pattern: "x-secret-"}}
	sanitized := sanitizeHeaders(map[string]string{"X-Trace-Id": "1", "x-secret-token": "blocked", "X-Remove": "gone"}, rules)
	if !reflect.DeepEqual(sanitized, map[string]string{"X-Trace-Id": "1"}) {
		t.Fatalf("expected blocklisted headers to be removed, got %v", sanitized)
	}

	filtered := filterResponseHeaders(http.Header{"Connection": []string{"keep-alive"}, "X-Request-Id": []string{"abc"}})
	if filtered.Get("Connection") != "" || filtered.Get("X-Request-Id") != "abc" {
		t.Fatalf("expected hop-by-hop response headers to be filtered, got %v", filtered)
	}
}

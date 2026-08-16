package audit

import (
	"net/http"
	"testing"
)

func TestHeaderNameSensitive(t *testing.T) {
	sensitive := []string{
		"Authorization", "authorization", "Cookie", "Set-Cookie", "Proxy-Authorization",
		"X-Api-Key", "x-goog-api-key", "X-AMZ-Security-Token", "X-Token", "Access-Token",
		"Client-Secret", "X-Client-Secret", "X-RapidAPI-Key", "Private-Key", "X-PayPal-Client-Secret",
		"Content-Type", "Accept", "X-Request-Id", "User-Agent", "Traceparent", "X-Profile-Id",
	}
	expected := []bool{
		true, true, true, true, true,
		true, true, true, true, true,
		true, true, true, true, true,
		false, false, false, false, false, false,
	}
	if len(sensitive) != len(expected) {
		t.Fatalf("fixture length mismatch")
	}
	for index, name := range sensitive {
		if got := HeaderNameSensitive(name); got != expected[index] {
			t.Errorf("HeaderNameSensitive(%q) = %v, want %v", name, got, expected[index])
		}
	}
}

func TestHeaderValueSensitive(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{value: "Bearer sk-abc123XYZ890", want: true},
		{value: "bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.signature", want: true},
		{value: "Basic dXNlcjpwYXNzd29yZA==", want: true},
		{value: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.fakeSignature123", want: true},
		{value: "token=sekritvalue42; path=/", want: true},
		{value: "password=hunter2", want: true},
		{value: "AKIAIOSFODNN7EXAMPLE", want: true},
		{value: "sk-proj-abcdefghijklmnopqrstuvwxyz123456", want: true},
		{value: "text/plain; charset=utf-8", want: false},
		{value: "application/json", want: false},
		{value: "Mozilla/5.0 (Macintosh; Intel Mac OS X)", want: false},
		{value: "example.com", want: false},
		{value: "2026-08-09T10:00:00Z", want: false},
	}
	for _, tc := range cases {
		if got := HeaderValueSensitive(tc.value); got != tc.want {
			t.Errorf("HeaderValueSensitive(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestScrubHeaderMap(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer sk-secret-token-value",
		"Cookie":        "session=abc",
		"X-Trace-Id":    "abc123",
		"X-Context":     "token=leaked",
		"X-Project":     "prism",
	}
	scrubbed := ScrubHeaderMap(headers)
	if scrubbed["authorization"] != scrubSentinel {
		t.Errorf("authorization = %q, want %q", scrubbed["authorization"], scrubSentinel)
	}
	if scrubbed["cookie"] != scrubSentinel {
		t.Errorf("cookie = %q, want %q", scrubbed["cookie"], scrubSentinel)
	}
	if scrubbed["x-trace-id"] != "abc123" {
		t.Errorf("x-trace-id = %q, want abc123", scrubbed["x-trace-id"])
	}
	if scrubbed["x-context"] != scrubSentinel {
		t.Errorf("x-context (embedded token) = %q, want %q", scrubbed["x-context"], scrubSentinel)
	}
	if scrubbed["x-project"] != "prism" {
		t.Errorf("x-project = %q, want prism", scrubbed["x-project"])
	}
}

func TestMarshalScrubbedHTTPHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("Authorization", "Bearer sk-secret")
	header.Set("Set-Cookie", "sid=abc")
	header.Add("X-Content", "a")
	header.Add("X-Content", "b")
	encoded := MarshalScrubbedHTTPHeaders(header)
	if encoded == nil {
		t.Fatal("expected non-nil")
	}
	jsonText := *encoded
	for _, forbidden := range []string{"sk-secret", "sid=abc"} {
		if containsString(jsonText, forbidden) {
			t.Errorf("marshal output contains forbidden value %q: %s", forbidden, jsonText)
		}
	}
	if !containsString(jsonText, "[REDACTED]") {
		t.Errorf("marshal output missing sentinel: %s", jsonText)
	}
	if !containsString(jsonText, "\"x-content\":\"a, b\"") {
		t.Errorf("multi-value flatten mismatch: %s", jsonText)
	}
	if got := MarshalScrubbedHTTPHeaders(nil); got != nil {
		t.Errorf("empty headers should marshal to nil, got %q", *got)
	}
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for index := 0; index+len(needle) <= len(haystack); index++ {
			if haystack[index:index+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

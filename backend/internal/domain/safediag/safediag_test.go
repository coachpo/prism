package safediag

import (
	"strings"
	"testing"
)

func TestSensitiveNameMatcher(t *testing.T) {
	matcher := NewSensitiveNameMatcher()
	cases := []struct {
		name    string
		sensitive bool
	}{
		{"Authorization", true},
		{"authorization", true},
		{"Proxy-Authorization", true},
		{"Cookie", true},
		{"Set-Cookie", true},
		{"X-Api-Key", true},
		{"x-goog-api-key", true},
		{"api-key", true},
		{"apikey", true},
		{"openai-api-key", true},
		{"anthropic-api-key", true},
		{"X-Amz-Security-Token", true},
		{"X-Api-Secret", true},
		{"X-Client-Secret-Value", true},
		{"X-Password", true},
		{"Private-Key-Header", true},
		{"X-Session-Token", true},
		{"Jwt-Payload", true},
		{"X-Api_Key", true},
		{"x-credential-id", true},
		// Non-secret exception
		{"Access-Control-Allow-Credentials", false},
		// Innocent names
		{"Content-Type", false},
		{"User-Agent", false},
		{"X-Request-ID", false},
		{"X-Correlation-ID", false},
		{"X-Stainless-Retry-Count", false},
	}
	for _, tc := range cases {
		if got := matcher.IsSensitiveName(tc.name); got != tc.sensitive {
			t.Errorf("IsSensitiveName(%q) = %v, want %v", tc.name, got, tc.sensitive)
		}
	}
}

func TestSensitiveNameMatcherExtraRules(t *testing.T) {
	matcher := NewSensitiveNameMatcher(
		SensitiveNameRule{MatchType: "exact", Pattern: "X-Custom-Secret"},
		SensitiveNameRule{MatchType: "prefix", Pattern: "x-prism-"}, // prefix matches are case-normalized too
	)
	if !matcher.IsSensitiveName("X-Custom-Secret") {
		t.Error("extra exact rule not applied")
	}
	if !matcher.IsSensitiveName("x-prism-internal-token") {
		t.Error("extra prefix rule not applied")
	}
	if matcher.IsSensitiveName("Content-Type") {
		t.Error("fixed bottom line must not redact innocent names")
	}
	// Extra rules must never weaken fixed rules.
	if !matcher.IsSensitiveName("Authorization") {
		t.Error("fixed exact rule lost with extra rules")
	}
}

func TestScrubValueCredentials(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		redacted bool
	}{
		{"Bearer sk-abc123", "Bearer " + RedactedMarker, true},
		{"Authorization: Bearer sk-abc123", "Authorization: Bearer " + RedactedMarker, true},
		{"basic dXNlcjpwYXNz", "basic " + RedactedMarker, true},
		{"plain message", "plain message", false},
	}
	for _, tc := range cases {
		result := ScrubValue(tc.input, ScrubOptions{})
		if result.Value != tc.want {
			t.Errorf("ScrubValue(%q) = %q, want %q", tc.input, result.Value, tc.want)
		}
		if result.Redacted != tc.redacted {
			t.Errorf("ScrubValue(%q).Redacted = %v, want %v", tc.input, result.Redacted, tc.redacted)
		}
	}
}

func TestScrubValueJWT(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	result := ScrubValue("token="+jwt, ScrubOptions{})
	if strings.Contains(result.Value, "eyJhbGci") {
		t.Errorf("JWT fragment not redacted: %q", result.Value)
	}
	if !result.Redacted {
		t.Error("JWT redaction flag not set")
	}
}

func TestScrubValueKeyValue(t *testing.T) {
	result := ScrubValue(`{"api_key": "sk-123", "message": "denied"}`, ScrubOptions{})
	if strings.Contains(result.Value, "sk-123") {
		t.Errorf("sensitive key=value not redacted: %q", result.Value)
	}
	if !result.Redacted {
		t.Error("key=value redaction flag not set")
	}
}

func TestScrubValueURLSecrets(t *testing.T) {
	result := ScrubValue("upstream https://user:pass@example.com/v1?key=abc123 failed", ScrubOptions{})
	if strings.Contains(result.Value, "pass") || strings.Contains(result.Value, "abc123") {
		t.Errorf("URL credentials leaked: %q", result.Value)
	}
}

func TestScrubValueUTF8Boundary(t *testing.T) {
	// Multi-byte character split at cap must not produce invalid UTF-8.
	input := strings.Repeat("界", 3000) // 3 bytes per rune
	result := ScrubValue(input, ScrubOptions{MaxBytes: MaxErrorDetailBytes})
	if len(result.Value) > MaxErrorDetailBytes {
		t.Errorf("result exceeds cap: %d > %d", len(result.Value), MaxErrorDetailBytes)
	}
	if !strings.Contains(result.Value, "界") {
		t.Error("valid prefix must retain whole runes")
	}
	if !result.Truncated {
		t.Error("truncation flag not set")
	}
}

func TestScrubValueControlCharacters(t *testing.T) {
	result := ScrubValue("a\x00b\x1fc  d", ScrubOptions{})
	if strings.Contains(result.Value, "\x00") || strings.Contains(result.Value, "\x1f") {
		t.Errorf("control characters survived: %q", result.Value)
	}
	if strings.Contains(result.Value, "  ") {
		t.Errorf("whitespace not folded: %q", result.Value)
	}
}

func TestScrubURLText(t *testing.T) {
	cases := []struct{ input, want string }{
		{"https://example.com/v1/chat/completions", "https://example.com/v1/chat/completions"},
		{"https://user:pass@example.com/v1?key=abc&x=1#frag", "https://" + RedactedMarker + "@example.com/v1?key=" + RedactedMarker + "&x=" + RedactedMarker},
		{"https://example.com/v1?api_key=sk-123", "https://example.com/v1?api_key=" + RedactedMarker},
	}
	for _, tc := range cases {
		if got := ScrubURLText(tc.input); got != tc.want {
			t.Errorf("ScrubURLText(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestScrubRequestURL(t *testing.T) {
	scrubbed, truncated := ScrubRequestURL("https://user:pass@example.com:8443/v1/chat?api_key=sk-1&model=gpt-4o#frag")
	if strings.Contains(scrubbed, "sk-1") || strings.Contains(scrubbed, "pass") {
		t.Errorf("request URL leaked credentials: %q", scrubbed)
	}
	if !strings.Contains(scrubbed, "api_key="+RedactedMarker) {
		t.Errorf("query value not redacted: %q", scrubbed)
	}
	if !strings.Contains(scrubbed, "model="+RedactedMarker) {
		t.Errorf("query name must stay but value must be redacted: %q", scrubbed)
	}
	if strings.Contains(scrubbed, "#frag") {
		t.Errorf("fragment not removed: %q", scrubbed)
	}
	if truncated {
		t.Error("short URL must not be truncated")
	}
}

func TestScrubEndpointBaseURL(t *testing.T) {
	scrubbed, _ := ScrubEndpointBaseURL("https://user:pass@example.com/v1?key=abc")
	if strings.Contains(scrubbed, "?") || strings.Contains(scrubbed, "pass") {
		t.Errorf("endpoint base URL must drop query and userinfo: %q", scrubbed)
	}
}

func TestValidErrorCode(t *testing.T) {
	valid := []string{"permission_denied", "upstream_http_403", "stream_missing_terminal_event", "prism_routing_failure", "transport_error", "client_disconnected", "a"}
	invalid := []string{"", " bad", "has space", "невалид", strings.Repeat("x", 121), "-leading-dash", ".leading-dot"}
	for _, code := range valid {
		if !ValidErrorCode(code) {
			t.Errorf("ValidErrorCode(%q) = false, want true", code)
		}
	}
	for _, code := range invalid {
		if ValidErrorCode(code) {
			t.Errorf("ValidErrorCode(%q) = true, want false", code)
		}
	}
}

func TestExtractProviderErrorEnvelopeOpenAI(t *testing.T) {
	sample := []byte(`{"error": {"message": "Incorrect API key provided: sk-abc. You can find your API key at https://platform.openai.com/account/api-keys.", "type": "invalid_request_error", "param": null, "code": "invalid_api_key"}}`)
	extraction := ExtractProviderErrorEnvelope(sample, "application/json")
	if !extraction.Recognized {
		t.Fatal("recognized envelope must be recognized")
	}
	if extraction.Code != "invalid_api_key" {
		t.Errorf("code = %q, want invalid_api_key", extraction.Code)
	}
	if strings.Contains(extraction.Detail, "sk-abc") {
		t.Errorf("credential leaked into detail: %q", extraction.Detail)
	}
	if !extraction.Redacted {
		t.Error("redaction flag must be set")
	}
}

func TestExtractProviderErrorEnvelopeAnthropic(t *testing.T) {
	sample := []byte(`{"type": "error", "error": {"type": "authentication_error", "message": "invalid x-api-key"}}`)
	extraction := ExtractProviderErrorEnvelope(sample, "application/json")
	if !extraction.Recognized || extraction.Code != "authentication_error" {
		t.Errorf("Anthropic envelope code = %q recognized=%v", extraction.Code, extraction.Recognized)
	}
}

func TestExtractProviderErrorEnvelopeGemini(t *testing.T) {
	sample := []byte(`{"error": {"code": 400, "message": "API key not valid. Please pass a valid API key.", "status": "INVALID_ARGUMENT"}}`)
	extraction := ExtractProviderErrorEnvelope(sample, "application/json")
	if !extraction.Recognized {
		t.Fatal("Gemini envelope must be recognized")
	}
	if extraction.Code != "INVALID_ARGUMENT" {
		t.Errorf("code = %q, want INVALID_ARGUMENT", extraction.Code)
	}
}

func TestExtractProviderErrorEnvelopeErrorsArray(t *testing.T) {
	sample := []byte(`{"errors": [{"code": "rate_limit_exceeded", "message": "Too many requests", "reason": "quota"}]}`)
	extraction := ExtractProviderErrorEnvelope(sample, "application/json")
	if !extraction.Recognized || extraction.Code != "rate_limit_exceeded" {
		t.Errorf("errors[] envelope code = %q recognized=%v", extraction.Code, extraction.Recognized)
	}
	if !strings.Contains(extraction.Detail, "Too many requests") {
		t.Errorf("message missing from detail: %q", extraction.Detail)
	}
}

func TestExtractProviderErrorEnvelopePriorityOrder(t *testing.T) {
	sample := []byte(`{"error": {"code": "c", "message": "m", "detail": "d", "reason": "r"}}`)
	extraction := ExtractProviderErrorEnvelope(sample, "application/json")
	if !strings.Contains(extraction.Detail, "m") || !strings.Contains(extraction.Detail, "d") || !strings.Contains(extraction.Detail, "r") {
		t.Errorf("message→detail→reason order broken: %q", extraction.Detail)
	}
	// Dedupe
	if strings.Count(extraction.Detail, "m") != 1 {
		t.Errorf("duplicate message not deduped: %q", extraction.Detail)
	}
}

func TestExtractProviderErrorEnvelopeUnrecognizedFallback(t *testing.T) {
	cases := []struct {
		name        string
		sample      []byte
		contentType string
	}{
		{"plain text", []byte("Service Unavailable"), "text/plain"},
		{"html", []byte("<html><body>denied</body></html>"), "text/html"},
		{"binary", []byte{0x00, 0x01, 0x02, 0xFF}, "application/octet-stream"},
		{"empty", []byte{}, "application/json"},
		{"invalid json", []byte(`{"error": `), "application/json"},
		{"json-like but unrecognized", []byte(`{"foo": {"bar": "baz"}}`), "application/json"},
		{"nested arbitrary payload", []byte(`{"error": {"message": {"nested": "text"}}}`), "application/json"},
	}
	for _, tc := range cases {
		extraction := ExtractProviderErrorEnvelope(tc.sample, tc.contentType)
		if extraction.Recognized {
			t.Errorf("%s: unrecognized content must not be recognized", tc.name)
		}
		if extraction.Code != "" || extraction.Detail != "" {
			t.Errorf("%s: no scalars may be extracted from unrecognized content", tc.name)
		}
	}
}

func TestExtractProviderErrorEnvelopeScrubsNested(t *testing.T) {
	sample := []byte(`{"error": {"message": "failed with api_key=sk-secret123 and token eyJh.e30.e30", "code": "x"}}`)
	extraction := ExtractProviderErrorEnvelope(sample, "application/json")
	if strings.Contains(extraction.Detail, "sk-secret123") || strings.Contains(extraction.Detail, "eyJh") {
		t.Errorf("nested credentials leaked: %q", extraction.Detail)
	}
	if !extraction.Redacted {
		t.Error("nested credential redaction flag missing")
	}
}

func TestExtractProviderErrorEnvelopeUTF8Cap(t *testing.T) {
	longMessage := strings.Repeat("界", 5000)
	sample := []byte(`{"error": {"message": "` + longMessage + `", "code": "x"}}`)
	extraction := ExtractProviderErrorEnvelope(sample, "application/json")
	if len(extraction.Detail) > MaxErrorDetailBytes {
		t.Errorf("detail exceeds 4 KiB: %d", len(extraction.Detail))
	}
	if !extraction.Truncated {
		t.Error("truncation flag must be set")
	}
}

func TestAdoptProviderCode(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"invalid_api_key", "invalid_api_key"},
		{"rate.limit:exceeded-1", "rate.limit:exceeded-1"},
		{" bad code ", ""},
		{"sk-secret-123", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := AdoptProviderCode(tc.input); got != tc.want {
			t.Errorf("AdoptProviderCode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestMetadataFieldEnum(t *testing.T) {
	// Fixed 13-field enum, sorted ordinals, canonical names.
	expected := []string{
		"caller_request_id",
		"provider_correlation_id",
		"caller_user_agent",
		"upstream_user_agent",
		"provider_request_id",
		"request_url",
		"endpoint_base_url",
		"requested_model_label",
		"resolved_model_label",
		"operation_name",
		"request_path",
		"terminal_target_label",
		"endpoint_label",
	}
	if MetadataFieldCount != len(expected) {
		t.Fatalf("MetadataFieldCount = %d, want %d", MetadataFieldCount, len(expected))
	}
	for ordinal, name := range expected {
		if MetadataFieldName(MetadataField(ordinal)) != name {
			t.Errorf("ordinal %d name = %q, want %q", ordinal, MetadataFieldName(MetadataField(ordinal)), name)
		}
		if _, ok := ParseMetadataFieldName(name); !ok {
			t.Errorf("ParseMetadataFieldName(%q) failed", name)
		}
	}
	if _, ok := ParseMetadataFieldName("other"); ok {
		t.Error("unknown field must not parse")
	}
}

func TestMetadataProvenanceCanonicalOrder(t *testing.T) {
	provenance := MetadataProvenance{}
	provenance.Record(MetadataFieldEndpointLabel, true, false)
	provenance.Record(MetadataFieldCallerRequestID, false, true)
	provenance.Record(MetadataFieldCallerRequestID, true, false)
	provenance.Record(MetadataFieldCallerRequestID, true, false) // dedupe
	redacted := CanonicalFieldNames(provenance.Redacted)
	if len(redacted) != 2 || redacted[0] != "caller_request_id" || redacted[1] != "endpoint_label" {
		t.Errorf("redacted = %v, want [caller_request_id endpoint_label]", redacted)
	}
	truncated := CanonicalFieldNames(provenance.Truncated)
	if len(truncated) != 1 || truncated[0] != "caller_request_id" {
		t.Errorf("truncated = %v, want [caller_request_id]", truncated)
	}
	provenance.Record(MetadataFieldTerminalTargetLabel, true, false)
	provenance.Record(MetadataFieldEndpointLabel, true, false)
	all := CanonicalFieldNames(provenance.Redacted)
	if len(all) != 3 || all[0] != "caller_request_id" || all[1] != "terminal_target_label" || all[2] != "endpoint_label" {
		t.Errorf("canonical order broken: %v", all)
	}
}

func TestScrubMetadataValueCaps(t *testing.T) {
	// Correlation ID must be <= 255 code points and <= 1 KiB.
	long := strings.Repeat("a", 300)
	result := ScrubMetadataValue(MetadataFieldCallerRequestID, long, 255)
	if len([]rune(result.Value)) > 255 {
		t.Errorf("correlation id exceeds 255 code points: %d", len([]rune(result.Value)))
	}
	if !result.Truncated {
		t.Error("truncation flag must be set")
	}
	// Physical column capacity is the minimum.
	result = ScrubMetadataValue(MetadataFieldOperationName, "abcdef", 3)
	if result.Value != "abc" {
		t.Errorf("physical column cap not applied: %q", result.Value)
	}
}

func TestTruncateUTF8Boundary(t *testing.T) {
	input := "a" + strings.Repeat("界", 100)
	safe, truncated := TruncateUTF8(input, 7)
	if !truncated {
		t.Fatal("expected truncation")
	}
	// 7 bytes: 'a'(1) + two full 3-byte runes = 7
	if safe != "a界界" {
		t.Errorf("safe = %q, want a界界", safe)
	}
}

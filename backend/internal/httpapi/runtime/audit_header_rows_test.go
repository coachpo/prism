package runtime

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

func TestAuditHeaderRowsApplyEffectiveBlocklist(t *testing.T) {
	rules := []safediag.SensitiveNameRule{
		{MatchType: "exact", Pattern: "x-forwarded-for"},
		{MatchType: "prefix", Pattern: "cf-"},
	}

	t.Run("exact rule masks request value and retains name", func(t *testing.T) {
		const original = "198.51.100.42"
		encoded := marshalAuditHeaders(map[string]string{
			"X-Forwarded-For": original,
		}, rules)

		entries := decodeAuditHeaderEntries(t, encoded)
		if !reflect.DeepEqual(entries, []auditHeaderEntry{{Name: "x-forwarded-for", Value: safediag.RedactedMarker}}) {
			t.Fatalf("unexpected entries: %+v", entries)
		}
		if strings.Contains(encoded, original) {
			t.Fatalf("serialized headers contain the blocklisted value %q: %s", original, encoded)
		}
	})

	t.Run("prefix rule matches cf headers but not xcf headers", func(t *testing.T) {
		encoded := marshalAuditHeaders(map[string]string{
			"cf-connecting-ip":  "198.51.100.43",
			"xcf-connecting-ip": "kept",
		}, rules)

		entries := decodeAuditHeaderEntries(t, encoded)
		want := []auditHeaderEntry{
			{Name: "cf-connecting-ip", Value: safediag.RedactedMarker},
			{Name: "xcf-connecting-ip", Value: "kept"},
		}
		if !reflect.DeepEqual(entries, want) {
			t.Fatalf("unexpected prefix results: got %+v want %+v", entries, want)
		}
	})

	t.Run("unrelated extra rules never weaken authorization masking", func(t *testing.T) {
		const original = "Bearer sk-live-audit-test-key"
		encoded := marshalAuditHeaders(map[string]string{
			"Authorization": original,
		}, []safediag.SensitiveNameRule{{MatchType: "exact", Pattern: "x-request-id"}})

		entries := decodeAuditHeaderEntries(t, encoded)
		if len(entries) != 1 || entries[0].Name != "authorization" || entries[0].Value != safediag.RedactedMarker {
			t.Fatalf("authorization was not fixed-rule redacted: %+v", entries)
		}
		if strings.Contains(encoded, original) {
			t.Fatalf("serialized headers contain authorization value %q: %s", original, encoded)
		}
	})

	t.Run("response headers use the same request-time matcher", func(t *testing.T) {
		headers := http.Header{}
		headers.Add("CF-Connecting-IP", "198.51.100.44")
		headers.Add("XCF-Connecting-IP", "visible")
		headers.Add("Set-Cookie", "session=live-cookie")

		encoded := marshalAuditHTTPHeaders(headers, rules)
		if encoded == nil {
			t.Fatal("expected response headers")
		}
		entries := decodeAuditHeaderEntries(t, *encoded)
		want := []auditHeaderEntry{
			{Name: "cf-connecting-ip", Value: safediag.RedactedMarker},
			{Name: "set-cookie", Value: safediag.RedactedMarker},
			{Name: "xcf-connecting-ip", Value: "visible"},
		}
		if !reflect.DeepEqual(entries, want) {
			t.Fatalf("unexpected response entries: got %+v want %+v", entries, want)
		}
		if strings.Contains(*encoded, "198.51.100.44") || strings.Contains(*encoded, "session=live-cookie") {
			t.Fatalf("response serialization contains a redacted value: %s", *encoded)
		}
	})
}

func TestBuildRuntimeAuditLogRowUsesPlanBlocklistForBothDirections(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://prism.test/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	plan := requestPlan{
		ProfileID: 1,
		BlocklistRules: []headerBlocklistRule{
			{MatchType: "exact", Pattern: "x-forwarded-for"},
			{MatchType: "prefix", Pattern: "cf-"},
		},
	}
	attempt := executionAttempt{
		Connection: runtimeConnection{
			ID:       21,
			Endpoint: runtimeEndpoint{ID: 22, BaseURL: "https://upstream.test"},
		},
		RequestURL:                  request.URL.String(),
		RequestHeaders:              map[string]string{"X-Forwarded-For": "198.51.100.45", "X-Visible": "safe"},
		ResponseHeaders:             http.Header{"Cf-Connecting-Ip": []string{"198.51.100.46"}, "Xcf-Connecting-Ip": []string{"safe-response"}},
		StatusCode:                  http.StatusOK,
		ResponseHeadersReceived:     true,
		AuditEnabledAtRequest:       true,
		AuditCaptureBodiesAtRequest: false,
	}
	telemetry := runtimeTelemetryEnvelopeContext{
		runtimeTelemetryPricingTimingContext: runtimeTelemetryPricingTimingContext{requestCompletedAt: time.Unix(0, 0).UTC()},
		attempts:                             []executionAttempt{attempt},
	}

	rows := buildRuntimeAuditLogRows(plan, request, telemetry)
	if len(rows) != 1 {
		t.Fatalf("expected one audit row, got %d", len(rows))
	}
	requestEntries := decodeAuditHeaderEntries(t, rows[0].RequestHeaders)
	responseEntries := decodeAuditHeaderEntries(t, *rows[0].ResponseHeaders)
	if !reflect.DeepEqual(requestEntries, []auditHeaderEntry{
		{Name: "x-forwarded-for", Value: safediag.RedactedMarker},
		{Name: "x-visible", Value: "safe"},
	}) {
		t.Fatalf("plan blocklist was not applied to request headers: %+v", requestEntries)
	}
	if !reflect.DeepEqual(responseEntries, []auditHeaderEntry{
		{Name: "cf-connecting-ip", Value: safediag.RedactedMarker},
		{Name: "xcf-connecting-ip", Value: "safe-response"},
	}) {
		t.Fatalf("plan blocklist was not applied to response headers: %+v", responseEntries)
	}
}

func TestAuditHeaderRowsKeepEmptyRuleBehavior(t *testing.T) {
	requestEncoded := marshalAuditHeaders(map[string]string{
		"Accept":        "application/json",
		"Authorization": "Bearer fixed-rule-secret",
	}, nil)
	if want := `[{"name":"accept","value":"application/json"},{"name":"authorization","value":"[REDACTED]"}]`; requestEncoded != want {
		t.Fatalf("empty-rule request output changed: got %s want %s", requestEncoded, want)
	}

	responseHeaders := http.Header{}
	responseHeaders.Add("X-Duplicate", "second")
	responseHeaders.Add("X-Duplicate", "first")
	responseEncoded := marshalAuditHTTPHeaders(responseHeaders, nil)
	if responseEncoded == nil {
		t.Fatal("expected non-nil response output")
	}
	if want := `[{"name":"x-duplicate","value":"first"},{"name":"x-duplicate","value":"second"}]`; *responseEncoded != want {
		t.Fatalf("empty-rule response output changed: got %s want %s", *responseEncoded, want)
	}
	if got := marshalAuditHeaders(nil, nil); got != "[]" {
		t.Fatalf("empty request map should remain []: got %s", got)
	}
	if got := marshalAuditHTTPHeaders(nil, nil); got != nil {
		t.Fatalf("empty response headers should remain nil: got %q", *got)
	}
}

func TestAuditHeaderRowsGoldenFixture(t *testing.T) {
	want := auditHeaderGoldenFixtureValue(t)
	path := auditHeaderGoldenFixturePath(t)
	if os.Getenv("PRISM_UPDATE_AUDIT_HEADER_GOLDEN") == "1" {
		encoded, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			t.Fatalf("marshal golden fixture: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden fixture directory: %v", err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
			t.Fatalf("write golden fixture: %v", err)
		}
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture %s: %v", path, err)
	}
	var got auditHeaderGoldenFixture
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("golden fixture is stale:\n got: %s\nwant: %s", contents, mustMarshalJSON(t, want))
	}
}

// auditHeaderGoldenFixture is the cross-language contract sample. Source is the
// pre-scrub input; Serialized is byte-for-byte what this package writes to the
// audit row, so it is also exactly what the frontend parser receives.
type auditHeaderGoldenFixture struct {
	RequestSource      string `json:"request_source"`
	RequestSerialized  string `json:"request_serialized"`
	ResponseSource     string `json:"response_source"`
	ResponseSerialized string `json:"response_serialized"`
}

func auditHeaderGoldenFixtureValue(t *testing.T) auditHeaderGoldenFixture {
	t.Helper()
	rules := []safediag.SensitiveNameRule{
		{MatchType: "exact", Pattern: "x-forwarded-for"},
		{MatchType: "prefix", Pattern: "cf-"},
	}
	requestInputEntries := []auditHeaderEntry{
		{Name: "Authorization", Value: "Bearer sk-live-golden-request-key"},
		{Name: "X-Forwarded-For", Value: "198.51.100.42"},
		{Name: "Set-Cookie", Value: "session=live-golden-cookie"},
		{Name: "X-Visible", Value: "visible"},
	}
	requestHeaders := make(map[string]string, len(requestInputEntries))
	for _, entry := range requestInputEntries {
		requestHeaders[entry.Name] = entry.Value
	}
	responseInputEntries := []auditHeaderEntry{
		{Name: "Set-Cookie", Value: "live-response-cookie-1"},
		{Name: "Set-Cookie", Value: "live-response-cookie-2"},
		{Name: "CF-Connecting-IP", Value: "198.51.100.43"},
		{Name: "XCF-Connecting-IP", Value: "visible-xcf"},
		{Name: "Access-Control-Allow-Credentials", Value: "true"},
	}
	responseHeaders := make(http.Header)
	for _, entry := range responseInputEntries {
		responseHeaders.Add(entry.Name, entry.Value)
	}

	return auditHeaderGoldenFixture{
		RequestSource:      mustMarshalJSON(t, requestInputEntries),
		RequestSerialized:  marshalAuditHeaders(requestHeaders, rules),
		ResponseSource:     mustMarshalJSON(t, responseInputEntries),
		ResponseSerialized: auditHeaderString(marshalAuditHTTPHeaders(responseHeaders, rules)),
	}
}

func auditHeaderGoldenFixturePath(t *testing.T) string {
	t.Helper()
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate audit header rows test")
	}
	return filepath.Join(filepath.Dir(sourcePath), "testdata", "audit_header_rows.golden.json")
}

func decodeAuditHeaderEntries(t *testing.T, encoded string) []auditHeaderEntry {
	t.Helper()
	var entries []auditHeaderEntry
	if err := json.Unmarshal([]byte(encoded), &entries); err != nil {
		t.Fatalf("decode audit headers %s: %v", encoded, err)
	}
	return entries
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON fixture: %v", err)
	}
	return string(encoded)
}

func auditHeaderString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

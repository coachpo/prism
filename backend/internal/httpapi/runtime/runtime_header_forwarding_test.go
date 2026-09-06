package runtime

import (
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHeaderHelpers(t *testing.T) {
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

func TestNewRuntimeHTTPClientAppliesNoTransportLimits(t *testing.T) {
	client := newRuntimeHTTPClient()
	if client.Timeout != 0 {
		t.Fatalf("expected no client timeout, got %v", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected runtime HTTP transport, got %T", client.Transport)
	}
	if !transport.DisableCompression {
		t.Fatal("expected DisableCompression to stay enabled")
	}
	if transport.MaxIdleConnsPerHost != math.MaxInt32 {
		t.Fatalf("expected explicit unlimited idle connections per host, got %d", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxIdleConns != 0 || transport.MaxConnsPerHost != 0 || transport.IdleConnTimeout != 0 || transport.ResponseHeaderTimeout != 0 || transport.TLSHandshakeTimeout != 0 || transport.ExpectContinueTimeout != 0 {
		t.Fatalf("expected no connection or timeout limits, got %+v", transport)
	}
}

func TestRuntimeProxyConfigProviderUpdatesNewPlansAndKeepsExistingPlanClient(t *testing.T) {
	oldClient := &http.Client{Timeout: 17 * time.Second}
	newClient := &http.Client{Timeout: 23 * time.Second}
	provider := &mutableRuntimeProxyConfigProvider{snapshot: RuntimeProxyConfigSnapshot{HTTPClient: oldClient}}
	service := &Service{runtimeProxyConfigProvider: provider}

	oldSnapshot := service.runtimeProxyConfigSnapshot()
	oldPlan := requestPlan{HTTPClient: oldSnapshot.HTTPClient}
	provider.snapshot = RuntimeProxyConfigSnapshot{HTTPClient: newClient}
	newSnapshot := service.runtimeProxyConfigSnapshot()
	newPlan := requestPlan{HTTPClient: newSnapshot.HTTPClient}

	if oldPlan.HTTPClient != oldClient || oldPlan.HTTPClient.Timeout != 17*time.Second {
		t.Fatalf("expected existing plan to keep old client snapshot, got %+v", oldPlan.HTTPClient)
	}
	if newPlan.HTTPClient != newClient || newPlan.HTTPClient.Timeout != 23*time.Second {
		t.Fatalf("expected new plan to use updated client snapshot, got %+v", newPlan.HTTPClient)
	}
}

func TestRuntimeProxyConfigProviderDoesNotRequireBufferingMode(t *testing.T) {
	client := &http.Client{Timeout: 19 * time.Second}
	provider := &mutableRuntimeProxyConfigProvider{snapshot: RuntimeProxyConfigSnapshot{HTTPClient: client}}
	service := &Service{runtimeProxyConfigProvider: provider}

	snapshot := service.runtimeProxyConfigSnapshot()
	if snapshot.HTTPClient != client {
		t.Fatalf("expected runtime proxy snapshot to carry HTTP client only, got %+v", snapshot.HTTPClient)
	}

	snapshotType := reflect.TypeOf(snapshot)
	for _, name := range []string{"BufferingMode", "bufferingMode"} {
		if _, ok := snapshotType.FieldByName(name); ok {
			t.Fatalf("%s still exposes %s", snapshotType.Name(), name)
		}
		if _, ok := snapshotType.MethodByName(name); ok {
			t.Fatalf("%s still exposes %s()", snapshotType.Name(), name)
		}
	}
}

type mutableRuntimeProxyConfigProvider struct {
	snapshot RuntimeProxyConfigSnapshot
}

func (p *mutableRuntimeProxyConfigProvider) RuntimeProxyConfigSnapshot() RuntimeProxyConfigSnapshot {
	return p.snapshot
}

func TestForwardableClientHeadersForwardsCallerHeadersVerbatim(t *testing.T) {
	proxyControlled := map[string]struct{}{"x-api-key": {}, "anthropic-version": {}}
	forwarded := forwardableClientHeaders(map[string]string{
		"User-Agent":         "opencode/1.4.2 (darwin; arm64)",
		"X-Opencode-Session": "ses_01JQ8Z",
		"Accept":             "text/event-stream",
		"Cookie":             "sid=abc",
		"Traceparent":        "00-1234-5678-01",
	}, proxyControlled)

	// Cookie and Traceparent clear this stage because it only withholds headers
	// that cannot survive a hop. Whether they actually reach an upstream is the
	// Header Blocklist's call downstream, and the seeded system rules stop both
	// — see TestHeaderBlocklistNowGovernsForwarding.
	for name, want := range map[string]string{
		"User-Agent":         "opencode/1.4.2 (darwin; arm64)",
		"X-Opencode-Session": "ses_01JQ8Z",
		"Accept":             "text/event-stream",
		"Cookie":             "sid=abc",
		"Traceparent":        "00-1234-5678-01",
	} {
		if forwarded[name] != want {
			t.Fatalf("expected %s to be forwarded verbatim as %q, got %q", name, want, forwarded[name])
		}
	}
}

func TestForwardableClientHeadersWithholdsHeadersThatCannotCrossAHop(t *testing.T) {
	proxyControlled := map[string]struct{}{"x-api-key": {}, "anthropic-version": {}}
	forwarded := forwardableClientHeaders(map[string]string{
		"Host":              "prism.internal",
		"Connection":        "keep-alive",
		"Transfer-Encoding": "chunked",
		"Accept-Encoding":   "gzip",
		"Content-Length":    "512",
		"Authorization":     "Bearer caller-secret",
		"X-Api-Key":         "caller-anthropic-key",
		"X-Goog-Api-Key":    "caller-gemini-key",
		"Anthropic-Version": "2023-01-01",
		"User-Agent":        "opencode/1.4.2",
	}, proxyControlled)

	if !reflect.DeepEqual(forwarded, map[string]string{"User-Agent": "opencode/1.4.2"}) {
		t.Fatalf("expected only the caller User-Agent to survive, got %v", forwarded)
	}
}

func TestForwardableClientHeadersRejectsHeaderInjectionValues(t *testing.T) {
	forwarded := forwardableClientHeaders(map[string]string{
		"X-Opencode-Session": "ses_01\r\nX-Injected: 1",
		"X-Clean":            "  kept  ",
	}, map[string]struct{}{})

	if _, present := forwarded["X-Opencode-Session"]; present {
		t.Fatalf("expected control characters to drop the header, got %v", forwarded)
	}
	if forwarded["X-Clean"] != "kept" {
		t.Fatalf("expected surrounding whitespace to be trimmed, got %q", forwarded["X-Clean"])
	}
}

func TestHeaderBlocklistNowGovernsForwarding(t *testing.T) {
	// These three mirror seeded system rules: "cookie", "traceparent", and the
	// "x-datadog-" prefix.
	rules := []headerBlocklistRule{
		{MatchType: "exact", Pattern: "cookie"},
		{MatchType: "exact", Pattern: "traceparent"},
		{MatchType: "prefix", Pattern: "x-datadog-"},
	}
	forwarded := forwardableClientHeaders(map[string]string{
		"User-Agent":         "opencode/1.4.2",
		"Cookie":             "sid=abc",
		"Traceparent":        "00-1234-5678-01",
		"X-Datadog-Trace-Id": "42",
		"X-Opencode-Session": "ses_01JQ8Z",
	}, map[string]struct{}{})

	sanitized := sanitizeHeaders(forwarded, rules)
	if _, present := sanitized["Cookie"]; present {
		t.Fatalf("expected an exact blocklist rule to stop forwarding, got %v", sanitized)
	}
	if _, present := sanitized["Traceparent"]; present {
		t.Fatalf("expected the traceparent rule to stop forwarding, got %v", sanitized)
	}
	if _, present := sanitized["X-Datadog-Trace-Id"]; present {
		t.Fatalf("expected a prefix blocklist rule to stop forwarding, got %v", sanitized)
	}
	if sanitized["User-Agent"] != "opencode/1.4.2" || sanitized["X-Opencode-Session"] != "ses_01JQ8Z" {
		t.Fatalf("expected unblocked headers to survive, got %v", sanitized)
	}
}

// A custom header and a forwarded header can name the same header under
// different map keys ("user-agent" vs "User-Agent"). Both reaching
// doUpstreamRequest would let Header.Set resolve them in random map order, so
// buildUpstreamHeaders must leave only the declared one.
func TestBuildUpstreamHeadersCustomHeaderOverridesForwardedCaseVariant(t *testing.T) {
	service := &Service{}
	connection := runtimeConnection{
		ID: 1,
		UpstreamAuth: &runtimeConnectionUpstreamAuthSnapshot{
			AuthHeader:            "x-api-key",
			AuthValue:             "upstream-key",
			ExtraHeaders:          map[string]string{"anthropic-version": "2023-06-01"},
			ControlledHeaderNames: map[string]struct{}{"x-api-key": {}, "anthropic-version": {}},
		},
		CustomHeaders: map[string]any{"user-agent": "declared-by-connection/1.0"},
	}
	clientHeaders := map[string]string{"User-Agent": "opencode/1.4.2", "Accept": "text/event-stream"}

	for attempt := range 64 {
		headers, err := service.buildUpstreamHeaders(connection, "anthropic", clientHeaders, nil, false)
		if err != nil {
			t.Fatalf("attempt %d: buildUpstreamHeaders: %v", attempt, err)
		}
		seen := make([]string, 0, 2)
		for key, value := range headers {
			if strings.EqualFold(key, "user-agent") {
				seen = append(seen, key+"="+value)
			}
		}
		if len(seen) != 1 || seen[0] != "user-agent=declared-by-connection/1.0" {
			t.Fatalf("attempt %d: expected exactly the declared user-agent, got %v", attempt, seen)
		}
		if headers["Accept"] != "text/event-stream" {
			t.Fatalf("attempt %d: expected unrelated forwarded header to survive, got %q", attempt, headers["Accept"])
		}
	}
}

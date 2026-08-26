package runtime

import (
	"math"
	"net/http"
	"reflect"
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

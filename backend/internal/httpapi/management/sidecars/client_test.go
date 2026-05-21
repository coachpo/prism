package sidecars

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	urlpkg "net/url"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCLIProxyBaseURLSecurityPolicy(t *testing.T) {
	t.Parallel()
	allowed, err := NormalizeCLIProxyBaseURL("http://127.0.0.1:9090/v0/management/", CLIProxyConnectionPolicy{AllowPrivateNetwork: true, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatalf("normalize allowed localhost URL: %v", err)
	}
	if allowed != "http://127.0.0.1:9090" {
		t.Fatalf("expected management prefix to normalize away, got %q", allowed)
	}

	for _, raw := range []string{"http://169.254.169.254:80", "http://0.0.0.0:9090", "http://224.0.0.1:9090"} {
		t.Run("unsafe literal "+raw, func(t *testing.T) {
			_, err := NormalizeCLIProxyBaseURL(raw, CLIProxyConnectionPolicy{AllowPrivateNetwork: true, AllowInsecureHTTP: true})
			assertCLIProxyErrorCode(t, err, CLIProxyErrorPrivateNetworkBlocked)
		})
	}

	tests := []struct {
		name string
		raw  string
		want CLIProxyErrorCode
	}{
		{name: "empty host", raw: "https:///v0/management", want: CLIProxyErrorInvalidBaseURL},
		{name: "userinfo", raw: "https://user:pass@example.com", want: CLIProxyErrorInvalidBaseURL},
		{name: "fragment", raw: "https://example.com/#secret", want: CLIProxyErrorInvalidBaseURL},
		{name: "non ascii host", raw: "https://bücher.example", want: CLIProxyErrorInvalidBaseURL},
		{name: "unsupported scheme", raw: "ftp://example.com", want: CLIProxyErrorInvalidBaseURL},
		{name: "insecure http blocked", raw: "http://example.com", want: CLIProxyErrorInsecureHTTPBlocked},
		{name: "private network blocked", raw: "https://127.0.0.1:8443", want: CLIProxyErrorPrivateNetworkBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCLIProxyBaseURL(tt.raw, CLIProxyConnectionPolicy{})
			assertCLIProxyErrorCode(t, err, tt.want)
		})
	}
}

func TestCLIProxyClientRetryNoRedirectAndAllowlist(t *testing.T) {
	t.Parallel()
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/auth-files" {
			t.Fatalf("unexpected management path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Management-Key"); got != "secret-key" {
			t.Fatalf("expected X-Management-Key header, got %q", got)
		}
		requestCount++
		if requestCount == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}
		w.Header().Set("X-CPA-COMMIT", "21FAD9DBB447A2AB70D51D0AC3E3D032525A6054")
		w.Header().Set("X-CPA-VERSION", "test-version")
		w.Header().Set("X-CPA-BUILD-DATE", "2026-05-21T00:00:00Z")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewCLIProxyClient(server.Client())
	var payload map[string]string
	started := time.Now()
	response, err := client.FetchJSON(context.Background(), CLIProxyTarget{BaseURL: server.URL, ManagementPassword: "secret-key", AllowPrivateNetwork: true, AllowInsecureHTTP: true}, http.MethodGet, "/auth-files", nil, &payload)
	if err != nil {
		t.Fatalf("expected retry to recover from transient 5xx: %v", err)
	}
	if requestCount != 2 || payload["status"] != "ok" {
		t.Fatalf("expected one retry and successful payload, count=%d payload=%v", requestCount, payload)
	}
	if response.Commit != cliProxyAuthFileDeleteBaselineCommit || response.Version != "test-version" || response.BuildDate == "" {
		t.Fatalf("expected CPA response metadata, got %+v", response)
	}
	if elapsed := time.Since(started); elapsed < 250*time.Millisecond {
		t.Fatalf("expected retry backoff of at least 250ms, got %s", elapsed)
	}

	unauthorizedHits := 0
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		unauthorizedHits++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer unauthorized.Close()
	_, err = client.FetchJSON(context.Background(), CLIProxyTarget{BaseURL: unauthorized.URL, ManagementPassword: "wrong", AllowPrivateNetwork: true, AllowInsecureHTTP: true}, http.MethodGet, "/auth-files", nil, &payload)
	assertCLIProxyErrorCode(t, err, CLIProxyErrorInvalidManagementAuth)
	if unauthorizedHits != 1 {
		t.Fatalf("401/403 must not be retried, hits=%d", unauthorizedHits)
	}

	redirectHits := 0
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/evil") {
			redirectHits++
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/evil", http.StatusFound)
	}))
	defer redirected.Close()
	_, err = client.FetchJSON(context.Background(), CLIProxyTarget{BaseURL: redirected.URL, AllowPrivateNetwork: true, AllowInsecureHTTP: true}, http.MethodGet, "/auth-files", nil, &payload)
	assertCLIProxyErrorCode(t, err, CLIProxyErrorUpstreamStatus)
	if redirectHits != 0 {
		t.Fatalf("client must not follow redirects, followed hits=%d", redirectHits)
	}

	_, err = client.FetchJSON(context.Background(), CLIProxyTarget{BaseURL: server.URL, AllowPrivateNetwork: true, AllowInsecureHTTP: true}, http.MethodGet, "/usage-queue", nil, &payload)
	assertCLIProxyErrorCode(t, err, CLIProxyErrorUnsupportedPath)
}

func TestCLIProxyManagementPathUnsupportedPathsStillFail(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/usage-queue", "/auth-files?url=https://example.com"} {
		t.Run(path, func(t *testing.T) {
			_, err := normalizeCLIProxyManagementPath(path)
			assertCLIProxyErrorCode(t, err, CLIProxyErrorUnsupportedPath)
		})
	}
}

func TestCLIProxyClientResponseBodyCap(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"oversized"}`))
	}))
	defer server.Close()

	client := NewCLIProxyClient(server.Client())
	client.bodyLimitBytes = 4
	var payload map[string]any
	_, err := client.FetchJSON(context.Background(), CLIProxyTarget{BaseURL: server.URL, AllowPrivateNetwork: true, AllowInsecureHTTP: true}, http.MethodGet, "/auth-files", nil, &payload)
	assertCLIProxyErrorCode(t, err, CLIProxyErrorOversizedBody)
}

func TestCLIProxyClientResolvedNetworkPolicyPinsValidatedAddresses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "127.0.0.1" || strings.HasPrefix(r.Host, "127.0.0.1:") {
			t.Fatalf("request should preserve the original sidecar host header, got %q", r.Host)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	serverURL, err := urlpkg.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	client := NewCLIProxyClient(nil)
	client.resolver = staticSidecarResolver{"sidecar.internal": {{IP: net.ParseIP("127.0.0.1")}}}
	var payload map[string]string
	_, err = client.FetchJSON(context.Background(), CLIProxyTarget{BaseURL: "http://sidecar.internal:" + serverURL.Port(), AllowPrivateNetwork: true, AllowInsecureHTTP: true}, http.MethodGet, "/auth-files", nil, &payload)
	if err != nil {
		t.Fatalf("expected fetch through the validated pinned address to succeed: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestCLIProxyClientResolvedNetworkPolicyRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		addresses []net.IPAddr
	}{
		{name: "metadata link-local", addresses: []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}},
		{name: "unspecified", addresses: []net.IPAddr{{IP: net.ParseIP("0.0.0.0")}}},
		{name: "multicast", addresses: []net.IPAddr{{IP: net.ParseIP("224.0.0.1")}}},
		{name: "reserved test-net ipv4", addresses: []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}},
		{name: "reserved test-net ipv6", addresses: []net.IPAddr{{IP: net.ParseIP("2001:db8::1")}}},
		{name: "ipv6 link-local", addresses: []net.IPAddr{{IP: net.ParseIP("fe80::1")}}},
		{name: "ipv6 unspecified", addresses: []net.IPAddr{{IP: net.ParseIP("::")}}},
		{name: "ipv6 multicast", addresses: []net.IPAddr{{IP: net.ParseIP("ff02::1")}}},
		{name: "mixed safe and unsafe answers", addresses: []net.IPAddr{{IP: net.ParseIP("198.51.100.10")}, {IP: net.ParseIP("169.254.169.254")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewCLIProxyClient(nil)
			client.resolver = staticSidecarResolver{"unsafe.internal": tt.addresses}
			var payload map[string]any
			_, err := client.FetchJSON(context.Background(), CLIProxyTarget{BaseURL: "http://unsafe.internal:9090", AllowPrivateNetwork: true, AllowInsecureHTTP: true}, http.MethodGet, "/auth-files", nil, &payload)
			assertCLIProxyErrorCode(t, err, CLIProxyErrorPrivateNetworkBlocked)
		})
	}
}

func TestCLIProxyTransportFailsClosedAndClearsDialTLS(t *testing.T) {
	t.Parallel()
	dialTLSCalled := false
	transport, ok := transportForTarget(CLIProxyTarget{}, &http.Transport{DialTLS: func(network string, address string) (net.Conn, error) {
		dialTLSCalled = true
		return nil, errors.New("unexpected DialTLS call")
	}}, resolvedNetworkTarget{host: "validated.internal", port: "80", addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}).(*http.Transport)
	if !ok {
		t.Fatalf("expected cloned http transport")
	}
	if transport.DialTLS != nil || transport.DialTLSContext != nil {
		t.Fatalf("expected sidecar transport to clear DialTLS hooks")
	}
	if _, err := transport.DialContext(context.Background(), "tcp", "other.internal:80"); err == nil || !strings.Contains(err.Error(), "did not match validated host") {
		t.Fatalf("expected fail-closed host mismatch, got %v", err)
	}
	if dialTLSCalled {
		t.Fatal("deprecated DialTLS hook was invoked")
	}
}

type staticSidecarResolver map[string][]net.IPAddr

func (r staticSidecarResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addresses, ok := r[strings.ToLower(host)]
	if !ok {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

func assertCLIProxyErrorCode(t *testing.T, err error, want CLIProxyErrorCode) {
	t.Helper()
	var clientErr *CLIProxyClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("expected CLIProxyClientError %s, got %T %v", want, err, err)
	}
	if clientErr.Code != want {
		t.Fatalf("expected error code %s, got %s (%v)", want, clientErr.Code, err)
	}
}

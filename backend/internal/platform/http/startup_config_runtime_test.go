package platformhttp

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestStartupConfigRuntimeInitializesSnapshotsFromSettings(t *testing.T) {
	t.Parallel()

	settings := startupConfigRuntimeTestSettings()
	runtime, err := NewStartupConfigRuntime(settings)
	if err != nil {
		t.Fatalf("create startup config runtime: %v", err)
	}

	snapshot := runtime.Snapshot()
	if got := snapshot.CORS().AllowedOrigins(); !reflect.DeepEqual(got, []string{"http://localhost:5173", "http://127.0.0.1:5173"}) {
		t.Fatalf("unexpected CORS origins: %v", got)
	}
	if !snapshot.CORS().AllowsOrigin(" http://127.0.0.1:5173 ") {
		t.Fatal("expected trimmed CORS origin to be allowed")
	}
	auth := snapshot.Auth()
	if auth.AccessTokenTTL != 17*time.Second || auth.RefreshTokenTTL != 29*time.Second {
		t.Fatalf("unexpected auth TTLs: %+v", auth)
	}
	if auth.AccessCookieName != "access_cookie" || auth.RefreshCookieName != "refresh_cookie" || !auth.CookieSecure {
		t.Fatalf("unexpected auth cookies: %+v", auth)
	}
	proxy := snapshot.RuntimeProxy()
	transportConfig := proxy.TransportConfig()
	if transportConfig.RequestTimeout != 17*time.Second || transportConfig.MaxIdleConns != 25 || transportConfig.MaxIdleConnsPerHost != 5 || transportConfig.MaxConnsPerHost != 9 {
		t.Fatalf("unexpected runtime transport config: %+v", transportConfig)
	}
	client := proxy.HTTPClient()
	if client == nil || client.Timeout != 17*time.Second {
		t.Fatalf("unexpected runtime HTTP client: %+v", client)
	}
	transport := unwrapStartupRuntimeTransport(t, client.Transport)
	if !transport.DisableCompression || transport.ResponseHeaderTimeout != 7*time.Second || transport.TLSHandshakeTimeout != 11*time.Second {
		t.Fatalf("unexpected runtime transport: %+v", transport)
	}
	limits := snapshot.Admission().Limits()
	if limits.ManagementM1 != 4 || limits.ManagementM2 != 3 || limits.ManagementM3 != 2 {
		t.Fatalf("unexpected admission limits: %+v", limits)
	}
	if snapshot.Admission().Controller() == nil {
		t.Fatal("expected admission controller seam")
	}
}

func TestStartupRuntimeSnapshotOmitsBufferingMode(t *testing.T) {
	t.Parallel()

	assertSnapshotOmitsRuntimeBufferingMode(t, reflect.TypeFor[StartupRuntimeProxySnapshot]())
	assertSnapshotOmitsRuntimeBufferingMode(t, reflect.TypeFor[runtimeapi.RuntimeProxyConfigSnapshot]())
}

func assertSnapshotOmitsRuntimeBufferingMode(t *testing.T, snapshotType reflect.Type) {
	t.Helper()

	for _, name := range []string{"BufferingMode", "bufferingMode"} {
		if _, ok := snapshotType.FieldByName(name); ok {
			t.Fatalf("%s still exposes %s", snapshotType.Name(), name)
		}
		if _, ok := snapshotType.MethodByName(name); ok {
			t.Fatalf("%s still exposes %s()", snapshotType.Name(), name)
		}
	}
}

func TestStartupConfigRuntimeSnapshotsProtectMutableValues(t *testing.T) {
	t.Parallel()

	runtime, err := NewStartupConfigRuntime(startupConfigRuntimeTestSettings())
	if err != nil {
		t.Fatalf("create startup config runtime: %v", err)
	}
	snapshot := runtime.Snapshot()

	origins := snapshot.CORS().AllowedOrigins()
	origins[0] = "http://evil.example"
	if got := snapshot.CORS().AllowedOrigins()[0]; got != "http://localhost:5173" {
		t.Fatalf("caller mutated CORS origin slice, got %q", got)
	}
	originSet := snapshot.CORS().AllowedOriginSet()
	delete(originSet, "http://localhost:5173")
	originSet["http://evil.example"] = struct{}{}
	if !snapshot.CORS().AllowsOrigin("http://localhost:5173") || snapshot.CORS().AllowsOrigin("http://evil.example") {
		t.Fatal("caller mutated CORS origin set")
	}

	transportConfig := snapshot.RuntimeProxy().TransportConfig()
	transportConfig.RequestTimeout = 99 * time.Second
	if transportConfig.RequestTimeout != 99*time.Second {
		t.Fatal("expected local runtime transport config copy to be mutable")
	}
	if snapshot.RuntimeProxy().TransportConfig().RequestTimeout == transportConfig.RequestTimeout {
		t.Fatal("caller mutated runtime transport config")
	}
	client := snapshot.RuntimeProxy().HTTPClient()
	client.Timeout = time.Nanosecond
	client.Transport = nil
	freshClient := snapshot.RuntimeProxy().HTTPClient()
	if freshClient.Timeout != 17*time.Second || freshClient.Transport == nil {
		t.Fatalf("caller mutated runtime HTTP client seam: %+v", freshClient)
	}
}

func startupConfigRuntimeTestSettings() config.Settings {
	return config.Settings{
		CORSAllowedOrigins:               "http://localhost:5173, http://127.0.0.1:5173",
		AuthAccessTokenTTLSeconds:        17,
		AuthRefreshTokenTTLSeconds:       29,
		AuthCookieName:                   " access_cookie ",
		AuthRefreshCookieName:            " refresh_cookie ",
		AuthCookieSecure:                 true,
		RuntimeTransportConfig:           startupConfigRuntimeTransportConfig(17 * time.Second),
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 7},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 2},
	}
}

func startupConfigRuntimeTransportConfig(requestTimeout time.Duration) config.RuntimeTransportConfig {
	return config.RuntimeTransportConfig{
		MaxIdleConns:          25,
		MaxIdleConnsPerHost:   5,
		MaxConnsPerHost:       9,
		RequestTimeout:        requestTimeout,
		IdleConnTimeout:       19 * time.Second,
		ResponseHeaderTimeout: 7 * time.Second,
		TLSHandshakeTimeout:   11 * time.Second,
		ExpectContinueTimeout: 3 * time.Second,
	}
}

func unwrapStartupRuntimeTransport(t *testing.T, roundTripper http.RoundTripper) *http.Transport {
	t.Helper()
	wrapper, ok := roundTripper.(*runtimeRoundTripper)
	if !ok {
		t.Fatalf("expected startup runtime round tripper, got %T", roundTripper)
	}
	transport, ok := wrapper.transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected HTTP transport, got %T", wrapper.transport)
	}
	return transport
}

package platformhttp

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/email"
)

func TestHotBootstrapConfigRuntimeInitializesSnapshotsFromSettings(t *testing.T) {
	t.Parallel()

	settings := hotBootstrapRuntimeTestSettings()
	settings.Mail = validSMTPMailConfig()

	runtime, err := NewHotBootstrapConfigRuntime(settings)
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
	}

	snapshot := runtime.Snapshot()
	if got := snapshot.CORS().AllowedOrigins(); !reflect.DeepEqual(got, []string{"http://localhost:5173", "http://127.0.0.1:5173"}) {
		t.Fatalf("unexpected CORS origins: %v", got)
	}
	if !snapshot.CORS().AllowsOrigin(" http://127.0.0.1:5173 ") {
		t.Fatal("expected trimmed CORS origin to be allowed")
	}
	auth := snapshot.Auth()
	if auth.AccessTokenTTL != 17*time.Second || auth.RefreshTokenTTL != 29*time.Second || auth.ResetCodeTTL != 31*time.Second {
		t.Fatalf("unexpected auth TTLs: %+v", auth)
	}
	if auth.AccessCookieName != "access_cookie" || auth.RefreshCookieName != "refresh_cookie" || !auth.CookieSecure {
		t.Fatalf("unexpected auth cookies: %+v", auth)
	}
	mail := snapshot.Mail()
	if !mail.DeliveryEnabled() {
		t.Fatal("expected enabled SMTP mail snapshot")
	}
	if _, ok := mail.Mailer().(*email.SMTPMailer); !ok {
		t.Fatalf("expected SMTP mailer, got %T", mail.Mailer())
	}
	if mail.Config().SMTP.Host != "127.0.0.1" {
		t.Fatalf("unexpected mail config host: %q", mail.Config().SMTP.Host)
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
	transport := unwrapHotRuntimeTransport(t, client.Transport)
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

func TestHotBootstrapRuntimeSnapshotOmitsBufferingMode(t *testing.T) {
	t.Parallel()

	assertSnapshotOmitsRuntimeBufferingMode(t, reflect.TypeFor[HotRuntimeProxySnapshot]())
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

func TestHotBootstrapConfigRuntimeSnapshotsProtectMutableValues(t *testing.T) {
	t.Parallel()

	runtime, err := NewHotBootstrapConfigRuntime(hotBootstrapRuntimeTestSettings())
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
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

	mailConfig := snapshot.Mail().Config()
	mailConfig.SMTP.Host = "mutated"
	if snapshot.Mail().Config().SMTP.Host == "mutated" {
		t.Fatal("caller mutated mail config")
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

func TestHotBootstrapConfigRuntimePublishUpdatesAtomicallyAndKeepsOldSnapshotStable(t *testing.T) {
	t.Parallel()

	initial := hotBootstrapRuntimeTestSettings()
	runtime, err := NewHotBootstrapConfigRuntime(initial)
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
	}
	oldSnapshot := runtime.Snapshot()
	oldClient := oldSnapshot.RuntimeProxy().HTTPClient()

	updated := hotBootstrapRuntimeUpdatedSettings()
	retired, err := runtime.Publish(updated)
	if err != nil {
		t.Fatalf("publish hot bootstrap runtime: %v", err)
	}
	retired.CloseIdleConnections()

	newSnapshot := runtime.Snapshot()
	if got := newSnapshot.CORS().AllowedOrigins(); !reflect.DeepEqual(got, []string{"https://app.example.test"}) {
		t.Fatalf("new CORS origins = %v", got)
	}
	if got := newSnapshot.Auth().AccessTokenTTL; got != 41*time.Second {
		t.Fatalf("new auth access TTL = %s", got)
	}
	if !newSnapshot.Mail().DeliveryEnabled() {
		t.Fatal("expected updated mail snapshot to enable delivery")
	}
	if got := newSnapshot.RuntimeProxy().TransportConfig().RequestTimeout; got != 23*time.Second {
		t.Fatalf("new runtime request timeout = %s", got)
	}
	if got := newSnapshot.Admission().Limits(); got.ManagementM1 != 4 || got.ManagementM2 != 5 || got.ManagementM3 != 4 {
		t.Fatalf("new admission limits = %+v", got)
	}

	if got := oldSnapshot.CORS().AllowedOrigins(); !reflect.DeepEqual(got, []string{"http://localhost:5173", "http://127.0.0.1:5173"}) {
		t.Fatalf("old CORS origins changed: %v", got)
	}
	if got := oldSnapshot.Auth().AccessTokenTTL; got != 17*time.Second {
		t.Fatalf("old auth access TTL changed: %s", got)
	}
	if oldSnapshot.Mail().DeliveryEnabled() {
		t.Fatal("old mail snapshot changed")
	}
	if got := oldSnapshot.RuntimeProxy().TransportConfig().RequestTimeout; got != 17*time.Second {
		t.Fatalf("old runtime request timeout changed: %s", got)
	}

	newClient := newSnapshot.RuntimeProxy().HTTPClient()
	if oldClient == newClient {
		t.Fatal("expected publish to expose a distinct HTTP client")
	}
	if unwrapHotRuntimeTransport(t, oldClient.Transport) == unwrapHotRuntimeTransport(t, newClient.Transport) {
		t.Fatal("expected publish to create a distinct runtime transport")
	}
}

func TestHotBootstrapConfigRuntimeRejectsInvalidMailerBeforePublishing(t *testing.T) {
	t.Parallel()

	initial := hotBootstrapRuntimeTestSettings()
	runtime, err := NewHotBootstrapConfigRuntime(initial)
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
	}

	invalid := hotBootstrapRuntimeUpdatedSettings()
	invalid.Mail.From = "not an address"
	validateErr := runtime.Validate(invalid)
	if validateErr == nil || !strings.Contains(validateErr.Error(), "create hot mailer") {
		t.Fatalf("expected hot mailer validation error, got %v", validateErr)
	}
	if _, err := runtime.Publish(invalid); err == nil {
		t.Fatal("expected invalid mailer publish to fail")
	}

	snapshot := runtime.Snapshot()
	if got := snapshot.Auth().AccessTokenTTL; got != 17*time.Second {
		t.Fatalf("failed publish changed auth snapshot: %s", got)
	}
	if snapshot.Mail().DeliveryEnabled() {
		t.Fatal("failed publish changed mail snapshot")
	}
	if got := snapshot.RuntimeProxy().TransportConfig().RequestTimeout; got != 17*time.Second {
		t.Fatalf("failed publish changed runtime snapshot: %s", got)
	}
}

func hotBootstrapRuntimeTestSettings() config.Settings {
	return config.Settings{
		CORSAllowedOrigins:               "http://localhost:5173, http://127.0.0.1:5173",
		AuthAccessTokenTTLSeconds:        17,
		AuthRefreshTokenTTLSeconds:       29,
		AuthResetCodeTTLSeconds:          31,
		AuthCookieName:                   " access_cookie ",
		AuthRefreshCookieName:            " refresh_cookie ",
		AuthCookieSecure:                 true,
		RuntimeTransportConfig:           hotBootstrapRuntimeTransportConfig(17 * time.Second),
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 7},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 2},
		Mail: config.MailConfig{
			Enabled: false,
			From:    "not an address",
			SMTP: config.MailSMTPConfig{
				Host:     "192.0.2.1",
				Port:     587,
				Mode:     config.MailSMTPModeStartTLSRequired,
				Auth:     config.MailSMTPAuthPlain,
				Username: "smtp-user",
			},
		},
	}
}

func hotBootstrapRuntimeUpdatedSettings() config.Settings {
	settings := hotBootstrapRuntimeTestSettings()
	settings.CORSAllowedOrigins = "https://app.example.test"
	settings.AuthAccessTokenTTLSeconds = 41
	settings.AuthRefreshTokenTTLSeconds = 43
	settings.AuthResetCodeTTLSeconds = 47
	settings.AuthCookieName = "new_access_cookie"
	settings.AuthRefreshCookieName = "new_refresh_cookie"
	settings.AuthCookieSecure = false
	settings.RuntimeTransportConfig = hotBootstrapRuntimeTransportConfig(23 * time.Second)
	settings.ManagementDatabasePoolBudget = config.DatabasePoolBudget{MaxConns: 9}
	settings.ManagementAdmissionControlBudget = config.ManagementAdmissionBudget{M2MaxConcurrent: 5, M3MaxConcurrent: 4}
	settings.Mail = validSMTPMailConfig()
	return settings
}

func hotBootstrapRuntimeTransportConfig(requestTimeout time.Duration) config.RuntimeTransportConfig {
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

func unwrapHotRuntimeTransport(t *testing.T, roundTripper http.RoundTripper) *http.Transport {
	t.Helper()
	wrapper, ok := roundTripper.(*hotRuntimeRoundTripper)
	if !ok {
		t.Fatalf("expected hot runtime round tripper, got %T", roundTripper)
	}
	transport, ok := wrapper.transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected HTTP transport, got %T", wrapper.transport)
	}
	return transport
}

package config

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadCanonicalDefaultSettings(t *testing.T) {
	settings := loadCanonicalDefaultSettings("")
	if settings.Host != "0.0.0.0" || settings.Port != 8000 {
		t.Fatalf("unexpected canonical server defaults: host=%q port=%d", settings.Host, settings.Port)
	}
	if got := settings.CORSAllowedOriginsList(); len(got) != 2 || got[0] != "http://localhost:5173" || got[1] != "http://127.0.0.1:5173" {
		t.Fatalf("unexpected canonical CORS defaults: %+v", got)
	}
	if settings.RuntimeTelemetryMode != RuntimeTelemetryModeDurableOutbox {
		t.Fatalf("unexpected canonical runtime telemetry mode: %q", settings.RuntimeTelemetryMode)
	}
	assertTelemetryDefaults(t, settings.Telemetry)
	assertRuntimeTransportConfig(t, settings.RuntimeTransport(), RuntimeTransportConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		MaxConnsPerHost:       16,
		RequestTimeout:        300 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 0,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	})
	if got := settings.RuntimeSideEffects(); got.AttemptTimeout != 10*time.Second {
		t.Fatalf("unexpected canonical side-effects defaults: %+v", got)
	}
	assertPostgresPoolsBudget(t, settings.PostgresPoolsBudgetOrDefault(), derivedPostgresPoolsBudget(runtime.NumCPU()))
	wantAdmission := derivedManagementAdmissionBudget(runtime.NumCPU())
	if got := settings.ManagementAdmissionControlBudget; got != wantAdmission {
		t.Fatalf("unexpected raw management admission defaults: %+v", got)
	}
	admission := settings.ManagementAdmissionBudget()
	if admission != wantAdmission {
		t.Fatalf("unexpected normalized management admission defaults: %+v", admission)
	}
	if reservedM1 := int64(settings.ManagementDatabaseBudget().MaxConns) - admission.M2MaxConcurrent; reservedM1 != 1 {
		t.Fatalf("expected management lane to leave M1 reservation of 1, got %d", reservedM1)
	}
	if settings.SecretEncryptionKey != defaultSeedSecretEncryptionKey || settings.StateTransferBundleEncryptionKey != "" || settings.AuthJWTSecret != defaultAuthJWTSecret {
		t.Fatalf("unexpected canonical secret defaults: runtime=%q stateTransfer=%q jwt=%q", settings.SecretEncryptionKey, settings.StateTransferBundleEncryptionKey, settings.AuthJWTSecret)
	}
	if settings.Mail.Enabled || settings.Mail.SMTP.Timeout != defaultMailSMTPTimeout {
		t.Fatalf("unexpected canonical disabled mail defaults: %+v", settings.Mail)
	}
}

func TestTelemetryDefaults(t *testing.T) {
	settings := loadCanonicalDefaultSettings("")
	assertTelemetryDefaults(t, settings.Telemetry)

	payload := seededBootstrapPayload(t)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("decode seeded bootstrap payload: %v", err)
	}
	telemetryRaw, ok := raw["telemetry"]
	if !ok {
		t.Fatal("expected seeded bootstrap payload to include telemetry")
	}
	var telemetry map[string]json.RawMessage
	if err := json.Unmarshal(telemetryRaw, &telemetry); err != nil {
		t.Fatalf("decode seeded telemetry payload: %v", err)
	}
	if len(telemetry) != 1 {
		t.Fatalf("expected disabled seeded telemetry to contain only enabled, got %v", telemetry)
	}
	var enabled bool
	if err := json.Unmarshal(telemetry["enabled"], &enabled); err != nil {
		t.Fatalf("decode telemetry.enabled: %v", err)
	}
	if enabled {
		t.Fatal("expected seeded telemetry.enabled to be false")
	}

	parsed, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(payload)
	if err != nil {
		t.Fatalf("parse seeded telemetry defaults: %v", err)
	}
	assertTelemetryDefaults(t, parsed.Telemetry)
}

func TestAlertingWebhookURLDefaultsParsesAndValidates(t *testing.T) {
	settings := loadCanonicalDefaultSettings("")
	if settings.Alerting.WebhookURL != "" {
		t.Fatalf("expected alerting webhook URL to default empty, got %q", settings.Alerting.WebhookURL)
	}

	document := seededBootstrapDocument(t)
	document.Alerting = &bootstrapAlerting{WebhookURL: stringPointer(" https://alerts.example.test/hook ")}
	parsed, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(marshalBootstrapDocument(t, document))
	if err != nil {
		t.Fatalf("parse valid alerting webhook URL: %v", err)
	}
	if parsed.Alerting.WebhookURL != "https://alerts.example.test/hook" {
		t.Fatalf("unexpected alerting webhook URL: %q", parsed.Alerting.WebhookURL)
	}

	document.Alerting.WebhookURL = stringPointer("ftp://alerts.example.test/hook")
	_, err = NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(marshalBootstrapDocument(t, document))
	if err == nil || !strings.Contains(err.Error(), "alerting.webhookUrl must use http or https") {
		t.Fatalf("expected invalid alerting webhook URL to fail, got %v", err)
	}
}

func TestBootstrapConfigAcceptsStaleOpenAITerminalTranslationMode(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal(seededBootstrapPayload(t), &payload); err != nil {
		t.Fatalf("decode seeded bootstrap payload: %v", err)
	}
	payload["runtime"].(map[string]any)["routing"] = map[string]any{"openaiTerminalTranslationMode": "safe_only"}

	settings, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(mustMarshalBootstrapPayload(t, payload))
	if err != nil {
		t.Fatalf("expected stale OpenAI terminal translation mode field to be ignored, got %v", err)
	}
	if settings.RuntimeTransportConfig.RequestTimeout != defaultRuntimeTransportRequestTimeout {
		t.Fatalf("expected stale routing field to leave runtime settings unchanged, got %+v", settings.RuntimeTransportConfig)
	}
}

func TestBootstrapConfigAcceptsStaleRealtimePool(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal(seededBootstrapPayload(t), &payload); err != nil {
		t.Fatalf("decode seeded bootstrap payload: %v", err)
	}
	payload["database"].(map[string]any)["pools"].(map[string]any)["realtime"] = map[string]any{"maxConns": float64(0), "minIdleConns": float64(0)}

	settings, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(mustMarshalBootstrapPayload(t, payload))
	if err != nil {
		t.Fatalf("expected stale realtime pool field to be ignored, got %v", err)
	}
	if got := settings.PostgresPoolsBudgetOrDefault().SumMaxConns(); got != DefaultPostgresPoolsBudget().SumMaxConns() {
		t.Fatalf("expected stale realtime pool to stay out of active budget, got sum=%d", got)
	}
}

func TestTelemetryToSettings(t *testing.T) {
	document := seededBootstrapDocument(t)
	document.Telemetry = validBootstrapTelemetryDocument()
	settings, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(marshalBootstrapDocument(t, document))
	if err != nil {
		t.Fatalf("parse valid telemetry document: %v", err)
	}
	telemetry := settings.Telemetry
	if !telemetry.Enabled || !telemetry.Metrics.Enabled || !telemetry.Traces.Enabled {
		t.Fatalf("expected telemetry signals to be enabled, got %+v", telemetry)
	}
	if telemetry.Service.Namespace != defaultTelemetryServiceNamespace || telemetry.Service.Name != defaultTelemetryServiceName {
		t.Fatalf("unexpected code-derived telemetry service identity: %+v", telemetry.Service)
	}
	if telemetry.Exporter.Endpoint != "https://otel-collector.example.test:4318" || telemetry.Exporter.Protocol != TelemetryExporterProtocolHTTPProtobuf || telemetry.Exporter.Compression != TelemetryExporterCompressionGzip || telemetry.Exporter.Timeout != 7*time.Second {
		t.Fatalf("unexpected telemetry exporter settings: %+v", telemetry.Exporter)
	}
	if telemetry.Exporter.Auth.Mode != TelemetryExporterAuthModeAuthorizationHeader || telemetry.Exporter.Auth.AuthorizationHeader != "Bearer otlp-secret" {
		t.Fatalf("unexpected telemetry auth settings: %+v", telemetry.Exporter.Auth)
	}
	if telemetry.Exporter.TLS.InsecureSkipVerify || telemetry.Exporter.TLS.CAFile != "/etc/prism/otel-ca.pem" {
		t.Fatalf("unexpected telemetry TLS settings: %+v", telemetry.Exporter.TLS)
	}
	if telemetry.Traces.SamplingRatio != 0.25 {
		t.Fatalf("expected sampling ratio 0.25, got %v", telemetry.Traces.SamplingRatio)
	}
}

func TestRejectsInvalidTelemetryProtocol(t *testing.T) {
	document := seededBootstrapDocument(t)
	document.Telemetry = validBootstrapTelemetryDocument()
	document.Telemetry.Exporter.Protocol = stringPointer("http/json")
	assertTelemetryParseError(t, document, "telemetry.exporter.protocol must be one of")
}

func TestRejectsInvalidCompression(t *testing.T) {
	document := seededBootstrapDocument(t)
	document.Telemetry = validBootstrapTelemetryDocument()
	document.Telemetry.Exporter.Compression = stringPointer("brotli")
	assertTelemetryParseError(t, document, "telemetry.exporter.compression must be one of")
}

func TestRejectsInvalidSamplingRatio(t *testing.T) {
	for _, ratio := range []float64{-0.01, 1.01} {
		document := seededBootstrapDocument(t)
		document.Telemetry = validBootstrapTelemetryDocument()
		document.Telemetry.Traces.SamplingRatio = float64Pointer(ratio)
		assertTelemetryParseError(t, document, "telemetry.traces.samplingRatio must be between 0 and 1")
	}
}

func TestNormalizeManagementAdmissionBudget(t *testing.T) {
	defaults := defaultManagementAdmissionBudget()
	got := normalizeManagementAdmissionBudget(ManagementAdmissionBudget{}, defaults, 3)
	if got != (ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 3}) {
		t.Fatalf("unexpected normalized empty management admission budget: %+v", got)
	}

	settings := loadCanonicalDefaultSettings("")
	admission := settings.ManagementAdmissionBudget()
	if admission != defaults {
		t.Fatalf("expected canonical admission budget to avoid clamp drift, got %+v", admission)
	}
	if reservedM1 := int64(settings.ManagementDatabaseBudget().MaxConns) - admission.M2MaxConcurrent; reservedM1 != 1 {
		t.Fatalf("expected normalized admission to leave one M1 slot, got %d", reservedM1)
	}

	clamped := normalizeManagementAdmissionBudget(ManagementAdmissionBudget{M2MaxConcurrent: 9, M3MaxConcurrent: 7}, defaults, 3)
	if clamped != (ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 3}) {
		t.Fatalf("unexpected high-budget clamp result: %+v", clamped)
	}
}

func TestManagementAdmissionClamp(t *testing.T) {
	settings := loadCanonicalDefaultSettings("")
	settings.PostgresPoolsBudget.Management = DatabasePoolBudget{MaxConns: 4, MinIdleConns: 1}
	settings.ManagementAdmissionControlBudget = ManagementAdmissionBudget{M2MaxConcurrent: 32, M3MaxConcurrent: 32}
	configured, effective, clamped := settings.ManagementAdmissionClamp()
	if !clamped {
		t.Fatal("expected clamp to be reported")
	}
	if configured.M2MaxConcurrent != 32 || effective.M2MaxConcurrent != 3 || effective.M3MaxConcurrent != 3 {
		t.Fatalf("unexpected clamp report: configured=%+v effective=%+v", configured, effective)
	}

	if _, _, clamped := loadCanonicalDefaultSettings("").ManagementAdmissionClamp(); clamped {
		t.Fatal("canonical defaults must not report a clamp")
	}
}

func TestNormalizeRuntimeTransportConfig(t *testing.T) {
	want := RuntimeTransportConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		MaxConnsPerHost:       16,
		RequestTimeout:        300 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 0,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if got := defaultRuntimeTransportConfig(); got.MaxConnsPerHost != 16 {
		t.Fatalf("expected fresh canonical transport defaults to seed max conns per host 16, got %+v", got)
	}
	zeroPreserved := want
	zeroPreserved.MaxConnsPerHost = 0
	assertRuntimeTransportConfig(t, normalizeRuntimeTransportConfig(RuntimeTransportConfig{}, defaultRuntimeTransportConfig()), zeroPreserved)
	assertRuntimeTransportConfig(t, normalizeRuntimeTransportConfig(defaultRuntimeTransportConfig(), defaultRuntimeTransportConfig()), want)

	candidate := RuntimeTransportConfig{
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   32,
		MaxConnsPerHost:       8,
		RequestTimeout:        -time.Second,
		IdleConnTimeout:       -time.Second,
		ResponseHeaderTimeout: -time.Second,
		TLSHandshakeTimeout:   0,
		ExpectContinueTimeout: 0,
	}
	assertRuntimeTransportConfig(t, normalizeRuntimeTransportConfig(candidate, defaultRuntimeTransportConfig()), RuntimeTransportConfig{
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		MaxConnsPerHost:       8,
		RequestTimeout:        300 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 0,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	})
}

func TestSeededBootstrapDocumentUsesDerivedPoolDefaults(t *testing.T) {
	document, err := buildSeededBootstrapDocument(Load(), time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build seeded bootstrap document: %v", err)
	}
	wantPools := DefaultPostgresPoolsBudget()
	pools := document.Database.Pools
	if *pools.TotalMaxConns != int(wantPools.TotalMaxConns) {
		t.Fatalf("seeded totalMaxConns=%d want %d", *pools.TotalMaxConns, wantPools.TotalMaxConns)
	}
	if *pools.Management.MaxConns != int(wantPools.Management.MaxConns) {
		t.Fatalf("seeded management maxConns=%d want %d", *pools.Management.MaxConns, wantPools.Management.MaxConns)
	}
	wantAdmission := defaultManagementAdmissionBudget()
	admission := document.Database.ManagementAdmission
	if *admission.M2MaxConcurrent != int(wantAdmission.M2MaxConcurrent) || *admission.M3MaxConcurrent != int(wantAdmission.M3MaxConcurrent) {
		t.Fatalf("seeded admission m2=%d m3=%d want %+v", *admission.M2MaxConcurrent, *admission.M3MaxConcurrent, wantAdmission)
	}
}

func seededBootstrapDocument(t *testing.T) bootstrapConfigDocument {
	t.Helper()
	document, err := buildSeededBootstrapDocument(loadCanonicalDefaultSettings(""), time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build seeded bootstrap document: %v", err)
	}
	return document
}

func seededBootstrapPayload(t *testing.T) []byte {
	t.Helper()
	return marshalBootstrapDocument(t, seededBootstrapDocument(t))
}

func marshalBootstrapDocument(t *testing.T, document bootstrapConfigDocument) []byte {
	t.Helper()
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal bootstrap document: %v", err)
	}
	return payload
}

func mustMarshalBootstrapPayload(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal bootstrap payload: %v", err)
	}
	return encoded
}

func validBootstrapTelemetryDocument() *bootstrapTelemetry {
	return &bootstrapTelemetry{
		Enabled: boolPointer(true),
		Exporter: &bootstrapTelemetryExporter{
			Endpoint:    stringPointer("https://otel-collector.example.test:4318"),
			Protocol:    stringPointer(string(TelemetryExporterProtocolHTTPProtobuf)),
			Compression: stringPointer(string(TelemetryExporterCompressionGzip)),
			Timeout:     stringPointer("7s"),
			Auth: &bootstrapTelemetryExporterAuth{
				Mode:                stringPointer(string(TelemetryExporterAuthModeAuthorizationHeader)),
				AuthorizationHeader: stringPointer("Bearer otlp-secret"),
			},
			TLS: &bootstrapTelemetryExporterTLS{
				InsecureSkipVerify: boolPointer(false),
				CAFile:             stringPointer("/etc/prism/otel-ca.pem"),
			},
		},
		Metrics: &bootstrapTelemetrySignal{Enabled: boolPointer(true)},
		Traces:  &bootstrapTelemetryTraces{Enabled: boolPointer(true), SamplingRatio: float64Pointer(0.25)},
	}
}

func assertTelemetryDefaults(t *testing.T, telemetry TelemetryConfig) {
	t.Helper()
	if telemetry.Enabled || telemetry.Metrics.Enabled || telemetry.Traces.Enabled {
		t.Fatalf("expected telemetry to default disabled, got %+v", telemetry)
	}
	if telemetry.Service.Namespace != defaultTelemetryServiceNamespace || telemetry.Service.Name != defaultTelemetryServiceName {
		t.Fatalf("unexpected telemetry service defaults: %+v", telemetry.Service)
	}
	if telemetry.Exporter.Endpoint != "" || telemetry.Exporter.Protocol != defaultTelemetryExporterProtocol || telemetry.Exporter.Compression != defaultTelemetryExporterCompression || telemetry.Exporter.Timeout != defaultTelemetryExporterTimeout {
		t.Fatalf("unexpected telemetry exporter defaults: %+v", telemetry.Exporter)
	}
	if telemetry.Exporter.Auth.Mode != defaultTelemetryExporterAuthMode || telemetry.Exporter.Auth.AuthorizationHeader != "" || telemetry.Exporter.TLS != (TelemetryExporterTLSConfig{}) {
		t.Fatalf("unexpected telemetry auth/tls defaults: auth=%+v tls=%+v", telemetry.Exporter.Auth, telemetry.Exporter.TLS)
	}
	if telemetry.Traces.SamplingRatio != defaultTelemetryTracesSamplingRatio {
		t.Fatalf("unexpected telemetry sampling default: %v", telemetry.Traces.SamplingRatio)
	}
}

func assertTelemetryParseError(t *testing.T, document bootstrapConfigDocument, want string) {
	t.Helper()
	_, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(marshalBootstrapDocument(t, document))
	if err == nil {
		t.Fatalf("expected telemetry parse error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected telemetry parse error containing %q, got %v", want, err)
	}
}

func assertRuntimeTransportConfig(t *testing.T, got RuntimeTransportConfig, want RuntimeTransportConfig) {
	t.Helper()
	if got != want {
		t.Fatalf("unexpected runtime transport config:\n got: %+v\nwant: %+v", got, want)
	}
}

func assertPostgresPoolsBudget(t *testing.T, got PostgresPoolsBudget, want PostgresPoolsBudget) {
	t.Helper()
	if got != want {
		t.Fatalf("unexpected postgres pools budget:\n got: %+v\nwant: %+v", got, want)
	}
	if got.SumMaxConns() != int64(want.TotalMaxConns) {
		t.Fatalf("expected pool lane sum %d to match total %d", got.SumMaxConns(), want.TotalMaxConns)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("expected postgres pool budget to validate: %v", err)
	}
}

func TestDerivedPoolDefaults(t *testing.T) {
	for _, tc := range []struct {
		cores int
		unit  int32
	}{
		{cores: 1, unit: 8},
		{cores: 8, unit: 8},
		{cores: 12, unit: 12},
		{cores: 32, unit: 16},
	} {
		budget := derivedPostgresPoolsBudget(tc.cores)
		admission := derivedManagementAdmissionBudget(tc.cores)
		if budget.Management.MaxConns != tc.unit+1 || budget.Management.MinIdleConns != 1 {
			t.Fatalf("cores=%d unexpected management budget: %+v", tc.cores, budget.Management)
		}
		if budget.RuntimeExecution.MaxConns != tc.unit || budget.RuntimeExecution.MinIdleConns != 2 {
			t.Fatalf("cores=%d unexpected runtime execution budget: %+v", tc.cores, budget.RuntimeExecution)
		}
		if budget.RuntimeTelemetry.MaxConns != tc.unit/2 {
			t.Fatalf("cores=%d unexpected runtime telemetry budget: %+v", tc.cores, budget.RuntimeTelemetry)
		}
		for lane, got := range map[string]DatabasePoolBudget{
			"runtimeFeedback": budget.RuntimeFeedback,
			"cacheRefresh":    budget.CacheRefresh,
			"backgroundJobs":  budget.BackgroundJobs,
		} {
			if got.MaxConns != tc.unit/4 || got.MinIdleConns != 0 {
				t.Fatalf("cores=%d unexpected %s budget: %+v", tc.cores, lane, got)
			}
		}
		if int64(budget.TotalMaxConns) != budget.SumMaxConns() {
			t.Fatalf("cores=%d total %d != lane sum %d", tc.cores, budget.TotalMaxConns, budget.SumMaxConns())
		}
		if err := budget.Validate(); err != nil {
			t.Fatalf("cores=%d derived budget must validate: %v", tc.cores, err)
		}
		if admission.M2MaxConcurrent != int64(tc.unit) || admission.M3MaxConcurrent != int64(tc.unit/2) {
			t.Fatalf("cores=%d unexpected derived admission: %+v", tc.cores, admission)
		}
		// m2 必须正好占满 management lane 减 1(留 M1 位),否则默认值会被自身 clamp。
		if admission.M2MaxConcurrent != int64(budget.Management.MaxConns-1) {
			t.Fatalf("cores=%d m2=%d must equal management.maxConns-1=%d", tc.cores, admission.M2MaxConcurrent, budget.Management.MaxConns-1)
		}
	}
	// 下限必须覆盖 settings 页 5 个并发 M2 请求。
	if got := derivedManagementAdmissionBudget(1).M2MaxConcurrent; got < 6 {
		t.Fatalf("floor m2=%d cannot admit the settings page fan-out of 5 concurrent M2 calls", got)
	}
}

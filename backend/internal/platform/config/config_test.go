package config

import (
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
	if settings.RuntimeTelemetryMode != RuntimeTelemetryModeDurableOutbox || settings.RuntimeBufferingMode != RuntimeBufferingModeStreaming {
		t.Fatalf("unexpected canonical runtime modes: telemetry=%q buffering=%q", settings.RuntimeTelemetryMode, settings.RuntimeBufferingMode)
	}
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
	assertPostgresPoolsBudget(t, settings.PostgresPoolsBudgetOrDefault(), PostgresPoolsBudget{
		TotalMaxConns:    24,
		Management:       DatabasePoolBudget{MaxConns: 4, MinIdleConns: 1},
		RuntimeExecution: DatabasePoolBudget{MaxConns: 8, MinIdleConns: 2},
		RuntimeTelemetry: DatabasePoolBudget{MaxConns: 4, MinIdleConns: 1},
		RuntimeFeedback:  DatabasePoolBudget{MaxConns: 2, MinIdleConns: 0},
		Realtime:         DatabasePoolBudget{MaxConns: 2, MinIdleConns: 0},
		CacheRefresh:     DatabasePoolBudget{MaxConns: 2, MinIdleConns: 0},
		BackgroundJobs:   DatabasePoolBudget{MaxConns: 2, MinIdleConns: 0},
	})
	if got := settings.ManagementAdmissionControlBudget; got != (ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 2}) {
		t.Fatalf("unexpected raw management admission defaults: %+v", got)
	}
	admission := settings.ManagementAdmissionBudget()
	if admission != (ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 2}) {
		t.Fatalf("unexpected normalized management admission defaults: %+v", admission)
	}
	if reservedM1 := int64(settings.ManagementDatabaseBudget().MaxConns) - admission.M2MaxConcurrent; reservedM1 != 1 {
		t.Fatalf("expected management lane to leave M1 reservation of 1, got %d", reservedM1)
	}
	if settings.SecretEncryptionKey != defaultSeedSecretEncryptionKey || settings.ConfigBundleEncryptionKey != defaultSeedSecretEncryptionKey || settings.AuthJWTSecret != defaultAuthJWTSecret {
		t.Fatalf("unexpected canonical secret defaults: runtime=%q bundle=%q jwt=%q", settings.SecretEncryptionKey, settings.ConfigBundleEncryptionKey, settings.AuthJWTSecret)
	}
	if settings.Mail.Enabled || settings.Mail.SMTP.Timeout != defaultMailSMTPTimeout {
		t.Fatalf("unexpected canonical disabled mail defaults: %+v", settings.Mail)
	}
}

func TestNormalizeManagementAdmissionBudget(t *testing.T) {
	defaults := ManagementAdmissionBudget{M2MaxConcurrent: defaultManagementM2MaxConcurrent, M3MaxConcurrent: defaultManagementM3MaxConcurrent}
	got := normalizeManagementAdmissionBudget(ManagementAdmissionBudget{}, defaults, 3)
	if got != (ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 2}) {
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

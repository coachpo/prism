package contract_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

const (
	bootstrapContractDatabaseURL = "postgres://prism:prism@localhost:5432/prism?sslmode=disable"
	bootstrapContractSecretKey   = "prism-dev-runtime-secret-change-me"
	bootstrapContractJWTSecret   = "prism-dev-jwt-secret-change-me-2026"
	bootstrapContractBundleKey   = "prism-dev-runtime-secret-change-me"
)

func TestBootstrapConfigSchema(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("valid fixture loads", func(t *testing.T) {
		if _, err := manager.Load(bootstrapFixturePath(t, "bootstrap-valid-v1.json")); err != nil {
			t.Fatalf("load valid bootstrap fixture: %v", err)
		}
	})

	t.Run("missing required schema field fails", func(t *testing.T) {
		_, err := manager.Load(bootstrapFixturePath(t, "bootstrap-invalid-schema-v1.json"))
		if err == nil {
			t.Fatal("expected invalid schema fixture to fail")
		}
		if !strings.Contains(err.Error(), "auth.cookieSecure is required") {
			t.Fatalf("expected missing auth.cookieSecure error, got %v", err)
		}
	})

	t.Run("unknown field fails", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["unexpected"] = true
		_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err == nil {
			t.Fatal("expected unknown field to fail")
		}
		if !strings.Contains(err.Error(), `unknown field "unexpected"`) {
			t.Fatalf("expected unknown field error, got %v", err)
		}
	})

	t.Run("legacy encrypted fixture fails fast", func(t *testing.T) {
		_, err := manager.Load(bootstrapFixturePath(t, "bootstrap-unsupported-encrypted-v1.json"))
		if err == nil {
			t.Fatal("expected legacy encrypted fixture to fail")
		}
		if !strings.Contains(err.Error(), "unsupported legacy encrypted format fields") {
			t.Fatalf("expected unsupported legacy format error, got %v", err)
		}
		if !strings.Contains(err.Error(), "secretPayload") {
			t.Fatalf("expected unsupported legacy field list to mention secretPayload, got %v", err)
		}
	})

	t.Run("legacy encrypted field mutations fail fast", func(t *testing.T) {
		tests := []struct {
			name    string
			mutate  func(map[string]any)
			wantErr string
		}{
			{
				name: "secret payload rejected",
				mutate: func(payload map[string]any) {
					payload["secretPayload"] = map[string]any{"kind": "encrypted"}
				},
				wantErr: "secretPayload",
			},
			{
				name: "database url secret ref rejected",
				mutate: func(payload map[string]any) {
					payload["database"].(map[string]any)["urlSecretRef"] = "database:primary:url"
				},
				wantErr: "database.urlSecretRef",
			},
			{
				name: "jwt signing key secret ref rejected",
				mutate: func(payload map[string]any) {
					payload["auth"].(map[string]any)["jwtSigningKeySecretRef"] = "auth:jwt:signing-key"
				},
				wantErr: "auth.jwtSigningKeySecretRef",
			},
			{
				name: "bundle encryption key secret ref rejected",
				mutate: func(payload map[string]any) {
					payload["stateTransfer"].(map[string]any)["bundleEncryptionKeySecretRef"] = "state-transfer:bundle-encryption-key"
				},
				wantErr: "stateTransfer.bundleEncryptionKeySecretRef",
			},
		}

		for _, testCase := range tests {
			t.Run(testCase.name, func(t *testing.T) {
				payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
				testCase.mutate(payload)

				_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
				if err == nil {
					t.Fatalf("expected %s to fail", testCase.name)
				}
				if !strings.Contains(err.Error(), "unsupported legacy encrypted format fields") || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("expected unsupported legacy format error containing %q, got %v", testCase.wantErr, err)
				}
			})
		}
	})
}

func TestBootstrapConfigSemanticValidation(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("fixture rejects management pool imbalance", func(t *testing.T) {
		_, err := manager.Load(bootstrapFixturePath(t, "bootstrap-invalid-semantic-v1.json"))
		if err == nil {
			t.Fatal("expected invalid semantic fixture to fail")
		}
		if !strings.Contains(err.Error(), "database.managementPool.minIdleConns must be less than or equal to database.managementPool.maxConns") {
			t.Fatalf("expected management pool imbalance error, got %v", err)
		}
	})

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "invalid duration string",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["transport"].(map[string]any)["idleConnTimeout"] = "not-a-duration"
			},
			wantErr: "runtime.transport.idleConnTimeout must parse as a Go duration",
		},
		{
			name: "runtime pool min idle exceeds max",
			mutate: func(payload map[string]any) {
				payload["database"].(map[string]any)["runtimePool"].(map[string]any)["minIdleConns"] = 7
			},
			wantErr: "database.runtimePool.minIdleConns must be less than or equal to database.runtimePool.maxConns",
		},
		{
			name: "management admission m3 exceeds m2",
			mutate: func(payload map[string]any) {
				payload["database"].(map[string]any)["managementAdmission"].(map[string]any)["m3MaxConcurrent"] = 11
			},
			wantErr: "database.managementAdmission.m3MaxConcurrent must be less than or equal to database.managementAdmission.m2MaxConcurrent",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
			testCase.mutate(payload)
			_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
			if err == nil {
				t.Fatalf("expected %s to fail", testCase.name)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("expected error containing %q, got %v", testCase.wantErr, err)
			}
		})
	}
}

func TestBootstrapConfigPlaintextMapping(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("fixture embeds plaintext startup values", func(t *testing.T) {
		raw := loadBootstrapFixtureBytes(t, "bootstrap-valid-v1.json")
		for _, value := range []string{bootstrapContractDatabaseURL, bootstrapContractSecretKey, bootstrapContractJWTSecret, bootstrapContractBundleKey} {
			if !bytes.Contains(raw, []byte(value)) {
				t.Fatalf("expected fixture to keep plaintext value %q", value)
			}
		}
		for _, forbidden := range []string{"secretPayload", "urlSecretRef", "jwtSigningKeySecretRef", "bundleEncryptionKeySecretRef", "enc:"} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Fatalf("expected fixture to omit legacy encrypted marker %q", forbidden)
			}
		}
	})

	t.Run("valid fixture resolves settings surface", func(t *testing.T) {
		settings, err := manager.Load(bootstrapFixturePath(t, "bootstrap-valid-v1.json"))
		if err != nil {
			t.Fatalf("load valid bootstrap fixture: %v", err)
		}
		if settings.Host != "0.0.0.0" || settings.Port != 18000 {
			t.Fatalf("unexpected server settings: %+v", settings)
		}
		if !settings.DocsEnabled() {
			t.Fatal("expected docs to be enabled from bootstrap config")
		}
		if settings.DatabaseURL != bootstrapContractDatabaseURL {
			t.Fatalf("expected database URL %q, got %q", bootstrapContractDatabaseURL, settings.DatabaseURL)
		}
		if settings.SecretEncryptionKey != bootstrapContractSecretKey {
			t.Fatalf("expected secret encryption key %q, got %q", bootstrapContractSecretKey, settings.SecretEncryptionKey)
		}
		if settings.ConfigBundleEncryptionKey != bootstrapContractBundleKey {
			t.Fatalf("expected bundle encryption key %q, got %q", bootstrapContractBundleKey, settings.ConfigBundleEncryptionKey)
		}
		if settings.AuthJWTSecret != bootstrapContractJWTSecret {
			t.Fatalf("expected JWT secret %q, got %q", bootstrapContractJWTSecret, settings.AuthJWTSecret)
		}
		if settings.AuthAccessTokenTTLSeconds != 900 || settings.AuthRefreshTokenTTLSeconds != 604800 || settings.AuthResetCodeTTLSeconds != 600 {
			t.Fatalf("unexpected auth TTL settings: %+v", settings)
		}
		if settings.AuthCookieName != "prism_access_token" || settings.AuthRefreshCookieName != "prism_refresh_token" || settings.AuthCookieSecure {
			t.Fatalf("unexpected auth cookie settings: %+v", settings)
		}
		if settings.RuntimeTelemetryMode != config.RuntimeTelemetryModeDurableOutbox {
			t.Fatalf("expected durable runtime telemetry mode, got %q", settings.RuntimeTelemetryMode)
		}
		if settings.RuntimeBufferingMode != config.RuntimeBufferingModeBuffered {
			t.Fatalf("expected buffered runtime buffering mode, got %q", settings.RuntimeBufferingMode)
		}
		transport := settings.RuntimeTransport()
		if transport.MaxIdleConns != 100 || transport.MaxIdleConnsPerHost != 8 || transport.MaxConnsPerHost != 0 {
			t.Fatalf("unexpected runtime transport pool settings: %+v", transport)
		}
		if transport.IdleConnTimeout != 90*time.Second || transport.ResponseHeaderTimeout != 0 || transport.TLSHandshakeTimeout != 10*time.Second || transport.ExpectContinueTimeout != time.Second {
			t.Fatalf("unexpected runtime transport timeouts: %+v", transport)
		}
		if got := settings.ManagementDatabaseBudget(); got.MaxConns != 12 || got.MinIdleConns != 0 {
			t.Fatalf("unexpected management database budget: %+v", got)
		}
		if got := settings.ManagementAdmissionBudget(); got.M2MaxConcurrent != 10 || got.M3MaxConcurrent != 6 {
			t.Fatalf("unexpected management admission budget: %+v", got)
		}
		if got := settings.CORSAllowedOriginsList(); len(got) != 2 || got[0] != "http://localhost:15173" || got[1] != "http://127.0.0.1:15173" {
			t.Fatalf("unexpected CORS origins: %+v", got)
		}
	})
}

func bootstrapFixturePath(t *testing.T, fileName string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve bootstrap config contract test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "bootstrap", fileName))
}

func loadBootstrapFixtureBytes(t *testing.T, fileName string) []byte {
	t.Helper()
	raw, err := os.ReadFile(bootstrapFixturePath(t, fileName))
	if err != nil {
		t.Fatalf("read bootstrap fixture %s: %v", fileName, err)
	}
	return raw
}

func loadBootstrapFixture(t *testing.T, fileName string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(loadBootstrapFixtureBytes(t, fileName), &payload); err != nil {
		t.Fatalf("decode bootstrap fixture %s: %v", fileName, err)
	}
	return payload
}

func mustMarshalBootstrapFixture(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal bootstrap fixture: %v", err)
	}
	return raw
}

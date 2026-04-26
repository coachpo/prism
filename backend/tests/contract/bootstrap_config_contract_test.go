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
	bootstrapContractMasterKey   = "bootstrap-contract-master-key"
	bootstrapContractDatabaseURL = "postgres://prism:bootstrap-password@db.internal:5432/prism?sslmode=disable"
	bootstrapContractJWTSecret   = "bootstrap-jwt-signing-secret"
	bootstrapContractBundleKey   = "bootstrap-bundle-encryption-key"
)

func TestBootstrapConfigSchema(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("valid fixture loads", func(t *testing.T) {
		if _, err := manager.Load(bootstrapFixturePath(t, "bootstrap-valid-v1.json"), bootstrapContractMasterKey); err != nil {
			t.Fatalf("load valid bootstrap fixture: %v", err)
		}
	})

	t.Run("missing required schema field fails", func(t *testing.T) {
		_, err := manager.Load(bootstrapFixturePath(t, "bootstrap-invalid-schema-v1.json"), bootstrapContractMasterKey)
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
		_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload), bootstrapContractMasterKey)
		if err == nil {
			t.Fatal("expected unknown field to fail")
		}
		if !strings.Contains(err.Error(), `unknown field "unexpected"`) {
			t.Fatalf("expected unknown field error, got %v", err)
		}
	})

	t.Run("invalid secret ref pattern fails", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["database"].(map[string]any)["urlSecretRef"] = "INVALID REF"
		_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload), bootstrapContractMasterKey)
		if err == nil {
			t.Fatal("expected invalid secret ref to fail")
		}
		if !strings.Contains(err.Error(), "database.urlSecretRef") {
			t.Fatalf("expected database.urlSecretRef error, got %v", err)
		}
	})
}

func TestBootstrapConfigSemanticValidation(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("fixture rejects management pool imbalance", func(t *testing.T) {
		_, err := manager.Load(bootstrapFixturePath(t, "bootstrap-invalid-semantic-v1.json"), bootstrapContractMasterKey)
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
				payload["database"].(map[string]any)["managementAdmission"].(map[string]any)["m3MaxConcurrent"] = 7
			},
			wantErr: "database.managementAdmission.m3MaxConcurrent must be less than or equal to database.managementAdmission.m2MaxConcurrent",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
			testCase.mutate(payload)
			_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload), bootstrapContractMasterKey)
			if err == nil {
				t.Fatalf("expected %s to fail", testCase.name)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("expected error containing %q, got %v", testCase.wantErr, err)
			}
		})
	}
}

func TestBootstrapConfigSecretResolution(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("fixture does not embed plaintext secrets", func(t *testing.T) {
		raw := loadBootstrapFixtureBytes(t, "bootstrap-valid-v1.json")
		for _, secret := range []string{bootstrapContractDatabaseURL, bootstrapContractJWTSecret, bootstrapContractBundleKey} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatalf("expected fixture to avoid plaintext secret %q", secret)
			}
		}
	})

	t.Run("valid fixture resolves settings surface", func(t *testing.T) {
		settings, err := manager.Load(bootstrapFixturePath(t, "bootstrap-valid-v1.json"), bootstrapContractMasterKey)
		if err != nil {
			t.Fatalf("load valid bootstrap fixture: %v", err)
		}
		if settings.Host != "127.0.0.1" || settings.Port != 18000 {
			t.Fatalf("unexpected server settings: %+v", settings)
		}
		if !settings.DocsEnabled() {
			t.Fatal("expected docs to be enabled from bootstrap config")
		}
		if settings.DatabaseURL != bootstrapContractDatabaseURL {
			t.Fatalf("expected database URL to resolve from secret payload, got %q", settings.DatabaseURL)
		}
		if settings.SecretEncryptionKey != bootstrapContractMasterKey {
			t.Fatalf("expected secret encryption key to use bootstrap master key, got %q", settings.SecretEncryptionKey)
		}
		if settings.ConfigBundleEncryptionKey != bootstrapContractBundleKey {
			t.Fatalf("expected bundle encryption key to resolve from secret payload, got %q", settings.ConfigBundleEncryptionKey)
		}
		if settings.AuthJWTSecret != bootstrapContractJWTSecret {
			t.Fatalf("expected JWT secret to resolve from secret payload, got %q", settings.AuthJWTSecret)
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
		if settings.RuntimeBufferingMode != config.RuntimeBufferingModeStreaming {
			t.Fatalf("expected streaming runtime buffering mode, got %q", settings.RuntimeBufferingMode)
		}
		transport := settings.RuntimeTransport()
		if transport.MaxIdleConns != 120 || transport.MaxIdleConnsPerHost != 12 || transport.MaxConnsPerHost != 24 {
			t.Fatalf("unexpected runtime transport pool settings: %+v", transport)
		}
		if transport.IdleConnTimeout != 45*time.Second || transport.ResponseHeaderTimeout != 15*time.Second || transport.TLSHandshakeTimeout != 10*time.Second || transport.ExpectContinueTimeout != time.Second {
			t.Fatalf("unexpected runtime transport timeouts: %+v", transport)
		}
		if got := settings.CORSAllowedOriginsList(); len(got) != 2 || got[0] != "http://localhost:15173" || got[1] != "http://127.0.0.1:15173" {
			t.Fatalf("unexpected CORS origins: %+v", got)
		}
	})

	t.Run("null bundle secret ref falls back to master key", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["stateTransfer"].(map[string]any)["bundleEncryptionKeySecretRef"] = nil
		payload["secretPayload"].(map[string]any)["entries"] = payload["secretPayload"].(map[string]any)["entries"].([]any)[:2]
		settings, err := manager.Parse(mustMarshalBootstrapFixture(t, payload), bootstrapContractMasterKey)
		if err != nil {
			t.Fatalf("parse bootstrap config with null bundle key ref: %v", err)
		}
		if settings.ConfigBundleEncryptionKey != bootstrapContractMasterKey {
			t.Fatalf("expected null bundle key ref to fall back to master key, got %q", settings.ConfigBundleEncryptionKey)
		}
	})

	t.Run("unreferenced secret payload entries are rejected", func(t *testing.T) {
		tests := []struct {
			name    string
			mutate  func(map[string]any)
			wantErr string
		}{
			{
				name: "arbitrary extra secret ref",
				mutate: func(payload map[string]any) {
					entries := payload["secretPayload"].(map[string]any)["entries"].([]any)
					ciphertext := entries[0].(map[string]any)["ciphertext"]
					payload["secretPayload"].(map[string]any)["entries"] = append(entries, map[string]any{
						"ref":        "startup:extra:secret",
						"ciphertext": ciphertext,
					})
				},
				wantErr: `secretPayload.entries contains unreferenced secret ref "startup:extra:secret"`,
			},
			{
				name: "bundle secret entry without bundle ref",
				mutate: func(payload map[string]any) {
					payload["stateTransfer"].(map[string]any)["bundleEncryptionKeySecretRef"] = nil
				},
				wantErr: `secretPayload.entries contains unreferenced secret ref "state-transfer:bundle-encryption-key"`,
			},
		}

		for _, testCase := range tests {
			t.Run(testCase.name, func(t *testing.T) {
				payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
				testCase.mutate(payload)

				_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload), bootstrapContractMasterKey)
				if err == nil {
					t.Fatalf("expected %s to fail", testCase.name)
				}
				if !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("expected error containing %q, got %v", testCase.wantErr, err)
				}
			})
		}
	})

	t.Run("missing secret refs are rejected without leaking plaintext", func(t *testing.T) {
		fixtures := []struct {
			name    string
			rawJSON []byte
			wantErr string
		}{
			{
				name:    "fixture missing optional bundle key ref",
				rawJSON: loadBootstrapFixtureBytes(t, "bootstrap-missing-secret-ref-v1.json"),
				wantErr: `stateTransfer.bundleEncryptionKeySecretRef references missing secret ref "state-transfer:missing-bundle-encryption-key"`,
			},
			{
				name: "missing database secret ref",
				rawJSON: func() []byte {
					payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
					payload["database"].(map[string]any)["urlSecretRef"] = "database:missing:url"
					return mustMarshalBootstrapFixture(t, payload)
				}(),
				wantErr: `database.urlSecretRef references missing secret ref "database:missing:url"`,
			},
			{
				name: "missing jwt secret ref",
				rawJSON: func() []byte {
					payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
					payload["auth"].(map[string]any)["jwtSigningKeySecretRef"] = "auth:missing:jwt"
					return mustMarshalBootstrapFixture(t, payload)
				}(),
				wantErr: `auth.jwtSigningKeySecretRef references missing secret ref "auth:missing:jwt"`,
			},
		}

		for _, fixture := range fixtures {
			t.Run(fixture.name, func(t *testing.T) {
				_, err := manager.Parse(fixture.rawJSON, bootstrapContractMasterKey)
				if err == nil {
					t.Fatalf("expected %s to fail", fixture.name)
				}
				if !strings.Contains(err.Error(), fixture.wantErr) {
					t.Fatalf("expected error containing %q, got %v", fixture.wantErr, err)
				}
				for _, secret := range []string{bootstrapContractDatabaseURL, bootstrapContractJWTSecret, bootstrapContractBundleKey} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("expected error to omit plaintext secret %q, got %v", secret, err)
					}
				}
			})
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

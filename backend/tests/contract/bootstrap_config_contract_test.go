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
	bootstrapContractJWTSecret   = "prism-dev-jwt-secret-change-me"
	bootstrapContractBundleKey   = "prism-dev-runtime-secret-change-me"
)

func TestBootstrapConfigSchema(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("valid fixture loads", func(t *testing.T) {
		snapshot, settings, err := manager.LoadBootstrapConfigDocument(bootstrapFixturePath(t, "bootstrap-valid-v1.json"))
		if err != nil {
			t.Fatalf("load valid bootstrap fixture: %v", err)
		}
		if settings.Mail.Enabled {
			t.Fatal("expected legacy bootstrap fixture without mail block to load with mail disabled")
		}
		if snapshot.Values.Runtime == nil || snapshot.Values.Runtime.SideEffects == nil || snapshot.Values.Runtime.SideEffects.AttemptTimeout == nil || *snapshot.Values.Runtime.SideEffects.AttemptTimeout != "10s" {
			t.Fatalf("expected safe runtime side_effects attempt_timeout to be exposed, got %+v", snapshot.Values.Runtime)
		}
		assertContractDisabledSafeMailValues(t, snapshot.Values.Mail)
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

	t.Run("missing request timeout fails", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		delete(payload["runtime"].(map[string]any)["transport"].(map[string]any), "requestTimeout")
		_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err == nil {
			t.Fatal("expected missing request timeout to fail")
		}
		if !strings.Contains(err.Error(), "runtime.transport.requestTimeout is required") {
			t.Fatalf("expected missing request timeout error, got %v", err)
		}
	})

	t.Run("missing runtime side effects fails", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		delete(payload["runtime"].(map[string]any), "sideEffects")
		_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err == nil {
			t.Fatal("expected missing runtime side effects to fail")
		}
		if !strings.Contains(err.Error(), "runtime.sideEffects is required") {
			t.Fatalf("expected missing runtime side effects error, got %v", err)
		}
	})

	t.Run("missing runtime side effects attempt timeout fails", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		delete(payload["runtime"].(map[string]any)["sideEffects"].(map[string]any), "attemptTimeout")
		_, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err == nil {
			t.Fatal("expected missing runtime side effects attempt timeout to fail")
		}
		if !strings.Contains(err.Error(), "runtime.sideEffects.attemptTimeout is required") {
			t.Fatalf("expected missing runtime side effects attempt timeout error, got %v", err)
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

func TestBootstrapConfigValidation(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("fixture rejects management pool imbalance", func(t *testing.T) {
		_, err := manager.Load(bootstrapFixturePath(t, "bootstrap-invalid-semantic-v1.json"))
		if err == nil {
			t.Fatal("expected invalid semantic fixture to fail")
		}
		if !strings.Contains(err.Error(), "lane=management min_idle_conns must be less than or equal to max_conns") {
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
			name: "invalid request timeout string",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["transport"].(map[string]any)["requestTimeout"] = "not-a-duration"
			},
			wantErr: "runtime.transport.requestTimeout must parse as a Go duration",
		},
		{
			name: "empty request timeout string",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["transport"].(map[string]any)["requestTimeout"] = "   "
			},
			wantErr: "runtime.transport.requestTimeout must be at least 1 characters",
		},
		{
			name: "zero request timeout",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["transport"].(map[string]any)["requestTimeout"] = "0s"
			},
			wantErr: "runtime.transport.requestTimeout must be greater than zero",
		},
		{
			name: "negative request timeout",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["transport"].(map[string]any)["requestTimeout"] = "-1s"
			},
			wantErr: "runtime.transport.requestTimeout must be greater than zero",
		},
		{
			name: "invalid side effects attempt timeout string",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["sideEffects"].(map[string]any)["attemptTimeout"] = "not-a-duration"
			},
			wantErr: "runtime.sideEffects.attemptTimeout must parse as a Go duration",
		},
		{
			name: "empty side effects attempt timeout string",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["sideEffects"].(map[string]any)["attemptTimeout"] = "   "
			},
			wantErr: "runtime.sideEffects.attemptTimeout must be at least 1 characters",
		},
		{
			name: "zero side effects attempt timeout",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["sideEffects"].(map[string]any)["attemptTimeout"] = "0s"
			},
			wantErr: "runtime.sideEffects.attemptTimeout must be greater than zero",
		},
		{
			name: "negative side effects attempt timeout",
			mutate: func(payload map[string]any) {
				payload["runtime"].(map[string]any)["sideEffects"].(map[string]any)["attemptTimeout"] = "-1s"
			},
			wantErr: "runtime.sideEffects.attemptTimeout must be greater than zero",
		},
		{
			name: "runtime execution pool min idle exceeds max",
			mutate: func(payload map[string]any) {
				payload["database"].(map[string]any)["pools"].(map[string]any)["runtimeExecution"].(map[string]any)["minIdleConns"] = 15
			},
			wantErr: "lane=runtime_execution min_idle_conns must be less than or equal to max_conns",
		},
		{
			name: "management admission m3 exceeds m2",
			mutate: func(payload map[string]any) {
				payload["database"].(map[string]any)["managementAdmission"].(map[string]any)["m3MaxConcurrent"] = 11
			},
			wantErr: "database.managementAdmission.m3MaxConcurrent must be less than or equal to database.managementAdmission.m2MaxConcurrent",
		},
		{
			name: "enabled mail requires from address",
			mutate: func(payload map[string]any) {
				payload["mail"] = map[string]any{"enabled": true, "smtp": validBootstrapMailSMTPPayload()}
			},
			wantErr: "mail.from is required",
		},
		{
			name: "enabled mail requires smtp host",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				delete(mailPayload["smtp"].(map[string]any), "host")
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.host is required",
		},
		{
			name: "enabled mail rejects invalid port",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				mailPayload["smtp"].(map[string]any)["port"] = 70000
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.port must be between 1 and 65535",
		},
		{
			name: "enabled mail rejects invalid mode",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				mailPayload["smtp"].(map[string]any)["mode"] = "opportunistic"
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.mode must be one of",
		},
		{
			name: "enabled mail rejects invalid timeout",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				mailPayload["smtp"].(map[string]any)["timeout"] = "slow"
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.timeout must parse as a Go duration",
		},
		{
			name: "enabled mail rejects invalid auth mode",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				mailPayload["smtp"].(map[string]any)["auth"] = "login"
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.auth must be one of",
		},
		{
			name: "enabled mail plain auth requires username",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				delete(mailPayload["smtp"].(map[string]any), "username")
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.username is required",
		},
		{
			name: "enabled mail plain auth requires password source",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				delete(mailPayload["smtp"].(map[string]any), "password")
				delete(mailPayload["smtp"].(map[string]any), "passwordFile")
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.password or mail.smtp.passwordFile is required when mail.smtp.auth is plain",
		},
		{
			name: "enabled mail rejects plaintext remote host",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				mailPayload["smtp"].(map[string]any)["mode"] = "plaintext_local_only"
				payload["mail"] = mailPayload
			},
			wantErr: "plaintext_local_only requires a localhost or loopback host",
		},
		{
			name: "enabled mail rejects conflicting password sources",
			mutate: func(payload map[string]any) {
				mailPayload := validBootstrapMailPayload()
				mailPayload["smtp"].(map[string]any)["passwordFile"] = "/run/secrets/smtp-password"
				payload["mail"] = mailPayload
			},
			wantErr: "mail.smtp.password and mail.smtp.passwordFile are mutually exclusive",
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
			if strings.Contains(err.Error(), "smtp-secret") {
				t.Fatalf("validation error exposed SMTP password: %v", err)
			}
		})
	}

	t.Run("custom positive request timeout maps to settings", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["runtime"].(map[string]any)["transport"].(map[string]any)["requestTimeout"] = "17s"
		settings, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err != nil {
			t.Fatalf("parse custom request timeout: %v", err)
		}
		if got := settings.RuntimeTransport().RequestTimeout; got != 17*time.Second {
			t.Fatalf("expected custom request timeout 17s, got %v", got)
		}
	})

	t.Run("custom positive side effects attempt timeout maps to settings", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["runtime"].(map[string]any)["sideEffects"].(map[string]any)["attemptTimeout"] = "19s"
		settings, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err != nil {
			t.Fatalf("parse custom side effects attempt timeout: %v", err)
		}
		if got := settings.RuntimeSideEffects().AttemptTimeout; got != 19*time.Second {
			t.Fatalf("expected custom side effects attempt timeout 19s, got %v", got)
		}
	})
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
		if transport.RequestTimeout != 60*time.Second || transport.IdleConnTimeout != 90*time.Second || transport.ResponseHeaderTimeout != 0 || transport.TLSHandshakeTimeout != 10*time.Second || transport.ExpectContinueTimeout != time.Second {
			t.Fatalf("unexpected runtime transport timeouts: %+v", transport)
		}
		if got := settings.RuntimeSideEffects(); got.AttemptTimeout != 10*time.Second {
			t.Fatalf("unexpected runtime side effects settings: %+v", got)
		}
		if got := settings.ManagementDatabaseBudget(); got.MaxConns != 6 || got.MinIdleConns != 0 {
			t.Fatalf("unexpected management database budget: %+v", got)
		}
		if got := settings.RuntimeDatabaseBudget(); got.MaxConns != 14 || got.MinIdleConns != 1 {
			t.Fatalf("unexpected runtime execution database budget: %+v", got)
		}
		if got := settings.ManagementAdmissionBudget(); got.M2MaxConcurrent != 5 || got.M3MaxConcurrent != 5 {
			t.Fatalf("unexpected management admission budget: %+v", got)
		}
		if got := settings.CORSAllowedOriginsList(); len(got) != 2 || got[0] != "http://localhost:15173" || got[1] != "http://127.0.0.1:15173" {
			t.Fatalf("unexpected CORS origins: %+v", got)
		}
	})
}

func TestBootstrapConfigMailMapping(t *testing.T) {
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})

	t.Run("disabled mail block is valid without smtp transport", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["mail"] = map[string]any{"enabled": false}
		settings, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err != nil {
			t.Fatalf("parse disabled mail block: %v", err)
		}
		if settings.Mail.Enabled {
			t.Fatal("expected explicit disabled mail to stay disabled")
		}
	})

	t.Run("disabled mail block does not require smtp fields", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["mail"] = map[string]any{"enabled": false, "smtp": map[string]any{}}
		settings, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err != nil {
			t.Fatalf("parse disabled mail block with empty smtp: %v", err)
		}
		if settings.Mail.Enabled {
			t.Fatal("expected disabled mail with empty smtp to stay disabled")
		}
	})

	t.Run("enabled smtp mail maps to settings", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		payload["mail"] = validBootstrapMailPayload()
		settings, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err != nil {
			t.Fatalf("parse enabled mail block: %v", err)
		}
		if !settings.Mail.Enabled || settings.Mail.From != "Prism <noreply@example.com>" || settings.Mail.SMTP.Host != "smtp.example.com" {
			t.Fatalf("unexpected mail settings: %+v", settings.Mail)
		}
		if settings.Mail.SMTP.Password != "smtp-secret" || settings.Mail.SMTP.Timeout != 15*time.Second {
			t.Fatalf("unexpected smtp secret or timeout mapping: %+v", settings.Mail.SMTP)
		}
	})

	t.Run("enabled smtp mail supports auth none without credentials", func(t *testing.T) {
		payload := loadBootstrapFixture(t, "bootstrap-valid-v1.json")
		mailPayload := validBootstrapMailPayload()
		smtpPayload := mailPayload["smtp"].(map[string]any)
		smtpPayload["auth"] = "none"
		delete(smtpPayload, "username")
		delete(smtpPayload, "password")
		delete(smtpPayload, "passwordFile")
		payload["mail"] = mailPayload
		settings, err := manager.Parse(mustMarshalBootstrapFixture(t, payload))
		if err != nil {
			t.Fatalf("parse enabled mail block with auth none: %v", err)
		}
		if !settings.Mail.Enabled || settings.Mail.SMTP.Auth != config.MailSMTPAuthNone {
			t.Fatalf("expected enabled auth=none mail settings, got %+v", settings.Mail)
		}
		if settings.Mail.SMTP.Username != "" || settings.Mail.SMTP.Password != "" || settings.Mail.SMTP.PasswordFile != "" {
			t.Fatalf("expected auth=none to omit credentials, got %+v", settings.Mail.SMTP)
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

func assertContractDisabledSafeMailValues(t *testing.T, values *config.BootstrapConfigMailValues) {
	t.Helper()
	if values == nil || values.Enabled == nil || *values.Enabled {
		t.Fatalf("expected explicit disabled safe mail values, got %+v", values)
	}
	if values.From != nil || values.ReplyTo != nil || values.SMTP != nil {
		t.Fatalf("expected legacy no-mail safe values to omit from/reply_to/smtp, got %+v", values)
	}
}

func validBootstrapMailPayload() map[string]any {
	return map[string]any{
		"enabled": true,
		"from":    "Prism <noreply@example.com>",
		"replyTo": "support@example.com",
		"smtp":    validBootstrapMailSMTPPayload(),
	}
}

func validBootstrapMailSMTPPayload() map[string]any {
	return map[string]any{
		"host":          "smtp.example.com",
		"port":          587,
		"mode":          "starttls_required",
		"ehloHostname":  "prism.example.com",
		"auth":          "plain",
		"username":      "smtp-user",
		"password":      "smtp-secret",
		"timeout":       "15s",
		"tlsServerName": "smtp.example.com",
	}
}

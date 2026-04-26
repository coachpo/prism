package integration_test

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
	bootstrapSeedMasterKey      = "phase3-bootstrap-master-key"
	bootstrapFixtureMasterKey   = "bootstrap-contract-master-key"
	bootstrapFixtureDatabaseURL = "postgres://prism:bootstrap-password@db.internal:5432/prism?sslmode=disable"
	bootstrapFixtureJWTSecret   = "bootstrap-jwt-signing-secret"
	bootstrapFixtureBundleKey   = "bootstrap-bundle-encryption-key"
)

type bootstrapSeededFile struct {
	Meta struct {
		SchemaVersion int    `json:"schemaVersion"`
		Revision      int    `json:"revision"`
		CreatedAt     string `json:"createdAt"`
		UpdatedAt     string `json:"updatedAt"`
	} `json:"meta"`
	Server struct {
		Host        string `json:"host"`
		Port        int    `json:"port"`
		DocsEnabled bool   `json:"docsEnabled"`
	} `json:"server"`
	Database struct {
		URLSecretRef string `json:"urlSecretRef"`
	} `json:"database"`
	Auth struct {
		JWTSigningKeySecretRef string `json:"jwtSigningKeySecretRef"`
	} `json:"auth"`
	StateTransfer struct {
		BundleEncryptionKeySecretRef *string `json:"bundleEncryptionKeySecretRef"`
	} `json:"stateTransfer"`
	SecretPayload struct {
		Kind    string `json:"kind"`
		Cipher  string `json:"cipher"`
		KeyID   string `json:"keyId"`
		Entries []struct {
			Ref        string `json:"ref"`
			Ciphertext string `json:"ciphertext"`
		} `json:"entries"`
	} `json:"secretPayload"`
}

func TestBootstrapConfigFileIsSeededFromLegacyEnv(t *testing.T) {
	seededPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	legacyDatabaseURL := "postgres://prism:legacy-password@db.seed.internal:5432/prism?sslmode=disable"
	legacyJWTSecret := "legacy-seeded-jwt-secret"
	legacyBundleKey := "legacy-seeded-bundle-key"
	legacySecretEncryptionKey := "legacy-seeded-secret-encryption-key"
	seededAt := time.Date(2026, 4, 26, 16, 15, 0, 0, time.UTC)

	setBootstrapSeedLegacyEnv(t, map[string]string{
		config.BootstrapConfigPathEnv:      seededPath,
		config.BootstrapConfigMasterKeyEnv: bootstrapSeedMasterKey,
		"HOST":                             "127.0.0.1",
		"PORT":                             "19090",
		"APP_ENV":                          "production",
		"DATABASE_URL":                     legacyDatabaseURL,
		"RUNTIME_TELEMETRY_MODE":           "synchronous",
		"RUNTIME_BUFFERING_MODE":           "streaming",
		"RUNTIME_TRANSPORT_MAX_IDLE_CONNS": "77",
		"RUNTIME_TRANSPORT_MAX_IDLE_CONNS_PER_HOST": "11",
		"RUNTIME_TRANSPORT_MAX_CONNS_PER_HOST":      "17",
		"RUNTIME_TRANSPORT_IDLE_CONN_TIMEOUT":       "35s",
		"RUNTIME_TRANSPORT_RESPONSE_HEADER_TIMEOUT": "7s",
		"RUNTIME_TRANSPORT_TLS_HANDSHAKE_TIMEOUT":   "9s",
		"RUNTIME_TRANSPORT_EXPECT_CONTINUE_TIMEOUT": "2s",
		"RUNTIME_DB_MAX_CONNS":                      "5",
		"RUNTIME_DB_MIN_IDLE_CONNS":                 "2",
		"MANAGEMENT_DB_MAX_CONNS":                   "14",
		"MANAGEMENT_DB_MIN_IDLE_CONNS":              "3",
		"MANAGEMENT_ADMISSION_M2_MAX_CONCURRENT":    "8",
		"MANAGEMENT_ADMISSION_M3_MAX_CONCURRENT":    "4",
		"SECRET_ENCRYPTION_KEY":                     legacySecretEncryptionKey,
		"CONFIG_BUNDLE_ENCRYPTION_KEY":              legacyBundleKey,
		"CORS_ALLOWED_ORIGINS":                      "http://localhost:15173,http://127.0.0.1:15173",
		"AUTH_JWT_SECRET":                           legacyJWTSecret,
		"AUTH_ACCESS_TOKEN_TTL_SECONDS":             "1200",
		"AUTH_REFRESH_TOKEN_TTL_SECONDS":            "7200",
		"AUTH_RESET_CODE_TTL_SECONDS":               "300",
		"AUTH_COOKIE_NAME":                          "prism_seed_access",
		"AUTH_REFRESH_COOKIE_NAME":                  "prism_seed_refresh",
		"AUTH_COOKIE_SECURE":                        "true",
	})

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return seededAt },
	})

	settings, err := manager.LoadOrSeedFromEnv()
	if err != nil {
		t.Fatalf("seed bootstrap config from legacy env: %v", err)
	}
	if settings.Host != "127.0.0.1" || settings.Port != 19090 {
		t.Fatalf("unexpected seeded server settings: %+v", settings)
	}
	if settings.DocsEnabled() {
		t.Fatal("expected seeded bootstrap config to disable docs for production APP_ENV")
	}
	if settings.DatabaseURL != legacyDatabaseURL {
		t.Fatalf("expected seeded database URL %q, got %q", legacyDatabaseURL, settings.DatabaseURL)
	}
	if settings.SecretEncryptionKey != bootstrapSeedMasterKey {
		t.Fatalf("expected seeded secret encryption key %q, got %q", bootstrapSeedMasterKey, settings.SecretEncryptionKey)
	}
	if settings.ConfigBundleEncryptionKey != legacyBundleKey || settings.AuthJWTSecret != legacyJWTSecret {
		t.Fatalf("unexpected seeded secret-backed settings: bundle=%q jwt=%q", settings.ConfigBundleEncryptionKey, settings.AuthJWTSecret)
	}
	if settings.RuntimeTelemetryMode != config.RuntimeTelemetryModeDurableOutbox || settings.RuntimeBufferingMode != config.RuntimeBufferingModeStreaming {
		t.Fatalf("unexpected seeded runtime modes: telemetry=%q buffering=%q", settings.RuntimeTelemetryMode, settings.RuntimeBufferingMode)
	}
	transport := settings.RuntimeTransport()
	if transport.MaxIdleConns != 77 || transport.MaxIdleConnsPerHost != 11 || transport.MaxConnsPerHost != 17 {
		t.Fatalf("unexpected seeded runtime transport pool: %+v", transport)
	}
	if transport.IdleConnTimeout != 35*time.Second || transport.ResponseHeaderTimeout != 7*time.Second || transport.TLSHandshakeTimeout != 9*time.Second || transport.ExpectContinueTimeout != 2*time.Second {
		t.Fatalf("unexpected seeded runtime transport timeouts: %+v", transport)
	}
	if got := settings.RuntimeDatabaseBudget(); got.MaxConns != 5 || got.MinIdleConns != 2 {
		t.Fatalf("unexpected seeded runtime DB budget: %+v", got)
	}
	if got := settings.ManagementDatabaseBudget(); got.MaxConns != 14 || got.MinIdleConns != 3 {
		t.Fatalf("unexpected seeded management DB budget: %+v", got)
	}
	if got := settings.ManagementAdmissionBudget(); got.M2MaxConcurrent != 8 || got.M3MaxConcurrent != 4 {
		t.Fatalf("unexpected seeded management admission budget: %+v", got)
	}
	if got := settings.CORSAllowedOriginsList(); len(got) != 2 || got[0] != "http://localhost:15173" || got[1] != "http://127.0.0.1:15173" {
		t.Fatalf("unexpected seeded CORS origins: %+v", got)
	}
	if settings.AuthAccessTokenTTLSeconds != 1200 || settings.AuthRefreshTokenTTLSeconds != 7200 || settings.AuthResetCodeTTLSeconds != 300 {
		t.Fatalf("unexpected seeded auth TTLs: %+v", settings)
	}
	if settings.AuthCookieName != "prism_seed_access" || settings.AuthRefreshCookieName != "prism_seed_refresh" || !settings.AuthCookieSecure {
		t.Fatalf("unexpected seeded auth cookies: %+v", settings)
	}

	raw, err := os.ReadFile(seededPath)
	if err != nil {
		t.Fatalf("read seeded bootstrap config file: %v", err)
	}
	for _, secret := range []string{legacyDatabaseURL, legacyJWTSecret, legacyBundleKey, legacySecretEncryptionKey, bootstrapSeedMasterKey} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("expected seeded bootstrap config to omit plaintext secret %q", secret)
		}
	}
	assertBootstrapTopLevelKeys(t, raw)

	var seeded bootstrapSeededFile
	if err := json.Unmarshal(raw, &seeded); err != nil {
		t.Fatalf("decode seeded bootstrap config JSON: %v", err)
	}
	if seeded.Meta.SchemaVersion != 1 || seeded.Meta.Revision != 1 || seeded.Meta.CreatedAt != seededAt.Format(time.RFC3339) || seeded.Meta.UpdatedAt != seededAt.Format(time.RFC3339) {
		t.Fatalf("unexpected seeded meta payload: %+v", seeded.Meta)
	}
	if seeded.Server.Host != "127.0.0.1" || seeded.Server.Port != 19090 || seeded.Server.DocsEnabled {
		t.Fatalf("unexpected seeded server payload: %+v", seeded.Server)
	}
	if seeded.Database.URLSecretRef != "database:primary:url" || seeded.Auth.JWTSigningKeySecretRef != "auth:jwt:signing-key" {
		t.Fatalf("unexpected seeded secret refs: database=%q auth=%q", seeded.Database.URLSecretRef, seeded.Auth.JWTSigningKeySecretRef)
	}
	if seeded.StateTransfer.BundleEncryptionKeySecretRef == nil || *seeded.StateTransfer.BundleEncryptionKeySecretRef != "state-transfer:bundle-encryption-key" {
		t.Fatalf("unexpected seeded bundle secret ref: %+v", seeded.StateTransfer.BundleEncryptionKeySecretRef)
	}
	if seeded.SecretPayload.Kind != "encrypted" || seeded.SecretPayload.Cipher != "prism-bootstrap-v1" || !strings.HasPrefix(seeded.SecretPayload.KeyID, "sha256:") {
		t.Fatalf("unexpected seeded secret payload header: %+v", seeded.SecretPayload)
	}
	if len(seeded.SecretPayload.Entries) != 3 {
		t.Fatalf("expected three encrypted secret payload entries, got %d", len(seeded.SecretPayload.Entries))
	}
	expectedRefs := map[string]struct{}{
		"database:primary:url":                 {},
		"auth:jwt:signing-key":                 {},
		"state-transfer:bundle-encryption-key": {},
	}
	for _, entry := range seeded.SecretPayload.Entries {
		if !strings.HasPrefix(entry.Ciphertext, "enc:") {
			t.Fatalf("expected seeded ciphertext for ref %q to stay encrypted, got %q", entry.Ref, entry.Ciphertext)
		}
		if _, ok := expectedRefs[entry.Ref]; !ok {
			t.Fatalf("unexpected seeded secret ref %q", entry.Ref)
		}
	}

	loadedAgain, err := manager.Load(seededPath, bootstrapSeedMasterKey)
	if err != nil {
		t.Fatalf("load seeded bootstrap config file: %v", err)
	}
	if loadedAgain.DatabaseURL != settings.DatabaseURL || loadedAgain.AuthJWTSecret != settings.AuthJWTSecret || loadedAgain.ConfigBundleEncryptionKey != settings.ConfigBundleEncryptionKey {
		t.Fatalf("expected seeded file to round-trip through bootstrap parser, got %+v", loadedAgain)
	}
}

func TestBootstrapConfigFileWinsWhenPresent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	original := loadIntegrationBootstrapFixtureBytes(t, "bootstrap-valid-v1.json")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatalf("write existing bootstrap fixture: %v", err)
	}

	setBootstrapSeedLegacyEnv(t, map[string]string{
		config.BootstrapConfigPathEnv:      configPath,
		config.BootstrapConfigMasterKeyEnv: bootstrapFixtureMasterKey,
		"PORT":                             "0",
		"DATABASE_URL":                     "",
		"CORS_ALLOWED_ORIGINS":             "not-a-uri",
		"AUTH_JWT_SECRET":                  "",
		"SECRET_ENCRYPTION_KEY":            "ignored-legacy-secret-key",
		"CONFIG_BUNDLE_ENCRYPTION_KEY":     "ignored-legacy-bundle-key",
	})

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	settings, err := manager.LoadOrSeedFromEnv()
	if err != nil {
		t.Fatalf("load existing bootstrap config file: %v", err)
	}
	if settings.Host != "127.0.0.1" || settings.Port != 18000 || !settings.DocsEnabled() {
		t.Fatalf("expected existing bootstrap file server settings to win, got %+v", settings)
	}
	if settings.DatabaseURL != bootstrapFixtureDatabaseURL || settings.AuthJWTSecret != bootstrapFixtureJWTSecret {
		t.Fatalf("expected existing bootstrap secrets to win, got database=%q jwt=%q", settings.DatabaseURL, settings.AuthJWTSecret)
	}
	if settings.SecretEncryptionKey != bootstrapFixtureMasterKey || settings.ConfigBundleEncryptionKey != bootstrapFixtureBundleKey {
		t.Fatalf("expected existing bootstrap encryption settings to win, got secret=%q bundle=%q", settings.SecretEncryptionKey, settings.ConfigBundleEncryptionKey)
	}
	if settings.RuntimeBufferingMode != config.RuntimeBufferingModeStreaming {
		t.Fatalf("expected existing bootstrap runtime buffering mode to win, got %q", settings.RuntimeBufferingMode)
	}
	if got := settings.CORSAllowedOriginsList(); len(got) != 2 || got[0] != "http://localhost:15173" || got[1] != "http://127.0.0.1:15173" {
		t.Fatalf("unexpected existing bootstrap CORS origins: %+v", got)
	}

	rawAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read bootstrap config after precedence load: %v", err)
	}
	if !bytes.Equal(original, rawAfter) {
		t.Fatal("expected existing bootstrap config file to remain unchanged when it already exists")
	}
}

func setBootstrapSeedLegacyEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for name, value := range values {
		t.Setenv(name, value)
	}
}

func assertBootstrapTopLevelKeys(t *testing.T, raw []byte) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode bootstrap top-level JSON: %v", err)
	}
	required := []string{"meta", "server", "database", "runtime", "http", "auth", "stateTransfer", "secretPayload"}
	if len(payload) != len(required) {
		t.Fatalf("expected seeded bootstrap config top-level keys %v, got %v", required, keysFromPayload(payload))
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected seeded bootstrap config top-level key %q", key)
		}
	}
	for _, forbidden := range []string{"profiles", "vendors", "models", "endpoints", "connections", "settings", "state"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("expected seeded bootstrap config to omit state section %q", forbidden)
		}
	}
}

func keysFromPayload(payload map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	return keys
}

func loadIntegrationBootstrapFixtureBytes(t *testing.T, fileName string) []byte {
	t.Helper()
	raw, err := os.ReadFile(integrationBootstrapFixturePath(t, fileName))
	if err != nil {
		t.Fatalf("read bootstrap fixture %s: %v", fileName, err)
	}
	return raw
}

func integrationBootstrapFixturePath(t *testing.T, fileName string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration bootstrap fixture path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "bootstrap", fileName))
}

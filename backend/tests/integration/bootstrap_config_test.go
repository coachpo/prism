package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

const (
	bootstrapFixtureDatabaseURL = "postgres://prism:bootstrap-password@db.internal:5432/prism?sslmode=disable"
	bootstrapFixtureSecretKey   = "bootstrap-runtime-secret-encryption-key"
	bootstrapFixtureJWTSecret   = "bootstrap-jwt-signing-secret"
	bootstrapFixtureBundleKey   = "bootstrap-bundle-encryption-key"
	bootstrapSeedDefaultSecret  = "prism-dev-runtime-secret-change-me"
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
		URL         string `json:"url"`
		RuntimePool struct {
			MaxConns     int `json:"maxConns"`
			MinIdleConns int `json:"minIdleConns"`
		} `json:"runtimePool"`
		ManagementPool struct {
			MaxConns     int `json:"maxConns"`
			MinIdleConns int `json:"minIdleConns"`
		} `json:"managementPool"`
		ManagementAdmission struct {
			M2MaxConcurrent int `json:"m2MaxConcurrent"`
			M3MaxConcurrent int `json:"m3MaxConcurrent"`
		} `json:"managementAdmission"`
	} `json:"database"`
	Runtime struct {
		BufferingMode       string `json:"bufferingMode"`
		SecretEncryptionKey string `json:"secretEncryptionKey"`
		Transport           struct {
			MaxIdleConns          int    `json:"maxIdleConns"`
			MaxIdleConnsPerHost   int    `json:"maxIdleConnsPerHost"`
			MaxConnsPerHost       int    `json:"maxConnsPerHost"`
			IdleConnTimeout       string `json:"idleConnTimeout"`
			ResponseHeaderTimeout string `json:"responseHeaderTimeout"`
			TLSHandshakeTimeout   string `json:"tlsHandshakeTimeout"`
			ExpectContinueTimeout string `json:"expectContinueTimeout"`
		} `json:"transport"`
	} `json:"runtime"`
	HTTP struct {
		CORSAllowedOrigins []string `json:"corsAllowedOrigins"`
	} `json:"http"`
	Auth struct {
		JWTSigningKey          string `json:"jwtSigningKey"`
		AccessTokenTTLSeconds  int    `json:"accessTokenTtlSeconds"`
		RefreshTokenTTLSeconds int    `json:"refreshTokenTtlSeconds"`
		ResetCodeTTLSeconds    int    `json:"resetCodeTtlSeconds"`
		AccessCookieName       string `json:"accessCookieName"`
		RefreshCookieName      string `json:"refreshCookieName"`
		CookieSecure           bool   `json:"cookieSecure"`
	} `json:"auth"`
	StateTransfer struct {
		BundleEncryptionKey string `json:"bundleEncryptionKey"`
	} `json:"stateTransfer"`
}

func TestBootstrapConfigFileIsSeededFromEnv(t *testing.T) {
	seededPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	legacyDatabaseURL := "postgres://prism:legacy-password@db.seed.internal:5432/prism?sslmode=disable"
	legacyJWTSecret := "legacy-seeded-jwt-secret"
	legacyBundleKey := "legacy-seeded-bundle-key"
	seededAt := time.Date(2026, 4, 26, 16, 15, 0, 0, time.UTC)

	setBootstrapSeedEnv(t, map[string]string{
		config.BootstrapConfigPathEnv:      seededPath,
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
	if settings.SecretEncryptionKey != bootstrapSeedDefaultSecret {
		t.Fatalf("expected seeded secret encryption key %q, got %q", bootstrapSeedDefaultSecret, settings.SecretEncryptionKey)
	}
	if settings.ConfigBundleEncryptionKey != legacyBundleKey || settings.AuthJWTSecret != legacyJWTSecret {
		t.Fatalf("unexpected seeded plaintext settings: bundle=%q jwt=%q", settings.ConfigBundleEncryptionKey, settings.AuthJWTSecret)
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
	for _, secret := range []string{legacyDatabaseURL, legacyJWTSecret, legacyBundleKey, bootstrapSeedDefaultSecret} {
		if !bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("expected seeded bootstrap config to keep plaintext value %q", secret)
		}
	}
	for _, forbidden := range []string{"secretPayload", "urlSecretRef", "jwtSigningKeySecretRef", "bundleEncryptionKeySecretRef", "enc:"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("expected seeded bootstrap config to omit legacy encrypted marker %q", forbidden)
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
	if seeded.Database.URL != legacyDatabaseURL {
		t.Fatalf("unexpected seeded database URL: %q", seeded.Database.URL)
	}
	if seeded.Database.RuntimePool.MaxConns != 5 || seeded.Database.RuntimePool.MinIdleConns != 2 {
		t.Fatalf("unexpected seeded runtime DB payload: %+v", seeded.Database.RuntimePool)
	}
	if seeded.Database.ManagementPool.MaxConns != 14 || seeded.Database.ManagementPool.MinIdleConns != 3 {
		t.Fatalf("unexpected seeded management DB payload: %+v", seeded.Database.ManagementPool)
	}
	if seeded.Database.ManagementAdmission.M2MaxConcurrent != 8 || seeded.Database.ManagementAdmission.M3MaxConcurrent != 4 {
		t.Fatalf("unexpected seeded admission payload: %+v", seeded.Database.ManagementAdmission)
	}
	if seeded.Runtime.BufferingMode != "streaming" || seeded.Runtime.SecretEncryptionKey != bootstrapSeedDefaultSecret {
		t.Fatalf("unexpected seeded runtime payload: %+v", seeded.Runtime)
	}
	if seeded.Runtime.Transport.MaxIdleConns != 77 || seeded.Runtime.Transport.MaxIdleConnsPerHost != 11 || seeded.Runtime.Transport.MaxConnsPerHost != 17 {
		t.Fatalf("unexpected seeded runtime transport payload: %+v", seeded.Runtime.Transport)
	}
	if seeded.Runtime.Transport.IdleConnTimeout != "35s" || seeded.Runtime.Transport.ResponseHeaderTimeout != "7s" || seeded.Runtime.Transport.TLSHandshakeTimeout != "9s" || seeded.Runtime.Transport.ExpectContinueTimeout != "2s" {
		t.Fatalf("unexpected seeded runtime transport payload: %+v", seeded.Runtime.Transport)
	}
	if len(seeded.HTTP.CORSAllowedOrigins) != 2 || seeded.HTTP.CORSAllowedOrigins[0] != "http://localhost:15173" || seeded.HTTP.CORSAllowedOrigins[1] != "http://127.0.0.1:15173" {
		t.Fatalf("unexpected seeded CORS payload: %+v", seeded.HTTP.CORSAllowedOrigins)
	}
	if seeded.Auth.JWTSigningKey != legacyJWTSecret || seeded.Auth.AccessTokenTTLSeconds != 1200 || seeded.Auth.RefreshTokenTTLSeconds != 7200 || seeded.Auth.ResetCodeTTLSeconds != 300 {
		t.Fatalf("unexpected seeded auth payload: %+v", seeded.Auth)
	}
	if seeded.Auth.AccessCookieName != "prism_seed_access" || seeded.Auth.RefreshCookieName != "prism_seed_refresh" || !seeded.Auth.CookieSecure {
		t.Fatalf("unexpected seeded auth payload: %+v", seeded.Auth)
	}
	if seeded.StateTransfer.BundleEncryptionKey != legacyBundleKey {
		t.Fatalf("unexpected seeded bundle key payload: %q", seeded.StateTransfer.BundleEncryptionKey)
	}

	loadedAgain, err := manager.Load(seededPath)
	if err != nil {
		t.Fatalf("load seeded bootstrap config file: %v", err)
	}
	if loadedAgain.DatabaseURL != settings.DatabaseURL || loadedAgain.AuthJWTSecret != settings.AuthJWTSecret || loadedAgain.ConfigBundleEncryptionKey != settings.ConfigBundleEncryptionKey || loadedAgain.SecretEncryptionKey != settings.SecretEncryptionKey {
		t.Fatalf("expected seeded file to round-trip through bootstrap parser, got %+v", loadedAgain)
	}
}

func TestBootstrapConfigFileWinsWhenPresent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	original := loadIntegrationBootstrapFixtureBytes(t, "bootstrap-valid-v1.json")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatalf("write existing bootstrap fixture: %v", err)
	}

	setBootstrapSeedEnv(t, map[string]string{
		config.BootstrapConfigPathEnv:  configPath,
		"PORT":                         "0",
		"DATABASE_URL":                 "",
		"CORS_ALLOWED_ORIGINS":         "not-a-uri",
		"AUTH_JWT_SECRET":              "",
		"CONFIG_BUNDLE_ENCRYPTION_KEY": "ignored-legacy-bundle-key",
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
		t.Fatalf("expected existing bootstrap plaintext values to win, got database=%q jwt=%q", settings.DatabaseURL, settings.AuthJWTSecret)
	}
	if settings.SecretEncryptionKey != bootstrapFixtureSecretKey || settings.ConfigBundleEncryptionKey != bootstrapFixtureBundleKey {
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

func TestEncryptedBootstrapConfigFileFailsFast(t *testing.T) {
	setBootstrapSeedEnv(t, map[string]string{
		config.BootstrapConfigPathEnv: integrationBootstrapFixturePath(t, "bootstrap-unsupported-encrypted-v1.json"),
		"DATABASE_URL":                "postgres://ignored:ignored@localhost:5432/ignored?sslmode=disable",
		"AUTH_JWT_SECRET":             "ignored-jwt-secret",
	})

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	_, err := manager.LoadOrSeedFromEnv()
	if err == nil {
		t.Fatal("expected legacy encrypted bootstrap file to fail")
	}
	if !strings.Contains(err.Error(), "unsupported legacy encrypted format fields") {
		t.Fatalf("expected unsupported legacy format error, got %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "master-key") {
		t.Fatalf("expected direct unsupported-format rejection instead of a retired master-key error, got %v", err)
	}
}

func setBootstrapSeedEnv(t *testing.T, values map[string]string) {
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
	required := []string{"meta", "server", "database", "runtime", "http", "auth", "stateTransfer"}
	if len(payload) != len(required) {
		t.Fatalf("expected seeded bootstrap config top-level keys %v, got %v", required, keysFromPayload(payload))
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected seeded bootstrap config top-level key %q", key)
		}
	}
	for _, forbidden := range []string{"secretPayload", "profiles", "vendors", "models", "endpoints", "connections", "settings", "state"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("expected seeded bootstrap config to omit section %q", forbidden)
		}
	}
}

func keysFromPayload(payload map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	slices.Sort(keys)
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

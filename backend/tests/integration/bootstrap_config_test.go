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
	bootstrapFixtureDatabaseURL      = "postgres://prism:prism@localhost:5432/prism?sslmode=disable"
	bootstrapFixtureSecretKey        = "prism-dev-runtime-secret-change-me"
	bootstrapFixtureJWTSecret        = "prism-dev-jwt-secret-change-me-2026"
	bootstrapFixtureBundleKey        = "prism-dev-runtime-secret-change-me"
	bootstrapSeedOverrideDatabaseURL = "postgres://prism:override-password@db.seed.internal:5432/prism?sslmode=disable"
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

func TestBootstrapConfigFileIsSeededFromCanonicalDefaults(t *testing.T) {
	seededPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	seededAt := time.Date(2026, 4, 26, 16, 15, 0, 0, time.UTC)

	setBootstrapSeedEnv(t, map[string]string{
		config.BootstrapConfigPathEnv: seededPath,
		"DATABASE_URL":                "",
	})

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return seededAt },
	})

	settings, err := manager.LoadOrSeedFromEnv()
	if err != nil {
		t.Fatalf("seed bootstrap config from canonical defaults: %v", err)
	}
	assertSeededBootstrapSettings(t, settings, bootstrapFixtureDatabaseURL)

	raw, err := os.ReadFile(seededPath)
	if err != nil {
		t.Fatalf("read seeded bootstrap config file: %v", err)
	}
	assertSeededBootstrapFile(t, raw, seededAt, bootstrapFixtureDatabaseURL)

	loadedAgain, err := manager.Load(seededPath)
	if err != nil {
		t.Fatalf("load seeded bootstrap config file: %v", err)
	}
	assertSeededBootstrapSettings(t, loadedAgain, bootstrapFixtureDatabaseURL)
}

func TestBootstrapConfigFileSeedingIgnoresDeletedLegacyEnvInputs(t *testing.T) {
	seededPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	seededAt := time.Date(2026, 4, 26, 16, 18, 0, 0, time.UTC)

	setBootstrapSeedEnv(t, map[string]string{
		config.BootstrapConfigPathEnv:      seededPath,
		"DATABASE_URL":                     "",
		"HOST":                             "127.0.0.1",
		"PORT":                             "19090",
		"APP_ENV":                          "production",
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
		"CORS_ALLOWED_ORIGINS":                      "https://legacy.example.invalid",
		"AUTH_JWT_SECRET":                           "legacy-seeded-jwt-secret",
		"AUTH_ACCESS_TOKEN_TTL_SECONDS":             "1200",
		"AUTH_REFRESH_TOKEN_TTL_SECONDS":            "7200",
		"AUTH_RESET_CODE_TTL_SECONDS":               "300",
		"AUTH_COOKIE_NAME":                          "prism_seed_access",
		"AUTH_REFRESH_COOKIE_NAME":                  "prism_seed_refresh",
		"AUTH_COOKIE_SECURE":                        "true",
		"CONFIG_BUNDLE_ENCRYPTION_KEY":              "legacy-seeded-bundle-key",
	})

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return seededAt },
	})

	settings, err := manager.LoadOrSeedFromEnv()
	if err != nil {
		t.Fatalf("seed bootstrap config while deleted legacy env inputs are set: %v", err)
	}
	assertSeededBootstrapSettings(t, settings, bootstrapFixtureDatabaseURL)

	raw, err := os.ReadFile(seededPath)
	if err != nil {
		t.Fatalf("read seeded bootstrap config file: %v", err)
	}
	assertSeededBootstrapFile(t, raw, seededAt, bootstrapFixtureDatabaseURL)

	loadedAgain, err := manager.Load(seededPath)
	if err != nil {
		t.Fatalf("load seeded bootstrap config file: %v", err)
	}
	assertSeededBootstrapSettings(t, loadedAgain, bootstrapFixtureDatabaseURL)
}

func TestBootstrapConfigFileSeedingAppliesDatabaseURLOverride(t *testing.T) {
	seededPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	seededAt := time.Date(2026, 4, 26, 16, 20, 0, 0, time.UTC)

	setBootstrapSeedEnv(t, map[string]string{
		config.BootstrapConfigPathEnv: seededPath,
		"DATABASE_URL":                bootstrapSeedOverrideDatabaseURL,
	})

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return seededAt },
	})

	settings, err := manager.LoadOrSeedFromEnv()
	if err != nil {
		t.Fatalf("seed bootstrap config from canonical defaults with DATABASE_URL override: %v", err)
	}
	assertSeededBootstrapSettings(t, settings, bootstrapSeedOverrideDatabaseURL)

	raw, err := os.ReadFile(seededPath)
	if err != nil {
		t.Fatalf("read seeded bootstrap config file: %v", err)
	}
	assertSeededBootstrapFile(t, raw, seededAt, bootstrapSeedOverrideDatabaseURL)

	loadedAgain, err := manager.Load(seededPath)
	if err != nil {
		t.Fatalf("load seeded bootstrap config file: %v", err)
	}
	assertSeededBootstrapSettings(t, loadedAgain, bootstrapSeedOverrideDatabaseURL)
}

func TestBootstrapConfigFileWinsWhenPresent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	original := loadIntegrationBootstrapFixtureBytes(t, "bootstrap-valid-v1.json")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatalf("write existing bootstrap fixture: %v", err)
	}

	setBootstrapSeedEnv(t, map[string]string{
		config.BootstrapConfigPathEnv: configPath,
		"DATABASE_URL":                bootstrapSeedOverrideDatabaseURL,
	})

	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{})
	settings, err := manager.LoadOrSeedFromEnv()
	if err != nil {
		t.Fatalf("load existing bootstrap config file: %v", err)
	}
	assertSeededBootstrapSettings(t, settings, bootstrapFixtureDatabaseURL)

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

func assertSeededBootstrapSettings(t *testing.T, settings config.Settings, wantDatabaseURL string) {
	t.Helper()
	if settings.Host != "0.0.0.0" || settings.Port != 18000 {
		t.Fatalf("unexpected seeded server settings: %+v", settings)
	}
	if settings.AppEnv != config.EnvironmentDevelopment || !settings.DocsEnabled() {
		t.Fatalf("unexpected seeded app environment: %q", settings.AppEnv)
	}
	if settings.DatabaseURL != wantDatabaseURL {
		t.Fatalf("expected seeded database URL %q, got %q", wantDatabaseURL, settings.DatabaseURL)
	}
	if settings.SecretEncryptionKey != bootstrapFixtureSecretKey || settings.ConfigBundleEncryptionKey != bootstrapFixtureBundleKey {
		t.Fatalf("unexpected seeded plaintext settings: secret=%q bundle=%q", settings.SecretEncryptionKey, settings.ConfigBundleEncryptionKey)
	}
	if settings.AuthJWTSecret != bootstrapFixtureJWTSecret {
		t.Fatalf("unexpected seeded auth JWT secret: %q", settings.AuthJWTSecret)
	}
	if settings.RuntimeTelemetryMode != config.RuntimeTelemetryModeDurableOutbox || settings.RuntimeBufferingMode != config.RuntimeBufferingModeBuffered {
		t.Fatalf("unexpected seeded runtime modes: telemetry=%q buffering=%q", settings.RuntimeTelemetryMode, settings.RuntimeBufferingMode)
	}
	transport := settings.RuntimeTransport()
	if transport.MaxIdleConns != 100 || transport.MaxIdleConnsPerHost != 8 || transport.MaxConnsPerHost != 0 {
		t.Fatalf("unexpected seeded runtime transport pool: %+v", transport)
	}
	if transport.IdleConnTimeout != 90*time.Second || transport.ResponseHeaderTimeout != 0 || transport.TLSHandshakeTimeout != 10*time.Second || transport.ExpectContinueTimeout != time.Second {
		t.Fatalf("unexpected seeded runtime transport timeouts: %+v", transport)
	}
	if got := settings.RuntimeDatabaseBudget(); got.MaxConns != 4 || got.MinIdleConns != 1 {
		t.Fatalf("unexpected seeded runtime DB budget: %+v", got)
	}
	if got := settings.ManagementDatabaseBudget(); got.MaxConns != 12 || got.MinIdleConns != 0 {
		t.Fatalf("unexpected seeded management DB budget: %+v", got)
	}
	if got := settings.ManagementAdmissionBudget(); got.M2MaxConcurrent != 10 || got.M3MaxConcurrent != 6 {
		t.Fatalf("unexpected seeded management admission budget: %+v", got)
	}
	if got := settings.CORSAllowedOriginsList(); len(got) != 2 || got[0] != "http://localhost:15173" || got[1] != "http://127.0.0.1:15173" {
		t.Fatalf("unexpected seeded CORS origins: %+v", got)
	}
	if settings.AuthAccessTokenTTLSeconds != 900 || settings.AuthRefreshTokenTTLSeconds != 604800 || settings.AuthResetCodeTTLSeconds != 600 {
		t.Fatalf("unexpected seeded auth TTLs: %+v", settings)
	}
	if settings.AuthCookieName != "prism_access_token" || settings.AuthRefreshCookieName != "prism_refresh_token" || settings.AuthCookieSecure {
		t.Fatalf("unexpected seeded auth cookies: %+v", settings)
	}
}

func assertSeededBootstrapFile(t *testing.T, raw []byte, seededAt time.Time, wantDatabaseURL string) {
	t.Helper()
	for _, secret := range []string{wantDatabaseURL, bootstrapFixtureJWTSecret, bootstrapFixtureSecretKey, bootstrapFixtureBundleKey} {
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
	if seeded.Server.Host != "0.0.0.0" || seeded.Server.Port != 18000 || !seeded.Server.DocsEnabled {
		t.Fatalf("unexpected seeded server payload: %+v", seeded.Server)
	}
	if seeded.Database.URL != wantDatabaseURL {
		t.Fatalf("unexpected seeded database URL: %q", seeded.Database.URL)
	}
	if seeded.Database.RuntimePool.MaxConns != 4 || seeded.Database.RuntimePool.MinIdleConns != 1 {
		t.Fatalf("unexpected seeded runtime DB payload: %+v", seeded.Database.RuntimePool)
	}
	if seeded.Database.ManagementPool.MaxConns != 12 || seeded.Database.ManagementPool.MinIdleConns != 0 {
		t.Fatalf("unexpected seeded management DB payload: %+v", seeded.Database.ManagementPool)
	}
	if seeded.Database.ManagementAdmission.M2MaxConcurrent != 10 || seeded.Database.ManagementAdmission.M3MaxConcurrent != 6 {
		t.Fatalf("unexpected seeded admission payload: %+v", seeded.Database.ManagementAdmission)
	}
	if seeded.Runtime.BufferingMode != "buffered" || seeded.Runtime.SecretEncryptionKey != bootstrapFixtureSecretKey {
		t.Fatalf("unexpected seeded runtime payload: %+v", seeded.Runtime)
	}
	if seeded.Runtime.Transport.MaxIdleConns != 100 || seeded.Runtime.Transport.MaxIdleConnsPerHost != 8 || seeded.Runtime.Transport.MaxConnsPerHost != 0 {
		t.Fatalf("unexpected seeded runtime transport payload: %+v", seeded.Runtime.Transport)
	}
	if seeded.Runtime.Transport.IdleConnTimeout != "1m30s" || seeded.Runtime.Transport.ResponseHeaderTimeout != "0s" || seeded.Runtime.Transport.TLSHandshakeTimeout != "10s" || seeded.Runtime.Transport.ExpectContinueTimeout != "1s" {
		t.Fatalf("unexpected seeded runtime transport payload: %+v", seeded.Runtime.Transport)
	}
	if len(seeded.HTTP.CORSAllowedOrigins) != 2 || seeded.HTTP.CORSAllowedOrigins[0] != "http://localhost:15173" || seeded.HTTP.CORSAllowedOrigins[1] != "http://127.0.0.1:15173" {
		t.Fatalf("unexpected seeded CORS payload: %+v", seeded.HTTP.CORSAllowedOrigins)
	}
	if seeded.Auth.JWTSigningKey != bootstrapFixtureJWTSecret || seeded.Auth.AccessTokenTTLSeconds != 900 || seeded.Auth.RefreshTokenTTLSeconds != 604800 || seeded.Auth.ResetCodeTTLSeconds != 600 {
		t.Fatalf("unexpected seeded auth payload: %+v", seeded.Auth)
	}
	if seeded.Auth.AccessCookieName != "prism_access_token" || seeded.Auth.RefreshCookieName != "prism_refresh_token" || seeded.Auth.CookieSecure {
		t.Fatalf("unexpected seeded auth payload: %+v", seeded.Auth)
	}
	if seeded.StateTransfer.BundleEncryptionKey != bootstrapFixtureBundleKey {
		t.Fatalf("unexpected seeded bundle key payload: %q", seeded.StateTransfer.BundleEncryptionKey)
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

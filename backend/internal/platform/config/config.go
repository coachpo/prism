package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Environment string

type RuntimeTelemetryMode string

type RuntimeBufferingMode string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentProduction  Environment = "production"
)

const (
	RuntimeTelemetryModeSynchronous   RuntimeTelemetryMode = "synchronous"
	RuntimeTelemetryModeDurableOutbox RuntimeTelemetryMode = "durable_outbox"
)

const (
	RuntimeBufferingModeBuffered  RuntimeBufferingMode = "buffered"
	RuntimeBufferingModeStreaming RuntimeBufferingMode = "streaming"
)

const defaultSeedSecretEncryptionKey = "prism-dev-runtime-secret-change-me-2026"

const (
	defaultRuntimeDatabaseMaxConns               int32 = 4
	defaultRuntimeDatabaseMinIdleConns           int32 = 1
	defaultManagementDatabaseMaxConns            int32 = 12
	defaultManagementDatabaseMinIdleConns        int32 = 0
	defaultManagementM2MaxConcurrent             int64 = 6
	defaultManagementM3MaxConcurrent             int64 = 2
	defaultRuntimeTransportMaxIdleConns                = 100
	defaultRuntimeTransportMaxIdleConnsPerHost         = 8
	defaultRuntimeTransportMaxConnsPerHost             = 0
	defaultRuntimeTransportIdleConnTimeout             = 90 * time.Second
	defaultRuntimeTransportResponseHeaderTimeout       = 0
	defaultRuntimeTransportTLSHandshakeTimeout         = 10 * time.Second
	defaultRuntimeTransportExpectContinueTimeout       = 1 * time.Second
)

type DatabasePoolBudget struct {
	MaxConns     int32
	MinIdleConns int32
}

type ManagementAdmissionBudget struct {
	M2MaxConcurrent int64
	M3MaxConcurrent int64
}

type RuntimeTransportConfig struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
	ResponseHeaderTimeout time.Duration
	TLSHandshakeTimeout   time.Duration
	ExpectContinueTimeout time.Duration
}

type Settings struct {
	Host                             string
	Port                             int
	AppEnv                           Environment
	DatabaseURL                      string
	RuntimeTelemetryMode             RuntimeTelemetryMode
	RuntimeBufferingMode             RuntimeBufferingMode
	RuntimeTransportConfig           RuntimeTransportConfig
	RuntimeDatabasePoolBudget        DatabasePoolBudget
	ManagementDatabasePoolBudget     DatabasePoolBudget
	ManagementAdmissionControlBudget ManagementAdmissionBudget
	SecretEncryptionKey              string
	ConfigBundleEncryptionKey        string
	CORSAllowedOrigins               string
	AuthJWTSecret                    string
	AuthAccessTokenTTLSeconds        int
	AuthRefreshTokenTTLSeconds       int
	AuthResetCodeTTLSeconds          int
	AuthCookieName                   string
	AuthRefreshCookieName            string
	AuthCookieSecure                 bool
}

func Load() Settings {
	return loadSeedSettingsFromEnv()
}

func loadSeedSettingsFromEnv() Settings {
	return Settings{
		Host:                             envOrDefault("HOST", "0.0.0.0"),
		Port:                             intEnvOrDefault("PORT", 8000),
		AppEnv:                           parseEnvironment(os.Getenv("APP_ENV")),
		DatabaseURL:                      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		RuntimeTelemetryMode:             loadRuntimeTelemetryModeFromEnv(RuntimeTelemetryModeDurableOutbox),
		RuntimeBufferingMode:             loadRuntimeBufferingModeFromEnv(RuntimeBufferingModeBuffered),
		RuntimeTransportConfig:           loadRuntimeTransportConfigFromEnv(defaultRuntimeTransportConfig()),
		RuntimeDatabasePoolBudget:        loadDatabasePoolBudgetFromEnv("RUNTIME_DB", DatabasePoolBudget{MaxConns: defaultRuntimeDatabaseMaxConns, MinIdleConns: defaultRuntimeDatabaseMinIdleConns}),
		ManagementDatabasePoolBudget:     loadDatabasePoolBudgetFromEnv("MANAGEMENT_DB", DatabasePoolBudget{MaxConns: defaultManagementDatabaseMaxConns, MinIdleConns: defaultManagementDatabaseMinIdleConns}),
		ManagementAdmissionControlBudget: loadManagementAdmissionBudgetFromEnv(ManagementAdmissionBudget{M2MaxConcurrent: defaultManagementM2MaxConcurrent, M3MaxConcurrent: defaultManagementM3MaxConcurrent}),
		SecretEncryptionKey:              defaultSeedSecretEncryptionKey,
		ConfigBundleEncryptionKey:        os.Getenv("CONFIG_BUNDLE_ENCRYPTION_KEY"),
		CORSAllowedOrigins:               envOrDefault("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173"),
		AuthJWTSecret:                    envOrDefault("AUTH_JWT_SECRET", "prism-dev-jwt-secret-change-me-2026"),
		AuthAccessTokenTTLSeconds:        intEnvOrDefault("AUTH_ACCESS_TOKEN_TTL_SECONDS", 900),
		AuthRefreshTokenTTLSeconds:       intEnvOrDefault("AUTH_REFRESH_TOKEN_TTL_SECONDS", 604800),
		AuthResetCodeTTLSeconds:          intEnvOrDefault("AUTH_RESET_CODE_TTL_SECONDS", 600),
		AuthCookieName:                   envOrDefault("AUTH_COOKIE_NAME", "prism_access_token"),
		AuthRefreshCookieName:            envOrDefault("AUTH_REFRESH_COOKIE_NAME", "prism_refresh_token"),
		AuthCookieSecure:                 boolEnvOrDefault("AUTH_COOKIE_SECURE", false),
	}
}

func (s Settings) ResolvedRuntimeTelemetryMode() RuntimeTelemetryMode {
	return normalizeRuntimeTelemetryMode(s.RuntimeTelemetryMode)
}

func (s Settings) ResolvedRuntimeBufferingMode() RuntimeBufferingMode {
	return normalizeRuntimeBufferingMode(s.RuntimeBufferingMode)
}

func (s Settings) RuntimeTransport() RuntimeTransportConfig {
	return normalizeRuntimeTransportConfig(s.RuntimeTransportConfig, defaultRuntimeTransportConfig())
}

func (s Settings) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

func (s Settings) RuntimeDatabaseBudget() DatabasePoolBudget {
	return normalizeDatabasePoolBudget(
		s.RuntimeDatabasePoolBudget,
		DatabasePoolBudget{MaxConns: defaultRuntimeDatabaseMaxConns, MinIdleConns: defaultRuntimeDatabaseMinIdleConns},
	)
}

func (s Settings) ManagementDatabaseBudget() DatabasePoolBudget {
	return normalizeDatabasePoolBudget(
		s.ManagementDatabasePoolBudget,
		DatabasePoolBudget{MaxConns: defaultManagementDatabaseMaxConns, MinIdleConns: defaultManagementDatabaseMinIdleConns},
	)
}

func (s Settings) ManagementAdmissionBudget() ManagementAdmissionBudget {
	defaultBudget := ManagementAdmissionBudget{M2MaxConcurrent: defaultManagementM2MaxConcurrent, M3MaxConcurrent: defaultManagementM3MaxConcurrent}
	maxLowerPriority := max(int64(s.ManagementDatabaseBudget().MaxConns)-1, int64(1))
	return normalizeManagementAdmissionBudget(s.ManagementAdmissionControlBudget, defaultBudget, maxLowerPriority)
}

func (s Settings) DocsEnabled() bool {
	return s.AppEnv != EnvironmentProduction
}

func (s Settings) CORSAllowedOriginsList() []string {
	parts := strings.Split(s.CORSAllowedOrigins, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		origins = append(origins, trimmed)
	}
	return origins
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	return value
}

func intEnvOrDefault(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func boolEnvOrDefault(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnvOrDefault(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func defaultRuntimeTransportConfig() RuntimeTransportConfig {
	return RuntimeTransportConfig{
		MaxIdleConns:          defaultRuntimeTransportMaxIdleConns,
		MaxIdleConnsPerHost:   defaultRuntimeTransportMaxIdleConnsPerHost,
		MaxConnsPerHost:       defaultRuntimeTransportMaxConnsPerHost,
		IdleConnTimeout:       defaultRuntimeTransportIdleConnTimeout,
		ResponseHeaderTimeout: defaultRuntimeTransportResponseHeaderTimeout,
		TLSHandshakeTimeout:   defaultRuntimeTransportTLSHandshakeTimeout,
		ExpectContinueTimeout: defaultRuntimeTransportExpectContinueTimeout,
	}
}

func loadDatabasePoolBudgetFromEnv(prefix string, defaults DatabasePoolBudget) DatabasePoolBudget {
	return normalizeDatabasePoolBudget(
		DatabasePoolBudget{
			MaxConns:     int32(intEnvOrDefault(prefix+"_MAX_CONNS", int(defaults.MaxConns))),
			MinIdleConns: int32(intEnvOrDefault(prefix+"_MIN_IDLE_CONNS", int(defaults.MinIdleConns))),
		},
		defaults,
	)
}

func loadManagementAdmissionBudgetFromEnv(defaults ManagementAdmissionBudget) ManagementAdmissionBudget {
	return normalizeManagementAdmissionBudget(
		ManagementAdmissionBudget{
			M2MaxConcurrent: int64(intEnvOrDefault("MANAGEMENT_ADMISSION_M2_MAX_CONCURRENT", int(defaults.M2MaxConcurrent))),
			M3MaxConcurrent: int64(intEnvOrDefault("MANAGEMENT_ADMISSION_M3_MAX_CONCURRENT", int(defaults.M3MaxConcurrent))),
		},
		defaults,
		0,
	)
}

func loadRuntimeTransportConfigFromEnv(defaults RuntimeTransportConfig) RuntimeTransportConfig {
	return normalizeRuntimeTransportConfig(
		RuntimeTransportConfig{
			MaxIdleConns:          intEnvOrDefault("RUNTIME_TRANSPORT_MAX_IDLE_CONNS", defaults.MaxIdleConns),
			MaxIdleConnsPerHost:   intEnvOrDefault("RUNTIME_TRANSPORT_MAX_IDLE_CONNS_PER_HOST", defaults.MaxIdleConnsPerHost),
			MaxConnsPerHost:       intEnvOrDefault("RUNTIME_TRANSPORT_MAX_CONNS_PER_HOST", defaults.MaxConnsPerHost),
			IdleConnTimeout:       durationEnvOrDefault("RUNTIME_TRANSPORT_IDLE_CONN_TIMEOUT", defaults.IdleConnTimeout),
			ResponseHeaderTimeout: durationEnvOrDefault("RUNTIME_TRANSPORT_RESPONSE_HEADER_TIMEOUT", defaults.ResponseHeaderTimeout),
			TLSHandshakeTimeout:   durationEnvOrDefault("RUNTIME_TRANSPORT_TLS_HANDSHAKE_TIMEOUT", defaults.TLSHandshakeTimeout),
			ExpectContinueTimeout: durationEnvOrDefault("RUNTIME_TRANSPORT_EXPECT_CONTINUE_TIMEOUT", defaults.ExpectContinueTimeout),
		},
		defaults,
	)
}

func loadRuntimeTelemetryModeFromEnv(fallback RuntimeTelemetryMode) RuntimeTelemetryMode {
	return normalizeRuntimeTelemetryMode(RuntimeTelemetryMode(envOrDefault("RUNTIME_TELEMETRY_MODE", string(fallback))))
}

func loadRuntimeBufferingModeFromEnv(fallback RuntimeBufferingMode) RuntimeBufferingMode {
	return normalizeRuntimeBufferingMode(RuntimeBufferingMode(envOrDefault("RUNTIME_BUFFERING_MODE", string(fallback))))
}

func normalizeDatabasePoolBudget(candidate DatabasePoolBudget, defaults DatabasePoolBudget) DatabasePoolBudget {
	normalized := candidate
	if normalized.MaxConns <= 0 {
		normalized.MaxConns = defaults.MaxConns
	}
	if normalized.MinIdleConns < 0 {
		normalized.MinIdleConns = defaults.MinIdleConns
	}
	if normalized.MinIdleConns > normalized.MaxConns {
		normalized.MinIdleConns = normalized.MaxConns
	}
	return normalized
}

func normalizeRuntimeTransportConfig(candidate RuntimeTransportConfig, defaults RuntimeTransportConfig) RuntimeTransportConfig {
	normalized := candidate
	if normalized.MaxIdleConns <= 0 {
		normalized.MaxIdleConns = defaults.MaxIdleConns
	}
	if normalized.MaxIdleConnsPerHost <= 0 {
		normalized.MaxIdleConnsPerHost = defaults.MaxIdleConnsPerHost
	}
	if normalized.MaxConnsPerHost < 0 {
		normalized.MaxConnsPerHost = defaults.MaxConnsPerHost
	}
	if normalized.MaxConnsPerHost > 0 && normalized.MaxIdleConnsPerHost > normalized.MaxConnsPerHost {
		normalized.MaxIdleConnsPerHost = normalized.MaxConnsPerHost
	}
	if normalized.MaxIdleConns < normalized.MaxIdleConnsPerHost {
		normalized.MaxIdleConns = normalized.MaxIdleConnsPerHost
	}
	if normalized.IdleConnTimeout <= 0 {
		normalized.IdleConnTimeout = defaults.IdleConnTimeout
	}
	if normalized.ResponseHeaderTimeout < 0 {
		normalized.ResponseHeaderTimeout = defaults.ResponseHeaderTimeout
	}
	if normalized.TLSHandshakeTimeout <= 0 {
		normalized.TLSHandshakeTimeout = defaults.TLSHandshakeTimeout
	}
	if normalized.ExpectContinueTimeout <= 0 {
		normalized.ExpectContinueTimeout = defaults.ExpectContinueTimeout
	}
	return normalized
}

func normalizeManagementAdmissionBudget(candidate ManagementAdmissionBudget, defaults ManagementAdmissionBudget, maxLowerPriority int64) ManagementAdmissionBudget {
	normalized := candidate
	if normalized.M2MaxConcurrent <= 0 {
		normalized.M2MaxConcurrent = defaults.M2MaxConcurrent
	}
	if normalized.M3MaxConcurrent <= 0 {
		normalized.M3MaxConcurrent = defaults.M3MaxConcurrent
	}
	if maxLowerPriority > 0 && normalized.M2MaxConcurrent > maxLowerPriority {
		normalized.M2MaxConcurrent = maxLowerPriority
	}
	if normalized.M3MaxConcurrent > normalized.M2MaxConcurrent {
		normalized.M3MaxConcurrent = normalized.M2MaxConcurrent
	}
	return normalized
}

func normalizeRuntimeTelemetryMode(candidate RuntimeTelemetryMode) RuntimeTelemetryMode {
	switch RuntimeTelemetryMode(strings.ToLower(strings.TrimSpace(string(candidate)))) {
	case RuntimeTelemetryModeDurableOutbox, RuntimeTelemetryModeSynchronous:
		return RuntimeTelemetryModeDurableOutbox
	default:
		return RuntimeTelemetryModeDurableOutbox
	}
}

func normalizeRuntimeBufferingMode(candidate RuntimeBufferingMode) RuntimeBufferingMode {
	switch RuntimeBufferingMode(strings.ToLower(strings.TrimSpace(string(candidate)))) {
	case RuntimeBufferingModeStreaming:
		return RuntimeBufferingModeStreaming
	case RuntimeBufferingModeBuffered:
		fallthrough
	default:
		return RuntimeBufferingModeBuffered
	}
}

func parseEnvironment(value string) Environment {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(EnvironmentDevelopment):
		return EnvironmentDevelopment
	case string(EnvironmentTest):
		return EnvironmentTest
	case string(EnvironmentProduction):
		return EnvironmentProduction
	default:
		return EnvironmentDevelopment
	}
}

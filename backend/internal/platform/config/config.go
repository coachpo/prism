package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentProduction  Environment = "production"
)

const (
	defaultRuntimeDatabaseMaxConns        int32 = 4
	defaultRuntimeDatabaseMinIdleConns    int32 = 1
	defaultManagementDatabaseMaxConns     int32 = 12
	defaultManagementDatabaseMinIdleConns int32 = 0
	defaultManagementM2MaxConcurrent      int64 = 6
	defaultManagementM3MaxConcurrent      int64 = 2
)

type DatabasePoolBudget struct {
	MaxConns     int32
	MinIdleConns int32
}

type ManagementAdmissionBudget struct {
	M2MaxConcurrent int64
	M3MaxConcurrent int64
}

type Settings struct {
	Host                             string
	Port                             int
	AppEnv                           Environment
	DatabaseURL                      string
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
	return Settings{
		Host:                             envOrDefault("HOST", "0.0.0.0"),
		Port:                             intEnvOrDefault("PORT", 8000),
		AppEnv:                           parseEnvironment(os.Getenv("APP_ENV")),
		DatabaseURL:                      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		RuntimeDatabasePoolBudget:        loadDatabasePoolBudgetFromEnv("RUNTIME_DB", DatabasePoolBudget{MaxConns: defaultRuntimeDatabaseMaxConns, MinIdleConns: defaultRuntimeDatabaseMinIdleConns}),
		ManagementDatabasePoolBudget:     loadDatabasePoolBudgetFromEnv("MANAGEMENT_DB", DatabasePoolBudget{MaxConns: defaultManagementDatabaseMaxConns, MinIdleConns: defaultManagementDatabaseMinIdleConns}),
		ManagementAdmissionControlBudget: loadManagementAdmissionBudgetFromEnv(ManagementAdmissionBudget{M2MaxConcurrent: defaultManagementM2MaxConcurrent, M3MaxConcurrent: defaultManagementM3MaxConcurrent}),
		SecretEncryptionKey:              os.Getenv("SECRET_ENCRYPTION_KEY"),
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

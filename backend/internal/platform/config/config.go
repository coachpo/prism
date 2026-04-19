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

type Settings struct {
	Host                       string
	Port                       int
	AppEnv                     Environment
	DatabaseURL                string
	SecretEncryptionKey        string
	ConfigBundleEncryptionKey  string
	CORSAllowedOrigins         string
	AuthJWTSecret              string
	AuthAccessTokenTTLSeconds  int
	AuthRefreshTokenTTLSeconds int
	AuthResetCodeTTLSeconds    int
	AuthCookieName             string
	AuthRefreshCookieName      string
	AuthCookieSecure           bool
}

func Load() Settings {
	return Settings{
		Host:                       envOrDefault("HOST", "0.0.0.0"),
		Port:                       intEnvOrDefault("PORT", 8000),
		AppEnv:                     parseEnvironment(os.Getenv("APP_ENV")),
		DatabaseURL:                strings.TrimSpace(os.Getenv("DATABASE_URL")),
		SecretEncryptionKey:        os.Getenv("SECRET_ENCRYPTION_KEY"),
		ConfigBundleEncryptionKey:  os.Getenv("CONFIG_BUNDLE_ENCRYPTION_KEY"),
		CORSAllowedOrigins:         envOrDefault("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173"),
		AuthJWTSecret:              envOrDefault("AUTH_JWT_SECRET", "prism-dev-jwt-secret-change-me-2026"),
		AuthAccessTokenTTLSeconds:  intEnvOrDefault("AUTH_ACCESS_TOKEN_TTL_SECONDS", 900),
		AuthRefreshTokenTTLSeconds: intEnvOrDefault("AUTH_REFRESH_TOKEN_TTL_SECONDS", 604800),
		AuthResetCodeTTLSeconds:    intEnvOrDefault("AUTH_RESET_CODE_TTL_SECONDS", 600),
		AuthCookieName:             envOrDefault("AUTH_COOKIE_NAME", "prism_access_token"),
		AuthRefreshCookieName:      envOrDefault("AUTH_REFRESH_COOKIE_NAME", "prism_refresh_token"),
		AuthCookieSecure:           boolEnvOrDefault("AUTH_COOKIE_SECURE", false),
	}
}

func (s Settings) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
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

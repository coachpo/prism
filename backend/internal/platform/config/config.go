package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Environment string

type RuntimeTelemetryMode string

type RuntimeBufferingMode string

type MailSMTPMode string

type MailSMTPAuth string

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

const (
	MailSMTPModeStartTLSRequired   MailSMTPMode = "starttls_required"
	MailSMTPModeImplicitTLS        MailSMTPMode = "implicit_tls"
	MailSMTPModePlaintextLocalOnly MailSMTPMode = "plaintext_local_only"
)

const (
	MailSMTPAuthNone  MailSMTPAuth = "none"
	MailSMTPAuthPlain MailSMTPAuth = "plain"
)

const (
	bootstrapDatabaseURLEnv            = "DATABASE_URL"
	defaultBootstrapHost               = "0.0.0.0"
	defaultBootstrapPort               = 18000
	defaultBootstrapDatabaseURL        = "postgres://prism:prism@localhost:5432/prism?sslmode=disable"
	defaultBootstrapCORSAllowedOrigins = "http://localhost:15173,http://127.0.0.1:15173"
	defaultSeedSecretEncryptionKey     = "prism-dev-runtime-secret-change-me"
	defaultAuthJWTSecret               = "prism-dev-jwt-secret-change-me"
	defaultAuthAccessTokenTTLSeconds   = 900
	defaultAuthRefreshTokenTTLSeconds  = 604800
	defaultAuthResetCodeTTLSeconds     = 600
	defaultAuthCookieName              = "prism_access_token"
	defaultAuthRefreshCookieName       = "prism_refresh_token"
	defaultMailSMTPTimeout             = 15 * time.Second
)

const (
	defaultRuntimeDatabaseMaxConns               int32 = 4
	defaultRuntimeDatabaseMinIdleConns           int32 = 1
	defaultManagementDatabaseMaxConns            int32 = 12
	defaultManagementDatabaseMinIdleConns        int32 = 0
	defaultManagementM2MaxConcurrent             int64 = 10
	defaultManagementM3MaxConcurrent             int64 = 6
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

type MailConfig struct {
	Enabled bool
	From    string
	ReplyTo string
	SMTP    MailSMTPConfig
}

type MailSMTPConfig struct {
	Host          string
	Port          int
	Mode          MailSMTPMode
	EHLOHostname  string
	Auth          MailSMTPAuth
	Username      string
	Password      string
	PasswordFile  string
	Timeout       time.Duration
	TLSServerName string
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
	Mail                             MailConfig
}

func Load() Settings {
	return loadCanonicalDefaultSettings(resolveDatabaseURLFromEnv())
}

func loadCanonicalDefaultSettings(databaseURL string) Settings {
	resolvedDatabaseURL := strings.TrimSpace(databaseURL)
	if resolvedDatabaseURL == "" {
		resolvedDatabaseURL = defaultBootstrapDatabaseURL
	}

	return Settings{
		Host:                             defaultBootstrapHost,
		Port:                             defaultBootstrapPort,
		AppEnv:                           EnvironmentDevelopment,
		DatabaseURL:                      resolvedDatabaseURL,
		RuntimeTelemetryMode:             RuntimeTelemetryModeDurableOutbox,
		RuntimeBufferingMode:             RuntimeBufferingModeBuffered,
		RuntimeTransportConfig:           defaultRuntimeTransportConfig(),
		RuntimeDatabasePoolBudget:        DatabasePoolBudget{MaxConns: defaultRuntimeDatabaseMaxConns, MinIdleConns: defaultRuntimeDatabaseMinIdleConns},
		ManagementDatabasePoolBudget:     DatabasePoolBudget{MaxConns: defaultManagementDatabaseMaxConns, MinIdleConns: defaultManagementDatabaseMinIdleConns},
		ManagementAdmissionControlBudget: ManagementAdmissionBudget{M2MaxConcurrent: defaultManagementM2MaxConcurrent, M3MaxConcurrent: defaultManagementM3MaxConcurrent},
		SecretEncryptionKey:              defaultSeedSecretEncryptionKey,
		ConfigBundleEncryptionKey:        defaultSeedSecretEncryptionKey,
		CORSAllowedOrigins:               defaultBootstrapCORSAllowedOrigins,
		AuthJWTSecret:                    defaultAuthJWTSecret,
		AuthAccessTokenTTLSeconds:        defaultAuthAccessTokenTTLSeconds,
		AuthRefreshTokenTTLSeconds:       defaultAuthRefreshTokenTTLSeconds,
		AuthResetCodeTTLSeconds:          defaultAuthResetCodeTTLSeconds,
		AuthCookieName:                   defaultAuthCookieName,
		AuthRefreshCookieName:            defaultAuthRefreshCookieName,
		AuthCookieSecure:                 false,
		Mail:                             defaultMailConfig(),
	}
}

func defaultMailConfig() MailConfig {
	return MailConfig{SMTP: MailSMTPConfig{Timeout: defaultMailSMTPTimeout}}
}

func resolveDatabaseURLFromEnv() string {
	value := strings.TrimSpace(os.Getenv(bootstrapDatabaseURLEnv))
	if value == "" {
		return defaultBootstrapDatabaseURL
	}
	return value
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

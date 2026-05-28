package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Environment string

type RuntimeTelemetryMode string

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
	defaultBootstrapPort               = 8000
	defaultBootstrapDatabaseURL        = "postgres://prism:prism@localhost:5432/prism?sslmode=disable"
	defaultBootstrapCORSAllowedOrigins = "http://localhost:5173,http://127.0.0.1:5173"
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
	defaultPostgresTotalMaxConns                 int32 = 24
	defaultManagementDatabaseMaxConns            int32 = 4
	defaultManagementDatabaseMinIdleConns        int32 = 1
	defaultRuntimeExecutionDatabaseMaxConns      int32 = 8
	defaultRuntimeExecutionDatabaseMinIdleConns  int32 = 2
	defaultRuntimeTelemetryDatabaseMaxConns      int32 = 4
	defaultRuntimeTelemetryDatabaseMinIdleConns  int32 = 1
	defaultRuntimeFeedbackDatabaseMaxConns       int32 = 2
	defaultRuntimeFeedbackDatabaseMinIdleConns   int32 = 0
	defaultRealtimeDatabaseMaxConns              int32 = 2
	defaultRealtimeDatabaseMinIdleConns          int32 = 0
	defaultCacheRefreshDatabaseMaxConns          int32 = 2
	defaultCacheRefreshDatabaseMinIdleConns      int32 = 0
	defaultBackgroundJobsDatabaseMaxConns        int32 = 2
	defaultBackgroundJobsDatabaseMinIdleConns    int32 = 0
	defaultManagementM2MaxConcurrent             int64 = 3
	defaultManagementM3MaxConcurrent             int64 = 2
	defaultRuntimeTransportMaxIdleConns                = 100
	defaultRuntimeTransportMaxIdleConnsPerHost         = 16
	defaultRuntimeTransportMaxConnsPerHost             = 16
	defaultRuntimeTransportRequestTimeout              = 300 * time.Second
	defaultRuntimeTransportIdleConnTimeout             = 90 * time.Second
	defaultRuntimeTransportResponseHeaderTimeout       = 0
	defaultRuntimeTransportTLSHandshakeTimeout         = 10 * time.Second
	defaultRuntimeTransportExpectContinueTimeout       = 1 * time.Second
	defaultRuntimeSideEffectsAttemptTimeout            = 10 * time.Second
)

type DatabasePoolBudget struct {
	MaxConns     int32
	MinIdleConns int32
}

type PostgresPoolLane string

const (
	PostgresLaneRuntimeExecution PostgresPoolLane = "runtime_execution"
	PostgresLaneRuntimeTelemetry PostgresPoolLane = "runtime_telemetry"
	PostgresLaneRuntimeFeedback  PostgresPoolLane = "runtime_feedback"
	PostgresLaneManagement       PostgresPoolLane = "management"
	PostgresLaneRealtime         PostgresPoolLane = "realtime"
	PostgresLaneCacheRefresh     PostgresPoolLane = "cache_refresh"
	PostgresLaneBackgroundJobs   PostgresPoolLane = "background_jobs"
)

type PostgresPoolsBudget struct {
	TotalMaxConns    int32
	Management       DatabasePoolBudget
	RuntimeExecution DatabasePoolBudget
	RuntimeTelemetry DatabasePoolBudget
	RuntimeFeedback  DatabasePoolBudget
	Realtime         DatabasePoolBudget
	CacheRefresh     DatabasePoolBudget
	BackgroundJobs   DatabasePoolBudget
}

type ManagementAdmissionBudget struct {
	M2MaxConcurrent int64
	M3MaxConcurrent int64
}

type RuntimeTransportConfig struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	RequestTimeout        time.Duration
	IdleConnTimeout       time.Duration
	ResponseHeaderTimeout time.Duration
	TLSHandshakeTimeout   time.Duration
	ExpectContinueTimeout time.Duration
}

type RuntimeSideEffectsConfig struct {
	AttemptTimeout time.Duration
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
	RuntimeTransportConfig           RuntimeTransportConfig
	RuntimeSideEffectsConfig         RuntimeSideEffectsConfig
	PostgresPoolsBudget              PostgresPoolsBudget
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
		RuntimeTransportConfig:           defaultRuntimeTransportConfig(),
		RuntimeSideEffectsConfig:         defaultRuntimeSideEffectsConfig(),
		PostgresPoolsBudget:              DefaultPostgresPoolsBudget(),
		RuntimeDatabasePoolBudget:        defaultRuntimeExecutionDatabasePoolBudget(),
		ManagementDatabasePoolBudget:     defaultManagementDatabasePoolBudget(),
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

func DefaultPostgresPoolsBudget() PostgresPoolsBudget {
	return PostgresPoolsBudget{
		TotalMaxConns:    defaultPostgresTotalMaxConns,
		Management:       defaultManagementDatabasePoolBudget(),
		RuntimeExecution: defaultRuntimeExecutionDatabasePoolBudget(),
		RuntimeTelemetry: DatabasePoolBudget{MaxConns: defaultRuntimeTelemetryDatabaseMaxConns, MinIdleConns: defaultRuntimeTelemetryDatabaseMinIdleConns},
		RuntimeFeedback:  DatabasePoolBudget{MaxConns: defaultRuntimeFeedbackDatabaseMaxConns, MinIdleConns: defaultRuntimeFeedbackDatabaseMinIdleConns},
		Realtime:         DatabasePoolBudget{MaxConns: defaultRealtimeDatabaseMaxConns, MinIdleConns: defaultRealtimeDatabaseMinIdleConns},
		CacheRefresh:     DatabasePoolBudget{MaxConns: defaultCacheRefreshDatabaseMaxConns, MinIdleConns: defaultCacheRefreshDatabaseMinIdleConns},
		BackgroundJobs:   DatabasePoolBudget{MaxConns: defaultBackgroundJobsDatabaseMaxConns, MinIdleConns: defaultBackgroundJobsDatabaseMinIdleConns},
	}
}

func defaultManagementDatabasePoolBudget() DatabasePoolBudget {
	return DatabasePoolBudget{MaxConns: defaultManagementDatabaseMaxConns, MinIdleConns: defaultManagementDatabaseMinIdleConns}
}

func defaultRuntimeExecutionDatabasePoolBudget() DatabasePoolBudget {
	return DatabasePoolBudget{MaxConns: defaultRuntimeExecutionDatabaseMaxConns, MinIdleConns: defaultRuntimeExecutionDatabaseMinIdleConns}
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

func (s Settings) RuntimeTransport() RuntimeTransportConfig {
	return normalizeRuntimeTransportConfig(s.RuntimeTransportConfig, defaultRuntimeTransportConfig())
}

func (s Settings) RuntimeSideEffects() RuntimeSideEffectsConfig {
	return normalizeRuntimeSideEffectsConfig(s.RuntimeSideEffectsConfig, defaultRuntimeSideEffectsConfig())
}

func (s Settings) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

func (s Settings) RuntimeDatabaseBudget() DatabasePoolBudget {
	return s.PostgresPoolsBudgetOrDefault().RuntimeExecution
}

func (s Settings) ManagementDatabaseBudget() DatabasePoolBudget {
	return s.PostgresPoolsBudgetOrDefault().Management
}

func (s Settings) PostgresPoolsBudgetOrDefault() PostgresPoolsBudget {
	budget := s.PostgresPoolsBudget
	if budget.isZero() {
		budget = DefaultPostgresPoolsBudget()
		if s.ManagementDatabasePoolBudget.MaxConns > 0 || s.ManagementDatabasePoolBudget.MinIdleConns > 0 {
			budget.Management = normalizeDatabasePoolBudget(s.ManagementDatabasePoolBudget, defaultManagementDatabasePoolBudget())
		}
		if s.RuntimeDatabasePoolBudget.MaxConns > 0 || s.RuntimeDatabasePoolBudget.MinIdleConns > 0 {
			budget.RuntimeExecution = normalizeDatabasePoolBudget(s.RuntimeDatabasePoolBudget, defaultRuntimeExecutionDatabasePoolBudget())
		}
	}
	return normalizePostgresPoolsBudget(budget)
}

func (s Settings) ManagementAdmissionBudget() ManagementAdmissionBudget {
	defaultBudget := ManagementAdmissionBudget{M2MaxConcurrent: defaultManagementM2MaxConcurrent, M3MaxConcurrent: defaultManagementM3MaxConcurrent}
	maxLowerPriority := max(int64(s.ManagementDatabaseBudget().MaxConns)-1, int64(1))
	return normalizeManagementAdmissionBudget(s.ManagementAdmissionControlBudget, defaultBudget, maxLowerPriority)
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
		RequestTimeout:        defaultRuntimeTransportRequestTimeout,
		IdleConnTimeout:       defaultRuntimeTransportIdleConnTimeout,
		ResponseHeaderTimeout: defaultRuntimeTransportResponseHeaderTimeout,
		TLSHandshakeTimeout:   defaultRuntimeTransportTLSHandshakeTimeout,
		ExpectContinueTimeout: defaultRuntimeTransportExpectContinueTimeout,
	}
}

func defaultRuntimeSideEffectsConfig() RuntimeSideEffectsConfig {
	return RuntimeSideEffectsConfig{AttemptTimeout: defaultRuntimeSideEffectsAttemptTimeout}
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

func normalizePostgresPoolsBudget(candidate PostgresPoolsBudget) PostgresPoolsBudget {
	defaults := DefaultPostgresPoolsBudget()
	normalized := candidate
	if normalized.TotalMaxConns <= 0 {
		normalized.TotalMaxConns = defaults.TotalMaxConns
	}
	normalized.Management = normalizeDatabasePoolBudget(normalized.Management, defaults.Management)
	normalized.RuntimeExecution = normalizeDatabasePoolBudget(normalized.RuntimeExecution, defaults.RuntimeExecution)
	normalized.RuntimeTelemetry = normalizeDatabasePoolBudget(normalized.RuntimeTelemetry, defaults.RuntimeTelemetry)
	normalized.RuntimeFeedback = normalizeDatabasePoolBudget(normalized.RuntimeFeedback, defaults.RuntimeFeedback)
	normalized.Realtime = normalizeDatabasePoolBudget(normalized.Realtime, defaults.Realtime)
	normalized.CacheRefresh = normalizeDatabasePoolBudget(normalized.CacheRefresh, defaults.CacheRefresh)
	normalized.BackgroundJobs = normalizeDatabasePoolBudget(normalized.BackgroundJobs, defaults.BackgroundJobs)
	return normalized
}

func (b PostgresPoolsBudget) isZero() bool {
	return b.TotalMaxConns == 0 && b.Management == (DatabasePoolBudget{}) && b.RuntimeExecution == (DatabasePoolBudget{}) && b.RuntimeTelemetry == (DatabasePoolBudget{}) && b.RuntimeFeedback == (DatabasePoolBudget{}) && b.Realtime == (DatabasePoolBudget{}) && b.CacheRefresh == (DatabasePoolBudget{}) && b.BackgroundJobs == (DatabasePoolBudget{})
}

func (b PostgresPoolsBudget) SumMaxConns() int64 {
	return int64(b.Management.MaxConns) + int64(b.RuntimeExecution.MaxConns) + int64(b.RuntimeTelemetry.MaxConns) + int64(b.RuntimeFeedback.MaxConns) + int64(b.Realtime.MaxConns) + int64(b.CacheRefresh.MaxConns) + int64(b.BackgroundJobs.MaxConns)
}

func (b PostgresPoolsBudget) Validate() error {
	if b.TotalMaxConns <= 0 {
		return fmt.Errorf("invalid postgres pool config: total_max_conns must be greater than zero")
	}
	for _, laneBudget := range []struct {
		lane   PostgresPoolLane
		budget DatabasePoolBudget
	}{
		{PostgresLaneManagement, b.Management},
		{PostgresLaneRuntimeExecution, b.RuntimeExecution},
		{PostgresLaneRuntimeTelemetry, b.RuntimeTelemetry},
		{PostgresLaneRuntimeFeedback, b.RuntimeFeedback},
		{PostgresLaneRealtime, b.Realtime},
		{PostgresLaneCacheRefresh, b.CacheRefresh},
		{PostgresLaneBackgroundJobs, b.BackgroundJobs},
	} {
		if laneBudget.budget.MaxConns <= 0 {
			return fmt.Errorf("invalid postgres pool config: lane=%s max_conns must be greater than zero", laneBudget.lane)
		}
		if laneBudget.budget.MinIdleConns < 0 {
			return fmt.Errorf("invalid postgres pool config: lane=%s min_idle_conns must be greater than or equal to zero", laneBudget.lane)
		}
		if laneBudget.budget.MinIdleConns > laneBudget.budget.MaxConns {
			return fmt.Errorf("invalid postgres pool config: lane=%s min_idle_conns must be less than or equal to max_conns", laneBudget.lane)
		}
	}
	if laneSum := b.SumMaxConns(); laneSum > int64(b.TotalMaxConns) {
		return fmt.Errorf("postgres pool budget exceeded: total_max_conns=%d lane_sum=%d", b.TotalMaxConns, laneSum)
	}
	return nil
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
	if normalized.RequestTimeout <= 0 {
		normalized.RequestTimeout = defaults.RequestTimeout
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

func normalizeRuntimeSideEffectsConfig(candidate RuntimeSideEffectsConfig, defaults RuntimeSideEffectsConfig) RuntimeSideEffectsConfig {
	normalized := candidate
	if normalized.AttemptTimeout <= 0 {
		normalized.AttemptTimeout = defaults.AttemptTimeout
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

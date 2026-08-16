package config

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

type Environment string

type RuntimeTelemetryMode string

type TelemetryExporterProtocol string

type TelemetryExporterCompression string

type TelemetryExporterAuthMode string

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
	TelemetryExporterProtocolGRPC         TelemetryExporterProtocol = "grpc"
	TelemetryExporterProtocolHTTPProtobuf TelemetryExporterProtocol = "http/protobuf"
)

const (
	TelemetryExporterCompressionNone TelemetryExporterCompression = "none"
	TelemetryExporterCompressionGzip TelemetryExporterCompression = "gzip"
)

const (
	TelemetryExporterAuthModeNone                TelemetryExporterAuthMode = "none"
	TelemetryExporterAuthModeAuthorizationHeader TelemetryExporterAuthMode = "authorization_header"
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
	bootstrapDatabaseURLEnv             = "DATABASE_URL"
	defaultBootstrapHost                = "0.0.0.0"
	defaultBootstrapPort                = 8000
	defaultBootstrapDatabaseURL         = "postgres://prism:prism@localhost:5432/prism?sslmode=disable"
	defaultBootstrapCORSAllowedOrigins  = "http://localhost:5173,http://127.0.0.1:5173"
	defaultSeedSecretEncryptionKey      = "prism-dev-runtime-secret-change-me"
	defaultAuthJWTSecret                = "prism-dev-jwt-secret-change-me"
	defaultAuthAccessTokenTTLSeconds    = 900
	defaultAuthRefreshTokenTTLSeconds   = 604800
	defaultAuthResetCodeTTLSeconds      = 600
	defaultAuthCookieName               = "prism_access_token"
	defaultAuthRefreshCookieName        = "prism_refresh_token"
	defaultTelemetryServiceNamespace    = "prism"
	defaultTelemetryServiceName         = "prism-backend"
	defaultTelemetryExporterProtocol    = TelemetryExporterProtocolHTTPProtobuf
	defaultTelemetryExporterCompression = TelemetryExporterCompressionNone
	defaultTelemetryExporterAuthMode    = TelemetryExporterAuthModeNone
	defaultTelemetryExporterTimeout     = 10 * time.Second
	defaultTelemetryTracesSamplingRatio = 1.0
	defaultMailSMTPTimeout              = 15 * time.Second
)

const (
	defaultRuntimeTransportMaxIdleConns          = 100
	defaultRuntimeTransportMaxIdleConnsPerHost   = 16
	defaultRuntimeTransportMaxConnsPerHost       = 16
	defaultRuntimeTransportRequestTimeout        = 300 * time.Second
	defaultRuntimeTransportIdleConnTimeout       = 90 * time.Second
	defaultRuntimeTransportResponseHeaderTimeout = 0
	defaultRuntimeTransportTLSHandshakeTimeout   = 10 * time.Second
	defaultRuntimeTransportExpectContinueTimeout = 1 * time.Second
	defaultRuntimeSideEffectsAttemptTimeout      = 10 * time.Second
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
	PostgresLaneCacheRefresh     PostgresPoolLane = "cache_refresh"
	PostgresLaneBackgroundJobs   PostgresPoolLane = "background_jobs"
)

type PostgresPoolsBudget struct {
	TotalMaxConns    int32
	Management       DatabasePoolBudget
	RuntimeExecution DatabasePoolBudget
	RuntimeTelemetry DatabasePoolBudget
	RuntimeFeedback  DatabasePoolBudget
	Realtime         DatabasePoolBudget // ponytail: parsed for live config.json compat; ignored
	CacheRefresh     DatabasePoolBudget
	BackgroundJobs   DatabasePoolBudget
}

type ManagementAdmissionBudget struct {
	M2MaxConcurrent int64
	M3MaxConcurrent int64
}

type TelemetryConfig struct {
	// ponytail: telemetry config parsed for live config.json compat; exporters removed
	Enabled  bool
	Service  TelemetryServiceConfig
	Exporter TelemetryExporterConfig
	Metrics  TelemetrySignalConfig
	Traces   TelemetryTracesConfig
}

type TelemetryServiceConfig struct {
	Namespace string
	Name      string
}

type TelemetryExporterConfig struct {
	Endpoint    string
	Protocol    TelemetryExporterProtocol
	Compression TelemetryExporterCompression
	Timeout     time.Duration
	Auth        TelemetryExporterAuthConfig
	TLS         TelemetryExporterTLSConfig
}

type TelemetryExporterAuthConfig struct {
	Mode                TelemetryExporterAuthMode
	AuthorizationHeader string
}

type TelemetryExporterTLSConfig struct {
	InsecureSkipVerify bool
	CAFile             string
}

type TelemetrySignalConfig struct {
	Enabled bool
}

type TelemetryTracesConfig struct {
	Enabled       bool
	SamplingRatio float64
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

type AlertingConfig struct {
	WebhookURL string
}

type MailConfig struct {
	// ponytail: mail config parsed for live config.json compat; delivery removed
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
	Telemetry                        TelemetryConfig
	RuntimeTransportConfig           RuntimeTransportConfig
	RuntimeSideEffectsConfig         RuntimeSideEffectsConfig
	PostgresPoolsBudget              PostgresPoolsBudget
	RuntimeDatabasePoolBudget        DatabasePoolBudget
	ManagementDatabasePoolBudget     DatabasePoolBudget
	ManagementAdmissionControlBudget ManagementAdmissionBudget
	SecretEncryptionKey              string
	StateTransferBundleEncryptionKey string // ponytail: parsed for live config.json compat; feature removed
	CORSAllowedOrigins               string
	AuthJWTSecret                    string
	AuthAccessTokenTTLSeconds        int
	AuthRefreshTokenTTLSeconds       int
	AuthResetCodeTTLSeconds          int
	AuthCookieName                   string
	AuthRefreshCookieName            string
	AuthCookieSecure                 bool
	Alerting                         AlertingConfig
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
		Telemetry:                        defaultTelemetryConfig(),
		RuntimeTransportConfig:           defaultRuntimeTransportConfig(),
		RuntimeSideEffectsConfig:         defaultRuntimeSideEffectsConfig(),
		PostgresPoolsBudget:              DefaultPostgresPoolsBudget(),
		RuntimeDatabasePoolBudget:        defaultRuntimeExecutionDatabasePoolBudget(),
		ManagementDatabasePoolBudget:     defaultManagementDatabasePoolBudget(),
		ManagementAdmissionControlBudget: defaultManagementAdmissionBudget(),
		SecretEncryptionKey:              defaultSeedSecretEncryptionKey,
		CORSAllowedOrigins:               defaultBootstrapCORSAllowedOrigins,
		AuthJWTSecret:                    defaultAuthJWTSecret,
		AuthAccessTokenTTLSeconds:        defaultAuthAccessTokenTTLSeconds,
		AuthRefreshTokenTTLSeconds:       defaultAuthRefreshTokenTTLSeconds,
		AuthResetCodeTTLSeconds:          defaultAuthResetCodeTTLSeconds,
		AuthCookieName:                   defaultAuthCookieName,
		AuthRefreshCookieName:            defaultAuthRefreshCookieName,
		AuthCookieSecure:                 false,
		Alerting:                         AlertingConfig{},
		Mail:                             defaultMailConfig(),
	}
}

func defaultMailConfig() MailConfig {
	return MailConfig{SMTP: MailSMTPConfig{Timeout: defaultMailSMTPTimeout}}
}

func defaultTelemetryConfig() TelemetryConfig {
	return TelemetryConfig{
		Service: TelemetryServiceConfig{
			Namespace: defaultTelemetryServiceNamespace,
			Name:      defaultTelemetryServiceName,
		},
		Exporter: TelemetryExporterConfig{
			Protocol:    defaultTelemetryExporterProtocol,
			Compression: defaultTelemetryExporterCompression,
			Timeout:     defaultTelemetryExporterTimeout,
			Auth:        TelemetryExporterAuthConfig{Mode: defaultTelemetryExporterAuthMode},
		},
		Traces: TelemetryTracesConfig{SamplingRatio: defaultTelemetryTracesSamplingRatio},
	}
}

// derivedPoolUnit maps usable CPU count to the sizing unit for pool and
// admission defaults. Floor 8 keeps the /system/settings page fan-out
// (5 concurrent M2 requests) admitted on small hosts; ceiling 16 keeps
// the lane sum (53) well under the postgres default max_connections=100.
// Callers pass runtime.GOMAXPROCS(0), which is cgroup-quota aware since
// Go 1.25, so CPU-limited containers are not sized by the host core count.
func derivedPoolUnit(cores int) int32 {
	return int32(min(max(cores, 8), 16))
}

func derivedPostgresPoolsBudget(cores int) PostgresPoolsBudget {
	unit := derivedPoolUnit(cores)
	budget := PostgresPoolsBudget{
		Management:       DatabasePoolBudget{MaxConns: unit + 1, MinIdleConns: 1},
		RuntimeExecution: DatabasePoolBudget{MaxConns: unit, MinIdleConns: 2},
		RuntimeTelemetry: DatabasePoolBudget{MaxConns: unit / 2, MinIdleConns: 1},
		RuntimeFeedback:  DatabasePoolBudget{MaxConns: unit / 4, MinIdleConns: 0},
		CacheRefresh:     DatabasePoolBudget{MaxConns: unit / 4, MinIdleConns: 0},
		BackgroundJobs:   DatabasePoolBudget{MaxConns: unit / 4, MinIdleConns: 0},
	}
	budget.TotalMaxConns = int32(budget.SumMaxConns())
	return budget
}

// derivedManagementAdmissionBudget keeps m2 == management.maxConns-1 so the
// derived defaults are never clamped by normalizeManagementAdmissionBudget.
func derivedManagementAdmissionBudget(cores int) ManagementAdmissionBudget {
	unit := derivedPoolUnit(cores)
	return ManagementAdmissionBudget{M2MaxConcurrent: int64(unit), M3MaxConcurrent: int64(unit / 2)}
}

func DefaultPostgresPoolsBudget() PostgresPoolsBudget {
	return derivedPostgresPoolsBudget(runtime.GOMAXPROCS(0))
}

func defaultManagementDatabasePoolBudget() DatabasePoolBudget {
	return DefaultPostgresPoolsBudget().Management
}

func defaultRuntimeExecutionDatabasePoolBudget() DatabasePoolBudget {
	return DefaultPostgresPoolsBudget().RuntimeExecution
}

func defaultManagementAdmissionBudget() ManagementAdmissionBudget {
	return derivedManagementAdmissionBudget(runtime.GOMAXPROCS(0))
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
	maxLowerPriority := max(int64(s.ManagementDatabaseBudget().MaxConns)-1, int64(1))
	return normalizeManagementAdmissionBudget(s.ManagementAdmissionControlBudget, defaultManagementAdmissionBudget(), maxLowerPriority)
}

// ManagementAdmissionClamp reports whether the configured M2 admission
// budget was reduced to fit database.pools.management.maxConns, so callers
// can surface the silent clamp at startup. Only the M2-vs-maxConns clamp is
// reported: bootstrap validation already rejects m3 > m2, so a lowered
// effective M3 can only be a consequence of the M2 clamp reported here.
func (s Settings) ManagementAdmissionClamp() (configured, effective ManagementAdmissionBudget, clamped bool) {
	configured = s.ManagementAdmissionControlBudget
	effective = s.ManagementAdmissionBudget()
	clamped = configured.M2MaxConcurrent > 0 && configured.M2MaxConcurrent > effective.M2MaxConcurrent
	return configured, effective, clamped
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
	normalized.CacheRefresh = normalizeDatabasePoolBudget(normalized.CacheRefresh, defaults.CacheRefresh)
	normalized.BackgroundJobs = normalizeDatabasePoolBudget(normalized.BackgroundJobs, defaults.BackgroundJobs)
	return normalized
}

func (b PostgresPoolsBudget) isZero() bool {
	return b.TotalMaxConns == 0 && b.Management == (DatabasePoolBudget{}) && b.RuntimeExecution == (DatabasePoolBudget{}) && b.RuntimeTelemetry == (DatabasePoolBudget{}) && b.RuntimeFeedback == (DatabasePoolBudget{}) && b.CacheRefresh == (DatabasePoolBudget{}) && b.BackgroundJobs == (DatabasePoolBudget{})
}

func (b PostgresPoolsBudget) SumMaxConns() int64 {
	return int64(b.Management.MaxConns) + int64(b.RuntimeExecution.MaxConns) + int64(b.RuntimeTelemetry.MaxConns) + int64(b.RuntimeFeedback.MaxConns) + int64(b.CacheRefresh.MaxConns) + int64(b.BackgroundJobs.MaxConns)
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

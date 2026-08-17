package config

import (
	"fmt"
	"math"
	"net/url"
	"strings"
)

type bootstrapConfigDocument struct {
	Meta          *bootstrapMeta          `json:"meta"`
	Server        *bootstrapServer        `json:"server"`
	Database      *bootstrapDatabase      `json:"database"`
	Runtime       *bootstrapRuntime       `json:"runtime"`
	HTTP          *bootstrapHTTP          `json:"http"`
	Auth          *bootstrapAuth          `json:"auth"`
	Alerting      *bootstrapAlerting      `json:"alerting,omitempty"`
	Mail          *bootstrapMail          `json:"mail,omitempty"`
	Telemetry     *bootstrapTelemetry     `json:"telemetry,omitempty"`
	StateTransfer *bootstrapStateTransfer `json:"stateTransfer,omitempty"`
}

type bootstrapMeta struct {
	SchemaVersion *int    `json:"schemaVersion"`
	Revision      *int    `json:"revision"`
	CreatedAt     *string `json:"createdAt"`
	UpdatedAt     *string `json:"updatedAt"`
}

type bootstrapServer struct {
	Host *string `json:"host"`
	Port *int    `json:"port"`
}

type bootstrapDatabase struct {
	URL                 *string                       `json:"url"`
	Pools               *bootstrapDatabasePools       `json:"pools"`
	ManagementAdmission *bootstrapManagementAdmission `json:"managementAdmission"`
}

type bootstrapDatabasePools struct {
	TotalMaxConns    *int                   `json:"totalMaxConns"`
	Management       *bootstrapDatabasePool `json:"management"`
	RuntimeExecution *bootstrapDatabasePool `json:"runtimeExecution"`
	RuntimeTelemetry *bootstrapDatabasePool `json:"runtimeTelemetry"`
	RuntimeFeedback  *bootstrapDatabasePool `json:"runtimeFeedback"`
	Realtime         *bootstrapDatabasePool `json:"realtime,omitempty"` // ponytail: parsed for live config.json compat; ignored
	CacheRefresh     *bootstrapDatabasePool `json:"cacheRefresh"`
	BackgroundJobs   *bootstrapDatabasePool `json:"backgroundJobs"`
}

type bootstrapDatabasePool struct {
	MaxConns     *int `json:"maxConns"`
	MinIdleConns *int `json:"minIdleConns"`
}

type bootstrapManagementAdmission struct {
	M2MaxConcurrent *int `json:"m2MaxConcurrent"`
	M3MaxConcurrent *int `json:"m3MaxConcurrent"`
}

type bootstrapRuntime struct {
	SecretEncryptionKey *string                      `json:"secretEncryptionKey"`
	SideEffects         *bootstrapRuntimeSideEffects `json:"sideEffects"`
	Routing             *bootstrapRuntimeRouting     `json:"routing,omitempty"`
}

type bootstrapRuntimeSideEffects struct {
	AttemptTimeout *string `json:"attemptTimeout"`
}

type bootstrapRuntimeRouting struct{}

type bootstrapHTTP struct {
	CORSAllowedOrigins *[]string `json:"corsAllowedOrigins"`
}

type bootstrapAuth struct {
	JWTSigningKey          *string `json:"jwtSigningKey"`
	AccessTokenTTLSeconds  *int    `json:"accessTokenTtlSeconds"`
	RefreshTokenTTLSeconds *int    `json:"refreshTokenTtlSeconds"`
	ResetCodeTTLSeconds    *int    `json:"resetCodeTtlSeconds"` // ponytail: parsed for live config.json compat; ignored by settings
	AccessCookieName       *string `json:"accessCookieName"`
	RefreshCookieName      *string `json:"refreshCookieName"`
	CookieSecure           *bool   `json:"cookieSecure"`
}

type bootstrapAlerting struct {
	WebhookURL *string `json:"webhookUrl"`
}

type bootstrapMail struct {
	Enabled *bool          `json:"enabled"`
	From    *string        `json:"from,omitempty"`
	ReplyTo *string        `json:"replyTo,omitempty"`
	SMTP    *bootstrapSMTP `json:"smtp,omitempty"`
}

type bootstrapSMTP struct {
	Host          *string `json:"host,omitempty"`
	Port          *int    `json:"port,omitempty"`
	Mode          *string `json:"mode,omitempty"`
	EHLOHostname  *string `json:"ehloHostname,omitempty"`
	Auth          *string `json:"auth,omitempty"`
	Username      *string `json:"username,omitempty"`
	Password      *string `json:"password,omitempty"`
	PasswordFile  *string `json:"passwordFile,omitempty"`
	Timeout       *string `json:"timeout,omitempty"`
	TLSServerName *string `json:"tlsServerName,omitempty"`
}

type bootstrapStateTransfer struct {
	BundleEncryptionKey *string `json:"bundleEncryptionKey"` // ponytail: parsed for live config.json compat; feature removed
}

func (d bootstrapConfigDocument) validateSchema() error {
	if d.Meta == nil {
		return missingBootstrapFieldError("meta")
	}
	if d.Server == nil {
		return missingBootstrapFieldError("server")
	}
	if d.Database == nil {
		return missingBootstrapFieldError("database")
	}
	if d.Runtime == nil {
		return missingBootstrapFieldError("runtime")
	}
	if d.HTTP == nil {
		return missingBootstrapFieldError("http")
	}
	if d.Auth == nil {
		return missingBootstrapFieldError("auth")
	}
	if err := d.Meta.validate(); err != nil {
		return err
	}
	if err := d.Server.validate(); err != nil {
		return err
	}
	if err := d.Database.validate(); err != nil {
		return err
	}
	if err := d.Runtime.validate(); err != nil {
		return err
	}
	if err := d.HTTP.validate(); err != nil {
		return err
	}
	if err := d.Auth.validate(); err != nil {
		return err
	}
	if d.Alerting != nil {
		if err := d.Alerting.validate(); err != nil {
			return err
		}
	}
	if d.Telemetry != nil {
		if err := d.Telemetry.validate(); err != nil {
			return err
		}
	}
	if d.StateTransfer != nil {
		return d.StateTransfer.validate()
	}
	return nil
}

func (m bootstrapMeta) validate() error {
	if _, err := requiredIntConst("meta.schemaVersion", m.SchemaVersion, bootstrapConfigSchemaVersion); err != nil {
		return err
	}
	if _, err := requiredIntMin("meta.revision", m.Revision, 1); err != nil {
		return err
	}
	if _, err := requiredDateTime("meta.createdAt", m.CreatedAt); err != nil {
		return err
	}
	if _, err := requiredDateTime("meta.updatedAt", m.UpdatedAt); err != nil {
		return err
	}
	return nil
}

func (s bootstrapServer) validate() error {
	if _, err := requiredTrimmedString("server.host", s.Host, 1, 255); err != nil {
		return err
	}
	if _, err := requiredIntRange("server.port", s.Port, 1, 65535); err != nil {
		return err
	}
	return nil
}

func (d bootstrapDatabase) validate() error {
	if _, err := requiredTrimmedString("database.url", d.URL, 1, 0); err != nil {
		return err
	}
	if d.Pools == nil {
		return fmt.Errorf("invalid postgres pool config: pools are required")
	}
	if d.ManagementAdmission == nil {
		return missingBootstrapFieldError("database.managementAdmission")
	}
	if _, err := d.Pools.toPostgresPoolsBudget(); err != nil {
		return err
	}
	return d.ManagementAdmission.validate()
}

func (p bootstrapDatabasePool) toDatabasePoolBudget(path string) (DatabasePoolBudget, error) {
	maxConns, err := requiredIntRange(path+".maxConns", p.MaxConns, 1, math.MaxInt32)
	if err != nil {
		return DatabasePoolBudget{}, err
	}
	minIdleConns, err := requiredIntRange(path+".minIdleConns", p.MinIdleConns, 0, math.MaxInt32)
	if err != nil {
		return DatabasePoolBudget{}, err
	}
	return DatabasePoolBudget{MaxConns: int32(maxConns), MinIdleConns: int32(minIdleConns)}, nil
}

func (p *bootstrapDatabasePools) toPostgresPoolsBudget() (PostgresPoolsBudget, error) {
	if p == nil {
		return PostgresPoolsBudget{}, fmt.Errorf("invalid postgres pool config: pools are required")
	}
	totalMaxConns, err := requiredIntRange("database.pools.totalMaxConns", p.TotalMaxConns, 1, math.MaxInt32)
	if err != nil {
		return PostgresPoolsBudget{}, fmt.Errorf("invalid postgres pool config: total_max_conns must be greater than zero")
	}
	lanePool := func(lane PostgresPoolLane, pool *bootstrapDatabasePool) (DatabasePoolBudget, error) {
		if pool == nil {
			return DatabasePoolBudget{}, fmt.Errorf("invalid postgres pool config: lane=%s is required", lane)
		}
		budget, err := pool.toDatabasePoolBudget("database.pools." + bootstrapDatabasePoolPath(lane))
		if err != nil {
			if strings.Contains(err.Error(), ".maxConns") {
				return DatabasePoolBudget{}, fmt.Errorf("invalid postgres pool config: lane=%s max_conns must be greater than zero", lane)
			}
			if strings.Contains(err.Error(), ".minIdleConns") {
				return DatabasePoolBudget{}, fmt.Errorf("invalid postgres pool config: lane=%s min_idle_conns must be greater than or equal to zero", lane)
			}
			return DatabasePoolBudget{}, err
		}
		return budget, nil
	}
	budget := PostgresPoolsBudget{TotalMaxConns: int32(totalMaxConns)}
	if budget.Management, err = lanePool(PostgresLaneManagement, p.Management); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.RuntimeExecution, err = lanePool(PostgresLaneRuntimeExecution, p.RuntimeExecution); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.RuntimeTelemetry, err = lanePool(PostgresLaneRuntimeTelemetry, p.RuntimeTelemetry); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.RuntimeFeedback, err = lanePool(PostgresLaneRuntimeFeedback, p.RuntimeFeedback); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.CacheRefresh, err = lanePool(PostgresLaneCacheRefresh, p.CacheRefresh); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.BackgroundJobs, err = lanePool(PostgresLaneBackgroundJobs, p.BackgroundJobs); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if err := budget.Validate(); err != nil {
		return PostgresPoolsBudget{}, err
	}
	return budget, nil
}

func bootstrapDatabasePoolPath(lane PostgresPoolLane) string {
	switch lane {
	case PostgresLaneRuntimeExecution:
		return "runtimeExecution"
	case PostgresLaneRuntimeTelemetry:
		return "runtimeTelemetry"
	case PostgresLaneRuntimeFeedback:
		return "runtimeFeedback"
	case PostgresLaneCacheRefresh:
		return "cacheRefresh"
	case PostgresLaneBackgroundJobs:
		return "backgroundJobs"
	default:
		return string(lane)
	}
}

func (a bootstrapManagementAdmission) validate() error {
	if _, err := requiredIntMin("database.managementAdmission.m2MaxConcurrent", a.M2MaxConcurrent, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("database.managementAdmission.m3MaxConcurrent", a.M3MaxConcurrent, 1); err != nil {
		return err
	}
	return nil
}

func (r bootstrapRuntime) validate() error {
	if _, err := requiredTrimmedString("secretEncryptionKey", r.SecretEncryptionKey, 1, 0); err != nil {
		return err
	}
	if r.SideEffects == nil {
		return missingBootstrapFieldError("sideEffects")
	}
	if err := r.SideEffects.validate(); err != nil {
		return err
	}
	if r.Routing == nil {
		return nil
	}
	return r.Routing.validate()
}

func (s bootstrapRuntimeSideEffects) validate() error {
	_, err := requiredTrimmedString("sideEffects.attemptTimeout", s.AttemptTimeout, 1, 0)
	return err
}

func (r bootstrapRuntimeRouting) validate() error {
	return nil
}

func (h bootstrapHTTP) validate() error {
	_, err := requiredAbsoluteURIs("http.corsAllowedOrigins", h.CORSAllowedOrigins)
	return err
}

func (a bootstrapAuth) validate() error {
	if _, err := requiredTrimmedString("auth.jwtSigningKey", a.JWTSigningKey, 1, 0); err != nil {
		return err
	}
	if _, err := requiredIntMin("auth.accessTokenTtlSeconds", a.AccessTokenTTLSeconds, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("auth.refreshTokenTtlSeconds", a.RefreshTokenTTLSeconds, 1); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("auth.accessCookieName", a.AccessCookieName, 1, 200); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("auth.refreshCookieName", a.RefreshCookieName, 1, 200); err != nil {
		return err
	}
	_, err := requiredBool("auth.cookieSecure", a.CookieSecure)
	return err
}

func (a bootstrapAlerting) validate() error {
	_, err := a.toAlertingConfig()
	return err
}

func (s bootstrapStateTransfer) validate() error {
	return nil
}

func (d bootstrapConfigDocument) validateSemantics() error {
	for _, field := range []struct {
		path  string
		value *string
	}{
		{path: "sideEffects.attemptTimeout", value: d.Runtime.SideEffects.AttemptTimeout},
	} {
		if _, err := parseDurationField(field.path, field.value); err != nil {
			return err
		}
	}
	if _, err := d.Database.Pools.toPostgresPoolsBudget(); err != nil {
		return err
	}
	m2MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m2MaxConcurrent", d.Database.ManagementAdmission.M2MaxConcurrent, 1)
	m3MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m3MaxConcurrent", d.Database.ManagementAdmission.M3MaxConcurrent, 1)
	if m3MaxConcurrent > m2MaxConcurrent {
		return fmt.Errorf("bootstrap config field database.managementAdmission.m3MaxConcurrent must be less than or equal to database.managementAdmission.m2MaxConcurrent")
	}
	return nil
}

func (d bootstrapConfigDocument) toSettings() (Settings, error) {
	host, err := requiredTrimmedString("server.host", d.Server.Host, 1, 255)
	if err != nil {
		return Settings{}, err
	}
	port, err := requiredIntRange("server.port", d.Server.Port, 1, 65535)
	if err != nil {
		return Settings{}, err
	}
	databaseURL, err := requiredTrimmedString("database.url", d.Database.URL, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	runtimeSecretEncryptionKey, err := requiredTrimmedString("secretEncryptionKey", d.Runtime.SecretEncryptionKey, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	runtimeSideEffects, err := d.Runtime.SideEffects.toRuntimeSideEffectsConfig()
	if err != nil {
		return Settings{}, err
	}
	postgresPoolsBudget, err := d.Database.Pools.toPostgresPoolsBudget()
	if err != nil {
		return Settings{}, err
	}
	m2MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m2MaxConcurrent", d.Database.ManagementAdmission.M2MaxConcurrent, 1)
	m3MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m3MaxConcurrent", d.Database.ManagementAdmission.M3MaxConcurrent, 1)
	corsAllowedOrigins, err := requiredAbsoluteURIs("http.corsAllowedOrigins", d.HTTP.CORSAllowedOrigins)
	if err != nil {
		return Settings{}, err
	}
	jwtSigningKey, err := requiredTrimmedString("auth.jwtSigningKey", d.Auth.JWTSigningKey, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	bundleEncryptionKey := ""
	if d.StateTransfer != nil && d.StateTransfer.BundleEncryptionKey != nil {
		bundleEncryptionKey = strings.TrimSpace(*d.StateTransfer.BundleEncryptionKey)
	}
	accessTokenTTLSeconds, _ := requiredIntMin("auth.accessTokenTtlSeconds", d.Auth.AccessTokenTTLSeconds, 1)
	refreshTokenTTLSeconds, _ := requiredIntMin("auth.refreshTokenTtlSeconds", d.Auth.RefreshTokenTTLSeconds, 1)
	accessCookieName, _ := requiredTrimmedString("auth.accessCookieName", d.Auth.AccessCookieName, 1, 200)
	refreshCookieName, _ := requiredTrimmedString("auth.refreshCookieName", d.Auth.RefreshCookieName, 1, 200)
	cookieSecure, _ := requiredBool("auth.cookieSecure", d.Auth.CookieSecure)
	telemetryConfig, err := d.Telemetry.toTelemetryConfig()
	if err != nil {
		return Settings{}, err
	}
	alertingConfig, err := d.Alerting.toAlertingConfig()
	if err != nil {
		return Settings{}, err
	}

	return Settings{
		Host:                             host,
		Port:                             port,
		AppEnv:                           EnvironmentDevelopment,
		DatabaseURL:                      databaseURL,
		RuntimeTelemetryMode:             RuntimeTelemetryModeDurableOutbox,
		Telemetry:                        telemetryConfig,
		RuntimeSideEffectsConfig:         runtimeSideEffects,
		PostgresPoolsBudget:              postgresPoolsBudget,
		RuntimeDatabasePoolBudget:        postgresPoolsBudget.RuntimeExecution,
		ManagementDatabasePoolBudget:     postgresPoolsBudget.Management,
		ManagementAdmissionControlBudget: ManagementAdmissionBudget{M2MaxConcurrent: int64(m2MaxConcurrent), M3MaxConcurrent: int64(m3MaxConcurrent)},
		SecretEncryptionKey:              runtimeSecretEncryptionKey,
		StateTransferBundleEncryptionKey: bundleEncryptionKey,
		CORSAllowedOrigins:               strings.Join(corsAllowedOrigins, ","),
		AuthJWTSecret:                    jwtSigningKey,
		AuthAccessTokenTTLSeconds:        accessTokenTTLSeconds,
		AuthRefreshTokenTTLSeconds:       refreshTokenTTLSeconds,
		AuthResetCodeTTLSeconds:          defaultAuthResetCodeTTLSeconds,
		AuthCookieName:                   accessCookieName,
		AuthRefreshCookieName:            refreshCookieName,
		AuthCookieSecure:                 cookieSecure,
		Alerting:                         alertingConfig,
		Mail:                             defaultMailConfig(),
	}, nil
}

func (a *bootstrapAlerting) toAlertingConfig() (AlertingConfig, error) {
	if a == nil || a.WebhookURL == nil {
		return AlertingConfig{}, nil
	}
	webhookURL, err := optionalTrimmedString("alerting.webhookUrl", a.WebhookURL, 4096)
	if err != nil {
		return AlertingConfig{}, err
	}
	if webhookURL == "" {
		return AlertingConfig{}, nil
	}
	parsed, err := url.Parse(webhookURL)
	if err != nil || strings.TrimSpace(parsed.Host) == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return AlertingConfig{}, fmt.Errorf("bootstrap config field alerting.webhookUrl must use http or https")
	}
	return AlertingConfig{WebhookURL: webhookURL}, nil
}

func (s bootstrapRuntimeSideEffects) toRuntimeSideEffectsConfig() (RuntimeSideEffectsConfig, error) {
	attemptTimeout, err := parseDurationField("sideEffects.attemptTimeout", s.AttemptTimeout)
	if err != nil {
		return RuntimeSideEffectsConfig{}, err
	}
	if attemptTimeout <= 0 {
		return RuntimeSideEffectsConfig{}, fmt.Errorf("bootstrap config field sideEffects.attemptTimeout must be greater than zero")
	}
	return RuntimeSideEffectsConfig{AttemptTimeout: attemptTimeout}, nil
}

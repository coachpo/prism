package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

type BootstrapConfigSnapshot struct {
	ConfigPath    string                                   `json:"config_path"`
	SchemaVersion int                                      `json:"schema_version"`
	FileRevision  int                                      `json:"file_revision"`
	CreatedAt     string                                   `json:"created_at"`
	UpdatedAt     string                                   `json:"updated_at"`
	DocumentETag  string                                   `json:"document_etag"`
	Values        BootstrapConfigValues                    `json:"values"`
	Secrets       map[string]BootstrapConfigSecretMetadata `json:"secrets"`
}

type BootstrapConfigValues struct {
	Server    *BootstrapConfigServerValues    `json:"server"`
	Database  *BootstrapConfigDatabaseValues  `json:"database"`
	Runtime   *BootstrapConfigRuntimeValues   `json:"runtime"`
	HTTP      *BootstrapConfigHTTPValues      `json:"http"`
	Auth      *BootstrapConfigAuthValues      `json:"auth"`
	Alerting  *BootstrapConfigAlertingValues  `json:"alerting"`
	Mail      *BootstrapConfigMailValues      `json:"mail,omitempty"`
	Telemetry *BootstrapConfigTelemetryValues `json:"telemetry,omitempty"`
}

type BootstrapConfigServerValues struct {
	Host *string `json:"host"`
	Port *int    `json:"port"`
}

type BootstrapConfigDatabaseValues struct {
	Pools               *BootstrapConfigDatabasePoolsValues       `json:"pools"`
	ManagementAdmission *BootstrapConfigManagementAdmissionValues `json:"management_admission"`
}

type BootstrapConfigDatabasePoolsValues struct {
	TotalMaxConns    *int                               `json:"total_max_conns"`
	Management       *BootstrapConfigDatabasePoolValues `json:"management"`
	RuntimeExecution *BootstrapConfigDatabasePoolValues `json:"runtime_execution"`
	RuntimeTelemetry *BootstrapConfigDatabasePoolValues `json:"runtime_telemetry"`
	RuntimeFeedback  *BootstrapConfigDatabasePoolValues `json:"runtime_feedback"`
	Realtime         *BootstrapConfigDatabasePoolValues `json:"realtime,omitempty"`
	CacheRefresh     *BootstrapConfigDatabasePoolValues `json:"cache_refresh"`
	BackgroundJobs   *BootstrapConfigDatabasePoolValues `json:"background_jobs"`
}

type BootstrapConfigDatabasePoolValues struct {
	MaxConns     *int `json:"max_conns"`
	MinIdleConns *int `json:"min_idle_conns"`
}

type BootstrapConfigManagementAdmissionValues struct {
	M2MaxConcurrent *int `json:"m2_max_concurrent"`
	M3MaxConcurrent *int `json:"m3_max_concurrent"`
}

type BootstrapConfigRuntimeValues struct {
	SideEffects *BootstrapConfigRuntimeSideEffectsValues `json:"side_effects"`
	Routing     *BootstrapConfigRuntimeRoutingValues     `json:"routing"`
}

type BootstrapConfigRuntimeSideEffectsValues struct {
	AttemptTimeout *string `json:"attempt_timeout"`
}

type BootstrapConfigRuntimeRoutingValues struct{}

type BootstrapConfigHTTPValues struct {
	CORSAllowedOrigins *[]string `json:"cors_allowed_origins"`
}

type BootstrapConfigAuthValues struct {
	AccessTokenTTLSeconds  *int    `json:"access_token_ttl_seconds"`
	RefreshTokenTTLSeconds *int    `json:"refresh_token_ttl_seconds"`
	ResetCodeTTLSeconds    *int    `json:"reset_code_ttl_seconds"`
	AccessCookieName       *string `json:"access_cookie_name"`
	RefreshCookieName      *string `json:"refresh_cookie_name"`
	CookieSecure           *bool   `json:"cookie_secure"`
}

type BootstrapConfigAlertingValues struct {
	WebhookURL *string `json:"webhook_url"`
}

type BootstrapConfigMailValues struct {
	Enabled *bool                          `json:"enabled"`
	From    *string                        `json:"from"`
	ReplyTo *string                        `json:"reply_to"`
	SMTP    *BootstrapConfigMailSMTPValues `json:"smtp"`
}

type BootstrapConfigMailSMTPValues struct {
	Host          *string `json:"host"`
	Port          *int    `json:"port"`
	Mode          *string `json:"mode"`
	EHLOHostname  *string `json:"ehlo_hostname"`
	Auth          *string `json:"auth"`
	Username      *string `json:"username"`
	PasswordFile  *string `json:"password_file"`
	Timeout       *string `json:"timeout"`
	TLSServerName *string `json:"tls_server_name"`
}

func safeBootstrapConfigValues(document bootstrapConfigDocument) BootstrapConfigValues {
	return BootstrapConfigValues{
		Server: &BootstrapConfigServerValues{
			Host: cloneStringPointer(document.Server.Host),
			Port: cloneIntPointer(document.Server.Port),
		},
		Database: &BootstrapConfigDatabaseValues{
			Pools: safeBootstrapDatabasePoolsValues(document.Database.Pools),
			ManagementAdmission: &BootstrapConfigManagementAdmissionValues{
				M2MaxConcurrent: cloneIntPointer(document.Database.ManagementAdmission.M2MaxConcurrent),
				M3MaxConcurrent: cloneIntPointer(document.Database.ManagementAdmission.M3MaxConcurrent),
			},
		},
		Runtime: &BootstrapConfigRuntimeValues{
			SideEffects: safeBootstrapRuntimeSideEffectsValues(document.Runtime.SideEffects),
			Routing:     safeBootstrapRuntimeRoutingValues(document.Runtime.Routing),
		},
		HTTP: &BootstrapConfigHTTPValues{
			CORSAllowedOrigins: cloneStringSlicePointer(document.HTTP.CORSAllowedOrigins),
		},
		Auth: &BootstrapConfigAuthValues{
			AccessTokenTTLSeconds:  cloneIntPointer(document.Auth.AccessTokenTTLSeconds),
			RefreshTokenTTLSeconds: cloneIntPointer(document.Auth.RefreshTokenTTLSeconds),
			ResetCodeTTLSeconds:    cloneIntPointer(document.Auth.ResetCodeTTLSeconds),
			AccessCookieName:       cloneStringPointer(document.Auth.AccessCookieName),
			RefreshCookieName:      cloneStringPointer(document.Auth.RefreshCookieName),
			CookieSecure:           cloneBoolPointer(document.Auth.CookieSecure),
		},
		Alerting:  safeBootstrapAlertingValues(document.Alerting),
		Mail:      safeBootstrapMailValues(document.Mail),
		Telemetry: safeBootstrapTelemetryValues(document.Telemetry),
	}
}

func canonicalBootstrapConfigPayload(document bootstrapConfigDocument) ([]byte, error) {
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal canonical bootstrap config JSON: %w", err)
	}
	return payload, nil
}

func bootstrapConfigETag(payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func safeBootstrapServerValues(server *bootstrapServer) *BootstrapConfigServerValues {
	if server == nil {
		return nil
	}
	return &BootstrapConfigServerValues{Host: cloneStringPointer(server.Host), Port: cloneIntPointer(server.Port)}
}

func safeBootstrapDatabaseValues(database *bootstrapDatabase) *BootstrapConfigDatabaseValues {
	if database == nil {
		return nil
	}
	return &BootstrapConfigDatabaseValues{
		Pools:               safeBootstrapDatabasePoolsValues(database.Pools),
		ManagementAdmission: safeBootstrapManagementAdmissionValues(database.ManagementAdmission),
	}
}

func safeBootstrapDatabasePoolsValues(pools *bootstrapDatabasePools) *BootstrapConfigDatabasePoolsValues {
	if pools == nil {
		return nil
	}
	return &BootstrapConfigDatabasePoolsValues{
		TotalMaxConns:    cloneIntPointer(pools.TotalMaxConns),
		Management:       safeBootstrapDatabasePoolValues(pools.Management),
		RuntimeExecution: safeBootstrapDatabasePoolValues(pools.RuntimeExecution),
		RuntimeTelemetry: safeBootstrapDatabasePoolValues(pools.RuntimeTelemetry),
		RuntimeFeedback:  safeBootstrapDatabasePoolValues(pools.RuntimeFeedback),
		Realtime:         safeBootstrapDatabasePoolValues(pools.Realtime),
		CacheRefresh:     safeBootstrapDatabasePoolValues(pools.CacheRefresh),
		BackgroundJobs:   safeBootstrapDatabasePoolValues(pools.BackgroundJobs),
	}
}

func safeBootstrapDatabasePoolValues(pool *bootstrapDatabasePool) *BootstrapConfigDatabasePoolValues {
	if pool == nil {
		return nil
	}
	return &BootstrapConfigDatabasePoolValues{MaxConns: cloneIntPointer(pool.MaxConns), MinIdleConns: cloneIntPointer(pool.MinIdleConns)}
}

func safeBootstrapManagementAdmissionValues(admission *bootstrapManagementAdmission) *BootstrapConfigManagementAdmissionValues {
	if admission == nil {
		return nil
	}
	return &BootstrapConfigManagementAdmissionValues{M2MaxConcurrent: cloneIntPointer(admission.M2MaxConcurrent), M3MaxConcurrent: cloneIntPointer(admission.M3MaxConcurrent)}
}

func safeBootstrapRuntimeValues(runtimeConfig *bootstrapRuntime) *BootstrapConfigRuntimeValues {
	if runtimeConfig == nil {
		return nil
	}
	return &BootstrapConfigRuntimeValues{
		SideEffects: safeBootstrapRuntimeSideEffectsValues(runtimeConfig.SideEffects),
		Routing:     safeBootstrapRuntimeRoutingValues(runtimeConfig.Routing),
	}
}

func safeBootstrapRuntimeSideEffectsValues(sideEffects *bootstrapRuntimeSideEffects) *BootstrapConfigRuntimeSideEffectsValues {
	if sideEffects == nil {
		return nil
	}
	return &BootstrapConfigRuntimeSideEffectsValues{AttemptTimeout: cloneStringPointer(sideEffects.AttemptTimeout)}
}

func safeBootstrapRuntimeRoutingValues(*bootstrapRuntimeRouting) *BootstrapConfigRuntimeRoutingValues {
	return defaultSafeBootstrapRuntimeRoutingValues()
}

func safeBootstrapHTTPValues(httpConfig *bootstrapHTTP) *BootstrapConfigHTTPValues {
	if httpConfig == nil {
		return nil
	}
	return &BootstrapConfigHTTPValues{CORSAllowedOrigins: cloneStringSlicePointer(httpConfig.CORSAllowedOrigins)}
}

func safeBootstrapAuthValues(auth *bootstrapAuth) *BootstrapConfigAuthValues {
	if auth == nil {
		return nil
	}
	return &BootstrapConfigAuthValues{
		AccessTokenTTLSeconds:  cloneIntPointer(auth.AccessTokenTTLSeconds),
		RefreshTokenTTLSeconds: cloneIntPointer(auth.RefreshTokenTTLSeconds),
		ResetCodeTTLSeconds:    cloneIntPointer(auth.ResetCodeTTLSeconds),
		AccessCookieName:       cloneStringPointer(auth.AccessCookieName),
		RefreshCookieName:      cloneStringPointer(auth.RefreshCookieName),
		CookieSecure:           cloneBoolPointer(auth.CookieSecure),
	}
}

func safeBootstrapAlertingValues(alerting *bootstrapAlerting) *BootstrapConfigAlertingValues {
	if alerting == nil {
		return &BootstrapConfigAlertingValues{WebhookURL: stringPointer("")}
	}
	return &BootstrapConfigAlertingValues{WebhookURL: cloneStringPointer(alerting.WebhookURL)}
}

func safeBootstrapMailValues(mailConfig *bootstrapMail) *BootstrapConfigMailValues {
	if mailConfig == nil {
		return canonicalDisabledBootstrapMailValues()
	}
	return &BootstrapConfigMailValues{
		Enabled: cloneBoolPointer(mailConfig.Enabled),
		From:    cloneStringPointer(mailConfig.From),
		ReplyTo: cloneStringPointer(mailConfig.ReplyTo),
		SMTP:    safeBootstrapSMTPValues(mailConfig.SMTP),
	}
}

func canonicalDisabledBootstrapMailValues() *BootstrapConfigMailValues {
	return &BootstrapConfigMailValues{Enabled: boolPointer(false)}
}

func canonicalDisabledBootstrapMailDocument() *bootstrapMail {
	return &bootstrapMail{Enabled: boolPointer(false)}
}

func safeBootstrapSMTPValues(smtp *bootstrapSMTP) *BootstrapConfigMailSMTPValues {
	if smtp == nil {
		return nil
	}
	return &BootstrapConfigMailSMTPValues{
		Host:          cloneStringPointer(smtp.Host),
		Port:          cloneIntPointer(smtp.Port),
		Mode:          cloneStringPointer(smtp.Mode),
		EHLOHostname:  cloneStringPointer(smtp.EHLOHostname),
		Auth:          cloneStringPointer(smtp.Auth),
		Username:      cloneStringPointer(smtp.Username),
		PasswordFile:  cloneStringPointer(smtp.PasswordFile),
		Timeout:       cloneStringPointer(smtp.Timeout),
		TLSServerName: cloneStringPointer(smtp.TLSServerName),
	}
}

func safeBootstrapTelemetryValues(telemetry *bootstrapTelemetry) *BootstrapConfigTelemetryValues {
	if telemetry == nil {
		return canonicalDisabledBootstrapTelemetryValues()
	}
	return &BootstrapConfigTelemetryValues{
		Enabled:  cloneBoolPointer(telemetry.Enabled),
		Exporter: safeBootstrapTelemetryExporterValues(telemetry.Exporter),
		Metrics:  safeBootstrapTelemetrySignalValues(telemetry.Metrics),
		Traces:   safeBootstrapTelemetryTracesValues(telemetry.Traces),
	}
}

func canonicalDisabledBootstrapTelemetryValues() *BootstrapConfigTelemetryValues {
	return &BootstrapConfigTelemetryValues{Enabled: boolPointer(false)}
}

func canonicalDisabledBootstrapTelemetryDocument() *bootstrapTelemetry {
	return &bootstrapTelemetry{Enabled: boolPointer(false)}
}

func safeBootstrapTelemetryExporterValues(exporter *bootstrapTelemetryExporter) *BootstrapConfigTelemetryExporterValues {
	if exporter == nil {
		return nil
	}
	return &BootstrapConfigTelemetryExporterValues{
		Endpoint:    cloneStringPointer(exporter.Endpoint),
		Protocol:    cloneStringPointer(exporter.Protocol),
		Compression: cloneStringPointer(exporter.Compression),
		Timeout:     cloneStringPointer(exporter.Timeout),
		Auth:        safeBootstrapTelemetryExporterAuthValues(exporter.Auth),
		TLS:         safeBootstrapTelemetryExporterTLSValues(exporter.TLS),
	}
}

func safeBootstrapTelemetryExporterAuthValues(auth *bootstrapTelemetryExporterAuth) *BootstrapConfigTelemetryExporterAuthValues {
	if auth == nil {
		return nil
	}
	return &BootstrapConfigTelemetryExporterAuthValues{Mode: cloneStringPointer(auth.Mode)}
}

func safeBootstrapTelemetryExporterTLSValues(tlsConfig *bootstrapTelemetryExporterTLS) *BootstrapConfigTelemetryExporterTLSValues {
	if tlsConfig == nil {
		return nil
	}
	return &BootstrapConfigTelemetryExporterTLSValues{
		InsecureSkipVerify: cloneBoolPointer(tlsConfig.InsecureSkipVerify),
		CAFile:             cloneStringPointer(tlsConfig.CAFile),
	}
}

func safeBootstrapTelemetrySignalValues(signal *bootstrapTelemetrySignal) *BootstrapConfigTelemetrySignalValues {
	if signal == nil {
		return nil
	}
	return &BootstrapConfigTelemetrySignalValues{Enabled: cloneBoolPointer(signal.Enabled)}
}

func safeBootstrapTelemetryTracesValues(traces *bootstrapTelemetryTraces) *BootstrapConfigTelemetryTracesValues {
	if traces == nil {
		return nil
	}
	return &BootstrapConfigTelemetryTracesValues{Enabled: cloneBoolPointer(traces.Enabled), SamplingRatio: cloneFloat64Pointer(traces.SamplingRatio)}
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	return intPointer(*value)
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPointer(*value)
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	return boolPointer(*value)
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	return float64Pointer(*value)
}

func cloneStringSlicePointer(value *[]string) *[]string {
	if value == nil {
		return nil
	}
	clone := append([]string(nil), (*value)...)
	return &clone
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

type BootstrapConfigApplyMode string

const (
	BootstrapConfigApplyModeHotApply        BootstrapConfigApplyMode = "hot_apply"
	BootstrapConfigApplyModeRestartRequired BootstrapConfigApplyMode = "restart_required"
)

type BootstrapConfigFieldCapability struct {
	Mode              BootstrapConfigApplyMode `json:"mode"`
	ConfirmationToken string                   `json:"confirmation_token,omitempty"`
}

type BootstrapConfigFieldChange struct {
	Field string                   `json:"field"`
	Mode  BootstrapConfigApplyMode `json:"mode"`
}

type BootstrapConfigFieldDiff struct {
	ChangedHotApplyFields        []string `json:"changed_hot_apply_fields"`
	ChangedRestartRequiredFields []string `json:"changed_restart_required_fields"`
	UnchangedFields              []string `json:"unchanged_fields"`
	UnknownFields                []string `json:"unknown_fields,omitempty"`
}

type BootstrapConfigHotApplyRetiredResources interface {
	CloseIdleConnections()
}

type BootstrapConfigHotApplyRuntime interface {
	Validate(Settings) error
	Publish(Settings) (BootstrapConfigHotApplyRetiredResources, error)
}

func (d BootstrapConfigFieldDiff) RestartRequired() bool {
	return len(d.ChangedRestartRequiredFields) > 0
}

func (d BootstrapConfigFieldDiff) HasChanges() bool {
	return len(d.ChangedHotApplyFields) > 0 || len(d.ChangedRestartRequiredFields) > 0
}

func (d BootstrapConfigFieldDiff) ChangedFields() []BootstrapConfigFieldChange {
	hot := stringSet(d.ChangedHotApplyFields)
	restart := stringSet(d.ChangedRestartRequiredFields)
	changes := make([]BootstrapConfigFieldChange, 0, len(d.ChangedHotApplyFields)+len(d.ChangedRestartRequiredFields))
	for _, field := range BootstrapConfigApplyCapabilityFields() {
		if _, ok := hot[field]; ok {
			changes = append(changes, BootstrapConfigFieldChange{Field: field, Mode: BootstrapConfigApplyModeHotApply})
			continue
		}
		if _, ok := restart[field]; ok {
			changes = append(changes, BootstrapConfigFieldChange{Field: field, Mode: BootstrapConfigApplyModeRestartRequired})
		}
	}
	return changes
}

type BootstrapConfigFieldClassificationError struct {
	Fields []string
}

func (e *BootstrapConfigFieldClassificationError) Error() string {
	if len(e.Fields) == 1 {
		return fmt.Sprintf("bootstrap config field %s is not classified", e.Fields[0])
	}
	return fmt.Sprintf("bootstrap config fields are not classified: %s", strings.Join(e.Fields, ", "))
}

const (
	bootstrapFieldHTTPCORSAllowedOrigins                 = "http.cors_allowed_origins"
	bootstrapFieldAuthAccessTokenTTLSeconds              = "auth.access_token_ttl_seconds"
	bootstrapFieldAuthRefreshTokenTTLSeconds             = "auth.refresh_token_ttl_seconds"
	bootstrapFieldAuthAccessCookieName                   = "auth.access_cookie_name"
	bootstrapFieldAuthRefreshCookieName                  = "auth.refresh_cookie_name"
	bootstrapFieldAuthCookieSecure                       = "auth.cookie_secure"
	bootstrapFieldAlertingWebhookURL                     = "alerting.webhookUrl"
	bootstrapFieldRuntimeTransportMaxIdleConns           = "transport.max_idle_conns"
	bootstrapFieldRuntimeTransportMaxIdleConnsPerHost    = "transport.max_idle_conns_per_host"
	bootstrapFieldRuntimeTransportMaxConnsPerHost        = "transport.max_conns_per_host"
	bootstrapFieldRuntimeTransportIdleConnTimeout        = "transport.idle_conn_timeout"
	bootstrapFieldRuntimeTransportRequestTimeout         = "transport.request_timeout"
	bootstrapFieldRuntimeTransportResponseHeaderTimeout  = "transport.response_header_timeout"
	bootstrapFieldRuntimeTransportTLSHandshakeTimeout    = "transport.tls_handshake_timeout"
	bootstrapFieldRuntimeTransportExpectContinueTimeout  = "transport.expect_continue_timeout"
	bootstrapFieldRuntimeSideEffectsAttemptTimeout       = "side_effects.attempt_timeout"
	bootstrapFieldTelemetryEnabled                       = "telemetry.enabled"
	bootstrapFieldTelemetryExporterEndpoint              = "telemetry.exporter.endpoint"
	bootstrapFieldTelemetryExporterProtocol              = "telemetry.exporter.protocol"
	bootstrapFieldTelemetryExporterCompression           = "telemetry.exporter.compression"
	bootstrapFieldTelemetryExporterTimeout               = "telemetry.exporter.timeout"
	bootstrapFieldTelemetryExporterAuthMode              = "telemetry.exporter.auth.mode"
	bootstrapFieldTelemetryExporterTLSInsecureSkipVerify = "telemetry.exporter.tls.insecure_skip_verify"
	bootstrapFieldTelemetryExporterTLSCAFile             = "telemetry.exporter.tls.ca_file"
	bootstrapFieldTelemetryMetricsEnabled                = "telemetry.metrics.enabled"
	bootstrapFieldTelemetryTracesEnabled                 = "telemetry.traces.enabled"
	bootstrapFieldTelemetryTracesSamplingRatio           = "telemetry.traces.sampling_ratio"
	bootstrapFieldDatabaseManagementAdmissionM2Max       = "database.management_admission.m2_max_concurrent"
	bootstrapFieldDatabaseManagementAdmissionM3Max       = "database.management_admission.m3_max_concurrent"
	bootstrapFieldServerHost                             = "server.host"
	bootstrapFieldServerPort                             = "server.port"
	bootstrapFieldDatabasePoolsTotalMaxConns             = "database.pools.total_max_conns"
	bootstrapFieldDatabasePoolsManagementMaxConns        = "database.pools.management.max_conns"
	bootstrapFieldDatabasePoolsManagementMinIdleConns    = "database.pools.management.min_idle_conns"
	bootstrapFieldDatabasePoolsRuntimeExecutionMaxConns  = "database.pools.runtime_execution.max_conns"
	bootstrapFieldDatabasePoolsRuntimeExecutionMinIdle   = "database.pools.runtime_execution.min_idle_conns"
	bootstrapFieldDatabasePoolsRuntimeTelemetryMaxConns  = "database.pools.runtime_telemetry.max_conns"
	bootstrapFieldDatabasePoolsRuntimeTelemetryMinIdle   = "database.pools.runtime_telemetry.min_idle_conns"
	bootstrapFieldDatabasePoolsRuntimeFeedbackMaxConns   = "database.pools.runtime_feedback.max_conns"
	bootstrapFieldDatabasePoolsRuntimeFeedbackMinIdle    = "database.pools.runtime_feedback.min_idle_conns"
	bootstrapFieldDatabasePoolsCacheRefreshMaxConns      = "database.pools.cache_refresh.max_conns"
	bootstrapFieldDatabasePoolsCacheRefreshMinIdle       = "database.pools.cache_refresh.min_idle_conns"
	bootstrapFieldDatabasePoolsBackgroundJobsMaxConns    = "database.pools.background_jobs.max_conns"
	bootstrapFieldDatabasePoolsBackgroundJobsMinIdle     = "database.pools.background_jobs.min_idle_conns"
)

type bootstrapConfigFieldRegistration struct {
	field      string
	capability BootstrapConfigFieldCapability
}

var bootstrapConfigFieldRegistry = []bootstrapConfigFieldRegistration{
	hotApplyBootstrapField(bootstrapFieldHTTPCORSAllowedOrigins),
	hotApplyBootstrapField(bootstrapFieldAuthAccessTokenTTLSeconds),
	hotApplyBootstrapField(bootstrapFieldAuthRefreshTokenTTLSeconds),
	hotApplyBootstrapField(bootstrapFieldAuthAccessCookieName),
	hotApplyBootstrapField(bootstrapFieldAuthRefreshCookieName),
	hotApplyBootstrapField(bootstrapFieldAuthCookieSecure),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportMaxIdleConns),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportMaxIdleConnsPerHost),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportMaxConnsPerHost),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportIdleConnTimeout),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportRequestTimeout),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportResponseHeaderTimeout),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportTLSHandshakeTimeout),
	hotApplyBootstrapField(bootstrapFieldRuntimeTransportExpectContinueTimeout),
	hotApplyBootstrapField(bootstrapFieldDatabaseManagementAdmissionM2Max),
	hotApplyBootstrapField(bootstrapFieldDatabaseManagementAdmissionM3Max),
	hotApplyBootstrapField(bootstrapFieldAlertingWebhookURL),
	restartRequiredBootstrapField(bootstrapFieldRuntimeSideEffectsAttemptTimeout, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryEnabled, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryExporterEndpoint, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryExporterProtocol, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryExporterCompression, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryExporterTimeout, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryExporterAuthMode, ""),
	restartRequiredBootstrapField(BootstrapConfigSecretTelemetryAuthorizationHeader, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryExporterTLSInsecureSkipVerify, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryExporterTLSCAFile, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryMetricsEnabled, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryTracesEnabled, ""),
	restartRequiredBootstrapField(bootstrapFieldTelemetryTracesSamplingRatio, ""),
	restartRequiredBootstrapField(bootstrapFieldServerHost, BootstrapConfigConfirmationServerHostChange),
	restartRequiredBootstrapField(bootstrapFieldServerPort, BootstrapConfigConfirmationServerPortChange),
	restartRequiredBootstrapField(BootstrapConfigSecretDatabaseURL, BootstrapConfigConfirmationDatabaseURLChange),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsTotalMaxConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsManagementMaxConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsManagementMinIdleConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsRuntimeExecutionMaxConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsRuntimeExecutionMinIdle, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsRuntimeTelemetryMaxConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsRuntimeTelemetryMinIdle, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsRuntimeFeedbackMaxConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsRuntimeFeedbackMinIdle, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsCacheRefreshMaxConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsCacheRefreshMinIdle, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsBackgroundJobsMaxConns, ""),
	restartRequiredBootstrapField(bootstrapFieldDatabasePoolsBackgroundJobsMinIdle, ""),
	restartRequiredBootstrapField(BootstrapConfigSecretRuntimeSecretEncryptionKey, ""),
	restartRequiredBootstrapField(BootstrapConfigSecretAuthJWTSigningKey, BootstrapConfigConfirmationAuthJWTSigningKeyChange),
}

var bootstrapConfigFieldCapabilityByPath = bootstrapConfigFieldRegistryMap()

func hotApplyBootstrapField(field string) bootstrapConfigFieldRegistration {
	return bootstrapConfigFieldRegistration{field: field, capability: BootstrapConfigFieldCapability{Mode: BootstrapConfigApplyModeHotApply}}
}

func restartRequiredBootstrapField(field string, confirmationToken string) bootstrapConfigFieldRegistration {
	return bootstrapConfigFieldRegistration{
		field: field,
		capability: BootstrapConfigFieldCapability{
			Mode:              BootstrapConfigApplyModeRestartRequired,
			ConfirmationToken: confirmationToken,
		},
	}
}

func BootstrapConfigApplyCapabilities() map[string]BootstrapConfigFieldCapability {
	capabilities := make(map[string]BootstrapConfigFieldCapability, len(bootstrapConfigFieldRegistry))
	for _, registration := range bootstrapConfigFieldRegistry {
		capabilities[registration.field] = registration.capability
	}
	return capabilities
}

func BootstrapConfigApplyCapabilityFields() []string {
	fields := make([]string, 0, len(bootstrapConfigFieldRegistry))
	for _, registration := range bootstrapConfigFieldRegistry {
		fields = append(fields, registration.field)
	}
	return fields
}

func ClassifyBootstrapConfigField(field string) (BootstrapConfigFieldCapability, bool) {
	capability, ok := bootstrapConfigFieldCapabilityByPath[strings.TrimSpace(field)]
	return capability, ok
}

func DiffBootstrapConfigFields(current BootstrapConfigValues, requested BootstrapConfigValues, secretUpdates map[string]BootstrapConfigSecretUpdate) (BootstrapConfigFieldDiff, error) {
	currentFields := bootstrapConfigSafeFieldValues(current)
	requestedFields := bootstrapConfigSafeFieldValues(requested)
	unknownFields := bootstrapConfigUnknownFieldPaths(currentFields, requestedFields, secretUpdates)
	if len(unknownFields) > 0 {
		return BootstrapConfigFieldDiff{UnknownFields: unknownFields}, &BootstrapConfigFieldClassificationError{Fields: unknownFields}
	}

	changed := make(map[string]struct{})
	for field, currentValue := range currentFields {
		if !currentValue.equal(requestedFields[field]) {
			changed[field] = struct{}{}
		}
	}
	for field, update := range secretUpdates {
		if update.Action == BootstrapConfigSecretActionReplace || update.Action == BootstrapConfigSecretActionClear {
			changed[field] = struct{}{}
		}
	}

	diff := BootstrapConfigFieldDiff{}
	for _, field := range BootstrapConfigApplyCapabilityFields() {
		capability := bootstrapConfigFieldCapabilityByPath[field]
		if _, ok := changed[field]; !ok {
			diff.UnchangedFields = append(diff.UnchangedFields, field)
			continue
		}
		if capability.Mode == BootstrapConfigApplyModeHotApply {
			diff.ChangedHotApplyFields = append(diff.ChangedHotApplyFields, field)
			continue
		}
		diff.ChangedRestartRequiredFields = append(diff.ChangedRestartRequiredFields, field)
	}
	return diff, nil
}

func bootstrapConfigFieldRegistryMap() map[string]BootstrapConfigFieldCapability {
	capabilities := make(map[string]BootstrapConfigFieldCapability, len(bootstrapConfigFieldRegistry))
	for _, registration := range bootstrapConfigFieldRegistry {
		capabilities[registration.field] = registration.capability
	}
	return capabilities
}

func DiffBootstrapConfigSettings(current Settings, requested Settings) (BootstrapConfigFieldDiff, error) {
	return DiffBootstrapConfigFields(
		bootstrapConfigValuesFromSettings(current),
		bootstrapConfigValuesFromSettings(requested),
		bootstrapConfigSecretUpdatesForSettingsDiff(current, requested),
	)
}

func bootstrapConfigUnknownFieldPaths(current map[string]bootstrapConfigFieldValue, requested map[string]bootstrapConfigFieldValue, secretUpdates map[string]BootstrapConfigSecretUpdate) []string {
	unknown := make(map[string]struct{})
	for field := range current {
		if _, ok := ClassifyBootstrapConfigField(field); !ok {
			unknown[field] = struct{}{}
		}
	}
	for field := range requested {
		if _, ok := ClassifyBootstrapConfigField(field); !ok {
			unknown[field] = struct{}{}
		}
	}
	for field := range secretUpdates {
		if _, ok := ClassifyBootstrapConfigField(field); !ok {
			unknown[field] = struct{}{}
		}
	}
	fields := make([]string, 0, len(unknown))
	for field := range unknown {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	return fields
}

type bootstrapConfigFieldValue struct {
	present bool
	value   any
}

func (v bootstrapConfigFieldValue) equal(other bootstrapConfigFieldValue) bool {
	return v.present == other.present && reflect.DeepEqual(v.value, other.value)
}

func bootstrapConfigSafeFieldValues(values BootstrapConfigValues) map[string]bootstrapConfigFieldValue {
	fields := make(map[string]bootstrapConfigFieldValue)
	addBootstrapServerFieldValues(fields, values.Server)
	addBootstrapDatabaseFieldValues(fields, values.Database)
	addBootstrapRuntimeFieldValues(fields, values.Runtime)
	addBootstrapHTTPFieldValues(fields, values.HTTP)
	addBootstrapAuthFieldValues(fields, values.Auth)
	addBootstrapAlertingFieldValues(fields, values.Alerting)
	addBootstrapTelemetryFieldValues(fields, values.Telemetry)
	return fields
}

func addBootstrapServerFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigServerValues) {
	if values == nil {
		fields[bootstrapFieldServerHost] = bootstrapStringFieldValue(nil)
		fields[bootstrapFieldServerPort] = bootstrapIntFieldValue(nil)
		return
	}
	fields[bootstrapFieldServerHost] = bootstrapStringFieldValue(values.Host)
	fields[bootstrapFieldServerPort] = bootstrapIntFieldValue(values.Port)
}

func addBootstrapDatabaseFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigDatabaseValues) {
	if values == nil {
		addBootstrapDatabasePoolFieldValues(fields, nil)
		fields[bootstrapFieldDatabaseManagementAdmissionM2Max] = bootstrapIntFieldValue(nil)
		fields[bootstrapFieldDatabaseManagementAdmissionM3Max] = bootstrapIntFieldValue(nil)
		return
	}
	addBootstrapDatabasePoolFieldValues(fields, values.Pools)
	if values.ManagementAdmission == nil {
		fields[bootstrapFieldDatabaseManagementAdmissionM2Max] = bootstrapIntFieldValue(nil)
		fields[bootstrapFieldDatabaseManagementAdmissionM3Max] = bootstrapIntFieldValue(nil)
		return
	}
	fields[bootstrapFieldDatabaseManagementAdmissionM2Max] = bootstrapIntFieldValue(values.ManagementAdmission.M2MaxConcurrent)
	fields[bootstrapFieldDatabaseManagementAdmissionM3Max] = bootstrapIntFieldValue(values.ManagementAdmission.M3MaxConcurrent)
}

func addBootstrapDatabasePoolFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigDatabasePoolsValues) {
	if values == nil {
		fields[bootstrapFieldDatabasePoolsTotalMaxConns] = bootstrapIntFieldValue(nil)
		addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsManagementMaxConns, bootstrapFieldDatabasePoolsManagementMinIdleConns, nil)
		addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsRuntimeExecutionMaxConns, bootstrapFieldDatabasePoolsRuntimeExecutionMinIdle, nil)
		addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsRuntimeTelemetryMaxConns, bootstrapFieldDatabasePoolsRuntimeTelemetryMinIdle, nil)
		addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsRuntimeFeedbackMaxConns, bootstrapFieldDatabasePoolsRuntimeFeedbackMinIdle, nil)
		addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsCacheRefreshMaxConns, bootstrapFieldDatabasePoolsCacheRefreshMinIdle, nil)
		addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsBackgroundJobsMaxConns, bootstrapFieldDatabasePoolsBackgroundJobsMinIdle, nil)
		return
	}
	fields[bootstrapFieldDatabasePoolsTotalMaxConns] = bootstrapIntFieldValue(values.TotalMaxConns)
	addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsManagementMaxConns, bootstrapFieldDatabasePoolsManagementMinIdleConns, values.Management)
	addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsRuntimeExecutionMaxConns, bootstrapFieldDatabasePoolsRuntimeExecutionMinIdle, values.RuntimeExecution)
	addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsRuntimeTelemetryMaxConns, bootstrapFieldDatabasePoolsRuntimeTelemetryMinIdle, values.RuntimeTelemetry)
	addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsRuntimeFeedbackMaxConns, bootstrapFieldDatabasePoolsRuntimeFeedbackMinIdle, values.RuntimeFeedback)
	addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsCacheRefreshMaxConns, bootstrapFieldDatabasePoolsCacheRefreshMinIdle, values.CacheRefresh)
	addBootstrapDatabasePoolLaneFieldValues(fields, bootstrapFieldDatabasePoolsBackgroundJobsMaxConns, bootstrapFieldDatabasePoolsBackgroundJobsMinIdle, values.BackgroundJobs)
}

func addBootstrapDatabasePoolLaneFieldValues(fields map[string]bootstrapConfigFieldValue, maxConnsField string, minIdleConnsField string, values *BootstrapConfigDatabasePoolValues) {
	if values == nil {
		fields[maxConnsField] = bootstrapIntFieldValue(nil)
		fields[minIdleConnsField] = bootstrapIntFieldValue(nil)
		return
	}
	fields[maxConnsField] = bootstrapIntFieldValue(values.MaxConns)
	fields[minIdleConnsField] = bootstrapIntFieldValue(values.MinIdleConns)
}

func addBootstrapRuntimeFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigRuntimeValues) {
	if values == nil {
		addBootstrapRuntimeTransportFieldValues(fields, nil)
		addBootstrapRuntimeSideEffectsFieldValues(fields, nil)
		addBootstrapRuntimeRoutingFieldValues(fields, nil)
		return
	}
	addBootstrapRuntimeTransportFieldValues(fields, values.Transport)
	addBootstrapRuntimeSideEffectsFieldValues(fields, values.SideEffects)
	addBootstrapRuntimeRoutingFieldValues(fields, values.Routing)
}

func addBootstrapRuntimeSideEffectsFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigRuntimeSideEffectsValues) {
	if values == nil {
		fields[bootstrapFieldRuntimeSideEffectsAttemptTimeout] = bootstrapDurationFieldValue(nil)
		return
	}
	fields[bootstrapFieldRuntimeSideEffectsAttemptTimeout] = bootstrapDurationFieldValue(values.AttemptTimeout)
}

func addBootstrapRuntimeRoutingFieldValues(map[string]bootstrapConfigFieldValue, *BootstrapConfigRuntimeRoutingValues) {
}

func addBootstrapRuntimeTransportFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigRuntimeTransportValues) {
	if values == nil {
		fields[bootstrapFieldRuntimeTransportMaxIdleConns] = bootstrapIntFieldValue(nil)
		fields[bootstrapFieldRuntimeTransportMaxIdleConnsPerHost] = bootstrapIntFieldValue(nil)
		fields[bootstrapFieldRuntimeTransportMaxConnsPerHost] = bootstrapIntFieldValue(nil)
		fields[bootstrapFieldRuntimeTransportIdleConnTimeout] = bootstrapDurationFieldValue(nil)
		fields[bootstrapFieldRuntimeTransportRequestTimeout] = bootstrapDurationFieldValue(nil)
		fields[bootstrapFieldRuntimeTransportResponseHeaderTimeout] = bootstrapDurationFieldValue(nil)
		fields[bootstrapFieldRuntimeTransportTLSHandshakeTimeout] = bootstrapDurationFieldValue(nil)
		fields[bootstrapFieldRuntimeTransportExpectContinueTimeout] = bootstrapDurationFieldValue(nil)
		return
	}
	fields[bootstrapFieldRuntimeTransportMaxIdleConns] = bootstrapIntFieldValue(values.MaxIdleConns)
	fields[bootstrapFieldRuntimeTransportMaxIdleConnsPerHost] = bootstrapIntFieldValue(values.MaxIdleConnsPerHost)
	fields[bootstrapFieldRuntimeTransportMaxConnsPerHost] = bootstrapIntFieldValue(values.MaxConnsPerHost)
	fields[bootstrapFieldRuntimeTransportIdleConnTimeout] = bootstrapDurationFieldValue(values.IdleConnTimeout)
	fields[bootstrapFieldRuntimeTransportRequestTimeout] = bootstrapDurationFieldValue(values.RequestTimeout)
	fields[bootstrapFieldRuntimeTransportResponseHeaderTimeout] = bootstrapDurationFieldValue(values.ResponseHeaderTimeout)
	fields[bootstrapFieldRuntimeTransportTLSHandshakeTimeout] = bootstrapDurationFieldValue(values.TLSHandshakeTimeout)
	fields[bootstrapFieldRuntimeTransportExpectContinueTimeout] = bootstrapDurationFieldValue(values.ExpectContinueTimeout)
}

func addBootstrapHTTPFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigHTTPValues) {
	if values == nil {
		fields[bootstrapFieldHTTPCORSAllowedOrigins] = bootstrapStringSliceSetFieldValue(nil)
		return
	}
	fields[bootstrapFieldHTTPCORSAllowedOrigins] = bootstrapStringSliceSetFieldValue(values.CORSAllowedOrigins)
}

func addBootstrapAuthFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigAuthValues) {
	if values == nil {
		fields[bootstrapFieldAuthAccessTokenTTLSeconds] = bootstrapIntFieldValue(nil)
		fields[bootstrapFieldAuthRefreshTokenTTLSeconds] = bootstrapIntFieldValue(nil)
		fields[bootstrapFieldAuthAccessCookieName] = bootstrapStringFieldValue(nil)
		fields[bootstrapFieldAuthRefreshCookieName] = bootstrapStringFieldValue(nil)
		fields[bootstrapFieldAuthCookieSecure] = bootstrapBoolFieldValue(nil)
		return
	}
	fields[bootstrapFieldAuthAccessTokenTTLSeconds] = bootstrapIntFieldValue(values.AccessTokenTTLSeconds)
	fields[bootstrapFieldAuthRefreshTokenTTLSeconds] = bootstrapIntFieldValue(values.RefreshTokenTTLSeconds)
	fields[bootstrapFieldAuthAccessCookieName] = bootstrapStringFieldValue(values.AccessCookieName)
	fields[bootstrapFieldAuthRefreshCookieName] = bootstrapStringFieldValue(values.RefreshCookieName)
	fields[bootstrapFieldAuthCookieSecure] = bootstrapBoolFieldValue(values.CookieSecure)
}

func addBootstrapAlertingFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigAlertingValues) {
	if values == nil {
		fields[bootstrapFieldAlertingWebhookURL] = bootstrapOptionalStringFieldValue(nil)
		return
	}
	fields[bootstrapFieldAlertingWebhookURL] = bootstrapOptionalStringFieldValue(values.WebhookURL)
}

func addBootstrapTelemetryFieldValues(fields map[string]bootstrapConfigFieldValue, values *BootstrapConfigTelemetryValues) {
	if values == nil {
		fields[bootstrapFieldTelemetryEnabled] = bootstrapBoolFieldValue(nil)
		addBootstrapTelemetryExporterFieldValues(fields, nil)
		addBootstrapTelemetrySignalFieldValues(fields, bootstrapFieldTelemetryMetricsEnabled, nil)
		addBootstrapTelemetryTracesFieldValues(fields, nil)
		return
	}
	fields[bootstrapFieldTelemetryEnabled] = bootstrapBoolFieldValue(values.Enabled)
	addBootstrapTelemetryExporterFieldValues(fields, values.Exporter)
	addBootstrapTelemetrySignalFieldValues(fields, bootstrapFieldTelemetryMetricsEnabled, values.Metrics)
	addBootstrapTelemetryTracesFieldValues(fields, values.Traces)
}

func addBootstrapTelemetryExporterFieldValues(fields map[string]bootstrapConfigFieldValue, exporter *BootstrapConfigTelemetryExporterValues) {
	if exporter == nil {
		fields[bootstrapFieldTelemetryExporterEndpoint] = bootstrapOptionalStringFieldValue(nil)
		fields[bootstrapFieldTelemetryExporterProtocol] = bootstrapOptionalStringFieldValue(nil)
		fields[bootstrapFieldTelemetryExporterCompression] = bootstrapOptionalStringFieldValue(nil)
		fields[bootstrapFieldTelemetryExporterTimeout] = bootstrapOptionalDurationFieldValue(nil)
		fields[bootstrapFieldTelemetryExporterAuthMode] = bootstrapOptionalStringFieldValue(nil)
		fields[bootstrapFieldTelemetryExporterTLSInsecureSkipVerify] = bootstrapOptionalBoolFieldValue(nil)
		fields[bootstrapFieldTelemetryExporterTLSCAFile] = bootstrapOptionalStringFieldValue(nil)
		return
	}
	fields[bootstrapFieldTelemetryExporterEndpoint] = bootstrapOptionalStringFieldValue(exporter.Endpoint)
	fields[bootstrapFieldTelemetryExporterProtocol] = bootstrapOptionalStringFieldValue(exporter.Protocol)
	fields[bootstrapFieldTelemetryExporterCompression] = bootstrapOptionalStringFieldValue(exporter.Compression)
	fields[bootstrapFieldTelemetryExporterTimeout] = bootstrapOptionalDurationFieldValue(exporter.Timeout)
	if exporter.Auth == nil {
		fields[bootstrapFieldTelemetryExporterAuthMode] = bootstrapOptionalStringFieldValue(nil)
	} else {
		fields[bootstrapFieldTelemetryExporterAuthMode] = bootstrapOptionalStringFieldValue(exporter.Auth.Mode)
	}
	if exporter.TLS == nil {
		fields[bootstrapFieldTelemetryExporterTLSInsecureSkipVerify] = bootstrapOptionalBoolFieldValue(nil)
		fields[bootstrapFieldTelemetryExporterTLSCAFile] = bootstrapOptionalStringFieldValue(nil)
		return
	}
	fields[bootstrapFieldTelemetryExporterTLSInsecureSkipVerify] = bootstrapOptionalBoolFieldValue(exporter.TLS.InsecureSkipVerify)
	fields[bootstrapFieldTelemetryExporterTLSCAFile] = bootstrapOptionalStringFieldValue(exporter.TLS.CAFile)
}

func addBootstrapTelemetrySignalFieldValues(fields map[string]bootstrapConfigFieldValue, field string, signal *BootstrapConfigTelemetrySignalValues) {
	if signal == nil {
		fields[field] = bootstrapOptionalBoolFieldValue(nil)
		return
	}
	fields[field] = bootstrapOptionalBoolFieldValue(signal.Enabled)
}

func addBootstrapTelemetryTracesFieldValues(fields map[string]bootstrapConfigFieldValue, traces *BootstrapConfigTelemetryTracesValues) {
	if traces == nil {
		fields[bootstrapFieldTelemetryTracesEnabled] = bootstrapOptionalBoolFieldValue(nil)
		fields[bootstrapFieldTelemetryTracesSamplingRatio] = bootstrapOptionalFloat64FieldValue(nil)
		return
	}
	fields[bootstrapFieldTelemetryTracesEnabled] = bootstrapOptionalBoolFieldValue(traces.Enabled)
	fields[bootstrapFieldTelemetryTracesSamplingRatio] = bootstrapOptionalFloat64FieldValue(traces.SamplingRatio)
}

func bootstrapIntFieldValue(value *int) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{}
	}
	return bootstrapConfigFieldValue{present: true, value: *value}
}

func bootstrapBoolFieldValue(value *bool) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{}
	}
	return bootstrapConfigFieldValue{present: true, value: *value}
}

func bootstrapOptionalBoolFieldValue(value *bool) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{present: true, value: false}
	}
	return bootstrapConfigFieldValue{present: true, value: *value}
}

func bootstrapOptionalFloat64FieldValue(value *float64) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{present: true, value: float64(0)}
	}
	return bootstrapConfigFieldValue{present: true, value: *value}
}

func bootstrapStringFieldValue(value *string) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{}
	}
	return bootstrapConfigFieldValue{present: true, value: strings.TrimSpace(*value)}
}

func bootstrapOptionalStringFieldValue(value *string) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{present: true, value: ""}
	}
	return bootstrapConfigFieldValue{present: true, value: strings.TrimSpace(*value)}
}

func bootstrapDurationFieldValue(value *string) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{}
	}
	return bootstrapDurationValue(strings.TrimSpace(*value))
}

func bootstrapOptionalDurationFieldValue(value *string) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{present: true, value: time.Duration(0)}
	}
	return bootstrapDurationValue(strings.TrimSpace(*value))
}

func bootstrapDurationValue(value string) bootstrapConfigFieldValue {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return bootstrapConfigFieldValue{present: true, value: value}
	}
	return bootstrapConfigFieldValue{present: true, value: parsed}
}

func bootstrapStringSliceSetFieldValue(value *[]string) bootstrapConfigFieldValue {
	if value == nil {
		return bootstrapConfigFieldValue{}
	}
	resolved := make([]string, 0, len(*value))
	for _, item := range *value {
		resolved = append(resolved, strings.TrimSpace(item))
	}
	slices.Sort(resolved)
	return bootstrapConfigFieldValue{present: true, value: resolved}
}

func bootstrapConfigValuesFromSettings(settings Settings) BootstrapConfigValues {
	postgresPools := settings.PostgresPoolsBudgetOrDefault()
	runtimeTransport := settings.RuntimeTransport()
	runtimeSideEffects := settings.RuntimeSideEffects()
	managementAdmission := settings.ManagementAdmissionBudget()
	corsAllowedOrigins := settings.CORSAllowedOriginsList()
	requestTimeout := bootstrapRequestTimeoutString(runtimeTransport.RequestTimeout)
	idleConnTimeout := runtimeTransport.IdleConnTimeout.String()
	responseHeaderTimeout := runtimeTransport.ResponseHeaderTimeout.String()
	tlsHandshakeTimeout := runtimeTransport.TLSHandshakeTimeout.String()
	expectContinueTimeout := runtimeTransport.ExpectContinueTimeout.String()
	runtimeSideEffectsAttemptTimeout := runtimeSideEffects.AttemptTimeout.String()
	return BootstrapConfigValues{
		Server: &BootstrapConfigServerValues{
			Host: stringPointer(strings.TrimSpace(settings.Host)),
			Port: intPointer(settings.Port),
		},
		Database: &BootstrapConfigDatabaseValues{
			Pools: &BootstrapConfigDatabasePoolsValues{
				TotalMaxConns:    intPointer(int(postgresPools.TotalMaxConns)),
				Management:       bootstrapConfigDatabasePoolValuesFromBudget(postgresPools.Management),
				RuntimeExecution: bootstrapConfigDatabasePoolValuesFromBudget(postgresPools.RuntimeExecution),
				RuntimeTelemetry: bootstrapConfigDatabasePoolValuesFromBudget(postgresPools.RuntimeTelemetry),
				RuntimeFeedback:  bootstrapConfigDatabasePoolValuesFromBudget(postgresPools.RuntimeFeedback),
				CacheRefresh:     bootstrapConfigDatabasePoolValuesFromBudget(postgresPools.CacheRefresh),
				BackgroundJobs:   bootstrapConfigDatabasePoolValuesFromBudget(postgresPools.BackgroundJobs),
			},
			ManagementAdmission: &BootstrapConfigManagementAdmissionValues{
				M2MaxConcurrent: intPointer(int(managementAdmission.M2MaxConcurrent)),
				M3MaxConcurrent: intPointer(int(managementAdmission.M3MaxConcurrent)),
			},
		},
		Runtime: &BootstrapConfigRuntimeValues{
			Transport: &BootstrapConfigRuntimeTransportValues{
				MaxIdleConns:          intPointer(runtimeTransport.MaxIdleConns),
				MaxIdleConnsPerHost:   intPointer(runtimeTransport.MaxIdleConnsPerHost),
				MaxConnsPerHost:       intPointer(runtimeTransport.MaxConnsPerHost),
				RequestTimeout:        &requestTimeout,
				IdleConnTimeout:       &idleConnTimeout,
				ResponseHeaderTimeout: &responseHeaderTimeout,
				TLSHandshakeTimeout:   &tlsHandshakeTimeout,
				ExpectContinueTimeout: &expectContinueTimeout,
			},
			SideEffects: &BootstrapConfigRuntimeSideEffectsValues{
				AttemptTimeout: &runtimeSideEffectsAttemptTimeout,
			},
			Routing: defaultSafeBootstrapRuntimeRoutingValues(),
		},
		HTTP: &BootstrapConfigHTTPValues{CORSAllowedOrigins: &corsAllowedOrigins},
		Auth: &BootstrapConfigAuthValues{
			AccessTokenTTLSeconds:  intPointer(settings.AuthAccessTokenTTLSeconds),
			RefreshTokenTTLSeconds: intPointer(settings.AuthRefreshTokenTTLSeconds),
			AccessCookieName:       stringPointer(strings.TrimSpace(settings.AuthCookieName)),
			RefreshCookieName:      stringPointer(strings.TrimSpace(settings.AuthRefreshCookieName)),
			CookieSecure:           boolPointer(settings.AuthCookieSecure),
		},
		Alerting:  &BootstrapConfigAlertingValues{WebhookURL: stringPointer(strings.TrimSpace(settings.Alerting.WebhookURL))},
		Telemetry: bootstrapConfigTelemetryValuesFromSettings(settings.Telemetry),
	}
}

func bootstrapConfigDatabasePoolValuesFromBudget(budget DatabasePoolBudget) *BootstrapConfigDatabasePoolValues {
	return &BootstrapConfigDatabasePoolValues{MaxConns: intPointer(int(budget.MaxConns)), MinIdleConns: intPointer(int(budget.MinIdleConns))}
}

func bootstrapConfigTelemetryValuesFromSettings(telemetry TelemetryConfig) *BootstrapConfigTelemetryValues {
	values := &BootstrapConfigTelemetryValues{Enabled: boolPointer(telemetry.Enabled)}
	if !telemetry.Enabled {
		return values
	}
	timeout := telemetry.Exporter.Timeout.String()
	values.Exporter = &BootstrapConfigTelemetryExporterValues{
		Endpoint:    optionalStringPointer(telemetry.Exporter.Endpoint),
		Protocol:    stringPointer(string(telemetry.Exporter.Protocol)),
		Compression: stringPointer(string(telemetry.Exporter.Compression)),
		Timeout:     &timeout,
		Auth: &BootstrapConfigTelemetryExporterAuthValues{
			Mode: stringPointer(string(telemetry.Exporter.Auth.Mode)),
		},
		TLS: &BootstrapConfigTelemetryExporterTLSValues{
			InsecureSkipVerify: boolPointer(telemetry.Exporter.TLS.InsecureSkipVerify),
			CAFile:             optionalStringPointer(telemetry.Exporter.TLS.CAFile),
		},
	}
	values.Metrics = &BootstrapConfigTelemetrySignalValues{Enabled: boolPointer(telemetry.Metrics.Enabled)}
	values.Traces = &BootstrapConfigTelemetryTracesValues{
		Enabled:       boolPointer(telemetry.Traces.Enabled),
		SamplingRatio: float64Pointer(telemetry.Traces.SamplingRatio),
	}
	return values
}

func bootstrapConfigSecretUpdatesForSettingsDiff(current Settings, requested Settings) map[string]BootstrapConfigSecretUpdate {
	updates := map[string]BootstrapConfigSecretUpdate{
		BootstrapConfigSecretDatabaseURL:                  {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretRuntimeSecretEncryptionKey:   {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretAuthJWTSigningKey:            {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretTelemetryAuthorizationHeader: {Action: BootstrapConfigSecretActionPreserve},
	}
	markReplace := func(field string, currentValue string, requestedValue string) {
		currentTrimmed := strings.TrimSpace(currentValue)
		requestedTrimmed := strings.TrimSpace(requestedValue)
		if currentTrimmed == requestedTrimmed {
			return
		}
		if requestedTrimmed == "" {
			updates[field] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionClear}
			return
		}
		updates[field] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace}
	}
	markReplace(BootstrapConfigSecretDatabaseURL, current.DatabaseURL, requested.DatabaseURL)
	markReplace(BootstrapConfigSecretRuntimeSecretEncryptionKey, current.SecretEncryptionKey, requested.SecretEncryptionKey)
	markReplace(BootstrapConfigSecretAuthJWTSigningKey, current.AuthJWTSecret, requested.AuthJWTSecret)
	markReplace(BootstrapConfigSecretTelemetryAuthorizationHeader, current.Telemetry.Exporter.Auth.AuthorizationHeader, requested.Telemetry.Exporter.Auth.AuthorizationHeader)
	return updates
}

func optionalStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return stringPointer(trimmed)
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

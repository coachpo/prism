package config

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestBootstrapConfigApplyRegistryCoversPlanFields(t *testing.T) {
	hotFields := []string{
		bootstrapFieldHTTPCORSAllowedOrigins,
		bootstrapFieldAuthAccessTokenTTLSeconds,
		bootstrapFieldAuthRefreshTokenTTLSeconds,
		bootstrapFieldAuthAccessCookieName,
		bootstrapFieldAuthRefreshCookieName,
		bootstrapFieldAuthCookieSecure,
		bootstrapFieldRuntimeTransportMaxIdleConns,
		bootstrapFieldRuntimeTransportMaxIdleConnsPerHost,
		bootstrapFieldRuntimeTransportMaxConnsPerHost,
		bootstrapFieldRuntimeTransportIdleConnTimeout,
		bootstrapFieldRuntimeTransportRequestTimeout,
		bootstrapFieldRuntimeTransportResponseHeaderTimeout,
		bootstrapFieldRuntimeTransportTLSHandshakeTimeout,
		bootstrapFieldRuntimeTransportExpectContinueTimeout,
		bootstrapFieldDatabaseManagementAdmissionM2Max,
		bootstrapFieldDatabaseManagementAdmissionM3Max,
	}
	restartFields := []string{
		bootstrapFieldRuntimeSideEffectsAttemptTimeout,
		bootstrapFieldTelemetryEnabled,
		bootstrapFieldTelemetryExporterEndpoint,
		bootstrapFieldTelemetryExporterProtocol,
		bootstrapFieldTelemetryExporterCompression,
		bootstrapFieldTelemetryExporterTimeout,
		bootstrapFieldTelemetryExporterAuthMode,
		BootstrapConfigSecretTelemetryAuthorizationHeader,
		bootstrapFieldTelemetryExporterTLSInsecureSkipVerify,
		bootstrapFieldTelemetryExporterTLSCAFile,
		bootstrapFieldTelemetryMetricsEnabled,
		bootstrapFieldTelemetryTracesEnabled,
		bootstrapFieldTelemetryTracesSamplingRatio,
		bootstrapFieldServerHost,
		bootstrapFieldServerPort,
		BootstrapConfigSecretDatabaseURL,
		bootstrapFieldDatabasePoolsTotalMaxConns,
		bootstrapFieldDatabasePoolsManagementMaxConns,
		bootstrapFieldDatabasePoolsManagementMinIdleConns,
		bootstrapFieldDatabasePoolsRuntimeExecutionMaxConns,
		bootstrapFieldDatabasePoolsRuntimeExecutionMinIdle,
		bootstrapFieldDatabasePoolsRuntimeTelemetryMaxConns,
		bootstrapFieldDatabasePoolsRuntimeTelemetryMinIdle,
		bootstrapFieldDatabasePoolsRuntimeFeedbackMaxConns,
		bootstrapFieldDatabasePoolsRuntimeFeedbackMinIdle,
		bootstrapFieldDatabasePoolsRealtimeMaxConns,
		bootstrapFieldDatabasePoolsRealtimeMinIdleConns,
		bootstrapFieldDatabasePoolsCacheRefreshMaxConns,
		bootstrapFieldDatabasePoolsCacheRefreshMinIdle,
		bootstrapFieldDatabasePoolsBackgroundJobsMaxConns,
		bootstrapFieldDatabasePoolsBackgroundJobsMinIdle,
		BootstrapConfigSecretRuntimeSecretEncryptionKey,
		BootstrapConfigSecretAuthJWTSigningKey,
	}
	capabilities := BootstrapConfigApplyCapabilities()
	fields := BootstrapConfigApplyCapabilityFields()
	wantCount := len(hotFields) + len(restartFields)
	if len(capabilities) != wantCount || len(fields) != wantCount {
		t.Fatalf("expected %d capabilities, got map=%d fields=%d", wantCount, len(capabilities), len(fields))
	}
	assertBootstrapFieldModes(t, capabilities, hotFields, BootstrapConfigApplyModeHotApply)
	assertBootstrapFieldModes(t, capabilities, restartFields, BootstrapConfigApplyModeRestartRequired)
	assertBootstrapCapabilityOrder(t, fields, append(append([]string{}, hotFields...), restartFields...))
	assertBootstrapConfirmationToken(t, capabilities, bootstrapFieldServerHost, BootstrapConfigConfirmationServerHostChange)
	assertBootstrapConfirmationToken(t, capabilities, bootstrapFieldServerPort, BootstrapConfigConfirmationServerPortChange)
	assertBootstrapConfirmationToken(t, capabilities, BootstrapConfigSecretDatabaseURL, BootstrapConfigConfirmationDatabaseURLChange)
	assertBootstrapConfirmationToken(t, capabilities, BootstrapConfigSecretAuthJWTSigningKey, BootstrapConfigConfirmationAuthJWTSigningKeyChange)
	legacyBundlePath := "state" + "Transfer.bundle" + "EncryptionKey"
	if _, ok := ClassifyBootstrapConfigField(legacyBundlePath); ok {
		t.Fatal("expected removed legacy bundle key to stay out of the bootstrap API field registry")
	}
	if _, ok := ClassifyBootstrapConfigField("database.pools.management.*"); ok {
		t.Fatal("expected wildcard pool paths to stay out of the exact field registry")
	}
}

func TestBootstrapConfigApplyRegistryUsesCanonicalRuntimeSecretPath(t *testing.T) {
	capability, ok := ClassifyBootstrapConfigField("runtime.secretEncryptionKey")
	if !ok {
		t.Fatal("expected runtime.secretEncryptionKey to be the canonical runtime secret API field")
	}
	if capability.Mode != BootstrapConfigApplyModeRestartRequired {
		t.Fatalf("expected runtime.secretEncryptionKey to require restart, got %s", capability.Mode)
	}
	if _, ok := ClassifyBootstrapConfigField("secretEncryptionKey"); ok {
		t.Fatal("expected bare secretEncryptionKey to stay out of the bootstrap API field registry")
	}
}

func TestBootstrapConfigFieldDiffDetectsHotOnlyChanges(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	requested := cloneManagementValues(t, current)
	requested.HTTP.CORSAllowedOrigins = &[]string{"https://console.example.test"}
	nextAccessTokenTTL := *current.Auth.AccessTokenTTLSeconds + 60
	nextRequestTimeout := "301s"
	requested.Auth.AccessTokenTTLSeconds = &nextAccessTokenTTL
	requested.Runtime.Transport.RequestTimeout = &nextRequestTimeout
	diff, err := DiffBootstrapConfigFields(current, requested, preserveManagementSecretUpdates())
	if err != nil {
		t.Fatalf("diff hot-only bootstrap fields: %v", err)
	}
	assertBootstrapFieldsEqual(t, diff.ChangedHotApplyFields, []string{
		bootstrapFieldHTTPCORSAllowedOrigins,
		bootstrapFieldAuthAccessTokenTTLSeconds,
		bootstrapFieldRuntimeTransportRequestTimeout,
	})
	assertBootstrapFieldsEqual(t, diff.ChangedRestartRequiredFields, nil)
	if diff.RestartRequired() {
		t.Fatal("expected hot-only diff not to require restart")
	}
}

func TestBootstrapConfigFieldDiffDetectsRestartOnlyChanges(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	requested := cloneManagementValues(t, current)
	nextPort := *current.Server.Port + 1
	nextManagementMaxConns := *current.Database.Pools.Management.MaxConns + 1
	requested.Server.Port = &nextPort
	requested.Database.Pools.Management.MaxConns = &nextManagementMaxConns
	diff, err := DiffBootstrapConfigFields(current, requested, preserveManagementSecretUpdates())
	if err != nil {
		t.Fatalf("diff restart-only bootstrap fields: %v", err)
	}
	assertBootstrapFieldsEqual(t, diff.ChangedHotApplyFields, nil)
	assertBootstrapFieldsEqual(t, diff.ChangedRestartRequiredFields, []string{
		bootstrapFieldServerPort,
		bootstrapFieldDatabasePoolsManagementMaxConns,
	})
	if !diff.RestartRequired() {
		t.Fatal("expected restart-only diff to require restart")
	}
}

func TestTelemetryFieldsAreRestartRequired(t *testing.T) {
	capabilities := BootstrapConfigApplyCapabilities()
	telemetryFields := []string{
		bootstrapFieldTelemetryEnabled,
		bootstrapFieldTelemetryExporterEndpoint,
		bootstrapFieldTelemetryExporterProtocol,
		bootstrapFieldTelemetryExporterCompression,
		bootstrapFieldTelemetryExporterTimeout,
		bootstrapFieldTelemetryExporterAuthMode,
		BootstrapConfigSecretTelemetryAuthorizationHeader,
		bootstrapFieldTelemetryExporterTLSInsecureSkipVerify,
		bootstrapFieldTelemetryExporterTLSCAFile,
		bootstrapFieldTelemetryMetricsEnabled,
		bootstrapFieldTelemetryTracesEnabled,
		bootstrapFieldTelemetryTracesSamplingRatio,
	}
	assertBootstrapFieldModes(t, capabilities, telemetryFields, BootstrapConfigApplyModeRestartRequired)
}

func TestBootstrapConfigFieldDiffDetectsRuntimeSideEffectsAttemptTimeoutRestartOnly(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	requested := cloneManagementValues(t, current)
	nextAttemptTimeout := "15s"
	requested.Runtime.SideEffects.AttemptTimeout = &nextAttemptTimeout
	diff, err := DiffBootstrapConfigFields(current, requested, preserveManagementSecretUpdates())
	if err != nil {
		t.Fatalf("diff side-effects attempt timeout bootstrap field: %v", err)
	}
	assertBootstrapFieldsEqual(t, diff.ChangedHotApplyFields, nil)
	assertBootstrapFieldsEqual(t, diff.ChangedRestartRequiredFields, []string{bootstrapFieldRuntimeSideEffectsAttemptTimeout})
	assertBootstrapFieldChangesEqual(t, diff.ChangedFields(), []BootstrapConfigFieldChange{
		{Field: bootstrapFieldRuntimeSideEffectsAttemptTimeout, Mode: BootstrapConfigApplyModeRestartRequired},
	})
	if !diff.RestartRequired() {
		t.Fatal("expected side-effects attempt timeout diff to require restart")
	}
}

func TestBootstrapConfigFieldDiffDetectsMixedChanges(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	requested := cloneManagementValues(t, current)
	nextCookieSecure := !*current.Auth.CookieSecure
	nextRuntimeExecutionMinIdle := *current.Database.Pools.RuntimeExecution.MinIdleConns + 1
	requested.Auth.CookieSecure = &nextCookieSecure
	requested.Database.Pools.RuntimeExecution.MinIdleConns = &nextRuntimeExecutionMinIdle
	diff, err := DiffBootstrapConfigFields(current, requested, preserveManagementSecretUpdates())
	if err != nil {
		t.Fatalf("diff mixed bootstrap fields: %v", err)
	}
	assertBootstrapFieldsEqual(t, diff.ChangedHotApplyFields, []string{bootstrapFieldAuthCookieSecure})
	assertBootstrapFieldsEqual(t, diff.ChangedRestartRequiredFields, []string{bootstrapFieldDatabasePoolsRuntimeExecutionMinIdle})
	assertBootstrapFieldChangesEqual(t, diff.ChangedFields(), []BootstrapConfigFieldChange{
		{Field: bootstrapFieldAuthCookieSecure, Mode: BootstrapConfigApplyModeHotApply},
		{Field: bootstrapFieldDatabasePoolsRuntimeExecutionMinIdle, Mode: BootstrapConfigApplyModeRestartRequired},
	})
}

func TestBootstrapConfigFieldDiffEmitsSecretUpdatePaths(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	updates := preserveManagementSecretUpdates()
	updates[BootstrapConfigSecretDatabaseURL] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace}
	updates[BootstrapConfigSecretRuntimeSecretEncryptionKey] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace}
	updates[BootstrapConfigSecretAuthJWTSigningKey] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace}
	updates[BootstrapConfigSecretTelemetryAuthorizationHeader] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace}
	diff, err := DiffBootstrapConfigFields(current, cloneManagementValues(t, current), updates)
	if err != nil {
		t.Fatalf("diff secret update bootstrap fields: %v", err)
	}
	assertBootstrapFieldsEqual(t, diff.ChangedHotApplyFields, nil)
	assertBootstrapFieldsEqual(t, diff.ChangedRestartRequiredFields, []string{
		BootstrapConfigSecretTelemetryAuthorizationHeader,
		BootstrapConfigSecretDatabaseURL,
		BootstrapConfigSecretRuntimeSecretEncryptionKey,
		BootstrapConfigSecretAuthJWTSigningKey,
	})
}

func TestBootstrapConfigFieldDiffReportsUnchangedFields(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	requested := cloneManagementValues(t, current)
	fiveMinutes := "5m"
	requested.Runtime.Transport.RequestTimeout = &fiveMinutes
	if current.HTTP.CORSAllowedOrigins != nil {
		origins := make([]string, len(*current.HTTP.CORSAllowedOrigins))
		for index, origin := range *current.HTTP.CORSAllowedOrigins {
			origins[len(origins)-1-index] = " " + origin + " "
		}
		requested.HTTP.CORSAllowedOrigins = &origins
	}
	diff, err := DiffBootstrapConfigFields(current, requested, preserveManagementSecretUpdates())
	if err != nil {
		t.Fatalf("diff unchanged bootstrap fields: %v", err)
	}
	assertBootstrapFieldsEqual(t, diff.ChangedHotApplyFields, nil)
	assertBootstrapFieldsEqual(t, diff.ChangedRestartRequiredFields, nil)
	assertBootstrapFieldsEqual(t, diff.UnchangedFields, BootstrapConfigApplyCapabilityFields())
	if diff.HasChanges() {
		t.Fatal("expected unchanged diff to report no changes")
	}
}

func TestBootstrapConfigFieldDiffRejectsUnclassifiedPaths(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	_, err := DiffBootstrapConfigFields(current, cloneManagementValues(t, current), map[string]BootstrapConfigSecretUpdate{
		"unknown.secret": {Action: BootstrapConfigSecretActionReplace},
	})
	if err == nil {
		t.Fatal("expected unknown field diff to fail")
	}
	var classificationError *BootstrapConfigFieldClassificationError
	if !errors.As(err, &classificationError) {
		t.Fatalf("expected classification error, got %T", err)
	}
	assertBootstrapFieldsEqual(t, classificationError.Fields, []string{"unknown.secret"})
}

func bootstrapApplyTestValues(t *testing.T) BootstrapConfigValues {
	t.Helper()
	path := t.TempDir() + "/config.json"
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{TimeNow: func() time.Time {
		return time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	}})
	if _, err := manager.LoadOrSeed(path); err != nil {
		t.Fatalf("seed bootstrap apply test values: %v", err)
	}
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load bootstrap apply test values: %v", err)
	}
	return snapshot.Values
}

func cloneManagementValues(t *testing.T, values BootstrapConfigValues) BootstrapConfigValues {
	t.Helper()
	payload, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal bootstrap config values: %v", err)
	}
	var clone BootstrapConfigValues
	if err := json.Unmarshal(payload, &clone); err != nil {
		t.Fatalf("unmarshal bootstrap config values: %v", err)
	}
	return clone
}

func preserveManagementSecretUpdates() map[string]BootstrapConfigSecretUpdate {
	return map[string]BootstrapConfigSecretUpdate{
		BootstrapConfigSecretDatabaseURL:                  {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretRuntimeSecretEncryptionKey:   {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretAuthJWTSigningKey:            {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretTelemetryAuthorizationHeader: {Action: BootstrapConfigSecretActionPreserve},
	}
}

func assertBootstrapFieldModes(t *testing.T, capabilities map[string]BootstrapConfigFieldCapability, fields []string, mode BootstrapConfigApplyMode) {
	t.Helper()
	for _, field := range fields {
		capability, ok := capabilities[field]
		if !ok {
			t.Fatalf("expected registry to include field %s", field)
		}
		if capability.Mode != mode {
			t.Fatalf("expected field %s mode %s, got %s", field, mode, capability.Mode)
		}
		classified, ok := ClassifyBootstrapConfigField(field)
		if !ok || classified != capability {
			t.Fatalf("expected field %s classification to round-trip", field)
		}
	}
}

func assertBootstrapCapabilityOrder(t *testing.T, got []string, want []string) {
	t.Helper()
	assertBootstrapFieldsEqual(t, got, want)
	seen := make(map[string]struct{}, len(got))
	for _, field := range got {
		if _, exists := seen[field]; exists {
			t.Fatalf("field %s appeared more than once in registry order", field)
		}
		seen[field] = struct{}{}
	}
}

func assertBootstrapConfirmationToken(t *testing.T, capabilities map[string]BootstrapConfigFieldCapability, field string, want string) {
	t.Helper()
	capability, ok := capabilities[field]
	if !ok {
		t.Fatalf("expected registry to include field %s", field)
	}
	if capability.ConfirmationToken != want {
		t.Fatalf("expected field %s confirmation token %q, got %q", field, want, capability.ConfirmationToken)
	}
}

func assertBootstrapFieldsEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected fields\n got: %v\nwant: %v", got, want)
	}
}

func assertBootstrapFieldChangesEqual(t *testing.T, got []BootstrapConfigFieldChange, want []BootstrapConfigFieldChange) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected field changes\n got: %+v\nwant: %+v", got, want)
	}
}

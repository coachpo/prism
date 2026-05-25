package config

import (
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
		bootstrapFieldAuthResetCodeTTLSeconds,
		bootstrapFieldAuthAccessCookieName,
		bootstrapFieldAuthRefreshCookieName,
		bootstrapFieldAuthCookieSecure,
		bootstrapFieldMailEnabled,
		bootstrapFieldMailFrom,
		bootstrapFieldMailReplyTo,
		bootstrapFieldMailSMTPHost,
		bootstrapFieldMailSMTPPort,
		bootstrapFieldMailSMTPMode,
		bootstrapFieldMailSMTPEHLOHostname,
		bootstrapFieldMailSMTPAuth,
		bootstrapFieldMailSMTPUsername,
		bootstrapFieldMailSMTPPasswordFile,
		bootstrapFieldMailSMTPTimeout,
		bootstrapFieldMailSMTPTLSServerName,
		BootstrapConfigSecretMailSMTPPassword,
		bootstrapFieldRuntimeBufferingMode,
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
		BootstrapConfigSecretStateTransferBundleKey,
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
	assertBootstrapConfirmationToken(t, capabilities, BootstrapConfigSecretStateTransferBundleKey, BootstrapConfigConfirmationStateTransferBundleKeyChange)
	if _, ok := ClassifyBootstrapConfigField("database.pools.management.*"); ok {
		t.Fatal("expected wildcard pool paths to stay out of the exact field registry")
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
	updates[BootstrapConfigSecretStateTransferBundleKey] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace}
	updates[BootstrapConfigSecretMailSMTPPassword] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace}
	diff, err := DiffBootstrapConfigFields(current, cloneManagementValues(t, current), updates)
	if err != nil {
		t.Fatalf("diff secret update bootstrap fields: %v", err)
	}
	assertBootstrapFieldsEqual(t, diff.ChangedHotApplyFields, []string{BootstrapConfigSecretMailSMTPPassword})
	assertBootstrapFieldsEqual(t, diff.ChangedRestartRequiredFields, []string{
		BootstrapConfigSecretDatabaseURL,
		BootstrapConfigSecretRuntimeSecretEncryptionKey,
		BootstrapConfigSecretAuthJWTSigningKey,
		BootstrapConfigSecretStateTransferBundleKey,
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

func TestBootstrapConfigApplyResultFromDiffInitializesEffectFields(t *testing.T) {
	current := bootstrapApplyTestValues(t)
	requested := cloneManagementValues(t, current)
	nextAccessTokenTTL := *current.Auth.AccessTokenTTLSeconds + 60
	nextPort := *current.Server.Port + 1
	requested.Auth.AccessTokenTTLSeconds = &nextAccessTokenTTL
	requested.Server.Port = &nextPort
	diff, err := DiffBootstrapConfigFields(current, requested, preserveManagementSecretUpdates())
	if err != nil {
		t.Fatalf("diff bootstrap fields for apply result: %v", err)
	}

	result := BootstrapConfigApplyResultFromDiff(diff)

	assertBootstrapFieldsEqual(t, result.AppliedNowFields, []string{})
	assertBootstrapFieldsEqual(t, result.RestartRequiredFields, []string{bootstrapFieldServerPort})
	assertBootstrapFieldsEqual(t, result.PendingHotApplyFields, []string{bootstrapFieldAuthAccessTokenTTLSeconds})
	assertBootstrapFieldsEqual(t, result.FailedHotApplyFields, []string{})
	if !containsString(result.UnchangedFields, bootstrapFieldAuthRefreshTokenTTLSeconds) {
		t.Fatal("expected apply result unchanged fields to include untouched capabilities")
	}
}

func bootstrapApplyTestValues(t *testing.T) BootstrapConfigValues {
	t.Helper()
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	snapshot, _, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load bootstrap apply test values: %v", err)
	}
	return snapshot.Values
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

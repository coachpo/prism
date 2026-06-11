package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	managementTestDatabaseURL                  = "postgres://prism:top-secret@db.internal:5432/prism?sslmode=disable&password=query-secret&sslpassword=ssl-password&passphrase=passphrase-secret&passwd=passwd-secret"
	managementTestRuntimeSecret                = "runtime-secret-for-management-test"
	managementTestJWTSecret                    = "jwt-secret-for-management-test"
	managementTestBundleSecret                 = "bundle-secret-for-management-test"
	managementTestSMTPPassword                 = "smtp-password-for-management-test"
	managementTestTelemetryAuthorizationHeader = "Bearer telemetry-secret-for-management-test"
)

func TestBootstrapConfigManagementLoadReturnsSafeMetadata(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})

	snapshot, settings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load bootstrap config management snapshot: %v", err)
	}

	if settings.DatabaseURL != managementTestDatabaseURL {
		t.Fatal("expected settings database URL to round-trip")
	}
	if settings.AuthJWTSecret != managementTestJWTSecret {
		t.Fatal("expected settings JWT secret to round-trip")
	}
	if snapshot.ConfigPath != path || snapshot.SchemaVersion != bootstrapConfigSchemaVersion || snapshot.FileRevision != 1 {
		t.Fatal("unexpected safe bootstrap snapshot metadata")
	}
	if snapshot.CreatedAt != createdAt.Format(time.RFC3339) || snapshot.UpdatedAt != createdAt.Format(time.RFC3339) {
		t.Fatal("unexpected safe bootstrap snapshot timestamps")
	}
	if !strings.HasPrefix(snapshot.DocumentETag, "sha256:") {
		t.Fatal("expected canonical sha256 etag")
	}
	if snapshot.Values.Database == nil || snapshot.Values.Database.Pools == nil || snapshot.Values.Database.Pools.RuntimeExecution == nil || snapshot.Values.Auth == nil {
		t.Fatal("expected safe values to include editable sections")
	}
	if snapshot.Values.Runtime == nil || snapshot.Values.Runtime.Transport == nil || snapshot.Values.Runtime.Transport.RequestTimeout == nil || *snapshot.Values.Runtime.Transport.RequestTimeout != "300s" {
		t.Fatalf("expected safe runtime transport request_timeout to be exposed, got %+v", snapshot.Values.Runtime)
	}
	if snapshot.Values.Runtime.SideEffects == nil || snapshot.Values.Runtime.SideEffects.AttemptTimeout == nil || *snapshot.Values.Runtime.SideEffects.AttemptTimeout != "10s" {
		t.Fatalf("expected safe runtime side_effects attempt_timeout to be exposed, got %+v", snapshot.Values.Runtime)
	}

	encoded := mustMarshalJSON(t, snapshot)
	assertSafeManagementSnapshot(t, encoded)
	if !bytes.Contains(encoded, []byte(`"request_timeout":"300s"`)) {
		t.Fatal("expected safe management snapshot to include request_timeout")
	}
	if !bytes.Contains(encoded, []byte(`"attempt_timeout":"10s"`)) {
		t.Fatal("expected safe management snapshot to include attempt_timeout")
	}
	if bytes.Contains(encoded, []byte("buffering_mode")) || bytes.Contains(encoded, []byte("bufferingMode")) {
		t.Fatalf("expected safe management snapshot to omit runtime buffering mode, got %s", encoded)
	}
	databaseSecret := snapshot.Secrets[BootstrapConfigSecretDatabaseURL]
	if !databaseSecret.Configured || !databaseSecret.Editable {
		t.Fatal("expected editable configured database secret metadata")
	}

	if !strings.Contains(databaseSecret.Masked, "***") || strings.Contains(databaseSecret.Masked, "top-secret") || strings.Contains(databaseSecret.Masked, "query-secret") {
		t.Fatal("expected database URL metadata to mask credentials")
	}
	for _, leaked := range []string{"ssl-password", "passphrase-secret", "passwd-secret"} {
		if strings.Contains(databaseSecret.Masked, leaked) {
			t.Fatalf("expected database URL metadata to mask sensitive query value %q", leaked)
		}
	}
	runtimeSecret, ok := snapshot.Secrets["runtime.secretEncryptionKey"]
	if !ok {
		t.Fatal("expected safe metadata to expose runtime.secretEncryptionKey")
	}
	if !runtimeSecret.Configured || runtimeSecret.Editable || runtimeSecret.Masked != "set" {
		t.Fatal("expected runtime secret metadata to be configured and read-only")
	}
	if _, ok := snapshot.Secrets["secretEncryptionKey"]; ok {
		t.Fatal("expected safe metadata to omit bare secretEncryptionKey")
	}
	smtpSecret := snapshot.Secrets[BootstrapConfigSecretMailSMTPPassword]
	if !smtpSecret.Configured || !smtpSecret.Editable || smtpSecret.Masked != "set" {
		t.Fatal("expected SMTP password metadata to be editable and masked")
	}
}

func TestTelemetryAuthorizationHeaderIsMasked(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	document := newManagementTestDocument(t, createdAt)
	document.Telemetry = managementTestTelemetryDocument()
	path, _ := writeManagementTestDocument(t, document)
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})

	snapshot, settings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load telemetry bootstrap config management snapshot: %v", err)
	}
	if settings.Telemetry.Exporter.Auth.AuthorizationHeader != managementTestTelemetryAuthorizationHeader {
		t.Fatal("expected raw settings to preserve telemetry authorization header")
	}
	if snapshot.Values.Telemetry == nil || snapshot.Values.Telemetry.Exporter == nil || snapshot.Values.Telemetry.Exporter.Auth == nil || snapshot.Values.Telemetry.Exporter.Auth.Mode == nil || *snapshot.Values.Telemetry.Exporter.Auth.Mode != string(TelemetryExporterAuthModeAuthorizationHeader) {
		t.Fatalf("expected safe telemetry auth mode without raw header, got %+v", snapshot.Values.Telemetry)
	}
	telemetrySecret := snapshot.Secrets[BootstrapConfigSecretTelemetryAuthorizationHeader]
	if !telemetrySecret.Configured || !telemetrySecret.Editable || telemetrySecret.Masked != "set" {
		t.Fatalf("expected telemetry authorization header metadata to be editable and masked, got %+v", telemetrySecret)
	}
	encoded := mustMarshalJSON(t, snapshot)
	assertNoSecretValue(t, encoded, "telemetry authorization header", managementTestTelemetryAuthorizationHeader)
	if !bytes.Contains(encoded, []byte(`"telemetry"`)) || !bytes.Contains(encoded, []byte(`"mode":"authorization_header"`)) {
		t.Fatalf("expected safe snapshot to expose telemetry values and auth metadata, got %s", encoded)
	}
}

func TestTelemetrySecretPreserveReplaceClear(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	document := newManagementTestDocument(t, createdAt)
	document.Telemetry = managementTestTelemetryDocument()
	path, _ := writeManagementTestDocument(t, document)
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return createdAt.Add(2 * time.Hour) },
	})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before telemetry secret update: %v", err)
	}

	preserved, err := manager.PrepareBootstrapConfigUpdate(path, managementRequestForSnapshot(t, snapshot))
	if err != nil {
		t.Fatalf("prepare telemetry secret preserve update: %v", err)
	}
	preservedSettings, err := manager.Parse(preserved.Payload)
	if err != nil {
		t.Fatalf("parse preserved telemetry secret payload: %v", err)
	}
	if preservedSettings.Telemetry.Exporter.Auth.AuthorizationHeader != managementTestTelemetryAuthorizationHeader {
		t.Fatal("expected preserve action to keep telemetry authorization header")
	}

	replacementHeader := "Bearer replacement-telemetry-secret"
	replaceRequest := managementRequestForSnapshot(t, snapshot)
	replaceRequest.SecretUpdates[BootstrapConfigSecretTelemetryAuthorizationHeader] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer(replacementHeader)}
	replaced, err := manager.PrepareBootstrapConfigUpdate(path, replaceRequest)
	if err != nil {
		t.Fatalf("prepare telemetry secret replace update: %v", err)
	}
	replacedSettings, err := manager.Parse(replaced.Payload)
	if err != nil {
		t.Fatalf("parse telemetry secret replace payload: %v", err)
	}
	if replacedSettings.Telemetry.Exporter.Auth.AuthorizationHeader != replacementHeader {
		t.Fatal("expected replace action to update telemetry authorization header")
	}
	assertNoSecretValue(t, mustMarshalJSON(t, replaced.Snapshot), "replacement telemetry authorization header", replacementHeader)

	clearRequest := managementRequestForSnapshot(t, snapshot)
	clearValues := cloneManagementValues(t, snapshot.Values)
	clearValues.Telemetry.Exporter.Auth.Mode = stringPointer(string(TelemetryExporterAuthModeNone))
	clearRequest.Values = &clearValues
	clearRequest.SecretUpdates[BootstrapConfigSecretTelemetryAuthorizationHeader] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionClear}
	cleared, err := manager.PrepareBootstrapConfigUpdate(path, clearRequest)
	if err != nil {
		t.Fatalf("prepare telemetry secret clear update: %v", err)
	}
	clearedSettings, err := manager.Parse(cleared.Payload)
	if err != nil {
		t.Fatalf("parse telemetry secret clear payload: %v", err)
	}
	if clearedSettings.Telemetry.Exporter.Auth.Mode != TelemetryExporterAuthModeNone || clearedSettings.Telemetry.Exporter.Auth.AuthorizationHeader != "" {
		t.Fatalf("expected clear action to remove telemetry authorization header, got %+v", clearedSettings.Telemetry.Exporter.Auth)
	}
	if cleared.Snapshot.Secrets[BootstrapConfigSecretTelemetryAuthorizationHeader].Configured {
		t.Fatalf("expected safe snapshot to mark telemetry authorization header unconfigured after clear, got %+v", cleared.Snapshot.Secrets[BootstrapConfigSecretTelemetryAuthorizationHeader])
	}
	assertNoSecretValue(t, cleared.Payload, "cleared telemetry authorization header", managementTestTelemetryAuthorizationHeader)
}

func TestBootstrapConfigManagementNoopPreservesOmittedTelemetry(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	document := newManagementTestDocument(t, createdAt)
	document.Telemetry = nil
	path, originalPayload := writeManagementTestDocument(t, document)
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})

	snapshot, settings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load legacy bootstrap config management snapshot: %v", err)
	}
	if settings.Telemetry.Enabled {
		t.Fatalf("expected omitted telemetry to resolve to disabled settings, got %+v", settings.Telemetry)
	}
	if snapshot.Values.Telemetry == nil || snapshot.Values.Telemetry.Enabled == nil || *snapshot.Values.Telemetry.Enabled {
		t.Fatalf("expected safe values to expose disabled telemetry, got %+v", snapshot.Values.Telemetry)
	}

	prepared, err := manager.PrepareBootstrapConfigUpdate(path, managementRequestForSnapshot(t, snapshot))
	if err != nil {
		t.Fatalf("prepare omitted telemetry no-op update: %v", err)
	}
	if !prepared.Noop {
		t.Fatal("expected unchanged safe update to preserve omitted telemetry as a no-op")
	}
	if !bytes.Equal(prepared.Payload, originalPayload) {
		t.Fatal("expected omitted telemetry no-op payload to match original legacy payload")
	}
	if bytes.Contains(prepared.Payload, []byte(`"telemetry"`)) {
		t.Fatalf("expected omitted telemetry to remain absent from prepared payload, got %s", prepared.Payload)
	}
}

func TestBootstrapConfigManagementLoadCanonicalizesOmittedMailToDisabledSafeValues(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	document := newManagementTestDocument(t, createdAt)
	document.Mail = nil
	path, originalPayload := writeManagementTestDocument(t, document)
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})

	snapshot, settings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load legacy bootstrap config management snapshot: %v", err)
	}
	if settings.Mail.Enabled {
		t.Fatalf("expected omitted mail to resolve to disabled settings, got %+v", settings.Mail)
	}
	assertDisabledSafeMailValues(t, snapshot.Values.Mail)
	assertSafeManagementSnapshot(t, mustMarshalJSON(t, snapshot))
	smtpSecret := snapshot.Secrets[BootstrapConfigSecretMailSMTPPassword]
	if smtpSecret.Configured || !smtpSecret.Editable || smtpSecret.Masked != "" {
		t.Fatalf("expected omitted SMTP password metadata to stay unconfigured and editable, got %+v", smtpSecret)
	}

	rawAfterLoad, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read legacy bootstrap config after snapshot load: %v", err)
	}
	if !bytes.Equal(rawAfterLoad, originalPayload) {
		t.Fatal("expected read-only snapshot load to leave omitted-mail source file unchanged")
	}
}

func TestSafeBootstrapProjectionReturnsCompletePoolValues(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})

	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load bootstrap config for complete safe projection: %v", err)
	}
	values := snapshot.Values
	if values.Server == nil || values.Server.Port == nil || *values.Server.Port != 8000 {
		t.Fatalf("expected safe projection server port=8000, got %+v", values.Server)
	}
	if values.HTTP == nil || values.HTTP.CORSAllowedOrigins == nil || !slices.Equal(*values.HTTP.CORSAllowedOrigins, []string{"http://localhost:5173", "http://127.0.0.1:5173"}) {
		t.Fatalf("expected safe projection CORS origins on frontend port 5173, got %+v", values.HTTP)
	}
	if values.Runtime == nil {
		t.Fatal("expected safe projection to include runtime values")
	}
	encodedRuntime := mustMarshalJSON(t, values.Runtime)
	if bytes.Contains(encodedRuntime, []byte("buffering_mode")) || bytes.Contains(encodedRuntime, []byte("bufferingMode")) {
		t.Fatalf("expected safe projection to omit runtime buffering mode, got %s", encodedRuntime)
	}
	if values.Database == nil || values.Database.Pools == nil {
		t.Fatalf("expected safe projection to include database pools, got %+v", values.Database)
	}
	pools := values.Database.Pools
	if pools.TotalMaxConns == nil || *pools.TotalMaxConns != 24 {
		t.Fatalf("expected safe projection total_max_conns=24, got %+v", pools.TotalMaxConns)
	}
	assertPool := func(name string, pool *BootstrapConfigDatabasePoolValues, maxConns int, minIdleConns int) {
		t.Helper()
		if pool == nil || pool.MaxConns == nil || pool.MinIdleConns == nil || *pool.MaxConns != maxConns || *pool.MinIdleConns != minIdleConns {
			t.Fatalf("expected safe projection pool %s to be %d/%d, got %+v", name, maxConns, minIdleConns, pool)
		}
	}
	assertPool("management", pools.Management, 4, 1)
	assertPool("runtime_execution", pools.RuntimeExecution, 8, 2)
	assertPool("runtime_telemetry", pools.RuntimeTelemetry, 4, 1)
	assertPool("runtime_feedback", pools.RuntimeFeedback, 2, 0)
	assertPool("realtime", pools.Realtime, 2, 0)
	assertPool("cache_refresh", pools.CacheRefresh, 2, 0)
	assertPool("background_jobs", pools.BackgroundJobs, 2, 0)

	admission := values.Database.ManagementAdmission
	if admission == nil || admission.M2MaxConcurrent == nil || admission.M3MaxConcurrent == nil || *admission.M2MaxConcurrent != 3 || *admission.M3MaxConcurrent != 2 {
		t.Fatalf("expected safe projection admission 3/2, got %+v", admission)
	}
	if values.Runtime == nil || values.Runtime.Transport == nil || values.Runtime.SideEffects == nil {
		t.Fatalf("expected safe projection to include runtime defaults, got %+v", values.Runtime)
	}
	transport := values.Runtime.Transport
	if transport.MaxIdleConns == nil || transport.MaxIdleConnsPerHost == nil || transport.MaxConnsPerHost == nil || *transport.MaxIdleConns != 100 || *transport.MaxIdleConnsPerHost != 16 || *transport.MaxConnsPerHost != 16 {
		t.Fatalf("expected safe projection runtime transport caps 100/16/16, got %+v", transport)
	}
	assertDuration := func(name string, got *string, want string) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("expected safe projection %s=%q, got %+v", name, want, got)
		}
	}
	assertDuration("transport.request_timeout", transport.RequestTimeout, "300s")
	assertDuration("transport.idle_conn_timeout", transport.IdleConnTimeout, "90s")
	assertDuration("transport.response_header_timeout", transport.ResponseHeaderTimeout, "0s")
	assertDuration("transport.tls_handshake_timeout", transport.TLSHandshakeTimeout, "10s")
	assertDuration("transport.expect_continue_timeout", transport.ExpectContinueTimeout, "1s")
	assertDuration("side_effects.attempt_timeout", values.Runtime.SideEffects.AttemptTimeout, "10s")
}

func TestBootstrapConfigManagementUpdatePersistsCanonicalDisabledMail(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 4, 28, 13, 45, 0, 0, time.UTC)
	tests := []struct {
		name            string
		omitCurrentMail bool
		mutate          func(t *testing.T, values *BootstrapConfigValues)
		forbiddenSecret string
	}{
		{
			name:            "omitted mail safe value from legacy config",
			omitCurrentMail: true,
			mutate: func(t *testing.T, values *BootstrapConfigValues) {
				t.Helper()
				values.Mail = nil
			},
		},
		{
			name:            "null mail safe value from legacy config",
			omitCurrentMail: true,
			mutate: func(t *testing.T, values *BootstrapConfigValues) {
				t.Helper()
				*values = managementValuesWithNullMail(t, *values)
			},
		},
		{
			name: "explicit disabled mail drops smtp payload",
			mutate: func(t *testing.T, values *BootstrapConfigValues) {
				t.Helper()
				values.Mail = &BootstrapConfigMailValues{
					Enabled: boolPointer(false),
					From:    stringPointer("Prism <noreply@example.com>"),
					ReplyTo: stringPointer("support@example.com"),
					SMTP: &BootstrapConfigMailSMTPValues{
						Host:         stringPointer("smtp.example.com"),
						Port:         intPointer(587),
						Mode:         stringPointer(string(MailSMTPModeStartTLSRequired)),
						Auth:         stringPointer(string(MailSMTPAuthPlain)),
						Username:     stringPointer("smtp-user"),
						PasswordFile: stringPointer("/run/secrets/prism-smtp-password"),
					},
				}
			},
			forbiddenSecret: managementTestSMTPPassword,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			document := newManagementTestDocument(t, createdAt)
			if testCase.omitCurrentMail {
				document.Mail = nil
			}
			path, _ := writeManagementTestDocument(t, document)
			manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{
				TimeNow: func() time.Time { return updatedAt },
			})
			snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
			if err != nil {
				t.Fatalf("load snapshot before disabled mail update: %v", err)
			}
			request := managementRequestForSnapshot(t, snapshot)
			values := cloneManagementValues(t, snapshot.Values)
			testCase.mutate(t, &values)
			request.Values = &values

			prepared, err := manager.PrepareBootstrapConfigUpdate(path, request)
			if err != nil {
				t.Fatalf("prepare disabled mail update: %v", err)
			}
			if prepared.Noop {
				t.Fatal("expected disabled mail canonicalization update to be effective")
			}
			if prepared.Snapshot.FileRevision != snapshot.FileRevision+1 || prepared.Snapshot.UpdatedAt != updatedAt.Format(time.RFC3339) {
				t.Fatal("expected disabled mail update to increment revision and refresh updatedAt")
			}
			assertDisabledSafeMailValues(t, prepared.Snapshot.Values.Mail)
			assertSafeManagementSnapshot(t, mustMarshalJSON(t, prepared.Snapshot))
			assertCanonicalDisabledMailPayload(t, prepared.Payload)
			assertNoSecretValue(t, prepared.Payload, "disabled SMTP password", testCase.forbiddenSecret)

			settings, err := manager.Parse(prepared.Payload)
			if err != nil {
				t.Fatalf("parse disabled mail update payload: %v", err)
			}
			if settings.Mail.Enabled {
				t.Fatalf("expected prepared disabled mail settings, got %+v", settings.Mail)
			}
			if _, err := manager.WriteBootstrapConfigUpdate(path, prepared); err != nil {
				t.Fatalf("write disabled mail update: %v", err)
			}
			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read written disabled mail update: %v", err)
			}
			assertCanonicalDisabledMailPayload(t, written)
			assertNoSecretValue(t, written, "written disabled SMTP password", testCase.forbiddenSecret)
		})
	}
}

func TestBootstrapConfigManagementDisabledMailIgnoresSMTPPasswordReplacement(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 4, 28, 16, 20, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return updatedAt },
	})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before disabled mail SMTP replacement: %v", err)
	}
	request := managementRequestForSnapshot(t, snapshot)
	values := cloneManagementValues(t, snapshot.Values)
	values.Mail = &BootstrapConfigMailValues{Enabled: boolPointer(false)}
	request.Values = &values
	stagedSMTPPassword := "disabled-mail-staged-smtp-password"
	request.SecretUpdates[BootstrapConfigSecretMailSMTPPassword] = BootstrapConfigSecretUpdate{
		Action: BootstrapConfigSecretActionReplace,
		Value:  stringPointer(stagedSMTPPassword),
	}

	prepared, err := manager.PrepareBootstrapConfigUpdate(path, request)
	if err != nil {
		t.Fatalf("prepare disabled mail with staged SMTP password replacement: %v", err)
	}
	if prepared.Noop {
		t.Fatal("expected disabled mail update to be effective")
	}
	assertDisabledSafeMailValues(t, prepared.Snapshot.Values.Mail)
	assertCanonicalDisabledMailPayload(t, prepared.Payload)
	assertNoSecretValue(t, prepared.Payload, "old SMTP password", managementTestSMTPPassword)
	assertNoSecretValue(t, prepared.Payload, "staged SMTP password", stagedSMTPPassword)
	if prepared.Snapshot.Secrets[BootstrapConfigSecretMailSMTPPassword].Configured {
		t.Fatalf("expected disabled mail snapshot to leave SMTP password unconfigured, got %+v", prepared.Snapshot.Secrets[BootstrapConfigSecretMailSMTPPassword])
	}
	settings, err := manager.Parse(prepared.Payload)
	if err != nil {
		t.Fatalf("parse disabled mail with staged SMTP password replacement payload: %v", err)
	}
	if settings.Mail.Enabled || settings.Mail.SMTP.Password != "" || settings.Mail.SMTP.PasswordFile != "" {
		t.Fatalf("expected canonical disabled mail without SMTP secrets, got %+v", settings.Mail)
	}
}

func TestBootstrapConfigManagementResponseAddsLoadedMetadata(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	snapshot, settings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot for safe response: %v", err)
	}

	currentResponse := BuildBootstrapConfigResponse(snapshot, settings, settings, snapshot.FileRevision, snapshot.DocumentETag, true, BootstrapConfigResponseOptions{})
	if currentResponse.RestartRequired || !currentResponse.Writable {
		t.Fatal("expected current loaded metadata to produce writable non-restart response")
	}
	if len(currentResponse.ApplyCapabilities) != len(BootstrapConfigApplyCapabilities()) {
		t.Fatal("expected response to include apply capabilities")
	}
	staleResponse := BuildBootstrapConfigResponse(snapshot, settings, settings, snapshot.FileRevision-1, "sha256:loaded", false, BootstrapConfigResponseOptions{})
	if staleResponse.RestartRequired || staleResponse.Writable {
		t.Fatal("expected revision and etag drift alone to produce read-only non-restart response")
	}
	restartedSettings := settings
	restartedSettings.Port++
	restartResponse := BuildBootstrapConfigResponse(snapshot, restartedSettings, settings, snapshot.FileRevision, snapshot.DocumentETag, true, BootstrapConfigResponseOptions{})
	if !restartResponse.RestartRequired {
		t.Fatal("expected restart-required field drift to require restart")
	}
	assertSafeManagementSnapshot(t, mustMarshalJSON(t, staleResponse))
}

func TestBootstrapConfigManagementCanonicalETagIsStable(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	document := newManagementTestDocument(t, createdAt)
	canonical := mustCanonicalManagementPayload(t, document)
	compact, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal compact management test payload: %v", err)
	}

	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	canonicalSnapshot, _, err := manager.LoadBootstrapConfigDocument(writeRawManagementPayload(t, canonical))
	if err != nil {
		t.Fatalf("load canonical management test payload: %v", err)
	}

	compactSnapshot, _, err := manager.LoadBootstrapConfigDocument(writeRawManagementPayload(t, compact))
	if err != nil {
		t.Fatalf("load compact management test payload: %v", err)
	}
	if canonicalSnapshot.DocumentETag != compactSnapshot.DocumentETag {
		t.Fatal("expected canonical etag to ignore source formatting")
	}
	if canonicalSnapshot.DocumentETag != bootstrapConfigETag(canonical) {
		t.Fatal("expected loaded etag to match package canonical payload")
	}
}

func TestBootstrapConfigManagementPrepareUpdateIncrementsMetaAndWritesAtomically(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 4, 28, 12, 30, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return updatedAt },
	})

	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before management update: %v", err)
	}
	values := cloneManagementValues(t, snapshot.Values)
	values.Auth.AccessTokenTTLSeconds = intPointer(1200)
	values.Runtime.Transport.RequestTimeout = stringPointer("17s")
	values.Runtime.SideEffects.AttemptTimeout = stringPointer("11s")
	rotatedJWTSecret := "rotated-jwt-signing-key"
	updates := preserveManagementSecretUpdates()
	updates[BootstrapConfigSecretAuthJWTSigningKey] = BootstrapConfigSecretUpdate{
		Action: BootstrapConfigSecretActionReplace,
		Value:  stringPointer(rotatedJWTSecret),
	}

	prepared, err := manager.PrepareBootstrapConfigUpdate(path, BootstrapConfigUpdateRequest{
		ExpectedRevision: snapshot.FileRevision,
		ExpectedETag:     snapshot.DocumentETag,
		Values:           &values,
		SecretUpdates:    updates,
		Confirmations:    []string{BootstrapConfigConfirmationAuthJWTSigningKeyChange},
	})
	if err != nil {
		t.Fatalf("prepare management update: %v", err)
	}
	if prepared.Noop {
		t.Fatal("expected secret replacement update to be effective")
	}

	if prepared.Snapshot.FileRevision != snapshot.FileRevision+1 {
		t.Fatal("expected management update to increment revision")
	}
	if prepared.Snapshot.CreatedAt != snapshot.CreatedAt || prepared.Snapshot.UpdatedAt != updatedAt.Format(time.RFC3339) {
		t.Fatal("expected management update to preserve createdAt and refresh updatedAt")
	}
	assertSafeManagementSnapshot(t, mustMarshalJSON(t, prepared.Snapshot))
	assertNoSecretValue(t, mustMarshalJSON(t, prepared.Snapshot), "rotated JWT secret", rotatedJWTSecret)

	settings, err := manager.Parse(prepared.Payload)
	if err != nil {
		t.Fatalf("parse prepared management payload: %v", err)
	}
	if settings.AuthAccessTokenTTLSeconds != 1200 || settings.AuthJWTSecret != rotatedJWTSecret || settings.RuntimeTransport().RequestTimeout != 17*time.Second || settings.RuntimeSideEffects().AttemptTimeout != 11*time.Second {
		t.Fatal("expected prepared payload to include auth, runtime transport, and runtime side effects changes")
	}
	if prepared.Snapshot.Values.Runtime == nil || prepared.Snapshot.Values.Runtime.Transport == nil || prepared.Snapshot.Values.Runtime.Transport.RequestTimeout == nil || *prepared.Snapshot.Values.Runtime.Transport.RequestTimeout != "17s" {
		t.Fatal("expected prepared safe snapshot to preserve request_timeout")
	}
	if prepared.Snapshot.Values.Runtime.SideEffects == nil || prepared.Snapshot.Values.Runtime.SideEffects.AttemptTimeout == nil || *prepared.Snapshot.Values.Runtime.SideEffects.AttemptTimeout != "11s" {
		t.Fatal("expected prepared safe snapshot to preserve attempt_timeout")
	}
	if _, err := manager.WriteBootstrapConfigUpdate(path, prepared); err != nil {
		t.Fatalf("write prepared management update: %v", err)
	}
	loadedSettings, err := manager.Load(path)
	if err != nil {
		t.Fatalf("load written management update: %v", err)
	}

	if loadedSettings.AuthJWTSecret != rotatedJWTSecret || loadedSettings.AuthAccessTokenTTLSeconds != 1200 || loadedSettings.RuntimeTransport().RequestTimeout != 17*time.Second || loadedSettings.RuntimeSideEffects().AttemptTimeout != 11*time.Second {
		t.Fatal("expected atomic write helper to persist prepared payload")
	}
}

func TestBootstrapConfigManagementSecretPreserveAndReplace(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return createdAt.Add(2 * time.Hour) },
	})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before secret update: %v", err)
	}

	values := cloneManagementValues(t, snapshot.Values)
	nextDatabaseURL := "postgres://prism:next-password@db.next.internal:5432/prism?sslmode=disable"
	nextBundleSecret := "replacement-bundle-encryption-key"
	nextSMTPPassword := "replacement-smtp-password"
	updates := preserveManagementSecretUpdates()
	updates[BootstrapConfigSecretDatabaseURL] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer(nextDatabaseURL)}

	updates[BootstrapConfigSecretStateTransferBundleKey] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer(nextBundleSecret)}
	updates[BootstrapConfigSecretMailSMTPPassword] = BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer(nextSMTPPassword)}
	prepared, err := manager.PrepareBootstrapConfigUpdate(path, BootstrapConfigUpdateRequest{
		ExpectedRevision: snapshot.FileRevision,
		ExpectedETag:     snapshot.DocumentETag,
		Values:           &values,
		SecretUpdates:    updates,
		Confirmations: []string{
			BootstrapConfigConfirmationDatabaseURLChange,
			BootstrapConfigConfirmationStateTransferBundleKeyChange,
		},
	})
	if err != nil {
		t.Fatalf("prepare secret preserve and replace update: %v", err)
	}
	settings, err := manager.Parse(prepared.Payload)
	if err != nil {
		t.Fatalf("parse secret preserve and replace payload: %v", err)
	}
	if settings.DatabaseURL != nextDatabaseURL || settings.ConfigBundleEncryptionKey != nextBundleSecret || settings.Mail.SMTP.Password != nextSMTPPassword {
		t.Fatal("expected replace actions to update selected secrets")
	}

	if settings.AuthJWTSecret != managementTestJWTSecret || settings.SecretEncryptionKey != managementTestRuntimeSecret {
		t.Fatal("expected preserve actions to keep existing secrets")
	}
	safePayload := mustMarshalJSON(t, prepared.Snapshot)
	assertNoSecretValue(t, safePayload, "replacement database password", "next-password")
	assertNoSecretValue(t, safePayload, "replacement bundle secret", nextBundleSecret)
	assertNoSecretValue(t, safePayload, "replacement SMTP password", nextSMTPPassword)
}

func TestBootstrapConfigManagementSMTPPasswordFileDropsPreservedInlinePassword(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 4, 28, 15, 15, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return updatedAt },
	})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before SMTP password file switch: %v", err)
	}

	request := managementRequestForSnapshot(t, snapshot)
	values := cloneManagementValues(t, snapshot.Values)
	values.Mail.SMTP.PasswordFile = stringPointer("/run/secrets/prism-smtp-password")
	request.Values = &values

	prepared, err := manager.PrepareBootstrapConfigUpdate(path, request)
	if err != nil {
		t.Fatalf("prepare SMTP password file switch: %v", err)
	}
	if prepared.Noop {
		t.Fatal("expected password file switch to be effective")
	}
	assertNoSecretValue(t, prepared.Payload, "old SMTP password", managementTestSMTPPassword)
	assertSafeManagementSnapshot(t, mustMarshalJSON(t, prepared.Snapshot))
	settings, err := manager.Parse(prepared.Payload)
	if err != nil {
		t.Fatalf("parse SMTP password file switch payload: %v", err)
	}
	if settings.Mail.SMTP.Password != "" {
		t.Fatal("expected password file switch to drop the preserved inline SMTP password")
	}
	if settings.Mail.SMTP.PasswordFile != "/run/secrets/prism-smtp-password" {
		t.Fatalf("expected SMTP password file to be configured, got %q", settings.Mail.SMTP.PasswordFile)
	}
	if prepared.Snapshot.Secrets[BootstrapConfigSecretMailSMTPPassword].Configured {
		t.Fatalf("expected safe snapshot to mark inline SMTP password unconfigured after password file switch, got %+v", prepared.Snapshot.Secrets[BootstrapConfigSecretMailSMTPPassword])
	}
}

func TestBootstrapConfigManagementSMTPPasswordPreserveWithoutPasswordFile(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before SMTP password preserve: %v", err)
	}

	prepared, err := manager.PrepareBootstrapConfigUpdate(path, managementRequestForSnapshot(t, snapshot))
	if err != nil {
		t.Fatalf("prepare SMTP password preserve update: %v", err)
	}
	settings, err := manager.Parse(prepared.Payload)
	if err != nil {
		t.Fatalf("parse SMTP password preserve payload: %v", err)
	}
	if settings.Mail.SMTP.Password != managementTestSMTPPassword || settings.Mail.SMTP.PasswordFile != "" {
		t.Fatalf("expected preserved inline SMTP password without password file, got %+v", settings.Mail.SMTP)
	}
	assertSafeManagementSnapshot(t, mustMarshalJSON(t, prepared.Snapshot))
}

func TestBootstrapConfigManagementRejectsSecretPlaceholdersAndRuntimeReplacement(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before invalid secret updates: %v", err)
	}

	runtimeReplacement := "replacement-runtime-secret"
	secretPlaceholder := "set"
	databasePlaceholder := snapshot.Secrets[BootstrapConfigSecretDatabaseURL].Masked
	tests := []struct {
		name      string
		field     string
		update    BootstrapConfigSecretUpdate
		forbidden string
	}{
		{
			name:   "auth placeholder",
			field:  BootstrapConfigSecretAuthJWTSigningKey,
			update: BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer(secretPlaceholder)},
		},
		{
			name:      "database masked placeholder",
			field:     BootstrapConfigSecretDatabaseURL,
			update:    BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer(databasePlaceholder)},
			forbidden: "top-secret",
		},
		{
			name:   "database sensitive query asterisks placeholder",
			field:  BootstrapConfigSecretDatabaseURL,
			update: BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer("postgres://prism@db.local/prism?sslpassword=***")},
		},
		{
			name:   "database sensitive query encoded asterisks placeholder",
			field:  BootstrapConfigSecretDatabaseURL,
			update: BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer("postgres://prism@db.local/prism?sslpassword=%2A%2A%2A")},
		},
		{
			name:   "database sensitive query redacted placeholder",
			field:  BootstrapConfigSecretDatabaseURL,
			update: BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer("postgres://prism@db.local/prism?sslpassword=redacted")},
		},
		{
			name:   "database sensitive query bracketed redacted placeholder",
			field:  BootstrapConfigSecretDatabaseURL,
			update: BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer("postgres://prism@db.local/prism?sslpassword=[redacted]")},
		},
		{
			name:   "database sensitive query set placeholder",
			field:  BootstrapConfigSecretDatabaseURL,
			update: BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer("postgres://prism@db.local/prism?sslpassword=set")},
		},
		{
			name:      "smtp password placeholder",
			field:     BootstrapConfigSecretMailSMTPPassword,
			update:    BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer("set")},
			forbidden: managementTestSMTPPassword,
		},
		{
			name:      "runtime replacement",
			field:     BootstrapConfigSecretRuntimeSecretEncryptionKey,
			update:    BootstrapConfigSecretUpdate{Action: BootstrapConfigSecretActionReplace, Value: stringPointer(runtimeReplacement)},
			forbidden: runtimeReplacement,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := managementRequestForSnapshot(t, snapshot)
			request.SecretUpdates[testCase.field] = testCase.update
			_, err := manager.PrepareBootstrapConfigUpdate(path, request)
			if err == nil {
				t.Fatal("expected invalid secret update to fail")
			}
			var operationError *BootstrapConfigSecretOperationError
			if !errors.As(err, &operationError) {
				t.Fatalf("expected secret operation error, got %T", err)
			}
			if operationError.Field != testCase.field {
				t.Fatal("expected secret operation error to identify the secret field")
			}
			if testCase.forbidden != "" && strings.Contains(err.Error(), testCase.forbidden) {
				t.Fatal("secret operation error exposed a forbidden value")
			}
		})
	}
}

func TestBootstrapConfigManagementRejectsConflictsAndMissingConfirmations(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before conflict checks: %v", err)
	}

	staleRevision := managementRequestForSnapshot(t, snapshot)
	staleRevision.ExpectedRevision = snapshot.FileRevision + 1
	_, err = manager.PrepareBootstrapConfigUpdate(path, staleRevision)
	var conflictError *BootstrapConfigConflictError
	if !errors.As(err, &conflictError) {
		t.Fatalf("expected stale revision conflict, got %T", err)
	}

	staleETag := managementRequestForSnapshot(t, snapshot)
	staleETag.ExpectedETag = "sha256:stale"
	_, err = manager.PrepareBootstrapConfigUpdate(path, staleETag)
	if !errors.As(err, &conflictError) {
		t.Fatalf("expected stale etag conflict, got %T", err)
	}

	zeroRevision := managementRequestForSnapshot(t, snapshot)
	zeroRevision.ExpectedRevision = 0
	_, err = manager.PrepareBootstrapConfigUpdate(path, zeroRevision)
	if !errors.As(err, &conflictError) {
		t.Fatalf("expected zero revision conflict, got %T", err)
	}

	emptyETag := managementRequestForSnapshot(t, snapshot)
	emptyETag.ExpectedETag = ""
	_, err = manager.PrepareBootstrapConfigUpdate(path, emptyETag)
	if !errors.As(err, &conflictError) {
		t.Fatalf("expected empty etag conflict, got %T", err)
	}

	missingConfirmation := managementRequestForSnapshot(t, snapshot)
	values := cloneManagementValues(t, snapshot.Values)
	values.Server.Port = intPointer(18001)
	missingConfirmation.Values = &values
	_, err = manager.PrepareBootstrapConfigUpdate(path, missingConfirmation)
	var confirmationError *BootstrapConfigMissingConfirmationsError
	if !errors.As(err, &confirmationError) {
		t.Fatalf("expected missing confirmation error, got %T", err)
	}
	if !containsString(confirmationError.RequiredConfirmations, BootstrapConfigConfirmationServerPortChange) {
		t.Fatal("expected server port confirmation to be required")
	}
}

func TestBootstrapConfigManagementRejectsInvalidSafeValuesAndPreservesStrictParsing(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, originalPayload := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before validation checks: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(BootstrapConfigValues)
		wantErr string
	}{
		{
			name: "invalid idle connection timeout",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.Transport.IdleConnTimeout = stringPointer("not-a-duration")
			},
			wantErr: "transport.idleConnTimeout must parse as a Go duration",
		},
		{
			name: "invalid request timeout",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.Transport.RequestTimeout = stringPointer("not-a-duration")
			},
			wantErr: "transport.requestTimeout must parse as a Go duration",
		},
		{
			name: "zero request timeout",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.Transport.RequestTimeout = stringPointer("0s")
			},
			wantErr: "transport.requestTimeout must be greater than zero",
		},
		{
			name: "missing side effects object",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.SideEffects = nil
			},
			wantErr: "sideEffects is required",
		},
		{
			name: "missing side effects attempt timeout",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.SideEffects = &BootstrapConfigRuntimeSideEffectsValues{}
			},
			wantErr: "sideEffects.attemptTimeout is required",
		},
		{
			name: "invalid side effects attempt timeout",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.SideEffects.AttemptTimeout = stringPointer("not-a-duration")
			},
			wantErr: "sideEffects.attemptTimeout must parse as a Go duration",
		},
		{
			name: "zero side effects attempt timeout",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.SideEffects.AttemptTimeout = stringPointer("0s")
			},
			wantErr: "sideEffects.attemptTimeout must be greater than zero",
		},
		{
			name: "negative side effects attempt timeout",
			mutate: func(values BootstrapConfigValues) {
				values.Runtime.SideEffects.AttemptTimeout = stringPointer("-1s")
			},
			wantErr: "sideEffects.attemptTimeout must be greater than zero",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := managementRequestForSnapshot(t, snapshot)
			values := cloneManagementValues(t, snapshot.Values)
			testCase.mutate(values)
			request.Values = &values
			_, err := manager.PrepareBootstrapConfigUpdate(path, request)
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("expected validation error containing %q, got %v", testCase.wantErr, err)
			}
		})
	}

	var payload map[string]any
	if err := json.Unmarshal(originalPayload, &payload); err != nil {
		t.Fatalf("decode canonical payload for unknown-field check: %v", err)
	}

	payload["unexpected"] = true
	unknownFieldPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal unknown-field payload: %v", err)
	}
	_, err = manager.Parse(unknownFieldPayload)
	if err == nil || !strings.Contains(err.Error(), `unknown field "unexpected"`) {
		t.Fatalf("expected strict unknown-field error, got %v", err)
	}
}

func TestBootstrapConfigManagementBuildSeededDocumentIncludesSideEffects(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	settings := loadCanonicalDefaultSettings(managementTestDatabaseURL)
	document, err := buildSeededBootstrapDocument(settings, createdAt)
	if err != nil {
		t.Fatalf("build seeded bootstrap document: %v", err)
	}
	if document.Runtime == nil || document.Runtime.SideEffects == nil || document.Runtime.SideEffects.AttemptTimeout == nil || *document.Runtime.SideEffects.AttemptTimeout != "10s" {
		t.Fatalf("expected seeded sideEffects.attemptTimeout to be 10s, got %+v", document.Runtime)
	}
	payload := mustCanonicalManagementPayload(t, document)
	if !bytes.Contains(payload, []byte(`"sideEffects"`)) || !bytes.Contains(payload, []byte(`"attemptTimeout": "10s"`)) {
		t.Fatalf("expected seeded payload to include runtime side effects attempt timeout, got %s", payload)
	}
	parsed, err := NewBootstrapConfigManager(BootstrapConfigManagerOptions{}).Parse(payload)
	if err != nil {
		t.Fatalf("parse seeded runtime side effects payload: %v", err)
	}
	if got := parsed.RuntimeSideEffects().AttemptTimeout; got != 10*time.Second {
		t.Fatalf("expected seeded runtime side effects attempt timeout 10s, got %v", got)
	}
}

func TestBootstrapConfigManagementNoopPreservesCurrentPayload(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, originalPayload := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot before no-op update: %v", err)
	}

	prepared, err := manager.PrepareBootstrapConfigUpdate(path, managementRequestForSnapshot(t, snapshot))

	if err != nil {
		t.Fatalf("prepare no-op management update: %v", err)
	}
	if !prepared.Noop {
		t.Fatal("expected identical values and preserve actions to be a no-op")
	}
	if prepared.Snapshot.FileRevision != snapshot.FileRevision || prepared.Snapshot.UpdatedAt != snapshot.UpdatedAt {
		t.Fatal("expected no-op update to preserve revision and updatedAt")
	}
	if !bytes.Equal(prepared.Payload, originalPayload) {
		t.Fatal("expected no-op prepared payload to match current canonical payload")
	}
	beforeWrite, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config before no-op write: %v", err)
	}
	if _, err := manager.WriteBootstrapConfigUpdate(path, prepared); err != nil {
		t.Fatalf("write no-op management update: %v", err)
	}
	afterWrite, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after no-op write: %v", err)
	}
	if !bytes.Equal(beforeWrite, afterWrite) || !bytes.Equal(afterWrite, originalPayload) {
		t.Fatal("expected no-op write helper to leave the config file unchanged")
	}
}

func newManagementTestDocument(t *testing.T, createdAt time.Time) bootstrapConfigDocument {
	t.Helper()
	settings := loadCanonicalDefaultSettings(managementTestDatabaseURL)
	settings.SecretEncryptionKey = managementTestRuntimeSecret
	settings.AuthJWTSecret = managementTestJWTSecret
	settings.ConfigBundleEncryptionKey = managementTestBundleSecret
	document, err := buildSeededBootstrapDocument(settings, createdAt)
	if err != nil {
		t.Fatalf("build management test bootstrap document: %v", err)
	}
	document.Mail = &bootstrapMail{
		Enabled: boolPointer(true),
		From:    stringPointer("Prism <noreply@example.com>"),
		SMTP: &bootstrapSMTP{
			Host:          stringPointer("smtp.example.com"),
			Port:          intPointer(587),
			Mode:          stringPointer(string(MailSMTPModeStartTLSRequired)),
			Auth:          stringPointer(string(MailSMTPAuthPlain)),
			Username:      stringPointer("smtp-user"),
			Password:      stringPointer(managementTestSMTPPassword),
			Timeout:       stringPointer("15s"),
			TLSServerName: stringPointer("smtp.example.com"),
		},
	}
	return document
}

func managementTestTelemetryDocument() *bootstrapTelemetry {
	return &bootstrapTelemetry{
		Enabled: boolPointer(true),
		Exporter: &bootstrapTelemetryExporter{
			Endpoint:    stringPointer("https://otel-collector.example.test:4318"),
			Protocol:    stringPointer(string(TelemetryExporterProtocolHTTPProtobuf)),
			Compression: stringPointer(string(TelemetryExporterCompressionGzip)),
			Timeout:     stringPointer("7s"),
			Auth: &bootstrapTelemetryExporterAuth{
				Mode:                stringPointer(string(TelemetryExporterAuthModeAuthorizationHeader)),
				AuthorizationHeader: stringPointer(managementTestTelemetryAuthorizationHeader),
			},
			TLS: &bootstrapTelemetryExporterTLS{
				InsecureSkipVerify: boolPointer(false),
				CAFile:             stringPointer("/etc/prism/otel-ca.pem"),
			},
		},
		Metrics: &bootstrapTelemetrySignal{Enabled: boolPointer(true)},
		Traces:  &bootstrapTelemetryTraces{Enabled: boolPointer(true), SamplingRatio: float64Pointer(0.25)},
	}
}

func writeManagementTestDocument(t *testing.T, document bootstrapConfigDocument) (string, []byte) {
	t.Helper()
	payload := mustCanonicalManagementPayload(t, document)
	return writeRawManagementPayload(t, payload), payload
}

func writeRawManagementPayload(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap-config.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write management test bootstrap config: %v", err)
	}
	return path
}

func mustCanonicalManagementPayload(t *testing.T, document bootstrapConfigDocument) []byte {
	t.Helper()
	payload, err := canonicalBootstrapConfigPayload(document)
	if err != nil {
		t.Fatalf("marshal canonical management test payload: %v", err)
	}
	return payload
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal safe management value: %v", err)
	}
	return encoded
}

func assertSafeManagementSnapshot(t *testing.T, payload []byte) {
	t.Helper()
	assertNoSecretValue(t, payload, "database password", "top-secret")
	assertNoSecretValue(t, payload, "database query password", "query-secret")
	assertNoSecretValue(t, payload, "database SSL password", "ssl-password")
	assertNoSecretValue(t, payload, "database passphrase", "passphrase-secret")
	assertNoSecretValue(t, payload, "database passwd", "passwd-secret")
	assertNoSecretValue(t, payload, "runtime secret", managementTestRuntimeSecret)
	assertNoSecretValue(t, payload, "JWT secret", managementTestJWTSecret)
	assertNoSecretValue(t, payload, "bundle secret", managementTestBundleSecret)
	assertNoSecretValue(t, payload, "SMTP password", managementTestSMTPPassword)
	assertNoSecretValue(t, payload, "telemetry authorization header", managementTestTelemetryAuthorizationHeader)
}

func assertNoSecretValue(t *testing.T, payload []byte, label string, secret string) {
	t.Helper()
	if secret == "" {
		return
	}
	if bytes.Contains(payload, []byte(secret)) {
		t.Fatalf("safe management payload exposed %s", label)
	}
}

func cloneManagementValues(t *testing.T, values BootstrapConfigValues) BootstrapConfigValues {
	t.Helper()
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal management values clone: %v", err)
	}
	var clone BootstrapConfigValues
	if err := json.Unmarshal(encoded, &clone); err != nil {
		t.Fatalf("unmarshal management values clone: %v", err)
	}
	return clone
}

func managementValuesWithNullMail(t *testing.T, values BootstrapConfigValues) BootstrapConfigValues {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(mustMarshalJSON(t, values), &payload); err != nil {
		t.Fatalf("decode management values for null mail clone: %v", err)
	}
	payload["mail"] = json.RawMessage("null")
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal management values with null mail: %v", err)
	}
	var clone BootstrapConfigValues
	if err := json.Unmarshal(encoded, &clone); err != nil {
		t.Fatalf("unmarshal management values with null mail: %v", err)
	}
	return clone
}

func assertDisabledSafeMailValues(t *testing.T, values *BootstrapConfigMailValues) {
	t.Helper()
	if values == nil || values.Enabled == nil || *values.Enabled {
		t.Fatalf("expected explicit disabled safe mail values, got %+v", values)
	}
	if values.From != nil || values.ReplyTo != nil || values.SMTP != nil {
		t.Fatalf("expected disabled safe mail values to omit from/reply_to/smtp, got %+v", values)
	}
}

func assertCanonicalDisabledMailPayload(t *testing.T, payload []byte) {
	t.Helper()
	var document struct {
		Mail map[string]json.RawMessage `json:"mail"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("decode disabled mail payload: %v", err)
	}
	if document.Mail == nil {
		t.Fatal("expected canonical payload to include mail")
	}
	if len(document.Mail) != 1 {
		t.Fatalf("expected canonical disabled mail payload to contain only enabled, got keys %v", keysFromRawMessageMap(document.Mail))
	}
	var enabled bool
	if err := json.Unmarshal(document.Mail["enabled"], &enabled); err != nil {
		t.Fatalf("decode canonical disabled mail enabled value: %v", err)
	}
	if enabled {
		t.Fatalf("expected canonical disabled mail payload, got %s", payload)
	}
}

func managementRequestForSnapshot(t *testing.T, snapshot BootstrapConfigSnapshot) BootstrapConfigUpdateRequest {
	t.Helper()
	values := cloneManagementValues(t, snapshot.Values)
	return BootstrapConfigUpdateRequest{
		ExpectedRevision: snapshot.FileRevision,
		ExpectedETag:     snapshot.DocumentETag,
		Values:           &values,
		SecretUpdates:    preserveManagementSecretUpdates(),
	}
}

func preserveManagementSecretUpdates() map[string]BootstrapConfigSecretUpdate {
	return map[string]BootstrapConfigSecretUpdate{
		BootstrapConfigSecretDatabaseURL:                  {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretRuntimeSecretEncryptionKey:   {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretAuthJWTSigningKey:            {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretStateTransferBundleKey:       {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretMailSMTPPassword:             {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretTelemetryAuthorizationHeader: {Action: BootstrapConfigSecretActionPreserve},
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func keysFromRawMessageMap(payload map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

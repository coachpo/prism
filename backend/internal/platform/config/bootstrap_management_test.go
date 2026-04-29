package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	managementTestDatabaseURL   = "postgres://prism:top-secret@db.internal:5432/prism?sslmode=disable&password=query-secret&sslpassword=ssl-password&passphrase=passphrase-secret&passwd=passwd-secret"
	managementTestRuntimeSecret = "runtime-secret-for-management-test"
	managementTestJWTSecret     = "jwt-secret-for-management-test"
	managementTestBundleSecret  = "bundle-secret-for-management-test"
	managementTestSMTPPassword  = "smtp-password-for-management-test"
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
	if snapshot.Values.Database == nil || snapshot.Values.Database.RuntimePool == nil || snapshot.Values.Auth == nil {
		t.Fatal("expected safe values to include editable sections")
	}

	encoded := mustMarshalJSON(t, snapshot)
	assertSafeManagementSnapshot(t, encoded)
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
	runtimeSecret := snapshot.Secrets[BootstrapConfigSecretRuntimeSecretEncryptionKey]
	if !runtimeSecret.Configured || runtimeSecret.Editable || runtimeSecret.Masked != "set" {
		t.Fatal("expected runtime secret metadata to be configured and read-only")
	}
	smtpSecret := snapshot.Secrets[BootstrapConfigSecretMailSMTPPassword]
	if !smtpSecret.Configured || !smtpSecret.Editable || smtpSecret.Masked != "set" {
		t.Fatal("expected SMTP password metadata to be editable and masked")
	}
}

func TestBootstrapConfigManagementResponseAddsLoadedMetadata(t *testing.T) {
	createdAt := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	path, _ := writeManagementTestDocument(t, newManagementTestDocument(t, createdAt))
	manager := NewBootstrapConfigManager(BootstrapConfigManagerOptions{})
	snapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load snapshot for safe response: %v", err)
	}

	currentResponse := BuildBootstrapConfigResponse(snapshot, snapshot.FileRevision, snapshot.DocumentETag, true)
	if currentResponse.RestartRequired || !currentResponse.Writable {
		t.Fatal("expected current loaded metadata to produce writable non-restart response")
	}
	staleResponse := BuildBootstrapConfigResponse(snapshot, snapshot.FileRevision-1, "sha256:loaded", false)
	if !staleResponse.RestartRequired || staleResponse.Writable {
		t.Fatal("expected stale loaded metadata to produce read-only restart-required response")
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
	if settings.AuthAccessTokenTTLSeconds != 1200 || settings.AuthJWTSecret != rotatedJWTSecret {
		t.Fatal("expected prepared payload to include auth changes")
	}
	if _, err := manager.WriteBootstrapConfigUpdate(path, prepared); err != nil {
		t.Fatalf("write prepared management update: %v", err)
	}
	loadedSettings, err := manager.Load(path)
	if err != nil {
		t.Fatalf("load written management update: %v", err)
	}

	if loadedSettings.AuthJWTSecret != rotatedJWTSecret || loadedSettings.AuthAccessTokenTTLSeconds != 1200 {
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

	invalidDuration := managementRequestForSnapshot(t, snapshot)
	values := cloneManagementValues(t, snapshot.Values)
	values.Runtime.Transport.IdleConnTimeout = stringPointer("not-a-duration")
	invalidDuration.Values = &values
	_, err = manager.PrepareBootstrapConfigUpdate(path, invalidDuration)
	if err == nil || !strings.Contains(err.Error(), "runtime.transport.idleConnTimeout must parse as a Go duration") {
		t.Fatalf("expected duration validation error, got %v", err)
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
		BootstrapConfigSecretDatabaseURL:                {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretRuntimeSecretEncryptionKey: {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretAuthJWTSigningKey:          {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretStateTransferBundleKey:     {Action: BootstrapConfigSecretActionPreserve},
		BootstrapConfigSecretMailSMTPPassword:           {Action: BootstrapConfigSecretActionPreserve},
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

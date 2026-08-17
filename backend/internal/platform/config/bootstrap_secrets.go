package config

import (
	"net/url"
	"strings"
)

const (
	BootstrapConfigSecretDatabaseURL                  = "database.url"
	BootstrapConfigSecretRuntimeSecretEncryptionKey   = "runtime.secretEncryptionKey"
	BootstrapConfigSecretAuthJWTSigningKey            = "auth.jwtSigningKey"
	BootstrapConfigSecretTelemetryAuthorizationHeader = "telemetry.exporter.auth.authorizationHeader"
)

type BootstrapConfigSecretMetadata struct {
	Configured bool   `json:"configured"`
	Editable   bool   `json:"editable"`
	Masked     string `json:"masked"`
}

func bootstrapConfigSecretMetadata(document bootstrapConfigDocument) map[string]BootstrapConfigSecretMetadata {
	return map[string]BootstrapConfigSecretMetadata{
		BootstrapConfigSecretDatabaseURL:                  secretMetadata(document.Database.URL, true, maskBootstrapDatabaseURL),
		BootstrapConfigSecretRuntimeSecretEncryptionKey:   secretMetadata(document.Runtime.SecretEncryptionKey, false, maskConfiguredBootstrapSecret),
		BootstrapConfigSecretAuthJWTSigningKey:            secretMetadata(document.Auth.JWTSigningKey, true, maskConfiguredBootstrapSecret),
		BootstrapConfigSecretTelemetryAuthorizationHeader: secretMetadata(bootstrapTelemetryAuthorizationHeader(document.Telemetry), true, maskConfiguredBootstrapSecret),
	}
}

func secretMetadata(value *string, editable bool, mask func(string) string) BootstrapConfigSecretMetadata {
	if value == nil || strings.TrimSpace(*value) == "" {
		return BootstrapConfigSecretMetadata{Editable: editable}
	}
	return BootstrapConfigSecretMetadata{Configured: true, Editable: editable, Masked: mask(*value)}
}

func maskConfiguredBootstrapSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "set"
}

func maskBootstrapDatabaseURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "set"
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(username, "***")
		}
	}
	query := parsed.Query()
	for key, values := range query {
		if !isSensitiveDatabaseURLQueryKey(key) {
			continue
		}
		for index := range values {
			values[index] = "***"
		}
		query[key] = values
	}
	parsed.RawQuery = query.Encode()
	masked := parsed.String()
	masked = strings.ReplaceAll(masked, "%2A%2A%2A", "***")
	masked = strings.ReplaceAll(masked, "%2a%2a%2a", "***")
	return masked
}

func isSensitiveDatabaseURLQueryKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return lower == "pass" || lower == "pwd" || lower == "passwd" || strings.Contains(lower, "password") || strings.Contains(lower, "passphrase") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "key")
}

func currentBootstrapDatabaseURL(document *bootstrapConfigDocument) *string {
	if document == nil || document.Database == nil {
		return nil
	}
	return cloneStringPointer(document.Database.URL)
}

func currentBootstrapRuntimeSecret(document *bootstrapConfigDocument) *string {
	if document == nil || document.Runtime == nil {
		return nil
	}
	return cloneStringPointer(document.Runtime.SecretEncryptionKey)
}

func currentBootstrapAuthJWTSigningKey(document *bootstrapConfigDocument) *string {
	if document == nil || document.Auth == nil {
		return nil
	}
	return cloneStringPointer(document.Auth.JWTSigningKey)
}

func currentBootstrapTelemetryAuthorizationHeader(document *bootstrapConfigDocument) *string {
	if document == nil {
		return nil
	}
	return cloneStringPointer(bootstrapTelemetryAuthorizationHeader(document.Telemetry))
}

func bootstrapTelemetryAuthorizationHeader(telemetry *bootstrapTelemetry) *string {
	if telemetry == nil || telemetry.Exporter == nil || telemetry.Exporter.Auth == nil {
		return nil
	}
	return telemetry.Exporter.Auth.AuthorizationHeader
}

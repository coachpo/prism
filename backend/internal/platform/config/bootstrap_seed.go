package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (m BootstrapConfigManager) seedPayloadFromDefaults() ([]byte, Settings, error) {
	document, err := buildSeededBootstrapDocument(Load(), m.resolvedTimeNow()().UTC())
	if err != nil {
		return nil, Settings{}, err
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, Settings{}, fmt.Errorf("marshal seeded bootstrap config JSON: %w", err)
	}
	settings, err := m.Parse(raw)
	if err != nil {
		return nil, Settings{}, fmt.Errorf("validate seeded bootstrap config: %w", err)
	}
	return raw, settings, nil
}

func buildSeededBootstrapDocument(settings Settings, now time.Time) (bootstrapConfigDocument, error) {
	postgresPoolsBudget := settings.PostgresPoolsBudgetOrDefault()
	managementAdmissionBudget := settings.ManagementAdmissionBudget()
	runtimeSideEffects := settings.RuntimeSideEffects()
	corsAllowedOrigins := settings.CORSAllowedOriginsList()
	databaseURL := strings.TrimSpace(settings.DatabaseURL)
	if databaseURL == "" {
		return bootstrapConfigDocument{}, fmt.Errorf("seeded database URL is empty")
	}
	runtimeSecretEncryptionKey := strings.TrimSpace(settings.SecretEncryptionKey)
	if runtimeSecretEncryptionKey == "" {
		return bootstrapConfigDocument{}, fmt.Errorf("seeded runtime secret encryption key is empty")
	}
	authJWTSecret := strings.TrimSpace(settings.AuthJWTSecret)
	if authJWTSecret == "" {
		return bootstrapConfigDocument{}, fmt.Errorf("seeded auth JWT signing key is empty")
	}
	timestamp := now.UTC().Format(time.RFC3339)

	return bootstrapConfigDocument{
		Meta: &bootstrapMeta{
			SchemaVersion: intPointer(bootstrapConfigSchemaVersion),
			Revision:      intPointer(1),
			CreatedAt:     stringPointer(timestamp),
			UpdatedAt:     stringPointer(timestamp),
		},
		Server: &bootstrapServer{
			Host: stringPointer(strings.TrimSpace(settings.Host)),
			Port: intPointer(settings.Port),
		},
		Database: &bootstrapDatabase{
			URL: stringPointer(databaseURL),
			Pools: &bootstrapDatabasePools{
				TotalMaxConns:    intPointer(int(postgresPoolsBudget.TotalMaxConns)),
				Management:       bootstrapDatabasePoolFromBudget(postgresPoolsBudget.Management),
				RuntimeExecution: bootstrapDatabasePoolFromBudget(postgresPoolsBudget.RuntimeExecution),
				RuntimeTelemetry: bootstrapDatabasePoolFromBudget(postgresPoolsBudget.RuntimeTelemetry),
				RuntimeFeedback:  bootstrapDatabasePoolFromBudget(postgresPoolsBudget.RuntimeFeedback),
				CacheRefresh:     bootstrapDatabasePoolFromBudget(postgresPoolsBudget.CacheRefresh),
				BackgroundJobs:   bootstrapDatabasePoolFromBudget(postgresPoolsBudget.BackgroundJobs),
			},
			ManagementAdmission: &bootstrapManagementAdmission{
				M2MaxConcurrent: intPointer(int(managementAdmissionBudget.M2MaxConcurrent)),
				M3MaxConcurrent: intPointer(int(managementAdmissionBudget.M3MaxConcurrent)),
			},
		},
		Runtime: &bootstrapRuntime{
			SecretEncryptionKey: stringPointer(runtimeSecretEncryptionKey),
			SideEffects: &bootstrapRuntimeSideEffects{
				AttemptTimeout: stringPointer(bootstrapDurationString(runtimeSideEffects.AttemptTimeout)),
			},
		},
		HTTP: &bootstrapHTTP{
			CORSAllowedOrigins: &corsAllowedOrigins,
		},
		Auth: &bootstrapAuth{
			JWTSigningKey:          stringPointer(authJWTSecret),
			AccessTokenTTLSeconds:  intPointer(settings.AuthAccessTokenTTLSeconds),
			RefreshTokenTTLSeconds: intPointer(settings.AuthRefreshTokenTTLSeconds),
			ResetCodeTTLSeconds:    intPointer(settings.AuthResetCodeTTLSeconds),
			AccessCookieName:       stringPointer(strings.TrimSpace(settings.AuthCookieName)),
			RefreshCookieName:      stringPointer(strings.TrimSpace(settings.AuthRefreshCookieName)),
			CookieSecure:           boolPointer(settings.AuthCookieSecure),
		},
		Alerting: &bootstrapAlerting{
			WebhookURL: stringPointer(strings.TrimSpace(settings.Alerting.WebhookURL)),
		},
		Mail: &bootstrapMail{
			Enabled: boolPointer(false),
		},
		Telemetry: &bootstrapTelemetry{
			Enabled: boolPointer(false),
		},
	}, nil
}

func bootstrapDatabasePoolFromBudget(budget DatabasePoolBudget) *bootstrapDatabasePool {
	return &bootstrapDatabasePool{MaxConns: intPointer(int(budget.MaxConns)), MinIdleConns: intPointer(int(budget.MinIdleConns))}
}

func bootstrapDurationString(duration time.Duration) string {
	return duration.String()
}

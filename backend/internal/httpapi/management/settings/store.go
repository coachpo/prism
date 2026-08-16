package settings

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadOrCreateLogRetentionSettings(ctx context.Context, exec queryExecutor, currentTime time.Time) (logRetentionSettingsRow, error) {
	record, err := scanLogRetentionSettingsRow(exec.QueryRow(ctx, `SELECT request_logs_retention_days, audit_logs_retention_days, statistics_retention_days, loadbalance_events_retention_days, created_at, updated_at FROM log_retention_settings WHERE singleton_key = 'global' FOR UPDATE`))
	if err == nil {
		return record, nil
	}
	if err != pgx.ErrNoRows {
		return logRetentionSettingsRow{}, fmt.Errorf("load global log retention settings: %w", err)
	}
	return scanLogRetentionSettingsRow(exec.QueryRow(ctx, `INSERT INTO log_retention_settings (singleton_key, created_at, updated_at) VALUES ('global', $1, $1) ON CONFLICT (singleton_key) DO UPDATE SET singleton_key = EXCLUDED.singleton_key RETURNING request_logs_retention_days, audit_logs_retention_days, statistics_retention_days, loadbalance_events_retention_days, created_at, updated_at`, currentTime))
}

func updateLogRetentionSettings(ctx context.Context, exec queryExecutor, record logRetentionSettingsRow) error {
	_, err := exec.Exec(ctx, `UPDATE log_retention_settings SET request_logs_retention_days = $1, audit_logs_retention_days = $2, statistics_retention_days = $3, loadbalance_events_retention_days = $4, updated_at = $5 WHERE singleton_key = 'global'`, record.RequestLogsRetentionDays, record.AuditLogsRetentionDays, record.StatisticsRetentionDays, record.LoadbalanceEventsRetentionDays, record.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update global log retention settings: %w", err)
	}
	return nil
}

func scanLogRetentionSettingsRow(row pgx.Row) (logRetentionSettingsRow, error) {
	var record logRetentionSettingsRow
	var requestLogsRetentionDays sql.NullInt32
	var auditLogsRetentionDays sql.NullInt32
	var statisticsRetentionDays sql.NullInt32
	var loadbalanceEventsRetentionDays sql.NullInt32
	if err := row.Scan(&requestLogsRetentionDays, &auditLogsRetentionDays, &statisticsRetentionDays, &loadbalanceEventsRetentionDays, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return logRetentionSettingsRow{}, err
	}
	record.RequestLogsRetentionDays = nullableIntValue(requestLogsRetentionDays)
	record.AuditLogsRetentionDays = nullableIntValue(auditLogsRetentionDays)
	record.StatisticsRetentionDays = nullableIntValue(statisticsRetentionDays)
	record.LoadbalanceEventsRetentionDays = nullableIntValue(loadbalanceEventsRetentionDays)
	return record, nil
}

func loadOrCreateUserSettings(ctx context.Context, tx pgx.Tx, profileID int, currentTime time.Time) (userSettingsRow, error) {
	record, found, err := loadUserSettings(ctx, tx, profileID, true)
	if err != nil {
		return userSettingsRow{}, err
	}
	if found {
		return record, nil
	}
	return insertDefaultUserSettings(ctx, tx, profileID, currentTime)
}

func loadUserSettings(ctx context.Context, exec queryExecutor, profileID int, forUpdate bool) (userSettingsRow, bool, error) {
	query := `SELECT id, profile_id,
		report_currency_code,
		report_currency_symbol,
		timezone_preference, current_reporting_currency_epoch_id, pricing_migration_state, legacy_migration_issues,
		pricing_template_generation, pricing_reference_generation, created_at, updated_at
		FROM user_settings WHERE profile_id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	record, err := scanUserSettingsRow(exec.QueryRow(ctx, query, profileID))
	if err == pgx.ErrNoRows {
		return userSettingsRow{}, false, nil
	}
	if err != nil {
		return userSettingsRow{}, false, fmt.Errorf("load user settings for profile %d: %w", profileID, err)
	}
	return record, true, nil
}

func insertDefaultUserSettings(ctx context.Context, exec queryExecutor, profileID int, currentTime time.Time) (userSettingsRow, error) {
	// Steady-state fresh profiles must create epoch 1 before the settings row
	// that points at it (SPEC 11.1 final seed order / 5.1 canonical code).
	var epochID int64
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO reporting_currency_epochs (
			profile_id, epoch, currency_code, currency_symbol, effective_at,
			superseded_at, created_at, updated_at
		) VALUES ($1, 1, $2, $3, NULL, NULL, $4, $4)
		ON CONFLICT (profile_id, epoch) DO NOTHING`,
		profileID,
		"USD",
		"$",
		currentTime,
	); err != nil {
		return userSettingsRow{}, fmt.Errorf("create default reporting currency epoch 1 for profile %d: %w", profileID, err)
	}
	if err := exec.QueryRow(
		ctx,
		`SELECT id FROM reporting_currency_epochs WHERE profile_id = $1 AND epoch = 1`,
		profileID,
	).Scan(&epochID); err != nil {
		return userSettingsRow{}, fmt.Errorf("load default reporting currency epoch 1 for profile %d: %w", profileID, err)
	}
	created, err := scanUserSettingsRow(exec.QueryRow(
		ctx,
		`INSERT INTO user_settings (
			profile_id, report_currency_code, report_currency_symbol, timezone_preference,
			current_reporting_currency_epoch_id, pricing_migration_state,
			pricing_template_generation, pricing_reference_generation,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 'ready', 0, 0, $6, $6)
			RETURNING id, profile_id, report_currency_code, report_currency_symbol, timezone_preference,
				current_reporting_currency_epoch_id, pricing_migration_state, legacy_migration_issues,
				pricing_template_generation, pricing_reference_generation, created_at, updated_at`,
		profileID,
		"USD",
		"$",
		nil,
		epochID,
		currentTime,
	))
	if err != nil {
		return userSettingsRow{}, fmt.Errorf("insert default user settings for profile %d: %w", profileID, err)
	}
	return created, nil
}

func updateUserSettings(ctx context.Context, exec queryExecutor, record userSettingsRow) error {
	if _, err := exec.Exec(
		ctx,
		`UPDATE user_settings SET report_currency_code = $2, report_currency_symbol = $3,
			timezone_preference = $4, updated_at = $5 WHERE id = $1`,
		record.ID,
		record.ReportCurrencyCode,
		record.ReportCurrencySymbol,
		nullableString(record.TimezonePreference),
		record.UpdatedAt,
	); err != nil {
		return fmt.Errorf("update user settings %d: %w", record.ID, err)
	}
	return nil
}

func listAuditSettings(ctx context.Context, exec queryExecutor, profileID int) ([]auditSettingsRow, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT api_family, audit_enabled, audit_capture_bodies, created_at, updated_at
		 FROM profile_api_family_audit_settings
		 WHERE profile_id = $1
		 ORDER BY CASE api_family WHEN 'openai' THEN 1 WHEN 'anthropic' THEN 2 WHEN 'gemini' THEN 3 ELSE 4 END`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query audit settings for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]auditSettingsRow, 0, len(auditAPIFamilies))
	for rows.Next() {
		var item auditSettingsRow
		if err := rows.Scan(&item.APIFamily, &item.AuditEnabled, &item.AuditCaptureBodies, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan audit setting: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit settings for profile %d: %w", profileID, err)
	}
	return items, nil
}

func replaceAuditSettings(ctx context.Context, tx pgx.Tx, profileID int, settings []auditSetting, currentTime time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM profile_api_family_audit_settings WHERE profile_id = $1`, profileID); err != nil {
		return fmt.Errorf("clear audit settings for profile %d: %w", profileID, err)
	}
	for _, setting := range settings {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)`,
			profileID,
			setting.APIFamily,
			setting.AuditEnabled,
			setting.AuditCaptureBodies,
			currentTime,
		); err != nil {
			return fmt.Errorf("insert audit setting %s for profile %d: %w", setting.APIFamily, profileID, err)
		}
	}
	return nil
}

func listValidConnectionPairs(ctx context.Context, exec queryExecutor, profileID int, endpointIDs []int) (map[string]struct{}, error) {
	valid := map[string]struct{}{}
	if len(endpointIDs) == 0 {
		return valid, nil
	}
	args := []any{profileID, toInt32Slice(endpointIDs)}
	rows, err := exec.Query(
		ctx,
		`SELECT DISTINCT model_configs.model_id, connections.endpoint_id
		 FROM model_configs
		 JOIN model_access_targets ON model_access_targets.source_model_config_id = model_configs.id
		 JOIN connections ON connections.id = model_access_targets.target_connection_id
		 WHERE model_configs.profile_id = $1
		   AND model_access_targets.profile_id = $1
		   AND model_access_targets.target_type = 'connection'
		   AND connections.profile_id = $1
		   AND connections.endpoint_id = ANY($2)
		 ORDER BY model_configs.model_id ASC, connections.endpoint_id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query valid connection pairs for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var modelID string
		var endpointID int
		if err := rows.Scan(&modelID, &endpointID); err != nil {
			return nil, fmt.Errorf("scan valid connection pair: %w", err)
		}
		valid[connectionPairKey(modelID, endpointID)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate valid connection pairs for profile %d: %w", profileID, err)
	}
	return valid, nil
}

func scanUserSettingsRow(scanner interface{ Scan(...any) error }) (userSettingsRow, error) {
	var code, symbol sql.NullString
	var timezone sql.NullString
	record := userSettingsRow{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &code, &symbol, &timezone, &record.CurrentReportingCurrencyEpochID, &record.PricingMigrationState, &record.LegacyMigrationIssues, &record.PricingTemplateGeneration, &record.PricingReferenceGeneration, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return userSettingsRow{}, err
	}
	if code.Valid {
		record.ReportCurrencyCode = code.String
	}
	if symbol.Valid {
		record.ReportCurrencySymbol = symbol.String
	}
	record.TimezonePreference = nullableStringValue(timezone)
	if record.LegacyMigrationIssues == nil {
		record.LegacyMigrationIssues = []string{}
	}
	return record, nil
}

func connectionPairKey(modelID string, endpointID int) string {
	return strings.TrimSpace(modelID) + "\x00" + fmt.Sprintf("%d", endpointID)
}

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntValue(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func toInt32Slice(values []int) []int32 {
	converted := make([]int32, 0, len(values))
	for _, value := range values {
		converted = append(converted, int32(value))
	}
	return converted
}

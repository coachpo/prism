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
	query := `SELECT id, profile_id, report_currency_code, report_currency_symbol, timezone_preference, request_logs_retention_days, statistics_retention_days, audit_logs_retention_days, created_at, updated_at FROM user_settings WHERE profile_id = $1`
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
	created, err := scanUserSettingsRow(exec.QueryRow(
		ctx,
		`INSERT INTO user_settings (profile_id, report_currency_code, report_currency_symbol, timezone_preference, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, profile_id, report_currency_code, report_currency_symbol, timezone_preference, request_logs_retention_days, statistics_retention_days, audit_logs_retention_days, created_at, updated_at`,
		profileID,
		"USD",
		"$",
		nil,
		currentTime,
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
		`UPDATE user_settings SET report_currency_code = $2, report_currency_symbol = $3, timezone_preference = $4, request_logs_retention_days = $5, statistics_retention_days = $6, audit_logs_retention_days = $7, updated_at = $8 WHERE id = $1`,
		record.ID,
		record.ReportCurrencyCode,
		record.ReportCurrencySymbol,
		nullableString(record.TimezonePreference),
		nullableInt(record.RequestLogsRetentionDays),
		nullableInt(record.StatisticsRetentionDays),
		nullableInt(record.AuditLogsRetentionDays),
		record.UpdatedAt,
	); err != nil {
		return fmt.Errorf("update user settings %d: %w", record.ID, err)
	}
	return nil
}

func listEndpointFXMappings(ctx context.Context, exec queryExecutor, profileID int) ([]endpointFXMapping, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT model_id, endpoint_id, fx_rate FROM endpoint_fx_rate_settings WHERE profile_id = $1 ORDER BY model_id ASC, endpoint_id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query endpoint fx mappings for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]endpointFXMapping, 0)
	for rows.Next() {
		var item endpointFXMapping
		if err := rows.Scan(&item.ModelID, &item.EndpointID, &item.FXRate); err != nil {
			return nil, fmt.Errorf("scan endpoint fx mapping: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint fx mappings for profile %d: %w", profileID, err)
	}
	return items, nil
}

func replaceEndpointFXMappings(ctx context.Context, tx pgx.Tx, profileID int, mappings []endpointFXMapping, currentTime time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM endpoint_fx_rate_settings WHERE profile_id = $1`, profileID); err != nil {
		return fmt.Errorf("clear endpoint fx mappings for profile %d: %w", profileID, err)
	}
	for _, mapping := range mappings {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			profileID,
			mapping.ModelID,
			mapping.EndpointID,
			mapping.FXRate,
			currentTime,
			currentTime,
		); err != nil {
			return fmt.Errorf("insert endpoint fx mapping (%s, %d): %w", mapping.ModelID, mapping.EndpointID, err)
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
		`SELECT model_configs.model_id, connections.endpoint_id
		 FROM model_configs
		 JOIN connections ON connections.model_config_id = model_configs.id
		 WHERE model_configs.profile_id = $1
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
	var timezone sql.NullString
	var requestLogsRetentionDays sql.NullInt32
	var statisticsRetentionDays sql.NullInt32
	var auditLogsRetentionDays sql.NullInt32
	record := userSettingsRow{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.ReportCurrencyCode, &record.ReportCurrencySymbol, &timezone, &requestLogsRetentionDays, &statisticsRetentionDays, &auditLogsRetentionDays, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return userSettingsRow{}, err
	}
	record.TimezonePreference = nullableStringValue(timezone)
	record.RequestLogsRetentionDays = nullableIntValue(requestLogsRetentionDays)
	record.StatisticsRetentionDays = nullableIntValue(statisticsRetentionDays)
	record.AuditLogsRetentionDays = nullableIntValue(auditLogsRetentionDays)
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

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func toInt32Slice(values []int) []int32 {
	converted := make([]int32, 0, len(values))
	for _, value := range values {
		converted = append(converted, int32(value))
	}
	return converted
}

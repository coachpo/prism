package settings

import (
	"context"
	"database/sql"
	"fmt"
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

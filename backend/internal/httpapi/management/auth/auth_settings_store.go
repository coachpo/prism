package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

type appAuthSettingsRow struct {
	ID                      int
	AuthEnabled             bool
	Username                sql.NullString
	PasswordHash            sql.NullString
	MustChangePassword      bool
	LastLoginAt             sql.NullTime
	TokenVersion            int
	Revision                int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
	EffectiveAuthGeneration int64
	TransitionState         sql.NullString
	TransitionOperationID   sql.NullString
	TransitionRetryAfterAt  sql.NullTime
	TransitionAttempts      int
}

type AppAuthSettingsSnapshot struct {
	ID                      int
	AuthEnabled             bool
	Username                string
	TokenVersion            int
	EffectiveAuthGeneration int64
	TransitionState         string
	TransitionOperationID   string
	TransitionRetryAfterAt  time.Time
}

func appAuthSettingsSnapshotFromRow(row appAuthSettingsRow) AppAuthSettingsSnapshot {
	retryAfterAt := time.Time{}
	if row.TransitionRetryAfterAt.Valid {
		retryAfterAt = row.TransitionRetryAfterAt.Time
	}
	return AppAuthSettingsSnapshot{
		ID:                      row.ID,
		AuthEnabled:             row.AuthEnabled,
		Username:                stringValue(row.Username),
		TokenVersion:            row.TokenVersion,
		EffectiveAuthGeneration: row.EffectiveAuthGeneration,
		TransitionState:         stringValue(row.TransitionState),
		TransitionOperationID:   stringValue(row.TransitionOperationID),
		TransitionRetryAfterAt:  retryAfterAt,
	}
}

func (s *Service) loadOrCreateAppAuthSettings(ctx context.Context, exec queryExecutor) (appAuthSettingsRow, error) {
	row, err := loadAppAuthSettings(ctx, exec)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if err != nil {
			slog.Error("auth settings load failed", "error", err)
		}
		return appAuthSettingsRow{}, err
	}

	now := s.nowUTC()
	var pointerSchema bool
	if err := exec.QueryRow(ctx, `SELECT to_regclass('public.auth_config_versions') IS NOT NULL`).Scan(&pointerSchema); err != nil {
		return appAuthSettingsRow{}, err
	}
	if pointerSchema {
		var legacyColumnsExist bool
		if err := exec.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'app_auth_settings'
			  AND column_name = 'auth_enabled'
		)`).Scan(&legacyColumnsExist); err != nil {
			return appAuthSettingsRow{}, err
		}
		if legacyColumnsExist {
			if _, err := exec.Exec(ctx, `INSERT INTO app_auth_settings (
				singleton_key, auth_enabled, username, password_hash, email_verification_attempt_count,
				must_change_password, last_login_at, token_version, created_at, updated_at
			) VALUES ($1, FALSE, NULL, NULL, 0, FALSE, NULL, 0, $2, $2)`, "app", now); err != nil {
				return appAuthSettingsRow{}, fmt.Errorf("insert auth settings: %w", err)
			}
			return loadAppAuthSettings(ctx, exec)
		}
		if _, err := exec.Exec(ctx, `INSERT INTO app_auth_settings (singleton_key, email_verification_attempt_count, must_change_password, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)`, "app", 0, false, now, now); err != nil {
			return appAuthSettingsRow{}, fmt.Errorf("insert auth settings: %w", err)
		}
		return loadAppAuthSettings(ctx, exec)
	}
	scanner := exec.QueryRow(ctx, `INSERT INTO app_auth_settings (
			singleton_key, auth_enabled, username, password_hash, email_verification_attempt_count,
			must_change_password, last_login_at, token_version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, auth_enabled, username, password_hash, must_change_password, last_login_at,
			token_version, created_at, updated_at, effective_auth_generation, auth_transition_state,
			auth_transition_operation_id, auth_transition_retry_after_at, auth_transition_attempts`,
		"app", false, nil, nil, 0, false, nil, 0, now, now)
	return scanAppAuthSettings(scanner)
}

func loadAppAuthSettings(ctx context.Context, exec queryExecutor) (appAuthSettingsRow, error) {
	// Schema-aware read (Settings SPEC §14.1 item 9): when the immutable
	// auth_config_versions pointer exists (post-000015), every consumer reads
	// the effective config version and never the transitional in-place
	// columns; after the explicit finalizer drops those columns, the pointer
	// query remains valid. Pre-000015 databases (staged migration prefixes)
	// fall back to the legacy columns.
	var pointerSchema bool
	if err := exec.QueryRow(ctx, `SELECT to_regclass('public.auth_config_versions') IS NOT NULL`).Scan(&pointerSchema); err != nil {
		return appAuthSettingsRow{}, err
	}
	if pointerSchema {
		return loadAppAuthSettingsWithPointer(ctx, exec)
	}
	scanner := exec.QueryRow(
		ctx,
		`SELECT id, auth_enabled, username, password_hash,
			must_change_password, last_login_at, token_version, created_at, updated_at,
			effective_auth_generation, auth_transition_state, auth_transition_operation_id,
			auth_transition_retry_after_at, auth_transition_attempts
		FROM app_auth_settings
		WHERE singleton_key = $1
		ORDER BY id ASC
		LIMIT 1`,
		"app",
	)
	return scanAppAuthSettings(scanner)
}

func loadAppAuthSettingsWithPointer(ctx context.Context, exec queryExecutor) (appAuthSettingsRow, error) {
	scanner := exec.QueryRow(
		ctx,
		`SELECT a.id, a.auth_revision, v.desired_mode, v.username, v.password_hash, v.session_version,
			a.must_change_password, a.last_login_at, a.created_at, a.updated_at,
			a.effective_auth_generation, a.auth_transition_state, a.auth_transition_operation_id,
			a.auth_transition_retry_after_at, a.auth_transition_attempts
		FROM app_auth_settings AS a
		LEFT JOIN auth_config_versions AS v ON v.id = a.effective_config_version_id
		WHERE a.singleton_key = $1
		ORDER BY a.id ASC
		LIMIT 1`,
		"app",
	)
	row := appAuthSettingsRow{}
	var configMode, configUsername, configPasswordHash *string
	var configSessionVersion *int64
	err := scanner.Scan(
		&row.ID,
		&row.Revision,
		&configMode,
		&configUsername,
		&configPasswordHash,
		&configSessionVersion,
		&row.MustChangePassword,
		&row.LastLoginAt,
		&row.CreatedAt,
		&row.UpdatedAt,
		&row.EffectiveAuthGeneration,
		&row.TransitionState,
		&row.TransitionOperationID,
		&row.TransitionRetryAfterAt,
		&row.TransitionAttempts,
	)
	if err != nil {
		return appAuthSettingsRow{}, err
	}
	if configMode != nil {
		row.AuthEnabled = *configMode == "enabled"
	}
	if configUsername != nil {
		row.Username = sql.NullString{String: *configUsername, Valid: true}
	}
	if configPasswordHash != nil {
		row.PasswordHash = sql.NullString{String: *configPasswordHash, Valid: true}
	}
	if configSessionVersion != nil {
		row.TokenVersion = int(*configSessionVersion)
	}
	return row, nil
}

func scanAppAuthSettings(scanner interface{ Scan(...any) error }) (appAuthSettingsRow, error) {
	row := appAuthSettingsRow{}
	err := scanner.Scan(
		&row.ID,
		&row.AuthEnabled,
		&row.Username,
		&row.PasswordHash,
		&row.MustChangePassword,
		&row.LastLoginAt,
		&row.TokenVersion,
		&row.CreatedAt,
		&row.UpdatedAt,
		&row.EffectiveAuthGeneration,
		&row.TransitionState,
		&row.TransitionOperationID,
		&row.TransitionRetryAfterAt,
		&row.TransitionAttempts,
	)
	if err != nil {
		return appAuthSettingsRow{}, err
	}
	return row, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func stringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

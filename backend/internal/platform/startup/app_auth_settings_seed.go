package startup

import (
	"context"
	"fmt"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s Service) seedAppAuthSettings(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		var existingID int
		err := tx.QueryRow(
			ctx,
			`SELECT id FROM app_auth_settings WHERE singleton_key = $1 LIMIT 1`,
			AppAuthSingletonKey,
		).Scan(&existingID)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("query application auth settings: %w", err)
		}

		now := s.timestamp()
		// Post-000012 databases carry the immutable auth_config_versions
		// pointer; the finalizer may already have dropped the transitional
		// in-place credential columns, so the INSERT shape must follow the
		// schema.
		var pointerSchema bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('public.auth_config_versions') IS NOT NULL`).Scan(&pointerSchema); err != nil {
			return fmt.Errorf("check auth config versions table: %w", err)
		}
		if err == pgx.ErrNoRows {
			var legacyCredentialColumns bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = 'app_auth_settings'
				  AND column_name = 'auth_enabled'
			)`).Scan(&legacyCredentialColumns); err != nil {
				return fmt.Errorf("check legacy application auth columns: %w", err)
			}
			// Fresh database: create the singleton row (000012 only backfills
			// existing rows so the startup clock keeps the seeds golden
			// deterministic).
			if pointerSchema && !legacyCredentialColumns {
				if _, err := tx.Exec(
					ctx,
					`INSERT INTO app_auth_settings (
						singleton_key,
						email_verification_attempt_count,
						must_change_password,
						created_at,
						updated_at
					) VALUES (
						$1, $2, $3, $4, $5
					)`,
					AppAuthSingletonKey,
					0,
					false,
					now,
					now,
				); err != nil {
					return fmt.Errorf("insert application auth settings: %w", err)
				}
			} else {
				if _, err := tx.Exec(
					ctx,
					`INSERT INTO app_auth_settings (
						singleton_key,
						auth_enabled,
						username,
						password_hash,
						email_verification_attempt_count,
						must_change_password,
						last_login_at,
						token_version,
						created_at,
						updated_at
					) VALUES (
						$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
					)`,
					AppAuthSingletonKey,
					false,
					nil,
					nil,
					0,
					false,
					nil,
					0,
					now,
					now,
				); err != nil {
					return fmt.Errorf("insert application auth settings: %w", err)
				}
			}
		}

		// Immutable auth config generation 1 + desired/effective pointers
		// (Settings SPEC §14.1 item 11): idempotent, never overwrites an
		// existing effective version.
		if pointerSchema {
			var configVersionExists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM auth_config_versions WHERE subject_key = $1 AND generation = '1'
			)`, AppAuthSingletonKey).Scan(&configVersionExists); err != nil {
				return fmt.Errorf("check auth config generation 1: %w", err)
			}
			if !configVersionExists {
				var configID int64
				if err := tx.QueryRow(ctx, `INSERT INTO auth_config_versions (
					subject_key, generation, desired_mode, username, password_hash,
					session_version, state, created_operation_id, created_at, updated_at
				) VALUES ($1, '1', 'disabled', NULL, NULL, 0, 'effective', NULL, $2, $2)
				RETURNING id`,
					AppAuthSingletonKey, now).Scan(&configID); err != nil {
					return fmt.Errorf("insert auth config generation 1: %w", err)
				}
				if _, err := tx.Exec(ctx, `UPDATE app_auth_settings SET
					auth_revision = 1,
					desired_config_version_id = $2,
					effective_config_version_id = $2,
					desired_generation = '1',
					effective_generation = '1',
					updated_at = $3
					WHERE singleton_key = $1`, AppAuthSingletonKey, configID, now); err != nil {
					return fmt.Errorf("point auth settings at config generation 1: %w", err)
				}
			}
		}
		return nil
	})
}

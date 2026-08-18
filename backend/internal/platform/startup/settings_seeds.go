package startup

import (
	"context"
	"fmt"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s Service) seedUserSettings(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		profileRows, err := tx.Query(
			ctx,
			`SELECT id FROM profiles WHERE deleted_at IS NULL ORDER BY id ASC`,
		)
		if err != nil {
			return fmt.Errorf("query non-deleted profiles for user settings: %w", err)
		}
		defer profileRows.Close()

		profileIDs := []int{}
		for profileRows.Next() {
			var profileID int
			if err := profileRows.Scan(&profileID); err != nil {
				return fmt.Errorf("scan profile id for user settings: %w", err)
			}
			profileIDs = append(profileIDs, profileID)
		}
		if err := profileRows.Err(); err != nil {
			return fmt.Errorf("iterate profile ids for user settings: %w", err)
		}
		if len(profileIDs) == 0 {
			return nil
		}
		profileIDArgs := toInt32Slice(profileIDs)

		existingRows, err := tx.Query(
			ctx,
			`SELECT profile_id FROM user_settings WHERE profile_id = ANY($1)`,
			profileIDArgs,
		)
		if err != nil {
			return fmt.Errorf("query existing user settings: %w", err)
		}
		defer existingRows.Close()

		existing := map[int]struct{}{}
		for existingRows.Next() {
			var profileID int
			if err := existingRows.Scan(&profileID); err != nil {
				return fmt.Errorf("scan existing user settings profile id: %w", err)
			}
			existing[profileID] = struct{}{}
		}
		if err := existingRows.Err(); err != nil {
			return fmt.Errorf("iterate existing user settings profile ids: %w", err)
		}

		now := s.timestamp()
		for _, profileID := range profileIDs {
			if _, ok := existing[profileID]; ok {
				continue
			}
			// Steady-state fresh profiles create epoch 1 before the settings
			// row that points at it (SPEC 11.1 final seed order / 5.1).
			var epochID int64
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO reporting_currency_epochs (
					profile_id, epoch, currency_code, currency_symbol, effective_at,
					superseded_at, created_at, updated_at
				) VALUES ($1, 1, $2, $3, NULL, NULL, $4, $4)
				ON CONFLICT (profile_id, epoch) DO NOTHING`,
				profileID,
				"USD",
				"$",
				now,
			); err != nil {
				return fmt.Errorf("insert default reporting currency epoch 1 for profile %d: %w", profileID, err)
			}
			if err := tx.QueryRow(
				ctx,
				`SELECT id FROM reporting_currency_epochs WHERE profile_id = $1 AND epoch = 1`,
				profileID,
			).Scan(&epochID); err != nil {
				return fmt.Errorf("load default reporting currency epoch 1 for profile %d: %w", profileID, err)
			}
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO user_settings (
					profile_id,
					report_currency_code,
					report_currency_symbol,
					timezone_preference,
					current_reporting_currency_epoch_id,
					pricing_migration_state,
					pricing_template_generation,
					pricing_reference_generation,
					created_at,
					updated_at
				) VALUES ($1, $2, $3, $4, $5, 'ready', 0, 0, $6, $6)`,
				profileID,
				"USD",
				"$",
				nil,
				epochID,
				now,
			); err != nil {
				return fmt.Errorf("insert default user settings for profile %d: %w", profileID, err)
			}
		}

		return nil
	})
}

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

// normalizeEndpointSecrets is the migration-era secret metadata backfill. It
// runs before management/runtime open, must never lose data, and follows the
// endpoint reference contract:
//
//   - empty keys: fingerprint/time stay null, revision stays 1;
//   - pre-migration non-empty keys: verify/decrypt, derive the display
//     fingerprint from the plaintext, and re-encrypt legacy plaintext values;
//   - api_key_updated_at always stays null (no independent identity evidence
//     exists for historical rows);
//   - the original updated_at is preserved and config_revision is left at its
//     default 1, so backfill never masquerades as an Endpoint mutation;
//   - decrypt/auth failure fails fast instead of pretending completion.

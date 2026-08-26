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

func toInt32Slice(values []int) []int32 {
	converted := make([]int32, 0, len(values))
	for _, value := range values {
		converted = append(converted, int32(value))
	}
	return converted
}

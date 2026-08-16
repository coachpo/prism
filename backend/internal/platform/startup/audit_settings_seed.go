package startup

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// seedProfileAuditSettings materializes the disabled three-family default for
// profiles created after the additive migration. Existing rows and their
// immutable migration provenance are preserved byte-for-byte.
func (s Service) seedProfileAuditSettings(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		var schemaExists bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('public.profile_audit_settings_state') IS NOT NULL`).Scan(&schemaExists); err != nil {
			return err
		}
		if !schemaExists {
			return nil
		}
		rows, err := tx.Query(ctx, `SELECT id FROM profiles WHERE deleted_at IS NULL ORDER BY id ASC`)
		if err != nil {
			return fmt.Errorf("query profiles for audit settings: %w", err)
		}
		profileIDs := make([]int, 0)
		for rows.Next() {
			var profileID int
			if err := rows.Scan(&profileID); err != nil {
				rows.Close()
				return fmt.Errorf("scan profile for audit settings: %w", err)
			}
			profileIDs = append(profileIDs, profileID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate profiles for audit settings: %w", err)
		}
		rows.Close()
		for _, profileID := range profileIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO profile_audit_settings_state
				(profile_id, revision, writer_generation, updated_at)
				VALUES ($1, 1, 1, now()) ON CONFLICT (profile_id) DO NOTHING`, profileID); err != nil {
				return fmt.Errorf("seed audit settings state for profile %d: %w", profileID, err)
			}
			for _, family := range []string{"openai", "anthropic", "gemini"} {
				if _, err := tx.Exec(ctx, `INSERT INTO profile_api_family_audit_settings
					(profile_id, api_family, audit_enabled, audit_capture_bodies,
					 migration_provenance, created_at, updated_at)
					VALUES ($1, $2, FALSE, FALSE, 'explicit', now(), now())
					ON CONFLICT ON CONSTRAINT uq_profile_api_family_audit_settings_profile_family DO NOTHING`,
					profileID, family); err != nil {
					return fmt.Errorf("seed audit family %s for profile %d: %w", family, profileID, err)
				}
			}
		}
		return nil
	})
}

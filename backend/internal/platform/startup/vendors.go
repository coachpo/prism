package startup

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type vendorRow struct {
	ID          int
	Key         string
	Name        string
	Description string
	IconKey     string
}

func (s Service) seedVendors(ctx context.Context, conn *pgx.Conn) error {
	return withTransaction(ctx, conn, func(tx pgx.Tx) error {
		now := s.timestamp()
		vendors, err := loadVendors(ctx, tx)
		if err != nil {
			return err
		}

		existingByKey := map[string]vendorRow{}
		for _, vendor := range vendors {
			existingByKey[vendor.Key] = vendor
		}

		legacyGoogleVendor, hasLegacyGoogle := existingByKey["google"]
		geminiVendor, hasGemini := existingByKey["gemini"]
		canonicalGemini, ok := systemVendorByKey["gemini"]
		if !ok {
			return fmt.Errorf("canonical gemini vendor definition is missing")
		}

		if hasLegacyGoogle {
			if hasGemini && geminiVendor.ID != legacyGoogleVendor.ID {
				if err := applyCanonicalVendorIdentity(ctx, tx, geminiVendor, canonicalGemini, now); err != nil {
					return err
				}
				if _, err := tx.Exec(
					ctx,
					`UPDATE model_configs SET vendor_id = $2 WHERE vendor_id = $1`,
					legacyGoogleVendor.ID,
					geminiVendor.ID,
				); err != nil {
					return fmt.Errorf("rewire legacy google vendor references: %w", err)
				}
				if _, err := tx.Exec(ctx, `DELETE FROM vendors WHERE id = $1`, legacyGoogleVendor.ID); err != nil {
					return fmt.Errorf("delete legacy google vendor: %w", err)
				}
			} else {
				if err := applyCanonicalVendorIdentity(ctx, tx, legacyGoogleVendor, canonicalGemini, now); err != nil {
					return err
				}
			}
		}

		vendors, err = loadVendors(ctx, tx)
		if err != nil {
			return err
		}
		existingByKey = map[string]vendorRow{}
		for _, vendor := range vendors {
			existingByKey[vendor.Key] = vendor
		}

		for _, definition := range DefaultVendors {
			if current, ok := existingByKey[definition.Key]; ok {
				if err := applyCanonicalVendorIdentity(ctx, tx, current, definition, now); err != nil {
					return err
				}
				continue
			}

			if _, err := tx.Exec(
				ctx,
				`INSERT INTO vendors (
					key,
					name,
					description,
					icon_key,
					audit_enabled,
					audit_capture_bodies,
					created_at,
					updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				definition.Key,
				definition.Name,
				definition.Description,
				definition.IconKey,
				false,
				true,
				now,
				now,
			); err != nil {
				return fmt.Errorf("insert canonical vendor %s: %w", definition.Key, err)
			}
		}

		return nil
	})
}

func loadVendors(ctx context.Context, exec queryExecutor) ([]vendorRow, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT id, key, name, COALESCE(description, ''), COALESCE(icon_key, '')
		FROM vendors
		ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query vendors: %w", err)
	}
	defer rows.Close()

	vendors := []vendorRow{}
	for rows.Next() {
		var row vendorRow
		if err := rows.Scan(&row.ID, &row.Key, &row.Name, &row.Description, &row.IconKey); err != nil {
			return nil, fmt.Errorf("scan vendor: %w", err)
		}
		vendors = append(vendors, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vendors: %w", err)
	}
	return vendors, nil
}

func applyCanonicalVendorIdentity(ctx context.Context, tx pgx.Tx, current vendorRow, canonical VendorDefinition, now time.Time) error {
	if current.Key == canonical.Key && current.Name == canonical.Name && current.Description == canonical.Description && current.IconKey == canonical.IconKey {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		`UPDATE vendors
		SET key = $2, name = $3, description = $4, icon_key = $5, updated_at = $6
		WHERE id = $1`,
		current.ID,
		canonical.Key,
		canonical.Name,
		canonical.Description,
		canonical.IconKey,
		now,
	); err != nil {
		return fmt.Errorf("canonicalize vendor %d: %w", current.ID, err)
	}
	return nil
}

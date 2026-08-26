package startup

import (
	"context"
	"fmt"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s Service) seedHeaderBlocklistRules(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		existingRows, err := tx.Query(
			ctx,
			`SELECT match_type, pattern
			FROM header_blocklist_rules
			WHERE is_system = TRUE`,
		)
		if err != nil {
			return fmt.Errorf("query system header blocklist rules: %w", err)
		}
		defer existingRows.Close()

		existing := map[string]struct{}{}
		for existingRows.Next() {
			var matchType string
			var pattern string
			if err := existingRows.Scan(&matchType, &pattern); err != nil {
				return fmt.Errorf("scan system header blocklist rule: %w", err)
			}
			existing[matchType+"\x00"+pattern] = struct{}{}
		}
		if err := existingRows.Err(); err != nil {
			return fmt.Errorf("iterate system header blocklist rules: %w", err)
		}

		now := s.timestamp()
		for _, definition := range SystemHeaderBlocklistDefaults {
			key := definition.MatchType + "\x00" + definition.Pattern
			if _, ok := existing[key]; ok {
				continue
			}
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO header_blocklist_rules (
					profile_id,
					name,
					match_type,
					pattern,
					enabled,
					is_system,
					created_at,
					updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				nil,
				definition.Name,
				definition.MatchType,
				definition.Pattern,
				true,
				true,
				now,
				now,
			); err != nil {
				return fmt.Errorf("insert system header blocklist rule %s: %w", definition.Name, err)
			}
		}

		return nil
	})
}

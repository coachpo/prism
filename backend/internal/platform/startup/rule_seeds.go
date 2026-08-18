package startup

import (
	"context"
	"fmt"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s Service) seedUserAgentClientRules(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		existingRules, err := loadSystemUserAgentClientRules(ctx, tx)
		if err != nil {
			return err
		}

		now := s.timestamp()
		for _, definition := range SystemUserAgentClientRuleDefaults {
			matchIndex := -1
			for index, current := range existingRules {
				if current.Name == definition.Name || current.Pattern == definition.Pattern {
					matchIndex = index
					break
				}
			}

			if matchIndex >= 0 {
				current := existingRules[matchIndex]
				if current.Name != definition.Name || current.Pattern != definition.Pattern {
					if _, err := tx.Exec(
						ctx,
						`UPDATE user_agent_client_rules
						SET name = $2, pattern = $3, updated_at = $4
						WHERE id = $1`,
						current.ID,
						definition.Name,
						definition.Pattern,
						now,
					); err != nil {
						return fmt.Errorf("canonicalize system user-agent rule %d: %w", current.ID, err)
					}
					existingRules[matchIndex].Name = definition.Name
					existingRules[matchIndex].Pattern = definition.Pattern
				}
				continue
			}

			var insertedID int
			if err := tx.QueryRow(
				ctx,
				`INSERT INTO user_agent_client_rules (
					profile_id,
					name,
					pattern,
					enabled,
					is_system,
					created_at,
					updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7)
				RETURNING id`,
				nil,
				definition.Name,
				definition.Pattern,
				true,
				true,
				now,
				now,
			).Scan(&insertedID); err != nil {
				return fmt.Errorf("insert system user-agent rule %s: %w", definition.Name, err)
			}
			existingRules = append(existingRules, userAgentClientRuleRow{ID: insertedID, Name: definition.Name, Pattern: definition.Pattern})
		}

		return nil
	})
}

type userAgentClientRuleRow struct {
	ID      int
	Name    string
	Pattern string
}

func loadSystemUserAgentClientRules(ctx context.Context, exec queryExecutor) ([]userAgentClientRuleRow, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT id, name, pattern
		FROM user_agent_client_rules
		WHERE is_system = TRUE
		ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query system user-agent client rules: %w", err)
	}
	defer rows.Close()

	rules := []userAgentClientRuleRow{}
	for rows.Next() {
		var row userAgentClientRuleRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Pattern); err != nil {
			return nil, fmt.Errorf("scan system user-agent client rule: %w", err)
		}
		rules = append(rules, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate system user-agent client rules: %w", err)
	}
	return rules, nil
}

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

func toInt32Slice(values []int) []int32 {
	converted := make([]int32, 0, len(values))
	for _, value := range values {
		converted = append(converted, int32(value))
	}
	return converted
}

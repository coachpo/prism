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
			if _, err := tx.Exec(
				ctx,
				`INSERT INTO user_settings (
					profile_id,
					report_currency_code,
					report_currency_symbol,
					timezone_preference,
					created_at,
					updated_at
				) VALUES ($1, $2, $3, $4, $5, $6)`,
				profileID,
				"USD",
				"$",
				nil,
				now,
				now,
			); err != nil {
				return fmt.Errorf("insert default user settings for profile %d: %w", profileID, err)
			}
		}

		return nil
	})
}

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

func (s Service) seedAppAuthSettings(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		var existingID int
		err := tx.QueryRow(
			ctx,
			`SELECT id FROM app_auth_settings WHERE singleton_key = $1 LIMIT 1`,
			AppAuthSingletonKey,
		).Scan(&existingID)
		if err == nil {
			return nil
		}
		if err != pgx.ErrNoRows {
			return fmt.Errorf("query application auth settings: %w", err)
		}

		now := s.timestamp()
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO app_auth_settings (
				singleton_key,
				auth_enabled,
				username,
				email,
				pending_email,
				password_hash,
				email_bound_at,
				email_verification_code_hash,
				email_verification_expires_at,
				email_verification_attempt_count,
				must_change_password,
				last_login_at,
				token_version,
				created_at,
				updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
			)`,
			AppAuthSingletonKey,
			false,
			nil,
			nil,
			nil,
			nil,
			nil,
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
		return nil
	})
}

func (s Service) normalizeEndpointSecrets(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		rows, err := tx.Query(
			ctx,
			`SELECT id, api_key FROM endpoints ORDER BY id ASC`,
		)
		if err != nil {
			return fmt.Errorf("query endpoints for secret normalization: %w", err)
		}
		defer rows.Close()

		type endpointRow struct {
			ID     int
			APIKey string
		}
		endpoints := []endpointRow{}
		for rows.Next() {
			var row endpointRow
			if err := rows.Scan(&row.ID, &row.APIKey); err != nil {
				return fmt.Errorf("scan endpoint for secret normalization: %w", err)
			}
			endpoints = append(endpoints, row)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate endpoints for secret normalization: %w", err)
		}

		now := s.timestamp()
		for _, endpoint := range endpoints {
			encrypted, err := encryptSecret(endpoint.APIKey, s.secretEncryptionKey, s.now)
			if err != nil {
				return fmt.Errorf("normalize endpoint secret %d: %w", endpoint.ID, err)
			}
			if encrypted == endpoint.APIKey {
				continue
			}
			if _, err := tx.Exec(
				ctx,
				`UPDATE endpoints SET api_key = $2, updated_at = $3 WHERE id = $1`,
				endpoint.ID,
				encrypted,
				now,
			); err != nil {
				return fmt.Errorf("update normalized endpoint secret %d: %w", endpoint.ID, err)
			}
		}

		return nil
	})
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

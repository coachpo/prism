package configrules

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func listUserAgentClientRules(ctx context.Context, exec queryExecutor, profileID int, includeDisabled bool) ([]userAgentClientRuleRow, error) {
	query := `SELECT id, name, pattern, enabled, is_system, profile_id, created_at, updated_at
		FROM user_agent_client_rules
		WHERE is_system = TRUE OR profile_id = $1`
	if !includeDisabled {
		query += ` AND enabled = TRUE`
	}
	query += ` ORDER BY is_system DESC, id ASC`
	rows, err := exec.Query(ctx, query, profileID)
	if err != nil {
		return nil, fmt.Errorf("query user-agent client rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]userAgentClientRuleRow, 0)
	for rows.Next() {
		item, scanErr := scanUserAgentClientRuleRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user-agent client rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadUserAgentClientRule(ctx context.Context, exec queryExecutor, profileID int, ruleID int, forUpdate bool) (userAgentClientRuleRow, bool, error) {
	query := `SELECT id, name, pattern, enabled, is_system, profile_id, created_at, updated_at
		FROM user_agent_client_rules
		WHERE id = $1 AND (is_system = TRUE OR profile_id = $2)`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	row, err := scanUserAgentClientRuleRow(exec.QueryRow(ctx, query, ruleID, profileID))
	if err == pgx.ErrNoRows {
		return userAgentClientRuleRow{}, false, nil
	}
	if err != nil {
		return userAgentClientRuleRow{}, false, fmt.Errorf("load user-agent client rule %d: %w", ruleID, err)
	}
	return row, true, nil
}

func loadOwnedUserAgentClientRule(ctx context.Context, exec queryExecutor, profileID int, ruleID int, forUpdate bool) (userAgentClientRuleRow, bool, error) {
	query := `SELECT id, name, pattern, enabled, is_system, profile_id, created_at, updated_at
		FROM user_agent_client_rules
		WHERE id = $1 AND profile_id = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	row, err := scanUserAgentClientRuleRow(exec.QueryRow(ctx, query, ruleID, profileID))
	if err == pgx.ErrNoRows {
		return userAgentClientRuleRow{}, false, nil
	}
	if err != nil {
		return userAgentClientRuleRow{}, false, fmt.Errorf("load owned user-agent client rule %d: %w", ruleID, err)
	}
	return row, true, nil
}

func insertUserAgentClientRule(ctx context.Context, exec queryExecutor, profileID int, requestBody userAgentClientRuleCreateRequest, currentTime time.Time) (userAgentClientRuleRow, error) {
	created, err := scanUserAgentClientRuleRow(exec.QueryRow(
		ctx,
		`INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, FALSE, $5, $6)
		 RETURNING id, name, pattern, enabled, is_system, profile_id, created_at, updated_at`,
		profileID,
		requestBody.Name,
		requestBody.Pattern,
		resolvedBool(requestBody.Enabled, true),
		currentTime,
		currentTime,
	))
	if err != nil {
		return userAgentClientRuleRow{}, fmt.Errorf("insert user-agent client rule %q: %w", requestBody.Name, err)
	}
	return created, nil
}

func updateUserAgentClientRule(ctx context.Context, exec queryExecutor, row userAgentClientRuleRow) (userAgentClientRuleRow, error) {
	updated, err := scanUserAgentClientRuleRow(exec.QueryRow(
		ctx,
		`UPDATE user_agent_client_rules SET name = $2, pattern = $3, enabled = $4, updated_at = $5
		 WHERE id = $1
		 RETURNING id, name, pattern, enabled, is_system, profile_id, created_at, updated_at`,
		row.ID,
		row.Name,
		row.Pattern,
		row.Enabled,
		row.UpdatedAt,
	))
	if err != nil {
		return userAgentClientRuleRow{}, fmt.Errorf("update user-agent client rule %d: %w", row.ID, err)
	}
	return updated, nil
}

func deleteUserAgentClientRule(ctx context.Context, exec queryExecutor, ruleID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM user_agent_client_rules WHERE id = $1`, ruleID); err != nil {
		return fmt.Errorf("delete user-agent client rule %d: %w", ruleID, err)
	}
	return nil
}

func scanUserAgentClientRuleRow(scanner interface{ Scan(...any) error }) (userAgentClientRuleRow, error) {
	var profileID sql.NullInt32
	row := userAgentClientRuleRow{}
	if err := scanner.Scan(&row.ID, &row.Name, &row.Pattern, &row.Enabled, &row.IsSystem, &profileID, &row.CreatedAt, &row.UpdatedAt); err != nil {
		return userAgentClientRuleRow{}, err
	}
	row.ProfileID = nullableInt32(profileID)
	return row, nil
}

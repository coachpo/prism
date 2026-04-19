package configrules

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func listHeaderBlocklistRules(ctx context.Context, exec queryExecutor, profileID int, includeDisabled bool) ([]headerBlocklistRuleRow, error) {
	query := `SELECT id, name, match_type, pattern, enabled, is_system, profile_id, created_at, updated_at
		FROM header_blocklist_rules
		WHERE is_system = TRUE OR profile_id = $1`
	if !includeDisabled {
		query += ` AND enabled = TRUE`
	}
	query += ` ORDER BY is_system DESC, id ASC`
	rows, err := exec.Query(ctx, query, profileID)
	if err != nil {
		return nil, fmt.Errorf("query header blocklist rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]headerBlocklistRuleRow, 0)
	for rows.Next() {
		item, scanErr := scanHeaderBlocklistRuleRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate header blocklist rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadHeaderBlocklistRule(ctx context.Context, exec queryExecutor, profileID int, ruleID int, forUpdate bool) (headerBlocklistRuleRow, bool, error) {
	query := `SELECT id, name, match_type, pattern, enabled, is_system, profile_id, created_at, updated_at
		FROM header_blocklist_rules
		WHERE id = $1 AND (is_system = TRUE OR profile_id = $2)`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	row, err := scanHeaderBlocklistRuleRow(exec.QueryRow(ctx, query, ruleID, profileID))
	if err == pgx.ErrNoRows {
		return headerBlocklistRuleRow{}, false, nil
	}
	if err != nil {
		return headerBlocklistRuleRow{}, false, fmt.Errorf("load header blocklist rule %d: %w", ruleID, err)
	}
	return row, true, nil
}

func loadOwnedHeaderBlocklistRule(ctx context.Context, exec queryExecutor, profileID int, ruleID int, forUpdate bool) (headerBlocklistRuleRow, bool, error) {
	query := `SELECT id, name, match_type, pattern, enabled, is_system, profile_id, created_at, updated_at
		FROM header_blocklist_rules
		WHERE id = $1 AND profile_id = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	row, err := scanHeaderBlocklistRuleRow(exec.QueryRow(ctx, query, ruleID, profileID))
	if err == pgx.ErrNoRows {
		return headerBlocklistRuleRow{}, false, nil
	}
	if err != nil {
		return headerBlocklistRuleRow{}, false, fmt.Errorf("load owned header blocklist rule %d: %w", ruleID, err)
	}
	return row, true, nil
}

func findHeaderBlocklistDuplicate(ctx context.Context, exec queryExecutor, profileID int, matchType string, pattern string, excludeID *int) (bool, error) {
	query := `SELECT id FROM header_blocklist_rules WHERE match_type = $1 AND pattern = $2 AND (is_system = TRUE OR profile_id = $3)`
	args := []any{matchType, pattern, profileID}
	if excludeID != nil {
		query += ` AND id != $4`
		args = append(args, *excludeID)
	}
	query += ` LIMIT 1`
	var existingID int
	err := exec.QueryRow(ctx, query, args...).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query header blocklist duplicate (%s, %s): %w", matchType, pattern, err)
	}
	return true, nil
}

func insertHeaderBlocklistRule(ctx context.Context, exec queryExecutor, profileID int, requestBody headerBlocklistRuleCreateRequest, currentTime time.Time) (headerBlocklistRuleRow, error) {
	created, err := scanHeaderBlocklistRuleRow(exec.QueryRow(
		ctx,
		`INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, FALSE, $6, $7)
		 RETURNING id, name, match_type, pattern, enabled, is_system, profile_id, created_at, updated_at`,
		profileID,
		requestBody.Name,
		requestBody.MatchType,
		requestBody.Pattern,
		resolvedBool(requestBody.Enabled, true),
		currentTime,
		currentTime,
	))
	if err != nil {
		return headerBlocklistRuleRow{}, fmt.Errorf("insert header blocklist rule %q: %w", requestBody.Name, err)
	}
	return created, nil
}

func updateHeaderBlocklistRule(ctx context.Context, exec queryExecutor, row headerBlocklistRuleRow) (headerBlocklistRuleRow, error) {
	updated, err := scanHeaderBlocklistRuleRow(exec.QueryRow(
		ctx,
		`UPDATE header_blocklist_rules SET name = $2, match_type = $3, pattern = $4, enabled = $5, updated_at = $6
		 WHERE id = $1
		 RETURNING id, name, match_type, pattern, enabled, is_system, profile_id, created_at, updated_at`,
		row.ID,
		row.Name,
		row.MatchType,
		row.Pattern,
		row.Enabled,
		row.UpdatedAt,
	))
	if err != nil {
		return headerBlocklistRuleRow{}, fmt.Errorf("update header blocklist rule %d: %w", row.ID, err)
	}
	return updated, nil
}

func deleteHeaderBlocklistRule(ctx context.Context, exec queryExecutor, ruleID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM header_blocklist_rules WHERE id = $1`, ruleID); err != nil {
		return fmt.Errorf("delete header blocklist rule %d: %w", ruleID, err)
	}
	return nil
}

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

func scanHeaderBlocklistRuleRow(scanner interface{ Scan(...any) error }) (headerBlocklistRuleRow, error) {
	var profileID sql.NullInt32
	row := headerBlocklistRuleRow{}
	if err := scanner.Scan(&row.ID, &row.Name, &row.MatchType, &row.Pattern, &row.Enabled, &row.IsSystem, &profileID, &row.CreatedAt, &row.UpdatedAt); err != nil {
		return headerBlocklistRuleRow{}, err
	}
	row.ProfileID = nullableInt32(profileID)
	return row, nil
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

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func resolvedBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

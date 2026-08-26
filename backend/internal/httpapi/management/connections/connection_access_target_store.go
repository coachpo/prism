package connections

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func nextModelAccessTargetPosition(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) (int, error) {
	var maxPosition sql.NullInt32
	if err := exec.QueryRow(ctx, `SELECT MAX(position) FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2`, profileID, modelConfigID).Scan(&maxPosition); err != nil {
		return 0, fmt.Errorf("query next access target position for model %d: %w", modelConfigID, err)
	}
	if !maxPosition.Valid {
		return 0, nil
	}
	return int(maxPosition.Int32) + 1, nil
}

func loadConnectionOwnerReference(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, connectionID int, forUpdate bool) (connectionReferenceRecord, bool, error) {
	query := `SELECT model_access_targets.id, model_configs.id, model_configs.model_id, model_configs.api_family, model_access_targets.position, model_access_targets.is_enabled FROM model_access_targets JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id WHERE model_access_targets.profile_id = $1 AND model_access_targets.source_model_config_id = $2 AND model_access_targets.target_connection_id = $3`
	if forUpdate {
		query += ` FOR UPDATE OF model_access_targets`
	}
	query += ` LIMIT 1`
	record, err := scanConnectionReferenceRecord(exec.QueryRow(ctx, query, profileID, modelConfigID, connectionID))
	if err == pgx.ErrNoRows {
		return connectionReferenceRecord{}, false, nil
	}
	if err != nil {
		return connectionReferenceRecord{}, false, fmt.Errorf("load owner target for model %d connection %d: %w", modelConfigID, connectionID, err)
	}
	return record, true, nil
}

func countEnabledModelAccessTargetsExcluding(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, excludeTargetID int) (int, error) {
	var count int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 AND is_enabled = TRUE AND id <> $3`, profileID, modelConfigID, excludeTargetID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count enabled access targets for model %d: %w", modelConfigID, err)
	}
	return count, nil
}

func compactModelAccessTargetPositions(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, currentTime time.Time) error {
	_, err := exec.Exec(ctx, `UPDATE model_access_targets AS target SET position = ordered.new_position, updated_at = $3 FROM (SELECT id, (ROW_NUMBER() OVER (ORDER BY position ASC, id ASC) - 1)::integer AS new_position FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2) AS ordered WHERE target.id = ordered.id AND target.position <> ordered.new_position`, profileID, modelConfigID, currentTime)
	if err != nil {
		return fmt.Errorf("compact access target positions for model %d: %w", modelConfigID, err)
	}
	return nil
}

func lockProfileAccessTargetRows(ctx context.Context, tx pgx.Tx, profileID int) error {
	rows, err := tx.Query(ctx, `SELECT id FROM model_access_targets WHERE profile_id = $1 FOR UPDATE`, profileID)
	if err != nil {
		return fmt.Errorf("lock access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate access target locks: %w", err)
	}
	return nil
}

func insertOwnerTerminalTargetAccess(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, terminalTargetID int, position int, currentTime time.Time) error {
	if _, err := exec.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, TRUE, $5, $5)`, profileID, modelConfigID, terminalTargetID, position, currentTime); err != nil {
		return fmt.Errorf("insert owner terminal target access for model %d terminal target %d: %w", modelConfigID, terminalTargetID, err)
	}
	return nil
}

func insertOwnerTerminalTargetAccessReturningID(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, terminalTargetID int, position int, currentTime time.Time) (int, error) {
	return insertOwnerTerminalTargetAccessWithEnabledReturningID(ctx, exec, profileID, modelConfigID, terminalTargetID, position, true, currentTime)
}

func insertOwnerTerminalTargetAccessWithEnabledReturningID(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, terminalTargetID int, position int, enabled bool, currentTime time.Time) (int, error) {
	var accessTargetID int
	if err := exec.QueryRow(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6) RETURNING id`, profileID, modelConfigID, terminalTargetID, position, enabled, currentTime).Scan(&accessTargetID); err != nil {
		return 0, fmt.Errorf("insert owner terminal target access for model %d terminal target %d: %w", modelConfigID, terminalTargetID, err)
	}
	return accessTargetID, nil
}

func loadOwnerMutationAccessTargets(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int) ([]connectionMutationAccessTarget, error) {
	rows, err := exec.Query(ctx, `SELECT id, target_type, target_connection_id, position, is_enabled FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 ORDER BY position ASC, id ASC`, profileID, modelConfigID)
	if err != nil {
		return nil, fmt.Errorf("query owner mutation access targets for model %d: %w", modelConfigID, err)
	}
	defer rows.Close()
	items := make([]connectionMutationAccessTarget, 0)
	for rows.Next() {
		var item connectionMutationAccessTarget
		var terminalTargetID sql.NullInt32
		if err := rows.Scan(&item.ID, &item.TargetType, &terminalTargetID, &item.Position, &item.IsEnabled); err != nil {
			return nil, fmt.Errorf("scan owner mutation access target for model %d: %w", modelConfigID, err)
		}
		if terminalTargetID.Valid {
			resolved := int(terminalTargetID.Int32)
			item.ConnectionID = &resolved
			item.TerminalTargetID = &resolved
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owner mutation access targets for model %d: %w", modelConfigID, err)
	}
	return items, nil
}

func lockModelAccessTargetRows(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int) error {
	_, err := tx.Exec(ctx, `SELECT id FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 ORDER BY position ASC, id ASC FOR UPDATE`, profileID, modelConfigID)
	if err != nil {
		return fmt.Errorf("lock access target rows for model %d: %w", modelConfigID, err)
	}
	return nil
}

func deleteModelAccessTargetRow(ctx context.Context, exec queryExecutor, targetID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM model_access_targets WHERE id = $1`, targetID); err != nil {
		return fmt.Errorf("delete model access target %d: %w", targetID, err)
	}
	return nil
}

package models

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func replaceAccessTargetsPreservingConnections(ctx context.Context, tx pgx.Tx, sourceProfileID int, sourceModelConfigID int, targets []resolvedAccessTarget, preservedConnectionTargets []preservedConnectionAccessTarget, currentTime time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_access_targets WHERE source_model_config_id = $1 AND target_model_config_id IS NOT NULL`, sourceModelConfigID); err != nil {
		return fmt.Errorf("delete access targets for model %d: %w", sourceModelConfigID, err)
	}
	for _, target := range sortPreservedConnectionAccessTargetsByPosition(preservedConnectionTargets) {
		if !target.Update {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE model_access_targets SET position = $3, is_enabled = $4, updated_at = $5 WHERE id = $1 AND source_model_config_id = $2 AND target_connection_id IS NOT NULL`, target.ID, sourceModelConfigID, target.Position, target.IsEnabled, currentTime); err != nil {
			return fmt.Errorf("update preserved connection access target %d for model %d: %w", target.ID, sourceModelConfigID, err)
		}
	}
	for _, target := range sortResolvedAccessTargetsByPosition(targets) {
		if target.TargetType == "model" {
			if target.Model == nil {
				return fmt.Errorf("replace access targets for model %d: missing model target", sourceModelConfigID)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, $5, $6, $6)`, sourceProfileID, sourceModelConfigID, target.Model.ID, target.Position, target.IsEnabled, currentTime); err != nil {
				return mapAccessTargetWriteError(err, sourceModelConfigID)
			}
			continue
		}
		if target.Connection == nil {
			return fmt.Errorf("replace access targets for model %d: missing connection target", sourceModelConfigID)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, sourceProfileID, sourceModelConfigID, target.Connection.ID, target.Position, target.IsEnabled, currentTime); err != nil {
			return mapAccessTargetWriteError(err, sourceModelConfigID)
		}
	}
	return nil
}

func mapAccessTargetWriteError(err error, sourceModelConfigID int) error {
	if isUniqueViolation(err, "uq_model_access_targets_source_target_model") || isUniqueViolation(err, "uq_model_access_targets_source_target_connection") {
		return &domainError{StatusCode: 400, Detail: "access_targets must contain unique target references"}
	}
	if isUniqueViolation(err, "uq_model_access_targets_source_position") {
		return &domainError{StatusCode: 400, Detail: "access_targets must contain unique position values"}
	}
	return fmt.Errorf("insert access target for model %d: %w", sourceModelConfigID, err)
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

// managementGraphMaxDepth mirrors the runtime planner's resolver depth limit
// so graph mutations reject over-deep graphs before they can be persisted.
const managementGraphMaxDepth = 32

func deleteSourceAccessTargets(ctx context.Context, tx pgx.Tx, sourceModelConfigID int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM model_access_targets WHERE source_model_config_id = $1`, sourceModelConfigID); err != nil {
		return fmt.Errorf("delete source access targets for model %d: %w", sourceModelConfigID, err)
	}
	return nil
}

func deleteSourceAccessTargetsAndOwnedConnections(ctx context.Context, tx pgx.Tx, profileID int, sourceModelConfigID int) error {
	rows, err := tx.Query(ctx, `SELECT target_connection_id FROM model_access_targets WHERE profile_id = $1 AND source_model_config_id = $2 AND target_connection_id IS NOT NULL ORDER BY target_connection_id ASC FOR UPDATE`, profileID, sourceModelConfigID)
	if err != nil {
		return fmt.Errorf("query owned connections for model %d: %w", sourceModelConfigID, err)
	}
	connectionIDs := make([]int, 0)
	for rows.Next() {
		var connectionID int
		if err := rows.Scan(&connectionID); err != nil {
			rows.Close()
			return fmt.Errorf("scan owned connection for model %d: %w", sourceModelConfigID, err)
		}
		connectionIDs = append(connectionIDs, connectionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate owned connections for model %d: %w", sourceModelConfigID, err)
	}
	rows.Close()
	if err := deleteSourceAccessTargets(ctx, tx, sourceModelConfigID); err != nil {
		return err
	}
	for _, connectionID := range connectionIDs {
		if err := deleteConnectionRowForProfile(ctx, tx, profileID, connectionID); err != nil {
			return err
		}
	}
	return nil
}

func deleteConnectionRowForProfile(ctx context.Context, exec queryExecutor, profileID int, connectionID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connections WHERE profile_id = $1 AND id = $2`, profileID, connectionID); err != nil {
		return fmt.Errorf("delete connection %d for profile %d: %w", connectionID, profileID, err)
	}
	return nil
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

func updateAccessTargetMetadata(ctx context.Context, exec queryExecutor, profileID int, modelConfigID int, items []accessTargetMutationItem, currentTime time.Time) error {
	for _, item := range items {
		enabled := true
		if item.Request.IsEnabled != nil {
			enabled = *item.Request.IsEnabled
		}
		commandTag, err := exec.Exec(ctx, `UPDATE model_access_targets SET position = $4, is_enabled = $5, updated_at = $6 WHERE profile_id = $1 AND source_model_config_id = $2 AND id = $3`, profileID, modelConfigID, item.ID, item.Request.Position, enabled, currentTime)
		if err != nil {
			return fmt.Errorf("update model access target %d metadata: %w", item.ID, err)
		}
		if commandTag.RowsAffected() == 0 {
			return &domainError{StatusCode: 404, Detail: "Model access target not found"}
		}
	}
	return nil
}

func lockConnectionRow(ctx context.Context, tx pgx.Tx, profileID int, connectionID int) error {
	var existingID int
	err := tx.QueryRow(ctx, `SELECT id FROM connections WHERE profile_id = $1 AND id = $2 FOR UPDATE`, profileID, connectionID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return &domainError{StatusCode: 404, Detail: "Connection not found"}
	}
	if err != nil {
		return fmt.Errorf("lock connection %d for profile %d: %w", connectionID, profileID, err)
	}
	return nil
}

func deleteModelAccessTargetRow(ctx context.Context, exec queryExecutor, targetID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM model_access_targets WHERE id = $1`, targetID); err != nil {
		return fmt.Errorf("delete model access target %d: %w", targetID, err)
	}
	return nil
}

func deleteConnectionRow(ctx context.Context, exec queryExecutor, connectionID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM connections WHERE id = $1`, connectionID); err != nil {
		return fmt.Errorf("delete connection %d: %w", connectionID, err)
	}
	return nil
}

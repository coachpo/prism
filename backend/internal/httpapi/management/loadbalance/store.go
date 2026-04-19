package loadbalance

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

func listStrategyRows(ctx context.Context, exec queryExecutor, profileID int) ([]strategyRow, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT loadbalance_strategies.id,
			loadbalance_strategies.profile_id,
			loadbalance_strategies.name,
			loadbalance_strategies.strategy_type,
			loadbalance_strategies.legacy_strategy_type,
			loadbalance_strategies.auto_recovery,
			loadbalance_strategies.routing_policy,
			loadbalance_strategies.created_at,
			loadbalance_strategies.updated_at,
			COUNT(model_configs.id) AS attached_model_count
		FROM loadbalance_strategies
		LEFT JOIN model_configs
		  ON model_configs.profile_id = loadbalance_strategies.profile_id
		 AND model_configs.loadbalance_strategy_id = loadbalance_strategies.id
		WHERE loadbalance_strategies.profile_id = $1
		GROUP BY loadbalance_strategies.id
		ORDER BY loadbalance_strategies.updated_at DESC, loadbalance_strategies.id DESC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query loadbalance strategies for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]strategyRow, 0)
	for rows.Next() {
		item, scanErr := scanStrategyRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate loadbalance strategies for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadStrategyRow(ctx context.Context, exec queryExecutor, profileID int, strategyID int, forUpdate bool) (strategyRow, bool, error) {
	query := `SELECT id, profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at, 0 AS attached_model_count
		FROM loadbalance_strategies
		WHERE profile_id = $1 AND id = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	item, err := scanStrategyRow(exec.QueryRow(ctx, query, profileID, strategyID))
	if err == pgx.ErrNoRows {
		return strategyRow{}, false, nil
	}
	if err != nil {
		return strategyRow{}, false, fmt.Errorf("load loadbalance strategy %d for profile %d: %w", strategyID, profileID, err)
	}
	count, err := countAttachedModels(ctx, exec, profileID, strategyID)
	if err != nil {
		return strategyRow{}, false, err
	}
	item.AttachedModelCount = count
	return item, true, nil
}

func countAttachedModels(ctx context.Context, exec queryExecutor, profileID int, strategyID int) (int, error) {
	var count int
	if err := exec.QueryRow(
		ctx,
		`SELECT COUNT(id) FROM model_configs WHERE profile_id = $1 AND loadbalance_strategy_id = $2`,
		profileID,
		strategyID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count attached models for strategy %d: %w", strategyID, err)
	}
	return count, nil
}

func strategyNameExists(ctx context.Context, exec queryExecutor, profileID int, name string, excludeID *int) (bool, error) {
	query := `SELECT id FROM loadbalance_strategies WHERE profile_id = $1 AND name = $2`
	args := []any{profileID, name}
	if excludeID != nil {
		query += ` AND id != $3`
		args = append(args, *excludeID)
	}
	query += ` LIMIT 1`
	var existingID int
	err := exec.QueryRow(ctx, query, args...).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query loadbalance strategy name %q: %w", name, err)
	}
	return true, nil
}

func insertStrategy(ctx context.Context, exec queryExecutor, profileID int, payload strategyPersistedPayload, currentTime time.Time) (strategyRow, error) {
	item, err := scanStrategyRow(exec.QueryRow(
		ctx,
		`INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8)
		 RETURNING id, profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at, 0 AS attached_model_count`,
		profileID,
		payload.Name,
		payload.StrategyType,
		nullableString(payload.LegacyStrategyType),
		nullableJSON(payload.AutoRecoveryJSON),
		nullableJSON(payload.RoutingPolicyJSON),
		currentTime,
		currentTime,
	))
	if err != nil {
		return strategyRow{}, fmt.Errorf("insert loadbalance strategy %q: %w", payload.Name, err)
	}
	return item, nil
}

func updateStrategy(ctx context.Context, exec queryExecutor, strategyID int, payload strategyPersistedPayload, currentTime time.Time) (strategyRow, error) {
	item, err := scanStrategyRow(exec.QueryRow(
		ctx,
		`UPDATE loadbalance_strategies
		 SET name = $2,
		     strategy_type = $3,
		     legacy_strategy_type = $4,
		     auto_recovery = $5::jsonb,
		     routing_policy = $6::jsonb,
		     updated_at = $7
		 WHERE id = $1
		 RETURNING id, profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at, 0 AS attached_model_count`,
		strategyID,
		payload.Name,
		payload.StrategyType,
		nullableString(payload.LegacyStrategyType),
		nullableJSON(payload.AutoRecoveryJSON),
		nullableJSON(payload.RoutingPolicyJSON),
		currentTime,
	))
	if err != nil {
		return strategyRow{}, fmt.Errorf("update loadbalance strategy %d: %w", strategyID, err)
	}
	return item, nil
}

func deleteStrategy(ctx context.Context, exec queryExecutor, strategyID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM loadbalance_strategies WHERE id = $1`, strategyID); err != nil {
		return fmt.Errorf("delete loadbalance strategy %d: %w", strategyID, err)
	}
	return nil
}

func clearStrategyState(ctx context.Context, exec queryExecutor, profileID int, strategyID int) error {
	if _, err := exec.Exec(
		ctx,
		`DELETE FROM routing_connection_runtime_state
		 WHERE profile_id = $1
		   AND connection_id IN (
		     SELECT connections.id
		     FROM connections
		     JOIN model_configs ON model_configs.id = connections.model_config_id
		     WHERE connections.profile_id = $1
		       AND model_configs.profile_id = $1
		       AND model_configs.loadbalance_strategy_id = $2
		   )`,
		profileID,
		strategyID,
	); err != nil {
		return fmt.Errorf("clear runtime state for strategy %d: %w", strategyID, err)
	}
	if _, err := exec.Exec(
		ctx,
		`DELETE FROM loadbalance_round_robin_state
		 WHERE profile_id = $1
		   AND model_config_id IN (
		     SELECT id FROM model_configs WHERE profile_id = $1 AND loadbalance_strategy_id = $2
		   )`,
		profileID,
		strategyID,
	); err != nil {
		return fmt.Errorf("clear round robin state for strategy %d: %w", strategyID, err)
	}
	return nil
}

func scanStrategyRow(scanner interface{ Scan(...any) error }) (strategyRow, error) {
	var legacyStrategyType sql.NullString
	var autoRecoveryRaw []byte
	var routingPolicyRaw []byte
	var attachedModelCount sql.NullInt64
	item := strategyRow{}
	if err := scanner.Scan(&item.ID, &item.ProfileID, &item.Name, &item.StrategyType, &legacyStrategyType, &autoRecoveryRaw, &routingPolicyRaw, &item.CreatedAt, &item.UpdatedAt, &attachedModelCount); err != nil {
		return strategyRow{}, err
	}
	item.LegacyStrategyType = nullableStringValue(legacyStrategyType)
	item.AutoRecoveryRaw = cloneBytes(autoRecoveryRaw)
	item.RoutingPolicyRaw = cloneBytes(routingPolicyRaw)
	if attachedModelCount.Valid {
		item.AttachedModelCount = int(attachedModelCount.Int64)
	}
	return item, nil
}

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}

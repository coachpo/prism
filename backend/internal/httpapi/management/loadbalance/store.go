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
			loadbalance_strategies.legacy_strategy_type,
			loadbalance_strategies.failure_status_codes,
			loadbalance_strategies.ban_mode,
			loadbalance_strategies.retry_base_delay_ms,
			loadbalance_strategies.retry_backoff_multiplier,
			loadbalance_strategies.retry_jitter_ratio,
			loadbalance_strategies.retry_max_delay_ms,
			loadbalance_strategies.retry_max_attempts,
			loadbalance_strategies.ban_duration_seconds,
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
	query := `SELECT id, profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, retry_max_attempts, ban_duration_seconds,
			created_at, updated_at, 0 AS attached_model_count
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
		`INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, retry_max_attempts, ban_duration_seconds, created_at, updated_at)
		 VALUES ($1, $2, $3, $4::integer[], $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id, profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, retry_max_attempts, ban_duration_seconds,
			created_at, updated_at, 0 AS attached_model_count`,
		profileID,
		payload.Name,
		payload.LegacyStrategyType,
		int32ArrayArg(payload.FailureStatusCodes),
		payload.BanMode,
		payload.RetryBaseDelayMS,
		payload.RetryBackoffMultiplier,
		payload.RetryJitterRatio,
		payload.RetryMaxDelayMS,
		payload.RetryMaxAttempts,
		payload.BanDurationSeconds,
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
		     legacy_strategy_type = $3,
		     failure_status_codes = $4::integer[],
		     ban_mode = $5,
		     retry_base_delay_ms = $6,
		     retry_backoff_multiplier = $7,
		     retry_jitter_ratio = $8,
		     retry_max_delay_ms = $9,
		     retry_max_attempts = $10,
		     ban_duration_seconds = $11,
		     updated_at = $12
		 WHERE id = $1
		 RETURNING id, profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, retry_max_attempts, ban_duration_seconds,
			created_at, updated_at, 0 AS attached_model_count`,
		strategyID,
		payload.Name,
		payload.LegacyStrategyType,
		int32ArrayArg(payload.FailureStatusCodes),
		payload.BanMode,
		payload.RetryBaseDelayMS,
		payload.RetryBackoffMultiplier,
		payload.RetryJitterRatio,
		payload.RetryMaxDelayMS,
		payload.RetryMaxAttempts,
		payload.BanDurationSeconds,
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

func scanStrategyRow(scanner interface{ Scan(...any) error }) (strategyRow, error) {
	var failureStatusCodes []int32
	var attachedModelCount sql.NullInt64
	item := strategyRow{}
	if err := scanner.Scan(
		&item.ID,
		&item.ProfileID,
		&item.Name,
		&item.LegacyStrategyType,
		&failureStatusCodes,
		&item.BanMode,
		&item.RetryBaseDelayMS,
		&item.RetryBackoffMultiplier,
		&item.RetryJitterRatio,
		&item.RetryMaxDelayMS,
		&item.RetryMaxAttempts,
		&item.BanDurationSeconds,
		&item.CreatedAt,
		&item.UpdatedAt,
		&attachedModelCount,
	); err != nil {
		return strategyRow{}, err
	}
	item.FailureStatusCodes = intSliceFromInt32(failureStatusCodes)
	if attachedModelCount.Valid {
		item.AttachedModelCount = int(attachedModelCount.Int64)
	}
	return item, nil
}

func int32ArrayArg(values []int) []int32 {
	items := make([]int32, 0, len(values))
	for _, value := range values {
		items = append(items, int32(value))
	}
	return items
}

func intSliceFromInt32(values []int32) []int {
	items := make([]int, 0, len(values))
	for _, value := range values {
		items = append(items, int(value))
	}
	return items
}

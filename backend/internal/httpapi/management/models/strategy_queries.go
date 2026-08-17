package models

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func listModelHealthStats(ctx context.Context, exec queryExecutor, profileID int) (map[string]modelHealthStats, error) {
	// Model health aggregates retained finalized ingress events (Observe SPEC
	// §3.2 shared classifier; Requests SPEC §6.9): total_requests counts each
	// finalized ingress once; success_count only counts final_result=completed
	// so retry/Hedge/failover never inflate model health.
	rows, err := exec.Query(ctx, `SELECT model_id, COUNT(*) AS total_requests, COALESCE(SUM(CASE WHEN (status_code BETWEEN 200 AND 299 AND stream_outcome IN ('not_streaming','completed')) THEN 1 ELSE 0 END), 0) AS success_count FROM usage_request_events WHERE profile_id = $1 GROUP BY model_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query model health stats for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	stats := map[string]modelHealthStats{}
	for rows.Next() {
		var modelID string
		var totalRequests int
		var successCount int
		if err := rows.Scan(&modelID, &totalRequests, &successCount); err != nil {
			return nil, fmt.Errorf("scan model health stats: %w", err)
		}
		var successRate *float64
		if totalRequests > 0 {
			rate := float64(successCount) / float64(totalRequests) * 100
			rate = float64(int(rate*100+0.5)) / 100
			successRate = &rate
		}
		stats[modelID] = modelHealthStats{SuccessRate: successRate, TotalRequests: totalRequests}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model health stats: %w", err)
	}
	return stats, nil
}

func loadStrategyRecordsByIDs(ctx context.Context, exec queryExecutor, profileID int, strategyIDs []int) (map[int]strategyRecord, error) {
	if len(strategyIDs) == 0 {
		return map[int]strategyRecord{}, nil
	}
	args := []any{profileID, int32ArrayArg(strategyIDs)}
	query := `SELECT id, name, legacy_strategy_type, is_default, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds FROM loadbalance_strategies WHERE profile_id = $1 AND id = ANY($2) ORDER BY id ASC`
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query strategies by id for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := map[int]strategyRecord{}
	for rows.Next() {
		record, scanErr := scanStrategyRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items[record.ID] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate strategies by id: %w", err)
	}
	return items, nil
}

func ensureLoadbalanceStrategyExists(ctx context.Context, exec queryExecutor, profileID int, strategyID int) error {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM loadbalance_strategies WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, strategyID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return &domainError{StatusCode: 400, Detail: "Loadbalance strategy not found"}
	}
	if err != nil {
		return fmt.Errorf("load strategy %d for profile %d: %w", strategyID, profileID, err)
	}
	return nil
}

func scanStrategyRecord(scanner interface{ Scan(...any) error }) (strategyRecord, error) {
	var failureStatusCodes []int32
	record := strategyRecord{}
	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.LegacyStrategyType,
		&record.IsDefault,
		&failureStatusCodes,
		&record.BanMode,
		&record.RetryBaseDelayMS,
		&record.RetryBackoffMultiplier,
		&record.RetryJitterRatio,
		&record.RetryMaxDelayMS,
		&record.CycleRetryAttemptLimit,
		&record.BanCumulativeRetryAttemptThreshold,
		&record.BanDurationSeconds,
	); err != nil {
		return strategyRecord{}, err
	}
	record.FailureStatusCodes = intSliceFromInt32(failureStatusCodes)
	return record, nil
}

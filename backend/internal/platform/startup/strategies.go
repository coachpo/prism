package startup

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// seedStrategyDefaults creates the canonical built-in strategy rows for any
// profile that has no strategies at all (new profiles), with the canonical
// fill-first row as the explicit default, and then verifies the steady-state
// invariant: every non-deleted profile has a non-empty strategy set and exactly
// one default. It MUST NOT re-run canonical completeness repair: editing or
// renaming a canonical row is legitimate authoring that may leave canonical
// completeness missing, and ordinary restarts must still succeed.
func (s Service) seedStrategyDefaults(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		profileIDs, err := loadNonDeletedProfileIDs(ctx, tx)
		if err != nil {
			return err
		}
		now := s.timestamp()
		for _, profileID := range profileIDs {
			var strategyCount int
			if err := tx.QueryRow(ctx, `SELECT COUNT(id) FROM loadbalance_strategies WHERE profile_id = $1`, profileID).Scan(&strategyCount); err != nil {
				return fmt.Errorf("count loadbalance strategies for profile %d: %w", profileID, err)
			}
			if strategyCount > 0 {
				continue
			}
			for _, spec := range loadbalancedomain.CanonicalDefaultStrategySpecs() {
				payload := loadbalancedomain.DefaultStrategyPayload(spec)
				isDefault := spec.LegacyStrategyType == "fill-first"
				legacyStrategyType := *payload.LegacyStrategyType
				failureStatusCodes := make([]int32, 0, len(payload.FailureStatusCodes))
				for _, code := range payload.FailureStatusCodes {
					failureStatusCodes = append(failureStatusCodes, int32(code))
				}
				if _, err := tx.Exec(
					ctx,
					`INSERT INTO loadbalance_strategies (
						profile_id, name, legacy_strategy_type, is_default, failure_status_codes, ban_mode,
						retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
						retry_max_delay_ms, cycle_retry_attempt_limit,
						ban_cumulative_retry_attempt_threshold, ban_duration_seconds,
						created_at, updated_at
					) VALUES ($1, $2, $3, $4, $5::integer[], $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
					profileID,
					payload.Name,
					legacyStrategyType,
					isDefault,
					failureStatusCodes,
					payload.BanMode,
					payload.RetryBaseDelayMS,
					payload.RetryBackoffMultiplier,
					payload.RetryJitterRatio,
					payload.RetryMaxDelayMS,
					payload.CycleRetryAttemptLimit,
					payload.BanCumulativeRetryAttemptThreshold,
					payload.BanDurationSeconds,
					now,
					now,
				); err != nil {
					return fmt.Errorf("seed canonical loadbalance strategy %q for profile %d: %w", payload.Name, profileID, err)
				}
			}
		}

		for _, profileID := range profileIDs {
			var strategyCount int
			var defaultCount int
			if err := tx.QueryRow(
				ctx,
				`SELECT COUNT(id), COUNT(*) FILTER (WHERE is_default)
				 FROM loadbalance_strategies
				 WHERE profile_id = $1`,
				profileID,
			).Scan(&strategyCount, &defaultCount); err != nil {
				return fmt.Errorf("verify loadbalance strategy default invariant for profile %d: %w", profileID, err)
			}
			if strategyCount < 1 || defaultCount != 1 {
				return fmt.Errorf("loadbalance strategy default invariant violated for profile %d: strategies=%d defaults=%d; the strategy set must be non-empty with exactly one is_default row", profileID, strategyCount, defaultCount)
			}
		}
		return nil
	})
}

func loadNonDeletedProfileIDs(ctx context.Context, exec queryExecutor) ([]int, error) {
	rows, err := exec.Query(ctx, `SELECT id FROM profiles WHERE deleted_at IS NULL ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query non-deleted profiles for strategy defaults: %w", err)
	}
	defer rows.Close()
	profileIDs := []int{}
	for rows.Next() {
		var profileID int
		if err := rows.Scan(&profileID); err != nil {
			return nil, fmt.Errorf("scan profile id for strategy defaults: %w", err)
		}
		profileIDs = append(profileIDs, profileID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profile ids for strategy defaults: %w", err)
	}
	return profileIDs, nil
}

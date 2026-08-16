package loadbalance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type RuntimeStrategy struct {
	ID                                 int
	Name                               string
	LegacyStrategyType                 *string
	FailureStatusCodes                 []int
	BanMode                            string
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
}

type ConnectionOrderCandidate struct {
	ID       int
	Priority int
}

var defaultRuntimeFailoverStatusCodes = []int{403, 422, 429, 500, 502, 503, 504, 529}

type runtimeFeedbackPolicy struct {
	Enabled                            bool
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BaseDelayMS                        int
	BackoffMultiplier                  float64
	JitterRatio                        float64
	MaxDelayMS                         int
	BanMode                            string
	BanDurationSeconds                 int
}

type runtimeAdmissionPolicy struct {
	RespectQPSLimit       bool
	RespectInFlightLimits bool
}

type RuntimeHedgePolicy struct {
	Enabled               bool
	Delay                 time.Duration
	MaxAdditionalAttempts int
}

func LoadRuntimeStrategy(ctx context.Context, exec queryExecutor, profileID int, strategyID int) (RuntimeStrategy, bool, error) {
	record := RuntimeStrategy{}
	var legacyStrategyType string
	var failureStatusCodes []int32
	err := exec.QueryRow(
		ctx,
		`SELECT id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds
		FROM loadbalance_strategies
		WHERE profile_id = $1 AND id = $2
		LIMIT 1`,
		profileID,
		strategyID,
	).Scan(
		&record.ID,
		&record.Name,
		&legacyStrategyType,
		&failureStatusCodes,
		&record.BanMode,
		&record.RetryBaseDelayMS,
		&record.RetryBackoffMultiplier,
		&record.RetryJitterRatio,
		&record.RetryMaxDelayMS,
		&record.CycleRetryAttemptLimit,
		&record.BanCumulativeRetryAttemptThreshold,
		&record.BanDurationSeconds,
	)
	if err == pgx.ErrNoRows {
		return RuntimeStrategy{}, false, nil
	}
	if err != nil {
		return RuntimeStrategy{}, false, fmt.Errorf("load runtime strategy %d for profile %d: %w", strategyID, profileID, err)
	}
	record.LegacyStrategyType = &legacyStrategyType
	record.FailureStatusCodes = intSliceFromInt32(failureStatusCodes)
	return record, true, nil
}

func OrderConnectionIDs(profileID int, modelConfigID int, strategy RuntimeStrategy, connections []ConnectionOrderCandidate, states map[int]RuntimeConnectionState, roundRobin RuntimeRoundRobinCursorSource, nowAt time.Time) ([]int, error) {
	ordered := append([]ConnectionOrderCandidate(nil), connections...)
	sort.Slice(ordered, func(left int, right int) bool {
		return compareStableCandidates(ordered[left], ordered[right]) < 0
	})
	if isRoundRobinStrategy(strategy) && len(ordered) >= 2 && roundRobin != nil {
		cursor := roundRobin.ClaimRoundRobinCursor(profileID, modelConfigID, len(ordered))
		if cursor != 0 {
			ordered = append(ordered[cursor:], ordered[:cursor]...)
		}
	}
	orderedIDs := extractConnectionIDs(ordered)
	if isSingleStrategy(strategy) && len(orderedIDs) > 1 {
		return orderedIDs[:1], nil
	}
	return orderedIDs, nil
}

func compareStableCandidates(left ConnectionOrderCandidate, right ConnectionOrderCandidate) int {
	if left.Priority != right.Priority {
		if left.Priority < right.Priority {
			return -1
		}
		return 1
	}
	if left.ID < right.ID {
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	return 0
}

func (strategy RuntimeStrategy) FailoverStatusCodes() []int {
	if len(strategy.FailureStatusCodes) == 0 {
		return append([]int(nil), defaultRuntimeFailoverStatusCodes...)
	}
	return append([]int(nil), strategy.FailureStatusCodes...)
}

func (strategy RuntimeStrategy) FeedbackPolicy() runtimeFeedbackPolicy {
	return runtimeFeedbackPolicy{
		Enabled:                            true,
		CycleRetryAttemptLimit:             maxInt(strategy.CycleRetryAttemptLimit, 1),
		BanCumulativeRetryAttemptThreshold: maxInt(strategy.BanCumulativeRetryAttemptThreshold, 0),
		BaseDelayMS:                        maxInt(strategy.RetryBaseDelayMS, 0),
		BackoffMultiplier:                  maxFloat(strategy.RetryBackoffMultiplier, 1),
		JitterRatio:                        clampFloat(strategy.RetryJitterRatio, 0, 1),
		MaxDelayMS:                         maxInt(strategy.RetryMaxDelayMS, 0),
		BanMode:                            normalizeBanMode(strategy.BanMode),
		BanDurationSeconds:                 maxInt(strategy.BanDurationSeconds, 0),
	}
}

func (strategy RuntimeStrategy) AdmissionPolicy() runtimeAdmissionPolicy {
	return runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}
}

func (strategy RuntimeStrategy) HedgePolicy() RuntimeHedgePolicy {
	return RuntimeHedgePolicy{}
}

func maxInt(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func maxFloat(value float64, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func clampFloat(value float64, minimum float64, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func normalizeBanMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "temporary", "until_reset":
		return normalized
	default:
		return "off"
	}
}

func intSliceFromInt32(values []int32) []int {
	items := make([]int, 0, len(values))
	for _, value := range values {
		items = append(items, int(value))
	}
	return items
}

func isRoundRobinStrategy(strategy RuntimeStrategy) bool {
	return isLegacyStrategyType(strategy, "round-robin")
}

func isSingleStrategy(strategy RuntimeStrategy) bool {
	return isLegacyStrategyType(strategy, "single")
}

func isLegacyStrategyType(strategy RuntimeStrategy, legacyType string) bool {
	if strategy.LegacyStrategyType == nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(*strategy.LegacyStrategyType)) == legacyType
}

func extractConnectionIDs(connections []ConnectionOrderCandidate) []int {
	ids := make([]int, 0, len(connections))
	for _, connection := range connections {
		ids = append(ids, connection.ID)
	}
	return ids
}

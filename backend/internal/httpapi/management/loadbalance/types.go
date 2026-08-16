package loadbalance

import (
	"time"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

type loadbalanceStrategyResponse struct {
	ID                                 int       `json:"id"`
	ProfileID                          int       `json:"profile_id"`
	Name                               string    `json:"name"`
	LegacyStrategyType                 string    `json:"legacy_strategy_type"`
	IsDefault                          bool      `json:"is_default"`
	FailureStatusCodes                 []int     `json:"failure_status_codes"`
	BanMode                            string    `json:"ban_mode"`
	RetryBaseDelayMS                   int       `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier             float64   `json:"retry_backoff_multiplier"`
	RetryJitterRatio                   float64   `json:"retry_jitter_ratio"`
	RetryMaxDelayMS                    int       `json:"retry_max_delay_ms"`
	CycleRetryAttemptLimit             int       `json:"cycle_retry_attempt_limit"`
	BanCumulativeRetryAttemptThreshold int       `json:"ban_cumulative_retry_attempt_threshold"`
	BanDurationSeconds                 int       `json:"ban_duration_seconds"`
	AttachedModelCount                 int       `json:"attached_model_count"`
	CreatedAt                          time.Time `json:"created_at"`
	UpdatedAt                          time.Time `json:"updated_at"`
}

type loadbalanceStrategyCanonicalResult struct {
	CanonicalName string `json:"canonical_name"`
	StrategyID    int    `json:"strategy_id"`
}

type loadbalanceStrategyDefaultsResponse struct {
	Created           []loadbalanceStrategyCanonicalResult `json:"created"`
	Existing          []loadbalanceStrategyCanonicalResult `json:"existing"`
	DefaultStrategyID *int                                 `json:"default_strategy_id"`
	DefaultChanged    bool                                 `json:"default_changed"`
	Complete          bool                                 `json:"complete"`
}

type strategyDefaultMutationResponse struct {
	DefaultStrategyID         int  `json:"default_strategy_id"`
	PreviousDefaultStrategyID *int `json:"previous_default_strategy_id"`
	Changed                   bool `json:"changed"`
}

type strategyImpactModelItem struct {
	ModelConfigID int    `json:"model_config_id"`
	ModelID       string `json:"model_id"`
	DisplayName   string `json:"display_name"`
	IsEnabled     bool   `json:"is_enabled"`
}

type strategyImpactListResponse struct {
	StrategyID         int                       `json:"strategy_id"`
	AttachedModelCount int                       `json:"attached_model_count"`
	Items              []strategyImpactModelItem `json:"items"`
	HasMore            bool                      `json:"has_more"`
	NextCursor         *string                   `json:"next_cursor"`
}

type strategyPolicyFieldsResponse struct {
	Name                               string  `json:"name,omitempty"`
	LegacyStrategyType                 string  `json:"legacy_strategy_type"`
	FailureStatusCodes                 []int   `json:"failure_status_codes"`
	BanMode                            string  `json:"ban_mode"`
	RetryBaseDelayMS                   int     `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier             float64 `json:"retry_backoff_multiplier"`
	RetryJitterRatio                   float64 `json:"retry_jitter_ratio"`
	RetryMaxDelayMS                    int     `json:"retry_max_delay_ms"`
	CycleRetryAttemptLimit             int     `json:"cycle_retry_attempt_limit"`
	BanCumulativeRetryAttemptThreshold int     `json:"ban_cumulative_retry_attempt_threshold"`
	BanDurationSeconds                 int     `json:"ban_duration_seconds"`
}

type strategyPreviewResponse struct {
	NormalizedPolicy strategyPolicyFieldsResponse `json:"normalized_policy"`
	loadbalancedomain.RetryPreviewResult
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}

type loadbalanceStrategyRequest struct {
	Name                               string   `json:"name"`
	LegacyStrategyType                 *string  `json:"legacy_strategy_type"`
	FailureStatusCodes                 []int    `json:"failure_status_codes"`
	BanMode                            *string  `json:"ban_mode"`
	RetryBaseDelayMS                   *int     `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier             *float64 `json:"retry_backoff_multiplier"`
	RetryJitterRatio                   *float64 `json:"retry_jitter_ratio"`
	RetryMaxDelayMS                    *int     `json:"retry_max_delay_ms"`
	CycleRetryAttemptLimit             *int     `json:"cycle_retry_attempt_limit"`
	BanCumulativeRetryAttemptThreshold *int     `json:"ban_cumulative_retry_attempt_threshold"`
	BanDurationSeconds                 *int     `json:"ban_duration_seconds"`
}

type strategyPersistedPayload struct {
	Name                               string
	LegacyStrategyType                 string
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

type strategyRow struct {
	ID                                 int
	ProfileID                          int
	Name                               string
	LegacyStrategyType                 string
	IsDefault                          bool
	FailureStatusCodes                 []int
	BanMode                            string
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
	AttachedModelCount                 int
	CreatedAt                          time.Time
	UpdatedAt                          time.Time
}

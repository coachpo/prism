package loadbalance

import "time"

type loadbalanceStrategyResponse struct {
	ID                     int       `json:"id"`
	ProfileID              int       `json:"profile_id"`
	Name                   string    `json:"name"`
	LegacyStrategyType     string    `json:"legacy_strategy_type"`
	FailureStatusCodes     []int     `json:"failure_status_codes"`
	BanMode                string    `json:"ban_mode"`
	RetryBaseDelayMS       int       `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier float64   `json:"retry_backoff_multiplier"`
	RetryJitterRatio       float64   `json:"retry_jitter_ratio"`
	RetryMaxDelayMS        int       `json:"retry_max_delay_ms"`
	RetryMaxAttempts       int       `json:"retry_max_attempts"`
	BanDurationSeconds     int       `json:"ban_duration_seconds"`
	AttachedModelCount     int       `json:"attached_model_count"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type loadbalanceStrategyDefaultsResponse struct {
	Items         []loadbalanceStrategyResponse `json:"items"`
	CreatedCount  int                           `json:"created_count"`
	CreatedNames  []string                      `json:"created_names"`
	ExistingNames []string                      `json:"existing_names"`
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}

type loadbalanceStrategyRequest struct {
	Name                   string   `json:"name"`
	LegacyStrategyType     *string  `json:"legacy_strategy_type"`
	FailureStatusCodes     []int    `json:"failure_status_codes"`
	BanMode                *string  `json:"ban_mode"`
	RetryBaseDelayMS       *int     `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier *float64 `json:"retry_backoff_multiplier"`
	RetryJitterRatio       *float64 `json:"retry_jitter_ratio"`
	RetryMaxDelayMS        *int     `json:"retry_max_delay_ms"`
	RetryMaxAttempts       *int     `json:"retry_max_attempts"`
	BanDurationSeconds     *int     `json:"ban_duration_seconds"`
}

type strategyPersistedPayload struct {
	Name                   string
	LegacyStrategyType     string
	FailureStatusCodes     []int
	BanMode                string
	RetryBaseDelayMS       int
	RetryBackoffMultiplier float64
	RetryJitterRatio       float64
	RetryMaxDelayMS        int
	RetryMaxAttempts       int
	BanDurationSeconds     int
}

type strategyRow struct {
	ID                     int
	ProfileID              int
	Name                   string
	LegacyStrategyType     string
	FailureStatusCodes     []int
	BanMode                string
	RetryBaseDelayMS       int
	RetryBackoffMultiplier float64
	RetryJitterRatio       float64
	RetryMaxDelayMS        int
	RetryMaxAttempts       int
	BanDurationSeconds     int
	AttachedModelCount     int
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type canonicalDefaultStrategySpec struct {
	Name               string
	LegacyStrategyType string
}

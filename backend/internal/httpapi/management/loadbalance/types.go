package loadbalance

import "time"

type loadbalanceStrategyResponse struct {
	ID                 int                    `json:"id"`
	ProfileID          int                    `json:"profile_id"`
	Name               string                 `json:"name"`
	StrategyType       string                 `json:"strategy_type"`
	LegacyStrategyType *string                `json:"legacy_strategy_type,omitempty"`
	AutoRecovery       *autoRecoveryDocument  `json:"auto_recovery,omitempty"`
	RoutingPolicy      *routingPolicyDocument `json:"routing_policy,omitempty"`
	AttachedModelCount int                    `json:"attached_model_count"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
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
	Name               string              `json:"name"`
	StrategyType       string              `json:"strategy_type"`
	LegacyStrategyType *string             `json:"legacy_strategy_type"`
	AutoRecovery       *autoRecoveryInput  `json:"auto_recovery"`
	RoutingPolicy      *routingPolicyInput `json:"routing_policy"`
}

type autoRecoveryInput struct {
	Mode        *string                    `json:"mode"`
	StatusCodes []int                      `json:"status_codes"`
	Cooldown    *autoRecoveryCooldownInput `json:"cooldown"`
	Ban         *autoRecoveryBanInput      `json:"ban"`
}

type autoRecoveryCooldownInput struct {
	BaseSeconds        *int     `json:"base_seconds"`
	FailureThreshold   *int     `json:"failure_threshold"`
	BackoffMultiplier  *float64 `json:"backoff_multiplier"`
	MaxCooldownSeconds *int     `json:"max_cooldown_seconds"`
	JitterRatio        *float64 `json:"jitter_ratio"`
}

type autoRecoveryBanInput struct {
	Mode                        *string `json:"mode"`
	MaxCooldownStrikesBeforeBan *int    `json:"max_cooldown_strikes_before_ban"`
	BanDurationSeconds          *int    `json:"ban_duration_seconds"`
}

type routingPolicyInput struct {
	Kind             *string                           `json:"kind"`
	RoutingObjective *string                           `json:"routing_objective"`
	Hedge            *routingPolicyHedgeInput          `json:"hedge"`
	CircuitBreaker   *routingPolicyCircuitBreakerInput `json:"circuit_breaker"`
	Admission        *routingPolicyAdmissionInput      `json:"admission"`
}

type routingPolicyHedgeInput struct {
	Enabled               *bool `json:"enabled"`
	DelayMS               *int  `json:"delay_ms"`
	MaxAdditionalAttempts *int  `json:"max_additional_attempts"`
}

type routingPolicyCircuitBreakerInput struct {
	FailureStatusCodes      []int    `json:"failure_status_codes"`
	BaseOpenSeconds         *int     `json:"base_open_seconds"`
	FailureThreshold        *int     `json:"failure_threshold"`
	BackoffMultiplier       *float64 `json:"backoff_multiplier"`
	MaxOpenSeconds          *int     `json:"max_open_seconds"`
	JitterRatio             *float64 `json:"jitter_ratio"`
	BanMode                 *string  `json:"ban_mode"`
	MaxOpenStrikesBeforeBan *int     `json:"max_open_strikes_before_ban"`
	BanDurationSeconds      *int     `json:"ban_duration_seconds"`
}

type routingPolicyAdmissionInput struct {
	RespectQPSLimit       *bool `json:"respect_qps_limit"`
	RespectInFlightLimits *bool `json:"respect_in_flight_limits"`
}

type autoRecoveryDocument struct {
	Mode        string                        `json:"mode"`
	StatusCodes []int                         `json:"status_codes,omitempty"`
	Cooldown    *autoRecoveryCooldownDocument `json:"cooldown,omitempty"`
	Ban         *autoRecoveryBanDocument      `json:"ban,omitempty"`
}

type autoRecoveryCooldownDocument struct {
	BaseSeconds        int     `json:"base_seconds"`
	FailureThreshold   int     `json:"failure_threshold"`
	BackoffMultiplier  float64 `json:"backoff_multiplier"`
	MaxCooldownSeconds int     `json:"max_cooldown_seconds"`
	JitterRatio        float64 `json:"jitter_ratio"`
}

type autoRecoveryBanDocument struct {
	Mode                        string `json:"mode"`
	MaxCooldownStrikesBeforeBan int    `json:"max_cooldown_strikes_before_ban"`
	BanDurationSeconds          int    `json:"ban_duration_seconds"`
}

type routingPolicyDocument struct {
	Kind             string                              `json:"kind"`
	RoutingObjective string                              `json:"routing_objective"`
	Hedge            routingPolicyHedgeDocument          `json:"hedge"`
	CircuitBreaker   routingPolicyCircuitBreakerDocument `json:"circuit_breaker"`
	Admission        routingPolicyAdmissionDocument      `json:"admission"`
}

type routingPolicyHedgeDocument struct {
	Enabled               bool `json:"enabled"`
	DelayMS               int  `json:"delay_ms"`
	MaxAdditionalAttempts int  `json:"max_additional_attempts"`
}

type routingPolicyCircuitBreakerDocument struct {
	FailureStatusCodes      []int   `json:"failure_status_codes"`
	BaseOpenSeconds         int     `json:"base_open_seconds"`
	FailureThreshold        int     `json:"failure_threshold"`
	BackoffMultiplier       float64 `json:"backoff_multiplier"`
	MaxOpenSeconds          int     `json:"max_open_seconds"`
	JitterRatio             float64 `json:"jitter_ratio"`
	BanMode                 string  `json:"ban_mode"`
	MaxOpenStrikesBeforeBan int     `json:"max_open_strikes_before_ban"`
	BanDurationSeconds      int     `json:"ban_duration_seconds"`
}

type routingPolicyAdmissionDocument struct {
	RespectQPSLimit       bool `json:"respect_qps_limit"`
	RespectInFlightLimits bool `json:"respect_in_flight_limits"`
}

type strategyPersistedPayload struct {
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecoveryJSON   []byte
	RoutingPolicyJSON  []byte
	AutoRecovery       *autoRecoveryDocument
	RoutingPolicy      *routingPolicyDocument
}

type strategyRow struct {
	ID                 int
	ProfileID          int
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecoveryRaw    []byte
	RoutingPolicyRaw   []byte
	AttachedModelCount int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type canonicalDefaultStrategySpec struct {
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecovery       *autoRecoveryDocument
	RoutingPolicy      *routingPolicyDocument
}

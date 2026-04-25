package loadbalance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type RuntimeStrategy struct {
	ID                 int
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecoveryRaw    []byte
	RoutingPolicyRaw   []byte
}

type ConnectionOrderCandidate struct {
	ID       int
	Priority int
}

var defaultRuntimeFailoverStatusCodes = []int{403, 422, 429, 500, 502, 503, 504, 529}

const adaptiveObservationFreshnessWindow = 5 * time.Minute

type autoRecoveryDocument struct {
	Mode        string                        `json:"mode"`
	StatusCodes []int                         `json:"status_codes"`
	Cooldown    *autoRecoveryCooldownDocument `json:"cooldown"`
	Ban         *autoRecoveryBanDocument      `json:"ban"`
}

type autoRecoveryCooldownDocument struct {
	BaseSeconds        int     `json:"base_seconds"`
	FailureThreshold   int     `json:"failure_threshold"`
	BackoffMultiplier  float64 `json:"backoff_multiplier"`
	MaxCooldownSeconds int     `json:"max_cooldown_seconds"`
}

type autoRecoveryBanDocument struct {
	Mode                        string `json:"mode"`
	MaxCooldownStrikesBeforeBan int    `json:"max_cooldown_strikes_before_ban"`
	BanDurationSeconds          int    `json:"ban_duration_seconds"`
}

type routingPolicyDocument struct {
	Hedge          routingPolicyHedgeDocument          `json:"hedge"`
	CircuitBreaker routingPolicyCircuitBreakerDocument `json:"circuit_breaker"`
	Admission      routingPolicyAdmissionDocument      `json:"admission"`
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
	BanMode                 string  `json:"ban_mode"`
	MaxOpenStrikesBeforeBan int     `json:"max_open_strikes_before_ban"`
	BanDurationSeconds      int     `json:"ban_duration_seconds"`
}

type routingPolicyAdmissionDocument struct {
	RespectQPSLimit       bool `json:"respect_qps_limit"`
	RespectInFlightLimits bool `json:"respect_in_flight_limits"`
}

type runtimeFeedbackPolicy struct {
	Enabled                 bool
	FailureThreshold        int
	BaseOpenSeconds         int
	BackoffMultiplier       float64
	MaxOpenSeconds          int
	BanMode                 string
	MaxOpenStrikesBeforeBan int
	BanDurationSeconds      int
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
	var legacyStrategyType sql.NullString
	record := RuntimeStrategy{}
	err := exec.QueryRow(
		ctx,
		`SELECT id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy
		FROM loadbalance_strategies
		WHERE profile_id = $1 AND id = $2
		LIMIT 1`,
		profileID,
		strategyID,
	).Scan(&record.ID, &record.Name, &record.StrategyType, &legacyStrategyType, &record.AutoRecoveryRaw, &record.RoutingPolicyRaw)
	if err == pgx.ErrNoRows {
		return RuntimeStrategy{}, false, nil
	}
	if err != nil {
		return RuntimeStrategy{}, false, fmt.Errorf("load runtime strategy %d for profile %d: %w", strategyID, profileID, err)
	}
	if legacyStrategyType.Valid {
		value := legacyStrategyType.String
		record.LegacyStrategyType = &value
	}
	return record, true, nil
}

func OrderConnectionIDs(profileID int, modelConfigID int, strategy RuntimeStrategy, connections []ConnectionOrderCandidate, states map[int]RuntimeConnectionState, roundRobin RuntimeRoundRobinCursorSource, nowAt time.Time) ([]int, error) {
	ordered := append([]ConnectionOrderCandidate(nil), connections...)
	sort.Slice(ordered, func(left int, right int) bool {
		return compareStableCandidates(ordered[left], ordered[right]) < 0
	})
	if isAdaptiveStrategy(strategy) {
		sort.SliceStable(ordered, func(left int, right int) bool {
			return compareAdaptiveCandidates(ordered[left], ordered[right], states, nowAt) < 0
		})
		return extractConnectionIDs(ordered), nil
	}
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

func compareAdaptiveCandidates(left ConnectionOrderCandidate, right ConnectionOrderCandidate, states map[int]RuntimeConnectionState, nowAt time.Time) int {
	leftState := states[left.ID]
	rightState := states[right.ID]

	if result := compareInts(adaptiveCircuitRank(leftState.CircuitState), adaptiveCircuitRank(rightState.CircuitState)); result != 0 {
		return result
	}
	if result := compareInts(adaptiveRecentFailurePenalty(leftState, nowAt), adaptiveRecentFailurePenalty(rightState, nowAt)); result != 0 {
		return result
	}
	if result, ok := compareOptionalInts(leftState.LiveP95LatencyMS, rightState.LiveP95LatencyMS); ok {
		return result
	}
	return compareStableCandidates(left, right)
}

func adaptiveCircuitRank(circuitState string) int {
	switch strings.ToLower(strings.TrimSpace(circuitState)) {
	case "half_open":
		return 1
	case "open":
		return 2
	default:
		return 0
	}
}

func adaptiveRecentFailurePenalty(state RuntimeConnectionState, nowAt time.Time) int {
	if state.LastLiveFailureAt == nil {
		return 0
	}
	if state.LastLiveSuccessAt != nil && state.LastLiveSuccessAt.After(*state.LastLiveFailureAt) {
		return 0
	}
	age := nowAt.UTC().Sub(state.LastLiveFailureAt.UTC())
	if age >= adaptiveObservationFreshnessWindow {
		return 0
	}
	if age < 0 {
		age = 0
	}
	return int((adaptiveObservationFreshnessWindow - age).Seconds())
}

func compareOptionalInts(left *int, right *int) (int, bool) {
	if left == nil || right == nil {
		return 0, false
	}
	return compareInts(*left, *right), true
}

func compareInts(left int, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func (strategy RuntimeStrategy) FailoverStatusCodes() []int {
	defaultCodes := append([]int(nil), defaultRuntimeFailoverStatusCodes...)
	if strings.ToLower(strings.TrimSpace(strategy.StrategyType)) == "adaptive" {
		var policy routingPolicyDocument
		if len(strategy.RoutingPolicyRaw) == 0 || json.Unmarshal(strategy.RoutingPolicyRaw, &policy) != nil || len(policy.CircuitBreaker.FailureStatusCodes) == 0 {
			return defaultCodes
		}
		return append([]int(nil), policy.CircuitBreaker.FailureStatusCodes...)
	}

	var recovery autoRecoveryDocument
	if len(strategy.AutoRecoveryRaw) == 0 || json.Unmarshal(strategy.AutoRecoveryRaw, &recovery) != nil || len(recovery.StatusCodes) == 0 {
		return defaultCodes
	}
	return append([]int(nil), recovery.StatusCodes...)
}

func (strategy RuntimeStrategy) FeedbackPolicy() runtimeFeedbackPolicy {
	if strings.ToLower(strings.TrimSpace(strategy.StrategyType)) == "adaptive" {
		return strategy.adaptiveFeedbackPolicy()
	}
	return strategy.legacyFeedbackPolicy()
}

func (strategy RuntimeStrategy) AdmissionPolicy() runtimeAdmissionPolicy {
	if strings.ToLower(strings.TrimSpace(strategy.StrategyType)) != "adaptive" {
		return runtimeAdmissionPolicy{}
	}
	policy := routingPolicyDocument{
		Admission: routingPolicyAdmissionDocument{
			RespectQPSLimit:       true,
			RespectInFlightLimits: true,
		},
	}
	if len(strategy.RoutingPolicyRaw) == 0 || json.Unmarshal(strategy.RoutingPolicyRaw, &policy) != nil {
		return runtimeAdmissionPolicy{RespectQPSLimit: true, RespectInFlightLimits: true}
	}
	return runtimeAdmissionPolicy{
		RespectQPSLimit:       policy.Admission.RespectQPSLimit,
		RespectInFlightLimits: policy.Admission.RespectInFlightLimits,
	}
}

func (strategy RuntimeStrategy) HedgePolicy() RuntimeHedgePolicy {
	if strings.ToLower(strings.TrimSpace(strategy.StrategyType)) != "adaptive" {
		return RuntimeHedgePolicy{}
	}
	policy := routingPolicyDocument{
		Hedge: routingPolicyHedgeDocument{
			Enabled:               false,
			DelayMS:               1500,
			MaxAdditionalAttempts: 1,
		},
	}
	if len(strategy.RoutingPolicyRaw) == 0 || json.Unmarshal(strategy.RoutingPolicyRaw, &policy) != nil {
		return RuntimeHedgePolicy{}
	}
	maxAdditionalAttempts := maxInt(policy.Hedge.MaxAdditionalAttempts, 1)
	if maxAdditionalAttempts > 1 {
		maxAdditionalAttempts = 1
	}
	return RuntimeHedgePolicy{
		Enabled:               policy.Hedge.Enabled && maxAdditionalAttempts > 0,
		Delay:                 time.Duration(maxInt(policy.Hedge.DelayMS, 0)) * time.Millisecond,
		MaxAdditionalAttempts: maxAdditionalAttempts,
	}
}

func (strategy RuntimeStrategy) legacyFeedbackPolicy() runtimeFeedbackPolicy {
	policy := runtimeFeedbackPolicy{
		Enabled:                 false,
		FailureThreshold:        1,
		BaseOpenSeconds:         0,
		BackoffMultiplier:       1,
		MaxOpenSeconds:          0,
		BanMode:                 "off",
		MaxOpenStrikesBeforeBan: 0,
		BanDurationSeconds:      0,
	}
	var recovery autoRecoveryDocument
	if len(strategy.AutoRecoveryRaw) == 0 || json.Unmarshal(strategy.AutoRecoveryRaw, &recovery) != nil {
		return policy
	}
	if strings.ToLower(strings.TrimSpace(recovery.Mode)) == "disabled" {
		return policy
	}
	policy.Enabled = true
	if recovery.Cooldown != nil {
		policy.FailureThreshold = maxInt(recovery.Cooldown.FailureThreshold, 1)
		policy.BaseOpenSeconds = maxInt(recovery.Cooldown.BaseSeconds, 0)
		policy.BackoffMultiplier = maxFloat(recovery.Cooldown.BackoffMultiplier, 1)
		policy.MaxOpenSeconds = maxInt(recovery.Cooldown.MaxCooldownSeconds, policy.BaseOpenSeconds)
	}
	if recovery.Ban != nil {
		policy.BanMode = normalizeBanMode(recovery.Ban.Mode)
		policy.MaxOpenStrikesBeforeBan = maxInt(recovery.Ban.MaxCooldownStrikesBeforeBan, 0)
		policy.BanDurationSeconds = maxInt(recovery.Ban.BanDurationSeconds, 0)
	}
	return policy
}

func (strategy RuntimeStrategy) adaptiveFeedbackPolicy() runtimeFeedbackPolicy {
	policy := runtimeFeedbackPolicy{
		Enabled:                 true,
		FailureThreshold:        1,
		BaseOpenSeconds:         0,
		BackoffMultiplier:       1,
		MaxOpenSeconds:          0,
		BanMode:                 "off",
		MaxOpenStrikesBeforeBan: 0,
		BanDurationSeconds:      0,
	}
	var document routingPolicyDocument
	if len(strategy.RoutingPolicyRaw) == 0 || json.Unmarshal(strategy.RoutingPolicyRaw, &document) != nil {
		return policy
	}
	policy.FailureThreshold = maxInt(document.CircuitBreaker.FailureThreshold, 1)
	policy.BaseOpenSeconds = maxInt(document.CircuitBreaker.BaseOpenSeconds, 0)
	policy.BackoffMultiplier = maxFloat(document.CircuitBreaker.BackoffMultiplier, 1)
	policy.MaxOpenSeconds = maxInt(document.CircuitBreaker.MaxOpenSeconds, policy.BaseOpenSeconds)
	policy.BanMode = normalizeBanMode(document.CircuitBreaker.BanMode)
	policy.MaxOpenStrikesBeforeBan = maxInt(document.CircuitBreaker.MaxOpenStrikesBeforeBan, 0)
	policy.BanDurationSeconds = maxInt(document.CircuitBreaker.BanDurationSeconds, 0)
	return policy
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

func normalizeBanMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "manual", "temporary":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "off"
	}
}

func isAdaptiveStrategy(strategy RuntimeStrategy) bool {
	return strings.ToLower(strings.TrimSpace(strategy.StrategyType)) == "adaptive"
}

func isRoundRobinStrategy(strategy RuntimeStrategy) bool {
	return isLegacyStrategyType(strategy, "round-robin")
}

func isSingleStrategy(strategy RuntimeStrategy) bool {
	return isLegacyStrategyType(strategy, "single")
}

func isLegacyStrategyType(strategy RuntimeStrategy, legacyType string) bool {
	if strings.ToLower(strings.TrimSpace(strategy.StrategyType)) != "legacy" {
		return false
	}
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

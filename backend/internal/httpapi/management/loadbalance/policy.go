package loadbalance

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

var defaultFailureStatusCodes = []int{403, 422, 429, 500, 502, 503, 504, 529}

const (
	defaultHedgeDelayMS          = 1500
	defaultMaxAdditionalAttempts = 1
	defaultOpenSeconds           = 60
	defaultFailureThreshold      = 2
	defaultBackoffMultiplier     = 2.0
	defaultMaxOpenSeconds        = 900
	defaultJitterRatio           = 0.2
)

func canonicalDefaultStrategySpecs() []canonicalDefaultStrategySpec {
	legacyType := "round-robin"
	legacy := buildDefaultAutoRecoveryDocument()
	adaptive := buildDefaultRoutingPolicyDocument()
	return []canonicalDefaultStrategySpec{
		{
			Name:               "Default legacy routing",
			StrategyType:       "legacy",
			LegacyStrategyType: &legacyType,
			AutoRecovery:       &legacy,
		},
		{
			Name:          "Default adaptive routing",
			StrategyType:  "adaptive",
			RoutingPolicy: &adaptive,
		},
	}
}

func buildDefaultAutoRecoveryDocument() autoRecoveryDocument {
	return autoRecoveryDocument{
		Mode:        "enabled",
		StatusCodes: append([]int(nil), defaultFailureStatusCodes...),
		Cooldown: &autoRecoveryCooldownDocument{
			BaseSeconds:        defaultOpenSeconds,
			FailureThreshold:   defaultFailureThreshold,
			BackoffMultiplier:  defaultBackoffMultiplier,
			MaxCooldownSeconds: defaultMaxOpenSeconds,
			JitterRatio:        defaultJitterRatio,
		},
		Ban: &autoRecoveryBanDocument{Mode: "off", MaxCooldownStrikesBeforeBan: 0, BanDurationSeconds: 0},
	}
}

func buildDefaultRoutingPolicyDocument() routingPolicyDocument {
	return routingPolicyDocument{
		Kind:             "adaptive",
		RoutingObjective: "minimize_latency",
		Hedge: routingPolicyHedgeDocument{
			Enabled:               false,
			DelayMS:               defaultHedgeDelayMS,
			MaxAdditionalAttempts: defaultMaxAdditionalAttempts,
		},
		CircuitBreaker: routingPolicyCircuitBreakerDocument{
			FailureStatusCodes:      append([]int(nil), defaultFailureStatusCodes...),
			BaseOpenSeconds:         defaultOpenSeconds,
			FailureThreshold:        defaultFailureThreshold,
			BackoffMultiplier:       defaultBackoffMultiplier,
			MaxOpenSeconds:          defaultMaxOpenSeconds,
			JitterRatio:             defaultJitterRatio,
			BanMode:                 "off",
			MaxOpenStrikesBeforeBan: 0,
			BanDurationSeconds:      0,
		},
		Admission: routingPolicyAdmissionDocument{RespectQPSLimit: true, RespectInFlightLimits: true},
	}
}

func canonicalizeStrategyRequest(requestBody loadbalanceStrategyRequest) (strategyPersistedPayload, error) {
	name := strings.TrimSpace(requestBody.Name)
	if name == "" {
		return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "name must not be empty"}
	}
	strategyType := strings.ToLower(strings.TrimSpace(requestBody.StrategyType))
	switch strategyType {
	case "legacy":
		if requestBody.LegacyStrategyType == nil {
			return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "legacy_strategy_type is required for legacy strategies"}
		}
		if requestBody.AutoRecovery == nil {
			return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "auto_recovery is required for legacy strategies"}
		}
		if requestBody.RoutingPolicy != nil {
			return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "routing_policy must be null for legacy strategies"}
		}
		legacyStrategyType, err := normalizeLegacyStrategyType(*requestBody.LegacyStrategyType)
		if err != nil {
			return strategyPersistedPayload{}, err
		}
		autoRecovery, err := canonicalizeAutoRecovery(requestBody.AutoRecovery)
		if err != nil {
			return strategyPersistedPayload{}, err
		}
		autoRecoveryJSON, err := json.Marshal(autoRecovery)
		if err != nil {
			return strategyPersistedPayload{}, fmt.Errorf("marshal auto_recovery: %w", err)
		}
		return strategyPersistedPayload{Name: name, StrategyType: strategyType, LegacyStrategyType: &legacyStrategyType, AutoRecovery: &autoRecovery, AutoRecoveryJSON: autoRecoveryJSON}, nil
	case "adaptive":
		if requestBody.RoutingPolicy == nil {
			return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "routing_policy is required for adaptive strategies"}
		}
		if requestBody.LegacyStrategyType != nil {
			return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "legacy_strategy_type must be null for adaptive strategies"}
		}
		if requestBody.AutoRecovery != nil {
			return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "auto_recovery must be null for adaptive strategies"}
		}
		routingPolicy, err := canonicalizeRoutingPolicy(requestBody.RoutingPolicy)
		if err != nil {
			return strategyPersistedPayload{}, err
		}
		routingPolicyJSON, err := json.Marshal(routingPolicy)
		if err != nil {
			return strategyPersistedPayload{}, fmt.Errorf("marshal routing_policy: %w", err)
		}
		return strategyPersistedPayload{Name: name, StrategyType: strategyType, RoutingPolicy: &routingPolicy, RoutingPolicyJSON: routingPolicyJSON}, nil
	default:
		return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "strategy_type must be one of 'legacy' or 'adaptive'"}
	}
}

func normalizeLegacyStrategyType(value string) (string, error) {
	resolved := strings.ToLower(strings.TrimSpace(value))
	switch resolved {
	case "single", "fill-first", "round-robin":
		return resolved, nil
	default:
		return "", &domainError{StatusCode: 400, Detail: "legacy_strategy_type must be one of 'single', 'fill-first', or 'round-robin'"}
	}
}

func canonicalizeAutoRecovery(input *autoRecoveryInput) (autoRecoveryDocument, error) {
	if input == nil {
		return autoRecoveryDocument{}, &domainError{StatusCode: 400, Detail: "auto_recovery is required for legacy strategies"}
	}
	mode := "enabled"
	if input.Mode != nil {
		mode = strings.ToLower(strings.TrimSpace(*input.Mode))
	}
	switch mode {
	case "disabled":
		return autoRecoveryDocument{Mode: "disabled"}, nil
	case "enabled":
	default:
		return autoRecoveryDocument{}, &domainError{StatusCode: 400, Detail: "mode must be 'disabled' or 'enabled'"}
	}
	statusCodes, err := normalizeStatusCodes(input.StatusCodes, defaultFailureStatusCodes)
	if err != nil {
		return autoRecoveryDocument{}, err
	}
	cooldown := autoRecoveryCooldownDocument{
		BaseSeconds:        resolvedInt(input.Cooldown, func(value *autoRecoveryCooldownInput) *int { return value.BaseSeconds }, defaultOpenSeconds),
		FailureThreshold:   resolvedInt(input.Cooldown, func(value *autoRecoveryCooldownInput) *int { return value.FailureThreshold }, defaultFailureThreshold),
		BackoffMultiplier:  resolvedFloat(input.Cooldown, func(value *autoRecoveryCooldownInput) *float64 { return value.BackoffMultiplier }, defaultBackoffMultiplier),
		MaxCooldownSeconds: resolvedInt(input.Cooldown, func(value *autoRecoveryCooldownInput) *int { return value.MaxCooldownSeconds }, defaultMaxOpenSeconds),
		JitterRatio:        resolvedFloat(input.Cooldown, func(value *autoRecoveryCooldownInput) *float64 { return value.JitterRatio }, defaultJitterRatio),
	}
	if err := validateCooldown(cooldown); err != nil {
		return autoRecoveryDocument{}, err
	}
	banMode := "off"
	if input.Ban != nil && input.Ban.Mode != nil {
		banMode = strings.ToLower(strings.TrimSpace(*input.Ban.Mode))
	}
	ban := autoRecoveryBanDocument{
		Mode:                        banMode,
		MaxCooldownStrikesBeforeBan: resolvedBanInt(input.Ban, func(value *autoRecoveryBanInput) *int { return value.MaxCooldownStrikesBeforeBan }, 0),
		BanDurationSeconds:          resolvedBanInt(input.Ban, func(value *autoRecoveryBanInput) *int { return value.BanDurationSeconds }, 0),
	}
	if err := validateAutoRecoveryBan(ban); err != nil {
		return autoRecoveryDocument{}, err
	}
	return autoRecoveryDocument{Mode: "enabled", StatusCodes: statusCodes, Cooldown: &cooldown, Ban: &ban}, nil
}

func canonicalizeRoutingPolicy(input *routingPolicyInput) (routingPolicyDocument, error) {
	if input == nil {
		input = &routingPolicyInput{}
	}
	kind := "adaptive"
	if input.Kind != nil {
		kind = strings.ToLower(strings.TrimSpace(*input.Kind))
	}
	if kind != "adaptive" {
		return routingPolicyDocument{}, &domainError{StatusCode: 400, Detail: "kind must be 'adaptive'"}
	}
	routingObjective := "minimize_latency"
	if input.RoutingObjective != nil {
		routingObjective = strings.ToLower(strings.TrimSpace(*input.RoutingObjective))
	}
	if routingObjective != "minimize_latency" && routingObjective != "maximize_availability" {
		return routingPolicyDocument{}, &domainError{StatusCode: 400, Detail: "routing_objective must be 'minimize_latency' or 'maximize_availability'"}
	}
	hedge := routingPolicyHedgeDocument{
		Enabled:               resolvedBool(input.Hedge, func(value *routingPolicyHedgeInput) *bool { return value.Enabled }, false),
		DelayMS:               resolvedInt(input.Hedge, func(value *routingPolicyHedgeInput) *int { return value.DelayMS }, defaultHedgeDelayMS),
		MaxAdditionalAttempts: resolvedInt(input.Hedge, func(value *routingPolicyHedgeInput) *int { return value.MaxAdditionalAttempts }, defaultMaxAdditionalAttempts),
	}
	if err := validateHedge(hedge); err != nil {
		return routingPolicyDocument{}, err
	}
	failureStatusCodes, err := normalizeStatusCodes(circuitStatusCodes(input.CircuitBreaker), defaultFailureStatusCodes)
	if err != nil {
		return routingPolicyDocument{}, err
	}
	circuitBreaker := routingPolicyCircuitBreakerDocument{
		FailureStatusCodes:      failureStatusCodes,
		BaseOpenSeconds:         resolvedInt(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *int { return value.BaseOpenSeconds }, defaultOpenSeconds),
		FailureThreshold:        resolvedInt(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *int { return value.FailureThreshold }, defaultFailureThreshold),
		BackoffMultiplier:       resolvedFloat(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *float64 { return value.BackoffMultiplier }, defaultBackoffMultiplier),
		MaxOpenSeconds:          resolvedInt(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *int { return value.MaxOpenSeconds }, defaultMaxOpenSeconds),
		JitterRatio:             resolvedFloat(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *float64 { return value.JitterRatio }, defaultJitterRatio),
		BanMode:                 resolvedString(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *string { return value.BanMode }, "off"),
		MaxOpenStrikesBeforeBan: resolvedInt(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *int { return value.MaxOpenStrikesBeforeBan }, 0),
		BanDurationSeconds:      resolvedInt(input.CircuitBreaker, func(value *routingPolicyCircuitBreakerInput) *int { return value.BanDurationSeconds }, 0),
	}
	if err := validateCircuitBreaker(circuitBreaker); err != nil {
		return routingPolicyDocument{}, err
	}
	admission := routingPolicyAdmissionDocument{
		RespectQPSLimit:       resolvedBool(input.Admission, func(value *routingPolicyAdmissionInput) *bool { return value.RespectQPSLimit }, true),
		RespectInFlightLimits: resolvedBool(input.Admission, func(value *routingPolicyAdmissionInput) *bool { return value.RespectInFlightLimits }, true),
	}
	return routingPolicyDocument{Kind: "adaptive", RoutingObjective: routingObjective, Hedge: hedge, CircuitBreaker: circuitBreaker, Admission: admission}, nil
}

func normalizeStatusCodes(values []int, defaults []int) ([]int, error) {
	if len(values) == 0 {
		cloned := append([]int(nil), defaults...)
		return cloned, nil
	}
	seen := map[int]struct{}{}
	items := make([]int, 0, len(values))
	for _, value := range values {
		if value < 100 || value > 599 {
			return nil, &domainError{StatusCode: 400, Detail: "status codes must contain valid HTTP status codes"}
		}
		if _, ok := seen[value]; ok {
			return nil, &domainError{StatusCode: 400, Detail: "status codes must not contain duplicates"}
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Ints(items)
	return items, nil
}

func validateCooldown(cooldown autoRecoveryCooldownDocument) error {
	if cooldown.BaseSeconds < 0 || cooldown.BaseSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "base_seconds must be between 0 and 86400"}
	}
	if cooldown.FailureThreshold < 1 || cooldown.FailureThreshold > 50 {
		return &domainError{StatusCode: 400, Detail: "failure_threshold must be between 1 and 50"}
	}
	if cooldown.BackoffMultiplier < 1.0 || cooldown.BackoffMultiplier > 10.0 {
		return &domainError{StatusCode: 400, Detail: "backoff_multiplier must be between 1.0 and 10.0"}
	}
	if cooldown.MaxCooldownSeconds < 1 || cooldown.MaxCooldownSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "max_cooldown_seconds must be between 1 and 86400"}
	}
	if cooldown.JitterRatio < 0.0 || cooldown.JitterRatio > 1.0 {
		return &domainError{StatusCode: 400, Detail: "jitter_ratio must be between 0.0 and 1.0"}
	}
	return nil
}

func validateAutoRecoveryBan(ban autoRecoveryBanDocument) error {
	if ban.MaxCooldownStrikesBeforeBan < 0 || ban.MaxCooldownStrikesBeforeBan > 100 {
		return &domainError{StatusCode: 400, Detail: "max_cooldown_strikes_before_ban must be between 0 and 100"}
	}
	if ban.BanDurationSeconds < 0 || ban.BanDurationSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "ban_duration_seconds must be between 0 and 86400"}
	}
	switch ban.Mode {
	case "off":
		if ban.MaxCooldownStrikesBeforeBan != 0 {
			return &domainError{StatusCode: 400, Detail: "mode='off' requires max_cooldown_strikes_before_ban=0"}
		}
		if ban.BanDurationSeconds != 0 {
			return &domainError{StatusCode: 400, Detail: "mode='off' requires ban_duration_seconds=0"}
		}
	case "manual":
		if ban.MaxCooldownStrikesBeforeBan < 1 {
			return &domainError{StatusCode: 400, Detail: "mode='manual' requires max_cooldown_strikes_before_ban >= 1"}
		}
		if ban.BanDurationSeconds != 0 {
			return &domainError{StatusCode: 400, Detail: "mode='manual' requires ban_duration_seconds=0"}
		}
	case "temporary":
		if ban.MaxCooldownStrikesBeforeBan < 1 {
			return &domainError{StatusCode: 400, Detail: "mode='temporary' requires max_cooldown_strikes_before_ban >= 1"}
		}
		if ban.BanDurationSeconds < 1 {
			return &domainError{StatusCode: 400, Detail: "mode='temporary' requires ban_duration_seconds >= 1"}
		}
	default:
		return &domainError{StatusCode: 400, Detail: "mode must be one of 'off', 'manual', or 'temporary'"}
	}
	return nil
}

func validateHedge(hedge routingPolicyHedgeDocument) error {
	if hedge.DelayMS < 0 || hedge.DelayMS > 300000 {
		return &domainError{StatusCode: 400, Detail: "delay_ms must be between 0 and 300000"}
	}
	if hedge.MaxAdditionalAttempts < 1 || hedge.MaxAdditionalAttempts > 10 {
		return &domainError{StatusCode: 400, Detail: "max_additional_attempts must be between 1 and 10"}
	}
	return nil
}

func validateCircuitBreaker(circuitBreaker routingPolicyCircuitBreakerDocument) error {
	if circuitBreaker.BaseOpenSeconds < 0 || circuitBreaker.BaseOpenSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "base_open_seconds must be between 0 and 86400"}
	}
	if circuitBreaker.FailureThreshold < 1 || circuitBreaker.FailureThreshold > 50 {
		return &domainError{StatusCode: 400, Detail: "failure_threshold must be between 1 and 50"}
	}
	if circuitBreaker.BackoffMultiplier < 1.0 || circuitBreaker.BackoffMultiplier > 10.0 {
		return &domainError{StatusCode: 400, Detail: "backoff_multiplier must be between 1.0 and 10.0"}
	}
	if circuitBreaker.MaxOpenSeconds < 1 || circuitBreaker.MaxOpenSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "max_open_seconds must be between 1 and 86400"}
	}
	if circuitBreaker.JitterRatio < 0.0 || circuitBreaker.JitterRatio > 1.0 {
		return &domainError{StatusCode: 400, Detail: "jitter_ratio must be between 0.0 and 1.0"}
	}
	if circuitBreaker.MaxOpenStrikesBeforeBan < 0 || circuitBreaker.MaxOpenStrikesBeforeBan > 100 {
		return &domainError{StatusCode: 400, Detail: "max_open_strikes_before_ban must be between 0 and 100"}
	}
	if circuitBreaker.BanDurationSeconds < 0 || circuitBreaker.BanDurationSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "ban_duration_seconds must be between 0 and 86400"}
	}
	switch circuitBreaker.BanMode {
	case "off":
		if circuitBreaker.MaxOpenStrikesBeforeBan != 0 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='off' requires max_open_strikes_before_ban=0"}
		}
		if circuitBreaker.BanDurationSeconds != 0 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='off' requires ban_duration_seconds=0"}
		}
	case "manual":
		if circuitBreaker.MaxOpenStrikesBeforeBan < 1 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='manual' requires max_open_strikes_before_ban >= 1"}
		}
		if circuitBreaker.BanDurationSeconds != 0 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='manual' requires ban_duration_seconds=0"}
		}
	case "temporary":
		if circuitBreaker.MaxOpenStrikesBeforeBan < 1 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='temporary' requires max_open_strikes_before_ban >= 1"}
		}
		if circuitBreaker.BanDurationSeconds < 1 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='temporary' requires ban_duration_seconds >= 1"}
		}
	default:
		return &domainError{StatusCode: 400, Detail: "ban_mode must be one of 'off', 'manual', or 'temporary'"}
	}
	return nil
}

func resolvedInt[T any](input *T, selector func(*T) *int, fallback int) int {
	if input == nil {
		return fallback
	}
	value := selector(input)
	if value == nil {
		return fallback
	}
	return *value
}

func resolvedFloat[T any](input *T, selector func(*T) *float64, fallback float64) float64 {
	if input == nil {
		return fallback
	}
	value := selector(input)
	if value == nil {
		return fallback
	}
	return *value
}

func resolvedBool[T any](input *T, selector func(*T) *bool, fallback bool) bool {
	if input == nil {
		return fallback
	}
	value := selector(input)
	if value == nil {
		return fallback
	}
	return *value
}

func resolvedString[T any](input *T, selector func(*T) *string, fallback string) string {
	if input == nil {
		return fallback
	}
	value := selector(input)
	if value == nil {
		return fallback
	}
	resolved := strings.ToLower(strings.TrimSpace(*value))
	if resolved == "" {
		return fallback
	}
	return resolved
}

func resolvedBanInt(input *autoRecoveryBanInput, selector func(*autoRecoveryBanInput) *int, fallback int) int {
	if input == nil {
		return fallback
	}
	value := selector(input)
	if value == nil {
		return fallback
	}
	return *value
}

func circuitStatusCodes(input *routingPolicyCircuitBreakerInput) []int {
	if input == nil {
		return nil
	}
	return input.FailureStatusCodes
}

func strategyResponseFromRow(row strategyRow) (loadbalanceStrategyResponse, error) {
	response := loadbalanceStrategyResponse{
		ID:                 row.ID,
		ProfileID:          row.ProfileID,
		Name:               row.Name,
		StrategyType:       row.StrategyType,
		AttachedModelCount: row.AttachedModelCount,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
	switch row.StrategyType {
	case "legacy":
		if row.LegacyStrategyType == nil {
			return loadbalanceStrategyResponse{}, fmt.Errorf("loadbalance strategy %d missing legacy_strategy_type", row.ID)
		}
		legacyStrategyType, err := normalizeLegacyStrategyType(*row.LegacyStrategyType)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		if len(row.AutoRecoveryRaw) == 0 {
			return loadbalanceStrategyResponse{}, fmt.Errorf("loadbalance strategy %d missing auto_recovery", row.ID)
		}
		var input autoRecoveryInput
		if err := json.Unmarshal(row.AutoRecoveryRaw, &input); err != nil {
			return loadbalanceStrategyResponse{}, fmt.Errorf("decode strategy %d auto_recovery: %w", row.ID, err)
		}
		autoRecovery, err := canonicalizeAutoRecovery(&input)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		response.LegacyStrategyType = &legacyStrategyType
		response.AutoRecovery = &autoRecovery
		return response, nil
	case "adaptive":
		if len(row.RoutingPolicyRaw) == 0 {
			return loadbalanceStrategyResponse{}, fmt.Errorf("loadbalance strategy %d missing routing_policy", row.ID)
		}
		var input routingPolicyInput
		if err := json.Unmarshal(row.RoutingPolicyRaw, &input); err != nil {
			return loadbalanceStrategyResponse{}, fmt.Errorf("decode strategy %d routing_policy: %w", row.ID, err)
		}
		routingPolicy, err := canonicalizeRoutingPolicy(&input)
		if err != nil {
			return loadbalanceStrategyResponse{}, err
		}
		response.RoutingPolicy = &routingPolicy
		return response, nil
	default:
		return loadbalanceStrategyResponse{}, &domainError{StatusCode: 500, Detail: "Invalid stored loadbalance strategy"}
	}
}

func strategyMatchesCanonicalDefault(response loadbalanceStrategyResponse, expected canonicalDefaultStrategySpec) bool {
	if response.Name != expected.Name || response.StrategyType != expected.StrategyType {
		return false
	}
	if !reflect.DeepEqual(response.LegacyStrategyType, expected.LegacyStrategyType) {
		return false
	}
	if !reflect.DeepEqual(response.AutoRecovery, expected.AutoRecovery) {
		return false
	}
	if !reflect.DeepEqual(response.RoutingPolicy, expected.RoutingPolicy) {
		return false
	}
	return true
}

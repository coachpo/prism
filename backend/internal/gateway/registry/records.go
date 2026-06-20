package registry

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type ModelProfile struct {
	ID                        string
	APIFamily                 string
	ContextWindowTokens       int
	DefaultOutputTokenReserve int
	MaxContextUtilization     float64
	RedirectModelID           string
	PreserveResponseShape     bool
}

type UpstreamEndpoint struct {
	ID                  string
	APIFamily           string
	BaseURL             string
	AuthProfileID       string
	SupportedOperations []string
	PriceCatalogVersion PriceCatalogVersion
	QPSLimit            int
	RPMLimit            int
	TPMLimit            int
	IPMLimit            int
	MaxConcurrency      int
}

type RouteRule struct {
	ID                 string
	Priority           int
	MatchModelID       string
	MatchOperationName string
	PolicyID           string
	RedirectModelID    string
	RedirectUpstreamID string
}

type LoadBalanceStrategy string

type RoutePolicy struct {
	ID               string
	Strategy         LoadBalanceStrategy
	MaxAttempts      int
	RetryStatusCodes []int
	AdmissionPolicy  AdmissionPolicy
}

type AdmissionPolicy struct {
	RequireQPSReservation         bool
	RequireRPMReservation         bool
	RequireTPMReservation         bool
	RequireIPMReservation         bool
	RequireConcurrencyReservation bool
}

type RouteReason string

type RoutePlan struct {
	OperationName        string
	RequestedModelID     string
	EffectiveModelID     string
	CandidateUpstreamIDs []string
	RouteReason          RouteReason
	Attempts             []RouteAttempt
	PriceCatalogVersion  PriceCatalogVersion
}

type RouteAttempt struct {
	UpstreamID  string
	ModelID     string
	RouteReason RouteReason
}

type PriceUnit string

type PriceCatalogVersion struct {
	CatalogID string
	Version   int
}

type PriceCatalog struct {
	Version                              PriceCatalogVersion
	CurrencyCode                         string
	Unit                                 PriceUnit
	InputPriceMicrosPerUnit              int64
	OutputPriceMicrosPerUnit             int64
	CacheReadInputPriceMicrosPerUnit     int64
	CacheCreationInputPriceMicrosPerUnit int64
	ReasoningPriceMicrosPerUnit          int64
	ImageInputPriceNanosPerUnit          int64
	ImageOutputPriceNanosPerUnit         int64
}

type ProviderCapability struct {
	ProviderID             string
	APIFamily              string
	NativeOperations       []string
	StreamingOperations    []string
	TokenCounting          bool
	TokenEstimation        bool
	OverflowClassification bool
	RequestConversion      bool
	ImageGeneration        bool
	ImageEditing           bool
}

const (
	LoadBalanceSingle     LoadBalanceStrategy = "single"
	LoadBalanceFillFirst  LoadBalanceStrategy = "fill-first"
	LoadBalanceRoundRobin LoadBalanceStrategy = "round-robin"

	RouteReasonDirectMatch         RouteReason = "direct_match"
	RouteReasonModelRedirect       RouteReason = "model_redirect"
	RouteReasonUpstreamRedirect    RouteReason = "upstream_redirect"
	RouteReasonQPSOverflow         RouteReason = "qps_overflow"
	RouteReasonRPMOverflow         RouteReason = "rpm_overflow"
	RouteReasonTPMOverflow         RouteReason = "tpm_overflow"
	RouteReasonIPMOverflow         RouteReason = "ipm_overflow"
	RouteReasonConcurrencyOverflow RouteReason = "concurrency_overflow"
	RouteReasonRetry429            RouteReason = "retry_429"
	RouteReasonRetry5xx            RouteReason = "retry_5xx"
	RouteReasonRetryConnectTimeout RouteReason = "retry_connect_timeout"
	RouteReasonCircuitOpenSkip     RouteReason = "circuit_open_skip"
	RouteReasonNoHealthyUpstream   RouteReason = "no_healthy_upstream"
	RouteReasonPolicyReject        RouteReason = "policy_reject"

	PriceUnitPerMillionTokens PriceUnit = "per_1m_tokens"
	PriceUnitPerImage         PriceUnit = "per_image"
)

var validRouteReasons = map[RouteReason]struct{}{
	RouteReasonDirectMatch: {}, RouteReasonModelRedirect: {}, RouteReasonUpstreamRedirect: {}, RouteReasonQPSOverflow: {}, RouteReasonRPMOverflow: {}, RouteReasonTPMOverflow: {}, RouteReasonIPMOverflow: {}, RouteReasonConcurrencyOverflow: {}, RouteReasonRetry429: {}, RouteReasonRetry5xx: {}, RouteReasonRetryConnectTimeout: {}, RouteReasonCircuitOpenSkip: {}, RouteReasonNoHealthyUpstream: {}, RouteReasonPolicyReject: {},
}

func (profile ModelProfile) Validate() error {
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "model_profile_id_empty", "id", "model profile id is required", profile.ID)
	issues = appendBlankIssue(issues, "model_profile_api_family_empty", "api_family", "api family is required", profile.APIFamily)
	if profile.ContextWindowTokens <= 0 {
		issues = append(issues, issue("model_profile_context_window_invalid", "context_window_tokens", "context window must be positive"))
	}
	if profile.DefaultOutputTokenReserve < 0 || (profile.ContextWindowTokens > 0 && profile.DefaultOutputTokenReserve >= profile.ContextWindowTokens) {
		issues = append(issues, issue("model_profile_output_reserve_invalid", "default_output_token_reserve", "output reserve must be non-negative and less than context window"))
	}
	if profile.MaxContextUtilization <= 0 || profile.MaxContextUtilization > 1 {
		issues = append(issues, issue("model_profile_context_utilization_invalid", "max_context_utilization", "context utilization must be > 0 and <= 1"))
	}
	if sameNonEmpty(profile.ID, profile.RedirectModelID) {
		issues = append(issues, issue("model_profile_redirect_self", "redirect_model_id", "model redirect cannot target itself"))
	}
	return newValidationError("ModelProfile", issues)
}

func (endpoint UpstreamEndpoint) Validate() error {
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "upstream_endpoint_id_empty", "id", "upstream endpoint id is required", endpoint.ID)
	issues = appendBlankIssue(issues, "upstream_endpoint_api_family_empty", "api_family", "api family is required", endpoint.APIFamily)
	if parsed, err := url.Parse(strings.TrimSpace(endpoint.BaseURL)); strings.TrimSpace(endpoint.BaseURL) == "" || err != nil || parsed.Scheme == "" || parsed.Host == "" {
		issues = append(issues, issue("upstream_endpoint_base_url_invalid", "base_url", "base url must be an absolute URL"))
	}
	issues = append(issues, validateStringSet("upstream_endpoint_supported_operations", "supported_operations", endpoint.SupportedOperations)...)
	issues = append(issues, validatePriceCatalogVersion(endpoint.PriceCatalogVersion, "price_catalog_version", false)...)
	for _, quota := range []struct {
		code  string
		field string
		value int
	}{
		{"upstream_endpoint_qps_limit_invalid", "qps_limit", endpoint.QPSLimit},
		{"upstream_endpoint_rpm_limit_invalid", "rpm_limit", endpoint.RPMLimit},
		{"upstream_endpoint_tpm_limit_invalid", "tpm_limit", endpoint.TPMLimit},
		{"upstream_endpoint_ipm_limit_invalid", "ipm_limit", endpoint.IPMLimit},
		{"upstream_endpoint_max_concurrency_invalid", "max_concurrency", endpoint.MaxConcurrency},
	} {
		if quota.value < 0 {
			issues = append(issues, issue(quota.code, quota.field, "quota and concurrency limits cannot be negative"))
		}
	}
	return newValidationError("UpstreamEndpoint", issues)
}

func (rule RouteRule) Validate() error {
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "route_rule_id_empty", "id", "route rule id is required", rule.ID)
	issues = appendBlankIssue(issues, "route_rule_policy_id_empty", "policy_id", "route policy id is required", rule.PolicyID)
	if strings.TrimSpace(rule.MatchModelID) == "" && strings.TrimSpace(rule.MatchOperationName) == "" {
		issues = append(issues, issue("route_rule_match_empty", "match", "route rule must match a model or operation"))
	}
	if strings.TrimSpace(rule.RedirectModelID) != "" && strings.TrimSpace(rule.RedirectUpstreamID) != "" {
		issues = append(issues, issue("route_rule_redirect_ambiguous", "redirect", "model and upstream redirect cannot both be set on one rule"))
	}
	return newValidationError("RouteRule", issues)
}

func (policy RoutePolicy) Validate() error {
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "route_policy_id_empty", "id", "route policy id is required", policy.ID)
	switch policy.Strategy {
	case LoadBalanceSingle, LoadBalanceFillFirst, LoadBalanceRoundRobin:
	default:
		issues = append(issues, issue("route_policy_strategy_invalid", "strategy", "load-balance strategy is unsupported"))
	}
	if policy.MaxAttempts <= 0 {
		issues = append(issues, issue("route_policy_max_attempts_invalid", "max_attempts", "max attempts must be positive"))
	}
	last := 0
	for index, status := range policy.RetryStatusCodes {
		if status < 400 || status > 599 {
			issues = append(issues, issue("route_policy_retry_status_invalid", "retry_status_codes", "retry status codes must be HTTP 4xx or 5xx"))
		}
		if index > 0 && status <= last {
			issues = append(issues, issue("route_policy_retry_status_order_invalid", "retry_status_codes", "retry status codes must be strictly ascending"))
		}
		last = status
	}
	return newValidationError("RoutePolicy", issues)
}

func (plan RoutePlan) Validate() error {
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "route_plan_operation_empty", "operation_name", "operation name is required", plan.OperationName)
	issues = appendBlankIssue(issues, "route_plan_requested_model_empty", "requested_model_id", "requested model id is required", plan.RequestedModelID)
	issues = appendBlankIssue(issues, "route_plan_effective_model_empty", "effective_model_id", "effective model id is required", plan.EffectiveModelID)
	issues = append(issues, validateStringSet("route_plan_candidate_upstreams", "candidate_upstream_ids", plan.CandidateUpstreamIDs)...)
	if _, ok := validRouteReasons[plan.RouteReason]; !ok {
		issues = append(issues, issue("route_plan_reason_invalid", "route_reason", "route reason is unsupported"))
	}
	if len(plan.Attempts) == 0 {
		issues = append(issues, issue("route_plan_attempts_empty", "attempts", "route plan must contain at least one attempt"))
	}
	for index, attempt := range plan.Attempts {
		fieldPrefix := "attempts[" + strconv.Itoa(index) + "]"
		issues = appendBlankIssue(issues, "route_plan_attempt_upstream_empty", fieldPrefix+".upstream_id", "attempt upstream id is required", attempt.UpstreamID)
		issues = appendBlankIssue(issues, "route_plan_attempt_model_empty", fieldPrefix+".model_id", "attempt model id is required", attempt.ModelID)
		if _, ok := validRouteReasons[attempt.RouteReason]; !ok {
			issues = append(issues, issue("route_plan_attempt_reason_invalid", fieldPrefix+".route_reason", "attempt route reason is unsupported"))
		}
	}
	issues = append(issues, validatePriceCatalogVersion(plan.PriceCatalogVersion, "price_catalog_version", true)...)
	return newValidationError("RoutePlan", issues)
}

func (catalog PriceCatalog) Validate() error {
	issues := make([]ValidationIssue, 0)
	issues = append(issues, validatePriceCatalogVersion(catalog.Version, "version", true)...)
	if currency := strings.TrimSpace(catalog.CurrencyCode); len(currency) != 3 || currency != strings.ToUpper(currency) {
		issues = append(issues, issue("price_catalog_currency_invalid", "currency_code", "currency code must be three uppercase letters"))
	}
	switch catalog.Unit {
	case PriceUnitPerMillionTokens, PriceUnitPerImage:
	default:
		issues = append(issues, issue("price_catalog_unit_invalid", "unit", "price unit is unsupported"))
	}
	for _, price := range []struct {
		code  string
		field string
		value int64
	}{
		{"price_catalog_input_micros_invalid", "input_price_micros_per_unit", catalog.InputPriceMicrosPerUnit},
		{"price_catalog_output_micros_invalid", "output_price_micros_per_unit", catalog.OutputPriceMicrosPerUnit},
		{"price_catalog_cache_read_micros_invalid", "cache_read_input_price_micros_per_unit", catalog.CacheReadInputPriceMicrosPerUnit},
		{"price_catalog_cache_creation_micros_invalid", "cache_creation_input_price_micros_per_unit", catalog.CacheCreationInputPriceMicrosPerUnit},
		{"price_catalog_reasoning_micros_invalid", "reasoning_price_micros_per_unit", catalog.ReasoningPriceMicrosPerUnit},
		{"price_catalog_image_input_nanos_invalid", "image_input_price_nanos_per_unit", catalog.ImageInputPriceNanosPerUnit},
		{"price_catalog_image_output_nanos_invalid", "image_output_price_nanos_per_unit", catalog.ImageOutputPriceNanosPerUnit},
	} {
		if price.value < 0 {
			issues = append(issues, issue(price.code, price.field, "integer micros/nanos prices cannot be negative"))
		}
	}
	return newValidationError("PriceCatalog", issues)
}

func (capability ProviderCapability) Validate() error {
	issues := make([]ValidationIssue, 0)
	issues = appendBlankIssue(issues, "provider_capability_provider_empty", "provider_id", "provider id is required", capability.ProviderID)
	issues = appendBlankIssue(issues, "provider_capability_api_family_empty", "api_family", "api family is required", capability.APIFamily)
	issues = append(issues, validateStringSet("provider_capability_native_operations", "native_operations", capability.NativeOperations)...)
	nativeSet := toStringSet(capability.NativeOperations)
	for _, operation := range capability.StreamingOperations {
		if _, ok := nativeSet[strings.TrimSpace(operation)]; !ok {
			issues = append(issues, issue("provider_capability_streaming_not_native", "streaming_operations", "streaming operation must also be native"))
		}
	}
	return newValidationError("ProviderCapability", issues)
}

func validatePriceCatalogVersion(version PriceCatalogVersion, field string, required bool) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if !required && strings.TrimSpace(version.CatalogID) == "" && version.Version == 0 {
		return nil
	}
	issues = appendBlankIssue(issues, "price_catalog_version_catalog_empty", field+".catalog_id", "price catalog id is required", version.CatalogID)
	if version.Version <= 0 {
		issues = append(issues, issue("price_catalog_version_invalid", field+".version", "price catalog version must be positive"))
	}
	return issues
}

func validateStringSet(codePrefix string, field string, values []string) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if len(values) == 0 {
		return append(issues, issue(codePrefix+"_empty", field, "at least one value is required"))
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			issues = append(issues, issue(codePrefix+"_blank", field, "blank values are not allowed"))
			continue
		}
		if _, exists := seen[trimmed]; exists {
			issues = append(issues, issue(codePrefix+"_duplicate", field, "duplicate values are not allowed"))
		}
		seen[trimmed] = struct{}{}
	}
	return issues
}

func toStringSet(values []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return set
}

func sameNonEmpty(left string, right string) bool {
	return strings.TrimSpace(left) != "" && strings.TrimSpace(left) == strings.TrimSpace(right)
}

func SortedRouteReasons() []RouteReason {
	reasons := make([]RouteReason, 0, len(validRouteReasons))
	for reason := range validRouteReasons {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)
	return reasons
}

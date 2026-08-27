package stats

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	ScopeIngress      = "ingress"
	ScopeFinal        = "final_execution"
	ScopeRouteAttempt = "route_attempt"
)

const (
	GroupNone               = "none"
	GroupAPIFamily          = "api_family"
	GroupIngressModel       = "ingress_model"
	GroupFinalTargetModel   = "final_target_model"
	GroupAttemptTargetModel = "attempt_target_model"
	GroupEndpoint           = "endpoint"
	GroupTerminalTarget     = "terminal_target"
	GroupProxyAPIKey        = "proxy_api_key"
	GroupAttemptTrigger     = "attempt_trigger"
	GroupAttemptResult      = "attempt_result"
)

var validScopes = map[string]struct{}{
	ScopeIngress: {}, ScopeFinal: {}, ScopeRouteAttempt: {},
}

type ScopeCaliber struct {
	Scope         string   `json:"scope"`
	Grain         string   `json:"grain"`
	IdentityBasis string   `json:"identity_basis"`
	OutcomeBasis  string   `json:"outcome_basis"`
	LatencyBasis  string   `json:"latency_basis"`
	CostBasis     string   `json:"cost_basis"`
	Datasets      []string `json:"datasets"`
}

type ScopeSampleCounts struct {
	ObservationCount    int `json:"observation_count"`
	LatencySampleCount  int `json:"latency_sample_count"`
	LatencyMissingCount int `json:"latency_missing_count"`
	CostSampleCount     int `json:"cost_sample_count"`
	CostMissingCount    int `json:"cost_missing_count"`
}

type DatasetCoverage struct {
	UsageRequestEvents *Coverage `json:"usage_request_events,omitempty"`
	RequestLogs        *Coverage `json:"request_logs,omitempty"`
	LoadbalanceEvents  *Coverage `json:"loadbalance_events,omitempty"`
}

func NormalizeScope(scope string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(scope))
	if trimmed == "" {
		return ScopeIngress, nil
	}
	if _, ok := validScopes[trimmed]; !ok {
		return "", &HTTPError{StatusCode: 422, Code: "scope_invalid", Detail: fmt.Sprintf("unknown scope %q", scope)}
	}
	return trimmed, nil
}

func CaliberForScope(scope string) ScopeCaliber {
	switch scope {
	case ScopeFinal:
		return ScopeCaliber{Scope: ScopeFinal, Grain: "finalized_execution", IdentityBasis: "final_target_model_id", OutcomeBasis: "final_result", LatencyBasis: "final_attempt_duration", CostBasis: "served_final_trusted_cost", Datasets: []string{"usage_request_events", "request_logs"}}
	case ScopeRouteAttempt:
		return ScopeCaliber{Scope: ScopeRouteAttempt, Grain: "upstream_attempt", IdentityBasis: "attempt_target_model_id", OutcomeBasis: "attempt_result", LatencyBasis: "attempt_duration", CostBasis: "none", Datasets: []string{"request_logs"}}
	default:
		return ScopeCaliber{Scope: ScopeIngress, Grain: "ingress_request", IdentityBasis: "ingress_model_id", OutcomeBasis: "final_result", LatencyBasis: "ingress_end_to_end", CostBasis: "served_final_trusted_cost", Datasets: []string{"usage_request_events"}}
	}
}

func CatalogCaliber(dataset string, identity string) ScopeCaliber {
	return ScopeCaliber{Scope: "catalog", Grain: "catalog_entry", IdentityBasis: identity, OutcomeBasis: "none", LatencyBasis: "none", CostBasis: "none", Datasets: []string{dataset}}
}

func ValidateGroupBy(scope, groupBy string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(groupBy))
	if normalized == "" {
		normalized = GroupNone
	}
	allowed := map[string]struct{}{GroupNone: {}, GroupAPIFamily: {}}
	switch scope {
	case ScopeIngress:
		allowed[GroupIngressModel] = struct{}{}
		allowed[GroupProxyAPIKey] = struct{}{}
	case ScopeFinal:
		allowed[GroupFinalTargetModel] = struct{}{}
		allowed[GroupEndpoint] = struct{}{}
		allowed[GroupTerminalTarget] = struct{}{}
	case ScopeRouteAttempt:
		allowed[GroupAttemptTargetModel] = struct{}{}
		allowed[GroupEndpoint] = struct{}{}
		allowed[GroupTerminalTarget] = struct{}{}
		allowed[GroupAttemptTrigger] = struct{}{}
		allowed[GroupAttemptResult] = struct{}{}
	default:
		return "", &HTTPError{StatusCode: 422, Code: "scope_invalid", Detail: fmt.Sprintf("unknown scope %q", scope)}
	}
	if _, ok := allowed[normalized]; !ok {
		return "", &HTTPError{StatusCode: 422, Code: "group_invalid", Detail: fmt.Sprintf("group_by %q not allowed for scope %q", groupBy, scope)}
	}
	return normalized, nil
}

func validateScopeFilter(scope string, filterKey string) error {
	common := map[string]struct{}{
		"scope": {}, "group_by": {}, "query_context": {}, "from_time": {}, "to_time": {}, "preset": {}, "limit": {}, "offset": {}, "top_n": {}, "interval": {}, "series_limit": {}, "metric": {}, "api_family": {},
	}
	if _, ok := common[filterKey]; ok {
		return nil
	}
	allowed := map[string]struct{}{}
	switch scope {
	case ScopeIngress:
		allowed["ingress_model_id"] = struct{}{}
		allowed["proxy_api_key_id"] = struct{}{}
	case ScopeFinal:
		allowed["final_target_model_id"] = struct{}{}
		allowed["endpoint_id"] = struct{}{}
		allowed["terminal_target_id"] = struct{}{}
	case ScopeRouteAttempt:
		allowed["attempt_target_model_id"] = struct{}{}
		allowed["endpoint_id"] = struct{}{}
		allowed["terminal_target_id"] = struct{}{}
		allowed["attempt_trigger"] = struct{}{}
		allowed["attempt_result"] = struct{}{}
	default:
		return &HTTPError{StatusCode: 422, Code: "scope_invalid", Detail: fmt.Sprintf("unknown scope %q", scope)}
	}
	if _, ok := allowed[filterKey]; !ok {
		return &HTTPError{StatusCode: 422, Code: "filter_invalid", Detail: fmt.Sprintf("filter %q not allowed for scope %q", filterKey, scope)}
	}
	return nil
}

func ValidateScopeQueryKeys(scope string, keys []string) error {
	for _, key := range keys {
		if err := validateScopeFilter(scope, key); err != nil {
			return err
		}
	}
	return nil
}

func ResolveDatasetCoverage(ctx context.Context, exec queryExecutor, domain string, preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time) (QueryBounds, Coverage, error) {
	source, err := LoadRetentionSourceProjection(ctx, exec, domain, referenceNow.UTC())
	if err != nil {
		return QueryBounds{}, Coverage{}, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return QueryBounds{}, Coverage{}, &HTTPError{StatusCode: 503, Code: domain + "_purge_in_progress", Detail: domain + " is temporarily unavailable while retention cleanup is publishing"}
	}
	actual, err := LoadActualCoverageProjection(ctx, exec, source)
	if err != nil {
		return QueryBounds{}, Coverage{}, err
	}
	bounds, err := ResolveQueryBoundsFromActualCoverage(preset, fromTime, toTime, referenceNow.UTC(), source, actual)
	if err != nil {
		return QueryBounds{}, Coverage{}, err
	}
	snapshot := QueryContextDomainSnapshot{Domain: domain, FromTime: bounds.UsageFrom, ToTime: bounds.UsageTo, RetentionFromTime: bounds.UsageRetentionFrom, RetentionEpoch: source.RetentionEpoch, RetentionGeneration: source.RetentionGeneration, FenceGeneration: source.FenceGeneration, SourceRevision: source.SourceRevision, CoverageRevision: actual.Revision, CoverageHash: actual.Hash, CoverageGeneratedAt: actual.GeneratedAt, MaterializationCut: actual.MaterializationCut, Gaps: append([]CoverageGap(nil), bounds.Gaps...), Complete: actual.Complete && actual.Freshness == "fresh" && bounds.Complete, Freshness: actual.Freshness, PurgeState: source.PurgeState}
	return bounds, CoverageFromQueryBounds(bounds, snapshot), nil
}

func ScopeCoverageFor(scope string, usage *Coverage, requests *Coverage) DatasetCoverage {
	coverage := DatasetCoverage{}
	switch scope {
	case ScopeRouteAttempt:
		coverage.RequestLogs = requests
	case ScopeFinal:
		coverage.UsageRequestEvents = usage
		coverage.RequestLogs = requests
	default:
		coverage.UsageRequestEvents = usage
	}
	return coverage
}

func normalizePositiveID(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	resolved := *value
	return &resolved
}

func queryCoverageToCoveragePointer(value QueryCoverage) *Coverage {
	gaps := make([]CoverageGap, 0, len(value.Gaps))
	for _, gap := range value.Gaps {
		gaps = append(gaps, CoverageGap{FromTime: gap.FromTime, ToTime: gap.ToTime, Reason: gap.Reason})
	}
	return &Coverage{
		FromTime: value.EffectiveFromTime, ToTime: value.EffectiveToTime, RetentionFromTime: value.RetentionFromTime,
		Source: "raw", Complete: value.Complete, Gaps: gaps, RetentionEpoch: value.RetentionEpoch,
		RetentionGeneration: value.RetentionGeneration, PurgeState: value.PurgeState, SourceRevision: value.SourceRevision,
	}
}

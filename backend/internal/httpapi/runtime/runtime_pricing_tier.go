package runtime

import "strings"

const (
	runtimePricingTierNotEvaluated  = "not_evaluated"
	runtimePricingTierNotApplicable = "not_applicable"
	runtimePricingTierBase          = "base"
	runtimePricingTierAbove         = "tier"

	// The persisted basis is BIGINT. Keep the bound explicit even though the
	// normal provider token fields are INTEGER: malformed or synthetic usage
	// values must fail closed instead of wrapping before the comparison.
	runtimePricingTierBasisUpperBound int64 = 1<<63 - 1
)

type runtimePricingTierSelection struct {
	Kind       string
	Snapshot   *runtimePricingTemplateSnapshot
	Threshold  *int
	Basis      *int64
	Incoherent bool
}

// selectRuntimePricingTier is the only tier decision point. It consumes the
// disjoint usage components and compares the exact token basis before any
// price arithmetic or FX conversion. Count-token operations deliberately do
// not use the generation pricing basis even when their template has a tier.
func selectRuntimePricingTier(snapshot *runtimePricingTemplateSnapshot, usage responseUsage, operation string) runtimePricingTierSelection {
	if runtimePricingTierOperationIsTokenCount(operation) {
		return runtimePricingTierSelection{Kind: runtimePricingTierNotApplicable}
	}
	if usage.InputTokens == nil || usage.OutputTokens == nil {
		return runtimePricingTierSelection{Kind: runtimePricingTierNotEvaluated}
	}
	if snapshot == nil || snapshot.TierInputTokensAbove == nil {
		return runtimePricingTierSelection{Kind: runtimePricingTierNotApplicable}
	}

	basis, ok := runtimePricingTierBasisTokens(usage)
	if !ok {
		return runtimePricingTierSelection{Kind: runtimePricingTierNotEvaluated, Incoherent: true}
	}
	threshold := *snapshot.TierInputTokensAbove
	selection := runtimePricingTierSelection{
		Kind:      runtimePricingTierBase,
		Snapshot:  snapshot,
		Threshold: intPtr(threshold),
		Basis:     int64Ptr(basis),
	}
	if basis > int64(threshold) {
		selection.Kind = runtimePricingTierAbove
		selection.Snapshot = runtimePricingTierSnapshot(snapshot)
		selection.Snapshot.InputPrice = snapshot.TierInputPrice
		selection.Snapshot.OutputPrice = snapshot.TierOutputPrice
		selection.Snapshot.CachedInputPrice = snapshot.TierCachedInputPrice
		selection.Snapshot.CacheCreationPrice = snapshot.TierCacheCreationPrice
		selection.Snapshot.ReasoningPrice = snapshot.TierReasoningPrice
	}
	return selection
}

func runtimePricingTierOperationIsTokenCount(operation string) bool {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return false
	}
	for _, candidate := range RuntimeOperationCatalog() {
		if candidate.Name != operation && candidate.HookCollectionID != operation {
			continue
		}
		hooks, ok := responseHooksForOperation(candidate)
		return ok && hooks.Kind == operationResponseKindTokenCount
	}
	return false
}

func runtimePricingTierBasisTokens(usage responseUsage) (int64, bool) {
	var basis int64
	for _, value := range []*int{usage.InputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens} {
		if value == nil || *value == 0 {
			continue
		}
		if *value < 0 {
			return 0, false
		}
		part := int64(*value)
		if part > runtimePricingTierBasisUpperBound-basis {
			return 0, false
		}
		basis += part
	}
	return basis, true
}

func runtimePricingTierSnapshot(snapshot *runtimePricingTemplateSnapshot) *runtimePricingTemplateSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := *snapshot
	copy.ReportingCurrencyEpoch = cloneRuntimeIntPointer(snapshot.ReportingCurrencyEpoch)
	if snapshot.VersionEffectiveAt != nil {
		effectiveAt := snapshot.VersionEffectiveAt.UTC()
		copy.VersionEffectiveAt = &effectiveAt
	}
	copy.TierInputTokensAbove = cloneRuntimeIntPointer(snapshot.TierInputTokensAbove)
	return &copy
}

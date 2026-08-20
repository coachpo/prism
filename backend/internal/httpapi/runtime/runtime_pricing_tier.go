package runtime

import "strings"

const (
	// The persisted basis is BIGINT. Keep the bound explicit even though the
	// normal provider token fields are INTEGER: malformed or synthetic usage
	// values must fail closed instead of wrapping before the comparison.
	runtimePricingTierBasisUpperBound int64 = 1<<63 - 1
)

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

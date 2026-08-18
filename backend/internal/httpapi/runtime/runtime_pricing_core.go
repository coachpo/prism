package runtime

type runtimePricingComponentCosts struct {
	Input              int64
	Output             int64
	CacheReadInput     int64
	CacheCreationInput int64
	Reasoning          int64
	Total              int64
}

// calculateRuntimePricingComponentCosts is shared by the status-only fixture
// classifier and the production currency-aware builder. It keeps all five
// component calculations on the exact big.Rat path and sums only after each
// component has been rounded, matching the existing pricing contract.
func calculateRuntimePricingComponentCosts(snapshot *runtimePricingTemplateSnapshot, usage responseUsage) (runtimePricingComponentCosts, bool) {
	if snapshot == nil {
		return runtimePricingComponentCosts{}, false
	}
	input, ok := runtimePriceConcreteComponentMicros(usage.InputTokens, snapshot.InputPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	output, ok := runtimePriceConcreteComponentMicros(usage.OutputTokens, snapshot.OutputPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	cacheReadInput, ok := runtimePriceConcreteComponentMicros(usage.CacheReadInputTokens, snapshot.CachedInputPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	cacheCreationInput, ok := runtimePriceConcreteComponentMicros(usage.CacheCreationInputTokens, snapshot.CacheCreationPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	reasoning, ok := runtimePriceConcreteComponentMicros(usage.ReasoningTokens, snapshot.ReasoningPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	total, ok := runtimeCheckedSumMicros(input, output, cacheReadInput, cacheCreationInput, reasoning)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	return runtimePricingComponentCosts{
		Input:              input,
		Output:             output,
		CacheReadInput:     cacheReadInput,
		CacheCreationInput: cacheCreationInput,
		Reasoning:          reasoning,
		Total:              total,
	}, true
}

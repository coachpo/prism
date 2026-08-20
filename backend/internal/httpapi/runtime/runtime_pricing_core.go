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
func calculateRuntimePricingComponentCosts(card runtimePricingCard, usage responseUsage) (runtimePricingComponentCosts, bool) {
	input, ok := runtimePriceConcreteComponentMicros(usage.InputTokens, card.InputPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	output, ok := runtimePriceConcreteComponentMicros(usage.OutputTokens, card.OutputPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	cacheReadInput, ok := runtimePriceConcreteComponentMicros(usage.CacheReadInputTokens, card.CachedInputPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	cacheCreationInput, ok := runtimePriceConcreteComponentMicros(usage.CacheCreationInputTokens, card.CacheCreationPrice)
	if !ok {
		return runtimePricingComponentCosts{}, false
	}
	reasoning, ok := runtimePriceConcreteComponentMicros(usage.ReasoningTokens, card.ReasoningPrice)
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

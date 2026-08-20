package runtime

import "strings"

func buildLegacyRuntimePricingResult(reportCurrencySnapshot runtimeReportCurrencySnapshot, pricingTemplateSnapshot *runtimePricingTemplateSnapshot, endpointFXSnapshot *runtimeEndpointFXSnapshot, usage responseUsage, streamOutcome string, operation string) runtimePricingResult {
	result := runtimePricingResult{Billable: true, PricingEvidenceTrust: runtimePricingEvidenceTrust}
	result.PricingTemplateIDUsed = templateIDPointer(pricingTemplateSnapshot)
	result.PricingTemplateNameSnapshot = templateNamePointer(pricingTemplateSnapshot)
	result.PricingTemplateRevisionIDUsed = templateRevisionIDPointer(pricingTemplateSnapshot)
	if pricingTemplateSnapshot != nil && pricingTemplateSnapshot.VersionEffectiveAt != nil {
		effective := pricingTemplateSnapshot.VersionEffectiveAt.UTC()
		result.PricingVersionEffectiveAt = &effective
	}
	result.ReportingCurrencyEpoch = nonZeroIntPointer(reportCurrencySnapshot.Epoch)
	if pricingTemplateSnapshot == nil {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonPricingOff)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	if !runtimePricingSnapshotUsableForReady(pricingTemplateSnapshot) {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionCurrencyMigrationRequired)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	selection := selectRuntimePricingTier(pricingTemplateSnapshot, usage, operation)
	if selection.Incoherent {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	pricingCard := selection.Snapshot
	if pricingCard == nil {
		pricingCard = pricingTemplateSnapshot
	}
	if !runtimePricingUsageCompleteForOperation(usage, operation) {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingUsage)
		if runtimeStreamOutcomeMakesUsageUnavailable(streamOutcome) {
			result.UnpricedReason = stringPtr(runtimeUnpricedReasonStreamUsageUnavailable)
		}
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	if strings.TrimSpace(pricingCard.PricingUnit) != runtimePricingUnitPerMillion {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionUnsupportedUnit)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	if !runtimePricingEpochCurrencyCoherent(reportCurrencySnapshot, pricingCard) {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	fxRate, fxSource, ok := resolveRuntimeFXRate(reportCurrencySnapshot, pricingCard, endpointFXSnapshot)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionCurrencyMigrationRequired)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	card := runtimePricingCard{InputPrice: pricingCard.InputPrice, OutputPrice: pricingCard.OutputPrice, CachedInputPrice: pricingCard.CachedInputPrice, CacheCreationPrice: pricingCard.CacheCreationPrice, ReasoningPrice: pricingCard.ReasoningPrice}
	missingComponents := missingPriceComponents(card, usage)
	if len(missingComponents) > 0 {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionMissingComponent)
		result.MissingPriceComponents = missingComponents
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	costs, ok := calculateRuntimePricingComponentCosts(card, usage)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	totalReportMicros, ok := runtimeConvertMicros(costs.Total, fxRate)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	result.Priced = true
	result.PricingStatus = runtimePricingStatusPriced
	result.InputCostMicros = int64Ptr(costs.Input)
	result.OutputCostMicros = int64Ptr(costs.Output)
	result.CacheReadInputCostMicros = int64Ptr(costs.CacheReadInput)
	result.CacheCreationInputCostMicros = int64Ptr(costs.CacheCreationInput)
	result.ReasoningCostMicros = int64Ptr(costs.Reasoning)
	result.TotalCostOriginalMicros = int64Ptr(costs.Total)
	result.TotalCostUserCurrencyMicros = int64Ptr(totalReportMicros)
	result.CurrencyCodeOriginal = runtimeOptionalTrimmedString(pricingCard.PricingCurrencyCode)
	result.ReportCurrencyCode = runtimeOptionalTrimmedString(reportCurrencySnapshot.Code)
	result.ReportCurrencySymbol = runtimeOptionalTrimmedString(reportCurrencySnapshot.Symbol)
	result.FXRateUsed = runtimeOptionalTrimmedString(fxRate)
	result.FXRateSource = runtimeOptionalTrimmedString(fxSource)
	result.PricingSnapshotUnit = runtimeOptionalTrimmedString(pricingCard.PricingUnit)
	result.PricingSnapshotInput = runtimeOptionalTrimmedString(pricingCard.InputPrice)
	result.PricingSnapshotOutput = runtimeOptionalTrimmedString(pricingCard.OutputPrice)
	result.PricingSnapshotCacheReadInput = runtimeOptionalTrimmedString(pricingCard.CachedInputPrice)
	result.PricingSnapshotCacheCreationInput = runtimeOptionalTrimmedString(pricingCard.CacheCreationPrice)
	result.PricingSnapshotReasoning = runtimeOptionalTrimmedString(pricingCard.ReasoningPrice)
	result.PricingConfigVersionUsed = intPtr(pricingCard.Version)
	return result
}

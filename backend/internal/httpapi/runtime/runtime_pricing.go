package runtime

import (
	"math/big"
	"net/http"
	"strings"
	"time"
)

const (
	runtimePricingUnitPerMillion                = "PER_1M"
	runtimeFXSourceEndpointSpecific             = "ENDPOINT_SPECIFIC"
	runtimeFXSourceDefaultOneToOne              = "DEFAULT_1_TO_1"
	runtimeUnpricedReasonPricingOff             = "PRICING_DISABLED"
	runtimeUnpricedReasonMissingData            = "MISSING_PRICE_DATA"
	runtimeUnpricedReasonMissingUsage           = "MISSING_TOKEN_USAGE"
	runtimeUnpricedReasonStreamUsageUnavailable = "STREAM_USAGE_UNAVAILABLE"
)

type runtimePricingResult struct {
	Billable                          bool
	Priced                            bool
	UnpricedReason                    *string
	InputCostMicros                   *int64
	OutputCostMicros                  *int64
	CacheReadInputCostMicros          *int64
	CacheCreationInputCostMicros      *int64
	ReasoningCostMicros               *int64
	TotalCostOriginalMicros           *int64
	TotalCostUserCurrencyMicros       *int64
	CurrencyCodeOriginal              *string
	ReportCurrencyCode                *string
	ReportCurrencySymbol              *string
	FXRateUsed                        *string
	FXRateSource                      *string
	PricingSnapshotUnit               *string
	PricingSnapshotInput              *string
	PricingSnapshotOutput             *string
	PricingSnapshotCacheReadInput     *string
	PricingSnapshotCacheCreationInput *string
	PricingSnapshotReasoning          *string
	PricingConfigVersionUsed          *int

	// Pricing cost-trust v2 (Pricing SPEC §3.2-§3.4/§6.4): the four-state
	// classifier, resolution kind, canonical missing components, evidence
	// trust (new writer always trusted), and immutable template/revision
	// identity snapshots.
	PricingStatus                 string
	PricingResolutionKind         *string
	MissingPriceComponents        []string
	PricingEvidenceTrust          string
	PricingTemplateIDUsed         *int
	PricingTemplateNameSnapshot   *string
	PricingTemplateRevisionIDUsed *int64
	PricingVersionEffectiveAt     *time.Time
	ReportingCurrencyEpoch        *int
}

// classifyRuntimePricing is the status-only entry point used by the pricing
// contract fixtures and read-model reconciliation. Runtime execution uses the
// richer builder below so it can include endpoint FX and request-time
// snapshots; both paths share the same pricing arithmetic.
func classifyRuntimePricing(finalHTTPStatus int, reportCurrency runtimeReportCurrencySnapshot, pricingTemplateSnapshot *runtimePricingTemplateSnapshot, usage responseUsage, streamOutcome string) runtimePricingResult {
	result := runtimePricingResult{
		Billable:             true,
		PricingEvidenceTrust: runtimePricingEvidenceTrust,
		ReportCurrencyCode:   runtimeOptionalTrimmedString(reportCurrency.Code),
		ReportCurrencySymbol: runtimeOptionalTrimmedString(reportCurrency.Symbol),
	}
	if reportCurrency.Epoch >= 1 {
		epoch := reportCurrency.Epoch
		result.ReportingCurrencyEpoch = &epoch
	}
	if finalHTTPStatus < http.StatusOK || finalHTTPStatus >= http.StatusMultipleChoices {
		result.PricingStatus = runtimePricingStatusIneligible
		result.Priced = false
		return result
	}
	if pricingTemplateSnapshot == nil {
		result.PricingStatus = runtimePricingStatusUnpriced
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonPricingOff)
		return result
	}
	if !runtimePricingSnapshotUsableForReady(pricingTemplateSnapshot) {
		result.PricingStatus = runtimePricingStatusUnpriced
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionCurrencyMigrationRequired)
		return result
	}
	if usage.InputTokens == nil || usage.OutputTokens == nil {
		if runtimeStreamOutcomeMakesUsageUnavailable(streamOutcome) {
			result.PricingStatus = runtimePricingStatusUnpriced
			result.UnpricedReason = stringPtr(runtimeUnpricedReasonStreamUsageUnavailable)
			return result
		}
		result.PricingStatus = runtimePricingStatusUnpriced
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingUsage)
		return result
	}
	if strings.TrimSpace(pricingTemplateSnapshot.PricingUnit) != runtimePricingUnitPerMillion {
		result.PricingStatus = runtimePricingStatusUnpriced
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionUnsupportedUnit)
		return result
	}
	if !runtimePricingEpochCurrencyCoherent(reportCurrency, pricingTemplateSnapshot) {
		result.PricingStatus = runtimePricingStatusUnpriced
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		return result
	}
	missing := runtimePricingMissingComponents(pricingTemplateSnapshot, usage)
	if len(missing) > 0 {
		result.PricingStatus = runtimePricingStatusUnpriced
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionMissingComponent)
		result.MissingPriceComponents = missing
		return result
	}
	inputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.InputTokens, pricingTemplateSnapshot.InputPrice)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	outputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.OutputTokens, pricingTemplateSnapshot.OutputPrice)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	cacheReadInputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.CacheReadInputTokens, pricingTemplateSnapshot.CachedInputPrice)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	cacheCreationInputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.CacheCreationInputTokens, pricingTemplateSnapshot.CacheCreationPrice)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	reasoningCostMicros, ok := runtimePriceConcreteComponentMicros(usage.ReasoningTokens, pricingTemplateSnapshot.ReasoningPrice)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	totalOriginalMicros, ok := runtimeCheckedSumMicros(inputCostMicros, outputCostMicros, cacheReadInputCostMicros, cacheCreationInputCostMicros, reasoningCostMicros)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	result.PricingStatus = runtimePricingStatusPriced
	result.Priced = true
	result.InputCostMicros = int64Ptr(inputCostMicros)
	result.OutputCostMicros = int64Ptr(outputCostMicros)
	result.CacheReadInputCostMicros = int64Ptr(cacheReadInputCostMicros)
	result.CacheCreationInputCostMicros = int64Ptr(cacheCreationInputCostMicros)
	result.ReasoningCostMicros = int64Ptr(reasoningCostMicros)
	result.TotalCostOriginalMicros = int64Ptr(totalOriginalMicros)
	result.TotalCostUserCurrencyMicros = int64Ptr(totalOriginalMicros)
	result.CurrencyCodeOriginal = runtimeOptionalTrimmedString(pricingTemplateSnapshot.PricingCurrencyCode)
	result.FXRateUsed = stringPtr("1")
	result.FXRateSource = stringPtr(runtimeFXSourceDefaultOneToOne)
	return result
}

// runtimePricingStatus values (Pricing SPEC §3.2).
const (
	runtimePricingStatusPriced     = "priced"
	runtimePricingStatusUnpriced   = "unpriced"
	runtimePricingStatusIneligible = "ineligible"
	runtimePricingStatusUnknown    = "unknown"
)

// runtimePricingResolutionKind values (Pricing SPEC §3.3).
const (
	runtimePricingResolutionMissingComponent          = "missing_component"
	runtimePricingResolutionCurrencyMigrationRequired = "currency_migration_required"
	runtimePricingResolutionUnsupportedUnit           = "unsupported_unit"
	runtimePricingResolutionSnapshotIncoherent        = "snapshot_incoherent"
)

// runtimePricingEvidenceTrust values (Pricing SPEC §6.4); new writer only
// produces trusted.
const runtimePricingEvidenceTrust = "trusted"

func runtimePricingSnapshotUsableForReady(snapshot *runtimePricingTemplateSnapshot) bool {
	return snapshot != nil && snapshot.RevisionID > 0 && snapshot.ReportingCurrencyEpoch != nil
}

func runtimePricingEpochCurrencyCoherent(reportCurrency runtimeReportCurrencySnapshot, snapshot *runtimePricingTemplateSnapshot) bool {
	if snapshot == nil || reportCurrency.Epoch < 1 || snapshot.ReportingCurrencyEpoch == nil {
		return false
	}
	return strings.TrimSpace(reportCurrency.Code) != "" && strings.TrimSpace(reportCurrency.Code) == strings.TrimSpace(snapshot.PricingCurrencyCode) && *snapshot.ReportingCurrencyEpoch == reportCurrency.Epoch
}

func runtimePricingMissingComponents(snapshot *runtimePricingTemplateSnapshot, usage responseUsage) []string {
	if snapshot == nil {
		return nil
	}
	missing := make([]string, 0, 5)
	appendMissing := func(component string, tokens *int, price string) {
		if tokens != nil && *tokens > 0 {
			if _, ok := parseRuntimeDecimalRat(price); !ok {
				missing = append(missing, component)
			}
		}
	}
	appendMissing("input_price", usage.InputTokens, snapshot.InputPrice)
	appendMissing("output_price", usage.OutputTokens, snapshot.OutputPrice)
	appendMissing("cached_input_price", usage.CacheReadInputTokens, snapshot.CachedInputPrice)
	appendMissing("cache_creation_price", usage.CacheCreationInputTokens, snapshot.CacheCreationPrice)
	appendMissing("reasoning_price", usage.ReasoningTokens, snapshot.ReasoningPrice)
	return missing
}

func runtimeSnapshotIncoherentPricingResult(result runtimePricingResult) runtimePricingResult {
	result.PricingStatus = runtimePricingStatusUnpriced
	result.Priced = false
	result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
	result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
	result.clearRuntimePricingCosts()
	return result
}

func runtimeCheckedSumMicros(values ...int64) (int64, bool) {
	var total int64
	for _, value := range values {
		if (value > 0 && total > int64(^uint64(0)>>1)-value) || (value < 0 && total < -int64(^uint64(0)>>1)-1-value) {
			return 0, false
		}
		total += value
	}
	return total, true
}

func buildRuntimePricingProvenance(reportCurrencySnapshot runtimeReportCurrencySnapshot, pricingTemplateSnapshot *runtimePricingTemplateSnapshot) runtimePricingResult {
	result := runtimePricingResult{
		PricingStatus:          runtimePricingStatusIneligible,
		PricingEvidenceTrust:   runtimePricingEvidenceTrust,
		ReportCurrencyCode:     runtimeOptionalTrimmedString(reportCurrencySnapshot.Code),
		ReportCurrencySymbol:   runtimeOptionalTrimmedString(reportCurrencySnapshot.Symbol),
		ReportingCurrencyEpoch: nonZeroIntPointer(reportCurrencySnapshot.Epoch),
	}
	if pricingTemplateSnapshot == nil {
		return result
	}
	result.PricingTemplateIDUsed = templateIDPointer(pricingTemplateSnapshot)
	result.PricingTemplateNameSnapshot = templateNamePointer(pricingTemplateSnapshot)
	result.PricingTemplateRevisionIDUsed = templateRevisionIDPointer(pricingTemplateSnapshot)
	if pricingTemplateSnapshot.VersionEffectiveAt != nil {
		effective := pricingTemplateSnapshot.VersionEffectiveAt.UTC()
		result.PricingVersionEffectiveAt = &effective
	}
	result.CurrencyCodeOriginal = runtimeOptionalTrimmedString(pricingTemplateSnapshot.PricingCurrencyCode)
	result.PricingSnapshotUnit = runtimeOptionalTrimmedString(pricingTemplateSnapshot.PricingUnit)
	result.PricingSnapshotInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.InputPrice)
	result.PricingSnapshotOutput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.OutputPrice)
	result.PricingSnapshotCacheReadInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.CachedInputPrice)
	result.PricingSnapshotCacheCreationInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.CacheCreationPrice)
	result.PricingSnapshotReasoning = runtimeOptionalTrimmedString(pricingTemplateSnapshot.ReasoningPrice)
	result.PricingConfigVersionUsed = intPtr(pricingTemplateSnapshot.Version)
	return result
}

func buildRuntimePricingResult(reportCurrencySnapshot runtimeReportCurrencySnapshot, pricingTemplateSnapshot *runtimePricingTemplateSnapshot, endpointFXSnapshot *runtimeEndpointFXSnapshot, usage responseUsage, streamOutcome string) runtimePricingResult {
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
	if usage.InputTokens == nil || usage.OutputTokens == nil {
		if runtimeStreamOutcomeMakesUsageUnavailable(streamOutcome) {
			result.UnpricedReason = stringPtr(runtimeUnpricedReasonStreamUsageUnavailable)
			result.PricingStatus = runtimePricingStatusUnpriced
			return result
		}
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingUsage)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}

	pricingUnit := strings.TrimSpace(pricingTemplateSnapshot.PricingUnit)
	if pricingUnit != runtimePricingUnitPerMillion {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionUnsupportedUnit)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	if !runtimePricingEpochCurrencyCoherent(reportCurrencySnapshot, pricingTemplateSnapshot) {
		return runtimeSnapshotIncoherentPricingResult(result)
	}

	fxRate, fxSource, ok := resolveRuntimeFXRate(reportCurrencySnapshot, pricingTemplateSnapshot, endpointFXSnapshot)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionCurrencyMigrationRequired)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}

	missingComponents := missingPriceComponents(pricingTemplateSnapshot, usage)
	if len(missingComponents) > 0 {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionMissingComponent)
		result.MissingPriceComponents = missingComponents
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}

	inputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.InputTokens, pricingTemplateSnapshot.InputPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	outputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.OutputTokens, pricingTemplateSnapshot.OutputPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	cacheReadInputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.CacheReadInputTokens, pricingTemplateSnapshot.CachedInputPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	cacheCreationInputCostMicros, ok := runtimePriceConcreteComponentMicros(usage.CacheCreationInputTokens, pricingTemplateSnapshot.CacheCreationPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}
	reasoningCostMicros, ok := runtimePriceConcreteComponentMicros(usage.ReasoningTokens, pricingTemplateSnapshot.ReasoningPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}

	totalOriginalMicros, ok := runtimeCheckedSumMicros(inputCostMicros, outputCostMicros, cacheReadInputCostMicros, cacheCreationInputCostMicros, reasoningCostMicros)
	if !ok {
		return runtimeSnapshotIncoherentPricingResult(result)
	}
	totalReportMicros, ok := runtimeConvertMicros(totalOriginalMicros, fxRate)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		result.PricingResolutionKind = stringPtr(runtimePricingResolutionSnapshotIncoherent)
		result.PricingStatus = runtimePricingStatusUnpriced
		return result
	}

	result.Priced = true
	result.PricingStatus = runtimePricingStatusPriced
	result.InputCostMicros = int64Ptr(inputCostMicros)
	result.OutputCostMicros = int64Ptr(outputCostMicros)
	result.CacheReadInputCostMicros = int64Ptr(cacheReadInputCostMicros)
	result.CacheCreationInputCostMicros = int64Ptr(cacheCreationInputCostMicros)
	result.ReasoningCostMicros = int64Ptr(reasoningCostMicros)
	result.TotalCostOriginalMicros = int64Ptr(totalOriginalMicros)
	result.TotalCostUserCurrencyMicros = int64Ptr(totalReportMicros)
	result.CurrencyCodeOriginal = runtimeOptionalTrimmedString(pricingTemplateSnapshot.PricingCurrencyCode)
	result.ReportCurrencyCode = runtimeOptionalTrimmedString(reportCurrencySnapshot.Code)
	result.ReportCurrencySymbol = runtimeOptionalTrimmedString(reportCurrencySnapshot.Symbol)
	result.FXRateUsed = runtimeOptionalTrimmedString(fxRate)
	result.FXRateSource = runtimeOptionalTrimmedString(fxSource)
	result.PricingSnapshotUnit = runtimeOptionalTrimmedString(pricingTemplateSnapshot.PricingUnit)
	result.PricingSnapshotInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.InputPrice)
	result.PricingSnapshotOutput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.OutputPrice)
	result.PricingSnapshotCacheReadInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.CachedInputPrice)
	result.PricingSnapshotCacheCreationInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.CacheCreationPrice)
	result.PricingSnapshotReasoning = runtimeOptionalTrimmedString(pricingTemplateSnapshot.ReasoningPrice)
	result.PricingConfigVersionUsed = intPtr(pricingTemplateSnapshot.Version)
	return result
}

// missingPriceComponents returns the canonical-ordered missing component
// literals for a template+usage snapshot. Only components with observed
// positive tokens and an unparseable/absent price count as missing; a null
// specialty price (not configured) is not a missing component unless tokens
// were observed for it.
func missingPriceComponents(pricingTemplateSnapshot *runtimePricingTemplateSnapshot, usage responseUsage) []string {
	if pricingTemplateSnapshot == nil {
		return nil
	}
	components := make([]string, 0, 5)
	if usage.InputTokens != nil && *usage.InputTokens > 0 && !runtimePriceComponentConcrete(pricingTemplateSnapshot.InputPrice) {
		components = append(components, "input_price")
	}
	if usage.OutputTokens != nil && *usage.OutputTokens > 0 && !runtimePriceComponentConcrete(pricingTemplateSnapshot.OutputPrice) {
		components = append(components, "output_price")
	}
	if usage.CacheReadInputTokens != nil && *usage.CacheReadInputTokens > 0 && !runtimePriceComponentConcrete(pricingTemplateSnapshot.CachedInputPrice) {
		components = append(components, "cached_input_price")
	}
	if usage.CacheCreationInputTokens != nil && *usage.CacheCreationInputTokens > 0 && !runtimePriceComponentConcrete(pricingTemplateSnapshot.CacheCreationPrice) {
		components = append(components, "cache_creation_price")
	}
	if usage.ReasoningTokens != nil && *usage.ReasoningTokens > 0 && !runtimePriceComponentConcrete(pricingTemplateSnapshot.ReasoningPrice) {
		components = append(components, "reasoning_price")
	}
	return components
}

// runtimePriceComponentConcrete reports whether a price literal is a
// parseable concrete decimal usable for costing (empty and unparseable
// literals are not concrete).
func runtimePriceComponentConcrete(price string) bool {
	_, ok := runtimePriceConcreteComponentMicros(intPtr(1), price)
	return ok
}

func templateIDPointer(snapshot *runtimePricingTemplateSnapshot) *int {
	if snapshot == nil {
		return nil
	}
	return intPtr(snapshot.ID)
}

func templateNamePointer(snapshot *runtimePricingTemplateSnapshot) *string {
	if snapshot == nil || strings.TrimSpace(snapshot.Name) == "" {
		return nil
	}
	return stringPtr(strings.TrimSpace(snapshot.Name))
}

func templateRevisionIDPointer(snapshot *runtimePricingTemplateSnapshot) *int64 {
	if snapshot == nil || snapshot.RevisionID <= 0 {
		return nil
	}
	return int64Ptr(snapshot.RevisionID)
}

func nonZeroIntPointer(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func enforceRuntimeSpendCoherence(success bool, result runtimePricingResult) runtimePricingResult {
	result.UnpricedReason = runtimeOptionalTrimmedStringPointer(result.UnpricedReason)
	if !success || !result.Billable {
		return result
	}
	if result.UnpricedReason != nil {
		result.Priced = false
		result.FXRateUsed = nil
		result.FXRateSource = nil
		return result
	}
	if result.TotalCostUserCurrencyMicros != nil {
		result.Priced = true
		result.FXRateUsed, result.FXRateSource = coherentRuntimeFXSnapshot(result)
		return result
	}
	if result.Priced {
		result.Priced = false
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
	}
	result.FXRateUsed = nil
	result.FXRateSource = nil
	return result
}

func (result *runtimePricingResult) clearRuntimePricingCosts() {
	result.InputCostMicros = nil
	result.OutputCostMicros = nil
	result.CacheReadInputCostMicros = nil
	result.CacheCreationInputCostMicros = nil
	result.ReasoningCostMicros = nil
	result.TotalCostOriginalMicros = nil
	result.TotalCostUserCurrencyMicros = nil
}

func coherentRuntimeFXSnapshot(result runtimePricingResult) (*string, *string) {
	normalizedRate := runtimeOptionalTrimmedStringPointer(result.FXRateUsed)
	normalizedSource := runtimeOptionalTrimmedStringPointer(result.FXRateSource)
	if normalizedRate != nil || normalizedSource != nil {
		return normalizedRate, normalizedSource
	}
	normalizedOriginalCurrency := runtimeOptionalTrimmedStringPointer(result.CurrencyCodeOriginal)
	normalizedReportCurrency := runtimeOptionalTrimmedStringPointer(result.ReportCurrencyCode)
	if normalizedOriginalCurrency != nil && normalizedReportCurrency != nil && *normalizedOriginalCurrency == *normalizedReportCurrency {
		return stringPtr("1"), stringPtr(runtimeFXSourceDefaultOneToOne)
	}
	return nil, nil
}

func runtimeStreamOutcomeMakesUsageUnavailable(outcome string) bool {
	switch strings.TrimSpace(outcome) {
	case runtimeStreamOutcomeProviderIncomplete, runtimeStreamOutcomeClientDisconnected, runtimeStreamOutcomeUpstreamReadError, runtimeStreamOutcomeUpstreamEndedWithoutTerminal, runtimeStreamOutcomeUnknown:
		return true
	default:
		return false
	}
}

func resolveRuntimeFXRate(reportCurrencySnapshot runtimeReportCurrencySnapshot, pricingTemplateSnapshot *runtimePricingTemplateSnapshot, endpointFXSnapshot *runtimeEndpointFXSnapshot) (string, string, bool) {
	reportCurrencyCode := strings.TrimSpace(reportCurrencySnapshot.Code)
	pricingCurrencyCode := strings.TrimSpace(pricingTemplateSnapshot.PricingCurrencyCode)
	if reportCurrencyCode == "" || pricingCurrencyCode == "" {
		return "", "", false
	}
	if reportCurrencyCode == pricingCurrencyCode {
		return "1", runtimeFXSourceDefaultOneToOne, true
	}
	if endpointFXSnapshot == nil {
		return "", "", false
	}
	fxRate := strings.TrimSpace(endpointFXSnapshot.FXRate)
	if _, ok := parseRuntimeDecimalRat(fxRate); !ok {
		return "", "", false
	}
	return fxRate, runtimeFXSourceEndpointSpecific, true
}

func runtimePriceConcreteComponentMicros(tokens *int, price string) (int64, bool) {
	if tokens == nil || *tokens == 0 {
		return 0, true
	}
	priceRat, ok := parseRuntimeDecimalRat(price)
	if !ok {
		return 0, false
	}
	component := new(big.Rat).Mul(big.NewRat(int64(*tokens), 1), priceRat)
	return roundRuntimeRatToInt64(component)
}

func runtimeConvertMicros(originalMicros int64, fxRate string) (int64, bool) {
	fxRateRat, ok := parseRuntimeDecimalRat(fxRate)
	if !ok {
		return 0, false
	}
	converted := new(big.Rat).Mul(big.NewRat(originalMicros, 1), fxRateRat)
	return roundRuntimeRatToInt64(converted)
}

func parseRuntimeDecimalRat(value string) (*big.Rat, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, false
	}
	parsed, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return nil, false
	}
	return parsed, true
}

func roundRuntimeRatToInt64(value *big.Rat) (int64, bool) {
	if value == nil || value.Denom().Sign() == 0 {
		return 0, false
	}
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)

	absDoubleRemainder := new(big.Int).Abs(remainder)
	absDoubleRemainder.Mul(absDoubleRemainder, big.NewInt(2))
	absDenominator := new(big.Int).Abs(value.Denom())
	if absDoubleRemainder.Cmp(absDenominator) >= 0 {
		if value.Sign() >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		} else {
			quotient.Sub(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0, false
	}
	return quotient.Int64(), true
}

func runtimeOptionalTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return stringPtr(trimmed)
}

func runtimeOptionalTrimmedStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return runtimeOptionalTrimmedString(*value)
}

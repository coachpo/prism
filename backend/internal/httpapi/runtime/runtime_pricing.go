package runtime

import (
	"math/big"
	"strings"
)

const (
	runtimePricingUnitPerMillion      = "PER_1M"
	runtimeFXSourceEndpointSpecific   = "ENDPOINT_SPECIFIC"
	runtimeFXSourceDefaultOneToOne    = "DEFAULT_1_TO_1"
	runtimeUnpricedReasonPricingOff   = "PRICING_DISABLED"
	runtimeUnpricedReasonMissingData  = "MISSING_PRICE_DATA"
	runtimeUnpricedReasonMissingUsage = "MISSING_TOKEN_USAGE"
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
}

func buildRuntimePricingResult(reportCurrencySnapshot runtimeReportCurrencySnapshot, pricingTemplateSnapshot *runtimePricingTemplateSnapshot, endpointFXSnapshot *runtimeEndpointFXSnapshot, usage responseUsage) runtimePricingResult {
	result := runtimePricingResult{Billable: true}
	if pricingTemplateSnapshot == nil {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonPricingOff)
		return result
	}

	pricingUnit := strings.TrimSpace(pricingTemplateSnapshot.PricingUnit)
	if pricingUnit != runtimePricingUnitPerMillion {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}
	if usage.InputTokens == nil || usage.OutputTokens == nil {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingUsage)
		return result
	}

	fxRate, fxSource, ok := resolveRuntimeFXRate(reportCurrencySnapshot, pricingTemplateSnapshot, endpointFXSnapshot)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}

	inputCostMicros, ok := runtimePriceComponentMicros(usage.InputTokens, pricingTemplateSnapshot.InputPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}
	outputCostMicros, ok := runtimePriceComponentMicros(usage.OutputTokens, pricingTemplateSnapshot.OutputPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}
	cacheReadInputCostMicros, ok := runtimePriceOptionalComponentMicros(usage.CacheReadInputTokens, pricingTemplateSnapshot.CachedInputPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}
	cacheCreationInputCostMicros, ok := runtimePriceOptionalComponentMicros(usage.CacheCreationInputTokens, pricingTemplateSnapshot.CacheCreationPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}
	reasoningCostMicros, ok := runtimePriceOptionalComponentMicros(usage.ReasoningTokens, pricingTemplateSnapshot.ReasoningPrice)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}

	totalOriginalMicros := runtimeSumMicros(inputCostMicros, outputCostMicros, cacheReadInputCostMicros, cacheCreationInputCostMicros, reasoningCostMicros)
	totalReportMicros, ok := runtimeConvertMicros(totalOriginalMicros, fxRate)
	if !ok {
		result.UnpricedReason = stringPtr(runtimeUnpricedReasonMissingData)
		return result
	}

	result.Priced = true
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
	result.PricingSnapshotCacheReadInput = runtimeOptionalOptionalTrimmedString(pricingTemplateSnapshot.CachedInputPrice)
	result.PricingSnapshotCacheCreationInput = runtimeOptionalOptionalTrimmedString(pricingTemplateSnapshot.CacheCreationPrice)
	result.PricingSnapshotReasoning = runtimeOptionalOptionalTrimmedString(pricingTemplateSnapshot.ReasoningPrice)
	result.PricingConfigVersionUsed = intPtr(pricingTemplateSnapshot.Version)
	return result
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

func runtimePriceComponentMicros(tokens *int, price string) (int64, bool) {
	if tokens == nil {
		return 0, false
	}
	priceRat, ok := parseRuntimeDecimalRat(price)
	if !ok {
		return 0, false
	}
	component := new(big.Rat).Mul(big.NewRat(int64(*tokens), 1), priceRat)
	return roundRuntimeRatToInt64(component)
}

func runtimePriceOptionalComponentMicros(tokens *int, price *string) (int64, bool) {
	if tokens == nil {
		return 0, true
	}
	if *tokens == 0 {
		return 0, true
	}
	if price == nil {
		return 0, false
	}
	return runtimePriceComponentMicros(tokens, *price)
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

func runtimeSumMicros(values ...int64) int64 {
	total := int64(0)
	for _, value := range values {
		total += value
	}
	return total
}

func runtimeOptionalTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return stringPtr(trimmed)
}

func runtimeOptionalOptionalTrimmedString(value *string) *string {
	if value == nil {
		return nil
	}
	return runtimeOptionalTrimmedString(*value)
}

package connections

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var pricingTemplateDecimalPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

type pricingTemplatePrices struct {
	InputPrice         string
	OutputPrice        string
	CachedInputPrice   *string
	CacheCreationPrice *string
	ReasoningPrice     *string
	Tier               *pricingTemplateTier
}

func normalizePricingTemplateTier(input *pricingTemplateTierInput, base pricingTemplatePrices) (*pricingTemplateTier, error) {
	if input == nil {
		return nil, nil
	}
	if input.InputTokensAbove == nil {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "tier.input_tokens_above is required"}
	}
	if *input.InputTokensAbove < 1 || int64(*input.InputTokensAbove) > int64(1<<31-1) {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "tier.input_tokens_above must be a positive 32-bit integer"}
	}
	inputPrice, err := normalizeRequiredPricingDecimalString("tier.input_price", input.InputPrice)
	if err != nil {
		return nil, err
	}
	outputPrice, err := normalizeRequiredPricingDecimalString("tier.output_price", input.OutputPrice)
	if err != nil {
		return nil, err
	}
	cachedPrice, err := normalizeOptionalPricingDecimalString("tier.cached_input_price", input.CachedInputPrice)
	if err != nil {
		return nil, err
	}
	cacheCreationPrice, err := normalizeOptionalPricingDecimalString("tier.cache_creation_price", input.CacheCreationPrice)
	if err != nil {
		return nil, err
	}
	reasoningPrice, err := normalizeOptionalPricingDecimalString("tier.reasoning_price", input.ReasoningPrice)
	if err != nil {
		return nil, err
	}
	if (base.CachedInputPrice == nil) != (cachedPrice == nil) {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "tier.cached_input_price must mirror the base price configuration"}
	}
	if (base.CacheCreationPrice == nil) != (cacheCreationPrice == nil) {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "tier.cache_creation_price must mirror the base price configuration"}
	}
	if (base.ReasoningPrice == nil) != (reasoningPrice == nil) {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "tier.reasoning_price must mirror the base price configuration"}
	}
	return &pricingTemplateTier{
		InputTokensAbove:   *input.InputTokensAbove,
		InputPrice:         inputPrice,
		OutputPrice:        outputPrice,
		CachedInputPrice:   cachedPrice,
		CacheCreationPrice: cacheCreationPrice,
		ReasoningPrice:     reasoningPrice,
	}, nil
}

func pricingTemplateTierFromResponse(tier *pricingTemplateTier) *pricingTemplateTierInput {
	if tier == nil {
		return nil
	}
	return &pricingTemplateTierInput{
		InputTokensAbove:   intPtr(tier.InputTokensAbove),
		InputPrice:         stringPtr(tier.InputPrice),
		OutputPrice:        stringPtr(tier.OutputPrice),
		CachedInputPrice:   cloneString(tier.CachedInputPrice),
		CacheCreationPrice: cloneString(tier.CacheCreationPrice),
		ReasoningPrice:     cloneString(tier.ReasoningPrice),
	}
}

func pricingTemplateTierEqual(left, right *pricingTemplateTier) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.InputTokensAbove == right.InputTokensAbove &&
		left.InputPrice == right.InputPrice &&
		left.OutputPrice == right.OutputPrice &&
		stringsEqualPointers(left.CachedInputPrice, right.CachedInputPrice) &&
		stringsEqualPointers(left.CacheCreationPrice, right.CacheCreationPrice) &&
		stringsEqualPointers(left.ReasoningPrice, right.ReasoningPrice)
}

func normalizePricingTemplatePrices(inputRaw *string, outputRaw *string, cachedRaw *string, cacheCreationRaw *string, reasoningRaw *string) (pricingTemplatePrices, error) {
	input, err := normalizeRequiredPricingDecimalString("input_price", inputRaw)
	if err != nil {
		return pricingTemplatePrices{}, err
	}
	output, err := normalizeRequiredPricingDecimalString("output_price", outputRaw)
	if err != nil {
		return pricingTemplatePrices{}, err
	}
	cached, err := normalizeOptionalPricingDecimalString("cached_input_price", cachedRaw)
	if err != nil {
		return pricingTemplatePrices{}, err
	}
	cacheCreation, err := normalizeOptionalPricingDecimalString("cache_creation_price", cacheCreationRaw)
	if err != nil {
		return pricingTemplatePrices{}, err
	}
	reasoning, err := normalizeOptionalPricingDecimalString("reasoning_price", reasoningRaw)
	if err != nil {
		return pricingTemplatePrices{}, err
	}
	return pricingTemplatePrices{InputPrice: input, OutputPrice: output, CachedInputPrice: cached, CacheCreationPrice: cacheCreation, ReasoningPrice: reasoning}, nil
}

// normalizeRequiredPricingDecimalString: missing/null/blank is a field-level
// 422 for base prices (SPEC 4.1); values are canonicalized per SPEC 4.2.
func normalizeRequiredPricingDecimalString(fieldName string, raw *string) (string, error) {
	if raw == nil {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s is required", fieldName)}
	}
	canonical, err := canonicalPricingDecimal(*raw)
	if err != nil {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s %s", fieldName, err.Error())}
	}
	return canonical, nil
}

// normalizeOptionalPricingDecimalString: explicit JSON null means
// "unconfigured"; missing or blank is a field-level 422 (SPEC 4.1). A null
// pointer passed here represents an explicit JSON null (the decoder keeps
// missing keys as nil too - the strict decoder covers presence separately).
func normalizeOptionalPricingDecimalString(fieldName string, raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s must not be empty; use null for unconfigured", fieldName)}
	}
	canonical, err := canonicalPricingDecimal(trimmed)
	if err != nil {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s %s", fieldName, err.Error())}
	}
	return stringPtr(canonical), nil
}

// canonicalPricingDecimal implements SPEC 4.2 canonicalization on the
// wire: ^\d+(\.\d+)?$, 1..20 chars, leading/trailing zeros removed, all
// numeric zeros collapse to "0".
func canonicalPricingDecimal(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("must be a non-negative decimal string")
	}
	if len(trimmed) > 20 || !pricingTemplateDecimalPattern.MatchString(trimmed) {
		return "", fmt.Errorf("must be a non-negative decimal string")
	}
	integral := trimmed
	fractional := ""
	if dot := strings.IndexByte(trimmed, '.'); dot >= 0 {
		integral = trimmed[:dot]
		fractional = trimmed[dot+1:]
	}
	integral = strings.TrimLeft(integral, "0")
	if integral == "" {
		integral = "0"
	}
	fractional = strings.TrimRight(fractional, "0")
	canonical := integral
	if fractional != "" {
		canonical = integral + "." + fractional
	}
	if len(canonical) > 20 {
		return "", fmt.Errorf("must be a non-negative decimal string")
	}
	return canonical, nil
}

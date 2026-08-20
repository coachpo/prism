package connections

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var pricingTemplateDecimalPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

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

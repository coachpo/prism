package settings

import (
	"encoding/hex"

	"fmt"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const currencyDraftChunkMaxRows = 100

func validateMigrationTargetForDraft(code string, symbol string) (string, string, error) {
	canonicalCode := canonicalCurrencyCode(code)
	if canonicalCode == "" {
		return "", "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "target_currency_code must be a canonical 3-letter uppercase ISO code"}
	}
	canonicalSymbol, valid := canonicalCurrencySymbol(symbol)
	if !valid {
		return "", "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "target_currency_symbol must be a canonical symbol"}
	}
	return canonicalCode, canonicalSymbol, nil
}

func normalizeCurrencyDraftChunk(rows []currencyMigrationDraftChunkRowRequest) ([]currencyMigrationDraftItem, string, error) {
	if len(rows) < 1 || len(rows) > currencyDraftChunkMaxRows {
		return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "currency migration chunk must contain 1-100 items"}
	}
	items := make([]currencyMigrationDraftItem, 0, len(rows))
	seen := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		if row.TemplateID < 1 || row.ExpectedVersion < 1 {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "template_id and expected_version must be positive"}
		}
		if _, ok := seen[row.TemplateID]; ok {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("duplicate template_id %d in chunk", row.TemplateID)}
		}
		seen[row.TemplateID] = struct{}{}
		kind := pricingkind.Kind(strings.TrimSpace(row.TemplateKind))
		if !kind.Valid() {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "template_kind must be standard, tiered, or peak_valley"}
		}
		expected, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(row.ExpectedUpdatedAt))
		if err != nil {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected_updated_at must be a valid RFC3339 timestamp"}
		}
		requiredRoles := pricingkind.RolesFor(kind)
		if len(row.Cards) != len(requiredRoles) {
			return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("template %d requires the complete %s card set", row.TemplateID, kind)}
		}
		required := make(map[string]struct{}, len(requiredRoles))
		for _, role := range requiredRoles {
			required[role] = struct{}{}
		}
		cards := make([]currencyMigrationCard, 0, len(row.Cards))
		roles := make(map[string]struct{}, len(row.Cards))
		var specialtyShape *[3]bool
		for _, input := range row.Cards {
			role := strings.TrimSpace(input.CardRole)
			if _, ok := required[role]; !ok {
				return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("template %d has invalid card role %q", row.TemplateID, role)}
			}
			if _, ok := roles[role]; ok {
				return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("template %d has duplicate card role %q", row.TemplateID, role)}
			}
			roles[role] = struct{}{}
			inputPrice, priceErr := canonicalCurrencyMigrationPrice(role+".input_price", input.InputPrice, false)
			if priceErr != nil {
				return nil, "", priceErr
			}
			outputPrice, priceErr := canonicalCurrencyMigrationPrice(role+".output_price", input.OutputPrice, false)
			if priceErr != nil {
				return nil, "", priceErr
			}
			cached, priceErr := canonicalCurrencyMigrationPrice(role+".cached_input_price", input.CachedInputPrice, true)
			if priceErr != nil {
				return nil, "", priceErr
			}
			creation, priceErr := canonicalCurrencyMigrationPrice(role+".cache_creation_price", input.CacheCreationPrice, true)
			if priceErr != nil {
				return nil, "", priceErr
			}
			reasoning, priceErr := canonicalCurrencyMigrationPrice(role+".reasoning_price", input.ReasoningPrice, true)
			if priceErr != nil {
				return nil, "", priceErr
			}
			shape := [3]bool{cached != "", creation != "", reasoning != ""}
			if specialtyShape == nil {
				specialtyShape = &shape
			} else if *specialtyShape != shape {
				return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("template %d cards must mirror specialty price configuration", row.TemplateID)}
			}
			cards = append(cards, currencyMigrationCard{CardRole: role, InputPrice: inputPrice, OutputPrice: outputPrice, CachedInputPrice: nullableCurrencyPricePtr(cached), CacheCreationPrice: nullableCurrencyPricePtr(creation), ReasoningPrice: nullableCurrencyPricePtr(reasoning)})
		}
		for _, role := range requiredRoles {
			if _, ok := roles[role]; !ok {
				return nil, "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("template %d is missing card role %q", row.TemplateID, role)}
			}
		}
		sortCurrencyMigrationCards(cards)
		items = append(items, currencyMigrationDraftItem{TemplateID: row.TemplateID, ExpectedVersion: row.ExpectedVersion, ExpectedUpdatedAt: expected.UTC().Format(time.RFC3339Nano), TemplateKind: kind, Cards: cards})
	}
	sortCurrencyDraftItems(items)
	return items, currencyDraftItemsHash(items), nil
}

func sortCurrencyMigrationCards(cards []currencyMigrationCard) {
	for i := 1; i < len(cards); i++ {
		for j := i; j > 0 && cards[j].CardRole < cards[j-1].CardRole; j-- {
			cards[j], cards[j-1] = cards[j-1], cards[j]
		}
	}
}

func currencyMigrationCardsHaveRoles(cards []currencyMigrationCard, kind pricingkind.Kind) bool {
	required := pricingkind.RolesFor(kind)
	if len(cards) != len(required) {
		return false
	}
	seen := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		seen[card.CardRole] = struct{}{}
	}
	for _, role := range required {
		if _, ok := seen[role]; !ok {
			return false
		}
	}
	return true
}

func nullableCurrencyPricePtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func canonicalCurrencyMigrationPrice(field string, raw *string, nullable bool) (string, error) {
	if raw == nil {
		if nullable {
			return "", nil
		}
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " is required and must not be null"}
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" || len(trimmed) > 20 {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
	}
	integral, fractional := trimmed, ""
	for i, ch := range trimmed {
		if ch == '.' {
			integral, fractional = trimmed[:i], trimmed[i+1:]
			break
		}
		if ch < '0' || ch > '9' {
			return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
		}
	}
	if integral == "" || strings.ContainsAny(fractional, "+-eE") {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
	}
	for _, ch := range fractional {
		if ch < '0' || ch > '9' {
			return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
		}
	}
	integral = strings.TrimLeft(integral, "0")
	if integral == "" {
		integral = "0"
	}
	fractional = strings.TrimRight(fractional, "0")
	canonical := integral
	if fractional != "" {
		canonical += "." + fractional
	}
	if len(canonical) > 20 {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: field + " must be a canonical non-negative decimal string"}
	}
	return canonical, nil
}

func normalizeUUIDV4(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", fmt.Errorf("invalid uuid")
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 || decoded[6]>>4 != 4 || decoded[8]&0xc0 != 0x80 {
		return "", fmt.Errorf("invalid uuidv4")
	}
	return value, nil
}

func canonicalPositiveDecimal(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != value {
		return "", fmt.Errorf("invalid positive decimal")
	}
	return strconv.FormatInt(parsed, 10), nil
}

func positiveDecimalPtr(value *int64) *string {
	if value == nil {
		return nil
	}
	result := strconv.FormatInt(*value, 10)
	return &result
}

func canonicalCurrencyCode(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 3 {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	for _, char := range upper {
		if char < 'A' || char > 'Z' {
			return ""
		}
	}
	return upper
}

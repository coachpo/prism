package connections

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

const pricingUnitPerMillion = "PER_1M"

var pricingTemplateDecimalPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

type pricingTemplateCreateRequest struct {
	Name                string  `json:"name"`
	Description         *string `json:"description"`
	PricingUnit         *string `json:"pricing_unit"`
	PricingCurrencyCode string  `json:"pricing_currency_code"`
	InputPrice          *string `json:"input_price"`
	OutputPrice         *string `json:"output_price"`
	CachedInputPrice    *string `json:"cached_input_price"`
	CacheCreationPrice  *string `json:"cache_creation_price"`
	ReasoningPrice      *string `json:"reasoning_price"`
}

type pricingTemplateUpdateRequest struct {
	ExpectedUpdatedAt   optionalString `json:"expected_updated_at"`
	Name                optionalString `json:"name"`
	Description         optionalString `json:"description"`
	PricingUnit         optionalString `json:"pricing_unit"`
	PricingCurrencyCode optionalString `json:"pricing_currency_code"`
	InputPrice          optionalString `json:"input_price"`
	OutputPrice         optionalString `json:"output_price"`
	CachedInputPrice    optionalString `json:"cached_input_price"`
	CacheCreationPrice  optionalString `json:"cache_creation_price"`
	ReasoningPrice      optionalString `json:"reasoning_price"`
}

func (s *Service) handleListPricingTemplates(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]pricingTemplateResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		return listPricingTemplates(r.Context(), tx, profile.ID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetPricingTemplate(w http.ResponseWriter, r *http.Request) {
	templateID, err := routeInt(r, "template_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		item, found, err := loadPricingTemplate(r.Context(), tx, profile.ID, templateID, false)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if !found {
			return pricingTemplateResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		return item, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreatePricingTemplate(w http.ResponseWriter, r *http.Request) {
	var requestBody pricingTemplateCreateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return pricingTemplateResponse{}, err
		}
		name, err := normalizePricingTemplateName(requestBody.Name)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if err := ensureUniquePricingTemplateName(r.Context(), tx, profile.ID, name, nil); err != nil {
			return pricingTemplateResponse{}, err
		}
		item, err := buildCreatedPricingTemplate(profile.ID, s.nowUTC(), requestBody)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		item.Name = name
		return insertPricingTemplate(r.Context(), tx, item)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdatePricingTemplate(w http.ResponseWriter, r *http.Request) {
	templateID, err := routeInt(r, "template_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody pricingTemplateUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return pricingTemplateResponse{}, err
		}
		current, found, err := loadPricingTemplate(r.Context(), tx, profile.ID, templateID, true)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if !found {
			return pricingTemplateResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		if err := validatePricingTemplateExpectedUpdatedAt(current.UpdatedAt, requestBody.ExpectedUpdatedAt); err != nil {
			return pricingTemplateResponse{}, err
		}
		next, err := buildUpdatedPricingTemplate(current, requestBody, s.nowUTC())
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if err := ensureUniquePricingTemplateName(r.Context(), tx, profile.ID, next.Name, &current.ID); err != nil {
			return pricingTemplateResponse{}, err
		}
		if err := updatePricingTemplate(r.Context(), tx, next); err != nil {
			return pricingTemplateResponse{}, err
		}
		updated, found, err := loadPricingTemplate(r.Context(), tx, profile.ID, templateID, false)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if !found {
			return pricingTemplateResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		return updated, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeletePricingTemplate(w http.ResponseWriter, r *http.Request) {
	templateID, err := routeInt(r, "template_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (deletedResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return deletedResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return deletedResponse{}, err
		}
		_, found, err := loadPricingTemplate(r.Context(), tx, profile.ID, templateID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		usageRows, err := listPricingTemplateConnectionUsageRows(r.Context(), tx, profile.ID, templateID)
		if err != nil {
			return deletedResponse{}, err
		}
		if len(usageRows) > 0 {
			connections := make([]pricingTemplateConnectionUsageItem, 0, len(usageRows))
			for _, row := range usageRows {
				connections = append(connections, pricingTemplateConnectionUsageItem{
					ConnectionID:   row.ConnectionID,
					ConnectionName: row.ConnectionName,
					ModelConfigID:  row.ModelConfigID,
					ModelID:        derefString(row.ModelID),
					EndpointID:     row.EndpointID,
					EndpointName:   derefString(row.EndpointName),
				})
			}
			return deletedResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: map[string]any{"message": "Cannot delete pricing template that is referenced by connections", "connections": connections}}
		}
		if err := deletePricingTemplate(r.Context(), tx, templateID); err != nil {
			return deletedResponse{}, err
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func buildCreatedPricingTemplate(profileID int, currentTime time.Time, requestBody pricingTemplateCreateRequest) (pricingTemplateResponse, error) {
	pricingUnit, err := normalizePricingUnit(requestBody.PricingUnit)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	currencyCode, err := normalizePricingCurrencyCode(requestBody.PricingCurrencyCode)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	inputPrice, err := normalizePricingDecimalString("input_price", requestBody.InputPrice)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	outputPrice, err := normalizePricingDecimalString("output_price", requestBody.OutputPrice)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	cachedInputPrice, err := normalizePricingDecimalString("cached_input_price", requestBody.CachedInputPrice)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	cacheCreationPrice, err := normalizePricingDecimalString("cache_creation_price", requestBody.CacheCreationPrice)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	reasoningPrice, err := normalizePricingDecimalString("reasoning_price", requestBody.ReasoningPrice)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	return pricingTemplateResponse{ProfileID: profileID, Description: normalizeOptionalTrimmedString(requestBody.Description), PricingUnit: pricingUnit, PricingCurrencyCode: currencyCode, InputPrice: inputPrice, OutputPrice: outputPrice, CachedInputPrice: cachedInputPrice, CacheCreationPrice: cacheCreationPrice, ReasoningPrice: reasoningPrice, Version: 1, CreatedAt: currentTime, UpdatedAt: currentTime}, nil
}

func buildUpdatedPricingTemplate(current pricingTemplateResponse, requestBody pricingTemplateUpdateRequest, currentTime time.Time) (pricingTemplateResponse, error) {
	next := current
	if requestBody.Name.Set {
		name, err := normalizeOptionalRequiredName("name", requestBody.Name.Value)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		next.Name = name
	}
	if requestBody.Description.Set {
		next.Description = normalizeOptionalTrimmedString(requestBody.Description.Value)
	}
	if requestBody.PricingUnit.Set {
		pricingUnit, err := normalizeOptionalRequiredPricingUnit(requestBody.PricingUnit.Value)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		next.PricingUnit = pricingUnit
	}
	if requestBody.PricingCurrencyCode.Set {
		currencyCode, err := normalizeOptionalRequiredPricingCurrencyCode(requestBody.PricingCurrencyCode.Value)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		next.PricingCurrencyCode = currencyCode
	}
	inputPrice, err := normalizePricingDecimalString("input_price", requestBody.InputPrice.Value)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	next.InputPrice = inputPrice
	outputPrice, err := normalizePricingDecimalString("output_price", requestBody.OutputPrice.Value)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	next.OutputPrice = outputPrice
	cachedInputPrice, err := normalizePricingDecimalString("cached_input_price", requestBody.CachedInputPrice.Value)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	next.CachedInputPrice = cachedInputPrice
	cacheCreationPrice, err := normalizePricingDecimalString("cache_creation_price", requestBody.CacheCreationPrice.Value)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	next.CacheCreationPrice = cacheCreationPrice
	reasoningPrice, err := normalizePricingDecimalString("reasoning_price", requestBody.ReasoningPrice.Value)
	if err != nil {
		return pricingTemplateResponse{}, err
	}
	next.ReasoningPrice = reasoningPrice
	if pricingTemplateVersionBumpRequired(current, next) {
		next.Version++
	}
	next.UpdatedAt = currentTime
	return next, nil
}

func pricingTemplateVersionBumpRequired(current pricingTemplateResponse, next pricingTemplateResponse) bool {
	return current.PricingUnit != next.PricingUnit ||
		current.PricingCurrencyCode != next.PricingCurrencyCode ||
		current.InputPrice != next.InputPrice ||
		current.OutputPrice != next.OutputPrice ||
		current.CachedInputPrice != next.CachedInputPrice ||
		current.CacheCreationPrice != next.CacheCreationPrice ||
		current.ReasoningPrice != next.ReasoningPrice
}

func validatePricingTemplateExpectedUpdatedAt(current time.Time, expected optionalString) error {
	if !expected.Set || expected.Value == nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*expected.Value))
	if err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "expected_updated_at must be a valid RFC3339 timestamp"}
	}
	if !current.UTC().Equal(parsed.UTC()) {
		return &domainError{StatusCode: http.StatusConflict, Detail: "Pricing template has changed. Please refresh and retry."}
	}
	return nil
}

func normalizePricingTemplateName(raw string) (string, error) {
	return normalizeOptionalRequiredName("name", &raw)
}

func normalizeOptionalRequiredName(fieldName string, raw *string) (string, error) {
	if raw == nil {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s is required", fieldName)}
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s must not be empty", fieldName)}
	}
	return trimmed, nil
}

func normalizePricingUnit(raw *string) (string, error) {
	if raw == nil {
		return pricingUnitPerMillion, nil
	}
	return normalizeOptionalRequiredPricingUnit(raw)
}

func normalizeOptionalRequiredPricingUnit(raw *string) (string, error) {
	if raw == nil {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "pricing_unit is required"}
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed != pricingUnitPerMillion {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "pricing_unit must be PER_1M"}
	}
	return trimmed, nil
}

func normalizePricingCurrencyCode(raw string) (string, error) {
	return normalizeOptionalRequiredPricingCurrencyCode(&raw)
}

func normalizeOptionalRequiredPricingCurrencyCode(raw *string) (string, error) {
	if raw == nil {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "pricing_currency_code is required"}
	}
	trimmed := strings.ToUpper(strings.TrimSpace(*raw))
	if len(trimmed) != 3 || strings.IndexFunc(trimmed, func(r rune) bool { return r < 'A' || r > 'Z' }) != -1 {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "pricing_currency_code must be a three-letter currency code"}
	}
	return trimmed, nil
}

func normalizePricingDecimalString(fieldName string, raw *string) (string, error) {
	if raw == nil {
		return "0", nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return "0", nil
	}
	if !pricingTemplateDecimalPattern.MatchString(trimmed) {
		return "", &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s must be a non-negative decimal string", fieldName)}
	}
	return trimmed, nil
}

func normalizeOptionalTrimmedString(raw *string) *string {
	if raw == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil
	}
	return stringPtr(trimmed)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

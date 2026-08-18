package connections

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
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

// pricingTemplateCreateRequest is the steady-state create schema (SPEC 4.1):
// the five price keys are all required; input/output must be non-null decimal
// strings, the three specialty prices use explicit JSON null for
// "unconfigured". pricing_unit and pricing_currency_code are never accepted:
// the active reporting-currency epoch owns the currency, and the unit is
// fixed to PER_1M.
type pricingTemplateTierInput struct {
	InputTokensAbove   *int    `json:"input_tokens_above"`
	InputPrice         *string `json:"input_price"`
	OutputPrice        *string `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

// UnmarshalJSON keeps tier object validation strict even when it is nested in
// a request whose outer decoder has already consumed the object boundary.
func (tier *pricingTemplateTierInput) UnmarshalJSON(data []byte) error {
	type plain pricingTemplateTierInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded plain
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*tier = pricingTemplateTierInput(decoded)
	return nil
}

type optionalPricingTemplateTier struct {
	Set   bool
	Value *pricingTemplateTierInput
}

func (tier *optionalPricingTemplateTier) UnmarshalJSON(data []byte) error {
	tier.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		tier.Value = nil
		return nil
	}
	var decoded pricingTemplateTierInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	tier.Value = &decoded
	return nil
}

type pricingTemplateTier struct {
	InputTokensAbove   int     `json:"input_tokens_above"`
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

type pricingTemplateCreateRequest struct {
	Name                string                    `json:"name"`
	Description         *string                   `json:"description"`
	InputPrice          *string                   `json:"input_price"`
	OutputPrice         *string                   `json:"output_price"`
	CachedInputPrice    *string                   `json:"cached_input_price"`
	CacheCreationPrice  *string                   `json:"cache_creation_price"`
	ReasoningPrice      *string                   `json:"reasoning_price"`
	Tier                *pricingTemplateTierInput `json:"tier"`
	PricingUnit         *string                   `json:"pricing_unit"`
	PricingCurrencyCode *string                   `json:"pricing_currency_code"`
}

type pricingTemplateUpdateRequest struct {
	ExpectedUpdatedAt   optionalString              `json:"expected_updated_at"`
	Name                optionalString              `json:"name"`
	Description         optionalString              `json:"description"`
	InputPrice          optionalString              `json:"input_price"`
	OutputPrice         optionalString              `json:"output_price"`
	CachedInputPrice    optionalString              `json:"cached_input_price"`
	CacheCreationPrice  optionalString              `json:"cache_creation_price"`
	ReasoningPrice      optionalString              `json:"reasoning_price"`
	Tier                optionalPricingTemplateTier `json:"tier"`
	PricingUnit         optionalString              `json:"pricing_unit"`
	PricingCurrencyCode optionalString              `json:"pricing_currency_code"`
}

func (s *Service) handleListPricingTemplates(w http.ResponseWriter, r *http.Request) {
	includeSetupReadiness := false
	expectedGeneration := ""
	for _, value := range r.URL.Query()["include"] {
		if strings.TrimSpace(value) == "setup_readiness" {
			includeSetupReadiness = true
		}
	}
	if includeSetupReadiness {
		expectedGeneration = strings.TrimSpace(r.URL.Query().Get("expected_route_witness_generation"))
		if expectedGeneration == "" {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "expected_route_witness_generation is required with include=setup_readiness")
			return
		}
		response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (PricingSetupReadiness, error) {
			profile, err := resolveEffectiveProfile(r.Context(), tx, r)
			if err != nil {
				return PricingSetupReadiness{}, err
			}
			return s.buildPricingSetupReadiness(r.Context(), tx, profile.ID, expectedGeneration)
		})
		if err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
		responseutil.WriteJSON(w, http.StatusOK, response)
		return
	}
	if r.URL.Query().Has("limit") || strings.TrimSpace(r.URL.Query().Get("cursor")) != "" || strings.TrimSpace(r.URL.Query().Get("q")) != "" {
		s.handleListPricingTemplatePage(w, r)
		return
	}
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
			return pricingTemplateResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
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
	raw, err := decodeJSONRawBody(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	var requestBody pricingTemplateCreateRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, responseutil.SanitizeDecodeError(err).Error())
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	for _, fieldName := range []string{"name", "input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price"} {
		if _, ok := fields[fieldName]; !ok {
			writeDomainError(w, r, s.corsSnapshot(), &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s is required", fieldName)})
			return
		}
	}
	if err := rejectLegacyPricingTemplateFields(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
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
		return createPricingTemplateWithRevision(r.Context(), tx, profile.ID, s.nowUTC(), name, requestBody)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleImportPricingTemplates(w http.ResponseWriter, r *http.Request) {
	// Preview-only import (SPEC 7.6): validates every row and returns the
	// per-row action, the summary and a preview hash binding the canonical
	// payload; nothing is written until the commit endpoint replays the
	// identical payload.
	response, err := s.previewPricingTemplateImport(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCommitPricingTemplateImport(w http.ResponseWriter, r *http.Request) {
	var requestBody pricingTemplateImportCommitRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	preview, err := s.previewImportPayload(r, pricingTemplateImportRequest{Mode: requestBody.Mode, Templates: requestBody.Templates})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if strings.TrimSpace(requestBody.PreviewHash) != preview.PreviewHash {
		responseutil.WriteJSON(w, http.StatusConflict, pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{{Detail: "pricing_import_stale: the import preview no longer matches the submitted payload"}}})
		return
	}
	if !preview.Committable {
		responseutil.WriteJSON(w, http.StatusConflict, pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{{Detail: "pricing_import_preview_required: the import preview is not committable"}}})
		return
	}
	commitResponse, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateImportResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return pricingTemplateImportResponse{}, err
		}
		return commitPricingTemplateImport(r.Context(), tx, profile.ID, s.nowUTC(), preview, requestBody.Mode)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, commitResponse)
}

func (s *Service) previewPricingTemplateImport(r *http.Request) (pricingTemplateImportResponse, error) {
	body, err := decodeJSONRawBody(r)
	if err != nil {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Invalid request body"}
	}
	if err := pricingTemplateImportKeysPresent(body); err != nil {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
	}
	var requestBody pricingTemplateImportRequest
	if err := json.Unmarshal(body, &requestBody); err != nil {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Invalid request body"}
	}
	return s.previewImportPayload(r, requestBody)
}

func (s *Service) previewImportPayload(r *http.Request, requestBody pricingTemplateImportRequest) (pricingTemplateImportResponse, error) {
	mode := strings.TrimSpace(requestBody.Mode)
	if mode != "upsert_by_name" && mode != "create_only" {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "mode must be upsert_by_name or create_only"}
	}
	if len(requestBody.Templates) == 0 {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "templates must not be empty"}
	}
	rows := make([]pricingTemplateImportRow, 0, len(requestBody.Templates))
	seen := map[string]int{}
	importErrors := make([]pricingTemplateImportError, 0)
	for index, template := range requestBody.Templates {
		if template.InputPrice == nil || template.OutputPrice == nil || template.CachedInputPrice == nil && false {
			// key presence is enforced by the strict decoder path
		}
		if template.CachedInputPrice == nil && template.CacheCreationPrice == nil && template.ReasoningPrice == nil && false {
			// keep compiler quiet; presence handled by pricingTemplateRowKeysPresent
		}
		name, err := normalizePricingTemplateName(template.Name)
		if err != nil {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: strings.TrimSpace(template.Name), Detail: err.Error()})
			continue
		}
		if firstIndex, ok := seen[name]; ok {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: name, Detail: fmt.Sprintf("duplicate name also appears at index %d", firstIndex)})
			continue
		}
		seen[name] = index
		if err := rejectLegacyPricingTemplateFields(&template); err != nil {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: name, Detail: err.Error()})
			continue
		}
		prices, err := normalizePricingTemplatePrices(template.InputPrice, template.OutputPrice, template.CachedInputPrice, template.CacheCreationPrice, template.ReasoningPrice)
		if err != nil {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: name, Detail: err.Error()})
			continue
		}
		tier, err := normalizePricingTemplateTier(template.Tier, prices)
		if err != nil {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: name, Detail: err.Error()})
			continue
		}
		prices.Tier = tier
		rows = append(rows, pricingTemplateImportRow{Name: name, Description: normalizeOptionalTrimmedString(template.Description), Prices: prices})
	}
	if len(importErrors) > 0 {
		return pricingTemplateImportResponse{Skipped: []string{}, Errors: importErrors}, nil
	}

	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateImportResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		existingItems, err := listPricingTemplates(r.Context(), tx, profile.ID)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		existingByName := map[string]pricingTemplateResponse{}
		for _, existing := range existingItems {
			existingByName[existing.Name] = existing
		}
		result := pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{}, Mode: mode, Rows: rows}
		for _, row := range rows {
			current, exists := existingByName[row.Name]
			if !exists {
				result.Created++
				result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "create"})
				continue
			}
			if mode == "create_only" {
				result.Skipped = append(result.Skipped, row.Name)
				result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "skipped"})
				continue
			}
			if pricingTemplatePricesEqual(current, row.Prices) {
				result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "no_op"})
				continue
			}
			result.Updated++
			result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "update", CurrentVersion: current.Version, NextVersion: current.Version + 1})
		}
		hashInput, err := json.Marshal(map[string]any{"mode": mode, "profile_id": profile.ID, "rows": result.Items})
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		sum := sha256.Sum256(hashInput)
		result.PreviewHash = fmt.Sprintf("%x", sum[:])
		result.Committable = true
		return result, nil
	})
	if err != nil {
		return pricingTemplateImportResponse{}, err
	}
	return response, nil
}

func commitPricingTemplateImport(ctx context.Context, tx pgx.Tx, profileID int, currentTime time.Time, preview pricingTemplateImportResponse, mode string) (pricingTemplateImportResponse, error) {
	existingItems, err := listPricingTemplates(ctx, tx, profileID)
	if err != nil {
		return pricingTemplateImportResponse{}, err
	}
	existingByName := map[string]pricingTemplateResponse{}
	for _, existing := range existingItems {
		existingByName[existing.Name] = existing
	}
	result := pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{}, Rows: preview.Rows}
	for _, item := range preview.Items {
		switch item.Action {
		case "create":
			var row pricingTemplateImportRow
			for _, candidate := range preview.Rows {
				if candidate.Name == item.Name {
					row = candidate
					break
				}
			}
			if _, err := createPricingTemplateWithPrices(ctx, tx, profileID, currentTime, row.Name, row.Description, row.Prices); err != nil {
				return pricingTemplateImportResponse{}, err
			}
			result.Created++
		case "update":
			current, ok := existingByName[item.Name]
			if !ok {
				return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("pricing_import_stale: template %q disappeared", item.Name)}
			}
			var row pricingTemplateImportRow
			for _, candidate := range preview.Rows {
				if candidate.Name == item.Name {
					row = candidate
					break
				}
			}
			if err := updatePricingTemplateWithPrices(ctx, tx, profileID, current, row.Name, row.Description, row.Prices, currentTime); err != nil {
				return pricingTemplateImportResponse{}, err
			}
			result.Updated++
		case "skipped":
			result.Skipped = append(result.Skipped, item.Name)
		default:
			// no_op: nothing to write, nothing to report
		}
	}
	return result, nil
}

func pricingTemplatePricesEqual(current pricingTemplateResponse, prices pricingTemplatePrices) bool {
	return current.InputPrice == prices.InputPrice &&
		current.OutputPrice == prices.OutputPrice &&
		stringsEqualPointers(current.CachedInputPrice, prices.CachedInputPrice) &&
		stringsEqualPointers(current.CacheCreationPrice, prices.CacheCreationPrice) &&
		stringsEqualPointers(current.ReasoningPrice, prices.ReasoningPrice) &&
		pricingTemplateTierEqual(current.Tier, prices.Tier)
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
			return pricingTemplateResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		if err := validatePricingTemplateExpectedUpdatedAt(current.UpdatedAt, requestBody.ExpectedUpdatedAt); err != nil {
			return pricingTemplateResponse{}, err
		}
		if err := rejectLegacyPricingTemplateUpdateFields(requestBody); err != nil {
			return pricingTemplateResponse{}, err
		}
		nextName := current.Name
		if requestBody.Name.Set {
			name, err := normalizeOptionalRequiredName("name", requestBody.Name.Value)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
			nextName = name
		}
		if err := ensureUniquePricingTemplateName(r.Context(), tx, profile.ID, nextName, &current.ID); err != nil {
			return pricingTemplateResponse{}, err
		}
		nextDescription := current.Description
		if requestBody.Description.Set {
			nextDescription = normalizeOptionalTrimmedString(requestBody.Description.Value)
		}
		prices := pricingTemplatePrices{
			InputPrice:         current.InputPrice,
			OutputPrice:        current.OutputPrice,
			CachedInputPrice:   current.CachedInputPrice,
			CacheCreationPrice: current.CacheCreationPrice,
			ReasoningPrice:     current.ReasoningPrice,
			Tier:               current.Tier,
		}
		if requestBody.InputPrice.Set {
			input, err := normalizeRequiredPricingDecimalString("input_price", requestBody.InputPrice.Value)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
			prices.InputPrice = input
		}
		if requestBody.OutputPrice.Set {
			output, err := normalizeRequiredPricingDecimalString("output_price", requestBody.OutputPrice.Value)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
			prices.OutputPrice = output
		}
		if requestBody.CachedInputPrice.Set {
			cached, err := normalizeOptionalPricingDecimalString("cached_input_price", requestBody.CachedInputPrice.Value)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
			prices.CachedInputPrice = cached
		}
		if requestBody.CacheCreationPrice.Set {
			created, err := normalizeOptionalPricingDecimalString("cache_creation_price", requestBody.CacheCreationPrice.Value)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
			prices.CacheCreationPrice = created
		}
		if requestBody.ReasoningPrice.Set {
			reasoning, err := normalizeOptionalPricingDecimalString("reasoning_price", requestBody.ReasoningPrice.Value)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
			prices.ReasoningPrice = reasoning
		}
		if requestBody.Tier.Set {
			prices.Tier, err = normalizePricingTemplateTier(requestBody.Tier.Value, prices)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
		} else if prices.Tier != nil {
			// A base-card edit must keep tier specialty parity even when the
			// caller omitted tier (omitted means preserve on PUT).
			preservedTier := pricingTemplateTierFromResponse(prices.Tier)
			prices.Tier, err = normalizePricingTemplateTier(preservedTier, prices)
			if err != nil {
				return pricingTemplateResponse{}, err
			}
		}
		if err := updatePricingTemplateWithPrices(r.Context(), tx, profile.ID, current, nextName, nextDescription, prices, s.nowUTC()); err != nil {
			return pricingTemplateResponse{}, err
		}
		updated, found, err := loadPricingTemplate(r.Context(), tx, profile.ID, templateID, false)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if !found {
			return pricingTemplateResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
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
		current, found, err := loadPricingTemplate(r.Context(), tx, profile.ID, templateID, true)
		if err != nil {
			return deletedResponse{}, err
		}
		if !found {
			return deletedResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		if current.DeletedAt != nil {
			return deletedResponse{Deleted: true}, nil
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
			return deletedResponse{}, &DomainError{StatusCode: http.StatusConflict, Detail: map[string]any{"message": "Cannot delete pricing template that is referenced by connections", "connections": connections}}
		}
		// Soft-delete keeps the tombstone and the append-only revision
		// history readable (SPEC 6.1); revisions never cascade.
		if _, err := tx.Exec(r.Context(), `UPDATE pricing_templates SET deleted_at = $2, updated_at = $2 WHERE id = $1`, templateID, s.nowUTC()); err != nil {
			return deletedResponse{}, fmt.Errorf("soft-delete pricing template %d: %w", templateID, err)
		}
		return deletedResponse{Deleted: true}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// ---------------------------------------------------------------------------
// Canonical write helpers (SPEC 4.1/4.2/6.1/6.2)
// ---------------------------------------------------------------------------

type pricingTemplatePrices struct {
	InputPrice         string
	OutputPrice        string
	CachedInputPrice   *string
	CacheCreationPrice *string
	ReasoningPrice     *string
	Tier               *pricingTemplateTier
}

type pricingTemplateImportRow struct {
	Name        string
	Description *string
	Prices      pricingTemplatePrices
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

func nullableTierThreshold(tier *pricingTemplateTier) any {
	if tier == nil {
		return nil
	}
	return tier.InputTokensAbove
}

func nullableTierPrice(tier *pricingTemplateTier, component string) any {
	if tier == nil {
		return nil
	}
	if component == "input" {
		return tier.InputPrice
	}
	return tier.OutputPrice
}

func nullableTierSpecialtyPrice(tier *pricingTemplateTier, component string) any {
	if tier == nil {
		return nil
	}
	switch component {
	case "cached_input":
		return nullableString(tier.CachedInputPrice)
	case "cache_creation":
		return nullableString(tier.CacheCreationPrice)
	default:
		return nullableString(tier.ReasoningPrice)
	}
}

// rejectLegacyPricingTemplateFields fails closed on the removed authoring
// fields instead of silently ignoring them (SPEC 5.3 strict decoder).
func rejectLegacyPricingTemplateFields(requestBody *pricingTemplateCreateRequest) error {
	if requestBody.PricingUnit != nil {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: pricing_unit is not accepted; the unit is fixed to PER_1M"}
	}
	if requestBody.PricingCurrencyCode != nil {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: pricing_currency_code is not accepted; the active reporting-currency epoch owns the currency"}
	}
	return nil
}

func rejectLegacyPricingTemplateUpdateFields(requestBody pricingTemplateUpdateRequest) error {
	if requestBody.PricingUnit.Set {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: pricing_unit is not accepted; the unit is fixed to PER_1M"}
	}
	if requestBody.PricingCurrencyCode.Set {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: pricing_currency_code is not accepted; the active reporting-currency epoch owns the currency"}
	}
	return nil
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

func normalizePricingTemplateName(raw string) (string, error) {
	return normalizeOptionalRequiredName("name", &raw)
}

func normalizeOptionalRequiredName(fieldName string, raw *string) (string, error) {
	if raw == nil {
		return "", &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s is required", fieldName)}
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return "", &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s must not be empty", fieldName)}
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

// pricingTemplateImportKeysPresent rejects unknown top-level keys and
// requires the mode/templates keys (fail-closed strict decoder, SPEC 5.3).
func pricingTemplateImportKeysPresent(body []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	for key := range raw {
		if key != "mode" && key != "templates" {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	if _, ok := raw["mode"]; !ok {
		return fmt.Errorf("mode is required")
	}
	if _, ok := raw["templates"]; !ok {
		return fmt.Errorf("templates is required")
	}
	return nil
}

// pricingTemplateRowKeysPresent requires the five price keys on every import
// row (SPEC 4.1).
func pricingTemplateRowKeysPresent(row []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(row, &raw); err != nil {
		return err
	}
	for _, key := range []string{"input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price"} {
		if _, ok := raw[key]; !ok {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

var _ = pgx.ErrNoRows

// handleListPricingTemplateRevisions returns the append-only revision history
// for a template (SPEC 7.3): version, effective boundary, currency/epoch,
// the five exact prices and the localized creation source.
func (s *Service) handleListPricingTemplateRevisions(w http.ResponseWriter, r *http.Request) {
	templateID, err := routeInt(r, "template_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]pricingTemplateRevisionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM pricing_templates WHERE profile_id = $1 AND id = $2)`, profile.ID, templateID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		return listPricingTemplateRevisions(r.Context(), tx, templateID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleGetPricingTemplateImpact returns the edit/delete impact preview:
// the current revision, the next version and the dependency reference list
// (SPEC 7.4/7.7 fresh preflight).
func (s *Service) handleGetPricingTemplateImpact(w http.ResponseWriter, r *http.Request) {
	templateID, err := routeInt(r, "template_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateImpactResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateImpactResponse{}, err
		}
		current, found, err := loadPricingTemplate(r.Context(), tx, profile.ID, templateID, false)
		if err != nil {
			return pricingTemplateImpactResponse{}, err
		}
		if !found {
			return pricingTemplateImpactResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Pricing template not found"}
		}
		usageRows, err := listPricingTemplateConnectionUsageRows(r.Context(), tx, profile.ID, templateID)
		if err != nil {
			return pricingTemplateImpactResponse{}, err
		}
		references := make([]pricingTemplateConnectionUsageItem, 0, len(usageRows))
		for _, row := range usageRows {
			references = append(references, pricingTemplateConnectionUsageItem{
				ConnectionID:   row.ConnectionID,
				ConnectionName: row.ConnectionName,
				ModelConfigID:  row.ModelConfigID,
				ModelID:        derefString(row.ModelID),
				EndpointID:     row.EndpointID,
				EndpointName:   derefString(row.EndpointName),
			})
		}
		return pricingTemplateImpactResponse{
			TemplateID:     templateID,
			Name:           current.Name,
			CurrentVersion: current.Version,
			NextVersion:    current.Version + 1,
			ReferenceCount: len(references),
			References:     references,
			RevisionCount:  current.RevisionCount,
			DeletedAt:      current.DeletedAt,
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func listPricingTemplateRevisions(ctx context.Context, tx pgx.Tx, templateID int) ([]pricingTemplateRevisionResponse, error) {
	rows, err := tx.Query(ctx, `SELECT id, version, pricing_unit, currency_code, reporting_currency_epoch, currency_attribution, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, tier_input_tokens_above, tier_input_price, tier_output_price, tier_cached_input_price, tier_cache_creation_price, tier_reasoning_price, effective_at, created_at, created_by_kind FROM pricing_template_revisions WHERE template_id = $1 ORDER BY version ASC`, templateID)
	if err != nil {
		return nil, fmt.Errorf("query pricing template revisions: %w", err)
	}
	defer rows.Close()
	items := make([]pricingTemplateRevisionResponse, 0)
	for rows.Next() {
		var item pricingTemplateRevisionResponse
		var tierThreshold sql.NullInt32
		var tierInput, tierOutput, tierCached, tierCreation, tierReasoning sql.NullString
		if err := rows.Scan(&item.RevisionID, &item.Version, &item.PricingUnit, &item.CurrencyCode, &item.ReportingCurrencyEpoch, &item.CurrencyAttribution, &item.InputPrice, &item.OutputPrice, &item.CachedInputPrice, &item.CacheCreationPrice, &item.ReasoningPrice, &tierThreshold, &tierInput, &tierOutput, &tierCached, &tierCreation, &tierReasoning, &item.EffectiveAt, &item.CreatedAt, &item.CreatedByKind); err != nil {
			return nil, fmt.Errorf("scan pricing template revision: %w", err)
		}
		if tierThreshold.Valid {
			item.Tier = &pricingTemplateTier{InputTokensAbove: int(tierThreshold.Int32), InputPrice: tierInput.String, OutputPrice: tierOutput.String, CachedInputPrice: nullableStringValue(tierCached), CacheCreationPrice: nullableStringValue(tierCreation), ReasoningPrice: nullableStringValue(tierReasoning)}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing template revisions: %w", err)
	}
	return items, nil
}

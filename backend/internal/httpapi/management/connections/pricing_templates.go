package connections

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// pricingTemplateCreateRequest is the steady-state create schema (SPEC 4.1):
// the five price keys are all required; input/output must be non-null decimal
// strings, the three specialty prices use explicit JSON null for
// "unconfigured". pricing_unit and pricing_currency_code are never accepted:
// the active reporting-currency epoch owns the currency, and the unit is
// fixed to PER_1M.
type pricingTemplateCardInput struct {
	InputPrice         *string `json:"input_price"`
	OutputPrice        *string `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
	present            map[string]bool
}

func (card *pricingTemplateCardInput) UnmarshalJSON(data []byte) error {
	type wireCard struct {
		InputPrice         *string `json:"input_price"`
		OutputPrice        *string `json:"output_price"`
		CachedInputPrice   *string `json:"cached_input_price"`
		CacheCreationPrice *string `json:"cache_creation_price"`
		ReasoningPrice     *string `json:"reasoning_price"`
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded wireCard
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*card = pricingTemplateCardInput{
		InputPrice: decoded.InputPrice, OutputPrice: decoded.OutputPrice,
		CachedInputPrice: decoded.CachedInputPrice, CacheCreationPrice: decoded.CacheCreationPrice,
		ReasoningPrice: decoded.ReasoningPrice, present: make(map[string]bool, len(raw)),
	}
	for key := range raw {
		card.present[key] = true
	}
	return nil
}

type pricingTemplateWindowInput struct {
	WeekdayMask int `json:"weekday_mask"`
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

type pricingTemplateScheduleInput struct {
	Timezone string                       `json:"timezone"`
	Windows  []pricingTemplateWindowInput `json:"windows"`
}

type pricingTemplateTierInput struct {
	InputTokensAbove   *int                      `json:"input_tokens_above"`
	InputPrice         *string                   `json:"input_price,omitempty"`
	OutputPrice        *string                   `json:"output_price,omitempty"`
	CachedInputPrice   *string                   `json:"cached_input_price,omitempty"`
	CacheCreationPrice *string                   `json:"cache_creation_price,omitempty"`
	ReasoningPrice     *string                   `json:"reasoning_price,omitempty"`
	Card               *pricingTemplateCardInput `json:"card,omitempty"`
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
	InputTokensAbove int                  `json:"input_tokens_above"`
	Card             *pricingTemplateCard `json:"card,omitempty"`
}

type pricingTemplateCreateRequest struct {
	Name         string                        `json:"name"`
	Description  *string                       `json:"description"`
	TemplateKind string                        `json:"template_kind"`
	Card         *pricingTemplateCardInput     `json:"card"`
	BaseCard     *pricingTemplateCardInput     `json:"base_card"`
	PeakCard     *pricingTemplateCardInput     `json:"peak_card"`
	OffpeakCard  *pricingTemplateCardInput     `json:"offpeak_card"`
	Schedule     *pricingTemplateScheduleInput `json:"schedule"`
	// Legacy fields remain decoded only to return a precise unknown-shape error.
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
	TemplateKind        optionalString              `json:"template_kind"`
	Card                optionalRawPricingShape     `json:"card"`
	BaseCard            optionalRawPricingShape     `json:"base_card"`
	PeakCard            optionalRawPricingShape     `json:"peak_card"`
	OffpeakCard         optionalRawPricingShape     `json:"offpeak_card"`
	Schedule            optionalRawPricingShape     `json:"schedule"`
	InputPrice          optionalString              `json:"input_price"`
	OutputPrice         optionalString              `json:"output_price"`
	CachedInputPrice    optionalString              `json:"cached_input_price"`
	CacheCreationPrice  optionalString              `json:"cache_creation_price"`
	ReasoningPrice      optionalString              `json:"reasoning_price"`
	Tier                optionalPricingTemplateTier `json:"tier"`
	PricingUnit         optionalString              `json:"pricing_unit"`
	PricingCurrencyCode optionalString              `json:"pricing_currency_code"`
}

type optionalRawPricingShape struct {
	Set bool
	Raw json.RawMessage
}

func (value *optionalRawPricingShape) UnmarshalJSON(data []byte) error {
	value.Set = true
	value.Raw = append(json.RawMessage(nil), data...)
	return nil
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
	for _, fieldName := range []string{"name", "template_kind"} {
		if _, ok := fields[fieldName]; !ok {
			writeDomainError(w, r, s.corsSnapshot(), &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s is required", fieldName)})
			return
		}
	}
	if err := rejectLegacyPricingTemplateFields(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if _, err := normalizePricingTemplateShape(requestBody); err != nil {
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
		if requestBody.TemplateKind.Set && requestBody.TemplateKind.Value == nil {
			return pricingTemplateResponse{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "template_kind cannot be null"}
		}
		if requestBody.TemplateKind.Set && requestBody.TemplateKind.Value != nil && strings.TrimSpace(*requestBody.TemplateKind.Value) != current.TemplateKind && (!requestBody.ExpectedUpdatedAt.Set || requestBody.ExpectedUpdatedAt.Value == nil) {
			return pricingTemplateResponse{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "expected_updated_at is required when changing template_kind"}
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
		createShape, err := pricingTemplateCreateRequestFromUpdate(current, requestBody)
		if err != nil {
			return pricingTemplateResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
		}
		shape, err := normalizePricingTemplateShape(createShape)
		if err != nil {
			return pricingTemplateResponse{}, err
		}
		if err := updatePricingTemplateWithShape(r.Context(), tx, profile.ID, current, nextName, nextDescription, shape, s.nowUTC()); err != nil {
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
	if requestBody.InputPrice.Set || requestBody.OutputPrice.Set || requestBody.CachedInputPrice.Set || requestBody.CacheCreationPrice.Set || requestBody.ReasoningPrice.Set {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "legacy pricing fields are not accepted; use the typed template shape"}
	}
	if requestBody.PricingUnit.Set {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: pricing_unit is not accepted; the unit is fixed to PER_1M"}
	}
	if requestBody.PricingCurrencyCode.Set {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: pricing_currency_code is not accepted; the active reporting-currency epoch owns the currency"}
	}
	return nil
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

// pricingTemplateRowKeysPresent requires the five price keys on every import
// row (SPEC 4.1).
func pricingTemplateRowKeysPresent(row []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(row, &raw); err != nil {
		return err
	}
	if _, ok := raw["template_kind"]; !ok {
		return fmt.Errorf("template_kind is required")
	}
	return nil
}

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
	rows, err := tx.Query(ctx, `SELECT id, version, pricing_unit, currency_code, reporting_currency_epoch, currency_attribution, template_kind, tier_input_tokens_above, pricing_schedule_timezone, pricing_schedule_digest, effective_at, created_at, created_by_kind FROM pricing_template_revisions WHERE template_id = $1 ORDER BY version ASC`, templateID)
	if err != nil {
		return nil, fmt.Errorf("query pricing template revisions: %w", err)
	}
	items := make([]pricingTemplateRevisionResponse, 0)
	itemIndex := make(map[int64]int)
	revisionIDs := make([]int64, 0)
	for rows.Next() {
		var item pricingTemplateRevisionResponse
		var kind, timezone, digest sql.NullString
		var threshold sql.NullInt32
		if err := rows.Scan(&item.RevisionID, &item.Version, &item.PricingUnit, &item.CurrencyCode, &item.ReportingCurrencyEpoch, &item.CurrencyAttribution, &kind, &threshold, &timezone, &digest, &item.EffectiveAt, &item.CreatedAt, &item.CreatedByKind); err != nil {
			return nil, fmt.Errorf("scan pricing template revision: %w", err)
		}
		item.TemplateKind = kind.String
		item.ScheduleTimezone = nullableStringValue(timezone)
		item.ScheduleDigest = nullableStringValue(digest)
		if threshold.Valid {
			item.Tier = &pricingTemplateTier{InputTokensAbove: int(threshold.Int32)}
		}
		if item.TemplateKind == "peak_valley" {
			item.Schedule = &pricingTemplateSchedule{Timezone: timezone.String}
		}
		itemIndex[item.RevisionID] = len(items)
		revisionIDs = append(revisionIDs, item.RevisionID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate pricing template revisions: %w", err)
	}
	rows.Close()

	if len(revisionIDs) == 0 {
		return items, nil
	}
	cardRows, err := tx.Query(ctx, `SELECT revision_id, card_role, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price FROM pricing_template_cards WHERE revision_id = ANY($1) ORDER BY revision_id, card_role`, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("query pricing template revision cards: %w", err)
	}
	for cardRows.Next() {
		var revisionID int64
		var role, input, output string
		var cached, creation, reasoning sql.NullString
		if err := cardRows.Scan(&revisionID, &role, &input, &output, &cached, &creation, &reasoning); err != nil {
			cardRows.Close()
			return nil, fmt.Errorf("scan pricing template revision card: %w", err)
		}
		index, ok := itemIndex[revisionID]
		if !ok {
			cardRows.Close()
			return nil, fmt.Errorf("pricing template revision card references unknown revision %d", revisionID)
		}
		card := &pricingTemplateCard{InputPrice: input, OutputPrice: output, CachedInputPrice: nullableStringValue(cached), CacheCreationPrice: nullableStringValue(creation), ReasoningPrice: nullableStringValue(reasoning)}
		switch role {
		case pricingkind.RoleStandard:
			items[index].Card = card
		case pricingkind.RoleTierBase:
			items[index].BaseCard = card
		case pricingkind.RoleTierAbove:
			if items[index].Tier == nil {
				items[index].Tier = &pricingTemplateTier{}
			}
			items[index].Tier.Card = card
		case pricingkind.RolePeak:
			items[index].PeakCard = card
		case pricingkind.RoleOffpeak:
			items[index].OffpeakCard = card
		}
	}
	if err := cardRows.Err(); err != nil {
		cardRows.Close()
		return nil, fmt.Errorf("iterate pricing template revision cards: %w", err)
	}
	cardRows.Close()

	windowRows, err := tx.Query(ctx, `SELECT revision_id, weekday_mask, start_minute, end_minute FROM pricing_template_windows WHERE revision_id = ANY($1) ORDER BY revision_id, weekday_mask, start_minute, end_minute`, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("query pricing template revision windows: %w", err)
	}
	for windowRows.Next() {
		var revisionID int64
		var mask, start, end int
		if err := windowRows.Scan(&revisionID, &mask, &start, &end); err != nil {
			windowRows.Close()
			return nil, fmt.Errorf("scan pricing template revision window: %w", err)
		}
		index, ok := itemIndex[revisionID]
		if !ok || items[index].Schedule == nil {
			windowRows.Close()
			return nil, fmt.Errorf("pricing template window references non-peak revision %d", revisionID)
		}
		items[index].Schedule.Windows = append(items[index].Schedule.Windows, pricingTemplateWindow{WeekdayMask: mask, StartMinute: start, EndMinute: end})
	}
	if err := windowRows.Err(); err != nil {
		windowRows.Close()
		return nil, fmt.Errorf("iterate pricing template revision windows: %w", err)
	}
	windowRows.Close()
	if err := validatePricingTemplateRevisionHistory(items); err != nil {
		return nil, err
	}
	return items, nil
}

func validatePricingTemplateRevisionHistory(items []pricingTemplateRevisionResponse) error {
	for _, item := range items {
		kind := pricingkind.Kind(item.TemplateKind)
		if !kind.Valid() {
			return &domainError{StatusCode: http.StatusConflict, Detail: "pricing_template_shape_unavailable"}
		}
		cards := map[string]*pricingTemplateCard{
			pricingkind.RoleStandard:  item.Card,
			pricingkind.RoleTierBase:  item.BaseCard,
			pricingkind.RoleTierAbove: nil,
			pricingkind.RolePeak:      item.PeakCard,
			pricingkind.RoleOffpeak:   item.OffpeakCard,
		}
		if item.Tier != nil {
			cards[pricingkind.RoleTierAbove] = item.Tier.Card
		}
		for _, role := range pricingkind.RolesFor(kind) {
			if cards[role] == nil {
				return &domainError{StatusCode: http.StatusConflict, Detail: "pricing_template_shape_unavailable"}
			}
		}
		if kind != pricingkind.PeakValley {
			if item.Schedule != nil || item.ScheduleDigest != nil {
				return &domainError{StatusCode: http.StatusConflict, Detail: "pricing_template_shape_unavailable"}
			}
			continue
		}
		if item.Schedule == nil || strings.TrimSpace(item.Schedule.Timezone) == "" || item.ScheduleDigest == nil || strings.TrimSpace(*item.ScheduleDigest) == "" || len(item.Schedule.Windows) == 0 {
			return &domainError{StatusCode: http.StatusConflict, Detail: "pricing_template_shape_unavailable"}
		}
		windows := make([]terminaltarget.Window, 0, len(item.Schedule.Windows))
		for _, window := range item.Schedule.Windows {
			windows = append(windows, terminaltarget.Window{WeekdayMask: window.WeekdayMask, StartMinute: window.StartMinute, EndMinute: window.EndMinute})
		}
		if pricingTemplateWindowsDigest(windows) != strings.TrimSpace(*item.ScheduleDigest) {
			return &domainError{StatusCode: http.StatusConflict, Detail: "pricing_template_shape_unavailable"}
		}
	}
	return nil
}

type pricingTemplateRevisionResponse struct {
	RevisionID             int64                    `json:"revision_id"`
	Version                int                      `json:"version"`
	PricingUnit            string                   `json:"pricing_unit"`
	CurrencyCode           string                   `json:"currency_code"`
	ReportingCurrencyEpoch *int                     `json:"reporting_currency_epoch"`
	CurrencyAttribution    string                   `json:"currency_attribution"`
	TemplateKind           string                   `json:"template_kind"`
	Card                   *pricingTemplateCard     `json:"card,omitempty"`
	BaseCard               *pricingTemplateCard     `json:"base_card,omitempty"`
	Tier                   *pricingTemplateTier     `json:"tier,omitempty"`
	PeakCard               *pricingTemplateCard     `json:"peak_card,omitempty"`
	OffpeakCard            *pricingTemplateCard     `json:"offpeak_card,omitempty"`
	Schedule               *pricingTemplateSchedule `json:"schedule,omitempty"`
	ScheduleTimezone       *string                  `json:"schedule_timezone,omitempty"`
	ScheduleDigest         *string                  `json:"schedule_digest,omitempty"`
	EffectiveAt            *time.Time               `json:"effective_at"`
	CreatedAt              time.Time                `json:"created_at"`
	CreatedByKind          string                   `json:"created_by_kind"`
}

type pricingTemplateImpactResponse struct {
	TemplateID     int                                  `json:"template_id"`
	Name           string                               `json:"name"`
	CurrentVersion int                                  `json:"current_version"`
	NextVersion    int                                  `json:"next_version"`
	ReferenceCount int                                  `json:"reference_count"`
	References     []pricingTemplateConnectionUsageItem `json:"references"`
	RevisionCount  int64                                `json:"revision_count"`
	DeletedAt      *time.Time                           `json:"deleted_at,omitempty"`
}

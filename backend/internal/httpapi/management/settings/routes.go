package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

var currencyCodeRE = regexp.MustCompile(`^[A-Z]{3}$`)

func (s *Service) handleGetCostingSettings(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (costingSettingsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return costingSettingsResponse{}, err
		}
		settingsRow, err := loadOrCreateUserSettings(r.Context(), tx, profile.ID, s.nowUTC())
		if err != nil {
			return costingSettingsResponse{}, err
		}
		return buildCostingSettingsResponse(r.Context(), tx, settingsRow)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutCostingSettings(w http.ResponseWriter, r *http.Request) {
	var requestBody costingSettingsUpdateRequest
	if err := decodeStrictJSONBody(r, &requestBody); err != nil {
		writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}, Details: map[string]any{"violations": []any{}}}, http.StatusBadRequest)
		return
	}
	if err := normalizeAndValidateCostingRequest(&requestBody); err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (costingSettingsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return costingSettingsResponse{}, err
		}
		settingsRow, err := loadOrCreateUserSettings(r.Context(), tx, profile.ID, s.nowUTC())
		if err != nil {
			return costingSettingsResponse{}, err
		}
		if err := validateCostingExpectedUpdatedAt(settingsRow.UpdatedAt, requestBody.ExpectedUpdatedAt); err != nil {
			return costingSettingsResponse{}, err
		}
		if err := applyCostingSettingsUpdate(r.Context(), tx, profile.ID, settingsRow, requestBody, s.nowUTC()); err != nil {
			return costingSettingsResponse{}, err
		}
		// Reload the row so the response reflects the committed update (the
		// in-memory row predates the write).
		refreshed, found, err := loadUserSettings(r.Context(), tx, profile.ID, false)
		if err != nil {
			return costingSettingsResponse{}, err
		}
		if !found {
			return costingSettingsResponse{}, fmt.Errorf("costing settings row disappeared after update")
		}
		return buildCostingSettingsResponse(r.Context(), tx, refreshed)
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetTimezonePreference(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (timezonePreferenceResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return timezonePreferenceResponse{}, err
		}
		settingsRow, err := loadOrCreateUserSettings(r.Context(), tx, profile.ID, s.nowUTC())
		if err != nil {
			return timezonePreferenceResponse{}, err
		}
		return buildTimezonePreferenceResponse(settingsRow), nil
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutTimezonePreference(w http.ResponseWriter, r *http.Request) {
	var requestBody timezonePreferenceUpdateRequest
	if err := decodeStrictJSONBody(r, &requestBody); err != nil {
		writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}, Details: map[string]any{"violations": []any{}}}, http.StatusBadRequest)
		return
	}
	if err := normalizeAndValidateTimezoneRequest(&requestBody); err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (timezonePreferenceResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return timezonePreferenceResponse{}, err
		}
		settingsRow, err := loadOrCreateUserSettings(r.Context(), tx, profile.ID, s.nowUTC())
		if err != nil {
			return timezonePreferenceResponse{}, err
		}
		settingsRow.TimezonePreference = requestBody.TimezonePreference
		settingsRow.UpdatedAt = s.nowUTC()
		if err := updateUserSettings(r.Context(), tx, settingsRow); err != nil {
			return timezonePreferenceResponse{}, err
		}
		return buildTimezonePreferenceResponse(settingsRow), nil
	})
	if err != nil {
		writeSettingsDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func buildCostingSettingsResponse(ctx context.Context, tx pgx.Tx, settingsRow userSettingsRow) (costingSettingsResponse, error) {
	var epoch int
	var effectiveAt *time.Time
	if settingsRow.CurrentReportingCurrencyEpochID != nil {
		if err := tx.QueryRow(ctx, `SELECT epochs.epoch, epochs.effective_at
			FROM reporting_currency_epochs AS epochs
			WHERE epochs.id = $1`, *settingsRow.CurrentReportingCurrencyEpochID).Scan(&epoch, &effectiveAt); err != nil {
			return costingSettingsResponse{}, fmt.Errorf("load active reporting currency epoch: %w", err)
		}
	}
	var effectiveString *string
	if effectiveAt != nil {
		formatted := effectiveAt.UTC().Format(time.RFC3339)
		effectiveString = &formatted
	}
	var activeTemplateCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pricing_templates WHERE profile_id = $1 AND deleted_at IS NULL`, settingsRow.ProfileID).Scan(&activeTemplateCount); err != nil {
		return costingSettingsResponse{}, fmt.Errorf("load active pricing template count: %w", err)
	}
	var code, symbol *string
	if settingsRow.ReportCurrencyCode != "" {
		codeValue := settingsRow.ReportCurrencyCode
		code = &codeValue
	}
	if settingsRow.ReportCurrencySymbol != "" {
		symbolValue := settingsRow.ReportCurrencySymbol
		symbol = &symbolValue
	}
	response := costingSettingsResponse{
		ProfileID:                  settingsRow.ProfileID,
		ReportCurrencyCode:         code,
		ReportCurrencySymbol:       symbol,
		ReportingCurrencyEpoch:     nil,
		CurrencyEffectiveAt:        effectiveString,
		PricingMigrationState:      settingsRow.PricingMigrationState,
		LegacyMigrationIssues:      settingsRow.LegacyMigrationIssues,
		TimezonePreference:         settingsRow.TimezonePreference,
		PricingTemplateGeneration:  fmt.Sprintf("%d", settingsRow.PricingTemplateGeneration),
		PricingReferenceGeneration: fmt.Sprintf("%d", settingsRow.PricingReferenceGeneration),
		ActiveTemplateCount:        activeTemplateCount,
		UpdatedAt:                  settingsRow.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if settingsRow.CurrentReportingCurrencyEpochID != nil {
		value := fmt.Sprintf("%d", epoch)
		response.ReportingCurrencyEpoch = &value
	}
	inventorySummary, inventoryErr := loadPricingMigrationInventorySummary(ctx, tx, settingsRow.ProfileID, settingsRow.CurrentReportingCurrencyEpochID == nil)
	if inventoryErr != nil {
		return costingSettingsResponse{}, fmt.Errorf("load pricing migration inventory: %w", inventoryErr)
	}
	response.PricingMigrationInventory = inventorySummary
	return response, nil
}

func buildTimezonePreferenceResponse(settingsRow userSettingsRow) timezonePreferenceResponse {
	return timezonePreferenceResponse{ProfileID: settingsRow.ProfileID, TimezonePreference: settingsRow.TimezonePreference}
}

func normalizeAndValidateCostingRequest(requestBody *costingSettingsUpdateRequest) error {
	// Legacy currency-code and FX authoring fields are rejected, never
	// silently accepted (SPEC 5.3): the active epoch owns the code and FX
	// authoring was hard-deleted.
	if requestBody.ReportCurrencyCode.Set {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: report_currency_code is not accepted; migrate the reporting currency through the currency migration flow"}
	}
	if requestBody.EndpointFXMappings != nil {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "unknown_field: endpoint_fx_mappings is not accepted; FX authoring was removed"}
	}
	if requestBody.ReportCurrencySymbol.Set {
		if requestBody.ReportCurrencySymbol.Value == nil {
			return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "invalid_currency_symbol: report_currency_symbol must be a non-null canonical symbol"}
		}
		symbol, valid := canonicalCurrencySymbol(*requestBody.ReportCurrencySymbol.Value)
		if !valid {
			return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "invalid_currency_symbol: report_currency_symbol must be a canonical symbol"}
		}
		requestBody.ReportCurrencySymbol.Value = &symbol
	}
	if requestBody.TimezonePreference.Set {
		trimmedTimezone, err := normalizeTimezonePreference(requestBody.TimezonePreference.Value)
		if err != nil {
			return err
		}
		requestBody.TimezonePreference = optionalString{Set: true, Value: trimmedTimezone}
	}
	return nil
}

// validateCostingExpectedUpdatedAt applies the settings CAS contract.
func validateCostingExpectedUpdatedAt(current time.Time, expected optionalString) error {
	if !expected.Set || expected.Value == nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*expected.Value))
	if err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "expected_updated_at must be a valid RFC3339 timestamp"}
	}
	// The GET projection formats seconds precision, so CAS compares at
	// second alignment to avoid false conflicts from sub-second storage.
	if !current.UTC().Truncate(time.Second).Equal(parsed.UTC().Truncate(time.Second)) {
		return &domainError{StatusCode: http.StatusConflict, Detail: costingSettingsChangedDetail{
			CurrentUpdatedAt: current.UTC().Format(time.RFC3339),
		}}
	}
	return nil
}

// applyCostingSettingsUpdate applies symbol-only and timezone updates; the
// active epoch's symbol is kept in sync in the same transaction (SPEC 5.3).
func applyCostingSettingsUpdate(ctx context.Context, tx pgx.Tx, profileID int, settingsRow userSettingsRow, requestBody costingSettingsUpdateRequest, currentTime time.Time) error {
	if settingsRow.CurrentReportingCurrencyEpochID == nil || settingsRow.ReportCurrencyCode == "" || settingsRow.ReportCurrencySymbol == "" {
		return &domainError{StatusCode: http.StatusConflict, Detail: "legacy_pricing_migration_required: reporting currency repair is required before ordinary costing updates"}
	}
	nextSymbol := settingsRow.ReportCurrencySymbol
	if requestBody.ReportCurrencySymbol.Set && requestBody.ReportCurrencySymbol.Value != nil {
		nextSymbol = strings.TrimSpace(*requestBody.ReportCurrencySymbol.Value)
	}
	nextTimezone := settingsRow.TimezonePreference
	if requestBody.TimezonePreference.Set {
		nextTimezone = requestBody.TimezonePreference.Value
	}
	if _, err := tx.Exec(ctx, `UPDATE user_settings SET report_currency_symbol = $2, timezone_preference = $3, updated_at = $4 WHERE id = $1`,
		settingsRow.ID, nextSymbol, nextTimezone, currentTime); err != nil {
		return fmt.Errorf("update costing settings %d: %w", settingsRow.ID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE reporting_currency_epochs SET currency_symbol = $2, updated_at = $3 WHERE id = $1`,
		*settingsRow.CurrentReportingCurrencyEpochID, nextSymbol, currentTime); err != nil {
		return fmt.Errorf("sync active epoch symbol: %w", err)
	}
	// Advancing the runtime planning generation is handled by the runtime
	// cache invalidation middleware; the authoring counters are untouched by
	// symbol-only updates.
	return nil
}

func normalizeAndValidateTimezoneRequest(requestBody *timezonePreferenceUpdateRequest) error {
	trimmedTimezone, err := normalizeTimezonePreference(requestBody.TimezonePreference)
	if err != nil {
		return err
	}
	requestBody.TimezonePreference = trimmedTimezone
	return nil
}

func normalizeTimezonePreference(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) > 100 {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "timezone_preference must be at most 100 characters"}
	}
	if _, err := time.LoadLocation(trimmed); err != nil {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "timezone_preference must be a valid IANA timezone"}
	}
	return &trimmed, nil
}

func decodeStrictJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return responseutil.SanitizeDecodeError(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"

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
		mappings, err := listEndpointFXMappings(r.Context(), tx, profile.ID)
		if err != nil {
			return costingSettingsResponse{}, err
		}
		return buildCostingSettingsResponse(settingsRow, mappings), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutCostingSettings(w http.ResponseWriter, r *http.Request) {
	var requestBody costingSettingsUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateCostingRequest(&requestBody); err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
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
		if err := validateEndpointFXMappings(r.Context(), tx, profile.ID, requestBody.EndpointFXMappings); err != nil {
			return costingSettingsResponse{}, err
		}
		settingsRow.ReportCurrencyCode = requestBody.ReportCurrencyCode
		settingsRow.ReportCurrencySymbol = requestBody.ReportCurrencySymbol
		settingsRow.TimezonePreference = requestBody.TimezonePreference
		settingsRow.UpdatedAt = s.nowUTC()
		if err := updateUserSettings(r.Context(), tx, settingsRow); err != nil {
			return costingSettingsResponse{}, err
		}
		if err := replaceEndpointFXMappings(r.Context(), tx, profile.ID, requestBody.EndpointFXMappings, s.nowUTC()); err != nil {
			return costingSettingsResponse{}, err
		}
		return buildCostingSettingsResponse(settingsRow, cloneMappings(requestBody.EndpointFXMappings)), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutTimezonePreference(w http.ResponseWriter, r *http.Request) {
	var requestBody timezonePreferenceUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateTimezoneRequest(&requestBody); err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetRetentionSettings(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (retentionSettingsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return retentionSettingsResponse{}, err
		}
		settingsRow, err := loadOrCreateUserSettings(r.Context(), tx, profile.ID, s.nowUTC())
		if err != nil {
			return retentionSettingsResponse{}, err
		}
		return buildRetentionSettingsResponse(settingsRow), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutRetentionSettings(w http.ResponseWriter, r *http.Request) {
	var requestBody retentionSettingsUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateRetentionRequest(&requestBody); err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (retentionSettingsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return retentionSettingsResponse{}, err
		}
		settingsRow, err := loadOrCreateUserSettings(r.Context(), tx, profile.ID, s.nowUTC())
		if err != nil {
			return retentionSettingsResponse{}, err
		}
		settingsRow.RequestLogsRetentionDays = requestBody.RequestLogsRetentionDays
		settingsRow.StatisticsRetentionDays = requestBody.StatisticsRetentionDays
		settingsRow.AuditLogsRetentionDays = requestBody.AuditLogsRetentionDays
		settingsRow.UpdatedAt = s.nowUTC()
		if err := updateUserSettings(r.Context(), tx, settingsRow); err != nil {
			return retentionSettingsResponse{}, err
		}
		return buildRetentionSettingsResponse(settingsRow), nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func buildCostingSettingsResponse(settingsRow userSettingsRow, mappings []endpointFXMapping) costingSettingsResponse {
	if mappings == nil {
		mappings = []endpointFXMapping{}
	}
	return costingSettingsResponse{
		ProfileID:            settingsRow.ProfileID,
		ReportCurrencyCode:   settingsRow.ReportCurrencyCode,
		ReportCurrencySymbol: settingsRow.ReportCurrencySymbol,
		TimezonePreference:   settingsRow.TimezonePreference,
		EndpointFXMappings:   mappings,
	}
}

func buildTimezonePreferenceResponse(settingsRow userSettingsRow) timezonePreferenceResponse {
	return timezonePreferenceResponse{ProfileID: settingsRow.ProfileID, TimezonePreference: settingsRow.TimezonePreference}
}

func buildRetentionSettingsResponse(settingsRow userSettingsRow) retentionSettingsResponse {
	return retentionSettingsResponse{
		ProfileID:                settingsRow.ProfileID,
		RequestLogsRetentionDays: settingsRow.RequestLogsRetentionDays,
		StatisticsRetentionDays:  settingsRow.StatisticsRetentionDays,
		AuditLogsRetentionDays:   settingsRow.AuditLogsRetentionDays,
	}
}

func normalizeAndValidateCostingRequest(requestBody *costingSettingsUpdateRequest) error {
	requestBody.ReportCurrencyCode = strings.ToUpper(strings.TrimSpace(requestBody.ReportCurrencyCode))
	requestBody.ReportCurrencySymbol = strings.TrimSpace(requestBody.ReportCurrencySymbol)
	trimmedTimezone, err := normalizeTimezonePreference(requestBody.TimezonePreference)
	if err != nil {
		return err
	}
	requestBody.TimezonePreference = trimmedTimezone
	if !currencyCodeRE.MatchString(requestBody.ReportCurrencyCode) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "report_currency_code must be a 3-letter uppercase ISO code"}
	}
	if requestBody.ReportCurrencySymbol == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "report_currency_symbol must not be empty"}
	}
	if len(requestBody.ReportCurrencySymbol) > 5 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "report_currency_symbol must be at most 5 characters"}
	}
	seen := map[string]struct{}{}
	for index := range requestBody.EndpointFXMappings {
		requestBody.EndpointFXMappings[index].ModelID = strings.TrimSpace(requestBody.EndpointFXMappings[index].ModelID)
		requestBody.EndpointFXMappings[index].FXRate = strings.TrimSpace(requestBody.EndpointFXMappings[index].FXRate)
		if err := validateFXRate(requestBody.EndpointFXMappings[index].FXRate); err != nil {
			return err
		}
		key := connectionPairKey(requestBody.EndpointFXMappings[index].ModelID, requestBody.EndpointFXMappings[index].EndpointID)
		if _, ok := seen[key]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate endpoint_fx_mapping for model_id=%s, endpoint_id=%d", requestBody.EndpointFXMappings[index].ModelID, requestBody.EndpointFXMappings[index].EndpointID)}
		}
		seen[key] = struct{}{}
	}
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

func normalizeAndValidateRetentionRequest(requestBody *retentionSettingsUpdateRequest) error {
	if err := validateRetentionDays(requestBody.RequestLogsRetentionDays, "request_logs_retention_days"); err != nil {
		return err
	}
	if err := validateRetentionDays(requestBody.StatisticsRetentionDays, "statistics_retention_days"); err != nil {
		return err
	}
	if err := validateRetentionDays(requestBody.AuditLogsRetentionDays, "audit_logs_retention_days"); err != nil {
		return err
	}
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
	return &trimmed, nil
}

func validateRetentionDays(value *int, fieldName string) error {
	if value == nil {
		return nil
	}
	if *value < 1 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("%s must be >= 1 when provided", fieldName)}
	}
	return nil
}

func validateFXRate(value string) error {
	parsed, ok := new(big.Float).SetString(value)
	if !ok {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "fx_rate must be a valid decimal"}
	}
	if parsed.Sign() <= 0 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "fx_rate must be > 0"}
	}
	return nil
}

func validateEndpointFXMappings(ctx context.Context, tx pgx.Tx, profileID int, mappings []endpointFXMapping) error {
	endpointIDs := make([]int, 0, len(mappings))
	seenEndpointIDs := map[int]struct{}{}
	for _, mapping := range mappings {
		if _, ok := seenEndpointIDs[mapping.EndpointID]; ok {
			continue
		}
		seenEndpointIDs[mapping.EndpointID] = struct{}{}
		endpointIDs = append(endpointIDs, mapping.EndpointID)
	}
	validPairs, err := listValidConnectionPairs(ctx, tx, profileID, endpointIDs)
	if err != nil {
		return err
	}
	for _, mapping := range mappings {
		if _, ok := validPairs[connectionPairKey(mapping.ModelID, mapping.EndpointID)]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("No connection found for model_id='%s' and endpoint_id=%d", mapping.ModelID, mapping.EndpointID)}
		}
	}
	return nil
}

func cloneMappings(values []endpointFXMapping) []endpointFXMapping {
	if len(values) == 0 {
		return []endpointFXMapping{}
	}
	cloned := make([]endpointFXMapping, len(values))
	copy(cloned, values)
	return cloned
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	return json.NewDecoder(request.Body).Decode(target)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeDomainError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var settingsErr *domainError
	if errors.As(err, &settingsErr) {
		writeError(w, r, allowedOrigins, settingsErr.StatusCode, settingsErr.Detail)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, allowedOrigins, profileErr)
		return
	}
	writeError(w, r, allowedOrigins, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, statusCode int, detail any) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if _, ok := allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}
	writeJSON(w, statusCode, map[string]any{"detail": detail})
}

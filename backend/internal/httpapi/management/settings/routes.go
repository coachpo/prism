package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/providercompat"
)

var currencyCodeRE = regexp.MustCompile(`^[A-Z]{3}$`)

var auditAPIFamilies = []string{
	providercompat.APIFamilyOpenAI,
	providercompat.APIFamilyAnthropic,
	providercompat.APIFamilyGemini,
}

func (s *Service) handleGetAuditSettings(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (auditSettingsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return auditSettingsResponse{}, err
		}
		rows, err := listAuditSettings(r.Context(), tx, profile.ID)
		if err != nil {
			return auditSettingsResponse{}, err
		}
		return buildAuditSettingsResponse(profile.ID, rows), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutAuditSettings(w http.ResponseWriter, r *http.Request) {
	var requestBody auditSettingsUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateAuditSettingsRequest(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (auditSettingsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return auditSettingsResponse{}, err
		}
		if err := replaceAuditSettings(r.Context(), tx, profile.ID, requestBody.Settings, s.nowUTC()); err != nil {
			return auditSettingsResponse{}, err
		}
		rows, err := listAuditSettings(r.Context(), tx, profile.ID)
		if err != nil {
			return auditSettingsResponse{}, err
		}
		return buildAuditSettingsResponse(profile.ID, rows), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

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
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutCostingSettings(w http.ResponseWriter, r *http.Request) {
	var requestBody costingSettingsUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateCostingRequest(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
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
		writeDomainError(w, r, s.corsSnapshot(), err)
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
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutTimezonePreference(w http.ResponseWriter, r *http.Request) {
	var requestBody timezonePreferenceUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateTimezoneRequest(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
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
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetRetentionSettings(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (retentionSettingsResponse, error) {
		settingsRow, err := loadOrCreateLogRetentionSettings(r.Context(), tx, s.nowUTC())
		if err != nil {
			return retentionSettingsResponse{}, err
		}
		return buildRetentionSettingsResponse(settingsRow), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutRetentionSettings(w http.ResponseWriter, r *http.Request) {
	var requestBody retentionSettingsUpdateRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := normalizeAndValidateRetentionRequest(&requestBody); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (retentionSettingsResponse, error) {
		settingsRow, err := loadOrCreateLogRetentionSettings(r.Context(), tx, s.nowUTC())
		if err != nil {
			return retentionSettingsResponse{}, err
		}
		settingsRow.RequestLogsRetentionDays = requestBody.RequestLogsRetentionDays
		settingsRow.AuditLogsRetentionDays = requestBody.AuditLogsRetentionDays
		settingsRow.StatisticsRetentionDays = requestBody.StatisticsRetentionDays
		settingsRow.LoadbalanceEventsRetentionDays = requestBody.LoadbalanceEventsRetentionDays
		settingsRow.UpdatedAt = s.nowUTC()
		if err := updateLogRetentionSettings(r.Context(), tx, settingsRow); err != nil {
			return retentionSettingsResponse{}, err
		}
		return buildRetentionSettingsResponse(settingsRow), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleCreateLogRetentionJob(w http.ResponseWriter, r *http.Request) {
	var requestBody logRetentionJobRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	requestBody.Table = strings.TrimSpace(requestBody.Table)
	requestBody.Reason = strings.TrimSpace(requestBody.Reason)
	if requestBody.Table == "" {
		writeDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "table is required"})
		return
	}
	cutoff := requestBody.Cutoff
	if cutoff == nil && !requestBody.DeleteAll {
		settingsRow, err := pgxutil.InTxValue(r.Context(), s.pool, "settings", func(tx pgx.Tx) (logRetentionSettingsRow, error) {
			return loadOrCreateLogRetentionSettings(r.Context(), tx, s.nowUTC())
		})
		if err != nil {
			writeDomainError(w, r, s.corsSnapshot(), err)
			return
		}
		retentionDays := retentionDaysForTable(settingsRow, requestBody.Table)
		if retentionDays == nil {
			writeDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "No log retention policy configured; provide cutoff or delete_all=true, or configure the table retention days in /api/settings/log-retention"})
			return
		}
		computed := s.nowUTC().Add(-time.Duration(*retentionDays) * 24 * time.Hour)
		cutoff = &computed
	}
	job, err := s.jobs.CreateLogRetentionJob(r.Context(), managementjobs.CreateLogRetentionJobRequest{RequestedBy: "global", IdempotencyKey: r.Header.Get("Idempotency-Key"), Reason: requestBody.Reason, Scope: managementjobs.LogRetentionScope{Table: requestBody.Table, Cutoff: cutoff, DeleteAll: requestBody.DeleteAll}})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), retentionJobError(err))
		return
	}
	w.Header().Set("Location", "/api/management/jobs/"+job.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "state": job.State, "status_url": "/api/management/jobs/" + job.ID, "scope": job.Scope})
}

func retentionDaysForTable(settingsRow logRetentionSettingsRow, tableName string) *int {
	switch tableName {
	case "request_logs":
		return settingsRow.RequestLogsRetentionDays
	case "audit_logs":
		return settingsRow.AuditLogsRetentionDays
	case "usage_request_events":
		return settingsRow.StatisticsRetentionDays
	case "loadbalance_events":
		return settingsRow.LoadbalanceEventsRetentionDays
	default:
		return nil
	}
}

func retentionJobError(err error) error {
	if err == nil {
		return nil
	}
	switch err.Error() {
	case "retention_scope_required", "retention_table_required", "retention_table_unknown":
		return &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
	default:
		return err
	}
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

func buildRetentionSettingsResponse(settingsRow logRetentionSettingsRow) retentionSettingsResponse {
	return retentionSettingsResponse{
		RequestLogsRetentionDays:       settingsRow.RequestLogsRetentionDays,
		AuditLogsRetentionDays:         settingsRow.AuditLogsRetentionDays,
		StatisticsRetentionDays:        settingsRow.StatisticsRetentionDays,
		LoadbalanceEventsRetentionDays: settingsRow.LoadbalanceEventsRetentionDays,
	}
}

func buildAuditSettingsResponse(profileID int, rows []auditSettingsRow) auditSettingsResponse {
	byFamily := make(map[string]auditSetting, len(rows))
	for _, row := range rows {
		family := providercompat.NormalizeAPIFamily(row.APIFamily)
		byFamily[family] = auditSetting{APIFamily: family, AuditEnabled: row.AuditEnabled, AuditCaptureBodies: row.AuditCaptureBodies}
	}
	settings := make([]auditSetting, 0, len(auditAPIFamilies))
	for _, family := range auditAPIFamilies {
		setting, ok := byFamily[family]
		if !ok {
			setting = auditSetting{APIFamily: family}
		}
		settings = append(settings, setting)
	}
	return auditSettingsResponse{ProfileID: profileID, Settings: settings}
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

func normalizeAndValidateAuditSettingsRequest(requestBody *auditSettingsUpdateRequest) error {
	if len(requestBody.Settings) != len(auditAPIFamilies) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "settings must include exactly openai, anthropic, and gemini"}
	}

	seen := make(map[string]auditSetting, len(auditAPIFamilies))
	for index := range requestBody.Settings {
		setting := requestBody.Settings[index]
		family := providercompat.NormalizeAPIFamily(setting.APIFamily)
		if !isAuditAPIFamily(family) {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("api_family %q is not supported", setting.APIFamily)}
		}
		if _, ok := seen[family]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate audit setting for api_family=%s", family)}
		}
		if !setting.AuditEnabled && setting.AuditCaptureBodies {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "audit_capture_bodies requires audit_enabled"}
		}
		setting.APIFamily = family
		seen[family] = setting
	}

	normalized := make([]auditSetting, 0, len(auditAPIFamilies))
	for _, family := range auditAPIFamilies {
		setting, ok := seen[family]
		if !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("settings must include api_family=%s", family)}
		}
		normalized = append(normalized, setting)
	}
	requestBody.Settings = normalized
	return nil
}

func isAuditAPIFamily(value string) bool {
	if !providercompat.IsSupportedAPIFamily(value) {
		return false
	}
	return slices.Contains(auditAPIFamilies, value)
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

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if settingsErr, ok := errors.AsType[*domainError](err); ok {
		writeError(w, r, corsSnapshot, settingsErr.StatusCode, settingsErr.Detail)
		return
	}
	if profileErr, ok := errors.AsType[*profiledomain.HTTPError](err); ok {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail any) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]any{"detail": detail})
}

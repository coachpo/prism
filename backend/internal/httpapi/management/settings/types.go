package settings

import "time"

type endpointFXMapping struct {
	ModelID    string `json:"model_id"`
	EndpointID int    `json:"endpoint_id"`
	FXRate     string `json:"fx_rate"`
}

type costingSettingsResponse struct {
	ProfileID            int                 `json:"profile_id"`
	ReportCurrencyCode   string              `json:"report_currency_code"`
	ReportCurrencySymbol string              `json:"report_currency_symbol"`
	TimezonePreference   *string             `json:"timezone_preference"`
	EndpointFXMappings   []endpointFXMapping `json:"endpoint_fx_mappings"`
}

type timezonePreferenceResponse struct {
	ProfileID          int     `json:"profile_id"`
	TimezonePreference *string `json:"timezone_preference"`
}

type retentionSettingsResponse struct {
	ProfileID                int  `json:"profile_id"`
	RequestLogsRetentionDays *int `json:"request_logs_retention_days"`
	StatisticsRetentionDays  *int `json:"statistics_retention_days"`
	AuditLogsRetentionDays   *int `json:"audit_logs_retention_days"`
}

type costingSettingsUpdateRequest struct {
	ProfileID            *int                `json:"profile_id"`
	ReportCurrencyCode   string              `json:"report_currency_code"`
	ReportCurrencySymbol string              `json:"report_currency_symbol"`
	TimezonePreference   *string             `json:"timezone_preference"`
	EndpointFXMappings   []endpointFXMapping `json:"endpoint_fx_mappings"`
}

type timezonePreferenceUpdateRequest struct {
	TimezonePreference *string `json:"timezone_preference"`
}

type retentionSettingsUpdateRequest struct {
	RequestLogsRetentionDays *int `json:"request_logs_retention_days"`
	StatisticsRetentionDays  *int `json:"statistics_retention_days"`
	AuditLogsRetentionDays   *int `json:"audit_logs_retention_days"`
}

type userSettingsRow struct {
	ID                       int
	ProfileID                int
	ReportCurrencyCode       string
	ReportCurrencySymbol     string
	TimezonePreference       *string
	RequestLogsRetentionDays *int
	StatisticsRetentionDays  *int
	AuditLogsRetentionDays   *int
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

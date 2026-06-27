package configbundle

import (
	"bytes"
	"encoding/json"
	"time"
)

type profileBundleResponse struct {
	Version               int                         `json:"version"`
	BundleKind            string                      `json:"bundle_kind"`
	ExportedAt            time.Time                   `json:"exported_at"`
	Endpoints             []endpointExport            `json:"endpoints"`
	PricingTemplates      []pricingTemplateExport     `json:"pricing_templates"`
	Connections           []connectionExport          `json:"connections"`
	LoadbalanceStrategies []loadbalanceStrategyExport `json:"loadbalance_strategies"`
	Models                []modelExport               `json:"models"`
	ProfileSettings       profileSettingsExport       `json:"profile_settings"`
	HeaderBlocklistRules  []headerBlocklistRuleExport `json:"header_blocklist_rules"`
	UserAgentClientRules  []userAgentClientRuleExport `json:"user_agent_client_rules"`
	SecretPayload         secretPayloadExport         `json:"secret_payload"`
}

type endpointExport struct {
	Name            string  `json:"name"`
	BaseURL         string  `json:"base_url"`
	APIKeySecretRef *string `json:"api_key_secret_ref"`
	Position        int     `json:"position"`
}

type pricingTemplateExport struct {
	Name                string  `json:"name"`
	Description         *string `json:"description"`
	PricingUnit         string  `json:"pricing_unit"`
	PricingCurrencyCode string  `json:"pricing_currency_code"`
	InputPrice          string  `json:"input_price"`
	OutputPrice         string  `json:"output_price"`
	CachedInputPrice    string  `json:"cached_input_price"`
	CacheCreationPrice  string  `json:"cache_creation_price"`
	ReasoningPrice      string  `json:"reasoning_price"`
	Version             int     `json:"version"`
}

type loadbalanceStrategyExport struct {
	Name                               string   `json:"name"`
	LegacyStrategyType                 *string  `json:"legacy_strategy_type"`
	FailureStatusCodes                 []int    `json:"failure_status_codes"`
	BanMode                            *string  `json:"ban_mode"`
	RetryBaseDelayMS                   *int     `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier             *float64 `json:"retry_backoff_multiplier"`
	RetryJitterRatio                   *float64 `json:"retry_jitter_ratio"`
	RetryMaxDelayMS                    *int     `json:"retry_max_delay_ms"`
	CycleRetryAttemptLimit             *int     `json:"cycle_retry_attempt_limit"`
	BanCumulativeRetryAttemptThreshold *int     `json:"ban_cumulative_retry_attempt_threshold"`
	BanDurationSeconds                 *int     `json:"ban_duration_seconds"`
}

type modelExport struct {
	APIFamily               string               `json:"api_family"`
	ModelID                 string               `json:"model_id"`
	DisplayName             *string              `json:"display_name"`
	LoadbalanceStrategyName *string              `json:"loadbalance_strategy_name"`
	OpenAIAcceptedFormat    *string              `json:"openai_accepted_format"`
	IsEnabled               bool                 `json:"is_enabled"`
	AccessTargets           []accessTargetExport `json:"access_targets"`
}

type accessTargetExport struct {
	Position      int     `json:"position"`
	IsEnabled     bool    `json:"is_enabled"`
	TargetType    string  `json:"target_type"`
	ConnectionRef *string `json:"connection_ref"`
	TargetModelID *string `json:"target_model_id"`
}

type connectionExport struct {
	Ref                        string            `json:"ref"`
	APIFamily                  string            `json:"api_family"`
	EndpointName               string            `json:"endpoint_name"`
	PricingTemplateName        *string           `json:"pricing_template_name"`
	IsActive                   bool              `json:"is_active"`
	Priority                   int               `json:"priority"`
	Name                       *string           `json:"name"`
	AuthType                   *string           `json:"auth_type"`
	CustomHeaders              map[string]string `json:"custom_headers"`
	OpenAIProbeEndpointVariant *string           `json:"openai_probe_endpoint_variant,omitempty"`
	OpenAITextCapability       *string           `json:"openai_text_capability"`
	OpenAITextCapabilitySet    bool              `json:"-"`
	QPSLimit                   *int              `json:"qps_limit"`
	MaxInFlightNonStream       *int              `json:"max_in_flight_non_stream"`
	MaxInFlightStream          *int              `json:"max_in_flight_stream"`
}

func (connection *connectionExport) UnmarshalJSON(data []byte) error {
	type connectionExportAlias connectionExport
	var decoded connectionExportAlias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decoded.OpenAITextCapabilitySet = false
	if _, ok := raw["openai_text_capability"]; ok {
		decoded.OpenAITextCapabilitySet = true
	}
	*connection = connectionExport(decoded)
	return nil
}

type profileSettingsExport struct {
	ReportCurrencyCode          string                        `json:"report_currency_code"`
	ReportCurrencySymbol        string                        `json:"report_currency_symbol"`
	TimezonePreference          *string                       `json:"timezone_preference"`
	EndpointFXMappings          []endpointFXMappingExport     `json:"endpoint_fx_mappings"`
	AuditAPIFamilySettings      []auditAPIFamilySettingExport `json:"audit_api_family_settings"`
	AuditAPIFamilySettingsIsSet bool                          `json:"-"`
}

func (settings *profileSettingsExport) UnmarshalJSON(data []byte) error {
	type profileSettingsExportAlias profileSettingsExport
	var decoded profileSettingsExportAlias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decoded.AuditAPIFamilySettingsIsSet = false
	if _, ok := raw["audit_api_family_settings"]; ok {
		decoded.AuditAPIFamilySettingsIsSet = true
	}
	*settings = profileSettingsExport(decoded)
	return nil
}

type auditAPIFamilySettingExport struct {
	APIFamily          string `json:"api_family"`
	AuditEnabled       bool   `json:"audit_enabled"`
	AuditCaptureBodies bool   `json:"audit_capture_bodies"`
}

type endpointFXMappingExport struct {
	ModelID       string `json:"model_id"`
	ConnectionRef string `json:"connection_ref"`
	FXRate        string `json:"fx_rate"`
}

type headerBlocklistRuleExport struct {
	Name      string `json:"name"`
	MatchType string `json:"match_type"`
	Pattern   string `json:"pattern"`
	Enabled   bool   `json:"enabled"`
}

type userAgentClientRuleExport struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Enabled bool   `json:"enabled"`
}

type secretPayloadExport struct {
	Kind    string               `json:"kind"`
	Cipher  string               `json:"cipher"`
	KeyID   string               `json:"key_id"`
	Entries []secretPayloadEntry `json:"entries"`
}

type secretPayloadEntry struct {
	Ref        string `json:"ref"`
	Ciphertext string `json:"ciphertext"`
}

type profileImportReplacementScope struct {
	Target                string `json:"target"`
	Endpoints             int    `json:"endpoints"`
	PricingTemplates      int    `json:"pricing_templates"`
	LoadbalanceStrategies int    `json:"loadbalance_strategies"`
	Models                int    `json:"models"`
	Connections           int    `json:"connections"`
	HeaderBlocklistRules  int    `json:"header_blocklist_rules"`
	UserAgentClientRules  int    `json:"user_agent_client_rules"`
	ProfileSettings       bool   `json:"profile_settings"`
}

type profileImportUntouchedScope struct {
	OtherProfiles bool `json:"other_profiles"`
	RequestLogs   bool `json:"request_logs"`
}

type profileImportSecretSummary struct {
	EndpointSecretRefs    int `json:"endpoint_secret_refs"`
	SecretPayloadEntries  int `json:"secret_payload_entries"`
	DecryptableSecretRefs int `json:"decryptable_secret_refs"`
}

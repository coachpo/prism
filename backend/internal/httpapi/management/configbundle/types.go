package configbundle

import (
	"encoding/json"
	"time"
)

type profileBundleResponse struct {
	Version               int                         `json:"version"`
	BundleKind            string                      `json:"bundle_kind"`
	ExportedAt            time.Time                   `json:"exported_at"`
	VendorRefs            []vendorRefExport           `json:"vendor_refs"`
	Endpoints             []endpointExport            `json:"endpoints"`
	PricingTemplates      []pricingTemplateExport     `json:"pricing_templates"`
	LoadbalanceStrategies []loadbalanceStrategyExport `json:"loadbalance_strategies"`
	Models                []modelExport               `json:"models"`
	ProfileSettings       profileSettingsExport       `json:"profile_settings"`
	HeaderBlocklistRules  []headerBlocklistRuleExport `json:"header_blocklist_rules"`
	SecretPayload         secretPayloadExport         `json:"secret_payload"`
}

type vendorCatalogResponse struct {
	Version    int                `json:"version"`
	BundleKind string             `json:"bundle_kind"`
	ExportedAt time.Time          `json:"exported_at"`
	Vendors    []vendorCatalogRow `json:"vendors"`
}

type vendorRefExport struct {
	Key             string  `json:"key"`
	NameHint        string  `json:"name_hint"`
	DescriptionHint *string `json:"description_hint"`
	IconKeyHint     *string `json:"icon_key_hint"`
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
	CachedInputPrice    *string `json:"cached_input_price"`
	CacheCreationPrice  *string `json:"cache_creation_price"`
	ReasoningPrice      *string `json:"reasoning_price"`
	Version             int     `json:"version"`
}

type loadbalanceStrategyExport struct {
	Name               string          `json:"name"`
	StrategyType       string          `json:"strategy_type"`
	LegacyStrategyType *string         `json:"legacy_strategy_type"`
	AutoRecovery       json.RawMessage `json:"auto_recovery"`
	RoutingPolicy      json.RawMessage `json:"routing_policy"`
}

type modelExport struct {
	VendorKey               *string             `json:"vendor_key"`
	APIFamily               string              `json:"api_family"`
	ModelID                 string              `json:"model_id"`
	DisplayName             *string             `json:"display_name"`
	ModelType               string              `json:"model_type"`
	ProxyTargets            []proxyTargetExport `json:"proxy_targets"`
	LoadbalanceStrategyName *string             `json:"loadbalance_strategy_name"`
	IsEnabled               bool                `json:"is_enabled"`
	Connections             []connectionExport  `json:"connections"`
}

type proxyTargetExport struct {
	TargetModelID string `json:"target_model_id"`
	Position      int    `json:"position"`
}

type connectionExport struct {
	EndpointName               string            `json:"endpoint_name"`
	PricingTemplateName        *string           `json:"pricing_template_name"`
	IsActive                   bool              `json:"is_active"`
	Priority                   int               `json:"priority"`
	Name                       *string           `json:"name"`
	AuthType                   *string           `json:"auth_type"`
	CustomHeaders              map[string]string `json:"custom_headers"`
	OpenAIProbeEndpointVariant *string           `json:"openai_probe_endpoint_variant,omitempty"`
	QPSLimit                   *int              `json:"qps_limit"`
	MaxInFlightNonStream       *int              `json:"max_in_flight_non_stream"`
	MaxInFlightStream          *int              `json:"max_in_flight_stream"`
}

type profileSettingsExport struct {
	ReportCurrencyCode   string                    `json:"report_currency_code"`
	ReportCurrencySymbol string                    `json:"report_currency_symbol"`
	TimezonePreference   *string                   `json:"timezone_preference"`
	EndpointFXMappings   []endpointFXMappingExport `json:"endpoint_fx_mappings"`
}

type endpointFXMappingExport struct {
	ModelID      string `json:"model_id"`
	EndpointName string `json:"endpoint_name"`
	FXRate       string `json:"fx_rate"`
}

type headerBlocklistRuleExport struct {
	Name      string `json:"name"`
	MatchType string `json:"match_type"`
	Pattern   string `json:"pattern"`
	Enabled   bool   `json:"enabled"`
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

type vendorCatalogRow struct {
	Key                string  `json:"key"`
	Name               string  `json:"name"`
	Description        *string `json:"description"`
	IconKey            *string `json:"icon_key"`
	AuditEnabled       bool    `json:"audit_enabled"`
	AuditCaptureBodies bool    `json:"audit_capture_bodies"`
}

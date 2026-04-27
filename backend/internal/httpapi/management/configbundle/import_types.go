package configbundle

import (
	"time"
)

type profileImportRequest struct {
	Version               int                         `json:"version"`
	BundleKind            string                      `json:"bundle_kind"`
	ExportedAt            time.Time                   `json:"exported_at"`
	VendorRefs            []vendorRefExport           `json:"vendor_refs"`
	Endpoints             []endpointExport            `json:"endpoints"`
	PricingTemplates      []pricingTemplateExport     `json:"pricing_templates"`
	LoadbalanceStrategies []loadbalanceStrategyExport `json:"loadbalance_strategies"`
	Models                []modelExport               `json:"models"`
	ProfileSettings       *profileSettingsExport      `json:"profile_settings"`
	HeaderBlocklistRules  []headerBlocklistRuleExport `json:"header_blocklist_rules"`
	UserAgentClientRules  []userAgentClientRuleExport `json:"user_agent_client_rules"`
	SecretPayload         secretPayloadExport         `json:"secret_payload"`
}

type profileImportPreviewResponse struct {
	Ready                    bool                            `json:"ready"`
	Version                  int                             `json:"version"`
	BundleKind               string                          `json:"bundle_kind"`
	EndpointsImported        int                             `json:"endpoints_imported"`
	PricingTemplatesImported int                             `json:"pricing_templates_imported"`
	StrategiesImported       int                             `json:"strategies_imported"`
	ModelsImported           int                             `json:"models_imported"`
	ConnectionsImported      int                             `json:"connections_imported"`
	VendorResolutions        []profileImportVendorResolution `json:"vendor_resolutions"`
	SecretKeyID              string                          `json:"secret_key_id"`
	DecryptableSecretRefs    []string                        `json:"decryptable_secret_refs"`
	BlockingErrors           []string                        `json:"blocking_errors"`
	Warnings                 []string                        `json:"warnings"`
}

type profileImportVendorResolution struct {
	VendorKey  string  `json:"vendor_key"`
	Resolution string  `json:"resolution"`
	Warning    *string `json:"warning"`
}

type profileImportResponse struct {
	EndpointsImported        int `json:"endpoints_imported"`
	PricingTemplatesImported int `json:"pricing_templates_imported"`
	StrategiesImported       int `json:"strategies_imported"`
	ModelsImported           int `json:"models_imported"`
	ConnectionsImported      int `json:"connections_imported"`
}

type vendorCatalogImportRequest struct {
	Version    int                `json:"version"`
	BundleKind string             `json:"bundle_kind"`
	ExportedAt time.Time          `json:"exported_at"`
	Vendors    []vendorCatalogRow `json:"vendors"`
}

type vendorCatalogImportPreviewResponse struct {
	Ready          bool     `json:"ready"`
	Version        int      `json:"version"`
	BundleKind     string   `json:"bundle_kind"`
	CreateCount    int      `json:"create_count"`
	UpdateCount    int      `json:"update_count"`
	BlockingErrors []string `json:"blocking_errors"`
	Warnings       []string `json:"warnings"`
}

type vendorCatalogImportResponse struct {
	CreatedCount int `json:"created_count"`
	UpdatedCount int `json:"updated_count"`
}

type importedStrategyPayload struct {
	Name               string
	StrategyType       string
	LegacyStrategyType *string
	AutoRecoveryJSON   []byte
	RoutingPolicyJSON  []byte
}

type secretPayloadEntryMap map[string]string

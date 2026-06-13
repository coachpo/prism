package configbundle

import (
	"time"
)

type profileImportRequest struct {
	Version               int                         `json:"version"`
	BundleKind            string                      `json:"bundle_kind"`
	ExportedAt            time.Time                   `json:"exported_at"`
	Endpoints             []endpointExport            `json:"endpoints"`
	PricingTemplates      []pricingTemplateExport     `json:"pricing_templates"`
	Connections           []connectionExport          `json:"connections"`
	LoadbalanceStrategies []loadbalanceStrategyExport `json:"loadbalance_strategies"`
	Models                []modelExport               `json:"models"`
	ProfileSettings       *profileSettingsExport      `json:"profile_settings"`
	HeaderBlocklistRules  []headerBlocklistRuleExport `json:"header_blocklist_rules"`
	UserAgentClientRules  []userAgentClientRuleExport `json:"user_agent_client_rules"`
	SecretPayload         secretPayloadExport         `json:"secret_payload"`
}

type profileImportPreviewResponse struct {
	Ready                    bool                          `json:"ready"`
	Version                  int                           `json:"version"`
	BundleKind               string                        `json:"bundle_kind"`
	PreviewToken             string                        `json:"preview_token"`
	BundleFingerprint        string                        `json:"bundle_fingerprint"`
	ReplacementScope         profileImportReplacementScope `json:"replacement_scope"`
	UntouchedScope           profileImportUntouchedScope   `json:"untouched_scope"`
	SecretSummary            profileImportSecretSummary    `json:"secret_summary"`
	EndpointsImported        int                           `json:"endpoints_imported"`
	PricingTemplatesImported int                           `json:"pricing_templates_imported"`
	StrategiesImported       int                           `json:"strategies_imported"`
	ModelsImported           int                           `json:"models_imported"`
	ConnectionsImported      int                           `json:"connections_imported"`
	SecretKeyID              string                        `json:"secret_key_id"`
	DecryptableSecretRefs    []string                      `json:"decryptable_secret_refs"`
	BlockingErrors           []string                      `json:"blocking_errors"`
	Warnings                 []string                      `json:"warnings"`
}

type profileImportResponse struct {
	EndpointsImported        int `json:"endpoints_imported"`
	PricingTemplatesImported int `json:"pricing_templates_imported"`
	StrategiesImported       int `json:"strategies_imported"`
	ModelsImported           int `json:"models_imported"`
	ConnectionsImported      int `json:"connections_imported"`
}

type importedStrategyPayload struct {
	Name                               string
	LegacyStrategyType                 string
	FailureStatusCodes                 []int
	BanMode                            string
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
}

type importedModelPayload struct {
	APIFamily                            string
	ModelID                              string
	DisplayName                          *string
	LoadbalanceStrategyName              *string
	ContextWindowTokens                  *int
	DefaultOutputTokenReserve            *int
	MaxContextUtilization                *float64
	PreferredContextUtilizationThreshold *float64
	FacadeEnabled                        bool
	FacadeSelectionPolicy                *string
	FacadeFallbackPolicy                 *string
	ContextOverflowPromotionTargetID     *string
	ContextOverflowPromotionTargetSet    bool
	IsEnabled                            bool
	AccessTargets                        []importedAccessTargetPayload
}

type importedAccessTargetPayload struct {
	Position               int
	IsEnabled              bool
	TargetType             string
	ConnectionRef          *string
	TargetModelID          *string
	Weight                 *int
	ResolvedWeight         int
	TargetPriority         *int
	ResolvedTargetPriority int
}

type secretPayloadEntryMap map[string]string

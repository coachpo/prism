package models

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
)

// exportSourceModelRow is one source response model row: selection truth,
// layered metadata with provenance/missing, catalog evidence, per-platform
// completeness, price risk, and the replayable enrichment candidate.
type exportSourceModelRow struct {
	ModelConfigID         int     `json:"model_config_id"`
	ModelID               string  `json:"model_id"`
	APIFamily             string  `json:"api_family"`
	DisplayName           *string `json:"display_name"`
	IsEnabled             bool    `json:"is_enabled"`
	DefaultSelected       bool    `json:"default_selected"`
	Selectable            bool    `json:"selectable"`
	UnselectableReason    *string `json:"unselectable_reason,omitempty"`
	OpenAIAcceptedFormat  *string `json:"openai_accepted_format,omitempty"`
	OpenAIImageOperations *string `json:"openai_image_operations,omitempty"`

	Catalog exportCatalogEvidenceWire `json:"catalog"`
	// Enrichment reports whether live models.dev facts back this row.
	Enrichment   exportEnrichmentEvidenceWire   `json:"enrichment"`
	Prism        map[string]json.RawMessage     `json:"prism_metadata"`
	ModelsDev    map[string]json.RawMessage     `json:"models_dev_metadata"`
	Merged       map[string]json.RawMessage     `json:"merged_metadata"`
	Provenance   map[string]string              `json:"metadata_provenance"`
	Missing      []string                       `json:"missing_metadata"`
	Completeness exportPlatformCompletenessWire `json:"platform_completeness"`

	Targets   []exportSourceTargetRow `json:"targets"`
	PriceRisk exportPriceRiskWire     `json:"price_risk"`
	Warnings  []string                `json:"warnings,omitempty"`

	// EnrichmentCandidate is the exact payload render replays; nil when
	// enrichment was unavailable.
	EnrichmentCandidate *enrichmentCandidateWire `json:"enrichment_candidate"`
}

type exportCatalogEvidenceWire struct {
	Bound           bool   `json:"bound"`
	ProviderID      string `json:"provider_id,omitempty"`
	CatalogModelID  string `json:"catalog_model_id,omitempty"`
	CatalogRevision string `json:"catalog_revision,omitempty"`
	MatchSource     string `json:"match_source,omitempty"`
	HasOverrides    bool   `json:"has_overrides"`
}

type exportEnrichmentEvidenceWire struct {
	Available          bool   `json:"available"`
	OfferingProviderID string `json:"offering_provider_id,omitempty"`
	OfferingModelID    string `json:"offering_model_id,omitempty"`
}

// exportPlatformCompletenessWire states which client-facing fields this
// platform's file will carry for the model, plus whether a cost group can be
// expressed. Absent never renders as zero anywhere downstream.
type exportPlatformCompletenessWire struct {
	MetadataFields map[string]bool `json:"metadata_fields"`
	CostExportable bool            `json:"cost_exportable"`
}

type exportPriceCardWire struct {
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

type exportTargetPricingWire struct {
	TerminalTargetID int                  `json:"terminal_target_id"`
	Kind             string               `json:"template_kind"`
	CurrencyCode     string               `json:"currency_code"`
	PricingUnit      string               `json:"pricing_unit"`
	TierThreshold    *int                 `json:"tier_threshold,omitempty"`
	Card             *exportPriceCardWire `json:"card,omitempty"`
	BaseCard         *exportPriceCardWire `json:"base_card,omitempty"`
	AboveCard        *exportPriceCardWire `json:"above_card,omitempty"`
}

type exportSourceTargetRow struct {
	TerminalTargetID     int                      `json:"terminal_target_id"`
	Position             int                      `json:"position"`
	EndpointID           int                      `json:"endpoint_id"`
	EndpointName         string                   `json:"endpoint_name"`
	OpenAITextCapability *string                  `json:"openai_text_capability,omitempty"`
	Pricing              *exportTargetPricingWire `json:"pricing,omitempty"`
}

type exportPriceRiskWire struct {
	// Exportable mirrors platform cost expressibility after every gate.
	Exportable   bool     `json:"exportable"`
	WarningCodes []string `json:"warning_codes,omitempty"`
}

// enrichmentCandidateWire is the lossless round-trip shape of one derived
// models.dev candidate: canonical metadata leaves plus platform-derived
// output fields.
type enrichmentCandidateWire struct {
	Metadata map[string]json.RawMessage `json:"metadata"`
	Derived  map[string]json.RawMessage `json:"derived,omitempty"`
	Warnings []string                   `json:"warnings,omitempty"`
}

func encodeEnrichmentCandidate(candidate modelexport.PlatformCandidate) *enrichmentCandidateWire {
	if candidate.Metadata.Empty() && len(candidate.DerivedFields) == 0 && len(candidate.WarningCodes) == 0 {
		return nil
	}
	wire := &enrichmentCandidateWire{Metadata: map[string]json.RawMessage{}}
	for _, leaf := range candidate.Metadata.Leaves() {
		value, _ := candidate.Metadata.Get(leaf)
		wire.Metadata[leaf] = value
	}
	if len(candidate.DerivedFields) > 0 {
		wire.Derived = map[string]json.RawMessage{}
		for field, raw := range candidate.DerivedFields {
			wire.Derived[field] = raw
		}
	}
	wire.Warnings = modelexport.MergeWarningCodes(candidate.WarningCodes)
	return wire
}

func (w *enrichmentCandidateWire) decode() modelexport.PlatformCandidate {
	if w == nil {
		return modelexport.PlatformCandidate{}
	}
	candidate := modelexport.PlatformCandidate{Metadata: modelexport.NewMetadataLayer(w.Metadata)}
	if len(w.Derived) > 0 {
		candidate.DerivedFields = map[string]json.RawMessage{}
		for field, raw := range w.Derived {
			candidate.DerivedFields[field] = raw
		}
	}
	candidate.WarningCodes = modelexport.MergeWarningCodes(w.Warnings)
	return candidate
}

type exportSourceResponse struct {
	Platform        string                 `json:"platform"`
	TargetVersion   string                 `json:"target_version"`
	CatalogRevision string                 `json:"catalog_revision,omitempty"`
	Models          []exportSourceModelRow `json:"models"`
	SourceDigest    string                 `json:"source_digest"`
	Warnings        []string               `json:"warnings,omitempty"`
}

// exportRenderRequest is the strict render body. ExpectedSourceDigest pins
// the snapshot; ModelConfigIDs is the explicit selection truth; Enhancements
// carry the manual third layer. Catalog candidates are always server-owned.
type exportRenderRequest struct {
	ExpectedSourceDigest string                         `json:"expected_source_digest"`
	ModelConfigIDs       []int                          `json:"model_config_ids"`
	BaseURL              string                         `json:"base_url"`
	ProviderID           string                         `json:"provider_id,omitempty"`
	Enhancements         map[int]*manualEnhancementWire `json:"enhancements,omitempty"`
	Credential           exportCredentialWire           `json:"credential,omitempty"`
	DefaultModelConfigID *int                           `json:"default_model_config_id,omitempty"`
	// DeprecatedEnrichmentCandidates is accepted only for compatibility with
	// pre-release callers. It is deliberately opaque and never decoded, hashed,
	// compared, or passed to a renderer; catalog evidence remains server-owned.
	DeprecatedEnrichmentCandidates json.RawMessage `json:"enrichment_candidates,omitempty"`
}

type exportCredentialWire struct {
	Include bool   `json:"include"`
	APIKey  string `json:"api_key,omitempty"`
}

type manualEnhancementWire struct {
	Fields         json.RawMessage `json:"fields,omitempty"`
	OverrideFields []string        `json:"override_fields,omitempty"`
}

func (w *manualEnhancementWire) decode() modelexport.ManualEnhancement {
	if w == nil {
		return modelexport.ManualEnhancement{}
	}
	return modelexport.ManualEnhancement{Fields: w.Fields, OverrideFields: w.OverrideFields}
}

type exportRenderResponse struct {
	Platform        string                          `json:"platform"`
	TargetVersion   string                          `json:"target_version"`
	CatalogRevision string                          `json:"catalog_revision,omitempty"`
	Content         string                          `json:"content"`
	ContentSHA256   string                          `json:"content_sha256"`
	FileName        string                          `json:"file_name"`
	MIMEType        string                          `json:"mime_type"`
	ModelResults    []modelexport.ModelRenderResult `json:"model_results"`
	Warnings        []string                        `json:"warnings,omitempty"`
}

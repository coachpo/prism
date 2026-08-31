package models

import (
	"encoding/json"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
)

// Pi export wires - static routes without platform param, Pi-only.

type piSelectedWire struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	API        string `json:"api"`
}

type piSourceModelRow struct {
	ModelConfigID         int     `json:"model_config_id"`
	ModelID               string  `json:"model_id"`
	APIFamily             string  `json:"api_family"`
	DisplayName           *string `json:"display_name"`
	IsEnabled             bool    `json:"is_enabled"`
	Selectable            bool    `json:"selectable"`
	UnselectableReason    *string `json:"unselectable_reason,omitempty"`
	OpenAIAcceptedFormat  *string `json:"openai_accepted_format,omitempty"`
	OpenAIImageOperations *string `json:"openai_image_operations,omitempty"`
	// PiAPI is the final Pi API Prism maps this model to, or empty when the
	// family/accepted-format pair has no Pi text API. Directory search and bind
	// are offered only for a model whose PiAPI is determinable, and every
	// offered coordinate must carry exactly this value.
	PiAPI string `json:"pi_api,omitempty"`

	Targets   []exportSourceTargetRow `json:"targets"`
	PriceRisk exportPriceRiskWire     `json:"price_risk"`
	Warnings  []string                `json:"warnings,omitempty"`

	// PiCandidates/CandidateStatus are live pi.dev catalog evidence: every
	// entry currently matching this model's exact model_id and expected Pi
	// API, and a summary of that search (not_in_catalog/api_mismatch/single/
	// multiple/catalog_unavailable). They never select or gate anything by
	// themselves.
	PiCandidates    []piCandidateWire `json:"pi_candidates"`
	CandidateStatus string            `json:"candidate_status"`
	// PiSelected/BindSource/CatalogRevision/FetchedAt/UpdatedAt/BindingStatus
	// describe the persisted model_pi_catalog_bindings row, when one exists.
	// PiSelected preserves the raw bound coordinate for rebind/unbind even
	// after Prism identity/API drift. BindingRenderable is the explicit render
	// health gate; BindingStatus separately reports live catalog drift.
	PiSelected        *piSelectedWire `json:"pi_selected,omitempty"`
	BindingStatus     string          `json:"pi_binding_status"`
	BindingRenderable bool            `json:"pi_binding_renderable"`
	BindSource        string          `json:"pi_bind_source,omitempty"`
	// BindingPrismModelID is the Prism full model id frozen at bind time.
	// A later Prism rename is checked against this value. Whether the binding is
	// cross-directory is determined separately by comparing PiSelected.ModelID
	// (the directory id) with this snapshot.
	BindingPrismModelID      string                     `json:"pi_binding_prism_model_id,omitempty"`
	CatalogRevision          string                     `json:"pi_binding_catalog_revision,omitempty"`
	FetchedAt                *time.Time                 `json:"pi_binding_fetched_at,omitempty"`
	UpdatedAt                *time.Time                 `json:"pi_binding_updated_at,omitempty"`
	BindingSourceMetadata    *piBindingMetadataPayload  `json:"pi_binding_source"`
	BindingOverrideMetadata  *piBindingMetadataPayload  `json:"pi_binding_override"`
	BindingEffectiveMetadata *piBindingMetadataPayload  `json:"pi_binding_effective"`
	BindingDroppedFields     []string                   `json:"pi_binding_dropped_fields,omitempty"`
	Prism                    map[string]json.RawMessage `json:"prism_metadata"`
	Merged                   map[string]json.RawMessage `json:"merged_metadata"`
	Provenance               map[string]string          `json:"metadata_provenance"`
	Missing                  []string                   `json:"missing_metadata"`
	Completeness             exportCompletenessWire     `json:"completeness"`
}

type piSourceResponse struct {
	TargetVersion string             `json:"target_version"`
	Catalog       piCatalogWire      `json:"catalog"`
	Models        []piSourceModelRow `json:"models"`
	SourceDigest  string             `json:"source_digest"`
	Warnings      []string           `json:"warnings,omitempty"`
}

// piRenderRequest's Selections is a pure assertion: it must name, for every
// model_config_id being rendered, the exact coordinate that model's
// model_pi_catalog_bindings row currently carries. It can never choose a
// candidate or change a binding; render fails closed on any mismatch.
type piRenderRequest struct {
	ExpectedSourceDigest string                  `json:"expected_source_digest"`
	ModelConfigIDs       []int                   `json:"model_config_ids"`
	BaseURL              string                  `json:"base_url"`
	ProviderID           string                  `json:"provider_id,omitempty"`
	Credential           exportCredentialWire    `json:"credential,omitempty"`
	Selections           map[int]*piSelectedWire `json:"selections,omitempty"`
}

type piRenderResponse struct {
	TargetVersion string                          `json:"target_version"`
	Content       string                          `json:"content"`
	ContentSHA256 string                          `json:"content_sha256"`
	FileName      string                          `json:"file_name"`
	MIMEType      string                          `json:"mime_type"`
	ModelResults  []modelexport.ModelRenderResult `json:"model_results"`
	Warnings      []string                        `json:"warnings,omitempty"`
	SourceDigest  string                          `json:"source_digest"`
}

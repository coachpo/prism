package models

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
)

type openCodeSourceModelRow struct {
	ModelConfigID         int     `json:"model_config_id"`
	ModelID               string  `json:"model_id"`
	APIFamily             string  `json:"api_family"`
	DisplayName           *string `json:"display_name"`
	IsEnabled             bool    `json:"is_enabled"`
	DirectRequestEnabled  bool    `json:"direct_request_enabled"`
	Selectable            bool    `json:"selectable"`
	UnselectableReason    *string `json:"unselectable_reason,omitempty"`
	OpenAIAcceptedFormat  *string `json:"openai_accepted_format,omitempty"`
	OpenAIImageOperations *string `json:"openai_image_operations,omitempty"`
	NPM                   string  `json:"npm"`
	APIPath               string  `json:"api_path"`

	Targets          []exportSourceTargetRow             `json:"targets"`
	PriceRisk        exportPriceRiskWire                 `json:"price_risk"`
	SourceMetadata   map[string]json.RawMessage          `json:"source_metadata"`
	OverrideMetadata map[string]json.RawMessage          `json:"override_metadata"`
	MergedMetadata   map[string]json.RawMessage          `json:"merged_metadata"`
	Provenance       map[string]string                   `json:"metadata_provenance"`
	MissingMetadata  []string                            `json:"missing_metadata"`
	MetadataIssues   []modelexport.OpenCodeMetadataIssue `json:"metadata_issues"`
	Warnings         []string                            `json:"warnings,omitempty"`
}

type openCodeSourceResponse struct {
	TargetVersion string                   `json:"target_version"`
	Models        []openCodeSourceModelRow `json:"models"`
	SourceDigest  string                   `json:"source_digest"`
	Warnings      []string                 `json:"warnings,omitempty"`
}

type openCodeRenderRequest struct {
	ExpectedSourceDigest string               `json:"expected_source_digest"`
	ModelConfigIDs       []int                `json:"model_config_ids"`
	BaseURL              string               `json:"base_url"`
	ProviderID           string               `json:"provider_id,omitempty"`
	Credential           exportCredentialWire `json:"credential,omitempty"`
}

type openCodeRenderResponse struct {
	TargetVersion string                          `json:"target_version"`
	Content       string                          `json:"content"`
	ContentSHA256 string                          `json:"content_sha256"`
	FileName      string                          `json:"file_name"`
	MIMEType      string                          `json:"mime_type"`
	ModelResults  []modelexport.ModelRenderResult `json:"model_results"`
	Warnings      []string                        `json:"warnings,omitempty"`
	SourceDigest  string                          `json:"source_digest"`
}

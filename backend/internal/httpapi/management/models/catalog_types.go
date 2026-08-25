package models

import (
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
)

// Catalog match sources persisted on a binding row.
const (
	catalogMatchSourceUnique = "unique_match"
	catalogMatchSourceManual = "manual"
)

// modelCatalogMetadataPayload is one metadata projection (source, override, or
// effective). Absent values stay null; explicit catalog zeros are preserved as
// authored. This payload never feeds runtime routing: it is management-only.
type modelCatalogMetadataPayload struct {
	Name             *string  `json:"name"`
	Description      *string  `json:"description"`
	Family           *string  `json:"family"`
	ReleaseDate      *string  `json:"release_date"`
	LastUpdated      *string  `json:"last_updated"`
	Knowledge        *string  `json:"knowledge"`
	Attachment       *bool    `json:"attachment"`
	Reasoning        *bool    `json:"reasoning"`
	ToolCall         *bool    `json:"tool_call"`
	StructuredOutput *bool    `json:"structured_output"`
	Temperature      *bool    `json:"temperature"`
	ModalitiesInput  []string `json:"modalities_input"`
	ModalitiesOutput []string `json:"modalities_output"`
	LimitContext     *int64   `json:"limit_context"`
	LimitInput       *int64   `json:"limit_input"`
	LimitOutput      *int64   `json:"limit_output"`
	OpenWeights      *bool    `json:"open_weights"`
	Status           *string  `json:"status"`
}

type modelCatalogAutoMatchPayload struct {
	Available  bool                  `json:"available"`
	Unique     bool                  `json:"unique"`
	Candidates []modelsdev.Candidate `json:"candidates"`
	Reason     string                `json:"reason,omitempty"`
}

type modelCatalogResponse struct {
	Bound           bool                          `json:"bound"`
	MatchSource     string                        `json:"match_source,omitempty"`
	ProviderID      string                        `json:"provider_id,omitempty"`
	CatalogModelID  string                        `json:"catalog_model_id,omitempty"`
	CatalogRevision string                        `json:"catalog_revision,omitempty"`
	FetchedAt       *time.Time                    `json:"fetched_at,omitempty"`
	UpdatedAt       *time.Time                    `json:"updated_at,omitempty"`
	Source          *modelCatalogMetadataPayload  `json:"source"`
	Override        *modelCatalogMetadataPayload  `json:"override"`
	Effective       *modelCatalogMetadataPayload  `json:"effective"`
	AutoMatch       *modelCatalogAutoMatchPayload `json:"auto_match,omitempty"`
}

// modelCatalogFieldChange is one source-value diff row of a refresh preview.
type modelCatalogFieldChange struct {
	Field   string  `json:"field"`
	Current *string `json:"current"`
	Next    *string `json:"next"`
	Kind    string  `json:"kind"` // added | removed | changed
}

type modelCatalogRefreshPreviewResponse struct {
	Bound           bool                      `json:"bound"`
	ProviderID      string                    `json:"provider_id,omitempty"`
	CatalogModelID  string                    `json:"catalog_model_id,omitempty"`
	Changed         bool                      `json:"changed"`
	Changes         []modelCatalogFieldChange `json:"changes"`
	CatalogRevision string                    `json:"catalog_revision"`
	FetchedAt       time.Time                 `json:"fetched_at"`
}

type modelCatalogRefreshCommitRequest struct {
	ExpectedCatalogRevision string `json:"expected_catalog_revision"`
}

type modelCatalogBindRequest struct {
	ProviderID              string `json:"provider_id"`
	CatalogModelID          string `json:"catalog_model_id"`
	ExpectedCatalogRevision string `json:"expected_catalog_revision"`
}

type modelCatalogCandidatesResponse struct {
	Items  []modelsdev.Candidate `json:"items"`
	Total  int                   `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
	Scope  string                `json:"scope"`
	Query  string                `json:"query,omitempty"`
}

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
	// BindingUpdatedAt is the local binding CAS token the preview was read
	// against. A commit must echo it back in expected_binding_updated_at, so
	// a rebind/override between preview and commit fails instead of letting
	// the commit clobber the newer local facts.
	BindingUpdatedAt time.Time `json:"binding_updated_at"`
}

type modelCatalogRefreshCommitRequest struct {
	ExpectedProviderID       string    `json:"expected_provider_id"`
	ExpectedCatalogModelID   string    `json:"expected_catalog_model_id"`
	ExpectedBindingUpdatedAt time.Time `json:"expected_binding_updated_at"`
	ExpectedCatalogRevision  string    `json:"expected_catalog_revision"`
}

type modelCatalogBindRequest struct {
	ProviderID     string `json:"provider_id"`
	CatalogModelID string `json:"catalog_model_id"`
	// ExpectedCatalogRevision is the models.dev ETag the operator confirmed.
	ExpectedCatalogRevision string `json:"expected_catalog_revision"`
	// ExpectedPrismModelID/ExpectedAPIFamily are the Prism identity the
	// operator confirmed the bind against. The write transaction re-verifies
	// both under the model row lock, so a rename or family edit between
	// preview and bind rejects with 409 instead of mislabelling metadata.
	ExpectedPrismModelID string `json:"expected_prism_model_id"`
	ExpectedAPIFamily    string `json:"expected_api_family"`
}

// modelCatalogUnbindRequest carries the binding snapshot the operator saw.
// Unbind deletes only when the persisted coordinate and updated_at still
// match; a concurrent rebind/refresh keeps the newer row and returns 409.
type modelCatalogUnbindRequest struct {
	ExpectedProviderID       string    `json:"expected_provider_id"`
	ExpectedCatalogModelID   string    `json:"expected_catalog_model_id"`
	ExpectedBindingUpdatedAt time.Time `json:"expected_binding_updated_at"`
}

type modelCatalogCandidatesResponse struct {
	Items  []modelsdev.Candidate `json:"items"`
	Total  int                   `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
	Scope  string                `json:"scope"`
	Query  string                `json:"query,omitempty"`
	// CatalogRevision/FetchedAt are the snapshot the page was computed from.
	// They publish models.dev revision evidence per page without claiming a
	// freshness state: this endpoint returns an already-validated snapshot,
	// and cold-fetch failure is a typed error, not a fake enum.
	CatalogRevision string     `json:"catalog_revision"`
	FetchedAt       *time.Time `json:"fetched_at,omitempty"`
}

type modelCatalogMatchPreviewResponse struct {
	Committable     bool                  `json:"committable"`
	ProviderID      string                `json:"provider_id,omitempty"`
	CatalogModelID  string                `json:"catalog_model_id,omitempty"`
	Candidates      []modelsdev.Candidate `json:"candidates"`
	Reason          string                `json:"reason"`
	CatalogRevision string                `json:"catalog_revision"`
	FetchedAt       time.Time             `json:"fetched_at"`
}

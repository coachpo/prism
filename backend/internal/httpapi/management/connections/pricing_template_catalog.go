package connections

import (
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
)

const catalogPricingImportSchemaVersion = 1

// catalogPricingTargetLimit bounds how many Terminal Targets one assignment
// may address in a single atomic commit.
const catalogPricingTargetLimit = 50

type catalogPricingPreviewRequest struct {
	ModelConfigID  *int   `json:"model_config_id"`
	ProviderID     string `json:"provider_id"`
	CatalogModelID string `json:"catalog_model_id"`
	ConnectionIDs  []int  `json:"connection_ids"`
}

// catalogPricingCommitRequest replays the preview payload (offering
// resolution + connection ids) together with the guards that make stale or
// conflicting commits impossible.
type catalogPricingCommitRequest struct {
	SchemaVersion           int    `json:"schema_version"`
	ModelConfigID           *int   `json:"model_config_id"`
	ProviderID              string `json:"provider_id"`
	CatalogModelID          string `json:"catalog_model_id"`
	ConnectionIDs           []int  `json:"connection_ids"`
	PreviewHash             string `json:"preview_hash"`
	ExpectedCatalogRevision string `json:"expected_catalog_revision"`
	ConfirmDrift            bool   `json:"confirm_drift"`
}

type catalogOfferingPayload struct {
	ProviderID     string  `json:"provider_id"`
	CatalogModelID string  `json:"catalog_model_id"`
	Name           string  `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	Family         *string `json:"family,omitempty"`
	Status         *string `json:"status,omitempty"`
	OpenWeights    *bool   `json:"open_weights,omitempty"`
}

type catalogPriceCardPayload struct {
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

type catalogPricePlanPayload struct {
	TemplateKind      string                             `json:"template_kind"`
	Cards             map[string]catalogPriceCardPayload `json:"cards"`
	TierThreshold     *int64                             `json:"tier_input_tokens_above,omitempty"`
	Incompatibilities []modelsdev.Incompatibility        `json:"incompatibilities"`
}

type catalogLinkedTemplatePayload struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Version      int    `json:"version"`
	RevisionID   int64  `json:"revision_id"`
	TemplateKind string `json:"template_kind"`
	UpdatedAt    string `json:"updated_at"`
}

type catalogTargetState struct {
	ConnectionID      int       `json:"connection_id"`
	Name              *string   `json:"name"`
	EndpointName      *string   `json:"endpoint_name"`
	PricingTemplateID *int      `json:"pricing_template_id"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type catalogPricingPreviewResponse struct {
	SchemaVersion         int                           `json:"schema_version"`
	Offering              catalogOfferingPayload        `json:"offering"`
	Model                 *catalogPrismModelPayload     `json:"model,omitempty"`
	CatalogRevision       string                        `json:"catalog_revision"`
	FetchedAt             time.Time                     `json:"fetched_at"`
	Plan                  catalogPricePlanPayload       `json:"plan"`
	Template              *catalogLinkedTemplatePayload `json:"template,omitempty"`
	Action                string                        `json:"action"` // create | reuse | drift
	Drift                 bool                          `json:"drift"`
	Committable           bool                          `json:"committable"`
	PreviewHash           string                        `json:"preview_hash,omitempty"`
	Targets               []catalogTargetState          `json:"targets"`
	ReportingCurrencyCode string                        `json:"reporting_currency_code"`
	// CatalogCurrency and PricingUnit state the fixed unit the five plan prices
	// are expressed in, so no reader has to infer it from the plan shape.
	CatalogCurrency string `json:"catalog_currency"`
	PricingUnit     string `json:"pricing_unit"`
}

type catalogPricingCommitResponse struct {
	Created        bool   `json:"created"`
	Updated        bool   `json:"updated"`
	Assigned       []int  `json:"assigned_connection_ids"`
	TemplateID     int    `json:"template_id"`
	TemplateName   string `json:"template_name"`
	RevisionID     int64  `json:"revision_id"`
	Version        int    `json:"version"`
	DriftConfirmed bool   `json:"drift_confirmed"`
}

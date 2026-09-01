package terminaltarget

import "time"

type Endpoint struct {
	ID                int
	ProfileID         int
	Name              string
	BaseURL           string
	APIKey            string
	APIKeyFingerprint *string
	APIKeyUpdatedAt   *time.Time
	ConfigRevision    int64
	Position          int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PricingTemplateSummary struct {
	ID                  int
	Name                string
	PricingUnit         string
	PricingCurrencyCode string
	// TemplateKind is the explicit standard/tiered/peak_valley wire identity.
	TemplateKind string
	Version      int
}

func (summary *PricingTemplateSummary) SetTemplateKind(kind string) {
	if summary != nil {
		summary.TemplateKind = kind
	}
}

type RuntimePricingTemplateSnapshot struct {
	ID                      int
	Name                    string
	RevisionID              int64
	PricingUnit             string
	PricingCurrencyCode     string
	ReportingCurrencyEpoch  *int
	TemplateKind            string
	Cards                   map[string]RuntimePricingCard
	TierInputTokensAbove    *int
	PricingScheduleTimezone *string
	PricingScheduleDigest   string
	PricingWindows          []Window
	Version                 int
	VersionEffectiveAt      *time.Time
}

// RuntimePricingCard is one immutable five-component price card in a
// published planning snapshot. A missing specialty component remains the
// empty string after SQL NULL scanning and is interpreted only by the pricing
// pipeline when matching it to observed usage.
type RuntimePricingCard struct {
	InputPrice         string
	OutputPrice        string
	CachedInputPrice   string
	CacheCreationPrice string
	ReasoningPrice     string
}

type Record struct {
	ID                 int
	ProfileID          int
	OwnerModelConfigID *int
	APIFamily          string
	EndpointID         int
	Endpoint           *Endpoint
	IsActive           bool
	Priority           int
	Name               *string
	AuthType           *string
	// UpstreamModelID is the explicit upstream model identity of this
	// Terminal Target. It is written on create (defaulting to the owner
	// model's current model_id), never cleared, and never cascades on model
	// rename. NULL means the target has no owner edge (orphan) or predates
	// the decoupling without a write since.
	UpstreamModelID         *string
	CustomHeaders           map[string]string
	CustomRequestParameters *CustomRequestParameters
	RoutingScheduleTimezone *string
	RoutingWindows          []Window
	OpenAITextCapability    *string
	OpenAIImageCapability   *string
	PricingTemplateID       *int
	QPSLimit                *int
	MaxInFlightNonStream    *int
	MaxInFlightStream       *int
	PricingTemplate         *PricingTemplateSummary
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type RuntimeEndpoint struct {
	ID              int
	Name            *string
	BaseURL         string
	EncryptedAPIKey string
}

type RuntimeRecord struct {
	ID                   int
	ProfileID            int
	APIFamily            string
	EndpointID           int
	Priority             int
	QPSLimit             *int
	MaxInFlightNonStream *int
	MaxInFlightStream    *int
	Name                 *string
	AuthType             *string
	// UpstreamModelID is the required frozen upstream identity read into the
	// planning snapshot. Runtime excludes orphan rows and rejects an owned
	// active target without a non-blank value.
	UpstreamModelID         *string
	CustomHeaders           map[string]any
	CustomRequestParameters *CustomRequestParameters
	PricingTemplateID       *int
	PricingTemplate         *RuntimePricingTemplateSnapshot
	OpenAITextCapability    *string
	OpenAIImageCapability   *string
	RoutingScheduleTimezone *string
	RoutingWindows          []Window
	Endpoint                RuntimeEndpoint
}

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
	ID                     int
	Name                   string
	RevisionID             int64
	PricingUnit            string
	PricingCurrencyCode    string
	ReportingCurrencyEpoch *int
	InputPrice             string
	OutputPrice            string
	CachedInputPrice       string
	CacheCreationPrice     string
	ReasoningPrice         string
	TierInputTokensAbove   *int
	TierInputPrice         string
	TierOutputPrice        string
	TierCachedInputPrice   string
	TierCacheCreationPrice string
	TierReasoningPrice     string
	Version                int
	VersionEffectiveAt     *time.Time
}

type Record struct {
	ID                      int
	ProfileID               int
	OwnerModelConfigID      *int
	APIFamily               string
	EndpointID              int
	Endpoint                *Endpoint
	IsActive                bool
	Priority                int
	Name                    *string
	AuthType                *string
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
	ID                      int
	ProfileID               int
	APIFamily               string
	EndpointID              int
	Priority                int
	QPSLimit                *int
	MaxInFlightNonStream    *int
	MaxInFlightStream       *int
	Name                    *string
	AuthType                *string
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

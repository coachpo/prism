package terminaltarget

import "time"

type Endpoint struct {
	ID        int
	ProfileID int
	Name      string
	BaseURL   string
	APIKey    string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PricingTemplateSummary struct {
	ID                  int
	Name                string
	PricingUnit         string
	PricingCurrencyCode string
	Version             int
}

type RuntimePricingTemplateSnapshot struct {
	ID                  int
	PricingUnit         string
	PricingCurrencyCode string
	InputPrice          string
	OutputPrice         string
	CachedInputPrice    string
	CacheCreationPrice  string
	ReasoningPrice      string
	Version             int
}

type Record struct {
	ID                                             int
	ProfileID                                      int
	OwnerModelConfigID                             *int
	APIFamily                                      string
	EndpointID                                     int
	Endpoint                                       *Endpoint
	ContextWindowTokens                            *int
	ContextWindowTokensOverridden                  bool
	DefaultOutputTokenReserve                      int
	DefaultOutputTokenReserveOverridden            bool
	MaxContextUtilization                          float64
	MaxContextUtilizationOverridden                bool
	PreferredContextUtilizationThreshold           *float64
	PreferredContextUtilizationThresholdOverridden bool
	IsActive                                       bool
	Priority                                       int
	Name                                           *string
	AuthType                                       *string
	CustomHeaders                                  map[string]string
	OpenAIProbeEndpointVariant                     *string
	OpenAITextCapability                           *string
	PricingTemplateID                              *int
	QPSLimit                                       *int
	MaxInFlightNonStream                           *int
	MaxInFlightStream                              *int
	PricingTemplate                                *PricingTemplateSummary
	HealthStatus                                   string
	HealthDetail                                   *string
	LastHealthCheck                                *time.Time
	CreatedAt                                      time.Time
	UpdatedAt                                      time.Time
}

type RuntimeEndpoint struct {
	ID              int
	Name            *string
	BaseURL         string
	EncryptedAPIKey string
}

type RuntimeRecord struct {
	ID                                   int
	ProfileID                            int
	APIFamily                            string
	EndpointID                           int
	Priority                             int
	QPSLimit                             *int
	MaxInFlightNonStream                 *int
	MaxInFlightStream                    *int
	Name                                 *string
	AuthType                             *string
	CustomHeaders                        map[string]any
	PricingTemplateID                    *int
	PricingTemplate                      *RuntimePricingTemplateSnapshot
	ContextWindowTokens                  *int
	DefaultOutputTokenReserve            int
	MaxContextUtilization                float64
	PreferredContextUtilizationThreshold *float64
	OpenAIProbeEndpointVariant           *string
	OpenAITextCapability                 *string
	Endpoint                             RuntimeEndpoint
}

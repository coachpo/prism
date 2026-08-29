package models

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

type exportCredentialWire struct {
	Include bool   `json:"include"`
	APIKey  string `json:"api_key,omitempty"`
}

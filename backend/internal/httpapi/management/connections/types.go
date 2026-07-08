package connections

import (
	"bytes"
	"encoding/json"
	"time"
)

type presenceMarker struct {
	Set bool
}

func (marker *presenceMarker) UnmarshalJSON(data []byte) error {
	marker.Set = true
	_ = data
	return nil
}

type optionalString struct {
	Set   bool
	Value *string
}

func (value *optionalString) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed string
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type optionalInt struct {
	Set   bool
	Value *int
}

func (value *optionalInt) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed int
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type optionalFloat struct {
	Set   bool
	Value *float64
}

func (value *optionalFloat) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed float64
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type optionalBool struct {
	Set   bool
	Value bool
}

func (value *optionalBool) UnmarshalJSON(data []byte) error {
	value.Set = true
	return json.Unmarshal(bytes.TrimSpace(data), &value.Value)
}

type optionalHeaders struct {
	Set   bool
	Value map[string]string
}

func (value *optionalHeaders) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed map[string]string
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = parsed
	return nil
}

type endpointCreateRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type optionalEndpointCreate struct {
	Set   bool
	Value *endpointCreateRequest
}

func (value *optionalEndpointCreate) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed endpointCreateRequest
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = &parsed
	return nil
}

type connectionCreateRequest struct {
	APIFamily            string                 `json:"api_family"`
	EndpointID           *int                   `json:"endpoint_id"`
	EndpointCreate       *endpointCreateRequest `json:"endpoint_create"`
	IsActive             *bool                  `json:"is_active"`
	Name                 *string                `json:"name"`
	AuthType             *string                `json:"auth_type"`
	CustomHeaders        map[string]string      `json:"custom_headers"`
	OpenAITextCapability *string                `json:"openai_text_capability"`
	PricingTemplateID    *int                   `json:"pricing_template_id"`
	QPSLimit             *int                   `json:"qps_limit"`
	MaxInFlightNonStream *int                   `json:"max_in_flight_non_stream"`
	MaxInFlightStream    *int                   `json:"max_in_flight_stream"`
	Priority             presenceMarker         `json:"priority"`
}

type connectionUpdateRequest struct {
	APIFamily            optionalString         `json:"api_family"`
	EndpointID           optionalInt            `json:"endpoint_id"`
	EndpointCreate       optionalEndpointCreate `json:"endpoint_create"`
	IsActive             optionalBool           `json:"is_active"`
	Name                 optionalString         `json:"name"`
	AuthType             optionalString         `json:"auth_type"`
	CustomHeaders        optionalHeaders        `json:"custom_headers"`
	OpenAITextCapability optionalString         `json:"openai_text_capability"`
	PricingTemplateID    optionalInt            `json:"pricing_template_id"`
	QPSLimit             optionalInt            `json:"qps_limit"`
	MaxInFlightNonStream optionalInt            `json:"max_in_flight_non_stream"`
	MaxInFlightStream    optionalInt            `json:"max_in_flight_stream"`
	Priority             presenceMarker         `json:"priority"`
}

type connectionPriorityMoveRequest struct {
	ToIndex int `json:"to_index"`
}

type connectionPricingTemplateUpdateRequest struct {
	PricingTemplateID optionalInt `json:"pricing_template_id"`
}

type modelConnectionsBatchRequest struct {
	ModelConfigIDs []int `json:"model_config_ids"`
}

type endpointResponse struct {
	ID           int       `json:"id"`
	ProfileID    int       `json:"profile_id"`
	Name         string    `json:"name"`
	BaseURL      string    `json:"base_url"`
	HasAPIKey    bool      `json:"has_api_key"`
	MaskedAPIKey *string   `json:"masked_api_key"`
	Position     int       `json:"position"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type connectionPricingTemplateSummary struct {
	ID                  int    `json:"id"`
	Name                string `json:"name"`
	PricingUnit         string `json:"pricing_unit"`
	PricingCurrencyCode string `json:"pricing_currency_code"`
	Version             int    `json:"version"`
}

type connectionResponse struct {
	ID                   int                               `json:"id"`
	ProfileID            int                               `json:"profile_id"`
	ModelConfigID        *int                              `json:"model_config_id,omitempty"`
	APIFamily            string                            `json:"api_family"`
	EndpointID           int                               `json:"endpoint_id"`
	Endpoint             *endpointResponse                 `json:"endpoint"`
	IsActive             bool                              `json:"is_active"`
	Priority             int                               `json:"priority"`
	Name                 *string                           `json:"name"`
	AuthType             *string                           `json:"auth_type"`
	CustomHeaders        map[string]string                 `json:"custom_headers"`
	OpenAITextCapability *string                           `json:"openai_text_capability"`
	PricingTemplateID    *int                              `json:"pricing_template_id"`
	QPSLimit             *int                              `json:"qps_limit"`
	MaxInFlightNonStream *int                              `json:"max_in_flight_non_stream"`
	MaxInFlightStream    *int                              `json:"max_in_flight_stream"`
	PricingTemplate      *connectionPricingTemplateSummary `json:"pricing_template"`
	HealthStatus         string                            `json:"health_status"`
	HealthDetail         *string                           `json:"health_detail"`
	LastHealthAt         *time.Time                        `json:"last_health_check"`
	CreatedAt            time.Time                         `json:"created_at"`
	UpdatedAt            time.Time                         `json:"updated_at"`
}

type connectionReferenceResponse struct {
	TargetID      int    `json:"target_id"`
	ModelConfigID int    `json:"model_config_id"`
	ModelID       string `json:"model_id"`
	APIFamily     string `json:"api_family"`
	Position      int    `json:"position"`
	IsEnabled     bool   `json:"is_enabled"`
}

type connectionReferencesResponse struct {
	ConnectionID int                           `json:"connection_id"`
	Items        []connectionReferenceResponse `json:"items"`
}

type pricingTemplateConnectionUsageItem struct {
	ConnectionID   int     `json:"connection_id"`
	ConnectionName *string `json:"connection_name"`
	ModelConfigID  int     `json:"model_config_id"`
	ModelID        string  `json:"model_id"`
	EndpointID     int     `json:"endpoint_id"`
	EndpointName   string  `json:"endpoint_name"`
}

type pricingTemplateConnectionsResponse struct {
	TemplateID int                                  `json:"template_id"`
	Items      []pricingTemplateConnectionUsageItem `json:"items"`
}

type modelConnectionsBatchItem struct {
	ModelConfigID int                  `json:"model_config_id"`
	Connections   []connectionResponse `json:"connections"`
}

type modelConnectionsBatchResponse struct {
	Items []modelConnectionsBatchItem `json:"items"`
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}

package models

import (
	"bytes"
	"encoding/json"
	"time"
)

type proxyTargetReference struct {
	TargetModelID  string `json:"target_model_id"`
	Position       int    `json:"position"`
	Weight         int    `json:"weight"`
	TargetPriority int    `json:"target_priority"`

	weightSet          bool
	weightNull         bool
	targetPrioritySet  bool
	targetPriorityNull bool
}

func (value *proxyTargetReference) UnmarshalJSON(data []byte) error {
	var parsed struct {
		TargetModelID  string      `json:"target_model_id"`
		Position       int         `json:"position"`
		Weight         optionalInt `json:"weight"`
		TargetPriority optionalInt `json:"target_priority"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &parsed); err != nil {
		return err
	}
	value.TargetModelID = parsed.TargetModelID
	value.Position = parsed.Position
	value.weightSet = parsed.Weight.Set
	value.weightNull = parsed.Weight.Set && parsed.Weight.Value == nil
	if parsed.Weight.Value != nil {
		value.Weight = *parsed.Weight.Value
	}
	value.targetPrioritySet = parsed.TargetPriority.Set
	value.targetPriorityNull = parsed.TargetPriority.Set && parsed.TargetPriority.Value == nil
	if parsed.TargetPriority.Value != nil {
		value.TargetPriority = *parsed.TargetPriority.Value
	}
	return nil
}

type modelCreateRequest struct {
	VendorID               *int                   `json:"vendor_id"`
	APIFamily              string                 `json:"api_family"`
	ModelID                string                 `json:"model_id"`
	DisplayName            *string                `json:"display_name"`
	ModelType              string                 `json:"model_type"`
	ProxySelectionStrategy *string                `json:"proxy_selection_strategy"`
	ProxyTargets           []proxyTargetReference `json:"proxy_targets"`
	LoadbalanceStrategyID  *int                   `json:"loadbalance_strategy_id"`
	IsEnabled              *bool                  `json:"is_enabled"`
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

type optionalBool struct {
	Set   bool
	Value bool
}

func (value *optionalBool) UnmarshalJSON(data []byte) error {
	value.Set = true
	return json.Unmarshal(bytes.TrimSpace(data), &value.Value)
}

type optionalProxyTargets struct {
	Set   bool
	Value []proxyTargetReference
}

func (value *optionalProxyTargets) UnmarshalJSON(data []byte) error {
	value.Set = true
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed []proxyTargetReference
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return err
	}
	value.Value = parsed
	return nil
}

type modelUpdateRequest struct {
	VendorID               optionalInt          `json:"vendor_id"`
	APIFamily              optionalString       `json:"api_family"`
	ModelID                optionalString       `json:"model_id"`
	DisplayName            optionalString       `json:"display_name"`
	ModelType              optionalString       `json:"model_type"`
	ProxySelectionStrategy optionalString       `json:"proxy_selection_strategy"`
	ProxyTargets           optionalProxyTargets `json:"proxy_targets"`
	LoadbalanceStrategyID  optionalInt          `json:"loadbalance_strategy_id"`
	IsEnabled              optionalBool         `json:"is_enabled"`
}

type vendorResponse struct {
	ID                 int       `json:"id"`
	Key                string    `json:"key"`
	Name               string    `json:"name"`
	Description        *string   `json:"description"`
	IconKey            *string   `json:"icon_key"`
	IsReadonly         bool      `json:"is_readonly"`
	AuditEnabled       bool      `json:"audit_enabled"`
	AuditCaptureBodies bool      `json:"audit_capture_bodies"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type loadbalanceStrategySummary struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	StrategyType       string `json:"strategy_type"`
	LegacyStrategyType any    `json:"legacy_strategy_type,omitempty"`
	AutoRecovery       any    `json:"auto_recovery,omitempty"`
	RoutingPolicy      any    `json:"routing_policy,omitempty"`
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
	ID                         int                               `json:"id"`
	ProfileID                  int                               `json:"profile_id"`
	ModelConfigID              int                               `json:"model_config_id"`
	EndpointID                 int                               `json:"endpoint_id"`
	Endpoint                   *endpointResponse                 `json:"endpoint"`
	IsActive                   bool                              `json:"is_active"`
	Priority                   int                               `json:"priority"`
	Name                       *string                           `json:"name"`
	AuthType                   *string                           `json:"auth_type"`
	CustomHeaders              map[string]string                 `json:"custom_headers"`
	OpenAIProbeEndpointVariant *string                           `json:"openai_probe_endpoint_variant"`
	PricingTemplateID          *int                              `json:"pricing_template_id"`
	QPSLimit                   *int                              `json:"qps_limit"`
	MaxInFlightNonStream       *int                              `json:"max_in_flight_non_stream"`
	MaxInFlightStream          *int                              `json:"max_in_flight_stream"`
	PricingTemplate            *connectionPricingTemplateSummary `json:"pricing_template"`
	HealthStatus               string                            `json:"health_status"`
	HealthDetail               *string                           `json:"health_detail"`
	LastHealthCheck            *time.Time                        `json:"last_health_check"`
	CreatedAt                  time.Time                         `json:"created_at"`
	UpdatedAt                  time.Time                         `json:"updated_at"`
}

type modelConfigListResponse struct {
	ID                     int                         `json:"id"`
	ProfileID              int                         `json:"profile_id"`
	VendorID               *int                        `json:"vendor_id"`
	Vendor                 *vendorResponse             `json:"vendor"`
	APIFamily              string                      `json:"api_family"`
	ModelID                string                      `json:"model_id"`
	DisplayName            *string                     `json:"display_name"`
	ModelType              string                      `json:"model_type"`
	ProxySelectionStrategy *string                     `json:"proxy_selection_strategy"`
	ProxyTargets           []proxyTargetReference      `json:"proxy_targets"`
	LoadbalanceStrategyID  *int                        `json:"loadbalance_strategy_id"`
	LoadbalanceStrategy    *loadbalanceStrategySummary `json:"loadbalance_strategy"`
	IsEnabled              bool                        `json:"is_enabled"`
	ConnectionCount        int                         `json:"connection_count"`
	ActiveConnectionCount  int                         `json:"active_connection_count"`
	HealthSuccessRate      *float64                    `json:"health_success_rate"`
	HealthTotalRequests    int                         `json:"health_total_requests"`
	CreatedAt              time.Time                   `json:"created_at"`
	UpdatedAt              time.Time                   `json:"updated_at"`
}

type modelConfigResponse struct {
	ID                     int                         `json:"id"`
	ProfileID              int                         `json:"profile_id"`
	VendorID               *int                        `json:"vendor_id"`
	Vendor                 *vendorResponse             `json:"vendor"`
	APIFamily              string                      `json:"api_family"`
	ModelID                string                      `json:"model_id"`
	DisplayName            *string                     `json:"display_name"`
	ModelType              string                      `json:"model_type"`
	ProxySelectionStrategy *string                     `json:"proxy_selection_strategy"`
	ProxyTargets           []proxyTargetReference      `json:"proxy_targets"`
	LoadbalanceStrategyID  *int                        `json:"loadbalance_strategy_id"`
	LoadbalanceStrategy    *loadbalanceStrategySummary `json:"loadbalance_strategy"`
	IsEnabled              bool                        `json:"is_enabled"`
	Connections            []connectionResponse        `json:"connections"`
	CreatedAt              time.Time                   `json:"created_at"`
	UpdatedAt              time.Time                   `json:"updated_at"`
}

type endpointModelsBatchItem struct {
	EndpointID int                       `json:"endpoint_id"`
	Models     []modelConfigListResponse `json:"models"`
}

type endpointModelsBatchResponse struct {
	Items []endpointModelsBatchItem `json:"items"`
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}

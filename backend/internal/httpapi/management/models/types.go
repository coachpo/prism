package models

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

type modelAccessTargetRequest struct {
	TargetType    string  `json:"target_type"`
	TargetModelID *string `json:"target_model_id"`
	ConnectionID  *int    `json:"connection_id"`
	Position      int     `json:"position"`
	IsEnabled     *bool   `json:"is_enabled"`
}

type modelAccessTargetCreateRequest struct {
	TargetType    string  `json:"target_type"`
	TargetModelID *string `json:"target_model_id"`
	ConnectionID  *int    `json:"connection_id"`
	Position      *int    `json:"position"`
	IsEnabled     *bool   `json:"is_enabled"`
}

type modelAccessTargetUpdateRequest struct {
	TargetType         optionalString `json:"target_type"`
	TargetModelID      optionalString `json:"target_model_id"`
	ConnectionID       optionalInt    `json:"connection_id"`
	TargetConnectionID optionalInt    `json:"target_connection_id"`
	Position           optionalInt    `json:"position"`
	IsEnabled          optionalBool   `json:"is_enabled"`
}

type modelAccessTargetMoveRequest struct {
	ToIndex int `json:"to_index"`
}

type modelCreateRequest struct {
	APIFamily             string                       `json:"api_family"`
	ModelID               string                       `json:"model_id"`
	DisplayName           *string                      `json:"display_name"`
	LoadbalanceStrategyID *int                         `json:"loadbalance_strategy_id"`
	OpenAIAcceptedFormat  optionalString               `json:"openai_accepted_format"`
	IsEnabled             *bool                        `json:"is_enabled"`
	InitialTerminalTarget *initialTerminalTargetCreate `json:"initial_terminal_target"`
}

// initialTerminalTargetCreate is the composite-create nested Terminal Target.
// It never receives api_family, owner/profile, priority/position or IDs.
type initialTerminalTargetCreate struct {
	EndpointID           *int                   `json:"endpoint_id"`
	EndpointCreate       *initialEndpointCreate `json:"endpoint_create"`
	Name                 *string                `json:"name"`
	IsActive             *bool                  `json:"is_active"`
	AuthType             *string                `json:"auth_type"`
	PricingTemplateID    *int                   `json:"pricing_template_id"`
	OpenAITextCapability *string                `json:"openai_text_capability"`
	QPSLimit             *int                   `json:"qps_limit"`
	MaxInFlightNonStream *int                   `json:"max_in_flight_non_stream"`
	MaxInFlightStream    *int                   `json:"max_in_flight_stream"`
	CustomHeaders        map[string]string      `json:"custom_headers"`
}

type initialEndpointCreate struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
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

type modelUpdateRequest struct {
	APIFamily             optionalString `json:"api_family"`
	ModelID               optionalString `json:"model_id"`
	DisplayName           optionalString `json:"display_name"`
	LoadbalanceStrategyID optionalInt    `json:"loadbalance_strategy_id"`
	OpenAIAcceptedFormat  optionalString `json:"openai_accepted_format"`
	IsEnabled             optionalBool   `json:"is_enabled"`
}

type loadbalanceStrategySummary struct {
	ID                                 int     `json:"id"`
	Name                               string  `json:"name"`
	LegacyStrategyType                 string  `json:"legacy_strategy_type"`
	FailureStatusCodes                 []int   `json:"failure_status_codes"`
	BanMode                            string  `json:"ban_mode"`
	RetryBaseDelayMS                   int     `json:"retry_base_delay_ms"`
	RetryBackoffMultiplier             float64 `json:"retry_backoff_multiplier"`
	RetryJitterRatio                   float64 `json:"retry_jitter_ratio"`
	RetryMaxDelayMS                    int     `json:"retry_max_delay_ms"`
	CycleRetryAttemptLimit             int     `json:"cycle_retry_attempt_limit"`
	BanCumulativeRetryAttemptThreshold int     `json:"ban_cumulative_retry_attempt_threshold"`
	BanDurationSeconds                 int     `json:"ban_duration_seconds"`
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

type connectionTargetSummary struct {
	ID                   int                               `json:"id"`
	ModelConfigID        int                               `json:"-"`
	ProfileID            int                               `json:"profile_id"`
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
	CreatedAt            time.Time                         `json:"created_at"`
	UpdatedAt            time.Time                         `json:"updated_at"`
}

type terminalTargetSummary = connectionTargetSummary

type modelTargetSummary struct {
	ID                    int     `json:"id"`
	ProfileID             int     `json:"profile_id"`
	APIFamily             string  `json:"api_family"`
	ModelID               string  `json:"model_id"`
	DisplayName           *string `json:"display_name"`
	LoadbalanceStrategyID *int    `json:"loadbalance_strategy_id"`
	OpenAIAcceptedFormat  *string `json:"openai_accepted_format"`
	IsEnabled             bool    `json:"is_enabled"`
}

type modelAccessTargetResponse struct {
	ID               int                      `json:"id"`
	TargetType       string                   `json:"target_type"`
	TargetModelID    *string                  `json:"target_model_id"`
	ConnectionID     *int                     `json:"connection_id"`
	TerminalTargetID *int                     `json:"terminal_target_id"`
	Position         int                      `json:"position"`
	IsEnabled        bool                     `json:"is_enabled"`
	TargetModel      *modelTargetSummary      `json:"target_model"`
	Connection       *connectionTargetSummary `json:"connection"`
	TerminalTarget   *terminalTargetSummary   `json:"terminal_target"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

type modelConfigListResponse struct {
	ID                    int                          `json:"id"`
	ProfileID             int                          `json:"profile_id"`
	APIFamily             string                       `json:"api_family"`
	ModelID               string                       `json:"model_id"`
	DisplayName           *string                      `json:"display_name"`
	LoadbalanceStrategyID *int                         `json:"loadbalance_strategy_id"`
	LoadbalanceStrategy   *loadbalanceStrategySummary  `json:"loadbalance_strategy"`
	OpenAIAcceptedFormat  *string                      `json:"openai_accepted_format"`
	AccessTargets         []modelAccessTargetResponse  `json:"access_targets"`
	IsEnabled             bool                         `json:"is_enabled"`
	ConnectionCount       int                          `json:"connection_count"`
	ActiveConnectionCount int                          `json:"active_connection_count"`
	HealthSuccessRate     *float64                     `json:"health_success_rate"`
	HealthTotalRequests   int                          `json:"health_total_requests"`
	RoutingSummary        *modelrouting.RoutingSummary `json:"routing_summary"`
	CreatedAt             time.Time                    `json:"created_at"`
	UpdatedAt             time.Time                    `json:"updated_at"`
}

// accessTargetMutationEnvelope is the fixed response envelope for Access
// Target mutations (create/update/move/delete/enable/order).
type accessTargetMutationEnvelope struct {
	AccessTargets         []modelAccessTargetResponse         `json:"access_targets"`
	ConfigurationWarnings []modelrouting.ConfigurationWarning `json:"configuration_warnings"`
}

// modelCreateResponse is the fixed composite-create envelope.
type modelCreateResponse struct {
	Model                 modelConfigResponse                 `json:"model"`
	ConfigurationWarnings []modelrouting.ConfigurationWarning `json:"configuration_warnings"`
}

type modelMutationEnvelope struct {
	Model                 modelConfigResponse                 `json:"model"`
	ConfigurationWarnings []modelrouting.ConfigurationWarning `json:"configuration_warnings"`
}

type modelConfigResponse struct {
	ID                    int                         `json:"id"`
	ProfileID             int                         `json:"profile_id"`
	APIFamily             string                      `json:"api_family"`
	ModelID               string                      `json:"model_id"`
	DisplayName           *string                     `json:"display_name"`
	LoadbalanceStrategyID *int                        `json:"loadbalance_strategy_id"`
	LoadbalanceStrategy   *loadbalanceStrategySummary `json:"loadbalance_strategy"`
	OpenAIAcceptedFormat  *string                     `json:"openai_accepted_format"`
	AccessTargets         []modelAccessTargetResponse `json:"access_targets"`
	IsEnabled             bool                        `json:"is_enabled"`
	CreatedAt             time.Time                   `json:"created_at"`
	UpdatedAt             time.Time                   `json:"updated_at"`
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

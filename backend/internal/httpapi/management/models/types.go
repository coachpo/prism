package models

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
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
	APIFamily             string                             `json:"api_family"`
	ModelID               string                             `json:"model_id"`
	DisplayName           *string                            `json:"display_name"`
	LoadbalanceStrategyID *int                               `json:"loadbalance_strategy_id"`
	OpenAIAcceptedFormat  optionalString                     `json:"openai_accepted_format"`
	OpenAIImageOperations optionalString                     `json:"openai_image_operations"`
	IsEnabled             *bool                              `json:"is_enabled"`
	InitialTerminalTarget *modelInitialTerminalTargetRequest `json:"initial_terminal_target"`
}

// modelInitialTerminalTargetRequest carries the optional first Terminal Target
// for the composite model create. endpoint_id and endpoint_create are XOR.
type modelInitialTerminalTargetRequest struct {
	EndpointID              *int                        `json:"endpoint_id"`
	EndpointCreate          *modelEndpointCreateRequest `json:"endpoint_create"`
	Name                    *string                     `json:"name"`
	IsActive                *bool                       `json:"is_active"`
	AuthType                *string                     `json:"auth_type"`
	PricingTemplateID       *int                        `json:"pricing_template_id"`
	OpenAITextCapability    *string                     `json:"openai_text_capability"`
	OpenAIImageCapability   *string                     `json:"openai_image_capability"`
	QPSLimit                *int                        `json:"qps_limit"`
	MaxInFlightNonStream    *int                        `json:"max_in_flight_non_stream"`
	MaxInFlightStream       *int                        `json:"max_in_flight_stream"`
	CustomHeaders           map[string]string           `json:"custom_headers"`
	CustomRequestParameters optionalRawMessage          `json:"custom_request_parameters"`
	RoutingSchedule         optionalRawMessage          `json:"routing_schedule"`
}

type modelEndpointCreateRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type optionalRawMessage struct {
	Set bool
	Raw json.RawMessage
}

func (value *optionalRawMessage) UnmarshalJSON(data []byte) error {
	value.Set = true
	value.Raw = append(json.RawMessage(nil), data...)
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

type modelUpdateRequest struct {
	APIFamily             optionalString `json:"api_family"`
	ModelID               optionalString `json:"model_id"`
	DisplayName           optionalString `json:"display_name"`
	LoadbalanceStrategyID optionalInt    `json:"loadbalance_strategy_id"`
	OpenAIAcceptedFormat  optionalString `json:"openai_accepted_format"`
	OpenAIImageOperations optionalString `json:"openai_image_operations"`
	IsEnabled             optionalBool   `json:"is_enabled"`
}

type loadbalanceStrategySummary struct {
	ID                                 int     `json:"id"`
	Name                               string  `json:"name"`
	LegacyStrategyType                 string  `json:"legacy_strategy_type"`
	IsDefault                          bool    `json:"is_default"`
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
	ID                int        `json:"id"`
	ProfileID         int        `json:"profile_id"`
	Name              string     `json:"name"`
	BaseURL           string     `json:"base_url"`
	HasAPIKey         bool       `json:"has_api_key"`
	APIKeyFingerprint *string    `json:"api_key_fingerprint"`
	APIKeyUpdatedAt   *time.Time `json:"api_key_updated_at"`
	ConfigRevision    int64      `json:"config_revision"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type connectionPricingTemplateSummary struct {
	ID                  int    `json:"id"`
	Name                string `json:"name"`
	PricingUnit         string `json:"pricing_unit"`
	PricingCurrencyCode string `json:"pricing_currency_code"`
	Version             int    `json:"version"`
}

type connectionTargetSummary struct {
	ID                      int                                     `json:"id"`
	ModelConfigID           int                                     `json:"-"`
	ProfileID               int                                     `json:"profile_id"`
	APIFamily               string                                  `json:"api_family"`
	EndpointID              int                                     `json:"endpoint_id"`
	Endpoint                *endpointResponse                       `json:"endpoint"`
	IsActive                bool                                    `json:"is_active"`
	Priority                int                                     `json:"priority"`
	Name                    *string                                 `json:"name"`
	AuthType                *string                                 `json:"auth_type"`
	CustomHeaders           map[string]string                       `json:"custom_headers"`
	CustomHeadersRedacted   []string                                `json:"custom_headers_redacted"`
	CustomRequestParameters *terminaltarget.CustomRequestParameters `json:"custom_request_parameters"`
	// RoutingSchedule and RoutingScheduleState reuse the connections package
	// types and its single state projection. The model detail page reads this
	// summary and the connection list through the same client type, so a shape
	// that differed here would degrade one surface while the other looked fine.
	RoutingSchedule       *connections.RoutingSchedulePayload      `json:"routing_schedule"`
	RoutingScheduleState  *connections.RoutingScheduleStatePayload `json:"routing_schedule_state"`
	OpenAITextCapability  *string                                  `json:"openai_text_capability"`
	OpenAIImageCapability *string                                  `json:"openai_image_capability"`
	// routingScheduleTimezone carries the parent-row column between the scanner
	// and the window batch read; the wire shape is assembled from it.
	routingScheduleTimezone *string
	PricingTemplateID       *int                              `json:"pricing_template_id"`
	QPSLimit                *int                              `json:"qps_limit"`
	MaxInFlightNonStream    *int                              `json:"max_in_flight_non_stream"`
	MaxInFlightStream       *int                              `json:"max_in_flight_stream"`
	PricingTemplate         *connectionPricingTemplateSummary `json:"pricing_template"`
	CreatedAt               time.Time                         `json:"created_at"`
	UpdatedAt               time.Time                         `json:"updated_at"`
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
	OpenAIImageOperations *string `json:"openai_image_operations"`
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
	ID                    int                                      `json:"id"`
	ProfileID             int                                      `json:"profile_id"`
	APIFamily             string                                   `json:"api_family"`
	ModelID               string                                   `json:"model_id"`
	DisplayName           *string                                  `json:"display_name"`
	LoadbalanceStrategyID *int                                     `json:"loadbalance_strategy_id"`
	LoadbalanceStrategy   *loadbalanceStrategySummary              `json:"loadbalance_strategy"`
	OpenAIAcceptedFormat  *string                                  `json:"openai_accepted_format"`
	OpenAIImageOperations *string                                  `json:"openai_image_operations"`
	AccessTargets         []modelAccessTargetResponse              `json:"access_targets"`
	IsEnabled             bool                                     `json:"is_enabled"`
	ConnectionCount       int                                      `json:"connection_count"`
	ActiveConnectionCount int                                      `json:"active_connection_count"`
	HealthSuccessRate     *float64                                 `json:"health_success_rate"`
	HealthTotalRequests   int                                      `json:"health_total_requests"`
	RoutingSummary        *modelrouting.RoutingSummary             `json:"routing_summary"`
	RouteReadiness        *modelrouting.ModelRouteReadinessSummary `json:"route_readiness,omitempty"`
	CreatedAt             time.Time                                `json:"created_at"`
	UpdatedAt             time.Time                                `json:"updated_at"`
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

type modelConfigResponse struct {
	ID                    int                         `json:"id"`
	ProfileID             int                         `json:"profile_id"`
	APIFamily             string                      `json:"api_family"`
	ModelID               string                      `json:"model_id"`
	DisplayName           *string                     `json:"display_name"`
	LoadbalanceStrategyID *int                        `json:"loadbalance_strategy_id"`
	LoadbalanceStrategy   *loadbalanceStrategySummary `json:"loadbalance_strategy"`
	OpenAIAcceptedFormat  *string                     `json:"openai_accepted_format"`
	OpenAIImageOperations *string                     `json:"openai_image_operations"`
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

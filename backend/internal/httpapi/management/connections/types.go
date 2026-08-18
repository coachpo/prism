package connections

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
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

// optionalCustomRequestParameters preserves the raw JSON value so the shared
// validator can return a field-specific 422 for non-object roots.
type optionalCustomRequestParameters struct {
	Set bool
	Raw json.RawMessage
}

func (value *optionalCustomRequestParameters) UnmarshalJSON(data []byte) error {
	value.Set = true
	value.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// RoutingWindowPayload is one wire routing window. EndMinute above
// 1440 continues into the following day, and WeekdayMask names the days the
// window opens on (not every day it covers).
type RoutingWindowPayload struct {
	WeekdayMask int `json:"weekday_mask"`
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

// RoutingSchedulePayload is the stored configuration of a Terminal
// Target's routing schedule. It carries no evaluated conclusion; the current
// state lives in RoutingScheduleStatePayload so that configuration reads stay
// clock-independent.
type RoutingSchedulePayload struct {
	Timezone string                 `json:"timezone"`
	Windows  []RoutingWindowPayload `json:"windows"`
}

// RoutingScheduleInput preserves the raw JSON of the routing_schedule field
// instead of decoding it here, so that array and scalar roots reach the shared
// field validator as a 422 rather than degrading into a generic 400, and so
// JSON null reaches the clear semantics. It is exported because other
// management packages forward the field into the shared owner-scoped writer.
type RoutingScheduleInput struct {
	Set bool
	Raw json.RawMessage
}

func (value *RoutingScheduleInput) UnmarshalJSON(data []byte) error {
	value.Set = true
	value.Raw = append(json.RawMessage(nil), data...)
	return nil
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

// optionalCustomRequestParameters captures the raw JSON of the
// custom_request_parameters field without decoding it, so that array/scalar
// roots reach the shared field validator (422) instead of degrading into a
// generic 400, and JSON null reaches the clear semantics. Validation and
// canonicalization happen in the handler before any SQL write.
// CustomRequestParametersInput is the exported envelope for the optional
// custom request parameters field so other management packages can forward it
// into the shared owner-scoped connection writer without duplicating the
// parser.
type CustomRequestParametersInput struct {
	Set bool
	Raw json.RawMessage
}

// EndpointCreateRequest is the exported inline-endpoint shape used by the
// shared owner-scoped connection writer so other management packages can pass
// inline endpoint data without duplicating the parser.
type EndpointCreateRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
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
	APIFamily               string                          `json:"api_family"`
	EndpointID              *int                            `json:"endpoint_id"`
	EndpointCreate          *endpointCreateRequest          `json:"endpoint_create"`
	IsActive                *bool                           `json:"is_active"`
	Name                    *string                         `json:"name"`
	AuthType                *string                         `json:"auth_type"`
	CustomHeaders           map[string]string               `json:"custom_headers"`
	CustomRequestParameters optionalCustomRequestParameters `json:"custom_request_parameters"`
	RoutingSchedule         RoutingScheduleInput            `json:"routing_schedule"`
	OpenAITextCapability    *string                         `json:"openai_text_capability"`
	OpenAIImageCapability   *string                         `json:"openai_image_capability"`
	PricingTemplateID       *int                            `json:"pricing_template_id"`
	QPSLimit                *int                            `json:"qps_limit"`
	MaxInFlightNonStream    *int                            `json:"max_in_flight_non_stream"`
	MaxInFlightStream       *int                            `json:"max_in_flight_stream"`
	Priority                presenceMarker                  `json:"priority"`
}

type connectionUpdateRequest struct {
	APIFamily               optionalString                  `json:"api_family"`
	EndpointID              optionalInt                     `json:"endpoint_id"`
	EndpointCreate          optionalEndpointCreate          `json:"endpoint_create"`
	IsActive                optionalBool                    `json:"is_active"`
	Name                    optionalString                  `json:"name"`
	AuthType                optionalString                  `json:"auth_type"`
	CustomHeaders           optionalHeaders                 `json:"custom_headers"`
	CustomRequestParameters optionalCustomRequestParameters `json:"custom_request_parameters"`
	RoutingSchedule         RoutingScheduleInput            `json:"routing_schedule"`
	OpenAITextCapability    optionalString                  `json:"openai_text_capability"`
	OpenAIImageCapability   optionalString                  `json:"openai_image_capability"`
	PricingTemplateID       optionalInt                     `json:"pricing_template_id"`
	// ExpectedConnectionUpdatedAt and ExpectedPricingTemplateID are required
	// CAS fields whenever pricing_template_id is present in the request. They
	// guard concurrent overwrites of the same Terminal Target's pricing
	// reference.
	ExpectedConnectionUpdatedAt optionalString `json:"expected_connection_updated_at"`
	ExpectedPricingTemplateID   optionalInt    `json:"expected_pricing_template_id"`
	QPSLimit                    optionalInt    `json:"qps_limit"`
	MaxInFlightNonStream        optionalInt    `json:"max_in_flight_non_stream"`
	MaxInFlightStream           optionalInt    `json:"max_in_flight_stream"`
	Priority                    presenceMarker `json:"priority"`
}

type connectionPriorityMoveRequest struct {
	ToIndex int `json:"to_index"`
}

type modelConnectionsBatchRequest struct {
	ModelConfigIDs []int `json:"model_config_ids"`
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

type connectionResponse struct {
	ID                      int                                     `json:"id"`
	ProfileID               int                                     `json:"profile_id"`
	ModelConfigID           *int                                    `json:"model_config_id,omitempty"`
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
	RoutingSchedule         *RoutingSchedulePayload                 `json:"routing_schedule"`
	RoutingScheduleState    *RoutingScheduleStatePayload            `json:"routing_schedule_state"`
	OpenAITextCapability    *string                                 `json:"openai_text_capability"`
	OpenAIImageCapability   *string                                 `json:"openai_image_capability"`
	PricingTemplateID       *int                                    `json:"pricing_template_id"`
	QPSLimit                *int                                    `json:"qps_limit"`
	MaxInFlightNonStream    *int                                    `json:"max_in_flight_non_stream"`
	MaxInFlightStream       *int                                    `json:"max_in_flight_stream"`
	PricingTemplate         *connectionPricingTemplateSummary       `json:"pricing_template"`
	CreatedAt               time.Time                               `json:"created_at"`
	UpdatedAt               time.Time                               `json:"updated_at"`
}

// maskedForWire returns a copy whose sensitive custom header values are
// replaced with the redaction sentinel and whose CustomHeadersRedacted lists
// the masked names, so management read APIs never leak stored header values.
func (response connectionResponse) maskedForWire() connectionResponse {
	masked := response
	masked.CustomHeaders, masked.CustomHeadersRedacted = redactCustomHeaders(response.CustomHeaders)
	return masked
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

// connectionMutationAccessTarget is the reduced canonical access-target row
// included in owner-scoped connection mutation envelopes. It carries the
// canonical row id, target discriminator and connection id so callers can
// navigate and join diagnostics; the full detail shape stays on the models
// management surface.
type connectionMutationAccessTarget struct {
	ID               int    `json:"id"`
	TargetType       string `json:"target_type"`
	ConnectionID     *int   `json:"connection_id"`
	TerminalTargetID *int   `json:"terminal_target_id"`
	Position         int    `json:"position"`
	IsEnabled        bool   `json:"is_enabled"`
}

// connectionMutationEnvelope is the fixed response envelope for owner-scoped
// Connection create/update. Delete uses deletedConnectionMutationEnvelope.
type connectionMutationEnvelope struct {
	Connection            connectionResponse                  `json:"connection"`
	AccessTargets         []connectionMutationAccessTarget    `json:"access_targets"`
	ConfigurationWarnings []modelrouting.ConfigurationWarning `json:"configuration_warnings"`
}

type deletedConnectionMutationEnvelope struct {
	Deleted               bool                                `json:"deleted"`
	AccessTargets         []connectionMutationAccessTarget    `json:"access_targets"`
	ConfigurationWarnings []modelrouting.ConfigurationWarning `json:"configuration_warnings"`
}

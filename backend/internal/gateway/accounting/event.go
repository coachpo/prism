package accounting

import (
	"fmt"
	"strings"
	"time"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

type EventPhase string

const (
	EventPhaseAttempt  EventPhase = "attempt"
	EventPhaseFinal    EventPhase = "final"
	EventPhaseFeedback EventPhase = "feedback"
)

type Event struct {
	Phase                    EventPhase              `json:"phase"`
	RequestID                string                  `json:"request_id,omitempty"`
	ProfileID                int                     `json:"profile_id,omitempty"`
	OperationName            string                  `json:"operation_name"`
	APIFamily                string                  `json:"api_family,omitempty"`
	RequestedModelID         string                  `json:"requested_model_id,omitempty"`
	EffectiveModelID         *string                 `json:"effective_model_id,omitempty"`
	EndpointID               *int                    `json:"endpoint_id,omitempty"`
	ConnectionID             *int                    `json:"connection_id,omitempty"`
	SelectedTerminalTargetID *int                    `json:"selected_terminal_target_id,omitempty"`
	AttemptNumber            int                     `json:"attempt_number,omitempty"`
	Final                    bool                    `json:"final"`
	StatusCode               int                     `json:"status_code,omitempty"`
	Success                  bool                    `json:"success"`
	RouteReason              gatewaycore.RouteReason `json:"route_reason"`
	UsageSource              gatewaycore.UsageSource `json:"usage_source"`
	PricingConfigVersionUsed *int                    `json:"pricing_config_version_used,omitempty"`
	StreamOutcome            string                  `json:"stream_outcome,omitempty"`
	AuditEnabled             bool                    `json:"audit_enabled"`
	AuditCaptureBodies       bool                    `json:"audit_capture_bodies"`
	ObservedAt               time.Time               `json:"observed_at"`
}

func NewEvent(event Event) (Event, error) {
	normalized := event.Normalize()
	if err := normalized.Validate(); err != nil {
		return Event{}, err
	}
	return normalized, nil
}

func (event Event) Normalize() Event {
	event.OperationName = strings.TrimSpace(event.OperationName)
	event.APIFamily = strings.TrimSpace(event.APIFamily)
	event.RequestID = strings.TrimSpace(event.RequestID)
	event.RequestedModelID = strings.TrimSpace(event.RequestedModelID)
	if event.EffectiveModelID != nil {
		trimmed := strings.TrimSpace(*event.EffectiveModelID)
		event.EffectiveModelID = nil
		if trimmed != "" {
			event.EffectiveModelID = &trimmed
		}
	}
	event.RouteReason = NormalizeRouteReason(event.RouteReason)
	event.UsageSource = NormalizeUsageSource(event.UsageSource)
	event.StreamOutcome = strings.TrimSpace(event.StreamOutcome)
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	} else {
		event.ObservedAt = event.ObservedAt.UTC()
	}
	return event
}

func (event Event) Validate() error {
	switch event.Phase {
	case EventPhaseAttempt, EventPhaseFinal, EventPhaseFeedback:
	default:
		return fmt.Errorf("accounting event phase %q is unsupported", event.Phase)
	}
	if strings.TrimSpace(event.OperationName) == "" {
		return fmt.Errorf("accounting event operation name is required")
	}
	if event.ProfileID < 0 {
		return fmt.Errorf("accounting event profile id must be non-negative")
	}
	if event.AttemptNumber < 0 {
		return fmt.Errorf("accounting event attempt number must be non-negative")
	}
	if event.StatusCode < 0 {
		return fmt.Errorf("accounting event status code must be non-negative")
	}
	if event.Phase == EventPhaseAttempt && event.AttemptNumber == 0 {
		return fmt.Errorf("accounting attempt event requires attempt number")
	}
	if event.Phase == EventPhaseFinal && !event.Final {
		return fmt.Errorf("accounting final event must be marked final")
	}
	return nil
}

func NormalizeRouteReason(reason gatewaycore.RouteReason) gatewaycore.RouteReason {
	switch reason {
	case gatewaycore.RouteReasonDirectMatch,
		gatewaycore.RouteReasonModelRedirect,
		gatewaycore.RouteReasonUpstreamRedirect,
		gatewaycore.RouteReasonQPSOverflow,
		gatewaycore.RouteReasonRPMOverflow,
		gatewaycore.RouteReasonTPMOverflow,
		gatewaycore.RouteReasonIPMOverflow,
		gatewaycore.RouteReasonConcurrencyOverflow,
		gatewaycore.RouteReasonRetry429,
		gatewaycore.RouteReasonRetry5xx,
		gatewaycore.RouteReasonRetryConnectTimeout,
		gatewaycore.RouteReasonCircuitOpenSkip,
		gatewaycore.RouteReasonNoHealthyUpstream,
		gatewaycore.RouteReasonPolicyReject:
		return reason
	default:
		return gatewaycore.RouteReasonDirectMatch
	}
}

func NormalizeUsageSource(source gatewaycore.UsageSource) gatewaycore.UsageSource {
	switch source {
	case gatewaycore.UsageSourceProvider,
		gatewaycore.UsageSourceProviderStreamTerminal,
		gatewaycore.UsageSourceLocalEstimate,
		gatewaycore.UsageSourceMissing:
		return source
	default:
		return gatewaycore.UsageSourceMissing
	}
}

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
	// Pricing evidence is orthogonal: kind identifies the template family,
	// state identifies selector evaluation, and role identifies the card used.
	PricingTemplateKind            *string    `json:"pricing_template_kind,omitempty"`
	PricingSelectionState          *string    `json:"pricing_selection_state,omitempty"`
	PricingCardRole                *string    `json:"pricing_card_role,omitempty"`
	PricingSelectorThresholdTokens *int       `json:"pricing_selector_threshold_tokens,omitempty"`
	PricingSelectorBasisTokens     *int64     `json:"pricing_selector_basis_tokens,omitempty"`
	PricingScheduleDecidedAt       *time.Time `json:"pricing_schedule_decided_at,omitempty"`
	PricingScheduleTimezone        *string    `json:"pricing_schedule_timezone,omitempty"`
	PricingScheduleLocalWeekday    *int       `json:"pricing_schedule_local_weekday,omitempty"`
	PricingScheduleLocalMinute     *int       `json:"pricing_schedule_local_minute,omitempty"`
	PricingScheduleDigest          *string    `json:"pricing_schedule_digest,omitempty"`
	StreamOutcome                  string     `json:"stream_outcome,omitempty"`
	AuditEnabled                   bool       `json:"audit_enabled"`
	AuditCaptureBodies             bool       `json:"audit_capture_bodies"`
	ObservedAt                     time.Time  `json:"observed_at"`
}

// SetPricingEvidence copies the orthogonal selector evidence into an event.
func (event *Event) SetPricingEvidence(kind, state, role *string, threshold *int, basis *int64, decidedAt *time.Time, timezone *string, weekday, minute *int, digest *string) {
	if event == nil {
		return
	}
	event.PricingTemplateKind = kind
	event.PricingSelectionState = state
	event.PricingCardRole = role
	event.PricingSelectorThresholdTokens = threshold
	event.PricingSelectorBasisTokens = basis
	event.PricingScheduleDecidedAt = decidedAt
	event.PricingScheduleTimezone = timezone
	event.PricingScheduleLocalWeekday = weekday
	event.PricingScheduleLocalMinute = minute
	event.PricingScheduleDigest = digest
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
	if event.PricingScheduleDecidedAt != nil {
		value := event.PricingScheduleDecidedAt.UTC()
		event.PricingScheduleDecidedAt = &value
	}
	for _, value := range []*string{event.PricingTemplateKind, event.PricingSelectionState, event.PricingCardRole, event.PricingScheduleTimezone, event.PricingScheduleDigest} {
		if value != nil {
			*value = strings.TrimSpace(*value)
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
		gatewaycore.RouteReasonRetryHTTP,
		gatewaycore.RouteReasonRetryConnectTimeout,
		gatewaycore.RouteReasonRetryTransport,
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
		gatewaycore.UsageSourceMissing,
		gatewaycore.UsageSourceNormalizationRejected:
		return source
	default:
		return gatewaycore.UsageSourceMissing
	}
}

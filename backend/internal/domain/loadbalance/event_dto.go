package loadbalance

import (
	"strings"
	"time"
)

// Event summary V1: a closed discriminated union of stable code + typed params.
// Backend MUST NOT return localized prose; the frontend renders the actual
// zh-CN catalog from code + params. Params structs are closed (no extra keys).
const (
	EventSummaryCodeRetryScheduled    = "loadbalance.retry_scheduled"
	EventSummaryCodeRetryExhausted    = "loadbalance.retry_exhausted"
	EventSummaryCodeBanned            = "loadbalance.banned"
	EventSummaryCodeUnbanned          = "loadbalance.unbanned"
	EventSummaryCodeRecovered         = "loadbalance.recovered"
	EventSummaryCodeAdmissionRejected = "loadbalance.admission_rejected"

	EvidenceStateComplete         = "complete"
	EvidenceStateLegacyIncomplete = "legacy_incomplete"

	AttributionIdentified   = "identified"
	AttributionUnattributed = "unattributed"
)

// EventSummaryV1 is the wire shape of the closed summary union.
type EventSummaryV1 struct {
	Version int                  `json:"version"`
	Code    string               `json:"code"`
	Params  EventSummaryV1Params `json:"params"`
}

// EventSummaryV1Params carries the per-variant typed params; each variant only
// populates its own keys and the Go struct is closed (additionalProperties
// would be false in JSON Schema terms).
type EventSummaryV1Params struct {
	EvidenceState                            string     `json:"evidence_state"`
	FailureKind                              *string    `json:"failure_kind,omitempty"`
	AdmissionReason                          *string    `json:"admission_reason,omitempty"`
	CycleRetryAttempts                       *int       `json:"cycle_retry_attempts,omitempty"`
	CumulativeRetryAttempts                  *int       `json:"cumulative_retry_attempts,omitempty"`
	LastRetryDelayMS                         *int       `json:"last_retry_delay_ms,omitempty"`
	NextRetryAt                              *time.Time `json:"next_retry_at,omitempty"`
	PolicyCycleRetryAttemptLimit             *int       `json:"policy_cycle_retry_attempt_limit,omitempty"`
	BanMode                                  *string    `json:"ban_mode,omitempty"`
	PolicyBanCumulativeRetryAttemptThreshold *int       `json:"policy_ban_cumulative_retry_attempt_threshold,omitempty"`
	BannedUntilAt                            *time.Time `json:"banned_until_at,omitempty"`
	LastSuccessAt                            *time.Time `json:"last_success_at,omitempty"`
}

func intParam(value int) *int {
	resolved := value
	return &resolved
}

// buildEventSummaryV1 maps an event row to its closed V1 summary. Legacy rows
// missing evidence that the current writer always persists report
// evidence_state=legacy_incomplete with named nulls; nothing is guessed from
// neighbouring rows or current configuration.
func buildEventSummaryV1(eventType string, failureKind *string, admissionReason *string, cycleRetryAttempts int, cumulativeRetryAttempts int, lastRetryDelayMS int, nextRetryAt *time.Time, policyCycleRetryAttemptLimit *int, banMode *string, policyBanCumulativeRetryAttemptThreshold *int, bannedUntilAt *time.Time, lastSuccessAt *time.Time) EventSummaryV1 {
	params := EventSummaryV1Params{
		EvidenceState:                            EvidenceStateComplete,
		FailureKind:                              failureKind,
		AdmissionReason:                          admissionReason,
		CycleRetryAttempts:                       intParam(cycleRetryAttempts),
		CumulativeRetryAttempts:                  intParam(cumulativeRetryAttempts),
		LastRetryDelayMS:                         intParam(lastRetryDelayMS),
		NextRetryAt:                              nextRetryAt,
		PolicyCycleRetryAttemptLimit:             policyCycleRetryAttemptLimit,
		BanMode:                                  banMode,
		PolicyBanCumulativeRetryAttemptThreshold: policyBanCumulativeRetryAttemptThreshold,
		BannedUntilAt:                            bannedUntilAt,
		LastSuccessAt:                            lastSuccessAt,
	}
	switch eventType {
	case "retry_scheduled":
		if failureKind == nil {
			params.EvidenceState = EvidenceStateLegacyIncomplete
		}
		return EventSummaryV1{Version: 1, Code: EventSummaryCodeRetryScheduled, Params: params}
	case "retry_exhausted":
		if failureKind == nil || policyCycleRetryAttemptLimit == nil {
			params.EvidenceState = EvidenceStateLegacyIncomplete
		}
		return EventSummaryV1{Version: 1, Code: EventSummaryCodeRetryExhausted, Params: params}
	case "banned":
		if failureKind == nil || banMode == nil || policyBanCumulativeRetryAttemptThreshold == nil {
			params.EvidenceState = EvidenceStateLegacyIncomplete
		}
		return EventSummaryV1{Version: 1, Code: EventSummaryCodeBanned, Params: params}
	case "unbanned":
		if banMode == nil || bannedUntilAt == nil {
			params.EvidenceState = EvidenceStateLegacyIncomplete
		}
		params.CycleRetryAttempts = nil
		params.CumulativeRetryAttempts = nil
		params.LastRetryDelayMS = nil
		params.NextRetryAt = nil
		params.PolicyCycleRetryAttemptLimit = nil
		params.PolicyBanCumulativeRetryAttemptThreshold = nil
		return EventSummaryV1{Version: 1, Code: EventSummaryCodeUnbanned, Params: params}
	case "recovered":
		if lastSuccessAt == nil {
			params.EvidenceState = EvidenceStateLegacyIncomplete
		}
		params.BanMode = nil
		params.BannedUntilAt = nil
		params.PolicyCycleRetryAttemptLimit = nil
		params.PolicyBanCumulativeRetryAttemptThreshold = nil
		return EventSummaryV1{Version: 1, Code: EventSummaryCodeRecovered, Params: params}
	case "admission_rejected":
		if admissionReason == nil {
			params.EvidenceState = EvidenceStateLegacyIncomplete
		}
		params.FailureKind = nil
		params.CycleRetryAttempts = nil
		params.CumulativeRetryAttempts = nil
		params.LastRetryDelayMS = nil
		params.NextRetryAt = nil
		params.PolicyCycleRetryAttemptLimit = nil
		params.BanMode = nil
		params.PolicyBanCumulativeRetryAttemptThreshold = nil
		params.BannedUntilAt = nil
		params.LastSuccessAt = nil
		return EventSummaryV1{Version: 1, Code: EventSummaryCodeAdmissionRejected, Params: params}
	default:
		// Unknown event type: keep the raw enum in the params-free fallback
		// shape; the frontend shows a localized generic fallback plus the raw
		// code for diagnostics.
		params = EventSummaryV1Params{EvidenceState: EvidenceStateLegacyIncomplete}
		return EventSummaryV1{Version: 1, Code: "loadbalance." + strings.ToLower(strings.TrimSpace(eventType)), Params: params}
	}
}

// EventModelProjection is the event-time model identity per SPEC §7.2.
type EventModelProjection struct {
	ModelConfigID *int    `json:"model_config_id"`
	ID            *string `json:"id"`
	Label         string  `json:"label"`
	Configured    *bool   `json:"configured"`
	Attribution   string  `json:"attribution"`
}

// EventEndpointProjection is the event-time endpoint identity per SPEC §7.2.
type EventEndpointProjection struct {
	ID          *int   `json:"id"`
	Label       string `json:"label"`
	Configured  *bool  `json:"configured"`
	Attribution string `json:"attribution"`
}

// EventTerminalTargetProjection is the event-time terminal target identity per
// SPEC §7.2; the owner model row id is only returned when the target's current
// unique owner can be resolved.
type EventTerminalTargetProjection struct {
	ID                 *int   `json:"id"`
	OwnerModelConfigID *int   `json:"owner_model_config_id"`
	Label              string `json:"label"`
	Configured         *bool  `json:"configured"`
	Attribution        string `json:"attribution"`
}

// EventRequestContextFilters is the closed V1 handoff shape for the
// "investigate nearby requests" contextual window.
type EventRequestContextFilters struct {
	SchemaVersion    int     `json:"schema_version"`
	Kind             string  `json:"kind"`
	Correlation      string  `json:"correlation"`
	FromTime         string  `json:"from_time"`
	ToTime           string  `json:"to_time"`
	ModelID          *string `json:"model_id"`
	EndpointID       *int    `json:"endpoint_id"`
	TerminalTargetID *int    `json:"terminal_target_id"`
}

// EventListItem is the events timeline row DTO (SPEC §7.2).
type EventListItem struct {
	EventID                                  string                        `json:"event_id"`
	CreatedAt                                time.Time                     `json:"created_at"`
	EventType                                string                        `json:"event_type"`
	Summary                                  EventSummaryV1                `json:"summary"`
	FailureKind                              *string                       `json:"failure_kind"`
	AdmissionReason                          *string                       `json:"admission_reason"`
	Model                                    EventModelProjection          `json:"model"`
	Endpoint                                 EventEndpointProjection       `json:"endpoint"`
	TerminalTarget                           EventTerminalTargetProjection `json:"terminal_target"`
	CycleRetryAttempts                       int                           `json:"cycle_retry_attempts"`
	CumulativeRetryAttempts                  int                           `json:"cumulative_retry_attempts"`
	NextRetryAt                              *time.Time                    `json:"next_retry_at"`
	LastRetryDelayMS                         int                           `json:"last_retry_delay_ms"`
	BanMode                                  *string                       `json:"ban_mode"`
	PolicyCycleRetryAttemptLimit             *int                          `json:"policy_cycle_retry_attempt_limit"`
	PolicyBanCumulativeRetryAttemptThreshold *int                          `json:"policy_ban_cumulative_retry_attempt_threshold"`
	BannedUntilAt                            *time.Time                    `json:"banned_until_at"`
	LastSuccessAt                            *time.Time                    `json:"last_success_at"`
	RequestContextFilters                    *EventRequestContextFilters   `json:"request_context_filters"`
	RequestContextUnavailableReason          *string                       `json:"request_context_unavailable_reason"`
}

// EventListEnvelope is the events list response envelope.
type EventListEnvelope struct {
	GeneratedAt  time.Time         `json:"generated_at"`
	Coverage     EventCoverage     `json:"coverage"`
	SourceStatus EventSourceStatus `json:"source_status"`
	Items        []EventListItem   `json:"items"`
	HasMore      bool              `json:"has_more"`
	NextCursor   *string           `json:"next_cursor"`
}

// EventCoverage reports whether the requested event window is fully covered.
type EventCoverage struct {
	Complete            bool               `json:"complete"`
	Gaps                []EventCoverageGap `json:"gaps"`
	RetentionEpoch      string             `json:"retention_epoch,omitempty"`
	RetentionGeneration string             `json:"retention_generation,omitempty"`
	PurgeState          string             `json:"purge_state,omitempty"`
	SourceRevision      string             `json:"source_revision,omitempty"`
}

// EventCoverageGap is one uncovered event interval.
type EventCoverageGap struct {
	FromTime string `json:"from_time"`
	ToTime   string `json:"to_time"`
	Reason   string `json:"reason"`
}

// EventSourceStatus reports the best-effort nature of the event source.
type EventSourceStatus struct {
	Delivery                 string `json:"delivery"`
	TransitionLedgerComplete bool   `json:"transition_ledger_complete"`
	DroppedEventCount        *int   `json:"dropped_event_count"`
}

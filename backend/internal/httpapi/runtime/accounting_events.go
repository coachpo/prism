package runtime

import (
	"strings"
	"time"

	gatewayaccounting "github.com/coachpo/prism/backend/internal/gateway/accounting"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

func buildRuntimeAccountingFinalEvent(event usageEventInsert, requestLogs []requestLogInsert, routeReason gatewaycore.RouteReason, usageSource gatewaycore.UsageSource) gatewayaccounting.Event {
	finalAuditEnabled, finalAuditCaptureBodies := runtimeAccountingFinalAuditState(requestLogs)
	accountingEvent, err := gatewayaccounting.NewEvent(gatewayaccounting.Event{
		Phase:                    gatewayaccounting.EventPhaseFinal,
		RequestID:                event.IngressRequestID,
		ProfileID:                event.ProfileID,
		OperationName:            event.OperationName,
		APIFamily:                event.APIFamily,
		RequestedModelID:         event.ModelID,
		EffectiveModelID:         cloneRuntimeStringPointer(event.ResolvedTargetModelID),
		EndpointID:               cloneRuntimeIntPointer(event.EndpointID),
		ConnectionID:             cloneRuntimeIntPointer(event.ConnectionID),
		SelectedTerminalTargetID: cloneRuntimeIntPointer(event.SelectedTerminalTargetID),
		AttemptNumber:            event.AttemptCount,
		Final:                    true,
		StatusCode:               event.StatusCode,
		Success:                  event.SuccessFlag,
		RouteReason:              routeReason,
		UsageSource:              usageSource,
		PricingConfigVersionUsed: cloneRuntimeIntPointer(event.PricingConfigVersionUsed),
		StreamOutcome:            event.StreamOutcome,
		AuditEnabled:             finalAuditEnabled,
		AuditCaptureBodies:       finalAuditCaptureBodies,
		ObservedAt:               event.CreatedAt,
	})
	if err != nil {
		return gatewayaccounting.Event{}
	}
	return accountingEvent
}

func runtimeAccountingFinalAuditState(requestLogs []requestLogInsert) (bool, bool) {
	if len(requestLogs) == 0 {
		return false, false
	}
	finalLog := requestLogs[len(requestLogs)-1]
	return finalLog.AuditEnabledAtRequest, finalLog.AuditCaptureBodiesAtRequest
}

func buildRuntimeAccountingAttemptEvents(requestLogs []requestLogInsert, routeReason gatewaycore.RouteReason, usageSource gatewaycore.UsageSource) []gatewayaccounting.Event {
	events := make([]gatewayaccounting.Event, 0, len(requestLogs))
	for index, requestLog := range requestLogs {
		attemptUsageSource := gatewaycore.UsageSourceMissing
		if index == len(requestLogs)-1 {
			attemptUsageSource = usageSource
		}
		accountingEvent, err := gatewayaccounting.NewEvent(gatewayaccounting.Event{
			Phase:                    gatewayaccounting.EventPhaseAttempt,
			RequestID:                requestLog.IngressRequestID,
			ProfileID:                requestLog.ProfileID,
			OperationName:            requestLog.OperationName,
			APIFamily:                requestLog.APIFamily,
			RequestedModelID:         requestLog.ModelID,
			EffectiveModelID:         cloneRuntimeStringPointer(requestLog.ResolvedTargetModelID),
			EndpointID:               cloneRuntimeIntPointer(requestLog.EndpointID),
			ConnectionID:             cloneRuntimeIntPointer(requestLog.ConnectionID),
			SelectedTerminalTargetID: cloneRuntimeIntPointer(requestLog.SelectedTerminalTargetID),
			AttemptNumber:            requestLog.AttemptNumber,
			Final:                    index == len(requestLogs)-1,
			StatusCode:               requestLog.StatusCode,
			Success:                  requestLog.SuccessFlag,
			RouteReason:              routeReason,
			UsageSource:              attemptUsageSource,
			PricingConfigVersionUsed: cloneRuntimeIntPointer(requestLog.PricingConfigVersionUsed),
			StreamOutcome:            requestLog.StreamOutcome,
			AuditEnabled:             requestLog.AuditEnabledAtRequest,
			AuditCaptureBodies:       requestLog.AuditCaptureBodiesAtRequest,
			ObservedAt:               requestLog.CreatedAt,
		})
		if err != nil {
			continue
		}
		events = append(events, accountingEvent)
	}
	return events
}

func runtimeUsageSourceFromCapture(capture runtimeResponseCapture, usage responseUsage, streamOutcome string) gatewaycore.UsageSource {
	if capture.UsageSource != "" {
		return gatewayaccounting.NormalizeUsageSource(capture.UsageSource)
	}
	return runtimeUsageSourceFromUsage(usage, streamOutcome)
}

func runtimeUsageSourceFromUsage(usage responseUsage, streamOutcome string) gatewaycore.UsageSource {
	if usage.discarded {
		return gatewaycore.UsageSourceNormalizationRejected
	}
	if !usage.hasValues() {
		return gatewaycore.UsageSourceMissing
	}
	if runtimeStreamOutcomeIsStreaming(streamOutcome) {
		return gatewaycore.UsageSourceProviderStreamTerminal
	}
	return gatewaycore.UsageSourceProvider
}

func runtimeResponseTiming(startedAt time.Time, completedAt time.Time, isStream bool, capture runtimeResponseCapture) (*int, *int) {
	var ttftMS *int
	if capture.FirstMeaningfulPayloadAt != nil {
		ttft := durationMilliseconds(capture.FirstMeaningfulPayloadAt.Sub(startedAt))
		ttftMS = &ttft
	}
	finishedAt := completedAt
	if capture.CompletedAt != nil && !capture.CompletedAt.IsZero() {
		finishedAt = capture.CompletedAt.UTC()
	}
	if !isStream {
		completionDuration := durationMilliseconds(finishedAt.Sub(startedAt))
		return ttftMS, &completionDuration
	}
	if capture.CompletedAt == nil {
		return ttftMS, nil
	}
	completionDuration := durationMilliseconds(finishedAt.Sub(startedAt))
	return ttftMS, &completionDuration
}

func runtimeStreamOutcomeForTelemetry(outcome string) string {
	switch strings.TrimSpace(outcome) {
	case runtimeStreamOutcomeNotStreaming:
		return runtimeStreamOutcomeNotStreaming
	case runtimeStreamOutcomeCompleted:
		return runtimeStreamOutcomeCompleted
	case runtimeStreamOutcomeProviderIncomplete:
		return runtimeStreamOutcomeProviderIncomplete
	case runtimeStreamOutcomeClientDisconnected:
		return runtimeStreamOutcomeClientDisconnected
	case runtimeStreamOutcomeUpstreamReadError:
		return runtimeStreamOutcomeUpstreamReadError
	case runtimeStreamOutcomeUpstreamEndedWithoutTerminal:
		return runtimeStreamOutcomeUpstreamEndedWithoutTerminal
	case runtimeStreamOutcomeUnknown:
		return runtimeStreamOutcomeUnknown
	default:
		return runtimeStreamOutcomeUnknown
	}
}

func runtimeStreamOutcomeIsStreaming(outcome string) bool {
	return runtimeStreamOutcomeForTelemetry(outcome) != runtimeStreamOutcomeNotStreaming
}

func runtimeCapturedAuditBody(enabled bool, body []byte) *string {
	if !enabled || len(body) == 0 {
		return nil
	}
	resolved := string(body)
	return &resolved
}

// billingStateUnpricedOnly preserves the legacy unpriced-reason derivation
// for non-2xx diagnostic rows without resurrecting billable/priced flags:
// the new writer never persists billable_flag/priced_flag (Requests SPEC §3.7).
func billingStateUnpricedOnly(success bool) *string {
	if !success {
		return nil
	}
	return stringPtr("missing_pricing_template")
}

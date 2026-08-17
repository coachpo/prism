package runtime

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

func resolvedTargetModelIDForResult(plan requestPlan, result executionResult) *string {
	if result.ResolvedTargetModelID != nil && strings.TrimSpace(*result.ResolvedTargetModelID) != "" {
		return result.ResolvedTargetModelID
	}
	return plan.ResolvedTargetModelID
}

func finalExecutionAttempt(result executionResult) *executionAttempt {
	if len(result.Attempts) == 0 {
		return nil
	}
	attempt := result.Attempts[len(result.Attempts)-1]
	return &attempt
}

func executionAttemptUpstreamOperationName(attempt *executionAttempt) *string {
	if attempt == nil {
		return nil
	}
	return trimmedStringPointer(attempt.UpstreamOperationName)
}

func executionAttemptUpstreamRequestPath(attempt *executionAttempt) *string {
	if attempt == nil {
		return nil
	}
	return trimmedStringPointer(attempt.UpstreamRequestPath)
}

func executionAttemptTranslationMode(attempt *executionAttempt) *string {
	if attempt == nil {
		return nil
	}
	return runtimeTranslationModePointer(attempt.OperationTranslationMode)
}

func runtimeEndpointLabelSnapshot(endpoint runtimeEndpoint) string {
	if endpoint.Name != nil {
		if label := strings.TrimSpace(*endpoint.Name); label != "" {
			return label
		}
	}
	if label := strings.TrimSpace(endpoint.BaseURL); label != "" {
		return label
	}
	if endpoint.ID > 0 {
		return fmt.Sprintf("Endpoint %d", endpoint.ID)
	}
	return "Unknown Endpoint"
}

func usageEventEndpointLabelSnapshotForInsert(usageEvent usageEventInsert) string {
	if label := strings.TrimSpace(usageEvent.EndpointLabelSnapshot); label != "" {
		return label
	}
	if usageEvent.EndpointID != nil {
		return fmt.Sprintf("Endpoint %d", *usageEvent.EndpointID)
	}
	return "Unknown Endpoint"
}

func buildRuntimeUsageEvent(plan requestPlan, result executionResult, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, requestLogCount int) usageEventInsert {
	attemptCount := upstreamAttemptCount(telemetry.attempts)
	finalAttempt := finalExecutionAttempt(result)
	usageEvent := usageEventInsert{
		ProfileID:                plan.ProfileID,
		IngressRequestID:         telemetry.ingressRequestID,
		ModelID:                  plan.RequestedModelID,
		ResolvedTargetModelID:    resolvedTargetModelIDForResult(plan, result),
		APIFamily:                plan.APIFamily,
		OperationName:            telemetry.operationName,
		UpstreamOperationName:    executionAttemptUpstreamOperationName(finalAttempt),
		OperationTranslationMode: executionAttemptTranslationMode(finalAttempt),
		EndpointID:               intPtr(result.Connection.Endpoint.ID),
		EndpointLabelSnapshot:    runtimeEndpointLabelSnapshot(result.Connection.Endpoint),
		ConnectionID:             intPtr(result.Connection.ID),
		SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
		ProxyAPIKeyIDSnapshot:    proxyKeyIDPointer(telemetry.proxyKey),
		ProxyAPIKeyNameSnapshot:  proxyKeyNamePointer(telemetry.proxyKey),
		StatusCode:               result.Response.StatusCode,
		SuccessFlag:              telemetry.successFlag,
		InputTokens:              telemetry.usage.InputTokens,
		OutputTokens:             telemetry.usage.OutputTokens,
		TotalTokens:              telemetry.usage.TotalTokens,
		CacheReadInputTokens:     telemetry.usage.CacheReadInputTokens,
		CacheCreationInputTokens: telemetry.usage.CacheCreationInputTokens,
		ReasoningTokens:          telemetry.usage.ReasoningTokens,
		AttemptCount:             attemptCount,
		RequestPath:              request.URL.Path,
		UpstreamRequestPath:      executionAttemptUpstreamRequestPath(finalAttempt),
		CreatedAt:                telemetry.requestCompletedAt,
		ResponseTimeMS:           intPtr(telemetry.responseTimeMS),
		CompletionDurationMS:     telemetry.completionDurationMS,
		TTFTMS:                   telemetry.ttftMS,
		StreamOutcome:            telemetry.streamOutcome,
		StreamErrorKind:          telemetry.streamErrorKind,
		CurrencyAttribution:      runtimeUsageCurrencyAttributionIdentified,
	}
	usageEvent.applyRuntimePricingResult(telemetry.pricingResult)
	applyRuntimeUsageEventFinalizedFields(&usageEvent, plan, result, telemetry, finalAttempt, requestLogCount)
	applyRuntimeUsageEventPricingScope(&usageEvent, usageEvent.StatusCode)
	return usageEvent
}

// applyRuntimeUsageEventPricingScope forces the finalized-ingress pricing
// classifier for the usage event: non-2xx finalized results are always
// ineligible (Pricing SPEC §3.4) with no reason/resolution/components, while
// 2xx abnormal streams stay in the pricing cohort.
func applyRuntimeUsageEventPricingScope(usageEvent *usageEventInsert, statusCode int) {
	if statusCode >= 200 && statusCode <= 299 {
		return
	}
	usageEvent.PricingStatus = runtimePricingStatusIneligible
	usageEvent.PricingEvidenceTrust = runtimePricingEvidenceTrust
	usageEvent.UnpricedReason = nil
	usageEvent.PricingResolutionKind = nil
	usageEvent.MissingPriceComponents = nil
}

// upstreamAttemptCount counts only real launched upstream rows; diagnostic
// rows never count as attempts.
func upstreamAttemptCount(attempts []executionAttempt) int {
	count := 0
	for _, attempt := range attempts {
		if attempt.LaunchOrdinal > 0 {
			count++
		}
	}
	if count == 0 && len(attempts) > 0 {
		count = 1
	}
	return count
}

// applyRuntimeUsageEventFinalizedFields fills the Observe finalized-ingress
// fields (Observe SPEC §3.5, Requests SPEC §3.6). The winner's identity comes
// from the executor's persisted arbitration; final_target_entry_trigger is the
// trigger of the winning actual target's first entry into the chain, never
// inferred from completion order.
func applyRuntimeUsageEventFinalizedFields(usageEvent *usageEventInsert, plan requestPlan, result executionResult, telemetry runtimeTelemetryEnvelopeContext, finalAttempt *executionAttempt, requestLogCount int) {
	expectedRows := requestLogCount
	if expectedRows < 1 {
		expectedRows = 1
	}
	usageEvent.ExpectedRequestLogRowCount = intPtr(expectedRows)

	// Routing evidence from persisted triggers.
	var winnerAttempt *executionAttempt
	for index := range telemetry.attempts {
		attempt := telemetry.attempts[index]
		switch attempt.AttemptTrigger {
		case attemptTriggerRetrySameTarget:
			usageEvent.SameTargetRetryOccurred = true
		case attemptTriggerHedge:
			usageEvent.HedgeOccurred = true
		case attemptTriggerFailover:
			usageEvent.FailoverOccurred = true
		}
		if telemetry.winnerOrdinal > 0 && attempt.LaunchOrdinal == telemetry.winnerOrdinal {
			winnerAttempt = &telemetry.attempts[index]
		}
	}
	if winnerAttempt == nil && finalAttempt != nil {
		winnerAttempt = finalAttempt
	}

	// Zero-launched requests: expected counts still reconcile (Observe SPEC
	// §3.5), but final attempt/target-entry identity stays null.
	if winnerAttempt != nil && winnerAttempt.LaunchOrdinal > 0 {
		usageEvent.FinalAttemptNumber = intPtr(winnerAttempt.LaunchOrdinal)
		usageEvent.FinalAttemptTrigger = optionalTrimmedStringPointer(winnerAttempt.AttemptTrigger)
		// The winning actual target's first entry trigger: the first launched
		// attempt targeting the same connection.
		for index := range telemetry.attempts {
			attempt := telemetry.attempts[index]
			if attempt.Connection.ID == winnerAttempt.Connection.ID && validateAttemptTrigger(attempt.AttemptTrigger) {
				usageEvent.FinalTargetEntryTrigger = optionalTrimmedStringPointer(attempt.AttemptTrigger)
				break
			}
		}
		if usageEvent.FinalTargetEntryTrigger == nil {
			usageEvent.FinalTargetEntryTrigger = optionalTrimmedStringPointer(winnerAttempt.AttemptTrigger)
		}
	}
	if !usageEvent.SameTargetRetryOccurred && !usageEvent.HedgeOccurred && !usageEvent.FailoverOccurred {
		// New writer rows always carry authoritative triggers for launched
		// attempts; a single-attempt ingress is complete evidence.
		usageEvent.RoutingEvidenceComplete = boolPtr(telemetry.winnerOrdinal > 0 || len(telemetry.attempts) > 0)
	} else {
		usageEvent.RoutingEvidenceComplete = boolPtr(true)
	}

	// Ingress wall-clock only from authoritative finalized evidence.
	if !telemetry.ingressStartedAt.IsZero() && !telemetry.requestCompletedAt.IsZero() {
		startedAt := telemetry.ingressStartedAt
		completedAt := telemetry.requestCompletedAt.UTC()
		usageEvent.IngressStartedAt = &startedAt
		usageEvent.IngressCompletedAt = &completedAt
	}

	// Terminal error code for failed/client-disconnected final results
	// (Requests SPEC §3.6): never copied into an upstream row, never invented
	// for completed results.
	if !usageEvent.SuccessFlag {
		switch telemetry.streamOutcome {
		case runtimeStreamOutcomeClientDisconnected:
			usageEvent.FinalErrorCode = stringPtr(safediag.CodeClientDisconnected)
		default:
			if winnerAttempt != nil && winnerAttempt.Diagnostics != nil && winnerAttempt.Diagnostics.Code != "" {
				usageEvent.FinalErrorCode = stringPtr(winnerAttempt.Diagnostics.Code)
			} else if winnerAttempt != nil && winnerAttempt.StatusCode >= 200 && winnerAttempt.StatusCode < 300 {
				// Abnormal 2xx stream without a captured code.
				usageEvent.FinalErrorCode = optionalTrimmedStringPointer(safediag.StreamOutcomeFallbackCode(telemetry.streamOutcome))
			} else if winnerAttempt != nil {
				usageEvent.FinalErrorCode = stringPtr(safediag.HTTPFallbackCode(winnerAttempt.StatusCode))
			}
		}
	}
}

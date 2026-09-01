package runtime

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

// buildRuntimeBudgetExhaustionTelemetryEnvelope materializes terminal
// execution failures after at least one real upstream launch. Despite the
// historical name, this path owns both ordinary all-transport failures and the
// 64-launch safety bound. Attempt rows keep their own actual identities while
// the finalized usage row carries an actual target only when an executor winner
// exists.
func (s *Service) buildRuntimeBudgetExhaustionTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	if selectedTerminalTargetID == nil {
		selectedTerminalTargetID = plan.selectedTerminalTargetID()
	}
	telemetryPlan := plan
	telemetryPlan.SelectedTerminalTargetID = cloneRuntimeIntPointer(selectedTerminalTargetID)
	routeReason := runtimeExecutionRouteReason(result.RouteReason)
	if routeReason == gatewaycore.RouteReasonDirectMatch {
		routeReason = gatewaycore.RouteReasonPolicyReject
	}
	outputDelivery := outputDeliveryForFailure(plan.RuntimeOperation)

	callerRequestID := strings.TrimSpace(runtimeCallerRequestIDFromContext(request.Context()))
	telemetry := runtimeTelemetryEnvelopeContext{
		runtimeTelemetryPricingTimingContext: runtimeTelemetryPricingTimingContext{
			requestCompletedAt:   requestCompletedAt,
			responseTimeMS:       responseTimeMS,
			streamOutcome:        runtimeStreamOutcomeNotStreaming,
			isStream:             plan.IsStreamingRequest,
			successFlag:          false,
			reportCurrencyCode:   runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code),
			reportCurrencySymbol: runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol),
			operationName:        strings.TrimSpace(plan.RuntimeOperation.Name),
			outputDelivery:       outputDelivery,
		},
		ingressRequestID:          ingressRequestID,
		ingressStartedAt:          startedAt.UTC(),
		callerRequestID:           optionalTrimmedStringPointer(callerRequestID),
		proxyKey:                  proxyKey,
		callerUserAgent:           trimmedStringPointer(request.UserAgent()),
		requestGenerationSnapshot: plan.RequestGenerationParamsSnapshot(),
		attempts:                  result.Attempts,
		winnerOrdinal:             result.WinnerOrdinal,
		routeReason:               routeReason,
	}
	requestLogs := buildRuntimeRequestLogRows(telemetryPlan, request, telemetry)
	if len(requestLogs) == 0 {
		requestLog := requestLogInsert{
			ProfileID:                   plan.ProfileID,
			ModelID:                     plan.RequestedModelID,
			ResolvedTargetModelID:       nil,
			APIFamily:                   plan.APIFamily,
			OperationName:               strings.TrimSpace(plan.RuntimeOperation.Name),
			IngressRequestID:            ingressRequestID,
			AttemptNumber:               0,
			UpstreamStatusCode:          nil,
			GatewayStatusCode:           intPtr(runtimeErr.StatusCode),
			IsStream:                    plan.IsStreamingRequest,
			SuccessFlag:                 false,
			RequestPath:                 request.URL.Path,
			CreatedAt:                   requestCompletedAt,
			CallerUserAgent:             trimmedStringPointer(request.UserAgent()),
			RowKind:                     requestLogRowKindAdmission,
			URLScrubProvenance:          runtimeURLScrubProvenanceRuntime,
			PricingStatus:               runtimePricingStatusIneligible,
			PricingEvidenceTrust:        runtimePricingEvidenceTrust,
			StreamOutcome:               runtimeStreamOutcomeNotStreaming,
			OutputRateState:             outputDelivery.State,
			OutputRateReason:            outputDelivery.Reason,
			AuditEnabledAtRequest:       plan.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: plan.AuditCaptureBodiesAtRequest,
		}
		applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)
		requestLogs = append(requestLogs, requestLog)
	}

	// Final identity belongs exclusively to the executor winner. All-transport
	// and launch-budget terminal errors have no winner and therefore keep every
	// actual final-target field null while retaining the planning-primary field.
	var finalResolvedTargetModelID *string
	var finalUpstreamModelID *string
	var finalEndpointID *int
	var finalConnectionID *int
	finalEndpointLabel := "Unknown Endpoint"
	var finalAttemptNumber *int
	var finalAttemptTrigger *string
	var finalTargetEntryTrigger *string
	winnerAttempt := executionAttemptForLaunchOrdinal(result.Attempts, result.WinnerOrdinal)
	if winnerAttempt != nil {
		finalResolvedTargetModelID = optionalTrimmedStringPointer(winnerAttempt.ResolvedTargetModelID)
		finalUpstreamModelID = upstreamModelIDSnapshot(winnerAttempt.Connection)
		if winnerAttempt.Connection.Endpoint.ID > 0 {
			finalEndpointID = intPtr(winnerAttempt.Connection.Endpoint.ID)
		}
		if winnerAttempt.Connection.ID > 0 {
			finalConnectionID = intPtr(winnerAttempt.Connection.ID)
			finalEndpointLabel = runtimeEndpointLabelSnapshot(winnerAttempt.Connection.Endpoint)
		}
		if winnerAttempt.LaunchOrdinal > 0 {
			finalAttemptNumber = intPtr(winnerAttempt.LaunchOrdinal)
		}
		finalAttemptTrigger = optionalTrimmedStringPointer(winnerAttempt.AttemptTrigger)
		finalTargetEntryTrigger = finalTargetEntryTriggerForAttempts(result.Attempts, winnerAttempt.Connection.ID)
	}
	attemptCount := len(result.Attempts)
	expectedRows := len(requestLogs)
	usageEvent := usageEventInsert{
		ProfileID:                  plan.ProfileID,
		IngressRequestID:           ingressRequestID,
		ModelID:                    plan.RequestedModelID,
		ResolvedTargetModelID:      finalResolvedTargetModelID,
		UpstreamModelID:            finalUpstreamModelID,
		APIFamily:                  plan.APIFamily,
		OperationName:              strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:                 finalEndpointID,
		EndpointLabelSnapshot:      finalEndpointLabel,
		ConnectionID:               finalConnectionID,
		SelectedTerminalTargetID:   selectedTerminalTargetID,
		ProxyAPIKeyIDSnapshot:      proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:    proxyKeyNamePointer(proxyKey),
		StatusCode:                 runtimeErr.StatusCode,
		SuccessFlag:                false,
		UnpricedReason:             nil,
		ReportCurrencyCode:         telemetry.reportCurrencyCode,
		ReportCurrencySymbol:       telemetry.reportCurrencySymbol,
		AttemptCount:               attemptCount,
		ExpectedRequestLogRowCount: intPtr(expectedRows),
		RequestPath:                request.URL.Path,
		CreatedAt:                  requestCompletedAt,
		ResponseTimeMS:             intPtr(responseTimeMS),
		CompletionDurationMS:       intPtr(responseTimeMS),
		StreamOutcome:              runtimeStreamOutcomeNotStreaming,
		OutputRateState:            outputDelivery.State,
		OutputRateReason:           outputDelivery.Reason,
		PricingStatus:              runtimePricingStatusIneligible,
		PricingEvidenceTrust:       runtimePricingEvidenceTrust,
		CurrencyAttribution:        runtimeUsageCurrencyAttributionIdentified,
		FinalErrorCode:             stringPtr(runtimeDiagnosticFailureCode(runtimeErr, failureStageUpstreamConnect)),
		FinalAttemptNumber:         finalAttemptNumber,
		FinalAttemptTrigger:        finalAttemptTrigger,
		FinalTargetEntryTrigger:    finalTargetEntryTrigger,
		IngressStartedAt:           &startedAt,
		IngressCompletedAt:         &requestCompletedAt,
	}
	applyRuntimeFailureRoutingEvidence(&usageEvent, result.Attempts)
	accountingAttempts := buildRuntimeAccountingAttemptEvents(requestLogs, routeReason, gatewaycore.UsageSourceMissing)
	for index := range accountingAttempts {
		accountingAttempts[index].Final = result.WinnerOrdinal > 0 && accountingAttempts[index].AttemptNumber == result.WinnerOrdinal
	}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   accountingAttempts,
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

func executionAttemptForLaunchOrdinal(attempts []executionAttempt, launchOrdinal int) *executionAttempt {
	if launchOrdinal <= 0 {
		return nil
	}
	for index := range attempts {
		if attempts[index].LaunchOrdinal == launchOrdinal {
			return &attempts[index]
		}
	}
	return nil
}

func finalTargetEntryTriggerForAttempts(attempts []executionAttempt, connectionID int) *string {
	if connectionID <= 0 {
		return nil
	}
	var first *executionAttempt
	for index := range attempts {
		attempt := &attempts[index]
		if attempt.Connection.ID != connectionID || attempt.LaunchOrdinal <= 0 || !validateAttemptTrigger(attempt.AttemptTrigger) {
			continue
		}
		if first == nil || attempt.LaunchOrdinal < first.LaunchOrdinal {
			first = attempt
		}
	}
	if first == nil {
		return nil
	}
	switch first.AttemptTrigger {
	case attemptTriggerInitial, attemptTriggerFailover, attemptTriggerHedge:
		return stringPtr(first.AttemptTrigger)
	default:
		return stringPtr(attemptResultUnknown)
	}
}

func applyRuntimeFailureRoutingEvidence(usageEvent *usageEventInsert, attempts []executionAttempt) {
	if usageEvent == nil {
		return
	}
	complete := true
	for _, attempt := range attempts {
		if attempt.LaunchOrdinal <= 0 || !validateAttemptTrigger(attempt.AttemptTrigger) {
			complete = false
		}
		switch attempt.AttemptTrigger {
		case attemptTriggerRetrySameTarget:
			usageEvent.SameTargetRetryOccurred = true
		case attemptTriggerHedge:
			usageEvent.HedgeOccurred = true
		case attemptTriggerFailover:
			usageEvent.FailoverOccurred = true
		}
	}
	usageEvent.RoutingEvidenceComplete = boolPtr(complete)
}

// applyRuntimeDiagnosticFailureFields writes the safe failure projection for a
// planning/admission diagnostic row from a Prism domain error (Requests SPEC
// §4.2/§4.4). The domain code wins when it matches the stable grammar;
// otherwise a stage fallback is used. The detail is value-scrubbed and capped
// at 4 KiB before persistence.
func applyRuntimeDiagnosticFailureFields(requestLog *requestLogInsert, runtimeErr *domainError) {
	if runtimeErr == nil {
		return
	}
	stage := failureStageRouting
	if requestLog.RowKind == requestLogRowKindAdmission {
		stage = failureStageAdmission
	}
	code := runtimeDiagnosticFailureCode(runtimeErr, stage)
	requestLog.ErrorSource = stringPtr(errorSourcePrism)
	requestLog.FailureStage = stringPtr(stage)
	requestLog.ErrorCode = stringPtr(code)
	scrubbed := scrubRuntimeDiagnosticDetail(runtimeErr, stage, code)
	requestLog.ErrorDetail = stringPtr(scrubbed.Value)
	requestLog.ErrorDetailRedacted = scrubbed.Redacted
	requestLog.ErrorDetailTruncated = scrubbed.Truncated
}

func runtimeDiagnosticFailureCode(runtimeErr *domainError, stage string) string {
	code := strings.TrimSpace(runtimeErr.ErrorCode)
	if !safediag.ValidErrorCode(code) {
		return safediag.PrismStageFallbackCode(stage)
	}
	return code
}

func scrubRuntimeDiagnosticDetail(runtimeErr *domainError, stage string, code string) safediag.ScrubResult {
	detail := strings.TrimSpace(runtimeErr.Detail)
	if detail == "" {
		detail = fmt.Sprintf("Prism %s failure (%s).", stage, code)
	}
	return safediag.ScrubValue(detail, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
}

// applyRuntimePlanningFailureMetadataScrub captures and scrubs caller/request
// metadata for planning/admission diagnostic rows. These synthetic rows must
// preserve the same caller correlation contract as upstream attempt rows.
func applyRuntimePlanningFailureMetadataScrub(requestLog *requestLogInsert, request *http.Request) {
	var provenance safediag.MetadataProvenance
	if callerRequestID := strings.TrimSpace(runtimeCallerRequestIDFromContext(request.Context())); callerRequestID != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerRequestID, callerRequestID, 255)
		requestLog.CallerRequestID = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerRequestID, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.CallerUserAgent != nil {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerUserAgent, *requestLog.CallerUserAgent, 0)
		requestLog.CallerUserAgent = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerUserAgent, scrubbed.Redacted, scrubbed.Truncated)
	}
	requestPath, pathTruncated := safediag.ScrubRequestPath(request.URL.Path)
	if pathTruncated {
		provenance.Record(safediag.MetadataFieldRequestPath, false, true)
	}
	requestLog.RequestPath = requestPath
	operationScrub := safediag.ScrubMetadataValue(safediag.MetadataFieldOperationName, requestLog.OperationName, 120)
	if operationScrub.Truncated {
		provenance.Record(safediag.MetadataFieldOperationName, operationScrub.Redacted, true)
	}
	requestLog.OperationName = operationScrub.Value
	requestLog.MetadataRedactedFields = safediag.CanonicalFieldNames(provenance.Redacted)
	requestLog.MetadataTruncatedFields = safediag.CanonicalFieldNames(provenance.Truncated)
}

func (s *Service) buildRuntimePlanningFailureTelemetryEnvelope(failure runtimePlanningFailureTelemetry, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	unpricedReason := billingStateUnpricedOnly(false)
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	reportCurrencyCode := runtimeOptionalTrimmedString(failure.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(failure.ReportCurrencySnapshot.Symbol)
	requestGenerationSnapshot := failure.RequestGenerationParams.clone()
	selectedTerminalTargetID := cloneRuntimeIntPointer(failure.SelectedTerminalTargetID)
	completionDurationMS := intPtr(responseTimeMS)
	outputDelivery := outputDeliveryForFailure(failure.RuntimeOperation)
	requestLog := requestLogInsert{
		ProfileID:                     failure.ProfileID,
		ModelID:                       failure.RequestedModelID,
		ResolvedTargetModelID:         nil,
		APIFamily:                     failure.APIFamily,
		OperationName:                 strings.TrimSpace(failure.RuntimeOperation.Name),
		UpstreamOperationName:         cloneRuntimeStringPointer(failure.UpstreamOperationName),
		OperationTranslationMode:      cloneRuntimeStringPointer(failure.OperationTranslationMode),
		EndpointID:                    nil,
		ConnectionID:                  nil,
		SelectedTerminalTargetID:      selectedTerminalTargetID,
		ProxyAPIKeyID:                 proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(proxyKey),
		IngressRequestID:              ingressRequestID,
		AttemptNumber:                 0,
		ProviderCorrelationID:         nil,
		EndpointBaseURL:               nil,
		EndpointDescription:           nil,
		StatusCode:                    runtimeErr.StatusCode,
		ResponseTimeMS:                responseTimeMS,
		IsStream:                      failure.IsStreamingRequest,
		SuccessFlag:                   false,
		UnpricedReason:                unpricedReason,
		ReportCurrencyCode:            reportCurrencyCode,
		ReportCurrencySymbol:          reportCurrencySymbol,
		RequestPath:                   failure.RequestPath,
		UpstreamRequestPath:           cloneRuntimeStringPointer(failure.UpstreamRequestPath),
		ErrorDetail:                   stringPtr(scrubRuntimeDiagnosticDetail(runtimeErr, failureStageRouting, runtimeDiagnosticFailureCode(runtimeErr, failureStageRouting)).Value),
		CreatedAt:                     requestCompletedAt,
		CallerUserAgent:               trimmedStringPointer(request.UserAgent()),
		UpstreamUserAgent:             nil,
		CompletionDurationMS:          completionDurationMS,
		TTFTMS:                        nil,
		StreamOutcome:                 runtimeStreamOutcomeNotStreaming,
		OutputRateState:               outputDelivery.State,
		OutputRateReason:              outputDelivery.Reason,
		AuditEnabledAtRequest:         failure.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:   failure.AuditCaptureBodiesAtRequest,
		RequestGenerationParams:       requestGenerationSnapshot.Params,
		RequestGenerationParamsStatus: trimmedStringPointer(requestGenerationSnapshot.Status),
		RowKind:                       requestLogRowKindPlanning,
		URLScrubProvenance:            runtimeURLScrubProvenanceRuntime,
		GatewayStatusCode:             intPtr(runtimeErr.StatusCode),
		PricingStatus:                 runtimePricingStatusIneligible,
		PricingEvidenceTrust:          runtimePricingEvidenceTrust,
	}
	applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)
	applyRuntimePlanningFailureMetadataScrub(&requestLog, request)
	// Zero-launch planning failure: attempt_count is real (0), not synthetic 1;
	// final identity stays null, selected remains planned.
	usageEvent := usageEventInsert{
		ProfileID:                  failure.ProfileID,
		IngressRequestID:           ingressRequestID,
		ModelID:                    failure.RequestedModelID,
		ResolvedTargetModelID:      nil,
		APIFamily:                  failure.APIFamily,
		OperationName:              strings.TrimSpace(failure.RuntimeOperation.Name),
		UpstreamOperationName:      cloneRuntimeStringPointer(failure.UpstreamOperationName),
		OperationTranslationMode:   cloneRuntimeStringPointer(failure.OperationTranslationMode),
		EndpointID:                 nil,
		EndpointLabelSnapshot:      "Unknown Endpoint",
		ConnectionID:               nil,
		SelectedTerminalTargetID:   selectedTerminalTargetID,
		ProxyAPIKeyIDSnapshot:      proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:    proxyKeyNamePointer(proxyKey),
		StatusCode:                 runtimeErr.StatusCode,
		SuccessFlag:                false,
		UnpricedReason:             unpricedReason,
		ReportCurrencyCode:         reportCurrencyCode,
		ReportCurrencySymbol:       reportCurrencySymbol,
		AttemptCount:               0,
		ExpectedRequestLogRowCount: intPtr(1),
		RequestPath:                failure.RequestPath,
		UpstreamRequestPath:        cloneRuntimeStringPointer(failure.UpstreamRequestPath),
		CreatedAt:                  requestCompletedAt,
		ResponseTimeMS:             intPtr(responseTimeMS),
		CompletionDurationMS:       completionDurationMS,
		TTFTMS:                     nil,
		StreamOutcome:              runtimeStreamOutcomeNotStreaming,
		StreamErrorKind:            nil,
		OutputRateState:            outputDelivery.State,
		OutputRateReason:           outputDelivery.Reason,
		PricingStatus:              runtimePricingStatusIneligible,
		PricingEvidenceTrust:       runtimePricingEvidenceTrust,
		CurrencyAttribution:        runtimeUsageCurrencyAttributionIdentified,
		RoutingEvidenceComplete:    boolPtr(true),
		FinalErrorCode:             stringPtr(runtimeDiagnosticFailureCode(runtimeErr, failureStageRouting)),
		IngressStartedAt:           &startedAt,
		IngressCompletedAt:         &requestCompletedAt,
	}
	routeReason := runtimeExecutionRouteReason(gatewaycore.RouteReasonPolicyReject)
	requestLogs := []requestLogInsert{requestLog}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   nil,
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

func (s *Service) buildRuntimeExecutionFailureTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	unpricedReason := billingStateUnpricedOnly(false)
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	requestGenerationSnapshot := plan.RequestGenerationParamsSnapshot()
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	if selectedTerminalTargetID == nil {
		selectedTerminalTargetID = plan.selectedTerminalTargetID()
	}
	routeReason := runtimeExecutionRouteReason(result.RouteReason)
	if routeReason == gatewaycore.RouteReasonDirectMatch {
		routeReason = gatewaycore.RouteReasonPolicyReject
	}
	completionDurationMS := intPtr(responseTimeMS)
	outputDelivery := outputDeliveryForFailure(plan.RuntimeOperation)
	requestLog := requestLogInsert{
		ProfileID:                     plan.ProfileID,
		ModelID:                       plan.RequestedModelID,
		ResolvedTargetModelID:         nil,
		APIFamily:                     plan.APIFamily,
		OperationName:                 strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:                    nil,
		ConnectionID:                  nil,
		SelectedTerminalTargetID:      selectedTerminalTargetID,
		ProxyAPIKeyID:                 proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(proxyKey),
		IngressRequestID:              ingressRequestID,
		AttemptNumber:                 0,
		StatusCode:                    runtimeErr.StatusCode,
		ResponseTimeMS:                responseTimeMS,
		IsStream:                      plan.IsStreamingRequest,
		SuccessFlag:                   false,
		UnpricedReason:                unpricedReason,
		ReportCurrencyCode:            reportCurrencyCode,
		ReportCurrencySymbol:          reportCurrencySymbol,
		RequestPath:                   request.URL.Path,
		CreatedAt:                     requestCompletedAt,
		CallerUserAgent:               trimmedStringPointer(request.UserAgent()),
		CompletionDurationMS:          completionDurationMS,
		StreamOutcome:                 runtimeStreamOutcomeNotStreaming,
		OutputRateState:               outputDelivery.State,
		OutputRateReason:              outputDelivery.Reason,
		AuditEnabledAtRequest:         plan.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:   plan.AuditCaptureBodiesAtRequest,
		RequestGenerationParams:       requestGenerationSnapshot.Params,
		RequestGenerationParamsStatus: trimmedStringPointer(requestGenerationSnapshot.Status),
		RowKind:                       requestLogRowKindAdmission,
		URLScrubProvenance:            runtimeURLScrubProvenanceRuntime,
		GatewayStatusCode:             intPtr(runtimeErr.StatusCode),
		PricingStatus:                 runtimePricingStatusIneligible,
		PricingEvidenceTrust:          runtimePricingEvidenceTrust,
	}
	applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)
	applyRuntimePlanningFailureMetadataScrub(&requestLog, request)
	// Zero-launch admission failure: real attempt_count 0, winner null, correct final error.
	usageEvent := usageEventInsert{
		ProfileID:                  plan.ProfileID,
		IngressRequestID:           ingressRequestID,
		ModelID:                    plan.RequestedModelID,
		ResolvedTargetModelID:      nil,
		APIFamily:                  plan.APIFamily,
		OperationName:              strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:                 nil,
		EndpointLabelSnapshot:      "Unknown Endpoint",
		ConnectionID:               nil,
		SelectedTerminalTargetID:   selectedTerminalTargetID,
		ProxyAPIKeyIDSnapshot:      proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:    proxyKeyNamePointer(proxyKey),
		StatusCode:                 runtimeErr.StatusCode,
		SuccessFlag:                false,
		UnpricedReason:             unpricedReason,
		ReportCurrencyCode:         reportCurrencyCode,
		ReportCurrencySymbol:       reportCurrencySymbol,
		AttemptCount:               0,
		ExpectedRequestLogRowCount: intPtr(1),
		RequestPath:                request.URL.Path,
		CreatedAt:                  requestCompletedAt,
		ResponseTimeMS:             intPtr(responseTimeMS),
		CompletionDurationMS:       completionDurationMS,
		StreamOutcome:              runtimeStreamOutcomeNotStreaming,
		OutputRateState:            outputDelivery.State,
		OutputRateReason:           outputDelivery.Reason,
		PricingStatus:              runtimePricingStatusIneligible,
		PricingEvidenceTrust:       runtimePricingEvidenceTrust,
		CurrencyAttribution:        runtimeUsageCurrencyAttributionIdentified,
		RoutingEvidenceComplete:    boolPtr(true),
		FinalErrorCode:             stringPtr(runtimeDiagnosticFailureCode(runtimeErr, failureStageAdmission)),
		IngressStartedAt:           &startedAt,
		IngressCompletedAt:         &requestCompletedAt,
	}
	requestLogs := []requestLogInsert{requestLog}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   nil,
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

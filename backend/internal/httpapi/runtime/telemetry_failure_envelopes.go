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

// buildRuntimeBudgetExhaustionTelemetryEnvelope materializes the 64-launch
// safety bound: one retained upstream row per launched attempt with typed
// attempt facts and safe diagnostics, plus a finalized usage summary with the
// gateway terminal code `attempt_budget_exhausted` and the true attempt count.
func (s *Service) buildRuntimeBudgetExhaustionTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	resolvedTargetModelID := cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	if resolvedTargetModelID == nil {
		resolvedTargetModelID = cloneRuntimeStringPointer(plan.ResolvedTargetModelID)
	}
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	if selectedTerminalTargetID == nil {
		selectedTerminalTargetID = plan.selectedTerminalTargetID()
	}
	routeReason := runtimeExecutionRouteReason(result.RouteReason)
	if routeReason == gatewaycore.RouteReasonDirectMatch {
		routeReason = gatewaycore.RouteReasonPolicyReject
	}

	requestLogs := make([]requestLogInsert, 0, len(result.Attempts))
	for _, attempt := range result.Attempts {
		attemptDurationMS := attempt.AttemptDurationMS
		if attemptDurationMS <= 0 {
			attemptDurationMS = attempt.ResponseTimeMS
		}
		requestLog := requestLogInsert{
			ProfileID:                   plan.ProfileID,
			ModelID:                     plan.RequestedModelID,
			ResolvedTargetModelID:       resolvedTargetModelID,
			APIFamily:                   plan.APIFamily,
			OperationName:               strings.TrimSpace(plan.RuntimeOperation.Name),
			EndpointID:                  intPtr(attempt.Connection.Endpoint.ID),
			ConnectionID:                intPtr(attempt.Connection.ID),
			SelectedTerminalTargetID:    selectedTerminalTargetID,
			ProxyAPIKeyID:               proxyKeyIDPointer(proxyKey),
			ProxyAPIKeyNameSnapshot:     proxyKeyNamePointer(proxyKey),
			IngressRequestID:            ingressRequestID,
			AttemptNumber:               attempt.LaunchOrdinal,
			AttemptTrigger:              optionalTrimmedStringPointer(attempt.AttemptTrigger),
			AttemptResult:               optionalTrimmedStringPointer(attempt.AttemptResult),
			IsWinner:                    boolPtr(attempt.IsWinner),
			LegacyStatusCode:            nil,
			AttemptDurationMS:           intPtr(attemptDurationMS),
			ProviderCorrelationID:       headerValuePointer(attempt.ResponseHeaders, "x-request-id", "request-id"),
			EndpointBaseURL:             optionalTrimmedStringPointer(attempt.Connection.Endpoint.BaseURL),
			UpstreamStatusCode:          intPtr(attempt.StatusCode),
			IsStream:                    plan.IsStreamingRequest,
			SuccessFlag:                 attempt.StatusCode >= 200 && attempt.StatusCode <= 299,
			ReportCurrencyCode:          reportCurrencyCode,
			ReportCurrencySymbol:        reportCurrencySymbol,
			RequestPath:                 request.URL.Path,
			CreatedAt:                   requestCompletedAt,
			CallerUserAgent:             trimmedStringPointer(request.UserAgent()),
			RowKind:                     requestLogRowKindUpstream,
			URLScrubProvenance:          runtimeURLScrubProvenanceRuntime,
			PricingStatus:               runtimePricingStatusIneligible,
			PricingEvidenceTrust:        runtimePricingEvidenceTrust,
			StreamOutcome:               runtimeStreamOutcomeNotStreaming,
			AuditEnabledAtRequest:       plan.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: plan.AuditCaptureBodiesAtRequest,
		}
		if attempt.StatusCode > 0 {
			requestLog.ErrorSource = stringPtr(errorSourceUpstream)
			requestLog.FailureStage = stringPtr(failureStageUpstreamResponse)
			requestLog.ErrorCode = stringPtr(stableHTTPErrorCode(attempt.StatusCode, ""))
		} else if attempt.Diagnostics != nil {
			requestLog.ErrorSource = stringPtr(attempt.Diagnostics.Source)
			requestLog.FailureStage = stringPtr(attempt.Diagnostics.Stage)
			requestLog.ErrorCode = stringPtr(attempt.Diagnostics.Code)
			requestLog.ErrorDetail = stringPtr(attempt.Diagnostics.Detail)
			requestLog.ErrorDetailRedacted = attempt.Diagnostics.Redacted
			requestLog.ErrorDetailTruncated = attempt.Diagnostics.Truncated
		}
		requestLogs = append(requestLogs, requestLog)
	}
	if len(requestLogs) == 0 {
		requestLog := requestLogInsert{
			ProfileID:                   plan.ProfileID,
			ModelID:                     plan.RequestedModelID,
			ResolvedTargetModelID:       resolvedTargetModelID,
			APIFamily:                   plan.APIFamily,
			OperationName:               strings.TrimSpace(plan.RuntimeOperation.Name),
			IngressRequestID:            ingressRequestID,
			AttemptNumber:               1,
			AttemptTrigger:              stringPtr(attemptTriggerInitial),
			AttemptResult:               stringPtr(attemptResultTransportError),
			IsWinner:                    boolPtr(false),
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
			AuditEnabledAtRequest:       plan.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: plan.AuditCaptureBodiesAtRequest,
		}
		applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)
		requestLogs = append(requestLogs, requestLog)
	}

	usageEvent := usageEventInsert{
		ProfileID:                plan.ProfileID,
		IngressRequestID:         ingressRequestID,
		ModelID:                  plan.RequestedModelID,
		ResolvedTargetModelID:    resolvedTargetModelID,
		APIFamily:                plan.APIFamily,
		OperationName:            strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:               intPtr(result.Connection.Endpoint.ID),
		EndpointLabelSnapshot:    runtimeEndpointLabelSnapshot(result.Connection.Endpoint),
		ConnectionID:             intPtr(result.Connection.ID),
		SelectedTerminalTargetID: selectedTerminalTargetID,
		ProxyAPIKeyIDSnapshot:    proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:  proxyKeyNamePointer(proxyKey),
		StatusCode:               runtimeErr.StatusCode,
		SuccessFlag:              false,
		UnpricedReason:           nil,
		ReportCurrencyCode:       reportCurrencyCode,
		ReportCurrencySymbol:     reportCurrencySymbol,
		AttemptCount:             len(requestLogs),
		RequestPath:              request.URL.Path,
		CreatedAt:                requestCompletedAt,
		ResponseTimeMS:           intPtr(responseTimeMS),
		CompletionDurationMS:     intPtr(responseTimeMS),
		StreamOutcome:            runtimeStreamOutcomeNotStreaming,
		PricingStatus:            runtimePricingStatusIneligible,
		PricingEvidenceTrust:     runtimePricingEvidenceTrust,
		CurrencyAttribution:      runtimeUsageCurrencyAttributionIdentified,
		FinalErrorCode:           stringPtr(safediag.CodeAttemptBudgetExhausted),
		FinalAttemptNumber:       nil,
		FinalAttemptTrigger:      nil,
		IngressStartedAt:         &startedAt,
		IngressCompletedAt:       &requestCompletedAt,
	}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
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
	resolvedTargetModelID := cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	completionDurationMS := intPtr(responseTimeMS)
	requestLog := requestLogInsert{
		ProfileID:                     failure.ProfileID,
		ModelID:                       failure.RequestedModelID,
		ResolvedTargetModelID:         resolvedTargetModelID,
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
		AttemptNumber:                 1,
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
	usageEvent := usageEventInsert{
		ProfileID:                failure.ProfileID,
		IngressRequestID:         ingressRequestID,
		ModelID:                  failure.RequestedModelID,
		ResolvedTargetModelID:    resolvedTargetModelID,
		APIFamily:                failure.APIFamily,
		OperationName:            strings.TrimSpace(failure.RuntimeOperation.Name),
		UpstreamOperationName:    cloneRuntimeStringPointer(failure.UpstreamOperationName),
		OperationTranslationMode: cloneRuntimeStringPointer(failure.OperationTranslationMode),
		EndpointID:               nil,
		EndpointLabelSnapshot:    "Unknown Endpoint",
		ConnectionID:             nil,
		SelectedTerminalTargetID: selectedTerminalTargetID,
		ProxyAPIKeyIDSnapshot:    proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:  proxyKeyNamePointer(proxyKey),
		StatusCode:               runtimeErr.StatusCode,
		SuccessFlag:              false,
		UnpricedReason:           unpricedReason,
		ReportCurrencyCode:       reportCurrencyCode,
		ReportCurrencySymbol:     reportCurrencySymbol,
		AttemptCount:             1,
		RequestPath:              failure.RequestPath,
		UpstreamRequestPath:      cloneRuntimeStringPointer(failure.UpstreamRequestPath),
		CreatedAt:                requestCompletedAt,
		ResponseTimeMS:           intPtr(responseTimeMS),
		CompletionDurationMS:     completionDurationMS,
		TTFTMS:                   nil,
		StreamOutcome:            runtimeStreamOutcomeNotStreaming,
		StreamErrorKind:          nil,
		PricingStatus:            runtimePricingStatusIneligible,
		PricingEvidenceTrust:     runtimePricingEvidenceTrust,
		CurrencyAttribution:      runtimeUsageCurrencyAttributionIdentified,
	}
	routeReason := runtimeExecutionRouteReason(gatewaycore.RouteReasonPolicyReject)
	requestLogs := []requestLogInsert{requestLog}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, routeReason, gatewaycore.UsageSourceMissing),
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
	resolvedTargetModelID := cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	if resolvedTargetModelID == nil {
		resolvedTargetModelID = cloneRuntimeStringPointer(plan.ResolvedTargetModelID)
	}
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	if selectedTerminalTargetID == nil {
		selectedTerminalTargetID = plan.selectedTerminalTargetID()
	}
	routeReason := runtimeExecutionRouteReason(result.RouteReason)
	if routeReason == gatewaycore.RouteReasonDirectMatch {
		routeReason = gatewaycore.RouteReasonPolicyReject
	}
	completionDurationMS := intPtr(responseTimeMS)
	requestLog := requestLogInsert{
		ProfileID:                     plan.ProfileID,
		ModelID:                       plan.RequestedModelID,
		ResolvedTargetModelID:         resolvedTargetModelID,
		APIFamily:                     plan.APIFamily,
		OperationName:                 strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:                    nil,
		ConnectionID:                  nil,
		SelectedTerminalTargetID:      selectedTerminalTargetID,
		ProxyAPIKeyID:                 proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(proxyKey),
		IngressRequestID:              ingressRequestID,
		AttemptNumber:                 1,
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
	usageEvent := usageEventInsert{
		ProfileID:                plan.ProfileID,
		IngressRequestID:         ingressRequestID,
		ModelID:                  plan.RequestedModelID,
		ResolvedTargetModelID:    resolvedTargetModelID,
		APIFamily:                plan.APIFamily,
		OperationName:            strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:               nil,
		EndpointLabelSnapshot:    "Unknown Endpoint",
		ConnectionID:             nil,
		SelectedTerminalTargetID: selectedTerminalTargetID,
		ProxyAPIKeyIDSnapshot:    proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:  proxyKeyNamePointer(proxyKey),
		StatusCode:               runtimeErr.StatusCode,
		SuccessFlag:              false,
		UnpricedReason:           unpricedReason,
		ReportCurrencyCode:       reportCurrencyCode,
		ReportCurrencySymbol:     reportCurrencySymbol,
		AttemptCount:             1,
		RequestPath:              request.URL.Path,
		CreatedAt:                requestCompletedAt,
		ResponseTimeMS:           intPtr(responseTimeMS),
		CompletionDurationMS:     completionDurationMS,
		StreamOutcome:            runtimeStreamOutcomeNotStreaming,
		PricingStatus:            runtimePricingStatusIneligible,
		PricingEvidenceTrust:     runtimePricingEvidenceTrust,
		CurrencyAttribution:      runtimeUsageCurrencyAttributionIdentified,
	}
	requestLogs := []requestLogInsert{requestLog}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

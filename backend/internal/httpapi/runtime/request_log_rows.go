package runtime

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

func buildRuntimeRequestLogRows(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext) []requestLogInsert {
	requestLogs := make([]requestLogInsert, 0, len(telemetry.attempts))
	for index := range telemetry.attempts {
		requestLogs = append(requestLogs, buildRuntimeRequestLogRow(plan, request, telemetry, telemetry.attemptContext(index)))
	}
	return requestLogs
}

func buildRuntimeRequestLogRow(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext) requestLogInsert {
	generationSnapshot := telemetry.requestGenerationSnapshot.clone()
	// A materialized attempt carries its own snapshot extracted from its final
	// effective upstream body. The only exception is the no-configuration
	// streaming request-body observer path, where the plan-level snapshot is
	// the source of truth because the probe attempt never materialized a body.
	if attempt.attempt.RequestGenerationParams != nil && plan.RequestGenerationSnapshot == nil {
		generationSnapshot = attempt.attempt.RequestGenerationParams.clone()
	}
	requestLog := requestLogInsert{
		ProfileID:                     plan.ProfileID,
		ModelID:                       plan.RequestedModelID,
		ResolvedTargetModelID:         resolvedTargetModelIDForAttempt(plan, attempt.attempt),
		APIFamily:                     plan.APIFamily,
		OperationName:                 telemetry.operationName,
		UpstreamOperationName:         trimmedStringPointer(attempt.attempt.UpstreamOperationName),
		OperationTranslationMode:      runtimeTranslationModePointer(attempt.attempt.OperationTranslationMode),
		EndpointID:                    intPtr(attempt.attempt.Connection.Endpoint.ID),
		ConnectionID:                  intPtr(attempt.attempt.Connection.ID),
		SelectedTerminalTargetID:      plan.selectedTerminalTargetID(),
		ProxyAPIKeyID:                 proxyKeyIDPointer(telemetry.proxyKey),
		ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(telemetry.proxyKey),
		IngressRequestID:              telemetry.ingressRequestID,
		AttemptNumber:                 attempt.attemptNumber,
		ProviderCorrelationID:         headerValuePointer(attempt.attempt.ResponseHeaders, "x-request-id", "request-id"),
		EndpointBaseURL:               stringPointerIfNotEmpty(attempt.attempt.Connection.Endpoint.BaseURL),
		EndpointDescription:           attempt.attempt.Connection.Endpoint.Name,
		StatusCode:                    attempt.attempt.StatusCode,
		ResponseTimeMS:                attempt.responseTimeMS,
		IsStream:                      telemetry.isStream,
		SuccessFlag:                   attempt.success,
		UnpricedReason:                attempt.unpricedReason,
		ReportCurrencyCode:            telemetry.reportCurrencyCode,
		ReportCurrencySymbol:          telemetry.reportCurrencySymbol,
		RequestPath:                   request.URL.Path,
		UpstreamRequestPath:           trimmedStringPointer(attempt.attempt.UpstreamRequestPath),
		CreatedAt:                     attempt.createdAt,
		CallerUserAgent:               telemetry.callerUserAgent,
		UpstreamUserAgent:             headerMapValuePointer(attempt.attempt.RequestHeaders, "User-Agent"),
		CompletionDurationMS:          nil,
		TTFTMS:                        nil,
		StreamOutcome:                 runtimeStreamOutcomeNotStreaming,
		AuditEnabledAtRequest:         attempt.attempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:   attempt.attempt.AuditCaptureBodiesAtRequest,
		RequestGenerationParams:       generationSnapshot.Params,
		RequestGenerationParamsStatus: trimmedStringPointer(generationSnapshot.Status),
		RowKind:                       requestLogRowKindUpstream,
		URLScrubProvenance:            runtimeURLScrubProvenanceRuntime,
		AttemptTrigger:                optionalTrimmedStringPointer(attempt.attempt.AttemptTrigger),
		AttemptResult:                 optionalTrimmedStringPointer(attempt.attempt.AttemptResult),
		IsWinner:                      boolPtr(attempt.isWinner),
		AttemptDurationMS:             nonNegativeIntPointer(attempt.attempt.AttemptDurationMS),
		UpstreamRequestStarted:        boolPtr(attempt.attempt.UpstreamRequestStarted),
		ResponseHeadersReceived:       boolPtr(attempt.attempt.ResponseHeadersReceived),
	}
	requestLog.applyRuntimePricingResult(buildRuntimePricingProvenance(plan.ReportCurrencySnapshot, attempt.attempt.Connection.PricingTemplateSnapshot))
	if attempt.attempt.ResponseHeadersReceived {
		requestLog.UpstreamStatusCode = intPtr(attempt.attempt.StatusCode)
	} else {
		requestLog.UpstreamStatusCode = nil
	}
	applyRuntimeRequestRowFailureFields(&requestLog, telemetry, attempt)
	applyRuntimeRequestRowMetadataScrub(&requestLog, telemetry, request)
	if attempt.isFinal {
		applyRuntimeFinalAttemptTelemetry(&requestLog, telemetry, attempt)
	}
	return requestLog
}

// applyRuntimeRequestRowFailureFields derives the scoped failure projection
// for one upstream row (Requests SPEC §3.2-§3.4/§4.2/§4.5). Success rows keep
// failure fields empty; transport rows keep upstream status null; cancelled
// Hedge losers carry a typed neutral cancellation without failure detail.
// Every new failed row is ineligible for pricing (Pricing SPEC §3.4: non-2xx
// is never priced/unpriced).
func applyRuntimeRequestRowFailureFields(requestLog *requestLogInsert, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext) {
	attemptFacts := attempt.attempt
	requestLog.PricingStatus = runtimePricingStatusIneligible
	requestLog.PricingEvidenceTrust = runtimePricingEvidenceTrust
	switch {
	case attemptFacts.AttemptResult == attemptResultCancelled:
		// Expected Hedge-loser arbitration: typed neutral cancellation, no
		// failure detail, no HTTP status.
		requestLog.AttemptResult = stringPtr(attemptResultCancelled)
		requestLog.UpstreamStatusCode = nil
		return
	case attemptFacts.AttemptResult == attemptResultTransportError || (attemptFacts.Diagnostics != nil && attemptFacts.Diagnostics.Source == errorSourceTransport):
		diagnostic := attemptFacts.diagnosticsOrFallback(http.StatusBadGateway)
		if attemptFacts.Diagnostics != nil {
			diagnostic = *attemptFacts.Diagnostics
		}
		requestLog.AttemptResult = stringPtr(attemptResultTransportError)
		requestLog.UpstreamStatusCode = nil
		requestLog.ErrorSource = stringPtr(diagnostic.Source)
		requestLog.FailureStage = stringPtr(diagnostic.Stage)
		requestLog.ErrorCode = stringPtr(diagnostic.Code)
		if diagnostic.Detail != "" {
			requestLog.ErrorDetail = stringPtr(diagnostic.Detail)
		}
		requestLog.ErrorDetailRedacted = diagnostic.Redacted
		requestLog.ErrorDetailTruncated = diagnostic.Truncated
		return
	}

	// Non-transport rows: complete the attempt result from response evidence
	// and stream outcome.
	isWinner := attempt.isWinner
	streamOutcome := telemetry.streamOutcome
	streamKind := telemetry.streamErrorKind
	streamDetail := telemetry.streamErrorDetail
	if !isWinner {
		// Intermediate attempts never carry stream outcome evidence; the
		// response status alone classifies them.
		streamOutcome = runtimeStreamOutcomeNotStreaming
		streamKind = nil
		streamDetail = nil
	}
	if attemptFacts.StatusCode >= 200 && attemptFacts.StatusCode <= 299 {
		switch streamOutcome {
		case runtimeStreamOutcomeCompleted, runtimeStreamOutcomeNotStreaming:
			if attemptFacts.AttemptResult == "" {
				requestLog.AttemptResult = stringPtr(attemptResultCompleted)
			}
			return
		case runtimeStreamOutcomeClientDisconnected:
			diagnostic := safeStreamDiagnostic(errorSourceClient, failureStageStream, dereferenceString(streamKind), streamOutcome, dereferenceString(streamDetail))
			requestLog.AttemptResult = stringPtr(attemptResultClientDisconnected)
			requestLog.ErrorSource = stringPtr(diagnostic.Source)
			requestLog.FailureStage = stringPtr(diagnostic.Stage)
			requestLog.ErrorCode = stringPtr(diagnostic.Code)
			requestLog.StreamErrorDetail = optionalTrimmedStringPointer(diagnostic.Detail)
			requestLog.StreamErrorDetailRedacted = diagnostic.Redacted
			requestLog.StreamErrorDetailTruncated = diagnostic.Truncated
			return
		default:
			// provider_incomplete / upstream_read_error /
			// upstream_ended_without_terminal / unknown -> stream_error.
			diagnostic := safeStreamDiagnostic(errorSourceUpstream, failureStageStream, dereferenceString(streamKind), streamOutcome, dereferenceString(streamDetail))
			requestLog.AttemptResult = stringPtr(attemptResultStreamError)
			requestLog.ErrorSource = stringPtr(diagnostic.Source)
			requestLog.FailureStage = stringPtr(diagnostic.Stage)
			requestLog.ErrorCode = stringPtr(diagnostic.Code)
			requestLog.StreamErrorDetail = optionalTrimmedStringPointer(diagnostic.Detail)
			requestLog.StreamErrorDetailRedacted = diagnostic.Redacted
			requestLog.StreamErrorDetailTruncated = diagnostic.Truncated
			return
		}
	}

	// Upstream HTTP failure (non-2xx). The sampler diagnostic wins when
	// completed; otherwise a generic status fallback is used (the sample never
	// blocks sealing).
	diagnostic := attemptFacts.diagnosticsOrFallback(attemptFacts.StatusCode)
	if attemptFacts.Diagnostics != nil {
		diagnostic = *attemptFacts.Diagnostics
	}
	requestLog.AttemptResult = stringPtr(attemptResultHTTPError)
	requestLog.ErrorSource = stringPtr(errorSourceUpstream)
	requestLog.FailureStage = stringPtr(failureStageUpstreamResponse)
	requestLog.ErrorCode = stringPtr(stableHTTPErrorCode(attemptFacts.StatusCode, diagnostic.Code))
	if strings.TrimSpace(diagnostic.Detail) == "" {
		diagnostic.Detail = fmt.Sprintf("upstream request returned HTTP %d", attemptFacts.StatusCode)
	}
	scrubbedDetail := safediag.ScrubValue(diagnostic.Detail, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
	requestLog.ErrorDetail = stringPtr(scrubbedDetail.Value)
	diagnostic.Redacted = diagnostic.Redacted || scrubbedDetail.Redacted
	diagnostic.Truncated = diagnostic.Truncated || scrubbedDetail.Truncated
	requestLog.ErrorDetailRedacted = diagnostic.Redacted
	requestLog.ErrorDetailTruncated = diagnostic.Truncated
}

// applyRuntimeRequestRowMetadataScrub applies the fixed-bottom-line value
// scrubber and per-field caps to externally controlled metadata strings and
// records redacted/truncated provenance (Requests SPEC §4.3).
func applyRuntimeRequestRowMetadataScrub(requestLog *requestLogInsert, telemetry runtimeTelemetryEnvelopeContext, request *http.Request) {
	var provenance safediag.MetadataProvenance

	if telemetry.callerRequestID != nil && strings.TrimSpace(*telemetry.callerRequestID) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerRequestID, *telemetry.callerRequestID, 255)
		requestLog.CallerRequestID = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerRequestID, scrubbed.Redacted, scrubbed.Truncated)
	}
	if telemetry.callerUserAgent != nil && strings.TrimSpace(*telemetry.callerUserAgent) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerUserAgent, *telemetry.callerUserAgent, 0)
		requestLog.CallerUserAgent = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerUserAgent, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.UpstreamUserAgent != nil && strings.TrimSpace(*requestLog.UpstreamUserAgent) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldUpstreamUserAgent, *requestLog.UpstreamUserAgent, 0)
		requestLog.UpstreamUserAgent = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldUpstreamUserAgent, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.ProviderCorrelationID != nil && strings.TrimSpace(*requestLog.ProviderCorrelationID) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldProviderCorrelationID, *requestLog.ProviderCorrelationID, 255)
		requestLog.ProviderCorrelationID = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldProviderCorrelationID, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.EndpointBaseURL != nil {
		scrubbed, truncated := safediag.ScrubEndpointBaseURL(*requestLog.EndpointBaseURL)
		requestLog.EndpointBaseURL = optionalTrimmedStringPointer(scrubbed)
		if truncated {
			provenance.Record(safediag.MetadataFieldEndpointBaseURL, false, true)
		}
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

func resolvedTargetModelIDForAttempt(plan requestPlan, attempt executionAttempt) *string {
	if trimmed := strings.TrimSpace(attempt.ResolvedTargetModelID); trimmed != "" {
		return &trimmed
	}
	return plan.ResolvedTargetModelID
}

func applyRuntimeFinalAttemptTelemetry(requestLog *requestLogInsert, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext) {
	requestLog.InputTokens = telemetry.usage.InputTokens
	requestLog.OutputTokens = telemetry.usage.OutputTokens
	requestLog.TotalTokens = telemetry.usage.TotalTokens
	requestLog.CacheReadInputTokens = telemetry.usage.CacheReadInputTokens
	requestLog.CacheCreationInputTokens = telemetry.usage.CacheCreationInputTokens
	requestLog.ReasoningTokens = telemetry.usage.ReasoningTokens
	requestLog.CompletionDurationMS = telemetry.completionDurationMS
	requestLog.TTFTMS = telemetry.ttftMS
	requestLog.StreamOutcome = telemetry.streamOutcome
	requestLog.StreamErrorKind = telemetry.streamErrorKind
	requestLog.StreamErrorDetail = telemetry.streamErrorDetail
	if attempt.success {
		requestLog.applyRuntimePricingResult(telemetry.pricingResult)
	}
}

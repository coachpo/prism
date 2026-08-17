package runtime

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

type runtimeTelemetryPricingTimingContext struct {
	requestCompletedAt   time.Time
	responseTimeMS       int
	usage                responseUsage
	streamOutcome        string
	isStream             bool
	ttftMS               *int
	completionDurationMS *int
	successFlag          bool
	reportCurrencyCode   *string
	reportCurrencySymbol *string
	operationName        string
	pricingResult        runtimePricingResult
	usageSource          gatewaycore.UsageSource
	streamErrorKind      *string
	streamErrorDetail    *string
}

type runtimeTelemetryEnvelopeContext struct {
	runtimeTelemetryPricingTimingContext
	ingressRequestID          string
	ingressStartedAt          time.Time
	callerRequestID           *string
	proxyKey                  *requestcontext.RuntimeProxyKeySnapshot
	callerUserAgent           *string
	requestGenerationSnapshot requestGenerationParamsSnapshot
	attempts                  []executionAttempt
	winnerOrdinal             int
	routeReason               gatewaycore.RouteReason
	capturedRequestBody       *string
	capturedResponseBody      *string
	capturedResponseObserved  int64
	capturedResponseStored    int64
	capturedResponseTruncated bool
}

type runtimeTelemetryAttemptContext struct {
	attempt        executionAttempt
	attemptNumber  int
	isFinal        bool
	isWinner       bool
	success        bool
	unpricedReason *string
	createdAt      time.Time
	responseTimeMS int
}

func (s *Service) buildRuntimeTelemetryEnvelopeContext(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelopeContext {
	pricingTiming := s.buildRuntimeTelemetryPricingTimingContext(plan, result, startedAt, responseCapture)
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	callerRequestID := strings.TrimSpace(runtimeCallerRequestIDFromContext(request.Context()))
	return runtimeTelemetryEnvelopeContext{
		runtimeTelemetryPricingTimingContext: pricingTiming,
		ingressRequestID:                     ingressRequestID,
		ingressStartedAt:                     startedAt.UTC(),
		callerRequestID:                      optionalTrimmedStringPointer(callerRequestID),
		proxyKey:                             proxyKey,
		callerUserAgent:                      trimmedStringPointer(request.UserAgent()),
		requestGenerationSnapshot:            plan.RequestGenerationParamsSnapshot(),
		attempts:                             runtimeTelemetryAttempts(plan, result, request, pricingTiming),
		winnerOrdinal:                        result.WinnerOrdinal,
		routeReason:                          runtimeExecutionRouteReason(result.RouteReason),
		capturedResponseBody:                 runtimeCapturedAuditResponseBodyForOperation(plan.RuntimeOperation, result.AuditEnabledAtRequest && result.AuditCaptureBodiesAtRequest, responseCapture.AuditBody, responseCapture.StreamOutcome),
		capturedResponseObserved:             responseCapture.AuditBodyObserved,
		capturedResponseStored:               responseCapture.AuditBodyStored,
		capturedResponseTruncated:            responseCapture.AuditBodyTruncated,
	}
}

func (s *Service) buildRuntimeTelemetryPricingTimingContext(plan requestPlan, result executionResult, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryPricingTimingContext {
	requestCompletedAt := s.nowUTC()
	if responseCapture.CompletedAt != nil && !responseCapture.CompletedAt.IsZero() {
		requestCompletedAt = responseCapture.CompletedAt.UTC()
	}
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	usage := responseCapture.extractedUsage()
	streamOutcome := runtimeStreamOutcomeForTelemetry(responseCapture.StreamOutcome)
	warnOnNormalizationRejectedUsage(plan, responseCapture, usage, streamOutcome)
	isStream := runtimeStreamOutcomeIsStreaming(streamOutcome)
	ttftMS, completionDurationMS := runtimeResponseTiming(startedAt, requestCompletedAt, isStream, responseCapture)
	successFlag := result.Response.StatusCode >= 200 && result.Response.StatusCode <= 299
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	pricingResult := buildRuntimePricingProvenance(plan.ReportCurrencySnapshot, result.Connection.PricingTemplateSnapshot)
	if successFlag {
		pricingResult = buildRuntimePricingResult(plan.ReportCurrencySnapshot, result.Connection.PricingTemplateSnapshot, result.Connection.EndpointFXSnapshot, usage, streamOutcome)
		pricingResult = withRuntimePricingSnapshotForPersistence(pricingResult, result.Connection.PricingTemplateSnapshot)
		pricingResult = enforceRuntimeSpendCoherence(successFlag, pricingResult)
	}
	return runtimeTelemetryPricingTimingContext{
		requestCompletedAt:   requestCompletedAt,
		responseTimeMS:       responseTimeMS,
		usage:                usage,
		streamOutcome:        streamOutcome,
		isStream:             isStream,
		ttftMS:               ttftMS,
		completionDurationMS: completionDurationMS,
		successFlag:          successFlag,
		reportCurrencyCode:   reportCurrencyCode,
		reportCurrencySymbol: reportCurrencySymbol,
		operationName:        strings.TrimSpace(plan.RuntimeOperation.Name),
		pricingResult:        pricingResult,
		usageSource:          runtimeUsageSourceFromCapture(responseCapture, usage, streamOutcome),
		streamErrorKind:      responseCapture.StreamErrorKind,
		streamErrorDetail:    responseCapture.StreamErrorDetail,
	}
}

func warnOnNormalizationRejectedUsage(plan requestPlan, responseCapture runtimeResponseCapture, usage responseUsage, streamOutcome string) {
	usageSource := runtimeUsageSourceFromCapture(responseCapture, usage, streamOutcome)
	if usageSource != gatewaycore.UsageSourceNormalizationRejected {
		return
	}
	slog.Warn("runtime usage normalization rejected upstream payload",
		"operation_name", strings.TrimSpace(plan.RuntimeOperation.Name),
		"api_family", strings.TrimSpace(plan.APIFamily),
		"stream_outcome", streamOutcome)
}

func runtimeTelemetryAttempts(plan requestPlan, result executionResult, request *http.Request, pricingTiming runtimeTelemetryPricingTimingContext) []executionAttempt {
	if len(result.Attempts) > 0 {
		return result.Attempts
	}
	selectedAttempt := firstTerminalAttempt(plan)
	attempt := executionAttempt{
		Connection:                  result.Connection,
		ResolvedTargetModelID:       dereferenceString(result.ResolvedTargetModelID),
		RequestURL:                  request.URL.String(),
		RequestHeaders:              result.RequestHeaders,
		ResponseHeaders:             result.Response.Header.Clone(),
		StatusCode:                  result.Response.StatusCode,
		ResponseTimeMS:              pricingTiming.responseTimeMS,
		CompletedAt:                 pricingTiming.requestCompletedAt,
		AuditEnabledAtRequest:       result.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: result.AuditCaptureBodiesAtRequest,
		UpstreamOperationName:       runtimeUpstreamOperationName(plan.RuntimeOperation, selectedAttempt.TranslationMode),
		UpstreamRequestPath:         dereferenceString(runtimeUpstreamRequestPath(plan.RuntimeOperation, selectedAttempt.TranslationMode, plan.EffectiveRequestPath)),
		OperationTranslationMode:    normalizedRuntimeTranslationMode(selectedAttempt.TranslationMode),
		RequestGenerationParams:     selectedAttempt.RequestGenerationParams.clonePointer(),
		LaunchOrdinal:               1,
		AttemptTrigger:              attemptTriggerInitial,
		UpstreamRequestStarted:      true,
		ResponseHeadersReceived:     result.Response != nil,
	}
	return []executionAttempt{attempt}
}

func firstTerminalAttempt(plan requestPlan) runtimeTerminalAttempt {
	attempts := plan.orderedTerminalAttempts()
	if len(attempts) == 0 {
		return runtimeTerminalAttempt{}
	}
	return attempts[0]
}

func (telemetry runtimeTelemetryEnvelopeContext) attemptContext(index int) runtimeTelemetryAttemptContext {
	attempt := telemetry.attempts[index]
	isFinal := index == len(telemetry.attempts)-1
	isWinner := telemetry.winnerOrdinal > 0 && attempt.LaunchOrdinal == telemetry.winnerOrdinal
	if attempt.LaunchOrdinal <= 0 {
		isWinner = isFinal
	}
	attemptSuccess := attempt.StatusCode >= 200 && attempt.StatusCode <= 299
	attemptUnpricedReason := billingStateUnpricedOnly(attemptSuccess)
	attemptCreatedAt := attempt.CompletedAt
	if attemptCreatedAt.IsZero() || isFinal {
		attemptCreatedAt = telemetry.requestCompletedAt
	}
	attemptResponseTimeMS := attempt.ResponseTimeMS
	if attemptResponseTimeMS < 1 || isFinal {
		attemptResponseTimeMS = telemetry.responseTimeMS
	}
	return runtimeTelemetryAttemptContext{
		attempt:        attempt,
		attemptNumber:  attempt.LaunchOrdinal,
		isFinal:        isFinal,
		isWinner:       isWinner,
		success:        attemptSuccess,
		unpricedReason: attemptUnpricedReason,
		createdAt:      attemptCreatedAt.UTC(),
		responseTimeMS: attemptResponseTimeMS,
	}
}

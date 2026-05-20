package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/proxykeyusage"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

type responseUsage struct {
	InputTokens              *int
	OutputTokens             *int
	TotalTokens              *int
	CacheReadInputTokens     *int
	CacheCreationInputTokens *int
	ReasoningTokens          *int
}

func (usage responseUsage) hasValues() bool {
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil || usage.ReasoningTokens != nil
}

func (usage responseUsage) normalized() responseUsage {
	if usage.TotalTokens == nil && (usage.InputTokens != nil || usage.OutputTokens != nil) {
		total := intValue(usage.InputTokens) + intValue(usage.OutputTokens)
		usage.TotalTokens = &total
	}
	return usage
}

func (usage *responseUsage) mergeStandardUsagePayload(usagePayload map[string]any) {
	if inputTokens := intPointerFromAny(firstValue(usagePayload, "prompt_tokens", "input_tokens")); inputTokens != nil {
		usage.InputTokens = inputTokens
	}
	if outputTokens := intPointerFromAny(firstValue(usagePayload, "completion_tokens", "output_tokens")); outputTokens != nil {
		usage.OutputTokens = outputTokens
	}
	if totalTokens := intPointerFromAny(usagePayload["total_tokens"]); totalTokens != nil {
		usage.TotalTokens = totalTokens
	}
	if cacheReadTokens := intPointerFromAny(usagePayload["cache_read_input_tokens"]); cacheReadTokens != nil {
		usage.CacheReadInputTokens = cacheReadTokens
	}
	if cacheCreationTokens := intPointerFromAny(usagePayload["cache_creation_input_tokens"]); cacheCreationTokens != nil {
		usage.CacheCreationInputTokens = cacheCreationTokens
	}
	if reasoningTokens := intPointerFromAny(firstValue(
		map[string]any{
			"completion": nestedValue(usagePayload, "completion_tokens_details", "reasoning_tokens"),
			"output":     nestedValue(usagePayload, "output_tokens_details", "reasoning_tokens"),
		},
		"completion",
		"output",
	)); reasoningTokens != nil {
		usage.ReasoningTokens = reasoningTokens
	}
}

func (usage *responseUsage) mergeGeminiUsagePayload(usagePayload map[string]any) {
	if inputTokens := intPointerFromAny(usagePayload["promptTokenCount"]); inputTokens != nil {
		usage.InputTokens = inputTokens
	}
	if outputTokens := intPointerFromAny(usagePayload["candidatesTokenCount"]); outputTokens != nil {
		usage.OutputTokens = outputTokens
	}
	if totalTokens := intPointerFromAny(usagePayload["totalTokenCount"]); totalTokens != nil {
		usage.TotalTokens = totalTokens
	}
	if cacheReadTokens := intPointerFromAny(usagePayload["cachedContentTokenCount"]); cacheReadTokens != nil {
		usage.CacheReadInputTokens = cacheReadTokens
	}
}

func buildUsageBodyFromResponseUsage(usage responseUsage) []byte {
	usage = usage.normalized()
	payload := map[string]any{}
	if usage.InputTokens != nil {
		payload["input_tokens"] = *usage.InputTokens
	}
	if usage.OutputTokens != nil {
		payload["output_tokens"] = *usage.OutputTokens
	}
	totalTokens := usage.TotalTokens
	if totalTokens != nil {
		payload["total_tokens"] = *totalTokens
	}
	if usage.CacheReadInputTokens != nil {
		payload["cache_read_input_tokens"] = *usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens != nil {
		payload["cache_creation_input_tokens"] = *usage.CacheCreationInputTokens
	}
	if usage.ReasoningTokens != nil {
		payload["output_tokens_details"] = map[string]any{"reasoning_tokens": *usage.ReasoningTokens}
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{"usage": payload})
	if err != nil {
		return nil
	}
	return body
}

type requestLogInsert struct {
	ProfileID                         int
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	VendorID                          *int
	VendorKey                         *string
	VendorName                        *string
	EndpointID                        int
	ConnectionID                      int
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
	IngressRequestID                  string
	AttemptNumber                     int
	ProviderCorrelationID             *string
	EndpointBaseURL                   string
	EndpointDescription               *string
	StatusCode                        int
	ResponseTimeMS                    int
	IsStream                          bool
	InputTokens                       *int
	OutputTokens                      *int
	TotalTokens                       *int
	SuccessFlag                       bool
	BillableFlag                      *bool
	PricedFlag                        *bool
	UnpricedReason                    *string
	CacheReadInputTokens              *int
	CacheCreationInputTokens          *int
	ReasoningTokens                   *int
	InputCostMicros                   *int64
	OutputCostMicros                  *int64
	CacheReadInputCostMicros          *int64
	CacheCreationInputCostMicros      *int64
	ReasoningCostMicros               *int64
	TotalCostOriginalMicros           *int64
	TotalCostUserCurrencyMicros       *int64
	CurrencyCodeOriginal              *string
	ReportCurrencyCode                *string
	ReportCurrencySymbol              *string
	FXRateUsed                        *string
	FXRateSource                      *string
	PricingSnapshotUnit               *string
	PricingSnapshotInput              *string
	PricingSnapshotOutput             *string
	PricingSnapshotCacheReadInput     *string
	PricingSnapshotCacheCreationInput *string
	PricingSnapshotReasoning          *string
	PricingConfigVersionUsed          *int
	RequestPath                       string
	ErrorDetail                       *string
	CreatedAt                         time.Time
	CallerUserAgent                   *string
	UpstreamUserAgent                 *string
	CompletionDurationMS              *int
	TTFTMS                            *int
	StreamOutcome                     string
	StreamErrorKind                   *string
	StreamErrorDetail                 *string
	AuditEnabledAtRequest             bool
	AuditCaptureBodiesAtRequest       bool
	RequestGenerationParams           *requestGenerationParams
	RequestGenerationParamsStatus     *string
}

type usageEventInsert struct {
	ProfileID                         int
	IngressRequestID                  string
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	EndpointID                        int
	ConnectionID                      int
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
	StatusCode                        int
	SuccessFlag                       bool
	BillableFlag                      *bool
	PricedFlag                        *bool
	UnpricedReason                    *string
	InputTokens                       *int
	OutputTokens                      *int
	TotalTokens                       *int
	CacheReadInputTokens              *int
	CacheCreationInputTokens          *int
	ReasoningTokens                   *int
	InputCostMicros                   *int64
	OutputCostMicros                  *int64
	CacheReadInputCostMicros          *int64
	CacheCreationInputCostMicros      *int64
	ReasoningCostMicros               *int64
	TotalCostOriginalMicros           *int64
	TotalCostUserCurrencyMicros       *int64
	CurrencyCodeOriginal              *string
	ReportCurrencyCode                *string
	ReportCurrencySymbol              *string
	FXRateUsed                        *string
	FXRateSource                      *string
	PricingSnapshotUnit               *string
	PricingSnapshotInput              *string
	PricingSnapshotOutput             *string
	PricingSnapshotCacheReadInput     *string
	PricingSnapshotCacheCreationInput *string
	PricingSnapshotReasoning          *string
	PricingConfigVersionUsed          *int
	AttemptCount                      int
	RequestPath                       string
	CreatedAt                         time.Time
	ResponseTimeMS                    *int
	CompletionDurationMS              *int
	TTFTMS                            *int
	StreamOutcome                     string
	StreamErrorKind                   *string
}

func (requestLog *requestLogInsert) applyRuntimePricingResult(pricingResult runtimePricingResult) {
	requestLog.BillableFlag = boolPtr(pricingResult.Billable)
	requestLog.PricedFlag = boolPtr(pricingResult.Priced)
	requestLog.UnpricedReason = pricingResult.UnpricedReason
	requestLog.InputCostMicros = pricingResult.InputCostMicros
	requestLog.OutputCostMicros = pricingResult.OutputCostMicros
	requestLog.CacheReadInputCostMicros = pricingResult.CacheReadInputCostMicros
	requestLog.CacheCreationInputCostMicros = pricingResult.CacheCreationInputCostMicros
	requestLog.ReasoningCostMicros = pricingResult.ReasoningCostMicros
	requestLog.TotalCostOriginalMicros = pricingResult.TotalCostOriginalMicros
	requestLog.TotalCostUserCurrencyMicros = pricingResult.TotalCostUserCurrencyMicros
	requestLog.CurrencyCodeOriginal = pricingResult.CurrencyCodeOriginal
	requestLog.ReportCurrencyCode = pricingResult.ReportCurrencyCode
	requestLog.ReportCurrencySymbol = pricingResult.ReportCurrencySymbol
	requestLog.FXRateUsed = pricingResult.FXRateUsed
	requestLog.FXRateSource = pricingResult.FXRateSource
	requestLog.PricingSnapshotUnit = pricingResult.PricingSnapshotUnit
	requestLog.PricingSnapshotInput = pricingResult.PricingSnapshotInput
	requestLog.PricingSnapshotOutput = pricingResult.PricingSnapshotOutput
	requestLog.PricingSnapshotCacheReadInput = pricingResult.PricingSnapshotCacheReadInput
	requestLog.PricingSnapshotCacheCreationInput = pricingResult.PricingSnapshotCacheCreationInput
	requestLog.PricingSnapshotReasoning = pricingResult.PricingSnapshotReasoning
	requestLog.PricingConfigVersionUsed = pricingResult.PricingConfigVersionUsed
}

func (usageEvent *usageEventInsert) applyRuntimePricingResult(pricingResult runtimePricingResult) {
	usageEvent.BillableFlag = boolPtr(pricingResult.Billable)
	usageEvent.PricedFlag = boolPtr(pricingResult.Priced)
	usageEvent.UnpricedReason = pricingResult.UnpricedReason
	usageEvent.InputCostMicros = pricingResult.InputCostMicros
	usageEvent.OutputCostMicros = pricingResult.OutputCostMicros
	usageEvent.CacheReadInputCostMicros = pricingResult.CacheReadInputCostMicros
	usageEvent.CacheCreationInputCostMicros = pricingResult.CacheCreationInputCostMicros
	usageEvent.ReasoningCostMicros = pricingResult.ReasoningCostMicros
	usageEvent.TotalCostOriginalMicros = pricingResult.TotalCostOriginalMicros
	usageEvent.TotalCostUserCurrencyMicros = pricingResult.TotalCostUserCurrencyMicros
	usageEvent.CurrencyCodeOriginal = pricingResult.CurrencyCodeOriginal
	usageEvent.ReportCurrencyCode = pricingResult.ReportCurrencyCode
	usageEvent.ReportCurrencySymbol = pricingResult.ReportCurrencySymbol
	usageEvent.FXRateUsed = pricingResult.FXRateUsed
	usageEvent.FXRateSource = pricingResult.FXRateSource
	usageEvent.PricingSnapshotUnit = pricingResult.PricingSnapshotUnit
	usageEvent.PricingSnapshotInput = pricingResult.PricingSnapshotInput
	usageEvent.PricingSnapshotOutput = pricingResult.PricingSnapshotOutput
	usageEvent.PricingSnapshotCacheReadInput = pricingResult.PricingSnapshotCacheReadInput
	usageEvent.PricingSnapshotCacheCreationInput = pricingResult.PricingSnapshotCacheCreationInput
	usageEvent.PricingSnapshotReasoning = pricingResult.PricingSnapshotReasoning
	usageEvent.PricingConfigVersionUsed = pricingResult.PricingConfigVersionUsed
}

type auditLogInsert struct {
	RequestLogAttemptNumber     int       `json:"request_log_attempt_number"`
	ProfileID                   int       `json:"profile_id"`
	VendorID                    *int      `json:"vendor_id,omitempty"`
	ModelID                     string    `json:"model_id"`
	EndpointID                  int       `json:"endpoint_id"`
	ConnectionID                int       `json:"connection_id"`
	EndpointBaseURL             string    `json:"endpoint_base_url"`
	EndpointDescription         *string   `json:"endpoint_description,omitempty"`
	RequestMethod               string    `json:"request_method"`
	RequestURL                  string    `json:"request_url"`
	RequestHeaders              string    `json:"request_headers"`
	RequestBody                 *string   `json:"request_body,omitempty"`
	RequestBodyStored           bool      `json:"request_body_stored"`
	ResponseStatus              int       `json:"response_status"`
	ResponseHeaders             *string   `json:"response_headers,omitempty"`
	ResponseBody                *string   `json:"response_body,omitempty"`
	ResponseBodyStored          bool      `json:"response_body_stored"`
	IsStream                    bool      `json:"is_stream"`
	DurationMS                  int       `json:"duration_ms"`
	CreatedAt                   time.Time `json:"created_at"`
	AuditEnabledAtRequest       bool      `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest bool      `json:"audit_capture_bodies_at_request"`
}

type runtimeProxyKeyUsageSignal struct {
	KeyID      int       `json:"key_id"`
	LastUsedAt time.Time `json:"last_used_at"`
	LastUsedIP string    `json:"last_used_ip,omitempty"`
}

type runtimeTelemetryEnvelope struct {
	RequestLogs   []requestLogInsert          `json:"request_logs"`
	AuditLogs     []auditLogInsert            `json:"audit_logs,omitempty"`
	UsageEvent    usageEventInsert            `json:"usage_event"`
	ProxyKeyUsage *runtimeProxyKeyUsageSignal `json:"proxy_key_usage,omitempty"`
}

func (s *Service) recordRuntimeActivity(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	envelope := s.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, responseCapture)
	if submit := s.runtimeSideEffects.SubmitRuntimeActivity(RuntimeActivityIntent{Envelope: envelope}); submit.Status != RuntimeSideEffectAccepted {
		slog.Error("failed to accept runtime activity telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func (s *Service) buildRuntimeTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	if responseCapture.CompletedAt != nil && !responseCapture.CompletedAt.IsZero() {
		requestCompletedAt = responseCapture.CompletedAt.UTC()
	}
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	usage := responseCapture.extractedUsage()
	streamOutcome := runtimeStreamOutcomeForTelemetry(responseCapture.StreamOutcome)
	isStream := runtimeStreamOutcomeIsStreaming(streamOutcome)
	ttftMS, completionDurationMS := runtimeResponseTiming(startedAt, requestCompletedAt, isStream, responseCapture)
	successFlag := result.Response.StatusCode >= 200 && result.Response.StatusCode <= 299
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	pricingResult := runtimePricingResult{
		ReportCurrencyCode:   reportCurrencyCode,
		ReportCurrencySymbol: reportCurrencySymbol,
	}
	if successFlag {
		pricingResult = buildRuntimePricingResult(plan.ReportCurrencySnapshot, result.Connection.PricingTemplateSnapshot, result.Connection.EndpointFXSnapshot, usage, streamOutcome)
	}
	ingressRequestID := strings.TrimSpace(middleware.GetReqID(request.Context()))
	if ingressRequestID == "" {
		ingressRequestID = fmt.Sprintf("runtime-%d", requestCompletedAt.UnixNano())
	}
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	callerUserAgent := trimmedStringPointer(request.UserAgent())
	requestGenerationSnapshot := plan.RequestGenerationParamsSnapshot()
	attempts := result.Attempts
	if len(attempts) == 0 {
		attempts = []executionAttempt{{
			Connection:      result.Connection,
			RequestURL:      request.URL.String(),
			RequestHeaders:  result.RequestHeaders,
			ResponseHeaders: result.Response.Header.Clone(),
			StatusCode:      result.Response.StatusCode,
			ResponseTimeMS:  responseTimeMS,
			CompletedAt:     requestCompletedAt,
		}}
	}

	capturedRequestBody := runtimeCapturedAuditBody(plan.AuditEnabledAtRequest && plan.AuditCaptureBodiesAtRequest, plan.UpstreamBody)
	capturedResponseBody := runtimeCapturedAuditBody(plan.AuditEnabledAtRequest && plan.AuditCaptureBodiesAtRequest, responseCapture.AuditBody)
	requestLogs := make([]requestLogInsert, 0, len(attempts))
	auditLogs := make([]auditLogInsert, 0, len(attempts))
	for index, attempt := range attempts {
		attemptGenerationSnapshot := requestGenerationSnapshot.clone()
		requestGenerationParamsStatus := trimmedStringPointer(attemptGenerationSnapshot.Status)
		attemptSuccess := attempt.StatusCode >= 200 && attempt.StatusCode <= 299
		attemptBillableFlag, attemptPricedFlag, attemptUnpricedReason := billingState(attemptSuccess)
		attemptCreatedAt := attempt.CompletedAt
		if attemptCreatedAt.IsZero() || index == len(attempts)-1 {
			attemptCreatedAt = requestCompletedAt
		}
		attemptCreatedAt = attemptCreatedAt.UTC()
		attemptResponseTimeMS := attempt.ResponseTimeMS
		if attemptResponseTimeMS < 1 || index == len(attempts)-1 {
			attemptResponseTimeMS = responseTimeMS
		}
		requestLog := requestLogInsert{
			ProfileID:                     plan.ProfileID,
			ModelID:                       plan.RequestedModelID,
			ResolvedTargetModelID:         plan.ResolvedTargetModelID,
			APIFamily:                     plan.APIFamily,
			VendorID:                      plan.RequestedVendorID,
			VendorKey:                     plan.RequestedVendorKey,
			VendorName:                    plan.RequestedVendorName,
			EndpointID:                    attempt.Connection.Endpoint.ID,
			ConnectionID:                  attempt.Connection.ID,
			ProxyAPIKeyID:                 proxyKeyIDPointer(proxyKey),
			ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(proxyKey),
			IngressRequestID:              ingressRequestID,
			AttemptNumber:                 index + 1,
			ProviderCorrelationID:         headerValuePointer(attempt.ResponseHeaders, "x-request-id", "request-id"),
			EndpointBaseURL:               attempt.Connection.Endpoint.BaseURL,
			EndpointDescription:           attempt.Connection.Endpoint.Name,
			StatusCode:                    attempt.StatusCode,
			ResponseTimeMS:                attemptResponseTimeMS,
			IsStream:                      isStream,
			SuccessFlag:                   attemptSuccess,
			BillableFlag:                  attemptBillableFlag,
			PricedFlag:                    attemptPricedFlag,
			UnpricedReason:                attemptUnpricedReason,
			ReportCurrencyCode:            reportCurrencyCode,
			ReportCurrencySymbol:          reportCurrencySymbol,
			RequestPath:                   request.URL.Path,
			ErrorDetail:                   nil,
			CreatedAt:                     attemptCreatedAt,
			CallerUserAgent:               callerUserAgent,
			UpstreamUserAgent:             headerMapValuePointer(attempt.RequestHeaders, "User-Agent"),
			CompletionDurationMS:          nil,
			TTFTMS:                        nil,
			StreamOutcome:                 runtimeStreamOutcomeNotStreaming,
			AuditEnabledAtRequest:         plan.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest:   plan.AuditCaptureBodiesAtRequest,
			RequestGenerationParams:       attemptGenerationSnapshot.Params,
			RequestGenerationParamsStatus: requestGenerationParamsStatus,
		}
		if index == len(attempts)-1 {
			requestLog.InputTokens = usage.InputTokens
			requestLog.OutputTokens = usage.OutputTokens
			requestLog.TotalTokens = usage.TotalTokens
			requestLog.CacheReadInputTokens = usage.CacheReadInputTokens
			requestLog.CacheCreationInputTokens = usage.CacheCreationInputTokens
			requestLog.ReasoningTokens = usage.ReasoningTokens
			requestLog.CompletionDurationMS = completionDurationMS
			requestLog.TTFTMS = ttftMS
			requestLog.StreamOutcome = streamOutcome
			requestLog.StreamErrorKind = responseCapture.StreamErrorKind
			requestLog.StreamErrorDetail = responseCapture.StreamErrorDetail
			if attemptSuccess {
				requestLog.applyRuntimePricingResult(pricingResult)
			}
		}
		requestLogs = append(requestLogs, requestLog)
		if !plan.AuditEnabledAtRequest {
			continue
		}
		auditLog := auditLogInsert{
			RequestLogAttemptNumber:     index + 1,
			ProfileID:                   plan.ProfileID,
			VendorID:                    plan.RequestedVendorID,
			ModelID:                     plan.RequestedModelID,
			EndpointID:                  attempt.Connection.Endpoint.ID,
			ConnectionID:                attempt.Connection.ID,
			EndpointBaseURL:             attempt.Connection.Endpoint.BaseURL,
			EndpointDescription:         attempt.Connection.Endpoint.Name,
			RequestMethod:               request.Method,
			RequestURL:                  runtimeAuditRequestURL(attempt.RequestURL, request),
			RequestHeaders:              marshalAuditHeaders(attempt.RequestHeaders),
			RequestBody:                 capturedRequestBody,
			RequestBodyStored:           capturedRequestBody != nil,
			ResponseStatus:              attempt.StatusCode,
			ResponseHeaders:             marshalAuditHTTPHeaders(attempt.ResponseHeaders),
			ResponseBody:                nil,
			ResponseBodyStored:          false,
			IsStream:                    isStream,
			DurationMS:                  attemptResponseTimeMS,
			CreatedAt:                   attemptCreatedAt,
			AuditEnabledAtRequest:       plan.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: plan.AuditCaptureBodiesAtRequest,
		}
		if index == len(attempts)-1 {
			auditLog.ResponseBody = capturedResponseBody
			auditLog.ResponseBodyStored = capturedResponseBody != nil
		}
		auditLogs = append(auditLogs, auditLog)
	}

	attemptCount := len(requestLogs)
	if attemptCount < 1 {
		attemptCount = 1
	}
	usageEvent := usageEventInsert{
		ProfileID:                plan.ProfileID,
		IngressRequestID:         ingressRequestID,
		ModelID:                  plan.RequestedModelID,
		ResolvedTargetModelID:    plan.ResolvedTargetModelID,
		APIFamily:                plan.APIFamily,
		EndpointID:               result.Connection.Endpoint.ID,
		ConnectionID:             result.Connection.ID,
		ProxyAPIKeyID:            proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:  proxyKeyNamePointer(proxyKey),
		StatusCode:               result.Response.StatusCode,
		SuccessFlag:              successFlag,
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		TotalTokens:              usage.TotalTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		ReasoningTokens:          usage.ReasoningTokens,
		AttemptCount:             attemptCount,
		RequestPath:              request.URL.Path,
		CreatedAt:                requestCompletedAt,
		ResponseTimeMS:           intPtr(responseTimeMS),
		CompletionDurationMS:     completionDurationMS,
		TTFTMS:                   ttftMS,
		StreamOutcome:            streamOutcome,
		StreamErrorKind:          responseCapture.StreamErrorKind,
	}
	usageEvent.applyRuntimePricingResult(pricingResult)
	return runtimeTelemetryEnvelope{
		RequestLogs:   requestLogs,
		AuditLogs:     auditLogs,
		UsageEvent:    usageEvent,
		ProxyKeyUsage: runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
	}
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

func runtimeAuditRequestURL(requestURL string, request *http.Request) string {
	trimmed := strings.TrimSpace(requestURL)
	if trimmed != "" {
		return trimmed
	}
	if request == nil || request.URL == nil {
		return ""
	}
	return request.URL.String()
}

func marshalAuditHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		if _, ok := clientAuthHeaders[normalizedKey]; ok {
			sanitized[normalizedKey] = "[REDACTED]"
			continue
		}
		sanitized[normalizedKey] = value
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func marshalAuditHTTPHeaders(headers http.Header) *string {
	if len(headers) == 0 {
		return nil
	}
	flattened := make(map[string]string, len(headers))
	for key, values := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		flattened[normalizedKey] = strings.Join(values, ", ")
	}
	encoded, err := json.Marshal(flattened)
	if err != nil {
		return nil
	}
	resolved := string(encoded)
	return &resolved
}

func materializeRuntimeTelemetryEnvelopeTx(ctx context.Context, tx pgx.Tx, logPartitions *runtimeLogPartitionCache, envelope runtimeTelemetryEnvelope) (int, error) {
	envelope = normalizeRuntimeTelemetryEnvelopeTimestamps(envelope)
	if err := ensureRuntimeTelemetryPartitions(ctx, logPartitions, envelope); err != nil {
		return 0, err
	}
	requestLogID, err := insertRequestLogsAndUsageEventTx(ctx, tx, envelope.RequestLogs, envelope.AuditLogs, envelope.UsageEvent)
	if err != nil {
		return 0, err
	}
	if err := recordRuntimeProxyKeyUsageTx(ctx, tx, envelope.ProxyKeyUsage); err != nil {
		return 0, err
	}
	return requestLogID, nil
}

func normalizeRuntimeTelemetryEnvelopeTimestamps(envelope runtimeTelemetryEnvelope) runtimeTelemetryEnvelope {
	requestCreatedAtByAttempt := make(map[int]time.Time, len(envelope.RequestLogs))
	for index := range envelope.RequestLogs {
		envelope.RequestLogs[index].CreatedAt = envelope.RequestLogs[index].CreatedAt.UTC()
		requestCreatedAtByAttempt[envelope.RequestLogs[index].AttemptNumber] = envelope.RequestLogs[index].CreatedAt
	}
	for index := range envelope.AuditLogs {
		if createdAt, ok := requestCreatedAtByAttempt[envelope.AuditLogs[index].RequestLogAttemptNumber]; ok {
			envelope.AuditLogs[index].CreatedAt = createdAt
		} else {
			envelope.AuditLogs[index].CreatedAt = envelope.AuditLogs[index].CreatedAt.UTC()
		}
	}
	if len(envelope.RequestLogs) > 0 {
		envelope.UsageEvent.CreatedAt = envelope.RequestLogs[len(envelope.RequestLogs)-1].CreatedAt
	} else {
		envelope.UsageEvent.CreatedAt = envelope.UsageEvent.CreatedAt.UTC()
	}
	return envelope
}

func ensureRuntimeTelemetryPartitions(ctx context.Context, logPartitions *runtimeLogPartitionCache, envelope runtimeTelemetryEnvelope) error {
	if logPartitions == nil {
		return fmt.Errorf("runtime log partition ensurer unavailable")
	}
	for _, requestLog := range envelope.RequestLogs {
		if err := logPartitions.EnsurePartitionForTime(ctx, "request_logs", requestLog.CreatedAt); err != nil {
			return err
		}
	}
	for _, auditLog := range envelope.AuditLogs {
		if err := logPartitions.EnsurePartitionForTime(ctx, "audit_logs", auditLog.CreatedAt); err != nil {
			return err
		}
	}
	if err := logPartitions.EnsurePartitionForTime(ctx, "usage_request_events", envelope.UsageEvent.CreatedAt); err != nil {
		return err
	}
	return nil
}

func insertRequestLogsAndUsageEventTx(ctx context.Context, tx pgx.Tx, requestLogs []requestLogInsert, auditLogs []auditLogInsert, usageEvent usageEventInsert) (int, error) {
	auditByAttempt := make(map[int]auditLogInsert, len(auditLogs))
	for _, auditLog := range auditLogs {
		auditByAttempt[auditLog.RequestLogAttemptNumber] = auditLog
	}
	var requestLogID int
	for _, requestLog := range requestLogs {
		err := tx.QueryRow(
			ctx,
			`INSERT INTO request_logs (profile_id, model_id, resolved_target_model_id, api_family, vendor_id, vendor_key, vendor_name, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, request_path, error_detail, endpoint_description, created_at, caller_user_agent, upstream_user_agent, completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind, stream_error_detail, audit_enabled_at_request, audit_capture_bodies_at_request, request_generation_params, request_generation_params_status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51, $52, $53, $54, $55, $56, $57, $58, $59, $60, $61, $62) RETURNING id`,
			requestLog.ProfileID,
			requestLog.ModelID,
			nullableStringArg(requestLog.ResolvedTargetModelID),
			requestLog.APIFamily,
			nullableIntArg(requestLog.VendorID),
			nullableStringArg(requestLog.VendorKey),
			nullableStringArg(requestLog.VendorName),
			requestLog.EndpointID,
			requestLog.ConnectionID,
			nullableIntArg(requestLog.ProxyAPIKeyID),
			nullableStringArg(requestLog.ProxyAPIKeyNameSnapshot),
			requestLog.IngressRequestID,
			requestLog.AttemptNumber,
			nullableStringArg(requestLog.ProviderCorrelationID),
			requestLog.EndpointBaseURL,
			requestLog.StatusCode,
			requestLog.ResponseTimeMS,
			requestLog.IsStream,
			nullableIntArg(requestLog.InputTokens),
			nullableIntArg(requestLog.OutputTokens),
			nullableIntArg(requestLog.TotalTokens),
			requestLog.SuccessFlag,
			nullableBoolArg(requestLog.BillableFlag),
			nullableBoolArg(requestLog.PricedFlag),
			nullableStringArg(requestLog.UnpricedReason),
			nullableIntArg(requestLog.CacheReadInputTokens),
			nullableIntArg(requestLog.CacheCreationInputTokens),
			nullableIntArg(requestLog.ReasoningTokens),
			nullableInt64Arg(requestLog.InputCostMicros),
			nullableInt64Arg(requestLog.OutputCostMicros),
			nullableInt64Arg(requestLog.CacheReadInputCostMicros),
			nullableInt64Arg(requestLog.CacheCreationInputCostMicros),
			nullableInt64Arg(requestLog.ReasoningCostMicros),
			nullableInt64Arg(requestLog.TotalCostOriginalMicros),
			nullableInt64Arg(requestLog.TotalCostUserCurrencyMicros),
			nullableStringArg(requestLog.CurrencyCodeOriginal),
			nullableStringArg(requestLog.ReportCurrencyCode),
			nullableStringArg(requestLog.ReportCurrencySymbol),
			nullableStringArg(requestLog.FXRateUsed),
			nullableStringArg(requestLog.FXRateSource),
			nullableStringArg(requestLog.PricingSnapshotUnit),
			nullableStringArg(requestLog.PricingSnapshotInput),
			nullableStringArg(requestLog.PricingSnapshotOutput),
			nullableStringArg(requestLog.PricingSnapshotCacheReadInput),
			nullableStringArg(requestLog.PricingSnapshotCacheCreationInput),
			nullableStringArg(requestLog.PricingSnapshotReasoning),
			nullableIntArg(requestLog.PricingConfigVersionUsed),
			requestLog.RequestPath,
			nullableStringArg(requestLog.ErrorDetail),
			nullableStringArg(requestLog.EndpointDescription),
			requestLog.CreatedAt.UTC(),
			nullableStringArg(requestLog.CallerUserAgent),
			nullableStringArg(requestLog.UpstreamUserAgent),
			nullableIntArg(requestLog.CompletionDurationMS),
			nullableIntArg(requestLog.TTFTMS),
			requestLog.StreamOutcome,
			nullableStringArg(requestLog.StreamErrorKind),
			nullableStringArg(requestLog.StreamErrorDetail),
			requestLog.AuditEnabledAtRequest,
			requestLog.AuditCaptureBodiesAtRequest,
			nullableJSONArg(requestLog.RequestGenerationParams),
			nullableStringArg(requestLog.RequestGenerationParamsStatus),
		).Scan(&requestLogID)
		if err != nil {
			return 0, fmt.Errorf("insert request log: %w", err)
		}
		if auditLog, ok := auditByAttempt[requestLog.AttemptNumber]; ok {
			auditLog.CreatedAt = requestLog.CreatedAt
			if err := insertRuntimeAuditLogTx(ctx, tx, requestLogID, requestLog.CreatedAt, requestLog.IngressRequestID, auditLog); err != nil {
				return 0, err
			}
		}
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind, billable_flag, priced_flag, unpriced_reason) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47)`,
		usageEvent.ProfileID,
		usageEvent.IngressRequestID,
		usageEvent.ModelID,
		nullableStringArg(usageEvent.ResolvedTargetModelID),
		usageEvent.APIFamily,
		usageEvent.EndpointID,
		usageEvent.ConnectionID,
		nullableIntArg(usageEvent.ProxyAPIKeyID),
		nullableStringArg(usageEvent.ProxyAPIKeyNameSnapshot),
		usageEvent.StatusCode,
		usageEvent.SuccessFlag,
		nullableIntArg(usageEvent.InputTokens),
		nullableIntArg(usageEvent.OutputTokens),
		nullableIntArg(usageEvent.TotalTokens),
		nullableIntArg(usageEvent.CacheReadInputTokens),
		nullableIntArg(usageEvent.CacheCreationInputTokens),
		nullableIntArg(usageEvent.ReasoningTokens),
		nullableInt64Arg(usageEvent.InputCostMicros),
		nullableInt64Arg(usageEvent.OutputCostMicros),
		nullableInt64Arg(usageEvent.CacheReadInputCostMicros),
		nullableInt64Arg(usageEvent.CacheCreationInputCostMicros),
		nullableInt64Arg(usageEvent.ReasoningCostMicros),
		nullableInt64Arg(usageEvent.TotalCostOriginalMicros),
		nullableInt64Arg(usageEvent.TotalCostUserCurrencyMicros),
		nullableStringArg(usageEvent.CurrencyCodeOriginal),
		nullableStringArg(usageEvent.ReportCurrencyCode),
		nullableStringArg(usageEvent.ReportCurrencySymbol),
		nullableStringArg(usageEvent.FXRateUsed),
		nullableStringArg(usageEvent.FXRateSource),
		nullableStringArg(usageEvent.PricingSnapshotUnit),
		nullableStringArg(usageEvent.PricingSnapshotInput),
		nullableStringArg(usageEvent.PricingSnapshotOutput),
		nullableStringArg(usageEvent.PricingSnapshotCacheReadInput),
		nullableStringArg(usageEvent.PricingSnapshotCacheCreationInput),
		nullableStringArg(usageEvent.PricingSnapshotReasoning),
		nullableIntArg(usageEvent.PricingConfigVersionUsed),
		usageEvent.AttemptCount,
		usageEvent.RequestPath,
		usageEvent.CreatedAt.UTC(),
		nullableIntArg(usageEvent.ResponseTimeMS),
		nullableIntArg(usageEvent.CompletionDurationMS),
		nullableIntArg(usageEvent.TTFTMS),
		usageEvent.StreamOutcome,
		nullableStringArg(usageEvent.StreamErrorKind),
		nullableBoolArg(usageEvent.BillableFlag),
		nullableBoolArg(usageEvent.PricedFlag),
		nullableStringArg(usageEvent.UnpricedReason),
	); err != nil {
		return 0, fmt.Errorf("insert usage event: %w", err)
	}
	return requestLogID, nil
}

func insertRuntimeAuditLogTx(ctx context.Context, tx pgx.Tx, requestLogID int, requestLogCreatedAt time.Time, ingressRequestID string, auditLog auditLogInsert) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO audit_logs (request_log_id, request_log_created_at, ingress_request_id, profile_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_stored, response_status, response_headers, response_body, response_body_stored, is_stream, duration_ms, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)`,
		requestLogID,
		requestLogCreatedAt.UTC(),
		ingressRequestID,
		auditLog.ProfileID,
		nullableIntArg(auditLog.VendorID),
		auditLog.ModelID,
		auditLog.EndpointID,
		auditLog.ConnectionID,
		auditLog.EndpointBaseURL,
		nullableStringArg(auditLog.EndpointDescription),
		auditLog.RequestMethod,
		auditLog.RequestURL,
		auditLog.RequestHeaders,
		nullableStringArg(auditLog.RequestBody),
		auditLog.RequestBodyStored,
		auditLog.ResponseStatus,
		nullableStringArg(auditLog.ResponseHeaders),
		nullableStringArg(auditLog.ResponseBody),
		auditLog.ResponseBodyStored,
		auditLog.IsStream,
		auditLog.DurationMS,
		auditLog.CreatedAt.UTC(),
		auditLog.AuditEnabledAtRequest,
		auditLog.AuditCaptureBodiesAtRequest,
	); err != nil {
		return fmt.Errorf("insert audit log for request log %d: %w", requestLogID, err)
	}
	return nil
}

func extractResponseUsage(body []byte) responseUsage {
	if len(body) == 0 {
		return responseUsage{}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return responseUsage{}
	}
	return extractResponseUsageFromPayload(payload)
}

func extractResponseUsageFromPayload(payload map[string]any) responseUsage {
	usage := responseUsage{}
	usagePayload, ok := responseUsagePayload(payload)
	if ok {
		usage.mergeStandardUsagePayload(usagePayload)
	}
	if usageMetadata, ok := payload["usageMetadata"].(map[string]any); ok {
		usage.mergeGeminiUsagePayload(usageMetadata)
	}
	return usage.normalized()
}

func responseUsagePayload(payload map[string]any) (map[string]any, bool) {
	if usagePayload, ok := payload["usage"].(map[string]any); ok {
		return usagePayload, true
	}
	responsePayload, ok := payload["response"].(map[string]any)
	if !ok {
		return nil, false
	}
	usagePayload, ok := responsePayload["usage"].(map[string]any)
	return usagePayload, ok
}

func billingState(success bool) (*bool, *bool, *string) {
	if !success {
		return boolPtr(false), boolPtr(false), nil
	}
	return boolPtr(true), boolPtr(false), stringPtr("missing_pricing_template")
}

func requestWantsStream(rawBody []byte, requestPath string) bool {
	if strings.Contains(strings.TrimSpace(requestPath), ":streamGenerateContent") {
		return true
	}
	return requestBodyWantsStream(rawBody)
}

func requestBodyWantsStream(rawBody []byte) bool {
	if len(rawBody) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return false
	}
	stream, ok := payload["stream"].(bool)
	return ok && stream
}

func durationMilliseconds(duration time.Duration) int {
	milliseconds := int(duration / time.Millisecond)
	if milliseconds < 1 {
		return 1
	}
	return milliseconds
}

func runtimeProxyKeyUsageSignalFromSnapshot(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *runtimeProxyKeyUsageSignal {
	if proxyKey == nil || proxyKey.ID <= 0 || proxyKey.LastUsedAt.IsZero() {
		return nil
	}
	return &runtimeProxyKeyUsageSignal{
		KeyID:      proxyKey.ID,
		LastUsedAt: proxyKey.LastUsedAt.UTC(),
		LastUsedIP: strings.TrimSpace(proxyKey.LastUsedIP),
	}
}

func recordRuntimeProxyKeyUsageTx(ctx context.Context, tx pgx.Tx, signal *runtimeProxyKeyUsageSignal) error {
	if signal == nil {
		return nil
	}
	if err := proxykeyusage.RecordTx(ctx, tx, signal.KeyID, signal.LastUsedAt, signal.LastUsedIP); err != nil {
		return fmt.Errorf("record runtime proxy api key usage: %w", err)
	}
	return nil
}

func headerValuePointer(header http.Header, keys ...string) *string {
	for _, key := range keys {
		value := strings.TrimSpace(header.Get(key))
		if value != "" {
			return &value
		}
	}
	return nil
}

func headerMapValuePointer(header map[string]string, key string) *string {
	for headerKey, value := range header {
		if strings.EqualFold(headerKey, key) {
			return trimmedStringPointer(value)
		}
	}
	return nil
}

func proxyKeyIDPointer(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *int {
	if proxyKey == nil {
		return nil
	}
	return &proxyKey.ID
}

func proxyKeyNamePointer(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *string {
	if proxyKey == nil {
		return nil
	}
	return &proxyKey.Name
}

func firstValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func nestedValue(values map[string]any, key string, nestedKey string) any {
	nested, ok := values[key].(map[string]any)
	if !ok {
		return nil
	}
	return nested[nestedKey]
}

func intPointerFromAny(value any) *int {
	switch typed := value.(type) {
	case float64:
		resolved := int(typed)
		return &resolved
	case json.Number:
		value, err := typed.Int64()
		if err != nil {
			return nil
		}
		resolved := int(value)
		return &resolved
	case int:
		resolved := typed
		return &resolved
	case int64:
		resolved := int(typed)
		return &resolved
	default:
		return nil
	}
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func trimmedStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Arg(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolArg(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableJSONArg(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(raw)
}

func intPtr(value int) *int {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

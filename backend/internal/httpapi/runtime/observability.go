package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

type responseUsage struct {
	InputTokens              *int
	OutputTokens             *int
	TotalTokens              *int
	CacheReadInputTokens     *int
	CacheCreationInputTokens *int
	ReasoningTokens          *int
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
	AuditEnabledAtRequest             bool
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
}

func (s *Service) recordRuntimeActivity(ctx context.Context, plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	usage := extractResponseUsage(responseCapture.Body)
	ttftMS, completionDurationMS := runtimeResponseTiming(startedAt, requestCompletedAt, requestWantsStream(plan.RawRequestBody), responseCapture)
	successFlag := result.Response.StatusCode >= 200 && result.Response.StatusCode <= 299
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	pricingResult := runtimePricingResult{
		ReportCurrencyCode:   reportCurrencyCode,
		ReportCurrencySymbol: reportCurrencySymbol,
	}
	billableFlag := boolPtr(false)
	pricedFlag := boolPtr(false)
	var unpricedReason *string
	if successFlag {
		pricingResult = buildRuntimePricingResult(plan.ReportCurrencySnapshot, result.Connection.PricingTemplateSnapshot, result.Connection.EndpointFXSnapshot, usage)
		billableFlag = boolPtr(pricingResult.Billable)
		pricedFlag = boolPtr(pricingResult.Priced)
		unpricedReason = pricingResult.UnpricedReason
	}
	ingressRequestID := strings.TrimSpace(middleware.GetReqID(request.Context()))
	if ingressRequestID == "" {
		ingressRequestID = fmt.Sprintf("runtime-%d", requestCompletedAt.UnixNano())
	}
	proxyKey, _ := managementauth.RuntimeProxyKeyFromContext(request.Context())
	callerUserAgent := trimmedStringPointer(request.UserAgent())
	isStream := requestWantsStream(plan.RawRequestBody)
	attempts := result.Attempts
	if len(attempts) == 0 {
		attempts = []executionAttempt{{
			Connection:      result.Connection,
			RequestHeaders:  result.RequestHeaders,
			ResponseHeaders: result.Response.Header.Clone(),
			StatusCode:      result.Response.StatusCode,
			ResponseTimeMS:  responseTimeMS,
			CompletedAt:     requestCompletedAt,
		}}
	}

	requestLogs := make([]requestLogInsert, 0, len(attempts))
	for index, attempt := range attempts {
		attemptSuccess := attempt.StatusCode >= 200 && attempt.StatusCode <= 299
		attemptBillableFlag, attemptPricedFlag, attemptUnpricedReason := billingState(attemptSuccess)
		attemptCreatedAt := attempt.CompletedAt
		if attemptCreatedAt.IsZero() || index == len(attempts)-1 {
			attemptCreatedAt = requestCompletedAt
		}
		attemptResponseTimeMS := attempt.ResponseTimeMS
		if attemptResponseTimeMS < 1 || index == len(attempts)-1 {
			attemptResponseTimeMS = responseTimeMS
		}
		requestLog := requestLogInsert{
			ProfileID:               plan.ProfileID,
			ModelID:                 plan.RequestedModelID,
			ResolvedTargetModelID:   plan.ResolvedTargetModelID,
			APIFamily:               plan.APIFamily,
			VendorID:                plan.RequestedVendorID,
			VendorKey:               plan.RequestedVendorKey,
			VendorName:              plan.RequestedVendorName,
			EndpointID:              attempt.Connection.Endpoint.ID,
			ConnectionID:            attempt.Connection.ID,
			ProxyAPIKeyID:           proxyKeyIDPointer(proxyKey),
			ProxyAPIKeyNameSnapshot: proxyKeyNamePointer(proxyKey),
			IngressRequestID:        ingressRequestID,
			AttemptNumber:           index + 1,
			ProviderCorrelationID:   headerValuePointer(attempt.ResponseHeaders, "x-request-id", "request-id"),
			EndpointBaseURL:         attempt.Connection.Endpoint.BaseURL,
			EndpointDescription:     attempt.Connection.Endpoint.Name,
			StatusCode:              attempt.StatusCode,
			ResponseTimeMS:          attemptResponseTimeMS,
			IsStream:                isStream,
			SuccessFlag:             attemptSuccess,
			BillableFlag:            attemptBillableFlag,
			PricedFlag:              attemptPricedFlag,
			UnpricedReason:          attemptUnpricedReason,
			ReportCurrencyCode:      reportCurrencyCode,
			ReportCurrencySymbol:    reportCurrencySymbol,
			RequestPath:             request.URL.Path,
			ErrorDetail:             nil,
			CreatedAt:               attemptCreatedAt,
			CallerUserAgent:         callerUserAgent,
			UpstreamUserAgent:       headerMapValuePointer(attempt.RequestHeaders, "User-Agent"),
			CompletionDurationMS:    nil,
			TTFTMS:                  nil,
			AuditEnabledAtRequest:   plan.AuditEnabledAtRequest,
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
			if attemptSuccess {
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
		}
		requestLogs = append(requestLogs, requestLog)
	}

	attemptCount := len(requestLogs)
	if attemptCount < 1 {
		attemptCount = 1
	}
	usageEvent := usageEventInsert{
		ProfileID:                         plan.ProfileID,
		IngressRequestID:                  ingressRequestID,
		ModelID:                           plan.RequestedModelID,
		ResolvedTargetModelID:             plan.ResolvedTargetModelID,
		APIFamily:                         plan.APIFamily,
		EndpointID:                        result.Connection.Endpoint.ID,
		ConnectionID:                      result.Connection.ID,
		ProxyAPIKeyID:                     proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:           proxyKeyNamePointer(proxyKey),
		StatusCode:                        result.Response.StatusCode,
		SuccessFlag:                       successFlag,
		BillableFlag:                      billableFlag,
		PricedFlag:                        pricedFlag,
		UnpricedReason:                    unpricedReason,
		InputTokens:                       usage.InputTokens,
		OutputTokens:                      usage.OutputTokens,
		TotalTokens:                       usage.TotalTokens,
		CacheReadInputTokens:              usage.CacheReadInputTokens,
		CacheCreationInputTokens:          usage.CacheCreationInputTokens,
		ReasoningTokens:                   usage.ReasoningTokens,
		InputCostMicros:                   pricingResult.InputCostMicros,
		OutputCostMicros:                  pricingResult.OutputCostMicros,
		CacheReadInputCostMicros:          pricingResult.CacheReadInputCostMicros,
		CacheCreationInputCostMicros:      pricingResult.CacheCreationInputCostMicros,
		ReasoningCostMicros:               pricingResult.ReasoningCostMicros,
		TotalCostOriginalMicros:           pricingResult.TotalCostOriginalMicros,
		TotalCostUserCurrencyMicros:       pricingResult.TotalCostUserCurrencyMicros,
		CurrencyCodeOriginal:              pricingResult.CurrencyCodeOriginal,
		ReportCurrencyCode:                pricingResult.ReportCurrencyCode,
		ReportCurrencySymbol:              pricingResult.ReportCurrencySymbol,
		FXRateUsed:                        pricingResult.FXRateUsed,
		FXRateSource:                      pricingResult.FXRateSource,
		PricingSnapshotUnit:               pricingResult.PricingSnapshotUnit,
		PricingSnapshotInput:              pricingResult.PricingSnapshotInput,
		PricingSnapshotOutput:             pricingResult.PricingSnapshotOutput,
		PricingSnapshotCacheReadInput:     pricingResult.PricingSnapshotCacheReadInput,
		PricingSnapshotCacheCreationInput: pricingResult.PricingSnapshotCacheCreationInput,
		PricingSnapshotReasoning:          pricingResult.PricingSnapshotReasoning,
		PricingConfigVersionUsed:          pricingResult.PricingConfigVersionUsed,
		AttemptCount:                      attemptCount,
		RequestPath:                       request.URL.Path,
		CreatedAt:                         requestCompletedAt,
		ResponseTimeMS:                    intPtr(responseTimeMS),
		CompletionDurationMS:              completionDurationMS,
		TTFTMS:                            ttftMS,
	}
	requestLogID, err := s.insertRequestLogsAndUsageEvent(ctx, requestLogs, usageEvent)
	if err != nil {
		return
	}
	if s.dashboardUpdates == nil {
		return
	}
	_, _ = s.dashboardUpdates.PublishDashboardUpdate(ctx, requestLogID, plan.ProfileID)
}

func runtimeResponseTiming(startedAt time.Time, completedAt time.Time, isStream bool, capture runtimeResponseCapture) (*int, *int) {
	var ttftMS *int
	if capture.FirstMeaningfulPayloadAt != nil {
		ttft := durationMilliseconds(capture.FirstMeaningfulPayloadAt.Sub(startedAt))
		ttftMS = &ttft
	}
	if !isStream {
		completionDuration := durationMilliseconds(completedAt.Sub(startedAt))
		return ttftMS, &completionDuration
	}
	if capture.CompletedAt == nil {
		return ttftMS, nil
	}
	completionDuration := durationMilliseconds(capture.CompletedAt.Sub(startedAt))
	return ttftMS, &completionDuration
}

func (s *Service) insertRequestLogsAndUsageEvent(ctx context.Context, requestLogs []requestLogInsert, usageEvent usageEventInsert) (int, error) {
	return pgxutil.InTxValue(ctx, s.pool, "runtime", func(tx pgx.Tx) (int, error) {
		var requestLogID int
		for _, requestLog := range requestLogs {
			err := tx.QueryRow(
				ctx,
				`INSERT INTO request_logs (profile_id, model_id, resolved_target_model_id, api_family, vendor_id, vendor_key, vendor_name, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, request_path, error_detail, endpoint_description, created_at, caller_user_agent, upstream_user_agent, completion_duration_ms, ttft_ms, audit_enabled_at_request) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51, $52, $53, $54, $55, $56) RETURNING id`,
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
				requestLog.CreatedAt,
				nullableStringArg(requestLog.CallerUserAgent),
				nullableStringArg(requestLog.UpstreamUserAgent),
				nullableIntArg(requestLog.CompletionDurationMS),
				nullableIntArg(requestLog.TTFTMS),
				requestLog.AuditEnabledAtRequest,
			).Scan(&requestLogID)
			if err != nil {
				return 0, fmt.Errorf("insert request log: %w", err)
			}
		}
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, billable_flag, priced_flag, unpriced_reason) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45)`,
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
			usageEvent.CreatedAt,
			nullableIntArg(usageEvent.ResponseTimeMS),
			nullableIntArg(usageEvent.CompletionDurationMS),
			nullableIntArg(usageEvent.TTFTMS),
			nullableBoolArg(usageEvent.BillableFlag),
			nullableBoolArg(usageEvent.PricedFlag),
			nullableStringArg(usageEvent.UnpricedReason),
		); err != nil {
			return 0, fmt.Errorf("insert usage event: %w", err)
		}
		return requestLogID, nil
	})
}

func extractResponseUsage(body []byte) responseUsage {
	if len(body) == 0 {
		return responseUsage{}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return responseUsage{}
	}
	usagePayload, ok := responseUsagePayload(payload)
	if !ok {
		return responseUsage{}
	}
	inputTokens := intPointerFromAny(firstValue(usagePayload, "prompt_tokens", "input_tokens"))
	outputTokens := intPointerFromAny(firstValue(usagePayload, "completion_tokens", "output_tokens"))
	totalTokens := intPointerFromAny(usagePayload["total_tokens"])
	if totalTokens == nil && (inputTokens != nil || outputTokens != nil) {
		total := intValue(inputTokens) + intValue(outputTokens)
		totalTokens = &total
	}
	reasoningTokens := intPointerFromAny(firstValue(
		map[string]any{
			"completion": nestedValue(usagePayload, "completion_tokens_details", "reasoning_tokens"),
			"output":     nestedValue(usagePayload, "output_tokens_details", "reasoning_tokens"),
		},
		"completion",
		"output",
	))
	return responseUsage{
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		TotalTokens:              totalTokens,
		CacheReadInputTokens:     intPointerFromAny(usagePayload["cache_read_input_tokens"]),
		CacheCreationInputTokens: intPointerFromAny(usagePayload["cache_creation_input_tokens"]),
		ReasoningTokens:          reasoningTokens,
	}
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

func requestWantsStream(rawBody []byte) bool {
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

func proxyKeyIDPointer(proxyKey *managementauth.RuntimeProxyKeySnapshot) *int {
	if proxyKey == nil {
		return nil
	}
	return &proxyKey.ID
}

func proxyKeyNamePointer(proxyKey *managementauth.RuntimeProxyKeySnapshot) *string {
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

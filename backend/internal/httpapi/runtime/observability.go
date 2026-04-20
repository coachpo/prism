package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
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
	ProfileID                   int
	ModelID                     string
	ResolvedTargetModelID       *string
	APIFamily                   string
	VendorID                    *int
	VendorKey                   *string
	VendorName                  *string
	EndpointID                  int
	ConnectionID                int
	ProxyAPIKeyID               *int
	ProxyAPIKeyNameSnapshot     *string
	IngressRequestID            string
	AttemptNumber               int
	ProviderCorrelationID       *string
	EndpointBaseURL             string
	EndpointDescription         *string
	StatusCode                  int
	ResponseTimeMS              int
	IsStream                    bool
	InputTokens                 *int
	OutputTokens                *int
	TotalTokens                 *int
	SuccessFlag                 bool
	BillableFlag                *bool
	PricedFlag                  *bool
	UnpricedReason              *string
	CacheReadInputTokens        *int
	CacheCreationInputTokens    *int
	ReasoningTokens             *int
	TotalCostOriginalMicros     *int64
	TotalCostUserCurrencyMicros *int64
	ReportCurrencyCode          *string
	ReportCurrencySymbol        *string
	RequestPath                 string
	CreatedAt                   time.Time
	CallerUserAgent             *string
	UpstreamUserAgent           *string
	CompletionDurationMS        *int
	TTFTMS                      *int
	AuditEnabledAtRequest       bool
}

type usageEventInsert struct {
	ProfileID                   int
	IngressRequestID            string
	ModelID                     string
	ResolvedTargetModelID       *string
	APIFamily                   string
	EndpointID                  int
	ConnectionID                int
	ProxyAPIKeyID               *int
	ProxyAPIKeyNameSnapshot     *string
	StatusCode                  int
	SuccessFlag                 bool
	BillableFlag                *bool
	PricedFlag                  *bool
	UnpricedReason              *string
	InputTokens                 *int
	OutputTokens                *int
	TotalTokens                 *int
	CacheReadInputTokens        *int
	CacheCreationInputTokens    *int
	ReasoningTokens             *int
	TotalCostUserCurrencyMicros *int64
	ReportCurrencyCode          *string
	ReportCurrencySymbol        *string
	AttemptCount                int
	RequestPath                 string
	CreatedAt                   time.Time
	ResponseTimeMS              *int
	CompletionDurationMS        *int
	TTFTMS                      *int
}

func (s *Service) recordRuntimeActivity(ctx context.Context, plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseBody []byte) {
	responseTimeMS := durationMilliseconds(s.nowUTC().Sub(startedAt))
	usage := extractResponseUsage(responseBody)
	successFlag := result.Response.StatusCode >= 200 && result.Response.StatusCode <= 299
	billableFlag, pricedFlag, unpricedReason := billingState(successFlag)
	reportCurrencyCode, reportCurrencySymbol, err := s.loadReportCurrency(ctx, plan.ProfileID)
	if err != nil {
		return
	}
	ingressRequestID := strings.TrimSpace(middleware.GetReqID(request.Context()))
	if ingressRequestID == "" {
		ingressRequestID = fmt.Sprintf("runtime-%d", s.nowUTC().UnixNano())
	}
	proxyKey, _ := managementauth.RuntimeProxyKeyFromContext(request.Context())
	providerCorrelationID := headerValuePointer(result.Response.Header, "x-request-id", "request-id")
	callerUserAgent := trimmedStringPointer(request.UserAgent())
	upstreamUserAgent := headerMapValuePointer(result.RequestHeaders, "User-Agent")
	createdAt := s.nowUTC()

	requestLog := requestLogInsert{
		ProfileID:                   plan.ProfileID,
		ModelID:                     plan.RequestedModelID,
		ResolvedTargetModelID:       plan.ResolvedTargetModelID,
		APIFamily:                   plan.APIFamily,
		VendorID:                    plan.RequestedVendorID,
		VendorKey:                   plan.RequestedVendorKey,
		VendorName:                  plan.RequestedVendorName,
		EndpointID:                  result.Connection.Endpoint.ID,
		ConnectionID:                result.Connection.ID,
		ProxyAPIKeyID:               proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:     proxyKeyNamePointer(proxyKey),
		IngressRequestID:            ingressRequestID,
		AttemptNumber:               1,
		ProviderCorrelationID:       providerCorrelationID,
		EndpointBaseURL:             result.Connection.Endpoint.BaseURL,
		EndpointDescription:         result.Connection.Endpoint.Name,
		StatusCode:                  result.Response.StatusCode,
		ResponseTimeMS:              responseTimeMS,
		IsStream:                    requestWantsStream(plan.RawRequestBody),
		InputTokens:                 usage.InputTokens,
		OutputTokens:                usage.OutputTokens,
		TotalTokens:                 usage.TotalTokens,
		SuccessFlag:                 successFlag,
		BillableFlag:                billableFlag,
		PricedFlag:                  pricedFlag,
		UnpricedReason:              unpricedReason,
		CacheReadInputTokens:        usage.CacheReadInputTokens,
		CacheCreationInputTokens:    usage.CacheCreationInputTokens,
		ReasoningTokens:             usage.ReasoningTokens,
		TotalCostOriginalMicros:     int64Ptr(0),
		TotalCostUserCurrencyMicros: int64Ptr(0),
		ReportCurrencyCode:          reportCurrencyCode,
		ReportCurrencySymbol:        reportCurrencySymbol,
		RequestPath:                 request.URL.Path,
		CreatedAt:                   createdAt,
		CallerUserAgent:             callerUserAgent,
		UpstreamUserAgent:           upstreamUserAgent,
		CompletionDurationMS:        nil,
		TTFTMS:                      nil,
		AuditEnabledAtRequest:       plan.AuditEnabledAtRequest,
	}
	usageEvent := usageEventInsert{
		ProfileID:                   plan.ProfileID,
		IngressRequestID:            ingressRequestID,
		ModelID:                     plan.RequestedModelID,
		ResolvedTargetModelID:       plan.ResolvedTargetModelID,
		APIFamily:                   plan.APIFamily,
		EndpointID:                  result.Connection.Endpoint.ID,
		ConnectionID:                result.Connection.ID,
		ProxyAPIKeyID:               proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:     proxyKeyNamePointer(proxyKey),
		StatusCode:                  result.Response.StatusCode,
		SuccessFlag:                 successFlag,
		BillableFlag:                billableFlag,
		PricedFlag:                  pricedFlag,
		UnpricedReason:              unpricedReason,
		InputTokens:                 usage.InputTokens,
		OutputTokens:                usage.OutputTokens,
		TotalTokens:                 usage.TotalTokens,
		CacheReadInputTokens:        usage.CacheReadInputTokens,
		CacheCreationInputTokens:    usage.CacheCreationInputTokens,
		ReasoningTokens:             usage.ReasoningTokens,
		TotalCostUserCurrencyMicros: int64Ptr(0),
		ReportCurrencyCode:          reportCurrencyCode,
		ReportCurrencySymbol:        reportCurrencySymbol,
		AttemptCount:                1,
		RequestPath:                 request.URL.Path,
		CreatedAt:                   createdAt,
		ResponseTimeMS:              intPtr(responseTimeMS),
		CompletionDurationMS:        nil,
		TTFTMS:                      nil,
	}
	requestLogID, err := s.insertRequestLogAndUsageEvent(ctx, requestLog, usageEvent)
	if err != nil {
		return
	}
	if s.dashboardUpdates == nil {
		return
	}
	_, _ = s.dashboardUpdates.PublishDashboardUpdate(ctx, requestLogID, plan.ProfileID)
}

func (s *Service) insertRequestLogAndUsageEvent(ctx context.Context, requestLog requestLogInsert, usageEvent usageEventInsert) (int, error) {
	return withTxValue(ctx, s.pool, func(tx pgx.Tx) (int, error) {
		var requestLogID int
		err := tx.QueryRow(
			ctx,
			`INSERT INTO request_logs (profile_id, model_id, resolved_target_model_id, api_family, vendor_id, vendor_key, vendor_name, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, total_cost_original_micros, total_cost_user_currency_micros, report_currency_code, report_currency_symbol, request_path, endpoint_description, created_at, caller_user_agent, upstream_user_agent, completion_duration_ms, ttft_ms, audit_enabled_at_request) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40) RETURNING id`,
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
			nullableInt64Arg(requestLog.TotalCostOriginalMicros),
			nullableInt64Arg(requestLog.TotalCostUserCurrencyMicros),
			nullableStringArg(requestLog.ReportCurrencyCode),
			nullableStringArg(requestLog.ReportCurrencySymbol),
			requestLog.RequestPath,
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
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, proxy_api_key_id, proxy_api_key_name_snapshot, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, total_cost_user_currency_micros, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, billable_flag, priced_flag, unpriced_reason, report_currency_code, report_currency_symbol) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)`,
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
			nullableInt64Arg(usageEvent.TotalCostUserCurrencyMicros),
			usageEvent.AttemptCount,
			usageEvent.RequestPath,
			usageEvent.CreatedAt,
			nullableIntArg(usageEvent.ResponseTimeMS),
			nullableIntArg(usageEvent.CompletionDurationMS),
			nullableIntArg(usageEvent.TTFTMS),
			nullableBoolArg(usageEvent.BillableFlag),
			nullableBoolArg(usageEvent.PricedFlag),
			nullableStringArg(usageEvent.UnpricedReason),
			nullableStringArg(usageEvent.ReportCurrencyCode),
			nullableStringArg(usageEvent.ReportCurrencySymbol),
		); err != nil {
			return 0, fmt.Errorf("insert usage event: %w", err)
		}
		return requestLogID, nil
	})
}

func (s *Service) loadReportCurrency(ctx context.Context, profileID int) (*string, *string, error) {
	var code string
	var symbol string
	err := s.pool.QueryRow(ctx, `SELECT report_currency_code, report_currency_symbol FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`, profileID).Scan(&code, &symbol)
	if err == nil {
		return trimmedStringPointer(code), trimmedStringPointer(symbol), nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return stringPtr("USD"), stringPtr("$"), nil
	}
	return nil, nil, fmt.Errorf("load report currency for profile %d: %w", profileID, err)
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

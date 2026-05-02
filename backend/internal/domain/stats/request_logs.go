package stats

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type requestLogListRow struct {
	ID                          int
	CreatedAt                   time.Time
	ModelID                     string
	ResolvedTargetModelID       *string
	APIFamily                   string
	VendorID                    *int
	VendorKey                   *string
	VendorName                  *string
	EndpointID                  *int
	ConnectionID                *int
	StatusCode                  int
	ResponseTimeMS              int
	TTFTMS                      *int
	CompletionDurationMS        *int
	IsStream                    bool
	StreamOutcome               string
	StreamErrorKind             *string
	OutputTokens                *int
	TotalTokens                 *int
	TotalCostUserCurrencyMicros *int64
	PricedFlag                  *bool
	UnpricedReason              *string
	ReasoningEffort             *string
	ReportCurrencySymbol        *string
	CallerUserAgent             *string
	UpstreamUserAgent           *string
	EndpointBaseURL             *string
}

type requestLogDetailRow struct {
	ProfileID                         int
	ID                                int
	CreatedAt                         time.Time
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	VendorID                          *int
	VendorKey                         *string
	VendorName                        *string
	StatusCode                        int
	ResponseTimeMS                    int
	TTFTMS                            *int
	CompletionDurationMS              *int
	IsStream                          bool
	StreamOutcome                     string
	StreamErrorKind                   *string
	StreamErrorDetail                 *string
	RequestPath                       string
	IngressRequestID                  *string
	AttemptNumber                     *int
	ProviderCorrelationID             *string
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
	CallerUserAgent                   *string
	UpstreamUserAgent                 *string
	ErrorDetail                       *string
	RequestGenerationParams           *json.RawMessage
	RequestGenerationParamsStatus     *string
	EndpointID                        *int
	ConnectionID                      *int
	EndpointBaseURL                   *string
	EndpointDescription               *string
	AuditEnabledAtRequest             bool
	AuditCaptureBodiesAtRequest       bool
	InputTokens                       *int
	OutputTokens                      *int
	TotalTokens                       *int
	SuccessFlag                       *bool
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
}

type requestLogModelRecord struct {
	ModelID     string
	DisplayName *string
	ModelType   string
}

func ListRequestLogs(ctx context.Context, exec queryExecutor, params RequestLogListParams) (RequestLogListResponse, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	currentEndpoints, currentEndpointsByID, err := loadCurrentEndpoints(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogListResponse{}, err
	}
	currentModels, currentModelsByID, err := loadRequestLogModels(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogListResponse{}, err
	}
	rules, err := loadCompiledUserAgentRules(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogListResponse{}, err
	}
	whereClause, args := buildRequestLogBrowseWhere(params)
	countQuery := `SELECT COUNT(*) FROM request_logs WHERE ` + whereClause
	var total int
	if err := exec.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return RequestLogListResponse{}, fmt.Errorf("count request logs for profile %d: %w", params.ProfileID, err)
	}
	rows, err := exec.Query(
		ctx,
		`SELECT id, created_at, model_id, resolved_target_model_id, api_family, vendor_id, vendor_key, vendor_name, endpoint_id, connection_id, status_code, response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, stream_error_kind, output_tokens, total_tokens, total_cost_user_currency_micros, priced_flag, unpriced_reason, request_generation_params #>> '{reasoning,effort}', report_currency_symbol, caller_user_agent, upstream_user_agent, endpoint_base_url
		 FROM request_logs
		 WHERE `+whereClause+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT $`+fmt.Sprintf("%d", len(args)+1)+` OFFSET $`+fmt.Sprintf("%d", len(args)+2),
		append(append(args, limit), offset)...,
	)
	if err != nil {
		return RequestLogListResponse{}, fmt.Errorf("query request logs for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	items := make([]RequestLogListItem, 0)
	for rows.Next() {
		item, scanErr := scanRequestLogListRow(rows)
		if scanErr != nil {
			return RequestLogListResponse{}, scanErr
		}
		currentEndpoint, _ := endpointFromMap(currentEndpointsByID, item.EndpointID)
		items = append(items, RequestLogListItem{
			ID:                          item.ID,
			CreatedAt:                   item.CreatedAt.UTC(),
			ModelID:                     item.ModelID,
			ModelLabel:                  resolveRequestLogModelLabel(currentModelsByID, item.ModelID),
			ResolvedTargetModelID:       item.ResolvedTargetModelID,
			ResolvedTargetModelLabel:    resolveRequestLogResolvedTargetModelLabel(currentModelsByID, item.ResolvedTargetModelID),
			IsProxyOrigin:               resolveRequestLogIsProxyOrigin(currentModelsByID, item.ModelID, item.ResolvedTargetModelID),
			APIFamily:                   item.APIFamily,
			VendorID:                    item.VendorID,
			VendorKey:                   item.VendorKey,
			VendorName:                  item.VendorName,
			EndpointID:                  item.EndpointID,
			EndpointLabel:               resolveEndpointLabel(currentEndpoint.Name, currentEndpoint.BaseURL, item.EndpointBaseURL, item.EndpointID, "Unknown Endpoint"),
			ConnectionID:                item.ConnectionID,
			StatusCode:                  item.StatusCode,
			ResponseTimeMS:              item.ResponseTimeMS,
			TTFTMS:                      item.TTFTMS,
			CompletionDurationMS:        item.CompletionDurationMS,
			IsStream:                    item.IsStream,
			StreamOutcome:               item.StreamOutcome,
			StreamErrorKind:             item.StreamErrorKind,
			OutputTokens:                item.OutputTokens,
			TotalTokens:                 item.TotalTokens,
			TotalCostUserCurrencyMicros: item.TotalCostUserCurrencyMicros,
			PricedFlag:                  item.PricedFlag,
			UnpricedReason:              item.UnpricedReason,
			ReasoningEffort:             item.ReasoningEffort,
			ReportCurrencySymbol:        item.ReportCurrencySymbol,
			CallerClientDisplay:         classifyUserAgentDisplay(item.CallerUserAgent, rules),
			UpstreamClientDisplay:       classifyUserAgentDisplay(item.UpstreamUserAgent, rules),
			UserAgentOverridden:         userAgentOverridden(item.CallerUserAgent, item.UpstreamUserAgent),
		})
	}
	if err := rows.Err(); err != nil {
		return RequestLogListResponse{}, fmt.Errorf("iterate request logs for profile %d: %w", params.ProfileID, err)
	}
	return RequestLogListResponse{
		FilterOptions: RequestLogListFilterOptions{
			Endpoints: buildRequestLogEndpointOptions(currentEndpoints, params.EndpointID),
			Models:    buildRequestLogModelOptions(currentModels, params.ModelID),
		},
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func GetRequestLogDetail(ctx context.Context, exec queryExecutor, profileID int, requestID int) (*RequestLogDetailResponse, error) {
	row, found, err := loadRequestLogDetailRow(ctx, exec, profileID, requestID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	_, currentEndpointsByID, err := loadCurrentEndpoints(ctx, exec, profileID)
	if err != nil {
		return nil, err
	}
	_, currentModelsByID, err := loadRequestLogModels(ctx, exec, profileID)
	if err != nil {
		return nil, err
	}
	rules, err := loadCompiledUserAgentRules(ctx, exec, profileID)
	if err != nil {
		return nil, err
	}
	currentEndpoint, _ := endpointFromMap(currentEndpointsByID, row.EndpointID)
	callerClientDisplay := classifyUserAgentDisplay(row.CallerUserAgent, rules)
	upstreamClientDisplay := classifyUserAgentDisplay(row.UpstreamUserAgent, rules)
	response := &RequestLogDetailResponse{
		Summary: RequestLogDetailSummary{
			ID:                       row.ID,
			CreatedAt:                row.CreatedAt.UTC(),
			ModelID:                  row.ModelID,
			ModelLabel:               resolveRequestLogModelLabel(currentModelsByID, row.ModelID),
			ResolvedTargetModelID:    row.ResolvedTargetModelID,
			ResolvedTargetModelLabel: resolveRequestLogResolvedTargetModelLabel(currentModelsByID, row.ResolvedTargetModelID),
			IsProxyOrigin:            resolveRequestLogIsProxyOrigin(currentModelsByID, row.ModelID, row.ResolvedTargetModelID),
			APIFamily:                row.APIFamily,
			VendorID:                 row.VendorID,
			VendorKey:                row.VendorKey,
			VendorName:               row.VendorName,
			StatusCode:               row.StatusCode,
			ResponseTimeMS:           row.ResponseTimeMS,
			TTFTMS:                   row.TTFTMS,
			CompletionDurationMS:     row.CompletionDurationMS,
			IsStream:                 row.IsStream,
			StreamOutcome:            row.StreamOutcome,
			StreamErrorKind:          row.StreamErrorKind,
			StreamErrorDetail:        row.StreamErrorDetail,
		},
		Request: RequestLogDetailRequest{
			RequestPath:                   row.RequestPath,
			IngressRequestID:              row.IngressRequestID,
			AttemptNumber:                 row.AttemptNumber,
			ProviderCorrelationID:         row.ProviderCorrelationID,
			ProxyAPIKeyID:                 row.ProxyAPIKeyID,
			ProxyAPIKeyNameSnapshot:       row.ProxyAPIKeyNameSnapshot,
			CallerUserAgent:               row.CallerUserAgent,
			UpstreamUserAgent:             row.UpstreamUserAgent,
			CallerClientDisplay:           callerClientDisplay,
			UpstreamClientDisplay:         upstreamClientDisplay,
			UserAgentOverridden:           userAgentOverridden(row.CallerUserAgent, row.UpstreamUserAgent),
			ErrorDetail:                   row.ErrorDetail,
			RequestGenerationParams:       row.RequestGenerationParams,
			RequestGenerationParamsStatus: row.RequestGenerationParamsStatus,
		},
		Routing: RequestLogDetailRouting{
			ProfileID:                   row.ProfileID,
			EndpointLabel:               resolveEndpointLabel(currentEndpoint.Name, currentEndpoint.BaseURL, row.EndpointBaseURL, row.EndpointID, "Unknown Endpoint"),
			EndpointID:                  row.EndpointID,
			ConnectionID:                row.ConnectionID,
			EndpointBaseURL:             row.EndpointBaseURL,
			EndpointDescription:         row.EndpointDescription,
			AuditEnabledAtRequest:       row.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: row.AuditCaptureBodiesAtRequest,
		},
		Usage: RequestLogDetailUsage{
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			TotalTokens:              row.TotalTokens,
			SuccessFlag:              row.SuccessFlag,
			BillableFlag:             row.BillableFlag,
			PricedFlag:               row.PricedFlag,
			UnpricedReason:           row.UnpricedReason,
			CacheReadInputTokens:     row.CacheReadInputTokens,
			CacheCreationInputTokens: row.CacheCreationInputTokens,
			ReasoningTokens:          row.ReasoningTokens,
		},
		Costing: RequestLogDetailCosting{
			InputCostMicros:              row.InputCostMicros,
			OutputCostMicros:             row.OutputCostMicros,
			CacheReadInputCostMicros:     row.CacheReadInputCostMicros,
			CacheCreationInputCostMicros: row.CacheCreationInputCostMicros,
			ReasoningCostMicros:          row.ReasoningCostMicros,
			TotalCostOriginalMicros:      row.TotalCostOriginalMicros,
			TotalCostUserCurrencyMicros:  row.TotalCostUserCurrencyMicros,
			CurrencyCodeOriginal:         row.CurrencyCodeOriginal,
			ReportCurrencyCode:           row.ReportCurrencyCode,
			ReportCurrencySymbol:         row.ReportCurrencySymbol,
			FXRateUsed:                   row.FXRateUsed,
			FXRateSource:                 row.FXRateSource,
		},
		Pricing: RequestLogDetailPricing{
			PricingSnapshotUnit:               row.PricingSnapshotUnit,
			PricingSnapshotInput:              row.PricingSnapshotInput,
			PricingSnapshotOutput:             row.PricingSnapshotOutput,
			PricingSnapshotCacheReadInput:     row.PricingSnapshotCacheReadInput,
			PricingSnapshotCacheCreationInput: row.PricingSnapshotCacheCreationInput,
			PricingSnapshotReasoning:          row.PricingSnapshotReasoning,
			PricingConfigVersionUsed:          row.PricingConfigVersionUsed,
		},
	}
	return response, nil
}

func buildRequestLogBrowseWhere(params RequestLogListParams) (string, []any) {
	clauses := []string{"profile_id = $1"}
	args := []any{params.ProfileID}
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		args = append(args, strings.TrimSpace(*params.IngressRequestID))
		clauses = append(clauses, fmt.Sprintf("ingress_request_id = $%d", len(args)))
	}
	if params.ModelID != nil && strings.TrimSpace(*params.ModelID) != "" {
		args = append(args, strings.TrimSpace(*params.ModelID))
		clauses = append(clauses, fmt.Sprintf("model_id = $%d", len(args)))
	}
	if params.StatusFamily != nil {
		switch strings.TrimSpace(strings.ToLower(*params.StatusFamily)) {
		case "4xx":
			clauses = append(clauses, "status_code BETWEEN 400 AND 499")
		case "5xx":
			clauses = append(clauses, "status_code BETWEEN 500 AND 599")
		}
	}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.EndpointID != nil {
		args = append(args, *params.EndpointID)
		clauses = append(clauses, fmt.Sprintf("endpoint_id = $%d", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

func buildRequestLogEndpointOptions(currentEndpoints []endpointRecord, selectedEndpointID *int) []RequestLogFilterEndpointOption {
	items := make([]RequestLogFilterEndpointOption, 0, len(currentEndpoints)+1)
	currentIDs := map[int]struct{}{}
	for _, endpoint := range currentEndpoints {
		items = append(items, RequestLogFilterEndpointOption{
			EndpointID:    endpoint.ID,
			EndpointLabel: resolveEndpointLabel(endpoint.Name, endpoint.BaseURL, nil, &endpoint.ID, "Unknown Endpoint"),
		})
		currentIDs[endpoint.ID] = struct{}{}
	}
	if selectedEndpointID == nil {
		return items
	}
	if _, ok := currentIDs[*selectedEndpointID]; ok {
		return items
	}
	return append([]RequestLogFilterEndpointOption{{EndpointID: *selectedEndpointID, EndpointLabel: fmt.Sprintf("Endpoint %d", *selectedEndpointID)}}, items...)
}

func loadRequestLogModels(ctx context.Context, exec queryExecutor, profileID int) ([]requestLogModelRecord, map[string]requestLogModelRecord, error) {
	rows, err := exec.Query(ctx, `SELECT model_id, display_name, model_type FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, nil, fmt.Errorf("query request-log models for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]requestLogModelRecord, 0)
	itemsByID := map[string]requestLogModelRecord{}
	for rows.Next() {
		var modelID string
		var displayName sql.NullString
		var modelType string
		if err := rows.Scan(&modelID, &displayName, &modelType); err != nil {
			return nil, nil, fmt.Errorf("scan request-log model record: %w", err)
		}
		trimmedModelID := strings.TrimSpace(modelID)
		if trimmedModelID == "" {
			continue
		}
		item := requestLogModelRecord{
			ModelID:     trimmedModelID,
			DisplayName: normalizeOptionalString(nullableString(displayName)),
			ModelType:   strings.TrimSpace(strings.ToLower(modelType)),
		}
		items = append(items, item)
		if _, exists := itemsByID[item.ModelID]; !exists {
			itemsByID[item.ModelID] = item
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate request-log models for profile %d: %w", profileID, err)
	}
	return items, itemsByID, nil
}

func buildRequestLogModelOptions(currentModels []requestLogModelRecord, selectedModelID *string) []RequestLogFilterModelOption {
	items := make([]RequestLogFilterModelOption, 0, len(currentModels)+1)
	currentIDs := map[string]struct{}{}
	for _, model := range currentModels {
		if _, exists := currentIDs[model.ModelID]; exists {
			continue
		}
		items = append(items, RequestLogFilterModelOption{
			ModelID:    model.ModelID,
			ModelLabel: requestLogModelLabel(model),
		})
		currentIDs[model.ModelID] = struct{}{}
	}
	selected := normalizeOptionalString(selectedModelID)
	if selected == nil {
		return items
	}
	if _, ok := currentIDs[*selected]; ok {
		return items
	}
	return append([]RequestLogFilterModelOption{{ModelID: *selected, ModelLabel: *selected}}, items...)
}

func requestLogModelLabel(model requestLogModelRecord) string {
	if model.DisplayName != nil && strings.TrimSpace(*model.DisplayName) != "" {
		return strings.TrimSpace(*model.DisplayName)
	}
	return strings.TrimSpace(model.ModelID)
}

func resolveRequestLogModelLabel(currentModelsByID map[string]requestLogModelRecord, modelID string) string {
	trimmedModelID := strings.TrimSpace(modelID)
	if currentModel, ok := currentModelsByID[trimmedModelID]; ok {
		return requestLogModelLabel(currentModel)
	}
	return trimmedModelID
}

func resolveRequestLogResolvedTargetModelLabel(currentModelsByID map[string]requestLogModelRecord, resolvedTargetModelID *string) *string {
	resolvedTarget := normalizeOptionalString(resolvedTargetModelID)
	if resolvedTarget == nil {
		return nil
	}
	label := resolveRequestLogModelLabel(currentModelsByID, *resolvedTarget)
	return &label
}

func resolveRequestLogIsProxyOrigin(currentModelsByID map[string]requestLogModelRecord, modelID string, resolvedTargetModelID *string) bool {
	trimmedModelID := strings.TrimSpace(modelID)
	resolvedTarget := normalizeOptionalString(resolvedTargetModelID)
	if resolvedTarget != nil && *resolvedTarget != trimmedModelID {
		return true
	}
	currentModel, ok := currentModelsByID[trimmedModelID]
	return ok && currentModel.ModelType == "proxy"
}

func endpointFromMap(items map[int]endpointRecord, endpointID *int) (endpointRecord, bool) {
	if endpointID == nil {
		return endpointRecord{}, false
	}
	item, ok := items[*endpointID]
	return item, ok
}

func loadRequestLogDetailRow(ctx context.Context, exec queryExecutor, profileID int, requestID int) (requestLogDetailRow, bool, error) {
	row := exec.QueryRow(
		ctx,
		`SELECT profile_id, id, created_at, model_id, resolved_target_model_id, api_family, vendor_id, vendor_key, vendor_name, status_code, response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, stream_error_kind, stream_error_detail, request_path, ingress_request_id, attempt_number, provider_correlation_id, proxy_api_key_id, proxy_api_key_name_snapshot, caller_user_agent, upstream_user_agent, error_detail, request_generation_params, request_generation_params_status, endpoint_id, connection_id, endpoint_base_url, endpoint_description, audit_enabled_at_request, audit_capture_bodies_at_request, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used
		 FROM request_logs
		 WHERE profile_id = $1 AND id = $2
		 LIMIT 1`,
		profileID,
		requestID,
	)
	record, err := scanRequestLogDetailRow(row)
	if err == pgx.ErrNoRows {
		return requestLogDetailRow{}, false, nil
	}
	if err != nil {
		return requestLogDetailRow{}, false, fmt.Errorf("load request log %d for profile %d: %w", requestID, profileID, err)
	}
	return record, true, nil
}

func scanRequestLogListRow(scanner interface{ Scan(...any) error }) (requestLogListRow, error) {
	var resolvedTargetModelID sql.NullString
	var vendorID sql.NullInt32
	var vendorKey sql.NullString
	var vendorName sql.NullString
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var ttftMS sql.NullInt32
	var completionDurationMS sql.NullInt32
	var streamOutcome sql.NullString
	var streamErrorKind sql.NullString
	var outputTokens sql.NullInt32
	var totalTokens sql.NullInt32
	var totalCostUserCurrencyMicros sql.NullInt64
	var pricedFlag sql.NullBool
	var unpricedReason sql.NullString
	var reasoningEffort sql.NullString
	var reportCurrencySymbol sql.NullString
	var callerUserAgent sql.NullString
	var upstreamUserAgent sql.NullString
	var endpointBaseURL sql.NullString
	item := requestLogListRow{}
	if err := scanner.Scan(&item.ID, &item.CreatedAt, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &vendorID, &vendorKey, &vendorName, &endpointID, &connectionID, &item.StatusCode, &item.ResponseTimeMS, &ttftMS, &completionDurationMS, &item.IsStream, &streamOutcome, &streamErrorKind, &outputTokens, &totalTokens, &totalCostUserCurrencyMicros, &pricedFlag, &unpricedReason, &reasoningEffort, &reportCurrencySymbol, &callerUserAgent, &upstreamUserAgent, &endpointBaseURL); err != nil {
		return requestLogListRow{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	item.VendorID = nullableInt32(vendorID)
	item.VendorKey = nullableString(vendorKey)
	item.VendorName = nullableString(vendorName)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.TTFTMS = nullableInt32(ttftMS)
	item.CompletionDurationMS = nullableInt32(completionDurationMS)
	item.StreamOutcome = normalizeRequestLogStreamOutcome(nullableString(streamOutcome), item.IsStream, item.CompletionDurationMS)
	item.StreamErrorKind = normalizeOptionalString(nullableString(streamErrorKind))
	item.OutputTokens = nullableInt32(outputTokens)
	item.TotalTokens = nullableInt32(totalTokens)
	item.TotalCostUserCurrencyMicros = nullableInt64(totalCostUserCurrencyMicros)
	item.PricedFlag = nullableBool(pricedFlag)
	item.UnpricedReason = nullableString(unpricedReason)
	item.ReasoningEffort = nullableString(reasoningEffort)
	item.ReportCurrencySymbol = nullableString(reportCurrencySymbol)
	item.CallerUserAgent = nullableString(callerUserAgent)
	item.UpstreamUserAgent = nullableString(upstreamUserAgent)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
	item = normalizeRequestLogListSpendState(item)
	return item, nil
}

func scanRequestLogDetailRow(scanner interface{ Scan(...any) error }) (requestLogDetailRow, error) {
	var resolvedTargetModelID sql.NullString
	var vendorID sql.NullInt32
	var vendorKey sql.NullString
	var vendorName sql.NullString
	var ttftMS sql.NullInt32
	var completionDurationMS sql.NullInt32
	var streamOutcome sql.NullString
	var streamErrorKind sql.NullString
	var streamErrorDetail sql.NullString
	var ingressRequestID sql.NullString
	var attemptNumber sql.NullInt32
	var providerCorrelationID sql.NullString
	var proxyAPIKeyID sql.NullInt32
	var proxyAPIKeyNameSnapshot sql.NullString
	var callerUserAgent sql.NullString
	var upstreamUserAgent sql.NullString
	var errorDetail sql.NullString
	var requestGenerationParams []byte
	var requestGenerationParamsStatus sql.NullString
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var endpointBaseURL sql.NullString
	var endpointDescription sql.NullString
	var inputTokens sql.NullInt32
	var outputTokens sql.NullInt32
	var totalTokens sql.NullInt32
	var successFlag sql.NullBool
	var billableFlag sql.NullBool
	var pricedFlag sql.NullBool
	var unpricedReason sql.NullString
	var cacheReadInputTokens sql.NullInt32
	var cacheCreationInputTokens sql.NullInt32
	var reasoningTokens sql.NullInt32
	var inputCostMicros sql.NullInt64
	var outputCostMicros sql.NullInt64
	var cacheReadInputCostMicros sql.NullInt64
	var cacheCreationInputCostMicros sql.NullInt64
	var reasoningCostMicros sql.NullInt64
	var totalCostOriginalMicros sql.NullInt64
	var totalCostUserCurrencyMicros sql.NullInt64
	var currencyCodeOriginal sql.NullString
	var reportCurrencyCode sql.NullString
	var reportCurrencySymbol sql.NullString
	var fxRateUsed sql.NullString
	var fxRateSource sql.NullString
	var pricingSnapshotUnit sql.NullString
	var pricingSnapshotInput sql.NullString
	var pricingSnapshotOutput sql.NullString
	var pricingSnapshotCacheReadInput sql.NullString
	var pricingSnapshotCacheCreationInput sql.NullString
	var pricingSnapshotReasoning sql.NullString
	var pricingConfigVersionUsed sql.NullInt32
	item := requestLogDetailRow{}
	if err := scanner.Scan(&item.ProfileID, &item.ID, &item.CreatedAt, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &vendorID, &vendorKey, &vendorName, &item.StatusCode, &item.ResponseTimeMS, &ttftMS, &completionDurationMS, &item.IsStream, &streamOutcome, &streamErrorKind, &streamErrorDetail, &item.RequestPath, &ingressRequestID, &attemptNumber, &providerCorrelationID, &proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &callerUserAgent, &upstreamUserAgent, &errorDetail, &requestGenerationParams, &requestGenerationParamsStatus, &endpointID, &connectionID, &endpointBaseURL, &endpointDescription, &item.AuditEnabledAtRequest, &item.AuditCaptureBodiesAtRequest, &inputTokens, &outputTokens, &totalTokens, &successFlag, &billableFlag, &pricedFlag, &unpricedReason, &cacheReadInputTokens, &cacheCreationInputTokens, &reasoningTokens, &inputCostMicros, &outputCostMicros, &cacheReadInputCostMicros, &cacheCreationInputCostMicros, &reasoningCostMicros, &totalCostOriginalMicros, &totalCostUserCurrencyMicros, &currencyCodeOriginal, &reportCurrencyCode, &reportCurrencySymbol, &fxRateUsed, &fxRateSource, &pricingSnapshotUnit, &pricingSnapshotInput, &pricingSnapshotOutput, &pricingSnapshotCacheReadInput, &pricingSnapshotCacheCreationInput, &pricingSnapshotReasoning, &pricingConfigVersionUsed); err != nil {
		return requestLogDetailRow{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	item.VendorID = nullableInt32(vendorID)
	item.VendorKey = nullableString(vendorKey)
	item.VendorName = nullableString(vendorName)
	item.TTFTMS = nullableInt32(ttftMS)
	item.CompletionDurationMS = nullableInt32(completionDurationMS)
	item.StreamOutcome = normalizeRequestLogStreamOutcome(nullableString(streamOutcome), item.IsStream, item.CompletionDurationMS)
	item.StreamErrorKind = normalizeOptionalString(nullableString(streamErrorKind))
	item.StreamErrorDetail = normalizeOptionalString(nullableString(streamErrorDetail))
	item.IngressRequestID = nullableString(ingressRequestID)
	item.AttemptNumber = nullableInt32(attemptNumber)
	item.ProviderCorrelationID = nullableString(providerCorrelationID)
	item.ProxyAPIKeyID = nullableInt32(proxyAPIKeyID)
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.CallerUserAgent = nullableString(callerUserAgent)
	item.UpstreamUserAgent = nullableString(upstreamUserAgent)
	item.ErrorDetail = nullableString(errorDetail)
	item.RequestGenerationParams = nullableJSONRawMessage(requestGenerationParams)
	item.RequestGenerationParamsStatus = nullableString(requestGenerationParamsStatus)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
	item.EndpointDescription = nullableString(endpointDescription)
	item.InputTokens = nullableInt32(inputTokens)
	item.OutputTokens = nullableInt32(outputTokens)
	item.TotalTokens = nullableInt32(totalTokens)
	item.SuccessFlag = nullableBool(successFlag)
	item.BillableFlag = nullableBool(billableFlag)
	item.PricedFlag = nullableBool(pricedFlag)
	item.UnpricedReason = nullableString(unpricedReason)
	item.CacheReadInputTokens = nullableInt32(cacheReadInputTokens)
	item.CacheCreationInputTokens = nullableInt32(cacheCreationInputTokens)
	item.ReasoningTokens = nullableInt32(reasoningTokens)
	item.InputCostMicros = nullableInt64(inputCostMicros)
	item.OutputCostMicros = nullableInt64(outputCostMicros)
	item.CacheReadInputCostMicros = nullableInt64(cacheReadInputCostMicros)
	item.CacheCreationInputCostMicros = nullableInt64(cacheCreationInputCostMicros)
	item.ReasoningCostMicros = nullableInt64(reasoningCostMicros)
	item.TotalCostOriginalMicros = nullableInt64(totalCostOriginalMicros)
	item.TotalCostUserCurrencyMicros = nullableInt64(totalCostUserCurrencyMicros)
	item.CurrencyCodeOriginal = nullableString(currencyCodeOriginal)
	item.ReportCurrencyCode = nullableString(reportCurrencyCode)
	item.ReportCurrencySymbol = nullableString(reportCurrencySymbol)
	item.FXRateUsed = nullableString(fxRateUsed)
	item.FXRateSource = nullableString(fxRateSource)
	item.PricingSnapshotUnit = nullableString(pricingSnapshotUnit)
	item.PricingSnapshotInput = nullableString(pricingSnapshotInput)
	item.PricingSnapshotOutput = nullableString(pricingSnapshotOutput)
	item.PricingSnapshotCacheReadInput = nullableString(pricingSnapshotCacheReadInput)
	item.PricingSnapshotCacheCreationInput = nullableString(pricingSnapshotCacheCreationInput)
	item.PricingSnapshotReasoning = nullableString(pricingSnapshotReasoning)
	item.PricingConfigVersionUsed = nullableInt32(pricingConfigVersionUsed)
	item = normalizeRequestLogDetailSpendState(item)
	return item, nil
}

func normalizeRequestLogStreamOutcome(value *string, isStream bool, completionDurationMS *int) string {
	if normalized := normalizeOptionalString(value); normalized != nil {
		return *normalized
	}
	if !isStream {
		return "not_streaming"
	}
	if completionDurationMS != nil {
		return "completed"
	}
	return "unknown"
}

func nullableJSONRawMessage(raw []byte) *json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	message := json.RawMessage(append([]byte(nil), raw...))
	return &message
}

func normalizeRequestLogListSpendState(item requestLogListRow) requestLogListRow {
	item.PricedFlag, item.UnpricedReason = normalizeObservedSpendCoherence(successfulStatusCode(item.StatusCode), item.PricedFlag, item.UnpricedReason, item.TotalCostUserCurrencyMicros != nil)
	return item
}

func normalizeRequestLogDetailSpendState(item requestLogDetailRow) requestLogDetailRow {
	success := successfulStatusCode(item.StatusCode)
	if item.SuccessFlag != nil {
		success = *item.SuccessFlag
	}
	item.PricedFlag, item.UnpricedReason = normalizeObservedSpendCoherence(success, item.PricedFlag, item.UnpricedReason, item.TotalCostUserCurrencyMicros != nil)
	item.FXRateUsed, item.FXRateSource = normalizeObservedFXCoherence(success, item.PricedFlag, item.UnpricedReason, item.TotalCostUserCurrencyMicros != nil, item.CurrencyCodeOriginal, item.ReportCurrencyCode, item.FXRateUsed, item.FXRateSource)
	return item
}

func successfulStatusCode(statusCode int) bool {
	return statusCode >= 200 && statusCode <= 299
}

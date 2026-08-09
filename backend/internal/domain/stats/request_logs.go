package stats

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type requestLogListRow struct {
	ID                          int
	CreatedAt                   time.Time
	ModelID                     string
	ResolvedTargetModelID       *string
	APIFamily                   string
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
	ProxyAPIKeyID               *int
	ProxyAPIKeyNameSnapshot     *string
	ProxyKeyAttributionState    string
	ProxyKeyAuthEnforcedAtReq   *bool
}

type requestLogDetailRow struct {
	ProfileID                         int
	ID                                int
	CreatedAt                         time.Time
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	StatusCode                        int
	ResponseTimeMS                    int
	TTFTMS                            *int
	CompletionDurationMS              *int
	IsStream                          bool
	StreamOutcome                     string
	StreamErrorKind                   *string
	StreamErrorDetail                 *string
	OperationName                     *string
	UpstreamOperationName             *string
	OperationTranslationMode          *string
	RequestPath                       string
	UpstreamRequestPath               *string
	IngressRequestID                  *string
	AttemptNumber                     *int
	ProviderCorrelationID             *string
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
	ProxyKeyAttributionState          string
	ProxyKeyAuthEnforcedAtRequest     *bool
	CallerUserAgent                   *string
	UpstreamUserAgent                 *string
	ErrorDetail                       *string
	RequestGenerationParams           *json.RawMessage
	RequestGenerationParamsStatus     *string
	EndpointID                        *int
	ConnectionID                      *int
	SelectedTerminalTargetID          *int
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
	if params.ClientRuleID != nil {
		rule, found, ruleErr := loadCompiledUserAgentRuleByID(ctx, exec, params.ProfileID, *params.ClientRuleID)
		if ruleErr != nil {
			return RequestLogListResponse{}, ruleErr
		}
		if !found {
			return RequestLogListResponse{}, &HTTPError{StatusCode: 400, Detail: "invalid client_rule_id"}
		}
		params.ClientRulePattern = &rule.RawPattern
	}
	whereClause, args := buildRequestLogBrowseWhere(params)
	// ponytail: 全量 COUNT，日志量上万后换估算或 keyset 分页
	countQuery := `SELECT COUNT(*) FROM request_logs WHERE ` + whereClause
	var total int
	if err := exec.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return RequestLogListResponse{}, fmt.Errorf("count request logs for profile %d: %w", params.ProfileID, err)
	}
	rows, err := exec.Query(
		ctx,
		`SELECT id, created_at, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, status_code, response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, stream_error_kind, output_tokens, total_tokens, total_cost_user_currency_micros, priced_flag, unpriced_reason, request_generation_params #>> '{reasoning,effort}', report_currency_symbol, caller_user_agent, upstream_user_agent, endpoint_base_url, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request
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
			APIFamily:                   item.APIFamily,
			EndpointID:                  item.EndpointID,
			EndpointLabel:               resolveEndpointLabel(currentEndpoint.Name, currentEndpoint.BaseURL, item.EndpointBaseURL, item.EndpointID, "Unknown Endpoint"),
			ConnectionID:                item.ConnectionID,
			TerminalTargetID:            item.ConnectionID,
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
			ProxyAPIKeyID:               item.ProxyAPIKeyID,
			ProxyAPIKeyNameSnapshot:     item.ProxyAPIKeyNameSnapshot,
			ProxyKeyAttributionState:    item.ProxyKeyAttributionState,
			ProxyKeyAuthEnforcedAtReq:   item.ProxyKeyAuthEnforcedAtReq,
		})
	}
	if err := rows.Err(); err != nil {
		return RequestLogListResponse{}, fmt.Errorf("iterate request logs for profile %d: %w", params.ProfileID, err)
	}
	return RequestLogListResponse{
		FilterOptions: RequestLogListFilterOptions{
			Endpoints:            buildRequestLogEndpointOptions(currentEndpoints, params.EndpointID),
			Models:               buildRequestLogModelOptions(currentModels, params.ModelID),
			ResolvedTargetModels: buildRequestLogResolvedTargetModelOptions(currentModels, params.ResolvedTargetModelID),
			Clients:              buildRequestLogClientOptions(rules),
		},
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

// ListRequestLogChains returns the ingress-chains view: outer pages of ingress
// groups selected by the ordinary filters, with a parameterized EXISTS matching
// at least one retained row snapshot when the proxy_api_key_id filter is set.
// Each item carries the bounded full retained chain for that ingress; nested
// rows indicate which row matched the filter without discarding non-matching
// diagnostic/attempt context.
func ListRequestLogChains(ctx context.Context, exec queryExecutor, params RequestLogListParams) (RequestLogChainResponse, error) {
	chainLimit := 20
	if params.ChainLimit != nil && *params.ChainLimit > 0 {
		chainLimit = *params.ChainLimit
	}
	if chainLimit > 50 {
		chainLimit = 50
	}
	currentEndpoints, currentEndpointsByID, err := loadCurrentEndpoints(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogChainResponse{}, err
	}
	currentModels, currentModelsByID, err := loadRequestLogModels(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogChainResponse{}, err
	}
	rules, err := loadCompiledUserAgentRules(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogChainResponse{}, err
	}

	whereClause, args := buildRequestLogBrowseWhere(params)
	// The chain outer selection uses EXISTS over the same filter predicate:
	// an ingress group is selected when at least one retained row matches.
	// The predicate is applied server-side before any chain pagination.
	existsClause := `EXISTS (SELECT 1 FROM request_logs matched WHERE matched.profile_id = request_logs.profile_id AND matched.ingress_request_id = request_logs.ingress_request_id AND ` + whereClause + `)`
	existsArgs := args

	var retainedIngressTotal int
	countQuery := `SELECT COUNT(DISTINCT ingress_request_id) FROM request_logs WHERE ` + whereClause
	if err := exec.QueryRow(ctx, countQuery, args...).Scan(&retainedIngressTotal); err != nil {
		return RequestLogChainResponse{}, fmt.Errorf("count retained ingress groups for profile %d: %w", params.ProfileID, err)
	}

	// Keyset pagination: order ingress groups by their last retained row.
	// The cursor encodes (last_seen, ingress_request_id) as an opaque string.
	var cursorLastSeen *time.Time
	var cursorIngress *string
	if params.ChainCursor != nil && *params.ChainCursor != "" {
		decoded, err := decodeRequestLogChainCursor(*params.ChainCursor)
		if err != nil {
			return RequestLogChainResponse{}, &HTTPError{StatusCode: 400, Detail: "invalid chain_cursor"}
		}
		cursorLastSeen = &decoded.LastSeen
		cursorIngress = &decoded.IngressRequestID
	}
	outerArgs := append([]any(nil), existsArgs...)
	cursorClause := ""
	if cursorLastSeen != nil && cursorIngress != nil {
		outerArgs = append(outerArgs, cursorLastSeen.UTC(), *cursorIngress)
		cursorClause = fmt.Sprintf("AND (EXISTS (SELECT 1 FROM request_logs g WHERE g.profile_id = request_logs.profile_id AND g.ingress_request_id = request_logs.ingress_request_id AND g.created_at < $%d) OR (NOT EXISTS (SELECT 1 FROM request_logs g2 WHERE g2.profile_id = request_logs.profile_id AND g2.ingress_request_id = request_logs.ingress_request_id AND g2.created_at < $%d) AND request_logs.ingress_request_id < $%d))", len(outerArgs)-1, len(outerArgs)-1, len(outerArgs))
	}

	_ = existsClause
	_ = outerArgs
	_ = cursorClause

	rows, err := exec.Query(ctx, `SELECT ingress_request_id, MIN(created_at) AS first_seen, MAX(created_at) AS last_seen
		 FROM request_logs
		 WHERE `+whereClause+`
		 GROUP BY ingress_request_id
		 ORDER BY last_seen DESC, ingress_request_id DESC
		 LIMIT `+fmt.Sprintf("%d", chainLimit+1), args...)
	if err != nil {
		return RequestLogChainResponse{}, fmt.Errorf("query request log chains for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()

	type ingressGroup struct {
		IngressRequestID string
		FirstSeen        time.Time
		LastSeen         time.Time
	}
	groups := make([]ingressGroup, 0, chainLimit+1)
	for rows.Next() {
		var group ingressGroup
		if err := rows.Scan(&group.IngressRequestID, &group.FirstSeen, &group.LastSeen); err != nil {
			return RequestLogChainResponse{}, fmt.Errorf("scan request log chain group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return RequestLogChainResponse{}, fmt.Errorf("iterate request log chain groups: %w", err)
	}

	hasMore := len(groups) > chainLimit
	if hasMore {
		groups = groups[:chainLimit]
	}

	items := make([]RequestLogChainItem, 0, len(groups))
	for _, group := range groups {
		chainRows, chainTotal, err := loadRequestLogChainRows(ctx, exec, params, group.IngressRequestID, currentEndpointsByID, currentModelsByID, rules)
		if err != nil {
			return RequestLogChainResponse{}, err
		}
		matched := 0
		for _, row := range chainRows {
			if row.MatchedByFilter {
				matched++
			}
		}
		items = append(items, RequestLogChainItem{
			IngressRequestID: group.IngressRequestID,
			FirstSeenAt:      group.FirstSeen.UTC(),
			LastSeenAt:       group.LastSeen.UTC(),
			RetainedRowCount: chainTotal,
			MatchedRowCount:  matched,
			Rows:             chainRows,
			RowsLoadedCount:  len(chainRows),
			RowsPageComplete: len(chainRows) == chainTotal,
		})
	}

	response := RequestLogChainResponse{
		View:                 "ingress_chains",
		Items:                items,
		HasMoreChains:        hasMore,
		RetainedIngressTotal: retainedIngressTotal,
		FilterOptions: RequestLogListFilterOptions{
			Endpoints:            buildRequestLogEndpointOptions(currentEndpoints, params.EndpointID),
			Models:               buildRequestLogModelOptions(currentModels, params.ModelID),
			ResolvedTargetModels: buildRequestLogResolvedTargetModelOptions(currentModels, params.ResolvedTargetModelID),
			Clients:              buildRequestLogClientOptions(rules),
		},
	}
	if hasMore {
		last := groups[len(groups)-1]
		cursor := encodeRequestLogChainCursor(requestLogChainCursor{LastSeen: last.LastSeen, IngressRequestID: last.IngressRequestID})
		response.NextChainCursor = &cursor
	}
	return response, nil
}

const requestLogChainRowPageLimit = 50

type requestLogChainCursor struct {
	LastSeen         time.Time
	IngressRequestID string
}

func encodeRequestLogChainCursor(cursor requestLogChainCursor) string {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeRequestLogChainCursor(value string) (requestLogChainCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return requestLogChainCursor{}, err
	}
	var cursor requestLogChainCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return requestLogChainCursor{}, err
	}
	return cursor, nil
}

func loadRequestLogChainRows(ctx context.Context, exec queryExecutor, params RequestLogListParams, ingressRequestID string, currentEndpointsByID map[int]endpointRecord, currentModelsByID map[string]requestLogModelRecord, rules []compiledUserAgentRule) ([]RequestLogChainRow, int, error) {
	baseWhere, baseArgs := buildRequestLogBrowseWhere(params)
	matchedClause := `(` + baseWhere + `)`
	matchedArgs := append([]any{}, baseArgs...)

	// The full retained chain: all rows for this ingress in the resolved
	// window, plus a per-row matched flag. The key filter never discards
	// non-matching diagnostic/attempt context inside a selected chain.
	rows, err := exec.Query(ctx, `SELECT id, created_at, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, status_code, response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, stream_error_kind, output_tokens, total_tokens, total_cost_user_currency_micros, priced_flag, unpriced_reason, request_generation_params #>> '{reasoning,effort}', report_currency_symbol, caller_user_agent, upstream_user_agent, endpoint_base_url, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request,
		 CASE WHEN `+matchedClause+` THEN TRUE ELSE FALSE END AS matched_by_filter
		 FROM request_logs
		 WHERE profile_id = $`+fmt.Sprintf("%d", len(matchedArgs)+1)+` AND ingress_request_id = $`+fmt.Sprintf("%d", len(matchedArgs)+2)+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT `+fmt.Sprintf("%d", requestLogChainRowPageLimit),
		append(matchedArgs, params.ProfileID, ingressRequestID)...)
	if err != nil {
		return nil, 0, fmt.Errorf("query request log chain rows for ingress %q: %w", ingressRequestID, err)
	}
	defer rows.Close()

	chainRows := make([]RequestLogChainRow, 0)
	for rows.Next() {
		item, matched, scanErr := scanRequestLogChainRow(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		currentEndpoint, _ := endpointFromMap(currentEndpointsByID, item.EndpointID)
		chainRows = append(chainRows, RequestLogChainRow{
			RequestLogListItem: RequestLogListItem{
				ID:                          item.ID,
				CreatedAt:                   item.CreatedAt.UTC(),
				ModelID:                     item.ModelID,
				ModelLabel:                  resolveRequestLogModelLabel(currentModelsByID, item.ModelID),
				ResolvedTargetModelID:       item.ResolvedTargetModelID,
				ResolvedTargetModelLabel:    resolveRequestLogResolvedTargetModelLabel(currentModelsByID, item.ResolvedTargetModelID),
				APIFamily:                   item.APIFamily,
				EndpointID:                  item.EndpointID,
				EndpointLabel:               resolveEndpointLabel(currentEndpoint.Name, currentEndpoint.BaseURL, item.EndpointBaseURL, item.EndpointID, "Unknown Endpoint"),
				ConnectionID:                item.ConnectionID,
				TerminalTargetID:            item.ConnectionID,
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
				ProxyAPIKeyID:               item.ProxyAPIKeyID,
				ProxyAPIKeyNameSnapshot:     item.ProxyAPIKeyNameSnapshot,
				ProxyKeyAttributionState:    item.ProxyKeyAttributionState,
				ProxyKeyAuthEnforcedAtReq:   item.ProxyKeyAuthEnforcedAtReq,
			},
			MatchedByFilter: matched,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate request log chain rows: %w", err)
	}
	// Full retained count for this ingress inside the resolved window,
	// independent of the filter: the outer EXISTS selects the group, the
	// nested rows expose the bounded full chain.
	var chainTotal int
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2`, params.ProfileID, ingressRequestID).Scan(&chainTotal); err != nil {
		return nil, 0, fmt.Errorf("count request log chain rows for ingress %q: %w", ingressRequestID, err)
	}
	return chainRows, chainTotal, nil
}

func scanRequestLogChainRow(scanner interface{ Scan(...any) error }) (requestLogListRow, bool, error) {
	var resolvedTargetModelID sql.NullString
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
	var proxyAPIKeyID sql.NullInt32
	var proxyAPIKeyNameSnapshot sql.NullString
	var proxyKeyAttributionState sql.NullString
	var proxyKeyAuthEnforcedAtRequest sql.NullBool
	var matched sql.NullBool
	item := requestLogListRow{}
	if err := scanner.Scan(&item.ID, &item.CreatedAt, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &endpointID, &connectionID, &item.StatusCode, &item.ResponseTimeMS, &ttftMS, &completionDurationMS, &item.IsStream, &streamOutcome, &streamErrorKind, &outputTokens, &totalTokens, &totalCostUserCurrencyMicros, &pricedFlag, &unpricedReason, &reasoningEffort, &reportCurrencySymbol, &callerUserAgent, &upstreamUserAgent, &endpointBaseURL, &proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &proxyKeyAttributionState, &proxyKeyAuthEnforcedAtRequest, &matched); err != nil {
		return requestLogListRow{}, false, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
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
	item.ProxyAPIKeyID = nullableInt32(proxyAPIKeyID)
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.ProxyKeyAttributionState = stringValue(nullableString(proxyKeyAttributionState))
	if proxyKeyAuthEnforcedAtRequest.Valid {
		enforced := proxyKeyAuthEnforcedAtRequest.Bool
		item.ProxyKeyAuthEnforcedAtReq = &enforced
	}
	return item, matched.Valid && matched.Bool, nil
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
			APIFamily:                row.APIFamily,
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
			OperationName:                 row.OperationName,
			UpstreamOperationName:         row.UpstreamOperationName,
			OperationTranslationMode:      row.OperationTranslationMode,
			RequestPath:                   row.RequestPath,
			UpstreamRequestPath:           row.UpstreamRequestPath,
			IngressRequestID:              row.IngressRequestID,
			AttemptNumber:                 row.AttemptNumber,
			ProviderCorrelationID:         row.ProviderCorrelationID,
			ProxyAPIKeyID:                 row.ProxyAPIKeyID,
			ProxyAPIKeyNameSnapshot:       row.ProxyAPIKeyNameSnapshot,
			ProxyKeyAttributionState:      row.ProxyKeyAttributionState,
			ProxyKeyAuthEnforcedAtRequest: row.ProxyKeyAuthEnforcedAtRequest,
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
			TerminalTargetID:            row.ConnectionID,
			SelectedTerminalTargetID:    row.SelectedTerminalTargetID,
			EndpointBaseURL:             row.EndpointBaseURL,
			EndpointDescription:         row.EndpointDescription,
			AuditEnabledAtRequest:       row.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: row.AuditCaptureBodiesAtRequest,
		},
		Usage: requestLogDetailUsageFromRow(row),
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
		Pricing: requestLogDetailPricingFromRow(row),
	}
	return response, nil
}

func requestLogDetailUsageFromRow(row requestLogDetailRow) RequestLogDetailUsage {
	return RequestLogDetailUsage{
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
	}
}

func requestLogDetailPricingFromRow(row requestLogDetailRow) RequestLogDetailPricing {
	return RequestLogDetailPricing{
		PricingSnapshotUnit:               row.PricingSnapshotUnit,
		PricingSnapshotInput:              row.PricingSnapshotInput,
		PricingSnapshotOutput:             row.PricingSnapshotOutput,
		PricingSnapshotCacheReadInput:     row.PricingSnapshotCacheReadInput,
		PricingSnapshotCacheCreationInput: row.PricingSnapshotCacheCreationInput,
		PricingSnapshotReasoning:          row.PricingSnapshotReasoning,
		PricingConfigVersionUsed:          row.PricingConfigVersionUsed,
	}
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
	if params.ResolvedTargetModelID != nil && strings.TrimSpace(*params.ResolvedTargetModelID) != "" {
		args = append(args, strings.TrimSpace(*params.ResolvedTargetModelID))
		clauses = append(clauses, fmt.Sprintf("resolved_target_model_id = $%d", len(args)))
	}
	if params.StatusFamily != nil {
		switch strings.TrimSpace(strings.ToLower(*params.StatusFamily)) {
		case "2xx":
			clauses = append(clauses, "status_code BETWEEN 200 AND 299")
		case "4xx":
			clauses = append(clauses, "status_code BETWEEN 400 AND 499")
		case "5xx":
			clauses = append(clauses, "status_code BETWEEN 500 AND 599")
		}
	}
	if params.StatusCode != nil {
		args = append(args, *params.StatusCode)
		clauses = append(clauses, fmt.Sprintf("status_code = $%d", len(args)))
	}
	if params.ErrorText != nil && strings.TrimSpace(*params.ErrorText) != "" {
		args = append(args, "%"+strings.TrimSpace(*params.ErrorText)+"%")
		clauses = append(clauses, fmt.Sprintf("error_detail ILIKE $%d", len(args)))
	}
	if params.PricedFlag != nil {
		args = append(args, *params.PricedFlag)
		clauses = append(clauses, fmt.Sprintf("priced_flag = $%d", len(args)))
	}
	if params.UnpricedReason != nil && strings.TrimSpace(*params.UnpricedReason) != "" {
		args = append(args, strings.TrimSpace(*params.UnpricedReason))
		clauses = append(clauses, fmt.Sprintf("unpriced_reason = $%d", len(args)))
	}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", len(args)))
	}
	if params.EndpointID != nil {
		args = append(args, *params.EndpointID)
		clauses = append(clauses, fmt.Sprintf("endpoint_id = $%d", len(args)))
	}
	if params.ClientRulePattern != nil && strings.TrimSpace(*params.ClientRulePattern) != "" {
		args = append(args, strings.TrimSpace(*params.ClientRulePattern))
		clauses = append(clauses, fmt.Sprintf("caller_user_agent IS NOT NULL AND btrim(caller_user_agent) <> '' AND caller_user_agent ~* $%d", len(args)))
	}
	if params.ProxyAPIKeyID != nil {
		// Ordinary filter hits the immutable snapshot column before COUNT,
		// sort and pagination. Deleted/renamed keys keep matching via the
		// request-time snapshot identity.
		args = append(args, *params.ProxyAPIKeyID)
		clauses = append(clauses, fmt.Sprintf("proxy_api_key_id_snapshot = $%d", len(args)))
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
	rows, err := exec.Query(ctx, `SELECT model_id, display_name FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, nil, fmt.Errorf("query request-log models for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]requestLogModelRecord, 0)
	itemsByID := map[string]requestLogModelRecord{}
	for rows.Next() {
		var modelID string
		var displayName sql.NullString
		if err := rows.Scan(&modelID, &displayName); err != nil {
			return nil, nil, fmt.Errorf("scan request-log model record: %w", err)
		}
		trimmedModelID := strings.TrimSpace(modelID)
		if trimmedModelID == "" {
			continue
		}
		item := requestLogModelRecord{
			ModelID:     trimmedModelID,
			DisplayName: normalizeOptionalString(nullableString(displayName)),
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

func buildRequestLogResolvedTargetModelOptions(currentModels []requestLogModelRecord, selectedModelID *string) []RequestLogFilterResolvedTargetModelOption {
	modelOptions := buildRequestLogModelOptions(currentModels, selectedModelID)
	items := make([]RequestLogFilterResolvedTargetModelOption, 0, len(modelOptions))
	for _, model := range modelOptions {
		items = append(items, RequestLogFilterResolvedTargetModelOption{ResolvedTargetModelID: model.ModelID, ModelLabel: model.ModelLabel})
	}
	return items
}

func buildRequestLogClientOptions(rules []compiledUserAgentRule) []RequestLogFilterClientOption {
	items := make([]RequestLogFilterClientOption, 0, len(rules))
	seen := map[int]struct{}{}
	for _, rule := range rules {
		if rule.ID <= 0 {
			continue
		}
		if _, exists := seen[rule.ID]; exists {
			continue
		}
		items = append(items, RequestLogFilterClientOption{ClientRuleID: rule.ID, ClientLabel: rule.Name})
		seen[rule.ID] = struct{}{}
	}
	return items
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
		`SELECT profile_id, id, created_at, model_id, resolved_target_model_id, api_family, status_code, response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, stream_error_kind, stream_error_detail, operation_name, upstream_operation_name, operation_translation_mode, request_path, upstream_request_path, ingress_request_id, attempt_number, provider_correlation_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request, caller_user_agent, upstream_user_agent, error_detail, request_generation_params, request_generation_params_status, endpoint_id, connection_id, selected_terminal_target_id, endpoint_base_url, endpoint_description, audit_enabled_at_request, audit_capture_bodies_at_request, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used
		 FROM request_logs
		 WHERE profile_id = $1 AND id = $2
		 ORDER BY created_at DESC
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
	var proxyAPIKeyID sql.NullInt32
	var proxyAPIKeyNameSnapshot sql.NullString
	var proxyKeyAttributionState sql.NullString
	var proxyKeyAuthEnforcedAtRequest sql.NullBool
	item := requestLogListRow{}
	if err := scanner.Scan(&item.ID, &item.CreatedAt, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &endpointID, &connectionID, &item.StatusCode, &item.ResponseTimeMS, &ttftMS, &completionDurationMS, &item.IsStream, &streamOutcome, &streamErrorKind, &outputTokens, &totalTokens, &totalCostUserCurrencyMicros, &pricedFlag, &unpricedReason, &reasoningEffort, &reportCurrencySymbol, &callerUserAgent, &upstreamUserAgent, &endpointBaseURL, &proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &proxyKeyAttributionState, &proxyKeyAuthEnforcedAtRequest); err != nil {
		return requestLogListRow{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
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
	item.ProxyAPIKeyID = nullableInt32(proxyAPIKeyID)
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.ProxyKeyAttributionState = stringValue(nullableString(proxyKeyAttributionState))
	if proxyKeyAuthEnforcedAtRequest.Valid {
		enforced := proxyKeyAuthEnforcedAtRequest.Bool
		item.ProxyKeyAuthEnforcedAtReq = &enforced
	}
	return item, nil
}

func scanRequestLogDetailRow(scanner interface{ Scan(...any) error }) (requestLogDetailRow, error) {
	var resolvedTargetModelID sql.NullString
	var ttftMS sql.NullInt32
	var completionDurationMS sql.NullInt32
	var streamOutcome sql.NullString
	var streamErrorKind sql.NullString
	var streamErrorDetail sql.NullString
	var operationName sql.NullString
	var upstreamOperationName sql.NullString
	var operationTranslationMode sql.NullString
	var upstreamRequestPath sql.NullString
	var ingressRequestID sql.NullString
	var attemptNumber sql.NullInt32
	var providerCorrelationID sql.NullString
	var proxyAPIKeyID sql.NullInt32
	var proxyAPIKeyNameSnapshot sql.NullString
	var proxyKeyAttributionState sql.NullString
	var proxyKeyAuthEnforcedAtRequest sql.NullBool
	var callerUserAgent sql.NullString
	var upstreamUserAgent sql.NullString
	var errorDetail sql.NullString
	var requestGenerationParams []byte
	var requestGenerationParamsStatus sql.NullString
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var selectedTerminalTargetID sql.NullInt32
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
	if err := scanner.Scan(&item.ProfileID, &item.ID, &item.CreatedAt, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &item.StatusCode, &item.ResponseTimeMS, &ttftMS, &completionDurationMS, &item.IsStream, &streamOutcome, &streamErrorKind, &streamErrorDetail, &operationName, &upstreamOperationName, &operationTranslationMode, &item.RequestPath, &upstreamRequestPath, &ingressRequestID, &attemptNumber, &providerCorrelationID, &proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &proxyKeyAttributionState, &proxyKeyAuthEnforcedAtRequest, &callerUserAgent, &upstreamUserAgent, &errorDetail, &requestGenerationParams, &requestGenerationParamsStatus, &endpointID, &connectionID, &selectedTerminalTargetID, &endpointBaseURL, &endpointDescription, &item.AuditEnabledAtRequest, &item.AuditCaptureBodiesAtRequest, &inputTokens, &outputTokens, &totalTokens, &successFlag, &billableFlag, &pricedFlag, &unpricedReason, &cacheReadInputTokens, &cacheCreationInputTokens, &reasoningTokens, &inputCostMicros, &outputCostMicros, &cacheReadInputCostMicros, &cacheCreationInputCostMicros, &reasoningCostMicros, &totalCostOriginalMicros, &totalCostUserCurrencyMicros, &currencyCodeOriginal, &reportCurrencyCode, &reportCurrencySymbol, &fxRateUsed, &fxRateSource, &pricingSnapshotUnit, &pricingSnapshotInput, &pricingSnapshotOutput, &pricingSnapshotCacheReadInput, &pricingSnapshotCacheCreationInput, &pricingSnapshotReasoning, &pricingConfigVersionUsed); err != nil {
		return requestLogDetailRow{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	item.TTFTMS = nullableInt32(ttftMS)
	item.CompletionDurationMS = nullableInt32(completionDurationMS)
	item.StreamOutcome = normalizeRequestLogStreamOutcome(nullableString(streamOutcome), item.IsStream, item.CompletionDurationMS)
	item.StreamErrorKind = normalizeOptionalString(nullableString(streamErrorKind))
	item.StreamErrorDetail = normalizeOptionalString(nullableString(streamErrorDetail))
	item.OperationName = normalizeOptionalString(nullableString(operationName))
	item.UpstreamOperationName = normalizeOptionalString(nullableString(upstreamOperationName))
	item.OperationTranslationMode = normalizeOptionalString(nullableString(operationTranslationMode))
	item.UpstreamRequestPath = normalizeOptionalString(nullableString(upstreamRequestPath))
	item.IngressRequestID = nullableString(ingressRequestID)
	item.AttemptNumber = nullableInt32(attemptNumber)
	item.ProviderCorrelationID = nullableString(providerCorrelationID)
	item.ProxyAPIKeyID = nullableInt32(proxyAPIKeyID)
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.ProxyKeyAttributionState = stringValue(nullableString(proxyKeyAttributionState))
	if proxyKeyAuthEnforcedAtRequest.Valid {
		enforced := proxyKeyAuthEnforcedAtRequest.Bool
		item.ProxyKeyAuthEnforcedAtRequest = &enforced
	}
	item.CallerUserAgent = nullableString(callerUserAgent)
	item.UpstreamUserAgent = nullableString(upstreamUserAgent)
	item.ErrorDetail = nullableString(errorDetail)
	item.RequestGenerationParams = nullableJSONRawMessage(requestGenerationParams)
	item.RequestGenerationParamsStatus = nullableString(requestGenerationParamsStatus)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.SelectedTerminalTargetID = nullableInt32(selectedTerminalTargetID)
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

// ListProxyAPIKeyFilterOptions returns the searchable, bounded option source
// for the ordinary proxy_api_key_id filter. The source is the union of current
// configured keys and distinct immutable key snapshots retained in the
// resolved ordinary Requests window; filtering happens in SQL before limit.
func ListProxyAPIKeyFilterOptions(ctx context.Context, exec queryExecutor, params ProxyAPIKeyFilterOptionsParams) (ProxyAPIKeyFilterOptionsResponse, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := ""
	if params.Query != nil {
		query = strings.TrimSpace(*params.Query)
	}
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}

	var fromTime, toTime time.Time
	if params.FromTime != nil {
		fromTime = params.FromTime.UTC()
	} else {
		fromTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if params.ToTime != nil {
		toTime = params.ToTime.UTC()
	} else {
		toTime = time.Now().UTC().Add(24 * time.Hour)
	}

	// Union: current configured keys plus distinct snapshots retained in the
	// window. The latest in-window name snapshot resolves deleted identities;
	// current rows join for name/preview.
	rows, err := exec.Query(ctx, `SELECT option_key_id, resolved_name, configured, key_prefix FROM (
			SELECT
				distinct_snapshot.key_id AS option_key_id,
				current.name AS resolved_name,
				TRUE AS configured,
				current.key_prefix AS key_prefix
			FROM proxy_api_keys current
			JOIN (
				SELECT DISTINCT proxy_api_key_id_snapshot AS key_id
				FROM request_logs
				WHERE profile_id = $1 AND proxy_api_key_id_snapshot IS NOT NULL
					AND created_at >= $2 AND created_at <= $3
			) distinct_snapshot ON distinct_snapshot.key_id = current.id
			UNION
			SELECT
				snapshot.key_id AS option_key_id,
				snapshot.name_snapshot AS resolved_name,
				FALSE AS configured,
				NULL AS key_prefix
			FROM (
				SELECT DISTINCT ON (proxy_api_key_id_snapshot)
					proxy_api_key_id_snapshot AS key_id, proxy_api_key_name_snapshot AS name_snapshot
				FROM request_logs
				WHERE profile_id = $1 AND proxy_api_key_id_snapshot IS NOT NULL
					AND proxy_api_key_name_snapshot IS NOT NULL
					AND created_at >= $2 AND created_at <= $3
				ORDER BY proxy_api_key_id_snapshot, created_at DESC, id DESC
			) snapshot
			WHERE NOT EXISTS (SELECT 1 FROM proxy_api_keys current2 WHERE current2.id = snapshot.key_id)
		) options
		WHERE $4 = '' OR options.resolved_name ILIKE '%' || $4 || '%' OR options.option_key_id::text = $4
		ORDER BY resolved_name ASC, option_key_id ASC
		LIMIT `+fmt.Sprintf("%d", limit+1), params.ProfileID, fromTime, toTime, query)
	if err != nil {
		return ProxyAPIKeyFilterOptionsResponse{}, fmt.Errorf("query proxy api key filter options: %w", err)
	}
	defer rows.Close()

	items := make([]ProxyAPIKeyFilterOption, 0, limit+1)
	seen := map[int]struct{}{}
	for rows.Next() {
		var keyID int
		var name sql.NullString
		var configured bool
		var keyPrefix sql.NullString
		if err := rows.Scan(&keyID, &name, &configured, &keyPrefix); err != nil {
			return ProxyAPIKeyFilterOptionsResponse{}, fmt.Errorf("scan proxy api key filter option: %w", err)
		}
		if _, ok := seen[keyID]; ok {
			continue
		}
		seen[keyID] = struct{}{}
		displayName := strings.TrimSpace(stringValue(nullableString(name)))
		if displayName == "" {
			displayName = fmt.Sprintf("#%d", keyID)
		}
		item := ProxyAPIKeyFilterOption{
			ProxyAPIKeyID: keyID,
			Name:          displayName,
			Configured:    configured,
		}
		if configured && keyPrefix.Valid {
			preview := strings.TrimSpace(keyPrefix.String)
			item.KeyPreview = &preview
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ProxyAPIKeyFilterOptionsResponse{}, fmt.Errorf("iterate proxy api key filter options: %w", err)
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	response := ProxyAPIKeyFilterOptionsResponse{
		Items:            items,
		ResolvedFromTime: &fromTime,
		ResolvedToTime:   &toTime,
	}
	if hasMore {
		last := items[len(items)-1]
		cursor := encodeProxyAPIKeyOptionCursor(last.Name, last.ProxyAPIKeyID)
		response.NextCursor = &cursor
	}

	if params.SelectedID != nil {
		selected, selectedErr := loadProxyAPIKeyFilterOption(ctx, exec, params.ProfileID, *params.SelectedID, fromTime, toTime)
		if selectedErr != nil {
			return ProxyAPIKeyFilterOptionsResponse{}, selectedErr
		}
		response.Selected = selected
	}
	return response, nil
}

func encodeProxyAPIKeyOptionCursor(name string, keyID int) string {
	raw, err := json.Marshal([]any{name, keyID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// loadProxyAPIKeyFilterOption resolves a selected ID even when it falls
// outside the current page/search. Deleted rows fall back to the latest
// in-window name snapshot with configured=false; when no snapshot survives the
// option renders as #<id> rather than dropping the URL filter.
func loadProxyAPIKeyFilterOption(ctx context.Context, exec queryExecutor, profileID int, keyID int, fromTime time.Time, toTime time.Time) (*ProxyAPIKeyFilterOption, error) {
	var option ProxyAPIKeyFilterOption
	var currentName sql.NullString
	var currentPrefix sql.NullString
	if err := exec.QueryRow(ctx, `SELECT name, key_prefix FROM proxy_api_keys WHERE id = $1`, keyID).Scan(&currentName, &currentPrefix); err == nil {
		option = ProxyAPIKeyFilterOption{
			ProxyAPIKeyID: keyID,
			Name:          strings.TrimSpace(stringValue(nullableString(currentName))),
			Configured:    true,
		}
		if currentPrefix.Valid {
			preview := strings.TrimSpace(currentPrefix.String)
			option.KeyPreview = &preview
		}
		return &option, nil
	}

	var snapshotName sql.NullString
	if err := exec.QueryRow(ctx, `SELECT proxy_api_key_name_snapshot FROM request_logs WHERE profile_id = $1 AND proxy_api_key_id_snapshot = $2 AND created_at >= $3 AND created_at <= $4 AND proxy_api_key_name_snapshot IS NOT NULL ORDER BY created_at DESC LIMIT 1`, profileID, keyID, fromTime, toTime).Scan(&snapshotName); err == nil {
		option = ProxyAPIKeyFilterOption{
			ProxyAPIKeyID: keyID,
			Name:          strings.TrimSpace(stringValue(nullableString(snapshotName))),
			Configured:    false,
		}
		return &option, nil
	}

	option = ProxyAPIKeyFilterOption{
		ProxyAPIKeyID: keyID,
		Name:          fmt.Sprintf("#%d", keyID),
		Configured:    false,
	}
	return &option, nil
}

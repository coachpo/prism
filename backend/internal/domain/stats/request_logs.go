package stats

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type requestLogListRow struct {
	ID                             int64
	CreatedAt                      time.Time
	ModelID                        string
	ResolvedTargetModelID          *string
	APIFamily                      string
	EndpointID                     *int
	ConnectionID                   *int
	ProxyAPIKeyID                  *int
	ProxyAPIKeyNameSnapshot        *string
	ProxyAPIKeyAttributionState    string
	ProxyAPIKeyAuthEnforced        *bool
	IngressRequestID               *string
	RowKind                        string
	StatusCode                     *int
	UpstreamStatusCode             *int
	GatewayStatusCode              *int
	LegacyStatusCode               *int
	AttemptDurationMS              *int
	LegacyDurationMS               *int
	TTFTMS                         *int
	CompletionDurationMS           *int
	IsStream                       bool
	StreamOutcome                  string
	StreamErrorKind                *string
	AttemptNumber                  *int
	AttemptTrigger                 *string
	AttemptResult                  *string
	IsWinner                       *bool
	ErrorSource                    *string
	ErrorCode                      *string
	FailureStage                   *string
	FailureDetailPreview           *string
	FailureDetailSource            string
	FailureDetailPreviewTruncated  bool
	FailureDetailRedacted          bool
	OutputTokens                   *int
	TotalTokens                    *int
	TotalCostUserCurrencyMicros    *int64
	PricingStatus                  string
	UnpricedReason                 *string
	PricingResolutionKind          *string
	PricingEvidenceTrust           string
	PricingTemplateKind            *string
	PricingSelectionState          *string
	PricingCardRole                *string
	PricingSelectorThresholdTokens *int
	PricingSelectorBasisTokens     *int64
	ReasoningEffort                *string
	ReportCurrencySymbol           *string
	CallerUserAgent                *string
	UpstreamUserAgent              *string
	EndpointBaseURL                *string
}

type requestLogModelRecord struct {
	ModelID     string
	DisplayName *string
}

// requestLogSortColumns maps the attempt-view `sort_by` grammar onto sortable
// SQL. The HTTP parser rejects anything outside this table, so an unknown key
// never reaches the query as a silent created_at fallback.
var requestLogSortColumns = map[string]string{
	"created_at":                      "created_at",
	"display_status":                  "(" + scopedRequestLogStatusSQL + ")",
	"ttft_ms":                         "ttft_ms",
	"total_tokens":                    "total_tokens",
	"total_cost_user_currency_micros": "total_cost_user_currency_micros",
}

// requestLogOrderBy renders the attempt-view ORDER BY. Rows without a value for
// the selected key sort last in both directions: a non-stream row has no TTFT
// and an unpriced row has no cost, and neither should occupy the head of the
// list just because the key is NULL. created_at/id break ties so offset paging
// stays deterministic across pages.
func requestLogOrderBy(sortBy string, sortOrder string) string {
	direction := "DESC"
	if strings.EqualFold(strings.TrimSpace(sortOrder), "asc") {
		direction = "ASC"
	}
	column, ok := requestLogSortColumns[strings.ToLower(strings.TrimSpace(sortBy))]
	if !ok || column == "created_at" {
		return "ORDER BY created_at " + direction + ", id " + direction
	}
	return "ORDER BY " + column + " " + direction + " NULLS LAST, created_at DESC, id DESC"
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
	var coverage QueryCoverage
	var err error
	if params.Coverage != nil {
		coverage = *params.Coverage
	} else {
		coverage, err = resolveOrdinaryRequestLogCoverage(ctx, exec, params)
		if err != nil {
			return RequestLogListResponse{}, err
		}
	}
	// The coverage owner is authoritative for the effective query interval.
	// Keep the SQL predicate and the response projection on the same snapshot;
	// otherwise a caller could receive a complete-looking coverage object for
	// rows outside the current retention floor.
	fromTime := coverage.EffectiveFromTime
	toTime := coverage.EffectiveToTime
	params.FromTime = &fromTime
	params.ToTime = &toTime
	currentEndpoints, currentEndpointsByID, err := loadCurrentEndpoints(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogListResponse{}, err
	}
	currentModels, currentModelsByID, err := loadRequestLogModels(ctx, exec, params.ProfileID)
	if err != nil {
		return RequestLogListResponse{}, err
	}
	currentConnectionsByID, err := loadCurrentConnections(ctx, exec, params.ProfileID)
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
	// requestLogCountCap bounds the browse-count scan. Requests is the
	// highest-write table on the gateway; an exact count is not worth a scan
	// across every retained partition. Above the cap the response carries
	// total=cap with total_is_exact=false so the UI can render "10,000+".
	countQuery := `SELECT COUNT(*) FROM (SELECT 1 FROM request_logs WHERE ` + whereClause +
		` LIMIT $` + fmt.Sprintf("%d", len(args)+1) + `) AS bounded`
	var counted int
	if err := exec.QueryRow(ctx, countQuery, append(args, requestLogCountCap+1)...).Scan(&counted); err != nil {
		return RequestLogListResponse{}, fmt.Errorf("count request logs for profile %d: %w", params.ProfileID, err)
	}
	total := counted
	totalIsExact := true
	if counted > requestLogCountCap {
		total = requestLogCountCap
		totalIsExact = false
	}
	rows, err := exec.Query(
		ctx,
		`SELECT id, created_at, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id,
		 proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request,
		 ingress_request_id, row_kind,
		 upstream_status_code, gateway_status_code, legacy_status_code, `+scopedRequestLogStatusSQL+` AS scoped_status, attempt_duration_ms, legacy_duration_ms,
		 ttft_ms, completion_duration_ms, is_stream, stream_outcome, stream_error_kind,
		 attempt_number, attempt_trigger, attempt_result, is_winner, error_source, error_code, failure_stage,
		 error_detail, error_detail_redacted, error_detail_truncated, stream_error_detail, stream_error_detail_redacted, stream_error_detail_truncated,
		 output_tokens, total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, pricing_resolution_kind, pricing_evidence_trust,
		 pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_selector_threshold_tokens, pricing_selector_basis_tokens,
		 request_generation_params #>> '{reasoning,effort}', report_currency_symbol, caller_user_agent, upstream_user_agent, endpoint_base_url
		 FROM request_logs
		 WHERE `+whereClause+`
		 `+requestLogOrderBy(params.SortBy, params.SortOrder)+`
		 LIMIT $`+fmt.Sprintf("%d", len(args)+1)+` OFFSET $`+fmt.Sprintf("%d", len(args)+2),
		append(append(args, limit+1), offset)...,
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
		terminalTarget := resolveTerminalTargetProjection(currentConnectionsByID, item.ConnectionID)
		listItem := RequestLogListItem{
			RequestLogID:                  fmt.Sprintf("%d", item.ID),
			RowKind:                       item.RowKind,
			IngressRequestID:              item.IngressRequestID,
			AttemptNumber:                 item.AttemptNumber,
			AttemptTrigger:                item.AttemptTrigger,
			AttemptResult:                 item.AttemptResult,
			IsWinner:                      item.IsWinner,
			CreatedAt:                     item.CreatedAt.UTC(),
			ModelID:                       item.ModelID,
			ModelLabel:                    resolveRequestLogModelLabel(currentModelsByID, item.ModelID),
			ResolvedTargetModelID:         item.ResolvedTargetModelID,
			ResolvedTargetModelLabel:      resolveRequestLogResolvedTargetModelLabel(currentModelsByID, item.ResolvedTargetModelID),
			APIFamily:                     item.APIFamily,
			EndpointID:                    item.EndpointID,
			EndpointLabel:                 resolveEndpointLabel(currentEndpoint.Name, currentEndpoint.BaseURL, item.EndpointBaseURL, item.EndpointID, "Unknown Endpoint"),
			TerminalTargetID:              item.ConnectionID,
			ProxyAPIKeyID:                 item.ProxyAPIKeyID,
			ProxyAPIKeyNameSnapshot:       item.ProxyAPIKeyNameSnapshot,
			ProxyAPIKeyAttributionState:   item.ProxyAPIKeyAttributionState,
			ProxyAPIKeyAuthEnforced:       item.ProxyAPIKeyAuthEnforced,
			TerminalTargetLabel:           terminalTarget.Label,
			TerminalTargetConfigured:      terminalTarget.Configured,
			TerminalTargetOwnerModelID:    terminalTarget.OwnerModelID,
			StatusCode:                    item.StatusCode,
			UpstreamStatusCode:            item.UpstreamStatusCode,
			GatewayStatusCode:             item.GatewayStatusCode,
			LegacyStatusCode:              item.LegacyStatusCode,
			AttemptDurationMS:             item.AttemptDurationMS,
			LegacyDurationMS:              item.LegacyDurationMS,
			TTFTMS:                        item.TTFTMS,
			CompletionDurationMS:          item.CompletionDurationMS,
			IsStream:                      item.IsStream,
			StreamOutcome:                 item.StreamOutcome,
			StreamErrorKind:               item.StreamErrorKind,
			ErrorSource:                   item.ErrorSource,
			ErrorCode:                     item.ErrorCode,
			FailureStage:                  item.FailureStage,
			FailureDetailPreview:          item.FailureDetailPreview,
			FailureDetailSource:           item.FailureDetailSource,
			FailureDetailPreviewTruncated: item.FailureDetailPreviewTruncated,
			FailureDetailRedacted:         item.FailureDetailRedacted,
			OutputTokens:                  item.OutputTokens,
			TotalTokens:                   item.TotalTokens,
			TotalCostUserCurrencyMicros:   item.TotalCostUserCurrencyMicros,
			PricingStatus:                 item.PricingStatus,
			UnpricedReason:                item.UnpricedReason,
			PricingResolutionKind:         item.PricingResolutionKind,
			PricingEvidenceTrust:          item.PricingEvidenceTrust,
			ReasoningEffort:               item.ReasoningEffort,
			ReportCurrencySymbol:          item.ReportCurrencySymbol,
			CallerClientDisplay:           classifyUserAgentDisplay(item.CallerUserAgent, rules),
			UpstreamClientDisplay:         classifyUserAgentDisplay(item.UpstreamUserAgent, rules),
			UserAgentOverridden:           userAgentOverridden(item.CallerUserAgent, item.UpstreamUserAgent),
		}
		// Attach typed selector evidence after the stable list projection is
		// assembled so the generic list and detail projections share columns
		// without conflating selection state with card role.
		listItem.PricingTemplateKind = item.PricingTemplateKind
		listItem.PricingSelectionState = item.PricingSelectionState
		listItem.PricingCardRole = item.PricingCardRole
		listItem.PricingSelectorThresholdTokens = item.PricingSelectorThresholdTokens
		listItem.PricingSelectorBasisTokens = item.PricingSelectorBasisTokens
		items = append(items, listItem)
	}
	if err := rows.Err(); err != nil {
		return RequestLogListResponse{}, fmt.Errorf("iterate request logs for profile %d: %w", params.ProfileID, err)
	}
	hasMore := false
	if len(items) > limit {
		hasMore = true
		items = items[:limit]
	}
	return RequestLogListResponse{
		Coverage: coverage,
		FilterOptions: RequestLogListFilterOptions{
			Endpoints:            buildRequestLogEndpointOptions(currentEndpoints, params.EndpointID),
			Models:               buildRequestLogModelOptions(currentModels, params.ModelID),
			ResolvedTargetModels: buildRequestLogResolvedTargetModelOptions(currentModels, params.ResolvedTargetModelID),
			Clients:              buildRequestLogClientOptions(rules),
		},
		Items:        items,
		Total:        total,
		TotalIsExact: totalIsExact,
		HasMore:      hasMore,
		Limit:        limit,
		Offset:       offset,
	}, nil
}

const requestLogCountCap = 10000

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
			clauses = append(clauses, scopedRequestLogStatusSQL+" BETWEEN 200 AND 299")
		case "4xx":
			clauses = append(clauses, scopedRequestLogStatusSQL+" BETWEEN 400 AND 499")
		case "5xx":
			clauses = append(clauses, scopedRequestLogStatusSQL+" BETWEEN 500 AND 599")
		}
	}
	if params.StatusCode != nil {
		args = append(args, *params.StatusCode)
		clauses = append(clauses, fmt.Sprintf(scopedRequestLogStatusSQL+" = $%d", len(args)))
	}
	if params.ErrorText != nil && strings.TrimSpace(*params.ErrorText) != "" {
		args = append(args, "%"+strings.TrimSpace(*params.ErrorText)+"%")
		clauses = append(clauses, fmt.Sprintf("(error_detail ILIKE $%d OR error_code ILIKE $%d OR stream_error_detail ILIKE $%d OR stream_error_kind ILIKE $%d)", len(args), len(args), len(args), len(args)))
	}
	if params.PricingStatus != nil && strings.TrimSpace(*params.PricingStatus) != "" {
		args = append(args, strings.TrimSpace(*params.PricingStatus))
		clauses = append(clauses, fmt.Sprintf("pricing_status = $%d", len(args)))
	}
	if len(params.UnpricedReasons) > 0 {
		placeholders := make([]string, 0, len(params.UnpricedReasons))
		for _, reason := range params.UnpricedReasons {
			args = append(args, strings.TrimSpace(reason))
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		clauses = append(clauses, "unpriced_reason IN ("+strings.Join(placeholders, ",")+")")
	}
	if params.PricingCardRole != nil && strings.TrimSpace(*params.PricingCardRole) != "" {
		args = append(args, strings.TrimSpace(*params.PricingCardRole))
		clauses = append(clauses, fmt.Sprintf("pricing_card_role = $%d", len(args)))
	}
	if params.PricingSelectionState != nil && strings.TrimSpace(*params.PricingSelectionState) != "" {
		args = append(args, strings.TrimSpace(*params.PricingSelectionState))
		clauses = append(clauses, fmt.Sprintf("pricing_selection_state = $%d", len(args)))
	}
	if params.FromTime != nil {
		args = append(args, params.FromTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if params.ToTime != nil {
		args = append(args, params.ToTime.UTC())
		clauses = append(clauses, fmt.Sprintf("created_at < $%d", len(args)))
	}
	if params.IngressFinalResult != nil || params.ConfirmedFailover != nil || params.FinalResult != nil || params.FinalModelID != nil || params.FinalEndpointID != nil || params.FinalTerminalTargetID != nil || params.FinalPricingStatus != nil || len(params.FinalUnpricedReasons) > 0 || params.FinalReportingEpoch != nil {
		clauses = append(clauses, buildFinalizedCohortExistsClause(params, &args))
	}
	if params.EndpointID != nil {
		args = append(args, *params.EndpointID)
		clauses = append(clauses, fmt.Sprintf("endpoint_id = $%d", len(args)))
	}
	if params.TerminalTargetID != nil {
		args = append(args, *params.TerminalTargetID)
		clauses = append(clauses, fmt.Sprintf("connection_id = $%d", len(args)))
	}
	if params.ProxyAPIKeyID != nil {
		args = append(args, *params.ProxyAPIKeyID)
		clauses = append(clauses, fmt.Sprintf("proxy_api_key_id_snapshot = $%d", len(args)))
	}
	if params.ClientRulePattern != nil && strings.TrimSpace(*params.ClientRulePattern) != "" {
		args = append(args, strings.TrimSpace(*params.ClientRulePattern))
		clauses = append(clauses, fmt.Sprintf("caller_user_agent IS NOT NULL AND btrim(caller_user_agent) <> '' AND caller_user_agent ~* $%d", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

// resolveOrdinaryRequestLogCoverage resolves Requests against the Requests
// actual-coverage owner. Retention policy/floor rows only provide deletion
// boundaries; they never provide an actual lower bound for `all` or a
// complete claim for 7d/30d.
func resolveOrdinaryRequestLogCoverage(ctx context.Context, exec queryExecutor, params RequestLogListParams) (QueryCoverage, error) {
	referenceNow := time.Now().UTC()
	if !params.CoverageReferenceNow.IsZero() {
		referenceNow = params.CoverageReferenceNow.UTC()
	}
	source, err := LoadRetentionSourceProjection(ctx, exec, "request_logs", referenceNow)
	if err != nil {
		return QueryCoverage{}, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return QueryCoverage{}, &HTTPError{StatusCode: 503, Code: "request_log_purge_in_progress", Detail: "request logs are temporarily unavailable while retention cleanup is publishing"}
	}
	actual, err := LoadActualCoverageProjection(ctx, exec, source)
	if err != nil {
		return QueryCoverage{}, err
	}
	preset := params.CoveragePreset
	fromTime := params.CoverageRequestedFrom
	toTime := params.CoverageRequestedTo
	if fromTime == nil {
		fromTime = params.FromTime
	}
	if toTime == nil {
		toTime = params.ToTime
	}
	preset, fromTime, toTime, err = normalizeActualCoveragePreset(preset, fromTime, toTime, referenceNow)
	if err != nil {
		return QueryCoverage{}, err
	}
	bounds, err := ResolveQueryBoundsFromActualCoverage(preset, fromTime, toTime, referenceNow, source, actual)
	if err != nil {
		return QueryCoverage{}, err
	}
	return QueryCoverageFromActualBounds(bounds, source, actual), nil
}

// buildFinalizedCohortExistsClause selects ingresses whose authoritative
// finalized usage summary (usage_request_events) matches the Observe
// deep-link selectors. It is an EXISTS over the same ingress so retained
// request_logs rows of every visible row kind are returned for the cohort;
// the final cohort is never derived from retained rows themselves.
func buildFinalizedCohortExistsClause(params RequestLogListParams, args *[]any) string {
	clauses := []string{
		"ue.profile_id = request_logs.profile_id",
		"ue.ingress_request_id = request_logs.ingress_request_id",
	}
	if params.IngressFinalResult != nil && strings.TrimSpace(*params.IngressFinalResult) != "" {
		*args = append(*args, strings.TrimSpace(*params.IngressFinalResult))
		clauses = append(clauses, fmt.Sprintf("(%s) = $%d", finalizedUsageResultClassifierSQL, len(*args)))
	}
	if params.ConfirmedFailover != nil {
		*args = append(*args, *params.ConfirmedFailover)
		clauses = append(clauses, fmt.Sprintf("ue.failover_occurred = $%d", len(*args)))
	}
	if params.QueryContextFrom != nil {
		*args = append(*args, params.QueryContextFrom.UTC())
		clauses = append(clauses, fmt.Sprintf("ue.created_at >= $%d", len(*args)))
	}
	if params.QueryContextTo != nil {
		*args = append(*args, params.QueryContextTo.UTC())
		clauses = append(clauses, fmt.Sprintf("ue.created_at < $%d", len(*args)))
	}
	if params.FinalModelID != nil && strings.TrimSpace(*params.FinalModelID) != "" {
		*args = append(*args, strings.TrimSpace(*params.FinalModelID))
		clauses = append(clauses, fmt.Sprintf("ue.model_id = $%d", len(*args)))
	}
	if params.FinalEndpointID != nil {
		*args = append(*args, *params.FinalEndpointID)
		clauses = append(clauses, fmt.Sprintf("ue.endpoint_id = $%d", len(*args)))
	}
	if params.FinalTerminalTargetID != nil {
		*args = append(*args, *params.FinalTerminalTargetID)
		clauses = append(clauses, fmt.Sprintf("ue.connection_id = $%d", len(*args)))
	}
	if params.FinalPricingStatus != nil && strings.TrimSpace(*params.FinalPricingStatus) != "" {
		*args = append(*args, strings.TrimSpace(*params.FinalPricingStatus))
		clauses = append(clauses, fmt.Sprintf("ue.pricing_status = $%d", len(*args)))
	}
	if len(params.FinalUnpricedReasons) > 0 {
		placeholders := make([]string, 0, len(params.FinalUnpricedReasons))
		for _, reason := range params.FinalUnpricedReasons {
			*args = append(*args, strings.TrimSpace(reason))
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(*args)))
		}
		clauses = append(clauses, "ue.unpriced_reason IN ("+strings.Join(placeholders, ",")+")")
	}
	if params.FinalReportingEpoch != nil {
		if *params.FinalReportingEpoch == "__legacy_unknown__" {
			clauses = append(clauses, "ue.reporting_currency_epoch IS NULL")
		} else {
			*args = append(*args, *params.FinalReportingEpoch)
			clauses = append(clauses, fmt.Sprintf("ue.reporting_currency_epoch = $%d", len(*args)))
		}
	}
	if params.FinalResult != nil && strings.TrimSpace(*params.FinalResult) != "" {
		// Shared finalized classifier (Observe SPEC §3.2): final_result is
		// derived from the finalized usage row, never a stored column.
		*args = append(*args, strings.TrimSpace(*params.FinalResult))
		clauses = append(clauses, fmt.Sprintf("(%s) = $%d", finalizedUsageResultClassifierSQL, len(*args)))
	}
	return "EXISTS (SELECT 1 FROM usage_request_events ue WHERE " + strings.Join(clauses, " AND ") + ")"
}

// finalizedUsageResultClassifierSQL mirrors the Observe finalized outcome
// classifier on the authoritative usage row: non-2xx -> failed (http_error),
// 2xx with an abnormal stream outcome -> failed (stream_error), explicit
// client_disconnected stays its own value, everything else completed.
const finalizedUsageResultClassifierSQL = `CASE
	WHEN ue.status_code NOT BETWEEN 200 AND 299 THEN 'failed'
	WHEN ue.stream_outcome = 'client_disconnected' THEN 'client_disconnected'
	WHEN ue.stream_outcome IN ('provider_incomplete','upstream_read_error','gateway_timeout','upstream_ended_without_terminal','unknown') THEN 'failed'
	ELSE 'completed' END`

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

func scanRequestLogListRow(scanner interface{ Scan(...any) error }) (requestLogListRow, error) {
	var resolvedTargetModelID sql.NullString
	var endpointID sql.NullInt32
	var connectionID sql.NullInt32
	var proxyAPIKeyID sql.NullInt32
	var proxyAPIKeyNameSnapshot sql.NullString
	var proxyAPIKeyAttributionState sql.NullString
	var proxyAPIKeyAuthEnforced sql.NullBool
	var ingressRequestID sql.NullString
	var ttftMS sql.NullInt32
	var completionDurationMS sql.NullInt32
	var streamOutcome sql.NullString
	var streamErrorKind sql.NullString
	var attemptNumber sql.NullInt32
	var attemptTrigger sql.NullString
	var attemptResult sql.NullString
	var isWinner sql.NullBool
	var upstreamStatusCode, gatewayStatusCode, legacyStatusCode, scopedStatus, attemptDurationMS, legacyDurationMS sql.NullInt32
	var errorSource, errorCode, failureStage sql.NullString
	var errorDetail, streamErrorDetail sql.NullString
	var errorDetailRedacted, errorDetailTruncated, streamErrorDetailRedacted, streamErrorDetailTruncated sql.NullBool
	var outputTokens sql.NullInt32
	var totalTokens sql.NullInt32
	var totalCostUserCurrencyMicros sql.NullInt64
	var pricingStatus sql.NullString
	var unpricedReason sql.NullString
	var pricingResolutionKind sql.NullString
	var pricingEvidenceTrust sql.NullString
	var pricingTemplateKind, pricingSelectionState, pricingCardRole sql.NullString
	var pricingSelectorThreshold sql.NullInt32
	var pricingSelectorBasis sql.NullInt64
	var reasoningEffort sql.NullString
	var reportCurrencySymbol sql.NullString
	var callerUserAgent sql.NullString
	var upstreamUserAgent sql.NullString
	var endpointBaseURL sql.NullString
	item := requestLogListRow{}
	if err := scanner.Scan(&item.ID, &item.CreatedAt, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &endpointID, &connectionID,
		&proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &proxyAPIKeyAttributionState, &proxyAPIKeyAuthEnforced, &ingressRequestID, &item.RowKind,
		&upstreamStatusCode, &gatewayStatusCode, &legacyStatusCode, &scopedStatus, &attemptDurationMS, &legacyDurationMS,
		&ttftMS, &completionDurationMS, &item.IsStream, &streamOutcome, &streamErrorKind,
		&attemptNumber, &attemptTrigger, &attemptResult, &isWinner, &errorSource, &errorCode, &failureStage,
		&errorDetail, &errorDetailRedacted, &errorDetailTruncated, &streamErrorDetail, &streamErrorDetailRedacted, &streamErrorDetailTruncated,
		&outputTokens, &totalTokens, &totalCostUserCurrencyMicros, &pricingStatus, &unpricedReason, &pricingResolutionKind, &pricingEvidenceTrust,
		&pricingTemplateKind, &pricingSelectionState, &pricingCardRole, &pricingSelectorThreshold, &pricingSelectorBasis,
		&reasoningEffort, &reportCurrencySymbol, &callerUserAgent, &upstreamUserAgent, &endpointBaseURL); err != nil {
		return requestLogListRow{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.ProxyAPIKeyID = nullableInt32(proxyAPIKeyID)
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.ProxyAPIKeyAttributionState = stringValue(nullableString(proxyAPIKeyAttributionState))
	item.ProxyAPIKeyAuthEnforced = nullableBool(proxyAPIKeyAuthEnforced)
	item.IngressRequestID = nullableString(ingressRequestID)
	item.RowKind = stringValue(normalizeOptionalString(nonNullString(item.RowKind)))
	item.StatusCode = nullableInt32(scopedStatus)
	item.UpstreamStatusCode = nullableInt32(upstreamStatusCode)
	item.GatewayStatusCode = nullableInt32(gatewayStatusCode)
	item.LegacyStatusCode = nullableInt32(legacyStatusCode)
	item.AttemptDurationMS = nullableInt32(attemptDurationMS)
	item.LegacyDurationMS = nullableInt32(legacyDurationMS)
	item.TTFTMS = nullableInt32(ttftMS)
	item.CompletionDurationMS = nullableInt32(completionDurationMS)
	item.StreamOutcome = normalizeRequestLogStreamOutcome(nullableString(streamOutcome), item.IsStream, item.CompletionDurationMS)
	item.StreamErrorKind = normalizeOptionalString(nullableString(streamErrorKind))
	item.AttemptNumber = nullableInt32(attemptNumber)
	item.AttemptTrigger = normalizeOptionalString(nullableString(attemptTrigger))
	item.AttemptResult = normalizeOptionalString(nullableString(attemptResult))
	item.IsWinner = nullableBool(isWinner)
	item.ErrorSource = normalizeOptionalString(nullableString(errorSource))
	item.ErrorCode = normalizeOptionalString(nullableString(errorCode))
	item.FailureStage = normalizeOptionalString(nullableString(failureStage))
	// Unified failure projection: error_detail wins; stream detail only for
	// rows where error_detail is absent (Requests SPEC §6.4).
	detail := nullableString(errorDetail)
	failureDetailSource := "error_detail"
	detailRedacted := errorDetailRedacted.Valid && errorDetailRedacted.Bool
	if detail == nil && nullableString(streamErrorDetail) != nil {
		detail = nullableString(streamErrorDetail)
		failureDetailSource = "stream_error_detail"
		detailRedacted = streamErrorDetailRedacted.Valid && streamErrorDetailRedacted.Bool
	}
	if detail != nil {
		preview, previewTruncated := truncateCodePoints(*detail, 240)
		item.FailureDetailPreview = &preview
		item.FailureDetailPreviewTruncated = previewTruncated
		item.FailureDetailRedacted = detailRedacted
	}
	item.FailureDetailSource = failureDetailSource
	item.OutputTokens = nullableInt32(outputTokens)
	item.TotalTokens = nullableInt32(totalTokens)
	item.TotalCostUserCurrencyMicros = nullableInt64(totalCostUserCurrencyMicros)
	item.PricingStatus = stringValue(nullableString(pricingStatus))
	item.UnpricedReason = nullableString(unpricedReason)
	item.PricingResolutionKind = nullableString(pricingResolutionKind)
	item.PricingEvidenceTrust = stringValue(nullableString(pricingEvidenceTrust))
	item.PricingTemplateKind = nullableString(pricingTemplateKind)
	item.PricingSelectionState = nullableString(pricingSelectionState)
	item.PricingCardRole = nullableString(pricingCardRole)
	item.PricingSelectorThresholdTokens = nullableInt32(pricingSelectorThreshold)
	item.PricingSelectorBasisTokens = nullableInt64(pricingSelectorBasis)
	item.ReasoningEffort = nullableString(reasoningEffort)
	item.ReportCurrencySymbol = nullableString(reportCurrencySymbol)
	item.CallerUserAgent = nullableString(callerUserAgent)
	item.UpstreamUserAgent = nullableString(upstreamUserAgent)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
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

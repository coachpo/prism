package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func loadUsageEventRecords(ctx context.Context, exec queryExecutor, profileID int, startAt *time.Time, endAt *time.Time, apiFamily *string, modelID *string, endpointID *int, connectionID *int) ([]usageEventRecord, error) {
	clauses := []string{"usage_request_events.profile_id = $1"}
	args := []any{profileID}
	if endAt != nil {
		args = append(args, endAt.UTC())
		clauses = append(clauses, fmt.Sprintf("usage_request_events.created_at <= $%d", len(args)))
	}
	if startAt != nil {
		args = append(args, startAt.UTC())
		clauses = append(clauses, fmt.Sprintf("usage_request_events.created_at >= $%d", len(args)))
	}
	if apiFamily != nil && strings.TrimSpace(*apiFamily) != "" {
		args = append(args, strings.TrimSpace(*apiFamily))
		clauses = append(clauses, fmt.Sprintf("usage_request_events.api_family = $%d", len(args)))
	}
	if modelID != nil && strings.TrimSpace(*modelID) != "" {
		args = append(args, strings.TrimSpace(*modelID))
		clauses = append(clauses, fmt.Sprintf("usage_request_events.model_id = $%d", len(args)))
	}
	if endpointID != nil {
		args = append(args, *endpointID)
		clauses = append(clauses, fmt.Sprintf("usage_request_events.endpoint_id = $%d", len(args)))
	}
	if connectionID != nil {
		args = append(args, *connectionID)
		clauses = append(clauses, fmt.Sprintf("usage_request_events.connection_id = $%d", len(args)))
	}
	rows, err := exec.Query(ctx, `SELECT usage_request_events.id, usage_request_events.created_at, usage_request_events.profile_id, usage_request_events.ingress_request_id, usage_request_events.model_id, usage_request_events.resolved_target_model_id, usage_request_events.api_family, usage_request_events.endpoint_id, usage_request_events.endpoint_label_snapshot, usage_request_events.connection_id, usage_request_events.proxy_api_key_id_snapshot, usage_request_events.proxy_api_key_name_snapshot, usage_request_events.status_code, usage_request_events.success_flag, usage_request_events.pricing_status, usage_request_events.pricing_evidence_trust, usage_request_events.unpriced_reason, usage_request_events.input_tokens, usage_request_events.output_tokens, usage_request_events.total_tokens, usage_request_events.cache_read_input_tokens, usage_request_events.cache_creation_input_tokens, usage_request_events.reasoning_tokens, usage_request_events.total_cost_user_currency_micros, usage_request_events.attempt_count, usage_request_events.final_attempt_number, usage_request_events.request_path, usage_request_events.stream_outcome, usage_request_events.response_time_ms, usage_request_events.ttft_ms, usage_request_events.completion_duration_ms, model_configs.display_name, endpoints.name, endpoints.base_url, proxy_api_keys.name, proxy_api_keys.key_prefix
		 FROM usage_request_events
		 LEFT JOIN model_configs ON model_configs.profile_id = usage_request_events.profile_id AND model_configs.model_id = usage_request_events.model_id
		 LEFT JOIN endpoints ON endpoints.profile_id = usage_request_events.profile_id AND endpoints.id = usage_request_events.endpoint_id
		 LEFT JOIN proxy_api_keys ON proxy_api_keys.id = usage_request_events.proxy_api_key_id_snapshot
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY usage_request_events.created_at DESC, usage_request_events.id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage events for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]usageEventRecord, 0)
	for rows.Next() {
		item, scanErr := scanUsageEventRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage events for profile %d: %w", profileID, err)
	}
	return items, nil
}

func scanUsageEventRecord(scanner interface{ Scan(...any) error }) (usageEventRecord, error) {
	var resolvedTargetModelID sql.NullString
	var endpointID sql.NullInt32
	var endpointLabelSnapshot sql.NullString
	var connectionID sql.NullInt32
	var proxyAPIKeyID sql.NullInt32
	var proxyAPIKeyNameSnapshot sql.NullString
	var pricingStatus sql.NullString
	var pricingEvidenceTrust sql.NullString
	var unpricedReason sql.NullString
	var inputTokens sql.NullInt32
	var outputTokens sql.NullInt32
	var totalTokens sql.NullInt32
	var cacheReadInputTokens sql.NullInt32
	var cacheCreationInputTokens sql.NullInt32
	var reasoningTokens sql.NullInt32
	var totalCostUserCurrencyMicros sql.NullInt64
	var finalAttemptNumber sql.NullInt32
	var streamOutcome sql.NullString
	var responseTimeMS sql.NullInt32
	var ttftMS sql.NullInt32
	var completionDurationMS sql.NullInt32
	var currentModelLabel sql.NullString
	var currentEndpointName sql.NullString
	var currentEndpointBaseURL sql.NullString
	var currentProxyAPIKeyName sql.NullString
	var currentProxyAPIKeyPrefix sql.NullString
	item := usageEventRecord{}
	if err := scanner.Scan(&item.ID, &item.CreatedAt, &item.ProfileID, &item.IngressRequestID, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &endpointID, &endpointLabelSnapshot, &connectionID, &proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &item.StatusCode, &item.SuccessFlag, &pricingStatus, &pricingEvidenceTrust, &unpricedReason, &inputTokens, &outputTokens, &totalTokens, &cacheReadInputTokens, &cacheCreationInputTokens, &reasoningTokens, &totalCostUserCurrencyMicros, &item.AttemptCount, &finalAttemptNumber, &item.RequestPath, &streamOutcome, &responseTimeMS, &ttftMS, &completionDurationMS, &currentModelLabel, &currentEndpointName, &currentEndpointBaseURL, &currentProxyAPIKeyName, &currentProxyAPIKeyPrefix); err != nil {
		return usageEventRecord{}, fmt.Errorf("scan usage event: %w", err)
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	item.EndpointID = normalizePositiveID(nullableInt32(endpointID))
	item.EndpointLabelSnapshot = stringValue(nullableString(endpointLabelSnapshot))
	item.ConnectionID = normalizePositiveID(nullableInt32(connectionID))
	item.ProxyAPIKeyID = normalizePositiveID(nullableInt32(proxyAPIKeyID))
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.PricingStatus = stringValue(nullableString(pricingStatus))
	item.PricingEvidenceTrust = stringValue(nullableString(pricingEvidenceTrust))
	item.UnpricedReason = nullableString(unpricedReason)
	item.InputTokens = intValue(nullableInt32(inputTokens))
	item.OutputTokens = intValue(nullableInt32(outputTokens))
	item.HasOutputTokens = outputTokens.Valid
	item.TotalTokens = intValue(nullableInt32(totalTokens))
	item.CacheReadInputTokens = intValue(nullableInt32(cacheReadInputTokens))
	item.CacheCreationInputTokens = intValue(nullableInt32(cacheCreationInputTokens))
	item.ReasoningTokens = intValue(nullableInt32(reasoningTokens))
	item.TotalCostUserCurrencyMicros = int64Value(nullableInt64(totalCostUserCurrencyMicros))
	item.HasTotalCostUserCurrencyMicros = totalCostUserCurrencyMicros.Valid
	item.FinalAttemptNumber = nullableInt32(finalAttemptNumber)
	item.StreamOutcome = stringValue(nullableString(streamOutcome))
	item.ResponseTimeMS = nullableInt32(responseTimeMS)
	item.TTFTMS = nullableInt32(ttftMS)
	item.CompletionDurationMS = nullableInt32(completionDurationMS)
	item.CurrentModelLabel = nullableString(currentModelLabel)
	item.CurrentEndpointName = nullableString(currentEndpointName)
	item.CurrentEndpointBaseURL = nullableString(currentEndpointBaseURL)
	item.CurrentProxyAPIKeyName = nullableString(currentProxyAPIKeyName)
	item.CurrentProxyAPIKeyPrefix = nullableString(currentProxyAPIKeyPrefix)
	return item, nil
}

package stats

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

type scopedStatObservation struct {
	IngressID                   string
	IngressModelID              string
	TargetModelID               *string
	APIFamily                   string
	EndpointID                  *int
	EndpointLabel               string
	ConnectionID                *int
	AttemptTrigger              *string
	AttemptResult               *string
	AttemptClass                attemptOutcomeClass
	OutcomeDetail               OutcomeDetail
	Success                     bool
	LatencyMS                   *int
	InputTokens                 int
	HasInputTokens              bool
	OutputTokens                int
	HasOutputTokens             bool
	TotalTokens                 int
	HasTotalTokens              bool
	CacheReadInputTokens        int
	HasCacheReadInputTokens     bool
	CacheCreationInputTokens    int
	HasCacheCreationInputTokens bool
	ReasoningTokens             int
	HasReasoningTokens          bool
	CacheBasisEligible          bool
	PricingStatus               string
	TrustedCost                 *int64
	OutputRateTPS               *float64
	OutputRateState             string
	OutputRateReason            *string
}

type scopedStatAccumulator struct {
	group       StatGroup
	latencies   []int
	missing     int
	costSamples int
	costMissing int
}

func GetStatsSummary(ctx context.Context, exec queryExecutor, params StatsSummaryParams) (StatsSummaryResponse, error) {
	scope, err := NormalizeScope(params.Scope)
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	groupBy, err := ValidateGroupBy(scope, stringValue(params.GroupBy))
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	if groupBy == GroupProxyAPIKey {
		return StatsSummaryResponse{}, &HTTPError{StatusCode: 422, Code: "group_invalid", Detail: "summary does not support group_by proxy_api_key"}
	}
	referenceNow := params.ReferenceNow.UTC()
	if referenceNow.IsZero() {
		referenceNow = time.Now().UTC()
	}
	preset := params.Preset
	if params.FromTime != nil || params.ToTime != nil {
		preset = "custom"
	}
	usageBounds, usageCoverage, err := ResolveDatasetCoverage(ctx, exec, "usage_request_events", preset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	requestBounds, requestCoverage, err := ResolveDatasetCoverage(ctx, exec, "request_logs", preset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	var observations []scopedStatObservation
	if scope == ScopeRouteAttempt {
		observations, err = loadAttemptStatObservations(ctx, exec, params, requestBounds)
	} else {
		observations, err = loadUsageStatObservations(ctx, exec, params, scope, usageBounds, requestBounds)
	}
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	response := buildScopedStatsSummary(observations, scope, groupBy)
	response.Coverage = ScopeCoverageFor(scope, &usageCoverage, &requestCoverage)
	return response, nil
}

func loadUsageStatObservations(ctx context.Context, exec queryExecutor, params StatsSummaryParams, scope string, usageBounds QueryBounds, requestBounds QueryBounds) ([]scopedStatObservation, error) {
	from, to := usageBounds.UsageFrom, usageBounds.UsageTo
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, &from, &to, params.APIFamily, nil, params.EndpointID, params.ConnectionID)
	if err != nil {
		return nil, err
	}
	finalLatencies := map[string]int{}
	if scope == ScopeFinal {
		finalLatencies, err = loadFinalAttemptDurations(ctx, exec, params.ProfileID, requestBounds.UsageFrom, requestBounds.UsageTo)
		if err != nil {
			return nil, err
		}
	}
	items := make([]scopedStatObservation, 0, len(records))
	for _, record := range records {
		if params.IngressModelID != nil && record.ModelID != strings.TrimSpace(*params.IngressModelID) {
			continue
		}
		if scope == ScopeFinal {
			if record.ResolvedTargetModelID == nil || record.FinalAttemptNumber == nil {
				continue
			}
			if params.FinalTargetModelID != nil && *record.ResolvedTargetModelID != strings.TrimSpace(*params.FinalTargetModelID) {
				continue
			}
		}
		outcome := ClassifyOutcomeDetail(record.StatusCode, &record.StreamOutcome)
		item := scopedStatObservation{
			IngressID: record.IngressRequestID, IngressModelID: record.ModelID, TargetModelID: record.ResolvedTargetModelID,
			APIFamily: record.APIFamily, EndpointID: normalizePositiveID(record.EndpointID), EndpointLabel: usageEventEndpointLabel(record),
			ConnectionID: normalizePositiveID(record.ConnectionID), Success: ClassifyFinalResult(outcome) == FinalResultCompleted,
			OutcomeDetail: outcome,
			InputTokens:   record.InputTokens, HasInputTokens: record.HasInputTokens,
			OutputTokens: record.OutputTokens, HasOutputTokens: record.HasOutputTokens,
			TotalTokens: record.TotalTokens, HasTotalTokens: record.HasTotalTokens,
			CacheReadInputTokens: record.CacheReadInputTokens, HasCacheReadInputTokens: record.HasCacheReadInputTokens,
			CacheCreationInputTokens: record.CacheCreationInputTokens, HasCacheCreationInputTokens: record.HasCacheCreationInputTokens,
			ReasoningTokens: record.ReasoningTokens, HasReasoningTokens: record.HasReasoningTokens,
			CacheBasisEligible: record.CacheBasisEligible,
			PricingStatus:      record.PricingStatus,
			OutputRateTPS:      requestOutputRateTPS(record.OutputTokens, record.HasOutputTokens, record.OutputRateState, record.OutputDeliverySpanMS),
			OutputRateState:    record.OutputRateState,
			OutputRateReason:   record.OutputRateReason,
		}
		if record.TrustedKnownCost() && record.HasTotalCostUserCurrencyMicros {
			cost := record.TotalCostUserCurrencyMicros
			item.TrustedCost = &cost
		}
		if scope == ScopeFinal {
			if latency, ok := finalLatencies[modelAttemptKey(record.IngressRequestID, *record.FinalAttemptNumber)]; ok {
				item.LatencyMS = &latency
			}
		} else {
			item.LatencyMS = record.ResponseTimeMS
		}
		items = append(items, item)
	}
	return items, nil
}

func loadFinalAttemptDurations(ctx context.Context, exec queryExecutor, profileID int, fromTime time.Time, toTime time.Time) (map[string]int, error) {
	rows, err := exec.Query(ctx, `SELECT ingress_request_id, attempt_number, attempt_duration_ms
		FROM request_logs
		WHERE profile_id = $1 AND row_kind = 'upstream' AND ingress_request_id IS NOT NULL
		  AND attempt_number IS NOT NULL AND created_at >= $2 AND created_at < $3
		ORDER BY created_at DESC, id DESC`, profileID, fromTime.UTC(), toTime.UTC())
	if err != nil {
		return nil, fmt.Errorf("query final-attempt durations: %w", err)
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var ingress string
		var attempt int
		var duration sql.NullInt32
		if err := rows.Scan(&ingress, &attempt, &duration); err != nil {
			return nil, err
		}
		if duration.Valid && duration.Int32 >= 0 {
			key := modelAttemptKey(ingress, attempt)
			if _, exists := result[key]; !exists {
				result[key] = int(duration.Int32)
			}
		}
	}
	return result, rows.Err()
}

func loadAttemptStatObservations(ctx context.Context, exec queryExecutor, params StatsSummaryParams, bounds QueryBounds) ([]scopedStatObservation, error) {
	clauses := []string{"profile_id = $1", "row_kind = 'upstream'", "created_at >= $2", "created_at < $3"}
	args := []any{params.ProfileID, bounds.UsageFrom.UTC(), bounds.UsageTo.UTC()}
	add := func(value any, template string) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(template, len(args)))
	}
	if params.APIFamily != nil && strings.TrimSpace(*params.APIFamily) != "" {
		add(strings.TrimSpace(*params.APIFamily), "api_family = $%d")
	}
	if params.AttemptTargetModelID != nil {
		add(strings.TrimSpace(*params.AttemptTargetModelID), "resolved_target_model_id = $%d")
	}
	if params.EndpointID != nil {
		add(*params.EndpointID, "endpoint_id = $%d")
	}
	if params.ConnectionID != nil {
		add(*params.ConnectionID, "connection_id = $%d")
	}
	if params.AttemptTrigger != nil {
		add(strings.TrimSpace(*params.AttemptTrigger), "attempt_trigger = $%d")
	}
	if params.AttemptResult != nil {
		add(strings.TrimSpace(*params.AttemptResult), "attempt_result = $%d")
	}
	rows, err := exec.Query(ctx, `SELECT ingress_request_id, model_id, resolved_target_model_id, api_family, endpoint_id, connection_id, attempt_trigger, attempt_result, attempt_duration_ms,
		input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, ttft_ms, completion_duration_ms,
		output_rate_state, output_rate_reason, output_delivery_span_ms,
		`+cacheBasisEligibleSQL+` AS cache_basis_eligible
		FROM request_logs WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at ASC, id ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query route-attempt statistics: %w", err)
	}
	defer rows.Close()
	items := make([]scopedStatObservation, 0)
	for rows.Next() {
		var ingress, modelID, apiFamily string
		var target sql.NullString
		var endpointID, connectionID, duration, inputTokens, outputTokens, totalTokens sql.NullInt32
		var cacheReadInputTokens, cacheCreationInputTokens, reasoningTokens, ttftMS, completionMS sql.NullInt32
		var outputRateState sql.NullString
		var outputRateReason sql.NullString
		var outputDeliverySpanMS sql.NullInt32
		var cacheBasisEligible bool
		var trigger, result sql.NullString
		if err := rows.Scan(&ingress, &modelID, &target, &apiFamily, &endpointID, &connectionID, &trigger, &result, &duration, &inputTokens, &outputTokens, &totalTokens, &cacheReadInputTokens, &cacheCreationInputTokens, &reasoningTokens, &ttftMS, &completionMS, &outputRateState, &outputRateReason, &outputDeliverySpanMS, &cacheBasisEligible); err != nil {
			return nil, err
		}
		resultValue := stringValue(nullableString(result))
		attemptClass := classifyAttemptResult(result)
		item := scopedStatObservation{
			IngressID: ingress, IngressModelID: modelID, TargetModelID: nullableString(target), APIFamily: apiFamily,
			EndpointID: normalizePositiveID(nullableInt32(endpointID)), ConnectionID: normalizePositiveID(nullableInt32(connectionID)),
			AttemptTrigger: nullableString(trigger), AttemptResult: nullableString(result), Success: resultValue == "completed",
			AttemptClass: attemptClass,
			LatencyMS:    nullableInt32(duration),
			InputTokens:  intValue(nullableInt32(inputTokens)), HasInputTokens: inputTokens.Valid,
			OutputTokens: intValue(nullableInt32(outputTokens)), HasOutputTokens: outputTokens.Valid,
			TotalTokens: intValue(nullableInt32(totalTokens)), HasTotalTokens: totalTokens.Valid,
			CacheReadInputTokens: intValue(nullableInt32(cacheReadInputTokens)), HasCacheReadInputTokens: cacheReadInputTokens.Valid,
			CacheCreationInputTokens: intValue(nullableInt32(cacheCreationInputTokens)), HasCacheCreationInputTokens: cacheCreationInputTokens.Valid,
			ReasoningTokens: intValue(nullableInt32(reasoningTokens)), HasReasoningTokens: reasoningTokens.Valid,
			CacheBasisEligible: cacheBasisEligible,
			OutputRateTPS:      requestOutputRateTPS(intValue(nullableInt32(outputTokens)), outputTokens.Valid, NormalizeOutputRateState(stringValue(nullableString(outputRateState))), nullableInt32(outputDeliverySpanMS)),
			OutputRateState:    NormalizeOutputRateState(stringValue(nullableString(outputRateState))),
			OutputRateReason:   nullableString(outputRateReason),
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func buildScopedStatsSummary(observations []scopedStatObservation, scope string, groupBy string) StatsSummaryResponse {
	response := StatsSummaryResponse{
		Groups: []StatGroup{}, Granularity: CaliberForScope(scope).Grain, LatencyBasis: CaliberForScope(scope).LatencyBasis,
		Caliber: CaliberForScope(scope), TotalRequests: len(observations),
	}
	latencies := make([]int, 0, len(observations))
	groups := map[string]*scopedStatAccumulator{}
	for _, observation := range observations {
		if observation.Success {
			response.SuccessCount++
		}
		if observation.LatencyMS != nil {
			latencies = append(latencies, *observation.LatencyMS)
		} else {
			response.Samples.LatencyMissingCount++
		}
		if scope != ScopeRouteAttempt {
			if observation.TrustedCost != nil {
				response.Samples.CostSampleCount++
			} else if observation.PricingStatus == "priced" || observation.PricingStatus == "unpriced" || observation.PricingStatus == "unknown" {
				response.Samples.CostMissingCount++
			}
		}
		response.TotalInputTokens += observation.InputTokens
		response.TotalOutputTokens += observation.OutputTokens
		response.TotalTokens += observation.TotalTokens
		if groupBy == GroupNone {
			continue
		}
		key := scopedObservationGroupKey(scope, groupBy, observation)
		group := groups[key]
		if group == nil {
			group = &scopedStatAccumulator{group: StatGroup{Key: key}}
			groups[key] = group
		}
		group.group.TotalRequests++
		if observation.Success {
			group.group.SuccessCount++
		}
		group.group.TotalTokens += observation.TotalTokens
		if observation.LatencyMS != nil {
			group.latencies = append(group.latencies, *observation.LatencyMS)
		} else {
			group.missing++
		}
		if scope != ScopeRouteAttempt {
			if observation.TrustedCost != nil {
				group.costSamples++
			} else if observation.PricingStatus == "priced" || observation.PricingStatus == "unpriced" || observation.PricingStatus == "unknown" {
				group.costMissing++
			}
		}
	}
	response.ErrorCount = response.TotalRequests - response.SuccessCount
	if response.TotalRequests > 0 {
		rate := successRate(response.SuccessCount, response.TotalRequests)
		response.SuccessRate = &rate
	}
	if len(latencies) > 0 {
		average := roundFloat(sumInts(latencies)/float64(len(latencies)), 1)
		response.AvgResponseTimeMS = &average
		response.P95ResponseTimeMS = percentileContInt(latencies, 0.95)
	}
	response.Samples.ObservationCount = response.TotalRequests
	response.Samples.LatencySampleCount = len(latencies)
	items := make([]StatGroup, 0, len(groups))
	for _, aggregate := range groups {
		item := aggregate.group
		item.ErrorCount = item.TotalRequests - item.SuccessCount
		if item.TotalRequests > 0 {
			rate := successRate(item.SuccessCount, item.TotalRequests)
			item.SuccessRate = &rate
		}
		if len(aggregate.latencies) > 0 {
			average := roundFloat(sumInts(aggregate.latencies)/float64(len(aggregate.latencies)), 1)
			item.AvgResponseTimeMS = &average
			item.P95ResponseTimeMS = percentileContInt(aggregate.latencies, 0.95)
		}
		item.Samples = ScopeSampleCounts{
			ObservationCount: item.TotalRequests, LatencySampleCount: len(aggregate.latencies), LatencyMissingCount: aggregate.missing,
			CostSampleCount: aggregate.costSamples, CostMissingCount: aggregate.costMissing,
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalRequests != items[j].TotalRequests {
			return items[i].TotalRequests > items[j].TotalRequests
		}
		return items[i].Key < items[j].Key
	})
	response.Groups = items
	return response
}

func scopedObservationGroupKey(scope string, groupBy string, item scopedStatObservation) string {
	switch groupBy {
	case GroupAPIFamily:
		return item.APIFamily
	case GroupIngressModel:
		return item.IngressModelID
	case GroupFinalTargetModel, GroupAttemptTargetModel:
		if item.TargetModelID != nil && strings.TrimSpace(*item.TargetModelID) != "" {
			return strings.TrimSpace(*item.TargetModelID)
		}
		return "unattributed"
	case GroupEndpoint:
		if item.EndpointID != nil {
			return fmt.Sprintf("%d", *item.EndpointID)
		}
		return "unattributed"
	case GroupTerminalTarget:
		if item.ConnectionID != nil {
			return fmt.Sprintf("%d", *item.ConnectionID)
		}
		return "unattributed"
	case GroupAttemptTrigger:
		if item.AttemptTrigger != nil && strings.TrimSpace(*item.AttemptTrigger) != "" {
			return strings.TrimSpace(*item.AttemptTrigger)
		}
		return "unknown"
	case GroupAttemptResult:
		if item.AttemptResult != nil && strings.TrimSpace(*item.AttemptResult) != "" {
			return strings.TrimSpace(*item.AttemptResult)
		}
		return "unknown"
	default:
		return "all"
	}
}

func sumInts(values []int) float64 {
	total := 0.0
	for _, value := range values {
		total += float64(value)
	}
	return total
}

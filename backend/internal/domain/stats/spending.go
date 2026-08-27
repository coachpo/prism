package stats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SpendingParams struct {
	ProfileID          int
	Preset             string
	FromTime           *time.Time
	ToTime             *time.Time
	APIFamily          *string
	IngressModelID     *string
	FinalTargetModelID *string
	EndpointID         *int
	ConnectionID       *int
	GroupBy            string
	Limit              int
	Offset             int
	TopN               int
	ReferenceNow       time.Time
	Scope              string
}

type spendingGroupAggregate struct {
	Key              string
	TotalCostMicros  int64
	TotalRequests    int
	PricedRequests   int
	UnpricedRequests int
	TotalTokens      int
	CostSamples      int
}

func GetSpending(ctx context.Context, exec queryExecutor, params SpendingParams) (SpendingReportResponse, error) {
	scope, err := NormalizeScope(params.Scope)
	if err != nil {
		return SpendingReportResponse{}, err
	}
	groupBy, err := validateSpendingGroupBy(scope, params.GroupBy)
	if err != nil {
		return SpendingReportResponse{}, err
	}
	if scope == ScopeRouteAttempt {
		response := SpendingReportResponse{Groups: []SpendingGroupRow{}, TopSpendingModels: []SpendingTopModel{}, TopSpendingEndpoints: []SpendingTopEndpoint{}, UnpricedBreakdown: map[string]int{}}
		preset := normalizeModelSpendingPreset(params.Preset)
		if params.FromTime != nil && params.ToTime != nil {
			preset = "custom"
		}
		_, coverage, coverageErr := ResolveDatasetCoverage(ctx, exec, "request_logs", preset, params.FromTime, params.ToTime, params.ReferenceNow.UTC())
		if coverageErr != nil {
			return SpendingReportResponse{}, coverageErr
		}
		response.ReportCurrencyCode, response.ReportCurrencySymbol, err = loadReportCurrencyPreferences(ctx, exec, params.ProfileID)
		if err != nil {
			return SpendingReportResponse{}, err
		}
		response.Scope = scope
		response.Caliber = CaliberForScope(scope)
		response.Coverage = DatasetCoverage{RequestLogs: &coverage}
		return response, nil
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	topN := params.TopN
	if topN <= 0 {
		topN = 5
	}
	coveragePreset := normalizeModelSpendingPreset(params.Preset)
	if params.FromTime != nil && params.ToTime != nil {
		coveragePreset = "custom"
	}
	bounds, coverage, err := ResolveDatasetCoverage(ctx, exec, "usage_request_events", coveragePreset, params.FromTime, params.ToTime, params.ReferenceNow.UTC())
	if err != nil {
		return SpendingReportResponse{}, err
	}
	startAt, endAt := &bounds.UsageFrom, &bounds.UsageTo
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, startAt, endAt, params.APIFamily, nil, params.EndpointID, params.ConnectionID)
	if err != nil {
		return SpendingReportResponse{}, err
	}
	successRecords := make([]usageEventRecord, 0)
	for _, record := range records {
		if params.IngressModelID != nil && record.ModelID != strings.TrimSpace(*params.IngressModelID) {
			continue
		}
		if params.FinalTargetModelID != nil && (record.ResolvedTargetModelID == nil || *record.ResolvedTargetModelID != strings.TrimSpace(*params.FinalTargetModelID)) {
			continue
		}
		if record.SuccessFlag {
			successRecords = append(successRecords, record)
		}
	}
	response := SpendingReportResponse{Scope: scope, Caliber: CaliberForScope(scope), Coverage: DatasetCoverage{UsageRequestEvents: &coverage}, Groups: []SpendingGroupRow{}, TopSpendingModels: []SpendingTopModel{}, TopSpendingEndpoints: []SpendingTopEndpoint{}, UnpricedBreakdown: map[string]int{}}
	for _, record := range successRecords {
		spend := int64(0)
		if record.TrustedKnownCost() {
			spend = record.TotalCostUserCurrencyMicros
			response.Summary.CostSampleCount++
		} else if record.PricingStatus != "ineligible" {
			response.Summary.CostMissingCount++
		}
		response.Summary.TotalCostMicros += spend
		response.Summary.SuccessfulRequestCount++
		addSpendingSummaryTokenTotals(&response.Summary, record)
		if record.Priced() {
			response.Summary.PricedRequestCount++
		} else {
			response.Summary.UnpricedRequestCount++
			reason := "UNKNOWN"
			if record.UnpricedReason != nil && strings.TrimSpace(*record.UnpricedReason) != "" {
				reason = strings.TrimSpace(*record.UnpricedReason)
			}
			response.UnpricedBreakdown[reason]++
		}
	}
	if response.Summary.SuccessfulRequestCount > 0 {
		response.Summary.AvgCostPerSuccessfulRequestMicros = response.Summary.TotalCostMicros / int64(response.Summary.SuccessfulRequestCount)
	}
	if response.Summary.CostSampleCount > 0 {
		cost := response.Summary.TotalCostMicros
		response.Summary.KnownCostMicros = &cost
	}
	groupAggregates := map[string]*spendingGroupAggregate{}
	modelSpend := map[string]int64{}
	modelCostSamples := map[string]int{}
	modelLabels := map[string]string{}
	endpointSpend := map[string]*SpendingTopEndpoint{}
	for _, record := range successRecords {
		spend := int64(0)
		if record.TrustedKnownCost() {
			spend = record.TotalCostUserCurrencyMicros
		}
		endpointLabel := usageEventEndpointLabel(record)
		groupKey := spendingGroupKey(scope, groupBy, record, endpointLabel)
		groupDisplayKey := groupKey
		if groupBy == "endpoint" {
			groupKey = spendingEndpointKey(record.EndpointID, endpointLabel)
			groupDisplayKey = endpointLabel
		}
		if groupBy == "" || groupBy == "none" {
			groupKey = "all"
			groupDisplayKey = "all"
		}
		aggregate := groupAggregates[groupKey]
		if aggregate == nil {
			groupAggregates[groupKey] = &spendingGroupAggregate{Key: groupDisplayKey}
			aggregate = groupAggregates[groupKey]
		}
		aggregate.TotalCostMicros += spend
		if record.TrustedKnownCost() {
			aggregate.CostSamples++
		}
		aggregate.TotalRequests++
		if record.Priced() {
			aggregate.PricedRequests++
		} else {
			aggregate.UnpricedRequests++
		}
		aggregate.TotalTokens += record.TotalTokens
		modelID := record.ModelID
		if scope == ScopeFinal {
			modelID = "unattributed"
			if record.ResolvedTargetModelID != nil && strings.TrimSpace(*record.ResolvedTargetModelID) != "" {
				modelID = strings.TrimSpace(*record.ResolvedTargetModelID)
			}
		}
		modelLabel := strings.TrimSpace(modelID)
		if scope == ScopeIngress && record.CurrentModelLabel != nil && strings.TrimSpace(*record.CurrentModelLabel) != "" {
			modelLabel = strings.TrimSpace(*record.CurrentModelLabel)
		}
		if existingLabel, ok := modelLabels[modelID]; !ok || existingLabel == strings.TrimSpace(modelID) {
			modelLabels[modelID] = modelLabel
		}
		modelSpend[modelID] += spend
		if record.TrustedKnownCost() {
			modelCostSamples[modelID]++
		}
		endpointKey := spendingEndpointKey(record.EndpointID, endpointLabel)
		endpoint := endpointSpend[endpointKey]
		if endpoint == nil {
			endpointSpend[endpointKey] = &SpendingTopEndpoint{EndpointID: record.EndpointID, EndpointLabel: endpointLabel}
			endpoint = endpointSpend[endpointKey]
		}
		if record.TrustedKnownCost() {
			if endpoint.KnownCostMicros == nil {
				endpoint.KnownCostMicros = new(int64)
			}
			*endpoint.KnownCostMicros += spend
		}
	}
	groupItems := make([]SpendingGroupRow, 0)
	if groupBy == "" || groupBy == "none" {
		groupItems = append(groupItems, spendingGroupRow("all", response.Summary.TotalCostMicros, response.Summary.CostSampleCount, response.Summary.SuccessfulRequestCount, response.Summary.PricedRequestCount, response.Summary.UnpricedRequestCount, response.Summary.TotalTokens))
		response.GroupsTotal = 1
	} else {
		response.GroupsTotal = len(groupAggregates)
		orderedGroups := make([]SpendingGroupRow, 0, len(groupAggregates))
		for _, aggregate := range groupAggregates {
			orderedGroups = append(orderedGroups, spendingGroupRow(aggregate.Key, aggregate.TotalCostMicros, aggregate.CostSamples, aggregate.TotalRequests, aggregate.PricedRequests, aggregate.UnpricedRequests, aggregate.TotalTokens))
		}
		sort.Slice(orderedGroups, func(i int, j int) bool {
			if int64Value(orderedGroups[i].KnownCostMicros) != int64Value(orderedGroups[j].KnownCostMicros) {
				return int64Value(orderedGroups[i].KnownCostMicros) > int64Value(orderedGroups[j].KnownCostMicros)
			}
			return orderedGroups[i].Key < orderedGroups[j].Key
		})
		start := offset
		if start > len(orderedGroups) {
			start = len(orderedGroups)
		}
		end := start + limit
		if end > len(orderedGroups) {
			end = len(orderedGroups)
		}
		groupItems = orderedGroups[start:end]
	}
	response.Groups = groupItems
	modelItems := make([]SpendingTopModel, 0, len(modelSpend))
	for modelID, spend := range modelSpend {
		if modelCostSamples[modelID] == 0 {
			continue
		}
		modelLabel := strings.TrimSpace(modelLabels[modelID])
		if modelLabel == "" {
			modelLabel = strings.TrimSpace(modelID)
		}
		cost := spend
		modelItems = append(modelItems, SpendingTopModel{ModelID: modelID, ModelLabel: modelLabel, KnownCostMicros: &cost})
	}
	sort.Slice(modelItems, func(i int, j int) bool {
		if int64Value(modelItems[i].KnownCostMicros) != int64Value(modelItems[j].KnownCostMicros) {
			return int64Value(modelItems[i].KnownCostMicros) > int64Value(modelItems[j].KnownCostMicros)
		}
		return modelItems[i].ModelID < modelItems[j].ModelID
	})
	if len(modelItems) > topN {
		modelItems = modelItems[:topN]
	}
	response.TopSpendingModels = modelItems
	endpointItems := make([]SpendingTopEndpoint, 0, len(endpointSpend))
	for _, item := range endpointSpend {
		if item.KnownCostMicros == nil {
			continue
		}
		endpointItems = append(endpointItems, *item)
	}
	sort.Slice(endpointItems, func(i int, j int) bool {
		if int64Value(endpointItems[i].KnownCostMicros) != int64Value(endpointItems[j].KnownCostMicros) {
			return int64Value(endpointItems[i].KnownCostMicros) > int64Value(endpointItems[j].KnownCostMicros)
		}
		if endpointIDOrMinusOne(endpointItems[i].EndpointID) != endpointIDOrMinusOne(endpointItems[j].EndpointID) {
			return endpointIDOrMinusOne(endpointItems[i].EndpointID) < endpointIDOrMinusOne(endpointItems[j].EndpointID)
		}
		return endpointItems[i].EndpointLabel < endpointItems[j].EndpointLabel
	})
	if len(endpointItems) > topN {
		endpointItems = endpointItems[:topN]
	}
	response.TopSpendingEndpoints = endpointItems
	response.ReportCurrencyCode, response.ReportCurrencySymbol, err = loadReportCurrencyPreferences(ctx, exec, params.ProfileID)
	if err != nil {
		return SpendingReportResponse{}, err
	}
	return response, nil
}

func addSpendingSummaryTokenTotals(summary *SpendingSummary, record usageEventRecord) {
	summary.TotalInputTokens += record.InputTokens
	summary.TotalOutputTokens += record.OutputTokens
	summary.TotalCacheReadInputTokens += record.CacheReadInputTokens
	summary.TotalCacheCreationInputTokens += record.CacheCreationInputTokens
	summary.TotalReasoningTokens += record.ReasoningTokens
	summary.TotalTokens += record.TotalTokens
}

func spendingGroupKey(scope string, groupBy string, record usageEventRecord, endpointLabel string) string {
	switch groupBy {
	case "day":
		return record.CreatedAt.UTC().Format("2006-01-02")
	case "week":
		year, week := record.CreatedAt.UTC().ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "month":
		return record.CreatedAt.UTC().Format("2006-01")
	case GroupAPIFamily:
		return record.APIFamily
	case GroupIngressModel:
		return record.ModelID
	case GroupFinalTargetModel:
		if record.ResolvedTargetModelID != nil && strings.TrimSpace(*record.ResolvedTargetModelID) != "" {
			return strings.TrimSpace(*record.ResolvedTargetModelID)
		}
		return "unattributed"
	case GroupEndpoint:
		return endpointLabel
	case GroupTerminalTarget:
		if record.ConnectionID != nil && *record.ConnectionID > 0 {
			return fmt.Sprintf("%d", *record.ConnectionID)
		}
		return "unattributed"
	case "ingress_model_endpoint":
		resolvedEndpointID := -1
		if record.EndpointID != nil {
			resolvedEndpointID = *record.EndpointID
		}
		return fmt.Sprintf("%s#%d", record.ModelID, resolvedEndpointID)
	case "final_target_model_endpoint":
		modelID := "unattributed"
		if record.ResolvedTargetModelID != nil && strings.TrimSpace(*record.ResolvedTargetModelID) != "" {
			modelID = strings.TrimSpace(*record.ResolvedTargetModelID)
		}
		return fmt.Sprintf("%s#%d", modelID, endpointIDOrMinusOne(record.EndpointID))
	default:
		return "all"
	}
}

func validateSpendingGroupBy(scope string, value string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(value))
	if normalized == "" {
		normalized = GroupNone
	}
	allowed := map[string]struct{}{GroupNone: {}, "day": {}, "week": {}, "month": {}, GroupAPIFamily: {}}
	if scope == ScopeIngress {
		allowed[GroupIngressModel] = struct{}{}
		allowed["ingress_model_endpoint"] = struct{}{}
	} else if scope == ScopeFinal {
		allowed[GroupFinalTargetModel] = struct{}{}
		allowed[GroupEndpoint] = struct{}{}
		allowed[GroupTerminalTarget] = struct{}{}
		allowed["final_target_model_endpoint"] = struct{}{}
	} else if scope == ScopeRouteAttempt {
		if normalized == GroupNone {
			return normalized, nil
		}
		return "", &HTTPError{StatusCode: 422, Code: "group_invalid", Detail: "route_attempt spending does not support grouping"}
	}
	if _, ok := allowed[normalized]; !ok {
		return "", &HTTPError{StatusCode: 422, Code: "group_invalid", Detail: fmt.Sprintf("group_by %q not allowed for scope %q", value, scope)}
	}
	return normalized, nil
}

func spendingGroupRow(key string, cost int64, costSamples int, requests int, priced int, unpriced int, tokens int) SpendingGroupRow {
	row := SpendingGroupRow{Key: key, TotalCostMicros: cost, CostSampleCount: costSamples, TotalRequests: requests, PricedRequests: priced, UnpricedRequests: unpriced, TotalTokens: tokens}
	if costSamples > 0 {
		value := cost
		row.KnownCostMicros = &value
	}
	return row
}

func spendingEndpointKey(endpointID *int, endpointLabel string) string {
	resolvedEndpointID := -1
	if endpointID != nil {
		resolvedEndpointID = *endpointID
	}
	return fmt.Sprintf("%d\x00%s", resolvedEndpointID, endpointLabel)
}

package stats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SpendingParams struct {
	ProfileID    int
	Preset       string
	FromTime     *time.Time
	ToTime       *time.Time
	APIFamily    *string
	ModelID      *string
	EndpointID   *int
	ConnectionID *int
	GroupBy      string
	Limit        int
	Offset       int
	TopN         int
	ReferenceNow time.Time
}

type spendingGroupAggregate struct {
	Key              string
	TotalCostMicros  int64
	TotalRequests    int
	PricedRequests   int
	UnpricedRequests int
	TotalTokens      int
}

func GetSpending(ctx context.Context, exec queryExecutor, params SpendingParams) (SpendingReportResponse, error) {
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
	startAt, endAt := resolveTimePreset(params.Preset, params.FromTime, params.ToTime, params.ReferenceNow.UTC())
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, startAt, endAt, params.APIFamily, params.ModelID, params.EndpointID, params.ConnectionID)
	if err != nil {
		return SpendingReportResponse{}, err
	}
	successRecords := make([]usageEventRecord, 0)
	for _, record := range records {
		if record.SuccessFlag {
			successRecords = append(successRecords, record)
		}
	}
	response := SpendingReportResponse{Groups: []SpendingGroupRow{}, TopSpendingModels: []SpendingTopModel{}, TopSpendingEndpoints: []SpendingTopEndpoint{}, UnpricedBreakdown: map[string]int{}}
	for _, record := range successRecords {
		spend := int64(0)
		if record.TrustedKnownCost() {
			spend = record.TotalCostUserCurrencyMicros
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
	groupBy := strings.TrimSpace(strings.ToLower(params.GroupBy))
	groupAggregates := map[string]*spendingGroupAggregate{}
	modelSpend := map[string]int64{}
	modelLabels := map[string]string{}
	endpointSpend := map[string]*SpendingTopEndpoint{}
	for _, record := range successRecords {
		spend := int64(0)
		if record.TrustedKnownCost() {
			spend = record.TotalCostUserCurrencyMicros
		}
		endpointLabel := usageEventEndpointLabel(record)
		groupKey := spendingGroupKey(groupBy, record, endpointLabel)
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
		aggregate.TotalRequests++
		if record.Priced() {
			aggregate.PricedRequests++
		} else {
			aggregate.UnpricedRequests++
		}
		aggregate.TotalTokens += record.TotalTokens
		modelLabel := strings.TrimSpace(record.ModelID)
		if record.CurrentModelLabel != nil && strings.TrimSpace(*record.CurrentModelLabel) != "" {
			modelLabel = strings.TrimSpace(*record.CurrentModelLabel)
		}
		if existingLabel, ok := modelLabels[record.ModelID]; !ok || existingLabel == strings.TrimSpace(record.ModelID) {
			modelLabels[record.ModelID] = modelLabel
		}
		modelSpend[record.ModelID] += spend
		endpointKey := spendingEndpointKey(record.EndpointID, endpointLabel)
		endpoint := endpointSpend[endpointKey]
		if endpoint == nil {
			endpointSpend[endpointKey] = &SpendingTopEndpoint{EndpointID: record.EndpointID, EndpointLabel: endpointLabel}
			endpoint = endpointSpend[endpointKey]
		}
		endpoint.TotalCostMicros += spend
	}
	groupItems := make([]SpendingGroupRow, 0)
	if groupBy == "" || groupBy == "none" {
		groupItems = append(groupItems, SpendingGroupRow{Key: "all", TotalCostMicros: response.Summary.TotalCostMicros, TotalRequests: response.Summary.SuccessfulRequestCount, PricedRequests: response.Summary.PricedRequestCount, UnpricedRequests: response.Summary.UnpricedRequestCount, TotalTokens: response.Summary.TotalTokens})
		response.GroupsTotal = 1
	} else {
		response.GroupsTotal = len(groupAggregates)
		orderedGroups := make([]SpendingGroupRow, 0, len(groupAggregates))
		for _, aggregate := range groupAggregates {
			orderedGroups = append(orderedGroups, SpendingGroupRow{Key: aggregate.Key, TotalCostMicros: aggregate.TotalCostMicros, TotalRequests: aggregate.TotalRequests, PricedRequests: aggregate.PricedRequests, UnpricedRequests: aggregate.UnpricedRequests, TotalTokens: aggregate.TotalTokens})
		}
		sort.Slice(orderedGroups, func(i int, j int) bool {
			if orderedGroups[i].TotalCostMicros != orderedGroups[j].TotalCostMicros {
				return orderedGroups[i].TotalCostMicros > orderedGroups[j].TotalCostMicros
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
		if spend <= 0 {
			continue
		}
		modelLabel := strings.TrimSpace(modelLabels[modelID])
		if modelLabel == "" {
			modelLabel = strings.TrimSpace(modelID)
		}
		modelItems = append(modelItems, SpendingTopModel{ModelID: modelID, ModelLabel: modelLabel, TotalCostMicros: spend})
	}
	sort.Slice(modelItems, func(i int, j int) bool {
		if modelItems[i].TotalCostMicros != modelItems[j].TotalCostMicros {
			return modelItems[i].TotalCostMicros > modelItems[j].TotalCostMicros
		}
		return modelItems[i].ModelID < modelItems[j].ModelID
	})
	if len(modelItems) > topN {
		modelItems = modelItems[:topN]
	}
	response.TopSpendingModels = modelItems
	endpointItems := make([]SpendingTopEndpoint, 0, len(endpointSpend))
	for _, item := range endpointSpend {
		if item.TotalCostMicros <= 0 {
			continue
		}
		endpointItems = append(endpointItems, *item)
	}
	sort.Slice(endpointItems, func(i int, j int) bool {
		if endpointItems[i].TotalCostMicros != endpointItems[j].TotalCostMicros {
			return endpointItems[i].TotalCostMicros > endpointItems[j].TotalCostMicros
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

func spendingGroupKey(groupBy string, record usageEventRecord, endpointLabel string) string {
	switch groupBy {
	case "day":
		return record.CreatedAt.UTC().Format("2006-01-02")
	case "week":
		year, week := record.CreatedAt.UTC().ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "month":
		return record.CreatedAt.UTC().Format("2006-01")
	case "api_family":
		return record.APIFamily
	case "model":
		return record.ModelID
	case "endpoint":
		return endpointLabel
	case "model_endpoint":
		resolvedEndpointID := -1
		if record.EndpointID != nil {
			resolvedEndpointID = *record.EndpointID
		}
		return fmt.Sprintf("%s#%d", record.ModelID, resolvedEndpointID)
	default:
		return "all"
	}
}

func spendingEndpointKey(endpointID *int, endpointLabel string) string {
	resolvedEndpointID := -1
	if endpointID != nil {
		resolvedEndpointID = *endpointID
	}
	return fmt.Sprintf("%d\x00%s", resolvedEndpointID, endpointLabel)
}

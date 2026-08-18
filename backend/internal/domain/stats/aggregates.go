package stats

import (
	"context"
	"sort"
	"strings"
)

type dashboardStatsSummaryGroup struct {
	group          StatGroup
	responseTimeMS []int
}

func GetStatsSummary(ctx context.Context, exec queryExecutor, params StatsSummaryParams) (StatsSummaryResponse, error) {
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, params.FromTime, params.ToTime, params.APIFamily, params.ModelID, params.EndpointID, params.ConnectionID)
	if err != nil {
		return StatsSummaryResponse{}, err
	}
	return buildDashboardStatsSummary(records, params), nil
}

func buildDashboardStatsSummary(records []usageEventRecord, params StatsSummaryParams) StatsSummaryResponse {
	response := StatsSummaryResponse{Groups: []StatGroup{}, Granularity: "request", LatencyBasis: "end_to_end"}
	response.TotalRequests = len(records)
	latencies := make([]int, 0, len(records))
	groups := map[string]*dashboardStatsSummaryGroup{}
	for _, record := range records {
		if record.SuccessFlag {
			response.SuccessCount++
		}
		if record.ResponseTimeMS != nil {
			latencies = append(latencies, *record.ResponseTimeMS)
		}
		response.TotalInputTokens += record.InputTokens
		response.TotalOutputTokens += record.OutputTokens
		response.TotalTokens += record.TotalTokens
		if normalizedGroupBy, ok := normalizedStatsSummaryGroupBy(params.GroupBy); ok {
			key := summaryUsageEventGroupKey(normalizedGroupBy, record)
			group := groups[key]
			if group == nil {
				groups[key] = &dashboardStatsSummaryGroup{group: StatGroup{Key: key}}
				group = groups[key]
			}
			group.group.TotalRequests++
			if record.SuccessFlag {
				group.group.SuccessCount++
			}
			group.group.TotalTokens += record.TotalTokens
			if record.ResponseTimeMS != nil {
				group.responseTimeMS = append(group.responseTimeMS, *record.ResponseTimeMS)
			}
		}
	}
	response.ErrorCount = response.TotalRequests - response.SuccessCount
	response.SuccessRate = successRate(response.SuccessCount, response.TotalRequests)
	if len(latencies) > 0 {
		response.AvgResponseTimeMS = roundFloat(sumInts(latencies)/float64(len(latencies)), 1)
	}
	if p95 := percentileContInt(latencies, 0.95); p95 != nil {
		response.P95ResponseTimeMS = *p95
	}
	groupItems := make([]StatGroup, 0, len(groups))
	for _, aggregate := range groups {
		item := aggregate.group
		item.ErrorCount = item.TotalRequests - item.SuccessCount
		if len(aggregate.responseTimeMS) > 0 {
			item.AvgResponseTimeMS = roundFloat(sumInts(aggregate.responseTimeMS)/float64(len(aggregate.responseTimeMS)), 1)
		}
		groupItems = append(groupItems, item)
	}
	sort.Slice(groupItems, func(i int, j int) bool {
		if groupItems[i].TotalRequests != groupItems[j].TotalRequests {
			return groupItems[i].TotalRequests > groupItems[j].TotalRequests
		}
		return groupItems[i].Key < groupItems[j].Key
	})
	response.Groups = groupItems
	return response
}

func summaryUsageEventGroupKey(groupBy string, record usageEventRecord) string {
	switch groupBy {
	case "model":
		return record.ModelID
	case "api_family":
		return record.APIFamily
	case "endpoint":
		return usageEventEndpointLabel(record)
	default:
		return ""
	}
}

func normalizedStatsSummaryGroupBy(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	switch strings.TrimSpace(strings.ToLower(*value)) {
	case "model", "api_family", "endpoint":
		return strings.TrimSpace(strings.ToLower(*value)), true
	default:
		return "", false
	}
}

func sumInts(values []int) float64 {
	total := 0.0
	for _, value := range values {
		total += float64(value)
	}
	return total
}

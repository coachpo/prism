package stats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type EndpointModelStatisticsParams struct {
	ProfileID  int
	EndpointID int
	Preset     string
	FromTime   *time.Time
	ToTime     *time.Time
}

type endpointModelAggregate struct {
	ModelID              string
	ModelLabel           string
	RequestCount         int
	SuccessCount         int
	FailedCount          int
	PricedRequestCount   int
	UnpricedRequestCount int
	TotalTokens          int
	TotalCostMicros      int64
	TTFTValues           []int
	OutputRateSum        float64
	EligibleOutputRates  int
}

func GetEndpointModelStatistics(ctx context.Context, exec queryExecutor, params EndpointModelStatisticsParams, referenceNow time.Time) ([]EndpointModelStatistic, error) {
	endpointExists, historicalExists, err := endpointOrHistoricalUsageExists(ctx, exec, params.ProfileID, params.EndpointID)
	if err != nil {
		return nil, err
	}
	if !endpointExists && !historicalExists {
		return nil, &HTTPError{StatusCode: 404, Detail: "Endpoint not found"}
	}
	preset := params.Preset
	if params.FromTime != nil || params.ToTime != nil {
		preset = "custom"
	}
	startAt, endAt := resolveTimePreset(preset, params.FromTime, params.ToTime, referenceNow.UTC())
	records, err := loadUsageEventRecords(ctx, exec, params.ProfileID, startAt, endAt, nil, nil, &params.EndpointID, nil)
	if err != nil {
		return nil, err
	}
	aggregates := map[string]*endpointModelAggregate{}
	for _, record := range records {
		group := aggregates[record.ModelID]
		modelLabel := record.ModelID
		if record.CurrentModelLabel != nil && strings.TrimSpace(*record.CurrentModelLabel) != "" {
			modelLabel = strings.TrimSpace(*record.CurrentModelLabel)
		}
		if group == nil {
			aggregates[record.ModelID] = &endpointModelAggregate{ModelID: record.ModelID, ModelLabel: modelLabel}
			group = aggregates[record.ModelID]
		}
		group.RequestCount++
		if record.SuccessFlag {
			group.SuccessCount++
			if record.Priced() {
				group.PricedRequestCount++
			} else {
				group.UnpricedRequestCount++
			}
		} else {
			group.FailedCount++
		}
		if record.TTFTMS != nil {
			group.TTFTValues = append(group.TTFTValues, *record.TTFTMS)
		}
		if outputRate := requestOutputRateTPS(record.OutputTokens, record.HasOutputTokens, record.TTFTMS, record.CompletionDurationMS); outputRate != nil {
			group.OutputRateSum += *outputRate
			group.EligibleOutputRates++
		}
		group.TotalTokens += record.TotalTokens
		if record.TrustedKnownCost() {
			group.TotalCostMicros += record.TotalCostUserCurrencyMicros
		}
	}
	items := make([]EndpointModelStatistic, 0, len(aggregates))
	for _, aggregate := range aggregates {
		items = append(items, EndpointModelStatistic{
			ModelID:              aggregate.ModelID,
			ModelLabel:           aggregate.ModelLabel,
			RequestCount:         aggregate.RequestCount,
			SuccessCount:         intPtr(aggregate.SuccessCount),
			FailedCount:          intPtr(aggregate.FailedCount),
			PricedRequestCount:   intPtr(aggregate.PricedRequestCount),
			UnpricedRequestCount: intPtr(aggregate.UnpricedRequestCount),
			SuccessRate:          successRate(aggregate.SuccessCount, aggregate.RequestCount),
			P50TTFTMS:            percentileContInt(aggregate.TTFTValues, 0.5),
			P95TTFTMS:            percentileContInt(aggregate.TTFTValues, 0.95),
			TotalTokens:          aggregate.TotalTokens,
			TotalCostMicros:      aggregate.TotalCostMicros,
			AvgOutputRateTPS:     averageOutputRatePointer(aggregate.OutputRateSum, aggregate.EligibleOutputRates),
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		if items[i].ModelLabel != items[j].ModelLabel {
			return items[i].ModelLabel < items[j].ModelLabel
		}
		return items[i].ModelID < items[j].ModelID
	})
	return items, nil
}

func endpointOrHistoricalUsageExists(ctx context.Context, exec queryExecutor, profileID int, endpointID int) (bool, bool, error) {
	var liveEndpointID int
	err := exec.QueryRow(ctx, `SELECT id FROM endpoints WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, endpointID).Scan(&liveEndpointID)
	liveExists := err == nil
	if err != nil && err != pgx.ErrNoRows {
		return false, false, fmt.Errorf("load endpoint %d for profile %d: %w", endpointID, profileID, err)
	}
	var historicalEndpointID int
	err = exec.QueryRow(ctx, `SELECT endpoint_id FROM usage_request_events WHERE profile_id = $1 AND endpoint_id = $2 LIMIT 1`, profileID, endpointID).Scan(&historicalEndpointID)
	historicalExists := err == nil
	if err != nil && err != pgx.ErrNoRows {
		return false, false, fmt.Errorf("check historical usage for endpoint %d in profile %d: %w", endpointID, profileID, err)
	}
	return liveExists, historicalExists, nil
}

func averageOutputRatePointer(sum float64, count int) *float64 {
	if count <= 0 {
		return nil
	}
	resolved := roundFloat(sum/float64(count), 2)
	return &resolved
}

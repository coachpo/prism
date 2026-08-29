package stats

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

type EndpointModelStatisticsParams struct {
	ProfileID    int
	EndpointID   int
	Preset       string
	FromTime     *time.Time
	ToTime       *time.Time
	Scope        string
	ReferenceNow time.Time
}

type EndpointModelStatisticsResponse struct {
	Items    []EndpointModelStatistic `json:"items"`
	Scope    string                   `json:"scope"`
	Caliber  ScopeCaliber             `json:"caliber"`
	Coverage DatasetCoverage          `json:"coverage"`
	Samples  ScopeSampleCounts        `json:"samples"`
}

func GetEndpointModelStatistics(ctx context.Context, exec queryExecutor, params EndpointModelStatisticsParams, referenceNow time.Time) (EndpointModelStatisticsResponse, error) {
	scope, err := NormalizeScope(params.Scope)
	if err != nil {
		return EndpointModelStatisticsResponse{}, err
	}
	if scope == ScopeIngress {
		return EndpointModelStatisticsResponse{}, &HTTPError{StatusCode: 422, Code: "scope_invalid", Detail: "endpoint model statistics support final_execution or route_attempt"}
	}
	endpointExists, historicalExists, err := endpointOrHistoricalUsageExists(ctx, exec, params.ProfileID, params.EndpointID)
	if err != nil {
		return EndpointModelStatisticsResponse{}, err
	}
	if !endpointExists && !historicalExists {
		return EndpointModelStatisticsResponse{}, &HTTPError{StatusCode: 404, Detail: "Endpoint not found"}
	}
	if referenceNow.IsZero() {
		referenceNow = params.ReferenceNow
	}
	preset := params.Preset
	if params.FromTime != nil || params.ToTime != nil {
		preset = "custom"
	}
	usageBounds, usageCoverage, err := ResolveDatasetCoverage(ctx, exec, "usage_request_events", preset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return EndpointModelStatisticsResponse{}, err
	}
	requestBounds, requestCoverage, err := ResolveDatasetCoverage(ctx, exec, "request_logs", preset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return EndpointModelStatisticsResponse{}, err
	}
	summaryParams := StatsSummaryParams{ProfileID: params.ProfileID, ReferenceNow: referenceNow, Scope: scope, EndpointID: &params.EndpointID}
	var observations []scopedStatObservation
	if scope == ScopeRouteAttempt {
		observations, err = loadAttemptStatObservations(ctx, exec, summaryParams, requestBounds)
	} else {
		observations, err = loadUsageStatObservations(ctx, exec, summaryParams, scope, usageBounds, requestBounds)
	}
	if err != nil {
		return EndpointModelStatisticsResponse{}, err
	}
	items, samples := buildEndpointModelStatistics(observations, scope)
	return EndpointModelStatisticsResponse{
		Items: items, Scope: scope, Caliber: CaliberForScope(scope), Coverage: ScopeCoverageFor(scope, &usageCoverage, &requestCoverage), Samples: samples,
	}, nil
}

type endpointModelStatisticAccumulator struct {
	modelID         string
	requestCount    int
	successCount    int
	latencies       []int
	totalTokens     int
	pricedCount     int
	unpricedCount   int
	costMicros      int64
	costSamples     int
	costMissing     int
	outputRateSum   float64
	outputRateCount int
}

func buildEndpointModelStatistics(observations []scopedStatObservation, scope string) ([]EndpointModelStatistic, ScopeSampleCounts) {
	aggregates := map[string]*endpointModelStatisticAccumulator{}
	samples := ScopeSampleCounts{ObservationCount: len(observations)}
	for _, observation := range observations {
		modelID := "unattributed"
		if observation.TargetModelID != nil && *observation.TargetModelID != "" {
			modelID = *observation.TargetModelID
		}
		aggregate := aggregates[modelID]
		if aggregate == nil {
			aggregate = &endpointModelStatisticAccumulator{modelID: modelID}
			aggregates[modelID] = aggregate
		}
		aggregate.requestCount++
		if observation.Success {
			aggregate.successCount++
		}
		if observation.LatencyMS != nil {
			aggregate.latencies = append(aggregate.latencies, *observation.LatencyMS)
			samples.LatencySampleCount++
		} else {
			samples.LatencyMissingCount++
		}
		aggregate.totalTokens += observation.TotalTokens
		if observation.OutputRateTPS != nil {
			aggregate.outputRateSum += *observation.OutputRateTPS
			aggregate.outputRateCount++
		}
		if scope != ScopeFinal {
			continue
		}
		switch observation.PricingStatus {
		case "priced":
			aggregate.pricedCount++
		case "unpriced":
			aggregate.unpricedCount++
		}
		if observation.TrustedCost != nil {
			aggregate.costMicros += *observation.TrustedCost
			aggregate.costSamples++
			samples.CostSampleCount++
		} else if observation.PricingStatus == "priced" || observation.PricingStatus == "unpriced" || observation.PricingStatus == "unknown" {
			aggregate.costMissing++
			samples.CostMissingCount++
		}
	}
	items := make([]EndpointModelStatistic, 0, len(aggregates))
	for _, aggregate := range aggregates {
		failed := aggregate.requestCount - aggregate.successCount
		item := EndpointModelStatistic{
			ModelID: aggregate.modelID, ModelLabel: aggregate.modelID, RequestCount: aggregate.requestCount,
			SuccessCount: intPtr(aggregate.successCount), FailedCount: intPtr(failed),
			SuccessRate: successRate(aggregate.successCount, aggregate.requestCount),
			P50TTFTMS:   percentileContInt(aggregate.latencies, 0.50), P95TTFTMS: percentileContInt(aggregate.latencies, 0.95),
			TotalTokens: aggregate.totalTokens, TotalCostMicros: aggregate.costMicros,
			AvgOutputRateTPS: averageOutputRatePointer(aggregate.outputRateSum, aggregate.outputRateCount),
			Samples: ScopeSampleCounts{
				ObservationCount: aggregate.requestCount, LatencySampleCount: len(aggregate.latencies),
				LatencyMissingCount: aggregate.requestCount - len(aggregate.latencies), CostSampleCount: aggregate.costSamples, CostMissingCount: aggregate.costMissing,
			},
		}
		if scope == ScopeFinal {
			item.PricedRequestCount = intPtr(aggregate.pricedCount)
			item.UnpricedRequestCount = intPtr(aggregate.unpricedCount)
			if aggregate.costSamples > 0 {
				known := aggregate.costMicros
				item.KnownCostMicros = &known
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		return items[i].ModelID < items[j].ModelID
	})
	return items, samples
}

func endpointOrHistoricalUsageExists(ctx context.Context, exec queryExecutor, profileID int, endpointID int) (bool, bool, error) {
	if endpointID <= 0 {
		return false, false, nil
	}
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
	if !historicalExists {
		err = exec.QueryRow(ctx, `SELECT endpoint_id FROM request_logs WHERE profile_id = $1 AND endpoint_id = $2 LIMIT 1`, profileID, endpointID).Scan(&historicalEndpointID)
		historicalExists = err == nil
		if err != nil && err != pgx.ErrNoRows {
			return false, false, fmt.Errorf("check historical request attempts for endpoint %d in profile %d: %w", endpointID, profileID, err)
		}
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

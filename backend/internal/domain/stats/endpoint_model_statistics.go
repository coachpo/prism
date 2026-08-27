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
	groupBy := GroupFinalTargetModel
	if scope == ScopeRouteAttempt {
		groupBy = GroupAttemptTargetModel
	}
	response, err := GetStatsSummary(ctx, exec, StatsSummaryParams{
		ProfileID: params.ProfileID, FromTime: &usageBounds.UsageFrom, ToTime: &usageBounds.UsageTo,
		Preset: "custom", ReferenceNow: referenceNow, Scope: scope, GroupBy: &groupBy, EndpointID: &params.EndpointID,
	})
	if scope == ScopeRouteAttempt {
		response, err = GetStatsSummary(ctx, exec, StatsSummaryParams{ProfileID: params.ProfileID, FromTime: &requestBounds.UsageFrom, ToTime: &requestBounds.UsageTo, Preset: "custom", ReferenceNow: referenceNow, Scope: scope, GroupBy: &groupBy, EndpointID: &params.EndpointID})
	}
	if err != nil {
		return EndpointModelStatisticsResponse{}, err
	}
	items := make([]EndpointModelStatistic, 0, len(response.Groups))
	for _, group := range response.Groups {
		item := EndpointModelStatistic{
			ModelID: group.Key, ModelLabel: group.Key, RequestCount: group.TotalRequests,
			SuccessCount: intPtr(group.SuccessCount), FailedCount: intPtr(group.ErrorCount), SuccessRate: floatValue(group.SuccessRate),
			P95TTFTMS: group.P95ResponseTimeMS, TotalTokens: group.TotalTokens,
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RequestCount > items[j].RequestCount })
	return EndpointModelStatisticsResponse{
		Items: items, Scope: scope, Caliber: CaliberForScope(scope), Coverage: ScopeCoverageFor(scope, &usageCoverage, &requestCoverage), Samples: response.Samples,
	}, nil
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
	return liveExists, historicalExists, nil
}

func averageOutputRatePointer(sum float64, count int) *float64 {
	if count <= 0 {
		return nil
	}
	resolved := roundFloat(sum/float64(count), 2)
	return &resolved
}

func floatValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ModelMetricsParams struct {
	ProfileID          int
	ModelIDs           []string
	SummaryWindowHours int
	SpendingPreset     string
	ReferenceNow       time.Time
}

type modelAttemptMetricRow struct {
	IngressRequestID string
	AttemptNumber    int
	ResolvedTargetID *string
	AttemptResult    string
	AttemptDuration  *int
}

type modelMetricAccumulator struct {
	observations int
	successes    int
	latencies    []int
	latencyMiss  int
	cost         int64
	costSamples  int
	costMissing  int
}

func GetModelMetrics(ctx context.Context, exec queryExecutor, params ModelMetricsParams) (ModelMetricsBatchResponse, error) {
	modelIDs := dedupeNonEmptyStrings(params.ModelIDs)
	response := ModelMetricsBatchResponse{Items: make([]ModelMetricsBatchItem, 0, len(modelIDs))}
	if len(modelIDs) == 0 {
		return response, nil
	}
	referenceNow := params.ReferenceNow.UTC()
	if referenceNow.IsZero() {
		referenceNow = time.Now().UTC()
	}
	hours := params.SummaryWindowHours
	if hours <= 0 {
		hours = 24
	}
	qualityFrom, qualityTo := referenceNow.Add(-time.Duration(hours)*time.Hour), referenceNow
	qualityUsageBounds, qualityUsageCoverage, err := ResolveDatasetCoverage(ctx, exec, "usage_request_events", "custom", &qualityFrom, &qualityTo, referenceNow)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	qualityRequestBounds, qualityRequestCoverage, err := ResolveDatasetCoverage(ctx, exec, "request_logs", "custom", &qualityFrom, &qualityTo, referenceNow)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	spendingPreset := normalizeModelSpendingPreset(params.SpendingPreset)
	spendingBounds, spendingCoverage, err := ResolveDatasetCoverage(ctx, exec, "usage_request_events", spendingPreset, nil, &referenceNow, referenceNow)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	qualityFromEffective, qualityToEffective := qualityUsageBounds.UsageFrom, qualityUsageBounds.UsageTo
	qualityUsage, err := loadUsageEventRecords(ctx, exec, params.ProfileID, &qualityFromEffective, &qualityToEffective, nil, nil, nil, nil)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	spendingFromEffective, spendingToEffective := spendingBounds.UsageFrom, spendingBounds.UsageTo
	spendingUsage, err := loadUsageEventRecords(ctx, exec, params.ProfileID, &spendingFromEffective, &spendingToEffective, nil, nil, nil, nil)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	attempts, err := loadModelAttemptMetricRows(ctx, exec, params.ProfileID, modelIDs, qualityRequestBounds.UsageFrom, qualityRequestBounds.UsageTo)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}

	ingress := newModelMetricAccumulatorMap(modelIDs)
	final := newModelMetricAccumulatorMap(modelIDs)
	attempt := newModelMetricAccumulatorMap(modelIDs)
	finalAttemptLatency := make(map[string]int, len(attempts))
	for _, row := range attempts {
		if row.ResolvedTargetID != nil {
			if acc := attempt[*row.ResolvedTargetID]; acc != nil {
				acc.observations++
				if row.AttemptResult == "completed" {
					acc.successes++
				}
				if row.AttemptDuration != nil {
					acc.latencies = append(acc.latencies, *row.AttemptDuration)
				} else {
					acc.latencyMiss++
				}
			}
		}
		if row.AttemptDuration != nil {
			finalAttemptLatency[modelAttemptKey(row.IngressRequestID, row.AttemptNumber)] = *row.AttemptDuration
		}
	}
	for _, row := range qualityUsage {
		if acc := ingress[row.ModelID]; acc != nil {
			acc.addUsageObservation(row, row.ResponseTimeMS)
		}
		if row.ResolvedTargetModelID == nil || row.FinalAttemptNumber == nil {
			continue
		}
		if acc := final[*row.ResolvedTargetModelID]; acc != nil {
			var latency *int
			if value, ok := finalAttemptLatency[modelAttemptKey(row.IngressRequestID, *row.FinalAttemptNumber)]; ok {
				latency = &value
			}
			acc.addUsageObservation(row, latency)
		}
	}
	for _, row := range spendingUsage {
		if acc := ingress[row.ModelID]; acc != nil {
			acc.addUsageCost(row)
		}
		if row.ResolvedTargetModelID != nil && row.FinalAttemptNumber != nil {
			if acc := final[*row.ResolvedTargetModelID]; acc != nil {
				acc.addUsageCost(row)
			}
		}
	}
	for _, modelID := range modelIDs {
		response.Items = append(response.Items, ModelMetricsBatchItem{
			ModelID:        modelID,
			Ingress:        metricBlockFromAccumulator(ScopeIngress, ingress[modelID]),
			FinalExecution: metricBlockFromAccumulator(ScopeFinal, final[modelID]),
			RouteAttempt:   metricBlockFromAccumulator(ScopeRouteAttempt, attempt[modelID]),
		})
	}
	response.Coverage = ModelMetricsCoverage{
		Quality:  DatasetCoverage{UsageRequestEvents: &qualityUsageCoverage, RequestLogs: &qualityRequestCoverage},
		Spending: DatasetCoverage{UsageRequestEvents: &spendingCoverage},
	}
	return response, nil
}

func normalizeModelSpendingPreset(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "today", "24h":
		return "24h"
	case "last_7_days", "7d":
		return "7d"
	case "all":
		return "all"
	default:
		return "30d"
	}
}

func newModelMetricAccumulatorMap(modelIDs []string) map[string]*modelMetricAccumulator {
	result := make(map[string]*modelMetricAccumulator, len(modelIDs))
	for _, modelID := range modelIDs {
		result[modelID] = &modelMetricAccumulator{}
	}
	return result
}

func (acc *modelMetricAccumulator) addUsageObservation(row usageEventRecord, latency *int) {
	acc.observations++
	outcome := ClassifyOutcomeDetail(row.StatusCode, &row.StreamOutcome)
	if ClassifyFinalResult(outcome) == FinalResultCompleted {
		acc.successes++
	}
	if latency != nil {
		acc.latencies = append(acc.latencies, *latency)
	} else {
		acc.latencyMiss++
	}
}

func (acc *modelMetricAccumulator) addUsageCost(row usageEventRecord) {
	if row.PricingStatus == "ineligible" {
		return
	}
	if row.TrustedKnownCost() && row.HasTotalCostUserCurrencyMicros {
		acc.cost += row.TotalCostUserCurrencyMicros
		acc.costSamples++
		return
	}
	acc.costMissing++
}

func metricBlockFromAccumulator(scope string, acc *modelMetricAccumulator) ModelScopeMetricBlock {
	block := ModelScopeMetricBlock{Caliber: CaliberForScope(scope)}
	if acc == nil {
		return block
	}
	block.RequestCount = acc.observations
	if acc.observations > 0 {
		rate := successRate(acc.successes, acc.observations)
		block.SuccessRate = &rate
	}
	block.P95LatencyMS = percentileContInt(acc.latencies, 0.95)
	if scope != ScopeRouteAttempt && acc.costSamples > 0 {
		cost := acc.cost
		block.KnownCostMicros = &cost
	}
	block.Samples = ScopeSampleCounts{
		ObservationCount: acc.observations, LatencySampleCount: len(acc.latencies), LatencyMissingCount: acc.latencyMiss,
		CostSampleCount: acc.costSamples, CostMissingCount: acc.costMissing,
	}
	return block
}

func loadModelAttemptMetricRows(ctx context.Context, exec queryExecutor, profileID int, modelIDs []string, fromTime time.Time, toTime time.Time) ([]modelAttemptMetricRow, error) {
	rows, err := exec.Query(ctx, `SELECT ingress_request_id, attempt_number, resolved_target_model_id, attempt_result, attempt_duration_ms
		FROM request_logs
		WHERE profile_id = $1 AND row_kind = 'upstream'
		  AND created_at >= $2 AND created_at < $3
		  AND resolved_target_model_id = ANY($4::text[])
		ORDER BY created_at ASC, id ASC`, profileID, fromTime.UTC(), toTime.UTC(), modelIDs)
	if err != nil {
		return nil, fmt.Errorf("query model attempt metrics for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	result := make([]modelAttemptMetricRow, 0)
	for rows.Next() {
		var ingress sql.NullString
		var attemptNumber sql.NullInt32
		var resolved sql.NullString
		var attemptResult sql.NullString
		var duration sql.NullInt32
		if err := rows.Scan(&ingress, &attemptNumber, &resolved, &attemptResult, &duration); err != nil {
			return nil, fmt.Errorf("scan model attempt metric row: %w", err)
		}
		if !ingress.Valid || strings.TrimSpace(ingress.String) == "" || !attemptNumber.Valid || attemptNumber.Int32 <= 0 {
			continue
		}
		result = append(result, modelAttemptMetricRow{
			IngressRequestID: ingress.String, AttemptNumber: int(attemptNumber.Int32), ResolvedTargetID: nullableString(resolved),
			AttemptResult: stringValue(nullableString(attemptResult)), AttemptDuration: nullableInt32(duration),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model attempt metric rows: %w", err)
	}
	return result, nil
}

func modelAttemptKey(ingressID string, attemptNumber int) string {
	return fmt.Sprintf("%s\x00%d", ingressID, attemptNumber)
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		items = append(items, trimmed)
	}
	return items
}

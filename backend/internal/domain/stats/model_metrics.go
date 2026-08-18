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

type summaryRequestLogRow struct {
	CreatedAt       time.Time
	ModelID         string
	APIFamily       string
	EndpointBaseURL *string
	EndpointID      *int
	ConnectionID    *int
	StatusCode      int
	ResponseTimeMS  int
	InputTokens     *int
	OutputTokens    *int
	TotalTokens     *int
}

func GetModelMetrics(ctx context.Context, exec queryExecutor, params ModelMetricsParams) (ModelMetricsBatchResponse, error) {
	uniqueModelIDs := dedupeNonEmptyStrings(params.ModelIDs)
	items := make([]ModelMetricsBatchItem, 0, len(uniqueModelIDs))
	if len(uniqueModelIDs) == 0 {
		return ModelMetricsBatchResponse{Items: items}, nil
	}
	summaryFrom := params.ReferenceNow.UTC().Add(-time.Duration(params.SummaryWindowHours) * time.Hour)
	summaryRows, err := loadModelMetricSummaryRows(ctx, exec, params.ProfileID, uniqueModelIDs, summaryFrom)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	spendingFrom, spendingTo := resolveTimePreset(params.SpendingPreset, nil, nil, params.ReferenceNow.UTC())
	spendingRows, err := loadUsageEventRecords(ctx, exec, params.ProfileID, spendingFrom, spendingTo, nil, nil, nil, nil)
	if err != nil {
		return ModelMetricsBatchResponse{}, err
	}
	return ModelMetricsBatchResponse{Items: buildModelMetricsItems(uniqueModelIDs, summaryRows, spendingRows)}, nil
}

// buildModelMetricsItems assembles the per-model metric rows. Fields that
// have no samples in the window stay nil: success_rate, p95_latency_ms, and
// spend_30d_micros are only set when there is evidence for them.
func buildModelMetricsItems(uniqueModelIDs []string, summaryRows []summaryRequestLogRow, spendingRows []usageEventRecord) []ModelMetricsBatchItem {
	resultByModelID := map[string]ModelMetricsBatchItem{}
	for _, modelID := range uniqueModelIDs {
		resultByModelID[modelID] = ModelMetricsBatchItem{ModelID: modelID}
	}
	summaryByModel := map[string][]summaryRequestLogRow{}
	for _, row := range summaryRows {
		summaryByModel[row.ModelID] = append(summaryByModel[row.ModelID], row)
	}
	for modelID, rows := range summaryByModel {
		item := resultByModelID[modelID]
		latencies := make([]int, 0, len(rows))
		successCount := 0
		for _, row := range rows {
			latencies = append(latencies, row.ResponseTimeMS)
			if row.StatusCode >= 200 && row.StatusCode <= 299 {
				successCount++
			}
		}
		item.RequestCount24H = len(rows)
		rate := successRate(successCount, len(rows))
		item.SuccessRate = &rate
		item.P95LatencyMS = percentileContInt(latencies, 0.95)
		resultByModelID[modelID] = item
	}
	for _, row := range spendingRows {
		item, ok := resultByModelID[row.ModelID]
		if !ok || !row.SuccessFlag {
			continue
		}
		if row.TrustedKnownCost() {
			if item.Spend30DMicros == nil {
				item.Spend30DMicros = new(int64)
			}
			*item.Spend30DMicros += row.TotalCostUserCurrencyMicros
		}
		resultByModelID[row.ModelID] = item
	}
	items := make([]ModelMetricsBatchItem, 0, len(uniqueModelIDs))
	for _, modelID := range uniqueModelIDs {
		items = append(items, resultByModelID[modelID])
	}
	return items
}

func loadModelMetricSummaryRows(ctx context.Context, exec queryExecutor, profileID int, modelIDs []string, fromTime time.Time) ([]summaryRequestLogRow, error) {
	if len(modelIDs) == 0 {
		return []summaryRequestLogRow{}, nil
	}
	placeholders := make([]string, 0, len(modelIDs))
	args := []any{profileID, fromTime.UTC()}
	for _, modelID := range modelIDs {
		args = append(args, modelID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := exec.Query(ctx, `SELECT created_at, model_id, api_family, endpoint_base_url, endpoint_id, connection_id, `+scopedRequestLogStatusSQL+` AS scoped_status, `+scopedRequestLogDurationSQL+` AS scoped_duration_ms, input_tokens, output_tokens, total_tokens FROM request_logs WHERE profile_id = $1 AND created_at >= $2 AND model_id IN (`+strings.Join(placeholders, ", ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query model metric summary rows for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items := make([]summaryRequestLogRow, 0)
	for rows.Next() {
		var endpointBaseURL sql.NullString
		var endpointID sql.NullInt32
		var connectionID sql.NullInt32
		var inputTokens sql.NullInt32
		var outputTokens sql.NullInt32
		var totalTokens sql.NullInt32
		var scopedStatus sql.NullInt32
		var scopedDuration sql.NullInt32
		var item summaryRequestLogRow
		if err := rows.Scan(&item.CreatedAt, &item.ModelID, &item.APIFamily, &endpointBaseURL, &endpointID, &connectionID, &scopedStatus, &scopedDuration, &inputTokens, &outputTokens, &totalTokens); err != nil {
			return nil, fmt.Errorf("scan model metric summary row: %w", err)
		}
		item.StatusCode = intValue(nullableInt32(scopedStatus))
		item.ResponseTimeMS = intValue(nullableInt32(scopedDuration))
		item.CreatedAt = item.CreatedAt.UTC()
		item.EndpointBaseURL = nullableString(endpointBaseURL)
		item.EndpointID = nullableInt32(endpointID)
		item.ConnectionID = nullableInt32(connectionID)
		item.InputTokens = nullableInt32(inputTokens)
		item.OutputTokens = nullableInt32(outputTokens)
		item.TotalTokens = nullableInt32(totalTokens)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model metric summary rows for profile %d: %w", profileID, err)
	}
	return items, nil
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

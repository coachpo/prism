package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type dashboardRecentActivityRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

type dashboardRecentActivityRow struct {
	RequestLogID                int
	CreatedAt                   time.Time
	ModelID                     string
	ResolvedTargetModelID       *string
	EndpointID                  *int
	StatusCode                  *int
	ResponseTimeMS              *int
	TTFTMS                      *int
	CompletionDurationMS        *int
	IsStream                    bool
	StreamOutcome               string
	TotalTokens                 *int
	TotalCostUserCurrencyMicros *int64
	PricingStatus               string
	UnpricedReason              *string
	ReportCurrencySymbol        *string
	EndpointBaseURL             *string
}

func GetDashboardRecentActivity(ctx context.Context, exec queryExecutor, profileID int, limit int, generatedAt time.Time) (DashboardRecentActivityResponse, error) {
	if limit <= 0 {
		limit = 12
	}
	if limit > 50 {
		limit = 50
	}
	_, currentEndpointsByID, err := loadCurrentEndpoints(ctx, exec, profileID)
	if err != nil {
		return DashboardRecentActivityResponse{}, err
	}
	_, currentModelsByID, err := loadRequestLogModels(ctx, exec, profileID)
	if err != nil {
		return DashboardRecentActivityResponse{}, err
	}
	// response_time_ms is the row-scoped end-to-end duration (stream rows use
	// completion_duration_ms) and is null when no duration resolved.
	rows, err := exec.Query(ctx, `SELECT id AS request_log_id, created_at, model_id, resolved_target_model_id, endpoint_id, `+scopedRequestLogStatusSQL+` AS status_code, `+scopedRequestLogDurationSQL+` AS response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, report_currency_symbol, endpoint_base_url
		 FROM request_logs
		 WHERE profile_id = $1
		 ORDER BY created_at DESC, request_log_id DESC
		 LIMIT $2`, profileID, limit)
	if err != nil {
		return DashboardRecentActivityResponse{}, fmt.Errorf("query dashboard recent activity for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	items, err := scanDashboardRecentActivityItems(profileID, rows, currentEndpointsByID, currentModelsByID, limit)
	if err != nil {
		return DashboardRecentActivityResponse{}, err
	}
	return newDashboardRecentActivityResponse(generatedAt, items), nil
}

func GetDashboardRecentActivityForRequestLog(ctx context.Context, exec queryExecutor, profileID int, requestLogID int, generatedAt time.Time) (DashboardRecentActivityResponse, error) {
	_, currentEndpointsByID, err := loadCurrentEndpoints(ctx, exec, profileID)
	if err != nil {
		return DashboardRecentActivityResponse{}, err
	}
	_, currentModelsByID, err := loadRequestLogModels(ctx, exec, profileID)
	if err != nil {
		return DashboardRecentActivityResponse{}, err
	}
	// response_time_ms is the row-scoped end-to-end duration (stream rows use
	// completion_duration_ms) and is null when no duration resolved.
	rows, err := exec.Query(ctx, `SELECT id AS request_log_id, created_at, model_id, resolved_target_model_id, endpoint_id, `+scopedRequestLogStatusSQL+` AS status_code, `+scopedRequestLogDurationSQL+` AS response_time_ms, ttft_ms, completion_duration_ms, is_stream, stream_outcome, total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, report_currency_symbol, endpoint_base_url
		 FROM request_logs
		 WHERE profile_id = $1 AND id = $2
		 LIMIT 1`, profileID, requestLogID)
	if err != nil {
		return DashboardRecentActivityResponse{}, fmt.Errorf("query dashboard recent activity request log %d for profile %d: %w", requestLogID, profileID, err)
	}
	defer rows.Close()
	items, err := scanDashboardRecentActivityItems(profileID, rows, currentEndpointsByID, currentModelsByID, 1)
	if err != nil {
		return DashboardRecentActivityResponse{}, err
	}
	return newDashboardRecentActivityResponse(generatedAt, items), nil
}

func scanDashboardRecentActivityItems(profileID int, rows dashboardRecentActivityRows, currentEndpointsByID map[int]endpointRecord, currentModelsByID map[string]requestLogModelRecord, capacity int) ([]DashboardRecentActivityItem, error) {
	items := make([]DashboardRecentActivityItem, 0, capacity)
	for rows.Next() {
		row, scanErr := scanDashboardRecentActivityRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		currentEndpoint, _ := endpointFromMap(currentEndpointsByID, row.EndpointID)
		items = append(items, DashboardRecentActivityItem{
			RequestLogID:                row.RequestLogID,
			CreatedAt:                   row.CreatedAt.UTC(),
			ModelID:                     row.ModelID,
			ModelLabel:                  resolveRequestLogModelLabel(currentModelsByID, row.ModelID),
			ResolvedTargetModelID:       row.ResolvedTargetModelID,
			ResolvedTargetModelLabel:    resolveRequestLogResolvedTargetModelLabel(currentModelsByID, row.ResolvedTargetModelID),
			EndpointID:                  row.EndpointID,
			EndpointLabel:               resolveEndpointLabel(currentEndpoint.Name, currentEndpoint.BaseURL, row.EndpointBaseURL, row.EndpointID, "Unknown Endpoint"),
			StatusCode:                  row.StatusCode,
			ResponseTimeMS:              row.ResponseTimeMS,
			TTFTMS:                      row.TTFTMS,
			CompletionDurationMS:        row.CompletionDurationMS,
			IsStream:                    row.IsStream,
			StreamOutcome:               row.StreamOutcome,
			TotalTokens:                 row.TotalTokens,
			TotalCostUserCurrencyMicros: row.TotalCostUserCurrencyMicros,
			PricingStatus:               row.PricingStatus,
			UnpricedReason:              row.UnpricedReason,
			ReportCurrencySymbol:        row.ReportCurrencySymbol,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard recent activity for profile %d: %w", profileID, err)
	}
	return items, nil
}

func newDashboardRecentActivityResponse(generatedAt time.Time, items []DashboardRecentActivityItem) DashboardRecentActivityResponse {
	return DashboardRecentActivityResponse{
		GeneratedAt:       generatedAt.UTC(),
		ActivityWatermark: dashboardRecentActivityWatermark(items),
		Items:             items,
	}
}

func dashboardRecentActivityWatermark(items []DashboardRecentActivityItem) DashboardRecentActivityWatermark {
	if len(items) == 0 {
		return DashboardRecentActivityWatermark{}
	}
	latestCreatedAt := items[0].CreatedAt.UTC()
	latestRequestLogID := items[0].RequestLogID
	return DashboardRecentActivityWatermark{
		LatestRequestLogCreatedAt: &latestCreatedAt,
		LatestRequestLogID:        &latestRequestLogID,
	}
}

func scanDashboardRecentActivityRow(scanner interface{ Scan(...any) error }) (dashboardRecentActivityRow, error) {
	var resolvedTargetModelID sql.NullString
	var endpointID sql.NullInt32
	var statusCode sql.NullInt32
	var responseTimeMS sql.NullInt32
	var ttftMS sql.NullInt32
	var completionDurationMS sql.NullInt32
	var streamOutcome sql.NullString
	var totalTokens sql.NullInt32
	var totalCostUserCurrencyMicros sql.NullInt64
	var pricingStatus sql.NullString
	var unpricedReason sql.NullString
	var reportCurrencySymbol sql.NullString
	var endpointBaseURL sql.NullString
	row := dashboardRecentActivityRow{}
	if err := scanner.Scan(&row.RequestLogID, &row.CreatedAt, &row.ModelID, &resolvedTargetModelID, &endpointID, &statusCode, &responseTimeMS, &ttftMS, &completionDurationMS, &row.IsStream, &streamOutcome, &totalTokens, &totalCostUserCurrencyMicros, &pricingStatus, &unpricedReason, &reportCurrencySymbol, &endpointBaseURL); err != nil {
		return dashboardRecentActivityRow{}, err
	}
	row.CreatedAt = row.CreatedAt.UTC()
	row.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	row.EndpointID = nullableInt32(endpointID)
	// response_time_ms is now the end-to-end duration and may be null for
	// rows without a resolved duration.
	row.StatusCode = nullableInt32(statusCode)
	row.ResponseTimeMS = nullableInt32(responseTimeMS)
	row.TTFTMS = nullableInt32(ttftMS)
	row.CompletionDurationMS = nullableInt32(completionDurationMS)
	row.StreamOutcome = normalizeRequestLogStreamOutcome(nullableString(streamOutcome), row.IsStream, row.CompletionDurationMS)
	row.TotalTokens = nullableInt32(totalTokens)
	row.TotalCostUserCurrencyMicros = nullableInt64(totalCostUserCurrencyMicros)
	row.PricingStatus = stringValue(nullableString(pricingStatus))
	row.UnpricedReason = nullableString(unpricedReason)
	row.ReportCurrencySymbol = nullableString(reportCurrencySymbol)
	row.EndpointBaseURL = nullableString(endpointBaseURL)
	return row, nil
}

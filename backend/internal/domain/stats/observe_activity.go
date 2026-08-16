package stats

import (
	"context"
	"fmt"
	"time"
)

// Finalized activity feed: one row per retained finalized ingress request with
// routing evidence, performance, usage and finalized pricing facts. Activity
// rows are never reconstructed from attempt-level request logs.

type ActivityItem struct {
	UsageEventID             string  `json:"usage_event_id"`
	FinalIngressRequestID    string  `json:"final_ingress_request_id"`
	CreatedAt                string  `json:"created_at"`
	ModelID                  string  `json:"model_id"`
	ModelLabel               string  `json:"model_label"`
	ResolvedTargetModelID    *string `json:"resolved_target_model_id"`
	ResolvedTargetModelLabel *string `json:"resolved_target_model_label"`
	RouteChanged             bool    `json:"route_changed"`
	AttemptCount             int     `json:"attempt_count"`
	RoutingEvidenceComplete  bool    `json:"routing_evidence_complete"`
	EndpointID               *int    `json:"endpoint_id"`
	EndpointLabel            string  `json:"endpoint_label"`
	TerminalTargetID         *int    `json:"terminal_target_id"`
	StatusCode               int     `json:"status_code"`
	FinalResult              string  `json:"final_result"`
	OutcomeDetail            string  `json:"outcome_detail"`
	IsStream                 *bool   `json:"is_stream"`
	StreamOutcome            string  `json:"stream_outcome"`
	StreamErrorKind          *string `json:"stream_error_kind"`
	TTFTMS                   *int    `json:"ttft_ms"`
	TotalDurationMS          *int    `json:"total_duration_ms"`
	OutputTokens             *int    `json:"output_tokens"`
	TotalTokens              *int    `json:"total_tokens"`
	KnownCostMicros          *string `json:"known_cost_micros"`
	FinalPricingStatus       string  `json:"final_pricing_status"`
	FinalUnpricedReason      *string `json:"final_unpriced_reason"`
	ReportingCurrencyEpoch   *int    `json:"reporting_currency_epoch"`
	ReportCurrencyCode       *string `json:"report_currency_code"`
	ReportCurrencySymbol     *string `json:"report_currency_symbol"`
}

type ActivityFeedResponse struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Coverage    Coverage       `json:"coverage"`
	Items       []ActivityItem `json:"items"`
	HasMore     bool           `json:"has_more"`
}

type ActivityParams struct {
	Limit  int
	Before *int64 // opaque cursor: last seen usage event id (older rows)
}

// LoadFinalizedActivity returns the newest finalized ingress rows for the
// usage window, optionally continuing before a cursor.
func LoadFinalizedActivity(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, params ActivityParams, referenceNow time.Time) (ActivityFeedResponse, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 50 {
		params.Limit = 50
	}
	result := ActivityFeedResponse{
		GeneratedAt: referenceNow.UTC(),
		Coverage: Coverage{
			RequestedPreset:   bounds.RequestedPreset,
			FromTime:          bounds.UsageFrom,
			ToTime:            bounds.UsageTo,
			RetentionFromTime: bounds.UsageRetentionFrom,
			Source:            bounds.Source,
			Complete:          bounds.Complete,
			Gaps:              bounds.Gaps,
			Precision:         &CoveragePrecision{TTFT: "exact", OutputRate: "exact"},
		},
		Items: []ActivityItem{},
	}
	beforeCondition := ""
	args := []any{profileID, bounds.UsageFrom, bounds.UsageTo}
	if params.Before != nil {
		args = append(args, *params.Before)
		beforeCondition = fmt.Sprintf("AND id < $%d", len(args))
	}
	rows, err := exec.Query(ctx, `
SELECT
	id, ingress_request_id, created_at, model_id, resolved_target_model_id, endpoint_id, endpoint_label_snapshot, connection_id,
	status_code, stream_outcome, stream_error_kind, ttft_ms, completion_duration_ms, output_tokens, total_tokens,
	total_cost_user_currency_micros, pricing_status, unpriced_reason, reporting_currency_epoch, report_currency_code, report_currency_symbol,
	attempt_count
FROM usage_request_events
WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3`+beforeCondition+`
ORDER BY id DESC
LIMIT $`+fmt.Sprintf("%d", len(args)+1), append(args, params.Limit)...)
	if err != nil {
		return result, fmt.Errorf("load finalized activity: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ActivityItem
		var id int64
		var createdAt time.Time
		var modelID, endpointLabel string
		var resolvedTargetModelID, streamErrorKind, unpricedReason, reportCode, reportSymbol *string
		var endpointID, connectionID, ttftMS, completionDurationMS, outputTokens, totalTokens, epoch *int
		var totalCost *int64
		var pricingStatus string
		var attemptCount int
		if err := rows.Scan(&id, &item.FinalIngressRequestID, &createdAt, &modelID, &resolvedTargetModelID,
			&endpointID, &endpointLabel, &connectionID, &item.StatusCode, &item.StreamOutcome, &streamErrorKind,
			&ttftMS, &completionDurationMS, &outputTokens, &totalTokens, &totalCost, &pricingStatus, &unpricedReason,
			&epoch, &reportCode, &reportSymbol, &attemptCount); err != nil {
			return result, err
		}
		item.UsageEventID = fmt.Sprintf("%d", id)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.ModelID = modelID
		item.ModelLabel = modelID
		item.ResolvedTargetModelID = resolvedTargetModelID
		item.RouteChanged = resolvedTargetModelID != nil && *resolvedTargetModelID != "" && *resolvedTargetModelID != modelID
		item.AttemptCount = attemptCount
		item.EndpointID = endpointID
		item.EndpointLabel = endpointLabel
		item.TerminalTargetID = connectionID
		item.StreamErrorKind = streamErrorKind
		item.TTFTMS = ttftMS
		item.TotalDurationMS = completionDurationMS
		item.OutputTokens = outputTokens
		item.TotalTokens = totalTokens
		item.FinalPricingStatus = pricingStatus
		item.FinalUnpricedReason = unpricedReason
		item.ReportingCurrencyEpoch = epoch
		item.ReportCurrencyCode = reportCode
		item.ReportCurrencySymbol = reportSymbol
		if totalCost != nil {
			value := fmt.Sprintf("%d", *totalCost)
			item.KnownCostMicros = &value
		}
		outcomeDetail := ClassifyOutcomeDetail(item.StatusCode, &item.StreamOutcome)
		item.OutcomeDetail = string(outcomeDetail)
		item.FinalResult = string(ClassifyFinalResult(outcomeDetail))
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.HasMore = len(result.Items) == params.Limit
	return result, nil
}

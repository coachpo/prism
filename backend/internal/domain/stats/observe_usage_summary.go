package stats

import (
	"context"
	"fmt"
	"time"
)

// UsageSummaryResult mirrors the usage-summary read model.
type UsageSummaryResult struct {
	GeneratedAt                        time.Time             `json:"generated_at"`
	Coverage                           Coverage              `json:"coverage"`
	CostSegments                       []ObserveCostSegment  `json:"cost_segments"`
	RequestCount                       int                   `json:"request_count"`
	HTTPSuccessCount                   int                   `json:"http_success_count"`
	HTTPFailedCount                    int                   `json:"http_failed_count"`
	HTTPSuccessRate                    *float64              `json:"http_success_rate"`
	CompletedCount                     int                   `json:"completed_count"`
	StreamErrorCount                   int                   `json:"stream_error_count"`
	ClientDisconnectedCount            int                   `json:"client_disconnected_count"`
	FailedCount                        int                   `json:"failed_count"`
	TTFTSampleCount                    int                   `json:"ttft_sample_count"`
	P50TTFTMS                          *int                  `json:"p50_ttft_ms"`
	P95TTFTMS                          *int                  `json:"p95_ttft_ms"`
	OutputRateSampleCount              int                   `json:"output_rate_sample_count"`
	AvgOutputRateTPS                   *float64              `json:"avg_output_rate_tps"`
	InputTokenSampleCount              int                   `json:"input_token_sample_count"`
	OutputTokenSampleCount             int                   `json:"output_token_sample_count"`
	CacheReadInputTokenSampleCount     int                   `json:"cache_read_input_token_sample_count"`
	CacheCreationInputTokenSampleCount int                   `json:"cache_creation_input_token_sample_count"`
	ReasoningTokenSampleCount          int                   `json:"reasoning_token_sample_count"`
	TotalTokenSampleCount              int                   `json:"total_token_sample_count"`
	InputTokens                        *int64                `json:"input_tokens"`
	OutputTokens                       *int64                `json:"output_tokens"`
	CacheReadInputTokens               *int64                `json:"cache_read_input_tokens"`
	CacheCreationInputTokens           *int64                `json:"cache_creation_input_tokens"`
	ReasoningTokens                    *int64                `json:"reasoning_tokens"`
	TotalTokens                        *int64                `json:"total_tokens"`
	CacheBasisRequestCount             int                   `json:"cache_basis_request_count"`
	CacheBasisInputTokens              *int64                `json:"cache_basis_input_tokens"`
	CacheBasisCacheReadTokens          *int64                `json:"cache_basis_cache_read_tokens"`
	CacheBasisCacheCreationTokens      *int64                `json:"cache_basis_cache_creation_tokens"`
	PricingReconciliation              PricingReconciliation `json:"pricing_reconciliation"`
	WindowAverageRPM                   *float64              `json:"window_average_rpm"`
	WindowAverageTPM                   *float64              `json:"window_average_tpm"`
}

// ObserveCostSegment mirrors the canonical cost segment shape.
type ObserveCostSegment struct {
	SegmentKey                    string         `json:"segment_key"`
	ReportingCurrencyEpoch        *int           `json:"reporting_currency_epoch"`
	CurrencyAttribution           string         `json:"currency_attribution"`
	CurrencyCode                  *string        `json:"currency_code"`
	DisplaySymbol                 *string        `json:"display_symbol"`
	ObservedSymbols               []string       `json:"observed_symbols"`
	ObservedSymbolCount           int            `json:"observed_symbol_count"`
	ObservedSymbolsTruncated      bool           `json:"observed_symbols_truncated"`
	RequestCount                  int            `json:"request_count"`
	PricingEligibleRequestCount   int            `json:"pricing_eligible_request_count"`
	PricingIneligibleRequestCount int            `json:"pricing_ineligible_request_count"`
	PricedRequestCount            int            `json:"priced_request_count"`
	UnpricedRequestCount          int            `json:"unpriced_request_count"`
	PricingUnknownRequestCount    int            `json:"pricing_unknown_request_count"`
	UnpricedReasonCounts          map[string]int `json:"unpriced_reason_counts"`
	PricingCoverageState          string         `json:"pricing_coverage_state"`
	KnownCostMicros               *string        `json:"known_cost_micros"`
	Sparkline                     *CostSparkline `json:"sparkline,omitempty"`
}

type CostSparkline struct {
	Interval string               `json:"interval"`
	Points   []CostSparklinePoint `json:"points"`
}

type CostSparklinePoint struct {
	BucketStart                   string         `json:"bucket_start"`
	RequestCount                  int            `json:"request_count"`
	PricingEligibleRequestCount   int            `json:"pricing_eligible_request_count"`
	PricingIneligibleRequestCount int            `json:"pricing_ineligible_request_count"`
	PricedRequestCount            int            `json:"priced_request_count"`
	UnpricedRequestCount          int            `json:"unpriced_request_count"`
	PricingUnknownRequestCount    int            `json:"pricing_unknown_request_count"`
	UnpricedReasonCounts          map[string]int `json:"unpriced_reason_counts"`
	PricingCoverageState          string         `json:"pricing_coverage_state"`
	KnownCostMicros               *string        `json:"known_cost_micros"`
}

// cacheBasisEligibleSQL is the single shared eligibility predicate for the
// cache-read share: both input and cache_read must be measured (null excludes
// the row, unlike cache_creation which coalesces to zero), operation_name must
// be known, and count_tokens / image operations are excluded because their
// cache components are not comparable under the disjoint basis. Rows with a
// null operation_name are indeterminate and excluded; this is a deliberate
// tradeoff, not a pass-through.
const cacheBasisEligibleSQL = `(
	input_tokens IS NOT NULL
	AND cache_read_input_tokens IS NOT NULL
	AND operation_name IS NOT NULL
	AND operation_name NOT IN (
		'anthropic.count_tokens', 'gemini.count_tokens',
		'openai.images.generations', 'openai.images.edits'
	)
)`

// LoadUsageSummary executes the single-statement window aggregate including
// the bounded cost sparkline.
func LoadUsageSummary(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, coverage Coverage, referenceNow time.Time, reportCurrencyCode string, reportCurrencySymbol string) (UsageSummaryResult, error) {
	spanMinutes := bounds.UsageTo.Sub(bounds.UsageFrom).Minutes()
	result := UsageSummaryResult{
		GeneratedAt: referenceNow.UTC(),
		Coverage:    coverage,
	}
	var p50, p95 *float64
	var avgRate *float64
	var priced, unpriced, ineligible, unknown int
	var reasonDisabled, reasonMissingUsage, reasonStreamUsage, reasonMissingData int
	var segmentsJSON []byte
	row := exec.QueryRow(ctx, `
WITH classified AS (
	SELECT
		`+outcomeDetailSQL+` AS outcome_detail,
		pricing_status,
		unpriced_reason,
		pricing_evidence_trust,
		ttft_ms,
		CASE WHEN output_tokens IS NOT NULL AND ttft_ms IS NOT NULL AND completion_duration_ms IS NOT NULL
		          AND completion_duration_ms - ttft_ms > 0
		     THEN output_tokens * 1000.0 / (completion_duration_ms - ttft_ms) END AS output_rate_tps,
		input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens,
		reasoning_tokens, total_tokens,
		operation_name,
		`+cacheBasisEligibleSQL+` AS cache_basis_eligible,
		id, created_at, reporting_currency_epoch, report_currency_code, report_currency_symbol,
		total_cost_user_currency_micros,
		`+canonicalCostSegmentKeySQL+` AS canonical_segment_key
	FROM usage_request_events
	WHERE `+usageWindowPredicate+`
),
bucketed AS (
	SELECT date_bin(interval '1 hour', created_at, $2) AS bucket_start
	FROM classified
	GROUP BY 1
)`+observeUsageSummarySegmentsCTEs+`
SELECT
	(SELECT COUNT(*) FROM classified)::int AS request_count,
	(SELECT COUNT(*) FROM classified WHERE outcome_detail = 'http_error')::int,
	(SELECT COUNT(*) FROM classified WHERE outcome_detail = 'stream_error')::int,
	(SELECT COUNT(*) FROM classified WHERE outcome_detail = 'client_disconnected')::int,
	(SELECT COUNT(*) FROM classified WHERE outcome_detail = 'completed')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'priced')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'unpriced')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'ineligible')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'unknown')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'unpriced' AND unpriced_reason = 'PRICING_DISABLED')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_TOKEN_USAGE')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'unpriced' AND unpriced_reason = 'STREAM_USAGE_UNAVAILABLE')::int,
	(SELECT COUNT(*) FROM classified WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_PRICE_DATA')::int,
	(SELECT COUNT(ttft_ms) FROM classified WHERE outcome_detail = 'completed' AND ttft_ms >= 0)::int,
	(SELECT percentile_cont(0.50) WITHIN GROUP (ORDER BY ttft_ms) FROM classified WHERE outcome_detail = 'completed' AND ttft_ms >= 0),
	(SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY ttft_ms) FROM classified WHERE outcome_detail = 'completed' AND ttft_ms >= 0),
	(SELECT COUNT(output_rate_tps) FROM classified)::int,
	(SELECT AVG(output_rate_tps) FROM classified),
	(SELECT COUNT(input_tokens) FROM classified)::int,
	(SELECT COUNT(output_tokens) FROM classified)::int,
	(SELECT COUNT(cache_read_input_tokens) FROM classified)::int,
	(SELECT COUNT(cache_creation_input_tokens) FROM classified)::int,
	(SELECT COUNT(reasoning_tokens) FROM classified)::int,
	(SELECT COUNT(total_tokens) FROM classified)::int,
	(SELECT SUM(input_tokens) FROM classified),
	(SELECT SUM(output_tokens) FROM classified),
	(SELECT SUM(cache_read_input_tokens) FROM classified),
	(SELECT SUM(cache_creation_input_tokens) FROM classified),
	(SELECT SUM(reasoning_tokens) FROM classified),
	(SELECT SUM(total_tokens) FROM classified),
	(SELECT COUNT(*) FROM classified WHERE cache_basis_eligible)::int,
	(SELECT SUM(input_tokens) FROM classified WHERE cache_basis_eligible),
	(SELECT SUM(cache_read_input_tokens) FROM classified WHERE cache_basis_eligible),
	(SELECT SUM(COALESCE(cache_creation_input_tokens, 0)) FROM classified WHERE cache_basis_eligible),
	(SELECT value FROM segments),
	(SELECT COALESCE(COUNT(DISTINCT bucket_start), 0) FROM bucketed)::int
`, profileID, bounds.UsageFrom, bounds.UsageTo)
	var requestCount, httpErrorCount, streamErrorCount, clientDisconnectedCount, completedCount int
	var bucketCount int
	if err := row.Scan(
		&requestCount,
		&httpErrorCount,
		&streamErrorCount,
		&clientDisconnectedCount,
		&completedCount,
		&priced,
		&unpriced,
		&ineligible,
		&unknown,
		&reasonDisabled,
		&reasonMissingUsage,
		&reasonStreamUsage,
		&reasonMissingData,
		&result.TTFTSampleCount,
		&p50,
		&p95,
		&result.OutputRateSampleCount,
		&avgRate,
		&result.InputTokenSampleCount,
		&result.OutputTokenSampleCount,
		&result.CacheReadInputTokenSampleCount,
		&result.CacheCreationInputTokenSampleCount,
		&result.ReasoningTokenSampleCount,
		&result.TotalTokenSampleCount,
		&result.InputTokens,
		&result.OutputTokens,
		&result.CacheReadInputTokens,
		&result.CacheCreationInputTokens,
		&result.ReasoningTokens,
		&result.TotalTokens,
		&result.CacheBasisRequestCount,
		&result.CacheBasisInputTokens,
		&result.CacheBasisCacheReadTokens,
		&result.CacheBasisCacheCreationTokens,
		&segmentsJSON,
		&bucketCount,
	); err != nil {
		return result, fmt.Errorf("load usage summary for profile %d: %w", profileID, err)
	}
	result.RequestCount = requestCount
	result.HTTPFailedCount = httpErrorCount
	result.FailedCount = httpErrorCount + streamErrorCount
	result.StreamErrorCount = streamErrorCount
	result.ClientDisconnectedCount = clientDisconnectedCount
	result.CompletedCount = completedCount
	result.HTTPSuccessCount = requestCount - httpErrorCount
	if requestCount > 0 {
		rate := float64(requestCount-httpErrorCount) * 100 / float64(requestCount)
		result.HTTPSuccessRate = &rate
	}
	result.P50TTFTMS = roundIntPointer(p50)
	result.P95TTFTMS = roundIntPointer(p95)
	result.AvgOutputRateTPS = avgRate
	if spanMinutes > 0 {
		rpm := float64(requestCount) / spanMinutes
		result.WindowAverageRPM = &rpm
		if result.TotalTokens != nil && *result.TotalTokens > 0 {
			tpm := float64(*result.TotalTokens) / spanMinutes
			result.WindowAverageTPM = &tpm
		}
	}
	reconciliation := NewPricingReconciliation()
	reconciliation.EligibleRequestCount = priced + unpriced + unknown
	reconciliation.IneligibleRequestCount = ineligible
	reconciliation.PricedRequestCount = priced
	reconciliation.UnpricedRequestCount = unpriced
	reconciliation.UnknownRequestCount = unknown
	reconciliation.UnpricedReasonCounts = map[string]int{
		UnpricedReasonPricingDisabled:        reasonDisabled,
		UnpricedReasonMissingTokenUsage:      reasonMissingUsage,
		UnpricedReasonStreamUsageUnavailable: reasonStreamUsage,
		UnpricedReasonMissingPriceData:       reasonMissingData,
	}
	FinalizePricingReconciliation(&reconciliation)
	result.PricingReconciliation = reconciliation

	segments, err := decodeObserveCostSegments(profileID, segmentsJSON)
	if err != nil {
		return result, err
	}
	if bucketCount > 0 && len(segments) > 0 {
		sparkline, err := loadCostSparkline(ctx, exec, profileID, bounds, reportCurrencyCode, reportCurrencySymbol)
		if err != nil {
			return result, err
		}
		segments[0].Sparkline = &sparkline
	}
	result.CostSegments = segments
	return result, nil
}

func loadCostSparkline(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, reportCurrencyCode string, reportCurrencySymbol string) (CostSparkline, error) {
	sparkline := CostSparkline{Interval: "auto"}
	rows, err := exec.Query(ctx, `
WITH classified AS (
	SELECT
		date_bin(interval '1 hour', created_at, $2) AS bucket_start,
		pricing_status,
		unpriced_reason,
		CASE WHEN pricing_status = 'priced' AND pricing_evidence_trust = 'trusted' THEN total_cost_user_currency_micros END AS trusted_cost
	FROM usage_request_events
	WHERE `+usageWindowPredicate+`
)
SELECT
	bucket_start,
	COUNT(*)::int,
	COUNT(*) FILTER (WHERE pricing_status = 'priced')::int,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced')::int,
	COUNT(*) FILTER (WHERE pricing_status = 'ineligible')::int,
	COUNT(*) FILTER (WHERE pricing_status = 'unknown')::int,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'PRICING_DISABLED')::int,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_TOKEN_USAGE')::int,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'STREAM_USAGE_UNAVAILABLE')::int,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_PRICE_DATA')::int,
	SUM(trusted_cost)
FROM classified
GROUP BY bucket_start
ORDER BY bucket_start ASC
LIMIT 120`, profileID, bounds.UsageFrom, bounds.UsageTo)
	if err != nil {
		return sparkline, fmt.Errorf("load cost sparkline for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var point CostSparklinePoint
		var bucket time.Time
		var priced, unpriced, ineligible, unknown int
		var reasonDisabled, reasonMissingUsage, reasonStreamUsage, reasonMissingData int
		var cost *int64
		if err := rows.Scan(&bucket, &point.RequestCount, &priced, &unpriced, &ineligible, &unknown, &reasonDisabled, &reasonMissingUsage, &reasonStreamUsage, &reasonMissingData, &cost); err != nil {
			return sparkline, err
		}
		point.BucketStart = bucket.UTC().Format(time.RFC3339)
		point.PricedRequestCount = priced
		point.UnpricedRequestCount = unpriced
		point.PricingIneligibleRequestCount = ineligible
		point.PricingUnknownRequestCount = unknown
		point.PricingEligibleRequestCount = priced + unpriced + unknown
		point.UnpricedReasonCounts = map[string]int{
			UnpricedReasonPricingDisabled:        reasonDisabled,
			UnpricedReasonMissingTokenUsage:      reasonMissingUsage,
			UnpricedReasonStreamUsageUnavailable: reasonStreamUsage,
			UnpricedReasonMissingPriceData:       reasonMissingData,
		}
		point.PricingCoverageState = PricingCoverageState(point.PricingEligibleRequestCount, priced, unpriced, unknown)
		if cost != nil {
			value := fmt.Sprintf("%d", *cost)
			point.KnownCostMicros = &value
		}
		sparkline.Points = append(sparkline.Points, point)
	}
	if err := rows.Err(); err != nil {
		return sparkline, err
	}
	return sparkline, nil
}

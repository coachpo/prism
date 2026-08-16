package stats

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Usage series, breakdowns and error aggregates. All aggregates run inside
// PostgreSQL with static SQL fragments; grouping Top N + Other re-aggregates
// the remainder from raw rows (never from averaged group values).

// ---- usage-series ----

type SeriesPoint struct {
	BucketStart             string                `json:"bucket_start"`
	RequestCount            int                   `json:"request_count"`
	HTTPSuccessCount        int                   `json:"http_success_count"`
	HTTPFailedCount         int                   `json:"http_failed_count"`
	FailedCount             int                   `json:"failed_count"`
	ClientDisconnectedCount int                   `json:"client_disconnected_count"`
	TTFTSampleCount         int                   `json:"ttft_sample_count"`
	P50TTFTMS               *int                  `json:"p50_ttft_ms"`
	P95TTFTMS               *int                  `json:"p95_ttft_ms"`
	TotalTokens             *int64                `json:"total_tokens"`
	KnownCostMicros         *string               `json:"known_cost_micros"`
	PricingReconciliation   PricingReconciliation `json:"pricing_reconciliation"`
}

type SeriesItem struct {
	Key          string        `json:"key"`
	EntityID     *string       `json:"entity_id"`
	Label        string        `json:"label"`
	Configured   *bool         `json:"configured"`
	RequestCount int           `json:"request_count"`
	Points       []SeriesPoint `json:"points"`
}

type UsageSeriesResult struct {
	GeneratedAt    time.Time    `json:"generated_at"`
	Coverage       Coverage     `json:"coverage"`
	Metric         string       `json:"metric"`
	GroupBy        string       `json:"group_by"`
	SelectionBasis string       `json:"selection_basis"`
	Interval       string       `json:"interval"`
	SeriesLimit    int          `json:"series_limit"`
	Truncated      bool         `json:"truncated"`
	Series         []SeriesItem `json:"series"`
}

// ResolveSeriesInterval resolves interval=auto into a concrete bucket size
// bounded to 24..120 buckets (hard cap 400). A zero-length span is the query
// context's empty half-open interval for a domain with no retained rows; it
// resolves to the finest bucket and yields an empty chart, not an error.
func ResolveSeriesInterval(interval string, from time.Time, to time.Time) (string, time.Duration, error) {
	span := to.Sub(from)
	if span < 0 {
		return "", 0, &HTTPError{StatusCode: 422, Detail: "invalid_time_range"}
	}
	normalized := strings.TrimSpace(interval)
	if normalized == "" || normalized == "auto" {
		for _, candidate := range []struct {
			name string
			size time.Duration
		}{
			{"1h", time.Hour}, {"6h", 6 * time.Hour}, {"1d", 24 * time.Hour},
			{"1w", 7 * 24 * time.Hour}, {"1mo", 30 * 24 * time.Hour}, {"1y", 365 * 24 * time.Hour},
		} {
			buckets := span / candidate.size
			if buckets >= 24 && buckets <= 120 {
				return candidate.name, candidate.size, nil
			}
		}
		// Fall back to the finest bounded bucket.
		if span <= 5*time.Minute {
			return "5m", 5 * time.Minute, nil
		}
		return "15m", 15 * time.Minute, nil
	}
	switch normalized {
	case "5m":
		return normalized, 5 * time.Minute, nil
	case "15m":
		return normalized, 15 * time.Minute, nil
	case "1h":
		return normalized, time.Hour, nil
	case "6h":
		return normalized, 6 * time.Hour, nil
	case "1d":
		return normalized, 24 * time.Hour, nil
	case "1w":
		return normalized, 7 * 24 * time.Hour, nil
	case "1mo":
		return normalized, 30 * 24 * time.Hour, nil
	case "1y":
		return normalized, 365 * 24 * time.Hour, nil
	default:
		return "", 0, &HTTPError{StatusCode: 422, Detail: fmt.Sprintf("unknown interval %q", normalized)}
	}
}

// LoadUsageSeries executes the two-statement main chart aggregate: statement 1
// selects Top entity IDs; statement 2 builds buckets for those entities plus
// the re-aggregated Other remainder.
func LoadUsageSeries(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, coverage Coverage, metric string, groupBy string, interval string, seriesLimit int, referenceNow time.Time, reportCurrencyCode string, reportCurrencySymbol string) (UsageSeriesResult, error) {
	intervalName, bucketSize, err := ResolveSeriesInterval(interval, bounds.UsageFrom, bounds.UsageTo)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	if seriesLimit < 2 || seriesLimit > 6 {
		seriesLimit = 6
	}
	result := UsageSeriesResult{
		GeneratedAt:    referenceNow.UTC(),
		Coverage:       coverage,
		Metric:         metric,
		GroupBy:        groupBy,
		SelectionBasis: "request_count",
		Interval:       intervalName,
		SeriesLimit:    seriesLimit,
	}
	groupColumn := groupColumnFor(groupBy)
	topIDs := make([]string, 0)
	if groupColumn != "" {
		rows, err := exec.Query(ctx, fmt.Sprintf(`
SELECT %[1]s::text AS entity_id, COUNT(*) AS request_count
FROM usage_request_events
WHERE `+usageWindowPredicate+`
GROUP BY %[1]s
ORDER BY request_count DESC, entity_id ASC
LIMIT %d`, groupColumn, seriesLimit-1), profileID, bounds.UsageFrom, bounds.UsageTo)
		if err != nil {
			return result, fmt.Errorf("load series top ids: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var entityID string
			var requestCount int
			if err := rows.Scan(&entityID, &requestCount); err != nil {
				return result, err
			}
			topIDs = append(topIDs, entityID)
		}
		if err := rows.Err(); err != nil {
			return result, err
		}
		result.Truncated = len(topIDs) == seriesLimit-1
	}
	series, err := loadSeriesBuckets(ctx, exec, profileID, bounds, metric, groupBy, topIDs, bucketSize, reportCurrencyCode, reportCurrencySymbol)
	if err != nil {
		return result, err
	}
	result.Series = series
	return result, nil
}

func groupColumnFor(groupBy string) string {
	switch groupBy {
	case "model":
		return "model_id"
	case "endpoint":
		return "endpoint_id"
	case "terminal_target":
		return "connection_id"
	default:
		return ""
	}
}

func loadSeriesBuckets(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, metric string, groupBy string, topIDs []string, bucketSize time.Duration, reportCurrencyCode string, reportCurrencySymbol string) ([]SeriesItem, error) {
	groupColumn := groupColumnFor(groupBy)
	// Build per-entity bucket rows plus Other in one statement using
	// grouping sets; the caller re-aggregates Other from raw rows.
	where := usageWindowPredicate
	args := []any{profileID, bounds.UsageFrom, bounds.UsageTo}
	entityExpr := "NULL"
	entityIDExpr := "NULL::text"
	if groupColumn != "" {
		if len(topIDs) == 0 {
			entityExpr = "'other'::text"
		} else {
			entityExpr = fmt.Sprintf(`CASE WHEN %[1]s::text = ANY($4::text[]) THEN %[1]s::text ELSE 'other'::text END`, groupColumn)
			entityIDExpr = fmt.Sprintf(`CASE WHEN %[1]s::text = ANY($4::text[]) THEN %[1]s::text ELSE NULL::text END`, groupColumn)
			args = append(args, intSliceStrings(topIDs))
		}
	}
	rows, err := exec.Query(ctx, fmt.Sprintf(`
WITH classified AS (
	SELECT
		date_bin(interval '%s', created_at, $2) AS bucket_start,
		%s AS entity_label,
		%s AS entity_id,
		`+outcomeDetailSQL+` AS outcome_detail,
		pricing_status,
		unpriced_reason,
		ttft_ms,
		total_tokens,
		CASE WHEN pricing_status = 'priced' AND pricing_evidence_trust = 'trusted' THEN total_cost_user_currency_micros END AS trusted_cost
	FROM usage_request_events
	WHERE %s
)
SELECT
	COALESCE(entity_label, 'other') AS entity_label,
	COALESCE(entity_id, 'other') AS entity_id,
	bucket_start,
	COUNT(*)::int AS request_count,
	COUNT(*) FILTER (WHERE outcome_detail = 'http_error')::int AS http_failed,
	COUNT(*) FILTER (WHERE outcome_detail = 'stream_error')::int AS stream_failed,
	COUNT(*) FILTER (WHERE outcome_detail = 'client_disconnected')::int AS client_disconnected,
	COUNT(ttft_ms) FILTER (WHERE outcome_detail = 'completed' AND ttft_ms >= 0)::int AS ttft_samples,
	percentile_cont(0.50) WITHIN GROUP (ORDER BY ttft_ms) FILTER (WHERE outcome_detail = 'completed' AND ttft_ms >= 0) AS p50,
	percentile_cont(0.95) WITHIN GROUP (ORDER BY ttft_ms) FILTER (WHERE outcome_detail = 'completed' AND ttft_ms >= 0) AS p95,
	SUM(total_tokens) AS total_tokens,
	SUM(trusted_cost) AS trusted_cost,
	COUNT(*) FILTER (WHERE pricing_status = 'priced')::int AS priced_count,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced')::int AS unpriced_count,
	COUNT(*) FILTER (WHERE pricing_status = 'ineligible')::int AS ineligible_count,
	COUNT(*) FILTER (WHERE pricing_status = 'unknown')::int AS unknown_count,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'PRICING_DISABLED')::int AS reason_disabled,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_TOKEN_USAGE')::int AS reason_usage,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'STREAM_USAGE_UNAVAILABLE')::int AS reason_stream,
	COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_PRICE_DATA')::int AS reason_data
FROM classified
GROUP BY entity_label, entity_id, bucket_start
ORDER BY entity_label ASC, bucket_start ASC
LIMIT 2000`, bucketDurationLiteral(bucketSize), entityExpr, entityIDExpr, where), args...)
	if err != nil {
		return nil, fmt.Errorf("load series buckets: %w", err)
	}
	defer rows.Close()
	type bucketRow struct {
		entityLabel string
		entityID    string
		point       SeriesPoint
	}
	buckets := make([]bucketRow, 0)
	for rows.Next() {
		var row bucketRow
		var p50, p95 *float64
		var tokens, cost *int64
		var bucketStart time.Time
		var priced, unpriced, ineligible, unknown, reasonDisabled, reasonUsage, reasonStream, reasonData, httpFailed, streamFailed, clientDisconnected, ttftSamples int
		if err := rows.Scan(&row.entityLabel, &row.entityID, &bucketStart, &row.point.RequestCount, &httpFailed, &streamFailed, &clientDisconnected, &ttftSamples, &p50, &p95, &tokens, &cost, &priced, &unpriced, &ineligible, &unknown, &reasonDisabled, &reasonUsage, &reasonStream, &reasonData); err != nil {
			return nil, err
		}
		row.point.BucketStart = bucketStart.UTC().Format(time.RFC3339)
		row.point.HTTPFailedCount = httpFailed
		row.point.FailedCount = httpFailed + streamFailed
		row.point.ClientDisconnectedCount = clientDisconnected
		row.point.HTTPSuccessCount = row.point.RequestCount - httpFailed
		row.point.TTFTSampleCount = ttftSamples
		row.point.P50TTFTMS = roundIntPointer(p50)
		row.point.P95TTFTMS = roundIntPointer(p95)
		row.point.TotalTokens = tokens
		if cost != nil {
			value := fmt.Sprintf("%d", *cost)
			row.point.KnownCostMicros = &value
		}
		reconciliation := NewPricingReconciliation()
		reconciliation.EligibleRequestCount = priced + unpriced + unknown
		reconciliation.IneligibleRequestCount = ineligible
		reconciliation.PricedRequestCount = priced
		reconciliation.UnpricedRequestCount = unpriced
		reconciliation.UnknownRequestCount = unknown
		reconciliation.UnpricedReasonCounts = map[string]int{
			UnpricedReasonPricingDisabled: reasonDisabled, UnpricedReasonMissingTokenUsage: reasonUsage,
			UnpricedReasonStreamUsageUnavailable: reasonStream, UnpricedReasonMissingPriceData: reasonData,
		}
		FinalizePricingReconciliation(&reconciliation)
		row.point.PricingReconciliation = reconciliation

		buckets = append(buckets, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Group rows by entity preserving stable order (Top IDs then other).
	orderedKeys := make([]string, 0)
	byKey := map[string]*SeriesItem{}
	for _, id := range topIDs {
		key := groupBy + ":" + id
		orderedKeys = append(orderedKeys, key)
		byKey[key] = &SeriesItem{Key: key, Label: id, Configured: boolPointer(true)}
	}
	for _, bucket := range buckets {
		key := bucket.entityID
		if groupBy == "" || groupBy == "none" {
			key = "total"
		} else if key == "" || key == "other" {
			key = "other"
		} else {
			key = groupBy + ":" + key
		}
		item, ok := byKey[key]
		if !ok {
			orderedKeys = append(orderedKeys, key)
			item = &SeriesItem{Key: key, Label: bucket.entityLabel}
			if key == "other" {
				item.Label = "Other"
				item.Configured = nil
			} else {
				item.Label = bucket.entityLabel
			}
			byKey[key] = item
		}
		item.RequestCount += bucket.point.RequestCount
		item.Points = append(item.Points, bucket.point)
	}
	series := make([]SeriesItem, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		series = append(series, *byKey[key])
	}
	return series, nil
}

func bucketDurationLiteral(duration time.Duration) string {
	switch duration {
	case 5 * time.Minute:
		return "5 minutes"
	case 15 * time.Minute:
		return "15 minutes"
	case time.Hour:
		return "1 hour"
	case 6 * time.Hour:
		return "6 hours"
	case 24 * time.Hour:
		return "1 day"
	case 7 * 24 * time.Hour:
		return "7 days"
	case 30 * 24 * time.Hour:
		return "30 days"
	case 365 * 24 * time.Hour:
		return "365 days"
	default:
		return "1 hour"
	}
}

func parseSeriesBucket(value string) string {
	parsed, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return value
		}
	}
	return parsed.UTC().Format(time.RFC3339)
}

func boolPointer(value bool) *bool {
	return &value
}

func intSliceStrings(values []string) []string {
	return append([]string(nil), values...)
}

func roundIntPointer(value *float64) *int {
	if value == nil {
		return nil
	}
	rounded := int(*value + 0.5)
	if *value < 0 {
		rounded = int(*value - 0.5)
	}
	return &rounded
}

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
	BucketStart             string  `json:"bucket_start"`
	RequestCount            int     `json:"request_count"`
	HTTPSuccessCount        int     `json:"http_success_count"`
	HTTPFailedCount         int     `json:"http_failed_count"`
	FailedCount             int     `json:"failed_count"`
	ClientDisconnectedCount int     `json:"client_disconnected_count"`
	TTFTSampleCount         int     `json:"ttft_sample_count"`
	P50TTFTMS               *int    `json:"p50_ttft_ms"`
	P95TTFTMS               *int    `json:"p95_ttft_ms"`
	TotalTokens             *int64  `json:"total_tokens"`
	KnownCostMicros         *string `json:"known_cost_micros"`
	// Output rate keeps the usage-summary caliber: a per-request tok/s value
	// only where output tokens, TTFT, and a positive stream duration exist,
	// averaged per request inside the bucket. A zero sample count leaves the
	// average null instead of publishing a fabricated zero.
	OutputRateSampleCount int      `json:"output_rate_sample_count"`
	AvgOutputRateTPS      *float64 `json:"avg_output_rate_tps"`
	// Cache basis carries the shared eligibility predicate's raw components so
	// the frontend can distinguish a real share, a genuine zero, no comparable
	// rows, and a zero denominator; they never collapse into one ratio here.
	CacheBasisRequestCount        int                   `json:"cache_basis_request_count"`
	CacheBasisInputTokens         *int64                `json:"cache_basis_input_tokens"`
	CacheBasisCacheReadTokens     *int64                `json:"cache_basis_cache_read_tokens"`
	CacheBasisCacheCreationTokens *int64                `json:"cache_basis_cache_creation_tokens"`
	PricingReconciliation         PricingReconciliation `json:"pricing_reconciliation"`
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
	GeneratedAt     time.Time         `json:"generated_at"`
	Coverage        Coverage          `json:"coverage"`
	Metric          string            `json:"metric"`
	GroupBy         string            `json:"group_by"`
	SelectionBasis  string            `json:"selection_basis"`
	Interval        string            `json:"interval"`
	SeriesLimit     int               `json:"series_limit"`
	Truncated       bool              `json:"truncated"`
	Series          []SeriesItem      `json:"series"`
	Caliber         ScopeCaliber      `json:"caliber"`
	DatasetCoverage DatasetCoverage   `json:"dataset_coverage"`
	Samples         ScopeSampleCounts `json:"samples"`
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

// seriesTopIDsQuery composes the Top-entity statement. The only interpolated
// fragments are the allowlisted group column from `groupColumnFor` and an
// already-validated integer bound; every runtime value binds through $n
// parameters, so the composed text never carries request data.
func seriesTopIDsQuery(groupColumn string, where string, limit int) string {
	return fmt.Sprintf(`
SELECT %[1]s::text AS entity_id, COUNT(*) AS request_count
FROM usage_request_events
	WHERE %[2]s AND %[1]s IS NOT NULL
GROUP BY %[1]s
ORDER BY request_count DESC, entity_id ASC
	LIMIT %[3]d`, groupColumn, where, limit)
}

// LoadUsageSeries executes the two-statement main chart aggregate: statement 1
// selects Top entity IDs; statement 2 builds buckets for those entities plus
// the re-aggregated Other remainder.
func LoadUsageSeries(ctx context.Context, exec queryExecutor, profileID int, scope string, bounds QueryBounds, usageCoverage Coverage, requestCoverage Coverage, metric string, groupBy string, interval string, seriesLimit int, referenceNow time.Time, reportCurrencyCode string, reportCurrencySymbol string) (UsageSeriesResult, error) {
	normalizedScope, err := NormalizeScope(scope)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	normalizedMetric, err := NormalizeMetric(normalizedScope, metric)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	if normalizedScope == ScopeRouteAttempt {
		return loadAttemptSeries(ctx, exec, profileID, bounds, requestCoverage, normalizedMetric, groupBy, interval, seriesLimit, referenceNow)
	}
	intervalName, bucketSize, err := ResolveSeriesInterval(interval, bounds.UsageFrom, bounds.UsageTo)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	if seriesLimit < 2 || seriesLimit > 6 {
		seriesLimit = 6
	}
	// Strict whitelist: Observe group_by must be allowlisted.
	groupBy, err = ValidateGroupBy(normalizedScope, groupBy)
	if err != nil {
		return UsageSeriesResult{}, err
	}
	result := UsageSeriesResult{
		GeneratedAt:     referenceNow.UTC(),
		Coverage:        usageCoverage,
		Metric:          normalizedMetric,
		GroupBy:         groupBy,
		SelectionBasis:  "request_count",
		Interval:        intervalName,
		SeriesLimit:     seriesLimit,
		Caliber:         CaliberForScope(scope),
		DatasetCoverage: ScopeCoverageFor(scope, &usageCoverage, &requestCoverage),
	}
	groupColumn := groupColumnFor(scope, groupBy)
	where := usageWindowPredicate
	if scope == ScopeFinal {
		where += " AND resolved_target_model_id IS NOT NULL AND final_attempt_number IS NOT NULL"
	}
	topIDs := make([]string, 0)
	if groupColumn != "" {
		// `endpoint_id` and `connection_id` are nullable: a request that failed
		// before routing settled records the outcome without an exit. Those rows
		// are real traffic but not an entity, so they must not compete for a Top
		// slot; the bucket aggregate below folds them into Other.
		rows, err := exec.Query(ctx, seriesTopIDsQuery(groupColumn, where, seriesLimit-1), profileID, bounds.UsageFrom, bounds.UsageTo)
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
	labels, err := loadSeriesLabels(ctx, exec, profileID, bounds, scope, groupBy, topIDs)
	if err != nil {
		return result, err
	}
	series, err := loadSeriesBuckets(ctx, exec, profileID, bounds, scope, groupBy, topIDs, labels, bucketSize, where)
	if err != nil {
		return result, err
	}
	result.Series = series
	for _, item := range series {
		result.Samples.ObservationCount += item.RequestCount
		for _, point := range item.Points {
			result.Samples.LatencySampleCount += point.TTFTSampleCount
			result.Samples.LatencyMissingCount += point.RequestCount - point.TTFTSampleCount
			result.Samples.CostSampleCount += point.PricingReconciliation.PricedRequestCount
			result.Samples.CostMissingCount += point.PricingReconciliation.UnpricedRequestCount + point.PricingReconciliation.UnknownRequestCount
		}
	}
	return result, nil
}

func validateSeriesMetric(scope string, metric string) error {
	return ValidateMetric(scope, metric)
}

func groupColumnFor(scope string, groupBy string) string {
	switch strings.TrimSpace(strings.ToLower(groupBy)) {
	case GroupIngressModel:
		return "model_id"
	case GroupFinalTargetModel:
		return "resolved_target_model_id"
	case GroupEndpoint:
		return "CASE WHEN endpoint_id > 0 THEN endpoint_id END"
	case GroupTerminalTarget:
		return "CASE WHEN connection_id > 0 THEN connection_id END"
	case GroupNone, "":
		return ""
	default:
		return ""
	}
}

// loadSeriesLabels resolves the display label for the Top entity IDs.
//
// The bucket aggregate groups on the entity ID, never on a label: a rename
// inside the window would otherwise split one entity into two series. Labels
// are therefore a separate lookup over at most `seriesLimit-1` ids, which also
// keeps the join off the hot aggregate. `model` needs no lookup because
// `model_id` is already the model identifier.
func loadSeriesLabels(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, scope string, groupBy string, topIDs []string) (map[string]string, error) {
	labels := map[string]string{}
	if len(topIDs) == 0 {
		return labels, nil
	}
	var query string
	switch groupBy {
	case GroupEndpoint:
		// The retained snapshot is the endpoint label source for usage
		// reporting, not the mutable endpoints.name; `usageEventEndpointLabel`
		// applies the same rule to the record surfaces.
		query = `
SELECT DISTINCT ON (endpoint_id)
	endpoint_id::text,
	COALESCE(NULLIF(endpoint_label_snapshot, ''), 'Unknown Endpoint')
FROM usage_request_events
WHERE ` + usageWindowPredicate + ` AND endpoint_id::text = ANY($4::text[])
ORDER BY endpoint_id, created_at DESC`
	case GroupTerminalTarget:
		// Same precedence as the terminal-target drill-down, so one target
		// cannot read as two different names on two surfaces.
		query = `
SELECT DISTINCT ON (usage_request_events.connection_id)
	usage_request_events.connection_id::text,
	COALESCE(NULLIF(connections.name, ''), endpoints.name, NULLIF(usage_request_events.endpoint_label_snapshot, ''), 'Terminal Target')
FROM usage_request_events
LEFT JOIN connections ON connections.id = usage_request_events.connection_id AND connections.profile_id = usage_request_events.profile_id
LEFT JOIN endpoints ON endpoints.id = usage_request_events.endpoint_id AND endpoints.profile_id = usage_request_events.profile_id
WHERE usage_request_events.profile_id = $1 AND usage_request_events.created_at >= $2 AND usage_request_events.created_at < $3
	AND usage_request_events.connection_id::text = ANY($4::text[])
ORDER BY usage_request_events.connection_id, usage_request_events.created_at DESC`
	default:
		return labels, nil
	}
	rows, err := exec.Query(ctx, query, profileID, bounds.UsageFrom, bounds.UsageTo, intSliceStrings(topIDs))
	if err != nil {
		return nil, fmt.Errorf("load series labels: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entityID string
		var label string
		if err := rows.Scan(&entityID, &label); err != nil {
			return nil, err
		}
		labels[entityID] = label
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return labels, nil
}

// seriesLabel keeps the raw id as the last resort: a series that was selected
// from the window always has a label, and an invented one would be worse than
// the id it stands for.
func seriesLabel(labels map[string]string, entityID string) string {
	if label := strings.TrimSpace(labels[entityID]); label != "" {
		return label
	}
	return entityID
}

// seriesBucketsQuery composes the per-entity bucket statement. Every
// interpolated fragment is allowlisted: the bucket literal comes from the
// closed `bucketDurationLiteral` enum, the entity expressions embed only the
// `groupColumnFor` column name, and the predicates are package constants;
// runtime values bind through $n parameters and never enter the text.
func seriesBucketsQuery(bucketLiteral string, entityExpr string, entityIDExpr string, latencyExpr string, extraJoin string, where string) string {
	return fmt.Sprintf(`
WITH classified AS (
	SELECT
		date_bin(interval '%s', created_at, $2) AS bucket_start,
		%s AS entity_label,
		%s AS entity_id,
		`+outcomeDetailSQL+` AS outcome_detail,
		pricing_status,
		unpriced_reason,
			%s AS ttft_ms,
		total_tokens,
		CASE WHEN pricing_status = 'priced' AND pricing_evidence_trust = 'trusted' THEN total_cost_user_currency_micros END AS trusted_cost,
			`+outputRateTPSSQL+` AS output_rate_tps,
		`+cacheBasisEligibleSQL+` AS cache_basis_eligible,
		input_tokens,
		cache_read_input_tokens,
		COALESCE(cache_creation_input_tokens, 0) AS cache_creation_tokens
		FROM usage_request_events
		%s
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
	COUNT(output_rate_tps)::int AS output_rate_samples,
	AVG(output_rate_tps) AS avg_output_rate,
	COUNT(*) FILTER (WHERE cache_basis_eligible)::int AS cache_basis_requests,
	SUM(input_tokens) FILTER (WHERE cache_basis_eligible) AS cache_basis_input,
	SUM(cache_read_input_tokens) FILTER (WHERE cache_basis_eligible) AS cache_basis_read,
	SUM(cache_creation_tokens) FILTER (WHERE cache_basis_eligible) AS cache_basis_creation,
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
	LIMIT 2000`, bucketLiteral, entityExpr, entityIDExpr, latencyExpr, extraJoin, where)
}

func loadSeriesBuckets(ctx context.Context, exec queryExecutor, profileID int, bounds QueryBounds, scope string, groupBy string, topIDs []string, labels map[string]string, bucketSize time.Duration, where string) ([]SeriesItem, error) {
	groupColumn := groupColumnFor(scope, groupBy)
	// Build per-entity bucket rows plus Other in one statement using
	// grouping sets; the caller re-aggregates Other from raw rows.
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
	latencyExpr := "usage_request_events.ttft_ms"
	extraJoin := ""
	if scope == ScopeFinal {
		latencyExpr = "final_attempt.attempt_duration_ms"
		extraJoin = `LEFT JOIN LATERAL (
			SELECT request_logs.attempt_duration_ms
			FROM request_logs
			WHERE request_logs.profile_id = usage_request_events.profile_id
			  AND request_logs.ingress_request_id = usage_request_events.ingress_request_id
			  AND request_logs.row_kind = 'upstream'
			  AND request_logs.attempt_number = usage_request_events.final_attempt_number
			ORDER BY request_logs.created_at DESC, request_logs.id DESC LIMIT 1
		) AS final_attempt ON TRUE`
	}
	rows, err := exec.Query(ctx, seriesBucketsQuery(bucketDurationLiteral(bucketSize), entityExpr, entityIDExpr, latencyExpr, extraJoin, where), args...)
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
		var avgOutputRate *float64
		var tokens, cost *int64
		var cacheBasisInput, cacheBasisRead, cacheBasisCreation *int64
		var bucketStart time.Time
		var priced, unpriced, ineligible, unknown, reasonDisabled, reasonUsage, reasonStream, reasonData, httpFailed, streamFailed, clientDisconnected, ttftSamples int
		var outputRateSamples, cacheBasisRequests int
		if err := rows.Scan(&row.entityLabel, &row.entityID, &bucketStart, &row.point.RequestCount, &httpFailed, &streamFailed, &clientDisconnected, &ttftSamples, &p50, &p95, &tokens, &cost, &outputRateSamples, &avgOutputRate, &cacheBasisRequests, &cacheBasisInput, &cacheBasisRead, &cacheBasisCreation, &priced, &unpriced, &ineligible, &unknown, &reasonDisabled, &reasonUsage, &reasonStream, &reasonData); err != nil {
			return nil, err
		}
		row.point.OutputRateSampleCount = outputRateSamples
		row.point.AvgOutputRateTPS = avgOutputRate
		row.point.CacheBasisRequestCount = cacheBasisRequests
		row.point.CacheBasisInputTokens = cacheBasisInput
		row.point.CacheBasisCacheReadTokens = cacheBasisRead
		row.point.CacheBasisCacheCreationTokens = cacheBasisCreation
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
		entityID := id
		orderedKeys = append(orderedKeys, key)
		byKey[key] = &SeriesItem{Key: key, EntityID: &entityID, Label: seriesLabel(labels, id), Configured: boolPointer(true)}
	}
	for _, bucket := range buckets {
		key := bucket.entityID
		if groupBy == "" || groupBy == GroupNone {
			key = "total"
		} else if key == "" || key == "other" {
			key = "other"
		} else {
			key = groupBy + ":" + key
		}
		item, ok := byKey[key]
		if !ok {
			orderedKeys = append(orderedKeys, key)
			item = &SeriesItem{Key: key}
			switch key {
			case "other":
				// The re-aggregated remainder is not an entity, so it carries
				// no id and no configured flag.
				item.Label = "Other"
			case "total":
				item.Label = "Total"
			default:
				entityID := bucket.entityID
				item.EntityID = &entityID
				item.Label = seriesLabel(labels, bucket.entityID)
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

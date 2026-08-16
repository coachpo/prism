package stats

import (
	"encoding/json"
	"fmt"
)

// observeUsageSummarySegmentsCTEs extends the Usage Summary statement with
// window-scoped canonical cost segments. Keeping this fragment beside the
// decoder makes the persisted-to-public segment projection one owned seam
// while LoadUsageSummary retains its single QueryRow boundary.
const observeUsageSummarySegmentsCTEs = `,
segment_groups AS (
	SELECT
		canonical_segment_key,
		CASE WHEN canonical_segment_key LIKE 'e.%' THEN MAX(reporting_currency_epoch) END AS reporting_currency_epoch,
		CASE
			WHEN canonical_segment_key LIKE 'e.%' THEN
				(ARRAY_AGG(report_currency_code ORDER BY created_at DESC, id DESC)
					FILTER (WHERE report_currency_code IS NOT NULL))[1]
			WHEN canonical_segment_key <> 'l.__unknown__' THEN SUBSTRING(canonical_segment_key FROM 3)
		END AS currency_code,
		CASE WHEN canonical_segment_key <> 'l.__unknown__' THEN
			(ARRAY_AGG(report_currency_symbol ORDER BY created_at DESC, id DESC)
				FILTER (WHERE report_currency_symbol IS NOT NULL AND report_currency_symbol <> ''))[1]
		END AS display_symbol,
		ARRAY_AGG(report_currency_symbol ORDER BY created_at, id)
			FILTER (WHERE report_currency_symbol IS NOT NULL AND report_currency_symbol <> '') AS observed_symbols,
		COUNT(*)::int AS request_count,
		COUNT(*) FILTER (WHERE pricing_status IN ('priced','unpriced','unknown'))::int AS eligible_count,
		COUNT(*) FILTER (WHERE pricing_status = 'ineligible')::int AS ineligible_count,
		COUNT(*) FILTER (WHERE pricing_status = 'priced')::int AS priced_count,
		COUNT(*) FILTER (WHERE pricing_status = 'unpriced')::int AS unpriced_count,
		COUNT(*) FILTER (WHERE pricing_status = 'unknown')::int AS unknown_count,
		COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'PRICING_DISABLED')::int AS pricing_disabled,
		COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_TOKEN_USAGE')::int AS missing_token_usage,
		COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'STREAM_USAGE_UNAVAILABLE')::int AS stream_usage_unavailable,
		COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_PRICE_DATA')::int AS missing_price_data,
		COALESCE(SUM(total_cost_user_currency_micros) FILTER (WHERE pricing_status = 'priced' AND pricing_evidence_trust = 'trusted'), 0) AS trusted_cost_micros,
		COUNT(total_cost_user_currency_micros) FILTER (WHERE pricing_status = 'priced' AND pricing_evidence_trust = 'trusted')::int AS trusted_cost_samples
	FROM classified
	GROUP BY canonical_segment_key
),
segments AS (
	SELECT COALESCE(JSONB_AGG(JSONB_BUILD_OBJECT(
		'segment_key', canonical_segment_key,
		'reporting_currency_epoch', reporting_currency_epoch,
		'currency_attribution', CASE WHEN reporting_currency_epoch IS NULL THEN 'legacy_unknown' ELSE 'identified' END,
		'currency_code', currency_code,
		'display_symbol', display_symbol,
		'observed_symbols', observed_symbols,
		'request_count', request_count,
		'pricing_eligible_request_count', eligible_count,
		'pricing_ineligible_request_count', ineligible_count,
		'priced_request_count', priced_count,
		'unpriced_request_count', unpriced_count,
		'pricing_unknown_request_count', unknown_count,
		'unpriced_reason_counts', JSONB_BUILD_OBJECT(
			'PRICING_DISABLED', pricing_disabled,
			'MISSING_TOKEN_USAGE', missing_token_usage,
			'STREAM_USAGE_UNAVAILABLE', stream_usage_unavailable,
			'MISSING_PRICE_DATA', missing_price_data),
		'pricing_coverage_state', CASE
			WHEN eligible_count = 0 THEN 'no_eligible'
			WHEN unpriced_count = 0 AND unknown_count = 0 THEN 'complete'
			WHEN priced_count > 0 THEN 'partial'
			ELSE 'no_trusted_cost'
		END,
		'known_cost_micros', CASE
			WHEN canonical_segment_key <> 'l.__unknown__' AND priced_count > 0 AND trusted_cost_samples > 0
			THEN trusted_cost_micros::text
		END
	) ORDER BY
		CASE
			WHEN canonical_segment_key LIKE 'e.%' THEN 0
			WHEN canonical_segment_key <> 'l.__unknown__' THEN 1
			ELSE 2
		END,
		reporting_currency_epoch DESC NULLS LAST,
		canonical_segment_key ASC), '[]'::jsonb) AS value
	FROM segment_groups
)`

func decodeObserveCostSegments(profileID int, raw []byte) ([]ObserveCostSegment, error) {
	segments := make([]ObserveCostSegment, 0)
	if err := json.Unmarshal(raw, &segments); err != nil {
		return nil, fmt.Errorf("decode usage-summary cost segments for profile %d: %w", profileID, err)
	}
	for index := range segments {
		segments[index].ObservedSymbols = dedupeSymbols(segments[index].ObservedSymbols)
		segments[index].ObservedSymbolCount = len(segments[index].ObservedSymbols)
		if segments[index].ObservedSymbolCount > 8 {
			segments[index].ObservedSymbolsTruncated = true
			segments[index].ObservedSymbols = segments[index].ObservedSymbols[:8]
		}
	}
	return segments, nil
}

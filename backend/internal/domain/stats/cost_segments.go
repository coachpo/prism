package stats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Cost segments catalogue (Pricing SPEC §5.7; Observe SPEC §3.4). Segment keys
// are server-generated and unique per response: identified rows are
// `e.<positive epoch>`, legacy rows with a verifiable code are `l.<AAA>`,
// and code-missing/invalid rows are `l.__unknown__`. Money sorting/summing
// only applies to `e.N` and `l.AAA` with trusted known cost; `l.__unknown__`
// is counts/drilldown only with canonical cost always null.

// CurrencyCostSegment is one canonical segment.
type CurrencyCostSegment struct {
	SegmentKey                    string                         `json:"segment_key"`
	ReportingCurrencyEpoch        *int                           `json:"reporting_currency_epoch"`
	CurrencyAttribution           string                         `json:"currency_attribution"`
	CurrencyCode                  *string                        `json:"currency_code"`
	DisplaySymbol                 *string                        `json:"display_symbol"`
	ObservedSymbols               []string                       `json:"observed_symbols"`
	ObservedSymbolCount           int                            `json:"observed_symbol_count"`
	ObservedSymbolsTruncated      bool                           `json:"observed_symbols_truncated"`
	RequestCount                  int                            `json:"request_count"`
	PricingEligibleRequestCount   int                            `json:"pricing_eligible_request_count"`
	PricingIneligibleRequestCount int                            `json:"pricing_ineligible_request_count"`
	PricedRequestCount            int                            `json:"priced_request_count"`
	UnpricedRequestCount          int                            `json:"unpriced_request_count"`
	PricingUnknownRequestCount    int                            `json:"pricing_unknown_request_count"`
	PricingCoverageState          string                         `json:"pricing_coverage_state"`
	UnpricedReasonCounts          UnpricedReasonCounts           `json:"unpriced_reason_counts"`
	KnownCostMicros               *string                        `json:"known_cost_micros"`
	PricingCardRoleBreakdown      []PricingCardRoleCostBreakdown `json:"pricing_card_role_breakdown"`
}

// UnpricedReasonCounts is the fixed four-reason breakdown.
type UnpricedReasonCounts struct {
	PRICING_DISABLED         int `json:"PRICING_DISABLED"`
	MISSING_TOKEN_USAGE      int `json:"MISSING_TOKEN_USAGE"`
	STREAM_USAGE_UNAVAILABLE int `json:"STREAM_USAGE_UNAVAILABLE"`
	MISSING_PRICE_DATA       int `json:"MISSING_PRICE_DATA"`
}

// CostSegmentPage is the bounded catalogue page.
type CostSegmentPage struct {
	CostSegments              []CurrencyCostSegment `json:"cost_segments"`
	CostSegmentsTotalCount    int                   `json:"cost_segments_total_count"`
	CostSegmentsConsumedCount int                   `json:"cost_segments_consumed_count"`
	CostSegmentsSnapshotHash  string                `json:"cost_segments_snapshot_hash"`
	CostSegmentsNextCursor    *string               `json:"cost_segments_next_cursor"`
}

// CostSegmentParams selects the segment catalogue.
type CostSegmentParams struct {
	ProfileID int
	Limit     int
	Cursor    *string
}

const (
	defaultCostSegmentLimit = 50
	maxCostSegmentLimit     = 100
)

func canonicalCostSegmentKeySQLFor(alias string) string {
	qualifier := strings.TrimSpace(alias)
	if qualifier != "" {
		qualifier += "."
	}
	return `CASE
	WHEN ` + qualifier + `reporting_currency_epoch > 0 THEN 'e.' || ` + qualifier + `reporting_currency_epoch::text
	WHEN ` + qualifier + `report_currency_code ~ '^[A-Z]{3}$' THEN 'l.' || ` + qualifier + `report_currency_code
	ELSE 'l.__unknown__'
END`
}

// canonicalCostSegmentKeySQL is the single SQL generator used by catalogue,
// Observe aggregates, and all retained-history filters.
var canonicalCostSegmentKeySQL = canonicalCostSegmentKeySQLFor("")

var costSegmentClassifiedEventsCTE = `WITH classified AS (
		SELECT *,
			` + canonicalCostSegmentKeySQL + ` AS canonical_segment_key
		FROM usage_request_events
		WHERE profile_id = $1
	)`

// ListCostSegments returns one bounded page of canonical segments ordered by
// identified epoch desc, then legacy code asc, unknown last.
func ListCostSegments(ctx context.Context, exec queryExecutor, params CostSegmentParams, cursorSigningKey []byte) (CostSegmentPage, error) {
	if len(cursorSigningKey) == 0 {
		return CostSegmentPage{}, fmt.Errorf("cost segment cursor signing key is unavailable")
	}
	if params.Limit <= 0 {
		params.Limit = defaultCostSegmentLimit
	}
	if params.Limit > maxCostSegmentLimit {
		params.Limit = maxCostSegmentLimit
	}

	rows, err := exec.Query(ctx, costSegmentClassifiedEventsCTE+`
		SELECT
			CASE
				WHEN canonical_segment_key LIKE 'e.%' THEN MAX(reporting_currency_epoch)
			END AS reporting_currency_epoch,
			CASE
				WHEN canonical_segment_key LIKE 'e.%' THEN
					(ARRAY_AGG(report_currency_code ORDER BY created_at DESC, id DESC)
						FILTER (WHERE report_currency_code IS NOT NULL))[1]
				WHEN canonical_segment_key <> 'l.__unknown__' THEN SUBSTRING(canonical_segment_key FROM 3)
			END AS report_currency_code,
			CASE
				WHEN canonical_segment_key <> 'l.__unknown__' THEN
					(ARRAY_AGG(report_currency_symbol ORDER BY created_at DESC, id DESC)
						FILTER (WHERE report_currency_symbol IS NOT NULL AND report_currency_symbol <> ''))[1]
			END AS display_symbol,
			COUNT(*) AS request_count,
			COUNT(*) FILTER (WHERE pricing_status IN ('priced','unpriced','unknown')) AS eligible_count,
			COUNT(*) FILTER (WHERE pricing_status = 'ineligible') AS ineligible_count,
			COUNT(*) FILTER (WHERE pricing_status = 'priced') AS priced_count,
			COUNT(*) FILTER (WHERE pricing_status = 'unpriced') AS unpriced_count,
			COUNT(*) FILTER (WHERE pricing_status = 'unknown') AS unknown_count,
			COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'PRICING_DISABLED') AS pricing_disabled,
			COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_TOKEN_USAGE') AS missing_token_usage,
			COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'STREAM_USAGE_UNAVAILABLE') AS stream_usage_unavailable,
			COUNT(*) FILTER (WHERE pricing_status = 'unpriced' AND unpriced_reason = 'MISSING_PRICE_DATA') AS missing_price_data,
			COALESCE(SUM(total_cost_user_currency_micros) FILTER (WHERE pricing_status = 'priced' AND pricing_evidence_trust = 'trusted'), 0) AS trusted_cost_micros,
			COUNT(total_cost_user_currency_micros) FILTER (WHERE pricing_status = 'priced' AND pricing_evidence_trust = 'trusted') AS trusted_cost_samples,
			ARRAY_AGG(report_currency_symbol ORDER BY created_at, id)
				FILTER (WHERE report_currency_symbol IS NOT NULL AND report_currency_symbol <> '') AS observed_symbols,
			COALESCE((
				SELECT JSONB_AGG(JSONB_BUILD_OBJECT(
					'card_role', role_rows.pricing_card_role,
					'request_count', role_rows.request_count,
					'priced_request_count', role_rows.priced_request_count,
					'known_cost_micros', CASE WHEN grouped.canonical_segment_key <> 'l.__unknown__' AND role_rows.priced_request_count > 0 AND role_rows.trusted_cost_samples > 0 THEN role_rows.trusted_cost_micros::text END
				) ORDER BY role_rows.pricing_card_role)
				FROM (
					SELECT pricing_card_role, COUNT(*)::int AS request_count,
						COUNT(*) FILTER (WHERE pricing_status = 'priced')::int AS priced_request_count,
						COALESCE(SUM(total_cost_user_currency_micros) FILTER (WHERE pricing_status = 'priced' AND pricing_evidence_trust = 'trusted'), 0) AS trusted_cost_micros,
						COUNT(total_cost_user_currency_micros) FILTER (WHERE pricing_status = 'priced' AND pricing_evidence_trust = 'trusted')::int AS trusted_cost_samples
					FROM classified AS role_events
					WHERE role_events.canonical_segment_key = grouped.canonical_segment_key
						AND role_events.pricing_selection_state = 'selected'
						AND role_events.pricing_card_role IS NOT NULL
					GROUP BY pricing_card_role
				) AS role_rows
			), '[]'::jsonb) AS pricing_card_role_breakdown
		FROM classified AS grouped
		GROUP BY grouped.canonical_segment_key`, params.ProfileID)
	if err != nil {
		return CostSegmentPage{}, fmt.Errorf("query cost segments for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()

	segments := make([]CurrencyCostSegment, 0)
	for rows.Next() {
		var segment CurrencyCostSegment
		var trustedCost int64
		var trustedCostSamples int
		var observedSymbols []string
		var displaySymbol *string
		var roleBreakdownJSON []byte
		if err := rows.Scan(
			&segment.ReportingCurrencyEpoch,
			&segment.CurrencyCode,
			&displaySymbol,
			&segment.RequestCount,
			&segment.PricingEligibleRequestCount,
			&segment.PricingIneligibleRequestCount,
			&segment.PricedRequestCount,
			&segment.UnpricedRequestCount,
			&segment.PricingUnknownRequestCount,
			&segment.UnpricedReasonCounts.PRICING_DISABLED,
			&segment.UnpricedReasonCounts.MISSING_TOKEN_USAGE,
			&segment.UnpricedReasonCounts.STREAM_USAGE_UNAVAILABLE,
			&segment.UnpricedReasonCounts.MISSING_PRICE_DATA,
			&trustedCost,
			&trustedCostSamples,
			&observedSymbols,
			&roleBreakdownJSON,
		); err != nil {
			return CostSegmentPage{}, fmt.Errorf("scan cost segment: %w", err)
		}
		segment.DisplaySymbol = displaySymbol
		if len(roleBreakdownJSON) > 0 {
			if err := json.Unmarshal(roleBreakdownJSON, &segment.PricingCardRoleBreakdown); err != nil {
				return CostSegmentPage{}, fmt.Errorf("decode cost segment role breakdown: %w", err)
			}
		}
		// Deduplicate observed symbols preserving first-seen order (max 8).
		segment.ObservedSymbols = dedupeSymbols(observedSymbols)
		segment.ObservedSymbolCount = len(segment.ObservedSymbols)
		segment.ObservedSymbolsTruncated = false
		if segment.ObservedSymbolCount > 8 {
			segment.ObservedSymbolsTruncated = true
			segment.ObservedSymbols = segment.ObservedSymbols[:8]
		}
		// Segment key derivation (Pricing SPEC §5.7) uses the same Go
		// authority as detail and finalized-summary projections. The SQL CTE
		// has already applied the equivalent canonical predicate to the group.
		legacyCode := ""
		legacyCodeValid := segment.CurrencyCode != nil
		if legacyCodeValid {
			legacyCode = *segment.CurrencyCode
		}
		segment.SegmentKey = CostSegmentKeyFor(segment.ReportingCurrencyEpoch, legacyCode, legacyCodeValid)
		if strings.HasPrefix(segment.SegmentKey, "e.") {
			segment.CurrencyAttribution = "identified"
		} else {
			segment.CurrencyAttribution = "legacy_unknown"
		}
		segment.PricingCoverageState = deriveCoverageState(segment.PricingEligibleRequestCount, segment.PricedRequestCount, segment.UnpricedRequestCount, segment.PricingUnknownRequestCount)
		if segment.PricedRequestCount > 0 && trustedCostSamples > 0 {
			cost := int64String(trustedCost)
			segment.KnownCostMicros = &cost
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return CostSegmentPage{}, fmt.Errorf("iterate cost segments: %w", err)
	}

	sortCostSegments(segments)
	totalCount := len(segments)
	page := CostSegmentPage{
		CostSegmentsTotalCount: totalCount,
	}
	// Keyset cursor over the ordered segments (server-before-limit).
	start := 0
	if params.Cursor != nil && strings.TrimSpace(*params.Cursor) != "" {
		decoded, err := decodeCostSegmentCursor(*params.Cursor, cursorSigningKey)
		if err != nil {
			return CostSegmentPage{}, &HTTPError{StatusCode: 400, Code: "cost_segment_cursor_invalid", Detail: "Cost segment cursor is invalid."}
		}
		if decoded.ProfileID != params.ProfileID {
			return CostSegmentPage{}, &HTTPError{StatusCode: 422, Code: "cost_segment_cursor_scope_mismatch", Detail: "Cost segment cursor does not match the profile."}
		}
		for index, segment := range segments {
			if segment.SegmentKey == decoded.LastSegmentKey {
				start = index + 1
				break
			}
		}
		page.CostSegmentsConsumedCount = decoded.Consumed
	}
	end := start + params.Limit
	if end > totalCount {
		end = totalCount
	}
	page.CostSegments = segments[start:end]
	page.CostSegmentsConsumedCount += len(page.CostSegments)
	if end < totalCount {
		lastKey := segments[end-1].SegmentKey
		encoded, err := encodeCostSegmentCursor(costSegmentCursorPayload{Version: 1, ProfileID: params.ProfileID, LastSegmentKey: lastKey, Consumed: page.CostSegmentsConsumedCount}, cursorSigningKey)
		if err != nil {
			return CostSegmentPage{}, err
		}
		page.CostSegmentsNextCursor = &encoded
	}
	page.CostSegmentsSnapshotHash = costSegmentSnapshotHash(segments)
	return page, nil
}

// NormalizeCostSegmentKey validates the public filter grammar and returns the
// canonical trimmed value. An empty value means that no filter was requested.
func NormalizeCostSegmentKey(raw string) (*string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	if !isCanonicalCostSegmentKey(value) {
		return nil, &HTTPError{StatusCode: 400, Code: "cost_segment_key_invalid", Detail: "Cost segment key is invalid."}
	}
	return &value, nil
}

func dedupeSymbols(symbols []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if symbol == "" {
			continue
		}
		if _, duplicate := seen[symbol]; duplicate {
			continue
		}
		seen[symbol] = struct{}{}
		result = append(result, symbol)
	}
	return result
}

func isUppercaseCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, c := range code {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func deriveCoverageState(eligible int, priced int, unpriced int, unknown int) string {
	switch {
	case eligible == 0:
		return "no_eligible"
	case unpriced == 0 && unknown == 0:
		return "complete"
	case priced > 0:
		return "partial"
	default:
		return "no_trusted_cost"
	}
}

func sortCostSegments(segments []CurrencyCostSegment) {
	sort.SliceStable(segments, func(left, right int) bool {
		leftRank, rightRank := costSegmentRank(segments[left].SegmentKey), costSegmentRank(segments[right].SegmentKey)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if strings.HasPrefix(segments[left].SegmentKey, "e.") {
			// Identified epochs sort desc.
			return segments[left].SegmentKey > segments[right].SegmentKey
		}
		return segments[left].SegmentKey < segments[right].SegmentKey
	})
}

// costSegmentRank: identified epochs first, then legacy codes, unknown last.
func costSegmentRank(key string) int {
	switch {
	case strings.HasPrefix(key, "e."):
		return 0
	case strings.HasPrefix(key, "l.") && key != "l.__unknown__":
		return 1
	default:
		return 2
	}
}

func costSegmentSnapshotHash(segments []CurrencyCostSegment) string {
	hasher := sha256.New()
	for _, segment := range segments {
		_, _ = hasher.Write([]byte(segment.SegmentKey + "|"))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

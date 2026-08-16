package stats

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	SegmentKey                   string            `json:"segment_key"`
	ReportingCurrencyEpoch       *int              `json:"reporting_currency_epoch"`
	CurrencyAttribution          string            `json:"currency_attribution"`
	CurrencyCode                 *string           `json:"currency_code"`
	DisplaySymbol                *string           `json:"display_symbol"`
	ObservedSymbols              []string          `json:"observed_symbols"`
	ObservedSymbolCount          int               `json:"observed_symbol_count"`
	ObservedSymbolsTruncated     bool              `json:"observed_symbols_truncated"`
	RequestCount                 int               `json:"request_count"`
	PricingEligibleRequestCount  int               `json:"pricing_eligible_request_count"`
	PricingIneligibleRequestCount int              `json:"pricing_ineligible_request_count"`
	PricedRequestCount           int               `json:"priced_request_count"`
	UnpricedRequestCount         int               `json:"unpriced_request_count"`
	PricingUnknownRequestCount   int               `json:"pricing_unknown_request_count"`
	PricingCoverageState         string            `json:"pricing_coverage_state"`
	UnpricedReasonCounts         UnpricedReasonCounts `json:"unpriced_reason_counts"`
	KnownCostMicros              *string           `json:"known_cost_micros"`
}
// UnpricedReasonCounts is the fixed four-reason breakdown.
type UnpricedReasonCounts struct {
	PRICING_DISABLED          int `json:"PRICING_DISABLED"`
	MISSING_TOKEN_USAGE       int `json:"MISSING_TOKEN_USAGE"`
	STREAM_USAGE_UNAVAILABLE  int `json:"STREAM_USAGE_UNAVAILABLE"`
	MISSING_PRICE_DATA        int `json:"MISSING_PRICE_DATA"`
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

// ListCostSegments returns one bounded page of canonical segments ordered by
// identified epoch desc, then legacy code asc, unknown last.
func ListCostSegments(ctx context.Context, exec queryExecutor, params CostSegmentParams) (CostSegmentPage, error) {
	if params.Limit <= 0 {
		params.Limit = defaultCostSegmentLimit
	}
	if params.Limit > maxCostSegmentLimit {
		params.Limit = maxCostSegmentLimit
	}

	rows, err := exec.Query(ctx, `SELECT
			reporting_currency_epoch,
			report_currency_code,
			report_currency_symbol,
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
			ARRAY_AGG(report_currency_symbol) FILTER (WHERE report_currency_symbol IS NOT NULL) AS observed_symbols
		FROM usage_request_events
		WHERE profile_id = $1
		GROUP BY reporting_currency_epoch, report_currency_code, report_currency_symbol`, params.ProfileID)
	if err != nil {
		return CostSegmentPage{}, fmt.Errorf("query cost segments for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()

	segments := make([]CurrencyCostSegment, 0)
	var symbolsOrder []string
	for rows.Next() {
		var segment CurrencyCostSegment
		var trustedCost int64
		var trustedCostSamples int
		var observedSymbols []string
		var displaySymbol *string
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
		); err != nil {
			return CostSegmentPage{}, fmt.Errorf("scan cost segment: %w", err)
		}
		segment.DisplaySymbol = displaySymbol
		// Deduplicate observed symbols preserving first-seen order (max 8).
		segment.ObservedSymbols = dedupeSymbols(observedSymbols, &symbolsOrder)
		segment.ObservedSymbolCount = len(segment.ObservedSymbols)
		segment.ObservedSymbolsTruncated = false
		if segment.ObservedSymbolCount > 8 {
			segment.ObservedSymbolsTruncated = true
			segment.ObservedSymbols = segment.ObservedSymbols[:8]
		}
		// Segment key derivation (Pricing SPEC §5.7).
		switch {
		case segment.ReportingCurrencyEpoch != nil && *segment.ReportingCurrencyEpoch > 0:
			segment.SegmentKey = fmt.Sprintf("e.%d", *segment.ReportingCurrencyEpoch)
			segment.CurrencyAttribution = "identified"
		case segment.CurrencyCode != nil && isUppercaseCode(*segment.CurrencyCode):
			segment.SegmentKey = "l." + *segment.CurrencyCode
			segment.CurrencyAttribution = "legacy_unknown"
		default:
			segment.SegmentKey = "l.__unknown__"
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
		decoded, err := decodeCostSegmentCursor(*params.Cursor)
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
		encoded, err := encodeCostSegmentCursor(costSegmentCursorPayload{Version: 1, ProfileID: params.ProfileID, LastSegmentKey: lastKey, Consumed: page.CostSegmentsConsumedCount})
		if err != nil {
			return CostSegmentPage{}, err
		}
		page.CostSegmentsNextCursor = &encoded
	}
	page.CostSegmentsSnapshotHash = costSegmentSnapshotHash(segments)
	return page, nil
}

func dedupeSymbols(symbols []string, order *[]string) []string {
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
	_ = order
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

type costSegmentCursorPayload struct {
	Version       int    `json:"v"`
	ProfileID     int    `json:"p"`
	LastSegmentKey string `json:"k"`
	Consumed      int    `json:"c"`
}

func encodeCostSegmentCursor(payload costSegmentCursorPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := signCostSegmentCursor(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func decodeCostSegmentCursor(encoded string) (costSegmentCursorPayload, error) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return costSegmentCursorPayload{}, fmt.Errorf("invalid cost segment cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return costSegmentCursorPayload{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return costSegmentCursorPayload{}, err
	}
	if !hmac.Equal(signature, signCostSegmentCursor(raw)) {
		return costSegmentCursorPayload{}, fmt.Errorf("invalid cost segment cursor signature")
	}
	var payload costSegmentCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return costSegmentCursorPayload{}, err
	}
	if payload.Version != 1 {
		return costSegmentCursorPayload{}, fmt.Errorf("unsupported cost segment cursor version")
	}
	return payload, nil
}

func signCostSegmentCursor(raw []byte) []byte {
	hasher := sha256.New()
	_, _ = hasher.Write(raw)
	return hasher.Sum(nil)
}

func costSegmentSnapshotHash(segments []CurrencyCostSegment) string {
	hasher := sha256.New()
	for _, segment := range segments {
		_, _ = hasher.Write([]byte(segment.SegmentKey + "|"))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

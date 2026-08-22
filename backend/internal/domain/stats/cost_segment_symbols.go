package stats

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// CostSegmentSymbolsPage is one offset page of every distinct nonempty symbol
// observed for a canonical cost segment.
type CostSegmentSymbolsPage struct {
	SegmentKey string   `json:"segment_key"`
	Symbols    []string `json:"symbols"`
	Total      int      `json:"total"`
	Limit      int      `json:"limit"`
	Offset     int      `json:"offset"`
}

// CostSegmentSymbolsParams selects one full-symbol page.
type CostSegmentSymbolsParams struct {
	ProfileID  int
	SegmentKey string
	Limit      int
	Offset     int
}

// ListCostSegmentSymbols returns distinct symbols in first-seen event order.
func ListCostSegmentSymbols(ctx context.Context, exec queryExecutor, params CostSegmentSymbolsParams) (CostSegmentSymbolsPage, error) {
	segmentKey, err := NormalizeCostSegmentKey(params.SegmentKey)
	if err != nil {
		return CostSegmentSymbolsPage{}, err
	}
	if segmentKey == nil {
		return CostSegmentSymbolsPage{}, &HTTPError{StatusCode: 400, Code: "cost_segment_key_invalid", Detail: "Cost segment key is invalid."}
	}
	if params.Limit <= 0 {
		params.Limit = defaultCostSegmentLimit
	}
	if params.Limit > maxCostSegmentLimit {
		params.Limit = maxCostSegmentLimit
	}
	if params.Offset < 0 {
		return CostSegmentSymbolsPage{}, &HTTPError{StatusCode: 400, Code: "cost_segment_offset_invalid", Detail: "Cost segment symbol offset is invalid."}
	}

	page := CostSegmentSymbolsPage{SegmentKey: *segmentKey, Symbols: []string{}, Limit: params.Limit, Offset: params.Offset}
	var exists bool
	err = exec.QueryRow(ctx, costSegmentClassifiedEventsCTE+`, matching AS (
		SELECT report_currency_symbol, created_at, id
		FROM classified
		WHERE canonical_segment_key = $2
	), first_seen AS (
		SELECT DISTINCT ON (report_currency_symbol)
			report_currency_symbol AS symbol, created_at, id
		FROM matching
		WHERE report_currency_symbol IS NOT NULL AND report_currency_symbol <> ''
		ORDER BY report_currency_symbol, created_at, id
	), symbol_page AS (
		SELECT symbol, created_at, id
		FROM first_seen
		ORDER BY created_at, id
		LIMIT $3 OFFSET $4
	)
	SELECT
		EXISTS (SELECT 1 FROM matching),
		COALESCE((SELECT ARRAY_AGG(symbol ORDER BY created_at, id) FROM symbol_page), ARRAY[]::text[]),
		(SELECT COUNT(*) FROM first_seen)`, params.ProfileID, *segmentKey, params.Limit, params.Offset).Scan(&exists, &page.Symbols, &page.Total)
	if err != nil {
		return CostSegmentSymbolsPage{}, fmt.Errorf("query cost segment symbols for profile %d: %w", params.ProfileID, err)
	}
	if !exists {
		return CostSegmentSymbolsPage{}, &HTTPError{StatusCode: 404, Code: "cost_segment_not_found", Detail: "Cost segment was not found."}
	}
	return page, nil
}

func isCanonicalCostSegmentKey(key string) bool {
	if key == "l.__unknown__" {
		return true
	}
	if strings.HasPrefix(key, "l.") {
		return isUppercaseCode(strings.TrimPrefix(key, "l."))
	}
	if !strings.HasPrefix(key, "e.") {
		return false
	}
	epoch, err := strconv.Atoi(strings.TrimPrefix(key, "e."))
	return err == nil && epoch > 0 && key == "e."+strconv.Itoa(epoch)
}

package stats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The outer page is keyed by the finalized usage event: `usage_request_events`
// holds exactly one row per ingress, so a page is a keyset walk along
// (profile_id, created_at) that reads only the rows it returns. Deriving the
// same order from `request_logs` instead means grouping the whole window by
// ingress and re-deriving MIN(created_at) on every page, which costs the same
// on page 500 as on page 1 and grows with the retention window rather than
// with the page.
//
// Chains without a usage event are still retained history and must stay
// visible (Requests SPEC §6.5). They are resolved by a second statement
// bounded by the time span this page actually covers, then merged into the
// ordered page. That keeps the anti-join proportional to one page instead of
// to the window.

// selectChainIngressSet resolves one ordered outer page of ingresses. With
// finalized cohort selectors the authoritative finalized usage events are the
// whole ingress set (Requests SPEC §6.4): a chain with no usage event cannot
// satisfy a finalized selector, so no merge is needed. In ordinary mode the
// page is the merge of the finalized walk and the retained-only chains.
func selectChainIngressSet(ctx context.Context, exec queryExecutor, params ChainQueryParams, cursor chainCursorPayload, hasCursor bool, sortOrder string) ([]chainIngressRef, error) {
	pageSize := params.ChainLimit + 1
	finalized, err := selectFinalizedChainIngressPage(ctx, exec, params, cursor, hasCursor, sortOrder, pageSize)
	if err != nil {
		return nil, err
	}
	if usesFinalizedChainCohort(params) {
		return finalized, nil
	}
	// An exact ingress selector resolves to at most one chain. Once the
	// finalized walk has it, there is nothing for the retained-only lookup to
	// add, and that lookup carries no window to prune on.
	if len(finalized) > 0 && params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		return finalized, nil
	}
	var cursorAt time.Time
	if hasCursor {
		parsed, parseErr := time.Parse(time.RFC3339Nano, cursor.OrderAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parse chain cursor timestamp: %w", parseErr)
		}
		cursorAt = parsed.UTC()
	}
	spanFrom, spanTo := orphanChainSpan(params, cursorAt, hasCursor, sortOrder, finalized, pageSize)
	orphans, err := selectOrphanChainIngressPage(ctx, exec, params, cursor, hasCursor, sortOrder, pageSize, spanFrom, spanTo)
	if err != nil {
		return nil, err
	}
	if len(orphans) == 0 {
		return finalized, nil
	}
	return mergeChainIngressPages(finalized, orphans, sortOrder, pageSize), nil
}

// selectFinalizedChainIngressPage walks the finalized usage events in page
// order. The retained-rows EXISTS keeps a usage event out of the page once its
// request logs have been purged: the chain view only shows retained history.
func selectFinalizedChainIngressPage(ctx context.Context, exec queryExecutor, params ChainQueryParams, cursor chainCursorPayload, hasCursor bool, sortOrder string, pageSize int) ([]chainIngressRef, error) {
	query := `SELECT ingress_request_id, id, created_at FROM usage_request_events
		WHERE profile_id = $1
		AND EXISTS (SELECT 1 FROM request_logs retained_rows
			WHERE retained_rows.profile_id = usage_request_events.profile_id
			AND retained_rows.ingress_request_id = usage_request_events.ingress_request_id)`
	queryArgs := []any{params.ProfileID}
	appendArg := func(value any) int {
		queryArgs = append(queryArgs, value)
		return len(queryArgs)
	}
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		query = fmt.Sprintf("%s AND ingress_request_id = $%d", query, appendArg(strings.TrimSpace(*params.IngressRequestID)))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		query = fmt.Sprintf("%s AND ingress_request_id ILIKE $%d", query, appendArg("%"+strings.TrimSpace(*params.Q)+"%"))
	}
	if params.ProxyAPIKeyID != nil {
		// Proxy-key attribution is a retained-row fact for the ordinary
		// cohort and a finalized fact for the finalized cohort. Each cohort
		// keeps the relation it has always been filtered on; switching the
		// spine must not silently re-scope a filter.
		if usesFinalizedChainCohort(params) {
			query = fmt.Sprintf("%s AND proxy_api_key_id_snapshot = $%d", query, appendArg(*params.ProxyAPIKeyID))
		} else {
			query = fmt.Sprintf("%s AND EXISTS (SELECT 1 FROM request_logs key_rows WHERE key_rows.profile_id = usage_request_events.profile_id AND key_rows.ingress_request_id = usage_request_events.ingress_request_id AND key_rows.proxy_api_key_id_snapshot = $%d)", query, appendArg(*params.ProxyAPIKeyID))
		}
	}
	if params.FromTime != nil {
		query = fmt.Sprintf("%s AND created_at >= $%d", query, appendArg(params.FromTime.UTC()))
	}
	if params.ToTime != nil {
		query = fmt.Sprintf("%s AND created_at < $%d", query, appendArg(params.ToTime.UTC()))
	}
	query += buildFinalizedChainSelectorClauses(&queryArgs, params)
	if hasChainRowFilter(params) {
		query = appendChainRowCohortExists(query, &queryArgs, params, "usage_request_events")
	}
	if hasCursor {
		queryArgs = append(queryArgs, cursor.OrderAt, cursor.IngressID)
		query += fmt.Sprintf(" AND (created_at, ingress_request_id) %s ($%d, $%d)",
			chainKeysetOperator(sortOrder), len(queryArgs)-1, len(queryArgs))
	}
	query += fmt.Sprintf(" ORDER BY created_at %s, ingress_request_id %s LIMIT $%d",
		strings.ToUpper(sortOrder), strings.ToUpper(sortOrder), appendArg(pageSize))

	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query chain ingress set for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	ingresses := make([]chainIngressRef, 0, pageSize)
	for rows.Next() {
		var ref chainIngressRef
		if err := rows.Scan(&ref.IngressRequestID, &ref.UsageEventID, &ref.OrderAt); err != nil {
			return nil, fmt.Errorf("scan chain ingress set: %w", err)
		}
		ref.OrderAt = ref.OrderAt.UTC()
		ingresses = append(ingresses, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chain ingress set: %w", err)
	}
	return ingresses, nil
}

// buildFinalizedChainSelectorClauses renders the finalized cohort selectors
// against the bare `usage_request_events` scope.
func buildFinalizedChainSelectorClauses(args *[]any, params ChainQueryParams) string {
	query := ""
	appendArg := func(value any) int {
		*args = append(*args, value)
		return len(*args)
	}
	if params.IngressFinalResult != nil {
		// Shared finalized classifier (Observe SPEC §3.2): final_result is
		// derived, never a stored column.
		classifier := `CASE WHEN status_code NOT BETWEEN 200 AND 299 THEN 'failed'
			WHEN stream_outcome = 'client_disconnected' THEN 'client_disconnected'
			WHEN stream_outcome IN ('provider_incomplete','upstream_read_error','gateway_timeout','upstream_ended_without_terminal','unknown') THEN 'failed'
			ELSE 'completed' END`
		query = fmt.Sprintf("%s AND %s = $%d", query, classifier, appendArg(*params.IngressFinalResult))
	}
	if params.ConfirmedFailover != nil {
		query = fmt.Sprintf("%s AND failover_occurred = $%d", query, appendArg(*params.ConfirmedFailover))
	}
	if params.FinalTargetModelID != nil && strings.TrimSpace(*params.FinalTargetModelID) != "" {
		query = fmt.Sprintf("%s AND resolved_target_model_id = $%d", query, appendArg(strings.TrimSpace(*params.FinalTargetModelID)))
	}
	if params.PricingStatus != nil {
		query = fmt.Sprintf("%s AND pricing_status = $%d", query, appendArg(*params.PricingStatus))
	}
	if len(params.UnpricedReasons) > 0 {
		placeholders := make([]string, 0, len(params.UnpricedReasons))
		for _, reason := range params.UnpricedReasons {
			placeholders = append(placeholders, fmt.Sprintf("$%d", appendArg(reason)))
		}
		query += " AND unpriced_reason IN (" + strings.Join(placeholders, ",") + ")"
	}
	if params.ReportingCurrencyEpoch != nil && strings.TrimSpace(*params.ReportingCurrencyEpoch) != "" {
		if *params.ReportingCurrencyEpoch == "__legacy_unknown__" {
			query += " AND reporting_currency_epoch IS NULL"
		} else {
			query = fmt.Sprintf("%s AND reporting_currency_epoch = $%d", query, appendArg(*params.ReportingCurrencyEpoch))
		}
	}
	if params.IsStream != nil {
		query = fmt.Sprintf("%s AND is_stream = $%d", query, appendArg(*params.IsStream))
	}
	if usesFinalizedChainCohort(params) && len(params.StreamOutcomes) > 0 {
		// stream_outcome is a retained-row selector that the finalized cohort
		// additionally applies to the finalized event. Narrower than the
		// totals, which reach it only through the row EXISTS; preserved as-is
		// because widening it is a cohort change, not a performance one.
		placeholders := make([]string, 0, len(params.StreamOutcomes))
		for _, outcome := range params.StreamOutcomes {
			placeholders = append(placeholders, fmt.Sprintf("$%d", appendArg(outcome)))
		}
		query += " AND stream_outcome IN (" + strings.Join(placeholders, ",") + ")"
	}
	if len(params.IngressFinalStatusCodes) > 0 {
		placeholders := make([]string, 0, len(params.IngressFinalStatusCodes))
		for _, code := range params.IngressFinalStatusCodes {
			placeholders = append(placeholders, fmt.Sprintf("$%d", appendArg(code)))
		}
		query += " AND status_code IN (" + strings.Join(placeholders, ",") + ")"
	}
	if params.CostSegmentKey != nil && strings.TrimSpace(*params.CostSegmentKey) != "" {
		query = fmt.Sprintf("%s AND %s = $%d", query, canonicalCostSegmentKeySQLFor(""), appendArg(strings.TrimSpace(*params.CostSegmentKey)))
	}
	// Upstream status is a retained-row selector, not a finalized usage fact;
	// it arrives through the ingress-level row EXISTS instead.
	return query
}

// orphanChainSpan is the created_at range the retained-only lookup has to
// cover for this page: from the cursor (or the window edge) down to the last
// finalized row the page walked. A short page means the finalized walk reached
// the end of the window, so the span runs to the window edge instead.
// Inclusive timestamps need one PostgreSQL microsecond before becoming an
// exclusive upper bound; pgx truncates sub-microsecond increments.
func orphanChainSpan(params ChainQueryParams, cursorAt time.Time, hasCursor bool, sortOrder string, finalized []chainIngressRef, pageSize int) (*time.Time, *time.Time) {
	from, to := params.FromTime, params.ToTime
	pageComplete := len(finalized) == pageSize
	var edge *time.Time
	if pageComplete {
		last := finalized[len(finalized)-1].OrderAt
		edge = &last
	}
	if sortOrder == "desc" {
		if hasCursor {
			upper := cursorAt.Add(time.Microsecond)
			to = &upper
		}
		if edge != nil {
			from = edge
		}
		return from, to
	}
	if hasCursor {
		lower := cursorAt
		from = &lower
	}
	if edge != nil {
		upper := edge.Add(time.Microsecond)
		to = &upper
	}
	return from, to
}

// selectOrphanChainIngressPage resolves the chains that have retained rows but
// no usage event. Candidates come from the page's own time span; their order
// key is then re-derived over the whole cohort window so a chain that straddles
// a page boundary sorts identically on both pages and the keyset excludes it
// exactly once.
func selectOrphanChainIngressPage(ctx context.Context, exec queryExecutor, params ChainQueryParams, cursor chainCursorPayload, hasCursor bool, sortOrder string, pageSize int, spanFrom, spanTo *time.Time) ([]chainIngressRef, error) {
	queryArgs := []any{params.ProfileID}
	appendArg := func(value any) int {
		queryArgs = append(queryArgs, value)
		return len(queryArgs)
	}
	candidates := `SELECT DISTINCT rl.ingress_request_id
			FROM request_logs rl
			WHERE rl.profile_id = $1 AND rl.ingress_request_id IS NOT NULL`
	candidates += chainRowBoundFragments(&queryArgs, spanFrom, spanTo, "rl")[0]
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		candidates = fmt.Sprintf("%s AND rl.ingress_request_id = $%d", candidates, appendArg(strings.TrimSpace(*params.IngressRequestID)))
	}
	if params.Q != nil && strings.TrimSpace(*params.Q) != "" {
		candidates = fmt.Sprintf("%s AND rl.ingress_request_id ILIKE $%d", candidates, appendArg("%"+strings.TrimSpace(*params.Q)+"%"))
	}
	if params.ProxyAPIKeyID != nil {
		candidates = fmt.Sprintf("%s AND EXISTS (SELECT 1 FROM request_logs key_rows WHERE key_rows.profile_id = rl.profile_id AND key_rows.ingress_request_id = rl.ingress_request_id AND key_rows.proxy_api_key_id_snapshot = $%d)", candidates, appendArg(*params.ProxyAPIKeyID))
	}
	if hasChainRowFilter(params) {
		candidates = appendChainRowCohortExists(candidates, &queryArgs, params, "rl")
	}

	windowClause := chainRowBoundFragments(&queryArgs, params.FromTime, params.ToTime, "chain_rows")[0]
	query := `SELECT candidates.ingress_request_id, MIN(chain_rows.created_at) AS order_at
		FROM (` + candidates + `) AS candidates
		JOIN request_logs chain_rows
			ON chain_rows.profile_id = $1
			AND chain_rows.ingress_request_id = candidates.ingress_request_id` + windowClause + `
		WHERE NOT EXISTS (SELECT 1 FROM usage_request_events ue
			WHERE ue.profile_id = $1 AND ue.ingress_request_id = candidates.ingress_request_id)
		GROUP BY candidates.ingress_request_id`
	if hasCursor {
		queryArgs = append(queryArgs, cursor.OrderAt, cursor.IngressID)
		query += fmt.Sprintf(" HAVING (MIN(chain_rows.created_at), candidates.ingress_request_id) %s ($%d, $%d)",
			chainKeysetOperator(sortOrder), len(queryArgs)-1, len(queryArgs))
	}
	query += fmt.Sprintf(" ORDER BY order_at %s, candidates.ingress_request_id %s LIMIT $%d",
		strings.ToUpper(sortOrder), strings.ToUpper(sortOrder), appendArg(pageSize))

	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query retained-only chain ingress set for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	ingresses := make([]chainIngressRef, 0)
	for rows.Next() {
		var ref chainIngressRef
		if err := rows.Scan(&ref.IngressRequestID, &ref.OrderAt); err != nil {
			return nil, fmt.Errorf("scan retained-only chain ingress set: %w", err)
		}
		ref.OrderAt = ref.OrderAt.UTC()
		ingresses = append(ingresses, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retained-only chain ingress set: %w", err)
	}
	return ingresses, nil
}

// mergeChainIngressPages merges two already-ordered pages into one page of at
// most pageSize refs. Both sources carry the same (order_at, ingress) key, so
// the merged page and its cursor stay on the single outer order.
func mergeChainIngressPages(finalized, orphans []chainIngressRef, sortOrder string, pageSize int) []chainIngressRef {
	merged := make([]chainIngressRef, 0, len(finalized)+len(orphans))
	merged = append(merged, finalized...)
	merged = append(merged, orphans...)
	descending := sortOrder == "desc"
	sort.SliceStable(merged, func(left, right int) bool {
		if !merged[left].OrderAt.Equal(merged[right].OrderAt) {
			if descending {
				return merged[left].OrderAt.After(merged[right].OrderAt)
			}
			return merged[left].OrderAt.Before(merged[right].OrderAt)
		}
		if descending {
			return merged[left].IngressRequestID > merged[right].IngressRequestID
		}
		return merged[left].IngressRequestID < merged[right].IngressRequestID
	})
	if len(merged) > pageSize {
		merged = merged[:pageSize]
	}
	return merged
}

func chainKeysetOperator(sortOrder string) string {
	if sortOrder == "desc" {
		return "<"
	}
	return ">"
}

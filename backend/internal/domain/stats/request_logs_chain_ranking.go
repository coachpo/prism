package stats

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ChainRanking is one ingress metric. Groups are identities, never a shared
// monetary scale. Missing values sort after measured values in both directions.
type ChainRanking struct {
	Metric string `json:"metric"`
	Value  *int64 `json:"value"`
	Group  string `json:"group"`
	State  string `json:"state"`
}

func isChainRanking(sortBy string) bool {
	return sortBy == "elapsed_ms" || sortBy == "total_cost_user_currency_micros"
}

// buildChainRankingQuery shares the retained cohort with totals and CSV. The
// projection boundary prevents cursor predicates from repeatedly
// evaluating currency and evidence expressions over every retained ingress. The
// newest finalized event is the same authority used by loadFinalizedSummaries;
// neither orphan rows nor retry rows manufacture additional metric samples.
func buildChainRankingQuery(params ChainQueryParams) (string, []any) {
	where, args := buildChainCohortWhere(params, "request_logs")
	from, to := chainCohortRowBounds(params)
	window := chainRowBoundFragments(&args, from, to, "events")[0]
	if params.IngressRequestID != nil && strings.TrimSpace(*params.IngressRequestID) != "" {
		args = append(args, strings.TrimSpace(*params.IngressRequestID))
		window += fmt.Sprintf(" AND events.ingress_request_id=$%d", len(args))
	}

	metric := `CASE WHEN ue.ingress_completed_at >= ue.ingress_started_at THEN
  FLOOR(EXTRACT(EPOCH FROM (ue.ingress_completed_at - ue.ingress_started_at))*1000)::bigint END`
	group := `''::text`
	state := `CASE WHEN ue.id IS NULL THEN 'missing_finalized'
  WHEN ue.ingress_started_at IS NULL OR ue.ingress_completed_at IS NULL OR ue.ingress_completed_at < ue.ingress_started_at THEN 'missing_elapsed'
  ELSE 'ranked' END`
	if params.SortBy == "total_cost_user_currency_micros" {
		trusted := `ue.pricing_status = 'priced' AND ue.pricing_evidence_trust = 'trusted' AND ue.total_cost_user_currency_micros IS NOT NULL`
		known := `ue.report_currency_code ~ '^[A-Z]{3}$'`
		metric = `CASE WHEN ` + trusted + ` AND ` + known + ` THEN ue.total_cost_user_currency_micros END`
		group = `CASE WHEN ` + trusted + ` AND ` + known + ` THEN (` + canonicalCostSegmentKeySQLFor("ue") + `) || ':' || ue.report_currency_code ELSE '' END`
		state = `CASE WHEN ue.id IS NULL THEN 'missing_finalized'
   WHEN NOT (` + trusted + `) THEN 'untrusted_cost'
   WHEN ue.report_currency_code IS NULL OR NOT (` + known + `) THEN 'unknown_currency'
   ELSE 'ranked' END`
	}
	// The projection fence also keeps the CSV's outer order from expanding
	// currency/trust expressions once for each referenced sort key.
	query := `WITH retained AS (
  SELECT ingress_request_id, MIN(created_at) AS retained_at,
   COUNT(*) AS row_count,COUNT(*) FILTER (WHERE row_kind='upstream') AS upstream_count,
   COUNT(*) FILTER (WHERE row_kind='legacy_unknown') AS legacy_count FROM request_logs
  WHERE ` + where + ` GROUP BY ingress_request_id
 ), finalized AS (
  SELECT DISTINCT ON (events.ingress_request_id)
   events.ingress_request_id,events.id,events.created_at,events.ingress_started_at,events.ingress_completed_at,
   events.pricing_status,events.pricing_evidence_trust,events.total_cost_user_currency_micros,
   events.report_currency_code,events.reporting_currency_epoch
  FROM usage_request_events events WHERE events.profile_id=$1` + window + `
  ORDER BY events.ingress_request_id,events.id DESC
 )
 SELECT retained.ingress_request_id, COALESCE(ue.id,0) AS usage_event_id,
  COALESCE(ue.created_at,retained.retained_at) AS order_at,
  ` + metric + ` AS rank_value,` + group + ` AS rank_group,` + state + ` AS rank_state,
  retained.row_count,retained.upstream_count,retained.legacy_count` + `
 FROM retained LEFT JOIN finalized ue ON ue.ingress_request_id=retained.ingress_request_id` + ` OFFSET 0`
	return query, args
}

func chainRankingOrderBy(alias, sortOrder string) string {
	if alias != "" {
		alias += "."
	}
	direction := "DESC"
	if sortOrder == "asc" {
		direction = "ASC"
	}
	return fmt.Sprintf("ORDER BY (%[1]srank_value IS NULL) ASC, %[1]srank_group ASC, %[1]srank_value %[2]s NULLS LAST, %[1]sorder_at %[2]s, %[1]singress_request_id %[2]s", alias, direction)
}

// Custom planning preserves parameter-aware partition pruning and array-ID
// probes on every page. Repeated real-API plans otherwise switch to generic
// estimates after the prepared-statement threshold and violate page budgets.
func prepareChainRankingRead(ctx context.Context, exec queryExecutor) error {
	if _, err := exec.Exec(ctx, "SET LOCAL plan_cache_mode = force_custom_plan"); err != nil {
		return fmt.Errorf("configure ranked read planning: %w", err)
	}
	return nil
}

func selectRankedChainIngressPage(ctx context.Context, exec queryExecutor, params ChainQueryParams, cursor chainCursorPayload, hasCursor bool, sortOrder string) ([]chainIngressRef, *chainRankingTotals, error) {
	if err := prepareChainRankingRead(ctx, exec); err != nil {
		return nil, nil, err
	}
	base, args := buildChainRankingQuery(params)
	page := `SELECT ingress_request_id,usage_event_id,order_at,rank_value,rank_group,rank_state FROM ranking`
	if hasCursor {
		args = append(args, cursor.RankValue == nil, cursor.RankGroup, cursor.RankValue, cursor.OrderAt, cursor.IngressID)
		n := len(args)
		page += fmt.Sprintf(` WHERE
   ((rank_value IS NULL),rank_group) > ($%d::boolean,$%d::text)
   OR ((rank_value IS NULL)=$%d AND rank_group=$%d AND (
    rank_value %s $%d::bigint OR
    (rank_value IS NOT DISTINCT FROM $%d::bigint AND (order_at,ingress_request_id) %s ($%d::timestamptz,$%d::text))))`,
			n-4, n-3, n-4, n-3, chainKeysetOperator(sortOrder), n-2, n-2, chainKeysetOperator(sortOrder), n-1, n)
	}
	args = append(args, params.ChainLimit+1)
	page += " " + chainRankingOrderBy("", sortOrder) + fmt.Sprintf(" LIMIT $%d", len(args))
	// One materialized ranked cohort serves both the bounded page and complete
	// evidence counts. The LEFT JOIN preserves those counts even for empty pages.
	query := `WITH ranking AS MATERIALIZED (` + base + `)
 SELECT page.ingress_request_id,page.usage_event_id,page.order_at,page.rank_value,page.rank_group,page.rank_state,
  totals.* FROM (
   SELECT COUNT(*) AS ingresses,COALESCE(SUM(row_count),0)::bigint AS rows,
    COALESCE(SUM(upstream_count),0)::bigint AS upstream,COALESCE(SUM(legacy_count),0)::bigint AS legacy,
    COUNT(*) FILTER (WHERE rank_state='ranked') AS rankable,
    COUNT(*) FILTER (WHERE rank_state='missing_finalized') AS missing_finalized,
    COUNT(*) FILTER (WHERE rank_state='missing_elapsed') AS missing_elapsed,
    COUNT(*) FILTER (WHERE rank_state='untrusted_cost') AS untrusted_cost,
    COUNT(*) FILTER (WHERE rank_state='unknown_currency') AS unknown_currency
   FROM ranking
  ) totals LEFT JOIN LATERAL (` + page + `) page ON true ` + chainRankingOrderBy("page", sortOrder)
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query ranked ingress chains: %w", err)
	}
	defer rows.Close()
	refs := make([]chainIngressRef, 0, params.ChainLimit+1)
	totals := chainRankingTotals{}
	for rows.Next() {
		var id, group, state *string
		var eventID, value *int64
		var at *time.Time
		if err := rows.Scan(&id, &eventID, &at, &value, &group, &state, &totals.Ingresses, &totals.Rows, &totals.Upstream, &totals.Legacy, &totals.Rankable, &totals.MissingFinalized, &totals.MissingElapsed, &totals.UntrustedCost, &totals.UnknownCurrency); err != nil {
			return nil, nil, fmt.Errorf("scan ranked ingress: %w", err)
		}
		if id == nil {
			continue
		}
		if eventID == nil || at == nil || group == nil || state == nil {
			return nil, nil, fmt.Errorf("incomplete ranked ingress projection")
		}
		refs = append(refs, chainIngressRef{IngressRequestID: *id, UsageEventID: *eventID, OrderAt: at.UTC(), Ranking: &ChainRanking{Metric: params.SortBy, Value: value, Group: *group, State: *state}})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read ranked ingress: %w", err)
	}
	return refs, &totals, nil
}

func normalizeChainSortBy(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "created_at"
	}
	if value != "created_at" && !isChainRanking(value) {
		return "", &HTTPError{StatusCode: 422, Code: "chain_sort_unsupported", Detail: "Ingress chains support created_at, elapsed_ms and total_cost_user_currency_micros."}
	}
	return value, nil
}

func chainRankValue(ref chainIngressRef) *int64 {
	if ref.Ranking == nil {
		return nil
	}
	return ref.Ranking.Value
}
func chainRankGroup(ref chainIngressRef) string {
	if ref.Ranking == nil {
		return ""
	}
	return ref.Ranking.Group
}

type ChainRankingCoverage struct {
	Metric                 string         `json:"metric"`
	RankableIngressCount   int            `json:"rankable_ingress_count"`
	UnrankableIngressCount int            `json:"unrankable_ingress_count"`
	UnrankableReasons      map[string]int `json:"unrankable_reasons"`
}

type chainRankingTotals struct {
	Ingresses, Rows, Upstream, Legacy                                          int
	Rankable, MissingFinalized, MissingElapsed, UntrustedCost, UnknownCurrency int
}

func (totals chainRankingTotals) coverage(metric string) *ChainRankingCoverage {
	return &ChainRankingCoverage{Metric: metric, RankableIngressCount: totals.Rankable, UnrankableIngressCount: totals.Ingresses - totals.Rankable, UnrankableReasons: map[string]int{
		"missing_finalized": totals.MissingFinalized, "missing_elapsed": totals.MissingElapsed, "untrusted_cost": totals.UntrustedCost, "unknown_currency": totals.UnknownCurrency,
	}}
}

package stats

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// chainWindowSlack is how far a retained row may sit outside the requested
// window and still count towards its ingress.
//
// `request_logs` and `usage_request_events` are range-partitioned on
// created_at, and PostgreSQL only prunes partitions when created_at appears
// directly in the scanned relation's WHERE clause: a window carried inside an
// EXISTS or a LATERAL prunes nothing, so every chain statement reads the whole
// retained history no matter how narrow the window is. Bounding the scanned
// relation itself is the only way to make these reads scale with the window
// rather than with the retention period.
//
// The bound is not free: an ingress whose retained rows straddle the boundary
// by more than the slack contributes only its in-bound rows to the whole-chain
// counts. One slack day is far beyond any chain's real span — a chain is one
// client request with its retries, bounded by the runtime attempt timeouts —
// so the excluded rows are rows no retention window would keep together
// anyway.
const chainWindowSlack = 24 * time.Hour

// chainCohortRowBounds returns the created_at bounds for retained rows of the
// cohort's chains. Both results are nil when the query carries no window (an
// exact-ingress read resolved against the whole retained domain).
func chainCohortRowBounds(params ChainQueryParams) (*time.Time, *time.Time) {
	var from, to *time.Time
	if params.FromTime != nil {
		lower := params.FromTime.UTC().Add(-chainWindowSlack)
		from = &lower
	}
	if params.ToTime != nil {
		upper := params.ToTime.UTC().Add(chainWindowSlack)
		to = &upper
	}
	return from, to
}

// chainPageRowBounds returns the created_at bounds for retained rows of one
// resolved outer page. Every order key is a chain's own clock — its finalized
// usage event, or for a retained-only chain its earliest retained row — so the
// page's rows live inside that span widened by the same slack.
func chainPageRowBounds(ingresses []chainIngressRef) (*time.Time, *time.Time) {
	if len(ingresses) == 0 {
		return nil, nil
	}
	earliest := ingresses[0].OrderAt
	latest := ingresses[0].OrderAt
	for _, ingress := range ingresses[1:] {
		if ingress.OrderAt.Before(earliest) {
			earliest = ingress.OrderAt
		}
		if ingress.OrderAt.After(latest) {
			latest = ingress.OrderAt
		}
	}
	from := earliest.UTC().Add(-chainWindowSlack)
	to := latest.UTC().Add(chainWindowSlack)
	return &from, &to
}

// chainRowBoundClauses binds the created_at range for one alias, appending the
// values to args. An absent bound contributes no clause.
func chainRowBoundClauses(args *[]any, alias string, from, to *time.Time) []string {
	fragments := chainRowBoundFragments(args, from, to, alias)
	if len(fragments) == 0 || fragments[0] == "" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(fragments[0], " AND "), " AND ")
}

// chainRowBoundFragments binds one created_at range and renders it as a WHERE
// fragment for each alias. The aliases share the same two placeholders, so a
// statement can prune the outer relation and its joined relations on one pair
// of bounds instead of binding the same instant several times.
func chainRowBoundFragments(args *[]any, from, to *time.Time, aliases ...string) []string {
	fromIndex, toIndex := 0, 0
	if from != nil {
		*args = append(*args, *from)
		fromIndex = len(*args)
	}
	if to != nil {
		*args = append(*args, *to)
		toIndex = len(*args)
	}
	fragments := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		fragment := ""
		if fromIndex > 0 {
			fragment += fmt.Sprintf(" AND %s.created_at >= $%d", alias, fromIndex)
		}
		if toIndex > 0 {
			fragment += fmt.Sprintf(" AND %s.created_at < $%d", alias, toIndex)
		}
		fragments = append(fragments, fragment)
	}
	return fragments
}

// chainStatementWorkMem is the per-node sort/hash budget for one chain read.
//
// Row filters select the ingress set through an ingress-level EXISTS, and a
// non-selective filter ("2xx" matches almost every chain) turns that into a
// hash semi-join over the whole window. At the stock 4 MB the hash spills to
// disk and the page takes seconds. The budget is deliberately set per
// transaction rather than on the server: the runtime write path has no sorts
// worth the memory, and the management lane caps concurrent readers, so the
// worst case is bounded by that cap rather than by the connection count of
// the whole process.
const chainStatementWorkMem = "32MB"

// applyChainStatementWorkMem raises the sort/hash budget for the current
// transaction only. Outside a transaction PostgreSQL scopes SET LOCAL to the
// implicit single-statement transaction and warns; that is harmless, so a
// failure here is never worth failing the read for.
func applyChainStatementWorkMem(ctx context.Context, exec queryExecutor) {
	_, _ = exec.Exec(ctx, "SET LOCAL work_mem = '"+chainStatementWorkMem+"'")
}

// Ranked pages and their finalized facts use the identical retained reach as
// the cohort/totals/export, even when finalization crosses a window edge.
func chainItemRowBounds(params ChainQueryParams, ingresses []chainIngressRef) (*time.Time, *time.Time) {
	if isChainRanking(params.SortBy) {
		return chainCohortRowBounds(params)
	}
	return chainPageRowBounds(ingresses)
}

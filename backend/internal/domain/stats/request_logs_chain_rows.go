package stats

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

type retainedRowCounts struct {
	Upstream int
	Total    int
	Legacy   int
	Matched  int
}

type retainedRowsPage struct {
	Rows    []ChainRowItem
	HasMore bool
}

// chainRowSelectList is the retained-row projection shared by the exact
// single-chain page and the page batch. Columns stay unqualified so both
// entry points can select them from a bare `request_logs` scope.
const chainRowSelectList = `id, row_kind, ingress_request_id, attempt_number, attempt_trigger, attempt_result, is_winner,
			attempt_duration_ms, legacy_duration_ms, upstream_status_code, gateway_status_code, legacy_status_code,
			error_source, error_code, failure_stage, error_detail, error_detail_redacted, error_detail_truncated,
			stream_error_detail, stream_error_detail_redacted, stream_error_detail_truncated,
			stream_outcome, stream_error_kind, model_id, resolved_target_model_id, endpoint_id, connection_id,
			total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, pricing_resolution_kind, pricing_evidence_trust,
			pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_selector_threshold_tokens, pricing_selector_basis_tokens, created_at,
			endpoint_base_url, endpoint_description`

// chainRowRawDetail carries the scanned values that need post-query
// projection (failure-detail fallback and Terminal Target labels).
type chainRowRawDetail struct {
	requestLogID               int64
	errorDetail                *string
	errorDetailRedacted        bool
	errorDetailTruncated       bool
	streamErrorDetail          *string
	streamErrorDetailRedacted  bool
	streamErrorDetailTruncated bool
	endpointBaseURL            *string
	endpointDescription        *string
}

func chainRowScanDest(item *ChainRowItem) ([]any, *chainRowRawDetail) {
	raw := &chainRowRawDetail{}
	dest := []any{
		&raw.requestLogID, &item.RowKind, &item.IngressRequestID, &item.AttemptNumber, &item.AttemptTrigger, &item.AttemptResult, &item.IsWinner,
		&item.AttemptDurationMS, &item.LegacyDurationMS, &item.UpstreamStatusCode, &item.GatewayStatusCode, &item.LegacyStatusCode,
		&item.ErrorSource, &item.ErrorCode, &item.FailureStage, &raw.errorDetail, &raw.errorDetailRedacted, &raw.errorDetailTruncated,
		&raw.streamErrorDetail, &raw.streamErrorDetailRedacted, &raw.streamErrorDetailTruncated,
		&item.StreamOutcome, &item.StreamErrorKind, &item.ModelID, &item.ResolvedTargetModelID, &item.EndpointID, &item.TerminalTargetID,
		&item.TotalTokens, &item.TotalCostUserCurrencyMicros, &item.PricingStatus, &item.UnpricedReason, &item.PricingResolutionKind, &item.PricingEvidenceTrust,
		&item.PricingTemplateKind, &item.PricingSelectionState, &item.PricingCardRole, &item.PricingSelectorThresholdTokens, &item.PricingSelectorBasisTokens, &item.CreatedAt,
		&raw.endpointBaseURL, &raw.endpointDescription, &item.MatchedByFilter,
	}
	return dest, raw
}

// applyChainRowProjection finalizes one retained-row item: unified failure
// detail projection and the current-connection Terminal Target labels.
func applyChainRowProjection(item *ChainRowItem, raw *chainRowRawDetail, connectionCatalog map[int]connectionRecord) {
	item.RequestLogID = strconv.FormatInt(raw.requestLogID, 10)
	item.CreatedAt = item.CreatedAt.UTC()
	item.EndpointID = normalizePositiveID(item.EndpointID)
	item.TerminalTargetID = normalizePositiveID(item.TerminalTargetID)
	// Unified failure projection: error_detail wins; stream detail is
	// used for 2xx abnormal streams and client disconnects.
	detail := raw.errorDetail
	source := "error_detail"
	redacted := raw.errorDetailRedacted
	truncated := raw.errorDetailTruncated
	if detail == nil && raw.streamErrorDetail != nil {
		detail = raw.streamErrorDetail
		source = "stream_error_detail"
		redacted = raw.streamErrorDetailRedacted
		truncated = raw.streamErrorDetailTruncated
	}
	if detail != nil {
		preview, previewTruncated := truncateCodePoints(*detail, 240)
		item.FailureDetailPreview = &preview
		item.FailureDetailPreviewTruncated = previewTruncated
		item.FailureDetailRedacted = redacted
		item.FailureDetailPersistenceTruncated = truncated
	}
	item.FailureDetailSource = source
	// Terminal target projection from the current connection catalog (no
	// historical snapshot; label follows renames).
	if item.TerminalTargetID != nil {
		target := resolveTerminalTargetProjection(connectionCatalog, item.TerminalTargetID)
		item.TerminalTargetLabel = target.Label
		item.TerminalTargetConfigured = target.Configured
		item.TerminalTargetOwnerModelID = target.OwnerModelID
	}
}

// loadRetainedRowCountsBatch computes full-chain retained counts for every
// listed ingress in one grouped aggregate. Counts cover the whole retained
// chain (not just the query window), exactly like the historical per-ingress
// lookup; the matched count keeps the row-filter/time predicate.
func loadRetainedRowCountsBatch(ctx context.Context, exec queryExecutor, params ChainQueryParams, ingressIDs []string) (map[string]retainedRowCounts, error) {
	counts := make(map[string]retainedRowCounts, len(ingressIDs))
	if len(ingressIDs) == 0 {
		return counts, nil
	}
	queryArgs := []any{params.ProfileID, ingressIDs}
	matchPredicate := buildChainRowMatchPredicate(&queryArgs, params, "request_logs")
	query := `SELECT ingress_request_id,
			COUNT(*) FILTER (WHERE row_kind = 'upstream'),
			COUNT(*),
			COUNT(*) FILTER (WHERE row_kind = 'legacy_unknown'),
			COUNT(*) FILTER (WHERE ` + matchPredicate + `)
		FROM request_logs
		WHERE profile_id = $1 AND ingress_request_id = ANY($2)
		GROUP BY ingress_request_id`
	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query retained row counts for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var ingressID string
		var count retainedRowCounts
		if err := rows.Scan(&ingressID, &count.Upstream, &count.Total, &count.Legacy, &count.Matched); err != nil {
			return nil, fmt.Errorf("scan retained row counts: %w", err)
		}
		counts[ingressID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retained row counts: %w", err)
	}
	return counts, nil
}

// loadRetainedRowsBatch loads one bounded first retained-row page per listed
// ingress in one statement. Each ingress keeps the exact historical shape:
// `ORDER BY created_at ASC, id ASC LIMIT row_limit+1`, so the limit+1
// sentinel still determines page completeness per chain without deriving
// full-chain counts from the bounded page.
func loadRetainedRowsBatch(ctx context.Context, exec queryExecutor, params ChainQueryParams, ingressIDs []string, rowLimit int, connectionCatalog map[int]connectionRecord) (map[string]retainedRowsPage, error) {
	pages := make(map[string]retainedRowsPage, len(ingressIDs))
	if len(ingressIDs) == 0 {
		return pages, nil
	}
	queryArgs := []any{params.ProfileID, ingressIDs, rowLimit + 1}
	matchPredicate := buildChainRowMatchPredicate(&queryArgs, params, "request_logs")
	query := `SELECT p.ord, r.*
		FROM unnest($2::text[]) WITH ORDINALITY AS p(ingress_id, ord)
		CROSS JOIN LATERAL (
			SELECT ` + chainRowSelectList + `, (` + matchPredicate + `) AS matched_by_filter
			FROM request_logs
			WHERE profile_id = $1 AND ingress_request_id = p.ingress_id
			ORDER BY created_at ASC, id ASC
			LIMIT $3
		) r
		ORDER BY p.ord ASC, r.created_at ASC, r.id ASC`
	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query retained row pages for profile %d: %w", params.ProfileID, err)
	}
	defer rows.Close()

	buffers := make([][]ChainRowItem, len(ingressIDs))
	for rows.Next() {
		var ord int
		var item ChainRowItem
		dest, raw := chainRowScanDest(&item)
		if err := rows.Scan(append([]any{&ord}, dest...)...); err != nil {
			return nil, fmt.Errorf("scan retained row: %w", err)
		}
		if ord < 1 || ord > len(ingressIDs) {
			return nil, fmt.Errorf("retained row ordinal %d outside page of %d ingresses", ord, len(ingressIDs))
		}
		applyChainRowProjection(&item, raw, connectionCatalog)
		buffers[ord-1] = append(buffers[ord-1], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retained row pages: %w", err)
	}
	for index, ingressID := range ingressIDs {
		items := buffers[index]
		hasMore := len(items) > rowLimit
		if hasMore {
			items = items[:rowLimit]
		}
		pages[ingressID] = retainedRowsPage{Rows: items, HasMore: hasMore}
	}
	return pages, nil
}

// loadRetainedRows loads one bounded retained-row page for one ingress. The
// limit+1 sentinel determines page completeness without deriving full-chain
// counts from the bounded page. This is the precise row-cursor continuation
// path: only it may apply the signed (created_at, id) keyset bound.
func loadRetainedRows(ctx context.Context, exec queryExecutor, params ChainQueryParams, ingressRequestID string, rowLimit int, cursor *rowCursorPayload, connectionCatalog map[int]connectionRecord) (retainedRowsPage, error) {
	profileID := params.ProfileID
	queryArgs := []any{profileID, ingressRequestID}
	matchPredicate := buildChainRowMatchPredicate(&queryArgs, params, "request_logs")
	query := `SELECT
			` + chainRowSelectList + `,
			(` + matchPredicate + `) AS matched_by_filter
		FROM request_logs
		WHERE profile_id = $1 AND ingress_request_id = $2`
	if cursor != nil {
		orderAt, err := time.Parse(time.RFC3339Nano, cursor.OrderAt)
		if err != nil {
			return retainedRowsPage{}, fmt.Errorf("parse retained-row cursor timestamp: %w", err)
		}
		requestLogID, err := strconv.ParseInt(cursor.RequestLogID, 10, 64)
		if err != nil {
			return retainedRowsPage{}, fmt.Errorf("parse retained-row cursor request log id: %w", err)
		}
		orderAtArg := len(queryArgs) + 1
		requestLogIDArg := len(queryArgs) + 2
		query += fmt.Sprintf(" AND (created_at, id) > ($%d, $%d)", orderAtArg, requestLogIDArg)
		queryArgs = append(queryArgs, orderAt.UTC(), requestLogID)
	}
	queryArgs = append(queryArgs, rowLimit+1)
	query += fmt.Sprintf(" ORDER BY created_at ASC, id ASC LIMIT $%d", len(queryArgs))
	rows, err := exec.Query(ctx, query, queryArgs...)
	if err != nil {
		return retainedRowsPage{}, fmt.Errorf("query retained rows for ingress %s: %w", ingressRequestID, err)
	}
	defer rows.Close()
	items := make([]ChainRowItem, 0, rowLimit+1)
	for rows.Next() {
		var item ChainRowItem
		dest, raw := chainRowScanDest(&item)
		if err := rows.Scan(dest...); err != nil {
			return retainedRowsPage{}, fmt.Errorf("scan retained row: %w", err)
		}
		applyChainRowProjection(&item, raw, connectionCatalog)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return retainedRowsPage{}, fmt.Errorf("iterate retained rows: %w", err)
	}
	hasMore := len(items) > rowLimit
	if hasMore {
		items = items[:rowLimit]
	}
	return retainedRowsPage{Rows: items, HasMore: hasMore}, nil
}

func truncateCodePoints(value string, limit int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]), true
}

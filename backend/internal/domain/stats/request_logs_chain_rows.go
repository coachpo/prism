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

func loadRetainedRowCounts(ctx context.Context, exec queryExecutor, params ChainQueryParams, ingressRequestID string) (retainedRowCounts, error) {
	var counts retainedRowCounts
	queryArgs := []any{params.ProfileID, ingressRequestID}
	matchPredicate := buildChainRowMatchPredicate(&queryArgs, params, "request_logs")
	err := exec.QueryRow(ctx, `SELECT
			COUNT(*) FILTER (WHERE row_kind = 'upstream'),
			COUNT(*),
			COUNT(*) FILTER (WHERE row_kind = 'legacy_unknown'),
			COUNT(*) FILTER (WHERE `+matchPredicate+`)
		FROM request_logs
		WHERE profile_id = $1 AND ingress_request_id = $2`, queryArgs...).Scan(
		&counts.Upstream,
		&counts.Total,
		&counts.Legacy,
		&counts.Matched,
	)
	if err != nil {
		return retainedRowCounts{}, fmt.Errorf("query retained row counts for ingress %s: %w", ingressRequestID, err)
	}
	return counts, nil
}

// loadRetainedRows loads one bounded retained-row page for one ingress. The
// limit+1 sentinel determines page completeness without deriving full-chain
// counts from the bounded page.
func loadRetainedRows(ctx context.Context, exec queryExecutor, params ChainQueryParams, ingressRequestID string, rowLimit int, cursor *rowCursorPayload, connectionCatalog map[int]connectionRecord) (retainedRowsPage, error) {
	profileID := params.ProfileID
	queryArgs := []any{profileID, ingressRequestID}
	matchPredicate := buildChainRowMatchPredicate(&queryArgs, params, "request_logs")
	query := `SELECT
			id, row_kind, ingress_request_id, attempt_number, attempt_trigger, attempt_result, is_winner,
			attempt_duration_ms, legacy_duration_ms, upstream_status_code, gateway_status_code, legacy_status_code,
			error_source, error_code, failure_stage, error_detail, error_detail_redacted, error_detail_truncated,
			stream_error_detail, stream_error_detail_redacted, stream_error_detail_truncated,
			stream_outcome, stream_error_kind, model_id, resolved_target_model_id, endpoint_id, connection_id,
			total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, pricing_evidence_trust, created_at,
			endpoint_base_url, endpoint_description,
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
		var requestLogID int64
		var errorDetail, streamErrorDetail *string
		var errorDetailRedacted, errorDetailTruncated, streamErrorDetailRedacted, streamErrorDetailTruncated bool
		var endpointBaseURL, endpointDescription *string
		if err := rows.Scan(
			&requestLogID, &item.RowKind, &item.IngressRequestID, &item.AttemptNumber, &item.AttemptTrigger, &item.AttemptResult, &item.IsWinner,
			&item.AttemptDurationMS, &item.LegacyDurationMS, &item.UpstreamStatusCode, &item.GatewayStatusCode, &item.LegacyStatusCode,
			&item.ErrorSource, &item.ErrorCode, &item.FailureStage, &errorDetail, &errorDetailRedacted, &errorDetailTruncated,
			&streamErrorDetail, &streamErrorDetailRedacted, &streamErrorDetailTruncated,
			&item.StreamOutcome, &item.StreamErrorKind, &item.ModelID, &item.ResolvedTargetModelID, &item.EndpointID, &item.TerminalTargetID,
			&item.TotalTokens, &item.TotalCostUserCurrencyMicros, &item.PricingStatus, &item.UnpricedReason, &item.PricingEvidenceTrust, &item.CreatedAt,
			&endpointBaseURL, &endpointDescription, &item.MatchedByFilter,
		); err != nil {
			return retainedRowsPage{}, fmt.Errorf("scan retained row: %w", err)
		}
		item.RequestLogID = strconv.FormatInt(requestLogID, 10)
		item.CreatedAt = item.CreatedAt.UTC()
		// Unified failure projection: error_detail wins; stream detail is
		// used for 2xx abnormal streams and client disconnects.
		detail := errorDetail
		source := "error_detail"
		redacted := errorDetailRedacted
		truncated := errorDetailTruncated
		if detail == nil && streamErrorDetail != nil {
			detail = streamErrorDetail
			source = "stream_error_detail"
			redacted = streamErrorDetailRedacted
			truncated = streamErrorDetailTruncated
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

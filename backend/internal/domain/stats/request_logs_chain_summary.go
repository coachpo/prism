package stats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// finalizedSummaryJoinSQL is the shared finalized-summary join shape: the
// authoritative final request-log row, the Terminal Target connection, and
// the Terminal Target's owner model. Aliases stay fixed so the select list
// below remains the single projection contract.
const finalizedSummaryJoinSQL = `
		FROM usage_request_events ue
		LEFT JOIN LATERAL (
			SELECT request_logs.id
			FROM request_logs
			WHERE request_logs.profile_id = ue.profile_id
			  AND request_logs.ingress_request_id = ue.ingress_request_id
			ORDER BY
				(ue.final_attempt_number IS NOT NULL
					AND request_logs.attempt_number = ue.final_attempt_number) DESC,
				(request_logs.is_winner IS TRUE) DESC,
				request_logs.created_at DESC,
				request_logs.id DESC
			LIMIT 1
		) AS final_request_log ON TRUE
		LEFT JOIN connections ON connections.id = ue.connection_id
		LEFT JOIN LATERAL (
			SELECT model_configs.model_id
			FROM model_access_targets
			JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id
			WHERE model_access_targets.profile_id = ue.profile_id
			  AND model_access_targets.target_connection_id = ue.connection_id
			ORDER BY model_access_targets.position ASC, model_access_targets.id ASC
			LIMIT 1
		) AS owner_model_configs ON TRUE`

// finalizedSummarySelectList is the finalized-ingress projection consumed by
// finalizedSummaryScan. One list, two entry points (single chain and page
// batch), so the public summary fields cannot drift between paths.
const finalizedSummarySelectList = `final_request_log.id,
		ue.model_id,
		ue.resolved_target_model_id,
		ue.status_code,
		ue.success_flag,
		ue.stream_outcome,
		ue.endpoint_id,
		ue.endpoint_label_snapshot,
		ue.connection_id,
		connections.name,
		connections.is_active,
		owner_model_configs.model_id,
		ue.ttft_ms,
		ue.output_tokens,
		ue.completion_duration_ms,
		ue.output_rate_state,
		ue.output_rate_reason,
		ue.output_delivery_span_ms,
		ue.total_tokens,
		ue.total_cost_user_currency_micros,
		ue.report_currency_code,
		ue.report_currency_symbol,
		ue.reporting_currency_epoch,
		ue.currency_attribution,
		ue.pricing_status,
		ue.unpriced_reason,
		ue.pricing_resolution_kind,
		ue.missing_price_components,
		ue.pricing_evidence_trust,
		ue.final_error_code,
		ue.final_attempt_number,
		ue.final_attempt_trigger,
		ue.final_target_entry_trigger,
		ue.same_target_retry_occurred,
		ue.hedge_occurred,
		ue.failover_occurred,
		ue.routing_evidence_complete,
		ue.attempt_count,
		ue.expected_request_log_row_count,
		ue.ingress_started_at,
		ue.ingress_completed_at,
		ue.pricing_template_id_used,
		ue.pricing_template_name_snapshot,
		ue.pricing_template_revision_id_used,
		ue.pricing_config_version_used,
		ue.pricing_version_effective_at,
		ue.pricing_snapshot_unit,
		ue.pricing_snapshot_input,
		ue.pricing_snapshot_output,
		ue.pricing_snapshot_cache_read_input,
		ue.pricing_snapshot_cache_creation_input,
		ue.pricing_snapshot_reasoning`

type rowScanner interface {
	Scan(dest ...any) error
}

// finalizedSummaryScan holds the raw scan targets of one finalized-summary
// row and assembles them into the public FinalizedSummary projection.
type finalizedSummaryScan struct {
	summary FinalizedSummary
	raw     struct {
		requestLogID         sql.NullInt64
		resolvedModelID      *string
		endpointID           *int
		endpointLabel        *string
		connectionID         *int
		terminalTargetName   *string
		configured           sql.NullBool
		ownerModelID         *string
		ingressStartedAt     *time.Time
		ingressCompletedAt   *time.Time
		effectiveAt          *time.Time
		outputRateState      sql.NullString
		outputRateReason     *string
		outputDeliverySpanMS *int
		pricingStatus        string
		unpricedReason       *string
		resolutionKind       *string
		missingComponents    []string
		trust                string
	}
}

func newFinalizedSummaryScan() *finalizedSummaryScan {
	return &finalizedSummaryScan{}
}

func (scan *finalizedSummaryScan) dest() []any {
	s := scan
	return []any{
		&s.raw.requestLogID,
		&s.summary.RequestedModelID,
		&s.raw.resolvedModelID,
		&s.summary.FinalStatusCode,
		&s.summary.SuccessFlag,
		&s.summary.StreamOutcome,
		&s.raw.endpointID,
		&s.raw.endpointLabel,
		&s.raw.connectionID,
		&s.raw.terminalTargetName,
		&s.raw.configured,
		&s.raw.ownerModelID,
		&s.summary.TTFTMS,
		&s.summary.OutputTokens,
		&s.summary.CompletionDurationMS,
		&s.raw.outputRateState,
		&s.raw.outputRateReason,
		&s.raw.outputDeliverySpanMS,
		&s.summary.TotalTokens,
		&s.summary.TotalCostUserCurrencyMicros,
		&s.summary.ReportCurrencyCode,
		&s.summary.ReportCurrencySymbol,
		&s.summary.ReportingCurrencyEpoch,
		&s.summary.CurrencyAttribution,
		&s.raw.pricingStatus,
		&s.raw.unpricedReason,
		&s.raw.resolutionKind,
		&s.raw.missingComponents,
		&s.raw.trust,
		&s.summary.FinalErrorCode,
		&s.summary.FinalAttemptNumber,
		&s.summary.FinalAttemptTrigger,
		&s.summary.FinalTargetEntryTrigger,
		&s.summary.SameTargetRetryOccurred,
		&s.summary.HedgeOccurred,
		&s.summary.FailoverOccurred,
		&s.summary.RoutingEvidenceComplete,
		&s.summary.AttemptCount,
		&s.summary.ExpectedRequestLogRowCount,
		&s.raw.ingressStartedAt,
		&s.raw.ingressCompletedAt,
		&s.summary.PricingTemplateIDUsed,
		&s.summary.PricingTemplateNameSnapshot,
		&s.summary.PricingTemplateRevisionIDUsed,
		&s.summary.PricingConfigVersionUsed,
		&s.raw.effectiveAt,
		&s.summary.PricingSnapshotUnit,
		&s.summary.PricingSnapshotInput,
		&s.summary.PricingSnapshotOutput,
		&s.summary.PricingSnapshotCacheReadInput,
		&s.summary.PricingSnapshotCacheCreationInput,
		&s.summary.PricingSnapshotReasoning,
	}
}

func (scan *finalizedSummaryScan) assemble() *FinalizedSummary {
	summary := &scan.summary
	raw := &scan.raw
	raw.endpointID = normalizePositiveID(raw.endpointID)
	raw.connectionID = normalizePositiveID(raw.connectionID)
	if raw.requestLogID.Valid {
		summary.RequestLogID = stringPointer(strconv.FormatInt(raw.requestLogID.Int64, 10))
	}
	summary.RequestedModel = &ModelRef{ID: summary.RequestedModelID, Label: summary.RequestedModelID}
	if raw.resolvedModelID != nil {
		summary.ResolvedModel = &ModelRef{ID: *raw.resolvedModelID, Label: *raw.resolvedModelID}
	}
	if raw.connectionID != nil {
		label := fmt.Sprintf("Terminal Target #%d", *raw.connectionID)
		if raw.terminalTargetName != nil && strings.TrimSpace(*raw.terminalTargetName) != "" {
			label = strings.TrimSpace(*raw.terminalTargetName)
		}
		summary.TerminalTarget = &TargetRef{
			ID:           *raw.connectionID,
			Label:        label,
			Configured:   raw.configured.Valid && raw.configured.Bool,
			OwnerModelID: raw.ownerModelID,
		}
	}
	if raw.endpointID != nil {
		label := fmt.Sprintf("Endpoint %d", *raw.endpointID)
		if raw.endpointLabel != nil && strings.TrimSpace(*raw.endpointLabel) != "" {
			label = strings.TrimSpace(*raw.endpointLabel)
		}
		summary.Endpoint = &EndpointRef{ID: *raw.endpointID, Label: label}
	}
	summary.FinalResult = deriveFinalResult(summary.FinalStatusCode, summary.SuccessFlag, summary.StreamOutcome)
	// The output rate is backend-authoritative measured evidence only:
	// historical rows (state NULL) project as unknown and never derive a
	// rate from TTFT/completion timings.
	summary.OutputRateState = NormalizeOutputRateState(stringValue(nullableString(raw.outputRateState)))
	summary.OutputRateReason = raw.outputRateReason
	summary.OutputRateTPS = OutputRateTPSFromEvidence(intValue(summary.OutputTokens), summary.OutputTokens != nil, summary.OutputRateState, raw.outputDeliverySpanMS)
	summary.FinalPricingStatus = raw.pricingStatus
	summary.FinalUnpricedReason = raw.unpricedReason
	summary.FinalPricingResolutionKind = raw.resolutionKind
	summary.MissingPriceComponents = raw.missingComponents
	summary.FinalPricingEvidenceTrust = raw.trust
	summary.IngressStartedAt = utcTimePointer(raw.ingressStartedAt)
	summary.IngressCompletedAt = utcTimePointer(raw.ingressCompletedAt)
	summary.PricingVersionEffectiveAt = utcTimePointer(raw.effectiveAt)
	legacyCode := ""
	legacyCodeValid := summary.ReportCurrencyCode != nil
	if legacyCodeValid {
		legacyCode = *summary.ReportCurrencyCode
	}
	key := CostSegmentKeyFor(summary.ReportingCurrencyEpoch, legacyCode, legacyCodeValid)
	summary.CostSegmentKey = &key
	return summary
}

// loadFinalizedSummaries loads the finalized usage projection for each listed
// ingress in one statement: the newest usage event per ingress wins
// (DISTINCT ON ingress ORDER BY id DESC), matching the historical per-ingress
// `ORDER BY id DESC LIMIT 1` lookup exactly. Ingresses without finalized
// evidence are absent from the returned map.
func loadFinalizedSummaries(ctx context.Context, exec queryExecutor, profileID int, ingressIDs []string) (map[string]*FinalizedSummary, error) {
	summaries := make(map[string]*FinalizedSummary, len(ingressIDs))
	if len(ingressIDs) == 0 {
		return summaries, nil
	}
	query := `SELECT DISTINCT ON (ue.ingress_request_id) ue.ingress_request_id,
			` + finalizedSummarySelectList + finalizedSummaryJoinSQL + `
		WHERE ue.profile_id = $1 AND ue.ingress_request_id = ANY($2)
		ORDER BY ue.ingress_request_id ASC, ue.id DESC`
	rows, err := exec.Query(ctx, query, profileID, ingressIDs)
	if err != nil {
		return nil, fmt.Errorf("load finalized summaries for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var ingressID string
		scan := newFinalizedSummaryScan()
		dest := append([]any{&ingressID}, scan.dest()...)
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan finalized summary: %w", err)
		}
		summaries[ingressID] = scan.assemble()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finalized summaries: %w", err)
	}
	return summaries, nil
}

// loadFinalizedSummary loads the finalized usage projection for one ingress.
// It is the single-chain form of loadFinalizedSummaries.
func loadFinalizedSummary(ctx context.Context, exec queryExecutor, profileID int, ingressRequestID string) (*FinalizedSummary, bool, error) {
	scan := newFinalizedSummaryScan()
	err := exec.QueryRow(ctx, `SELECT `+finalizedSummarySelectList+finalizedSummaryJoinSQL+`
		WHERE ue.profile_id = $1 AND ue.ingress_request_id = $2
		ORDER BY ue.id DESC LIMIT 1`,
		profileID, ingressRequestID).Scan(scan.dest()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load finalized summary for ingress %s: %w", ingressRequestID, err)
	}
	return scan.assemble(), true, nil
}

func deriveFinalResult(statusCode int, successFlag bool, streamOutcome string) string {
	if statusCode < 200 || statusCode > 299 {
		return "failed"
	}
	switch streamOutcome {
	case "client_disconnected":
		return "client_disconnected"
	case "provider_incomplete", "upstream_read_error", "gateway_timeout", "upstream_ended_without_terminal", "unknown":
		return "failed"
	default:
		return "completed"
	}
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

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

// loadFinalizedSummary loads the finalized usage projection for an ingress.
func loadFinalizedSummary(ctx context.Context, exec queryExecutor, profileID int, ingressRequestID string) (*FinalizedSummary, bool, error) {
	var summary FinalizedSummary
	var ingressStartedAt, ingressCompletedAt, effectiveAt *time.Time
	var resolvedModelID, ownerModelID *string
	var terminalTargetID *int
	var terminalTargetName *string
	var configured sql.NullBool
	var endpointID *int
	var endpointName *string
	var pricingStatus string
	var trust string
	var unpricedReason, resolutionKind *string
	var missingComponents []string
	var requestLogID sql.NullInt64
	err := exec.QueryRow(ctx, `SELECT
			final_request_log.id,
			usage_request_events.model_id,
			usage_request_events.resolved_target_model_id,
			usage_request_events.status_code,
			usage_request_events.success_flag,
			usage_request_events.stream_outcome,
			usage_request_events.endpoint_id,
			usage_request_events.endpoint_label_snapshot,
			usage_request_events.connection_id,
			connections.name,
			connections.is_active,
			owner_model_configs.model_id,
			usage_request_events.ttft_ms,
			usage_request_events.output_tokens,
			usage_request_events.completion_duration_ms,
			usage_request_events.total_tokens,
			usage_request_events.total_cost_user_currency_micros,
			usage_request_events.report_currency_code,
			usage_request_events.report_currency_symbol,
			usage_request_events.reporting_currency_epoch,
			usage_request_events.currency_attribution,
			usage_request_events.pricing_status,
			usage_request_events.unpriced_reason,
			usage_request_events.pricing_resolution_kind,
			usage_request_events.missing_price_components,
			usage_request_events.pricing_evidence_trust,
			usage_request_events.final_error_code,
			usage_request_events.final_attempt_number,
			usage_request_events.final_attempt_trigger,
			usage_request_events.final_target_entry_trigger,
			usage_request_events.same_target_retry_occurred,
			usage_request_events.hedge_occurred,
			usage_request_events.failover_occurred,
			usage_request_events.routing_evidence_complete,
			usage_request_events.attempt_count,
			usage_request_events.expected_request_log_row_count,
			usage_request_events.ingress_started_at,
			usage_request_events.ingress_completed_at,
			usage_request_events.pricing_template_id_used,
			usage_request_events.pricing_template_name_snapshot,
			usage_request_events.pricing_template_revision_id_used,
			usage_request_events.pricing_config_version_used,
			usage_request_events.pricing_version_effective_at,
			usage_request_events.pricing_snapshot_unit,
			usage_request_events.pricing_snapshot_input,
			usage_request_events.pricing_snapshot_output,
			usage_request_events.pricing_snapshot_cache_read_input,
			usage_request_events.pricing_snapshot_cache_creation_input,
			usage_request_events.pricing_snapshot_reasoning
		FROM usage_request_events
		LEFT JOIN LATERAL (
			SELECT request_logs.id
			FROM request_logs
			WHERE request_logs.profile_id = usage_request_events.profile_id
			  AND request_logs.ingress_request_id = usage_request_events.ingress_request_id
			ORDER BY
				(usage_request_events.final_attempt_number IS NOT NULL
					AND request_logs.attempt_number = usage_request_events.final_attempt_number) DESC,
				(request_logs.is_winner IS TRUE) DESC,
				request_logs.created_at DESC,
				request_logs.id DESC
			LIMIT 1
		) AS final_request_log ON TRUE
		LEFT JOIN connections ON connections.id = usage_request_events.connection_id
		LEFT JOIN LATERAL (
			SELECT model_configs.model_id
			FROM model_access_targets
			JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id
			WHERE model_access_targets.profile_id = usage_request_events.profile_id
			  AND model_access_targets.target_connection_id = usage_request_events.connection_id
			ORDER BY model_access_targets.position ASC, model_access_targets.id ASC
			LIMIT 1
		) AS owner_model_configs ON TRUE
		WHERE usage_request_events.profile_id = $1 AND usage_request_events.ingress_request_id = $2
		ORDER BY usage_request_events.id DESC LIMIT 1`,
		profileID, ingressRequestID).Scan(
		&requestLogID,
		&summary.RequestedModelID,
		&resolvedModelID,
		&summary.FinalStatusCode,
		&summary.SuccessFlag,
		&summary.StreamOutcome,
		&endpointID,
		&endpointName,
		&terminalTargetID,
		&terminalTargetName,
		&configured,
		&ownerModelID,
		&summary.TTFTMS,
		&summary.OutputTokens,
		&summary.CompletionDurationMS,
		&summary.TotalTokens,
		&summary.TotalCostUserCurrencyMicros,
		&summary.ReportCurrencyCode,
		&summary.ReportCurrencySymbol,
		&summary.ReportingCurrencyEpoch,
		&summary.CurrencyAttribution,
		&pricingStatus,
		&unpricedReason,
		&resolutionKind,
		&missingComponents,
		&trust,
		&summary.FinalErrorCode,
		&summary.FinalAttemptNumber,
		&summary.FinalAttemptTrigger,
		&summary.FinalTargetEntryTrigger,
		&summary.SameTargetRetryOccurred,
		&summary.HedgeOccurred,
		&summary.FailoverOccurred,
		&summary.RoutingEvidenceComplete,
		&summary.AttemptCount,
		&summary.ExpectedRequestLogRowCount,
		&ingressStartedAt,
		&ingressCompletedAt,
		&summary.PricingTemplateIDUsed,
		&summary.PricingTemplateNameSnapshot,
		&summary.PricingTemplateRevisionIDUsed,
		&summary.PricingConfigVersionUsed,
		&effectiveAt,
		&summary.PricingSnapshotUnit,
		&summary.PricingSnapshotInput,
		&summary.PricingSnapshotOutput,
		&summary.PricingSnapshotCacheReadInput,
		&summary.PricingSnapshotCacheCreationInput,
		&summary.PricingSnapshotReasoning,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load finalized summary for ingress %s: %w", ingressRequestID, err)
	}
	if requestLogID.Valid {
		summary.RequestLogID = stringPointer(strconv.FormatInt(requestLogID.Int64, 10))
	}
	summary.RequestedModel = &ModelRef{ID: summary.RequestedModelID, Label: summary.RequestedModelID}
	if resolvedModelID != nil {
		summary.ResolvedModel = &ModelRef{ID: *resolvedModelID, Label: *resolvedModelID}
	}
	if terminalTargetID != nil {
		label := fmt.Sprintf("Terminal Target #%d", *terminalTargetID)
		if terminalTargetName != nil && strings.TrimSpace(*terminalTargetName) != "" {
			label = strings.TrimSpace(*terminalTargetName)
		}
		summary.TerminalTarget = &TargetRef{ID: *terminalTargetID, Label: label, Configured: configured.Valid && configured.Bool, OwnerModelID: ownerModelID}
	}
	if endpointID != nil {
		label := fmt.Sprintf("Endpoint %d", *endpointID)
		if endpointName != nil && strings.TrimSpace(*endpointName) != "" {
			label = strings.TrimSpace(*endpointName)
		}
		summary.Endpoint = &EndpointRef{ID: *endpointID, Label: label}
	}
	summary.FinalResult = deriveFinalResult(summary.FinalStatusCode, summary.SuccessFlag, summary.StreamOutcome)
	summary.FinalPricingStatus = pricingStatus
	summary.FinalUnpricedReason = unpricedReason
	summary.FinalPricingResolutionKind = resolutionKind
	summary.MissingPriceComponents = missingComponents
	summary.FinalPricingEvidenceTrust = trust
	summary.IngressStartedAt = utcTimePointer(ingressStartedAt)
	summary.IngressCompletedAt = utcTimePointer(ingressCompletedAt)
	summary.PricingVersionEffectiveAt = utcTimePointer(effectiveAt)
	if summary.OutputTokens != nil && summary.TTFTMS != nil && summary.CompletionDurationMS != nil && *summary.CompletionDurationMS-*summary.TTFTMS > 0 {
		rate := float64(*summary.OutputTokens) * 1000 / float64(*summary.CompletionDurationMS-*summary.TTFTMS)
		summary.OutputRateTPS = &rate
	}
	legacyCode := ""
	legacyCodeValid := summary.ReportCurrencyCode != nil
	if legacyCodeValid {
		legacyCode = *summary.ReportCurrencyCode
	}
	key := CostSegmentKeyFor(summary.ReportingCurrencyEpoch, legacyCode, legacyCodeValid)
	summary.CostSegmentKey = &key
	return &summary, true, nil
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

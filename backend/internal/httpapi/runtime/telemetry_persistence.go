package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/jackc/pgx/v5"
)

func materializeRuntimeTelemetryEnvelopeTx(ctx context.Context, tx pgx.Tx, logPartitions *runtimeLogPartitionCache, envelope runtimeTelemetryEnvelope) (int, error) {
	envelope = normalizeRuntimeTelemetryEnvelopeTimestamps(envelope)
	for index := range envelope.RequestLogs {
		envelope.RequestLogs[index].ProxyAPIKeyAuthEnforcedAtRequest = envelope.ProxyKeyAuthEnforced
	}
	envelope.UsageEvent.ProxyAPIKeyAuthEnforcedAtRequest = envelope.ProxyKeyAuthEnforced
	if err := ensureRuntimeTelemetryPartitions(ctx, logPartitions, envelope); err != nil {
		return 0, err
	}
	requestLogID, err := insertRequestLogsAndUsageEventTx(ctx, tx, envelope.RequestLogs, envelope.AuditLogs, envelope.UsageEvent)
	if err != nil {
		return 0, err
	}
	if err := recordRuntimeProxyKeyUsageTx(ctx, tx, envelope.ProxyKeyUsage); err != nil {
		return 0, err
	}
	return requestLogID, nil
}

func normalizeRuntimeTelemetryEnvelopeTimestamps(envelope runtimeTelemetryEnvelope) runtimeTelemetryEnvelope {
	requestCreatedAtByAttempt := make(map[int]time.Time, len(envelope.RequestLogs))
	for index := range envelope.RequestLogs {
		envelope.RequestLogs[index].CreatedAt = envelope.RequestLogs[index].CreatedAt.UTC()
		if envelope.RequestLogs[index].PricingVersionEffectiveAt != nil {
			effectiveAt := envelope.RequestLogs[index].PricingVersionEffectiveAt.UTC()
			envelope.RequestLogs[index].PricingVersionEffectiveAt = &effectiveAt
		}
		requestCreatedAtByAttempt[envelope.RequestLogs[index].AttemptNumber] = envelope.RequestLogs[index].CreatedAt
	}
	for index := range envelope.AuditLogs {
		if createdAt, ok := requestCreatedAtByAttempt[envelope.AuditLogs[index].RequestLogAttemptNumber]; ok {
			envelope.AuditLogs[index].CreatedAt = createdAt
		} else {
			envelope.AuditLogs[index].CreatedAt = envelope.AuditLogs[index].CreatedAt.UTC()
		}
	}
	if len(envelope.RequestLogs) > 0 {
		envelope.UsageEvent.CreatedAt = envelope.RequestLogs[len(envelope.RequestLogs)-1].CreatedAt
	} else {
		envelope.UsageEvent.CreatedAt = envelope.UsageEvent.CreatedAt.UTC()
	}
	if envelope.UsageEvent.PricingVersionEffectiveAt != nil {
		effectiveAt := envelope.UsageEvent.PricingVersionEffectiveAt.UTC()
		envelope.UsageEvent.PricingVersionEffectiveAt = &effectiveAt
	}
	// Envelopes accepted before currency attribution became explicit have no
	// attribution field in their serialized payload. They remain conservative
	// historical evidence instead of being inferred from epoch/code at drain
	// time. New producers always set identified before enqueue.
	if strings.TrimSpace(envelope.UsageEvent.CurrencyAttribution) == "" {
		envelope.UsageEvent.CurrencyAttribution = runtimeUsageCurrencyAttributionLegacyUnknown
	}
	normalizeRuntimeOutputRateEvidence(&envelope)
	if len(envelope.AccountingAttempts) > 0 {
		for index := range envelope.AccountingAttempts {
			if createdAt, ok := requestCreatedAtByAttempt[envelope.AccountingAttempts[index].AttemptNumber]; ok {
				envelope.AccountingAttempts[index].ObservedAt = createdAt
			} else {
				envelope.AccountingAttempts[index].ObservedAt = envelope.AccountingAttempts[index].ObservedAt.UTC()
			}
		}
	}
	if !envelope.AccountingEvent.ObservedAt.IsZero() {
		envelope.AccountingEvent.ObservedAt = envelope.UsageEvent.CreatedAt.UTC()
	}
	return envelope
}

func ensureRuntimeTelemetryPartitions(ctx context.Context, logPartitions *runtimeLogPartitionCache, envelope runtimeTelemetryEnvelope) error {
	if logPartitions == nil {
		return fmt.Errorf("runtime log partition ensurer unavailable")
	}
	for _, requestLog := range envelope.RequestLogs {
		if err := logPartitions.EnsurePartitionForTime(ctx, "request_logs", requestLog.CreatedAt); err != nil {
			return err
		}
	}
	for _, auditLog := range envelope.AuditLogs {
		if err := logPartitions.EnsurePartitionForTime(ctx, "audit_logs", auditLog.CreatedAt); err != nil {
			return err
		}
	}
	if err := logPartitions.EnsurePartitionForTime(ctx, "usage_request_events", envelope.UsageEvent.CreatedAt); err != nil {
		return err
	}
	return nil
}

func insertRequestLogsAndUsageEventTx(ctx context.Context, tx pgx.Tx, requestLogs []requestLogInsert, auditLogs []auditLogInsert, usageEvent usageEventInsert) (int, error) {
	auditByAttempt := make(map[int]auditLogInsert, len(auditLogs))
	auditTimes := make([]time.Time, 0, len(auditLogs))
	for _, auditLog := range auditLogs {
		auditByAttempt[auditLog.RequestLogAttemptNumber] = auditLog
	}
	var requestLogID int
	for _, requestLog := range requestLogs {
		err := tx.QueryRow(
			ctx,
			`INSERT INTO request_logs (
				profile_id, model_id, resolved_target_model_id, upstream_model_id, api_family, operation_name,
				row_kind, caller_request_id, url_scrub_provenance, metadata_redacted_fields, metadata_truncated_fields,
				endpoint_id, connection_id, selected_terminal_target_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state,
				ingress_request_id, attempt_number, attempt_trigger, attempt_result, is_winner, attempt_duration_ms, legacy_duration_ms,
				provider_correlation_id, endpoint_base_url, endpoint_description,
				upstream_status_code, gateway_status_code, legacy_status_code,
				error_source, error_code, failure_stage, error_detail, error_detail_redacted, error_detail_truncated,
				stream_error_detail, stream_error_detail_redacted, stream_error_detail_truncated,
				upstream_request_started, response_headers_received, first_body_or_stream_event_seen,
				is_stream, input_tokens, output_tokens, total_tokens, success_flag,
				cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
				input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros,
				total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol,
				fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
				pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used,
				pricing_status, unpriced_reason, pricing_resolution_kind, missing_price_components, pricing_evidence_trust,
				pricing_template_id_used, pricing_template_name_snapshot, pricing_template_revision_id_used, reporting_currency_epoch,
				request_path, created_at, caller_user_agent, upstream_user_agent,
				completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind,
				output_rate_state, output_rate_reason, output_delivery_event_count, output_delivery_span_ms,
				audit_enabled_at_request, audit_capture_bodies_at_request,
				request_generation_params, request_generation_params_status, upstream_operation_name, operation_translation_mode, upstream_request_path,
				proxy_api_key_auth_enforced_at_request, pricing_version_effective_at,
				pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_selector_threshold_tokens, pricing_selector_basis_tokens,
				pricing_schedule_decided_at, pricing_schedule_timezone, pricing_schedule_local_weekday, pricing_schedule_local_minute, pricing_schedule_digest
			) VALUES (
				$1, $2, $3, $4, $5, $6,
				$7, $8, $9, $10, $11,
				$12, $13, $14, $15, $16,
				CASE
					WHEN $15::bigint IS NOT NULL AND $16::varchar IS NOT NULL THEN 'identified'
					WHEN $15::bigint IS NULL AND $16::varchar IS NULL THEN 'none'
					ELSE 'unknown'
				END,
				$17, $18, $19, $20, $21, $22, $23,
				$24, $25, $26,
				$27, $28, $29,
				$30, $31, $32, $33, $34, $35,
				$36, $37, $38,
				$39, $40, $41,
				$42, $43, $44, $45, $46,
				$47, $48, $49,
				$50, $51, $52, $53, $54,
				$55, $56, $57, $58, $59,
				$60, $61, $62, $63, $64,
				$65, $66, $67, $68,
				$69, $70, $71, $72, $73,
				$74, $75, $76, $77,
				$78, $79, $80, $81,
				$82, $83, $84, $85,
				$86, $87, $88, $89,
				$90, $91,
				$92, $93, $94, $95, $96,
				$97, $98,
				$99, $100, $101, $102, $103,
				$104, $105, $106, $107, $108
			) RETURNING id`,
			requestLog.ProfileID,
			requestLog.ModelID,
			nullableStringArg(requestLog.ResolvedTargetModelID),
			nullableStringArg(requestLog.UpstreamModelID),
			requestLog.APIFamily,
			requestLog.OperationName,
			requestLog.RowKind,
			nullableStringArg(requestLog.CallerRequestID),
			requestLog.URLScrubProvenance,
			notNullStringArrayArg(requestLog.MetadataRedactedFields),
			notNullStringArrayArg(requestLog.MetadataTruncatedFields),
			nullableIntArg(requestLog.EndpointID),
			nullableIntArg(requestLog.ConnectionID),
			nullableIntArg(requestLog.SelectedTerminalTargetID),
			nullableIntArg(requestLog.ProxyAPIKeyID),
			nullableStringArg(requestLog.ProxyAPIKeyNameSnapshot),
			requestLog.IngressRequestID,
			nullableAttemptNumberArg(requestLog.RowKind, requestLog.AttemptNumber),
			nullableStringArg(requestLog.AttemptTrigger),
			nullableStringArg(requestLog.AttemptResult),
			nullableBoolArg(requestLog.IsWinner),
			nullableIntArg(requestLog.AttemptDurationMS),
			nullableIntArg(requestLog.LegacyDurationMS),
			nullableStringArg(requestLog.ProviderCorrelationID),
			nullableStringArg(requestLog.EndpointBaseURL),
			nullableStringArg(requestLog.EndpointDescription),
			nullableIntArg(requestLog.UpstreamStatusCode),
			nullableIntArg(requestLog.GatewayStatusCode),
			nullableIntArg(requestLog.LegacyStatusCode),
			nullableStringArg(requestLog.ErrorSource),
			nullableStringArg(requestLog.ErrorCode),
			nullableStringArg(requestLog.FailureStage),
			nullableStringArg(requestLog.ErrorDetail),
			requestLog.ErrorDetailRedacted,
			requestLog.ErrorDetailTruncated,
			nullableStringArg(requestLog.StreamErrorDetail),
			requestLog.StreamErrorDetailRedacted,
			requestLog.StreamErrorDetailTruncated,
			nullableBoolArg(requestLog.UpstreamRequestStarted),
			nullableBoolArg(requestLog.ResponseHeadersReceived),
			nullableBoolArg(requestLog.FirstBodyOrStreamEventSeen),
			requestLog.IsStream,
			nullableIntArg(requestLog.InputTokens),
			nullableIntArg(requestLog.OutputTokens),
			nullableIntArg(requestLog.TotalTokens),
			requestLog.SuccessFlag,
			nullableIntArg(requestLog.CacheReadInputTokens),
			nullableIntArg(requestLog.CacheCreationInputTokens),
			nullableIntArg(requestLog.ReasoningTokens),
			nullableInt64Arg(requestLog.InputCostMicros),
			nullableInt64Arg(requestLog.OutputCostMicros),
			nullableInt64Arg(requestLog.CacheReadInputCostMicros),
			nullableInt64Arg(requestLog.CacheCreationInputCostMicros),
			nullableInt64Arg(requestLog.ReasoningCostMicros),
			nullableInt64Arg(requestLog.TotalCostOriginalMicros),
			nullableInt64Arg(requestLog.TotalCostUserCurrencyMicros),
			nullableStringArg(requestLog.CurrencyCodeOriginal),
			nullableStringArg(requestLog.ReportCurrencyCode),
			nullableStringArg(requestLog.ReportCurrencySymbol),
			nullableStringArg(requestLog.FXRateUsed),
			nullableStringArg(requestLog.FXRateSource),
			nullableStringArg(requestLog.PricingSnapshotUnit),
			nullableStringArg(requestLog.PricingSnapshotInput),
			nullableStringArg(requestLog.PricingSnapshotOutput),
			nullableStringArg(requestLog.PricingSnapshotCacheReadInput),
			nullableStringArg(requestLog.PricingSnapshotCacheCreationInput),
			nullableStringArg(requestLog.PricingSnapshotReasoning),
			nullableIntArg(requestLog.PricingConfigVersionUsed),
			requestLog.PricingStatus,
			nullableStringArg(requestLog.UnpricedReason),
			nullableStringArg(requestLog.PricingResolutionKind),
			nullableStringSliceArg(requestLog.MissingPriceComponents),
			requestLog.PricingEvidenceTrust,
			nullableIntArg(requestLog.PricingTemplateIDUsed),
			nullableStringArg(requestLog.PricingTemplateNameSnapshot),
			nullableInt64Arg(requestLog.PricingTemplateRevisionIDUsed),
			nullableIntArg(requestLog.ReportingCurrencyEpoch),
			requestLog.RequestPath,
			requestLog.CreatedAt.UTC(),
			nullableStringArg(requestLog.CallerUserAgent),
			nullableStringArg(requestLog.UpstreamUserAgent),
			nullableIntArg(requestLog.CompletionDurationMS),
			nullableIntArg(requestLog.TTFTMS),
			requestLog.StreamOutcome,
			nullableStringArg(requestLog.StreamErrorKind),
			nullableStringArg(emptyStringToNil(requestLog.OutputRateState)),
			nullableStringArg(requestLog.OutputRateReason),
			nullableIntArg(requestLog.OutputDeliveryEventCount),
			nullableIntArg(requestLog.OutputDeliverySpanMS),
			requestLog.AuditEnabledAtRequest,
			requestLog.AuditCaptureBodiesAtRequest,
			nullableJSONArg(requestLog.RequestGenerationParams),
			nullableStringArg(requestLog.RequestGenerationParamsStatus),
			nullableStringArg(requestLog.UpstreamOperationName),
			nullableStringArg(requestLog.OperationTranslationMode),
			nullableStringArg(requestLog.UpstreamRequestPath),
			nullableBoolArg(requestLog.ProxyAPIKeyAuthEnforcedAtRequest),
			nullableTimeArg(requestLog.PricingVersionEffectiveAt),
			nullableStringArg(requestLog.PricingTemplateKind),
			nullableStringArg(requestLog.PricingSelectionState),
			nullableStringArg(requestLog.PricingCardRole),
			nullableIntArg(requestLog.PricingSelectorThresholdTokens),
			nullableInt64Arg(requestLog.PricingSelectorBasisTokens),
			nullableTimeArg(requestLog.PricingScheduleDecidedAt),
			nullableStringArg(requestLog.PricingScheduleTimezone),
			nullableIntArg(requestLog.PricingScheduleLocalWeekday),
			nullableIntArg(requestLog.PricingScheduleLocalMinute),
			nullableStringArg(requestLog.PricingScheduleDigest),
		).Scan(&requestLogID)
		if err != nil {
			return 0, fmt.Errorf("insert request log: %w (row_kind=%s pricing_status=%s reason=%v resolution=%v components=%v trust=%s)", err, requestLog.RowKind, requestLog.PricingStatus, dereferenceString(requestLog.UnpricedReason), dereferenceString(requestLog.PricingResolutionKind), requestLog.MissingPriceComponents, requestLog.PricingEvidenceTrust)
		}
		if auditLog, ok := auditByAttempt[requestLog.AttemptNumber]; ok {
			auditLog.CreatedAt = requestLog.CreatedAt
			inserted, err := insertRuntimeAuditLogTx(ctx, tx, requestLogID, requestLog.CreatedAt, requestLog.IngressRequestID, auditLog)
			if err != nil {
				return 0, err
			}
			auditTimes = appendAuditTimeIfInserted(auditTimes, auditLog.CreatedAt, inserted)
		}
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO usage_request_events (
			profile_id, ingress_request_id, model_id, resolved_target_model_id, upstream_model_id, api_family, operation_name,
			endpoint_id, connection_id, selected_terminal_target_id, proxy_api_key_name_snapshot,
			status_code, success_flag, input_tokens, output_tokens, total_tokens,
			cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
			input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol,
			fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
			pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used,
			attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind,
			output_rate_state, output_rate_reason, output_delivery_event_count, output_delivery_span_ms,
			pricing_status, unpriced_reason, pricing_resolution_kind, missing_price_components, pricing_evidence_trust,
			pricing_template_id_used, pricing_template_name_snapshot, pricing_template_revision_id_used, reporting_currency_epoch,
			expected_request_log_row_count, final_attempt_number, final_attempt_trigger, final_target_entry_trigger,
			same_target_retry_occurred, hedge_occurred, failover_occurred, routing_evidence_complete, final_error_code,
			ingress_started_at, ingress_completed_at, proxy_api_key_id_snapshot, proxy_api_key_attribution_state,
			upstream_operation_name, operation_translation_mode, upstream_request_path, endpoint_label_snapshot,
			proxy_api_key_auth_enforced_at_request, currency_attribution, pricing_version_effective_at,
			pricing_template_kind, pricing_selection_state, pricing_card_role, pricing_selector_threshold_tokens, pricing_selector_basis_tokens,
			pricing_schedule_decided_at, pricing_schedule_timezone, pricing_schedule_local_weekday, pricing_schedule_local_minute, pricing_schedule_digest
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, $19,
			$20, $21, $22, $23, $24,
			$25, $26, $27, $28, $29,
			$30, $31, $32, $33, $34,
			$35, $36, $37, $38,
			$39, $40, $41, $42, $43, $44, $45, $46,
			$47, $48, $49, $50,
			$51, $52, $53, $54, $55,
			$56, $57, $58, $59,
			$60, $61, $62, $63,
			$64, $65, $66, $67, $68,
			$69, $70, $71,
			CASE
				WHEN $71::bigint IS NOT NULL AND $11::varchar IS NOT NULL THEN 'identified'
				WHEN $71::bigint IS NULL AND $11::varchar IS NULL THEN 'none'
				ELSE 'unknown'
			END,
			$72, $73, $74, $75,
			$76, $77, $78,
			$79, $80, $81, $82, $83,
			$84, $85, $86, $87, $88
		)`,

		usageEvent.ProfileID,
		usageEvent.IngressRequestID,
		usageEvent.ModelID,
		nullableStringArg(usageEvent.ResolvedTargetModelID),
		nullableStringArg(usageEvent.UpstreamModelID),
		usageEvent.APIFamily,
		usageEvent.OperationName,
		nullableIntArg(usageEvent.EndpointID),
		nullableIntArg(usageEvent.ConnectionID),
		nullableIntArg(usageEvent.SelectedTerminalTargetID),
		nullableStringArg(usageEvent.ProxyAPIKeyNameSnapshot),
		usageEvent.StatusCode,
		usageEvent.SuccessFlag,
		nullableIntArg(usageEvent.InputTokens),
		nullableIntArg(usageEvent.OutputTokens),
		nullableIntArg(usageEvent.TotalTokens),
		nullableIntArg(usageEvent.CacheReadInputTokens),
		nullableIntArg(usageEvent.CacheCreationInputTokens),
		nullableIntArg(usageEvent.ReasoningTokens),
		nullableInt64Arg(usageEvent.InputCostMicros),
		nullableInt64Arg(usageEvent.OutputCostMicros),
		nullableInt64Arg(usageEvent.CacheReadInputCostMicros),
		nullableInt64Arg(usageEvent.CacheCreationInputCostMicros),
		nullableInt64Arg(usageEvent.ReasoningCostMicros),
		nullableInt64Arg(usageEvent.TotalCostOriginalMicros),
		nullableInt64Arg(usageEvent.TotalCostUserCurrencyMicros),
		nullableStringArg(usageEvent.CurrencyCodeOriginal),
		nullableStringArg(usageEvent.ReportCurrencyCode),
		nullableStringArg(usageEvent.ReportCurrencySymbol),
		nullableStringArg(usageEvent.FXRateUsed),
		nullableStringArg(usageEvent.FXRateSource),
		nullableStringArg(usageEvent.PricingSnapshotUnit),
		nullableStringArg(usageEvent.PricingSnapshotInput),
		nullableStringArg(usageEvent.PricingSnapshotOutput),
		nullableStringArg(usageEvent.PricingSnapshotCacheReadInput),
		nullableStringArg(usageEvent.PricingSnapshotCacheCreationInput),
		nullableStringArg(usageEvent.PricingSnapshotReasoning),
		nullableIntArg(usageEvent.PricingConfigVersionUsed),
		usageEvent.AttemptCount,
		usageEvent.RequestPath,
		usageEvent.CreatedAt.UTC(),
		nullableIntArg(usageEvent.ResponseTimeMS),
		nullableIntArg(usageEvent.CompletionDurationMS),
		nullableIntArg(usageEvent.TTFTMS),
		usageEvent.StreamOutcome,
		nullableStringArg(usageEvent.StreamErrorKind),
		nullableStringArg(emptyStringToNil(usageEvent.OutputRateState)),
		nullableStringArg(usageEvent.OutputRateReason),
		nullableIntArg(usageEvent.OutputDeliveryEventCount),
		nullableIntArg(usageEvent.OutputDeliverySpanMS),
		usageEvent.PricingStatus,
		nullableStringArg(usageEvent.UnpricedReason),
		nullableStringArg(usageEvent.PricingResolutionKind),
		nullableStringSliceArg(usageEvent.MissingPriceComponents),
		usageEvent.PricingEvidenceTrust,
		nullableIntArg(usageEvent.PricingTemplateIDUsed),
		nullableStringArg(usageEvent.PricingTemplateNameSnapshot),
		nullableInt64Arg(usageEvent.PricingTemplateRevisionIDUsed),
		nullableIntArg(usageEvent.ReportingCurrencyEpoch),
		nullableIntArg(usageEvent.ExpectedRequestLogRowCount),
		nullableIntArg(usageEvent.FinalAttemptNumber),
		nullableStringArg(usageEvent.FinalAttemptTrigger),
		nullableStringArg(usageEvent.FinalTargetEntryTrigger),
		usageEvent.SameTargetRetryOccurred,
		usageEvent.HedgeOccurred,
		usageEvent.FailoverOccurred,
		nullableBoolArg(usageEvent.RoutingEvidenceComplete),
		nullableStringArg(usageEvent.FinalErrorCode),
		nullableTimeArg(usageEvent.IngressStartedAt),
		nullableTimeArg(usageEvent.IngressCompletedAt),
		// The attribution state is derived in SQL from the two identity values
		// actually written, exactly as request_logs does it, so no builder can
		// author a triple the check constraint rejects.
		nullableIntArg(usageEvent.ProxyAPIKeyIDSnapshot),
		nullableStringArg(usageEvent.UpstreamOperationName),
		nullableStringArg(usageEvent.OperationTranslationMode),
		nullableStringArg(usageEvent.UpstreamRequestPath),
		usageEventEndpointLabelSnapshotForInsert(usageEvent),
		nullableBoolArg(usageEvent.ProxyAPIKeyAuthEnforcedAtRequest),
		usageEvent.CurrencyAttribution,
		nullableTimeArg(usageEvent.PricingVersionEffectiveAt),
		nullableStringArg(usageEvent.PricingTemplateKind),
		nullableStringArg(usageEvent.PricingSelectionState),
		nullableStringArg(usageEvent.PricingCardRole),
		nullableIntArg(usageEvent.PricingSelectorThresholdTokens),
		nullableInt64Arg(usageEvent.PricingSelectorBasisTokens),
		nullableTimeArg(usageEvent.PricingScheduleDecidedAt),
		nullableStringArg(usageEvent.PricingScheduleTimezone),
		nullableIntArg(usageEvent.PricingScheduleLocalWeekday),
		nullableIntArg(usageEvent.PricingScheduleLocalMinute),
		nullableStringArg(usageEvent.PricingScheduleDigest),
	); err != nil {
		return 0, fmt.Errorf("insert usage event: %w (ingress=%s status=%d pricing_status=%s trust=%s created=%s)", err, usageEvent.IngressRequestID, usageEvent.StatusCode, usageEvent.PricingStatus, usageEvent.PricingEvidenceTrust, usageEvent.CreatedAt.UTC().Format(time.RFC3339))
	}
	requestTimes := make([]time.Time, 0, len(requestLogs))
	for _, requestLog := range requestLogs {
		requestTimes = append(requestTimes, requestLog.CreatedAt)
	}
	if err := statsdomain.RecordActualCoverageAppend(ctx, tx, "request_logs", requestTimes, usageEvent.CreatedAt); err != nil {
		return 0, fmt.Errorf("advance request-log coverage owner: %w", err)
	}
	if err := statsdomain.RecordActualCoverageAppend(ctx, tx, "audit_logs", auditTimes, usageEvent.CreatedAt); err != nil {
		return 0, fmt.Errorf("advance audit coverage owner: %w", err)
	}
	if err := statsdomain.RecordActualCoverageAppend(ctx, tx, "usage_request_events", []time.Time{usageEvent.CreatedAt}, usageEvent.CreatedAt); err != nil {
		return 0, fmt.Errorf("advance usage coverage owner: %w", err)
	}
	return requestLogID, nil
}

func insertRuntimeAuditLogTx(ctx context.Context, tx pgx.Tx, requestLogID int, requestLogCreatedAt time.Time, ingressRequestID string, auditLog auditLogInsert) (bool, error) {
	tag, err := tx.Exec(
		ctx,
		`INSERT INTO audit_logs (
			request_log_id, request_log_created_at, ingress_request_id, profile_id, model_id,
			endpoint_id, connection_id, endpoint_base_url, endpoint_description,
			request_method, request_url, request_url_truncated, endpoint_base_url_truncated,
			request_headers, request_headers_scrub_provenance, request_headers_capture_status, request_headers_capture_limit_reason,
			response_headers, response_headers_scrub_provenance, response_headers_capture_status, response_headers_capture_limit_reason,
			request_body, request_body_encoding, request_body_capture_provenance, request_body_capture_end_state,
			request_body_capture_status, request_body_capture_limit_reason, request_body_truncated,
			request_body_bytes_observed, request_body_bytes_stored,
			response_body, response_body_encoding, response_body_capture_provenance, response_body_capture_end_state,
			response_body_capture_status, response_body_capture_limit_reason, response_body_truncated,
			response_body_bytes_observed, response_body_bytes_stored,
			row_kind, attempt_number, attempt_duration_ms, upstream_status_code,
			url_scrub_provenance, is_stream, created_at,
			audit_enabled_at_request, audit_capture_bodies_at_request
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48)`,
		requestLogID,
		requestLogCreatedAt.UTC(),
		ingressRequestID,
		auditLog.ProfileID,
		auditLog.ModelID,
		auditLog.EndpointID,
		auditLog.ConnectionID,
		auditLog.EndpointBaseURL,
		nullableStringArg(auditLog.EndpointDescription),
		auditLog.RequestMethod,
		auditLog.RequestURL,
		auditLog.RequestURLTruncated,
		auditLog.EndpointBaseURLTruncated,
		nullableStringArg(stringPtr(auditLog.RequestHeaders)),
		auditLog.RequestHeadersScrubProvenance,
		runtimeAuditHeadersCaptureStatus(auditLog.RequestHeaders),
		"none",
		nullableStringArg(auditLog.ResponseHeaders),
		auditLog.ResponseHeadersScrubProvenance,
		runtimeAuditHeadersCaptureStatusOptional(auditLog.ResponseHeaders),
		"none",
		nullableBytesArg(auditLog.RequestBody),
		nullableStringArg(auditLog.RequestBodyEncoding),
		auditLog.RequestBodyCaptureProvenance,
		nullableStringArg(auditLog.RequestBodyCaptureEndState),
		auditLog.RequestBodyCaptureStatus,
		auditLog.RequestBodyCaptureLimitReason,
		auditLog.RequestBodyTruncated,
		nullableInt64Arg(auditLog.RequestBodyBytesObserved),
		nullableInt64Arg(auditLog.RequestBodyBytesStored),
		nullableBytesArg(auditLog.ResponseBody),
		nullableStringArg(auditLog.ResponseBodyEncoding),
		auditLog.ResponseBodyCaptureProvenance,
		nullableStringArg(auditLog.ResponseBodyCaptureEndState),
		auditLog.ResponseBodyCaptureStatus,
		auditLog.ResponseBodyCaptureLimitReason,
		auditLog.ResponseBodyTruncated,
		nullableInt64Arg(auditLog.ResponseBodyBytesObserved),
		nullableInt64Arg(auditLog.ResponseBodyBytesStored),
		auditLog.RowKind,
		nullableIntArg(auditLog.AttemptNumber),
		nullableIntArg(auditLog.AttemptDurationMS),
		nullableIntArg(auditLog.UpstreamStatusCode),
		auditLog.URLScrubProvenance,
		auditLog.IsStream,
		auditLog.CreatedAt.UTC(),
		auditLog.AuditEnabledAtRequest,
		auditLog.AuditCaptureBodiesAtRequest,
	)
	if err != nil {
		return false, fmt.Errorf("insert audit log for request log %d: %w (row_kind=%s status=%s bytes_observed=%d bytes_stored=%d truncated=%v body_nil=%v)", requestLogID, err, auditLog.RowKind, auditLog.ResponseBodyCaptureStatus, derefInt64(auditLog.ResponseBodyBytesObserved), derefInt64(auditLog.ResponseBodyBytesStored), auditLog.ResponseBodyTruncated, auditLog.ResponseBody == nil)
	}
	return tag.RowsAffected() == 1, nil
}

func appendAuditTimeIfInserted(times []time.Time, at time.Time, inserted bool) []time.Time {
	if !inserted {
		return times
	}
	return append(times, at)
}

func runtimeAuditHeadersCaptureStatus(serialized string) string {
	if strings.TrimSpace(serialized) == "" || serialized == "{}" {
		return "not_requested"
	}
	return "captured"
}

func runtimeAuditHeadersCaptureStatusOptional(serialized *string) string {
	if serialized == nil {
		return "not_requested"
	}
	return runtimeAuditHeadersCaptureStatus(*serialized)
}

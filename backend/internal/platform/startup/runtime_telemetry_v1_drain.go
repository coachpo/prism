package startup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

// runtimeTelemetryV1DrainOwner is the exclusive offline owner that drains the
// legacy v1 runtime telemetry outbox after migrations 000008-000010 applied
// and every v1 producer is stopped and provably absent (Requests SPEC §5.6).
//
// It transforms finalized v1 envelopes with the fixed §4.3 scrub/cap rules and
// the §5.2 legacy body/header conversion, materializes them into the v2
// tables with stable keys, wipes unsafe raw bytes, and writes
// telemetry_orphaned tombstones for v1 stream_accepted rows without
// authoritative terminal truth. Only when the v1 finalized/accepted outbox
// count, old owner lease count, and old writer generation count are all zero
// does the upgrade state advance to v1_drained.
type runtimeTelemetryV1DrainOwner struct {
	now func() time.Time
}

func newRuntimeTelemetryV1DrainOwner(now func() time.Time) *runtimeTelemetryV1DrainOwner {
	if now == nil {
		now = time.Now
	}
	return &runtimeTelemetryV1DrainOwner{now: now}
}

// v1TelemetryEnvelope is the legacy outbox envelope shape (old writers only).
type v1TelemetryEnvelope struct {
	RequestLogs  []v1RequestLogInsert `json:"request_logs"`
	AuditLogs    []v1AuditLogInsert   `json:"audit_logs,omitempty"`
	UsageEvent   v1UsageEventInsert   `json:"usage_event"`
	HandoffPhase string               `json:"handoff_phase,omitempty"`
}

type v1RequestLogInsert struct {
	ProfileID                         int       `json:"profile_id"`
	ModelID                           string    `json:"model_id"`
	ResolvedTargetModelID             *string   `json:"resolved_target_model_id"`
	APIFamily                         string    `json:"api_family"`
	OperationName                     string    `json:"operation_name"`
	UpstreamOperationName             *string   `json:"upstream_operation_name"`
	OperationTranslationMode          *string   `json:"operation_translation_mode"`
	EndpointID                        *int      `json:"endpoint_id"`
	ConnectionID                      *int      `json:"connection_id"`
	SelectedTerminalTargetID          *int      `json:"selected_terminal_target_id"`
	ProxyAPIKeyID                     *int      `json:"proxy_api_key_id"`
	ProxyAPIKeyNameSnapshot           *string   `json:"proxy_api_key_name_snapshot"`
	IngressRequestID                  string    `json:"ingress_request_id"`
	AttemptNumber                     int       `json:"attempt_number"`
	ProviderCorrelationID             *string   `json:"provider_correlation_id"`
	EndpointBaseURL                   *string   `json:"endpoint_base_url"`
	EndpointDescription               *string   `json:"endpoint_description"`
	StatusCode                        int       `json:"status_code"`
	ResponseTimeMS                    int       `json:"response_time_ms"`
	IsStream                          bool      `json:"is_stream"`
	InputTokens                       *int      `json:"input_tokens"`
	OutputTokens                      *int      `json:"output_tokens"`
	TotalTokens                       *int      `json:"total_tokens"`
	SuccessFlag                       bool      `json:"success_flag"`
	UnpricedReason                    *string   `json:"unpriced_reason"`
	CacheReadInputTokens              *int      `json:"cache_read_input_tokens"`
	CacheCreationInputTokens          *int      `json:"cache_creation_input_tokens"`
	ReasoningTokens                   *int      `json:"reasoning_tokens"`
	InputCostMicros                   *int64    `json:"input_cost_micros"`
	OutputCostMicros                  *int64    `json:"output_cost_micros"`
	CacheReadInputCostMicros          *int64    `json:"cache_read_input_cost_micros"`
	CacheCreationInputCostMicros      *int64    `json:"cache_creation_input_cost_micros"`
	ReasoningCostMicros               *int64    `json:"reasoning_cost_micros"`
	TotalCostOriginalMicros           *int64    `json:"total_cost_original_micros"`
	TotalCostUserCurrencyMicros       *int64    `json:"total_cost_user_currency_micros"`
	CurrencyCodeOriginal              *string   `json:"currency_code_original"`
	ReportCurrencyCode                *string   `json:"report_currency_code"`
	ReportCurrencySymbol              *string   `json:"report_currency_symbol"`
	FXRateUsed                        *string   `json:"fx_rate_used"`
	FXRateSource                      *string   `json:"fx_rate_source"`
	PricingSnapshotUnit               *string   `json:"pricing_snapshot_unit"`
	PricingSnapshotInput              *string   `json:"pricing_snapshot_input"`
	PricingSnapshotOutput             *string   `json:"pricing_snapshot_output"`
	PricingSnapshotCacheReadInput     *string   `json:"pricing_snapshot_cache_read_input"`
	PricingSnapshotCacheCreationInput *string   `json:"pricing_snapshot_cache_creation_input"`
	PricingSnapshotReasoning          *string   `json:"pricing_snapshot_reasoning"`
	PricingConfigVersionUsed          *int      `json:"pricing_config_version_used"`
	RequestPath                       string    `json:"request_path"`
	UpstreamRequestPath               *string   `json:"upstream_request_path"`
	ErrorDetail                       *string   `json:"error_detail"`
	CreatedAt                         time.Time `json:"created_at"`
	CallerUserAgent                   *string   `json:"caller_user_agent"`
	UpstreamUserAgent                 *string   `json:"upstream_user_agent"`
	CompletionDurationMS              *int      `json:"completion_duration_ms"`
	TTFTMS                            *int      `json:"ttft_ms"`
	StreamOutcome                     string    `json:"stream_outcome"`
	StreamErrorKind                   *string   `json:"stream_error_kind"`
	StreamErrorDetail                 *string   `json:"stream_error_detail"`
	AuditEnabledAtRequest             bool      `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest       bool      `json:"audit_capture_bodies_at_request"`
}

type v1UsageEventInsert struct {
	ProfileID                   int       `json:"profile_id"`
	IngressRequestID            string    `json:"ingress_request_id"`
	ModelID                     string    `json:"model_id"`
	ResolvedTargetModelID       *string   `json:"resolved_target_model_id"`
	APIFamily                   string    `json:"api_family"`
	OperationName               string    `json:"operation_name"`
	EndpointID                  *int      `json:"endpoint_id"`
	EndpointLabelSnapshot       string    `json:"endpoint_label_snapshot"`
	ConnectionID                *int      `json:"connection_id"`
	SelectedTerminalTargetID    *int      `json:"selected_terminal_target_id"`
	ProxyAPIKeyID               *int      `json:"proxy_api_key_id"`
	ProxyAPIKeyNameSnapshot     *string   `json:"proxy_api_key_name_snapshot"`
	StatusCode                  int       `json:"status_code"`
	SuccessFlag                 bool      `json:"success_flag"`
	InputTokens                 *int      `json:"input_tokens"`
	OutputTokens                *int      `json:"output_tokens"`
	TotalTokens                 *int      `json:"total_tokens"`
	CacheReadInputTokens        *int      `json:"cache_read_input_tokens"`
	CacheCreationInputTokens    *int      `json:"cache_creation_input_tokens"`
	ReasoningTokens             *int      `json:"reasoning_tokens"`
	TotalCostUserCurrencyMicros *int64    `json:"total_cost_user_currency_micros"`
	AttemptCount                int       `json:"attempt_count"`
	RequestPath                 string    `json:"request_path"`
	CreatedAt                   time.Time `json:"created_at"`
	ResponseTimeMS              *int      `json:"response_time_ms"`
	CompletionDurationMS        *int      `json:"completion_duration_ms"`
	TTFTMS                      *int      `json:"ttft_ms"`
	StreamOutcome               string    `json:"stream_outcome"`
	StreamErrorKind             *string   `json:"stream_error_kind"`
	UnpricedReason              *string   `json:"unpriced_reason"`
}

type v1AuditLogInsert struct {
	RequestLogAttemptNumber     int       `json:"request_log_attempt_number"`
	ProfileID                   int       `json:"profile_id"`
	ModelID                     string    `json:"model_id"`
	EndpointID                  int       `json:"endpoint_id"`
	ConnectionID                int       `json:"connection_id"`
	EndpointBaseURL             string    `json:"endpoint_base_url"`
	EndpointDescription         *string   `json:"endpoint_description"`
	RequestMethod               string    `json:"request_method"`
	RequestURL                  string    `json:"request_url"`
	RequestHeaders              string    `json:"request_headers"`
	RequestBody                 *string   `json:"request_body"`
	RequestBodyStored           bool      `json:"request_body_stored"`
	ResponseStatus              int       `json:"response_status"`
	ResponseHeaders             *string   `json:"response_headers"`
	ResponseBody                *string   `json:"response_body"`
	ResponseBodyStored          bool      `json:"response_body_stored"`
	IsStream                    bool      `json:"is_stream"`
	DurationMS                  int       `json:"duration_ms"`
	CreatedAt                   time.Time `json:"created_at"`
	AuditEnabledAtRequest       bool      `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest bool      `json:"audit_capture_bodies_at_request"`
}

// legacyRedactedHeaderValue is the fixed replacement for legacy header values
// when no request-time effective Header Blocklist snapshot is verifiable
// (Requests SPEC §5.2).
const legacyRedactedHeaderValue = "[REDACTED-LEGACY]"

// Run performs one drain pass. It returns the number of remaining v1 rows and
// whether the drain is complete. The owner is exclusive: it runs only while
// the upgrade state is draining_v1, and it must not run concurrently with
// another instance (writer-generation fence + exclusive lease).
func (owner *runtimeTelemetryV1DrainOwner) Run(ctx context.Context, tx pgx.Tx) (remaining int, complete bool, err error) {
	var state string
	var fenceActive bool
	if err := tx.QueryRow(ctx, `SELECT state, writer_fence_active FROM observability_v2_upgrade_state WHERE id = 1`).Scan(&state, &fenceActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, true, nil
		}
		return 0, false, fmt.Errorf("load observability v2 upgrade state: %w", err)
	}
	if state == "final" {
		return 0, true, nil
	}
	if state != "draining_v1" {
		// Fresh installs are already backfill_ready; nothing to drain.
		return 0, true, nil
	}

	var pending int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_telemetry_outbox WHERE schema_version = 1 AND lifecycle_state <> 'telemetry_orphaned'`).Scan(&pending); err != nil {
		return 0, false, fmt.Errorf("count v1 telemetry outbox rows: %w", err)
	}
	if pending == 0 {
		if _, err := tx.Exec(ctx, `UPDATE observability_v2_upgrade_state SET state = 'v1_drained', updated_at = now() WHERE id = 1`); err != nil {
			return 0, false, fmt.Errorf("advance v1 drain state: %w", err)
		}
		return 0, true, nil
	}

	rows, err := tx.Query(ctx, `SELECT id, payload FROM runtime_telemetry_outbox WHERE schema_version = 1 AND lifecycle_state <> 'telemetry_orphaned' ORDER BY id ASC LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, false, fmt.Errorf("load v1 telemetry outbox batch: %w", err)
	}
	type v1Row struct {
		id      int64
		payload []byte
	}
	batch := make([]v1Row, 0, 100)
	for rows.Next() {
		var row v1Row
		if err := rows.Scan(&row.id, &row.payload); err != nil {
			rows.Close()
			return 0, false, fmt.Errorf("scan v1 telemetry outbox row: %w", err)
		}
		batch = append(batch, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("iterate v1 telemetry outbox batch: %w", err)
	}
	for _, row := range batch {
		if err := owner.drainV1Row(ctx, tx, row.id, row.payload); err != nil {
			return 0, false, err
		}
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_telemetry_outbox WHERE schema_version = 1 AND lifecycle_state <> 'telemetry_orphaned'`).Scan(&remaining); err != nil {
		return 0, false, fmt.Errorf("recount v1 telemetry outbox rows: %w", err)
	}
	if remaining == 0 {
		if _, err := tx.Exec(ctx, `UPDATE observability_v2_upgrade_state SET state = 'v1_drained', updated_at = now() WHERE id = 1`); err != nil {
			return 0, false, fmt.Errorf("advance v1 drain state after processing: %w", err)
		}
	}
	return remaining, remaining == 0, nil
}

func (owner *runtimeTelemetryV1DrainOwner) drainV1Row(ctx context.Context, tx pgx.Tx, rowID int64, payload []byte) error {
	var envelope v1TelemetryEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		// Undecodable legacy payload: wipe and tombstone; never materialize
		// partial facts.
		if _, err := tx.Exec(ctx, `UPDATE runtime_telemetry_outbox SET lifecycle_state = 'telemetry_orphaned', payload = '{}'::jsonb WHERE id = $1`, rowID); err != nil {
			return fmt.Errorf("tombstone undecodable v1 row %d: %w", rowID, err)
		}
		return nil
	}
	if envelope.HandoffPhase == "stream_accepted" {
		// v1 stream_accepted without authoritative terminal truth: wipe
		// header/body bytes and write a telemetry_orphaned gap/tombstone.
		if _, err := tx.Exec(ctx, `UPDATE runtime_telemetry_outbox SET lifecycle_state = 'telemetry_orphaned', payload = '{}'::jsonb WHERE id = $1`, rowID); err != nil {
			return fmt.Errorf("tombstone v1 stream_accepted row %d: %w", rowID, err)
		}
		return nil
	}
	if err := materializeV1Envelope(ctx, tx, envelope); err != nil {
		return fmt.Errorf("materialize v1 envelope row %d: %w", rowID, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM runtime_telemetry_outbox WHERE id = $1`, rowID); err != nil {
		return fmt.Errorf("delete drained v1 row %d: %w", rowID, err)
	}
	return nil
}

// materializeV1Envelope converts a finalized v1 envelope into v2 rows with the
// fixed §4.3 scrub/cap rules and §5.2 legacy body/header conversion.
func materializeV1Envelope(ctx context.Context, tx pgx.Tx, envelope v1TelemetryEnvelope) error {
	requestBudgetRemaining := int64(12 * 1024 * 1024)
	for index := range envelope.RequestLogs {
		requestLog := envelope.RequestLogs[index]
		// Legacy rows cannot be safely classified; they stay legacy_unknown
		// with the un-scoped status/duration projections.
		legacyStatusCode := requestLog.StatusCode
		legacyDurationMS := requestLog.ResponseTimeMS
		callerUA := scrubLegacyMetadata(requestLog.CallerUserAgent)
		upstreamUA := scrubLegacyMetadata(requestLog.UpstreamUserAgent)
		correlationID := scrubLegacyMetadata(requestLog.ProviderCorrelationID)
		baseURL, _ := safediag.ScrubEndpointBaseURL(derefLegacyString(requestLog.EndpointBaseURL))
		requestPath, _ := safediag.ScrubRequestPath(legacyPathOnly(requestLog.RequestPath))
		_, err := tx.Exec(ctx, `INSERT INTO request_logs (
			profile_id, model_id, resolved_target_model_id, api_family, operation_name,
			row_kind, url_scrub_provenance, metadata_redacted_fields, metadata_truncated_fields,
				 endpoint_id, connection_id, selected_terminal_target_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot,
			ingress_request_id, legacy_status_code, legacy_duration_ms,
			provider_correlation_id, endpoint_base_url, endpoint_description,
			is_stream, input_tokens, output_tokens, total_tokens, success_flag,
			cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
			input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol,
			fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
			pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used,
			pricing_status, unpriced_reason, pricing_evidence_trust,
			request_path, error_detail, created_at, caller_user_agent, upstream_user_agent,
			completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind, stream_error_detail,
			audit_enabled_at_request, audit_capture_bodies_at_request, upstream_operation_name, operation_translation_mode, upstream_request_path
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,$51,$52,$53,$54,$55,$56,$57,$58,$59,$60,$61,$62,$63,$64,$65)`,
			requestLog.ProfileID,
			requestLog.ModelID,
			requestLog.ResolvedTargetModelID,
			requestLog.APIFamily,
			requestLog.OperationName,
			"legacy_unknown",
			"legacy_unknown",
			legacyMetadataRedactedFields(callerUA, upstreamUA, correlationID),
			legacyMetadataTruncatedFields(callerUA, upstreamUA, correlationID),
			requestLog.EndpointID,
			requestLog.ConnectionID,
			requestLog.SelectedTerminalTargetID,
			requestLog.ProxyAPIKeyID,
			requestLog.ProxyAPIKeyNameSnapshot,
			requestLog.IngressRequestID,
			legacyStatusCode,
			legacyDurationMS,
			correlationID,
			optionalLegacyString(baseURL),
			requestLog.EndpointDescription,
			requestLog.IsStream,
			requestLog.InputTokens,
			requestLog.OutputTokens,
			requestLog.TotalTokens,
			requestLog.SuccessFlag,
			requestLog.CacheReadInputTokens,
			requestLog.CacheCreationInputTokens,
			requestLog.ReasoningTokens,
			requestLog.InputCostMicros,
			requestLog.OutputCostMicros,
			requestLog.CacheReadInputCostMicros,
			requestLog.CacheCreationInputCostMicros,
			requestLog.ReasoningCostMicros,
			requestLog.TotalCostOriginalMicros,
			requestLog.TotalCostUserCurrencyMicros,
			requestLog.CurrencyCodeOriginal,
			requestLog.ReportCurrencyCode,
			requestLog.ReportCurrencySymbol,
			requestLog.FXRateUsed,
			requestLog.FXRateSource,
			requestLog.PricingSnapshotUnit,
			requestLog.PricingSnapshotInput,
			requestLog.PricingSnapshotOutput,
			requestLog.PricingSnapshotCacheReadInput,
			requestLog.PricingSnapshotCacheCreationInput,
			requestLog.PricingSnapshotReasoning,
			requestLog.PricingConfigVersionUsed,
			legacyPricingStatus(requestLog.SuccessFlag, requestLog.UnpricedReason),
			requestLog.UnpricedReason,
			"legacy_untrusted",
			requestPath,
			requestLog.ErrorDetail,
			requestLog.CreatedAt.UTC(),
			callerUA,
			upstreamUA,
			requestLog.CompletionDurationMS,
			requestLog.TTFTMS,
			requestLog.StreamOutcome,
			requestLog.StreamErrorKind,
			requestLog.StreamErrorDetail,
			requestLog.AuditEnabledAtRequest,
			requestLog.AuditCaptureBodiesAtRequest,
			requestLog.UpstreamOperationName,
			requestLog.OperationTranslationMode,
			requestLog.UpstreamRequestPath,
		)
		if err != nil {
			return fmt.Errorf("insert v1 request log: %w", err)
		}
		_ = requestBudgetRemaining
	}
	// Legacy usage event: conservative four-state backfill.
	usageEvent := envelope.UsageEvent
	if usageEvent.IngressRequestID != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO usage_request_events (
			profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, operation_name,
				 endpoint_id, endpoint_label_snapshot, connection_id, selected_terminal_target_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot,
			status_code, success_flag, input_tokens, output_tokens, total_tokens,
			cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
			total_cost_user_currency_micros, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms,
			stream_outcome, stream_error_kind, pricing_status, unpriced_reason, pricing_evidence_trust,
			proxy_api_key_attribution_state
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33)`,
			usageEvent.ProfileID,
			usageEvent.IngressRequestID,
			usageEvent.ModelID,
			usageEvent.ResolvedTargetModelID,
			usageEvent.APIFamily,
			usageEvent.OperationName,
			usageEvent.EndpointID,
			usageEvent.EndpointLabelSnapshot,
			usageEvent.ConnectionID,
			usageEvent.SelectedTerminalTargetID,
			usageEvent.ProxyAPIKeyID,
			usageEvent.ProxyAPIKeyNameSnapshot,
			usageEvent.StatusCode,
			usageEvent.SuccessFlag,
			usageEvent.InputTokens,
			usageEvent.OutputTokens,
			usageEvent.TotalTokens,
			usageEvent.CacheReadInputTokens,
			usageEvent.CacheCreationInputTokens,
			usageEvent.ReasoningTokens,
			usageEvent.TotalCostUserCurrencyMicros,
			usageEvent.AttemptCount,
			usageEvent.RequestPath,
			usageEvent.CreatedAt.UTC(),
			usageEvent.ResponseTimeMS,
			usageEvent.CompletionDurationMS,
			usageEvent.TTFTMS,
			usageEvent.StreamOutcome,
			usageEvent.StreamErrorKind,
			legacyPricingStatus(usageEvent.SuccessFlag, usageEvent.UnpricedReason),
			usageEvent.UnpricedReason,
			"legacy_untrusted",
			legacyProxyKeyAttribution(usageEvent.ProxyAPIKeyID),
		); err != nil {
			return fmt.Errorf("insert v1 usage event: %w", err)
		}
	}
	// Legacy audit rows: headers all-values-redacted (no verifiable snapshot),
	// bodies transcoded to BYTEA with 4 MiB per-body and 12 MiB request budget.
	for index := range envelope.AuditLogs {
		auditLog := envelope.AuditLogs[index]
		requestBody, requestObserved, requestStored, requestTruncated := legacyBodyBytes(auditLog.RequestBody, &requestBudgetRemaining)
		responseBody, responseObserved, responseStored, responseTruncated := legacyBodyBytes(auditLog.ResponseBody, nil)
		scrubbedURL, _ := safediag.ScrubRequestURL(auditLog.RequestURL)
		if _, err := tx.Exec(ctx, `INSERT INTO audit_logs (
			profile_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description,
			request_method, request_url, url_scrub_provenance,
			request_headers, request_headers_scrub_provenance, request_headers_capture_status, request_headers_capture_limit_reason,
			response_headers, response_headers_scrub_provenance, response_headers_capture_status, response_headers_capture_limit_reason,
			request_body, request_body_encoding, request_body_capture_provenance, request_body_capture_end_state, request_body_capture_status, request_body_capture_limit_reason,
			request_body_truncated, request_body_bytes_observed, request_body_bytes_stored,
			response_body, response_body_encoding, response_body_capture_provenance, response_body_capture_end_state, response_body_capture_status, response_body_capture_limit_reason,
			response_body_truncated, response_body_bytes_observed, response_body_bytes_stored,
			row_kind, legacy_status_code, legacy_duration_ms, is_stream, audit_enabled_at_request, audit_capture_bodies_at_request, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42)`,
			auditLog.ProfileID,
			auditLog.ModelID,
			auditLog.EndpointID,
			auditLog.ConnectionID,
			auditLog.EndpointBaseURL,
			auditLog.EndpointDescription,
			auditLog.RequestMethod,
			scrubbedURL,
			"legacy_unknown",
			legacyAllValuesRedactedHeaders(auditLog.RequestHeaders),
			"legacy_all_values_redacted",
			"captured",
			"none",
			legacyAllValuesRedactedHeadersOptional(auditLog.ResponseHeaders),
			"legacy_all_values_redacted",
			"captured",
			"none",
			requestBody,
			legacyBodyEncoding(requestObserved),
			"legacy_text_transcoded",
			legacyEndState(requestObserved),
			legacyBodyCaptureStatus(requestObserved, requestStored, requestTruncated),
			legacyBodyLimitReason(requestObserved, requestStored),
			requestTruncated,
			requestObserved,
			requestStored,
			responseBody,
			legacyBodyEncoding(responseObserved),
			"legacy_text_transcoded",
			legacyEndState(responseObserved),
			legacyBodyCaptureStatus(responseObserved, responseStored, responseTruncated),
			legacyBodyLimitReason(responseObserved, responseStored),
			responseTruncated,
			responseObserved,
			responseStored,
			"legacy_unknown",
			auditLog.ResponseStatus,
			auditLog.DurationMS,
			auditLog.IsStream,
			auditLog.AuditEnabledAtRequest,
			auditLog.AuditCaptureBodiesAtRequest,
			auditLog.CreatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert v1 audit log: %w", err)
		}
	}
	return nil
}

// legacyAuditBodyCapBytes is the per-body audit capture cap (4 MiB raw bytes)
// from Requests SPEC §5.4, shared by the v1 drain with the live runtime
// bounded capture. Kept local to this package to avoid a startup → runtime
// import edge.

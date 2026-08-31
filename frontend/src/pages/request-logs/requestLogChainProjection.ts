import type { RequestLogListItem } from "@/lib/types";
import type {
  ChainIngressItem,
  ChainResponse,
  RequestLogChainRow,
} from "@/lib/types/request-logs";
import { getStaticMessages } from "@/i18n/staticMessages";

// Chain envelopes deliberately carry a smaller retained-row shape than the
// attempts table. This projection is the single boundary that fills the
// table's honest absent/default fields without moving query state into a view.
export function flattenChainItems(
  response: ChainResponse,
): RequestLogListItem[] {
  const rows: RequestLogListItem[] = [];
  const copy = getStaticMessages().requestLogs;
  for (const chain of response.items) {
    const finalized = chain.finalized_summary;
    for (const row of chain.retained_rows) {
      const isFinalizedRow =
        finalized?.request_log_id != null &&
        finalized.request_log_id === row.request_log_id;
      const hasFinalizedRowIdentity = finalized?.request_log_id != null;
      const withLabels = row as RequestLogChainRow & {
        model_label?: string;
        attempt_target_model_label?: string | null;
        api_family?: string;
        output_tokens?: number | null;
        ttft_ms?: number | null;
        completion_duration_ms?: number | null;
        reasoning_effort?: string | null;
        report_currency_symbol?: string | null;
      };
      rows.push({
        request_log_id: row.request_log_id,
        row_kind: row.row_kind,
        ingress_request_id: row.ingress_request_id,
        attempt_number: row.attempt_number,
        attempt_trigger: row.attempt_trigger,
        attempt_result: row.attempt_result,
        is_winner: row.is_winner,
        created_at: row.created_at,
        ingress_model_id: row.ingress_model_id,
        model_label: withLabels.model_label ?? row.ingress_model_id,
        attempt_target_model_id: row.attempt_target_model_id,
        attempt_target_model_label:
          withLabels.attempt_target_model_label ?? row.attempt_target_model_id,
        caller_client_display: null,
        upstream_client_display: null,
        user_agent_overridden: false,
        api_family:
          (withLabels.api_family as RequestLogListItem["api_family"]) ??
          "openai",
        endpoint_id: row.endpoint_id,
        endpoint_label:
          row.endpoint_label ??
          (row.endpoint_id === null
            ? copy.actualEndpointMissing
            : copy.endpointId(row.endpoint_id)),
        terminal_target_id: row.terminal_target_id,
        terminal_target_label: row.terminal_target_label,
        terminal_target_configured: row.terminal_target_configured,
        terminal_target_owner_model_id:
          row.terminal_target_owner_model_id ?? null,
        ttft_ms: withLabels.ttft_ms ?? null,
        completion_duration_ms: withLabels.completion_duration_ms ?? null,
        // The finalized summary owns the per-request rate. Project it only onto
        // its contributing final row; other known intermediate rows are
        // not-applicable rather than being mislabeled as historical unknown.
        output_rate_tps: isFinalizedRow ? finalized!.output_rate_tps : null,
        output_rate_state: isFinalizedRow
          ? finalized!.output_rate_state
          : hasFinalizedRowIdentity
            ? "not_applicable"
            : "unknown",
        output_rate_reason: isFinalizedRow
          ? finalized!.output_rate_reason
          : null,
        upstream_status_code: row.upstream_status_code,
        gateway_status_code: row.gateway_status_code,
        legacy_status_code: row.legacy_status_code,
        attempt_duration_ms: row.attempt_duration_ms,
        legacy_duration_ms: row.legacy_duration_ms,
        is_stream: row.stream_outcome !== "not_streaming",
        stream_outcome:
          row.stream_outcome as RequestLogListItem["stream_outcome"],
        stream_error_kind:
          row.stream_error_kind as RequestLogListItem["stream_error_kind"],
        error_source: row.error_source,
        error_code: row.error_code,
        failure_stage: row.failure_stage,
        failure_detail_preview: row.failure_detail_preview,
        failure_detail_source: row.failure_detail_source,
        failure_detail_preview_truncated: row.failure_detail_preview_truncated,
        failure_detail_redacted: row.failure_detail_redacted,
        reasoning_effort: withLabels.reasoning_effort ?? null,
        output_tokens: withLabels.output_tokens ?? null,
        total_tokens: row.total_tokens,
        total_cost_user_currency_micros: row.total_cost_user_currency_micros,
        pricing_status: row.pricing_status,
        pricing_resolution_kind: row.pricing_resolution_kind,
        pricing_evidence_trust: row.pricing_evidence_trust,
        unpriced_reason: row.unpriced_reason,
        pricing_template_kind: row.pricing_template_kind,
        pricing_selection_state: row.pricing_selection_state,
        pricing_card_role: row.pricing_card_role,
        pricing_selector_threshold_tokens:
          row.pricing_selector_threshold_tokens,
        pricing_selector_basis_tokens: row.pricing_selector_basis_tokens,
        report_currency_symbol: withLabels.report_currency_symbol ?? null,
        proxy_api_key_id: null,
        proxy_api_key_name_snapshot: null,
        proxy_api_key_attribution_state: "unknown",
        proxy_api_key_auth_enforced_at_request: null,
      });
    }
  }
  return rows;
}

export function appendUniqueRequestItems(
  current: RequestLogListItem[],
  incoming: RequestLogListItem[],
): RequestLogListItem[] {
  const seen = new Set(current.map((item) => item.request_log_id));
  return [
    ...current,
    ...incoming.filter((item) => {
      if (seen.has(item.request_log_id)) return false;
      seen.add(item.request_log_id);
      return true;
    }),
  ];
}

export function mergeChainRowPage(
  current: ChainIngressItem,
  page: ChainIngressItem,
): ChainIngressItem {
  const seen = new Set(current.retained_rows.map((row) => row.request_log_id));
  const retainedRows = [
    ...current.retained_rows,
    ...page.retained_rows.filter((row) => {
      if (seen.has(row.request_log_id)) return false;
      seen.add(row.request_log_id);
      return true;
    }),
  ];

  return {
    ...page,
    retained_rows: retainedRows,
    retained_rows_loaded_count: retainedRows.length,
  };
}

// Full filtered CSV export is server-side (Requests SPEC §6.8): one
// REPEATABLE READ snapshot, bounded rows, formula-safe cells. The frontend
// only downloads the produced file; current-page client CSV is gone.
import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { StatsRequestParams } from "@/lib/types";
import { timeRangeToFromTime, type RequestLogPageState } from "./queryParams";
import type { RequestLogListItem } from "@/lib/types";

function parseOptionalStatusCode(value: string): number | undefined {
  return /^\d+$/.test(value) ? Number(value) : undefined;
}

// Kept as a small projection helper for unit fixtures and accessibility
// assertions; production CSV bytes come from the server export endpoint.
export function rowValue(row: RequestLogListItem, key: string): string {
  const messages = getStaticMessages();
  if (key === "proxy_api_key") {
    if (row.proxy_api_key_attribution_state === "identified") {
      return row.proxy_api_key_name_snapshot ?? `#${row.proxy_api_key_id ?? ""}`;
    }
    if (row.proxy_api_key_attribution_state === "unknown") {
      return messages.requestLogs.proxyKeyAttributionUnknown;
    }
    return messages.requestLogs.noIdentifiedProxyKey;
  }
  if (key === "requested_model") return row.model_label || row.model_id;
  if (key === "terminal_target") return row.terminal_target_label ?? (row.terminal_target_id === null ? "" : `Terminal Target #${row.terminal_target_id}`);
  if (key === "total_tokens") return row.total_tokens === null ? "" : String(row.total_tokens);
  if (key === "created_at") return row.created_at;
  return "";
}

export function buildExportParams(state: RequestLogPageState): StatsRequestParams {
  return {
    time_range: state.from_time && state.to_time ? "custom" : state.time_range,
    ingress_request_id: state.ingress_request_id || undefined,
    model_id: state.model_id || undefined,
    proxy_api_key_id: state.proxy_api_key_id ? parseInt(state.proxy_api_key_id, 10) : undefined,
    client_rule_id: state.client_rule_id ? parseInt(state.client_rule_id, 10) : undefined,
    terminal_target_id: state.terminal_target_id ? parseInt(state.terminal_target_id, 10) : undefined,
    resolved_target_model_id: state.resolved_target_model_id || undefined,
    status_family: state.status_family === "all" ? undefined : state.status_family,
    status_code: parseOptionalStatusCode(state.status_code),
    error_text: state.error_text || undefined,
    pricing_status: state.pricing_status === "all" ? undefined : state.pricing_status,
    unpriced_reason: state.pricing_status === "unpriced" ? state.unpriced_reason || undefined : undefined,
    endpoint_id: state.endpoint_id ? parseInt(state.endpoint_id, 10) : undefined,
    from_time: state.from_time || timeRangeToFromTime(state.time_range),
    to_time: state.to_time || undefined,
    view: state.view,
  };
}

export async function downloadRequestLogsCsv(state?: RequestLogPageState): Promise<void> {
  const messages = getStaticMessages();
  try {
    const blob = await api.stats.exportCsv(state ? buildExportParams(state) : {});
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "prism-request-logs.csv";
    anchor.click();
    URL.revokeObjectURL(url);
  } catch (err) {
    console.error("request log CSV export failed", err);
    alert(err instanceof Error ? err.message : messages.requestLogs.loadFailed);
  }
}

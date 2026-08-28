// Full filtered CSV export is server-side (Requests SPEC §6.8): one
// REPEATABLE READ snapshot, bounded rows, formula-safe cells. The frontend
// only downloads the produced file; current-page client CSV is gone.
import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { StatsRequestParams } from "@/lib/types";
import type { RequestLogPageState } from "./queryParams";
import type { RequestLogListItem } from "@/lib/types";
import {
  buildRequestLogFilterParams,
  buildRequestLogTimeParams,
} from "./requestLogQuery";

// Kept as a small projection helper for unit fixtures and accessibility
// assertions; production CSV bytes come from the server export endpoint.
export function rowValue(row: RequestLogListItem, key: string): string {
  const messages = getStaticMessages();
  if (key === "proxy_api_key") {
    if (row.proxy_api_key_attribution_state === "identified") {
      return (
        row.proxy_api_key_name_snapshot ?? `#${row.proxy_api_key_id ?? ""}`
      );
    }
    if (row.proxy_api_key_attribution_state === "unknown") {
      return messages.requestLogs.proxyKeyAttributionUnknown;
    }
    return messages.requestLogs.noIdentifiedProxyKey;
  }
  if (key === "requested_model") return row.model_label || row.ingress_model_id;
  if (key === "terminal_target")
    return (
      row.terminal_target_label ??
      (row.terminal_target_id === null
        ? ""
        : `Terminal Target #${row.terminal_target_id}`)
    );
  if (key === "total_tokens")
    return row.total_tokens === null ? "" : String(row.total_tokens);
  if (key === "created_at") return row.created_at;
  return "";
}

export function buildExportParams(
  state: RequestLogPageState,
): StatsRequestParams {
  return {
    ...buildRequestLogTimeParams(state),
    ...buildRequestLogFilterParams(state),
    view: state.view,
  } as StatsRequestParams;
}

export async function downloadRequestLogsCsv(
  state?: RequestLogPageState,
): Promise<void> {
  const messages = getStaticMessages();
  try {
    const blob = await api.stats.exportCsv(
      state ? buildExportParams(state) : {},
    );
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

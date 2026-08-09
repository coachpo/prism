import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import { formatApiFamily } from "@/lib/utils";
import type { RequestLogListItem } from "@/lib/types";
import {
  getStreamOutcomeLabel,
  hasStreamTelemetryOutcome,
  isStreamUsageUnavailableReason,
} from "./streamTelemetry";
import { formatCost, formatTokenRate, formatTokens, formatTtft, getColumns } from "./columns";

const EMPTY_VALUE = "";

function csvEscape(value: string): string {
  return /[",\n\r]/.test(value) ? `"${value.replaceAll("\"", "\"\"")}"` : value;
}

function csvValue(value: string | number | null | undefined): string {
  if (value === null || value === undefined) return EMPTY_VALUE;
  return csvEscape(String(value));
}

export function rowValue(row: RequestLogListItem, key: string): string {
  const messages = getStaticMessages();
  switch (key) {
    case "created_at":
      return row.created_at;
    case "status_code":
      return String(row.status_code);
    case "response_time_ms":
      return `${formatNumber(row.response_time_ms, getCurrentLocale())}ms`;
    case "ttft_ms":
      return formatTtft(row.ttft_ms);
    case "token_rate":
      return formatTokenRate(row.output_tokens, row.ttft_ms, row.completion_duration_ms);
    case "requested_model":
      return row.model_label || row.model_id;
    case "final_target_model":
      return row.resolved_target_model_label ?? row.resolved_target_model_id ?? row.model_label ?? row.model_id;
    case "endpoint_id":
      return row.endpoint_label;
    case "client":
      return row.user_agent_overridden
        ? `${row.caller_client_display ?? ""} -> ${row.upstream_client_display ?? ""}`.trim()
        : row.caller_client_display ?? row.upstream_client_display ?? "";
    case "proxy_api_key": {
      const attribution = row.proxy_api_key_attribution_state;
      if (attribution === "identified") {
        return row.proxy_api_key_name_snapshot ?? `#${row.proxy_api_key_id ?? ""}`;
      }
      if (attribution === "unknown") {
        return messages.requestLogs.proxyKeyAttributionUnknown;
      }
      return messages.requestLogs.noIdentifiedProxyKey;
    }
    case "reasoning_effort":
      return row.reasoning_effort ?? "";
    case "api_family":
      return formatApiFamily(row.api_family ?? "");
    case "total_tokens":
      return formatTokens(row.total_tokens);
    case "total_cost":
      return isStreamUsageUnavailableReason(row.unpriced_reason)
        ? messages.requestLogs.streamUsageUnavailable
        : formatCost(row.total_cost_user_currency_micros, row.report_currency_symbol);
    case "is_stream":
      return hasStreamTelemetryOutcome(row.stream_outcome)
        ? getStreamOutcomeLabel(row.stream_outcome, messages.requestLogs)
        : "";
    default:
      return "";
  }
}

// ponytail: exports current page only (<=500 rows); add a backend streaming endpoint if full-range export is ever needed.
export function downloadRequestLogsCsv(rows: RequestLogListItem[]) {
  const columns = getColumns();
  const lines = [
    columns.map((column) => csvValue(column.label)).join(","),
    ...rows.map((row) => columns.map((column) => csvValue(rowValue(row, column.key))).join(",")),
  ];
  const blob = new Blob([lines.join("\n")], {
    type: "text/csv;charset=utf-8",
  });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = "prism-request-logs.csv";
  anchor.click();
  URL.revokeObjectURL(url);
}

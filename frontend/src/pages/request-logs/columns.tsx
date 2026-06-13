import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { TypeBadge, ValueBadge } from "@/components/StatusBadge";
import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import { formatMoneyMicros, resolveSpendTrustState } from "@/lib/costing";
import { cn, formatApiFamily } from "@/lib/utils";
import type { RequestLogListItem } from "@/lib/types";
import { AlertCircle, Clock } from "lucide-react";
import {
  getStreamOutcomeIntent,
  getStreamOutcomeLabel,
  hasStreamTelemetryOutcome,
  isStreamUsageUnavailableReason,
} from "./streamTelemetry";

export const ROW_HEIGHT = 45;

function formatCost(micros: number | null, symbol: string | null): string {
  if (micros === null) return "—";
  return formatMoneyMicros(micros, symbol ?? undefined, undefined, 2, 6, getCurrentLocale());
}

function formatTokens(tokens: number | null): string {
  if (tokens === null) return "—";
  return formatNumber(tokens, getCurrentLocale());
}

function formatTtft(ttftMs: number | null | undefined): string {
  if (ttftMs === null || ttftMs === undefined || !Number.isFinite(ttftMs)) {
    return "—";
  }

  return `${formatNumber(ttftMs, getCurrentLocale())}ms`;
}

function formatTokenRate(
  outputTokens: number | null | undefined,
  ttftMs: number | null | undefined,
  completionDurationMs: number | null | undefined,
): string {
  if (
    outputTokens === null ||
    outputTokens === undefined ||
    !Number.isFinite(outputTokens) ||
    ttftMs === null ||
    ttftMs === undefined ||
    !Number.isFinite(ttftMs) ||
    completionDurationMs === null ||
    completionDurationMs === undefined ||
    !Number.isFinite(completionDurationMs)
  ) {
    return "—";
  }

  const decodeDurationMs = completionDurationMs - ttftMs;
  if (decodeDurationMs <= 0) {
    return "—";
  }

  const tokensPerSecond = (outputTokens * 1000) / decodeDurationMs;
  return `${formatNumber(tokensPerSecond, getCurrentLocale(), {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  })} tok/s`;
}

function statusIntent(code: number) {
  if (code >= 200 && code < 300) return "success" as const;
  if (code >= 400 && code < 500) return "warning" as const;
  return "danger" as const;
}

function latencyColor(ms: number): string {
  if (ms < 500) return "text-success";
  if (ms < 2000) return "text-foreground";
  if (ms < 5000) return "font-medium text-warning";
  return "font-bold text-destructive";
}

function getSingleClientDisplay(row: RequestLogListItem): string {
  return row.caller_client_display ?? row.upstream_client_display ?? "—";
}

function resolveRequestLogSpendTrust(row: RequestLogListItem) {
  return resolveSpendTrustState({
    costMicros: row.total_cost_user_currency_micros,
    priced: row.priced_flag,
    unpricedReason: row.unpriced_reason,
  });
}

function renderClientCell(row: RequestLogListItem): React.ReactNode {
  if (!row.user_agent_overridden) {
    return <span className="block truncate text-xs font-medium">{getSingleClientDisplay(row)}</span>;
  }

  const callerDisplay = row.caller_client_display ?? "—";
  const upstreamDisplay = row.upstream_client_display ?? "—";

  return (
    <div className="flex min-w-0 items-center gap-1.5 text-xs">
      <span className="truncate text-muted-foreground">{callerDisplay}</span>
      <span className="shrink-0 text-muted-foreground">→</span>
      <span className="truncate font-medium">{upstreamDisplay}</span>
    </div>
  );
}

function renderSpendCell(row: RequestLogListItem): React.ReactNode {
  const spendTrust = resolveRequestLogSpendTrust(row);
  const messages = getStaticMessages();
  const value = isStreamUsageUnavailableReason(row.unpriced_reason)
    ? messages.requestLogs.streamUsageUnavailable
    : spendTrust === "unpriced"
      ? messages.spendTrust.unpriced
      : formatCost(row.total_cost_user_currency_micros, row.report_currency_symbol);

  return (
    <div className="flex flex-col items-end gap-1">
      <span className={cn("text-xs font-mono", spendTrust === "unpriced" ? "font-medium text-destructive" : "font-medium text-foreground")}>
        {value}
      </span>
    </div>
  );
}

export interface ColumnDef {
  key: string;
  label: string;
  width: number;
  grow?: number;
  headerTestId?: string;
  align?: "left" | "right" | "center";
  render: (
    row: RequestLogListItem,
    formatTimestamp: (iso: string) => string,
  ) => React.ReactNode;
}

export function getColumns(): ColumnDef[] {
  const staticMessages = getStaticMessages();
  const messages = staticMessages.requestLogs;
  return [
    {
      key: "created_at",
      label: messages.time,
      width: 168,
      grow: 0,
      render: (row, fmt) => (
        <div className="flex items-center gap-2">
          {row.status_code >= 500 && <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />}
          {row.response_time_ms >= 5000 && row.status_code < 500 && <Clock className="h-3.5 w-3.5 shrink-0 text-warning" />}
          <span className="truncate text-xs text-muted-foreground font-mono">{fmt(row.created_at)}</span>
        </div>
      ),
    },
    {
      key: "status_code",
      label: messages.status,
      width: 84,
      grow: 0,
      align: "center",
      render: (row) => <ValueBadge label={String(row.status_code)} intent={statusIntent(row.status_code)} className="px-1.5 py-0 font-mono" />,
    },
    {
      key: "response_time_ms",
      label: messages.latency,
      width: 108,
      grow: 0,
      align: "right",
      render: (row) => (
        <span className={cn("text-xs font-mono", latencyColor(row.response_time_ms))}>
          {new Intl.NumberFormat(getCurrentLocale()).format(row.response_time_ms)}ms
        </span>
      ),
    },
    {
      key: "ttft_ms",
      label: messages.ttft,
      width: 96,
      grow: 0,
      align: "right",
      render: (row) => (
        <span className="text-xs font-mono text-muted-foreground">
          {formatTtft(row.ttft_ms)}
        </span>
      ),
    },
    {
      key: "token_rate",
      label: messages.tokenRate,
      width: 118,
      grow: 0,
      align: "right",
      render: (row) => (
        <span className="text-xs font-mono text-muted-foreground">
          {formatTokenRate(row.output_tokens, row.ttft_ms, row.completion_duration_ms)}
        </span>
      ),
    },
    {
      key: "requested_model",
      label: messages.requestedModel,
      width: 170,
      grow: 2,
      render: (row) => {
        const requestedModelValue = row.model_label || row.model_id;

        return (
          <div className="min-w-0">
            <span className="block truncate text-xs font-medium">{requestedModelValue}</span>
          </div>
        );
      },
    },
    {
      key: "final_target_model",
      label: messages.finalTargetModel,
      width: 190,
      grow: 2,
      render: (row) => {
        const finalTargetValue = row.resolved_target_model_label ?? row.resolved_target_model_id ?? row.model_label ?? row.model_id;

        return (
          <div className="min-w-0">
            <span className="block truncate text-xs font-medium">{finalTargetValue}</span>
          </div>
        );
      },
    },
    {
      key: "endpoint_id",
      label: messages.endpoint,
      width: 180,
      grow: 2,
      render: (row) => (
        <div className="min-w-0">
          <span className="block truncate text-xs font-medium">{row.endpoint_label}</span>
        </div>
      ),
    },
    {
      key: "client",
      label: messages.client,
      width: 180,
      grow: 2,
      render: (row) => <div className="min-w-0">{renderClientCell(row)}</div>,
    },
    {
      key: "reasoning_effort",
      label: messages.reasoningEffort,
      width: 132,
      grow: 0,
      render: (row) => (
        <span className="block truncate text-xs text-muted-foreground">
          {row.reasoning_effort ?? "—"}
        </span>
      ),
    },
    {
      key: "api_family",
      label: staticMessages.common.apiFamily,
      width: 150,
      grow: 1,
      render: (row) => (
        <div className="min-w-0">
          <span className="mt-0.5 flex items-center gap-1.5 overflow-hidden text-[11px] text-muted-foreground">
            <ApiFamilyIcon apiFamily={row.api_family ?? ""} size={13} className="text-muted-foreground" />
            <span className="truncate">{formatApiFamily(row.api_family ?? "")}</span>
          </span>
        </div>
      ),
    },
    {
      key: "total_tokens",
      label: messages.tokens,
      width: 110,
      grow: 0,
      align: "right",
      render: (row) => (
        <span className="text-xs font-mono text-muted-foreground">
          {formatTokens(row.total_tokens)}
        </span>
      ),
    },
    {
      key: "total_cost",
      label: messages.spend,
      width: 132,
      grow: 0,
      align: "right",
      render: (row) => renderSpendCell(row),
    },
    {
      key: "is_stream",
      label: messages.stream,
      width: 168,
      grow: 0,
      align: "center",
      render: (row) =>
        hasStreamTelemetryOutcome(row.stream_outcome) ? (
          <TypeBadge
            label={getStreamOutcomeLabel(row.stream_outcome, messages)}
            intent={getStreamOutcomeIntent(row.stream_outcome)}
            className="px-2 py-0.5"
            preserveLabel
          />
        ) : (
          <span className="text-[10px] text-muted-foreground">—</span>
        ),
    },
  ];
}

export { formatCost, formatTokenRate, formatTokens, formatTtft };

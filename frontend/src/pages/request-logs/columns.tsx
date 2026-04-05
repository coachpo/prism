import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { TypeBadge, ValueBadge } from "@/components/StatusBadge";
import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import { formatMoneyMicros } from "@/lib/costing";
import { cn, formatApiFamily } from "@/lib/utils";
import type { ModelConfigListItem, RequestLogListItem } from "@/lib/types";
import { AlertCircle, Clock } from "lucide-react";

export const ROW_HEIGHT = 45;

function formatCost(micros: number | null, symbol: string | null): string {
  if (micros === null || micros === 0) return "—";
  return formatMoneyMicros(micros, symbol ?? "$", undefined, 2, 6, getCurrentLocale());
}

function formatTokens(tokens: number | null): string {
  if (tokens === null) return "—";
  return formatNumber(tokens, getCurrentLocale());
}

function statusIntent(code: number) {
  if (code >= 200 && code < 300) return "success" as const;
  if (code >= 400 && code < 500) return "warning" as const;
  return "danger" as const;
}

function latencyColor(ms: number): string {
  if (ms < 500) return "text-emerald-600 dark:text-emerald-400";
  if (ms < 2000) return "text-foreground";
  if (ms < 5000) return "text-amber-600 dark:text-amber-400 font-medium";
  return "text-red-600 dark:text-red-400 font-bold";
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
    resolveModelLabel: RequestLogModelResolver,
    resolveEndpointLabel: RequestLogEndpointResolver,
  ) => React.ReactNode;
}

export type RequestLogModelResolver = ((modelId: string) => string) & {
  getModelMetadata?: (modelId: string) => ModelConfigListItem | undefined;
};

export type RequestLogEndpointResolver = (endpointId: number | null) => string;

function getRequestModelMetadata(
  resolveModelLabel: RequestLogModelResolver,
  modelId: string,
): ModelConfigListItem | undefined {
  return resolveModelLabel.getModelMetadata?.(modelId);
}

export function isProxyOriginRequest(
  row: Pick<RequestLogListItem, "model_id" | "resolved_target_model_id">,
  resolveModelLabel: RequestLogModelResolver,
): boolean {
  if (
    row.resolved_target_model_id !== null &&
    row.resolved_target_model_id !== row.model_id
  ) {
    return true;
  }

  return getRequestModelMetadata(resolveModelLabel, row.model_id)?.model_type === "proxy";
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
          {row.status_code >= 500 && <AlertCircle className="h-3.5 w-3.5 text-red-500 shrink-0" />}
          {row.response_time_ms >= 5000 && row.status_code < 500 && <Clock className="h-3.5 w-3.5 text-amber-500 shrink-0" />}
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
      key: "model_id",
      label: messages.model,
      width: 240,
      grow: 3,
      render: (row, _formatTimestamp, resolveModelLabel) => {
        const requestedModelLabel = resolveModelLabel(row.model_id);
        const resolvedTargetLabel = row.resolved_target_model_id
          ? resolveModelLabel(row.resolved_target_model_id)
          : null;
        const isProxyOrigin = isProxyOriginRequest(row, resolveModelLabel);

        return (
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-1.5">
              <span className="block min-w-0 truncate text-xs font-medium">{requestedModelLabel}</span>
              {isProxyOrigin ? (
                <TypeBadge label={messages.proxyOrigin} intent="accent" className="px-2 py-0.5" />
              ) : null}
            </div>
            {resolvedTargetLabel && row.resolved_target_model_id !== row.model_id ? (
              <span className="block truncate text-[11px] text-muted-foreground">
                {messages.resolvedTarget} → {resolvedTargetLabel}
              </span>
            ) : null}
          </div>
        );
      },
    },
    {
      key: "endpoint_id",
      label: messages.endpoint,
      width: 180,
      grow: 2,
      render: (row, _formatTimestamp, _resolveModelLabel, resolveEndpointLabel) => {
        const endpointLabel = resolveEndpointLabel(row.endpoint_id);

        return (
          <div className="min-w-0">
            <span className="block truncate text-xs font-medium">{endpointLabel}</span>
          </div>
        );
      },
    },
    {
      key: "vendor_api_family",
      label: `${staticMessages.common.vendor} / API`,
      width: 150,
      grow: 1,
      render: (row) => (
        <div className="min-w-0">
          <span className="block truncate text-xs font-medium">
            {row.vendor_name ?? "—"}
          </span>
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
      width: 104,
      grow: 0,
      align: "right",
      render: (row) => (
        <span className={cn("text-xs font-mono", row.total_cost_user_currency_micros && row.total_cost_user_currency_micros > 0 ? "text-foreground font-medium" : "text-muted-foreground")}>
          {formatCost(row.total_cost_user_currency_micros, row.report_currency_symbol)}
        </span>
      ),
    },
    {
      key: "is_stream",
      label: messages.stream,
      width: 92,
      grow: 0,
      align: "center",
      render: (row) =>
        row.is_stream ? (
          <TypeBadge label={messages.streaming} intent="blue" className="px-2 py-0.5" />
        ) : (
          <span className="text-[10px] text-muted-foreground">—</span>
        ),
    },
  ];
}

export { formatCost, formatTokens };

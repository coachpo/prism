import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  formatUnpricedReasonLabel,
  resolveSpendTrustState,
} from "@/lib/costing";
import { cn } from "@/lib/utils";
import type { RequestLogListItem } from "@/lib/types";
import {
  OperatorMissingValue,
  OperatorTypeBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import { AlertCircle } from "lucide-react";
import {
  getStreamOutcomeIntent,
  getStreamOutcomeLabel,
  hasStreamTelemetryOutcome,
  isStreamUsageUnavailableReason,
} from "./streamTelemetry";
import { describeUnpricedCause } from "./pricingExplanation";
import {
  formatCost,
  describeTokenRateMissing,
  formatTokenRate,
  formatTokens,
  formatTtft,
} from "./requestLogMetricPresentation";

export const ROW_HEIGHT = 45;

function pricingRoleLabel(
  role: RequestLogListItem["pricing_card_role"],
  copy: ReturnType<typeof getStaticMessages>["requestLogs"],
) {
  switch (role) {
    case "standard":
      return copy.pricingCardStandard;
    case "tier_base":
      return copy.pricingCardTierBase;
    case "tier_above":
      return copy.pricingCardTierAbove;
    case "peak":
      return copy.pricingCardPeak;
    case "offpeak":
      return copy.pricingCardOffpeak;
    default:
      return null;
  }
}

function pricingSelectionListLabel(
  row: RequestLogListItem,
  copy: ReturnType<typeof getStaticMessages>["requestLogs"],
) {
  switch (row.pricing_selection_state) {
    case "selected":
      return `${copy.pricingSelectionSelected} · ${pricingRoleLabel(row.pricing_card_role, copy) ?? copy.pricingSelectionUnavailable}`;
    case "not_applicable":
      return `${copy.pricingSelectionNotApplicable} · ${pricingRoleLabel(row.pricing_card_role, copy) ?? copy.pricingSelectionUnavailable}`;
    case "not_evaluated":
      return copy.pricingSelectionNotEvaluated;
    case "unresolved":
      return row.pricing_resolution_kind === "schedule_unresolved"
        ? copy.pricingResolutionScheduleUnresolved
        : copy.pricingSelectionUnresolved;
    default:
      return copy.pricingSelectionUnavailable;
  }
}

function statusIntent(code: number) {
  if (code >= 200 && code < 300) return "healthy" as const;
  if (code >= 400 && code < 500) return "degraded" as const;
  return "failing" as const;
}

/** Return the scoped status only; transport rows intentionally have none. */
export function scopedStatus(row: RequestLogListItem): number | null {
  if (row.row_kind === "planning" || row.row_kind === "admission") {
    return row.gateway_status_code;
  }
  if (row.row_kind === "upstream") {
    return row.upstream_status_code;
  }
  return row.legacy_status_code;
}

function rowDurationMs(row: RequestLogListItem): number | null {
  return row.attempt_duration_ms ?? row.response_time_ms ?? null;
}

function getSingleClientDisplay(row: RequestLogListItem): string {
  return row.caller_client_display ?? row.upstream_client_display ?? "—";
}

function resolveRequestLogSpendTrust(row: RequestLogListItem) {
  return resolveSpendTrustState({
    costMicros: row.total_cost_user_currency_micros,
    pricingStatus: row.pricing_status,
    unpricedReason: row.unpriced_reason,
  });
}

function renderClientCell(row: RequestLogListItem): React.ReactNode {
  if (!row.user_agent_overridden) {
    return (
      <span className="block truncate text-xs font-medium">
        {getSingleClientDisplay(row)}
      </span>
    );
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
      : formatCost(
          row.total_cost_user_currency_micros,
          row.report_currency_symbol,
        );

  return (
    <div className="flex flex-col items-end gap-1">
      <span
        className={cn(
          "text-xs font-mono",
          spendTrust === "unpriced"
            ? "font-medium text-destructive"
            : "font-medium text-foreground",
        )}
      >
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
          {(scopedStatus(row) ?? 0) >= 500 && (
            <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />
          )}
          <span className="truncate text-xs text-muted-foreground font-mono">
            {fmt(row.created_at)}
          </span>
        </div>
      ),
    },
    {
      key: "status_code",
      label: messages.status,
      width: 84,
      grow: 0,
      align: "center",
      render: (row) => {
        const status = scopedStatus(row);
        return status === null ? (
          <span className="text-xs text-muted-foreground">—</span>
        ) : (
          <OperatorValueBadge
            label={String(status)}
            intent={statusIntent(status)}
            className="px-1.5 py-0 font-mono"
          />
        );
      },
    },
    {
      key: "response_time_ms",
      label: messages.latency,
      width: 108,
      grow: 0,
      align: "right",
      render: (row) => (
        <span className="text-xs font-mono text-muted-foreground">
          {rowDurationMs(row) === null
            ? "—"
            : `${new Intl.NumberFormat(getCurrentLocale()).format(rowDurationMs(row) ?? 0)}ms`}
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
      render: (row) => {
        if (
          row.output_rate_state === "measured" &&
          row.output_rate_tps !== null
        ) {
          return (
            <span className="text-xs font-mono text-muted-foreground">
              {formatTokenRate(row.output_rate_tps, row.output_rate_state)}
            </span>
          );
        }
        return (
          <OperatorMissingValue
            reason={describeTokenRateMissing({
              rateTps: row.output_rate_tps,
              state: row.output_rate_state,
              reason: row.output_rate_reason,
            })}
          />
        );
      },
    },
    {
      key: "requested_model",
      label: messages.requestedModel,
      width: 170,
      grow: 2,
      render: (row) => {
        const requestedModelValue = row.model_label || row.ingress_model_id;

        return (
          <div className="min-w-0">
            <span className="block truncate text-xs font-medium">
              {requestedModelValue}
            </span>
          </div>
        );
      },
    },
    {
      key: "attempt_target_model",
      label: messages.attemptTargetModel,
      width: 190,
      grow: 2,
      render: (row) => {
        const finalTargetValue =
          row.attempt_target_model_label ?? row.attempt_target_model_id;

        return (
          <div className="min-w-0">
            {finalTargetValue ? (
              <span className="block truncate text-xs font-medium">
                {finalTargetValue}
              </span>
            ) : (
              <span
                className="block truncate text-xs text-muted-foreground"
                title={messages.finalTargetEvidenceUnavailable}
              >
                —
              </span>
            )}
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
          <span className="block truncate text-xs font-medium">
            {row.endpoint_label}
          </span>
        </div>
      ),
    },
    {
      key: "terminal_target",
      label: messages.terminalTarget ?? "Terminal Target",
      width: 180,
      grow: 2,
      render: (row) => (
        <span className="block truncate text-xs font-medium">
          {row.terminal_target_label ??
            (row.terminal_target_id === null
              ? "—"
              : messages.terminalTargetId(row.terminal_target_id))}
        </span>
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
      key: "proxy_api_key",
      label: messages.proxyKey,
      width: 180,
      grow: 2,
      render: (row) => {
        const attribution = row.proxy_api_key_attribution_state;
        if (attribution === "identified") {
          return (
            <div className="min-w-0">
              <span className="block truncate text-xs font-medium">
                {row.proxy_api_key_name_snapshot ??
                  `#${row.proxy_api_key_id ?? ""}`}
              </span>
            </div>
          );
        }
        if (attribution === "unknown") {
          return (
            <div className="min-w-0">
              <span className="block truncate text-xs text-muted-foreground">
                {messages.proxyKeyAttributionUnknown}
              </span>
            </div>
          );
        }
        return (
          <div className="min-w-0">
            <span className="block truncate text-xs text-muted-foreground">
              {messages.noIdentifiedProxyKey}
            </span>
          </div>
        );
      },
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
            <ApiFamilyIcon
              apiFamily={row.api_family ?? ""}
              size={13}
              className="text-muted-foreground"
            />
            <span className="truncate">
              {formatApiFamily(row.api_family ?? "")}
            </span>
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
      key: "pricing_state",
      label: messages.pricingStateLabel,
      width: 168,
      grow: 1,
      render: (row) => {
        const copy = getStaticMessages().requestLogs;
        const label =
          row.pricing_status === "priced"
            ? copy.priced
            : row.pricing_status === "unpriced"
              ? row.unpriced_reason
                ? formatUnpricedReasonLabel(row.unpriced_reason)
                : copy.pricingStatusUnpriced
              : row.pricing_status === "ineligible"
                ? copy.ineligible
                : copy.unknown;
        const cause = describeUnpricedCause({
          pricingStatus: row.pricing_status,
          unpricedReason: row.unpriced_reason,
          streamOutcome: row.stream_outcome,
        });
        return (
          <div className="min-w-0" title={cause ?? undefined}>
            <span className="block truncate text-xs font-medium">{label}</span>
            <span className="block truncate text-[10px] text-muted-foreground">
              {pricingSelectionListLabel(row, copy)}
            </span>
          </div>
        );
      },
    },
    {
      key: "is_stream",
      label: messages.stream,
      width: 168,
      grow: 0,
      align: "center",
      render: (row) =>
        hasStreamTelemetryOutcome(row.stream_outcome) ? (
          <OperatorTypeBadge
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

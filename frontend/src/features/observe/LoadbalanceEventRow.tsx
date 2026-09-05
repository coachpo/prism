import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { LoadbalanceEventListItem } from "@/lib/types";
import {
  OperatorMissingValue,
  OperatorTypeBadge,
} from "@/shared/design-system";
import { renderEventSummary } from "./eventSummary";

type RoutingHealthCopy = ReturnType<
  typeof useLocale
>["messages"]["routingHealth"];

export function LoadbalanceEventRow({
  copy,
  formatTime,
  item,
  onOpenDetail,
}: {
  copy: RoutingHealthCopy;
  formatTime: (value: string, options?: Intl.DateTimeFormatOptions) => string;
  item: LoadbalanceEventListItem;
  onOpenDetail: () => void;
}) {
  const summary = renderEventSummary(item.summary, copy.eventSummary);
  const modelLabel = item.model.label || item.model.model_id || null;
  const targetLabel =
    item.terminal_target.label || `#${item.terminal_target.id ?? "?"}`;
  const endpointLabel =
    item.endpoint.label ||
    (item.endpoint.id != null ? `#${item.endpoint.id}` : null);
  // 「相关窗口」按事件类型换口径：已封禁看封禁截止，其余优先看下次重试，
  // 都没有才回落到上次成功。相邻两行的口径可以不同、相对事件时间的方向也会
  // 翻转，所以这里连口径标签一起给出，单元格不能只留一个裸时间戳。
  const windowBasis =
    item.event_type === "banned" && item.banned_until_at
      ? { label: copy.banUntilColumn, value: item.banned_until_at }
      : item.next_retry_at
        ? { label: copy.nextRetryColumn, value: item.next_retry_at }
        : item.last_success_at
          ? { label: copy.lastSuccessField, value: item.last_success_at }
          : null;

  return (
    <TableRow data-testid={`event-row-${item.event_id}`}>
      <TableCell className="whitespace-nowrap font-mono tabular-nums">
        {formatTime(item.created_at)}
      </TableCell>
      <TableCell>
        <div className="flex flex-col gap-1">
          <span className="font-medium">{summary.label}</span>
          <span className="text-xs text-muted-foreground">
            {summary.reason}
          </span>
          <div className="flex flex-wrap gap-1">
            <OperatorTypeBadge
              label={eventTypeLabel(item.event_type, copy)}
              preserveLabel
            />
            {item.failure_kind ? (
              <OperatorTypeBadge
                label={failureKindLabel(item.failure_kind, copy)}
                intent="degraded"
                preserveLabel
              />
            ) : null}
            {item.admission_reason ? (
              <OperatorTypeBadge
                label={admissionReasonLabel(item.admission_reason, copy)}
                intent="degraded"
                preserveLabel
              />
            ) : null}
          </div>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-col">
          <span>{modelLabel ?? <OperatorMissingValue />}</span>
          <span className="font-mono text-xs text-muted-foreground">
            {item.model.model_id ?? ""}
          </span>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-col">
          <span>{targetLabel}</span>
          <span className="text-xs text-muted-foreground">
            {endpointLabel ?? <OperatorMissingValue />}
          </span>
        </div>
      </TableCell>
      <TableCell className="whitespace-nowrap">
        {windowBasis ? (
          <div className="flex flex-col">
            <span className="text-xs text-muted-foreground">
              {windowBasis.label}
            </span>
            <span className="font-mono tabular-nums">
              {formatTime(windowBasis.value)}
            </span>
          </div>
        ) : (
          <OperatorMissingValue reason={copy.windowMissingReason} />
        )}
      </TableCell>
      <TableCell className="text-right">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onOpenDetail}
        >
          {copy.viewDetail}
        </Button>
      </TableCell>
    </TableRow>
  );
}

function eventTypeLabel(value: string, copy: RoutingHealthCopy): string {
  switch (value) {
    case "retry_scheduled":
      return copy.eventSummary.retryScheduled;
    case "retry_exhausted":
      return copy.eventSummary.retryExhausted;
    case "banned":
      return copy.eventSummary.banned;
    case "unbanned":
      return copy.eventSummary.unbanned;
    case "recovered":
      return copy.eventSummary.recovered;
    case "admission_rejected":
      return copy.eventSummary.admissionRejected;
    default:
      return copy.eventSummary.unknownEvent;
  }
}

function failureKindLabel(value: string, copy: RoutingHealthCopy): string {
  switch (value) {
    case "transient_http":
      return copy.eventSummary.failureTransientHttp;
    case "connect_error":
      return copy.eventSummary.failureConnectError;
    case "timeout":
      return copy.eventSummary.failureTimeout;
    default:
      return copy.eventSummary.unknownEvent;
  }
}

function admissionReasonLabel(value: string, copy: RoutingHealthCopy): string {
  switch (value) {
    case "qps_limit":
      return copy.eventSummary.admissionQpsLimit;
    case "max_in_flight_stream":
      return copy.eventSummary.admissionMaxInFlightStream;
    case "max_in_flight_non_stream":
      return copy.eventSummary.admissionMaxInFlightNonStream;
    default:
      return copy.eventSummary.unknownEvent;
  }
}

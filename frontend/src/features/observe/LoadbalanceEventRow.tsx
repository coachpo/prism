import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { LoadbalanceEventListItem } from "@/lib/types";
import {
  OperatorMissingValue,
  OperatorTypeBadge,
} from "@/shared/design-system";
import {
  admissionReasonLabel,
  failureKindLabel,
  renderEventSummary,
} from "./eventSummary";

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
  // 同一件事此前渲染三遍：标题行、原因句、再加一枚与标题逐字相同的徽章。
  // 身份只留带形状标记的徽章（它带着「历史证据不完整」后缀），原因句降为第二行。
  //
  // 失败类型同理：原因句已经点名触发失败的行不再挂同义徽章；「已成功」与
  // 「封禁已到期」的 failure_kind 说的是此前那一类失败，不加限定词并排读起来
  // 像是这次成功里也有 HTTP 失败。
  const failureIsPrior =
    item.event_type === "recovered" || item.event_type === "unbanned";
  const failureKind = item.failure_kind
    ? failureKindLabel(item.failure_kind, copy.eventSummary)
    : null;
  const failureBadge =
    failureKind === null
      ? null
      : failureIsPrior
        ? copy.priorFailureBadge(failureKind)
        : item.summary.params.failure_kind
          ? null
          : failureKind;
  const admissionBadge =
    item.admission_reason && !item.summary.params.admission_reason
      ? admissionReasonLabel(item.admission_reason, copy.eventSummary)
      : null;
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
          <div className="flex flex-wrap gap-1">
            <OperatorTypeBadge label={summary.label} preserveLabel />
            {failureBadge ? (
              <OperatorTypeBadge
                label={failureBadge}
                intent="degraded"
                preserveLabel
              />
            ) : null}
            {admissionBadge ? (
              <OperatorTypeBadge
                label={admissionBadge}
                intent="degraded"
                preserveLabel
              />
            ) : null}
          </div>
          <span className="text-xs text-muted-foreground">
            {summary.reason}
          </span>
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

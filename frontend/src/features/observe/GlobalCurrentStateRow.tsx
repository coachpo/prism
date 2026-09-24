import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  TableCell,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { GlobalCurrentStateItem } from "@/lib/types";
import {
  OperatorMissingValue,
  OperatorStatusBadge,
  type OperatorStatusTier,
} from "@/shared/design-system";
import { requestServiceLabel } from "@/pages/request-logs/requestFailurePresentation";
import { cn } from "@/lib/utils";
import { operationalRowStripe } from "@/shared/table/operationalTable";
import { canResetCooldown } from "./useRoutingHealthCurrentStateReset";

type RoutingHealthCopy = ReturnType<
  typeof useLocale
>["messages"]["routingHealth"];

export function GlobalCurrentStateRow({
  copy,
  formatNumber,
  formatTime,
  item,
  onRequestReset,
  resetting,
  showActions,
}: {
  copy: RoutingHealthCopy;
  formatNumber: (value: number) => string;
  formatTime: (value: string, options?: Intl.DateTimeFormatOptions) => string;
  item: GlobalCurrentStateItem;
  onRequestReset: () => void;
  resetting: boolean;
  /** The table carries an actions column only while some row can be reset. */
  showActions: boolean;
}) {
  const observed = item.observation_state === "observed";
  const hasAttemptCounters =
    item.cycle_retry_attempts !== null &&
    item.cumulative_retry_attempts !== null;
  const tier = stateTier(item);

  return (
    <TableRow
      data-testid={`runtime-row-${item.terminal_target.id}`}
      className={cn("group/row", operationalRowStripe(tier))}
    >
      {/* 行状态条把首格强制成 relative（选择器特异度更高），
          冻结列必须把 position 抢回来；sticky 同样是定位元素，::before 的状态条照常显示。 */}
      <TableCell className="sticky! left-0 z-10 bg-panel shadow-[inset_-1px_0_0_0_var(--color-border)]">
        <div className="flex flex-col">
          <span className="font-medium">{item.model.label}</span>
          <span className="font-mono text-xs text-muted-foreground">
            {item.model.id}
          </span>
        </div>
      </TableCell>
      <TableCell>
        <OperatorStatusBadge
          intent={tier}
          label={stateLabel(observed ? item.state : null, copy)}
          preserveLabel
        />
      </TableCell>
      <TableCell>
        <div className="flex flex-col">
          <span>{requestServiceLabel(item.endpoint.label, copy.unnamedService)}</span>
          {requestServiceLabel(item.terminal_target.label, "") && <span className="text-xs text-muted-foreground">{requestServiceLabel(item.terminal_target.label, "")}</span>}
        </div>
      </TableCell>
      <TableCell className="font-mono tabular-nums">
        {observed && item.last_success_at ? formatTime(item.last_success_at) : <OperatorMissingValue reason={copy.lastSuccessMissingReason} />}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {observed && hasAttemptCounters ? (
          `${formatNumber(item.cycle_retry_attempts!)} / ${formatNumber(item.cumulative_retry_attempts!)}`
        ) : (
          <OperatorMissingValue
            reason={
              observed ? copy.attemptsMissingReason : copy.stateUnobserved
            }
          />
        )}
      </TableCell>
      <TableCell className="font-mono tabular-nums">
        {observed && item.next_retry_at ? (
          formatTime(item.next_retry_at)
        ) : (
          <OperatorMissingValue
            reason={
              observed ? copy.nextRetryAbsentReason : copy.stateUnobserved
            }
          />
        )}
      </TableCell>
      <TableCell className="font-mono tabular-nums">
        {observed && item.banned_until_at ? (
          formatTime(item.banned_until_at)
        ) : (
          <OperatorMissingValue
            reason={
              observed ? copy.banUntilAbsentReason : copy.stateUnobserved
            }
          />
        )}
      </TableCell>
      {showActions ? (
        <TableCell className="sticky right-0 z-10 bg-panel text-right shadow-[inset_1px_0_0_0_var(--color-border)]">
          {/* 没有等待或暂停的服务无事可恢复，不再每行放一个点不动的按钮。 */}
          {canResetCooldown(item) ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={resetting}
              aria-busy={resetting}
              onClick={onRequestReset}
              className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover/row:opacity-100"
            >
              {resetting ? (
                <Loader2 data-icon="inline-start" className="animate-spin" />
              ) : null}
              {copy.resetCooldown}
            </Button>
          ) : null}
        </TableCell>
      ) : null}
    </TableRow>
  );
}

function stateTier(item: GlobalCurrentStateItem): OperatorStatusTier {
  if (item.observation_state !== "observed" || item.state === null) {
    return "idle";
  }
  if (item.state === "banned") return "failing";
  if (item.state === "retry_wait") return "degraded";
  return "healthy";
}

function stateLabel(
  state: string | null,
  copy: RoutingHealthCopy,
): string {
  switch (state) {
    case "available":
      return copy.stateAvailable;
    case "retry_wait":
      return copy.stateRetryWait;
    case "banned":
      return copy.stateBanned;
    default:
      return copy.stateUnobserved;
  }
}

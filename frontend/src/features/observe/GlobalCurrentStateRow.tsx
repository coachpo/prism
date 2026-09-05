import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  TableCell,
  TableRow,
} from "@/components/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useLocale } from "@/i18n/useLocale";
import type { GlobalCurrentStateItem } from "@/lib/types";
import {
  OperatorMissingValue,
  OperatorStatusBadge,
  type OperatorStatusTier,
} from "@/shared/design-system";
import { cn } from "@/lib/utils";
import { operationalRowStripe } from "@/shared/table/operationalTable";

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
}: {
  copy: RoutingHealthCopy;
  formatNumber: (value: number) => string;
  formatTime: (value: string, options?: Intl.DateTimeFormatOptions) => string;
  item: GlobalCurrentStateItem;
  onRequestReset: () => void;
  resetting: boolean;
}) {
  const observed = item.observation_state === "observed";
  const hasAttemptCounters =
    item.cycle_retry_attempts !== null &&
    item.cumulative_retry_attempts !== null;
  const tier = stateTier(item);
  const hasCooldown =
    observed && (item.state === "retry_wait" || item.state === "banned");
  const disabledReason = !observed
    ? copy.resetCooldownDisabledUnobserved
    : !hasCooldown
      ? copy.resetCooldownDisabledNoCooldown
      : null;

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
        <div className="flex flex-col">
          <span>{item.terminal_target.label}</span>
          <span className="font-mono text-xs text-muted-foreground">
            #{item.terminal_target.id}
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
      <TableCell className="sticky right-0 z-10 bg-panel text-right shadow-[inset_1px_0_0_0_var(--color-border)]">
        {disabledReason ? (
          // 「为什么不能重置」是这一页唯一需要解释的状态。原来的写法把它挂在
          // 只有打开时才挂载的 TooltipContent 上，而 disabled 按钮又不可聚焦，
          // 于是这条解释对键盘和读屏完全不存在，外面那层无名 span 还多出一个
          // 停靠点。改成 aria-disabled（可聚焦、无动作），理由常驻在 sr-only
          // 节点上，aria-describedby 永远指向真实存在的元素。
          <>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-disabled="true"
                  aria-describedby={`reset-reason-${item.terminal_target.id}`}
                  className="aria-disabled:opacity-50"
                >
                  {copy.resetCooldown}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{disabledReason}</TooltipContent>
            </Tooltip>
            <span
              id={`reset-reason-${item.terminal_target.id}`}
              className="sr-only"
            >
              {disabledReason}
            </span>
          </>
        ) : (
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
        )}
      </TableCell>
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

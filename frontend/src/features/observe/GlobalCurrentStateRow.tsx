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
      <TableCell>
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
      <TableCell className="text-right">
        {disabledReason ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span tabIndex={0}>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled
                  aria-describedby={`reset-reason-${item.terminal_target.id}`}
                >
                  {copy.resetCooldown}
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent id={`reset-reason-${item.terminal_target.id}`}>
              {disabledReason}
            </TooltipContent>
          </Tooltip>
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

import { Loader2, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { StatusBadge, ValueBadge } from "@/components/StatusBadge";
import { useLocale } from "@/i18n/useLocale";
import type {
  LoadbalanceCurrentStateItem,
  LoadbalanceCurrentStateValue,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import type { FormatTime } from "./connectionCardTypes";
import { buildCurrentStateCopy } from "./ConnectionCardSectionsShared";

export function ConnectionCardCooldownState({
  currentState,
  formatTime,
  isResetting,
  onResetCooldown,
}: {
  currentState: LoadbalanceCurrentStateItem | undefined;
  formatTime: FormatTime;
  isResetting: boolean;
  onResetCooldown: (connectionId: number) => void;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelDetail;

  if (!currentState) {
    return null;
  }

  if (currentState.state === "available" && currentState.cumulative_retry_attempts <= 0) {
    return null;
  }

  const tone = getCurrentStateTone(currentState.state, copy);
  const failureCountLabel = copy.failureCount(currentState.cumulative_retry_attempts);
  const formatCumulativeFailures = (count: number) => {
    const formattedCount = formatNumber(count);
    return `${formattedCount} cumulative failure${count === 1 ? "" : "s"}`;
  };

  return (
    <div className={cn("rounded-lg border px-3 py-2 text-xs", tone.panelClassName)}>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge label={tone.label} intent={tone.intent} />
            <ValueBadge label={failureCountLabel} intent="default" />
          </div>
          <p className="leading-5 text-muted-foreground">
            {buildCurrentStateCopy(currentState, formatTime, {
              consecutiveFailures: formatCumulativeFailures,
              cooldownMinutes: copy.cooldownMinutes,
              cooldownMinutesSeconds: copy.cooldownMinutesSeconds,
              cooldownSeconds: copy.cooldownSeconds,
              currentStateBlocked: copy.currentStateBlocked,
              currentStateCounting: copy.currentStateCounting,
              currentStateTemporaryBan: (failureSummary, until) =>
                `${failureSummary} triggered a temporary ban until ${until ?? "the ban expires"}.`,
              currentStateUntilResetBan: (failureSummary) =>
                `${failureSummary} triggered an until reset ban. Use ${copy.resetBanPolicyState} to return this connection to routing.`,
              failureKindConnectError: copy.failureKindConnectError,
              failureKindTimeout: copy.failureKindTimeout,
              failureKindTransientHttp: copy.failureKindTransientHttp,
              failureKindUnknown: copy.failureKindUnknown,
            })}
          </p>
        </div>

        <Button
          size="xs"
          variant="outline"
          disabled={isResetting}
          onClick={() => onResetCooldown(currentState.connection_id)}
          className="self-start"
        >
          {isResetting ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <RotateCcw className="h-3 w-3" />
          )}
          {copy.resetBanPolicyState}
        </Button>
      </div>
    </div>
  );
}

function getCurrentStateTone(
  state: LoadbalanceCurrentStateValue,
  copy: {
    banned: string;
    retryWindowBlocked: string;
    retryWindowCounting: string;
  },
): {
  label: string;
  intent: "warning" | "danger" | "info";
  panelClassName: string;
} {
  if (state === "retry_wait") {
    return {
      label: copy.retryWindowBlocked,
      intent: "danger",
      panelClassName: "border-red-500/20 bg-red-500/5",
    };
  }

  if (state === "banned") {
    return {
      label: copy.banned,
      intent: "danger",
      panelClassName: "border-red-500/20 bg-red-500/5",
    };
  }

  return {
    label: copy.retryWindowCounting,
    intent: "warning",
    panelClassName: "border-amber-500/20 bg-amber-500/5",
  };
}

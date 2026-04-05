import type { LoadbalanceCurrentStateItem } from "@/lib/types";
import type { FormatTime } from "./connectionCardTypes";

export type ConnectionCardCurrentStateCopy = {
  consecutiveFailures: (count: number) => string;
  cooldownMinutes: (minutes: number) => string;
  cooldownMinutesSeconds: (minutes: number, seconds: number) => string;
  cooldownSeconds: (seconds: number) => string;
  currentStateBlocked: (
    failureSummary: string,
    cooldown: string,
    failureKind: string,
    blockedUntil: string | null,
  ) => string;
  currentStateCounting: (failureSummary: string, failureKind: string) => string;
  currentStateManualBan: string;
  currentStateTemporaryBan: (until: string | null) => string;
  failureKindConnectError: string;
  failureKindTimeout: string;
  failureKindTransientHttp: string;
  failureKindUnknown: string;
};

export function getPriorityBadgeClasses(priority: number): string {
  if (priority === 0) {
    return "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400";
  }

  if (priority === 1) {
    return "border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-400";
  }

  if (priority === 2) {
    return "border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-400";
  }

  if (priority === 3) {
    return "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-400";
  }

  return "border-muted-foreground/20 bg-muted/60 text-muted-foreground";
}

export function buildCurrentStateCopy(
  currentState: LoadbalanceCurrentStateItem,
  formatTime: FormatTime,
  copy: ConnectionCardCurrentStateCopy,
): string {
  const cooldown = formatCooldownSeconds(currentState.last_cooldown_seconds, copy);
  const failureSummary = copy.consecutiveFailures(currentState.consecutive_failures);
  const failureKindLabel = getFailureKindLabel(currentState.last_failure_kind, copy);
  const blockedUntilLabel = currentState.blocked_until_at
    ? formatTime(currentState.blocked_until_at, {
        hour: "numeric",
        minute: "numeric",
        second: "numeric",
      })
    : null;
  const bannedUntilLabel = currentState.banned_until_at
    ? formatTime(currentState.banned_until_at, {
        hour: "numeric",
        minute: "numeric",
        second: "numeric",
      })
    : null;

  if (currentState.state === "banned") {
    if (currentState.ban_mode === "temporary") {
      return copy.currentStateTemporaryBan(bannedUntilLabel);
    }

    return copy.currentStateManualBan;
  }

  if (currentState.state === "blocked") {
    return copy.currentStateBlocked(failureSummary, cooldown, failureKindLabel, blockedUntilLabel);
  }

  return copy.currentStateCounting(failureSummary, failureKindLabel);
}

function formatCooldownSeconds(seconds: number, copy: Pick<ConnectionCardCurrentStateCopy, "cooldownMinutes" | "cooldownMinutesSeconds" | "cooldownSeconds">): string {
  const roundedSeconds = Math.max(0, Math.round(seconds));
  const minutes = Math.floor(roundedSeconds / 60);
  const remainingSeconds = roundedSeconds % 60;

  if (minutes === 0) {
    return copy.cooldownSeconds(roundedSeconds);
  }

  if (remainingSeconds === 0) {
    return copy.cooldownMinutes(minutes);
  }

  return copy.cooldownMinutesSeconds(minutes, remainingSeconds);
}

function getFailureKindLabel(
  failureKind: LoadbalanceCurrentStateItem["last_failure_kind"],
  copy: ConnectionCardCurrentStateCopy,
): string {
  if (failureKind === "transient_http") {
    return copy.failureKindTransientHttp;
  }
  if (failureKind === "connect_error") {
    return copy.failureKindConnectError;
  }
  if (failureKind === "timeout") {
    return copy.failureKindTimeout;
  }
  return copy.failureKindUnknown;
}

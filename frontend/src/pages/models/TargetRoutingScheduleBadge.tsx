import { useLocale } from "@/i18n/useLocale";
import type { RoutingSchedule, RoutingScheduleState } from "@/lib/types/routing";
import { OperatorStalenessBadge, OperatorTypeBadge } from "@/shared/design-system";
import { useNowTick } from "@/hooks/useNowTick";

interface TargetRoutingScheduleBadgeProps {
  schedule: RoutingSchedule | null;
  state: RoutingScheduleState | null;
}

/**
 * The routing-window state of one Terminal Target.
 *
 * Every state it can show comes from the server. The client never evaluates a
 * window itself: window arithmetic involves IANA zones and DST, and a second
 * implementation would drift from the one routing actually uses. The only
 * client-side judgement is whether the server's answer has passed its own
 * stated boundary, which is a comparison, not a recomputation.
 */
export function TargetRoutingScheduleBadge({ schedule, state }: TargetRoutingScheduleBadgeProps) {
  const { messages } = useLocale();
  const copy = messages.modelsUi;
  const nowMs = useNowTick();

  // No schedule means no restriction, which is the pre-feature behaviour and
  // deserves no badge at all rather than a badge saying "always".
  if (!schedule) return null;

  // Configuration present but no evaluated state: the read that should have
  // carried it did not. Saying "open" or "closed" here would be a guess.
  if (!state) {
    return <OperatorTypeBadge intent="degraded" label={copy.routingScheduleStateUnavailable} preserveLabel />;
  }

  const boundary =
    state.status === "open"
      ? state.next_close_at_known
        ? state.next_close_at
        : undefined
      : state.status === "closed"
        ? state.next_open_at_known
          ? state.next_open_at
          : undefined
        : undefined;
  if (boundary) {
    const boundaryMs = Date.parse(boundary);
    if (!Number.isNaN(boundaryMs) && nowMs >= boundaryMs) {
      return <OperatorStalenessBadge label={copy.routingScheduleStale} reason={copy.routingScheduleStaleReason} />;
    }
  }

  switch (state.status) {
    case "open":
      return <OperatorTypeBadge intent="healthy" label={copy.routingScheduleOpen} preserveLabel />;
    case "closed":
      return (
        <OperatorTypeBadge
          intent="degraded"
          // The reopen instant is only appended when the server said it knows
          // it; a schedule whose next opening could not be computed must not
          // be rendered as if it had one.
          label={
            state.next_open_at_known && state.next_open_at
              ? copy.routingScheduleClosedUntil(state.next_open_at)
              : copy.routingScheduleClosed
          }
          preserveLabel
        />
      );
    case "unresolved":
      return <OperatorTypeBadge intent="danger" label={copy.routingScheduleUnresolved} preserveLabel />;
    case "not_evaluated":
      return (
        <OperatorTypeBadge
          intent="degraded"
          label={
            state.not_evaluated_reason === "connection_inactive"
              ? copy.routingScheduleNotEvaluatedInactive
              : copy.routingScheduleNotEvaluated
          }
          preserveLabel
        />
      );
    default:
      // Named fallback rather than echoing the raw status: an unknown enum
      // value must not reach the operator as a bare key.
      return <OperatorTypeBadge intent="degraded" label={copy.routingScheduleNotEvaluated} preserveLabel />;
  }
}

import { useLocale } from "@/i18n/useLocale";
import type {
  LoadbalanceEventType,
  LoadbalanceFailureKind,
} from "@/lib/types/loadbalance";
import { OperatorTypeBadge, type OperatorBadgeIntent } from "@/shared/design-system";

interface EventTypeBadgeProps {
  eventType: LoadbalanceEventType;
  className?: string;
}

export function EventTypeBadge({ eventType, className }: EventTypeBadgeProps) {
  const { messages } = useLocale();
  const eventTypeConfig = {
    retry_scheduled: {
      label: messages.loadbalanceEvents.eventTypeExtended,
      intent: "warning" satisfies OperatorBadgeIntent,
    },
    retry_exhausted: {
      label: messages.loadbalanceEvents.eventTypeMaxCooldownStrike,
      intent: "warning" satisfies OperatorBadgeIntent,
    },
    banned: {
      label: messages.loadbalanceEvents.eventTypeBanned,
      intent: "danger" satisfies OperatorBadgeIntent,
    },
    unbanned: {
      label: messages.loadbalanceEvents.eventTypeProbeEligible,
      intent: "info" satisfies OperatorBadgeIntent,
    },
    recovered: {
      label: messages.loadbalanceEvents.eventTypeRecovered,
      intent: "success" satisfies OperatorBadgeIntent,
    },
    admission_rejected: {
      label: messages.loadbalanceEvents.eventTypeNotOpened,
      intent: "muted" satisfies OperatorBadgeIntent,
    },
  } as const;
  const config = eventTypeConfig[eventType];
  return <OperatorTypeBadge className={className} intent={config.intent} label={config.label} preserveLabel />;
}

interface FailureKindBadgeProps {
  failureKind: LoadbalanceFailureKind | null;
  className?: string;
}

export function FailureKindBadge({ failureKind, className }: FailureKindBadgeProps) {
  const { messages } = useLocale();

  if (!failureKind) {
    return <OperatorTypeBadge className={className} intent="muted" label={messages.common.notApplicable} preserveLabel />;
  }

  const failureKindConfig = {
    transient_http: {
      label: messages.loadbalanceEvents.failureKindTransientHttp,
      intent: "warning" satisfies OperatorBadgeIntent,
    },
    connect_error: {
      label: messages.loadbalanceEvents.failureKindConnectError,
      intent: "danger" satisfies OperatorBadgeIntent,
    },
    timeout: {
      label: messages.loadbalanceEvents.failureKindTimeout,
      intent: "warning" satisfies OperatorBadgeIntent,
    },
  } as const;
  const config = failureKindConfig[failureKind];

  return <OperatorTypeBadge className={className} intent={config.intent} label={config.label} preserveLabel />;
}

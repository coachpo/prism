import { Badge } from "@/components/ui/badge";
import { useLocale } from "@/i18n/useLocale";
import type {
  LoadbalanceEventType,
  LoadbalanceFailureKind,
} from "@/lib/types/loadbalance";
import { cn } from "@/lib/utils";

const INTENT_CLASSES = {
  success: "bg-emerald-500/10 text-emerald-700 border-emerald-500/25 dark:text-emerald-400",
  warning: "bg-amber-500/10 text-amber-700 border-amber-500/25 dark:text-amber-400",
  danger: "bg-red-500/10 text-red-700 border-red-500/25 dark:text-red-400",
  info: "bg-sky-500/10 text-sky-700 border-sky-500/25 dark:text-sky-400",
  muted: "bg-muted text-muted-foreground",
} as const;

interface EventTypeBadgeProps {
  eventType: LoadbalanceEventType;
  className?: string;
}

export function EventTypeBadge({ eventType, className }: EventTypeBadgeProps) {
  const { messages } = useLocale();
  const eventTypeConfig = {
    opened: {
      label: messages.loadbalanceEvents.eventTypeOpened,
      intent: "danger" as const,
    },
    extended: {
      label: messages.loadbalanceEvents.eventTypeExtended,
      intent: "warning" as const,
    },
    max_cooldown_strike: {
      label: messages.loadbalanceEvents.eventTypeMaxCooldownStrike,
      intent: "warning" as const,
    },
    banned: {
      label: messages.loadbalanceEvents.eventTypeBanned,
      intent: "danger" as const,
    },
    recovered: {
      label: messages.loadbalanceEvents.eventTypeRecovered,
      intent: "success" as const,
    },
    not_opened: {
      label: messages.loadbalanceEvents.eventTypeNotOpened,
      intent: "muted" as const,
    },
  } as const;
  const config = eventTypeConfig[eventType];
  return (
    <Badge
      variant="outline"
      className={cn(
        "text-[10px] shrink-0",
        INTENT_CLASSES[config.intent],
        className
      )}
    >
      {config.label}
    </Badge>
  );
}

interface FailureKindBadgeProps {
  failureKind: LoadbalanceFailureKind | null;
  className?: string;
}

export function FailureKindBadge({ failureKind, className }: FailureKindBadgeProps) {
  const { messages } = useLocale();

  if (!failureKind) {
    return (
      <Badge
        variant="outline"
        className={cn("text-[10px] shrink-0", INTENT_CLASSES.muted, className)}
      >
        {messages.common.notApplicable}
      </Badge>
    );
  }

  const failureKindConfig = {
    transient_http: {
      label: messages.loadbalanceEvents.failureKindTransientHttp,
      intent: "warning" as const,
    },
    connect_error: {
      label: messages.loadbalanceEvents.failureKindConnectError,
      intent: "danger" as const,
    },
    timeout: {
      label: messages.loadbalanceEvents.failureKindTimeout,
      intent: "warning" as const,
    },
  } as const;
  const config = failureKindConfig[failureKind];

  return (
    <Badge
      variant="outline"
      className={cn(
        "text-[10px] shrink-0",
        INTENT_CLASSES[config.intent],
        className
      )}
    >
      {config.label}
    </Badge>
  );
}

import { StatusBadge, ValueBadge } from "@/components/StatusBadge";
import { StatusDot, type StatusDotIntent } from "@/components/ui/status-dot";
import { useLocale } from "@/i18n/useLocale";
import type { Connection } from "@/lib/types";
import { cn } from "@/lib/utils";
import { getPriorityBadgeClasses } from "./ConnectionCardSectionsShared";

export function ConnectionCardHeader({
  connection,
  connectionName,
  isChecking,
}: {
  connection: Connection;
  connectionName: string;
  isChecking: boolean;
}) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const healthIntent: StatusDotIntent = isChecking
    ? "primary"
    : connection.health_status === "healthy"
      ? "healthy"
    : connection.health_status === "unhealthy"
        ? "unhealthy"
        : "muted";
  const healthAnimated = isChecking || connection.health_status !== "unknown";

  return (
    <div className="flex flex-wrap items-center gap-2">
      <StatusDot intent={healthIntent} animated={healthAnimated} aria-hidden="true" />
      <ValueBadge
        label={`P${connection.priority}`}
        intent="default"
        className={cn("tabular-nums", getPriorityBadgeClasses(connection.priority))}
      />
      <span className="truncate text-sm font-medium">{connectionName}</span>
      <StatusBadge
        label={connection.pricing_template ? copy.pricingOn : copy.pricingOff}
        intent={connection.pricing_template ? "success" : "muted"}
      />
      {connection.pricing_template ? (
        <div className="flex items-center gap-1">
          <ValueBadge
            label={`${connection.pricing_template.name} v${connection.pricing_template.version}`}
            intent="info"
          />
          <ValueBadge label={connection.pricing_template.pricing_currency_code} intent="accent" />
        </div>
      ) : null}
      {!connection.is_active ? <StatusBadge label={copy.inactive} intent="muted" /> : null}
    </div>
  );
}

import {
  OperatorStatusBadge,
  OperatorTypeBadge,
  OperatorValueBadge,
  type OperatorBadgeIntent,
} from "@/shared/design-system";

export type BadgeIntent = OperatorBadgeIntent;
interface BaseBadgeProps {
  label: string;
  intent?: BadgeIntent;
  className?: string;
  preserveLabel?: boolean;
}

/** Boolean state indicators: On/Off, Enabled/Disabled, Active/Inactive */
/** @deprecated Use OperatorStatusBadge from "@/shared/design-system" for new surfaces. */
export function StatusBadge({ label, intent = "default", className }: BaseBadgeProps) {
  return <OperatorStatusBadge label={label} intent={intent} className={className} />;
}

/** Category/classification labels: Model/Connection, Exact/Prefix, Stream */
/** @deprecated Use OperatorTypeBadge from "@/shared/design-system" for new surfaces. */
export function TypeBadge({ label, intent = "default", className, preserveLabel = false }: BaseBadgeProps) {
  return <OperatorTypeBadge label={label} intent={intent} className={className} preserveLabel={preserveLabel} />;
}

/** Raw data values displayed as-is: HTTP codes, percentages, priorities, methods */
/** @deprecated Use OperatorValueBadge from "@/shared/design-system" for new surfaces. */
export function ValueBadge({ label, intent = "default", className }: BaseBadgeProps) {
  return <OperatorValueBadge label={label} intent={intent} className={className} />;
}

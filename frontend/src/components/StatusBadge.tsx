import { Badge } from "@/components/ui/badge";
import { cn, formatLabel } from "@/lib/utils";

const INTENT_CLASSES = {
  success: "border-success/25 bg-success/10 text-success",
  warning: "border-warning/30 bg-warning/10 text-warning",
  danger: "border-destructive/30 bg-destructive/10 text-destructive",
  info: "border-info/25 bg-info/10 text-info",
  accent: "border-primary/25 bg-primary/10 text-primary",
  blue: "border-info/25 bg-info/10 text-info",
  muted: "border-border/70 bg-muted text-muted-foreground",
  default: "",
} as const;

export type BadgeIntent = keyof typeof INTENT_CLASSES;
interface BaseBadgeProps {
  label: string;
  intent?: BadgeIntent;
  className?: string;
  preserveLabel?: boolean;
}

/** Boolean state indicators: On/Off, Enabled/Disabled, Active/Inactive */
export function StatusBadge({ label, intent = "default", className }: BaseBadgeProps) {
  return (
    <Badge
      variant="outline"
      className={cn("text-[10px] shrink-0", INTENT_CLASSES[intent], className)}
    >
      {formatLabel(label)}
    </Badge>
  );
}

/** Category/classification labels: Model/Connection, Exact/Prefix, Stream */
export function TypeBadge({ label, intent = "default", className, preserveLabel = false }: BaseBadgeProps) {
  return (
    <Badge
      variant="outline"
      className={cn("text-[10px] shrink-0", INTENT_CLASSES[intent], className)}
    >
      {preserveLabel ? label : formatLabel(label)}
    </Badge>
  );
}

/** Raw data values displayed as-is: HTTP codes, percentages, priorities, methods */
export function ValueBadge({ label, intent = "default", className }: BaseBadgeProps) {
  return (
    <Badge
      variant="outline"
      className={cn("text-[10px] shrink-0 font-mono", INTENT_CLASSES[intent], className)}
    >
      {label}
    </Badge>
  );
}

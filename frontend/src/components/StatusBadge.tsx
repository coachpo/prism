import { Badge } from "@/components/ui/badge";
import { cn, formatLabel } from "@/lib/utils";

const INTENT_CLASSES = {
  success: "bg-emerald-500/10 text-emerald-700 border-emerald-500/25 dark:text-emerald-400",
  warning: "bg-amber-500/10 text-amber-700 border-amber-500/25 dark:text-amber-400",
  danger: "bg-red-500/10 text-red-700 border-red-500/25 dark:text-red-400",
  info: "bg-sky-500/10 text-sky-700 border-sky-500/25 dark:text-sky-400",
  accent: "bg-violet-500/10 text-violet-700 border-violet-500/25 dark:text-violet-400",
  blue: "bg-blue-500/10 text-blue-700 border-blue-500/25 dark:text-blue-400",
  muted: "bg-muted text-muted-foreground",
  default: "",
} as const;

export type BadgeIntent = keyof typeof INTENT_CLASSES;
interface BaseBadgeProps {
  label: string;
  intent?: BadgeIntent;
  className?: string;
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

/** Category/classification labels: Proxy/Native, Exact/Prefix, Stream */
export function TypeBadge({ label, intent = "default", className }: BaseBadgeProps) {
  return (
    <Badge
      variant="outline"
      className={cn("text-[10px] shrink-0", INTENT_CLASSES[intent], className)}
    >
      {formatLabel(label)}
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

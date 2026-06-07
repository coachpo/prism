import type { ComponentProps, ReactNode } from "react";
import { AlertCircle, CheckCircle2, Info, TriangleAlert } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

type SemanticCalloutIntent = "info" | "success" | "warning" | "danger" | "muted";
type SemanticCalloutRole = ComponentProps<typeof Alert>["role"];

const CALLOUT_TONES: Record<SemanticCalloutIntent, string> = {
  info: "border-info/25 bg-info/10 text-info [&_[data-slot=alert-description]]:text-info/90",
  success: "border-success/25 bg-success/10 text-success [&_[data-slot=alert-description]]:text-success/90",
  warning: "border-warning/30 bg-warning/10 text-warning [&_[data-slot=alert-description]]:text-warning/90",
  danger: "border-destructive/30 bg-destructive/10 text-destructive [&_[data-slot=alert-description]]:text-destructive/90",
  muted: "border-border/70 bg-muted/35 text-foreground [&_[data-slot=alert-description]]:text-muted-foreground",
};

const DEFAULT_ICONS = {
  info: Info,
  success: CheckCircle2,
  warning: TriangleAlert,
  danger: AlertCircle,
  muted: Info,
} as const;
interface SemanticCalloutProps {
  intent?: SemanticCalloutIntent;
  title?: ReactNode;
  children?: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  icon?: ReactNode;
  className?: string;
  role?: SemanticCalloutRole;
}

export function SemanticCallout({
  intent = "info",
  title,
  children,
  description,
  action,
  icon,
  className,
  role,
}: SemanticCalloutProps) {
  const Icon = DEFAULT_ICONS[intent];
  const content = description ?? children;
  const calloutRole = role ?? (intent === "danger" ? "alert" : "note");

  return (
    <Alert role={calloutRole} className={cn("items-center", CALLOUT_TONES[intent], className)}>
      {icon ?? <Icon />}
      <div className="col-start-2 flex min-w-0 flex-col gap-1 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
        <div className="min-w-0">
          {title ? <AlertTitle>{title}</AlertTitle> : null}
          {content ? <AlertDescription>{content}</AlertDescription> : null}
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>
    </Alert>
  );
}

export type { SemanticCalloutIntent };

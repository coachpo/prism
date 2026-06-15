import type { ComponentProps, ReactNode } from "react";

import { Alert } from "@/components/ui/alert";
import { OperatorCallout } from "@/shared/design-system";

type SemanticCalloutIntent = "info" | "success" | "warning" | "danger" | "muted";
type SemanticCalloutRole = ComponentProps<typeof Alert>["role"];

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

/** @deprecated Use OperatorCallout from "@/shared/design-system" for new surfaces. */
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
  return (
    <OperatorCallout
      action={action}
      className={className}
      description={description}
      icon={icon}
      intent={intent}
      role={role}
      title={title}
    >
      {children}
    </OperatorCallout>
  );
}

export type { SemanticCalloutIntent };

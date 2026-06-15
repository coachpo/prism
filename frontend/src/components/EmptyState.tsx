import type { ReactNode } from "react";
import { FileX } from "lucide-react";

import { OperatorEmptyState } from "@/shared/design-system";

interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
  testId?: string;
}

/** @deprecated Use OperatorEmptyState from "@/shared/design-system" for new surfaces. */
export function EmptyState({ icon, title, description, action, className, testId }: EmptyStateProps) {
  return (
    <OperatorEmptyState
      action={action}
      className={className}
      description={description}
      icon={icon ?? <FileX />}
      testId={testId}
      title={title}
    />
  );
}

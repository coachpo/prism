import type { ReactNode } from "react";
import { OperatorPageHeader } from "@/shared/design-system";

interface PageHeaderProps {
  title: string;
  description?: string;
  children?: ReactNode;
  className?: string;
}

/** @deprecated Use OperatorPageHeader from "@/shared/design-system" for new surfaces. */
export function PageHeader({ title, description, children, className }: PageHeaderProps) {
  return <OperatorPageHeader title={title} description={description} className={className}>{children}</OperatorPageHeader>;
}

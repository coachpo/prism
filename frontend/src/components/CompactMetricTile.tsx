import type { ReactNode } from "react";
import { OperatorMetricTile } from "@/shared/design-system";

interface CompactMetricTileProps {
  className?: string;
  detail?: ReactNode;
  label: ReactNode;
  value: ReactNode;
  valueClassName?: string;
}

/** @deprecated Use OperatorMetricTile from "@/shared/design-system" for new surfaces. */
export function CompactMetricTile({
  className,
  detail,
  label,
  value,
  valueClassName,
}: CompactMetricTileProps) {
  return <OperatorMetricTile className={className} detail={detail} label={label} value={value} valueClassName={valueClassName} />;
}

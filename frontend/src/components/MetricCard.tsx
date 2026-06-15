import type { ReactNode } from "react";
import { OperatorMetricCard } from "@/shared/design-system";

interface MetricCardProps {
  label: ReactNode;
  value: ReactNode;
  detail?: ReactNode;
  icon?: ReactNode;
  trend?: { value: string; positive?: boolean };
  className?: string;
  onClick?: () => void;
}

/** @deprecated Use OperatorMetricCard from "@/shared/design-system" for new surfaces. */
export function MetricCard({ label, value, detail, icon, trend, className, onClick }: MetricCardProps) {
  return <OperatorMetricCard label={label} value={value} detail={detail} icon={icon} trend={trend} className={className} onClick={onClick} />;
}

import type { LucideIcon } from "lucide-react";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { OperatorMetricTile } from "@/shared/design-system";

export function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1.5 py-1.5 text-sm sm:grid-cols-[minmax(7rem,0.38fr)_minmax(0,1fr)] sm:gap-3">
      <span className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">{label}</span>
      <div className="min-w-0 text-sm font-medium text-foreground">{children}</div>
    </div>
  );
}

export function SummaryStat({
  label,
  value,
  valueClassName,
}: {
  label: string;
  value: React.ReactNode;
  valueClassName?: string;
}) {
  return (
    <OperatorMetricTile
      className="border-border bg-panel [&_[data-slot=metric-label]]:text-[11px] [&_[data-slot=metric-label]]:uppercase [&_[data-slot=metric-label]]:tracking-[0.18em] [&_[data-slot=metric-value]]:text-sm"
      label={label}
      value={value}
      valueClassName={valueClassName}
    />
  );
}

export function SectionCard({
  icon: Icon,
  title,
  children,
}: {
  icon: LucideIcon;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Card className="border-border">
      <CardHeader className="px-3 py-2.5">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold tracking-tight">
          <Icon className="size-4 text-muted-foreground" />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="px-3 pb-3 pt-0">{children}</CardContent>
    </Card>
  );
}

export function ApiFamilyPill({ apiFamily }: { apiFamily: string | null | undefined }) {
  if (!apiFamily) {
    return null;
  }

  return (
    <Badge variant="outline" className="gap-1.5 border-border bg-panel text-[10px] font-medium">
      <ApiFamilyIcon apiFamily={apiFamily} size={12} />
      {formatApiFamily(apiFamily)}
    </Badge>
  );
}

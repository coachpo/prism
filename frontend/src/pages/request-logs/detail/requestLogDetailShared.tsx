import type { LucideIcon } from "lucide-react";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { CompactMetricTile } from "@/components/CompactMetricTile";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatApiFamily } from "@/lib/utils";

export function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[92px_minmax(0,1fr)] items-start gap-2.5 py-1 text-sm">
      <span className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">{label}</span>
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
    <CompactMetricTile
      className="border-border/60 bg-background/80 [&_[data-slot=metric-label]]:text-[11px] [&_[data-slot=metric-label]]:uppercase [&_[data-slot=metric-label]]:tracking-[0.18em] [&_[data-slot=metric-value]]:text-sm"
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
    <Card className="border-border/70 shadow-sm">
      <CardHeader className="space-y-0 px-3 py-2">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold tracking-tight">
          <Icon className="h-4 w-4 text-muted-foreground" />
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
    <Badge variant="outline" className="gap-1.5 border-border/70 bg-background/80 text-[10px] font-medium">
      <ApiFamilyIcon apiFamily={apiFamily} size={12} />
      {formatApiFamily(apiFamily)}
    </Badge>
  );
}

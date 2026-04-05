import { CompactMetricTile } from "@/components/CompactMetricTile";

export function ConnectionCardMetricTile({ label, value }: { label: string; value: string }) {
  return (
    <CompactMetricTile
      className="border-border/60 bg-background/70 [&_[data-slot=metric-label]]:text-[11px] [&_[data-slot=metric-value]]:text-sm"
      label={label}
      value={value}
    />
  );
}

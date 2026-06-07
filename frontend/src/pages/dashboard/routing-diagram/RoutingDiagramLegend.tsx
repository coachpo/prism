import { useLocale } from "@/i18n/useLocale";

export function RoutingDiagramLegend() {
  const { messages } = useLocale();

  return (
    <div
      data-testid="routing-diagram-legend"
      role="list"
      aria-label={messages.dashboard.routingTitle}
      className="mb-4 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground"
    >
      <LegendPill label={messages.dashboard.routingModelNodeType} color="var(--chart-1)" />
      <LegendPill label={messages.modelDetail.connections} color="var(--chart-4)" />
      <LegendPill label={messages.dashboard.routingEndpointNodeType} color="var(--chart-2)" />
      <LegendPill label={messages.modelDetail.disabled} color="var(--muted-foreground)" muted />
      <LegendPill label={messages.modelDetail.inactive} color="var(--muted-foreground)" muted />
    </div>
  );
}

function LegendPill({
  color,
  label,
  muted = false,
}: {
  color: string;
  label: string;
  muted?: boolean;
}) {
  return (
    <span
      role="listitem"
      aria-label={label}
      className="inline-flex items-center gap-2 rounded-full border bg-background/80 px-2.5 py-1"
    >
      <span
        className="h-2.5 w-2.5 rounded-full border"
        style={{
          backgroundColor: color,
          borderColor: muted ? "var(--border)" : "transparent",
          opacity: muted ? 0.45 : 0.9,
        }}
        aria-hidden="true"
      />
      <span className="font-medium text-foreground">{label}</span>
    </span>
  );
}

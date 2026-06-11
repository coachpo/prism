import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import { getRoutingDiagramNodeVisualMetadata } from "./routingDiagramPresentationUtils";

export function RoutingDiagramLegend() {
  const { messages } = useLocale();

  return (
    <div
      data-testid="routing-diagram-legend"
      role="list"
      aria-label={messages.dashboard.routingTitle}
      className="mb-4 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground"
    >
      <LegendPill label={messages.dashboard.routingModelNodeType} nodeVisual={getRoutingDiagramNodeVisualMetadata("model")} />
      <LegendPill label={messages.modelDetail.connections} nodeVisual={getRoutingDiagramNodeVisualMetadata("terminal_target")} />
      <LegendPill label={messages.dashboard.routingEndpointNodeType} nodeVisual={getRoutingDiagramNodeVisualMetadata("endpoint")} />
      <LegendPill label={messages.modelDetail.disabled} color="var(--muted-foreground)" muted />
      <LegendPill label={messages.modelDetail.inactive} color="var(--muted-foreground)" muted />
    </div>
  );
}

function LegendPill({
  color,
  label,
  muted = false,
  nodeVisual,
}: {
  color?: string;
  label: string;
  muted?: boolean;
  nodeVisual?: ReturnType<typeof getRoutingDiagramNodeVisualMetadata>;
}) {
  return (
    <span
      role="listitem"
      aria-label={label}
      className="inline-flex items-center gap-2 rounded-full border bg-background/80 px-2.5 py-1"
    >
      <span
        className={cn("h-2.5 w-2.5 border", nodeVisual?.markerClassName ?? "rounded-full")}
        data-node-shape={nodeVisual?.shape}
        style={{
          backgroundColor: nodeVisual?.color ?? color,
          borderColor: muted ? "var(--border)" : "transparent",
          clipPath: nodeVisual?.markerClipPath,
          opacity: muted ? 0.45 : 0.9,
        }}
        aria-hidden="true"
      />
      <span className="font-medium text-foreground">{label}</span>
    </span>
  );
}

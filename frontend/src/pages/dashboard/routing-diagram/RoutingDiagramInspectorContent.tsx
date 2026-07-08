import {
  formatSuccessRate,
  getRouteHealthState,
} from "./routingDiagramPresentationUtils";
import type {
  RoutingDiagramGraphEdge,
  RoutingDiagramGraphNode,
} from "../routingDiagram";
import { useLocale } from "@/i18n/useLocale";

interface RoutingDiagramInspectorContentProps {
  node?: RoutingDiagramGraphNode | null;
  edge?: RoutingDiagramGraphEdge | null;
}

export function RoutingDiagramInspectorContent({
  node,
  edge,
}: RoutingDiagramInspectorContentProps) {
  const { formatNumber, messages } = useLocale();

  if (node) {
    return (
      <div className="min-w-[14rem] rounded-xl border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-xl">
        <p className="text-sm font-semibold">{node.label}</p>
        {node.sublabel ? <p className="mt-1 text-muted-foreground">{node.sublabel}</p> : null}
        <div className="mt-3 flex flex-col gap-1.5">
          <TooltipRow label={messages.dashboard.routingNodeType} value={getNodeTypeLabel(node.kind, messages)} />
          <TooltipRow label={messages.dashboard.routingActiveTerminalTargets} value={formatNumber(node.activeTerminalTargetCount)} />
          <TooltipRow
            label={messages.dashboard.routing24hSuccessRate}
            value={
              formatSuccessRate(node.successRate24h, node.requestCount24h) ??
              messages.dashboard.routingLegendNoData
            }
          />
          <TooltipRow label={messages.dashboard.routing24hTotalRequests} value={formatNumber(node.requestCount24h)} />
          {node.kind === "model" ? (
            <TooltipRow label={messages.requestLogs.view} value={messages.dashboard.routingActionOpenModelDetail} />
          ) : null}
          {node.kind === "endpoint" ? (
            <TooltipRow label={messages.requestLogs.view} value={messages.dashboard.reviewRequests} />
          ) : null}
        </div>
      </div>
    );
  }

  if (edge) {
    const routeHealthState = getRouteHealthState(
      edge.successRate24h,
      edge.requestCount24h,
    );
    const routeHealthLabel =
      routeHealthState === "healthy"
        ? messages.dashboard.routingLegendHealthy
        : routeHealthState === "degraded"
          ? messages.dashboard.routingLegendDegraded
          : routeHealthState === "failing"
            ? messages.dashboard.routingLegendFailing
            : messages.dashboard.routingLegendNoData;
    const formattedSuccessRate =
      formatSuccessRate(edge.successRate24h, edge.requestCount24h) ??
      messages.dashboard.routingLegendNoData;

    return (
      <div className="min-w-[16rem] rounded-xl border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-xl">
        <p className="text-sm font-semibold">{edge.sourceLabel} → {edge.targetLabel}</p>
        <div className="mt-3 flex flex-col gap-1.5">
          <TooltipRow label={messages.dashboard.routing24hStatus} value={routeHealthLabel} />
          <TooltipRow label={messages.dashboard.routing24hSuccessRate} value={formattedSuccessRate} />
          <TooltipRow label={messages.dashboard.routing24hTotalRequests} value={formatNumber(edge.requestCount24h)} />
          <TooltipRow label={messages.dashboard.routingActiveTerminalTargets} value={formatNumber(edge.activeTerminalTargetCount)} />
          <TooltipRow label={messages.dashboard.routing24hSuccessfulRequests} value={formatNumber(edge.successCount24h)} />
          <TooltipRow label={messages.dashboard.routing24hErrors} value={formatNumber(edge.errorCount24h)} />
        </div>
      </div>
    );
  }

  return null;
}

function getNodeTypeLabel(
  kind: "endpoint" | "model" | "terminal_target",
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (kind === "endpoint") {
    return messages.dashboard.routingEndpointNodeType;
  }

  if (kind === "terminal_target") {
    return messages.modelDetail.connections;
  }

  return messages.dashboard.routingModelNodeType;
}

function TooltipRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right font-medium">{value}</span>
    </div>
  );
}

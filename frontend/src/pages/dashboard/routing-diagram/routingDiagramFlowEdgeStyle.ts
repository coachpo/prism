import type { RoutingDiagramFlowEdge as RoutingDiagramFlowLayoutEdge } from "../routingDiagram";

import { getRouteHealthColor } from "./routingDiagramPresentationUtils";

export interface RoutingDiagramFlowEdgeStyle {
  stroke: string;
  strokeOpacity: number;
  strokeWidth: number;
}

export function getRoutingDiagramFlowEdgeStyle(
  edge: RoutingDiagramFlowLayoutEdge["data"] | undefined,
): RoutingDiagramFlowEdgeStyle {
  const requestCount24h = edge?.requestCount24h ?? 0;
  const activeTerminalTargetCount = edge?.activeTerminalTargetCount ?? 0;

  return {
    stroke: getRouteHealthColor(edge?.successRate24h ?? null, requestCount24h),
    strokeOpacity: requestCount24h > 0 ? 0.38 : 0.24,
    strokeWidth: Math.min(6, Math.max(1.5, 1 + activeTerminalTargetCount)),
  };
}

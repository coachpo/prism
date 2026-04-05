import type { RoutingDiagramData } from "./routingDiagram";

export function getEmptyRoutingDiagramData(): RoutingDiagramData {
  return {
    nodes: [],
    links: [],
    endpointCount: 0,
    modelCount: 0,
    activeConnectionTotal: 0,
    trafficRequestTotal24h: 0,
  };
}

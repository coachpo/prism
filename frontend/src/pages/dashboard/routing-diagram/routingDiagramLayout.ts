import type {
  RoutingDiagramChartLink,
  RoutingDiagramChartNode,
  RoutingDiagramData,
  RoutingDiagramLink,
} from "./routingDiagramContracts";
import { compareStringsForLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

export function getRoutingDiagramChartData(
  data: RoutingDiagramData,
): { nodes: RoutingDiagramChartNode[]; links: RoutingDiagramChartLink[] } {
  const filteredLinks = data.links
    .filter((link) => link.activeConnectionCount > 0)
    .sort(compareLinksByPriority);

  if (filteredLinks.length === 0) {
    return { nodes: [], links: [] };
  }

  const nodeIds = new Set<string>();
  for (const link of filteredLinks) {
    nodeIds.add(link.sourceNodeId);
    nodeIds.add(link.targetNodeId);
  }

  const nodes = data.nodes
    .filter((node) => nodeIds.has(node.id))
    .map<RoutingDiagramChartNode>((node) => ({
      ...node,
      value: Math.max(node.activeConnectionCount, 1),
    }));

  const nodeIndex = new Map(nodes.map((node, index) => [node.id, index]));

  return {
    nodes,
    links: filteredLinks.map<RoutingDiagramChartLink>((link) => ({
      ...link,
      source: nodeIndex.get(link.sourceNodeId) ?? 0,
      target: nodeIndex.get(link.targetNodeId) ?? 0,
      value: Math.max(link.activeConnectionCount, 1),
    })),
  };
}

export function getRoutingDiagramEmptyState(
  data: RoutingDiagramData,
): { kind: "no_active_routes" | "no_recent_traffic"; title: string; description: string } {
  const copy = getStaticMessages().dashboard;
  if (data.links.length === 0) {
    return {
      kind: "no_active_routes",
      title: copy.routingNoActiveRoutes,
      description: copy.routingNoActiveRoutesDescription,
    };
  }

  return {
    kind: "no_recent_traffic",
    title: copy.routingNoRecentTraffic,
    description: copy.routingNoRecentTrafficDescription,
  };
}

function compareLinksByPriority(left: RoutingDiagramLink, right: RoutingDiagramLink): number {
  if (right.activeConnectionCount !== left.activeConnectionCount) {
    return right.activeConnectionCount - left.activeConnectionCount;
  }

  if (right.trafficRequestCount24h !== left.trafficRequestCount24h) {
    return right.trafficRequestCount24h - left.trafficRequestCount24h;
  }

  return compareStringsForLocale(left.endpointLabel, right.endpointLabel);
}

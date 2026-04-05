import type { DashboardRouteSnapshot } from "@/lib/types";
import { compareStringsForLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  RoutingDiagramData,
  RoutingDiagramLink,
  RoutingDiagramNode,
} from "./routingDiagramContracts";

interface DiagramTotals {
  activeConnectionCount: number;
  errorCount24h: number;
  label: string;
  requestCount24h: number;
  successCount24h: number;
  trafficRequestCount24h: number;
}

interface ModelTotals extends DiagramTotals {
  modelId: string;
}

export function applyRoutingDiagramRealtimeUpdate(
  currentData: RoutingDiagramData | null,
  routeSnapshot: DashboardRouteSnapshot | null,
): RoutingDiagramData | null {
  if (!routeSnapshot) {
    return currentData;
  }

  const routeId = buildRouteId(routeSnapshot.model_id, routeSnapshot.endpoint_id);
  const existingLink = currentData?.links.find((link) => link.id === routeId) ?? null;
  const modelConfigId = routeSnapshot.model_config_id ?? existingLink?.modelConfigId ?? null;

  if (modelConfigId === null) {
    return currentData;
  }

  const nextLink: RoutingDiagramLink = {
    id: routeId,
    sourceNodeId: buildEndpointNodeId(routeSnapshot.endpoint_id),
    targetNodeId: buildModelNodeId(modelConfigId),
    modelId: routeSnapshot.model_id,
    modelLabel: routeSnapshot.model_label,
    modelConfigId,
    endpointId: routeSnapshot.endpoint_id,
    endpointLabel: routeSnapshot.endpoint_label,
    activeConnectionCount: routeSnapshot.active_connection_count,
    trafficRequestCount24h: routeSnapshot.traffic_request_count_24h,
    requestCount24h: routeSnapshot.request_count_24h,
    successCount24h: routeSnapshot.success_count_24h,
    errorCount24h: routeSnapshot.error_count_24h,
    successRate24h: routeSnapshot.success_rate_24h,
  };

  const links = [...(currentData?.links ?? [])];
  const existingIndex = links.findIndex((link) => link.id === routeId);

  if (existingIndex >= 0) {
    links[existingIndex] = nextLink;
  } else {
    links.push(nextLink);
  }

  return rebuildRoutingDiagramData(links);
}

function rebuildRoutingDiagramData(links: RoutingDiagramLink[]): RoutingDiagramData {
  const sortedLinks = [...links]
    .filter((link) => shouldKeepLink(link))
    .sort(compareLinksByPriority);

  const endpointTotals = new Map<number, DiagramTotals>();
  const modelTotals = new Map<number, ModelTotals>();

  for (const link of sortedLinks) {
    const endpointTotal = endpointTotals.get(link.endpointId) ?? {
      activeConnectionCount: 0,
      errorCount24h: 0,
      label: link.endpointLabel,
      requestCount24h: 0,
      successCount24h: 0,
      trafficRequestCount24h: 0,
    };

    endpointTotal.activeConnectionCount += link.activeConnectionCount;
    endpointTotal.errorCount24h += link.errorCount24h;
    endpointTotal.requestCount24h += link.requestCount24h;
    endpointTotal.successCount24h += link.successCount24h;
    endpointTotal.trafficRequestCount24h += link.trafficRequestCount24h;
    endpointTotals.set(link.endpointId, endpointTotal);

    const modelTotal = modelTotals.get(link.modelConfigId) ?? {
      activeConnectionCount: 0,
      errorCount24h: 0,
      label: link.modelLabel,
      modelId: link.modelId,
      requestCount24h: 0,
      successCount24h: 0,
      trafficRequestCount24h: 0,
    };

    modelTotal.activeConnectionCount += link.activeConnectionCount;
    modelTotal.errorCount24h += link.errorCount24h;
    modelTotal.requestCount24h += link.requestCount24h;
    modelTotal.successCount24h += link.successCount24h;
    modelTotal.trafficRequestCount24h += link.trafficRequestCount24h;
    modelTotals.set(link.modelConfigId, modelTotal);
  }

  const endpointNodes = [...endpointTotals.entries()]
    .sort(compareTotalsByPriority)
    .map<RoutingDiagramNode>(([endpointId, totals]) => ({
      id: buildEndpointNodeId(endpointId),
      name: totals.label,
      kind: "endpoint",
      label: totals.label,
      sublabel: getStaticMessages().common.endpointWithId(String(endpointId)),
      endpointId,
      modelId: null,
      modelConfigId: null,
      activeConnectionCount: totals.activeConnectionCount,
      trafficRequestCount24h: totals.trafficRequestCount24h,
      requestCount24h: totals.requestCount24h,
      successCount24h: totals.successCount24h,
      errorCount24h: totals.errorCount24h,
      successRate24h: calculateSuccessRate(totals.successCount24h, totals.requestCount24h),
    }));

  const modelNodes = [...modelTotals.entries()]
    .sort(compareTotalsByPriority)
    .map<RoutingDiagramNode>(([modelConfigId, totals]) => ({
      id: buildModelNodeId(modelConfigId),
      name: totals.label,
      kind: "model",
      label: totals.label,
      sublabel: totals.label === totals.modelId ? null : totals.modelId,
      endpointId: null,
      modelId: totals.modelId,
      modelConfigId,
      activeConnectionCount: totals.activeConnectionCount,
      trafficRequestCount24h: totals.trafficRequestCount24h,
      requestCount24h: totals.requestCount24h,
      successCount24h: totals.successCount24h,
      errorCount24h: totals.errorCount24h,
      successRate24h: calculateSuccessRate(totals.successCount24h, totals.requestCount24h),
    }));

  return {
    nodes: [...endpointNodes, ...modelNodes],
    links: sortedLinks,
    endpointCount: endpointNodes.length,
    modelCount: modelNodes.length,
    activeConnectionTotal: sortedLinks.reduce(
      (total, link) => total + link.activeConnectionCount,
      0,
    ),
    trafficRequestTotal24h: sortedLinks.reduce(
      (total, link) => total + link.trafficRequestCount24h,
      0,
    ),
  };
}

function shouldKeepLink(link: RoutingDiagramLink): boolean {
  return (
    link.activeConnectionCount > 0 ||
    link.requestCount24h > 0 ||
    link.trafficRequestCount24h > 0
  );
}

function buildEndpointNodeId(endpointId: number): string {
  return `endpoint-${endpointId}`;
}

function buildModelNodeId(modelConfigId: number): string {
  return `model-${modelConfigId}`;
}

function buildRouteId(modelId: string, endpointId: number): string {
  return `${modelId}#${endpointId}`;
}

function calculateSuccessRate(successCount: number, requestCount: number): number | null {
  if (requestCount <= 0) {
    return null;
  }

  return Math.round((successCount / requestCount) * 10000) / 100;
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

function compareTotalsByPriority(
  left: [number, DiagramTotals | ModelTotals],
  right: [number, DiagramTotals | ModelTotals],
): number {
  if (right[1].activeConnectionCount !== left[1].activeConnectionCount) {
    return right[1].activeConnectionCount - left[1].activeConnectionCount;
  }

  return compareStringsForLocale(left[1].label, right[1].label);
}

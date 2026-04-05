import type { Connection } from "@/lib/types";
import { compareStringsForLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  RoutingDiagramData,
  RoutingDiagramLink,
  RoutingDiagramNode,
  RoutingDiagramSource,
} from "./routingDiagramContracts";

interface RoutingEdgeAccumulator {
  modelId: string;
  modelLabel: string;
  modelConfigId: number;
  endpointId: number;
  endpointLabel: string;
  connectionIds: number[];
  activeConnectionCount: number;
  trafficRequestCount24h: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
}

interface EndpointTotals {
  activeConnectionCount: number;
  trafficRequestCount24h: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
  label: string;
}

interface ModelTotals extends EndpointTotals {
  modelId: string;
}

export function buildRoutingDiagramData({
  connectionsByModel,
  connectionSuccessRates,
  trafficGroups,
}: RoutingDiagramSource): RoutingDiagramData {
  const edgeMap = new Map<string, RoutingEdgeAccumulator>();
  const connectionToEdgeKey = new Map<number, string>();

  for (const { model, connections } of connectionsByModel) {
    if (!model.is_enabled) {
      continue;
    }

    if (model.model_type === "proxy" && connections.length === 0) {
      continue;
    }

    const modelLabel = model.display_name?.trim() || model.model_id;

    for (const connection of connections) {
      if (!connection.is_active) {
        continue;
      }

      const edgeKey = buildEdgeKey(model.model_id, connection.endpoint_id);
      const existing = edgeMap.get(edgeKey);

      if (existing) {
        existing.connectionIds.push(connection.id);
        existing.activeConnectionCount += 1;
        connectionToEdgeKey.set(connection.id, edgeKey);
        continue;
      }

      edgeMap.set(edgeKey, {
        modelId: model.model_id,
        modelLabel,
        modelConfigId: model.id,
        endpointId: connection.endpoint_id,
        endpointLabel: getEndpointLabel(connection),
        connectionIds: [connection.id],
        activeConnectionCount: 1,
        trafficRequestCount24h: 0,
        requestCount24h: 0,
        successCount24h: 0,
        errorCount24h: 0,
      });
      connectionToEdgeKey.set(connection.id, edgeKey);
    }
  }

  for (const group of trafficGroups) {
    const parsed = parseTrafficGroupKey(group.key);
    if (!parsed) {
      continue;
    }

    const edge = edgeMap.get(buildEdgeKey(parsed.modelId, parsed.endpointId));
    if (!edge) {
      continue;
    }

    edge.trafficRequestCount24h = Math.max(0, Math.trunc(group.total_requests || 0));
  }

  for (const rate of connectionSuccessRates) {
    const edgeKey = connectionToEdgeKey.get(rate.connection_id);
    if (!edgeKey) {
      continue;
    }

    const edge = edgeMap.get(edgeKey);
    if (!edge) {
      continue;
    }

    edge.requestCount24h += Math.max(0, Math.trunc(rate.total_requests || 0));
    edge.successCount24h += Math.max(0, Math.trunc(rate.success_count || 0));
    edge.errorCount24h += Math.max(0, Math.trunc(rate.error_count || 0));
  }

  const links = [...edgeMap.values()]
    .map<RoutingDiagramLink>((edge) => ({
      id: buildEdgeKey(edge.modelId, edge.endpointId),
      sourceNodeId: buildEndpointNodeId(edge.endpointId),
      targetNodeId: buildModelNodeId(edge.modelConfigId),
      modelId: edge.modelId,
      modelLabel: edge.modelLabel,
      modelConfigId: edge.modelConfigId,
      endpointId: edge.endpointId,
      endpointLabel: edge.endpointLabel,
      activeConnectionCount: edge.activeConnectionCount,
      trafficRequestCount24h: edge.trafficRequestCount24h,
      requestCount24h: edge.requestCount24h,
      successCount24h: edge.successCount24h,
      errorCount24h: edge.errorCount24h,
      successRate24h:
        edge.requestCount24h > 0
          ? roundRate((edge.successCount24h / edge.requestCount24h) * 100)
          : null,
    }))
    .sort(compareLinksByPriority);

  const endpointTotals = new Map<number, EndpointTotals>();
  const modelTotals = new Map<number, ModelTotals>();

  for (const link of links) {
    const endpointTotal = endpointTotals.get(link.endpointId) ?? {
      activeConnectionCount: 0,
      trafficRequestCount24h: 0,
      requestCount24h: 0,
      successCount24h: 0,
      errorCount24h: 0,
      label: link.endpointLabel,
    };
    endpointTotal.activeConnectionCount += link.activeConnectionCount;
    endpointTotal.trafficRequestCount24h += link.trafficRequestCount24h;
    endpointTotal.requestCount24h += link.requestCount24h;
    endpointTotal.successCount24h += link.successCount24h;
    endpointTotal.errorCount24h += link.errorCount24h;
    endpointTotals.set(link.endpointId, endpointTotal);

    const modelTotal = modelTotals.get(link.modelConfigId) ?? {
      activeConnectionCount: 0,
      trafficRequestCount24h: 0,
      requestCount24h: 0,
      successCount24h: 0,
      errorCount24h: 0,
      label: link.modelLabel,
      modelId: link.modelId,
    };
    modelTotal.activeConnectionCount += link.activeConnectionCount;
    modelTotal.trafficRequestCount24h += link.trafficRequestCount24h;
    modelTotal.requestCount24h += link.requestCount24h;
    modelTotal.successCount24h += link.successCount24h;
    modelTotal.errorCount24h += link.errorCount24h;
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
      successRate24h:
        totals.requestCount24h > 0
          ? roundRate((totals.successCount24h / totals.requestCount24h) * 100)
          : null,
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
      successRate24h:
        totals.requestCount24h > 0
          ? roundRate((totals.successCount24h / totals.requestCount24h) * 100)
          : null,
    }));

  return {
    nodes: [...endpointNodes, ...modelNodes],
    links,
    endpointCount: endpointNodes.length,
    modelCount: modelNodes.length,
    activeConnectionTotal: links.reduce((total, link) => total + link.activeConnectionCount, 0),
    trafficRequestTotal24h: links.reduce((total, link) => total + link.trafficRequestCount24h, 0),
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

function compareTotalsByPriority(
  left: [number, EndpointTotals | ModelTotals],
  right: [number, EndpointTotals | ModelTotals],
): number {
  if (right[1].activeConnectionCount !== left[1].activeConnectionCount) {
    return right[1].activeConnectionCount - left[1].activeConnectionCount;
  }

  return compareStringsForLocale(left[1].label, right[1].label);
}

function buildEdgeKey(modelId: string, endpointId: number): string {
  return `${modelId}#${endpointId}`;
}

function buildEndpointNodeId(endpointId: number): string {
  return `endpoint-${endpointId}`;
}

function buildModelNodeId(modelConfigId: number): string {
  return `model-${modelConfigId}`;
}

function getEndpointLabel(connection: Connection): string {
  const endpointName = connection.endpoint?.name?.trim();
  if (endpointName) {
    return endpointName;
  }

  const endpointBaseUrl = connection.endpoint?.base_url?.trim();
  if (endpointBaseUrl) {
    return endpointBaseUrl;
  }

  return getStaticMessages().common.endpointWithId(String(connection.endpoint_id));
}

function parseTrafficGroupKey(value: string): { modelId: string; endpointId: number } | null {
  const separatorIndex = value.lastIndexOf("#");
  if (separatorIndex <= 0) {
    return null;
  }

  const modelId = value.slice(0, separatorIndex);
  const endpointId = Number.parseInt(value.slice(separatorIndex + 1), 10);

  if (!modelId || !Number.isFinite(endpointId) || endpointId < 0) {
    return null;
  }

  return { modelId, endpointId };
}

function roundRate(value: number): number {
  return Math.round(value * 100) / 100;
}

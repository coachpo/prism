import type {
  RoutingDiagramData,
  RoutingDiagramGraph,
  RoutingDiagramGraphEdge,
  RoutingDiagramGraphNode,
  RoutingDiagramLinkKind,
  RoutingDiagramNode,
  RoutingDiagramNodeKind,
  RoutingDiagramSummary,
} from "./routingDiagramContracts";
import { compareStringsForLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getTerminalTargetId } from "@/lib/types";

type NormalizedRoutingDiagramNode = Omit<RoutingDiagramNode, "activeTerminalTargetCount">;

type TerminalTargetRollup = {
  activeTerminalTargetCount: number;
  errorCount24h: number;
  requestCount24h: number;
  successCount24h: number;
  successRate24h: number | null;
};

export interface RoutingDiagramMobileRelation {
  id: string;
  linkKind: RoutingDiagramLinkKind;
  nodeId: string;
  nodeKind: RoutingDiagramNodeKind;
  label: string;
  sublabel: string | null;
}

export interface RoutingDiagramMobileNode extends RoutingDiagramGraphNode {
  incoming: RoutingDiagramMobileRelation[];
  outgoing: RoutingDiagramMobileRelation[];
}

export interface RoutingDiagramMobileSection {
  kind: RoutingDiagramNodeKind;
  nodes: RoutingDiagramMobileNode[];
}

export interface RoutingDiagramMobileData {
  sections: RoutingDiagramMobileSection[];
}

export function getRoutingDiagramGraph(data: RoutingDiagramData): RoutingDiagramGraph {
  const nodes = data.nodes
    .map(normalizeRoutingDiagramNode)
    .filter((value): value is NormalizedRoutingDiagramNode => value !== null);
  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const edges = data.edges
    .map(normalizeRoutingDiagramLinkKind)
    .filter((edge): edge is RoutingDiagramLinkKindCarrier => edge !== null)
    .filter((edge) => nodeById.has(edge.source_node_id) && nodeById.has(edge.target_node_id))
    .sort(compareEdgesByPriority);

  const outgoingEdgesBySource = new Map<string, RoutingDiagramLinkKindCarrier[]>();
  const incomingEdgesByTarget = new Map<string, RoutingDiagramLinkKindCarrier[]>();
  for (const edge of edges) {
    const outgoing = outgoingEdgesBySource.get(edge.source_node_id) ?? [];
    outgoing.push(edge);
    outgoingEdgesBySource.set(edge.source_node_id, outgoing);

    const incoming = incomingEdgesByTarget.get(edge.target_node_id) ?? [];
    incoming.push(edge);
    incomingEdgesByTarget.set(edge.target_node_id, incoming);
  }

  const terminalTargetCache = new Map<string, string[]>();
  const graphNodes = nodes
    .map<RoutingDiagramGraphNode>((node) => {
      const terminalTargetIds = collectReachableTerminalTargetIdsForNode(
        node.id,
        nodeById,
        outgoingEdgesBySource,
        incomingEdgesByTarget,
        terminalTargetCache,
      );
      const rollup = buildTerminalTargetRollup(terminalTargetIds, nodeById);

      return {
        ...node,
        activeTerminalTargetCount: rollup.activeTerminalTargetCount,
        requestCount24h: node.kind === "terminal_target" ? node.requestCount24h : rollup.requestCount24h,
        successCount24h: node.kind === "terminal_target" ? node.successCount24h : rollup.successCount24h,
        errorCount24h: node.kind === "terminal_target" ? node.errorCount24h : rollup.errorCount24h,
        successRate24h: node.kind === "terminal_target" ? node.successRate24h : rollup.successRate24h,
      };
    })
    .sort(compareNodesByPriority);
  const graphNodeById = new Map(graphNodes.map((node) => [node.id, node]));

  const graphEdges = edges.map<RoutingDiagramGraphEdge>((edge) => {
    const terminalTargetIds = collectReachableTerminalTargetIdsForEdge(
      edge,
      nodeById,
      outgoingEdgesBySource,
      incomingEdgesByTarget,
      terminalTargetCache,
    );
    const rollup = buildTerminalTargetRollup(terminalTargetIds, nodeById);
    const sourceNode = graphNodeById.get(edge.source_node_id);
    const targetNode = graphNodeById.get(edge.target_node_id);

    return {
      id: edge.id,
      kind: edge.kind,
      sourceNodeId: edge.source_node_id,
      targetNodeId: edge.target_node_id,
      sourceLabel: sourceNode?.label ?? edge.source_node_id,
      targetLabel: targetNode?.label ?? edge.target_node_id,
      enabled: edge.enabled ?? null,
      position: edge.position ?? null,
      activeTerminalTargetCount: rollup.activeTerminalTargetCount,
      requestCount24h: rollup.requestCount24h,
      successCount24h: rollup.successCount24h,
      errorCount24h: rollup.errorCount24h,
      successRate24h: rollup.successRate24h,
    };
  });

  return { nodes: graphNodes, edges: graphEdges };
}

export function filterRoutingDiagramGraphByModelIds(
  graphData: RoutingDiagramGraph,
  selectedModelIds: ReadonlySet<string>,
): RoutingDiagramGraph {
  const modelNodeIds = graphData.nodes
    .filter((node) => node.kind === "model")
    .map((node) => node.id);
  const selectedCurrentModelIds = modelNodeIds.filter((id) => selectedModelIds.has(id));

  if (selectedCurrentModelIds.length === modelNodeIds.length) {
    return graphData;
  }

  if (selectedCurrentModelIds.length === 0) {
    return { nodes: [], edges: [] };
  }

  const nodeById = new Map(graphData.nodes.map((node) => [node.id, node]));
  const outgoingEdgesBySource = new Map<string, RoutingDiagramGraphEdge[]>();
  for (const edge of graphData.edges) {
    const outgoingEdges = outgoingEdgesBySource.get(edge.sourceNodeId) ?? [];
    outgoingEdges.push(edge);
    outgoingEdgesBySource.set(edge.sourceNodeId, outgoingEdges);
  }

  const includedNodeIds = new Set(selectedCurrentModelIds);
  const queue = [...selectedCurrentModelIds];

  for (let index = 0; index < queue.length; index += 1) {
    const sourceNodeId = queue[index];
    for (const edge of outgoingEdgesBySource.get(sourceNodeId) ?? []) {
      const targetNode = nodeById.get(edge.targetNodeId);
      if (!targetNode) {
        continue;
      }

      if (targetNode.kind === "model" && !selectedModelIds.has(targetNode.id)) {
        continue;
      }

      if (includedNodeIds.has(targetNode.id)) {
        continue;
      }

      includedNodeIds.add(targetNode.id);
      queue.push(targetNode.id);
    }
  }

  return {
    nodes: graphData.nodes.filter((node) => includedNodeIds.has(node.id)),
    edges: graphData.edges.filter(
      (edge) => includedNodeIds.has(edge.sourceNodeId) && includedNodeIds.has(edge.targetNodeId),
    ),
  };
}

export function getRoutingDiagramMobileData(graphData: RoutingDiagramGraph): RoutingDiagramMobileData {
  const nodeById = new Map(graphData.nodes.map((node) => [node.id, node]));
  const relationsByNodeId = new Map(
    graphData.nodes.map((node) => [
      node.id,
      {
        incoming: [] as RoutingDiagramMobileRelation[],
        outgoing: [] as RoutingDiagramMobileRelation[],
      },
    ]),
  );

  for (const edge of graphData.edges) {
    const sourceNode = nodeById.get(edge.sourceNodeId);
    const targetNode = nodeById.get(edge.targetNodeId);
    if (!sourceNode || !targetNode) {
      continue;
    }

    relationsByNodeId.get(sourceNode.id)?.outgoing.push({
      id: edge.id,
      linkKind: edge.kind,
      nodeId: targetNode.id,
      nodeKind: targetNode.kind,
      label: targetNode.label,
      sublabel: targetNode.sublabel,
    });
    relationsByNodeId.get(targetNode.id)?.incoming.push({
      id: edge.id,
      linkKind: edge.kind,
      nodeId: sourceNode.id,
      nodeKind: sourceNode.kind,
      label: sourceNode.label,
      sublabel: sourceNode.sublabel,
    });
  }

  return {
    sections: (["model", "terminal_target", "endpoint"] as const)
      .map((kind) => ({
        kind,
        nodes: graphData.nodes
          .filter((node) => node.kind === kind)
          .map((node) => {
            const relations = relationsByNodeId.get(node.id);
            return {
              ...node,
              incoming: relations?.incoming ?? [],
              outgoing: relations?.outgoing ?? [],
            };
          }),
      }))
      .filter((section) => section.nodes.length > 0),
  };
}

export function getRoutingDiagramSummary(data: RoutingDiagramData): RoutingDiagramSummary {
  const recentRequestTotal24h = data.nodes.reduce((total, node) => {
    return isTopologyTerminalTargetNode(node)
      ? total + (node.recent_request_count ?? 0)
      : total;
  }, 0);

  return {
    endpointCount: data.stats.endpoint_count,
    modelCount: data.stats.model_count,
    activeTargetCount: data.stats.active_terminal_target_count,
    recentRequestTotal24h,
  };
}

export function getRoutingDiagramEmptyState(
  data: RoutingDiagramData,
): { kind: "no_active_routes" | "no_recent_traffic"; title: string; description: string } {
  const copy = getStaticMessages().dashboard;
  if (data.stats.edge_count <= 0) {
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

type RoutingDiagramLinkKindCarrier = {
  id: string;
  kind: RoutingDiagramLinkKind;
  source_node_id: string;
  target_node_id: string;
  enabled?: boolean | null;
  position?: number | null;
};

function normalizeRoutingDiagramNode(
  node: RoutingDiagramData["nodes"][number],
): NormalizedRoutingDiagramNode | null {
  const kind = normalizeRoutingDiagramNodeKind(node);
  if (!kind) {
    return null;
  }

  const requestCount24h = node.recent_request_count ?? 0;
  const successCount24h = getSuccessfulRequestCount(requestCount24h, node.recent_success_rate ?? null);
  const errorCount24h = Math.max(requestCount24h - successCount24h, 0);

  return {
    id: node.id,
    kind,
    label: node.label,
    sublabel: node.sublabel ?? null,
    status: node.status,
    modelConfigId: node.model_config_id ?? null,
    modelId: node.model_id ?? null,
    terminalTargetId: getTerminalTargetId(node),
    endpointId: node.endpoint_id ?? null,
    active: node.active ?? null,
    requestCount24h,
    successCount24h,
    errorCount24h,
    successRate24h: requestCount24h > 0 ? node.recent_success_rate ?? null : null,
    lastRequestAt: node.last_request_at ?? null,
  };
}

function normalizeRoutingDiagramNodeKind(
  node: RoutingDiagramData["nodes"][number],
): RoutingDiagramNodeKind | null {
  if (node.kind === "model" || node.kind === "endpoint") {
    return node.kind;
  }

  return isTopologyTerminalTargetNode(node) ? "terminal_target" : null;
}

function isTopologyTerminalTargetNode(
  node: RoutingDiagramData["nodes"][number],
): boolean {
  return node.kind === "terminal_target" || node.kind === "connection" || node.product_kind === "terminal_target";
}

function normalizeRoutingDiagramLinkKind(
  edge: RoutingDiagramData["edges"][number],
): RoutingDiagramLinkKindCarrier | null {
  const kind =
    edge.product_kind === "model_to_model" || edge.kind === "model_to_model"
      ? "model_to_model"
      : edge.product_kind === "model_to_terminal_target" || edge.kind === "model_to_terminal_target" || edge.kind === "model_to_connection"
        ? "model_to_terminal_target"
        : edge.product_kind === "terminal_target_to_endpoint" || edge.kind === "terminal_target_to_endpoint" || edge.kind === "connection_to_endpoint"
          ? "terminal_target_to_endpoint"
          : null;

  if (!kind) {
    return null;
  }

  return {
    id: edge.id,
    kind,
    source_node_id: edge.source_node_id,
    target_node_id: edge.target_node_id,
    enabled: edge.enabled ?? null,
    position: edge.position ?? null,
  };
}

function collectReachableTerminalTargetIdsForNode(
  nodeId: string,
  nodeById: Map<string, NormalizedRoutingDiagramNode>,
  outgoingEdgesBySource: Map<string, RoutingDiagramLinkKindCarrier[]>,
  incomingEdgesByTarget: Map<string, RoutingDiagramLinkKindCarrier[]>,
  cache: Map<string, string[]>,
  trail: Set<string> = new Set(),
): string[] {
  if (cache.has(nodeId)) {
    return cache.get(nodeId) ?? [];
  }

  if (trail.has(nodeId)) {
    return [];
  }

  const node = nodeById.get(nodeId);
  if (!node) {
    return [];
  }

  if (node.kind === "terminal_target") {
    const result = [node.id];
    cache.set(nodeId, result);
    return result;
  }

  const nextTrail = new Set(trail);
  nextTrail.add(nodeId);
  const terminalTargetIds = new Set<string>();

  if (node.kind === "endpoint") {
    const incomingEdges = incomingEdgesByTarget.get(nodeId) ?? [];
    for (const edge of incomingEdges) {
      if (edge.kind !== "terminal_target_to_endpoint") {
        continue;
      }
      for (const terminalTargetId of collectReachableTerminalTargetIdsForNode(
        edge.source_node_id,
        nodeById,
        outgoingEdgesBySource,
        incomingEdgesByTarget,
        cache,
        nextTrail,
      )) {
        terminalTargetIds.add(terminalTargetId);
      }
    }
  } else {
    const outgoingEdges = outgoingEdgesBySource.get(nodeId) ?? [];
    for (const edge of outgoingEdges) {
      if (edge.kind !== "model_to_model" && edge.kind !== "model_to_terminal_target") {
        continue;
      }
      for (const terminalTargetId of collectReachableTerminalTargetIdsForNode(
        edge.target_node_id,
        nodeById,
        outgoingEdgesBySource,
        incomingEdgesByTarget,
        cache,
        nextTrail,
      )) {
        terminalTargetIds.add(terminalTargetId);
      }
    }
  }

  const result = Array.from(terminalTargetIds);
  cache.set(nodeId, result);
  return result;
}

function collectReachableTerminalTargetIdsForEdge(
  edge: RoutingDiagramLinkKindCarrier,
  nodeById: Map<string, NormalizedRoutingDiagramNode>,
  outgoingEdgesBySource: Map<string, RoutingDiagramLinkKindCarrier[]>,
  incomingEdgesByTarget: Map<string, RoutingDiagramLinkKindCarrier[]>,
  cache: Map<string, string[]>,
): string[] {
  if (edge.kind === "terminal_target_to_endpoint") {
    return collectReachableTerminalTargetIdsForNode(
      edge.source_node_id,
      nodeById,
      outgoingEdgesBySource,
      incomingEdgesByTarget,
      cache,
    );
  }

  return collectReachableTerminalTargetIdsForNode(
    edge.target_node_id,
    nodeById,
    outgoingEdgesBySource,
    incomingEdgesByTarget,
    cache,
  );
}

function buildTerminalTargetRollup(
  terminalTargetIds: string[],
  nodeById: Map<string, NormalizedRoutingDiagramNode>,
): TerminalTargetRollup {
  let activeTerminalTargetCount = 0;
  let requestCount24h = 0;
  let successCount24h = 0;

  for (const terminalTargetId of new Set(terminalTargetIds)) {
    const node = nodeById.get(terminalTargetId);
    if (!node || node.kind !== "terminal_target") {
      continue;
    }

    if (node.active !== false) {
      activeTerminalTargetCount += 1;
    }

    requestCount24h += node.requestCount24h;
    successCount24h += node.successCount24h;
  }

  const errorCount24h = Math.max(requestCount24h - successCount24h, 0);
  return {
    activeTerminalTargetCount,
    requestCount24h,
    successCount24h,
    errorCount24h,
    successRate24h:
      requestCount24h > 0 ? (successCount24h / requestCount24h) * 100 : null,
  };
}

function getSuccessfulRequestCount(
  requestCount: number,
  successRate: number | null,
): number {
  if (requestCount <= 0 || successRate === null) {
    return 0;
  }

  return Math.round((requestCount * successRate) / 100);
}

function compareNodesByPriority(
  left: RoutingDiagramGraphNode,
  right: RoutingDiagramGraphNode,
): number {
  const priorityByKind: Record<RoutingDiagramNodeKind, number> = {
    model: 0,
    terminal_target: 1,
    endpoint: 2,
  };

  if (priorityByKind[left.kind] !== priorityByKind[right.kind]) {
    return priorityByKind[left.kind] - priorityByKind[right.kind];
  }

  return compareStringsForLocale(left.label, right.label);
}

function compareEdgesByPriority(
  left: RoutingDiagramLinkKindCarrier,
  right: RoutingDiagramLinkKindCarrier,
): number {
  const priorityByKind: Record<RoutingDiagramLinkKind, number> = {
    model_to_model: 0,
    model_to_terminal_target: 1,
    terminal_target_to_endpoint: 2,
  };

  if (priorityByKind[left.kind] !== priorityByKind[right.kind]) {
    return priorityByKind[left.kind] - priorityByKind[right.kind];
  }

  if (left.source_node_id !== right.source_node_id) {
    return compareStringsForLocale(left.source_node_id, right.source_node_id);
  }

  return compareStringsForLocale(left.target_node_id, right.target_node_id);
}

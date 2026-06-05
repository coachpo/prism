import { compareStringsForLocale } from "@/i18n/format";
import type {
  RoutingDiagramGraph,
  RoutingDiagramGraphEdge,
  RoutingDiagramGraphNode,
  RoutingDiagramNodeKind,
} from "./routingDiagramContracts";

const MODEL_AND_ENDPOINT_WIDTH = 224;
const TERMINAL_TARGET_WIDTH = 208;
const NODE_HEIGHT = 60;
const ROW_GAP = 28;
const COLUMN_GAP = 168;
const HORIZONTAL_PADDING = 40;
const VERTICAL_PADDING = 24;

export const ROUTING_DIAGRAM_FLOW_NODE_TYPE = "routing-diagram-node";
export const ROUTING_DIAGRAM_FLOW_EDGE_TYPE = "routing-diagram-edge";

export interface RoutingDiagramFlowNode {
  id: string;
  type: typeof ROUTING_DIAGRAM_FLOW_NODE_TYPE;
  data: RoutingDiagramGraphNode;
  position: { x: number; y: number };
  width: number;
  height: number;
  sourcePosition: "right";
  targetPosition: "left";
}

export interface RoutingDiagramFlowEdge {
  id: string;
  type: typeof ROUTING_DIAGRAM_FLOW_EDGE_TYPE;
  source: string;
  target: string;
  data: RoutingDiagramGraphEdge;
}

export interface RoutingDiagramFlowBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface RoutingDiagramFlowLayout {
  nodes: RoutingDiagramFlowNode[];
  edges: RoutingDiagramFlowEdge[];
  bounds: RoutingDiagramFlowBounds;
}

type FlowLayoutNodeRecord = {
  node: RoutingDiagramGraphNode;
  column: number;
  row: number;
  width: number;
  orphan: boolean;
};

type FlowLayoutEdge = RoutingDiagramGraphEdge & { position: number | null };

export function getRoutingDiagramFlowLayout(
  graphData: RoutingDiagramGraph,
): RoutingDiagramFlowLayout {
  if (graphData.nodes.length === 0) {
    return {
      nodes: [],
      edges: [],
      bounds: { x: 0, y: 0, width: 0, height: 0 },
    };
  }

  const nodeById = new Map(graphData.nodes.map((node) => [node.id, node]));
  const validEdges = graphData.edges
    .filter(
      (edge): edge is FlowLayoutEdge =>
        nodeById.has(edge.sourceNodeId) && nodeById.has(edge.targetNodeId),
    )
    .map((edge) => ({ ...edge, position: edge.position ?? null }));

  const incomingEdgesByTarget = groupEdgesByTarget(validEdges);
  const outgoingEdgesBySource = groupEdgesBySource(validEdges);
  const modelDepthById = getModelDepthById(graphData.nodes, incomingEdgesByTarget, outgoingEdgesBySource);

  const columnByNodeId = new Map<string, number>();
  for (const node of graphData.nodes) {
    columnByNodeId.set(
      node.id,
      getNodeColumn(node, modelDepthById, columnByNodeId, incomingEdgesByTarget),
    );
  }

  const recordsByColumn = new Map<number, FlowLayoutNodeRecord[]>();
  for (const node of graphData.nodes) {
    const record: FlowLayoutNodeRecord = {
      node,
      column: columnByNodeId.get(node.id) ?? 0,
      row: 0,
      width: getNodeWidth(node.kind),
      orphan: isOrphanNode(node, incomingEdgesByTarget, outgoingEdgesBySource),
    };
    const records = recordsByColumn.get(record.column) ?? [];
    records.push(record);
    recordsByColumn.set(record.column, records);
  }

  const columnIds = Array.from(recordsByColumn.keys()).sort((left, right) => left - right);
  const rowOrderByNodeId = new Map<string, number>();

  for (const columnId of columnIds) {
    const records = recordsByColumn.get(columnId) ?? [];
    records.sort((left, right) =>
      compareColumnRecords(left, right, columnByNodeId, rowOrderByNodeId, incomingEdgesByTarget),
    );

    records.forEach((record, index) => {
      record.row = index;
      rowOrderByNodeId.set(record.node.id, index);
    });
  }

  const xByColumn = getColumnXById(columnIds, recordsByColumn);
  const nodes = columnIds.flatMap((columnId) => {
    const records = recordsByColumn.get(columnId) ?? [];
    return records.map<RoutingDiagramFlowNode>((record) => ({
      id: record.node.id,
      type: ROUTING_DIAGRAM_FLOW_NODE_TYPE,
      data: record.node,
      position: {
        x: xByColumn.get(columnId) ?? HORIZONTAL_PADDING,
        y: VERTICAL_PADDING + record.row * (NODE_HEIGHT + ROW_GAP),
      },
      width: record.width,
      height: NODE_HEIGHT,
      sourcePosition: "right",
      targetPosition: "left",
    }));
  });

  const nodeIndexById = new Map(nodes.map((node, index) => [node.id, index]));
  const edges = [...validEdges]
    .sort((left, right) =>
      compareFlowEdges(left, right, columnByNodeId, rowOrderByNodeId, nodeIndexById),
    )
    .map<RoutingDiagramFlowEdge>((edge) => ({
      id: edge.id,
      type: ROUTING_DIAGRAM_FLOW_EDGE_TYPE,
      source: edge.sourceNodeId,
      target: edge.targetNodeId,
      data: edge,
    }));

  return {
    nodes,
    edges,
    bounds: getLayoutBounds(nodes),
  };
}

function getModelDepthById(
  nodes: RoutingDiagramGraphNode[],
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
  outgoingEdgesBySource: Map<string, FlowLayoutEdge[]>,
): Map<string, number> {
  const modelNodes = nodes.filter((node) => node.kind === "model");
  const modelNodeById = new Map(modelNodes.map((node) => [node.id, node]));
  const incomingModelEdgesByTarget = new Map(
    Array.from(incomingEdgesByTarget.entries()).map(([nodeId, edges]) => [
      nodeId,
      edges.filter((edge) => edge.kind === "model_to_model"),
    ]),
  );
  const depthEdgeIds = collectModelDepthEdgeIds(modelNodes, outgoingEdgesBySource, modelNodeById);
  const depthByNodeId = new Map<string, number>();

  const getDepth = (nodeId: string): number => {
    if (depthByNodeId.has(nodeId)) {
      return depthByNodeId.get(nodeId) ?? 0;
    }

    const incomingEdges = (incomingModelEdgesByTarget.get(nodeId) ?? []).filter((edge) =>
      depthEdgeIds.has(edge.id),
    );

    const depth = incomingEdges.reduce((maxDepth, edge) => {
      return Math.max(maxDepth, getDepth(edge.sourceNodeId) + 1);
    }, 0);

    depthByNodeId.set(nodeId, depth);
    return depth;
  };

  for (const node of modelNodes) {
    getDepth(node.id);
  }

  return depthByNodeId;
}

function collectModelDepthEdgeIds(
  modelNodes: RoutingDiagramGraphNode[],
  outgoingEdgesBySource: Map<string, FlowLayoutEdge[]>,
  modelNodeById: Map<string, RoutingDiagramGraphNode>,
): Set<string> {
  const roots = modelNodes
    .filter((node) => !hasIncomingModelEdge(node.id, modelNodes, outgoingEdgesBySource))
    .sort(compareNodesByLabelAndId);
  const orderedStarts = [...roots];

  for (const node of [...modelNodes].sort(compareNodesByLabelAndId)) {
    if (!orderedStarts.some((item) => item.id === node.id)) {
      orderedStarts.push(node);
    }
  }

  const discovered = new Set<string>();
  const stack = new Set<string>();
  const depthEdgeIds = new Set<string>();

  const visit = (nodeId: string) => {
    if (discovered.has(nodeId)) {
      return;
    }

    discovered.add(nodeId);
    stack.add(nodeId);

    const outgoingEdges = [...(outgoingEdgesBySource.get(nodeId) ?? [])]
      .filter((edge) => edge.kind === "model_to_model" && modelNodeById.has(edge.targetNodeId))
      .sort(compareEdgesForTraversal);

    for (const edge of outgoingEdges) {
      if (stack.has(edge.targetNodeId)) {
        continue;
      }

      depthEdgeIds.add(edge.id);
      visit(edge.targetNodeId);
    }

    stack.delete(nodeId);
  };

  for (const node of orderedStarts) {
    visit(node.id);
  }

  return depthEdgeIds;
}

function getNodeColumn(
  node: RoutingDiagramGraphNode,
  modelDepthById: Map<string, number>,
  columnByNodeId: Map<string, number>,
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
): number {
  if (node.kind === "model") {
    return modelDepthById.get(node.id) ?? 0;
  }

  if (node.kind === "terminal_target") {
    return getTerminalTargetColumn(node.id, modelDepthById, incomingEdgesByTarget);
  }

  const upstreamColumns = (incomingEdgesByTarget.get(node.id) ?? [])
    .filter((edge) => edge.kind === "terminal_target_to_endpoint")
    .map((edge) => {
      return columnByNodeId.get(edge.sourceNodeId)
        ?? getTerminalTargetColumn(edge.sourceNodeId, modelDepthById, incomingEdgesByTarget);
    });

  return upstreamColumns.length > 0 ? Math.max(...upstreamColumns) + 1 : 2;
}

function getTerminalTargetColumn(
  nodeId: string,
  modelDepthById: Map<string, number>,
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
): number {
  const upstreamColumns = (incomingEdgesByTarget.get(nodeId) ?? [])
    .filter((edge) => edge.kind === "model_to_terminal_target")
    .map((edge) => modelDepthById.get(edge.sourceNodeId) ?? 0);

  return upstreamColumns.length > 0 ? Math.max(...upstreamColumns) + 1 : 1;
}

function isOrphanNode(
  node: RoutingDiagramGraphNode,
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
  outgoingEdgesBySource: Map<string, FlowLayoutEdge[]>,
): boolean {
  if (node.kind === "model") {
    const incomingEdges = (incomingEdgesByTarget.get(node.id) ?? []).filter(
      (edge) => edge.kind === "model_to_model",
    );
    const outgoingEdges = (outgoingEdgesBySource.get(node.id) ?? []).filter(
      (edge) => edge.kind === "model_to_model" || edge.kind === "model_to_terminal_target",
    );
    return incomingEdges.length === 0 && outgoingEdges.length === 0;
  }

  if (node.kind === "terminal_target") {
    return !(incomingEdgesByTarget.get(node.id) ?? []).some(
      (edge) => edge.kind === "model_to_terminal_target",
    );
  }

  return !(incomingEdgesByTarget.get(node.id) ?? []).some(
    (edge) => edge.kind === "terminal_target_to_endpoint",
  );
}

function compareColumnRecords(
  left: FlowLayoutNodeRecord,
  right: FlowLayoutNodeRecord,
  columnByNodeId: Map<string, number>,
  rowOrderByNodeId: Map<string, number>,
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
): number {
  if (left.orphan !== right.orphan) {
    return left.orphan ? 1 : -1;
  }

  if (!left.orphan && !right.orphan) {
    const positionDifference = compareNumberKeys(
      getNodePositionSortKey(left, columnByNodeId, incomingEdgesByTarget),
      getNodePositionSortKey(right, columnByNodeId, incomingEdgesByTarget),
    );
    if (positionDifference !== 0) {
      return positionDifference;
    }

    const upstreamDifference = compareNumberKeys(
      getNodeUpstreamOrderSortKey(left, columnByNodeId, rowOrderByNodeId, incomingEdgesByTarget),
      getNodeUpstreamOrderSortKey(right, columnByNodeId, rowOrderByNodeId, incomingEdgesByTarget),
    );
    if (upstreamDifference !== 0) {
      return upstreamDifference;
    }
  }

  return compareNodesByLabelAndId(left.node, right.node);
}

function getNodePositionSortKey(
  record: FlowLayoutNodeRecord,
  columnByNodeId: Map<string, number>,
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
): number {
  const relevantEdges = getRelevantIncomingEdges(record, columnByNodeId, incomingEdgesByTarget);
  const positions = relevantEdges
    .map((edge) => edge.position)
    .filter((value): value is number => value !== null);

  return positions.length > 0 ? Math.min(...positions) : Number.POSITIVE_INFINITY;
}

function getNodeUpstreamOrderSortKey(
  record: FlowLayoutNodeRecord,
  columnByNodeId: Map<string, number>,
  rowOrderByNodeId: Map<string, number>,
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
): number {
  const upstreamOrders = getRelevantIncomingEdges(record, columnByNodeId, incomingEdgesByTarget)
    .map((edge) => rowOrderByNodeId.get(edge.sourceNodeId))
    .filter((value): value is number => value !== undefined);

  return upstreamOrders.length > 0 ? Math.min(...upstreamOrders) : Number.POSITIVE_INFINITY;
}

function getRelevantIncomingEdges(
  record: FlowLayoutNodeRecord,
  columnByNodeId: Map<string, number>,
  incomingEdgesByTarget: Map<string, FlowLayoutEdge[]>,
): FlowLayoutEdge[] {
  const expectedSourceColumn = record.column - 1;

  return (incomingEdgesByTarget.get(record.node.id) ?? []).filter((edge) => {
    if (!isRelevantEdgeForNodeKind(edge, record.node.kind)) {
      return false;
    }

    return (columnByNodeId.get(edge.sourceNodeId) ?? -1) === expectedSourceColumn;
  });
}

function isRelevantEdgeForNodeKind(
  edge: FlowLayoutEdge,
  nodeKind: RoutingDiagramNodeKind,
): boolean {
  if (nodeKind === "model") {
    return edge.kind === "model_to_model";
  }

  if (nodeKind === "terminal_target") {
    return edge.kind === "model_to_terminal_target";
  }

  return edge.kind === "terminal_target_to_endpoint";
}

function compareFlowEdges(
  left: FlowLayoutEdge,
  right: FlowLayoutEdge,
  columnByNodeId: Map<string, number>,
  rowOrderByNodeId: Map<string, number>,
  nodeIndexById: Map<string, number>,
): number {
  const leftSourceColumn = columnByNodeId.get(left.sourceNodeId) ?? 0;
  const rightSourceColumn = columnByNodeId.get(right.sourceNodeId) ?? 0;
  const sourceColumnDifference = compareNumberKeys(leftSourceColumn, rightSourceColumn);
  if (sourceColumnDifference !== 0) {
    return sourceColumnDifference;
  }

  const leftSourceRow = rowOrderByNodeId.get(left.sourceNodeId) ?? nodeIndexById.get(left.sourceNodeId) ?? 0;
  const rightSourceRow = rowOrderByNodeId.get(right.sourceNodeId) ?? nodeIndexById.get(right.sourceNodeId) ?? 0;
  const sourceRowDifference = compareNumberKeys(leftSourceRow, rightSourceRow);
  if (sourceRowDifference !== 0) {
    return sourceRowDifference;
  }

  const leftTargetColumn = columnByNodeId.get(left.targetNodeId) ?? 0;
  const rightTargetColumn = columnByNodeId.get(right.targetNodeId) ?? 0;
  const targetColumnDifference = compareNumberKeys(leftTargetColumn, rightTargetColumn);
  if (targetColumnDifference !== 0) {
    return targetColumnDifference;
  }

  const leftTargetRow = rowOrderByNodeId.get(left.targetNodeId) ?? nodeIndexById.get(left.targetNodeId) ?? 0;
  const rightTargetRow = rowOrderByNodeId.get(right.targetNodeId) ?? nodeIndexById.get(right.targetNodeId) ?? 0;
  const targetRowDifference = compareNumberKeys(leftTargetRow, rightTargetRow);
  if (targetRowDifference !== 0) {
    return targetRowDifference;
  }

  const positionDifference = compareNumberKeys(
    left.position ?? Number.POSITIVE_INFINITY,
    right.position ?? Number.POSITIVE_INFINITY,
  );
  if (positionDifference !== 0) {
    return positionDifference;
  }

  return compareStringsForLocale(left.id, right.id);
}

function getColumnXById(
  columnIds: number[],
  recordsByColumn: Map<number, FlowLayoutNodeRecord[]>,
): Map<number, number> {
  const xByColumn = new Map<number, number>();
  let currentX = HORIZONTAL_PADDING;

  for (const columnId of columnIds) {
    xByColumn.set(columnId, currentX);
    const records = recordsByColumn.get(columnId) ?? [];
    const columnWidth = records.reduce((maxWidth, record) => Math.max(maxWidth, record.width), 0);
    currentX += columnWidth + COLUMN_GAP;
  }

  return xByColumn;
}

function getLayoutBounds(nodes: RoutingDiagramFlowNode[]): RoutingDiagramFlowBounds {
  const maxRight = nodes.reduce((maxValue, node) => {
    return Math.max(maxValue, node.position.x + (node.width ?? getNodeWidth(node.data.kind)));
  }, 0);
  const maxBottom = nodes.reduce((maxValue, node) => {
    return Math.max(maxValue, node.position.y + (node.height ?? NODE_HEIGHT));
  }, 0);

  return {
    x: 0,
    y: 0,
    width: maxRight + HORIZONTAL_PADDING,
    height: maxBottom + VERTICAL_PADDING,
  };
}

function getNodeWidth(kind: RoutingDiagramNodeKind): number {
  return kind === "terminal_target" ? TERMINAL_TARGET_WIDTH : MODEL_AND_ENDPOINT_WIDTH;
}

function hasIncomingModelEdge(
  nodeId: string,
  modelNodes: RoutingDiagramGraphNode[],
  outgoingEdgesBySource: Map<string, FlowLayoutEdge[]>,
): boolean {
  return modelNodes.some((node) => {
    return (outgoingEdgesBySource.get(node.id) ?? []).some(
      (edge) => edge.kind === "model_to_model" && edge.targetNodeId === nodeId,
    );
  });
}

function compareEdgesForTraversal(left: FlowLayoutEdge, right: FlowLayoutEdge): number {
  const positionDifference = compareNumberKeys(
    left.position ?? Number.POSITIVE_INFINITY,
    right.position ?? Number.POSITIVE_INFINITY,
  );
  if (positionDifference !== 0) {
    return positionDifference;
  }

  if (left.targetLabel !== right.targetLabel) {
    return compareStringsForLocale(left.targetLabel, right.targetLabel);
  }

  return compareStringsForLocale(left.targetNodeId, right.targetNodeId);
}

function compareNodesByLabelAndId(
  left: RoutingDiagramGraphNode,
  right: RoutingDiagramGraphNode,
): number {
  if (left.label !== right.label) {
    return compareStringsForLocale(left.label, right.label);
  }

  return compareStringsForLocale(left.id, right.id);
}

function compareNumberKeys(left: number, right: number): number {
  if (left === right) {
    return 0;
  }

  return left < right ? -1 : 1;
}

function groupEdgesByTarget(edges: FlowLayoutEdge[]): Map<string, FlowLayoutEdge[]> {

  const groupedEdges = new Map<string, FlowLayoutEdge[]>();

  for (const edge of edges) {
    const targetEdges = groupedEdges.get(edge.targetNodeId) ?? [];
    targetEdges.push(edge);
    groupedEdges.set(edge.targetNodeId, targetEdges);
  }

  return groupedEdges;
}

function groupEdgesBySource(edges: FlowLayoutEdge[]): Map<string, FlowLayoutEdge[]> {
  const groupedEdges = new Map<string, FlowLayoutEdge[]>();

  for (const edge of edges) {
    const sourceEdges = groupedEdges.get(edge.sourceNodeId) ?? [];
    sourceEdges.push(edge);
    groupedEdges.set(edge.sourceNodeId, sourceEdges);
  }

  return groupedEdges;
}

import type { Node } from "@xyflow/react";
import type { RoutingDiagramFlowLayout } from "./routingDiagramFlowLayout";

export function getRoutingDiagramFlowLayoutSignature(flowLayout: RoutingDiagramFlowLayout) {
  return JSON.stringify({
    edges: flowLayout.edges.map((edge) => [edge.id, edge.source, edge.target]),
    nodes: flowLayout.nodes.map((node) => [
      node.id,
      node.position.x,
      node.position.y,
      node.width,
      node.height,
    ]),
  });
}

export function reconcileRoutingDiagramFlowNodes<TNode extends Node>(
  currentNodes: TNode[],
  nextNodes: TNode[],
): TNode[] {
  const currentNodeById = new Map(currentNodes.map((node) => [node.id, node]));

  return nextNodes.map((node) => {
    const currentNode = currentNodeById.get(node.id);
    if (!currentNode) {
      return node;
    }

    return {
      ...currentNode,
      ...node,
      position: currentNode.position,
    };
  });
}

import type {
  RoutingDiagramGraph,
  RoutingDiagramGraphEdge,
  RoutingDiagramGraphNode,
  RoutingDiagramNode,
} from "../routingDiagram";

export interface RoutingDiagramChartProps {
  graphData: RoutingDiagramGraph;
  chartHeight: number;
  isCompact: boolean;
  onActivateNode: (node: RoutingDiagramNode) => void;
}

export interface RoutingNodeShapeProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  payload?: RoutingDiagramGraphNode;
}

export interface RoutingLinkShapeProps {
  sourceX?: number;
  sourceY?: number;
  sourceControlX?: number;
  targetX?: number;
  targetY?: number;
  targetControlX?: number;
  linkWidth?: number;
  payload?: RoutingDiagramGraphEdge;
}

export interface RoutingDiagramTooltipProps {
  active?: boolean;
  payload?: Array<{
    payload?: {
      payload?: unknown;
    };
  }>;
}

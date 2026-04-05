import type {
  RoutingDiagramChartLink,
  RoutingDiagramChartNode,
  RoutingDiagramNode,
} from "../routingDiagram";

export interface RoutingDiagramChartProps {
  chartData: {
    nodes: RoutingDiagramChartNode[];
    links: RoutingDiagramChartLink[];
  };
  chartHeight: number;
  isCompact: boolean;
  onActivateNode: (node: RoutingDiagramNode) => void;
}

export interface RoutingNodeShapeProps {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  payload?: RoutingDiagramChartNode;
}

export interface RoutingLinkShapeProps {
  sourceX?: number;
  sourceY?: number;
  sourceControlX?: number;
  targetX?: number;
  targetY?: number;
  targetControlX?: number;
  linkWidth?: number;
  payload?: RoutingDiagramChartLink;
}

export interface RoutingDiagramTooltipProps {
  active?: boolean;
  payload?: Array<{
    payload?: {
      payload?: unknown;
    };
  }>;
}

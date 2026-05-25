import type {
  DashboardRoutingHealthMap,
  DashboardRoutingLink,
  DashboardRoutingNode,
} from "@/lib/types";

export type RoutingDiagramNode = DashboardRoutingNode;
export type RoutingDiagramLink = DashboardRoutingLink;
export type RoutingDiagramData = DashboardRoutingHealthMap;

export interface RoutingDiagramChartNode extends RoutingDiagramNode {
  value: number;
}

export interface RoutingDiagramChartLink extends RoutingDiagramLink {
  source: number;
  target: number;
  value: number;
}

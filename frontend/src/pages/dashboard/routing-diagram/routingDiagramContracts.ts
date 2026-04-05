import type {
  Connection,
  ConnectionSuccessRate,
  ModelConfigListItem,
  SpendingGroupRow,
} from "@/lib/types";

export interface RoutingDiagramNode {
  id: string;
  name: string;
  kind: "endpoint" | "model";
  label: string;
  sublabel: string | null;
  endpointId: number | null;
  modelId: string | null;
  modelConfigId: number | null;
  activeConnectionCount: number;
  trafficRequestCount24h: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
  successRate24h: number | null;
}

export interface RoutingDiagramLink {
  id: string;
  sourceNodeId: string;
  targetNodeId: string;
  modelId: string;
  modelLabel: string;
  modelConfigId: number;
  endpointId: number;
  endpointLabel: string;
  activeConnectionCount: number;
  trafficRequestCount24h: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
  successRate24h: number | null;
}

export interface RoutingDiagramData {
  nodes: RoutingDiagramNode[];
  links: RoutingDiagramLink[];
  endpointCount: number;
  modelCount: number;
  activeConnectionTotal: number;
  trafficRequestTotal24h: number;
}

export interface RoutingDiagramChartNode extends RoutingDiagramNode {
  value: number;
}

export interface RoutingDiagramChartLink extends RoutingDiagramLink {
  source: number;
  target: number;
  value: number;
}

export interface RoutingDiagramSource {
  connectionsByModel: Array<{
    model: ModelConfigListItem;
    connections: Connection[];
  }>;
  connectionSuccessRates: ConnectionSuccessRate[];
  trafficGroups: SpendingGroupRow[];
}

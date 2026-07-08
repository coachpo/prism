import type { DashboardTopologyGraph } from "@/lib/types";

export type RoutingDiagramData = DashboardTopologyGraph;

export type RoutingDiagramNodeKind = "endpoint" | "model" | "terminal_target";

export type RoutingDiagramLinkKind =
  | "model_to_model"
  | "model_to_terminal_target"
  | "terminal_target_to_endpoint";

export interface RoutingDiagramNode {
  id: string;
  kind: RoutingDiagramNodeKind;
  label: string;
  sublabel: string | null;
  status: string;
  modelConfigId: number | null;
  modelId: string | null;
  terminalTargetId: number | null;
  endpointId: number | null;
  active: boolean | null;
  activeTerminalTargetCount: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
  successRate24h: number | null;
  lastRequestAt: string | null;
}

export interface RoutingDiagramLink {
  id: string;
  kind: RoutingDiagramLinkKind;
  sourceNodeId: string;
  targetNodeId: string;
  sourceLabel: string;
  targetLabel: string;
  enabled: boolean | null;
  position: number | null;
  activeTerminalTargetCount: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
  successRate24h: number | null;
}

export interface RoutingDiagramSummary {
  endpointCount: number;
  modelCount: number;
  activeTargetCount: number;
  recentRequestTotal24h: number;
}

export type RoutingDiagramGraphNode = RoutingDiagramNode;

export type RoutingDiagramGraphEdge = RoutingDiagramLink;

export interface RoutingDiagramGraph {
  nodes: RoutingDiagramGraphNode[];
  edges: RoutingDiagramGraphEdge[];
}

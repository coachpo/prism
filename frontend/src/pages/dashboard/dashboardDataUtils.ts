import type { RoutingDiagramData } from "./routingDiagram";

export function getEmptyRoutingDiagramData(): RoutingDiagramData {
  return {
    nodes: [],
    edges: [],
    stats: {
      model_count: 0,
      active_model_count: 0,
      disabled_model_count: 0,
      terminal_target_count: 0,
      active_terminal_target_count: 0,
      inactive_terminal_target_count: 0,
      endpoint_count: 0,
      edge_count: 0,
    },
  };
}

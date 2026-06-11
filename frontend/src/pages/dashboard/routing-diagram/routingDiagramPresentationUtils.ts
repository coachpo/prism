import type { CSSProperties } from "react";

import type { RoutingDiagramNode, RoutingDiagramNodeKind } from "../routingDiagram";
import { formatNumber, getCurrentLocale } from "@/i18n/format";

export const ROUTE_HEALTH_COLOR = {
  healthy: "#10b981",
  degraded: "#f59e0b",
  failing: "#ef4444",
  noData: "#64748b",
} as const;

export type RouteHealthState = keyof typeof ROUTE_HEALTH_COLOR;

export type RoutingDiagramNodeShape = "panel" | "capsule" | "cut-corner";

export interface RoutingDiagramNodeVisualMetadata {
  background: string;
  color: string;
  cutCornerClipPath?: string;
  markerClassName: string;
  markerClipPath?: string;
  shape: RoutingDiagramNodeShape;
  shapeClassName: string;
}

const ROUTING_DIAGRAM_NODE_VISUAL_METADATA: Record<
  RoutingDiagramNodeKind,
  RoutingDiagramNodeVisualMetadata
> = {
  model: {
    background: "color-mix(in oklab, var(--chart-1) 14%, var(--background))",
    color: "var(--chart-1)",
    markerClassName: "rounded-[0.2rem]",
    shape: "panel",
    shapeClassName: "rounded-xl",
  },
  endpoint: {
    background: "color-mix(in oklab, var(--chart-2) 16%, var(--background))",
    color: "var(--chart-2)",
    markerClassName: "rounded-full",
    shape: "capsule",
    shapeClassName: "rounded-[1.5rem]",
  },
  terminal_target: {
    background: "color-mix(in oklab, var(--chart-4) 16%, var(--background))",
    color: "var(--chart-4)",
    cutCornerClipPath: "polygon(0 0, calc(100% - 0.875rem) 0, 100% 0.875rem, 100% 100%, 0 100%)",
    markerClassName: "rounded-[0.15rem] [clip-path:polygon(0_0,70%_0,100%_30%,100%_100%,0_100%)]",
    markerClipPath: "polygon(0 0, 70% 0, 100% 30%, 100% 100%, 0 100%)",
    shape: "cut-corner",
    shapeClassName: "rounded-md",
  },
};

export function getRoutingDiagramNodeVisualMetadata(
  kind: RoutingDiagramNodeKind,
): RoutingDiagramNodeVisualMetadata {
  return ROUTING_DIAGRAM_NODE_VISUAL_METADATA[kind];
}

export function getRoutingDiagramNodeCardStyle(
  nodeVisual: RoutingDiagramNodeVisualMetadata,
  muted: boolean,
): CSSProperties & Record<"--routing-node-color" | "--routing-node-background", string> {
  return {
    "--routing-node-color": nodeVisual.color,
    "--routing-node-background": nodeVisual.background,
    background: "linear-gradient(135deg, color-mix(in oklab, var(--routing-node-color) 10%, transparent), transparent 44%), var(--routing-node-background)",
    borderColor: muted ? "var(--border)" : "color-mix(in oklab, var(--routing-node-color) 45%, var(--border))",
    clipPath: nodeVisual.cutCornerClipPath,
  };
}

export function truncateLabel(value: string, limit: number): string {
  if (value.length <= limit) {
    return value;
  }

  return `${value.slice(0, Math.max(limit - 3, 1))}...`;
}

export function getRouteHealthColor(successRate: number | null, requestCount: number): string {
  return ROUTE_HEALTH_COLOR[getRouteHealthState(successRate, requestCount)];
}

export function getRouteHealthState(
  successRate: number | null,
  requestCount: number,
): RouteHealthState {
  if (requestCount <= 0 || successRate === null) {
    return "noData";
  }

  if (successRate >= 99) {
    return "healthy";
  }

  if (successRate >= 95) {
    return "degraded";
  }

  return "failing";
}

export function formatSuccessRate(
  successRate: number | null,
  requestCount: number,
): string | null {
  if (requestCount <= 0 || successRate === null) {
    return null;
  }

  return `${formatNumber(successRate, getCurrentLocale(), {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}%`;
}

export function isRoutingDiagramInteractiveNode(node: RoutingDiagramNode): boolean {
  return node.kind === "model" || node.kind === "endpoint";
}

export function isRoutingDiagramMutedNode(node: RoutingDiagramNode): boolean {
  return node.kind === "terminal_target"
    ? node.active === false || node.status === "inactive"
    : node.status === "disabled";
}

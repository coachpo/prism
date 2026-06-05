import type { RoutingDiagramNode } from "../routingDiagram";
import { formatNumber, getCurrentLocale } from "@/i18n/format";

export const ROUTE_HEALTH_COLOR = {
  healthy: "#10b981",
  degraded: "#f59e0b",
  failing: "#ef4444",
  noData: "#64748b",
} as const;

export type RouteHealthState = keyof typeof ROUTE_HEALTH_COLOR;

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

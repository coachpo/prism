import type { CSSProperties } from "react";

import { BaseEdge, getBezierPath } from "@xyflow/react";

import type { RoutingDiagramFlowEdge as RoutingDiagramFlowLayoutEdge } from "../routingDiagram";
import { useLocale } from "@/i18n/useLocale";

import { getRoutingDiagramFlowEdgeStyle } from "./routingDiagramFlowEdgeStyle";

export interface RoutingDiagramFlowEdgeProps {
  id: string;
  sourceX: number;
  sourceY: number;
  targetX: number;
  targetY: number;
  sourcePosition?: "left" | "right" | "top" | "bottom";
  targetPosition?: "left" | "right" | "top" | "bottom";
  markerEnd?: string;
  style?: CSSProperties;
  data?: RoutingDiagramFlowLayoutEdge["data"];
  onInspectEdge?: (edge: RoutingDiagramFlowLayoutEdge["data"]) => void;
}

export function RoutingDiagramFlowEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition = "right",
  targetPosition = "left",
  markerEnd,
  style,
  data,
  onInspectEdge,
}: RoutingDiagramFlowEdgeProps) {
  const { messages } = useLocale();
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition: sourcePosition as Parameters<typeof getBezierPath>[0]["sourcePosition"],
    targetX,
    targetY,
    targetPosition: targetPosition as Parameters<typeof getBezierPath>[0]["targetPosition"],
  });
  const edgeStyle = getRoutingDiagramFlowEdgeStyle(data);
  const edgeId = data?.id ?? id;

  return (
    <>
      <BaseEdge
        path={edgePath}
        markerEnd={markerEnd}
        interactionWidth={24}
        aria-label={data ? messages.dashboard.routingLinkAria(data.sourceLabel, data.targetLabel) : messages.dashboard.routingLink}
        className="transition-opacity duration-150"
        data-testid={`routing-diagram-edge-${edgeId}`}
        style={{ ...style, ...edgeStyle }}
      />
      {data ? (
        <path
          d={edgePath}
          fill="none"
          stroke="transparent"
          strokeWidth={24}
          pointerEvents="stroke"
          aria-hidden="true"
          data-testid={`routing-diagram-edge-hit-area-${edgeId}`}
          onMouseEnter={() => onInspectEdge?.(data)}
        />
      ) : null}
    </>
  );
}

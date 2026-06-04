import type { KeyboardEvent } from "react";
import type { RoutingDiagramNode } from "../routingDiagram";
import { useLocale } from "@/i18n/useLocale";
import {
  isRoutingDiagramInteractiveNode,
  isRoutingDiagramMutedNode,
  truncateLabel,
} from "./routingDiagramChartUtils";
import type { RoutingNodeShapeProps } from "./routingDiagramChartTypes";

interface RoutingDiagramNodeShapeProps {
  compact: boolean;
  onActivate: (node: RoutingDiagramNode) => void;
  props: RoutingNodeShapeProps;
}

export function RoutingDiagramNodeShape({
  compact,
  onActivate,
  props,
}: RoutingDiagramNodeShapeProps) {
  const { messages } = useLocale();
  const { x = 0, y = 0, width = 0, height = 0, payload } = props;

  if (!payload) {
    return null;
  }

  const interactive = isRoutingDiagramInteractiveNode(payload);
  const muted = isRoutingDiagramMutedNode(payload);
  const rectFill =
    payload.kind === "endpoint"
      ? "var(--chart-2)"
      : payload.kind === "terminal_target"
        ? "var(--chart-4)"
        : "var(--chart-1)";
  const labelText = truncateLabel(payload.label, compact ? 12 : 22);
  const secondaryText = getSecondaryText(payload, messages);
  const textAnchor = payload.kind === "endpoint" ? "end" : "start";
  const textX = payload.kind === "endpoint" ? x - 10 : x + width + 10;

  const activate = () => onActivate(payload);
  const handleKeyDown = (event: KeyboardEvent<SVGGElement>) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      activate();
    }
  };

  return (
    <g
      aria-label={interactive ? payload.label : undefined}
      className={interactive ? "cursor-pointer" : undefined}
      onClick={interactive ? activate : undefined}
      onKeyDown={interactive ? handleKeyDown : undefined}
      role={interactive ? "button" : undefined}
      tabIndex={interactive ? 0 : undefined}
    >
      <rect
        x={x}
        y={y}
        width={width}
        height={height}
        rx={Math.min(6, height / 3)}
        fill={rectFill}
        fillOpacity={muted ? 0.3 : 0.88}
        stroke={muted ? "var(--border)" : "var(--background)"}
        strokeDasharray={muted ? "4 2" : undefined}
        strokeWidth={1.5}
      />
      <text
        x={textX}
        y={y + Math.max(12, height / 2 - 2)}
        textAnchor={textAnchor}
        fontSize={compact ? 10 : 11}
        fontWeight={600}
        fill={muted ? "var(--muted-foreground)" : "var(--foreground)"}
      >
        {labelText}
      </text>
      {secondaryText ? (
        <text
          x={textX}
          y={y + Math.max(24, height / 2 + 12)}
          textAnchor={textAnchor}
          fontSize={compact ? 9 : 10}
          fill="var(--muted-foreground)"
        >
          {truncateLabel(secondaryText, compact ? 16 : 28)}
        </text>
      ) : null}
    </g>
  );
}

function getSecondaryText(
  payload: RoutingDiagramNode,
  messages: ReturnType<typeof useLocale>["messages"],
): string | null {
  if (payload.kind === "terminal_target") {
    return `${messages.modelDetail.connections} · ${payload.active === false ? messages.modelDetail.inactive : messages.modelDetail.active}`;
  }

  if (payload.kind === "model" && payload.status === "disabled") {
    return payload.sublabel
      ? `${payload.sublabel} · ${messages.modelDetail.disabled}`
      : messages.modelDetail.disabled;
  }

  return payload.sublabel;
}

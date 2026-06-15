import type { RoutingDiagramFlowNode as RoutingDiagramFlowLayoutNode, RoutingDiagramNode } from "../routingDiagram";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import {
  getRoutingDiagramNodeCardStyle,
  getRoutingDiagramNodeVisualMetadata,
  isRoutingDiagramInteractiveNode,
  isRoutingDiagramMutedNode,
  truncateLabel,
} from "./routingDiagramPresentationUtils";

export interface RoutingDiagramFlowNodeProps {
  data: RoutingDiagramFlowLayoutNode["data"];
  onActivateNode?: (node: RoutingDiagramFlowLayoutNode["data"]) => void;
}

export function RoutingDiagramFlowNode({
  data,
  onActivateNode,
}: RoutingDiagramFlowNodeProps) {
  const { formatNumber, messages } = useLocale();
  const interactive = isRoutingDiagramInteractiveNode(data);
  const muted = isRoutingDiagramMutedNode(data);
  const nodeVisual = getRoutingDiagramNodeVisualMetadata(data.kind);
  const testId = getNodeTestId(data);
  const secondaryText = getSecondaryText(data, messages);
  const actionLabel = getNodeActionLabel(data, messages);
  const actionText = getNodeActionText(data, messages);
  const pillText =
    data.kind === "terminal_target"
      ? data.active === false
        ? messages.modelDetail.inactive
        : messages.modelDetail.active
      : messages.dashboard.activeTargets(formatNumber(data.activeTerminalTargetCount));

  return (
    <article
      data-interactive={interactive ? "true" : "false"}
      data-muted={muted ? "true" : "false"}
      data-node-shape={nodeVisual.shape}
      data-testid={testId}
      className={cn(data.kind === "terminal_target" ? "w-[208px]" : "w-[224px]")}
    >
      <div
        className={cn(
          "grid gap-2.5 border border-outline-variant p-3 transition-opacity",
          nodeVisual.shapeClassName,
          muted && "border-dashed opacity-70",
        )}
        style={getRoutingDiagramNodeCardStyle(nodeVisual, muted)}
      >
        <div className="flex items-start">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <div className="flex flex-wrap items-center gap-2">
              <p className="truncate text-sm font-semibold text-foreground">
                {truncateLabel(data.label, data.kind === "terminal_target" ? 20 : 24)}
              </p>
              {getStateBadge(data, messages)}
            </div>
            {data.kind === "terminal_target" && data.sublabel ? (
              <p className="truncate text-xs text-muted-foreground">
                {truncateLabel(data.sublabel, 28)}
              </p>
            ) : null}
            {secondaryText ? (
              <p className="truncate text-xs text-muted-foreground">
                {truncateLabel(secondaryText, data.kind === "terminal_target" ? 28 : 34)}
              </p>
            ) : null}
          </div>
        </div>

        <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
          <Pill>{pillText}</Pill>
          <Pill>{messages.dashboard.successfulRequests24h(formatNumber(data.successCount24h))}</Pill>
        </div>

        {interactive && actionText ? (
          <Button
            type="button"
            variant="outline"
            size="xs"
            className="nodrag nopan h-auto min-h-[var(--density-control-h-xs)] w-full justify-start overflow-hidden rounded-lg border-outline-variant px-3 py-1.5 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            aria-label={actionLabel ?? undefined}
            onClick={() => onActivateNode?.(data)}
          >
            <span className="block w-full truncate">{actionText}</span>
          </Button>
        ) : null}
      </div>
    </article>
  );
}

function Pill({ children }: { children: string }) {
  return <Badge variant="outline">{children}</Badge>;
}

function getNodeTestId(node: Pick<RoutingDiagramNode, "id" | "kind">): string {
  if (node.kind === "model") {
    return `routing-diagram-node-model-${node.id}`;
  }

  if (node.kind === "terminal_target") {
    return `routing-diagram-node-terminal-target-${node.id}`;
  }

  return `routing-diagram-node-endpoint-${node.id}`;
}

function getNodeActionLabel(
  node: Pick<RoutingDiagramNode, "kind" | "label">,
  messages: ReturnType<typeof useLocale>["messages"],
): string | null {
  if (node.kind === "model") {
    return messages.modelsUi.viewModelDetails(node.label);
  }

  if (node.kind === "endpoint") {
    return `${messages.modelDetail.viewRequestLogs}: ${node.label}`;
  }

  return null;
}

function getNodeActionText(
  node: Pick<RoutingDiagramNode, "kind" | "label">,
  messages: ReturnType<typeof useLocale>["messages"],
): string | null {
  if (node.kind === "model") {
    return messages.modelsUi.viewModelDetails(node.label);
  }

  if (node.kind === "endpoint") {
    return messages.modelDetail.viewRequestLogs;
  }

  return null;
}

function getSecondaryText(
  node: RoutingDiagramNode,
  messages: ReturnType<typeof useLocale>["messages"],
): string | null {
  if (node.kind === "terminal_target") {
    return `${messages.modelDetail.connections} · ${node.active === false ? messages.modelDetail.inactive : messages.modelDetail.active}`;
  }

  if (node.kind === "model" && node.status === "disabled") {
    return node.sublabel ? `${node.sublabel} · ${messages.modelDetail.disabled}` : messages.modelDetail.disabled;
  }

  return node.sublabel;
}

function getStateBadge(
  node: RoutingDiagramNode,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  if (node.kind === "model" && node.status === "disabled") {
    return <Badge variant="outline">{messages.modelDetail.disabled}</Badge>;
  }

  if (node.kind === "terminal_target") {
    return <Badge variant="outline">{node.active === false ? messages.modelDetail.inactive : messages.modelDetail.active}</Badge>;
  }

  return null;
}

import type { RoutingDiagramFlowNode as RoutingDiagramFlowLayoutNode, RoutingDiagramNode } from "../routingDiagram";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import {
  isRoutingDiagramInteractiveNode,
  isRoutingDiagramMutedNode,
  truncateLabel,
} from "./routingDiagramChartUtils";

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
      data-testid={testId}
      className={cn(data.kind === "terminal_target" ? "w-[208px]" : "w-[224px]")}
    >
      <div
        className={cn(
          "grid gap-3 rounded-xl border border-border/70 bg-background/90 p-3 shadow-none transition-opacity",
          muted && "border-dashed opacity-70",
        )}
      >
        <div className="flex items-start gap-3">
          <span
            aria-hidden="true"
            className="mt-0.5 size-2.5 shrink-0 rounded-full border"
            style={getNodeDotStyle(data.kind, muted)}
          />
          <div className="min-w-0 flex-1 space-y-1">
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
            className="h-auto min-h-[var(--density-control-h-xs)] w-full justify-start whitespace-normal rounded-lg border-border/70 px-3 py-2 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            aria-label={actionLabel ?? undefined}
            onClick={() => onActivateNode?.(data)}
          >
            {actionText}
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

function getNodeDotStyle(kind: RoutingDiagramNode["kind"], muted: boolean) {
  const backgroundColor =
    kind === "endpoint"
      ? "var(--chart-2)"
      : kind === "terminal_target"
        ? "var(--chart-4)"
        : "var(--chart-1)";

  return {
    backgroundColor,
    borderColor: muted ? "var(--border)" : "transparent",
    opacity: muted ? 0.45 : 0.9,
  };
}

import { useMemo, type KeyboardEvent, type ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import { RoutingDiagramInspectorContent } from "./RoutingDiagramInspectorContent";
import { RoutingDiagramLegend } from "./RoutingDiagramLegend";
import { RoutingDiagramVisualizationShell } from "./RoutingDiagramVisualizationShell";
import {
  getRoutingDiagramNodeCardStyle,
  getRoutingDiagramNodeVisualMetadata,
  isRoutingDiagramMutedNode,
} from "./routingDiagramPresentationUtils";
import type {
  RoutingDiagramMobileData,
  RoutingDiagramMobileNode,
  RoutingDiagramNode,
  RoutingDiagramNodeKind,
} from "../routingDiagram";

interface RoutingDiagramMobileListProps {
  inspectedNode?: RoutingDiagramNode | null;
  mobileData: RoutingDiagramMobileData;
  onActivateNode: (node: RoutingDiagramNode) => void;
  onInspectNode: (node: RoutingDiagramNode) => void;
}

export function RoutingDiagramMobileList({
  inspectedNode,
  mobileData,
  onActivateNode,
  onInspectNode,
}: RoutingDiagramMobileListProps) {
  const { formatNumber, messages } = useLocale();
  const nodeById = useMemo(() => {
    return new Map(
      mobileData.sections.flatMap((section) => section.nodes.map((node) => [node.id, node] as const)),
    );
  }, [mobileData.sections]);

  return (
    <RoutingDiagramVisualizationShell
      visualization={
        <div className="grid gap-3" data-testid="routing-diagram-mobile">
          {inspectedNode ? (
            <div data-testid="routing-diagram-inspector" className="max-w-full justify-self-start">
              <RoutingDiagramInspectorContent node={inspectedNode} />
            </div>
          ) : null}
          {mobileData.sections.map((section) => (
            <section key={section.kind} className="grid gap-2">
              <div className="flex items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                  <NodeKindDot kind={section.kind} />
                  <h4 className="text-sm font-semibold text-foreground">
                    {getNodeKindLabel(section.kind, messages)}
                  </h4>
                </div>
                <Badge variant="outline">{formatNumber(section.nodes.length)}</Badge>
              </div>

              <div className="grid gap-2">
                {section.nodes.map((node) => (
                  <MobileNodeCard
                    key={node.id}
                    node={node}
                    nodeById={nodeById}
                    onActivateNode={onActivateNode}
                    onInspectNode={onInspectNode}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      }
    >
      <RoutingDiagramLegend />
    </RoutingDiagramVisualizationShell>
  );
}

function MobileNodeCard({
  node,
  nodeById,
  onActivateNode,
  onInspectNode,
}: {
  node: RoutingDiagramMobileNode;
  nodeById: Map<string, RoutingDiagramMobileNode>;
  onActivateNode: (node: RoutingDiagramNode) => void;
  onInspectNode: (node: RoutingDiagramNode) => void;
}) {
  const { formatNumber, messages } = useLocale();
  const actionText =
    node.kind === "model"
      ? messages.modelsUi.viewModelDetails(node.label)
      : node.kind === "endpoint"
        ? messages.modelDetail.viewRequestLogs
        : null;
  const actionLabel = getNodeActionLabel(node, messages);
  const incomingGroups = groupRelationsByKind(node.incoming);
  const outgoingGroups = groupRelationsByKind(node.outgoing);
  const nodeVisual = getRoutingDiagramNodeVisualMetadata(node.kind);
  const muted = isRoutingDiagramMutedNode(node);

  return (
    <article
      className={cn(
        "cursor-pointer border border-outline-variant p-3 transition-colors hover:border-outline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
        nodeVisual.shapeClassName,
        muted && "border-dashed opacity-70",
      )}
      data-node-shape={nodeVisual.shape}
      style={getRoutingDiagramNodeCardStyle(nodeVisual, muted)}
      data-testid={`routing-diagram-list-node-${node.id}`}
      role="button"
      tabIndex={0}
      aria-label={node.label}
      onClick={() => onInspectNode(node)}
      onKeyDown={(event) => {
        if (event.target !== event.currentTarget) {
          return;
        }
        if (isActivationKey(event)) {
          event.preventDefault();
          onInspectNode(node);
        }
      }}
    >
      <div className="flex flex-col gap-3">
        <div className="grid gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <h5 className="min-w-0 text-sm font-medium text-foreground">{node.label}</h5>
            {getStateBadge(node, messages)}
          </div>
          {node.sublabel ? (
            <p className="break-words text-xs text-muted-foreground">{node.sublabel}</p>
          ) : null}
        </div>

        <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
          {node.kind === "terminal_target" ? (
            <MetricPill>
              {node.active === false ? messages.modelDetail.inactive : messages.modelDetail.active}
            </MetricPill>
          ) : (
            <MetricPill>
              {messages.dashboard.activeTargets(formatNumber(node.activeTerminalTargetCount))}
            </MetricPill>
          )}
          <MetricPill>
            {messages.dashboard.successfulRequests24h(formatNumber(node.successCount24h))}
          </MetricPill>
        </div>

        {actionText ? (
          <Button
            type="button"
            variant="outline"
            size="xs"
            className="w-full justify-center"
            aria-label={actionLabel ?? undefined}
            onClick={(event) => {
              event.stopPropagation();
              onActivateNode(node);
            }}
          >
            {actionText}
          </Button>
        ) : null}

        <div className="grid gap-2">
          {incomingGroups.map(([kind, relations]) => (
            <RelationGroup
              key={`incoming-${node.id}-${kind}`}
              arrow="←"
              kind={kind}
              nodeById={nodeById}
              onActivateNode={onActivateNode}
              relationIds={relations.map((relation) => relation.nodeId)}
            />
          ))}
          {outgoingGroups.map(([kind, relations]) => (
            <RelationGroup
              key={`outgoing-${node.id}-${kind}`}
              arrow="→"
              kind={kind}
              nodeById={nodeById}
              onActivateNode={onActivateNode}
              relationIds={relations.map((relation) => relation.nodeId)}
            />
          ))}
        </div>
      </div>
    </article>
  );
}

function RelationGroup({
  arrow,
  kind,
  nodeById,
  onActivateNode,
  relationIds,
}: {
  arrow: string;
  kind: RoutingDiagramNodeKind;
  nodeById: Map<string, RoutingDiagramMobileNode>;
  onActivateNode: (node: RoutingDiagramNode) => void;
  relationIds: string[];
}) {
  const { messages } = useLocale();

  return (
    <div className="grid gap-1.5 rounded-lg bg-surface-container-low p-2.5">
      <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">
        {arrow} {getNodeKindLabel(kind, messages)}
      </p>
      <div className="flex flex-wrap gap-2">
        {relationIds.map((relationId) => {
          const relatedNode = nodeById.get(relationId);
          if (!relatedNode) {
            return null;
          }

          const interactive = relatedNode.kind === "model" || relatedNode.kind === "endpoint";
          if (interactive) {
            const actionLabel = getNodeActionLabel(relatedNode, messages);

            return (
              <Button
                key={relatedNode.id}
                type="button"
                variant="outline"
                size="xs"
                className="h-auto max-w-full justify-start whitespace-normal px-2.5 py-1 text-left"
                aria-label={actionLabel ?? undefined}
                onClick={(event) => {
                  event.stopPropagation();
                  onActivateNode(relatedNode);
                }}
              >
                {relatedNode.label}
              </Button>
            );
          }

          return <MetricPill key={relatedNode.id}>{relatedNode.label}</MetricPill>;
        })}
      </div>
    </div>
  );
}

function MetricPill({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex max-w-full items-center rounded-full border border-outline-variant bg-surface px-2.5 py-1 leading-tight text-foreground">
      {children}
    </span>
  );
}

function NodeKindDot({ kind }: { kind: RoutingDiagramNodeKind }) {
  const nodeVisual = getRoutingDiagramNodeVisualMetadata(kind);

  return (
    <span
      className={cn("h-2.5 w-2.5 shrink-0 border", nodeVisual.markerClassName)}
      data-node-shape={nodeVisual.shape}
      style={{
        backgroundColor: nodeVisual.color,
        borderColor: "transparent",
        clipPath: nodeVisual.markerClipPath,
        opacity: 0.9,
      }}
      aria-hidden="true"
    />
  );
}

function getNodeActionLabel(
  node: Pick<RoutingDiagramNode, "kind" | "label">,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  if (node.kind === "model") {
    return messages.modelsUi.viewModelDetails(node.label);
  }

  if (node.kind === "endpoint") {
    return `${messages.modelDetail.viewRequestLogs}: ${node.label}`;
  }

  return null;
}

function getStateBadge(
  node: RoutingDiagramMobileNode,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  if (node.kind === "model" && node.status === "disabled") {
    return <Badge variant="outline">{messages.modelDetail.disabled}</Badge>;
  }

  if (node.kind === "terminal_target") {
    return (
      <Badge variant="outline">{node.active === false ? messages.modelDetail.inactive : messages.modelDetail.active}</Badge>
    );
  }

  return null;
}

function getNodeKindLabel(
  kind: RoutingDiagramNodeKind,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  if (kind === "model") {
    return messages.dashboard.routingModelNodeType;
  }

  if (kind === "terminal_target") {
    return messages.modelDetail.connections;
  }

  return messages.dashboard.routingEndpointNodeType;
}

function groupRelationsByKind(
  relations: RoutingDiagramMobileNode["incoming"],
): Array<[RoutingDiagramNodeKind, RoutingDiagramMobileNode["incoming"]]> {
  const grouped = new Map<RoutingDiagramNodeKind, RoutingDiagramMobileNode["incoming"]>();

  for (const relation of relations) {
    const existing = grouped.get(relation.nodeKind) ?? [];
    existing.push(relation);
    grouped.set(relation.nodeKind, existing);
  }

  return Array.from(grouped.entries());
}

function isActivationKey(event: KeyboardEvent<HTMLElement>): boolean {
  return event.key === "Enter" || event.key === " ";
}

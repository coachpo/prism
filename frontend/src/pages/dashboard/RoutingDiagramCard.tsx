import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import {
  filterRoutingDiagramGraphByModelIds,
  getRoutingDiagramEmptyState,
  getRoutingDiagramGraph,
  getRoutingDiagramMobileData,
  RoutingDiagramFlow,
  RoutingDiagramMobileList,
  type RoutingDiagramData,
  type RoutingDiagramNode,
} from "./routingDiagram";
import { useLocale } from "@/i18n/useLocale";
import { RoutingDiagramShell } from "./RoutingDiagramShell";

interface RoutingDiagramCardProps {
  data: RoutingDiagramData | null;
  loading: boolean;
  error: string | null;
  onSelectModel: (modelConfigId: number) => void;
  onDrillDownRequests?: (params: { endpoint_id?: number; model_id?: string }) => void;
}

type RoutingDiagramModelFilterOption = Pick<RoutingDiagramNode, "id" | "label" | "sublabel">;

export function RoutingDiagramCard({
  data,
  loading,
  error,
  onSelectModel,
  onDrillDownRequests,
}: RoutingDiagramCardProps) {
  const { messages } = useLocale();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [containerWidth, setContainerWidth] = useState(0);
  const [hiddenModelIds, setHiddenModelIds] = useState<ReadonlySet<string>>(() => new Set());
  const [viewportHeight, setViewportHeight] = useState(() =>
    typeof window === "undefined" ? 0 : window.innerHeight,
  );

  useLayoutEffect(() => {
    const element = containerRef.current;
    if (!element) {
      return;
    }

    const updateMeasurements = () => {
      setContainerWidth(element.getBoundingClientRect().width);
      setViewportHeight(window.innerHeight);
    };

    updateMeasurements();
    window.addEventListener("resize", updateMeasurements);

    if (typeof ResizeObserver === "undefined") {
      return () => window.removeEventListener("resize", updateMeasurements);
    }

    const observer = new ResizeObserver(() => {
      updateMeasurements();
    });

    observer.observe(element);
    return () => {
      window.removeEventListener("resize", updateMeasurements);
      observer.disconnect();
    };
  }, []);

  const hasMeasuredContainer = containerWidth > 0;
  const isCompact = hasMeasuredContainer && containerWidth < 640;
  const chartHeight = isCompact ? 320 : Math.max(760, viewportHeight - 120);

  const graphData = useMemo(() => {
    return data ? getRoutingDiagramGraph(data) : { nodes: [], edges: [] };
  }, [data]);

  const modelFilterOptions = useMemo<RoutingDiagramModelFilterOption[]>(() => {
    return graphData.nodes
      .filter((node) => node.kind === "model")
      .map((node) => ({
        id: node.id,
        label: node.label,
        sublabel: node.sublabel,
      }));
  }, [graphData.nodes]);

  const selectedModelIds = useMemo(() => {
    return new Set(
      modelFilterOptions
        .filter((option) => !hiddenModelIds.has(option.id))
        .map((option) => option.id),
    );
  }, [hiddenModelIds, modelFilterOptions]);

  const filteredGraphData = useMemo(() => {
    return filterRoutingDiagramGraphByModelIds(graphData, selectedModelIds);
  }, [graphData, selectedModelIds]);

  const mobileData = useMemo(() => {
    return getRoutingDiagramMobileData(filteredGraphData);
  }, [filteredGraphData]);

  const modelFilterActive = selectedModelIds.size < modelFilterOptions.length;

  const emptyState = useMemo(() => {
    if (!data) {
      return null;
    }

    if (modelFilterOptions.length > 0 && filteredGraphData.nodes.length === 0) {
      return {
        title: messages.dashboard.routingFilteredEmptyTitle,
        description: messages.dashboard.routingFilteredEmptyDescription,
      };
    }

    const baseEmptyState = getRoutingDiagramEmptyState(data);
    if (baseEmptyState.kind === "no_active_routes") {
      return {
        title: messages.dashboard.routingNoActiveRoutes,
        description: messages.dashboard.routingNoActiveRoutesDescription,
      };
    }

    return {
      title: messages.dashboard.routingNoRecentTraffic,
      description: messages.dashboard.routingNoRecentTrafficDescription,
    };
  }, [
    data,
    filteredGraphData.nodes.length,
    messages.dashboard.routingFilteredEmptyDescription,
    messages.dashboard.routingFilteredEmptyTitle,
    messages.dashboard.routingNoActiveRoutes,
    messages.dashboard.routingNoActiveRoutesDescription,
    messages.dashboard.routingNoRecentTraffic,
    messages.dashboard.routingNoRecentTrafficDescription,
    modelFilterOptions.length,
  ]);

  const hasChartContent = modelFilterActive
    ? filteredGraphData.nodes.length > 0
    : graphData.nodes.length > 0 && graphData.edges.length > 0;

  const toggleModelFilter = useCallback((modelId: string, checked: boolean) => {
    setHiddenModelIds((current) => {
      const next = new Set(current);
      if (checked) {
        next.delete(modelId);
      } else {
        next.add(modelId);
      }
      return next;
    });
  }, []);

  const headerContent = useMemo(() => {
    if (modelFilterOptions.length === 0) {
      return null;
    }

    return (
      <RoutingDiagramModelFilter
        copy={{
          description: messages.dashboard.routingModelFilterDescription,
          label: messages.dashboard.routingModelFilterLabel,
        }}
        options={modelFilterOptions}
        selectedModelIds={selectedModelIds}
        onToggleModel={toggleModelFilter}
      />
    );
  }, [
    messages.dashboard.routingModelFilterDescription,
    messages.dashboard.routingModelFilterLabel,
    modelFilterOptions,
    selectedModelIds,
    toggleModelFilter,
  ]);

  const activateNode = useCallback(
    (node: RoutingDiagramNode) => {
      if (node.kind === "model" && node.modelConfigId !== null) {
        onSelectModel(node.modelConfigId);
      }
      if (node.kind === "endpoint" && node.endpointId !== null && onDrillDownRequests) {
        onDrillDownRequests({ endpoint_id: node.endpointId });
      }
    },
    [onDrillDownRequests, onSelectModel],
  );

  return (
    <div ref={containerRef}>
      <RoutingDiagramShell
        chartContent={
          data && hasChartContent ? (
            isCompact ? (
              <RoutingDiagramMobileList mobileData={mobileData} onActivateNode={activateNode} />
            ) : hasMeasuredContainer ? (
              <RoutingDiagramFlow
                graphData={filteredGraphData}
                chartHeight={chartHeight}
                onActivateNode={activateNode}
              />
            ) : (
              <div
                data-testid="routing-diagram-desktop-pending"
                className="w-full rounded-xl border border-border/70 bg-background/60"
                style={{ height: chartHeight }}
                aria-hidden="true"
              />
            )
          ) : null
        }
        emptyState={
          emptyState
            ? {
                description: emptyState.description,
                title: emptyState.title,
              }
            : undefined
        }
        error={error}
        headerContent={headerContent}
        loading={loading}
      />
    </div>
  );
}

function RoutingDiagramModelFilter({
  copy,
  onToggleModel,
  options,
  selectedModelIds,
}: {
  copy: {
    description: string;
    label: string;
  };
  onToggleModel: (modelId: string, checked: boolean) => void;
  options: RoutingDiagramModelFilterOption[];
  selectedModelIds: ReadonlySet<string>;
}) {
  return (
    <FieldSet className="rounded-xl border border-border/70 bg-muted/20 p-3">
      <FieldLegend className="mb-1 text-sm">{copy.label}</FieldLegend>
      <FieldDescription>{copy.description}</FieldDescription>
      <FieldGroup data-slot="checkbox-group" className="flex-row flex-wrap gap-2 pt-1">
        {options.map((option) => {
          const inputId = `routing-model-filter-${option.id}`;
          const checked = selectedModelIds.has(option.id);

          return (
            <Field
              key={option.id}
              orientation="horizontal"
              className="w-auto items-center gap-2 rounded-full border border-border/70 bg-background/70 px-3 py-2"
            >
              <Checkbox
                id={inputId}
                checked={checked}
                onCheckedChange={(nextChecked) => onToggleModel(option.id, nextChecked === true)}
              />
              <FieldContent className="min-w-0 gap-0.5">
                <FieldLabel htmlFor={inputId} className="max-w-48 truncate text-sm">
                  {option.label}
                </FieldLabel>
                {option.sublabel ? (
                  <FieldDescription className="max-w-48 truncate text-xs">
                    {option.sublabel}
                  </FieldDescription>
                ) : null}
              </FieldContent>
            </Field>
          );
        })}
      </FieldGroup>
    </FieldSet>
  );
}

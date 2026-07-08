import { useCallback, useMemo, useState } from "react";
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
  const [hiddenModelIds, setHiddenModelIds] = useState<ReadonlySet<string>>(() => new Set());

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
    <RoutingDiagramShell
      chartContent={
        data && hasChartContent ? (
          <RoutingDiagramMobileList mobileData={mobileData} onActivateNode={activateNode} />
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
    <FieldSet className="min-w-0 max-w-full gap-2 [min-inline-size:0]">
      <FieldLegend className="mb-1 text-sm">{copy.label}</FieldLegend>
      <FieldDescription className="max-w-full text-xs">{copy.description}</FieldDescription>
      <FieldGroup data-slot="checkbox-group" className="min-w-0 max-w-full flex-row flex-wrap gap-1.5 pt-0 [min-inline-size:0]">
        {options.map((option) => {
          const inputId = `routing-model-filter-${option.id}`;
          const checked = selectedModelIds.has(option.id);

          return (
            <Field
              key={option.id}
              orientation="horizontal"
              className="w-full min-w-0 max-w-full overflow-hidden [min-inline-size:0] sm:w-auto sm:flex-[1_1_9rem]"
            >
              <Checkbox
                id={inputId}
                checked={checked}
                onCheckedChange={(nextChecked) => onToggleModel(option.id, nextChecked === true)}
              />
              <FieldContent className="min-w-0 gap-0 overflow-hidden">
                <FieldLabel htmlFor={inputId} className="block w-full max-w-full truncate text-xs font-medium">
                  {option.label}
                </FieldLabel>
                {option.sublabel ? (
                  <FieldDescription className="w-full max-w-full truncate text-[11px] leading-tight">
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

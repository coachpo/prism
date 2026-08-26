import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import {
  fetchModelExportSource,
} from "@/lib/api/modelExport";
import type {
  ExportPlatform,
  ExportSourceResponse,
} from "./exportTypes";

type ModelExportMetadataFilter = "all" | "complete" | "incomplete";

interface ExportSelectionState {
  platform: ExportPlatform;
  sourceDigest: string | null;
  ids: Set<number>;
  defaultModelConfigId?: number;
}

function useExportSelection(
  platform: ExportPlatform,
  source: ExportSourceResponse | undefined,
  selectableIds: Set<number>,
) {
  const [state, setState] = useState<ExportSelectionState>({
    platform,
    sourceDigest: null,
    ids: new Set(),
  });

  if (
    source?.platform === platform &&
    (state.platform !== platform || state.sourceDigest !== source.source_digest)
  ) {
    const adoptDefaults =
      state.platform !== platform || state.sourceDigest === null;
    const ids = adoptDefaults
      ? new Set(
          source.models
            .filter((model) => model.default_selected)
            .map((model) => model.model_config_id),
        )
      : new Set([...state.ids].filter((id) => selectableIds.has(id)));
    const next: ExportSelectionState = {
      platform,
      sourceDigest: source.source_digest,
      ids,
      defaultModelConfigId:
        state.defaultModelConfigId !== undefined &&
        ids.has(state.defaultModelConfigId)
          ? state.defaultModelConfigId
          : undefined,
    };
    setState(next);
    return [next, setState] as const;
  }

  return [state, setState] as const;
}

export function useModelExportSource() {
  const [platform, setPlatform] = useState<ExportPlatform>("pi");
  const [searchText, setSearchText] = useState("");
  const [familyFilter, setFamilyFilter] = useState("all");
  const [metadataFilter, setMetadataFilter] =
    useState<ModelExportMetadataFilter>("all");
  // This filter is a visibility and batch-selection aid only. It never
  // changes the operator's existing selection.
  const [priceCompleteOnly, setPriceCompleteOnly] = useState(false);

  const sourceQuery = useQuery<ExportSourceResponse>({
    queryKey: ["model-export-source", platform],
    queryFn: ({ signal }) => fetchModelExportSource(platform, signal),
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: "always",
  });
  const models = useMemo(
    () => sourceQuery.data?.models ?? [],
    [sourceQuery.data],
  );
  const selectableIds = useMemo(
    () =>
      new Set(models.filter((model) => model.selectable).map((model) => model.model_config_id)),
    [models],
  );
  // First source load adopts backend defaults. A new digest on the same
  // platform intersects the existing selection with still-selectable rows.
  const [selection, setSelection] = useExportSelection(
    platform,
    sourceQuery.data,
    selectableIds,
  );
  const selectedIds = selection.ids;

  const updateSelectedIds = useCallback(
    (update: (current: Set<number>) => Set<number>) => {
      setSelection((current) => {
        const ids = update(current.ids);
        return {
          ...current,
          ids,
          defaultModelConfigId:
            current.defaultModelConfigId !== undefined &&
            ids.has(current.defaultModelConfigId)
              ? current.defaultModelConfigId
              : undefined,
        };
      });
    },
    [setSelection],
  );

  const handlePlatformSwitch = useCallback(
    (next: ExportPlatform) => {
      if (next === platform) return;
      setSelection({
        platform: next,
        sourceDigest: null,
        ids: new Set(),
      });
      setPlatform(next);
    },
    [platform, setSelection],
  );

  const visibleModels = useMemo(() => {
    const needle = searchText.trim().toLowerCase();
    return models.filter((model) => {
      const mergedName = model.merged_metadata.name;
      const searchable = [
        model.model_id,
        model.display_name,
        typeof mergedName === "string" ? mergedName : null,
      ]
        .filter((value): value is string => typeof value === "string")
        .join(" ")
        .toLowerCase();
      if (needle && !searchable.includes(needle)) return false;
      if (familyFilter !== "all" && model.api_family !== familyFilter)
        return false;
      const fieldStates = Object.values(
        model.platform_completeness.metadata_fields,
      );
      const metadataComplete =
        fieldStates.length > 0
          ? fieldStates.every(Boolean)
          : model.missing_metadata.length === 0;
      if (metadataFilter === "complete" && !metadataComplete) return false;
      if (metadataFilter === "incomplete" && metadataComplete) return false;
      if (priceCompleteOnly && !model.price_risk.exportable) return false;
      return true;
    });
  }, [familyFilter, metadataFilter, models, priceCompleteOnly, searchText]);

  const selectedModels = useMemo(
    () => models.filter((model) => selectedIds.has(model.model_config_id)),
    [models, selectedIds],
  );
  const selectedRiskSummary = useMemo(() => {
    let metadataIncomplete = 0;
    let costOmitted = 0;
    let enrichmentUnavailable = 0;
    for (const model of selectedModels) {
      const fieldStates = Object.values(
        model.platform_completeness.metadata_fields,
      );
      const metadataComplete =
        fieldStates.length > 0
          ? fieldStates.every(Boolean)
          : model.missing_metadata.length === 0;
      if (!metadataComplete) metadataIncomplete += 1;
      if (!model.price_risk.exportable) costOmitted += 1;
      if ((model.warnings ?? []).includes("enrichment_unavailable")) {
        enrichmentUnavailable += 1;
      }
    }
    return { metadataIncomplete, costOmitted, enrichmentUnavailable };
  }, [selectedModels]);

  const toggleModel = useCallback(
    (id: number, checked: boolean) => {
      updateSelectedIds((current) => {
        const next = new Set(current);
        if (checked) next.add(id);
        else next.delete(id);
        return next;
      });
    },
    [updateSelectedIds],
  );

  const batchSelectVisible = useCallback(() => {
    updateSelectedIds((current) => {
      const next = new Set(current);
      for (const model of visibleModels) {
        if (
          model.selectable &&
          (!priceCompleteOnly || model.price_risk.exportable)
        ) {
          next.add(model.model_config_id);
        }
      }
      return next;
    });
  }, [priceCompleteOnly, updateSelectedIds, visibleModels]);

  const batchClearVisible = useCallback(() => {
    updateSelectedIds((current) => {
      const next = new Set(current);
      for (const model of visibleModels) next.delete(model.model_config_id);
      return next;
    });
  }, [updateSelectedIds, visibleModels]);

  const setDefaultModelConfigId = useCallback(
    (value: number | undefined) => {
      setSelection((current) => ({
        ...current,
        defaultModelConfigId: value,
      }));
    },
    [setSelection],
  );

  return {
    batchClearVisible,
    batchSelectVisible,
    defaultModelConfigId: selection.defaultModelConfigId,
    familyFilter,
    handlePlatformSwitch,
    metadataFilter,
    models,
    platform,
    priceCompleteOnly,
    selectedCount: selectedIds.size,
    selectedIds,
    selectedModels,
    selectedRiskSummary,
    setDefaultModelConfigId,
    setFamilyFilter,
    setMetadataFilter,
    setPriceCompleteOnly,
    setSearchText,
    sourceQuery,
    toggleModel,
    visibleModels,
    searchText,
  };
}

export type ModelExportSourceState = ReturnType<typeof useModelExportSource>;

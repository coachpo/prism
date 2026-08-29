import { useCallback, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";

import {
  bindModelPi,
  clearModelPiOverride,
  fetchModelExportSource,
  putModelPiOverride,
  refreshModelPiCommit,
  refreshModelPiPreview,
  unbindModelPi,
  type PiOverrideFieldValue,
} from "@/lib/api/modelExport";
import type { ExportSourceModelRow, PiRefreshPreviewResponse } from "./exportTypes";

type ModelExportMetadataFilter = "all" | "complete" | "incomplete";

export function useModelExportSource() {
  const [searchText, setSearchText] = useState("");
  const [familyFilter, setFamilyFilter] = useState("all");
  const [metadataFilter, setMetadataFilter] =
    useState<ModelExportMetadataFilter>("all");
  const [priceCompleteOnly, setPriceCompleteOnly] = useState(false);

  const sourceQuery = useQuery({
    queryKey: ["model-export-source"],
    queryFn: ({ signal }: { signal?: AbortSignal }) =>
      fetchModelExportSource(signal),
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: "always",
  });

  const models = useMemo(
    () => sourceQuery.data?.models ?? [],
    [sourceQuery.data],
  );
  const catalog = sourceQuery.data?.catalog;

  const selectableIds = useMemo(
    () =>
      new Set(
        models
          .filter((m: ExportSourceModelRow) => m.selectable)
          .map((m: ExportSourceModelRow) => m.model_config_id),
      ),
    [models],
  );

  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());

  // Sync default selection on source load: adopt every selectable model,
  // then keep the selection intersected with what stays selectable.
  const sourceDigest = sourceQuery.data?.source_digest ?? null;
  const [prevDigest, setPrevDigest] = useState<string | null>(null);
  if (sourceDigest && prevDigest !== sourceDigest) {
    setPrevDigest(sourceDigest);
    if (prevDigest === null) {
      setSelectedIds(new Set(selectableIds));
    } else {
      setSelectedIds(
        (prev: Set<number>) =>
          new Set([...prev].filter((id: number) => selectableIds.has(id))),
      );
    }
  }

  const updateSelectedIds = useCallback(
    (update: (current: Set<number>) => Set<number>) => {
      setSelectedIds((current) => update(new Set(current)));
    },
    [],
  );

  const visibleModels = useMemo(() => {
    const needle = searchText.trim().toLowerCase();
    return models.filter((model) => {
      const mergedName = (model.merged_metadata as Record<string, unknown>)
        ?.name;
      const searchable = [
        model.model_id,
        model.display_name,
        typeof mergedName === "string" ? mergedName : null,
      ]
        .filter((v): v is string => typeof v === "string")
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
    () => models.filter((m) => selectedIds.has(m.model_config_id)),
    [models, selectedIds],
  );

  const selectedRiskSummary = useMemo(() => {
    let metadataIncomplete = 0;
    let costOmitted = 0;
    let unbound = 0;
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
      if (model.pi_binding_status !== "bound") unbound += 1;
    }
    return { metadataIncomplete, costOmitted, unbound };
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
        )
          next.add(model.model_config_id);
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

  const refetchSource = sourceQuery.refetch;

  const bindMutation = useMutation({
    mutationFn: (input: {
      modelConfigId: number;
      providerId?: string;
      catalogModelId?: string;
      expectedCatalogRevision: string;
    }) =>
      bindModelPi(input.modelConfigId, {
        provider_id: input.providerId,
        catalog_model_id: input.catalogModelId,
        expected_catalog_revision: input.expectedCatalogRevision,
      }),
    onSuccess: () => void refetchSource(),
  });

  const refreshPreviewMutation = useMutation<
    PiRefreshPreviewResponse,
    unknown,
    { modelConfigId: number }
  >({
    mutationFn: (input) => refreshModelPiPreview(input.modelConfigId),
  });

  const refreshCommitMutation = useMutation({
    mutationFn: (input: {
      modelConfigId: number;
      expectedCatalogRevision: string;
    }) =>
      refreshModelPiCommit(input.modelConfigId, input.expectedCatalogRevision),
    onSuccess: () => void refetchSource(),
  });

  const overrideMutation = useMutation({
    mutationFn: (input: {
      modelConfigId: number;
      fields: Record<string, PiOverrideFieldValue>;
    }) => putModelPiOverride(input.modelConfigId, input.fields),
    onSuccess: () => void refetchSource(),
  });

  const clearOverrideMutation = useMutation({
    mutationFn: (input: { modelConfigId: number }) =>
      clearModelPiOverride(input.modelConfigId),
    onSuccess: () => void refetchSource(),
  });

  const unbindMutation = useMutation({
    mutationFn: (input: { modelConfigId: number }) =>
      unbindModelPi(input.modelConfigId),
    onSuccess: () => void refetchSource(),
  });

  return {
    batchClearVisible,
    batchSelectVisible,
    bindMutation,
    catalog,
    clearOverrideMutation,
    familyFilter,
    metadataFilter,
    models,
    overrideMutation,
    priceCompleteOnly,
    refreshCommitMutation,
    refreshPreviewMutation,
    selectedCount: selectedIds.size,
    selectedIds,
    selectedModels,
    selectedRiskSummary,
    setFamilyFilter,
    setMetadataFilter,
    setPriceCompleteOnly,
    setSearchText,
    sourceQuery,
    toggleModel,
    unbindMutation,
    visibleModels,
    searchText,
  };
}

export type ModelExportSourceState = ReturnType<typeof useModelExportSource>;

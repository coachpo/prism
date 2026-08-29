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

export class ModelExportSourceReconciliationError extends Error {
  readonly sourceError: unknown;

  constructor(sourceError: unknown) {
    super("model export source reconciliation failed");
    this.name = "ModelExportSourceReconciliationError";
    this.sourceError = sourceError;
  }
}

export function isModelExportSourceReconciliationError(
  error: unknown,
): error is ModelExportSourceReconciliationError {
  return error instanceof ModelExportSourceReconciliationError;
}

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
      const metadataComplete = model.missing_metadata.length === 0;
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
      const metadataComplete = model.missing_metadata.length === 0;
      if (!metadataComplete) metadataIncomplete += 1;
      if (!model.price_risk.exportable) costOmitted += 1;
      if (!model.pi_binding_renderable) unbound += 1;
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
  const reconcileSource = useCallback(async () => {
    try {
      const result = await refetchSource();
      if (result.isError) throw result.error;
    } catch (error) {
      throw new ModelExportSourceReconciliationError(error);
    }
  }, [refetchSource]);

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
    onSuccess: reconcileSource,
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
      expected: {
        provider_id: string;
        catalog_model_id: string;
        api: string;
        binding_updated_at: string;
        catalog_revision: string;
      };
    }) =>
      refreshModelPiCommit(input.modelConfigId, input.expected),
    onSuccess: reconcileSource,
  });

  const overrideMutation = useMutation({
    mutationFn: (input: {
      modelConfigId: number;
      fields: Record<string, PiOverrideFieldValue>;
    }) => putModelPiOverride(input.modelConfigId, input.fields),
    onSuccess: reconcileSource,
  });

  const clearOverrideMutation = useMutation({
    mutationFn: (input: { modelConfigId: number }) =>
      clearModelPiOverride(input.modelConfigId),
    onSuccess: reconcileSource,
  });

  const unbindMutation = useMutation({
    mutationFn: (input: { modelConfigId: number }) =>
      unbindModelPi(input.modelConfigId),
    onSuccess: reconcileSource,
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
    sourceActionsBlocked: sourceQuery.isFetching || sourceQuery.isError,
    toggleModel,
    unbindMutation,
    visibleModels,
    searchText,
  };
}

export type ModelExportSourceState = ReturnType<typeof useModelExportSource>;

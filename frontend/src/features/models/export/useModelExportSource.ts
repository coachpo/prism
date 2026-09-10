import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import type { ExportSourceModelRow, ExportSourceResponse } from "@/lib/types";
import {
  piViewFromExportRow,
  type PiCatalogModelView,
} from "@/features/models/catalog/pi/usePiBindingController";

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

/**
 * Host adapter for the export page: it owns only the authoritative source
 * query, batch selection, filters, and per-row Pi views for the shared
 * binding dialogs. Every Pi mutation and the paged directory search live in
 * the shared {@link PiBindingController}; after each successful mutation the
 * controller calls this host's `reconcile()` — the whole source snapshot is
 * re-read authoritatively and the operator's selection is intersected with
 * what stays selectable. No single-model GETs are issued per row (no N+1).
 */
export function useModelExportSource() {
  const [searchText, setSearchText] = useState("");
  const [familyFilter, setFamilyFilter] = useState("all");
  const [metadataFilter, setMetadataFilter] =
    useState<ModelExportMetadataFilter>("all");
  const [priceCompleteOnly, setPriceCompleteOnly] = useState(false);

  const sourceQuery = useQuery({
    queryKey: ["model-export-source"],
    queryFn: ({ signal }: { signal?: AbortSignal }) =>
      api.modelExport.fetchModelExportSource(signal),
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
          .filter((m: ExportSourceModelRow) => m.direct_request_enabled === true && m.selectable && m.readiness?.status === "ready")
          .map((m: ExportSourceModelRow) => m.model_config_id),
      ),
    [models],
  );

  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());

  // Reconcile every authoritative response, even if a malformed response keeps
  // the old digest but withdraws readiness. Repair never auto-selects new rows.
  const [previousSource, setPreviousSource] = useState<ExportSourceResponse | null>(null);
  const currentSource = sourceQuery.data;
  if (currentSource && previousSource !== currentSource) {
    setPreviousSource(currentSource);
    setSelectedIds((current) => previousSource === null
      ? new Set(selectableIds)
      : new Set([...current].filter((id) => selectableIds.has(id))));
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
      if (model.direct_request_enabled !== true) return false;
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
        if (checked && selectableIds.has(id)) next.add(id);
        else next.delete(id);
        return next;
      });
    },
    [selectableIds, updateSelectedIds],
  );

  const batchSelectVisible = useCallback(() => {
    updateSelectedIds((current) => {
      const next = new Set(current);
      for (const model of visibleModels) {
        if (
          selectableIds.has(model.model_config_id) &&
          (!priceCompleteOnly || model.price_risk.exportable)
        )
          next.add(model.model_config_id);
      }
      return next;
    });
  }, [priceCompleteOnly, selectableIds, updateSelectedIds, visibleModels]);

  const batchClearVisible = useCallback(() => {
    updateSelectedIds((current) => {
      const next = new Set(current);
      for (const model of visibleModels) next.delete(model.model_config_id);
      return next;
    });
  }, [updateSelectedIds, visibleModels]);

  // 把当前选择收敛到给定集合。主操作被某个选中项挡住时，页面用它给出
  // 一键出路，而不是让操作者逐行去猜该取消哪些。
  const retainSelection = useCallback(
    (keep: ReadonlySet<number>) => {
      updateSelectedIds(
        (current) => new Set([...current].filter((id: number) => keep.has(id) && selectableIds.has(id))),
      );
    },
    [selectableIds, updateSelectedIds],
  );

  const refetchSource = sourceQuery.refetch;
  const reconcileSource = useCallback(async () => {
    try {
      const result = await refetchSource();
      if (result.isError) throw result.error;
    } catch (error) {
      throw new ModelExportSourceReconciliationError(error);
    }
  }, [refetchSource]);

  /** Row adapter for the shared Pi binding controller/dialogs. */
  const piViewFor = useCallback(
    (row: ExportSourceModelRow): PiCatalogModelView =>
      piViewFromExportRow({
        model_config_id: row.model_config_id,
        model_id: row.model_id,
        pi_api: row.pi_api,
        pi_candidates: row.pi_candidates,
        pi_selected: row.pi_selected ?? null,
        pi_binding_prism_model_id: row.pi_binding_prism_model_id,
        pi_binding_source: row.pi_binding_source ?? null,
        pi_binding_override: row.pi_binding_override ?? null,
        pi_binding_effective: row.pi_binding_effective ?? null,
        pi_binding_status: row.pi_binding_status,
        pi_binding_renderable: row.pi_binding_renderable,
        sourceCatalogRevision: catalog?.revision ?? "",
        sourceCatalogFresh: catalog?.status === "fresh",
      }),
    [catalog],
  );

  return {
    replaceSelection: (ids: Set<number>) => setSelectedIds(new Set([...ids].filter((id) => selectableIds.has(id)))),
    batchClearVisible,
    batchSelectVisible,
    catalog,
    familyFilter,
    metadataFilter,
    models,
    piViewFor,
    priceCompleteOnly,
    retainSelection,
    selectableIds,
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
    visibleModels,
    searchText,
    reconcileSource,
  };
}

export type ModelExportSourceState = ReturnType<typeof useModelExportSource>;

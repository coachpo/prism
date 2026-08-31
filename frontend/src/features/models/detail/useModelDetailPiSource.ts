import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import {
  piViewFromModelRead,
  type PiCatalogModelView,
} from "@/features/models/catalog/pi/usePiBindingController";

/**
 * Model-detail host adapter for the single-model Pi management read
 * (`GET /api/models/{id}/pi`). It owns only the authoritative query and the
 * narrow view mapping for the shared binding controller/dialogs; every Pi
 * mutation reconciles through this query's refetch. No export snapshot,
 * targets, pricing, digest, or credential is loaded.
 */
export function useModelDetailPiSource(modelConfigId: number | null) {
  const query = useQuery({
    queryKey: ["model-detail-pi", modelConfigId],
    queryFn: () =>
      modelConfigId === null
        ? Promise.reject(new Error("model_config_id missing"))
        : api.modelExport.fetchModelPi(modelConfigId),
    enabled: modelConfigId !== null,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: "always",
  });

  const read = query.data ?? null;
  const readError = query.isError
    ? query.error instanceof Error
      ? query.error.message
      : String(query.error)
    : null;

  const piView: PiCatalogModelView | null = read
    ? piViewFromModelRead({
        model: read.model,
        catalog: read.catalog,
        candidates: read.candidates,
        binding: {
          bound: read.binding.bound,
          provider_id: read.binding.provider_id,
          catalog_model_id: read.binding.catalog_model_id,
          api: read.binding.api,
          prism_model_id_at_bind: read.binding.prism_model_id_at_bind,
          source: read.binding.source,
          override: read.binding.override,
          effective: read.binding.effective,
        },
        binding_status: read.binding_status,
        binding_renderable: read.binding_renderable,
      })
    : null;

  const reconcile = useCallback(async () => {
    const result = await query.refetch();
    if (result.isError) throw result.error;
  }, [query]);

  return {
    query,
    read,
    piView,
    reconcile,
    readPending: query.isPending,
    readRefreshing: query.isFetching,
    readFailed: query.isError,
    readStale: Boolean(read && query.isError),
    readError,
    lastSuccessfulAt:
      read && query.dataUpdatedAt > 0
        ? new Date(query.dataUpdatedAt).toISOString()
        : null,
    // The Pi panel stays inert while its authoritative read is unavailable.
    actionsBlocked: query.isFetching || query.isError,
  };
}
